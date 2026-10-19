using KVGuzzler.Core.Billing;
using KVGuzzler.Core.History;
using KVGuzzler.Core.Power;
using Xunit;

namespace KVGuzzler.Tests;

public class PeakHistoryTests
{
    private static readonly DateTimeOffset T0 = new(2026, 10, 18, 12, 0, 0, TimeSpan.Zero);

    /// <summary>The GPU and CPU take turns being busy, so each peaks at 300 / 200 W but together they never pass 350 W.</summary>
    private sealed class TakingTurns : IPowerSource
    {
        public int Second;
        public string Name => "turns";
        public string? UnavailableReason => null;
        public IReadOnlyList<PowerReading> Read() => Second % 2 == 0
            ? new[] { new PowerReading("GPU", 300, ReadingScope.Component, PowerKind.Gpu), new PowerReading("CPU package", 50, ReadingScope.Component, PowerKind.Cpu) }
            : new[] { new PowerReading("GPU", 100, ReadingScope.Component, PowerKind.Gpu), new PowerReading("CPU package", 200, ReadingScope.Component, PowerKind.Cpu) };
        public void Dispose() { }
    }

    [Fact]
    public void TheSensorTotalsPeakIsWhatTheyReachedTogetherNotTheSumOfTheirOwnPeaks()
    {
        using var h = new TempHistory();
        var turns = new TakingTurns();
        var c = new PowerCollector(h.Store, new PowerSampler(new IPowerSource[] { turns }, maxGap: TimeSpan.FromMinutes(5)));
        c.Start(T0);
        for (var i = 0; i <= 150; i++) { turns.Second = i; c.Tick(T0.AddSeconds(i)); }
        c.Stop();

        var peak = h.Store.QueryPeak(T0, T0.AddMinutes(10))!;
        Assert.Equal(350, peak.Watts);                  // 300 + 50 together; not 300 + 200
        Assert.True(peak.Exact);
        Assert.Equal(T0, peak.At);                      // reached in the first minute, at second 2

        // Each sensor's own peak is still saved apart.
        Assert.Equal(300, h.Store.QuerySeries(T0, T0.AddMinutes(10), ChartSeriesKind.Gpu).Max(r => r.PeakWatts));
    }

    [Fact]
    public void TheWallMeterPeakIsTheWholePcAndSaysWhen()
    {
        using var h = new TempHistory();
        var m = T0.ToUnixTimeSeconds();
        h.Store.AddMinutes(new[]
        {
            new MinuteRow(m, WallMeterSource.ReadingName, ReadingScope.Wall, 0.01, 60, 180),
            new MinuteRow(m + 120, WallMeterSource.ReadingName, ReadingScope.Wall, 0.01, 60, 420),
            new MinuteRow(m + 240, WallMeterSource.ReadingName, ReadingScope.Wall, 0.01, 60, 260),
            new MinuteRow(m + 120, "GPU", ReadingScope.Component, 0.01, 60, 999),   // not the wall
        });

        var peak = h.Store.QueryPeak(T0, T0.AddMinutes(10), ReadingScope.Wall)!;
        Assert.Equal(420, peak.Watts);
        Assert.Equal(T0.AddMinutes(2), peak.At);
        Assert.True(peak.Exact);
        Assert.Equal(260, h.Store.QueryPeak(T0.AddMinutes(3), T0.AddMinutes(10), ReadingScope.Wall)!.Watts);   // only the asked period
    }

    [Fact]
    public void NothingRecordedIsNoPeak()
    {
        using var h = new TempHistory();
        Assert.Null(h.Store.QueryPeak(T0, T0.AddHours(1)));
        Assert.Null(h.Store.QueryPeak(T0, T0.AddHours(1), ReadingScope.Wall));
    }

    [Fact]
    public void MinutesSavedBeforeTheTotalWasTrackedGiveOnlyAnUpperBound()
    {
        using var h = new TempHistory();
        var m = T0.ToUnixTimeSeconds();
        h.Store.AddMinutes(new[]
        {
            new MinuteRow(m, "GPU", ReadingScope.Component, 0.01, 60, 300),
            new MinuteRow(m, "CPU package", ReadingScope.Component, 0.01, 60, 200),   // an old minute: 500 W at most
            new MinuteRow(m + 60, "GPU", ReadingScope.Component, 0.01, 60, 100),
        });
        h.Store.AddTotalPeak(m + 60, 130);                                              // a newer minute, known exactly

        var peak = h.Store.QueryPeak(T0, T0.AddMinutes(5))!;
        Assert.Equal(500, peak.Watts);
        Assert.False(peak.Exact);
        Assert.Equal(T0, peak.At);

        var recentOnly = h.Store.QueryPeak(T0.AddMinutes(1), T0.AddMinutes(5))!;        // a period that holds only the tracked minute
        Assert.Equal(130, recentOnly.Watts);
        Assert.True(recentOnly.Exact);
    }

    [Fact]
    public void AMinuteKeepsTheHighestTotalSeen()
    {
        using var h = new TempHistory();
        var m = T0.ToUnixTimeSeconds();
        h.Store.AddTotalPeak(m, 200);
        h.Store.AddTotalPeak(m, 150);   // saving a minute in pieces never lowers it
        h.Store.AddTotalPeak(m, 260);
        Assert.Equal(260, h.Store.QueryPeak(T0, T0.AddMinutes(1))!.Watts);
    }

    [Fact]
    public void TheUsageStatCarriesThePeakOfTheSameSourceAsTheEnergy()
    {
        using var h = new TempHistory();
        var m = T0.ToUnixTimeSeconds();
        var schedule = new RateSchedule(RateMode.OneRate, 0.20m, Array.Empty<Bill>(), TimeZoneInfo.Utc);
        h.Store.AddMinutes(new[] { new MinuteRow(m, "GPU", ReadingScope.Component, 0.01, 60, 300) });
        h.Store.AddTotalPeak(m, 300);

        var sensors = UsageSummary.Compute(h.Store, schedule, null, T0.AddHours(1));
        Assert.False(sensors.WholePc);
        Assert.Equal(300, sensors.Peak!.Watts);

        h.Store.AddMinutes(new[] { new MinuteRow(m, WallMeterSource.ReadingName, ReadingScope.Wall, 0.02, 60, 410) });
        var wall = UsageSummary.Compute(h.Store, schedule, null, T0.AddHours(1));
        Assert.True(wall.WholePc);
        Assert.Equal(410, wall.Peak!.Watts);
    }
}
