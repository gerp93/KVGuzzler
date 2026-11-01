package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// threeDays records 1 kW for every minute of an hour on each of three days (the 20th, 21st and 22nd), so every day is 1 kWh.
func threeDays(t *testing.T, s *Store, id string) {
	t.Helper()
	for d := 20; d <= 22; d++ {
		for m := 0; m < 60; m++ {
			if err := s.AddMinute(context.Background(), id, time.Date(2026, 10, d, 12, m, 0, 0, time.UTC), 1000, 1000, 60); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestNothingBeforeTheStartOfHistoryIsLookedAtWhateverThePeriod(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	id := monitor(t, s, "refoss-floor", 1)[0]
	threeDays(t, s, id)
	long := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)

	all, _ := s.Totals(ctx, id, long, end)
	if !near(all.KWh, 3) {
		t.Fatalf("all time without a cutoff = %v kWh, want 3", all.KWh)
	}

	// History starts on the 21st, at midnight (UTC here): the 20th is never looked at, even by "all time".
	if err := s.SetHistoryStart(ctx, "2026-10-21", time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if tot, _ := s.Totals(ctx, id, long, end); !near(tot.KWh, 2) {
		t.Errorf("all time = %v kWh, want 2 (the 20th is before history starts)", tot.KWh)
	}
	if tot, _ := s.Totals(ctx, id, time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)); tot.KWh != 0 {
		t.Errorf("a period wholly before the start = %v kWh, want 0", tot.KWh)
	}
	pts, _ := s.Series(ctx, id, long, end, "hour", time.UTC)
	if len(pts) != 2 || pts[0].T.Day() != 21 {
		t.Errorf("series = %+v, want the 21st and 22nd only", pts)
	}
	if mins, _ := s.MeterMinutes(ctx, id, long, end); len(mins) != 120 || mins[0].Minute.Day() != 21 {
		t.Errorf("%d minutes, want 120 from the 21st", len(mins))
	}
	if w, _ := s.MinuteWatts(ctx, []string{id}, long, end); len(w) != 120 {
		t.Errorf("%d whole minutes, want 120", len(w))
	}
	if h, _ := s.HourlyKWh(ctx, []string{id}, long, end); len(h) != 2 {
		t.Errorf("%d hours, want 2", len(h))
	}
	if first, ok, _ := s.FirstData(ctx); !ok || first.Day() != 21 {
		t.Errorf("first data = %v, %v, want the 21st", first, ok)
	}
	if first, ok, _ := s.FirstMinute(ctx, id); !ok || first.Day() != 21 {
		t.Errorf("first minute = %v, %v, want the 21st", first, ok)
	}
	// Only what the cutoff hides is still there to ask about.
	if ever, ok, _ := s.FirstDataEver(ctx); !ok || ever.Day() != 20 {
		t.Errorf("first ever = %v, %v, want the 20th", ever, ok)
	}

	// It is saved: a database opened again still starts on the 21st.
	path := filepath.Join(t.TempDir(), "again.db")
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if err := s2.SetHistoryStart(ctx, "2026-10-21", time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	s2.Close()
	s3, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s3.Close()
	if day, at, ok := s3.HistoryStart(); !ok || day != "2026-10-21" || !at.Equal(time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("reopened: %v %v %v", day, at, ok)
	}

	// Taking it off brings everything back: nothing was deleted.
	if err := s.SetHistoryStart(ctx, "", time.Time{}); err != nil {
		t.Fatal(err)
	}
	if tot, _ := s.Totals(ctx, id, long, end); !near(tot.KWh, 3) {
		t.Errorf("with the cutoff off = %v kWh, want 3 again", tot.KWh)
	}
	if _, _, ok := s.HistoryStart(); ok {
		t.Error("the cutoff is still there")
	}
}

func TestOdometerReadingsAndChargesBeforeTheStartOfHistoryAreLeftOut(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	id := monitor(t, s, "refoss-floor-ev", 1)[0]
	if _, err := s.SetMeterEV(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	for day, miles := range map[string]float64{"2026-10-20": 100, "2026-10-21": 150, "2026-10-22": 200} {
		if err := s.SetOdometer(ctx, id, day, miles); err != nil {
			t.Fatal(err)
		}
	}
	c, err := s.StartEvCharge(ctx, id, time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	_ = c
	if err := s.SetHistoryStart(ctx, "2026-10-21", time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.Odometer(ctx, id); len(r) != 2 || r[0].Day != "2026-10-21" {
		t.Errorf("readings = %+v, want the 21st and the 22nd", r)
	}
	if list, _ := s.EvCharges(ctx, id, 10); len(list) != 0 {
		t.Errorf("%d charges, want none: the only one was on the 20th", len(list))
	}
}
