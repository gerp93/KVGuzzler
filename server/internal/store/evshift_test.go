package store

import (
	"context"
	"testing"
)

func TestShiftingReadingsMovesEachOneADayLaterAndKeepsTodaysLookAsPending(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	id := monitor(t, s, "refoss-shift-ev", 1)[0]
	if _, err := s.SetMeterEV(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	for day, miles := range map[string]float64{"2026-10-27": 100, "2026-10-28": 110, "2026-10-29": 120, "2026-10-31": 150, "2026-11-02": 160} {
		if err := s.SetOdometer(ctx, id, day, miles); err != nil {
			t.Fatal(err)
		}
	}
	moved, err := s.ShiftOdometerDays(ctx, id, "2026-10-28", "2026-11-02")
	if err != nil {
		t.Fatal(err)
	}
	if moved != 3 {
		t.Errorf("moved = %d, want 3", moved)
	}
	got, err := s.Odometer(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		day   string
		miles float64
	}{{"2026-10-27", 100}, {"2026-10-29", 110}, {"2026-10-30", 120}, {"2026-11-01", 150}}
	if len(got) != len(want) {
		t.Fatalf("readings = %+v, want %+v", got, want)
	}
	for i, w := range want {
		if got[i].Day != w.day || got[i].Miles != w.miles {
			t.Errorf("reading %d = %+v, want %+v", i, got[i], w)
		}
	}
	// The reading of a day that was not over is a look, kept as the one waiting for the day to end.
	day, miles, ok, err := s.PendingOdometer(ctx, id)
	if err != nil || !ok || day != "2026-11-02" || miles != 160 {
		t.Errorf("pending = %q %v %v %v", day, miles, ok, err)
	}
}
