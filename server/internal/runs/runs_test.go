package runs

import (
	"math"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)

// day builds minutes from (start minute, length, watts) stretches over a resting level; minutes not listed are at rest.
func day(minutes int, rest float64, on ...[3]float64) []Point {
	pts := make([]Point, minutes)
	for i := range pts {
		pts[i] = Point{Minute: t0.Add(time.Duration(i) * time.Minute), AvgW: rest, MaxW: rest, Seconds: 60}
	}
	for _, o := range on {
		for m := int(o[0]); m < int(o[0]+o[1]); m++ {
			pts[m].AvgW, pts[m].MaxW = o[2], o[2]*1.05
		}
	}
	return pts
}

func TestAChargerSessionIsOneRunWithItsEnergyDurationAndPeak(t *testing.T) {
	pts := day(1440, 4, [3]float64{60, 180, 7200}) // three hours at 7.2 kW
	th := Threshold(pts)
	got := Detect(pts, th, DefaultGapMinutes, DefaultMinMinutes)
	if len(got) != 1 {
		t.Fatalf("%d runs, want 1 (threshold %v)", len(got), th)
	}
	r := got[0]
	if !r.Start.Equal(t0.Add(60*time.Minute)) || !r.End.Equal(t0.Add(240*time.Minute)) || r.Minutes != 180 {
		t.Errorf("run = %+v, want minutes 60 to 240", r)
	}
	if math.Abs(r.KWh-21.6) > 1e-9 || math.Abs(r.AvgWatts-7200) > 1e-6 || math.Abs(r.PeakWatts-7560) > 1e-6 {
		t.Errorf("energy %v kWh, average %v W, peak %v W, want 21.6, 7200 and 7560", r.KWh, r.AvgWatts, r.PeakWatts)
	}
}

func TestADipInsideARunDoesNotEndItButALongGapDoes(t *testing.T) {
	// Two stretches four minutes apart are one run (a thermostat clicking off); two hours apart they are two.
	near := day(600, 5, [3]float64{100, 30, 3000}, [3]float64{134, 30, 3000})
	got := Detect(near, 500, DefaultGapMinutes, DefaultMinMinutes)
	if len(got) != 1 || got[0].Minutes != 64 {
		t.Fatalf("near: %+v, want one run of 64 minutes", got)
	}
	// The dip's own minutes are part of the run's energy: 60 minutes at 3 kW plus four at 5 W.
	if want := (60*3000.0 + 4*5.0) / 1000 / 60; math.Abs(got[0].KWh-want) > 1e-9 {
		t.Errorf("energy %v, want %v", got[0].KWh, want)
	}
	far := day(600, 5, [3]float64{100, 30, 3000}, [3]float64{250, 30, 3000})
	if got := Detect(far, 500, DefaultGapMinutes, DefaultMinMinutes); len(got) != 2 {
		t.Errorf("far: %d runs, want 2", len(got))
	}
}

func TestABlipIsNotARunAndAMeterThatJustStaysOnNeverRuns(t *testing.T) {
	blip := day(300, 5, [3]float64{50, 2, 2000}, [3]float64{100, 40, 2000})
	if got := Detect(blip, Threshold(blip), DefaultGapMinutes, DefaultMinMinutes); len(got) != 1 || got[0].Minutes != 40 {
		t.Errorf("blip: %+v, want only the 40-minute run", got)
	}
	flat := day(300, 120)
	for i := range flat {
		flat[i].AvgW = 120 + float64(i%3) // a fridge-sized steady draw
	}
	if th := Threshold(flat); !math.IsInf(th, 1) {
		t.Errorf("a steady meter has threshold %v, want +Inf", th)
	}
	if got := Detect(flat, Threshold(flat), DefaultGapMinutes, DefaultMinMinutes); len(got) != 0 {
		t.Errorf("a steady meter has runs: %+v", got)
	}
	if Threshold(nil) != math.Inf(1) || len(Detect(nil, 10, 5, 3)) != 0 {
		t.Error("no minutes, no runs")
	}
}

func TestMissingMinutesEndARunLikeALongGapAndPartialMinutesCountForWhatWasSeen(t *testing.T) {
	pts := day(100, 5, [3]float64{10, 20, 1000})
	// Cut a 30-minute hole in the middle of the run: the data stops, so the run does too.
	cut := append(append([]Point{}, pts[:20]...), pts[50:]...)
	got := Detect(cut, 500, DefaultGapMinutes, DefaultMinMinutes)
	if len(got) != 1 || got[0].Minutes != 10 { // minutes 10 to 19 only: the rest of the stretch was never recorded
		t.Errorf("with a hole: %+v", got)
	}
	half := day(100, 5, [3]float64{10, 10, 1000})
	for i := 10; i < 20; i++ {
		half[i].Seconds = 30
	}
	got = Detect(half, 500, DefaultGapMinutes, DefaultMinMinutes)
	if len(got) != 1 || math.Abs(got[0].KWh-(10*1000.0*30)/3.6e6) > 1e-9 {
		t.Errorf("half-covered minutes: %+v", got)
	}
}

func TestARunsAverageCurrentIsOverTheTimeItWasReportedAndNilWithoutOne(t *testing.T) {
	pts := day(600, 5, [3]float64{100, 60, 3000})
	// 20 A for the first half hour of the run, 30 A for the second; the minutes before and after it report 1 A and are not part of it.
	for i := range pts {
		a := 1.0
		if i >= 100 && i < 130 {
			a = 20
		} else if i >= 130 && i < 160 {
			a = 30
		}
		pts[i].AvgAmps, pts[i].MaxAmps, pts[i].HaveAmps = a, a+2, true
	}
	got := Detect(pts, 500, DefaultGapMinutes, DefaultMinMinutes)
	if len(got) != 1 || got[0].AvgAmps == nil || got[0].PeakAmps == nil {
		t.Fatalf("runs = %+v", got)
	}
	if math.Abs(*got[0].AvgAmps-25) > 1e-9 || math.Abs(*got[0].PeakAmps-32) > 1e-9 {
		t.Errorf("avg %v peak %v, want 25 and 32", *got[0].AvgAmps, *got[0].PeakAmps)
	}
	// Without a current on any minute the run has none.
	plain := Detect(day(600, 5, [3]float64{100, 60, 3000}), 500, DefaultGapMinutes, DefaultMinMinutes)
	if len(plain) != 1 || plain[0].AvgAmps != nil || plain[0].PeakAmps != nil {
		t.Errorf("a run without currents: %+v", plain)
	}
}

func TestAMeterThatWorksOnlyAFewPercentOfTheTimeStillHasARunLevel(t *testing.T) {
	// A water heater: 30 minutes of 4.4 kW in a day, zero the rest (2% of the time). The 95th percentile of such a day is zero, the 99th is not.
	pts := day(1440, 0, [3]float64{360, 30, 4400})
	th := Threshold(pts)
	if math.IsInf(th, 1) {
		t.Fatal("a meter that works 2% of the time was called steady")
	}
	got := Detect(pts, th, DefaultGapMinutes, DefaultMinMinutes)
	if len(got) != 1 || got[0].Minutes != 30 || math.Abs(got[0].KWh-2.2) > 1e-9 {
		t.Errorf("runs = %+v, want the one half-hour heating, 2.2 kWh", got)
	}
}

func TestLevelsOfAMeterThatWorksAndRests(t *testing.T) {
	// A fridge-like meter: 3 W at rest most of the time, 120 W while it works.
	var pts []Point
	start := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	for m := 0; m < 600; m++ {
		w := 3.0
		if m%100 < 20 {
			w = 120
		}
		pts = append(pts, Point{Minute: start.Add(time.Duration(m) * time.Minute), AvgW: w, MaxW: w, Seconds: 60})
	}
	l, ok := LevelsOf(pts)
	if !ok || !l.HasWorking {
		t.Fatalf("levels = %+v ok=%v, want a working level", l, ok)
	}
	if l.RestWatts != 3 {
		t.Errorf("rest = %v, want 3", l.RestWatts)
	}
	if l.WorkingAboveWatts <= 3 || l.WorkingAboveWatts >= 120 {
		t.Errorf("working level = %v, want between rest and working", l.WorkingAboveWatts)
	}
}

func TestLevelsOfASteadyMeterIsAlwaysResting(t *testing.T) {
	var pts []Point
	start := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	for m := 0; m < 300; m++ {
		pts = append(pts, Point{Minute: start.Add(time.Duration(m) * time.Minute), AvgW: 90 + float64(m%3), MaxW: 95, Seconds: 60})
	}
	l, ok := LevelsOf(pts)
	if !ok || l.HasWorking {
		t.Fatalf("levels = %+v ok=%v, want resting only", l, ok)
	}
	if l.RestWatts < 90 || l.RestWatts > 92 {
		t.Errorf("rest = %v, want about 91", l.RestWatts)
	}
}

func TestAFridgeThatSpendsMostOfItsTimeRunningRestsAtThatLevel(t *testing.T) {
	// A kitchen island with a mini fridge: 15 W while the compressor is off (40% of the time), 74 W while it is on (60%). That is its normal
	// cycle, so 78 W is not "working" and the resting draw is 74 W, not 15 W.
	var pts []Point
	start := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	for m := 0; m < 1000; m++ {
		w := 74.0 + float64(m%5)*0.5
		if m%10 < 4 {
			w = 15
		}
		pts = append(pts, Point{Minute: start.Add(time.Duration(m) * time.Minute), AvgW: w, MaxW: w, Seconds: 60})
	}
	l, ok := LevelsOf(pts)
	if !ok || l.HasWorking {
		t.Fatalf("levels = %+v ok=%v, want resting only", l, ok)
	}
	if l.RestWatts < 74 || l.RestWatts > 76 {
		t.Errorf("rest = %v, want about 75", l.RestWatts)
	}
	if got := Detect(pts, Threshold(pts), DefaultGapMinutes, DefaultMinMinutes); len(got) != 0 {
		t.Errorf("a cycling fridge has %d runs, want none", len(got))
	}
}

func TestLevelsNeedEnoughHistory(t *testing.T) {
	var pts []Point
	start := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	for m := 0; m < MinLevelMinutes-1; m++ {
		pts = append(pts, Point{Minute: start.Add(time.Duration(m) * time.Minute), AvgW: 5, Seconds: 60})
	}
	if _, ok := LevelsOf(pts); ok {
		t.Error("a short history must not claim a level")
	}
}
