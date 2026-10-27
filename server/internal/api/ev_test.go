package api

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"testing"

	"github.com/gerp93/KVGuzzler/server/internal/model"
)

// evCircuit finds a simulated circuit that used energy in the first two hours, so a stretch of miles has something to divide.
func evCircuit(t *testing.T, srvURL string) (id string, kwh float64) {
	t.Helper()
	var s model.Summary
	// The two local days the readings below are on (the test server's zone is UTC).
	from, to := "2026-10-22T00:00:00Z", "2026-10-23T00:00:00Z"
	get(t, fmt.Sprintf("%s/api/v1/summary?from=%s&to=%s", srvURL, from, to), &s)
	for _, m := range s.Meters {
		if m.Scope == model.ScopeCircuit && m.KWh > 0.01 && (id == "" || m.KWh > kwh) {
			id, kwh = m.MeterID, m.KWh
		}
	}
	if id == "" {
		t.Fatal("no circuit used any energy")
	}
	return id, kwh
}

type evBody struct {
	MeterID   string                  `json:"meter_id"`
	Readings  []model.OdometerReading `json:"readings"`
	Intervals []struct {
		FromDay       string   `json:"from_day"`
		ToDay         string   `json:"to_day"`
		Miles         float64  `json:"miles"`
		KWh           float64  `json:"kwh"`
		Cost          *string  `json:"cost"`
		CostPerMile   *string  `json:"cost_per_mile"`
		MilesPerKWh   *float64 `json:"miles_per_kwh"`
		Coverage      float64  `json:"coverage"`
		CostEstimated bool     `json:"cost_estimated"`
	} `json:"intervals"`
	Totals *struct {
		Miles       float64  `json:"miles"`
		KWh         float64  `json:"kwh"`
		Cost        *string  `json:"cost"`
		CostPerMile *string  `json:"cost_per_mile"`
		MilesPerKWh *float64 `json:"miles_per_kwh"`
	} `json:"totals"`
	SinceLast *struct {
		FromDay string  `json:"from_day"`
		KWh     float64 `json:"kwh"`
	} `json:"since_last"`
}

func TestAChargingMeterTurnsOdometerReadingsIntoCostPerMile(t *testing.T) {
	srv, _ := newServer(t, 120)
	id, kwh := evCircuit(t, srv.URL)
	base := srv.URL + "/api/v1/meters/" + id
	put(t, srv.URL+"/api/v1/rates", `{"mode":"one","fixed_rate":"0.20","currency":"USD"}`)

	// Not an EV yet: no odometer, and the readings are refused.
	if r := get(t, base+"/ev", nil); r.StatusCode != http.StatusConflict {
		t.Fatalf("an ordinary meter's /ev: %d, want 409", r.StatusCode)
	}
	if r, _ := put(t, base+"/ev/odometer/2026-10-22", `{"miles":1000}`); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("a reading on an ordinary meter: %d, want 400", r.StatusCode)
	}
	if r := patch(t, base, `{"ev":true}`); r.StatusCode != http.StatusOK {
		t.Fatalf("mark as EV: %d", r.StatusCode)
	}
	var meters struct{ Meters []model.Meter }
	get(t, srv.URL+"/api/v1/meters", &meters)
	for _, m := range meters.Meters {
		if m.ID == id && !m.EV {
			t.Error("the meter does not say it is an EV")
		}
	}

	// One reading has no miles yet, only what was used since.
	r, body := put(t, base+"/ev/odometer/2026-10-22", `{"miles":1000}`)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("first reading: %d %s", r.StatusCode, body)
	}
	var ev evBody
	get(t, base+"/ev", &ev)
	if len(ev.Readings) != 1 || ev.Totals != nil || len(ev.Intervals) != 0 || ev.SinceLast == nil || math.Abs(ev.SinceLast.KWh-kwh) > kwh*0.01 { // it stops at the server's clock, before the last simulated minute
		t.Fatalf("after one reading: %+v since=%+v (the meter used %v kWh)", ev, ev.SinceLast, kwh)
	}

	// A second reading a day later: 100 miles over the energy the meter used in between, at 20 cents.
	if r, body := put(t, base+"/ev/odometer/2026-10-23", `{"miles":1100}`); r.StatusCode != http.StatusOK {
		t.Fatalf("second reading: %d %s", r.StatusCode, body)
	}
	ev = evBody{}
	get(t, base+"/ev", &ev)
	if len(ev.Intervals) != 1 || ev.Totals == nil {
		t.Fatalf("after two readings: %+v", ev)
	}
	iv := ev.Intervals[0]
	if iv.Miles != 100 || math.Abs(iv.KWh-kwh) > 1e-6 || iv.FromDay != "2026-10-22" || iv.ToDay != "2026-10-23" {
		t.Errorf("interval = %+v, want 100 miles over %v kWh", iv, kwh)
	}
	wantPerMile := kwh * 0.20 / 100
	if iv.CostPerMile == nil {
		t.Fatalf("no cost per mile with a price set: %+v", iv)
	}
	if got, _ := strconv.ParseFloat(*iv.CostPerMile, 64); math.Abs(got-wantPerMile) > 1e-5 {
		t.Errorf("cost per mile = %s, want about %.6f", *iv.CostPerMile, wantPerMile)
	}
	if iv.MilesPerKWh == nil || math.Abs(*iv.MilesPerKWh-100/kwh) > 1e-6 {
		t.Errorf("miles per kWh = %v, want %v", iv.MilesPerKWh, 100/kwh)
	}
	if iv.Coverage <= 0 || iv.Coverage > 1 {
		t.Errorf("coverage = %v: it must be a share of the stretch the meter was measured", iv.Coverage)
	}
	if ev.Totals.Miles != 100 || ev.Totals.CostPerMile == nil || *ev.Totals.CostPerMile != *iv.CostPerMile {
		t.Errorf("totals = %+v, want the one stretch", ev.Totals)
	}

	// Readings from before the meter was read have no energy to divide: no cost per mile, never a free mile.
	put(t, base+"/ev/odometer/2026-09-01", `{"miles":900}`)
	ev = evBody{}
	get(t, base+"/ev", &ev)
	if len(ev.Intervals) != 2 || ev.Intervals[0].KWh != 0 || ev.Intervals[0].CostPerMile != nil || ev.Intervals[0].Coverage != 0 {
		t.Errorf("a stretch before the meter was read: %+v", ev.Intervals)
	}
	if r, _ := put(t, base+"/ev/odometer/2026-09-01", `{"miles":1000}`); r.StatusCode != http.StatusOK {
		t.Errorf("a reading equal to the next one: %d", r.StatusCode)
	}
	del0 := func() {
		req, _ := http.NewRequest(http.MethodDelete, base+"/ev/odometer/2026-09-01", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	del0()

	// An odometer only goes up: a reading below the one before, or above the one after, is refused and changes nothing.
	for _, bad := range []struct{ day, body string }{
		{"2026-10-24", `{"miles":1050}`}, {"2026-10-22", `{"miles":1500}`}, {"2026-10-25", `{"miles":-1}`}, {"2026-13-40", `{"miles":5}`}, {"2026-10-25", `{"miles":"far"}`},
	} {
		if r, _ := put(t, base+"/ev/odometer/"+bad.day, bad.body); r.StatusCode != http.StatusBadRequest {
			t.Errorf("%s %s: %d, want 400", bad.day, bad.body, r.StatusCode)
		}
	}
	ev = evBody{}
	get(t, base+"/ev", &ev)
	if len(ev.Readings) != 2 {
		t.Errorf("a refused reading was kept: %+v", ev.Readings)
	}

	// Correcting a day replaces it; removing one takes it away; taking the mark off keeps the readings.
	put(t, base+"/ev/odometer/2026-10-23", `{"miles":1200}`)
	ev = evBody{}
	get(t, base+"/ev", &ev)
	if ev.Totals == nil || ev.Totals.Miles != 200 {
		t.Errorf("a corrected reading: %+v", ev.Totals)
	}
	patch(t, base, `{"ev":false}`)
	patch(t, base, `{"ev":true}`)
	ev = evBody{}
	get(t, base+"/ev", &ev)
	if len(ev.Readings) != 2 {
		t.Errorf("unmarking and marking again lost the readings: %+v", ev.Readings)
	}
	del := func(path string) int {
		req, _ := http.NewRequest(http.MethodDelete, base+path, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if c := del("/ev/odometer/2026-10-23"); c != http.StatusNoContent {
		t.Errorf("delete: %d", c)
	}
	if c := del("/ev/odometer/2026-10-23"); c != http.StatusNotFound {
		t.Errorf("delete again: %d, want 404", c)
	}
}

func TestOnlyACircuitOrAPlugCanChargeAnEV(t *testing.T) {
	srv, _ := newServer(t, 1)
	if r := patch(t, srv.URL+"/api/v1/meters/sim-em16p:mains-a", `{"ev":true}`); r.StatusCode != http.StatusBadRequest {
		t.Errorf("the mains as an EV: %d, want 400", r.StatusCode)
	}
	if r := patch(t, srv.URL+"/api/v1/meters/nope", `{"ev":true}`); r.StatusCode != http.StatusNotFound {
		t.Errorf("an unknown meter: %d, want 404", r.StatusCode)
	}
}
