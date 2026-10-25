package anomaly

import (
	"math"
	"testing"

	"github.com/gerp93/KVGuzzler/server/internal/rates"
)

// 2026-10-05 is a Monday.
var start = rates.MustDate("2026-10-05")

// run makes `n` days from start with kwh(i) for day i.
func run(n int, kwh func(i int, d rates.Date) float64) []Day {
	out := make([]Day, n)
	for i := range out {
		d := start.AddDays(i)
		out[i] = Day{Date: d, KWh: kwh(i, d)}
	}
	return out
}

func TestADayFarAboveTheUsualIsFoundWithHowMuchMore(t *testing.T) {
	days := run(22, func(i int, d rates.Date) float64 {
		if i == 20 {
			return 30 // a Sunday-ish spike: the whole day 3x
		}
		return 10 + float64(i%3)*0.2
	})
	got := Detect(days, start.AddDays(14), start.AddDays(22), 0.5)
	if len(got) != 1 || got[0].Date != start.AddDays(20) {
		t.Fatalf("found %+v, want only day 20", got)
	}
	f := got[0]
	if f.KWh != 30 || math.Abs(f.Usual-10.2) > 0.3 || math.Abs(f.Extra-(30-f.Usual)) > 1e-9 || f.Ratio < 2.5 {
		t.Errorf("finding = %+v", f)
	}
}

func TestAWeekendThatAlwaysUsesMoreIsNotUnusualOnAWeekend(t *testing.T) {
	days := run(28, func(i int, d rates.Date) float64 {
		if weekend(d) {
			return 25
		}
		return 10
	})
	if got := Detect(days, start.AddDays(14), start.AddDays(28), 0.5); len(got) != 0 {
		t.Errorf("a steady weekly pattern is not unusual: %+v", got)
	}
}

func TestANoisyMetersOrdinarySwingsAreNotReported(t *testing.T) {
	vals := []float64{2, 9, 4, 12, 3, 10, 5, 8, 2, 11, 4, 9, 3, 12, 5, 7, 2, 10, 4, 9, 3, 11, 5, 8, 2, 12, 4, 9}
	days := run(28, func(i int, d rates.Date) float64 { return vals[i] })
	if got := Detect(days, start.AddDays(14), start.AddDays(28), 0.5); len(got) != 0 {
		t.Errorf("swings between 2 and 12 are this meter's ordinary: %+v", got)
	}
}

func TestSmallExtrasAndTooLittleHistoryAreNotReported(t *testing.T) {
	// 0.2 kWh a day and then 0.6: three times the usual, but less than half a kWh more.
	tiny := run(22, func(i int, d rates.Date) float64 {
		if i == 20 {
			return 0.6
		}
		return 0.2
	})
	if got := Detect(tiny, start.AddDays(14), start.AddDays(22), 0.5); len(got) != 0 {
		t.Errorf("a small extra: %+v", got)
	}
	// Three days of history is not enough to say what is usual.
	few := run(4, func(i int, d rates.Date) float64 { return []float64{10, 10, 10, 40}[i] })
	if got := Detect(few, start, start.AddDays(4), 0.5); len(got) != 0 {
		t.Errorf("with 3 earlier days: %+v", got)
	}
}

func TestTodayIsNeverJudgedAndDaysWithNoDataAreSkipped(t *testing.T) {
	days := run(22, func(i int, d rates.Date) float64 {
		if i == 21 {
			return 99
		}
		return 10
	})
	if got := Detect(days, start.AddDays(14), start.AddDays(21), 0.5); len(got) != 0 {
		t.Errorf("day 21 is today and still going: %+v", got)
	}
	days = append(days[:20], days[21:]...) // no data for day 20
	if got := Detect(days, start.AddDays(14), start.AddDays(22), 0.5); len(got) != 0 && got[0].Date == start.AddDays(20) {
		t.Errorf("a day with no data was judged: %+v", got)
	}
}

func TestTheBiggestExtraComesFirst(t *testing.T) {
	days := run(26, func(i int, d rates.Date) float64 {
		switch i {
		case 18:
			return 25
		case 23:
			return 45
		}
		return 10
	})
	got := Detect(days, start.AddDays(14), start.AddDays(26), 0.5)
	if len(got) != 2 || got[0].Date != start.AddDays(23) || got[1].Date != start.AddDays(18) {
		t.Errorf("found %+v, want day 23 then day 18", got)
	}
}
