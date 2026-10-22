package rates

// These are the cases of tests/KVGuzzler.Tests/BillingTests.cs (BillTests and RateScheduleTests), ported one for one,
// so the Go server and the desktop app price the same way. If a rule changes, change it in both.

import (
	"math"
	"strings"
	"testing"
	"time"
)

func bill(start, end, total string, kwh float64, fees ...FixedFee) Bill {
	return Bill{Start: MustDate(start), End: MustDate(end), Total: MustMoney(total), Kwh: kwh, FixedFees: fees}
}

func fee(label, amount string) FixedFee { return FixedFee{Label: label, Amount: MustMoney(amount)} }

func near(t *testing.T, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("got %v, want %v (±%v)", got, want, tol)
	}
}

func wantMoney(t *testing.T, got Money, want string) {
	t.Helper()
	if !got.Equal(MustMoney(want)) {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestWithNoFeesThePriceIsTheTotalOverTheKwh(t *testing.T) {
	b := bill("2026-08-20", "2026-09-21", "206.32", 1036)
	near(t, b.PricePerKwh().Float(), 0.19915, 5e-6) // the all-in price from the real bill
	if b.Days() != 33 {
		t.Errorf("days = %d, want 33", b.Days())
	}
}

func TestFlatFeesAreLeftOutEvenCreditsWhichAreNegative(t *testing.T) {
	// $6.00 basic charge and a $10.00 credit: -4.00 of fixed charges, so $210.32 is for the electricity.
	b := bill("2026-08-20", "2026-09-21", "206.32", 1036, fee("Basic service", "6.00"), fee("Res Savers credit", "-10.00"))
	wantMoney(t, b.FixedTotal(), "-4.00")
	wantMoney(t, b.Variable(), "210.32")
	near(t, b.PricePerKwh().Float(), 0.20301, 5e-6)
}

func TestZeroOrManyFeesAreFine(t *testing.T) {
	wantMoney(t, bill("2026-01-01", "2026-01-31", "100", 500).FixedTotal(), "0")
	many := bill("2026-01-01", "2026-01-31", "100", 500, fee("a", "1"), fee("b", "2"), fee("c", "3"), fee("d", "-1"))
	wantMoney(t, many.FixedTotal(), "5")
	wantMoney(t, many.Variable(), "95")
}

func TestASingleDayBillCoversOneDay(t *testing.T) {
	if d := bill("2026-01-05", "2026-01-05", "5", 10).Days(); d != 1 {
		t.Errorf("days = %d, want 1", d)
	}
}

func TestAGoodBillIsAccepted(t *testing.T) {
	if err := bill("2026-08-20", "2026-09-21", "206.32", 1036).ValidateAgainst(nil); err != nil {
		t.Error(err)
	}
}

func TestDatesInTheWrongOrderOrTooLongAreRefused(t *testing.T) {
	for _, c := range []struct{ start, end, expected string }{
		{"2026-09-21", "2026-08-20", "before the start"}, // wrong order
		{"2025-01-01", "2026-06-01", "more than a year"},
	} {
		err := bill(c.start, c.end, "100", 500).ValidateAgainst(nil)
		if err == nil || !strings.Contains(err.Error(), c.expected) {
			t.Errorf("%s to %s: err = %v, want one containing %q", c.start, c.end, err, c.expected)
		}
	}
}

func TestATotalOrUsageThatIsNotPositiveIsRefused(t *testing.T) {
	for _, c := range []struct {
		total string
		kwh   float64
	}{{"100", 0}, {"100", -5}, {"100", math.NaN()}, {"100", math.Inf(1)}, {"0", 500}, {"-3", 500}} {
		if err := bill("2026-01-01", "2026-01-31", c.total, c.kwh).ValidateAgainst(nil); err == nil {
			t.Errorf("total %s, kwh %v was accepted", c.total, c.kwh)
		}
	}
}

func TestFeesThatSwallowTheWholeTotalAreRefused(t *testing.T) {
	err := bill("2026-01-01", "2026-01-31", "100", 500, fee("everything", "100")).ValidateAgainst(nil)
	if err == nil || !strings.Contains(err.Error(), "nothing is left to price") {
		t.Errorf("err = %v", err)
	}
}

func TestATooLongFeeNameIsRefused(t *testing.T) {
	err := bill("2026-01-01", "2026-01-31", "100", 500, fee(strings.Repeat("x", 61), "1")).ValidateAgainst(nil)
	if err == nil || !strings.Contains(err.Error(), "too long") {
		t.Errorf("err = %v", err)
	}
}

func TestTwoBillsClaimingTheSameDayIsRefused(t *testing.T) {
	existing := []Bill{bill("2026-08-20", "2026-09-21", "206.32", 1036)}
	for _, c := range []struct{ start, end string }{
		{"2026-09-21", "2026-10-23"}, // shares 9/21
		{"2026-08-01", "2026-08-20"}, // shares 8/20
		{"2026-09-01", "2026-09-05"}, // inside it
	} {
		err := bill(c.start, c.end, "150", 900).ValidateAgainst(existing)
		if err == nil || !strings.Contains(err.Error(), "overlap") {
			t.Errorf("%s to %s: err = %v, want an overlap", c.start, c.end, err)
		}
	}
	if err := bill("2026-09-22", "2026-10-23", "150", 900).ValidateAgainst(existing); err != nil { // the next day is fine
		t.Error(err)
	}
}

func TestPastBillsCanBeEnteredInAnyOrderAndABillCanBeEditedWithoutClashingWithItself(t *testing.T) {
	march := bill("2026-03-01", "2026-03-31", "100", 500)
	august := bill("2026-08-01", "2026-08-31", "100", 500)

	// Entering February after August and March is fine.
	if err := bill("2026-02-01", "2026-02-28", "100", 500).ValidateAgainst([]Bill{march, august}); err != nil {
		t.Error(err)
	}
	// Editing March: the other bills are checked, not its own old copy.
	edited := march
	edited.Total = MustMoney("120")
	if err := edited.ValidateAgainst([]Bill{august}); err != nil {
		t.Error(err)
	}
	if err := ValidateBills([]Bill{august, edited, bill("2026-02-01", "2026-02-28", "100", 500)}); err != nil {
		t.Errorf("ValidateBills: %v", err)
	}
	if err := ValidateBills([]Bill{march, bill("2026-03-31", "2026-04-30", "100", 500)}); err == nil {
		t.Error("ValidateBills accepted two bills sharing a day")
	}
}

// Rate schedule

func ts(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s+"Z")
	if err != nil {
		panic(err)
	}
	return t
}

func ptr(s string) *Money { m := MustMoney(s); return &m }

func rateIs(t *testing.T, got *Money, want string) {
	t.Helper()
	if got == nil || !got.Equal(MustMoney(want)) {
		t.Errorf("rate = %v, want %s", got, want)
	}
}

var (
	august  = bill("2026-08-20", "2026-09-21", "200", 1000) // $0.20
	october = bill("2026-09-22", "2026-10-23", "180", 1000) // $0.18
	gapBill = bill("2026-12-01", "2026-12-31", "250", 1000) // $0.25, after a gap
)

func TestOneRateIgnoresTheBills(t *testing.T) {
	s := NewSchedule(ModeOne, ptr("0.15"), []Bill{august}, time.UTC)
	rateIs(t, s.RateAt(MustDate("2026-09-01")), "0.15")
	if s.IsEstimate(MustDate("2026-09-01")) {
		t.Error("one rate is never an estimate")
	}
}

func TestBillsModeWithNoBillsFallsBackToTheSingleRate(t *testing.T) {
	rateIs(t, NewSchedule(ModeBills, ptr("0.15"), nil, time.UTC).RateAt(MustDate("2026-09-01")), "0.15")
}

func TestNothingKnownMeansNoRate(t *testing.T) {
	if r := NewSchedule(ModeOne, nil, nil, time.UTC).RateAt(MustDate("2026-09-01")); r != nil {
		t.Errorf("rate = %v, want none", r)
	}
}

func TestADayInsideABillUsesThatBillsPrice(t *testing.T) {
	s := NewSchedule(ModeBills, ptr("0.15"), []Bill{october, august}, time.UTC) // given out of order
	rateIs(t, s.RateAt(MustDate("2026-08-20")), "0.20")                         // first day
	rateIs(t, s.RateAt(MustDate("2026-09-21")), "0.20")                         // last day
	rateIs(t, s.RateAt(MustDate("2026-09-22")), "0.18")                         // the next bill starts
	if s.IsEstimate(MustDate("2026-10-01")) {
		t.Error("a day inside a bill is not an estimate")
	}
}

func TestAfterTheLatestBillAndInGapsTheMostRecentPriceIsAnEstimate(t *testing.T) {
	s := NewSchedule(ModeBills, ptr("0.15"), []Bill{august, october, gapBill}, time.UTC)
	rateIs(t, s.RateAt(MustDate("2026-11-15")), "0.18") // in the gap after October: October's price
	if !s.IsEstimate(MustDate("2026-11-15")) {
		t.Error("a day in a gap should be an estimate")
	}
	rateIs(t, s.RateAt(MustDate("2027-02-01")), "0.25") // after the latest bill
	if !s.IsEstimate(MustDate("2027-02-01")) {
		t.Error("a day after the latest bill should be an estimate")
	}
}

func TestBeforeTheFirstBillTheSingleRateIsUsedOrTheFirstBillsPriceIfThereIsNone(t *testing.T) {
	rateIs(t, NewSchedule(ModeBills, ptr("0.15"), []Bill{august}, time.UTC).RateAt(MustDate("2026-07-01")), "0.15")
	rateIs(t, NewSchedule(ModeBills, nil, []Bill{august}, time.UTC).RateAt(MustDate("2026-07-01")), "0.20")
	if !NewSchedule(ModeBills, ptr("0.15"), []Bill{august}, time.UTC).IsEstimate(MustDate("2026-07-01")) {
		t.Error("a day before the first bill should be an estimate")
	}
}

func TestAPeriodIsSplitAtMidnightIntoStretchesOfOnePrice(t *testing.T) {
	s := NewSchedule(ModeBills, ptr("0.15"), []Bill{august, october}, time.UTC)

	segs := s.Segments(ts("2026-09-20T12:00:00"), ts("2026-09-23T12:00:00"))

	if len(segs) != 2 {
		t.Fatalf("%d segments, want 2", len(segs))
	}
	if !segs[0].From.Equal(ts("2026-09-20T12:00:00")) || !segs[0].To.Equal(ts("2026-09-22T00:00:00")) {
		t.Errorf("first segment %v to %v", segs[0].From, segs[0].To)
	}
	rateIs(t, segs[0].Rate, "0.20")
	if !segs[1].From.Equal(ts("2026-09-22T00:00:00")) || !segs[1].To.Equal(ts("2026-09-23T12:00:00")) {
		t.Errorf("second segment %v to %v", segs[1].From, segs[1].To)
	}
	rateIs(t, segs[1].Rate, "0.18")
}

func TestMidnightFollowsTheLocalTimeZone(t *testing.T) {
	minusSix := time.FixedZone("test", -6*3600)
	s := NewSchedule(ModeBills, ptr("0.15"), []Bill{august, october}, minusSix)

	// 2026-09-22 begins at 06:00 UTC in a zone six hours behind.
	segs := s.Segments(ts("2026-09-21T12:00:00"), ts("2026-09-22T12:00:00"))

	if !segs[0].To.Equal(ts("2026-09-22T06:00:00")) {
		t.Errorf("first segment ends %v", segs[0].To)
	}
	rateIs(t, segs[0].Rate, "0.20")
	rateIs(t, segs[1].Rate, "0.18")
}

func hours(a, b time.Time) float64 { return b.Sub(a).Hours() }

func TestCostAddsEachStretchAtItsOwnPrice(t *testing.T) {
	s := NewSchedule(ModeBills, ptr("0.15"), []Bill{august, october}, time.UTC)
	// One kWh every hour: 36 hours at $0.20 then 36 hours at $0.18.
	cost := s.Cost(hours, ts("2026-09-20T12:00:00"), ts("2026-09-23T12:00:00"))

	near(t, cost.Kwh, 72, 1e-9)
	wantMoney(t, cost.Cost, "13.68") // 36 x 0.20 + 36 x 0.18, exactly
	if cost.Estimated || cost.Unpriced {
		t.Errorf("estimated=%v unpriced=%v, want neither", cost.Estimated, cost.Unpriced)
	}
}

func TestCostAfterTheLastBillIsMarkedAsAnEstimate(t *testing.T) {
	s := NewSchedule(ModeBills, ptr("0.15"), []Bill{august, october}, time.UTC)
	cost := s.Cost(hours, ts("2026-10-22T00:00:00"), ts("2026-10-26T00:00:00")) // two days in, two days past the bill
	if !cost.Estimated {
		t.Error("expected an estimate")
	}
	wantMoney(t, cost.Cost, "17.28") // 96 x 0.18: the estimate is the last bill's price
}

func TestWithNoPriceAtAllTheEnergyIsStillCountedAndFlaggedUnpriced(t *testing.T) {
	s := NewSchedule(ModeOne, nil, nil, time.UTC)
	cost := s.Cost(hours, ts("2026-10-01T00:00:00"), ts("2026-10-02T00:00:00"))
	near(t, cost.Kwh, 24, 1e-9)
	wantMoney(t, cost.Cost, "0")
	if !cost.Unpriced {
		t.Error("expected unpriced")
	}
}

func TestTheAveragePriceIsWeightedByTime(t *testing.T) {
	s := NewSchedule(ModeBills, ptr("0.15"), []Bill{august, october}, time.UTC)
	// 2 days at $0.20 (9/20 to 9/22) and 4 days at $0.18: (2 x 0.20 + 4 x 0.18) / 6
	avg := s.AverageRate(ts("2026-09-20T00:00:00"), ts("2026-09-26T00:00:00"))
	if avg == nil {
		t.Fatal("no average")
	}
	near(t, avg.Float(), (2*0.20+4*0.18)/6, 1e-9)
}

func TestABillCoversFromItsFirstMidnightToTheEndOfItsLastDay(t *testing.T) {
	s := NewSchedule(ModeBills, nil, []Bill{august}, time.UTC)
	from, to := s.Span(august)
	if !from.Equal(ts("2026-08-20T00:00:00")) || !to.Equal(ts("2026-09-22T00:00:00")) {
		t.Errorf("span %v to %v", from, to)
	}
}

// Money and JSON

func TestMoneyIsExactAndFormatsWithAtLeastTwoDecimals(t *testing.T) {
	for in, want := range map[string]string{"12": "12.00", "12.3": "12.30", "0.199155": "0.199155", "-4": "-4.00"} {
		if got := MustMoney(in).String(); got != want {
			t.Errorf("String(%q) = %q, want %q", in, got, want)
		}
	}
	// 0.1 + 0.2 is exactly 0.3 here (not in floating point).
	wantMoney(t, MustMoney("0.1").Add(MustMoney("0.2")), "0.3")
	for _, bad := range []string{"", "1,5", "abc", "1e3", "1.1234567", ".5"} {
		if _, err := ParseMoney(bad); err == nil {
			t.Errorf("ParseMoney(%q) succeeded", bad)
		}
	}
}

func TestDateParsing(t *testing.T) {
	if _, err := ParseDate("2026-02-30"); err == nil {
		t.Error("2026-02-30 is not a date")
	}
	if d := MustDate("2026-09-21").AddDays(1); d != MustDate("2026-09-22") {
		t.Errorf("AddDays = %v", d)
	}
}
