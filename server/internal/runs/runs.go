// Package runs finds the times a meter was working: a car charging, a dryer drying, an air conditioner running. It looks only at the
// minutes the server already keeps, with no device and no clock, so it can be tested on made-up minutes.
//
// A run is a stretch where the meter drew clearly more than it does at rest. A short dip inside a run (a heating element switching off and
// on) does not end it, and a blip of a minute or two is not a run.
package runs

import (
	"math"
	"sort"
	"time"
)

// Point is one recorded minute of one meter.
type Point struct {
	Minute  time.Time
	AvgW    float64
	MaxW    float64
	Seconds float64
	// AvgAmps and MaxAmps are the current of the minute, when the meter reports one (HaveAmps).
	AvgAmps, MaxAmps float64
	HaveAmps         bool
}

// Run is one stretch of the meter working.
type Run struct {
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"` // the end of the last working minute
	Minutes   int       `json:"minutes"`
	KWh       float64   `json:"kwh"`
	AvgWatts  float64   `json:"avg_watts"`
	PeakWatts float64   `json:"peak_watts"`
	// AvgAmps is the current over the run, weighted by the time it was reported; PeakAmps the highest minute's. Null when the meter reports none.
	AvgAmps  *float64 `json:"avg_amps"`
	PeakAmps *float64 `json:"peak_amps"`
}

// Defaults for Detect.
const (
	// DefaultGapMinutes is how long a dip may last without ending a run.
	DefaultGapMinutes = 5
	// DefaultMinMinutes is the shortest stretch that counts as a run.
	DefaultMinMinutes = 3
	// minSpread: a meter whose busiest and quietest minutes differ by less than this (in watts) never "runs": it is just on.
	minSpread = 15.0
)

// percentile is the p-th percentile (0 to 1) of sorted values.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	i := p * float64(len(sorted)-1)
	lo := int(math.Floor(i))
	hi := int(math.Ceil(i))
	return sorted[lo] + (sorted[hi]-sorted[lo])*(i-float64(lo))
}

// Threshold picks the level above which a minute counts as working: a quarter of the way from the meter's resting power (its median: what it draws
// for most of the time) up to its working power (its 99th percentile, so a meter that works only a few percent of the time, a water heater, still has one), never below 10 W.
// The median, not a low percentile: a fridge that cycles between 15 W and 74 W spends more than half its time at 74 W, which is its normal, not work.
// A meter that barely changes from its usual draw has no such level: +Inf, so nothing in it is a run.
func Threshold(points []Point) float64 {
	vals := make([]float64, 0, len(points))
	for _, p := range points {
		if p.Seconds > 0 {
			vals = append(vals, p.AvgW)
		}
	}
	if len(vals) == 0 {
		return math.Inf(1)
	}
	sort.Float64s(vals)
	base, peak := percentile(vals, 0.5), percentile(vals, 0.99)
	if peak-base < minSpread {
		return math.Inf(1)
	}
	return math.Max(10, base+0.25*(peak-base))
}

// Levels is what a meter does at rest and the level above which it is working, for showing at a glance whether it is working right now.
type Levels struct {
	// RestWatts is the middle of what it draws while resting: the median of its minutes below the working level (all of its minutes when it has none).
	RestWatts float64
	// WorkingAboveWatts is the working level (Threshold); only meaningful when HasWorking.
	WorkingAboveWatts float64
	// HasWorking is false for a meter that barely changes: it is on, it never works, so it is always resting.
	HasWorking bool
	// Minutes is how many recorded minutes the levels were learned from.
	Minutes int
}

// MinLevelMinutes is the least history that says what a meter's resting is: under it, no level is claimed.
const MinLevelMinutes = 120

// LevelsOf learns a meter's levels from its minutes. False when there are too few recorded minutes to say.
func LevelsOf(points []Point) (Levels, bool) {
	var vals []float64
	for _, p := range points {
		if p.Seconds > 0 {
			vals = append(vals, p.AvgW)
		}
	}
	if len(vals) < MinLevelMinutes {
		return Levels{}, false
	}
	l := Levels{Minutes: len(vals)}
	threshold := Threshold(points)
	var resting []float64
	if math.IsInf(threshold, 1) {
		resting = vals
	} else {
		l.HasWorking, l.WorkingAboveWatts = true, threshold
		for _, v := range vals {
			if v < threshold {
				resting = append(resting, v)
			}
		}
	}
	sort.Float64s(resting)
	l.RestWatts = percentile(resting, 0.5)
	return l, true
}

// Detect finds the runs in points (in time order): stretches whose minutes reach threshold, joined across dips of up to gapMinutes, and at least
// minMinutes long. Each run's energy is every recorded minute from its first to its last working minute, the dips inside it included.
func Detect(points []Point, threshold float64, gapMinutes, minMinutes int) []Run {
	if math.IsInf(threshold, 1) || len(points) == 0 {
		return nil
	}
	type span struct{ first, last int }
	var spans []span
	cur := -1 // index into spans, or -1
	for i, p := range points {
		if p.Seconds <= 0 || p.AvgW < threshold {
			continue
		}
		if cur >= 0 {
			// Minutes between this working minute and the last one that worked.
			between := int(p.Minute.Sub(points[spans[cur].last].Minute)/time.Minute) - 1
			if between <= gapMinutes {
				spans[cur].last = i
				continue
			}
		}
		spans = append(spans, span{i, i})
		cur = len(spans) - 1
	}

	var out []Run
	for _, s := range spans {
		r := Run{Start: points[s.first].Minute}
		var ws, aWs, aSecs, aPeak float64
		for _, p := range points[s.first : s.last+1] {
			ws += p.AvgW * p.Seconds
			r.PeakWatts = math.Max(r.PeakWatts, p.MaxW)
			if p.HaveAmps && p.Seconds > 0 {
				aWs += p.AvgAmps * p.Seconds
				aSecs += p.Seconds
				aPeak = math.Max(aPeak, p.MaxAmps)
			}
		}
		if aSecs > 0 {
			avg, peak := aWs/aSecs, aPeak
			r.AvgAmps, r.PeakAmps = &avg, &peak
		}
		last := points[s.last].Minute
		r.End = last.Add(time.Minute)
		r.Minutes = int(last.Sub(r.Start)/time.Minute) + 1
		if r.Minutes < minMinutes {
			continue
		}
		r.KWh = ws / 3.6e6
		r.AvgWatts = r.KWh * 1000 / (float64(r.Minutes) / 60)
		out = append(out, r)
	}
	return out
}
