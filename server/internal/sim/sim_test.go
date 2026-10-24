package sim

import (
	"context"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/model"
)

// Over a whole day the mains must never read less than the circuits, and nothing may exceed its cap.
func TestMainsCoverCircuitsAndStayUnderCaps(t *testing.T) {
	day := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	var now time.Time
	s := NewAt(func() time.Time { return now })
	caps := map[string]float64{}
	scope := map[string]model.Scope{}
	for _, c := range s.Meters() {
		caps[c.ID] = *c.MaxWatts
		scope[c.ID] = c.Scope
	}

	for sec := 0; sec < 24*3600; sec += 37 {
		now = day.Add(time.Duration(sec) * time.Second)
		rs, err := s.Poll(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		var mains, circuits float64
		for _, r := range rs {
			if _, inside := s.Inside()[r.MeterID]; inside && scope[r.MeterID] == model.ScopeCircuit {
				continue // already in the reading of the feed that holds it
			}
			if r.Watts < 0 || r.Watts > caps[r.MeterID] {
				t.Fatalf("%s read %.0f W at %s, cap %.0f", r.MeterID, r.Watts, now, caps[r.MeterID])
			}
			switch scope[r.MeterID] {
			case model.ScopeMains:
				mains += r.Watts
			case model.ScopeCircuit:
				circuits += r.Watts
			}
		}
		if mains < circuits {
			t.Fatalf("at %s mains %.0f W is below circuits %.0f W", now, mains, circuits)
		}
	}
}

func TestEveryReadingHasAMeter(t *testing.T) {
	s := NewAt(func() time.Time { return time.Date(2026, 10, 22, 18, 30, 0, 0, time.UTC) })
	known := map[string]bool{}
	for _, c := range s.Meters() {
		known[c.ID] = true
	}
	rs, _ := s.Poll(context.Background())
	if len(rs) != len(known) {
		t.Errorf("%d readings for %d meters", len(rs), len(known))
	}
	for _, r := range rs {
		if !known[r.MeterID] {
			t.Errorf("reading for unknown meter %s", r.MeterID)
		}
	}
}

func TestHistoryHasEveryMeterEveryMinuteAndTheSameDayAlwaysLooksTheSame(t *testing.T) {
	end := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	var rows []MinuteRow
	if err := History(context.Background(), end, 2, func(b []MinuteRow) error {
		rows = append(rows, b...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	meters := len(New().Meters())
	if want := 2 * 24 * 60 * meters; len(rows) != want {
		t.Fatalf("%d rows, want %d (every meter, every minute of two days)", len(rows), want)
	}
	caps := map[string]float64{}
	for _, m := range New().Meters() {
		caps[m.ID] = *m.MaxWatts
	}
	for _, r := range rows {
		if r.Seconds != 60 || r.AvgW < 0 || r.MaxW < r.AvgW {
			t.Fatalf("a bad minute: %+v", r)
		}
		if c, ok := caps[r.MeterID]; !ok || r.MaxW > c {
			t.Fatalf("%s: %+v over its cap %v or not a meter of the house", r.MeterID, r, c)
		}
	}
	// The same day is the same whenever it is made, and the plans differ from day to day.
	var again []MinuteRow
	_ = History(context.Background(), end, 2, func(b []MinuteRow) error { again = append(again, b...); return nil })
	for i := range rows {
		if rows[i] != again[i] {
			t.Fatalf("row %d differs between two runs: %+v and %+v", i, rows[i], again[i])
		}
	}
	plans := map[bool]int{}
	for d := 1; d <= 20; d++ {
		plans[planFor(time.Date(2026, 10, d, 12, 0, 0, 0, time.UTC)).evOn]++
	}
	if plans[true] == 0 || plans[false] == 0 {
		t.Errorf("the car charged on %d of 20 days: the days should differ", plans[true])
	}
}

func TestAWindowCanRunPastMidnight(t *testing.T) {
	for _, c := range []struct {
		h, start, length float64
		want             bool
	}{{23.5, 23, 2, true}, {0.5, 23, 2, true}, {1.5, 23, 2, false}, {22.9, 23, 2, false}, {12, 11.5, 1, true}} {
		if got := window(c.h, c.start, c.length); got != c.want {
			t.Errorf("window(%v, %v, %v) = %v", c.h, c.start, c.length, got)
		}
	}
}
