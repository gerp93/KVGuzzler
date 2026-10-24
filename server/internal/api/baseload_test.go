package api

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type baseBody struct {
	Scope    string  `json:"scope"`
	MeterID  string  `json:"meter_id"`
	Watts    float64 `json:"watts"`
	AvgWatts float64 `json:"avg_watts"`
	Share    float64 `json:"share"`
	Minutes  int     `json:"minutes"`
	KWhYear  float64 `json:"kwh_per_year"`
	CostYear *string `json:"cost_per_year"`
	Rate     *string `json:"rate"`
}

func baseURL(srv *httptest.Server, path string, from, to time.Time) string {
	return fmt.Sprintf("%s/api/v1/%s?from=%s&to=%s", srv.URL, path, from.Format(time.RFC3339), to.Format(time.RFC3339))
}

func TestAMeterBaseLoadIsWhatItRestsAtAndWhatThatCostsAYear(t *testing.T) {
	srv, start := runsServer(t)
	put(t, srv.URL+"/api/v1/rates", `{"mode":"one","fixed_rate":"0.20","currency":"USD"}`)
	var b baseBody
	if r := get(t, baseURL(srv, "meters/charger/baseload", start, start.Add(72*time.Hour)), &b); r.StatusCode != 200 {
		t.Fatalf("status %d", r.StatusCode)
	}
	if b.Scope != "meter" || b.MeterID != "charger" || math.Abs(b.Watts-3) > 1e-9 || b.Minutes != 3*1440 {
		t.Errorf("base = %+v, want 3 W over every minute", b)
	}
	// 3 W for a year is 26.298 kWh, and at 0.20 that is 5.2596.
	if math.Abs(b.KWhYear-26.298) > 1e-9 || b.CostYear == nil || *b.CostYear != "5.2596" || b.Rate == nil || *b.Rate != "0.20" {
		t.Errorf("year = %v kWh, cost %v at %v", b.KWhYear, b.CostYear, b.Rate)
	}
	// The charger runs 2 of 24 hours at 7.2 kW: about 600 W on average, so the always-on part is a sliver.
	if b.Share <= 0 || b.Share > 0.02 {
		t.Errorf("share = %v", b.Share)
	}
}

func TestTheHouseBaseLoadIsTheMainsLegsAddedAndNeverOneLegShort(t *testing.T) {
	srv, start := runsServer(t)
	var b baseBody
	get(t, baseURL(srv, "baseload", start, start.Add(72*time.Hour)), &b)
	// Leg A rests at 500 W and leg B at 400 W. Leg B is missing for an hour: those minutes are left out, not counted at 500 W.
	if b.Scope != "house" || math.Abs(b.Watts-900) > 1e-9 || b.Minutes != 3*1440-60 {
		t.Errorf("house base = %+v, want 900 W over %d minutes", b, 3*1440-60)
	}
	// No price saved: no cost, not a zero.
	if b.CostYear != nil || b.Rate != nil {
		t.Errorf("cost %v at %v with no price", b.CostYear, b.Rate)
	}
}

func TestBaseLoadNeedsAnHourAndKnowsWhichMeters(t *testing.T) {
	srv, start := runsServer(t)
	if r := get(t, baseURL(srv, "meters/charger/baseload", start, start.Add(30*time.Minute)), nil); r.StatusCode != http.StatusNoContent {
		t.Errorf("half an hour: %d, want 204", r.StatusCode)
	}
	if r := get(t, baseURL(srv, "meters/nope/baseload", start, start.Add(time.Hour)), nil); r.StatusCode != http.StatusNotFound {
		t.Errorf("unknown meter: %d, want 404", r.StatusCode)
	}
	if r := get(t, baseURL(srv, "baseload", start.Add(time.Hour), start), nil); r.StatusCode != http.StatusBadRequest {
		t.Errorf("backwards period: %d, want 400", r.StatusCode)
	}
}

func TestTheHouseBreakdownListsOnlyMetersThatHoldNothingAndNeverAddsThem(t *testing.T) {
	srv, start := runsServer(t)
	var b struct {
		Watts float64 `json:"watts"`
		Parts []struct {
			MeterID string  `json:"meter_id"`
			Watts   float64 `json:"watts"`
		} `json:"parts"`
	}
	get(t, baseURL(srv, "baseload", start, start.Add(72*time.Hour)), &b)
	// The fridge rests at 90 W and the charger at 3 W; the mains are the house itself and are not parts of it.
	if len(b.Parts) != 2 || b.Parts[0].MeterID != "fridge" || b.Parts[1].MeterID != "charger" || b.Parts[0].Watts < b.Parts[1].Watts {
		t.Fatalf("parts = %+v", b.Parts)
	}
	if b.Parts[0].Watts < 89 || b.Parts[0].Watts > 93 {
		t.Errorf("fridge floor = %v, want about 90 W", b.Parts[0].Watts)
	}
}
