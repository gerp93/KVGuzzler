package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/collector"
	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

// reconServer has a house (one mains meter) that drew a steady 1000 W (24 kWh a day) every minute from 1 Oct to 9 Nov, 10 Nov being today.
func reconServer(t *testing.T) (*httptest.Server, *Server) {
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
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	var rows []store.MinuteRow
	for m := 0; m < 40*24*60; m++ {
		rows = append(rows, store.MinuteRow{MeterID: "ma", Minute: start.Add(time.Duration(m) * time.Minute), AvgW: 1000, MaxW: 1000, Seconds: 60})
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

func TestBillsAreCheckedAgainstTheRecordedEnergy(t *testing.T) {
	srv, _ := reconServer(t)
	mustPut(t, srv.URL+"/api/v1/rates", `{"mode":"one","fixed_rate":"0.20"}`)
	// October recorded 744 kWh. The first bill says 750 (0.8% more: a match), the second 800 (7% more: differs), and September has no recording at all.
	mustPut(t, srv.URL+"/api/v1/bills", `{"bills":[
		{"start":"2026-09-01","end":"2026-09-30","total":"150.00","kwh":700},
		{"start":"2026-10-01","end":"2026-10-15","total":"60.00","kwh":375,"fixed_fees":[{"label":"Basic","amount":"10.00"}]},
		{"start":"2026-10-16","end":"2026-10-31","total":"100.00","kwh":420}]}`)
	var got reconciliation
	if r := get(t, srv.URL+"/api/v1/bills/reconciliation", &got); r.StatusCode != http.StatusOK {
		t.Fatalf("status %d", r.StatusCode)
	}
	if !got.HaveHouse || got.PriceSource != "one" || len(got.Bills) != 3 {
		t.Fatalf("%+v", got)
	}
	// Newest first.
	oct2, oct1, sep := got.Bills[0], got.Bills[1], got.Bills[2]
	if sep.RecordedKWh != nil || sep.Verdict != nil {
		t.Errorf("a bill from before the recording has nothing to compare: %+v", sep)
	}
	// 1 to 15 Oct: 15 days x 24 = 360 kWh against 375 billed: 4% less recorded.
	if oct1.RecordedKWh == nil || abs(*oct1.RecordedKWh-360) > 0.01 || oct1.Coverage == nil || *oct1.Coverage < 0.999 {
		t.Fatalf("first October bill %+v", oct1)
	}
	if oct1.DiffShare == nil || abs(*oct1.DiffShare-(-0.04)) > 0.0001 || oct1.Verdict == nil || *oct1.Verdict != "close" {
		t.Errorf("share %v verdict %v", oct1.DiffShare, oct1.Verdict)
	}
	// The bill's variable charge is 50.00 (60 less the 10 flat fee) for 375 kWh; the recorded 360 kWh at 0.20 cost 72.00.
	if oct1.BilledVariable != "50" && oct1.BilledVariable != "50.00" {
		t.Errorf("billed variable %s", oct1.BilledVariable)
	}
	if oct1.RecordedCost == nil || *oct1.RecordedCost != "72.00" && *oct1.RecordedCost != "72" {
		t.Errorf("recorded cost %v", oct1.RecordedCost)
	}
	// 16 to 31 Oct: 16 days x 24 = 384 kWh against 420 billed: 8.6% less recorded, differs.
	if oct2.Verdict == nil || *oct2.Verdict != "differs" {
		t.Errorf("second October bill verdict %v", oct2.Verdict)
	}
	// Two fully recorded bills give a typical share, the middle of 0.96 and 0.914.
	if got.TypicalShare == nil || got.TypicalBills != 2 || abs(*got.TypicalShare-0.9371) > 0.001 {
		t.Errorf("typical %v of %d", got.TypicalShare, got.TypicalBills)
	}
}

func TestAnOutageInsideABillIsCounted(t *testing.T) {
	srv, api := reconServer(t)
	mustPut(t, srv.URL+"/api/v1/bills", `{"bills":[{"start":"2026-10-01","end":"2026-10-31","total":"100.00","kwh":744}]}`)
	if _, err := api.Store.AddOutage(context.Background(), store.Outage{Kind: store.OutagePower, Started: time.Date(2026, 10, 10, 1, 0, 0, 0, time.UTC), Ended: time.Date(2026, 10, 10, 3, 0, 0, 0, time.UTC), Source: store.OutageManual}); err != nil {
		t.Fatal(err)
	}
	var got reconciliation
	get(t, srv.URL+"/api/v1/bills/reconciliation", &got)
	if len(got.Bills) != 1 || got.Bills[0].Outages.PowerSeconds != 7200 {
		t.Fatalf("%+v", got)
	}
	if got.Bills[0].Verdict == nil || *got.Bills[0].Verdict != "match" || got.TypicalShare != nil {
		t.Errorf("verdict %v typical %v", got.Bills[0].Verdict, got.TypicalShare)
	}
}
