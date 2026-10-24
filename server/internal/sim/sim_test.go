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
