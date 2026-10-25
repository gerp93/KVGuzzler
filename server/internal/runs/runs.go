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
}

// Run is one stretch of the meter working.
type Run struct {
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"` // the end of the last working minute
	Minutes   int       `json:"minutes"`
	KWh       float64   `json:"kwh"`
	AvgWatts  float64   `json:"avg_watts"`
	PeakWatts float64   `json:"peak_watts"`
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

// Threshold picks the level above which a minute counts as working: a quarter of the way from the meter's resting power (its 10th percentile) up to
// its working power (its 95th), never below 10 W. A meter that barely changes has no such level: +Inf, so nothing in it is a run.
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
	base, peak := percentile(vals, 0.10), percentile(vals, 0.95)
	if peak-base < minSpread {
		return math.Inf(1)
	}
	return math.Max(10, base+0.25*(peak-base))
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
		var ws float64
		for _, p := range points[s.first : s.last+1] {
			ws += p.AvgW * p.Seconds
			r.PeakWatts = math.Max(r.PeakWatts, p.MaxW)
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
