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
}

type evTotals struct {
	Miles       float64  `json:"miles"`
	KWh         float64  `json:"kwh"`
	Cost        *string  `json:"cost"`
	CostPerMile *string  `json:"cost_per_mile"`
	MilesPerKWh *float64 `json:"miles_per_kwh"`
}

// evSince is what the meter has used since the latest reading, which has no miles yet to divide it by.
type evSince struct {
	FromDay string  `json:"from_day"`
	KWh     float64 `json:"kwh"`
	Cost    *string `json:"cost"`
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

	var miles, kwh float64
	var cost rates.Money
	allPriced := true
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
		miles += iv.Miles
		kwh += cs.KWh
		if cs.Cost == nil {
			allPriced = false
		} else {
			cost = cost.Add(priced.Cost)
		}
		out.Intervals = append(out.Intervals, iv)
	}
	if len(readings) >= 2 {
		t := evTotals{Miles: miles, KWh: kwh}
		if allPriced {
			c := cost.String()
			t.Cost = &c
			if miles > 0 && kwh > 0 {
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
		}
	}
	return out, nil
}
