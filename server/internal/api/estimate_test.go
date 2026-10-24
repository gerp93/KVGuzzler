package api

import (
	"encoding/json"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/pcrelay"
)

func hoursOfSensors(t *testing.T, start time.Time, minutes int, watts float64) string {
	t.Helper()
	in := pcrelay.Ingest{APIVersion: 1, SourceID: "pc-est", SourceName: "Desk"}
	for i := 0; i < minutes; i++ {
		in.Minutes = append(in.Minutes, pcrelay.Minute{
			Minute:   start.Add(time.Duration(i) * time.Minute),
			Readings: []pcrelay.Reading{{Name: "CPU package", Scope: "component", Kind: "cpu", AvgWatts: watts, MaxWatts: watts, Samples: 60}},
		})
	}
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func estimateOf(t *testing.T, srvURL, id, query string) (model.ComputerEstimate, int) {
	t.Helper()
	var e model.ComputerEstimate
	r := get(t, srvURL+"/api/v1/computers/"+id+"/estimate"+query, &e)
	return e, r.StatusCode
}

func TestTheEstimateGivesTodaysPriceAndTheRealUsagePerDayOverTheTimeRecorded(t *testing.T) {
	start := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	clock := start.Add(3 * time.Hour)
	srv := movingServer(t, &clock)
	if r, body := postBody(t, srv.URL+"/api/v1/ingest/pc", hoursOfSensors(t, start, 180, 100)); r.StatusCode != 200 {
		t.Fatalf("%d %s", r.StatusCode, body)
	}
	id := detailOf(t, srv).Computer.ID

	// No price yet; the usage is already known: 100 W for the 3 hours recorded, though a day was asked for.
	e, code := estimateOf(t, srv.URL, id, "?window=24h")
	if code != http.StatusOK || e.Rate != nil || e.Usage == nil {
		t.Fatalf("estimate = %+v (%d)", e, code)
	}
	u := e.Usage
	if u.WholePC || math.Abs(u.Days-0.125) > 1e-9 || math.Abs(u.KWh-0.3) > 1e-9 || math.Abs(u.KWhPerDay-2.4) > 1e-9 || math.Abs(u.AverageWatts-100) > 1e-6 {
		t.Errorf("usage = %+v: 0.3 kWh over the 3 h recorded is 2.4 kWh a day, a steady 100 W", u)
	}

	put(t, srv.URL+"/api/v1/rates", `{"mode":"one","fixed_rate":"0.20","currency":"EUR"}`)
	if e, _ = estimateOf(t, srv.URL, id, ""); e.Rate == nil || *e.Rate != "0.20" || e.Currency != "EUR" || e.RateEstimated {
		t.Errorf("rate = %+v", e)
	}

	if _, code := estimateOf(t, srv.URL, id, "?window=fortnight"); code != http.StatusBadRequest {
		t.Errorf("bad window: %d, want 400", code)
	}
	if r := get(t, srv.URL+"/api/v1/computers/comp-nope/estimate", nil); r.StatusCode != http.StatusNotFound {
		t.Errorf("unknown computer: %d, want 404", r.StatusCode)
	}
}

func TestTheEstimateSaysNothingAboutUsageUntilAnHourIsRecorded(t *testing.T) {
	start := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	clock := start.Add(40 * time.Minute)
	srv := movingServer(t, &clock)
	postBody(t, srv.URL+"/api/v1/ingest/pc", hoursOfSensors(t, start, 30, 100))
	id := detailOf(t, srv).Computer.ID
	if e, _ := estimateOf(t, srv.URL, id, ""); e.Usage != nil {
		t.Errorf("usage = %+v, want none: only 40 minutes are recorded", e.Usage)
	}
}

func TestAComputerWithNothingRecordedHasNoUsage(t *testing.T) {
	clock := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	srv := movingServer(t, &clock)
	_, body := postBody(t, srv.URL+"/api/v1/computers", `{"name":"Empty"}`)
	var made model.Computer
	if err := json.Unmarshal([]byte(body), &made); err != nil {
		t.Fatal(err)
	}
	if e, code := estimateOf(t, srv.URL, made.ID, ""); code != http.StatusOK || e.Usage != nil {
		t.Errorf("estimate = %+v (%d)", e, code)
	}
}

func TestTheEstimateCountsTheWallPlugAndOnlyThePlugsAskedFor(t *testing.T) {
	srv, st := computerServer(t) // the clock is 2026-10-22 19:00 UTC
	ctx := t.Context()
	start := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	for _, ch := range []model.Meter{
		{ID: "wall", DeviceID: "d-wall", Scope: model.ScopePC, Label: "PC"},
		{ID: "monitor", DeviceID: "d-mon", Scope: model.ScopePlug, Label: "Monitor"},
		{ID: "speakers", DeviceID: "d-spk", Scope: model.ScopePlug, Label: "Speakers"},
		{ID: "kettle", DeviceID: "d-ket", Scope: model.ScopePlug, Label: "Kettle"},
	} {
		if err := st.UpsertMeter(ctx, ch); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 120; i++ { // two hours at 300 W, 50 W, 10 W and 2000 W
		at := start.Add(time.Duration(i) * time.Minute)
		for id, w := range map[string]float64{"wall": 300, "monitor": 50, "speakers": 10, "kettle": 2000} {
			if err := st.AddMinute(ctx, id, at, w, w, 60); err != nil {
				t.Fatal(err)
			}
		}
	}
	_, body := send(t, http.MethodPost, srv.URL+"/api/v1/computers", `{"name":"Desktop"}`)
	comp := decode[model.Computer](t, body)
	for _, ch := range []string{"wall", "monitor", "speakers"} { // the kettle is not part of this computer
		send(t, http.MethodPatch, srv.URL+"/api/v1/meters/"+ch, `{"computer_id":"`+comp.ID+`"}`)
	}
	base := srv.URL + "/api/v1/computers/" + comp.ID + "/estimate"
	usage := func(query string) *model.AverageUse {
		t.Helper()
		var e model.ComputerEstimate
		if r := get(t, base+query, &e); r.StatusCode != 200 {
			t.Fatalf("%s: %d", query, r.StatusCode)
		}
		return e.Usage
	}

	u := usage("?window=24h")
	if u == nil || !u.WholePC || !u.IncludesPC || u.PeripheralCount != 0 || math.Abs(u.KWh-0.6) > 1e-9 || math.Abs(u.Days-7.0/24) > 1e-9 {
		t.Fatalf("pc alone = %+v: 0.6 kWh over the 7 h since recording began", u)
	}
	if want := 0.6 / (7.0 / 24); math.Abs(u.KWhPerDay-want) > 1e-9 {
		t.Errorf("per day = %v, want %v: it counts the hours nothing was drawn too", u.KWhPerDay, want)
	}

	u = usage("?window=24h&peripherals=monitor,speakers,kettle,nope")
	if u.PeripheralCount != 2 || math.Abs(u.KWh-0.72) > 1e-9 {
		t.Errorf("with plugs = %+v: the monitor and speakers count (0.1 + 0.02), the kettle and a made-up id do not", u)
	}
	u = usage("?window=24h&include_pc=false&peripherals=monitor")
	if u == nil || u.IncludesPC || u.WholePC || u.PeripheralCount != 1 || math.Abs(u.KWh-0.1) > 1e-9 {
		t.Errorf("plugs only = %+v", u)
	}
	if u = usage("?window=24h&include_pc=false"); u != nil {
		t.Errorf("nothing to count = %+v, want no usage", u)
	}
}
