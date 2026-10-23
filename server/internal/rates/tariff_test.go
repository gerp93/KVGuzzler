package rates

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

// These are the cases of TariffAndProfileTests in the C# test project, one for one (the tariff is built here instead of parsed from OpenEI's
// answer: that parsing is tested in the openei package).

// tou is 8 cents always, except 20 cents from 5pm to 8pm on weekdays.
func tou() *Tariff {
	grid := make([]float64, GridSize)
	for m := 0; m < 12; m++ {
		for h := 0; h < 24; h++ {
			grid[GridIndex(m, true, h)] = 0.08
			grid[GridIndex(m, false, h)] = 0.08
			if h >= 17 && h < 20 {
				grid[GridIndex(m, false, h)] = 0.20
			}
		}
	}
	return &Tariff{Label: "tou-1", Utility: "Example Electric", Name: "Residential Time of Use", FetchedAt: time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC), Grid: grid, Periods: 2, Tiers: 2}
}

// within is whether two numbers agree to a nanounit.
func within(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestATariffSurvivesBeingSavedAsTextAndADamagedOneIsNothing(t *testing.T) {
	orig := tou()
	text, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	back := ParseTariff(string(text))
	if back == nil || back.Label != orig.Label || len(back.Grid) != GridSize || back.Grid[GridIndex(5, false, 18)] != 0.20 {
		t.Fatalf("back = %+v", back)
	}
	for name, bad := range map[string]string{
		"empty":     "",
		"not json":  "not json",
		"too short": `{"label":"x","utility":"u","name":"n","fetched_at":"2026-01-01T00:00:00Z","grid":[1,2,3],"periods":1,"tiers":1}`,
	} {
		if ParseTariff(bad) != nil {
			t.Errorf("%s: want nil", name)
		}
	}
}

func TestTheProfileIsTheShareOfUsageInEachHourSeparatelyForWeekdaysAndWeekends(t *testing.T) {
	zone := time.FixedZone("test", -6*3600)
	// Monday 19 Oct 2026, 18:00 and 08:00 local; Saturday 24 Oct 12:00 local.
	at := func(day, hour int) int64 { return time.Date(2026, 10, day, hour, 0, 0, 0, zone).Unix() }
	p := ProfileFromHourly([]HourEnergy{{at(19, 18), 3}, {at(19, 8), 1}, {at(24, 12), 2}}, zone)

	if !within(p.Weekday[18], 0.75) || !within(p.Weekday[8], 0.25) || !within(p.Weekend[12], 1.0) {
		t.Errorf("profile = %+v", p)
	}
	var sum float64
	for _, w := range p.Weekday {
		sum += w
	}
	if !within(sum, 1) {
		t.Errorf("weekday shares add to %v", sum)
	}
}

func TestNoUsageMeansAnEvenDay(t *testing.T) {
	p := ProfileFromHourly(nil, time.UTC)
	for _, w := range append(append([]float64(nil), p.Weekday...), p.Weekend...) {
		if !within(w, 1.0/24) {
			t.Fatalf("share = %v", w)
		}
	}
	text, _ := json.Marshal(p)
	if ParseProfile(string(text)) == nil {
		t.Error("a profile should survive being saved")
	}
	if ParseProfile(`{"weekday":[1],"weekend":[1]}`) != nil {
		t.Error("a profile that is not 24 hours should be nothing")
	}
	// Nonsense usage is ignored, not allowed to poison the shares.
	q := ProfileFromHourly([]HourEnergy{{time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC).Unix(), math.NaN()}, {time.Date(2026, 10, 19, 13, 0, 0, 0, time.UTC).Unix(), -4}}, time.UTC)
	if !within(q.Weekday[12], 1.0/24) {
		t.Errorf("share = %v", q.Weekday[12])
	}
}

var (
	monday   = MustDate("2026-10-19")
	saturday = MustDate("2026-10-24")
)

func hourProfile(hour int) *UsageProfile {
	weekday := make([]float64, 24)
	weekday[hour] = 1
	u := UniformProfile()
	return &UsageProfile{Weekday: weekday, Weekend: u.Weekend}
}

// priceIs: a looked-up price is built from floats, so it is compared to the nanounit rather than exactly.
func priceIs(t *testing.T, got *Money, want float64) {
	t.Helper()
	if got == nil || math.Abs(got.Float()-want) > 1e-9 {
		t.Errorf("price = %v, want %v", got, want)
	}
}

func TestADaysPriceIsTheTariffWeightedByWhenThePowerIsUsed(t *testing.T) {
	look := func(p *UsageProfile) *Schedule {
		return NewSchedule(ModeLookup, nil, nil, time.UTC).WithTariff(tou(), p)
	}
	priceIs(t, look(hourProfile(18)).RateAt(monday), 0.20)
	priceIs(t, look(hourProfile(3)).RateAt(monday), 0.08)
	// An even day: 3 peak hours at 20 cents, 21 off-peak at 8 cents.
	got := look(nil).RateAt(monday)
	if got == nil || math.Abs(got.Float()-(3*0.20+21*0.08)/24) > 1e-9 {
		t.Errorf("even day = %v", got)
	}
	// A weekend has no peak at all.
	priceIs(t, look(hourProfile(18)).RateAt(saturday), 0.08)
}

func TestCostsFollowTheLookedUpPriceDayByDay(t *testing.T) {
	s := NewSchedule(ModeLookup, nil, nil, time.UTC).WithTariff(tou(), hourProfile(18))
	from := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)                                                       // Friday and Saturday
	cost := s.Cost(func(a, b time.Time) float64 { return b.Sub(a).Hours() / 24 }, from, from.Add(48*time.Hour)) // 1 kWh a day
	if math.Abs(cost.Cost.Float()-(0.20+0.08)) > 1e-9 || cost.Unpriced {
		t.Errorf("cost = %v unpriced %v", cost.Cost, cost.Unpriced)
	}
}

func TestLookupWithoutATariffFallsBackOnTheSingleRateAndOtherModesIgnoreAnyTariff(t *testing.T) {
	flat := MustMoney("0.15")
	rateIs(t, NewSchedule(ModeLookup, &flat, nil, time.UTC).RateAt(monday), "0.15")
	if NewSchedule(ModeLookup, nil, nil, time.UTC).RateAt(monday) != nil {
		t.Error("no tariff and no single rate should have no price")
	}
	rateIs(t, NewSchedule(ModeOne, &flat, nil, time.UTC).WithTariff(tou(), nil).RateAt(monday), "0.15")
}

func TestTheSummaryNamesWhoWhatAndWhatItCosts(t *testing.T) {
	if got := tou().Summary("$"); got != "Example Electric: Residential Time of Use · $0.0800 to $0.2000 per kWh, 2 time-of-use periods" {
		t.Errorf("summary = %q", got)
	}
}
