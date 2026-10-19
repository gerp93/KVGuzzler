using System.Globalization;
using Avalonia.Controls;
using Avalonia.Headless.XUnit;
using KVGuzzler.Collector;
using KVGuzzler.Core.Billing;
using KVGuzzler.Core.History;
using KVGuzzler.Core.Power;
using KVGuzzler.Views;
using Xunit;

namespace KVGuzzler.Tests;

public class PeripheralRenameStoreTests
{
    private static readonly DateTimeOffset T0 = new(2026, 10, 18, 12, 0, 0, TimeSpan.Zero);

    [Fact]
    public void RenamingMovesTheSavedMinutesAndGapEnergyToTheNewName()
    {
        using var h = new TempHistory();
        var m = T0.ToUnixTimeSeconds();
        h.Store.AddMinutes(new[]
        {
            new MinuteRow(m, "Monitor", ReadingScope.Peripheral, 0.005, 60, 40),
            new MinuteRow(m + 60, "Monitor", ReadingScope.Peripheral, 0.001, 60, 55),
            new MinuteRow(m, "Speakers", ReadingScope.Peripheral, 0.002, 60, 15),
        });
        h.Store.AddGap(new Gap(T0.AddMinutes(5), T0.AddMinutes(65), GapReason.Asleep));
        h.Store.SetGapPeripheralEnergy(T0.AddMinutes(5), "Monitor", 0.03);

        h.Store.RenamePeripheral("Monitor", "Desk monitor");

        var use = h.Store.QueryPeripherals(T0, T0.AddHours(2));
        Assert.Equal(new[] { "Desk monitor", "Speakers" }, use.Select(u => u.Name));
        Assert.Equal(0.006 + 0.03, use[0].KilowattHours, precision: 9);
        Assert.Equal(55, use[0].PeakWatts);
        Assert.Equal(0, h.Store.PeripheralKwh(T0, T0.AddHours(2), new[] { "Monitor" }));
    }

    [Fact]
    public void RenamingOntoAnExistingNameAddsToItAndRunningItAgainChangesNothing()
    {
        using var h = new TempHistory();
        var m = T0.ToUnixTimeSeconds();
        h.Store.AddMinutes(new[]
        {
            new MinuteRow(m, "Old", ReadingScope.Peripheral, 0.005, 60, 40),
            new MinuteRow(m, "New", ReadingScope.Peripheral, 0.001, 30, 90),   // the same minute: the two are added
        });

        h.Store.RenamePeripheral("Old", "New");
        h.Store.RenamePeripheral("Old", "New");   // the sweep that follows a rename

        var only = Assert.Single(h.Store.QueryPeripherals(T0, T0.AddHours(1)));
        Assert.Equal("New", only.Name);
        Assert.Equal(0.006, only.KilowattHours, precision: 9);
        Assert.Equal(90, only.PeakWatts);
    }

    [Fact]
    public void ARenameOnlyTouchesExtraMeters()
    {
        using var h = new TempHistory();
        var m = T0.ToUnixTimeSeconds();
        h.Store.AddMinutes(new[] { new MinuteRow(m, "GPU", ReadingScope.Component, 0.004, 60, 90) });
        h.Store.RenamePeripheral("GPU", "Something");
        Assert.Equal(0.004, h.Store.Query(T0, T0.AddHours(1)).KilowattHours, precision: 9);   // a sensor of the same name is left alone
        Assert.Empty(h.Store.QueryPeripherals(T0, T0.AddHours(1)));
    }
}

public class PeripheralNotesTests : IDisposable
{
    private readonly string _dir = Path.Combine(Path.GetTempPath(), "kvguzzler-periphnotes-" + Guid.NewGuid().ToString("N")[..8]);
    private readonly FakePlug _plug = new();
    private readonly FakeClock _clock = new();
    private readonly List<IDisposable> _toDispose = new();

    public void Dispose()
    {
        foreach (var d in _toDispose) d.Dispose();
        try { Directory.Delete(_dir, recursive: true); } catch (IOException) { /* best effort */ }
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
    public async Task ASilentMeterIsNamedWithItsAddressAndWhy_AndOnlyOnceItsLastAnswerIsOld()
    {
        var path = Path.Combine(_dir, PeripheralMetersConfig.FileName);
        new PeripheralMetersConfig(new[] { new PeripheralMeter("Monitor", "192.168.1.51") }).Save(path);
        var source = new PeripheralMetersSource(path, _plug, TimeSpan.FromMilliseconds(20), _clock);
        _toDispose.Add(source);
        _ = source.Read();
        Assert.True(await Eventually(() => source.Read().Count == 1));
        Assert.Empty(source.Notes);   // answering: nothing to say

        _plug.Respond = _ => throw new HttpRequestException("No route to host");
        Assert.True(await Eventually(() => source.ProblemWith("Monitor") is not null));
        Assert.Empty(source.Notes);   // the last good answer is still recent: a missed poll is not a notice

        _clock.Advance(PeripheralMetersSource.Fresh + TimeSpan.FromSeconds(1));
        var note = Assert.Single(source.Notes);
        Assert.StartsWith("Monitor (192.168.1.51): Could not reach 192.168.1.51", note);
        Assert.Equal("Could not reach 192.168.1.51: No route to host", MainWindow.ProblemText(source.Notes, "Monitor"));
    }

    [Fact]
    public async Task TheSamplerPassesTheNotesOnNextToTheOtherSourcesReasons()
    {
        _plug.Respond = _ => throw new HttpRequestException("No route to host");
        var path = Path.Combine(_dir, PeripheralMetersConfig.FileName);
        new PeripheralMetersConfig(new[] { new PeripheralMeter("Monitor", "192.168.1.51") }).Save(path);
        var source = new PeripheralMetersSource(path, _plug, TimeSpan.FromMilliseconds(20), _clock);
        var sampler = new PowerSampler(new IPowerSource[] { source });
        _toDispose.Add(sampler);

        Assert.True(await Eventually(() => sampler.Sample(DateTimeOffset.UtcNow).Notes.Count == 1));
        Assert.Contains("Monitor (192.168.1.51)", sampler.Sample(DateTimeOffset.UtcNow).Notes[0]);
    }
}

public class PeripheralPcToggleTests
{
    private static readonly DateTimeOffset T0 = new(2026, 10, 18, 12, 0, 0, TimeSpan.Zero);
    private static readonly RateSchedule Flat = new(RateMode.OneRate, 0.20m, Array.Empty<Bill>(), TimeZoneInfo.Utc);

    private static TempHistory Seeded()
    {
        var h = new TempHistory();
        var m = T0.ToUnixTimeSeconds();
        h.Store.AddMinutes(new[]
        {
            new MinuteRow(m, WallMeterSource.ReadingName, ReadingScope.Wall, 0.01, 60, 300),
            new MinuteRow(m, "Monitor", ReadingScope.Peripheral, 0.005, 60, 40),
            new MinuteRow(m + 7200, "Monitor", ReadingScope.Peripheral, 0.003, 60, 45),
        });
        return h;
    }

    [Fact]
    public void WithoutThePcTheFiguresAreTheExtraMetersAlone()
    {
        using var h = Seeded();
        var stat = UsageSummary.Compute(h.Store, Flat, null, T0.AddHours(3), new[] { "Monitor" }, includePc: false);
        Assert.False(stat.IncludesPc);
        Assert.Equal(0.008, stat.Kwh, precision: 9);
        Assert.Equal(0.008m * 0.20m, stat.Cost!.Value, precision: 6);
        Assert.Equal(45, stat.Peak!.Watts);                 // the monitor's own, not the PC's 300 W
        Assert.Null(stat.CountedSince);
        Assert.Equal(T0.AddSeconds(0), stat.From);          // starts where the first thing was recorded

        var withPc = UsageSummary.Compute(h.Store, Flat, null, T0.AddHours(3), new[] { "Monitor" });
        Assert.True(withPc.IncludesPc);
        Assert.Equal(0.018, withPc.Kwh, precision: 9);
    }

    [Fact]
    public void TheAverageDayAndTheStartOfAllTimeFollowTheSameChoice()
    {
        using var h = Seeded();
        var m = T0.ToUnixTimeSeconds();
        h.Store.AddMinutes(new[] { new MinuteRow(m - 3600, "Speakers", ReadingScope.Peripheral, 0.001, 60, 10) });   // recorded before the PC
        var use = UsageSummary.AverageDay(h.Store, null, T0.AddHours(3), new[] { "Monitor", "Speakers" }, includePc: false)!;
        Assert.False(use.IncludesPc);
        Assert.Equal(0.009, use.Kwh, precision: 9);
        Assert.Equal(T0.AddHours(-1), use.From);   // averaged from when the extra meters began, not the PC
        Assert.Equal(4, use.Days * 24, precision: 6);
    }

    [Fact]
    public void WithNoExtraMeterRecordedThereIsNothingToShow()
    {
        using var h = new TempHistory();
        h.Store.AddMinutes(new[] { new MinuteRow(T0.ToUnixTimeSeconds(), WallMeterSource.ReadingName, ReadingScope.Wall, 0.01, 60, 300) });
        Assert.Null(UsageSummary.AverageDay(h.Store, null, T0.AddHours(3), new[] { "Monitor" }, includePc: false));
        var stat = UsageSummary.Compute(h.Store, Flat, null, T0.AddHours(3), new[] { "Monitor" }, includePc: false);
        Assert.Equal(0, stat.Kwh);
        Assert.Null(stat.Peak);
    }
}

public class PeripheralGapFillTests
{
    private static readonly DateTimeOffset T0 = new(2026, 10, 18, 12, 0, 0, TimeSpan.Zero);
    private const string MonitorHost = "192.168.1.51";
    private const double AwakeWatts = 30;
    private const double AsleepWatts = 3;

    private sealed class PcAndMonitor : IPowerSource, IPeripheralEnergyCounters
    {
        public Dictionary<string, WallCounter> Counters { get; } = new(StringComparer.OrdinalIgnoreCase);
        public string Name => "pc and monitor";
        public string? UnavailableReason => null;
        public IReadOnlyDictionary<string, WallCounter> LatestCounters => Counters;
        public IReadOnlyList<PowerReading> Read() => new[]
        {
            new PowerReading("GPU", 100, ReadingScope.Component),
            new PowerReading("Monitor", AwakeWatts, ReadingScope.Peripheral),
        };
        public void Dispose() { }
    }

    private static PowerCollector Make(TempHistory h, PcAndMonitor src) =>
        new(h.Store, new PowerSampler(new IPowerSource[] { src }, maxGap: TimeSpan.FromMinutes(5)));

    private static double RunAwake(PowerCollector c, PcAndMonitor src, DateTimeOffset from, int seconds, double startWh, string host = MonitorHost)
    {
        for (var i = 0; i <= seconds; i++)
        {
            var at = from.AddSeconds(i);
            src.Counters["Monitor"] = new WallCounter(host, startWh + AwakeWatts * i / 3600.0, at);
            c.Tick(at);
        }
        return startWh + AwakeWatts * seconds / 3600.0;
    }

    [Fact]
    public void AMonitorThatKeepsCountingWhileThePcSleepsIsCountedForTheGap()
    {
        using var h = new TempHistory();
        var src = new PcAndMonitor();
        var c = Make(h, src);
        c.Start(T0);
        var wh = RunAwake(c, src, T0, 30, startWh: 500);

        var slept = TimeSpan.FromHours(8);
        var wake = T0.AddSeconds(30) + slept;
        wh += AsleepWatts * slept.TotalHours;   // 24 Wh while asleep
        RunAwake(c, src, wake, 30, startWh: wh);
        c.Stop();

        var end = wake.AddMinutes(1);
        var sampled = AwakeWatts * 60 / 3_600_000.0;
        var use = Assert.Single(h.Store.QueryPeripherals(T0, end));
        Assert.Equal(0.024, use.GapKwh, precision: 6);
        Assert.Equal(sampled + 0.024, use.KilowattHours, precision: 6);
        Assert.Equal(sampled + 0.024, h.Store.PeripheralKwh(T0, end, new[] { "Monitor" }), precision: 6);

        // Half the sleep gets half of its energy; the sampled part is untouched.
        var halfway = T0.AddSeconds(30) + slept / 2;
        Assert.Equal(0.012, h.Store.QueryPeripherals(T0.AddSeconds(30), halfway).Single().GapKwh, precision: 6);

        // The sensors and the PC's own figures know nothing of it.
        Assert.Equal(100.0 * 60 / 3_600_000.0, h.Store.Query(T0, end).KilowattHours, precision: 6);
        Assert.Equal(TimeSpan.FromSeconds(60), h.Store.Query(T0, end).Monitored);
    }

    [Fact]
    public void ACounterThatWentDownOrCameFromAnotherPlugLeavesTheGapUnfilled()
    {
        foreach (var (host, dropsBy) in new[] { (MonitorHost, 200.0), ("192.168.1.99", 0.0) })
        {
            using var h = new TempHistory();
            var src = new PcAndMonitor();
            var c = Make(h, src);
            c.Start(T0);
            var wh = RunAwake(c, src, T0, 30, startWh: 500);
            var wake = T0.AddSeconds(30) + TimeSpan.FromHours(8);
            RunAwake(c, src, wake, 30, startWh: wh + 24 - dropsBy - 500 + (dropsBy > 0 ? 0 : 500), host: host);
            c.Stop();
            Assert.Equal(0, h.Store.QueryPeripherals(T0, wake.AddMinutes(1)).Single().GapKwh);
        }
    }

    [Fact]
    public void AMeterWithNoCounterBeforeTheGapIsNeverFilled()
    {
        using var h = new TempHistory();
        var src = new PcAndMonitor();
        var c = Make(h, src);
        c.Start(T0);
        for (var i = 0; i <= 30; i++) c.Tick(T0.AddSeconds(i));   // no counter was ever reported
        var wake = T0.AddSeconds(30) + TimeSpan.FromHours(8);
        RunAwake(c, src, wake, 30, startWh: 900);
        c.Stop();
        Assert.Equal(0, h.Store.QueryPeripherals(T0, wake.AddMinutes(1)).Single().GapKwh);
    }

    [Fact]
    public void TheCountersSurviveTheCollectorBeingStoppedAndStartedLater()
    {
        using var h = new TempHistory();
        var first = new PcAndMonitor();
        var c1 = Make(h, first);
        c1.Start(T0);
        var wh = RunAwake(c1, first, T0, 30, startWh: 500);
        c1.Stop();
        Assert.Equal(wh, h.Store.GetPeripheralCounters()["Monitor"].TotalWh, precision: 6);   // saved with the last reading

        // Three hours later a new collector starts; the monitor's plug has been counting all along.
        var restart = T0.AddSeconds(30).AddHours(3);
        var second = new PcAndMonitor();
        var c2 = Make(h, second);
        c2.Start(restart);
        RunAwake(c2, second, restart, 30, startWh: wh + AsleepWatts * 3);
        c2.Stop();

        var gap = Assert.Single(h.Store.Query(T0, restart.AddMinutes(1)).Gaps);
        Assert.Equal(GapReason.Off, gap.Reason);
        Assert.Equal(0.009, h.Store.QueryPeripherals(T0, restart.AddMinutes(1)).Single().GapKwh, precision: 6);
    }
}

public class HistoryExportTests
{
    private static readonly DateTimeOffset T0 = new(2026, 10, 18, 12, 0, 0, TimeSpan.Zero);
    private static readonly RateSchedule Flat = new(RateMode.OneRate, 0.20m, Array.Empty<Bill>(), TimeZoneInfo.Utc);

    private static string[] Lines(string csv) => csv.Split(new[] { "\r\n", "\n" }, StringSplitOptions.RemoveEmptyEntries);

    [Fact]
    public void OneRowPerDayWithTheWholePcSensorsEachMeterAndTheCosts()
    {
        using var h = new TempHistory();
        var m = T0.ToUnixTimeSeconds();
        h.Store.AddMinutes(new[]
        {
            new MinuteRow(m, WallMeterSource.ReadingName, ReadingScope.Wall, 0.01, 60, 300),
            new MinuteRow(m, "GPU", ReadingScope.Component, 0.004, 60, 90),
            new MinuteRow(m, "Monitor", ReadingScope.Peripheral, 0.005, 60, 40),
            new MinuteRow(m + 86400, WallMeterSource.ReadingName, ReadingScope.Wall, 0.02, 60, 300),
        });

        var csv = Lines(HistoryExport.DailyCsv(h.Store, Flat, T0, T0.AddDays(1).AddHours(1), TimeZoneInfo.Utc));
        Assert.Equal("date,whole_pc_kwh,sensors_kwh,Monitor (kwh),pc_cost,peripherals_cost", csv[0]);
        Assert.Equal("2026-10-18,0.01,0.004,0.005,0.0020,0.0010", csv[1]);
        Assert.Equal("2026-10-19,0.02,0,0,0.0040,0.0000", csv[2]);
        Assert.Equal(3, csv.Length);
    }

    [Fact]
    public void WithoutAWallMeterOrAPriceTheCellsAreBlankNotMadeUp()
    {
        using var h = new TempHistory();
        h.Store.AddMinutes(new[] { new MinuteRow(T0.ToUnixTimeSeconds(), "GPU", ReadingScope.Component, 0.004, 60, 90) });
        var noPrice = new RateSchedule(RateMode.OneRate, null, Array.Empty<Bill>(), TimeZoneInfo.Utc);

        var csv = Lines(HistoryExport.DailyCsv(h.Store, noPrice, T0, T0.AddHours(1), TimeZoneInfo.Utc));
        Assert.Equal("date,whole_pc_kwh,sensors_kwh,pc_cost,peripherals_cost", csv[0]);
        Assert.Equal("2026-10-18,,0.004,,", csv[1]);
    }

    [Fact]
    public void ALabelACellCouldRunAsAFormulaIsExportedAsText_AndCommasAreQuoted()
    {
        using var h = new TempHistory();
        h.Store.AddMinutes(new[]
        {
            new MinuteRow(T0.ToUnixTimeSeconds(), "=HYPERLINK(\"http://x\")", ReadingScope.Peripheral, 0.001, 60, 5),
            new MinuteRow(T0.ToUnixTimeSeconds(), "Desk, left", ReadingScope.Peripheral, 0.0005, 60, 5),
        });

        var header = Lines(HistoryExport.DailyCsv(h.Store, Flat, T0, T0.AddHours(1), TimeZoneInfo.Utc))[0];
        Assert.Contains("\"'=HYPERLINK(\"\"http://x\"\") (kwh)\"", header);
        Assert.Contains("\"Desk, left (kwh)\"", header);
        Assert.DoesNotContain(",=HYPERLINK", header);
    }

    [Fact]
    public void DaysStartAtLocalMidnightAndTheNumbersIgnoreTheComputersLanguage()
    {
        var old = CultureInfo.CurrentCulture;
        CultureInfo.CurrentCulture = new CultureInfo("de-DE");
        try
        {
            using var h = new TempHistory();
            var zone = TimeZoneInfo.CreateCustomTimeZone("test", TimeSpan.FromHours(-5), "test", "test");
            // 02:00 UTC on the 19th is still the evening of the 18th at UTC-5.
            var evening = new DateTimeOffset(2026, 10, 19, 2, 0, 0, TimeSpan.Zero);
            h.Store.AddMinutes(new[] { new MinuteRow(evening.ToUnixTimeSeconds(), "GPU", ReadingScope.Component, 0.0042, 60, 90) });

            var csv = Lines(HistoryExport.DailyCsv(h.Store, Flat, evening.AddHours(-1), evening.AddHours(1), zone));
            Assert.Equal("2026-10-18,,0.0042,0.0008,0.0000", csv[1]);   // a dot, and the 18th
        }
        finally { CultureInfo.CurrentCulture = old; }
    }
}

public class PeripheralFollowupUiTests
{
    private static void UseInvariantCulture() => CultureInfo.CurrentCulture = CultureInfo.InvariantCulture;
    private static string ConfigPath(TempData t) => Path.Combine(t.HistoryDir, PeripheralMetersConfig.FileName);

    private static async Task<bool> Eventually(Func<bool> condition)
    {
        for (var i = 0; i < 200; i++)
        {
            if (condition()) return true;
            await Task.Delay(25, TestContext.Current.CancellationToken);
        }
        return false;
    }

    [AvaloniaFact]
    public async Task RenamingKeepsTheHistoryTheSwitchedOffChoiceAndRefusesTakenNames()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var monitor = new PeripheralMeter("Monitor", "192.168.1.51");
        new PeripheralMetersConfig(new[] { monitor, new PeripheralMeter("Speakers", "192.168.1.52") }).Save(ConfigPath(t));
        var minute = DateTimeOffset.UtcNow.AddMinutes(-30).ToUnixTimeSeconds() / 60 * 60;
        using (var store = new HistoryStore(Path.Combine(t.HistoryDir, HistoryStore.FileName)))
            store.AddMinutes(new[] { new MinuteRow(minute, "Monitor", ReadingScope.Peripheral, 0.02, 60, 40) });
        t.Services.Database!.SetSetting(KVGuzzler.Data.SettingKeys.PeripheralsOff, "Monitor");   // switched off before

        var window = new MainWindow(t.Services);
        window.Show();
        MainWindow.RenameSweepDelay = TimeSpan.FromMilliseconds(50);

        window.RenamePeripheral(monitor, "speakers");
        Assert.Contains("already a meter called", window.PeripheralStatusText.Text);
        window.RenamePeripheral(monitor, "  ");
        Assert.Contains("Give the meter a name", window.PeripheralStatusText.Text);
        Assert.Equal("Monitor", PeripheralMetersConfig.Load(ConfigPath(t)).Meters[0].Label);

        window.RenamePeripheral(monitor, "Desk monitor");
        Assert.StartsWith("Renamed Monitor to Desk monitor", window.PeripheralStatusText.Text);
        var meters = PeripheralMetersConfig.Load(ConfigPath(t)).Meters;
        Assert.Equal(new[] { "Desk monitor", "Speakers" }, meters.Select(m => m.Label));   // same place in the list, same plug
        Assert.Equal("192.168.1.51", meters[0].Host);
        var boxes = window.PeripheralFilterPanel.Children.OfType<CheckBox>().ToList();
        Assert.Equal("Desk monitor", boxes[0].Content);
        Assert.False(boxes[0].IsChecked);   // it was switched off, and still is
        Assert.True(boxes[1].IsChecked);

        using var check = new HistoryStore(Path.Combine(t.HistoryDir, HistoryStore.FileName));
        Assert.True(await Eventually(() => check.QueryPeripherals(DateTimeOffset.UtcNow.AddHours(-1), DateTimeOffset.UtcNow).Select(u => u.Name).SequenceEqual(new[] { "Desk monitor" })),
            "the history was not moved to the new name");
    }

    [AvaloniaFact]
    public async Task UntickingThePcShowsTheExtraMetersAlone()
    {
        UseInvariantCulture();
        using var t = new TempData();
        t.Services.QueryCollectorService = () => ServiceState.Running;
        new PeripheralMetersConfig(new[] { new PeripheralMeter("Monitor", "192.168.1.51") }).Save(ConfigPath(t));
        using (var store = new HistoryStore(Path.Combine(t.HistoryDir, HistoryStore.FileName)))
        {
            var minute = DateTimeOffset.UtcNow.AddMinutes(-30).ToUnixTimeSeconds() / 60 * 60;
            store.AddMinutes(new[]
            {
                new MinuteRow(minute, WallMeterSource.ReadingName, ReadingScope.Wall, 0.100, 60, 300),
                new MinuteRow(minute, "Monitor", ReadingScope.Peripheral, 0.020, 60, 40),
            });
            store.SetLive(DateTimeOffset.UtcNow, new List<PowerReading>
            {
                new(WallMeterSource.ReadingName, 200, ReadingScope.Wall),
                new("Monitor", 30, ReadingScope.Peripheral),
            }, Array.Empty<string>());
        }

        var window = new MainWindow(t.Services);
        window.Show();
        await window.RefreshCollectorAsync(includeService: true);
        Assert.Equal("0.12 kWh", window.StatEnergyText.Text);
        Assert.Equal("230.0 W", window.NowWattsText.Text);

        window.CountPcBox.IsChecked = false;
        await window.RefreshCollectorAsync(includeService: false);
        Assert.Equal("0.02 kWh", window.StatEnergyText.Text);
        Assert.Contains("only (the PC is not counted)", window.StatEnergyCaption.Text);
        Assert.Equal("Max draw · peripherals only", window.StatMaxLabel.Text);
        Assert.Equal("40.0 W", window.StatMaxText.Text);
        Assert.Equal("Right now · peripherals only", window.NowLabelText.Text);
        Assert.Equal("30.0 W", window.NowWattsText.Text);
        Assert.Equal("Whole PC at the wall: 200.0 W", window.WallPowerText.Text);   // the PC's own live line is unchanged

        // Remembered by a fresh window; and with the monitor unticked too there is nothing to total.
        Assert.False(new MainWindow(t.Services).CountPcBox.IsChecked);
        window.PeripheralFilterPanel.Children.OfType<CheckBox>().Single().IsChecked = false;
        await window.RefreshCollectorAsync(includeService: false);
        Assert.Equal("-", window.NowWattsText.Text);
        Assert.Equal("0 Wh", window.StatEnergyText.Text);
    }

    [AvaloniaFact]
    public async Task ExportWritesTheChosenPeriodAsACsvAndSaysSo()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();
        var path = Path.Combine(t.Dir, "out.csv");

        await window.ExportHistoryAsync(path);
        Assert.Equal("There is no history to export yet.", window.ExportStatusText.Text);   // no history file yet: nothing is made up
        Assert.False(File.Exists(path));

        var minute = DateTimeOffset.UtcNow.AddMinutes(-30).ToUnixTimeSeconds() / 60 * 60;
        using (var store = new HistoryStore(Path.Combine(t.HistoryDir, HistoryStore.FileName)))
            store.AddMinutes(new[] { new MinuteRow(minute, "Monitor", ReadingScope.Peripheral, 0.02, 60, 40) });
        window = new MainWindow(t.Services);
        window.Show();
        await window.ExportHistoryAsync(path);

        Assert.StartsWith("Saved out.csv", window.ExportStatusText.Text);
        var text = await File.ReadAllTextAsync(path, TestContext.Current.CancellationToken);
        Assert.Contains("date,whole_pc_kwh,sensors_kwh,Monitor (kwh)", text);
        Assert.Contains(",0.02,", text);
        Assert.Equal(0xEF, File.ReadAllBytes(path)[0]);   // a byte-order mark, so Excel reads it as UTF-8
    }
}
