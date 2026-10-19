using KVGuzzler.Core.Billing;
using KVGuzzler.Core.History;
using KVGuzzler.Core.Power;
using Xunit;

namespace KVGuzzler.Tests;

public class PeripheralConfigTests : IDisposable
{
    private readonly string _dir = Path.Combine(Path.GetTempPath(), "kvguzzler-periph-" + Guid.NewGuid().ToString("N")[..8]);
    private string File_ => Path.Combine(_dir, PeripheralMetersConfig.FileName);

    public void Dispose()
    {
        try { Directory.Delete(_dir, recursive: true); } catch (IOException) { /* best effort */ }
    }

    [Fact]
    public void SavesAndLoadsBackInOrder()
    {
        new PeripheralMetersConfig(new[] { new PeripheralMeter("Monitor", "192.168.1.51"), new PeripheralMeter("Speakers", "192.168.1.52") }).Save(File_);
        Assert.Equal(new[] { "Monitor", "Speakers" }, PeripheralMetersConfig.Load(File_).Meters.Select(m => m.Label));
    }

    [Fact]
    public void AMissingOrDamagedFileMeansNoMeters()
    {
        Assert.Empty(PeripheralMetersConfig.Load(File_).Meters);
        Directory.CreateDirectory(_dir);
        File.WriteAllText(File_, "not json {");
        Assert.Empty(PeripheralMetersConfig.Load(File_).Meters);
    }

    [Fact]
    public void HandEditedBadEntriesAreSkippedAndTheGoodOnesKept()
    {
        Directory.CreateDirectory(_dir);
        File.WriteAllText(File_, """
            {"meters":[
              {"label":"Monitor","host":"192.168.1.51"},
              {"label":"Bad host","host":"http://evil.example/x"},
              {"label":"monitor","host":"192.168.1.60"},
              {"label":"Same plug","host":"192.168.1.51"},
              {"label":"","host":"192.168.1.61"},
              {"label":"Speakers","host":"192.168.1.52"}]}
            """);
        Assert.Equal(new[] { "Monitor", "Speakers" }, PeripheralMetersConfig.Load(File_).Meters.Select(m => m.Label));
    }

    [Fact]
    public void ExplainsWhyAMeterCannotBeAdded()
    {
        var config = new PeripheralMetersConfig(new[] { new PeripheralMeter("Monitor", "192.168.1.51") });
        Assert.Null(config.ProblemAdding(new PeripheralMeter("Speakers", "192.168.1.52")));
        Assert.Contains("already a meter called", config.ProblemAdding(new PeripheralMeter("MONITOR", "192.168.1.52")));
        Assert.Contains("already set up at that address", config.ProblemAdding(new PeripheralMeter("Speakers", "192.168.1.51")));
        Assert.Contains("wall meter for the whole PC", config.ProblemAdding(new PeripheralMeter("Speakers", "192.168.1.50"), wallHost: "192.168.1.50"));
        Assert.Contains("Give the meter a name", config.ProblemAdding(new PeripheralMeter(" ", "192.168.1.52")));
        Assert.Contains("used by the wall meter", config.ProblemAdding(new PeripheralMeter(WallMeterSource.ReadingName, "192.168.1.52")));
        Assert.Contains("without http://", config.ProblemAdding(new PeripheralMeter("Speakers", "http://192.168.1.52")));
        Assert.Contains("at most", config.ProblemAdding(new PeripheralMeter(new string('x', 41), "192.168.1.52")));
    }
}

public class PeripheralSourceTests : IDisposable
{
    private readonly string _dir = Path.Combine(Path.GetTempPath(), "kvguzzler-periphsrc-" + Guid.NewGuid().ToString("N")[..8]);
    private string ConfigPath => Path.Combine(_dir, PeripheralMetersConfig.FileName);
    private readonly FakePlug _plug = new();
    private readonly FakeClock _clock = new();
    private readonly List<IDisposable> _toDispose = new();

    public void Dispose()
    {
        foreach (var d in _toDispose) d.Dispose();
        try { Directory.Delete(_dir, recursive: true); } catch (IOException) { /* best effort */ }
    }

    private PeripheralMetersSource Make()
    {
        var source = new PeripheralMetersSource(ConfigPath, _plug, TimeSpan.FromMilliseconds(20), _clock);
        _toDispose.Add(source);
        return source;
    }

    private static async Task<bool> Eventually(Func<bool> condition)
    {
        for (var i = 0; i < 200; i++)
        {
            if (condition()) return true;
            await Task.Delay(20, TestContext.Current.CancellationToken);
        }
        return false;
    }

    [Fact]
    public void WithNoMetersItReadsNothingAndAsksNothingOfTheNetwork()
    {
        var source = Make();
        Assert.Null(source.UnavailableReason);   // having none is normal, not a problem to report
        Assert.Empty(source.Read());
        Assert.Empty(_plug.Requests);
    }

    [Fact]
    public async Task EachPlugIsAReadingNamedByItsLabelAndScopedAsAPeripheral()
    {
        new PeripheralMetersConfig(new[] { new PeripheralMeter("Monitor", "192.168.1.51"), new PeripheralMeter("Speakers", "192.168.1.52") }).Save(ConfigPath);
        var source = Make();
        _ = source.Read();   // looks at the config and starts polling

        Assert.True(await Eventually(() => source.Read().Count == 2), "readings did not arrive");
        var readings = source.Read();
        Assert.Equal(new[] { "Monitor", "Speakers" }, readings.Select(r => r.Name).OrderBy(n => n));
        Assert.All(readings, r => Assert.Equal(ReadingScope.Peripheral, r.Scope));
        Assert.All(_plug.Requests, r => Assert.Equal(HttpMethod.Get, r.Method));   // read only, always
        Assert.Contains(_plug.Requests, r => r.Uri.Host == "192.168.1.52");
    }

    [Fact]
    public async Task APlugThatDoesNotAnswerIsLeftOutWithoutHidingTheOthers()
    {
        _plug.Respond = r => r.RequestUri!.Host == "192.168.1.52"
            ? throw new HttpRequestException("No route to host")
            : new HttpResponseMessage(System.Net.HttpStatusCode.OK) { Content = new StringContent(FakePlug.Answer) };
        new PeripheralMetersConfig(new[] { new PeripheralMeter("Monitor", "192.168.1.51"), new PeripheralMeter("Speakers", "192.168.1.52") }).Save(ConfigPath);
        var source = Make();
        _ = source.Read();

        Assert.True(await Eventually(() => source.ProblemWith("Speakers")?.StartsWith("Could not reach") == true));
        Assert.Equal("Monitor", Assert.Single(source.Read()).Name);
    }

    [Fact]
    public async Task AddingAndRemovingAMeterIsFollowedWithoutARestart()
    {
        new PeripheralMetersConfig(new[] { new PeripheralMeter("Monitor", "192.168.1.51") }).Save(ConfigPath);
        var source = Make();
        _ = source.Read();
        Assert.True(await Eventually(() => source.Read().Count == 1));

        new PeripheralMetersConfig(new[] { new PeripheralMeter("Speakers", "192.168.1.52") }).Save(ConfigPath);
        File.SetLastWriteTimeUtc(ConfigPath, DateTime.UtcNow.AddMinutes(1));   // a change the file's timestamp is sure to show
        _clock.Advance(TimeSpan.FromSeconds(3));                                // and the config is looked at again

        Assert.True(await Eventually(() => source.Read().Select(r => r.Name).SequenceEqual(new[] { "Speakers" })), "the new list was not followed");
    }

    [Fact]
    public void TheyAreNeverPartOfThePcTotal()
    {
        var sampler = new PowerSampler(new IPowerSource[]
        {
            new Fixed(new PowerReading("CPU package", 60, ReadingScope.Component, PowerKind.Cpu)),
            new Fixed(new PowerReading("Monitor", 30, ReadingScope.Peripheral)),
        });
        var snapshot = sampler.Sample(DateTimeOffset.UtcNow);
        Assert.Equal(2, snapshot.Readings.Count);
        Assert.Equal(60, snapshot.TotalWatts);
    }

    private sealed class Fixed : IPowerSource
    {
        private readonly PowerReading _r;
        public Fixed(PowerReading r) => _r = r;
        public string Name => _r.Name;
        public string? UnavailableReason => null;
        public IReadOnlyList<PowerReading> Read() => new[] { _r };
        public void Dispose() { }
    }
}

public class PeripheralHistoryTests
{
    private static readonly DateTimeOffset T0 = new(2026, 10, 18, 12, 0, 0, TimeSpan.Zero);
    private static readonly RateSchedule Flat = new(RateMode.OneRate, 0.20m, Array.Empty<Bill>(), TimeZoneInfo.Utc);

    /// <summary>A PC on the wall meter (0.01 kWh, peak 300 W) plus a monitor (0.005, then 0.001 the next minute) and speakers (0.002).</summary>
    private static TempHistory Seeded()
    {
        var h = new TempHistory();
        var m = T0.ToUnixTimeSeconds();
        h.Store.AddMinutes(new[]
        {
            new MinuteRow(m, WallMeterSource.ReadingName, ReadingScope.Wall, 0.01, 60, 300),
            new MinuteRow(m, "Monitor", ReadingScope.Peripheral, 0.005, 60, 40),
            new MinuteRow(m, "Speakers", ReadingScope.Peripheral, 0.002, 60, 15),
            new MinuteRow(m + 60, "Monitor", ReadingScope.Peripheral, 0.001, 60, 55),
        });
        return h;
    }

    [Fact]
    public void ExtraMetersNeverLeakIntoTheSensorOrWallFigures()
    {
        using var h = Seeded();
        Assert.Equal(0.01, h.Store.Query(T0, T0.AddHours(1), ReadingScope.Wall).KilowattHours, precision: 9);
        Assert.Equal(0, h.Store.Query(T0, T0.AddHours(1)).KilowattHours, precision: 9);   // no CPU or GPU sensors here
        Assert.Equal(0.01, h.Store.EnergyKwh(T0, T0.AddHours(1), ReadingScope.Wall), precision: 9);
    }

    [Fact]
    public void EachMeterIsListedOnItsOwnBiggestFirst()
    {
        using var h = Seeded();
        var use = h.Store.QueryPeripherals(T0, T0.AddHours(1));
        Assert.Equal(new[] { "Monitor", "Speakers" }, use.Select(u => u.Name));
        Assert.Equal(0.006, use[0].KilowattHours, precision: 9);
        Assert.Equal(55, use[0].PeakWatts);
    }

    [Fact]
    public void OnlyTheChosenMetersAreAddedUp()
    {
        using var h = Seeded();
        Assert.Equal(0.008, h.Store.PeripheralKwh(T0, T0.AddHours(1), new[] { "Monitor", "Speakers" }), precision: 9);
        Assert.Equal(0.006, h.Store.PeripheralKwh(T0, T0.AddHours(1), new[] { "Monitor" }), precision: 9);
        Assert.Equal(0, h.Store.PeripheralKwh(T0, T0.AddHours(1), Array.Empty<string>()));
        Assert.Equal(0, h.Store.PeripheralKwh(T0, T0.AddHours(1), new[] { "x') OR 1=1 --" }));   // a name is a value, never SQL
    }

    [Fact]
    public void TheHeadlineIsThePcAloneUntilMetersAreChosen()
    {
        using var h = Seeded();
        var alone = UsageSummary.Compute(h.Store, Flat, null, T0.AddHours(1));
        Assert.Equal(0.01, alone.Kwh, precision: 9);
        Assert.Equal(0, alone.PeripheralKwh);
        Assert.Equal(0, alone.PeripheralCount);
        Assert.Equal(300, alone.Peak!.Watts);
        Assert.True(alone.Peak.Exact);

        var all = UsageSummary.Compute(h.Store, Flat, null, T0.AddHours(1), new[] { "Monitor", "Speakers" });
        Assert.Equal(0.018, all.Kwh, precision: 9);
        Assert.Equal(0.008, all.PeripheralKwh, precision: 9);
        Assert.Equal(2, all.PeripheralCount);
        Assert.Equal(0.018m * 0.20m, all.Cost!.Value, precision: 6);   // priced like the PC
        Assert.Equal(300 + 55, all.Peak!.Watts);                      // the PC's peak plus the meters' worst minute (40 + 15 in the first, 55 in the second)
        Assert.False(all.Peak.Exact);                                 // they need not all peak in the same second

        var onlyMonitor = UsageSummary.Compute(h.Store, Flat, null, T0.AddHours(1), new[] { "Monitor" });
        Assert.Equal(0.016, onlyMonitor.Kwh, precision: 9);
    }

    [Fact]
    public void TheAverageDayCanIncludeThemToo()
    {
        using var h = Seeded();
        var m = T0.ToUnixTimeSeconds();
        h.Store.AddMinutes(new[] { new MinuteRow(m + 7200, WallMeterSource.ReadingName, ReadingScope.Wall, 0.01, 60, 300) });
        var pc = UsageSummary.AverageDay(h.Store, null, T0.AddHours(3))!;
        var all = UsageSummary.AverageDay(h.Store, null, T0.AddHours(3), new[] { "Monitor", "Speakers" })!;
        Assert.Equal(0.02, pc.Kwh, precision: 9);
        Assert.Equal(0.028, all.Kwh, precision: 9);
        Assert.Equal(2, all.PeripheralCount);
    }

    [Fact]
    public void TheChartGetsALineForEachChosenMeterToldApartByVariant()
    {
        using var h = Seeded();
        var none = PowerChart.Build(h.Store, T0, T0.AddHours(1), TimeSpan.FromMinutes(1));
        Assert.DoesNotContain(none.Series, s => s.Kind == ChartSeriesKind.Peripheral);

        var some = PowerChart.Build(h.Store, T0, T0.AddHours(1), TimeSpan.FromMinutes(1), new[] { "Monitor", "Speakers" });
        var lines = some.Series.Where(s => s.Kind == ChartSeriesKind.Peripheral).ToList();
        Assert.Equal(new[] { "Monitor", "Speakers" }, lines.Select(l => l.Name));
        Assert.Equal(new[] { 0, 1 }, lines.Select(l => l.Variant));
        Assert.Equal(0.005 * 3_600_000 / 60, lines[0].Points[0].Watts, precision: 6);   // first minute, as watts
        Assert.Equal(0.002 * 3_600_000 / 60, lines[1].Points[0].Watts, precision: 6);

        var only = PowerChart.Build(h.Store, T0, T0.AddHours(1), TimeSpan.FromMinutes(1), new[] { "Monitor" }, includePc: false);
        Assert.Equal(new[] { "Monitor" }, only.Series.Select(s => s.Name));   // no whole-PC line without the PC
    }

    [Fact]
    public void TheCollectorSavesEachMeterUnderItsLabelWithoutTouchingTheTotal()
    {
        using var h = new TempHistory();
        var sampler = new PowerSampler(new IPowerSource[] { new MonitorSource() }, maxGap: TimeSpan.FromMinutes(5));
        var c = new PowerCollector(h.Store, sampler);
        c.Start(T0);
        for (var i = 0; i <= 120; i++) c.Tick(T0.AddSeconds(i));
        c.Stop();

        var use = Assert.Single(h.Store.QueryPeripherals(T0, T0.AddMinutes(10)));
        Assert.Equal("Monitor", use.Name);
        Assert.Equal(30 * 120 / 3_600_000.0, use.KilowattHours, precision: 7);
        Assert.Equal(0, h.Store.Query(T0, T0.AddMinutes(10)).KilowattHours);
        Assert.Null(h.Store.QueryPeak(T0, T0.AddMinutes(10)));   // the PC's total peak is untouched by a monitor
    }

    private sealed class MonitorSource : IPowerSource
    {
        public string Name => "monitor";
        public string? UnavailableReason => null;
        public IReadOnlyList<PowerReading> Read() => new[] { new PowerReading("Monitor", 30, ReadingScope.Peripheral) };
        public void Dispose() { }
    }
}
