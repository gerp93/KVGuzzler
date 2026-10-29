package api

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/evcharge"
	"github.com/gerp93/KVGuzzler/server/internal/ford"
	"github.com/gerp93/KVGuzzler/server/internal/model"
)

const (
	// maxEvCharges is how many of the newest charges the list holds.
	maxEvCharges = 200
	// minGainPct is the smallest gain in battery charge that energy per percent is worked out from. The car reports whole-ish percents and a charge
	// that moved the battery by one has an energy per percent that is mostly rounding; preconditioning draws power and gains none at all.
	minGainPct = 2.0
)

// How a charge's battery figures stand, which the dashboard words: the energy per percent is only given for "ok".
const (
	pctActive    = "active"     // still charging
	pctNone      = "none"       // the car was not asked, or did not answer, at the start
	pctWaiting   = "waiting"    // the car has not yet reported after the end
	pctEarly     = "early"      // the car only reported from before the end, so the gain is short by the last of the charge
	pctSmallGain = "small_gain" // the battery gained under minGainPct: nothing to divide by (preconditioning, a top-up)
	pctOK        = "ok"
)

type evCharge struct {
	ID        int64      `json:"id"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
	Active    bool       `json:"active"`
	// DurationSeconds is from the start to the last moment the meter drew power (to now for a charge that goes on).
	DurationSeconds int     `json:"duration_seconds"`
	KWh             float64 `json:"kwh"`
	// Cost is the energy at the price of each local day, or null when no price is known for it.
	Cost          *string  `json:"cost"`
	CostEstimated bool     `json:"cost_estimated"`
	PeakWatts     *float64 `json:"peak_watts"`
	// Coverage is the share of the charge the meter was measured (0 to 1): time it did not answer is not credited, so a low figure means the energy is too low.
	Coverage   float64    `json:"coverage"`
	StartPct   *float64   `json:"start_pct"`
	StartPctAt *time.Time `json:"start_pct_at"`
	EndPct     *float64   `json:"end_pct"`
	EndPctAt   *time.Time `json:"end_pct_at"`
	// GainPct is the battery's gain, whenever both reports exist (it may be short of the real gain; see PctStatus).
	GainPct   *float64 `json:"gain_pct"`
	PctStatus string   `json:"pct_status"`
	// KWhPerPct is the energy at the wall for each percent the battery gained, given only when PctStatus is "ok".
	KWhPerPct *float64 `json:"kwh_per_pct"`
}

// evConsistency is how steady the energy per percent is over the charges that have one.
type evConsistency struct {
	Charges int `json:"charges"`
	// KWhPerPct is the energy of those charges over the gain of those charges (so a bigger charge counts for more than a small one), Lowest and
	// Highest the extremes of single charges.
	KWhPerPct float64 `json:"kwh_per_pct"`
	Lowest    float64 `json:"lowest"`
	Highest   float64 `json:"highest"`
}

type evChargesAnswer struct {
	MeterID string `json:"meter_id"`
	// CarLinked says the battery's charge can be read for this meter (Ford is linked to it); without it charges have energy and cost but no battery figures.
	CarLinked     bool           `json:"car_linked"`
	Charges       []evCharge     `json:"charges"`
	Consistency   *evConsistency `json:"consistency"`
	CostEstimated bool           `json:"cost_estimated"`
}

// EvTracker is the tracker that finds the vehicle's charges; the server's main runs it.
func (s *Server) EvTracker() *evcharge.Tracker {
	s.evOnce.Do(func() {
		live := func(id string) (float64, bool) {
			if s.Collector == nil {
				return 0, false
			}
			for _, r := range s.Collector.LiveReadings() {
				if r.MeterID == id {
					return r.Watts, true
				}
			}
			return 0, false
		}
		s.evTracker = &evcharge.Tracker{Store: s.Store, Live: live, Car: fordCar{s}, Now: s.now}
	})
	return s.evTracker
}

// fordCar reads the battery's charge from Ford for the meter that charges the linked vehicle.
type fordCar struct{ s *Server }

// fordRefused is Ford's complaint about a look, which asks for a long wait: Ford can disable an application that retries hard.
type fordRefused struct{ message string }

func (e fordRefused) Error() string             { return e.message }
func (e fordRefused) RetryAfter() time.Duration { return ford.RetryAfterError }

func (c fordCar) Battery(ctx context.Context, meterID string) (evcharge.Reading, error) {
	if err := c.linked(ctx, meterID); err != nil {
		return evcharge.Reading{}, err
	}
	st, err := c.s.FordFeed().Read(ctx)
	if err != nil {
		return evcharge.Reading{}, err // too soon after the last look, or the server's own trouble
	}
	if !st.OK {
		return evcharge.Reading{}, fordRefused{st.Error}
	}
	if st.Battery == nil {
		return evcharge.Reading{}, fordRefused{"Ford's answer has no battery charge in it (the link may lack the evData permission)."}
	}
	return evcharge.Reading{Percent: st.Battery.Percent, At: st.Battery.At}, nil
}

// evChargeView works a charge's energy, cost and energy per percent out of the meter's minutes and the car's two reports.
func (s *Server) evChargeView(ctx context.Context, m model.Meter, c model.EvCharge, now time.Time) (evCharge, error) {
	out := evCharge{ID: c.ID, StartedAt: c.StartedAt, EndedAt: c.EndedAt, Active: c.EndedAt == nil,
		StartPct: c.StartPct, StartPctAt: c.StartPctAt, EndPct: c.EndPct, EndPctAt: c.EndPctAt}
	end := now
	if c.EndedAt != nil {
		end = *c.EndedAt
	}
	if d := end.Sub(c.StartedAt); d > 0 {
		out.DurationSeconds = int(d.Seconds())
	}
	// Minutes are whole: the charge is the minute it began in through the minute it last drew power in.
	from := c.StartedAt.Truncate(time.Minute)
	to := end.Truncate(time.Minute).Add(time.Minute)
	cfg, err := s.rateConfig(ctx)
	if err != nil {
		return evCharge{}, err
	}
	cs, priced, err := s.meterSummary(ctx, m, from, to, cfg.Schedule(s.Loc))
	if err != nil {
		return evCharge{}, err
	}
	out.KWh, out.Cost, out.PeakWatts = cs.KWh, cs.Cost, cs.PeakWatts
	out.CostEstimated = cs.Cost != nil && priced.Estimated
	if secs := to.Sub(from).Seconds(); secs > 0 {
		out.Coverage = math.Min(1, cs.MonitoredHours*3600/secs)
	}

	switch {
	case out.Active:
		out.PctStatus = pctActive
	case c.StartPct == nil:
		out.PctStatus = pctNone
	case c.EndPct == nil:
		out.PctStatus = pctWaiting
		if now.Sub(*c.EndedAt) >= evcharge.EndGiveUp {
			out.PctStatus = pctNone
		}
	default:
		gain := *c.EndPct - *c.StartPct
		out.GainPct = &gain
		switch {
		case !evcharge.Settled(c):
			out.PctStatus = pctEarly
			if now.Sub(*c.EndedAt) < evcharge.EndGiveUp {
				out.PctStatus = pctWaiting
			}
		case gain < minGainPct:
			out.PctStatus = pctSmallGain
		default:
			out.PctStatus = pctOK
			if out.KWh > 0 {
				v := out.KWh / gain
				out.KWhPerPct = &v
			}
		}
	}
	return out, nil
}

// evCharges answers a charging meter's charges, newest first, with how steady the energy per percent is.
func (s *Server) evCharges(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	m, err := s.Store.Meter(ctx, r.PathValue("meterId"))
	if err != nil {
		evStatus(w, err)
		return
	}
	if !m.EV {
		writeProblem(w, http.StatusConflict, "not_ev", "this meter is not marked as charging an electric vehicle")
		return
	}
	list, err := s.Store.EvCharges(ctx, m.ID, maxEvCharges)
	if err != nil {
		serverError(w, err)
		return
	}
	out := evChargesAnswer{MeterID: m.ID, Charges: []evCharge{}}
	out.CarLinked = fordCar{s}.linked(ctx, m.ID) == nil
	now := s.now()
	var kwh, gain float64
	for _, c := range list {
		v, err := s.evChargeView(ctx, m, c, now)
		if err != nil {
			serverError(w, err)
			return
		}
		out.CostEstimated = out.CostEstimated || v.CostEstimated
		if v.KWhPerPct != nil {
			g := *v.GainPct
			kwh += v.KWh
			gain += g
			if out.Consistency == nil {
				out.Consistency = &evConsistency{Lowest: *v.KWhPerPct, Highest: *v.KWhPerPct}
			}
			out.Consistency.Charges++
			out.Consistency.Lowest = math.Min(out.Consistency.Lowest, *v.KWhPerPct)
			out.Consistency.Highest = math.Max(out.Consistency.Highest, *v.KWhPerPct)
		}
		out.Charges = append(out.Charges, v)
	}
	if out.Consistency != nil && gain > 0 {
		out.Consistency.KWhPerPct = kwh / gain
	}
	writeJSON(w, http.StatusOK, out)
}

// linked is nil when the battery's charge can be asked for this meter: Ford is set up for it and linked. Otherwise evcharge.ErrNoCar (or the
// server's own trouble).
func (c fordCar) linked(ctx context.Context, meterID string) error {
	cfg, err := c.s.loadFordConfig(ctx)
	if err != nil {
		return err
	}
	if cfg == nil || cfg.MeterID != meterID {
		return evcharge.ErrNoCar
	}
	_, ok, err := c.s.Store.Secret(ctx, fordRefreshKey)
	if err == nil && !ok {
		err = evcharge.ErrNoCar
	}
	return err
}

// deleteEvCharge removes a charge that should not be there (a false one). One still going is refused: the tracker would open it again.
func (s *Server) deleteEvCharge(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(r.PathValue("chargeId"), 10, 64)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "unknown_charge", "no such charge")
		return
	}
	meterID := r.PathValue("meterId")
	open, err := s.Store.OpenEvCharge(ctx, meterID)
	if err != nil {
		serverError(w, err)
		return
	}
	if open != nil && open.ID == id {
		writeProblem(w, http.StatusConflict, "charge_active", "this charge is still going: it can be removed once it has ended")
		return
	}
	if err := s.Store.DeleteEvCharge(ctx, meterID, id); err != nil {
		evStatus(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
