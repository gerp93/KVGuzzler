package api

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/baseload"
	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/rates"
)

// The standby report: what each appliance draws when it is doing nothing (the always-on power, see package baseload), the biggest first, with a year of it in
// energy and money and how it has moved. The house's own card (baseload.go) lists the top few; this lists them all and compares two stretches of time, so the
// effect of unplugging something (or of something starting to waste power) shows. Each figure is that meter's own floor, never added to the others or to the house's.

const (
	defaultStandbyDays = 30
	maxStandbyDays     = 180
	// maxStandbyParts bounds the list.
	maxStandbyParts = 200
	// standbyNoise: a change smaller than this (in watts) is not a change: a floor is a percentile of a noisy reading.
	standbyNoise = 0.5
)

type standbyPart struct {
	MeterID string  `json:"meter_id"`
	Label   string  `json:"label"`
	Watts   float64 `json:"watts"`
	// PrevWatts is the floor over the stretch before; null when it has too little recorded. ChangeWatts is Watts less that.
	PrevWatts   *float64 `json:"prev_watts"`
	ChangeWatts *float64 `json:"change_watts"`
	KWhYear     float64  `json:"kwh_per_year"`
	// CostYear is a year of it at today's price; ChangeCostYear the change in that. Null with no price (or no earlier floor).
	CostYear       *string `json:"cost_per_year"`
	ChangeCostYear *string `json:"change_cost_per_year"`
}

type standbyAnswer struct {
	// From and To are the stretch the floors are taken over; PrevFrom and PrevTo the stretch they are compared with.
	From     time.Time `json:"from"`
	To       time.Time `json:"to"`
	PrevFrom time.Time `json:"prev_from"`
	PrevTo   time.Time `json:"prev_to"`
	// Change is the day the two stretches are split at, when one was asked for (the day something was unplugged, say); null for the last N days against the N before.
	Change *rates.Date `json:"change"`
	// House is the house's own floor (the mains added) over the same two stretches; null with no mains meter or too little recorded.
	HouseWatts     *float64      `json:"house_watts"`
	HousePrevWatts *float64      `json:"house_prev_watts"`
	Rate           *string       `json:"rate"`
	RateEstimated  bool          `json:"rate_estimated"`
	Parts          []standbyPart `json:"parts"`
}

// floorOf is a meter's (or the mains') always-on power over [from, to), nil when under an hour was recorded.
func (s *Server) floorOf(ctx context.Context, ids []string, from, to time.Time) (*baseload.Result, error) {
	w, err := s.Store.MinuteWatts(ctx, ids, from, to)
	if err != nil {
		return nil, err
	}
	r, ok := baseload.Of(w)
	if !ok {
		return nil, nil
	}
	return &r, nil
}

func (s *Server) standby(w http.ResponseWriter, r *http.Request) {
	days := defaultStandbyDays
	if v := r.URL.Query().Get("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 3 || n > maxStandbyDays {
			writeProblem(w, http.StatusBadRequest, "bad_days", "days is a number from 3 to 180")
			return
		}
		days = n
	}
	now := s.now()
	today := rates.DateOf(now, s.Loc)
	out := standbyAnswer{Parts: []standbyPart{}}
	span := time.Duration(days) * 24 * time.Hour
	if v := r.URL.Query().Get("change"); v != "" {
		day, err := rates.ParseDate(v)
		if err != nil || !day.Before(today) {
			writeProblem(w, http.StatusBadRequest, "bad_change", "change is a day before today, like 2026-10-15")
			return
		}
		at := s.scheduleDayStart(day)
		out.Change = &day
		out.PrevFrom, out.PrevTo = at.Add(-span), at
		out.From, out.To = at, at.Add(span)
		if out.To.After(now) {
			out.To = now
		}
	} else {
		out.To, out.From = now, now.Add(-span)
		out.PrevTo, out.PrevFrom = out.From, out.From.Add(-span)
	}

	on, byID, err := s.enabledMeters(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	cfg, err := s.rateConfig(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	schedule := cfg.Schedule(s.Loc)
	rate := schedule.RateAt(today)
	if rate != nil {
		rs := rate.String()
		out.Rate, out.RateEstimated = &rs, schedule.IsEstimate(today)
	}
	if mains, err := s.mainsIDs(r.Context()); err != nil {
		serverError(w, err)
		return
	} else if len(mains) > 0 {
		if cur, err := s.floorOf(r.Context(), mains, out.From, out.To); err != nil {
			serverError(w, err)
			return
		} else if cur != nil {
			v := cur.Watts
			out.HouseWatts = &v
		}
		if prev, err := s.floorOf(r.Context(), mains, out.PrevFrom, out.PrevTo); err != nil {
			serverError(w, err)
			return
		} else if prev != nil {
			v := prev.Watts
			out.HousePrevWatts = &v
		}
	}

	holds := map[string]bool{}
	for _, m := range on {
		if m.ParentID != nil {
			holds[*m.ParentID] = true
		}
	}
	cost := func(watts float64) *string {
		if rate == nil {
			return nil
		}
		c := rates.MoneyFromFloat(watts * baseload.HoursPerYear / 1000).Mul(*rate).String()
		return &c
	}
	for _, m := range on {
		if m.Scope == model.ScopeMains || holds[m.ID] {
			continue
		}
		cur, err := s.floorOf(r.Context(), []string{m.ID}, out.From, out.To)
		if err != nil {
			serverError(w, err)
			return
		}
		prev, err := s.floorOf(r.Context(), []string{m.ID}, out.PrevFrom, out.PrevTo)
		if err != nil {
			serverError(w, err)
			return
		}
		// Listed when it rests at something worth a line now, or did before (the thing that was unplugged).
		if (cur == nil || cur.Watts < minBasePartWatts) && (prev == nil || prev.Watts < minBasePartWatts) {
			continue
		}
		p := standbyPart{MeterID: m.ID, Label: s.meterLabel(byID, m.ID)}
		if cur != nil {
			p.Watts, p.KWhYear, p.CostYear = cur.Watts, cur.KWhPerYear(), cost(cur.Watts)
		}
		if prev != nil {
			pw := prev.Watts
			p.PrevWatts = &pw
			if cur != nil {
				ch := cur.Watts - prev.Watts
				if ch > -standbyNoise && ch < standbyNoise {
					ch = 0
				}
				p.ChangeWatts = &ch
				if rate != nil {
					cc := rates.MoneyFromFloat(ch * baseload.HoursPerYear / 1000).Mul(*rate).String()
					p.ChangeCostYear = &cc
				}
			}
		}
		out.Parts = append(out.Parts, p)
	}
	sort.Slice(out.Parts, func(i, j int) bool { return out.Parts[i].Watts > out.Parts[j].Watts })
	if len(out.Parts) > maxStandbyParts {
		out.Parts = out.Parts[:maxStandbyParts]
	}
	writeJSON(w, http.StatusOK, out)
}
