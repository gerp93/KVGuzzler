package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

func TestStandbyListsEachAppliancesFloorAgainstTheStretchBefore(t *testing.T) {
	w := newAlertWorld(t) // the septic pump rests at 2 W; the mains draw a steady 1000 W
	if r := sendJSON(t, "PUT", w.srv.URL+"/api/v1/rates", `{"mode":"one","fixed_rate":"0.10"}`, nil); r.StatusCode != 200 {
		t.Fatalf("rates: %d", r.StatusCode)
	}
	var got standbyAnswer
	if r := get(t, w.srv.URL+"/api/v1/standby?days=10", &got); r.StatusCode != http.StatusOK {
		t.Fatalf("status %d", r.StatusCode)
	}
	if got.HouseWatts == nil || abs(*got.HouseWatts-1000) > 0.01 || got.HousePrevWatts == nil || abs(*got.HousePrevWatts-1000) > 0.01 {
		t.Errorf("house %v %v", got.HouseWatts, got.HousePrevWatts)
	}
	// The septic circuit is the one appliance with a floor worth a line (the EV charger recorded nothing; the mains are the house).
	if len(got.Parts) != 1 || got.Parts[0].Label != "Septic" {
		t.Fatalf("parts %+v", got.Parts)
	}
	p := got.Parts[0]
	if abs(p.Watts-2) > 0.05 || p.PrevWatts == nil || abs(*p.PrevWatts-2) > 0.05 || p.ChangeWatts == nil || *p.ChangeWatts != 0 {
		t.Errorf("part %+v", p)
	}
	// 2 W all year is 17.5 kWh, which is 1.75 at 0.10.
	if abs(p.KWhYear-17.532) > 0.1 || p.CostYear == nil || moneyShort(*p.CostYear) != "1.75" {
		t.Errorf("a year: %v kWh, %v", p.KWhYear, p.CostYear)
	}
	if got.Change != nil {
		t.Errorf("no change day was asked for: %v", got.Change)
	}
}

func TestStandbyAroundADayShowsWhatChanged(t *testing.T) {
	w := newAlertWorld(t)
	ctx := context.Background()
	// A dehumidifier-like load that drew 40 W until 20 Oct and was unplugged: its floor falls from 40 W to nothing.
	if err := w.api.Store.UpsertMeter(ctx, model.Meter{ID: "fan", DeviceID: "d", Scope: model.ScopeCircuit, Label: "Attic fan"}); err != nil {
		t.Fatal(err)
	}
	var rows []store.MinuteRow
	for i := 0; i < 12*24*60; i++ {
		at := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Minute)
		watts := 40.0
		if at.After(time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)) {
			watts = 0.2
		}
		rows = append(rows, store.MinuteRow{MeterID: "fan", Minute: at, AvgW: watts, MaxW: watts, Seconds: 60})
	}
	if err := w.api.Store.AddMinutes(ctx, rows); err != nil {
		t.Fatal(err)
	}
	var got standbyAnswer
	get(t, w.srv.URL+"/api/v1/standby?days=10&change=2026-10-20", &got)
	if got.Change == nil || got.Change.String() != "2026-10-20" {
		t.Fatalf("change %v", got.Change)
	}
	var fan *standbyPart
	for i := range got.Parts {
		if got.Parts[i].MeterID == "fan" {
			fan = &got.Parts[i]
		}
	}
	// Listed because it rested at something worth a line before, with what it rests at now beside it.
	if fan == nil || fan.PrevWatts == nil || abs(*fan.PrevWatts-40) > 0.1 || fan.ChangeWatts == nil || abs(*fan.ChangeWatts+39.8) > 0.2 {
		t.Fatalf("fan %+v", fan)
	}
	for _, bad := range []string{"days=2", "days=999", "change=nope", "change=2030-01-01"} {
		if r := get(t, w.srv.URL+"/api/v1/standby?"+bad, nil); r.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, r.StatusCode)
		}
	}
}
