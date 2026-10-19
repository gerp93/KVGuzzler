using KVGuzzler.Core.Billing;
using Xunit;

namespace KVGuzzler.Tests;

public class BillTests
{
    private static Bill Make(string start, string end, decimal total, double kwh, params FixedFee[] fees) =>
        new(0, DateOnly.Parse(start), DateOnly.Parse(end), total, kwh, fees);

    [Fact]
    public void WithNoFeesThePriceIsTheTotalOverTheKwh()
    {
        var bill = Make("2026-08-20", "2026-09-21", 206.32m, 1036);
        Assert.Equal(0.19915, (double)bill.PricePerKwh, precision: 5);   // the all-in price from the real bill
        Assert.Equal(33, bill.Days);
    }

    [Fact]
    public void FlatFeesAreLeftOutEvenCreditsWhichAreNegative()
    {
        // $6.00 basic charge and a $10.00 credit: -4.00 of fixed charges, so $210.32 is for the electricity.
        var bill = Make("2026-08-20", "2026-09-21", 206.32m, 1036, new FixedFee("Basic service", 6.00m), new FixedFee("Res Savers credit", -10.00m));
        Assert.Equal(-4.00m, bill.FixedTotal);
        Assert.Equal(210.32m, bill.Variable);
        Assert.Equal(0.20301, (double)bill.PricePerKwh, precision: 5);
    }

    [Fact]
    public void ZeroOrManyFeesAreFine()
    {
        Assert.Equal(0m, Make("2026-01-01", "2026-01-31", 100m, 500).FixedTotal);
        var many = Make("2026-01-01", "2026-01-31", 100m, 500, new FixedFee("a", 1), new FixedFee("b", 2), new FixedFee("c", 3), new FixedFee("d", -1));
        Assert.Equal(5m, many.FixedTotal);
        Assert.Equal(95m, many.Variable);
    }

    [Fact]
    public void ASingleDayBillCoversOneDay() => Assert.Equal(1, Make("2026-01-05", "2026-01-05", 5m, 10).Days);

    [Fact]
    public void AGoodBillIsAccepted() =>
        Assert.Null(BillValidator.Validate(Make("2026-08-20", "2026-09-21", 206.32m, 1036), Array.Empty<Bill>()));

    [Theory]
    [InlineData("2026-09-21", "2026-08-20", "before the start")]           // wrong order
    [InlineData("2025-01-01", "2026-06-01", "more than a year")]
    public void DatesInTheWrongOrderOrTooLongAreRefused(string start, string end, string expected) =>
        Assert.Contains(expected, BillValidator.Validate(Make(start, end, 100m, 500), Array.Empty<Bill>()));

    [Theory]
    [InlineData(100, 0)]
    [InlineData(100, -5)]
    [InlineData(100, double.NaN)]
    [InlineData(0, 500)]
    [InlineData(-3, 500)]
    public void ATotalOrUsageThatIsNotPositiveIsRefused(decimal total, double kwh) =>
        Assert.NotNull(BillValidator.Validate(Make("2026-01-01", "2026-01-31", total, kwh), Array.Empty<Bill>()));

    [Fact]
    public void FeesThatSwallowTheWholeTotalAreRefused() =>
        Assert.Contains("nothing is left to price",
            BillValidator.Validate(Make("2026-01-01", "2026-01-31", 100m, 500, new FixedFee("everything", 100m)), Array.Empty<Bill>()));

    [Fact]
    public void ATwoBillsClaimingTheSameDayIsRefused()
    {
        var existing = new[] { Make("2026-08-20", "2026-09-21", 206.32m, 1036) with { Id = 1 } };

        Assert.Contains("overlap", BillValidator.Validate(Make("2026-09-21", "2026-10-23", 150m, 900), existing));   // shares 9/21
        Assert.Contains("overlap", BillValidator.Validate(Make("2026-08-01", "2026-08-20", 150m, 900), existing));   // shares 8/20
        Assert.Contains("overlap", BillValidator.Validate(Make("2026-09-01", "2026-09-05", 20m, 100), existing));    // inside it
        Assert.Null(BillValidator.Validate(Make("2026-09-22", "2026-10-23", 150m, 900), existing));                  // the next day is fine
    }

    [Fact]
    public void PastBillsCanBeEnteredInAnyOrderAndABillCanBeEditedWithoutClashingWithItself()
    {
        var march = Make("2026-03-01", "2026-03-31", 100m, 500) with { Id = 1 };
        var august = Make("2026-08-01", "2026-08-31", 100m, 500) with { Id = 2 };

        // Entering February after August and March is fine.
        Assert.Null(BillValidator.Validate(Make("2026-02-01", "2026-02-28", 100m, 500), new[] { march, august }));
        // Editing March: it overlaps its own stored copy, which must not count.
        Assert.Null(BillValidator.Validate(march with { Total = 120m }, new[] { march, august }));
    }
}

public class RateScheduleTests
{
    private static readonly TimeZoneInfo Utc = TimeZoneInfo.Utc;
    private static DateOnly D(string s) => DateOnly.Parse(s);
    private static DateTimeOffset T(string s) => DateTimeOffset.Parse(s + "Z");

    private static Bill Bill(string start, string end, decimal total, double kwh) => new(0, D(start), D(end), total, kwh, Array.Empty<FixedFee>());

    private static readonly Bill August = Bill("2026-08-20", "2026-09-21", 200m, 1000);   // $0.20
    private static readonly Bill October = Bill("2026-09-22", "2026-10-23", 180m, 1000);  // $0.18
    private static readonly Bill Gap = Bill("2026-12-01", "2026-12-31", 250m, 1000);      // $0.25, after a gap

    [Fact]
    public void OneRateIgnoresTheBills()
    {
        var s = new RateSchedule(RateMode.OneRate, 0.15m, new[] { August }, Utc);
        Assert.Equal(0.15m, s.RateAt(D("2026-09-01")));
        Assert.False(s.IsEstimate(D("2026-09-01")));
    }

    [Fact]
    public void BillsModeWithNoBillsFallsBackToTheSingleRate() =>
        Assert.Equal(0.15m, new RateSchedule(RateMode.FromBills, 0.15m, Array.Empty<Bill>(), Utc).RateAt(D("2026-09-01")));

    [Fact]
    public void NothingKnownMeansNoRate() =>
        Assert.Null(new RateSchedule(RateMode.OneRate, null, Array.Empty<Bill>(), Utc).RateAt(D("2026-09-01")));

    [Fact]
    public void ADayInsideABillUsesThatBillsPrice()
    {
        var s = new RateSchedule(RateMode.FromBills, 0.15m, new[] { October, August }, Utc);   // given out of order
        Assert.Equal(0.20m, s.RateAt(D("2026-08-20")));    // first day
        Assert.Equal(0.20m, s.RateAt(D("2026-09-21")));    // last day
        Assert.Equal(0.18m, s.RateAt(D("2026-09-22")));    // the next bill starts
        Assert.False(s.IsEstimate(D("2026-10-01")));
    }

    [Fact]
    public void AfterTheLatestBillAndInGapsTheMostRecentPriceIsAnEstimate()
    {
        var s = new RateSchedule(RateMode.FromBills, 0.15m, new[] { August, October, Gap }, Utc);
        Assert.Equal(0.18m, s.RateAt(D("2026-11-15")));     // in the gap after October: October's price
        Assert.True(s.IsEstimate(D("2026-11-15")));
        Assert.Equal(0.25m, s.RateAt(D("2027-02-01")));     // after the latest bill
        Assert.True(s.IsEstimate(D("2027-02-01")));
    }

    [Fact]
    public void BeforeTheFirstBillTheSingleRateIsUsedOrTheFirstBillsPriceIfThereIsNone()
    {
        Assert.Equal(0.15m, new RateSchedule(RateMode.FromBills, 0.15m, new[] { August }, Utc).RateAt(D("2026-07-01")));
        Assert.Equal(0.20m, new RateSchedule(RateMode.FromBills, null, new[] { August }, Utc).RateAt(D("2026-07-01")));
        Assert.True(new RateSchedule(RateMode.FromBills, 0.15m, new[] { August }, Utc).IsEstimate(D("2026-07-01")));
    }

    [Fact]
    public void APeriodIsSplitAtMidnightIntoStretchesOfOnePrice()
    {
        var s = new RateSchedule(RateMode.FromBills, 0.15m, new[] { August, October }, Utc);

        var segments = s.Segments(T("2026-09-20T12:00:00"), T("2026-09-23T12:00:00"));

        Assert.Equal(2, segments.Count);
        Assert.Equal((T("2026-09-20T12:00:00"), T("2026-09-22T00:00:00"), 0.20m), (segments[0].From, segments[0].To, segments[0].Rate));
        Assert.Equal((T("2026-09-22T00:00:00"), T("2026-09-23T12:00:00"), 0.18m), (segments[1].From, segments[1].To, segments[1].Rate));
    }

    [Fact]
    public void MidnightFollowsTheLocalTimeZone()
    {
        var minusSix = TimeZoneInfo.CreateCustomTimeZone("test", TimeSpan.FromHours(-6), "test", "test");
        var s = new RateSchedule(RateMode.FromBills, 0.15m, new[] { August, October }, minusSix);

        // 2026-09-22 begins at 06:00 UTC in a zone six hours behind.
        var segments = s.Segments(T("2026-09-21T12:00:00"), T("2026-09-22T12:00:00"));

        Assert.Equal(T("2026-09-22T06:00:00"), segments[0].To);
        Assert.Equal((0.20m, 0.18m), (segments[0].Rate, segments[1].Rate));
    }

    [Fact]
    public void CostAddsEachStretchAtItsOwnPrice()
    {
        var s = new RateSchedule(RateMode.FromBills, 0.15m, new[] { August, October }, Utc);
        // One kWh every hour: 36 hours at $0.20 then 36 hours at $0.18.
        var cost = s.Cost((a, b) => (b - a).TotalHours, T("2026-09-20T12:00:00"), T("2026-09-23T12:00:00"));

        Assert.Equal(72, cost.Kwh, precision: 9);
        Assert.Equal(36m * 0.20m + 36m * 0.18m, cost.Cost);
        Assert.False(cost.Estimated);
        Assert.False(cost.Unpriced);
    }

    [Fact]
    public void CostAfterTheLastBillIsMarkedAsAnEstimate()
    {
        var s = new RateSchedule(RateMode.FromBills, 0.15m, new[] { August, October }, Utc);
        var cost = s.Cost((a, b) => (b - a).TotalHours, T("2026-10-22T00:00:00"), T("2026-10-26T00:00:00"));   // two days in, two days past the bill
        Assert.True(cost.Estimated);
        Assert.Equal(96m * 0.18m, cost.Cost);   // the estimate is the last bill's price
    }

    [Fact]
    public void WithNoPriceAtAllTheEnergyIsStillCountedAndFlaggedUnpriced()
    {
        var s = new RateSchedule(RateMode.OneRate, null, Array.Empty<Bill>(), Utc);
        var cost = s.Cost((a, b) => (b - a).TotalHours, T("2026-10-01T00:00:00"), T("2026-10-02T00:00:00"));
        Assert.Equal(24, cost.Kwh, precision: 9);
        Assert.Equal(0m, cost.Cost);
        Assert.True(cost.Unpriced);
    }

    [Fact]
    public void TheAveragePriceIsWeightedByTime()
    {
        var s = new RateSchedule(RateMode.FromBills, 0.15m, new[] { August, October }, Utc);
        // Three days at $0.20 (9/20 to 9/22 is 2 days) and 4 days at $0.18: (2 x 0.20 + 4 x 0.18) / 6
        var average = s.AverageRate(T("2026-09-20T00:00:00"), T("2026-09-26T00:00:00"));
        Assert.Equal((2 * 0.20 + 4 * 0.18) / 6, (double)average!.Value, precision: 9);
    }

    [Fact]
    public void ABillCoversFromItsFirstMidnightToTheEndOfItsLastDay()
    {
        var s = new RateSchedule(RateMode.FromBills, null, new[] { August }, Utc);
        var (from, to) = s.Span(August);
        Assert.Equal(T("2026-08-20T00:00:00"), from);
        Assert.Equal(T("2026-09-22T00:00:00"), to);
    }
}
