package api

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/baseload"
	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/rates"
)

type baseLoadAnswer struct {
	// Scope is "house" (the mains legs together) or "meter".
	Scope   string `json:"scope"`
	MeterID string `json:"meter_id,omitempty"`
	// Watts is the power it never goes below (the 5th percentile of its minute averages); AvgWatts the average of the same minutes.
	Watts    float64 `json:"watts"`
	AvgWatts float64 `json:"avg_watts"`
	// Share is the part (0 to 1) of what it uses that is always on.
	Share   float64 `json:"share"`
	Minutes int     `json:"minutes"`
	KWhYear float64 `json:"kwh_per_year"`
	// CostYear is a year of that power at today's price; Rate is that price. Both null with no price saved.
	CostYear      *string   `json:"cost_per_year"`
	Rate          *string   `json:"rate"`
	RateEstimated bool      `json:"rate_estimated"`
	From          time.Time `json:"from"`
	To            time.Time `json:"to"`
	// Parts (house only) is the always-on power of the meters that hold no other meter (the fridge, the workshop, the desk PC), biggest first.
	// Each is that meter's own floor, never added to the house's or to each other: the lowest minutes of different meters need not coincide.
	Parts []basePart `json:"parts,omitempty"`
}

type basePart struct {
	MeterID  string  `json:"meter_id"`
	Label    string  `json:"label"`
	Watts    float64 `json:"watts"`
	KWhYear  float64 `json:"kwh_per_year"`
	CostYear *string `json:"cost_per_year"`
}

// maxBaseParts is how many meters the house's breakdown lists.
const maxBaseParts = 8

// minBasePartWatts: a meter that rests below this is not worth a line.
const minBasePartWatts = 1.0

// houseBaseLoad is the power the whole house (the mains legs added) never goes below over a period. 204 when under an hour was recorded.
func (s *Server) houseBaseLoad(w http.ResponseWriter, r *http.Request) {
	on, _, err := s.enabledMeters(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	var ids []string
	for _, ch := range on {
		if ch.Scope == model.ScopeMains {
			ids = append(ids, ch.ID)
		}
	}
	s.baseLoad(w, r, "house", "", ids, on)
}

// meterBaseLoad is the power one meter never goes below over a period. Never added to anything: it is a view of that meter's own reading.
func (s *Server) meterBaseLoad(w http.ResponseWriter, r *http.Request) {
	_, byID, err := s.enabledMeters(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	id := r.PathValue("meterId")
	if _, ok := byID[id]; !ok {
		writeProblem(w, http.StatusNotFound, "unknown_meter", "no such meter")
		return
	}
	s.baseLoad(w, r, "meter", id, []string{id}, nil)
}

func (s *Server) baseLoad(w http.ResponseWriter, r *http.Request, scope, meterID string, ids []string, all []model.Meter) {
	from, to, err := parsePeriod(r)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "bad_period", err.Error())
		return
	}
	watts, err := s.Store.MinuteWatts(r.Context(), ids, from, to)
	if err != nil {
		serverError(w, err)
		return
	}
	res, ok := baseload.Of(watts)
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	out := baseLoadAnswer{Scope: scope, MeterID: meterID, Watts: res.Watts, AvgWatts: res.AvgWatts, Share: res.Share(), Minutes: res.Minutes,
		KWhYear: res.KWhPerYear(), From: from.UTC(), To: to.UTC()}
	cfg, err := s.rateConfig(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	schedule := cfg.Schedule(s.Loc)
	today := rates.DateOf(s.now(), s.Loc)
	if rate := schedule.RateAt(today); rate != nil {
		rs, cs := rate.String(), rates.MoneyFromFloat(out.KWhYear).Mul(*rate).String()
		out.Rate, out.CostYear, out.RateEstimated = &rs, &cs, schedule.IsEstimate(today)
	}
	if scope == "house" {
		if out.Parts, err = s.baseParts(r.Context(), all, from, to, schedule.RateAt(today)); err != nil {
			serverError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// baseParts is each leaf meter's own floor: the meters that hold no other (enabled) meter and are not the mains.
func (s *Server) baseParts(ctx context.Context, all []model.Meter, from, to time.Time, rate *rates.Money) ([]basePart, error) {
	holds := map[string]bool{}
	for _, m := range all {
		if m.ParentID != nil {
			holds[*m.ParentID] = true
		}
	}
	var out []basePart
	for _, m := range all {
		if m.Scope == model.ScopeMains || holds[m.ID] {
			continue
		}
		watts, err := s.Store.MinuteWatts(ctx, []string{m.ID}, from, to)
		if err != nil {
			return nil, err
		}
		res, ok := baseload.Of(watts)
		if !ok || res.Watts < minBasePartWatts {
			continue
		}
		p := basePart{MeterID: m.ID, Label: m.Label, Watts: res.Watts, KWhYear: res.KWhPerYear()}
		if rate != nil {
			c := rates.MoneyFromFloat(p.KWhYear).Mul(*rate).String()
			p.CostYear = &c
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Watts > out[j].Watts })
	if len(out) > maxBaseParts {
		out = out[:maxBaseParts]
	}
	return out, nil
}
