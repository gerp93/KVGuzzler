package api

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/collector"
	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

type runsBody struct {
	MeterID        string   `json:"meter_id"`
	ThresholdWatts *float64 `json:"threshold_watts"`
	MeterKWh       float64  `json:"meter_kwh"`
	Truncated      bool     `json:"truncated"`
	Runs           []struct {
		Start    time.Time `json:"start"`
		Minutes  int       `json:"minutes"`
		KWh      float64   `json:"kwh"`
		Cost     *string   `json:"cost"`
		AvgAmps  *float64  `json:"avg_amps"`
		PeakAmps *float64  `json:"peak_amps"`
	} `json:"runs"`
	Totals struct {
		Count          int     `json:"count"`
		KWh            float64 `json:"kwh"`
		Cost           *string `json:"cost"`
		LongestMinutes int     `json:"longest_minutes"`
	} `json:"totals"`
}

// runsServer has a charger that charged for two hours on each of three days (7.2 kW over 3 W at rest) and a fridge that only ever hums.
func runsServer(t *testing.T) (*httptest.Server, time.Time) {
	t.Helper()
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.UpsertDevice(ctx, model.Device{ID: "d", Kind: "refoss", Name: "d"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"charger", "fridge", "ma", "mb"} {
		scope := model.ScopeCircuit
		if id[0] == 'm' {
			scope = model.ScopeMains
		}
		if err := s.UpsertMeter(ctx, model.Meter{ID: id, DeviceID: "d", Scope: scope, Label: id}); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	var rows []store.MinuteRow
	for day := 0; day < 3; day++ {
		for m := 0; m < 1440; m++ {
			at := start.Add(time.Duration(day*1440+m) * time.Minute)
			w := 3.0
			if m >= 18*60 && m < 20*60 {
				w = 7200
			}
			rows = append(rows,
				store.MinuteRow{MeterID: "charger", Minute: at, AvgW: w, MaxW: w, Seconds: 60, AvgA: amps(w / 240), MaxA: amps(w/240 + 1)},
				store.MinuteRow{MeterID: "fridge", Minute: at, AvgW: 90 + float64(m%3), MaxW: 95, Seconds: 60},
				// The mains: leg A carries the charger's 7.2 kW on top of 500 W, leg B 400 W. Leg B is missing for the first hour of day one.
				store.MinuteRow{MeterID: "ma", Minute: at, AvgW: 500 + w - 3, MaxW: 600 + w, Seconds: 60})
			if day > 0 || m >= 60 {
				rows = append(rows, store.MinuteRow{MeterID: "mb", Minute: at, AvgW: 400, MaxW: 410, Seconds: 60})
			}
		}
	}
	if err := s.AddMinutes(ctx, rows); err != nil {
		t.Fatal(err)
	}
	now := start.Add(72 * time.Hour)
	api := &Server{Store: s, Collector: collector.New(s, time.Second), Loc: time.UTC, Version: "test", Now: func() time.Time { return now }}
	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)
	return srv, start
}

func amps(a float64) *float64 { return &a }

func runsURL(srv *httptest.Server, id string, from, to time.Time, extra string) string {
	return fmt.Sprintf("%s/api/v1/meters/%s/runs?from=%s&to=%s%s", srv.URL, id, from.Format(time.RFC3339), to.Format(time.RFC3339), extra)
}

func TestRunsListEachChargeNewestFirstWithEnergyAndTotals(t *testing.T) {
	srv, start := runsServer(t)
	var b runsBody
	if r := get(t, runsURL(srv, "charger", start, start.Add(72*time.Hour), ""), &b); r.StatusCode != 200 {
		t.Fatalf("status %d", r.StatusCode)
	}
	if len(b.Runs) != 3 || b.Totals.Count != 3 || b.Truncated {
		t.Fatalf("runs = %+v, want three charges", b)
	}
	if !b.Runs[0].Start.After(b.Runs[1].Start) || !b.Runs[1].Start.After(b.Runs[2].Start) {
		t.Errorf("not newest first: %v", b.Runs)
	}
	if b.Runs[0].Minutes != 120 || math.Abs(b.Runs[0].KWh-14.4) > 1e-9 || math.Abs(b.Totals.KWh-43.2) > 1e-9 || b.Totals.LongestMinutes != 120 {
		t.Errorf("a charge is 2 h at 7.2 kW = 14.4 kWh; got %+v totals %+v", b.Runs[0], b.Totals)
	}
	if b.ThresholdWatts == nil || *b.ThresholdWatts < 100 || *b.ThresholdWatts > 3000 {
		t.Errorf("threshold = %v", b.ThresholdWatts)
	}
	// The runs hold nearly all the meter's energy; the rest is the 3 W it draws at rest.
	if b.MeterKWh < b.Totals.KWh || b.MeterKWh > b.Totals.KWh+0.2 {
		t.Errorf("meter %v kWh vs runs %v kWh", b.MeterKWh, b.Totals.KWh)
	}
	// No price saved: no cost, and not a made-up zero.
	if b.Totals.Cost != nil || b.Runs[0].Cost != nil {
		t.Errorf("cost %v / %v with no price saved", b.Totals.Cost, b.Runs[0].Cost)
	}
}

func TestRunsAreCostedAtTheirOwnPrice(t *testing.T) {
	srv, start := runsServer(t)
	put(t, srv.URL+"/api/v1/rates", `{"mode":"one","fixed_rate":"0.25","currency":"USD"}`)
	var b runsBody
	get(t, runsURL(srv, "charger", start, start.Add(72*time.Hour), ""), &b)
	if b.Runs[0].Cost == nil || b.Totals.Cost == nil {
		t.Fatalf("no cost: %+v", b)
	}
	if *b.Runs[0].Cost != "3.60" || *b.Totals.Cost != "10.80" {
		t.Errorf("14.4 kWh at 0.25 is 3.60 and three of them 10.80; got %v / %v", *b.Runs[0].Cost, *b.Totals.Cost)
	}
}

func TestASteadyMeterHasNoRuns(t *testing.T) {
	srv, start := runsServer(t)
	var b runsBody
	if r := get(t, runsURL(srv, "fridge", start, start.Add(72*time.Hour), ""), &b); r.StatusCode != 200 {
		t.Fatalf("status %d", r.StatusCode)
	}
	if b.ThresholdWatts != nil || len(b.Runs) != 0 || b.Runs == nil {
		t.Errorf("a fridge that only hums has no runs: %+v", b)
	}
	// Asking for a level of one's own overrides the guess.
	get(t, runsURL(srv, "fridge", start, start.Add(72*time.Hour), "&min_watts=92"), &b)
	if b.ThresholdWatts == nil || *b.ThresholdWatts != 92 {
		t.Errorf("threshold = %v, want 92", b.ThresholdWatts)
	}
}

func TestARunIsListedInThePeriodItStartsIn(t *testing.T) {
	srv, start := runsServer(t)
	// A window that opens in the middle of the first day's charge does not list that charge, but one that ends inside the second does.
	var b runsBody
	get(t, runsURL(srv, "charger", start.Add(19*time.Hour), start.Add(24*time.Hour+19*time.Hour), ""), &b)
	if len(b.Runs) != 1 || !b.Runs[0].Start.Equal(start.Add(24*time.Hour+18*time.Hour)) {
		t.Errorf("runs = %+v, want only day two's charge", b.Runs)
	}
	// The level is learned from the days before the window too, so an hour of a charge is still judged against a charger's usual level.
	get(t, runsURL(srv, "charger", start.Add(48*time.Hour+17*time.Hour), start.Add(48*time.Hour+19*time.Hour), ""), &b)
	if len(b.Runs) != 1 || b.Runs[0].Minutes != 120 {
		t.Errorf("runs = %+v, want the third charge, whole", b.Runs)
	}
}

func TestRunsRefuseBadAsks(t *testing.T) {
	srv, start := runsServer(t)
	end := start.Add(24 * time.Hour)
	for name, url := range map[string]string{
		"unknown meter": runsURL(srv, "nope", start, end, ""),
		"min_watts nan": runsURL(srv, "charger", start, end, "&min_watts=abc"),
		"min_watts <0":  runsURL(srv, "charger", start, end, "&min_watts=-5"),
		"bad period":    runsURL(srv, "charger", end, start, ""),
	} {
		r := get(t, url, nil)
		want := http.StatusBadRequest
		if name == "unknown meter" {
			want = http.StatusNotFound
		}
		if r.StatusCode != want {
			t.Errorf("%s: %d, want %d", name, r.StatusCode, want)
		}
	}
}

func TestARunKnowsItsAverageAndPeakCurrentWhenTheMeterReportsOne(t *testing.T) {
	srv, start := runsServer(t)
	var b runsBody
	get(t, runsURL(srv, "charger", start, start.Add(72*time.Hour), ""), &b)
	r := b.Runs[0]
	// 7.2 kW at 240 V is 30 A, the average over the run (the minutes at rest around it are not part of it); the highest minute was 1 A more.
	if r.AvgAmps == nil || math.Abs(*r.AvgAmps-30) > 1e-9 || r.PeakAmps == nil || math.Abs(*r.PeakAmps-31) > 1e-9 {
		t.Errorf("run current = %v avg, %v peak, want 30 and 31", r.AvgAmps, r.PeakAmps)
	}
	// A meter that never reported a current has none: null, not 0 A.
	get(t, runsURL(srv, "fridge", start, start.Add(72*time.Hour), "&min_watts=92"), &b)
	if len(b.Runs) == 0 || b.Runs[0].AvgAmps != nil || b.Runs[0].PeakAmps != nil {
		t.Errorf("fridge runs = %+v: no current was ever reported", b.Runs)
	}
}
