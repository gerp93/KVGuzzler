package api

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/collector"
	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/openei"
	"github.com/gerp93/KVGuzzler/server/internal/rates"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

const goodKey = "abcdefghij0123456789"

// fakeOpenEi plays the part of OpenEI for the server.
type fakeOpenEi struct {
	rates  []openei.Candidate
	byName map[string]*openei.Candidate
	fail   error
	asked  []string
	gets   int
}

func (f *fakeOpenEi) Find(_ context.Context, key, address string, _ time.Time) ([]openei.Candidate, error) {
	f.asked = append(f.asked, key+"|"+address)
	return f.rates, f.fail
}

func (f *fakeOpenEi) Get(_ context.Context, _, label string, now time.Time) (*openei.Candidate, error) {
	f.gets++
	if f.fail != nil {
		return nil, f.fail
	}
	c := f.byName[label]
	if c != nil {
		copy := *c
		t := *c.Tariff
		t.FetchedAt = now
		copy.Tariff = &t
		return &copy, nil
	}
	return nil, nil
}

func flatTariff(label string, price float64) *rates.Tariff {
	grid := make([]float64, rates.GridSize)
	for i := range grid {
		grid[i] = price
	}
	return &rates.Tariff{Label: label, Utility: "Example Electric", Name: "Residential Standard", FetchedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Grid: grid, Periods: 1, Tiers: 1}
}

// eveningTariff is 8 cents, 20 cents from 5pm to 8pm on weekdays.
func eveningTariff(label string) *rates.Tariff {
	t := flatTariff(label, 0.08)
	t.Periods = 2
	for m := 0; m < 12; m++ {
		for h := 17; h < 20; h++ {
			t.Grid[rates.GridIndex(m, false, h)] = 0.20
		}
	}
	return t
}

func lookupServer(t *testing.T, f *fakeOpenEi, clock *time.Time) (*httptest.Server, *store.Store, *Server) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	api := &Server{Store: st, Collector: collector.New(st, time.Second), Loc: time.UTC, Version: "test", Now: func() time.Time { return *clock }, Lookup: f}
	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)
	return srv, st, api
}

func theClock() *time.Time {
	c := time.Date(2026, 10, 22, 19, 0, 0, 0, time.UTC) // a Thursday
	return &c
}

func TestTheKeyIsSavedWithoutEverBeingSentBack(t *testing.T) {
	srv, st, _ := lookupServer(t, &fakeOpenEi{}, theClock())
	var s model.OpenEiStatus
	if get(t, srv.URL+"/api/v1/openei", &s); s.HasKey {
		t.Fatal("a fresh server has a key")
	}
	for _, bad := range []string{"short", "has spaces in it!", `{"x"}`} {
		if r, _ := send(t, http.MethodPut, srv.URL+"/api/v1/openei", `{"api_key":"`+bad+`"}`); r.StatusCode != 400 {
			t.Errorf("key %q: %d, want 400", bad, r.StatusCode)
		}
	}
	r, body := send(t, http.MethodPut, srv.URL+"/api/v1/openei", `{"api_key":"`+goodKey+`"}`)
	if r.StatusCode != 200 || strings.Contains(body, goodKey) || !strings.Contains(body, `"has_key":true`) {
		t.Errorf("save: %d %s", r.StatusCode, body)
	}
	if _, body := send(t, http.MethodGet, srv.URL+"/api/v1/openei", ""); strings.Contains(body, goodKey) {
		t.Errorf("the key came back: %s", body)
	}
	if k, _, _ := st.Secret(t.Context(), openEiKeyKey); k != goodKey {
		t.Errorf("stored key = %q", k)
	}
	if r, body := send(t, http.MethodPut, srv.URL+"/api/v1/openei", `{"api_key":""}`); r.StatusCode != 200 || !strings.Contains(body, `"has_key":false`) {
		t.Errorf("remove: %d %s", r.StatusCode, body)
	}
}

func TestLookingRatesUpNeedsAKeyAndAnAddressAndPassesOnWhatOpenEiSays(t *testing.T) {
	f := &fakeOpenEi{rates: []openei.Candidate{
		{Tariff: flatTariff("flat-1", 0.12), IsDefault: true},
		{Tariff: eveningTariff("tou-1"), Approved: true},
	}}
	srv, _, _ := lookupServer(t, f, theClock())
	url := srv.URL + "/api/v1/rates/lookup"

	if r, _ := send(t, http.MethodGet, url+"?address=80202", ""); r.StatusCode != 400 {
		t.Errorf("without a key: %d, want 400", r.StatusCode)
	}
	send(t, http.MethodPut, srv.URL+"/api/v1/openei", `{"api_key":"`+goodKey+`"}`)
	if r, _ := send(t, http.MethodGet, url, ""); r.StatusCode != 400 {
		t.Errorf("without an address: %d, want 400", r.StatusCode)
	}

	var got model.TariffCandidates
	if r := get(t, url+"?address=80202", &got); r.StatusCode != 200 {
		t.Fatalf("lookup: %d", r.StatusCode)
	}
	if len(got.Candidates) != 2 || got.Candidates[0].Label != "flat-1" || !got.Candidates[0].IsDefault || !got.Candidates[1].VariesByTime || got.Candidates[1].Dearest != 0.20 {
		t.Errorf("candidates = %+v", got.Candidates)
	}
	if !strings.Contains(got.Candidates[0].Summary, "$0.1200 per kWh") {
		t.Errorf("summary = %q", got.Candidates[0].Summary)
	}
	if len(f.asked) != 1 || f.asked[0] != goodKey+"|80202" {
		t.Errorf("asked = %v", f.asked)
	}

	f.fail = &openei.Error{Message: "OpenEI did not accept the API key. Check it was copied whole."}
	r, body := send(t, http.MethodGet, url+"?address=80202", "")
	if r.StatusCode != http.StatusBadGateway || !strings.Contains(body, "did not accept the API key") || strings.Contains(body, goodKey) {
		t.Errorf("openei error: %d %s", r.StatusCode, body)
	}
}

func TestChoosingARateSavesItAndPricesWithIt(t *testing.T) {
	f := &fakeOpenEi{byName: map[string]*openei.Candidate{"flat-1": {Tariff: flatTariff("flat-1", 0.12)}}}
	clock := theClock()
	srv, st, _ := lookupServer(t, f, clock)
	ctx := t.Context()
	put := func(body string) (*http.Response, string) {
		return send(t, http.MethodPut, srv.URL+"/api/v1/rates", body)
	}

	if r, body := put(`{"mode":"lookup"}`); r.StatusCode != 400 || !strings.Contains(body, "Pick a rate") {
		t.Errorf("no rate picked: %d %s", r.StatusCode, body)
	}
	if r, body := put(`{"mode":"lookup","tariff_label":"flat-1"}`); r.StatusCode != 400 || !strings.Contains(body, "API key") {
		t.Errorf("no key: %d %s", r.StatusCode, body)
	}
	send(t, http.MethodPut, srv.URL+"/api/v1/openei", `{"api_key":"`+goodKey+`"}`)
	if r, body := put(`{"mode":"lookup","tariff_label":"gone"}`); r.StatusCode != 400 || !strings.Contains(body, "no longer has that rate") {
		t.Errorf("a rate OpenEI does not have: %d %s", r.StatusCode, body)
	}
	if r, _ := put(`{"mode":"lookup","tariff_label":"flat-1","fixed_rate":"0.15"}`); r.StatusCode != 200 {
		t.Fatalf("choose: %d", r.StatusCode)
	}
	if got, _, _ := st.Meta(ctx, tariffKey); !strings.Contains(got, `"flat-1"`) || strings.Contains(got, goodKey) {
		t.Errorf("saved tariff = %.80s", got)
	}

	var info model.TariffInfo
	if r := get(t, srv.URL+"/api/v1/rates/tariff", &info); r.StatusCode != 200 || info.Label != "flat-1" || info.VariesByTime || !info.FetchedAt.Equal(*clock) {
		t.Errorf("tariff info = %+v (%d)", info, r.StatusCode)
	}

	// A wall plug that used 1 kWh costs 12 cents at the looked-up price, not the 15 cents of the single rate.
	if err := st.UpsertChannel(ctx, model.Channel{ID: "wall", DeviceID: "d", Scope: model.ScopePC, Label: "PC"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 60; i++ {
		if err := st.AddMinute(ctx, "wall", time.Date(2026, 10, 22, 12, i, 0, 0, time.UTC), 1000, 1000, 60); err != nil {
			t.Fatal(err)
		}
	}
	var sum model.Summary
	get(t, srv.URL+"/api/v1/summary?from=2026-10-22T00:00:00Z&to=2026-10-23T00:00:00Z", &sum)
	var cost *string
	for _, c := range sum.Channels {
		if c.ChannelID == "wall" {
			cost = c.Cost
		}
	}
	if cost == nil || !strings.HasPrefix(*cost, "0.12") {
		t.Errorf("cost = %v, want 0.12 (1 kWh at the looked-up price); channels %+v", cost, sum.Channels)
	}

	// Choosing the same rate again does not ask OpenEI again.
	before := f.gets
	put(`{"mode":"lookup","tariff_label":"flat-1"}`)
	if f.gets != before {
		t.Errorf("OpenEI was asked %d more time(s) for a rate already in use", f.gets-before)
	}
}

func TestATariffOlderThanAWeekIsReadAgainAndTheOldOneStaysIfThatFails(t *testing.T) {
	f := &fakeOpenEi{byName: map[string]*openei.Candidate{"flat-1": {Tariff: flatTariff("flat-1", 0.14)}}}
	clock := theClock()
	srv, st, api := lookupServer(t, f, clock)
	ctx := t.Context()
	send(t, http.MethodPut, srv.URL+"/api/v1/openei", `{"api_key":"`+goodKey+`"}`)
	send(t, http.MethodPut, srv.URL+"/api/v1/rates", `{"mode":"lookup","tariff_label":"flat-1"}`)

	if err := api.RefreshRates(ctx); err != nil || f.gets != 1 {
		t.Fatalf("a fresh tariff: gets %d, err %v (the choosing itself asked once)", f.gets, err)
	}
	*clock = clock.Add(8 * 24 * time.Hour)
	f.byName["flat-1"].Tariff = flatTariff("flat-1", 0.15)
	if err := api.RefreshRates(ctx); err != nil || f.gets != 2 {
		t.Fatalf("an old tariff: gets %d, err %v", f.gets, err)
	}
	if got := mustTariff(t, st); got.Grid[0] != 0.15 || !got.FetchedAt.Equal(*clock) {
		t.Errorf("tariff = %v fetched %v, want the new one", got.Grid[0], got.FetchedAt)
	}

	*clock = clock.Add(8 * 24 * time.Hour)
	f.fail = &openei.Error{Message: "OpenEI answered with an error (500)."}
	if err := api.RefreshRates(ctx); err == nil {
		t.Error("a failed refresh should be reported for the log")
	}
	if got := mustTariff(t, st); got.Grid[0] != 0.15 {
		t.Errorf("a failed refresh replaced the tariff: %v", got.Grid[0])
	}

	// Other price modes do nothing at all.
	send(t, http.MethodPut, srv.URL+"/api/v1/rates", `{"mode":"one","fixed_rate":"0.20"}`)
	gets := f.gets
	if err := api.RefreshRates(ctx); err != nil || f.gets != gets {
		t.Errorf("one rate: gets %d -> %d, err %v", gets, f.gets, err)
	}
}

func mustTariff(t *testing.T, st *store.Store) *rates.Tariff {
	t.Helper()
	text, ok, err := st.Meta(t.Context(), tariffKey)
	if err != nil || !ok {
		t.Fatalf("no saved tariff: %v", err)
	}
	tf := rates.ParseTariff(text)
	if tf == nil {
		t.Fatal("the saved tariff is damaged")
	}
	return tf
}

func TestATimeOfUseTariffIsWeightedByTheHoursTheHouseReallyUsesPower(t *testing.T) {
	f := &fakeOpenEi{byName: map[string]*openei.Candidate{"tou-1": {Tariff: eveningTariff("tou-1")}}}
	clock := theClock()
	srv, st, api := lookupServer(t, f, clock)
	ctx := t.Context()
	send(t, http.MethodPut, srv.URL+"/api/v1/openei", `{"api_key":"`+goodKey+`"}`)
	send(t, http.MethodPut, srv.URL+"/api/v1/rates", `{"mode":"lookup","tariff_label":"tou-1"}`)

	// The house only ever draws power at 18:00 on weekdays (a Monday and a Tuesday here): its day is all peak.
	if err := st.UpsertChannel(ctx, model.Channel{ID: "mains-a", DeviceID: "d", Scope: model.ScopeMains, Label: "Mains A"}); err != nil {
		t.Fatal(err)
	}
	for _, day := range []int{19, 20} {
		for m := 0; m < 60; m++ {
			if err := st.AddMinute(ctx, "mains-a", time.Date(2026, 10, day, 18, m, 0, 0, time.UTC), 1000, 1000, 60); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := api.RefreshRates(ctx); err != nil {
		t.Fatal(err)
	}
	cfg, err := api.rateConfig(ctx)
	if err != nil || cfg.Profile == nil || math.Abs(cfg.Profile.Weekday[18]-1) > 1e-9 {
		t.Fatalf("profile = %+v, %v", cfg.Profile, err)
	}
	sched := cfg.Schedule(time.UTC)
	if r := sched.RateAt(rates.MustDate("2026-10-21")); r == nil || math.Abs(r.Float()-0.20) > 1e-9 { // a Wednesday
		t.Errorf("weekday price = %v, want the 20 cent peak", r)
	}
	if r := sched.RateAt(rates.MustDate("2026-10-24")); r == nil || math.Abs(r.Float()-0.08) > 1e-9 { // a Saturday, no peak
		t.Errorf("weekend price = %v, want 8 cents", r)
	}

	// It is learned at most once an hour.
	if err := st.SetMeta(ctx, profileKey, "damaged"); err != nil {
		t.Fatal(err)
	}
	if err := api.RefreshRates(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := st.Meta(ctx, profileKey); got != "damaged" {
		t.Error("the profile was learned again within the hour")
	}
	*clock = clock.Add(61 * time.Minute)
	if err := api.RefreshRates(ctx); err != nil {
		t.Fatal(err)
	}
	if p, _ := api.savedProfile(ctx); p == nil {
		t.Error("the profile was not learned again an hour later")
	}
}
