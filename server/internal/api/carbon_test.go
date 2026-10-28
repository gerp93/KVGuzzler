package api

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/carbon"
	"github.com/gerp93/KVGuzzler/server/internal/collector"
	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

// eiaOK is EIA's answer for one complete hour (22:00 UTC on 22 Oct 2026), about 373 g/kWh.
func eiaRows(period string) string {
	row := func(fuel, mwh string) string {
		return `{"period":"` + period + `","respondent":"MISO","fueltype":"` + fuel + `","value":"` + mwh + `","value-units":"megawatthours"}`
	}
	return row("COL", "16146") + "," + row("NG", "20970") + "," + row("NUC", "8329") + "," + row("WND", "18383")
}

func eiaOK(w http.ResponseWriter, _ *http.Request) {
	fmt.Fprint(w, `{"response":{"data":[`+eiaRows("2026-10-22T22")+`]}}`)
}

// carbonServer is a server whose grid-intensity source is a fake EIA the test controls.
func carbonServer(t *testing.T, respond func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, *store.Store, *[]*http.Request) {
	t.Helper()
	var seen []*http.Request
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r)
		respond(w, r)
	}))
	t.Cleanup(fake.Close)
	st, err := store.Open(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	clock := time.Date(2026, 10, 22, 23, 0, 0, 0, time.UTC)
	api := &Server{Store: st, Collector: collector.New(st, time.Second), Loc: time.UTC, Version: "test", Now: func() time.Time { return clock },
		Carbon: &carbon.Client{EiaBase: fake.URL + "/eia/"}}
	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)
	return srv, st, &seen
}

func okMapsAnswer(w http.ResponseWriter, r *http.Request) { eiaOK(w, r) }

func TestTheCarbonSetupOnlyKeepsAKeyAndPlaceThatGotAReadingAndNeverShowsTheKeyAgain(t *testing.T) {
	status := 403
	srv, st, seen := carbonServer(t, func(w http.ResponseWriter, r *http.Request) {
		if status != 200 {
			w.WriteHeader(status)
			return
		}
		eiaOK(w, r)
	})
	url := srv.URL + "/api/v1/carbon"
	day := func() (time.Time, time.Time) {
		return time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	}
	const key = "abcdefghijklmnopqrstuvwxyz0123456789ABCD"

	var v model.CarbonConfig
	if get(t, url, &v); v.Configured {
		t.Fatal("a fresh server is not set up")
	}
	for name, body := range map[string]string{
		"no key":                              `{"place":"MISO"}`,
		"bad key":                             `{"key":"no","place":"MISO"}`,
		"no place":                            `{"key":"` + key + `"}`,
		"bad place":                           `{"key":"` + key + `","place":"x&y=1"}`,
		"a zone":                              `{"key":"` + key + `","place":"US-MIDW-MISO"}`,
		"unknown key":                         `{"key":"` + key + `","place":"MISO","extra":1}`,
		"an old source field is not accepted": `{"source":"electricitymaps","key":"` + key + `","place":"MISO"}`,
	} {
		if r, _ := send(t, http.MethodPut, url, body); r.StatusCode != 400 {
			t.Errorf("%s: %d, want 400", name, r.StatusCode)
		}
	}

	// The key is refused by EIA: not saved, and the reason is EIA's.
	r, body := send(t, http.MethodPut, url, `{"key":"`+key+`","place":"MISO"}`)
	if r.StatusCode != 400 || !strings.Contains(body, "Not saved. EIA did not accept the key") {
		t.Errorf("refused key: %d %s", r.StatusCode, body)
	}
	if get(t, url, &v); v.Configured {
		t.Error("a key that got no reading was kept")
	}

	status = 200
	r, body = send(t, http.MethodPut, url, `{"key":"`+key+`","place":"miso"}`)
	if r.StatusCode != 200 || strings.Contains(body, key) {
		t.Fatalf("save: %d %s", r.StatusCode, body)
	}
	v = decode[model.CarbonConfig](t, body)
	if !v.Configured || v.Place != "MISO" || v.LastGrams == nil || math.Abs(*v.LastGrams-373.1) > 0.5 || v.LastFetch == nil {
		t.Errorf("config = %+v", v)
	}
	from, to := day()
	if hours, _ := st.QueryCarbon(t.Context(), from, to); len(hours) != 1 {
		t.Errorf("the reading was not saved: %v", hours)
	}

	// Leaving the key out keeps the saved one and uses it.
	if r, _ := send(t, http.MethodPut, url, `{"place":"PJM"}`); r.StatusCode != 200 {
		t.Fatalf("change place: %d", r.StatusCode)
	}
	if last := (*seen)[len(*seen)-1]; !strings.Contains(last.URL.RawQuery, "api_key="+key) || !strings.Contains(last.URL.RawQuery, "PJM") {
		t.Errorf("the saved key was not used: %s", last.URL.RawQuery)
	}

	// Removing it keeps the hours already saved.
	if r, _ := send(t, http.MethodDelete, url, ""); r.StatusCode != http.StatusNoContent {
		t.Errorf("remove: %d", r.StatusCode)
	}
	if get(t, url, &v); v.Configured || v.LastOutcome == nil || *v.LastOutcome != "Not set up." {
		t.Errorf("after removing: %+v", v)
	}
	if hours, _ := st.QueryCarbon(t.Context(), from, to); len(hours) != 1 {
		t.Errorf("removing the key lost the saved hours: %v", hours)
	}
}

func TestAKeyIsNeverWrittenIntoTheDatabaseFile(t *testing.T) {
	srv, st, _ := carbonServer(t, eiaOK)
	const key = "abcdefghijklmnopqrstuvwxyz0123456789ABCD"
	if r, b := send(t, http.MethodPut, srv.URL+"/api/v1/carbon", `{"key":"`+key+`","place":"MISO"}`); r.StatusCode != 200 {
		t.Fatalf("%d %s", r.StatusCode, b)
	}
	if r, b := send(t, http.MethodPut, srv.URL+"/api/v1/openei", `{"api_key":"openeikey123456"}`); r.StatusCode != 200 {
		t.Fatalf("%d %s", r.StatusCode, b)
	}
	// Whatever the database holds (the rows, and the write-ahead log beside it), neither key is in it.
	dir := st.DataDir()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() == "secrets.json" {
			continue
		}
		b, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		if strings.Contains(string(b), key) || strings.Contains(string(b), "openeikey123456") {
			t.Errorf("a key is in %s", e.Name())
		}
	}
	if b, err := os.ReadFile(filepath.Join(dir, "secrets.json")); err != nil || !strings.Contains(string(b), key) {
		t.Errorf("the secrets file: %v", err)
	}
}

func TestMetersCarryTheCarbonOfTheirEnergyHourByHour(t *testing.T) {
	srv, st, _ := carbonServer(t, okMapsAnswer)
	ctx := t.Context()
	if err := st.UpsertMeter(ctx, model.Meter{ID: "wall", DeviceID: "d", Scope: model.ScopePC, Label: "PC"}); err != nil {
		t.Fatal(err)
	}
	// 12:00, 13:00 and 14:00 each use 0.1, 0.2 and 0.1 kWh; the first two hours have an intensity of their own.
	energy := func(hour int, kwh float64) {
		for m := 0; m < 60; m++ {
			if err := st.AddMinute(ctx, "wall", time.Date(2026, 10, 22, hour, m, 0, 0, time.UTC), kwh*1000, kwh*1000, 60); err != nil {
				t.Fatal(err)
			}
		}
	}
	energy(12, 0.1)
	energy(13, 0.2)
	energy(14, 0.1)
	period := "?from=2026-10-22T00:00:00Z&to=2026-10-23T00:00:00Z"
	summary := func() model.MeterSummary {
		var sum model.Summary
		get(t, srv.URL+"/api/v1/summary"+period, &sum)
		return sum.Meters[0]
	}
	if cs := summary(); cs.CarbonG != nil {
		t.Fatalf("with no intensity ever saved there is nothing to show: %v", *cs.CarbonG)
	}

	_ = st.AddCarbon(ctx, time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC), 400, false)
	_ = st.AddCarbon(ctx, time.Date(2026, 10, 22, 13, 0, 0, 0, time.UTC), 200, true)
	cs := summary()
	// 0.1 x 400 + 0.2 x 200 + 0.1 at the average of the known hours (300) = 40 + 40 + 30 g
	if cs.CarbonG == nil || math.Abs(*cs.CarbonG-110) > 1e-6 || cs.CarbonEstimatedKWh == nil || math.Abs(*cs.CarbonEstimatedKWh-0.1) > 1e-9 {
		t.Errorf("carbon = %v g, estimated %v kWh", cs.CarbonG, cs.CarbonEstimatedKWh)
	}
}

func TestThePcSummaryAndTheCsvCarryCarbonOnceThereIsIntensityData(t *testing.T) {
	srv, st, _ := carbonServer(t, okMapsAnswer)
	postBody(t, srv.URL+"/api/v1/ingest/pc", contractExample(t, "ingest-pc.json"))
	var list struct{ Computers []model.Computer }
	get(t, srv.URL+"/api/v1/computers", &list)
	id := list.Computers[0].ID
	period := "?from=2026-10-22T00:00:00Z&to=2026-10-23T00:00:00Z"

	var d model.ComputerDetail
	get(t, srv.URL+"/api/v1/computers/"+id+"/detail"+period, &d)
	if d.PC.CarbonG != nil {
		t.Errorf("carbon with no intensity saved: %v", *d.PC.CarbonG)
	}
	csv := func() string {
		resp, err := http.Get(srv.URL + "/api/v1/computers/" + id + "/export.csv" + period)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}
	if strings.Contains(csv(), "co2") {
		t.Error("the CSV has carbon columns with no intensity saved")
	}

	_ = st.AddCarbon(t.Context(), time.Date(2026, 10, 22, 18, 0, 0, 0, time.UTC), 400, false)
	get(t, srv.URL+"/api/v1/computers/"+id+"/detail"+period, &d)
	if d.PC.CarbonG == nil || *d.PC.CarbonG <= 0 {
		t.Errorf("pc carbon = %v", d.PC.CarbonG)
	}
	rows := lines(csv())
	if !strings.HasSuffix(rows[0], ",pc_co2_kg,peripherals_co2_kg") {
		t.Fatalf("csv = %q", rows)
	}
	cells := strings.Split(rows[1], ",")
	if len(cells) != 7 || cells[5] == "" || cells[6] != "0" { // a PC's kilograms, and no meters counted as zero
		t.Errorf("row = %q", rows[1])
	}
}

func TestTheCarbonSeriesIsTheHousesEnergyAtEachHoursIntensityAndAddsUpToTheSummary(t *testing.T) {
	srv, st := houseServer(t) // the mains used 0.8 kW for two hours from 12:00 on 22 Oct 2026; the dryer is a circuit, no part of the house
	_ = st.AddCarbon(t.Context(), time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC), 400, false)
	_ = st.AddCarbon(t.Context(), time.Date(2026, 10, 22, 14, 0, 0, 0, time.UTC), 300, false) // an hour with a figure and no energy
	var s model.CarbonSeries
	get(t, srv.URL+"/api/v1/carbon/series"+period, &s)
	if s.Bucket != model.BucketHour || len(s.Points) != 3 {
		t.Fatalf("bucket %q with %d points, want hour with 3: %+v", s.Bucket, len(s.Points), s.Points)
	}
	p := s.Points[0]
	if p.GridGramsPerKWh == nil || *p.GridGramsPerKWh != 400 || math.Abs(p.KWh-0.8) > 1e-9 || math.Abs(p.CarbonG-320) > 1e-6 || p.EstimatedKWh != 0 {
		t.Errorf("12:00: %+v", p)
	}
	if q := s.Points[1]; math.Abs(q.EstimatedKWh-0.8) > 1e-9 || q.GridGramsPerKWh != nil { // 13:00 has none of its own: valued at the average (350)
		t.Errorf("13:00: %+v", q)
	}
	if s.GridAverageGramsPerKWh == nil || *s.GridAverageGramsPerKWh != 350 { // the average of the two saved hours, 400 and 300
		t.Errorf("grid average = %v, want 350", s.GridAverageGramsPerKWh)
	}
	if q := s.Points[2]; q.KWh != 0 || q.GridGramsPerKWh == nil || *q.GridGramsPerKWh != 300 {
		t.Errorf("14:00 (grid only): %+v", q)
	}
	var sum model.Summary
	get(t, srv.URL+"/api/v1/summary"+period, &sum)
	var total float64
	for _, q := range s.Points {
		total += q.CarbonG
	}
	if sum.HouseCarbonG == nil || math.Abs(total-*sum.HouseCarbonG) > 1e-6 {
		t.Errorf("the points add up to %v g, the summary says %v", total, sum.HouseCarbonG)
	}
}

func TestTheCarbonSeriesIsEmptyBeforeAnyIntensityWasSaved(t *testing.T) {
	srv, _ := houseServer(t)
	var s model.CarbonSeries
	get(t, srv.URL+"/api/v1/carbon/series"+period, &s)
	if s.Points == nil || len(s.Points) != 0 {
		t.Errorf("points = %v, want an empty list", s.Points)
	}
}

func TestTheCarbonSeriesStartsWhereTheHousesRecordDoes(t *testing.T) {
	srv, st := houseServer(t) // the mains' first minute is 12:00 on 22 Oct 2026
	_ = st.AddCarbon(t.Context(), time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC), 500, false)
	_ = st.AddCarbon(t.Context(), time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC), 400, false)
	var s model.CarbonSeries
	get(t, srv.URL+"/api/v1/carbon/series?from=2026-10-20T00:00:00Z&to=2026-10-23T00:00:00Z", &s)
	if len(s.Points) == 0 || !s.Points[0].T.Equal(time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("the series should begin at 12:00 on the 22nd, not with the grid hour on the 21st: %+v", s.Points)
	}
}
