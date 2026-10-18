using System.Globalization;
using KVGuzzler.Core;
using Xunit;

namespace KVGuzzler.Tests;

public class EnergyMeterTests
{
    private static readonly DateTimeOffset T0 = new(2026, 10, 18, 12, 0, 0, TimeSpan.Zero);

    [Fact]
    public void ConstantLoadForAnHourIsWattsOverThousand()
    {
        var meter = new EnergyMeter(maxGap: TimeSpan.FromMinutes(5));
        for (var i = 0; i <= 60; i++) meter.AddSample(T0.AddMinutes(i), 400);
        Assert.Equal(0.4, meter.KilowattHours, precision: 9);
        Assert.Equal(TimeSpan.FromHours(1), meter.Covered);
    }

    [Fact]
    public void UsesRealTimestampsNotANominalInterval()
    {
        var meter = new EnergyMeter();
        meter.AddSample(T0, 100);
        meter.AddSample(T0.AddSeconds(10), 100);   // 10 s, not 1 s
        Assert.Equal(100.0 * 10 / 3_600_000.0, meter.KilowattHours, precision: 12);
    }

    [Fact]
    public void RampIsIntegratedAsATrapezoid()
    {
        var meter = new EnergyMeter();
        meter.AddSample(T0, 0);
        meter.AddSample(T0.AddSeconds(30), 200);   // average 100 W for 30 s
        Assert.Equal(100.0 * 30 / 3_600_000.0, meter.KilowattHours, precision: 12);
    }

    [Fact]
    public void AGapLongerThanMaxGapIsNotBilled()
    {
        var meter = new EnergyMeter(maxGap: TimeSpan.FromMinutes(1));
        meter.AddSample(T0, 500);
        meter.AddSample(T0.AddHours(8), 500);      // slept overnight
        Assert.Equal(0, meter.KilowattHours);
        Assert.Equal(TimeSpan.Zero, meter.Covered);

        meter.AddSample(T0.AddHours(8).AddSeconds(30), 500);   // sampling resumes
        Assert.Equal(500.0 * 30 / 3_600_000.0, meter.KilowattHours, precision: 12);
    }

    [Fact]
    public void OutOfOrderSamplesAddNothing()
    {
        var meter = new EnergyMeter();
        meter.AddSample(T0.AddSeconds(10), 300);
        meter.AddSample(T0, 300);
        Assert.Equal(0, meter.KilowattHours);
    }

    [Theory]
    [InlineData(-1)]
    [InlineData(double.NaN)]
    [InlineData(double.PositiveInfinity)]
    public void RejectsNonsenseWatts(double watts) =>
        Assert.Throws<ArgumentOutOfRangeException>(() => new EnergyMeter().AddSample(T0, watts));
}

public class CostTests
{
    [Fact]
    public void WorkedExampleFromTheSettingsPage() =>
        // 400 W for 1 hour = 0.4 kWh = $0.06 at $0.15/kWh
        Assert.Equal(0.06m, CostCalculator.CostOfConstantLoad(400, TimeSpan.FromHours(1), 0.15m), precision: 6);

    [Fact]
    public void MonthOfAnIdleMachine() =>
        Assert.Equal(10.8m, CostCalculator.CostOfConstantLoad(100, TimeSpan.FromDays(30), 0.15m), precision: 6);

    [Theory]
    [InlineData("en-US", "0.15", true, "0.15")]
    [InlineData("en-US", " 15 ", true, "15")]
    [InlineData("de-DE", "0,15", true, "0.15")]     // the user's own decimal separator
    [InlineData("de-DE", "0.15", true, "0.15")]     // must NOT be read as 15 where '.' groups thousands
    [InlineData("en-US", "", false, "0")]
    [InlineData("en-US", "abc", false, "0")]
    [InlineData("en-US", "-0.1", false, "0")]
    public void ParsesRates(string culture, string text, bool valid, string expected)
    {
        var saved = CultureInfo.CurrentCulture;
        try
        {
            CultureInfo.CurrentCulture = new CultureInfo(culture);
            Assert.Equal(valid, Rate.TryParse(text, out var rate));
            if (valid) Assert.Equal(decimal.Parse(expected, CultureInfo.InvariantCulture), rate);
        }
        finally { CultureInfo.CurrentCulture = saved; }
    }
}
