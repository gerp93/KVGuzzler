package carbon

import "sort"

// Stretch is one stretch (an hour, a day) of a carbon series: its energy, what that came to in kilograms, and the grid's intensity in it.
type Stretch struct {
	Start        int64 // seconds since the epoch
	KWh          float64
	Kg           float64
	EstimatedKWh float64
	// GridGrams is the average of the intensities saved for the hours of the stretch, nil when none of them has one.
	GridGrams *float64
}

// Bucketed is Value kept apart by stretch: each hour's kWh at that hour's intensity (an hour without one at the period's average, or everAverage when no
// hour in the period has one), added up under startOf(hour), the start of the stretch the hour belongs to. An hour that has an intensity but used no energy
// still makes its stretch, so the grid's line is drawn where nothing was recorded. The stretches add up to exactly what Value gives for the same hours.
func Bucketed(known map[int64]float64, everAverage float64, hours []HourKWh, startOf func(hour int64) int64) []Stretch {
	periodAverage := everAverage
	if len(known) > 0 {
		var sum float64
		for _, g := range known {
			sum += g
		}
		periodAverage = sum / float64(len(known))
	}
	type acc struct {
		Stretch
		gSum float64
		gN   int
	}
	by := map[int64]*acc{}
	at := func(hour int64) *acc {
		start := startOf(hour)
		a := by[start]
		if a == nil {
			a = &acc{Stretch: Stretch{Start: start}}
			by[start] = a
		}
		return a
	}
	for _, h := range hours {
		a := at(h.Hour)
		grams, own := known[h.Hour]
		if !own {
			grams = periodAverage
			a.EstimatedKWh += h.KWh
		}
		a.Kg += h.KWh * grams / 1000
		a.KWh += h.KWh
	}
	for hour, g := range known {
		a := at(hour)
		a.gSum += g
		a.gN++
	}
	out := make([]Stretch, 0, len(by))
	for _, a := range by {
		if a.gN > 0 {
			avg := a.gSum / float64(a.gN)
			a.GridGrams = &avg
		}
		out = append(out, a.Stretch)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}
