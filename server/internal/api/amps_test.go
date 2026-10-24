package api

import (
	"fmt"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/model"
)

func TestTheHighestCurrentIsKeptWithTheHighestPowerAndShownInSeriesAndSummary(t *testing.T) {
	srv, now := newServer(t, 5)
	from, to := day.Add(-time.Minute).Format(time.RFC3339), now.Add(time.Minute).Format(time.RFC3339)
	id := "sim-em16p:mains-a" // the simulated mains leg A reads 121 V, so its amps are its watts over 121

	var s model.Summary
	get(t, fmt.Sprintf("%s/api/v1/summary?from=%s&to=%s", srv.URL, from, to), &s)
	var mine *model.MeterSummary
	for i := range s.Meters {
		if s.Meters[i].MeterID == id {
			mine = &s.Meters[i]
		}
	}
	if mine == nil || mine.PeakAmps == nil || mine.PeakWatts == nil || math.Abs(*mine.PeakAmps-*mine.PeakWatts/121) > 1e-6 {
		t.Fatalf("summary = %+v: the peak current should be the peak power over 121 V", mine)
	}

	var sr model.Series
	get(t, fmt.Sprintf("%s/api/v1/series?meter_id=%s&from=%s&to=%s&bucket=minute", srv.URL, id, from, to), &sr)
	if len(sr.Points) == 0 || sr.Points[0].MaxAmps == nil || math.Abs(*sr.Points[0].MaxAmps-sr.Points[0].MaxWatts/121) > 1e-6 {
		t.Errorf("series = %+v", sr.Points)
	}
	// The house's figures are watts only: no current is made up for the legs added together.
	if s.HouseKWh == nil {
		t.Error("no house")
	}
}

func TestABreakerSizeIsSetAndForgottenThroughTheMeterAndBadOnesAreRefused(t *testing.T) {
	srv, _ := newServer(t, 1)
	url := srv.URL + "/api/v1/meters/sim-em16p:ch-01"
	read := func() *float64 {
		var chs struct{ Meters []model.Meter }
		get(t, srv.URL+"/api/v1/meters", &chs)
		for _, m := range chs.Meters {
			if m.ID == "sim-em16p:ch-01" {
				return m.BreakerAmps
			}
		}
		t.Fatal("no such meter")
		return nil
	}
	if r := patch(t, url, `{"breaker_amps":30}`); r.StatusCode != http.StatusOK {
		t.Fatalf("set: %d", r.StatusCode)
	}
	if b := read(); b == nil || *b != 30 {
		t.Errorf("breaker = %v, want 30", b)
	}
	for _, bad := range []string{`{"breaker_amps":0}`, `{"breaker_amps":-3}`, `{"breaker_amps":5000}`} {
		if r := patch(t, url, bad); r.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", bad, r.StatusCode)
		}
	}
	if r := patch(t, url, `{"breaker_amps":"big"}`); r.StatusCode != http.StatusBadRequest {
		t.Errorf("a word: %d, want 400", r.StatusCode)
	}
	if b := read(); b == nil || *b != 30 {
		t.Errorf("a refused change altered the breaker: %v", b)
	}
	if r := patch(t, url, `{"breaker_amps":null}`); r.StatusCode != http.StatusOK {
		t.Fatalf("forget: %d", r.StatusCode)
	}
	if b := read(); b != nil {
		t.Errorf("breaker = %v after forgetting", *b)
	}
}

func TestLiveReadingsCarryVoltsAndAmps(t *testing.T) {
	srv, _ := newServer(t, 1)
	var live model.Live
	get(t, srv.URL+"/api/v1/live", &live)
	got := 0
	for _, r := range live.Readings {
		if r.Volts != nil && r.Amps != nil && math.Abs(*r.Amps**r.Volts-r.Watts) < 1e-6 {
			got++
		}
	}
	if got != len(live.Readings) || got == 0 {
		t.Errorf("%d of %d readings have volts and amps that agree with watts", got, len(live.Readings))
	}
}
