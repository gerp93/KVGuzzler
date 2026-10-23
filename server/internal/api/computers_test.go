package api

import (
	"bytes"
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

const period = "?from=2026-10-22T00:00:00Z&to=2026-10-23T00:00:00Z"

// computerServer is a server on a fixed clock (2026-10-22 19:00 UTC) and its store, so a test can put readings in directly.
func computerServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	clock := time.Date(2026, 10, 22, 19, 0, 0, 0, time.UTC)
	api := &Server{Store: s, Collector: collector.New(s, time.Second), Loc: time.UTC, Version: "test", Now: func() time.Time { return clock }}
	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)
	return srv, s
}

func send(t *testing.T, method, url, body string) (*http.Response, string) {
	t.Helper()
	var rdr *bytes.Buffer
	if body != "" {
		rdr = bytes.NewBufferString(body)
	} else {
		rdr = &bytes.Buffer{}
	}
	req, _ := http.NewRequest(method, url, rdr)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out bytes.Buffer
	_, _ = out.ReadFrom(resp.Body)
	return resp, out.String()
}

func decode[T any](t *testing.T, body string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, body)
	}
	return v
}

func TestAPcPushAddsAComputerAndShowsItsInsidesAndResendingReplacesIt(t *testing.T) {
	srv, _ := computerServer(t)
	example := contractExample(t, "ingest-pc.json")

	for pass := 0; pass < 2; pass++ {
		resp, body := postBody(t, srv.URL+"/api/v1/ingest/pc", example)
		if resp.StatusCode != 200 || !strings.Contains(body, `"accepted":1`) || !strings.Contains(body, `"api_version":1`) {
			t.Fatalf("pass %d: %d %s", pass, resp.StatusCode, body)
		}
	}

	var list struct{ Computers []model.Computer }
	get(t, srv.URL+"/api/v1/computers", &list)
	if len(list.Computers) != 1 {
		t.Fatalf("computers = %+v, want the one the first push added (and not another per push)", list.Computers)
	}
	comp := list.Computers[0]
	if comp.Name != "Desktop" || comp.SourceID == nil || *comp.SourceID != "pc-7f3a9c1e" || comp.LastSeen == nil || comp.WallChannelID != nil {
		t.Errorf("computer = %+v", comp)
	}

	var d model.ComputerDetail
	get(t, srv.URL+"/api/v1/computers/"+comp.ID+"/detail"+period, &d)
	if d.PC == nil || d.Wall != nil || len(d.Peripherals) != 0 || d.SetupKWh != nil || d.SetupCost != nil {
		t.Fatalf("detail = %+v: no plugs are tied yet, so there is no wall figure and no setup", d)
	}
	var wall, comfy *float64
	for _, c := range d.PC.Components {
		if c.Name == "Wall meter" {
			k := c.KWh
			wall = &k
		}
	}
	for _, p := range d.PC.Programs {
		if p.Name == "python.exe (ComfyUI)" {
			k := p.KWh
			comfy = &k
		}
	}
	if wall == nil || math.Abs(*wall-312.5*60/3.6e6) > 1e-9 {
		t.Errorf("wall kWh = %v, want one minute at 312.5 W (not doubled by the resend)", wall)
	}
	if comfy == nil || math.Abs(*comfy-0.0031) > 1e-9 {
		t.Errorf("ComfyUI kWh = %v, want 0.0031", comfy)
	}
	if len(d.PC.Gaps) != 1 || d.PC.Gaps[0].Reason != "asleep" || d.PC.TotalPeakWatts == nil || *d.PC.TotalPeakWatts != 402.3 {
		t.Errorf("pc = %+v", d.PC)
	}
}

func TestPcPushRefusals(t *testing.T) {
	srv, _ := computerServer(t)
	example := contractExample(t, "ingest-pc.json")

	resp, body := postBody(t, srv.URL+"/api/v1/ingest/pc", strings.Replace(example, `"api_version": 1`, `"api_version": 2`, 1))
	if resp.StatusCode != http.StatusUpgradeRequired || !strings.Contains(body, `"server_api_version":1`) || !strings.Contains(body, "api_version_mismatch") {
		t.Errorf("version mismatch: %d %s", resp.StatusCode, body)
	}
	for name, bad := range map[string]string{
		"not json":              `nope`,
		"minute off the minute": strings.Replace(example, "2026-10-22T18:03:00Z", "2026-10-22T18:03:07Z", 1),
		"unknown scope":         strings.Replace(example, `"scope": "wall"`, `"scope": "mains"`, 1),
		"no source id":          strings.Replace(example, `"source_id": "pc-7f3a9c1e"`, `"source_id": ""`, 1),
	} {
		if r, _ := postBody(t, srv.URL+"/api/v1/ingest/pc", bad); r.StatusCode != 400 {
			t.Errorf("%s: %d, want 400", name, r.StatusCode)
		}
	}
	var list struct{ Computers []model.Computer }
	get(t, srv.URL+"/api/v1/computers", &list)
	if len(list.Computers) != 0 {
		t.Errorf("a refused push added a computer: %+v", list.Computers)
	}
}

func TestPcPushTellsWhatItDropped(t *testing.T) {
	srv, _ := computerServer(t)
	example := strings.Replace(contractExample(t, "ingest-pc.json"), `"avg_watts": 214.7`, `"avg_watts": 90000`, 1) // the GPU's first-read nonsense
	resp, body := postBody(t, srv.URL+"/api/v1/ingest/pc", example)
	if resp.StatusCode != 200 || !strings.Contains(body, "Dropped 1 impossible") {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	var list struct{ Computers []model.Computer }
	get(t, srv.URL+"/api/v1/computers", &list)
	var d model.ComputerDetail
	get(t, srv.URL+"/api/v1/computers/"+list.Computers[0].ID+"/detail"+period, &d)
	for _, c := range d.PC.Components {
		if c.Name == "RTX 5070 Ti" {
			t.Errorf("the impossible GPU reading was stored: %+v", c)
		}
	}
}

func TestComputersCanBeAddedRenamedLinkedAndRemoved(t *testing.T) {
	srv, _ := computerServer(t)
	base := srv.URL + "/api/v1/computers"

	resp, body := send(t, http.MethodPost, base, `{"name":"  Laptop "}`)
	if resp.StatusCode != 201 {
		t.Fatalf("add: %d %s", resp.StatusCode, body)
	}
	laptop := decode[model.Computer](t, body)
	if laptop.Name != "Laptop" || laptop.PeripheralChannelIDs == nil {
		t.Errorf("laptop = %+v", laptop)
	}

	// In order: each step depends on the ones before it.
	for _, c := range []struct {
		name, method, url, body string
		want                    int
	}{
		{"blank name", http.MethodPost, base, `{"name":"  "}`, 400},
		{"unknown field", http.MethodPost, base, `{"name":"X","colour":"red"}`, 400},
		{"rename", http.MethodPatch, base + "/" + laptop.ID, `{"name":"Work laptop"}`, 200},
		{"link a collector", http.MethodPatch, base + "/" + laptop.ID, `{"source_id":"pc-9"}`, 200},
		{"a second computer on that collector", http.MethodPost, base, `{"name":"Other","source_id":"pc-9"}`, 409},
		{"let the collector go", http.MethodPatch, base + "/" + laptop.ID, `{"source_id":null}`, 200},
		{"nothing to change", http.MethodPatch, base + "/" + laptop.ID, `{}`, 400},
		{"unknown computer", http.MethodPatch, base + "/comp-nope", `{"name":"X"}`, 404},
		{"bad source type", http.MethodPatch, base + "/" + laptop.ID, `{"source_id":5}`, 400},
		{"remove", http.MethodDelete, base + "/" + laptop.ID, ``, 204},
		{"remove again", http.MethodDelete, base + "/" + laptop.ID, ``, 404},
		{"detail of a removed one", http.MethodGet, base + "/" + laptop.ID + "/detail" + period, ``, 404},
	} {
		if r, b := send(t, c.method, c.url, c.body); r.StatusCode != c.want {
			t.Errorf("%s: %d, want %d (%s)", c.name, r.StatusCode, c.want, b)
		}
	}
	if r := get(t, base+"/comp-x/detail?from=x&to=y", nil); r.StatusCode != 400 {
		t.Errorf("bad period: %d, want 400", r.StatusCode)
	}
}

func TestPlugsTieToAComputerAndTheDetailAddsUpTheSetupSeparately(t *testing.T) {
	srv, st := computerServer(t)
	ctx := t.Context()
	at := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

	for _, ch := range []model.Channel{
		{ID: "wall", DeviceID: "d-wall", Scope: model.ScopePC, Label: "PC"},
		{ID: "wall2", DeviceID: "d-wall2", Scope: model.ScopePC, Label: "Second PC plug"},
		{ID: "monitor", DeviceID: "d-mon", Scope: model.ScopePlug, Label: "Monitor"},
		{ID: "speakers", DeviceID: "d-spk", Scope: model.ScopePlug, Label: "Speakers"},
		{ID: "dryer", DeviceID: "d-dry", Scope: model.ScopeCircuit, Label: "Dryer"},
	} {
		if err := st.UpsertChannel(ctx, ch); err != nil {
			t.Fatal(err)
		}
	}
	// One minute of each: the PC at 300 W, the monitor 50 W, the speakers 10 W.
	for id, w := range map[string]float64{"wall": 300, "monitor": 50, "speakers": 10} {
		if err := st.AddMinute(ctx, id, at, w, w, 60); err != nil {
			t.Fatal(err)
		}
	}
	if r, b := send(t, http.MethodPut, srv.URL+"/api/v1/rates", `{"mode":"one","fixed_rate":"0.20"}`); r.StatusCode != 200 {
		t.Fatalf("rates: %d %s", r.StatusCode, b)
	}
	_, body := send(t, http.MethodPost, srv.URL+"/api/v1/computers", `{"name":"Desktop"}`)
	comp := decode[model.Computer](t, body)
	assign := func(channel, computer string) int {
		r, _ := send(t, http.MethodPatch, srv.URL+"/api/v1/channels/"+channel, `{"computer_id":`+computer+`}`)
		return r.StatusCode
	}
	id := `"` + comp.ID + `"`

	for _, ch := range []string{"wall", "monitor", "speakers"} {
		if code := assign(ch, id); code != 200 {
			t.Fatalf("assign %s: %d", ch, code)
		}
	}
	if code := assign("wall2", id); code != 409 {
		t.Errorf("a second wall plug: %d, want 409", code)
	}
	if code := assign("dryer", id); code != 400 {
		t.Errorf("a circuit: %d, want 400 (only plugs belong to a computer)", code)
	}
	if code := assign("monitor", `"comp-nope"`); code != 400 {
		t.Errorf("unknown computer: %d, want 400", code)
	}
	if code := assign("monitor", `5`); code != 400 {
		t.Errorf("a number for the computer: %d, want 400", code)
	}

	var d model.ComputerDetail
	get(t, srv.URL+"/api/v1/computers/"+comp.ID+"/detail"+period, &d)
	pcKwh := 300.0 * 60 / 3.6e6
	if d.Wall == nil || d.Wall.ChannelID != "wall" || math.Abs(d.Wall.KWh-pcKwh) > 1e-12 || d.Wall.Cost == nil {
		t.Fatalf("wall = %+v", d.Wall)
	}
	if len(d.Peripherals) != 2 {
		t.Fatalf("peripherals = %+v", d.Peripherals)
	}
	wantSetup := (300.0 + 50 + 10) * 60 / 3.6e6
	if d.SetupKWh == nil || math.Abs(*d.SetupKWh-wantSetup) > 1e-12 {
		t.Errorf("setup kWh = %v, want the wall plus both peripherals = %v", d.SetupKWh, wantSetup)
	}
	if got, want := parseMoney(t, d.SetupCost), wantSetup*0.20; math.Abs(got-want) > 1e-5 {
		t.Errorf("setup cost = %v, want %v", got, want)
	}
	if d.PC != nil {
		t.Errorf("no collector reports for this computer, so pc should be null: %+v", d.PC)
	}
	if d.Wall.ComputerID == nil || *d.Wall.ComputerID != comp.ID {
		t.Errorf("the wall summary should say which computer it belongs to: %+v", d.Wall)
	}

	// The house summary lists them with their computer, so a client can group them instead of showing them alone.
	var sum model.Summary
	get(t, srv.URL+"/api/v1/summary"+period, &sum)
	tied := 0
	for _, c := range sum.Channels {
		if c.ComputerID != nil && *c.ComputerID == comp.ID {
			tied++
		}
	}
	if tied != 3 {
		t.Errorf("%d channels in the summary carry the computer, want 3", tied)
	}

	// A plug the user hides is neither shown nor counted; letting a plug go takes it out of the computer.
	send(t, http.MethodPatch, srv.URL+"/api/v1/channels/speakers", `{"enabled":false}`)
	assign("monitor", `null`)
	get(t, srv.URL+"/api/v1/computers/"+comp.ID+"/detail"+period, &d)
	if len(d.Peripherals) != 0 || d.SetupKWh == nil || math.Abs(*d.SetupKWh-pcKwh) > 1e-12 {
		t.Errorf("after hiding the speakers and freeing the monitor: peripherals %+v, setup %v, want just the PC", d.Peripherals, d.SetupKWh)
	}
	var list struct{ Computers []model.Computer }
	get(t, srv.URL+"/api/v1/computers", &list)
	if len(list.Computers[0].PeripheralChannelIDs) != 1 || list.Computers[0].PeripheralChannelIDs[0] != "speakers" {
		t.Errorf("the hidden plug is still tied to the computer: %+v", list.Computers[0])
	}
}

func TestSetupHasNoCostWhenNothingIsPriced(t *testing.T) {
	srv, st := computerServer(t)
	ctx := t.Context()
	if err := st.UpsertChannel(ctx, model.Channel{ID: "wall", DeviceID: "d", Scope: model.ScopePC, Label: "PC"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddMinute(ctx, "wall", time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC), 300, 300, 60); err != nil {
		t.Fatal(err)
	}
	_, body := send(t, http.MethodPost, srv.URL+"/api/v1/computers", `{"name":"Desktop"}`)
	comp := decode[model.Computer](t, body)
	send(t, http.MethodPatch, srv.URL+"/api/v1/channels/wall", `{"computer_id":"`+comp.ID+`"}`)

	var d model.ComputerDetail
	get(t, srv.URL+"/api/v1/computers/"+comp.ID+"/detail"+period, &d)
	if d.SetupKWh == nil || d.SetupCost != nil || d.Wall == nil || d.Wall.Cost != nil {
		t.Errorf("with no rate set the energy is counted and nothing is priced: %+v", d)
	}
}
