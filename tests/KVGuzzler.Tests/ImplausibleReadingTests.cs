using KVGuzzler.Core.History;
using KVGuzzler.Core.Power;
using KVGuzzler.Core.Programs;
using Xunit;

namespace KVGuzzler.Tests;

/// <summary>NVML answers with nonsense (hundreds of kilowatts) on the first read after the PC wakes. It must not become energy.</summary>
public class ImplausibleReadingTests
{
    private static readonly DateTimeOffset T0 = new(2026, 10, 18, 12, 0, 0, TimeSpan.Zero);
    private static PowerReading Gpu(double w) => new("GPU", w, ReadingScope.Component, PowerKind.Gpu);

    [Fact]
    public void AnImpossibleReadingIsDroppedLikeAMissingOneAndSaysSo()
    {
        var s = new PowerSampler(new[] { new FakeSource("gpu", () => new[] { Gpu(297_204), new PowerReading("CPU package", 60, ReadingScope.Component, PowerKind.Cpu) }) })
            .Sample(T0);

        Assert.Equal(60, s.TotalWatts);
        Assert.DoesNotContain(s.Readings, r => r.Name == "GPU");
        Assert.Contains("ignored an impossible reading", Assert.Single(s.Notes));
    }

    [Fact]
    public void AReadingThatIsMerelyHighStays()
    {
        var s = new PowerSampler(new[] { new FakeSource("gpu", () => new[] { Gpu(PowerSampler.MaxComponentWatts) }) }).Sample(T0);
        Assert.Equal(PowerSampler.MaxComponentWatts, s.TotalWatts);
    }

    [Fact]
    public void ThePlugsReadingsMayBeHigherThanASensorsButNotWithoutLimit()
    {
        Assert.True(PowerSampler.MaxWattsFor(ReadingScope.Wall) > PowerSampler.MaxWattsFor(ReadingScope.Component));
        var s = new PowerSampler(new[]
        {
            new FakeSource("plug", () => new[] { new PowerReading("Whole PC", 3000, ReadingScope.Wall), new PowerReading("Monitor", 90_000, ReadingScope.Peripheral) }),
        }).Sample(T0);
        Assert.Equal("Whole PC", Assert.Single(s.Readings).Name);
    }

    /// <summary>A GPU that answers 50 W, sleeps, then answers garbage on its first read after waking.</summary>
    private sealed class GarbageAfterWake : IPowerSource
    {
        public double Next = 50;
        public string Name => "gpu";
        public string? UnavailableReason => null;
        public IReadOnlyList<PowerReading> Read() => new[] { Gpu(Next) };
        public void Dispose() { }
    }

    [Fact]
    public void GarbageOnTheFirstReadAfterWakingNeverReachesTheHistory()
    {
        using var h = new TempHistory();
        var gpu = new GarbageAfterWake();
        var c = new PowerCollector(h.Store, new PowerSampler(new IPowerSource[] { gpu }, maxGap: TimeSpan.FromMinutes(5)));
        c.Start(T0);
        for (var i = 0; i < 20; i++) c.Tick(T0.AddSeconds(i));

        var wake = T0.AddHours(3);
        gpu.Next = 297_204;
        c.Tick(wake);
        gpu.Next = 50;
        for (var i = 1; i < 60; i++) c.Tick(wake.AddSeconds(i));
        c.Stop();

        var rows = h.Store.QuerySeries(T0, wake.AddMinutes(5), ChartSeriesKind.Gpu);
        Assert.NotEmpty(rows);
        Assert.All(rows, r => Assert.True(r.PeakWatts <= 50 + 1e-6, $"peak {r.PeakWatts}"));
        Assert.All(rows, r => Assert.Equal(50, r.KilowattHours * 3_600_000.0 / r.Seconds, precision: 6));
        Assert.True(h.Store.QueryPeak(T0, wake.AddMinutes(5))!.Watts <= 50 + 1e-6);
    }

    [Fact]
    public void HistorySavedFromGarbageIsRepairedToTheSensorsOwnAverage()
    {
        using var h = new TempHistory();
        var m = T0.ToUnixTimeSeconds() / 60 * 60;
        // Two good minutes at 50 W, and one where a 297 kW reading was averaged in.
        h.Store.AddMinutes(new[]
        {
            new MinuteRow(m, "GPU", ReadingScope.Component, 50 * 60 / 3_600_000.0, 60, 50, PowerKind.Gpu),
            new MinuteRow(m + 60, "GPU", ReadingScope.Component, 50 * 60 / 3_600_000.0, 60, 50, PowerKind.Gpu),
            new MinuteRow(m + 120, "GPU", ReadingScope.Component, 0.0656, 54, 297_204, PowerKind.Gpu),
            new MinuteRow(m + 120, "CPU package", ReadingScope.Component, 30 * 54 / 3_600_000.0, 54, 40, PowerKind.Cpu),
        });
        h.Store.AddTotalPeak(m + 120, 297_318);
        h.Store.AddProgramMinutes(m + 120, new[] { new ProgramEnergy("System", 0, 0.0271, 10) });

        Assert.Equal(1, h.Store.RepairImplausibleReadings());

        var rows = h.Store.QuerySeries(T0, T0.AddMinutes(10), ChartSeriesKind.Gpu);
        Assert.Equal(3, rows.Count);
        Assert.All(rows, r => Assert.Equal(50, r.KilowattHours * 3_600_000.0 / r.Seconds, precision: 6));
        Assert.All(rows, r => Assert.True(r.PeakWatts <= 50 + 1e-6));

        var peak = h.Store.QueryPeak(T0, T0.AddMinutes(10))!;
        Assert.True(peak.Watts < PowerSampler.MaxComponentWatts);

        // The programs' share of that minute shrank with the GPU's energy instead of keeping the garbage.
        Assert.True(h.Store.QueryPrograms(T0, T0.AddMinutes(10)).Sum(p => p.GpuKwh) < 0.001);

        Assert.Equal(0, h.Store.RepairImplausibleReadings());   // nothing left to repair: running it again changes nothing
    }
}
