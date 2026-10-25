package api

import (
	"net/http"
	"sort"
	"strconv"

	"github.com/gerp93/KVGuzzler/server/internal/anomaly"
	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/rates"
)

const (
	defaultAnomalyDays = 30
	maxAnomalyDays     = 90
	// minHouseExtraKWh and minMeterExtraKWh are the least extra energy worth saying for the house and for one meter.
	minHouseExtraKWh = 1.0
	minMeterExtraKWh = 0.3
	// maxCauses is how many meters a day names as what made it unusual.
	maxCauses = 3
)

type anomalyCause struct {
	MeterID  string  `json:"meter_id"`
	Label    string  `json:"label"`
	ExtraKWh float64 `json:"extra_kwh"`
}

type anomalyDay struct {
	Date rates.Date `json:"date"`
	// MeterID is the meter whose day it was; null for the whole house (the mains legs added).
	MeterID  *string `json:"meter_id"`
	Label    string  `json:"label"`
	KWh      float64 `json:"kwh"`
	UsualKWh float64 `json:"usual_kwh"`
	// ExtraKWh is how much more than usual; ExtraCost the same at that day's price (null with no price).
	ExtraKWh  float64 `json:"extra_kwh"`
	ExtraCost *string `json:"extra_cost"`
	Ratio     float64 `json:"ratio"`
	// Causes are the meters inside it whose own day was also unusual (the biggest first); they are already part of this figure, never added to it.
	Causes []anomalyCause `json:"causes"`
}

type anomalyAnswer struct {
	From rates.Date   `json:"from"`
	To   rates.Date   `json:"to"`
	Days []anomalyDay `json:"days"`
}

// anomalies lists the days the house, or a meter, used far more than it usually does (see package anomaly). Without meter_id it is the house and
// every meter, but a meter whose day is explained by an unusual day of something that holds it is named as a cause of that day, not listed again.
func (s *Server) anomalies(w http.ResponseWriter, r *http.Request) {
	days := defaultAnomalyDays
	if v := r.URL.Query().Get("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxAnomalyDays {
			writeProblem(w, http.StatusBadRequest, "bad_days", "days is a number from 1 to 90")
			return
		}
		days = n
	}
	only := r.URL.Query().Get("meter_id")
	on, byID, err := s.enabledMeters(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	if only != "" {
		if _, ok := byID[only]; !ok || !byID[only].Enabled {
			writeProblem(w, http.StatusNotFound, "unknown_meter", "no such meter")
			return
		}
	}
	cfg, err := s.rateConfig(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	schedule := cfg.Schedule(s.Loc)

	today := rates.DateOf(s.now(), s.Loc)
	from := today.AddDays(-days)
	readFrom := s.scheduleDayStart(from.AddDays(-anomaly.LookbackDays))
	readTo := s.scheduleDayStart(today)

	perDay := func(ids []string) ([]anomaly.Day, error) {
		sum := map[rates.Date]float64{}
		for _, id := range ids {
			points, err := s.Store.Series(r.Context(), id, readFrom, readTo, model.BucketDay, s.Loc)
			if err != nil {
				return nil, err
			}
			for _, p := range points {
				sum[rates.DateOf(p.T, s.Loc)] += p.KWh
			}
		}
		out := make([]anomaly.Day, 0, len(sum))
		for d, k := range sum {
			out = append(out, anomaly.Day{Date: d, KWh: k})
		}
		return out, nil
	}

	// Every subject: the house (the mains legs added) and each other shown meter, with what unusual days each had.
	type subject struct {
		id, label string // id "" is the house
		found     map[rates.Date]anomaly.Finding
	}
	var subjects []*subject
	var mains []string
	for _, m := range on {
		if m.Scope == model.ScopeMains {
			mains = append(mains, m.ID)
		}
	}
	add := func(id, label string, ids []string, minExtra float64) error {
		series, err := perDay(ids)
		if err != nil {
			return err
		}
		sub := &subject{id: id, label: label, found: map[rates.Date]anomaly.Finding{}}
		for _, f := range anomaly.Detect(series, from, today, minExtra) {
			sub.found[f.Date] = f
		}
		subjects = append(subjects, sub)
		return nil
	}
	if len(mains) > 0 && only == "" {
		if err := add("", "House", mains, minHouseExtraKWh); err != nil {
			serverError(w, err)
			return
		}
	}
	for _, m := range on {
		if m.Scope == model.ScopeMains {
			continue
		}
		if err := add(m.ID, m.Label, []string{m.ID}, minMeterExtraKWh); err != nil {
			serverError(w, err)
			return
		}
	}

	// What holds what: a meter's ancestors are the meters it is inside (the house holds every meter that is not a mains).
	parentOf := func(id string) string {
		if m, ok := byID[id]; ok && m.ParentID != nil {
			if p, ok := byID[*m.ParentID]; ok && p.Enabled {
				return p.ID
			}
		}
		return ""
	}
	bySubject := map[string]*subject{}
	for _, sub := range subjects {
		bySubject[sub.id] = sub
	}
	// anomalousOn: is this subject's day unusual?
	unusual := func(id string, d rates.Date) bool {
		sub := bySubject[id]
		if sub == nil {
			return false
		}
		_, ok := sub.found[d]
		return ok
	}
	// holder reports whether `outer` holds `inner` (the house, id "", holds every meter).
	holder := func(outer, inner string) bool {
		if outer == "" {
			return inner != ""
		}
		for cur, depth := parentOf(inner), 0; cur != "" && depth < 16; cur, depth = parentOf(cur), depth+1 {
			if cur == outer {
				return true
			}
		}
		return false
	}

	out := anomalyAnswer{From: from, To: today.AddDays(-1), Days: []anomalyDay{}}
	for _, sub := range subjects {
		if only != "" && sub.id != only {
			continue
		}
		for d, f := range sub.found {
			if only == "" {
				// Explained by something that holds it being unusual the same day: it is named there, not listed again.
				hidden := false
				for _, o := range subjects {
					if o.id != sub.id && holder(o.id, sub.id) && unusual(o.id, d) {
						hidden = true
						break
					}
				}
				if hidden {
					continue
				}
			}
			// The causes: meters inside it that were unusual that day and hold no unusual meter of their own.
			var causes []anomalyCause
			for _, c := range subjects {
				if c.id == sub.id || !holder(sub.id, c.id) || !unusual(c.id, d) {
					continue
				}
				leaf := true
				for _, o := range subjects {
					if o.id != c.id && holder(c.id, o.id) && unusual(o.id, d) {
						leaf = false
						break
					}
				}
				if leaf {
					causes = append(causes, anomalyCause{MeterID: c.id, Label: c.label, ExtraKWh: c.found[d].Extra})
				}
			}
			sort.Slice(causes, func(i, j int) bool { return causes[i].ExtraKWh > causes[j].ExtraKWh })
			if len(causes) > maxCauses {
				causes = causes[:maxCauses]
			}
			if causes == nil {
				causes = []anomalyCause{}
			}
			e := anomalyDay{Date: d, Label: sub.label, KWh: f.KWh, UsualKWh: f.Usual, ExtraKWh: f.Extra, Ratio: f.Ratio, Causes: causes}
			if sub.id != "" {
				id := sub.id
				e.MeterID = &id
			}
			if rate := schedule.RateAt(d); rate != nil {
				c := rates.MoneyFromFloat(f.Extra).Mul(*rate).String()
				e.ExtraCost = &c
			}
			out.Days = append(out.Days, e)
		}
	}
	sort.Slice(out.Days, func(i, j int) bool {
		if out.Days[i].Date != out.Days[j].Date {
			return out.Days[i].Date.After(out.Days[j].Date)
		}
		return out.Days[i].ExtraKWh > out.Days[j].ExtraKWh
	})
	writeJSON(w, http.StatusOK, out)
}
