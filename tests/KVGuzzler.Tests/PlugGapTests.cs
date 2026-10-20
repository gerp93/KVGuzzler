using KVGuzzler.Core.History;
using KVGuzzler.Core.Power;
using KVGuzzler.Views;
using Microsoft.Data.Sqlite;
using Xunit;

namespace KVGuzzler.Tests;

public class ShellyCounterTests
{
    [Fact]
    public void ReadsTheEnergyCounterOfARealisticAnswer()
    {
        var status = ShellyPlug.ParseStatus(FakePlug.Answer);
        Assert.Equal(52.1, status.Watts);
        Assert.Equal(1234.5, status.EnergyWh);
    }

    [Theory]
    [InlineData("""{"apower":52.1}""")]                                  // no counter at all
    [InlineData("""{"apower":52.1,"aenergy":{}}""")]
    [InlineData("""{"apower":52.1,"aenergy":{"total":"12"}}""")]         // text, not a number
    [InlineData("""{"apower":52.1,"aenergy":{"total":-4}}""")]
    [InlineData("""{"apower":52.1,"aenergy":5}""")]
    public void AMissingOrOddCounterIsNullAndTheWattsAreStillGood(string answer)
    {
        var status = ShellyPlug.ParseStatus(answer);
        Assert.Equal(52.1, status.Watts);
        Assert.Null(status.EnergyWh);
    }

    [Fact]
    public async Task ReadingTheCounterIsStillOnlyAGet()
    {
        var plug = new FakePlug();
        var status = await ShellyPlug.ReadStatusAsync(new HttpClient(plug), "192.168.1.50", TestContext.Current.CancellationToken);
        Assert.Equal(1234.5, status.EnergyWh);
        var (method, uri) = Assert.Single(plug.Requests);
        Assert.Equal(HttpMethod.Get, method);
        Assert.Equal("http://192.168.1.50/rpc/Switch.GetStatus?id=0", uri.ToString());
    }
}

public class WallMeterCounterSourceTests : IDisposable
{
    private readonly string _dir = Path.Combine(Path.GetTempPath(), "kvguzzler-wallctr-" + Guid.NewGuid().ToString("N")[..8]);
    private readonly FakePlug _plug = new();
    private WallMeterSource? _source;

    public void Dispose()
    {
        _source?.Dispose();
        try { Directory.Delete(_dir, recursive: true); } catch (IOException) { /* best effort */ }
    }

    private WallMeterSource Make()
    {
        var path = Path.Combine(_dir, WallMeterConfig.FileName);
        new WallMeterConfig("192.168.1.50").Save(path);
        _source = new WallMeterSource(path, _plug, TimeSpan.FromMilliseconds(20), new FakeClock());
        _ = _source.UnavailableReason;
        return _source;
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
    public async Task KeepsTheCounterOfTheLatestAnswerWithTheAddressItCameFrom()
    {
        var source = Make();
        Assert.True(await Eventually(() => source.LatestCounter is not null));
        var counter = source.LatestCounter!;
        Assert.Equal(1234.5, counter.TotalWh);
        Assert.Equal("192.168.1.50", counter.Host);
        Assert.All(_plug.Snapshot(), r => Assert.Equal(HttpMethod.Get, r.Method));
    }

    [Fact]
    public async Task AnAnswerWithoutACounterLeavesNoCounter()
    {
        _plug.Respond = _ => new HttpResponseMessage(System.Net.HttpStatusCode.OK) { Content = new StringContent("""{"apower":30.0}""") };
        var source = Make();
        Assert.True(await Eventually(() => source.UnavailableReason is null));
        Assert.Null(source.LatestCounter);
        Assert.Single(source.Read());   // the watts are fine without it
    }
}

public class PlugGapFillTests
{
    private static readonly DateTimeOffset T0 = new(2026, 10, 18, 12, 0, 0, TimeSpan.Zero);
    private const string Host = "192.168.1.50";
    private const double AwakeWatts = 200;
    private const double AsleepWatts = 5;

    /// <summary>The PC's sensors and a plug: 100 W of GPU and 50 W of CPU inside a 200 W whole PC, and a counter the test moves by hand.</summary>
    private sealed class PcWithPlug : IPowerSource, IWallEnergyCounter
    {
        public WallCounter? Counter { get; set; }
        public string Name => "pc with plug";
        public string? UnavailableReason => null;
        public WallCounter? LatestCounter => Counter;
        public IReadOnlyList<PowerReading> Read() => new[]
        {
            new PowerReading("GPU", 100, ReadingScope.Component),
            new PowerReading("CPU package", 50, ReadingScope.Component),
            new PowerReading(WallMeterSource.ReadingName, AwakeWatts, ReadingScope.Wall),
        };
        public void Dispose() { }
    }

    private static PowerCollector Make(TempHistory h, PcWithPlug pc) =>
        new(h.Store, new PowerSampler(new IPowerSource[] { pc }, maxGap: TimeSpan.FromMinutes(5)));

    /// <summary>The PC runs for <paramref name="seconds"/>, the plug answering every second; returns the counter at the end.</summary>
    private static double RunAwake(PowerCollector c, PcWithPlug pc, DateTimeOffset from, int seconds, double startWh, string host = Host)
    {
        for (var i = 0; i <= seconds; i++)
        {
            var at = from.AddSeconds(i);
            pc.Counter = new WallCounter(host, startWh + AwakeWatts * i / 3600.0, at);
            c.Tick(at);
        }
        return startWh + AwakeWatts * seconds / 3600.0;
    }

    private static DateTimeOffset Wake(DateTimeOffset sleepStart, TimeSpan asleep) => sleepStart + asleep;

    [Fact]
    public void ACounterThatKeepsCountingAcrossASleepGivesTheGapItsEnergy()
    {
        using var h = new TempHistory();
        var pc = new PcWithPlug();
        var c = Make(h, pc);
        c.Start(T0);
        var wh = RunAwake(c, pc, T0, 30, startWh: 1000);

        var slept = TimeSpan.FromHours(8);
        var wake = Wake(T0.AddSeconds(30), slept);
        wh += AsleepWatts * slept.TotalHours;                       // the plug counted 40 Wh while the PC slept
        RunAwake(c, pc, wake, 30, startWh: wh);
        c.Stop();

        var end = wake.AddMinutes(1);
        var gap = Assert.Single(h.Store.Query(T0, end).Gaps);
        Assert.Equal(GapReason.Asleep, gap.Reason);
        Assert.Equal(0.040, gap.PlugKwh!.Value, precision: 6);

        // The whole PC is what was sampled (200 W for 60 s) plus the plug's figure for the sleep.
        var sampledWall = AwakeWatts * 60 / 3_600_000.0;
        Assert.Equal(sampledWall + 0.040, h.Store.Query(T0, end, ReadingScope.Wall).KilowattHours, precision: 6);
        Assert.Equal(sampledWall + 0.040, h.Store.EnergyKwh(T0, end, ReadingScope.Wall), precision: 6);
        Assert.Equal(0.040, h.Store.PlugGapKwh(T0, end), precision: 6);
    }

    [Fact]
    public void TheFilledEnergyNeverTouchesTheSensorsOrTheSampledTime()
    {
        using var h = new TempHistory();
        var pc = new PcWithPlug();
        var c = Make(h, pc);
        c.Start(T0);
        var wh = RunAwake(c, pc, T0, 30, startWh: 1000);
        var wake = Wake(T0.AddSeconds(30), TimeSpan.FromHours(8));
        RunAwake(c, pc, wake, 30, startWh: wh + AsleepWatts * 8);
        c.Stop();

        var end = wake.AddMinutes(1);
        var (activeKwh, monitored, _) = h.Store.Query(T0, end);
        Assert.Equal(150.0 * 60 / 3_600_000.0, activeKwh, precision: 6);   // sensors: only the 60 s sampled
        Assert.Equal(TimeSpan.FromSeconds(60), monitored);                  // and the time covered does not grow by the sleep
        Assert.Equal(60, h.Store.Query(T0, end, ReadingScope.Wall).Monitored.TotalSeconds);

        // The saved minute rows hold only what was sampled: no row was made up for the gap.
        Assert.Equal(AwakeWatts * 60 / 3_600_000.0, h.Store.QuerySeries(T0, end, ChartSeriesKind.Wall).Sum(m => m.KilowattHours), precision: 6);
    }

    [Fact]
    public void AReportCountsTheFilledEnergyUnderTheWholePcAndSaysWhereItCameFrom()
    {
        using var h = new TempHistory();
        var pc = new PcWithPlug();
        var c = Make(h, pc);
        c.Start(T0);
        var wh = RunAwake(c, pc, T0, 30, startWh: 1000);
        var wake = Wake(T0.AddSeconds(30), TimeSpan.FromHours(8));
        RunAwake(c, pc, wake, 30, startWh: wh + AsleepWatts * 8);
        c.Stop();

        var report = HistoryReport.Build(h.Store, "Last 24 hours", T0, wake.AddMinutes(1), sleepWatts: 2);
        var sampledWall = AwakeWatts * 60 / 3_600_000.0;
        Assert.Equal(sampledWall + 0.040, report.WallKwh!.Value, precision: 6);
        Assert.Equal(0.040, report.PlugGapKwh, precision: 6);
        Assert.Equal(150.0 * 60 / 3_600_000.0, report.MeasuredKwh, precision: 6);   // the sensors' share is not inflated by it

        var row = MainWindow.HistoryRowFor(report, rate: 0.5m, currency: "$");
        Assert.Equal("whole PC", row.Tag);
        Assert.Matches(@"0[.,]043 kWh whole PC", row.Title);
        Assert.Contains("plug's own energy counter", row.Detail);
        Assert.DoesNotContain("sleep wattage", row.Detail);   // the estimate is not what the figure rests on any more
    }

    [Fact]
    public void APeriodThatIsEntirelySleepIsStillTheWholePcFromThePlug()
    {
        using var h = new TempHistory();
        var pc = new PcWithPlug();
        var c = Make(h, pc);
        c.Start(T0);
        var wh = RunAwake(c, pc, T0, 30, startWh: 1000);
        var wake = Wake(T0.AddSeconds(30), TimeSpan.FromHours(8));
        RunAwake(c, pc, wake, 5, startWh: wh + AsleepWatts * 8);
        c.Stop();

        // The hour from 02:00 to 03:00 into the sleep: nothing was sampled, five watts for an hour.
        var from = T0.AddHours(2);
        var report = HistoryReport.Build(h.Store, "an asleep hour", from, from.AddHours(1), sleepWatts: 2);
        Assert.Equal(0.005, report.WallKwh!.Value, precision: 6);
        Assert.Equal(0.005, report.PlugGapKwh, precision: 6);
        Assert.Equal(0, report.MeasuredKwh);
    }

    [Fact]
    public void APeriodThatCutsAGapGetsItsShareOfTheGapsEnergy()
    {
        using var h = new TempHistory();
        h.Store.AddGap(new Gap(T0, T0.AddHours(10), GapReason.Asleep));
        h.Store.SetGapPlugEnergy(T0, 0.05);

        Assert.Equal(0.005, h.Store.EnergyKwh(T0.AddHours(2), T0.AddHours(3), ReadingScope.Wall), precision: 9);
        var clipped = Assert.Single(h.Store.Query(T0.AddHours(5), T0.AddHours(20)).Gaps);
        Assert.Equal(0.025, clipped.PlugKwh!.Value, precision: 9);
        Assert.Equal(0.05, h.Store.EnergyKwh(T0.AddHours(-5), T0.AddHours(20), ReadingScope.Wall), precision: 9);
        // Adjacent periods add up exactly: pricing day by day sees every watt-hour once.
        Assert.Equal(
            h.Store.EnergyKwh(T0, T0.AddHours(20), ReadingScope.Wall),
            h.Store.EnergyKwh(T0, T0.AddHours(4), ReadingScope.Wall) + h.Store.EnergyKwh(T0.AddHours(4), T0.AddHours(20), ReadingScope.Wall),
            precision: 12);
        Assert.Equal(0, h.Store.EnergyKwh(T0, T0.AddHours(20)));   // sensors: nothing
    }

    [Fact]
    public void APlugThatRestartedDuringTheSleepLeavesTheGapAsItWas()
    {
        using var h = new TempHistory();
        var pc = new PcWithPlug();
        var c = Make(h, pc);
        c.Start(T0);
        RunAwake(c, pc, T0, 30, startWh: 1000);
        var wake = Wake(T0.AddSeconds(30), TimeSpan.FromHours(8));
        RunAwake(c, pc, wake, 30, startWh: 0.2);        // the counter started over from zero: lower than before
        c.Stop();

        var end = wake.AddMinutes(1);
        var gap = Assert.Single(h.Store.Query(T0, end).Gaps);
        Assert.Null(gap.PlugKwh);
        var sampledWall = AwakeWatts * 60 / 3_600_000.0;
        Assert.Equal(sampledWall, h.Store.Query(T0, end, ReadingScope.Wall).KilowattHours, precision: 6);   // nothing invented
        Assert.Equal(0, HistoryReport.Build(h.Store, "x", T0, end, 2).PlugGapKwh);
    }

    [Fact]
    public void ACounterThatRestartedAndCountedBackUpPastTheOldValueIsNotBelieved()
    {
        using var h = new TempHistory();
        var pc = new PcWithPlug();
        var c = Make(h, pc);
        c.Start(T0);
        RunAwake(c, pc, T0, 30, startWh: 1000);
        var wake = Wake(T0.AddSeconds(30), TimeSpan.FromHours(1));
        RunAwake(c, pc, wake, 5, startWh: 9000);        // 8 kWh in an hour is 8000 W: more than a plug can carry

        Assert.Null(Assert.Single(h.Store.Query(T0, wake.AddMinutes(1)).Gaps).PlugKwh);
    }

    [Fact]
    public void WithoutAnEnergyCounterTheGapStaysAsItWas()
    {
        using var h = new TempHistory();
        var pc = new PcWithPlug();
        var c = Make(h, pc);
        c.Start(T0);
        for (var i = 0; i <= 30; i++) c.Tick(T0.AddSeconds(i));        // the plug's answers carry no aenergy: Counter stays null
        var wake = Wake(T0.AddSeconds(30), TimeSpan.FromHours(8));
        for (var i = 0; i <= 30; i++) c.Tick(wake.AddSeconds(i));
        c.Stop();

        var end = wake.AddMinutes(1);
        Assert.Null(Assert.Single(h.Store.Query(T0, end).Gaps).PlugKwh);
        Assert.Equal(AwakeWatts * 60 / 3_600_000.0, h.Store.Query(T0, end, ReadingScope.Wall).KilowattHours, precision: 6);
    }

    [Fact]
    public void ACounterMissingOnlyAfterTheGapLeavesItAsItWas()
    {
        using var h = new TempHistory();
        var pc = new PcWithPlug();
        var c = Make(h, pc);
        c.Start(T0);
        RunAwake(c, pc, T0, 30, startWh: 1000);
        var wake = Wake(T0.AddSeconds(30), TimeSpan.FromHours(8));
        pc.Counter = null;                                              // the plug answers again, but without a counter
        for (var i = 0; i <= 30; i++) c.Tick(wake.AddSeconds(i));

        Assert.Null(Assert.Single(h.Store.Query(T0, wake.AddMinutes(1)).Gaps).PlugKwh);
    }

    [Fact]
    public void ThePlugAnsweringALittleAfterWakingStillFillsTheGap()
    {
        using var h = new TempHistory();
        var pc = new PcWithPlug();
        var c = Make(h, pc);
        c.Start(T0);
        var wh = RunAwake(c, pc, T0, 30, startWh: 1000);
        var wake = Wake(T0.AddSeconds(30), TimeSpan.FromHours(8));

        // Waking: the network is not back yet, so the plug source still holds its answer from before the sleep.
        for (var i = 0; i < 5; i++) c.Tick(wake.AddSeconds(i));
        Assert.Null(Assert.Single(h.Store.Query(T0, wake.AddMinutes(1)).Gaps).PlugKwh);

        var back = wake.AddSeconds(5);
        pc.Counter = new WallCounter(Host, wh + AsleepWatts * 8 + AsleepWatts * 5 / 3600.0, back);
        c.Tick(back);

        Assert.Equal(0.040, Assert.Single(h.Store.Query(T0, wake.AddMinutes(1)).Gaps).PlugKwh!.Value, precision: 4);
    }

    [Fact]
    public void ThePlugNeverComingBackLeavesTheGapAsItWas()
    {
        using var h = new TempHistory();
        var pc = new PcWithPlug();
        var c = Make(h, pc);
        c.Start(T0);
        RunAwake(c, pc, T0, 30, startWh: 1000);
        var wake = Wake(T0.AddSeconds(30), TimeSpan.FromHours(8));
        for (var i = 0; i < 300; i++) c.Tick(wake.AddSeconds(i));      // five minutes with only the old answer

        pc.Counter = new WallCounter(Host, 1100, wake.AddSeconds(300));   // too late: it would also cover time that was sampled
        c.Tick(wake.AddSeconds(300));

        Assert.Null(Assert.Single(h.Store.Query(T0, wake.AddMinutes(10)).Gaps).PlugKwh);
    }

    [Fact]
    public void ADifferentPlugAfterTheGapIsNotComparedWithTheOldOne()
    {
        using var h = new TempHistory();
        var pc = new PcWithPlug();
        var c = Make(h, pc);
        c.Start(T0);
        RunAwake(c, pc, T0, 30, startWh: 1000);
        var wake = Wake(T0.AddSeconds(30), TimeSpan.FromHours(8));
        RunAwake(c, pc, wake, 30, startWh: 1100, host: "192.168.1.77");

        Assert.Null(Assert.Single(h.Store.Query(T0, wake.AddMinutes(1)).Gaps).PlugKwh);
    }

    [Fact]
    public void ACounterReadLongBeforeTheGapStartedIsNotUsed()
    {
        using var h = new TempHistory();
        var pc = new PcWithPlug();
        var c = Make(h, pc);
        c.Start(T0);
        RunAwake(c, pc, T0, 5, startWh: 1000);
        // The plug stops answering; the PC keeps running for ten minutes with the sensors only, then sleeps.
        pc.Counter = new WallCounter(Host, 1001, T0.AddSeconds(5));
        for (var i = 6; i <= 600; i++) c.Tick(T0.AddSeconds(i));
        var wake = Wake(T0.AddSeconds(600), TimeSpan.FromHours(8));
        RunAwake(c, pc, wake, 30, startWh: 1045);

        Assert.Null(Assert.Single(h.Store.Query(T0, wake.AddMinutes(1)).Gaps).PlugKwh);
    }

    [Fact]
    public void AStopAndRestartIsFilledFromTheCounterSavedWithTheLastSample()
    {
        using var h = new TempHistory();
        var pc = new PcWithPlug();
        var first = Make(h, pc);
        first.Start(T0);
        var wh = RunAwake(first, pc, T0, 30, startWh: 1000);
        first.Stop();                                                    // shutdown: the PC is off, the plug still counts 3 W of standby

        var later = T0.AddSeconds(30).AddHours(10);
        pc.Counter = null;                                               // a new run: the plug source has not asked yet
        var second = Make(h, pc);
        second.Start(later);
        second.Tick(later);
        pc.Counter = new WallCounter(Host, wh + 3.0 * 10, later.AddSeconds(1));
        second.Tick(later.AddSeconds(1));

        var gap = Assert.Single(h.Store.Query(T0, later.AddMinutes(1)).Gaps);
        Assert.Equal(GapReason.Off, gap.Reason);
        Assert.Equal(0.030, gap.PlugKwh!.Value, precision: 6);
    }

    [Fact]
    public void ARestartAfterALostPowerGapIsFilledToo()
    {
        using var h = new TempHistory();
        var pc = new PcWithPlug();
        var first = Make(h, pc);
        first.Start(T0);
        var wh = RunAwake(first, pc, T0, 30, startWh: 1000);             // no Stop(): the power was cut

        var later = T0.AddSeconds(30).AddHours(2);
        var second = Make(h, pc);
        second.Start(later);
        pc.Counter = new WallCounter(Host, wh + 1.5, later);
        second.Tick(later);

        var gap = Assert.Single(h.Store.Query(T0, later.AddMinutes(1)).Gaps);
        Assert.Equal(GapReason.LostPower, gap.Reason);
        Assert.Equal(0.0015, gap.PlugKwh!.Value, precision: 6);
    }

    [Fact]
    public void ARestartWithNoCounterSavedLeavesTheGapAsItWas()
    {
        using var h = new TempHistory();
        var pc = new PcWithPlug();                                       // never a counter
        var first = Make(h, pc);
        first.Start(T0);
        for (var i = 0; i <= 30; i++) first.Tick(T0.AddSeconds(i));
        first.Stop();

        var later = T0.AddHours(3);
        pc.Counter = new WallCounter(Host, 500, later);
        var second = Make(h, pc);
        second.Start(later);
        second.Tick(later);

        Assert.Null(Assert.Single(h.Store.Query(T0, later.AddMinutes(1)).Gaps).PlugKwh);
    }

    [Fact]
    public void NoPlugAtAllChangesNothing()
    {
        using var h = new TempHistory();
        var sampler = new PowerSampler(new IPowerSource[] { new NoPlug() }, maxGap: TimeSpan.FromMinutes(5));
        var c = new PowerCollector(h.Store, sampler);
        c.Start(T0);
        for (var i = 0; i <= 30; i++) c.Tick(T0.AddSeconds(i));
        for (var i = 0; i <= 30; i++) c.Tick(T0.AddHours(8).AddSeconds(i));

        var end = T0.AddHours(8).AddMinutes(1);
        Assert.Null(Assert.Single(h.Store.Query(T0, end).Gaps).PlugKwh);
        var report = HistoryReport.Build(h.Store, "x", T0, end, sleepWatts: 2);
        Assert.Null(report.WallKwh);                                     // no wall meter: the sensors are the headline, sleep is an estimate
        Assert.Equal(0, report.PlugGapKwh);
        Assert.True(report.EstimatedKwh > 0);
    }

    private sealed class NoPlug : IPowerSource
    {
        public string Name => "sensors only";
        public string? UnavailableReason => null;
        public IReadOnlyList<PowerReading> Read() => new[] { new PowerReading("GPU", 100, ReadingScope.Component) };
        public void Dispose() { }
    }

    [Fact]
    public void AnOlderHistoryFileWithoutTheColumnIsUpgradedAndKeepsItsGaps()
    {
        var dir = Path.Combine(Path.GetTempPath(), "kvguzzler-old-" + Guid.NewGuid().ToString("N")[..8]);
        Directory.CreateDirectory(dir);
        var path = Path.Combine(dir, HistoryStore.FileName);
        try
        {
            using (var c = new SqliteConnection(new SqliteConnectionStringBuilder { DataSource = path }.ToString()))
            {
                c.Open();
                using var cmd = c.CreateCommand();
                cmd.CommandText = $"""
                    CREATE TABLE gaps (start_ms INTEGER PRIMARY KEY, end_ms INTEGER NOT NULL, reason INTEGER NOT NULL);
                    INSERT INTO gaps VALUES ({T0.ToUnixTimeMilliseconds()}, {T0.AddHours(1).ToUnixTimeMilliseconds()}, 0);
                    """;
                cmd.ExecuteNonQuery();
            }
            SqliteConnection.ClearAllPools();

            using var store = new HistoryStore(path);
            var gap = Assert.Single(store.Query(T0, T0.AddHours(2)).Gaps);
            Assert.Null(gap.PlugKwh);
            store.SetGapPlugEnergy(T0, 0.02);
            Assert.Equal(0.02, store.PlugGapKwh(T0, T0.AddHours(2)), precision: 9);
        }
        finally
        {
            SqliteConnection.ClearAllPools();
            try { Directory.Delete(dir, recursive: true); } catch (IOException) { /* best effort */ }
        }
    }
}
