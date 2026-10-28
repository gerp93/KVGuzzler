package api

import (
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/model"
)

func TestTheSummaryCarriesTheLowestWholeMinuteOfEachMeterAndOfTheHouse(t *testing.T) {
	srv, st := houseServer(t)
	ctx := t.Context()
	at := func(h, m int) time.Time { return time.Date(2026, 10, 22, h, m, 0, 0, time.UTC) }
	// 15:00: both mains legs report a whole minute (40 + 20 = 60 W, the house's lowest). 15:01: only one leg reports, so the house has no figure for
	// that minute, though that leg's own minute counts for it. 15:02: ten seconds of a leg at 1 W is not a minute, for anyone.
	for _, r := range []struct {
		id      string
		minute  time.Time
		w, secs float64
	}{
		{"mains-a", at(15, 0), 40, 60}, {"mains-b", at(15, 0), 20, 60},
		{"mains-a", at(15, 1), 5, 60},
		{"mains-a", at(15, 2), 1, 10},
	} {
		if err := st.AddMinute(ctx, r.id, r.minute, r.w, r.w, r.secs); err != nil {
			t.Fatal(err)
		}
	}
	var sum model.Summary
	get(t, srv.URL+"/api/v1/summary"+period, &sum)
	if sum.HouseMinWatts == nil || *sum.HouseMinWatts != 60 {
		t.Errorf("house lowest = %v, want 60 (the legs added minute by minute, never their lowest minutes added: that would be 25)", sum.HouseMinWatts)
	}
	want := map[string]float64{"mains-a": 5, "mains-b": 20, "dryer": 200}
	for _, m := range sum.Meters {
		if m.MinWatts == nil || *m.MinWatts != want[m.MeterID] {
			t.Errorf("%s lowest = %v, want %v", m.MeterID, m.MinWatts, want[m.MeterID])
		}
	}

	// A day with nothing recorded has no lowest, not zero.
	var empty model.Summary
	get(t, srv.URL+"/api/v1/summary?from=2026-10-23T00:00:00Z&to=2026-10-24T00:00:00Z", &empty)
	if empty.HouseMinWatts != nil {
		t.Errorf("house lowest on an empty day = %v, want none", *empty.HouseMinWatts)
	}
	for _, m := range empty.Meters {
		if m.MinWatts != nil {
			t.Errorf("%s lowest on an empty day = %v, want none", m.MeterID, *m.MinWatts)
		}
	}
}
