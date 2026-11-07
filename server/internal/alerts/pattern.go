// Package alerts holds the parts of the alert system that need no database or clock of their own, so they can be tested on made-up minutes: what a meter's
// usual pattern looks like, and when what it is doing now departs from that.
//
// A meter that works in short bursts (a septic pump, a sump pump, a water softener) is not described by a threshold on its power but by its rhythm: how
// long it rests and at what level, how often it works, how long each time lasts. The pattern is learned from the meter's own recent minutes, never typed
// in, and a rule only says which departures matter.
package alerts

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/runs"
)

const (
	// MinRuns is how many runs a meter needs in the days learned from before a pattern is claimed: under it nothing can be said about "usual".
	MinRuns = 4
	// DefaultMargin is how far past the usual a departure has to go to count: the longest wait seen, with a quarter more.
	DefaultMargin = 1.25
	// A burst may dip for a few minutes without ending, and even one minute is a run: a pump that works for a minute is the thing being watched.
	detectGapMinutes = 5
	detectMinMinutes = 1
	// Recent is the stretch not learned from, so that a pattern gone wrong is not learned as the usual one.
	Recent = 24 * time.Hour
	// restWindow and minRestMinutes: the resting level is judged from the last few hours, and only with an hour of them recorded.
	restWindow     = 3 * time.Hour
	minRestMinutes = 60
	// minSpike is the least rise (watts) over the resting level that is a burst at all.
	minSpike = 3.0
)

// Pattern is a meter's usual behaviour.
type Pattern struct {
	// Level is the power above which a minute counts as working, judged by the minute's highest reading so that a burst of seconds is seen.
	Level float64
	// RestWatts is the middle of what it draws below that level.
	RestWatts float64
	// Runs is how many runs were learned from, over Span.
	Runs int
	Span time.Duration
	// GapMedian and GapMax are the time from the start of one run to the start of the next, the middle and the longest.
	GapMedian time.Duration
	GapMax    time.Duration
	// RunMedianMinutes and RunMaxMinutes are how long a run lasts; PeakMedianWatts how high it reaches.
	RunMedianMinutes int
	RunMaxMinutes    int
	PeakMedianWatts  float64
	// MostPerDay is the most runs that started within any 24 hours.
	MostPerDay int
	// LastStart and LastEnd are the latest run, which may be in the recent stretch; zero with none.
	LastStart time.Time
	LastEnd   time.Time
	// Ok is whether there is a pattern to hold the meter to; Why says what is missing when there is not ("no_spikes", "too_few_runs", "no_data").
	Ok  bool
	Why string
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	i := p * float64(len(sorted)-1)
	lo, hi := int(math.Floor(i)), int(math.Ceil(i))
	return sorted[lo] + (sorted[hi]-sorted[lo])*(i-float64(lo))
}

// bursts is points with each minute's highest reading standing in for its average, so a run is found by the burst, not by the minute's average.
func bursts(points []runs.Point) []runs.Point {
	out := make([]runs.Point, len(points))
	for i, p := range points {
		p.AvgW = p.MaxW
		out[i] = p
	}
	return out
}

func recorded(points []runs.Point) []runs.Point {
	out := make([]runs.Point, 0, len(points))
	for _, p := range points {
		if p.Seconds > 0 {
			out = append(out, p)
		}
	}
	return out
}

// SuggestLevel picks the level above which a minute is a burst, from the minutes themselves: a third of the way from the resting level up to the highest
// readings (all but the top 0.05%, so one stray reading is not taken for the usual burst). False when the meter does not rise at least a few watts: it has
// no bursts to watch.
func SuggestLevel(points []runs.Point) (float64, bool) {
	pts := recorded(points)
	if len(pts) < runs.MinLevelMinutes {
		return 0, false
	}
	avg := make([]float64, len(pts))
	max := make([]float64, len(pts))
	for i, p := range pts {
		avg[i], max[i] = p.AvgW, p.MaxW
	}
	sort.Float64s(avg)
	sort.Float64s(max)
	rest, peak := percentile(avg, 0.5), percentile(max, 0.9995)
	if peak-rest < minSpike {
		return 0, false
	}
	return rest + (peak-rest)/3, true
}

// Learn works out a meter's pattern from its minutes (in time order) as of `now`: the runs above `level`, learned from everything before the recent stretch.
// The latest run is taken from all of it, so how long ago it was is known.
func Learn(points []runs.Point, level float64, now time.Time) Pattern {
	pts := recorded(points)
	p := Pattern{Level: level}
	if len(pts) < runs.MinLevelMinutes {
		p.Why = "no_data"
		return p
	}
	cut := now.Add(-Recent)
	var base []runs.Point
	for _, x := range pts {
		if x.Minute.Before(cut) {
			base = append(base, x)
		}
	}
	if len(base) < runs.MinLevelMinutes {
		p.Why = "no_data"
		return p
	}
	var rest []float64
	for _, x := range base {
		if x.MaxW < level {
			rest = append(rest, x.AvgW)
		}
	}
	sort.Float64s(rest)
	p.RestWatts = percentile(rest, 0.5)
	p.Span = base[len(base)-1].Minute.Sub(base[0].Minute)

	found := runs.Detect(bursts(base), level, detectGapMinutes, detectMinMinutes)
	p.Runs = len(found)
	if all := runs.Detect(bursts(pts), level, detectGapMinutes, detectMinMinutes); len(all) > 0 {
		p.LastStart, p.LastEnd = all[len(all)-1].Start, all[len(all)-1].End
	}
	if len(found) == 0 {
		p.Why = "no_spikes"
		return p
	}
	if len(found) < MinRuns {
		p.Why = "too_few_runs"
		return p
	}
	var gaps, lengths, peaks []float64
	starts := make([]time.Time, 0, len(found))
	for i, r := range found {
		lengths = append(lengths, float64(r.Minutes))
		peaks = append(peaks, r.PeakWatts)
		starts = append(starts, r.Start)
		if i > 0 {
			gaps = append(gaps, r.Start.Sub(found[i-1].Start).Seconds())
		}
	}
	sort.Float64s(gaps)
	sort.Float64s(lengths)
	sort.Float64s(peaks)
	p.GapMedian = time.Duration(percentile(gaps, 0.5) * float64(time.Second))
	p.GapMax = time.Duration(gaps[len(gaps)-1] * float64(time.Second))
	p.RunMedianMinutes = int(math.Round(percentile(lengths, 0.5)))
	p.RunMaxMinutes = int(lengths[len(lengths)-1])
	p.PeakMedianWatts = percentile(peaks, 0.5)
	p.MostPerDay = mostWithin(starts, 24*time.Hour)
	p.Ok = true
	return p
}

// mostWithin is the most of the (sorted) times that fall in any window of the given length.
func mostWithin(times []time.Time, window time.Duration) int {
	best, lo := 0, 0
	for hi := range times {
		for times[hi].Sub(times[lo]) >= window {
			lo++
		}
		if n := hi - lo + 1; n > best {
			best = n
		}
	}
	return best
}

// Watch is which departures from the pattern are watched.
type Watch struct {
	// Overdue: it has not run for longer than it ever has between runs.
	Overdue bool `json:"overdue"`
	// TooOften: it ran more times in the last day than it ever has in a day.
	TooOften bool `json:"too_often"`
	// TooLong: a run is lasting much longer than a run ever has.
	TooLong bool `json:"too_long"`
	// Rest: what it draws between runs has moved well away from the usual.
	Rest bool `json:"rest"`
	// Margin is how much past the usual counts (1.25: a quarter more than the longest wait seen); 0 is the default.
	Margin float64 `json:"margin"`
}

// Issue is one departure from the pattern.
type Issue struct {
	// Code is "overdue", "too_often", "too_long", "rest_high" or "rest_low".
	Code   string
	Title  string
	Detail string
}

// Humane says a length of time the way it is read: "12 min", "5.5 hours", "2.7 days".
func Humane(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(math.Round(d.Minutes())))
	case d < 48*time.Hour:
		return fmt.Sprintf("%.1f hours", d.Hours())
	}
	return fmt.Sprintf("%.1f days", d.Hours()/24)
}

// Check says how what the meter is doing now departs from its pattern. `points` are its recent minutes (at least the last day, in time order); `name` is how the
// meter is called in the sentence. Nothing is said without a pattern.
func Check(p Pattern, points []runs.Point, now time.Time, w Watch, name string) []Issue {
	if !p.Ok {
		return nil
	}
	margin := w.Margin
	if margin < 1 {
		margin = DefaultMargin
	}
	pts := recorded(points)
	var out []Issue
	var recent []runs.Point
	for _, x := range pts {
		if !x.Minute.Before(now.Add(-Recent)) {
			recent = append(recent, x)
		}
	}
	found := runs.Detect(bursts(recent), p.Level, detectGapMinutes, detectMinMinutes)
	working := false
	if n := len(found); n > 0 && now.Sub(found[n-1].End) <= 3*time.Minute {
		working = true
	}

	if w.Overdue && !working && !p.LastEnd.IsZero() {
		since := now.Sub(p.LastEnd)
		limit := time.Duration(float64(p.GapMax) * margin)
		// A wait with the meter not recorded says nothing about whether it ran: a device that was away looks the same as a pump that did not work.
		have := 0
		for _, x := range pts {
			if x.Minute.After(p.LastEnd) {
				have++
			}
		}
		if since > limit && float64(have) >= 0.8*since.Minutes() {
			out = append(out, Issue{Code: "overdue", Title: fmt.Sprintf("%s has not run for %s", name, Humane(since)),
				Detail: fmt.Sprintf("It usually runs every %s; the longest wait in the days before was %s.", Humane(p.GapMedian), Humane(p.GapMax))})
		}
	}
	if w.TooOften {
		limit := int(math.Max(math.Ceil(float64(p.MostPerDay)*margin), float64(p.MostPerDay)+1))
		since := now.Add(-24 * time.Hour)
		n := 0
		for _, r := range found {
			if !r.Start.Before(since) {
				n++
			}
		}
		if n > limit {
			out = append(out, Issue{Code: "too_often", Title: fmt.Sprintf("%s ran %d times in the last day", name, n),
				Detail: fmt.Sprintf("The most in a day before was %d.", p.MostPerDay)})
		}
	}
	if w.TooLong {
		limit := math.Max(float64(p.RunMaxMinutes)*2, float64(p.RunMaxMinutes)+10)
		if n := len(found); n > 0 && working && float64(found[n-1].Minutes) > limit {
			out = append(out, Issue{Code: "too_long", Title: fmt.Sprintf("%s has been running for %d min", name, found[n-1].Minutes),
				Detail: fmt.Sprintf("A run usually lasts about %d min, at most %d.", p.RunMedianMinutes, p.RunMaxMinutes)})
		}
	}
	if w.Rest && !working {
		var rest []float64
		for _, x := range pts {
			if !x.Minute.Before(now.Add(-restWindow)) && x.MaxW < p.Level {
				rest = append(rest, x.AvgW)
			}
		}
		if len(rest) >= minRestMinutes {
			sort.Float64s(rest)
			level := percentile(rest, 0.5)
			high, low := p.RestWatts*2+1, p.RestWatts*0.4
			switch {
			case level > high:
				out = append(out, Issue{Code: "rest_high", Title: fmt.Sprintf("%s is drawing more between runs", name),
					Detail: fmt.Sprintf("%.1f W for the last %d hours; it usually rests at %.1f W.", level, int(restWindow.Hours()), p.RestWatts)})
			case p.RestWatts >= 1 && level < low:
				out = append(out, Issue{Code: "rest_low", Title: fmt.Sprintf("%s is drawing less between runs", name),
					Detail: fmt.Sprintf("%.1f W for the last %d hours; it usually rests at %.1f W.", level, int(restWindow.Hours()), p.RestWatts)})
			}
		}
	}
	return out
}
