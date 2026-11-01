package api

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/rates"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

// evInterval is the stretch between two odometer readings: the miles the car went and what the meter that charges it used over the same days.
// A reading counts as taken at the start of its day, so a stretch runs from the start of one reading's day to the start of the next's.
type evInterval struct {
	FromDay string  `json:"from_day"`
	ToDay   string  `json:"to_day"`
	Miles   float64 `json:"miles"`
	KWh     float64 `json:"kwh"`
	// Cost is the energy at the price of each local day, or null when no price is known for it.
	Cost        *string  `json:"cost"`
	CostPerMile *string  `json:"cost_per_mile"`
	MilesPerKWh *float64 `json:"miles_per_kwh"`
	// Coverage is the share of the stretch the meter was actually measured (0 to 1): time it did not answer is not credited, so a low figure
	// means the cost per mile is too low.
	Coverage      float64 `json:"coverage"`
	CostEstimated bool    `json:"cost_estimated"`
	// BankedPct is how much the battery's charge rose over the stretch (negative when it fell: the miles were driven on energy bought earlier), when the
	// car's reports say what it held at both ends. UsedKWh and UsedCost are then the stretch's energy and cost less the energy that is still in the battery,
	// which is what the miles used; the cost per mile and the efficiency are worked out from them. All null for a stretch the reports do not cover, which
	// keeps the raw figures.
	BankedPct *float64 `json:"banked_pct"`
	UsedKWh   *float64 `json:"used_kwh"`
	UsedCost  *string  `json:"used_cost"`
}

type evTotals struct {
	Miles       float64  `json:"miles"`
	KWh         float64  `json:"kwh"`
	Cost        *string  `json:"cost"`
	CostPerMile *string  `json:"cost_per_mile"`
	MilesPerKWh *float64 `json:"miles_per_kwh"`
	// Adjusted says the totals are over the stretches whose energy was netted against the battery (kwh and cost are then what the miles used): from
	// FromDay, the first of them. Without any such stretch they add up every stretch as it was measured.
	Adjusted bool    `json:"adjusted"`
	FromDay  *string `json:"from_day"`
}

// evSince is what the meter has used since the latest reading, which has no miles yet to divide it by. BankedPct and BankedKWh say how much of it went into the
// battery rather than into driving, when the car's reports cover the time.
type evSince struct {
	FromDay   string   `json:"from_day"`
	KWh       float64  `json:"kwh"`
	Cost      *string  `json:"cost"`
	BankedPct *float64 `json:"banked_pct"`
	BankedKWh *float64 `json:"banked_kwh"`
}

type evAnswer struct {
	MeterID   string                  `json:"meter_id"`
	Readings  []model.OdometerReading `json:"readings"`
	Intervals []evInterval            `json:"intervals"`
	// Totals add up every stretch; null until there are two readings.
	Totals        *evTotals `json:"totals"`
	SinceLast     *evSince  `json:"since_last"`
	CostEstimated bool      `json:"cost_estimated"`
}

var errNotEV = errors.New("not an ev")

// evStatus answers the problem a store error stands for.
func evStatus(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "unknown_meter", "no such meter or reading")
	case errors.Is(err, store.ErrInvalid):
		writeProblem(w, http.StatusBadRequest, "bad_odometer", err.Error())
	default:
		serverError(w, err)
	}
}

// meterEV answers a charging meter's odometer readings with the miles, energy and cost per mile of each stretch between them.
func (s *Server) meterEV(w http.ResponseWriter, r *http.Request) {
	out, err := s.evAnswer(r.Context(), r.PathValue("meterId"))
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, out)
	case errors.Is(err, errNotEV):
		writeProblem(w, http.StatusConflict, "not_ev", "this meter is not marked as charging an electric vehicle")
	default:
		evStatus(w, err)
	}
}

// putOdometer saves the reading of one day and answers like meterEV does.
func (s *Server) putOdometer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Miles *float64 `json:"miles"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil || body.Miles == nil || math.IsNaN(*body.Miles) {
		writeProblem(w, http.StatusBadRequest, "bad_request", `send the odometer reading as {"miles": number}`)
		return
	}
	if err := s.Store.SetOdometer(r.Context(), r.PathValue("meterId"), r.PathValue("day"), *body.Miles); err != nil {
		evStatus(w, err)
		return
	}
	s.meterEV(w, r)
}

func (s *Server) deleteOdometer(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteOdometer(r.Context(), r.PathValue("meterId"), r.PathValue("day")); err != nil {
		evStatus(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) evAnswer(ctx context.Context, id string) (evAnswer, error) {
	m, err := s.Store.Meter(ctx, id)
	if err != nil {
		return evAnswer{}, err
	}
	if !m.EV {
		return evAnswer{}, errNotEV
	}
	readings, err := s.Store.Odometer(ctx, id)
	if err != nil {
		return evAnswer{}, err
	}
	cfg, err := s.rateConfig(ctx)
	if err != nil {
		return evAnswer{}, err
	}
	schedule := cfg.Schedule(s.Loc)
	out := evAnswer{MeterID: id, Readings: readings, Intervals: []evInterval{}}

	day := func(d string) (time.Time, error) { return time.ParseInLocation("2006-01-02", d, s.Loc) }

	// What each stretch measured, kept as it was so the battery can be netted out of it below.
	type measured struct {
		from, to time.Time
		kwh      float64
		cost     rates.Money
		priced   bool
	}
	var raw []measured
	for i := 1; i < len(readings); i++ {
		a, b := readings[i-1], readings[i]
		from, err := day(a.Day)
		if err != nil {
			return evAnswer{}, err
		}
		to, err := day(b.Day)
		if err != nil {
			return evAnswer{}, err
		}
		cs, priced, err := s.meterSummary(ctx, m, from, to, schedule)
		if err != nil {
			return evAnswer{}, err
		}
		iv := evInterval{FromDay: a.Day, ToDay: b.Day, Miles: b.Miles - a.Miles, KWh: cs.KWh, Cost: cs.Cost, CostEstimated: cs.Cost != nil && priced.Estimated}
		if hours := to.Sub(from).Hours(); hours > 0 {
			iv.Coverage = math.Min(1, cs.MonitoredHours/hours)
		}
		if iv.Miles > 0 {
			if cs.KWh > 0 {
				mpk := iv.Miles / cs.KWh
				iv.MilesPerKWh = &mpk
			}
			// No energy recorded for the stretch (the meter was not yet being read) is not a free mile.
			if cs.Cost != nil && cs.KWh > 0 {
				p := priced.Cost.Div(rates.MoneyFromFloat(iv.Miles)).String()
				iv.CostPerMile = &p
			}
		}
		out.CostEstimated = out.CostEstimated || iv.CostEstimated
		out.Intervals = append(out.Intervals, iv)
		raw = append(raw, measured{from: from, to: to, kwh: cs.KWh, cost: priced.Cost, priced: cs.Cost != nil})
	}

	// The energy charged in a stretch is not all driven in it: what the battery gained is still unspent. Taken out where the car's reports cover both ends.
	points, err := s.evSocPoints(ctx, id)
	if err != nil {
		return evAnswer{}, err
	}
	perPct, chargePrice, havePerPct, err := s.wallKWhPerPct(ctx, m)
	if err != nil {
		return evAnswer{}, err
	}
	// A price for energy that went into the battery (or came out of it): the stretch's own when it charged any, else what the charges cost on average.
	priceOf := func(r measured) (rates.Money, bool) {
		if r.priced && r.kwh > 0 {
			return r.cost.Div(rates.MoneyFromFloat(r.kwh)), true
		}
		if chargePrice != nil {
			return *chargePrice, true
		}
		return rates.Money{}, false
	}
	if havePerPct {
		for i := range out.Intervals {
			iv, r := &out.Intervals[i], raw[i]
			pctA, okA := socAt(points, r.from)
			pctB, okB := socAt(points, r.to)
			price, havePrice := priceOf(r)
			if !okA || !okB || !havePrice || !r.priced {
				continue
			}
			banked := pctB - pctA
			bankedKWh := banked * perPct
			usedKWh := r.kwh - bankedKWh
			usedCost := r.cost.Sub(price.MulFloat(bankedKWh))
			c := usedCost.String()
			iv.BankedPct, iv.UsedKWh, iv.UsedCost = &banked, &usedKWh, &c
			iv.CostPerMile, iv.MilesPerKWh = nil, nil
			if iv.Miles > 0 && usedKWh > 0 {
				mpk := iv.Miles / usedKWh
				iv.MilesPerKWh = &mpk
				if usedCost.Sign() > 0 {
					p := usedCost.Div(rates.MoneyFromFloat(iv.Miles)).String()
					iv.CostPerMile = &p
				}
			}
		}
	}

	// The totals: over the stretches netted against the battery when there are any (the others charge energy in one stretch and drive it in another), else
	// over every stretch as measured.
	adjusted := false
	for _, iv := range out.Intervals {
		adjusted = adjusted || iv.UsedCost != nil
	}
	var miles, kwh float64
	var cost rates.Money
	allPriced := true
	var firstDay *string
	for i := range out.Intervals {
		iv := &out.Intervals[i]
		switch {
		case adjusted && iv.UsedCost == nil:
			continue
		case adjusted:
			if firstDay == nil {
				firstDay = &iv.FromDay
			}
			miles += iv.Miles
			kwh += *iv.UsedKWh
			if c, err := rates.ParseMoney(*iv.UsedCost); err == nil {
				cost = cost.Add(c)
			}
		default:
			miles += iv.Miles
			kwh += raw[i].kwh
			if raw[i].priced {
				cost = cost.Add(raw[i].cost)
			} else {
				allPriced = false
			}
		}
	}
	if len(readings) >= 2 {
		t := evTotals{Miles: miles, KWh: kwh, Adjusted: adjusted, FromDay: firstDay}
		if allPriced {
			c := cost.String()
			t.Cost = &c
			if miles > 0 && kwh > 0 && cost.Sign() > 0 {
				p := cost.Div(rates.MoneyFromFloat(miles)).String()
				t.CostPerMile = &p
			}
		}
		if miles > 0 && kwh > 0 {
			mpk := miles / kwh
			t.MilesPerKWh = &mpk
		}
		out.Totals = &t
	}
	if n := len(readings); n >= 1 {
		from, err := day(readings[n-1].Day)
		if err != nil {
			return evAnswer{}, err
		}
		if now := s.now(); now.After(from) {
			cs, _, err := s.meterSummary(ctx, m, from, now, schedule)
			if err != nil {
				return evAnswer{}, err
			}
			out.SinceLast = &evSince{FromDay: readings[n-1].Day, KWh: cs.KWh, Cost: cs.Cost}
			if havePerPct {
				pctA, okA := socAt(points, from)
				pctB, okB := socAt(points, now)
				if okA && okB {
					banked := pctB - pctA
					bankedKWh := banked * perPct
					out.SinceLast.BankedPct, out.SinceLast.BankedKWh = &banked, &bankedKWh
				}
			}
		}
	}
	return out, nil
}
