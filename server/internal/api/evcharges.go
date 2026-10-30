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
	"github.com/gerp93/KVGuzzler/server/internal/rates"
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
	// BatteryKWh is the energy that reached the battery (the gain times the battery's usable kWh), LossKWh what the wall gave that the battery did not take
	// (the charger, the battery's heat, keeping the battery and cabin at temperature), Efficiency the first over the wall's energy and LossCost the loss at the
	// charge's own price. Null without the battery's size, for a charge with no energy per percent, or for one the meter measured under 95 % of the time.
	BatteryKWh *float64 `json:"battery_kwh"`
	LossKWh    *float64 `json:"loss_kwh"`
	Efficiency *float64 `json:"efficiency"`
	LossCost   *string  `json:"loss_cost"`
}

// evLosses is what charging lost over the charges that have a loss figure.
type evLosses struct {
	Charges    int     `json:"charges"`
	WallKWh    float64 `json:"wall_kwh"`
	BatteryKWh float64 `json:"battery_kwh"`
	LossKWh    float64 `json:"loss_kwh"`
	// Efficiency is the battery's energy over the wall's, over all of those charges (so a big charge counts for more than a small one); Lowest and Highest are
	// the extremes of single charges.
	Efficiency float64 `json:"efficiency"`
	Lowest     float64 `json:"lowest"`
	Highest    float64 `json:"highest"`
	// LossCost is the lost energy's price, null when a charge had no price.
	LossCost *string `json:"loss_cost"`
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
	CarLinked   bool           `json:"car_linked"`
	Charges     []evCharge     `json:"charges"`
	Consistency *evConsistency `json:"consistency"`
	// BatteryKWh is the battery's usable energy as the owner set it (null until set); the loss figures need it.
	BatteryKWh    *float64  `json:"battery_kwh"`
	Losses        *evLosses `json:"losses"`
	CostEstimated bool      `json:"cost_estimated"`
}

// minLossCoverage is the share of a charge the meter must have measured for its loss to be worked out: time it did not answer is not credited, so a charge
// measured less than this has too little wall energy and would look more efficient than it was.
const minLossCoverage = 0.95

// addLoss works a charge's loss out of the battery's usable kWh. It needs the energy per percent, so only a settled charge with a real gain has one.
func addLoss(v *evCharge, capacity float64) {
	if v.KWhPerPct == nil || v.GainPct == nil || v.KWh <= 0 || v.Coverage < minLossCoverage {
		return
	}
	battery := *v.GainPct * capacity / 100
	loss := v.KWh - battery
	eff := battery / v.KWh
	v.BatteryKWh, v.LossKWh, v.Efficiency = &battery, &loss, &eff
	if v.Cost != nil && loss >= 0 {
		if cost, err := rates.ParseMoney(*v.Cost); err == nil {
			c := cost.Mul(rates.MoneyFromFloat(loss / v.KWh)).String()
			v.LossCost = &c
		}
	}
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
	capacity, err := s.evBatteryKWh(ctx, m.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	out.BatteryKWh = capacity
	var kwh, gain float64
	var lossCost rates.Money
	allPriced := true
	for _, c := range list {
		v, err := s.evChargeView(ctx, m, c, now)
		if err != nil {
			serverError(w, err)
			return
		}
		if capacity != nil {
			addLoss(&v, *capacity)
		}
		if v.Efficiency != nil {
			if out.Losses == nil {
				out.Losses = &evLosses{Lowest: *v.Efficiency, Highest: *v.Efficiency}
			}
			l := out.Losses
			l.Charges++
			l.WallKWh += v.KWh
			l.BatteryKWh += *v.BatteryKWh
			l.LossKWh += *v.LossKWh
			l.Lowest, l.Highest = math.Min(l.Lowest, *v.Efficiency), math.Max(l.Highest, *v.Efficiency)
			if v.LossCost != nil {
				if m, err := rates.ParseMoney(*v.LossCost); err == nil {
					lossCost = lossCost.Add(m)
				}
			} else {
				allPriced = false
			}
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
	if l := out.Losses; l != nil {
		l.Efficiency = l.BatteryKWh / l.WallKWh
		if allPriced {
			c := lossCost.String()
			l.LossCost = &c
		}
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
