package api

import (
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/rates"
	"github.com/gerp93/KVGuzzler/server/internal/runs"
)

// maxRuns is how many runs one answer lists (the newest); the totals always count all of them.
const maxRuns = 500

// thresholdLookback: the level that separates "working" from "resting" is learned from at least this much history ending at the period's end,
// so a short period (an hour) is judged by the same level as a month, not by whatever the meter happened to do inside the hour.
const thresholdLookback = 30 * 24 * time.Hour

// runTail is how far past the end of the period a run that began inside it is followed.
const runTail = 12 * time.Hour

type runItem struct {
	runs.Run
	// Cost is the run's energy at the price of the local day it started, or null when no price is known.
	Cost *string `json:"cost"`
}

type runTotals struct {
	Count          int     `json:"count"`
	KWh            float64 `json:"kwh"`
	Cost           *string `json:"cost"`
	Minutes        int     `json:"minutes"`
	LongestMinutes int     `json:"longest_minutes"`
	PeakWatts      float64 `json:"peak_watts"`
}

type runsAnswer struct {
	MeterID string `json:"meter_id"`
	// ThresholdWatts is the level above which a minute counts as working; null for a meter that barely changes (it is on, it does not run).
	ThresholdWatts *float64 `json:"threshold_watts"`
	// MeterKWh is everything the meter used over the period, so the runs' share of it can be said.
	MeterKWh      float64   `json:"meter_kwh"`
	Runs          []runItem `json:"runs"`
	Totals        runTotals `json:"totals"`
	Truncated     bool      `json:"truncated"`
	CostEstimated bool      `json:"cost_estimated"`
	From          time.Time `json:"from"`
	To            time.Time `json:"to"`
}

// meterRuns lists the times a meter was working over a period: a car charging, a dryer drying. It reads only the recorded minutes. A run is
// listed in the period it starts in.
func (s *Server) meterRuns(w http.ResponseWriter, r *http.Request) {
	from, to, err := parsePeriod(r)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "bad_period", err.Error())
		return
	}
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
	var minWatts *float64
	if v := r.URL.Query().Get("min_watts"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f <= 0 || math.IsInf(f, 0) || math.IsNaN(f) || f > 1e6 {
			writeProblem(w, http.StatusBadRequest, "bad_min_watts", "min_watts is a positive number of watts")
			return
		}
		minWatts = &f
	}

	readFrom := from
	if lb := to.Add(-thresholdLookback); lb.Before(readFrom) {
		readFrom = lb
	}
	// A little past the end too, so a run that begins inside the period and carries on after it is whole.
	points, err := s.Store.MeterMinutes(r.Context(), id, readFrom, to.Add(runTail))
	if err != nil {
		serverError(w, err)
		return
	}
	threshold := runs.Threshold(points)
	if minWatts != nil {
		threshold = *minWatts
	}
	found := runs.Detect(points, threshold, runs.DefaultGapMinutes, runs.DefaultMinMinutes)

	out := runsAnswer{MeterID: id, Runs: []runItem{}, From: from.UTC(), To: to.UTC()}
	if !math.IsInf(threshold, 1) {
		t := threshold
		out.ThresholdWatts = &t
	}
	tot, err := s.Store.Totals(r.Context(), id, from, to)
	if err != nil {
		serverError(w, err)
		return
	}
	out.MeterKWh = tot.KWh

	cfg, err := s.rateConfig(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	schedule := cfg.Schedule(s.Loc)
	var cost rates.Money
	priced := true
	items := make([]runItem, 0, len(found))
	for _, run := range found {
		if run.Start.Before(from) || !run.Start.Before(to) {
			continue // it began outside the period: it belongs to the period it started in
		}
		it := runItem{Run: run}
		day := rates.DateOf(run.Start, s.Loc)
		if rate := schedule.RateAt(day); rate != nil {
			c := rates.MoneyFromFloat(run.KWh).Mul(*rate)
			cs := c.String()
			it.Cost = &cs
			cost = cost.Add(c)
			out.CostEstimated = out.CostEstimated || schedule.IsEstimate(day)
		} else {
			priced = false
		}
		out.Totals.Count++
		out.Totals.KWh += run.KWh
		out.Totals.Minutes += run.Minutes
		out.Totals.LongestMinutes = max(out.Totals.LongestMinutes, run.Minutes)
		out.Totals.PeakWatts = math.Max(out.Totals.PeakWatts, run.PeakWatts)
		items = append(items, it)
	}
	if priced && out.Totals.Count > 0 {
		c := cost.String()
		out.Totals.Cost = &c
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Start.After(items[j].Start) })
	if len(items) > maxRuns {
		items, out.Truncated = items[:maxRuns], true
	}
	out.Runs = items
	writeJSON(w, http.StatusOK, out)
}
