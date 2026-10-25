package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestTheHighestCurrentOfAMinuteIsKeptNextToTheHighestPowerAndMerges(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	ids := monitor(t, s, "refoss-amps", 2)
	t0 := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	f := func(v float64) *float64 { return &v }

	// A minute written in two halves keeps the larger current, and a half with no current does not erase the other's.
	if err := s.AddMinuteAmps(ctx, ids[0], t0, 1000, 1100, f(8.5), 30); err != nil {
		t.Fatal(err)
	}
	if err := s.AddMinuteAmps(ctx, ids[0], t0, 1000, 1200, f(9.5), 20); err != nil {
		t.Fatal(err)
	}
	if err := s.AddMinute(ctx, ids[0], t0, 1000, 1000, 10); err != nil {
		t.Fatal(err)
	}
	tot, err := s.Totals(ctx, ids[0], t0, t0.Add(time.Minute))
	if err != nil || tot.PeakAmps == nil || *tot.PeakAmps != 9.5 {
		t.Fatalf("totals = %+v, %v: want the larger current, 9.5 A", tot, err)
	}

	// A meter that never reported a current has none: nil, not 0 A.
	if err := s.AddMinute(ctx, ids[1], t0, 100, 100, 60); err != nil {
		t.Fatal(err)
	}
	if tot, _ := s.Totals(ctx, ids[1], t0, t0.Add(time.Minute)); tot.PeakAmps != nil {
		t.Errorf("a meter with no current reported has peak %v A", *tot.PeakAmps)
	}
}

func TestSeriesCarryTheHighestCurrentOfEachBucket(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	ids := monitor(t, s, "refoss-sa", 1)
	t0 := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	rows := []MinuteRow{}
	for i := 0; i < 120; i++ {
		a := 5.0
		if i == 70 {
			a = 17
		}
		rows = append(rows, MinuteRow{MeterID: ids[0], Minute: t0.Add(time.Duration(i) * time.Minute), AvgW: 600, MaxW: 700, Seconds: 60, MaxA: &a})
	}
	if err := s.AddMinutes(ctx, rows); err != nil {
		t.Fatal(err)
	}
	hours, err := s.Series(ctx, ids[0], t0, t0.Add(2*time.Hour), "hour", time.UTC)
	if err != nil || len(hours) != 2 || hours[0].MaxAmps == nil || *hours[0].MaxAmps != 5 || *hours[1].MaxAmps != 17 {
		t.Fatalf("hours = %+v, %v: the second hour's highest is 17 A", hours, err)
	}
	mins, _ := s.Series(ctx, ids[0], t0, t0.Add(2*time.Hour), "minute", time.UTC)
	if len(mins) != 120 || *mins[70].MaxAmps != 17 {
		t.Errorf("minute 70 = %+v", mins[70])
	}
}

func TestPairingKeepsTheLargerLegsCurrentNeverTheSum(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	ids := monitor(t, s, "refoss-pa", 2)
	t0 := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	a, b := 30.0, 32.0
	for i := 0; i < 10; i++ {
		at := t0.Add(time.Duration(i) * time.Minute)
		if err := s.AddMinuteAmps(ctx, ids[0], at, 3000, 3100, &a, 60); err != nil {
			t.Fatal(err)
		}
		if err := s.AddMinuteAmps(ctx, ids[1], at, 3000, 3100, &b, 60); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.SetMeterWiring(ctx, ids[0], WiringChange{SetPair: true, PairWith: &ids[1]}); err != nil {
		t.Fatal(err)
	}
	tot, _ := s.Totals(ctx, ids[0], t0, t0.Add(time.Hour))
	if tot.PeakAmps == nil || *tot.PeakAmps != 32 {
		t.Errorf("the pair's current = %v, want the larger leg's 32 A (the same current runs through both legs), not 62", tot.PeakAmps)
	}
	if tot.PeakWatts != 6200 {
		t.Errorf("the pair's peak power = %v, want the two legs added", tot.PeakWatts)
	}
}

func TestABreakerSizeIsSavedChangedAndForgotten(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	ids := monitor(t, s, "refoss-br", 1)
	v := 50.0
	m, err := s.SetMeterBreaker(ctx, ids[0], &v)
	if err != nil || m.BreakerAmps == nil || *m.BreakerAmps != 50 {
		t.Fatalf("%+v %v", m, err)
	}
	if got, _ := s.Meter(ctx, ids[0]); got.BreakerAmps == nil || *got.BreakerAmps != 50 {
		t.Errorf("not kept: %+v", got.BreakerAmps)
	}
	if m, err = s.SetMeterBreaker(ctx, ids[0], nil); err != nil || m.BreakerAmps != nil {
		t.Errorf("forgetting: %+v %v", m.BreakerAmps, err)
	}
	for _, bad := range []float64{0, -5, 1001} {
		bad := bad
		if _, err := s.SetMeterBreaker(ctx, ids[0], &bad); err == nil {
			t.Errorf("%v A was accepted as a breaker", bad)
		}
	}
	if _, err := s.SetMeterBreaker(ctx, "nope", &v); err == nil {
		t.Error("an unknown meter was accepted")
	}
}

func TestOpeningADatabaseFromBeforeCurrentsWereKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`CREATE TABLE meters (id TEXT PRIMARY KEY, device_id TEXT NOT NULL, idx INTEGER, scope TEXT NOT NULL, label TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1, max_watts REAL, computer_id TEXT, group_id TEXT);
CREATE TABLE minutes (meter_id TEXT NOT NULL, minute INTEGER NOT NULL, avg_w REAL NOT NULL, max_w REAL NOT NULL, seconds REAL NOT NULL, PRIMARY KEY (meter_id, minute)) WITHOUT ROWID;
INSERT INTO meters(id, device_id, scope, label) VALUES ('m', 'd', 'circuit', 'Old');
INSERT INTO minutes VALUES ('m', 1000020, 100, 120, 60);`); err != nil {
		t.Fatal(err)
	}
	old.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tot, err := s.Totals(context.Background(), "m", time.Unix(1000000, 0), time.Unix(1001000, 0))
	if err != nil || tot.KWh == 0 || tot.PeakAmps != nil {
		t.Fatalf("old minutes: %+v %v: their energy is kept and they have no current", tot, err)
	}
	ms, err := s.Meters(context.Background())
	if err != nil || len(ms) != 1 || ms[0].BreakerAmps != nil {
		t.Errorf("meters = %+v %v", ms, err)
	}
}
