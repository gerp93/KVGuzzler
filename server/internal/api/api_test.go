package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/collector"
	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/sim"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

var day = time.Date(2026, 10, 22, 17, 0, 0, 0, time.UTC)

// newServer runs a simulated house for `minutes` minutes starting at `day` and returns a test server over it.
func newServer(t *testing.T, minutes int) (*httptest.Server, time.Time) {
	t.Helper()
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	var now time.Time
	c := collector.New(s, time.Second)
	if err := c.Add(ctx, sim.NewAt(func() time.Time { return now })); err != nil {
		t.Fatal(err)
	}
	for i := 0; i <= minutes*60; i++ {
		now = day.Add(time.Duration(i) * time.Second)
		c.SetClock(func() time.Time { return now })
		c.PollOnce(ctx, now)
	}
	c.FlushAll(ctx)

	api := &Server{Store: s, Collector: c, Loc: time.UTC, Version: "test", Now: func() time.Time { return now }}
	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)
	return srv, now
}

func get(t *testing.T, url string, into any) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if into != nil && resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
			t.Fatal(err)
		}
	}
	return resp
}

func TestStatusAndVersionHeader(t *testing.T) {
	srv, _ := newServer(t, 1)
	var st struct {
		APIVersion int      `json:"api_version"`
		Notes      []string `json:"notes"`
	}
	resp := get(t, srv.URL+"/api/v1/status", &st)
	if st.APIVersion != 1 || resp.Header.Get("X-KVG-Api-Version") != "1" || st.Notes == nil {
		t.Errorf("status %+v, header %q", st, resp.Header.Get("X-KVG-Api-Version"))
	}
}

func TestLiveHouseIsTheMainsAlone(t *testing.T) {
	srv, _ := newServer(t, 2)
	var chs struct{ Channels []model.Channel }
	get(t, srv.URL+"/api/v1/channels", &chs)
	scope := map[string]model.Scope{}
	for _, c := range chs.Channels {
		scope[c.ID] = c.Scope
	}

	var live model.Live
	get(t, srv.URL+"/api/v1/live", &live)
	var mains, circuits float64
	for _, r := range live.Readings {
		switch scope[r.ChannelID] {
		case model.ScopeMains:
			mains += r.Watts
		case model.ScopeCircuit:
			circuits += r.Watts
		}
	}
	if live.HouseWatts == nil || math.Abs(*live.HouseWatts-mains) > 1e-6 {
		t.Fatalf("house_watts = %v, want the mains sum %v", live.HouseWatts, mains)
	}
	if *live.HouseWatts < circuits {
		t.Errorf("house %v below circuits %v: something was added to or taken from the house", *live.HouseWatts, circuits)
	}
}

func patch(t *testing.T, url, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPatch, url, bytes.NewBufferString(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

func TestRenameAndSwitchOffAChannel(t *testing.T) {
	srv, _ := newServer(t, 2)
	id := "sim-em16p:ch-01"

	if r := patch(t, srv.URL+"/api/v1/channels/"+id, `{"label":"  Clothes dryer "}`); r.StatusCode != 200 {
		t.Fatalf("rename: %d", r.StatusCode)
	}
	if r := patch(t, srv.URL+"/api/v1/channels/"+id, `{"enabled":false}`); r.StatusCode != 200 {
		t.Fatalf("disable: %d", r.StatusCode)
	}
	if r := patch(t, srv.URL+"/api/v1/channels/nope", `{"enabled":false}`); r.StatusCode != 404 {
		t.Errorf("unknown channel: %d, want 404", r.StatusCode)
	}
	if r := patch(t, srv.URL+"/api/v1/channels/"+id, `{"label":""}`); r.StatusCode != 400 {
		t.Errorf("empty label: %d, want 400", r.StatusCode)
	}
	if r := patch(t, srv.URL+"/api/v1/channels/"+id, `{"nonsense":1}`); r.StatusCode != 400 {
		t.Errorf("unknown field: %d, want 400", r.StatusCode)
	}

	var chs struct{ Channels []model.Channel }
	get(t, srv.URL+"/api/v1/channels", &chs)
	for _, c := range chs.Channels {
		if c.ID == id && (c.Label != "Clothes dryer" || c.Enabled) {
			t.Errorf("channel = %+v", c)
		}
	}
	var live model.Live
	get(t, srv.URL+"/api/v1/live", &live)
	for _, r := range live.Readings {
		if r.ChannelID == id {
			t.Error("a switched-off channel is still in the live view")
		}
	}
}

func TestSummaryUnmeteredNeverNegativeAndSeries(t *testing.T) {
	srv, end := newServer(t, 30)
	q := "?from=" + day.Format(time.RFC3339) + "&to=" + end.Add(time.Minute).Format(time.RFC3339)

	var sum model.Summary
	get(t, srv.URL+"/api/v1/summary"+q, &sum)
	if sum.HouseKWh == nil || sum.UnmeteredKWh == nil || *sum.UnmeteredKWh < 0 || *sum.HouseKWh <= 0 {
		t.Fatalf("summary house=%v unmetered=%v", sum.HouseKWh, sum.UnmeteredKWh)
	}
	var circuits float64
	for _, c := range sum.Channels {
		if c.Scope == model.ScopeCircuit {
			circuits += c.KWh
		}
	}
	if math.Abs(*sum.HouseKWh-circuits-*sum.UnmeteredKWh) > 1e-9 {
		t.Errorf("house %v - circuits %v != unmetered %v", *sum.HouseKWh, circuits, *sum.UnmeteredKWh)
	}

	var ser model.Series
	get(t, srv.URL+"/api/v1/series"+q+"&channel_id=sim-em16p:mains-a", &ser)
	if ser.Bucket != model.BucketMinute || len(ser.Points) < 29 {
		t.Errorf("series bucket %s with %d points", ser.Bucket, len(ser.Points))
	}

	if r := get(t, srv.URL+"/api/v1/series"+q+"&channel_id=nope", nil); r.StatusCode != 400 {
		t.Errorf("unknown channel series: %d, want 400", r.StatusCode)
	}
	if r := get(t, srv.URL+"/api/v1/summary?from=x&to=y", nil); r.StatusCode != 400 {
		t.Errorf("bad period: %d, want 400", r.StatusCode)
	}
}

func TestUnbuiltRoutesSayNotImplemented(t *testing.T) {
	srv, _ := newServer(t, 1)
	for _, path := range []string{"/api/v1/carbon"} {
		if r := get(t, srv.URL+path, nil); r.StatusCode != http.StatusNotImplemented {
			t.Errorf("%s: %d, want 501", path, r.StatusCode)
		}
	}
}

func TestDefaultBucket(t *testing.T) {
	cases := map[time.Duration]model.Bucket{
		time.Hour:           model.BucketMinute,
		24 * time.Hour:      model.BucketHour,
		15 * 24 * time.Hour: model.BucketHour,
		31 * 24 * time.Hour: model.BucketDay,
	}
	for span, want := range cases {
		if got := defaultBucket(span); got != want {
			t.Errorf("defaultBucket(%v) = %s, want %s", span, got, want)
		}
	}
}

func put(t *testing.T, url, body string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPut, url, bytes.NewBufferString(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return resp, buf.String()
}

func TestRatesDefaultSaveAndRefuse(t *testing.T) {
	srv, _ := newServer(t, 1)

	var cfg struct {
		Mode  string            `json:"mode"`
		Bills []json.RawMessage `json:"bills"`
	}
	get(t, srv.URL+"/api/v1/rates", &cfg)
	if cfg.Mode != "one" || cfg.Bills == nil || len(cfg.Bills) != 0 {
		t.Fatalf("default = %+v, want mode one and an empty (not null) bills list", cfg)
	}

	if r, body := put(t, srv.URL+"/api/v1/rates", `{"mode":"one","fixed_rate":"0.15"}`); r.StatusCode != 200 {
		t.Fatalf("save: %d %s", r.StatusCode, body)
	}
	var saved struct {
		FixedRate string `json:"fixed_rate"`
		Currency  string `json:"currency"`
	}
	get(t, srv.URL+"/api/v1/rates", &saved)
	if saved.FixedRate != "0.15" || saved.Currency != "USD" {
		t.Errorf("saved = %+v", saved)
	}

	for name, body := range map[string]string{
		"lookup is not built":    `{"mode":"lookup"}`,
		"unknown mode":           `{"mode":"free"}`,
		"negative rate":          `{"mode":"one","fixed_rate":"-1"}`,
		"rate not a decimal":     `{"mode":"one","fixed_rate":"abc"}`,
		"rate sent as a number":  `{"mode":"one","fixed_rate":0.15}`,
		"overlapping bills":      `{"mode":"bills","bills":[{"start":"2026-08-01","end":"2026-08-31","total":"100","kwh":500},{"start":"2026-08-31","end":"2026-09-30","total":"100","kwh":500}]}`,
		"fees swallow the total": `{"mode":"bills","bills":[{"start":"2026-08-01","end":"2026-08-31","total":"100","kwh":500,"fixed_fees":[{"label":"x","amount":"100"}]}]}`,
		"unknown field":          `{"mode":"one","nonsense":1}`,
		"bad currency":           `{"mode":"one","currency":"dollars"}`,
	} {
		if r, _ := put(t, srv.URL+"/api/v1/rates", body); r.StatusCode != 400 {
			t.Errorf("%s: status %d, want 400", name, r.StatusCode)
		}
	}
}

func parseMoney(t *testing.T, s *string) float64 {
	t.Helper()
	if s == nil {
		t.Fatal("expected a cost, got null")
	}
	var f float64
	if _, err := fmt.Sscanf(*s, "%f", &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestSummaryCostFollowsTheRate(t *testing.T) {
	srv, end := newServer(t, 30)
	q := "?from=" + day.Format(time.RFC3339) + "&to=" + end.Add(time.Minute).Format(time.RFC3339)

	// No rate yet: energy is counted, nothing is priced.
	var before model.Summary
	get(t, srv.URL+"/api/v1/summary"+q, &before)
	if before.HouseCost != nil {
		t.Errorf("house cost %s with no rate set, want null", *before.HouseCost)
	}
	for _, c := range before.Channels {
		if c.KWh > 0 && c.Cost != nil {
			t.Errorf("%s priced with no rate", c.ChannelID)
		}
	}

	put(t, srv.URL+"/api/v1/rates", `{"mode":"one","fixed_rate":"0.20"}`)
	var after model.Summary
	get(t, srv.URL+"/api/v1/summary"+q, &after)
	if after.HouseKWh == nil || after.HouseCost == nil {
		t.Fatalf("house kwh=%v cost=%v", after.HouseKWh, after.HouseCost)
	}
	if got, want := parseMoney(t, after.HouseCost), *after.HouseKWh*0.20; math.Abs(got-want) > 0.0001 {
		t.Errorf("house cost %v, want kWh x 0.20 = %v", got, want)
	}
	for _, c := range after.Channels {
		if got, want := parseMoney(t, c.Cost), c.KWh*0.20; math.Abs(got-want) > 0.0001 {
			t.Errorf("%s cost %v, want %v", c.ChannelID, got, want)
		}
	}
}

func TestBillPricesAreUsedAndEstimatesAreFlagged(t *testing.T) {
	srv, end := newServer(t, 30)
	q := "?from=" + day.Format(time.RFC3339) + "&to=" + end.Add(time.Minute).Format(time.RFC3339)

	// A bill that ended the day before the data: every day in the period is "after the latest bill", so an estimate.
	bills := `{"mode":"bills","bills":[{"start":"2026-09-01","end":"2026-10-20","total":"150","kwh":1000}]}`
	if r, body := put(t, srv.URL+"/api/v1/rates", bills); r.StatusCode != 200 {
		t.Fatalf("save: %d %s", r.StatusCode, body)
	}
	var sum model.Summary
	get(t, srv.URL+"/api/v1/summary"+q, &sum)
	if !sum.CostEstimated {
		t.Error("cost_estimated should be true for days after the latest bill")
	}
	if got, want := parseMoney(t, sum.HouseCost), *sum.HouseKWh*0.15; math.Abs(got-want) > 0.0001 {
		t.Errorf("house cost %v, want the bill's $0.15 x kWh = %v", got, want)
	}

	// A bill that covers the data: not an estimate.
	covering := `{"mode":"bills","bills":[{"start":"2026-10-01","end":"2026-10-31","total":"150","kwh":1000}]}`
	put(t, srv.URL+"/api/v1/rates", covering)
	get(t, srv.URL+"/api/v1/summary"+q, &sum)
	if sum.CostEstimated {
		t.Error("cost_estimated should be false when a bill covers the period")
	}
}
