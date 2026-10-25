package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/collector"
	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

type anomalyBody struct {
	From, To string
	Days     []struct {
		Date      string  `json:"date"`
		MeterID   *string `json:"meter_id"`
		Label     string  `json:"label"`
		KWh       float64 `json:"kwh"`
		UsualKWh  float64 `json:"usual_kwh"`
		ExtraKWh  float64 `json:"extra_kwh"`
		ExtraCost *string `json:"extra_cost"`
		Causes    []struct {
			MeterID  string  `json:"meter_id"`
			Label    string  `json:"label"`
			ExtraKWh float64 `json:"extra_kwh"`
		} `json:"causes"`
	} `json:"days"`
}

// anomalyServer has 40 days (1 Oct to 9 Nov) of a house whose days are all alike: an EV charger (10 kWh) and a workshop (2 kWh) inside a garage panel (12),
// a fridge (1.5), and a mains of 20 kWh. On 5 Nov the charger used 40 instead of 10; on 2 Nov the fridge used 3 instead of 1.5. Today is 10 Nov.
func anomalyServer(t *testing.T) *httptest.Server {
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
	for id, scope := range map[string]model.Scope{"ma": model.ScopeMains, "garage": model.ScopeCircuit, "ev": model.ScopeCircuit, "shop": model.ScopeCircuit, "fridge": model.ScopeCircuit} {
		if err := s.UpsertMeter(ctx, model.Meter{ID: id, DeviceID: "d", Scope: scope, Label: map[string]string{"ma": "Mains", "garage": "Garage panel", "ev": "EV charger", "shop": "Workshop", "fridge": "Fridge"}[id]}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.SetMeterParent(ctx, "ev", strp("garage")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetMeterParent(ctx, "shop", strp("garage")); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	// One minute row a day whose power makes the day's energy: kWh = watts x 60 s / 3.6e6.
	row := func(id string, day int, kwh float64) store.MinuteRow {
		return store.MinuteRow{MeterID: id, Minute: start.AddDate(0, 0, day), AvgW: kwh * 60000, MaxW: kwh * 60000, Seconds: 60}
	}
	var rows []store.MinuteRow
	for day := 0; day < 40; day++ {
		ev, fridge := 10.0, 1.5
		if day == 35 { // 5 Nov
			ev = 40
		}
		if day == 32 { // 2 Nov
			fridge = 3
		}
		garage := ev + 2
		rows = append(rows, row("ev", day, ev), row("shop", day, 2), row("garage", day, garage), row("fridge", day, fridge), row("ma", day, garage+fridge+6.5))
	}
	if err := s.AddMinutes(ctx, rows); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	api := &Server{Store: s, Collector: collector.New(s, time.Second), Loc: time.UTC, Version: "test", Now: func() time.Time { return now }}
	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)
	return srv
}

func TestAnUnusualDayIsListedOnceWithItsCauseAndWhatTheExtraCost(t *testing.T) {
	srv := anomalyServer(t)
	put(t, srv.URL+"/api/v1/rates", `{"mode":"one","fixed_rate":"0.20","currency":"USD"}`)
	var a anomalyBody
	if r := get(t, srv.URL+"/api/v1/anomalies", &a); r.StatusCode != 200 {
		t.Fatalf("status %d", r.StatusCode)
	}
	if len(a.Days) != 2 {
		t.Fatalf("days = %+v, want the house on 5 Nov and the fridge on 2 Nov", a.Days)
	}
	h := a.Days[0] // newest first
	if h.Date != "2026-11-05" || h.MeterID != nil || h.Label != "House" || h.KWh != 50 || h.UsualKWh != 20 || h.ExtraKWh != 30 {
		t.Errorf("the house day = %+v", h)
	}
	// The garage panel and the charger were unusual that day too, but are explained by the house: named as the cause, not listed again.
	if len(h.Causes) != 1 || h.Causes[0].Label != "EV charger" || h.Causes[0].ExtraKWh != 30 {
		t.Errorf("causes = %+v, want the charger (the garage panel holds it, so it is not a cause of its own)", h.Causes)
	}
	if h.ExtraCost == nil || *h.ExtraCost != "6.00" {
		t.Errorf("extra cost = %v, want 30 kWh at 0.20", h.ExtraCost)
	}
	f := a.Days[1]
	if f.Date != "2026-11-02" || f.MeterID == nil || *f.MeterID != "fridge" || f.ExtraKWh != 1.5 || len(f.Causes) != 0 {
		t.Errorf("the fridge day = %+v", f)
	}
}

func TestAMetersPageGetsItsOwnUnusualDaysAndTodayIsNeverJudged(t *testing.T) {
	srv := anomalyServer(t)
	var a anomalyBody
	get(t, srv.URL+"/api/v1/anomalies?meter_id=garage", &a)
	if len(a.Days) != 1 || a.Days[0].Date != "2026-11-05" || a.Days[0].Label != "Garage panel" || len(a.Days[0].Causes) != 1 || a.Days[0].Causes[0].Label != "EV charger" {
		t.Errorf("garage = %+v", a.Days)
	}
	get(t, srv.URL+"/api/v1/anomalies?meter_id=ev", &a)
	if len(a.Days) != 1 || a.Days[0].ExtraKWh != 30 {
		t.Errorf("the charger = %+v: it is listed on its own page even though the house explains it", a.Days)
	}
	// A short window that ends before the spike lists nothing: only complete days up to today are looked at.
	get(t, srv.URL+"/api/v1/anomalies?days=3", &a)
	if len(a.Days) != 0 {
		t.Errorf("the last 3 days had nothing unusual: %+v", a.Days)
	}
}

func TestAnomalyAsksAreChecked(t *testing.T) {
	srv := anomalyServer(t)
	for url, want := range map[string]int{
		"/api/v1/anomalies?days=0":          http.StatusBadRequest,
		"/api/v1/anomalies?days=500":        http.StatusBadRequest,
		"/api/v1/anomalies?days=x":          http.StatusBadRequest,
		"/api/v1/anomalies?meter_id=nobody": http.StatusNotFound,
	} {
		if r := get(t, srv.URL+url, nil); r.StatusCode != want {
			t.Errorf("%s: %d, want %d", url, r.StatusCode, want)
		}
	}
}
