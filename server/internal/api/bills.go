package api

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/gerp93/KVGuzzler/server/internal/rates"
)

// The electricity bills the owner has entered. They are kept whatever the price comes from: when it comes from the bills they set the price of the days
// they cover, and either way they are what the recorded energy is checked against (see reconcile.go). They are stored inside the price settings, but have
// their own endpoints so that saving the price source can never touch them and a bill cannot be lost by saving the source.

type billsAnswer struct {
	Bills []rates.Bill `json:"bills"`
}

func (s *Server) getBills(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.savedRateConfig(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, billsAnswer{Bills: sortedBills(cfg.Bills)})
}

func sortedBills(in []rates.Bill) []rates.Bill {
	out := append([]rates.Bill{}, in...)
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}

// putBills replaces the list of bills. When prices come from the bills, the finished days that a new, changed or removed bill covers are priced again
// (the bill is what sets their price, and correcting a bill is meant to correct them); when they come from anything else, no price changes.
func (s *Server) putBills(w http.ResponseWriter, r *http.Request) {
	var body billsAnswer
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "bad_bills", `send {"bills": [{"start": "2026-09-21", "end": "2026-10-20", "total": "162.40", "kwh": 1100}]}`)
		return
	}
	if body.Bills == nil {
		body.Bills = []rates.Bill{}
	}
	if err := rates.ValidateBills(body.Bills); err != nil {
		writeProblem(w, http.StatusBadRequest, "bad_bills", err.Error())
		return
	}
	cfg, err := s.savedRateConfig(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	if cfg.Mode == rates.ModeBills {
		// The days of a bill that was added, changed or removed: one bill's dates cover many days.
		for _, b := range changedBills(cfg.Bills, body.Bills) {
			if _, err := s.Store.ForgetDayPrices(r.Context(), b.Start.String(), b.End.String()); err != nil {
				serverError(w, err)
				return
			}
		}
	}
	cfg.Bills = sortedBills(body.Bills)
	if err := s.saveRateConfig(r.Context(), cfg); err != nil {
		serverError(w, err)
		return
	}
	if cfg.Mode == rates.ModeBills {
		if _, err := s.freezePrices(r.Context(), cfg); err != nil {
			serverError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, billsAnswer{Bills: cfg.Bills})
}

// changedBills are the bills in either list that are not, exactly as they are, in the other: the ones added, removed or edited (an edited bill is
// listed twice, as it was and as it is, so the days of both versions are covered).
func changedBills(was, now []rates.Bill) []rates.Bill {
	key := func(b rates.Bill) string {
		j, _ := json.Marshal(b)
		return string(j)
	}
	inWas, inNow := map[string]bool{}, map[string]bool{}
	for _, b := range was {
		inWas[key(b)] = true
	}
	for _, b := range now {
		inNow[key(b)] = true
	}
	var out []rates.Bill
	for _, b := range was {
		if !inNow[key(b)] {
			out = append(out, b)
		}
	}
	for _, b := range now {
		if !inWas[key(b)] {
			out = append(out, b)
		}
	}
	return out
}
