package store

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/model"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "house.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestRegisteringAgainKeepsTheUsersLabelAndSwitch(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	ch := model.Channel{ID: "d/ch-01", DeviceID: "d", Scope: model.ScopeCircuit, Label: "Channel 1"}
	if err := s.UpsertChannel(ctx, ch); err != nil {
		t.Fatal(err)
	}
	label, off := "Dryer", false
	if _, err := s.UpdateChannel(ctx, ch.ID, &label, &off); err != nil {
		t.Fatal(err)
	}

	ch.Label = "Channel 1 (renamed by the device)"
	if err := s.UpsertChannel(ctx, ch); err != nil {
		t.Fatal(err)
	}
	got, err := s.Channels(ctx)
	if err != nil || len(got) != 1 {
		t.Fatalf("channels = %v, %v", got, err)
	}
	if got[0].Label != "Dryer" || got[0].Enabled {
		t.Errorf("after re-registering: %+v, want label Dryer and disabled", got[0])
	}
}

func TestUpdateUnknownChannel(t *testing.T) {
	label := "x"
	if _, err := open(t).UpdateChannel(context.Background(), "nope", &label, nil); err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestAddMinuteMergesWeightedBySeconds(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	m := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	// 20 s at 100 W then 40 s at 400 W: 60 s, average 300 W, 5 Wh.
	if err := s.AddMinute(ctx, "c", m, 100, 120, 20); err != nil {
		t.Fatal(err)
	}
	if err := s.AddMinute(ctx, "c", m, 400, 450, 40); err != nil {
		t.Fatal(err)
	}
	tot, err := s.Totals(ctx, "c", m, m.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !near(tot.KWh, 300.0*60/3.6e6) || tot.PeakWatts != 450 || !near(tot.MonitoredHours, 60.0/3600) {
		t.Errorf("totals = %+v", tot)
	}
}

func TestSeriesBuckets(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	loc := time.FixedZone("test", -5*3600)
	// Local midnight is 05:00 UTC. Two hours of 1000 W before it (previous local day), two hours of 2000 W after.
	start := time.Date(2026, 10, 22, 3, 0, 0, 0, time.UTC)
	for i := 0; i < 4*60; i++ {
		w := 1000.0
		if i >= 2*60 {
			w = 2000
		}
		if err := s.AddMinute(ctx, "c", start.Add(time.Duration(i)*time.Minute), w, w, 60); err != nil {
			t.Fatal(err)
		}
	}
	from, to := start, start.Add(4*time.Hour)

	hours, err := s.Series(ctx, "c", from, to, model.BucketHour, loc)
	if err != nil || len(hours) != 4 {
		t.Fatalf("hours = %d points, %v", len(hours), err)
	}
	if !near(hours[0].KWh, 1.0) || !near(hours[3].KWh, 2.0) {
		t.Errorf("hour kWh = %v ... %v", hours[0].KWh, hours[3].KWh)
	}

	days, err := s.Series(ctx, "c", from, to, model.BucketDay, loc)
	if err != nil || len(days) != 2 {
		t.Fatalf("days = %d points, %v", len(days), err)
	}
	if !near(days[0].KWh, 2.0) || !near(days[1].KWh, 4.0) || !near(days[1].AvgWatts, 2000) {
		t.Errorf("day points = %+v", days)
	}

	minutes, err := s.Series(ctx, "c", from, from.Add(10*time.Minute), model.BucketMinute, loc)
	if err != nil || len(minutes) != 10 {
		t.Fatalf("minutes = %d points, %v", len(minutes), err)
	}
}
