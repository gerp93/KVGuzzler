package baseload

import (
	"math"
	"testing"
)

func flat(n int, w float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = w
	}
	return out
}

func TestTheBaseLoadIsWhatItRestsAt(t *testing.T) {
	// A day: 20 hours at 300 W and 4 hours at 3 kW; the 5th percentile of the minutes is the 300 W.
	day := append(flat(20*60, 300), flat(4*60, 3000)...)
	r, ok := Of(day)
	if !ok || math.Abs(r.Watts-300) > 1e-9 {
		t.Fatalf("base = %+v %v, want 300 W", r, ok)
	}
	if want := (20*60*300.0 + 4*60*3000.0) / 1440; math.Abs(r.AvgWatts-want) > 1e-9 {
		t.Errorf("average %v, want %v", r.AvgWatts, want)
	}
	if got := r.Share(); math.Abs(got-300/r.AvgWatts) > 1e-9 {
		t.Errorf("share %v", got)
	}
	if got := r.KWhPerYear(); math.Abs(got-300*8766.0/1000) > 1e-9 {
		t.Errorf("per year %v", got)
	}
}

func TestOneGlitchedMinuteDoesNotSetIt(t *testing.T) {
	w := flat(1000, 120)
	w[500] = 0 // a dropout
	w[501] = 0
	r, _ := Of(w)
	if r.Watts != 120 {
		t.Errorf("base %v, two dropped minutes in a thousand must not pull it below 120", r.Watts)
	}
}

func TestTooLittleDataSaysNothing(t *testing.T) {
	if _, ok := Of(flat(MinMinutes-1, 100)); ok {
		t.Error("under an hour of minutes gave a figure")
	}
	if _, ok := Of(nil); ok {
		t.Error("no minutes gave a figure")
	}
}

func TestNegativeAndBrokenReadingsAreNotPower(t *testing.T) {
	w := append(flat(100, 50), math.NaN(), math.Inf(1))
	for i := 0; i < 10; i++ {
		w[i] = -400
	}
	r, ok := Of(w)
	if !ok || r.Watts < 0 || math.IsNaN(r.AvgWatts) || r.Minutes != 100 {
		t.Errorf("%+v %v", r, ok)
	}
}

func TestAMeterThatUsesNothingHasNoShare(t *testing.T) {
	r, _ := Of(flat(120, 0))
	if r.Share() != 0 || r.Watts != 0 {
		t.Errorf("%+v", r)
	}
}
