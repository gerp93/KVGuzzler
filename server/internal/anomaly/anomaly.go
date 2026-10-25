// Package anomaly finds the days a meter used far more than it usually does. It looks only at one energy figure per local day, so it can be tested on
// made-up days.
//
// "Usually" is the middle (median) of the same kind of day, a weekday or a weekend day, over the four weeks before it: a house that always uses more
// on a Saturday is not unusual on a Saturday. A day counts as unusual only when it is far above that by three measures at once, so that neither a
// quiet meter's small wobble nor a noisy meter's ordinary swings are reported.
package anomaly

import (
	"math"
	"sort"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/rates"
)

const (
	// LookbackDays is how far back the usual days are taken from.
	LookbackDays = 28
	// MinReference is the fewest usual days a verdict needs; with less, nothing is said.
	MinReference = 4
	// MinRatio: a day must be at least this many times the usual.
	MinRatio = 1.5
	// MinScore: and this many typical swings above it (a swing is the median absolute deviation, scaled to a standard deviation).
	MinScore = 3.5
	// floorShare and floorKWh keep the swing from being nearly nothing for a meter whose days are almost identical.
	floorShare = 0.1
	floorKWh   = 0.05
)

// Day is one local day's energy of one meter.
type Day struct {
	Date rates.Date
	KWh  float64
}

// Finding is one unusual day.
type Finding struct {
	Date rates.Date
	KWh  float64
	// Usual is the median of the same kind of day before it; Extra is how much more than that this day used.
	Usual, Extra float64
	// Ratio is KWh over Usual (0 when the usual is nothing).
	Ratio float64
}

func weekend(d rates.Date) bool {
	w := d.Weekday()
	return w == time.Saturday || w == time.Sunday
}

func median(sorted []float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

// Detect looks at every complete day from `from` up to but not including `today` (today is still going, so it always looks low) and returns
// the unusual ones, the biggest extra first. minExtra is the least extra energy worth saying, in kWh.
func Detect(days []Day, from, today rates.Date, minExtra float64) []Finding {
	byDate := map[rates.Date]float64{}
	for _, d := range days {
		byDate[d.Date] = d.KWh
	}
	var out []Finding
	for day := from; day.Before(today); day = day.AddDays(1) {
		kwh, ok := byDate[day]
		if !ok {
			continue
		}
		var ref []float64
		for back := 1; back <= LookbackDays; back++ {
			p := day.AddDays(-back)
			if v, ok := byDate[p]; ok && weekend(p) == weekend(day) {
				ref = append(ref, v)
			}
		}
		if len(ref) < MinReference {
			continue
		}
		sort.Float64s(ref)
		usual := median(ref)
		dev := make([]float64, len(ref))
		for i, v := range ref {
			dev[i] = math.Abs(v - usual)
		}
		sort.Float64s(dev)
		swing := math.Max(1.4826*median(dev), math.Max(floorShare*usual, floorKWh))
		extra := kwh - usual
		if extra < minExtra || (kwh-usual)/swing < MinScore || (usual > 0 && kwh/usual < MinRatio) {
			continue
		}
		f := Finding{Date: day, KWh: kwh, Usual: usual, Extra: extra}
		if usual > 0 {
			f.Ratio = kwh / usual
		}
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Extra > out[j].Extra })
	return out
}
