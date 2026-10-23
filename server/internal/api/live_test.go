package api

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/collector"
	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

// movingServer is a server whose clock the test can move.
func movingServer(t *testing.T, clock *time.Time) *httptest.Server {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	api := &Server{Store: s, Collector: collector.New(s, time.Second), Loc: time.UTC, Version: "test", Now: func() time.Time { return *clock }}
	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)
	return srv
}

func detailOf(t *testing.T, srv *httptest.Server) model.ComputerDetail {
	t.Helper()
	var list struct{ Computers []model.Computer }
	get(t, srv.URL+"/api/v1/computers", &list)
	if len(list.Computers) != 1 {
		t.Fatalf("computers = %+v", list.Computers)
	}
	var d model.ComputerDetail
	get(t, srv.URL+"/api/v1/computers/"+list.Computers[0].ID+"/detail"+period, &d)
	return d
}

func TestTheLiveLookAndRunsReachTheComputerPageAndItsAgeIsByTheServersClock(t *testing.T) {
	clock := time.Date(2026, 10, 22, 19, 0, 0, 0, time.UTC)
	srv := movingServer(t, &clock)

	resp, body := postBody(t, srv.URL+"/api/v1/ingest/pc", contractExample(t, "ingest-pc.json"))
	if resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}

	d := detailOf(t, srv)
	if d.PC == nil || d.PC.Live == nil {
		t.Fatalf("pc = %+v", d.PC)
	}
	l := d.PC.Live
	if len(l.Readings) != 3 || l.Readings[0].Name != "CPU package" || len(l.Programs) != 1 || l.Programs[0].GPUWatts != 160.2 ||
		len(l.Notes) != 1 || l.IdleCPUWatts == nil || *l.IdleCPUWatts != 18.5 {
		t.Errorf("live = %+v", l)
	}
	if l.AgeSeconds != 0 {
		t.Errorf("age = %v, want 0: it has just arrived", l.AgeSeconds)
	}
	if len(d.PC.Runs) != 1 || d.PC.Runs[0].Name != "python.exe (ComfyUI)" || !d.PC.Runs[0].StillRunning {
		t.Errorf("runs = %+v", d.PC.Runs)
	}

	// The age is how long ago the server received it, by the server's clock, whatever the PC's clock said.
	clock = clock.Add(95 * time.Second)
	if age := detailOf(t, srv).PC.Live.AgeSeconds; math.Abs(age-95) > 0.5 {
		t.Errorf("age = %v, want 95", age)
	}
}

func TestALiveOnlyPushIsAcceptedAndKeepsTheMinutesAlreadyStored(t *testing.T) {
	clock := time.Date(2026, 10, 22, 19, 0, 0, 0, time.UTC)
	srv := movingServer(t, &clock)
	postBody(t, srv.URL+"/api/v1/ingest/pc", contractExample(t, "ingest-pc.json"))
	before := detailOf(t, srv)

	clock = clock.Add(5 * time.Second)
	liveOnly := `{"api_version":1,"source_id":"pc-7f3a9c1e","minutes":[],"live":{"at":"2026-10-22T18:59:58Z","readings":[{"name":"CPU package","scope":"component","kind":"cpu","watts":88.8}],"programs":[],"notes":[]}}`
	resp, body := postBody(t, srv.URL+"/api/v1/ingest/pc", liveOnly)
	if resp.StatusCode != 200 || !strings.Contains(body, `"accepted":0`) {
		t.Fatalf("live only: %d %s", resp.StatusCode, body)
	}

	after := detailOf(t, srv)
	if after.PC.Live.Readings[0].Watts != 88.8 || len(after.PC.Live.Programs) != 0 {
		t.Errorf("live = %+v, want the newer look", after.PC.Live)
	}
	if len(after.PC.Components) != len(before.PC.Components) || len(after.PC.Programs) != len(before.PC.Programs) || len(after.PC.Runs) != 1 {
		t.Errorf("a live-only push changed the stored minutes or runs: %d/%d components, %d/%d programs, %d runs",
			len(after.PC.Components), len(before.PC.Components), len(after.PC.Programs), len(before.PC.Programs), len(after.PC.Runs))
	}
}

func TestABadLiveTimeIsIgnoredButTheMinutesInThePushAreKept(t *testing.T) {
	clock := time.Date(2026, 10, 22, 19, 0, 0, 0, time.UTC)
	srv := movingServer(t, &clock)
	example := strings.Replace(contractExample(t, "ingest-pc.json"), `"at": "2026-10-22T18:04:11Z"`, `"at": "2030-01-01T00:00:00Z"`, 1)
	if r, body := postBody(t, srv.URL+"/api/v1/ingest/pc", example); r.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", r.StatusCode, body)
	}
	d := detailOf(t, srv)
	if d.PC.Live != nil || len(d.PC.Components) == 0 {
		t.Errorf("live %+v, components %d: the snapshot should be dropped and the minutes kept", d.PC.Live, len(d.PC.Components))
	}
}

func TestTheLightLiveEndpointGivesJustTheLookWithItsAgeAnd204WhenThereIsNone(t *testing.T) {
	clock := time.Date(2026, 10, 22, 19, 0, 0, 0, time.UTC)
	srv := movingServer(t, &clock)

	// A computer with no collector: nothing to give.
	r0, body := postBody(t, srv.URL+"/api/v1/computers", `{"name":"Lonely"}`)
	var made model.Computer
	if err := json.Unmarshal([]byte(body), &made); err != nil || made.ID == "" {
		t.Fatalf("add computer: %d %s", r0.StatusCode, body)
	}
	if r := get(t, srv.URL+"/api/v1/computers/"+made.ID+"/live", nil); r.StatusCode != http.StatusNoContent {
		t.Errorf("no collector: %d, want 204", r.StatusCode)
	}
	if r := get(t, srv.URL+"/api/v1/computers/comp-nope/live", nil); r.StatusCode != http.StatusNotFound {
		t.Errorf("unknown computer: %d, want 404", r.StatusCode)
	}

	postBody(t, srv.URL+"/api/v1/ingest/pc", contractExample(t, "ingest-pc.json"))
	var list struct{ Computers []model.Computer }
	get(t, srv.URL+"/api/v1/computers", &list)
	var withPC string
	for _, c := range list.Computers {
		if c.SourceID != nil {
			withPC = c.ID
		}
	}
	clock = clock.Add(12 * time.Second)
	var live model.PcLive
	if r := get(t, srv.URL+"/api/v1/computers/"+withPC+"/live", &live); r.StatusCode != http.StatusOK {
		t.Fatalf("live: %d", r.StatusCode)
	}
	if len(live.Readings) != 3 || math.Abs(live.AgeSeconds-12) > 0.5 {
		t.Errorf("live = %+v", live)
	}
}

func TestTheComputerSeriesHasALinePerPartAndGapsAnd204WithoutACollector(t *testing.T) {
	clock := time.Date(2026, 10, 22, 19, 0, 0, 0, time.UTC)
	srv := movingServer(t, &clock)
	period := "?from=2026-10-22T00:00:00Z&to=2026-10-23T00:00:00Z"

	_, body := postBody(t, srv.URL+"/api/v1/computers", `{"name":"Lonely"}`)
	var made model.Computer
	if err := json.Unmarshal([]byte(body), &made); err != nil {
		t.Fatal(err)
	}
	if r := get(t, srv.URL+"/api/v1/computers/"+made.ID+"/series"+period, nil); r.StatusCode != http.StatusNoContent {
		t.Errorf("no collector: %d, want 204", r.StatusCode)
	}
	if r := get(t, srv.URL+"/api/v1/computers/"+made.ID+"/series"+period+"&bucket=week", nil); r.StatusCode != http.StatusBadRequest {
		t.Errorf("bad bucket: %d, want 400", r.StatusCode)
	}
	if r := get(t, srv.URL+"/api/v1/computers/comp-nope/series"+period, nil); r.StatusCode != http.StatusNotFound {
		t.Errorf("unknown computer: %d, want 404", r.StatusCode)
	}

	postBody(t, srv.URL+"/api/v1/ingest/pc", contractExample(t, "ingest-pc.json"))
	var series model.PcSeries
	if r := get(t, srv.URL+"/api/v1/computers/"+made.ID+"/series"+period, &series); r.StatusCode != http.StatusOK {
		t.Fatalf("series: %d", r.StatusCode)
	}
	if len(series.Lines) == 0 || series.Lines[0].Scope != "wall" || len(series.Gaps) == 0 {
		t.Errorf("series = %+v", series)
	}
	for _, l := range series.Lines {
		if l.Scope == "breakdown" {
			t.Errorf("a part of a sensor became a line: %+v", l)
		}
	}
}

func TestProgramsAndRunsAreEstimatedAtTheSavedPriceAndLeftUnpricedWithoutOne(t *testing.T) {
	clock := time.Date(2026, 10, 22, 19, 0, 0, 0, time.UTC)
	srv := movingServer(t, &clock)
	postBody(t, srv.URL+"/api/v1/ingest/pc", contractExample(t, "ingest-pc.json"))

	d := detailOf(t, srv)
	for _, p := range d.PC.Programs {
		if p.Cost != nil {
			t.Errorf("%s priced as %s with no rate saved", p.Name, *p.Cost)
		}
	}
	if d.PC.Runs[0].Cost != nil {
		t.Errorf("run priced with no rate saved: %s", *d.PC.Runs[0].Cost)
	}

	if r, body := put(t, srv.URL+"/api/v1/rates", `{"mode":"one","fixed_rate":"0.20"}`); r.StatusCode != 200 {
		t.Fatalf("set rate: %d %s", r.StatusCode, body)
	}
	d = detailOf(t, srv)
	byName := map[string]model.PcProgramTotal{}
	for _, p := range d.PC.Programs {
		byName[p.Name] = p
	}
	comfy := byName["python.exe (ComfyUI)"]
	if comfy.Cost == nil || *comfy.Cost != "0.00062" { // (0.0011 + 0.002) kWh at $0.20
		t.Errorf("program cost = %v, want 0.00062", comfy.Cost)
	}
	if run := d.PC.Runs[0]; run.Cost == nil || *run.Cost != "0.0324" { // (0.021 + 0.141) kWh at $0.20
		t.Errorf("run cost = %v, want 0.0324", run.Cost)
	}
}

func TestTheEnergyHistoryHasARowPerPeriodWithGapsByReason(t *testing.T) {
	clock := time.Date(2026, 10, 23, 3, 0, 0, 0, time.UTC)
	srv := movingServer(t, &clock)
	postBody(t, srv.URL+"/api/v1/ingest/pc", contractExample(t, "ingest-pc.json"))
	var list struct{ Computers []model.Computer }
	get(t, srv.URL+"/api/v1/computers", &list)
	id := list.Computers[0].ID

	var h model.ComputerHistory
	if r := get(t, srv.URL+"/api/v1/computers/"+id+"/history", &h); r.StatusCode != http.StatusOK {
		t.Fatalf("history: %d", r.StatusCode)
	}
	keys := []string{}
	for _, p := range h.Periods {
		keys = append(keys, p.Key)
	}
	if len(keys) != 6 || keys[0] != "1h" || keys[5] != "all" {
		t.Fatalf("periods = %v", keys)
	}
	all := h.Periods[5]
	// No wall plug is tied to this computer, so the headline is what the sensors measured, and no price is known.
	if all.Basis != "sensors" || all.KWh <= 0 || all.KWh != all.SensorsKWh || all.Cost != nil {
		t.Errorf("all = %+v", all)
	}
	// The example's sleep gap (3 h 50 min on the 21st) is in the week and the all-time row but not in the last hour.
	if h.Periods[0].AsleepSeconds != 0 || all.AsleepSeconds < 3*3600 || all.PlugGapKWh <= 0 {
		t.Errorf("hour %+v, all %+v: the gap belongs only to the longer periods", h.Periods[0], all)
	}

	if r := get(t, srv.URL+"/api/v1/computers/comp-nope/history", nil); r.StatusCode != http.StatusNotFound {
		t.Errorf("unknown computer: %d, want 404", r.StatusCode)
	}

	// With a rate, a sensors-only row is priced at the period's average price and said to be an estimate.
	put(t, srv.URL+"/api/v1/rates", `{"mode":"one","fixed_rate":"0.20"}`)
	get(t, srv.URL+"/api/v1/computers/"+id+"/history", &h)
	if all := h.Periods[5]; all.Cost == nil || !all.CostEstimated {
		t.Errorf("priced all = %+v", all)
	}
}

func TestTheStatusSaysWhereTheDatabaseIsAndHowBigOnlyWhenTold(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "house.db")
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	with := httptest.NewServer((&Server{Store: s, Collector: collector.New(s, time.Second), Loc: time.UTC, Version: "test", DatabasePath: path}).Handler())
	t.Cleanup(with.Close)
	without := httptest.NewServer((&Server{Store: s, Collector: collector.New(s, time.Second), Loc: time.UTC, Version: "test"}).Handler())
	t.Cleanup(without.Close)

	var st map[string]any
	get(t, with.URL+"/api/v1/status", &st)
	if st["database_path"] != path || st["database_bytes"] == nil || st["database_bytes"].(float64) <= 0 {
		t.Errorf("status = %v", st)
	}
	var plain map[string]any
	get(t, without.URL+"/api/v1/status", &plain)
	if _, ok := plain["database_path"]; ok {
		t.Errorf("a server not told where its database is should not say: %v", plain)
	}
}
