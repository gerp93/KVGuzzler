package alerts

import (
	"strings"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/runs"
)

var t0 = time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)

// pump is `days` days of minutes for a meter that rests at 2 W and works for runLen minutes at 800 W each time one of `at` (hours after t0) comes.
func pump(days int, at []float64, runLen int) []runs.Point {
	n := days * 24 * 60
	pts := make([]runs.Point, n)
	for i := range pts {
		pts[i] = runs.Point{Minute: t0.Add(time.Duration(i) * time.Minute), AvgW: 2, MaxW: 2.2, Seconds: 60}
	}
	for _, h := range at {
		first := int(h * 60)
		for m := first; m < first+runLen && m < n; m++ {
			pts[m].AvgW, pts[m].MaxW = 800, 1000
		}
	}
	return pts
}

// everyHours is run times spaced by the given gaps (hours), starting at hour 10.
func everyHours(gaps ...float64) []float64 {
	out := []float64{10}
	for _, g := range gaps {
		out = append(out, out[len(out)-1]+g)
	}
	return out
}

// septic is the real shape: a run of 4 minutes every 34 to 59 hours.
var septicGaps = []float64{44, 58, 37, 47, 52, 35, 41, 55, 38, 49, 43, 36, 57, 40}

func at(h float64) time.Time { return t0.Add(time.Duration(h * float64(time.Hour))) }

func TestAPatternIsLearnedFromTheRuns(t *testing.T) {
	times := everyHours(septicGaps...)
	pts := pump(30, times, 4)
	now := at(times[len(times)-1] + 10)
	level, ok := SuggestLevel(pts)
	if !ok || level < 5 || level > 500 {
		t.Fatalf("suggested level %v %v", level, ok)
	}
	p := Learn(pts[:int(now.Sub(t0).Minutes())], level, now)
	// The last run is inside the recent day, which is not learned from.
	if !p.Ok || p.Runs != len(times)-1 {
		t.Fatalf("pattern %+v", p)
	}
	if p.RestWatts < 1.9 || p.RestWatts > 2.1 || p.RunMedianMinutes != 4 || p.RunMaxMinutes != 4 || p.PeakMedianWatts != 1000 {
		t.Errorf("pattern %+v", p)
	}
	if p.GapMax != 58*time.Hour || p.MostPerDay != 1 {
		t.Errorf("longest gap %v, most per day %d", p.GapMax, p.MostPerDay)
	}
	if p.LastStart.IsZero() || p.LastEnd.Sub(p.LastStart) != 4*time.Minute {
		t.Errorf("last run %v to %v", p.LastStart, p.LastEnd)
	}
}

func TestTooFewRunsOrNoSpikesIsNotAPattern(t *testing.T) {
	pts := pump(30, everyHours(100, 100), 4) // three runs
	if p := Learn(pts, 20, at(30*24)); p.Ok || p.Why != "too_few_runs" || p.Runs != 3 {
		t.Errorf("%+v", p)
	}
	flat := pump(30, nil, 4)
	if _, ok := SuggestLevel(flat); ok {
		t.Error("a meter that never rises has no level to suggest")
	}
	if p := Learn(flat, 20, at(30*24)); p.Ok || p.Why != "no_spikes" {
		t.Errorf("%+v", p)
	}
	if p := Learn(flat[:30], 20, at(30*24)); p.Ok || p.Why != "no_data" {
		t.Errorf("%+v", p)
	}
}

// check learns from everything up to `now` and checks the same minutes.
func check(t *testing.T, pts []runs.Point, now time.Time, w Watch) []Issue {
	t.Helper()
	pts = pts[:int(now.Sub(t0).Minutes())]
	p := Learn(pts, 20, now)
	if !p.Ok {
		t.Fatalf("no pattern: %+v", p)
	}
	return check2(p, pts, now, w)
}

func check2(p Pattern, pts []runs.Point, now time.Time, w Watch) []Issue {
	return Check(p, pts, now, w, "Septic")
}

func codes(is []Issue) string {
	var s []string
	for _, i := range is {
		s = append(s, i.Code)
	}
	return strings.Join(s, ",")
}

var all = Watch{Overdue: true, TooOften: true, TooLong: true, Rest: true}

func TestAMeterThatKeepsItsPatternIsLeftAlone(t *testing.T) {
	times := everyHours(septicGaps...)
	pts := pump(40, times, 4)
	last := times[len(times)-1]
	for _, wait := range []float64{0.2, 5, 30, 58, 70} {
		if got := check(t, pts, at(last+wait), all); len(got) != 0 {
			t.Errorf("%v h after the last run: %v", wait, codes(got))
		}
	}
}

func TestARunThatIsOverdueIsSaid(t *testing.T) {
	times := everyHours(septicGaps...)
	pts := pump(40, times, 4)
	last := times[len(times)-1]
	// The longest wait was 58 hours; a quarter more is 72.5. 74 hours after the last run's end it is overdue.
	got := check(t, pts, at(last+4.0/60+74), all)
	if codes(got) != "overdue" || !strings.Contains(got[0].Title, "Septic has not run for 3.1 days") || !strings.Contains(got[0].Detail, "58.0 hours") && !strings.Contains(got[0].Detail, "2.4 days") {
		t.Fatalf("%+v", got)
	}
	if got := check(t, pts, at(last+4.0/60+72), Watch{Overdue: true}); len(got) != 0 {
		t.Errorf("72 hours is still inside the margin: %v", codes(got))
	}
	if got := check(t, pts, at(last+4.0/60+74), Watch{Overdue: true, Margin: 1.5}); len(got) != 0 {
		t.Errorf("a wider margin: %v", codes(got))
	}
	if got := check(t, pts, at(last+4.0/60+74), Watch{TooOften: true, TooLong: true, Rest: true}); len(got) != 0 {
		t.Errorf("overdue not watched: %v", codes(got))
	}
}

func TestAWaitWithNothingRecordedIsNotAMissedRun(t *testing.T) {
	times := everyHours(septicGaps...)
	pts := pump(40, times, 4)
	last := times[len(times)-1]
	now := at(last + 4.0/60 + 80)
	cut := int(now.Sub(t0).Minutes())
	// The device was away for most of the wait: only every fifth minute after the last run was recorded.
	kept := pts[:int(at(last+0.2).Sub(t0).Minutes())]
	for i := len(kept); i < cut; i++ {
		if i%5 == 0 {
			kept = append(kept, pts[i])
		}
	}
	p := Learn(kept, 20, now)
	if got := Check(p, kept, now, Watch{Overdue: true}, "Septic"); len(got) != 0 {
		t.Errorf("a gap in the recording says nothing: %v", codes(got))
	}
}

func TestTooOftenAndTooLongAndARestThatMoved(t *testing.T) {
	times := everyHours(septicGaps...)
	last := times[len(times)-1]
	// Six runs in the last day, where the most ever was one.
	often := append(append([]float64{}, times...), last+2, last+6, last+10, last+14, last+18, last+22)
	pts := pump(40, often, 4)
	if got := check(t, pts, at(last+23), Watch{TooOften: true}); codes(got) != "too_often" {
		t.Errorf("often: %v", codes(got))
	}

	// A run of 45 minutes ending now, where the longest ever was 4.
	long := pump(40, times, 4)
	start := int(at(last + 30).Sub(t0).Minutes())
	for m := start; m < start+45; m++ {
		long[m].AvgW, long[m].MaxW = 800, 1000
	}
	now := at(last + 30).Add(44 * time.Minute)
	got := check(t, long, now, Watch{TooLong: true})
	if codes(got) != "too_long" || !strings.Contains(got[0].Title, "running for 44 min") {
		t.Errorf("long: %+v", got)
	}

	// It rests at 9 W for the last three hours instead of 2 W, and at 0.5 W in another case.
	for _, c := range []struct {
		watts float64
		want  string
	}{{9, "rest_high"}, {0.5, "rest_low"}, {2.5, ""}} {
		moved := pump(40, times, 4)
		from := int(at(last + 30).Sub(t0).Minutes())
		for m := from; m < from+4*60; m++ {
			moved[m].AvgW, moved[m].MaxW = c.watts, c.watts+0.2
		}
		if got := check(t, moved, at(last+30+4), Watch{Rest: true}); codes(got) != c.want {
			t.Errorf("rest at %v W: %v, want %q", c.watts, codes(got), c.want)
		}
	}
}

func TestNothingIsSaidWithoutAPattern(t *testing.T) {
	if got := Check(Pattern{}, pump(3, nil, 4), at(48), all, "X"); got != nil {
		t.Errorf("%v", got)
	}
}

func TestHumane(t *testing.T) {
	for d, want := range map[time.Duration]string{12 * time.Minute: "12 min", 5*time.Hour + 30*time.Minute: "5.5 hours", 65*time.Hour + 12*time.Minute: "2.7 days"} {
		if got := Humane(d); got != want {
			t.Errorf("%v: %q, want %q", d, got, want)
		}
	}
}
