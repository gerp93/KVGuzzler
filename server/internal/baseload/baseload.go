// Package baseload works out the power a meter (or the house) never goes below: what it draws all day and night, however quiet the house is.
// It looks only at the minutes the server already keeps, so it can be tested on made-up minutes.
//
// The figure is the 5th percentile of the minute averages, not the lowest minute: one minute where a meter dropped out or a clamp glitched
// must not set it, and a meter that is off for a few minutes is still "always on" at the level it rests at the rest of the time.
package baseload

import (
	"math"
	"sort"
)

const (
	// Percentile is the share of minutes (0 to 1) allowed to be below the base load.
	Percentile = 0.05
	// MinMinutes is the least data that gives a figure: an hour of minutes. Less says nothing about what is "always" on.
	MinMinutes = 60
	// HoursPerYear is a year of 365.25 days.
	HoursPerYear = 8766.0
)

// Result is the base load of one meter over a period.
type Result struct {
	// Watts is the 5th percentile of the minute averages.
	Watts float64
	// AvgWatts is the average of the same minutes, so the share of use that is always on can be said.
	AvgWatts float64
	// Minutes is how many minutes it rests on.
	Minutes int
}

// Of is the base load of the given minute averages (watts, one per recorded minute); false when there are too few minutes.
func Of(watts []float64) (Result, bool) {
	if len(watts) < MinMinutes {
		return Result{}, false
	}
	vals := make([]float64, 0, len(watts))
	var sum float64
	for _, w := range watts {
		if math.IsNaN(w) || math.IsInf(w, 0) {
			continue
		}
		w = math.Max(0, w) // a reading that stayed negative after the clamp's sign was set is not power
		vals = append(vals, w)
		sum += w
	}
	if len(vals) < MinMinutes {
		return Result{}, false
	}
	sort.Float64s(vals)
	i := Percentile * float64(len(vals)-1)
	lo, hi := int(math.Floor(i)), int(math.Ceil(i))
	base := vals[lo] + (vals[hi]-vals[lo])*(i-float64(lo))
	return Result{Watts: base, AvgWatts: sum / float64(len(vals)), Minutes: len(vals)}, true
}

// Share is the part (0 to 1) of what the meter uses that is always on; 0 when it uses nothing.
func (r Result) Share() float64 {
	if r.AvgWatts <= 0 {
		return 0
	}
	return math.Min(1, r.Watts/r.AvgWatts)
}

// KWhPerYear is the energy that base load adds up to over a year.
func (r Result) KWhPerYear() float64 { return r.Watts * HoursPerYear / 1000 }
