package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/collector"
	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/rates"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

// freezeServer has a house (one mains meter) that used 20 kWh a day from 1 Oct to 9 Nov, with 10 Nov as today.
func freezeServer(t *testing.T) (*httptest.Server, *Server) {
	t.Helper()
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.UpsertDevice(ctx, model.Device{ID: "d", Kind: "refoss", Name: "d"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertMeter(ctx, model.Meter{ID: "ma", DeviceID: "d", Scope: model.ScopeMains, Label: "Mains"}); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	var rows []store.MinuteRow
	for day := 0; day < 40; day++ {
		rows = append(rows, store.MinuteRow{MeterID: "ma", Minute: start.AddDate(0, 0, day), AvgW: 20 * 60000, MaxW: 20 * 60000, Seconds: 60})
	}
	if err := s.AddMinutes(ctx, rows); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	api := &Server{Store: s, Collector: collector.New(s, time.Second), Loc: time.UTC, Version: "test", Now: func() time.Time { return now }}
	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)
	return srv, api
}

func rateOn(t *testing.T, api *Server, day string) string {
	t.Helper()
	cfg, err := api.rateConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r := cfg.Schedule(time.UTC).RateAt(rates.MustDate(day))
	if r == nil {
		return "none"
	}
	return r.String()
}

func mustPut(t *testing.T, url, body string) string {
	t.Helper()
	resp, text := put(t, url, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT %s %s: %d %s", url, body, resp.StatusCode, text)
	}
	return text
}

func TestChangingThePriceSourceStartsToday(t *testing.T) {
	srv, api := freezeServer(t)
	mustPut(t, srv.URL+"/api/v1/rates", `{"mode":"one","fixed_rate":"0.10"}`)
	// Nothing is saved yet: there was no price to save when the first one was set.
	if got := rateOn(t, api, "2026-11-05"); got != "0.10" {
		t.Fatalf("before any change the old days follow the one rate: %s", got)
	}
	mustPut(t, srv.URL+"/api/v1/rates", `{"mode":"one","fixed_rate":"0.30"}`)
	if got := rateOn(t, api, "2026-11-05"); got != "0.10" {
		t.Errorf("a finished day kept its price: got %s", got)
	}
	if got := rateOn(t, api, "2026-10-01"); got != "0.10" {
		t.Errorf("the first day kept its price: got %s", got)
	}
	if got := rateOn(t, api, "2026-11-10"); got != "0.30" {
		t.Errorf("today follows the settings: got %s", got)
	}
	// Switching the source to the bills (with none entered) or back changes no finished day either.
	mustPut(t, srv.URL+"/api/v1/rates", `{"mode":"bills"}`)
	mustPut(t, srv.URL+"/api/v1/rates", `{"mode":"one","fixed_rate":"0.99"}`)
	if got := rateOn(t, api, "2026-11-09"); got != "0.10" {
		t.Errorf("after several changes yesterday still costs what it did: got %s", got)
	}
}

func TestRepriceShowsTheDifferenceAndAppliesOnlyOnRequest(t *testing.T) {
	srv, api := freezeServer(t)
	mustPut(t, srv.URL+"/api/v1/rates", `{"mode":"one","fixed_rate":"0.10"}`)
	mustPut(t, srv.URL+"/api/v1/rates", `{"mode":"one","fixed_rate":"0.30"}`)

	var dry repriceAnswer
	resp, text := send(t, "POST", srv.URL+"/api/v1/rates/reprice", `{"from":"2026-11-01","to":"2026-11-02"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", resp.StatusCode, text)
	}
	if err := json.Unmarshal([]byte(text), &dry); err != nil {
		t.Fatal(err)
	}
	// Two days of 20 kWh: 40 kWh at 0.10 is 4.00, at 0.30 is 12.00.
	if dry.Days != 2 || dry.Applied || dry.OldCost == nil || dry.NewCost == nil || dry.Difference == nil {
		t.Fatalf("dry run %+v", dry)
	}
	if rates.MustMoney(*dry.OldCost).Fixed(2) != "4.00" || rates.MustMoney(*dry.NewCost).Fixed(2) != "12.00" || rates.MustMoney(*dry.Difference).Fixed(2) != "8.00" {
		t.Errorf("old %s new %s difference %s", *dry.OldCost, *dry.NewCost, *dry.Difference)
	}
	if got := rateOn(t, api, "2026-11-01"); got != "0.10" {
		t.Errorf("a dry run changes nothing: got %s", got)
	}

	resp, text = send(t, "POST", srv.URL+"/api/v1/rates/reprice", `{"from":"2026-11-01","to":"2026-11-02","apply":true}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", resp.StatusCode, text)
	}
	if rateOn(t, api, "2026-11-01") != "0.30" || rateOn(t, api, "2026-11-02") != "0.30" {
		t.Errorf("the days in the range follow the settings now")
	}
	if got := rateOn(t, api, "2026-11-03"); got != "0.10" {
		t.Errorf("the days outside the range keep their price: got %s", got)
	}

	for _, bad := range []string{`{"from":"2026-11-10"}`, `{"from":"2026-11-05","to":"2026-11-01"}`, `{"from":"nope"}`} {
		if resp, _ := send(t, "POST", srv.URL+"/api/v1/rates/reprice", bad); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s answered %d", bad, resp.StatusCode)
		}
	}
}

func TestBillsAreKeptApartFromThePriceSource(t *testing.T) {
	srv, api := freezeServer(t)
	mustPut(t, srv.URL+"/api/v1/bills", `{"bills":[{"start":"2026-10-01","end":"2026-10-31","total":"62.00","kwh":620}]}`)
	// Saving the price source does not take the bills out, whatever the request carries.
	mustPut(t, srv.URL+"/api/v1/rates", `{"mode":"one","fixed_rate":"0.20","bills":[]}`)
	var got billsAnswer
	get(t, srv.URL+"/api/v1/bills", &got)
	if len(got.Bills) != 1 {
		t.Fatalf("bills after saving the source: %+v", got)
	}
	// With the price from another source, entering a bill changes no price.
	mustPut(t, srv.URL+"/api/v1/bills", `{"bills":[{"start":"2026-10-01","end":"2026-10-31","total":"310.00","kwh":620}]}`)
	if got := rateOn(t, api, "2026-10-15"); got != "0.20" {
		t.Errorf("bills do not price anything unless prices come from them: got %s", got)
	}
}

func TestABillEnteredWhilePricedFromBillsPricesItsDays(t *testing.T) {
	srv, api := freezeServer(t)
	mustPut(t, srv.URL+"/api/v1/rates", `{"mode":"bills"}`)
	// Prices come from the bills but none is entered, so nothing is priced and nothing is saved.
	if got := rateOn(t, api, "2026-10-15"); got != "none" {
		t.Fatalf("no price yet: %s", got)
	}
	mustPut(t, srv.URL+"/api/v1/bills", `{"bills":[{"start":"2026-10-01","end":"2026-10-31","total":"62.00","kwh":620}]}`)
	if got := rateOn(t, api, "2026-10-15"); got != "0.10" {
		t.Errorf("a day the bill covers: %s", got)
	}
	// November is after the latest bill: priced as an estimate, not saved, so the next bill can correct it.
	if got := rateOn(t, api, "2026-11-05"); got != "0.10" {
		t.Errorf("an estimated day uses the latest bill: %s", got)
	}
	list, err := api.Store.DayPrices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range list {
		if d.Day > "2026-10-31" {
			t.Errorf("an estimated day was saved: %+v", d)
		}
	}
	// Correcting the bill prices its days again.
	mustPut(t, srv.URL+"/api/v1/bills", `{"bills":[{"start":"2026-10-01","end":"2026-10-31","total":"124.00","kwh":620}]}`)
	if got := rateOn(t, api, "2026-10-15"); got != "0.20" {
		t.Errorf("a corrected bill prices its days: %s", got)
	}
}

func TestFrozenSummaryAndScheduleHonourSavedDays(t *testing.T) {
	srv, _ := freezeServer(t)
	mustPut(t, srv.URL+"/api/v1/rates", `{"mode":"one","fixed_rate":"0.10"}`)
	mustPut(t, srv.URL+"/api/v1/rates", `{"mode":"one","fixed_rate":"0.20"}`)
	var f frozenAnswer
	get(t, srv.URL+"/api/v1/rates/frozen", &f)
	if f.Days != 40 || f.Through == nil || *f.Through != "2026-11-09" || f.From == nil || *f.From != "2026-10-01" || f.BySource["one"] != 40 {
		t.Errorf("summary %+v", f)
	}

	s := rates.NewSchedule(rates.ModeOne, ptrMoney("0.30"), nil, time.UTC).WithFrozen(map[rates.Date]rates.Money{rates.MustDate("2026-10-01"): rates.MustMoney("0.10")})
	if r := s.RateAt(rates.MustDate("2026-10-01")); r == nil || r.String() != "0.10" || s.IsEstimate(rates.MustDate("2026-10-01")) || !s.IsFrozen(rates.MustDate("2026-10-01")) {
		t.Errorf("a saved day: %v", r)
	}
	if r := s.RateAt(rates.MustDate("2026-10-02")); r == nil || r.String() != "0.30" {
		t.Errorf("a day not saved: %v", r)
	}
}

func ptrMoney(s string) *rates.Money { m := rates.MustMoney(s); return &m }
