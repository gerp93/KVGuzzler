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
