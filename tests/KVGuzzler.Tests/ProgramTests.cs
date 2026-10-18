using System.Globalization;
using Avalonia.Headless.XUnit;
using KVGuzzler.Collector;
using KVGuzzler.Core.History;
using KVGuzzler.Core.Power;
using KVGuzzler.Core.Programs;
using KVGuzzler.Views;
using Xunit;

namespace KVGuzzler.Tests;

public class ProgramAttributorTests
{
    private static double Sum(IEnumerable<ProgramEnergy> rows) => rows.Sum(r => r.CpuKwh + r.GpuKwh);
    private static ProgramEnergy Row(IEnumerable<ProgramEnergy> rows, string name) => rows.Single(r => r.Program == name);

    [Fact]
    public void OnlyThePowerAboveIdleIsSharedAndEveryKwhIsAccountedFor()
    {
        var activity = new[]
        {
            new ProgramActivity("game.exe", CpuBusy: 30, GpuBusy: 90),
            new ProgramActivity("chrome.exe", CpuBusy: 10, GpuBusy: 10),
        };

        var rows = ProgramAttributor.Attribute(cpuKwh: 1.0, cpuIdleKwh: 0.4, gpuKwh: 2.0, gpuIdleKwh: 0.5, seconds: 2, activity);

        Assert.Equal(0.4, Row(rows, ProgramAttributor.Baseline).CpuKwh, precision: 9);
        Assert.Equal(0.5, Row(rows, ProgramAttributor.Baseline).GpuKwh, precision: 9);
        Assert.Equal(0.45, Row(rows, "game.exe").CpuKwh, precision: 9);       // 0.6 above idle x 30/40
        Assert.Equal(0.15, Row(rows, "chrome.exe").CpuKwh, precision: 9);
        Assert.Equal(1.35, Row(rows, "game.exe").GpuKwh, precision: 9);       // 1.5 above idle x 90/100
        Assert.Equal(0.15, Row(rows, "chrome.exe").GpuKwh, precision: 9);
        Assert.Equal(3.0, Sum(rows), precision: 9);                           // nothing lost, nothing invented
    }

    [Fact]
    public void IdleWorthMoreThanTheEnergyMeansNoProgramIsBlamed()
    {
        var rows = ProgramAttributor.Attribute(1.0, cpuIdleKwh: 5.0, 1.0, gpuIdleKwh: 5.0, 2,
            new[] { new ProgramActivity("a.exe", 50, 50) });
        Assert.Equal(2.0, Row(rows, ProgramAttributor.Baseline).CpuKwh + Row(rows, ProgramAttributor.Baseline).GpuKwh, precision: 9);
        Assert.DoesNotContain(rows, r => r.Program == "a.exe" && r.TotalKwh > 0);
    }

    [Fact]
    public void PowerAboveIdleThatNoProgramClaimedIsKeptApart()
    {
        var rows = ProgramAttributor.Attribute(1.0, 0.4, 0.5, 0.5, 2, Array.Empty<ProgramActivity>());
        var lost = Row(rows, ProgramAttributor.Unattributed);
        Assert.Equal(0.6, lost.CpuKwh, precision: 9);
        Assert.Equal(0.0, lost.GpuKwh, precision: 9);
        Assert.Equal(1.5, Sum(rows), precision: 9);
    }

    [Fact]
    public void AProgramIsActiveOnlyAboveAThreshold()
    {
        var rows = ProgramAttributor.Attribute(1, 0, 1, 0, seconds: 2, new[]
        {
            new ProgramActivity("busy.exe", CpuBusy: 5, GpuBusy: 0),
            new ProgramActivity("gpu.exe", CpuBusy: 0, GpuBusy: 3),
            new ProgramActivity("quiet.exe", CpuBusy: 0.1, GpuBusy: 0.2),
        });
        Assert.Equal(2, Row(rows, "busy.exe").ActiveSeconds);
        Assert.Equal(2, Row(rows, "gpu.exe").ActiveSeconds);
        Assert.Equal(0, Row(rows, "quiet.exe").ActiveSeconds);
    }

    [Fact]
    public void ManyTinyProgramsAreMergedWithoutLosingEnergy()
    {
        var rows = new List<ProgramEnergy> { new(ProgramAttributor.Baseline, 1, 1, 0), new("game.exe", 5, 5, 60) };
        for (var i = 0; i < 30; i++) rows.Add(new ProgramEnergy($"tiny{i}.exe", 0.0001, 0, 2));

        var lumped = ProgramAttributor.Lump(rows, keepTop: 5);

        Assert.Equal(rows.Sum(r => r.TotalKwh), lumped.Sum(r => r.TotalKwh), precision: 9);
        Assert.Contains(lumped, r => r.Program == ProgramAttributor.Others);
        Assert.Contains(lumped, r => r.Program == "game.exe");
        Assert.Contains(lumped, r => r.Program == ProgramAttributor.Baseline);   // the special rows are never merged
        Assert.True(lumped.Count < rows.Count);
    }

    [Fact]
    public void ABigShareIsKeptEvenOutsideTheTop()
    {
        var rows = Enumerable.Range(0, 5).Select(i => new ProgramEnergy($"p{i}.exe", 1, 0, 1)).ToList();
        var lumped = ProgramAttributor.Lump(rows, keepTop: 1, minShare: 0.1);   // every program is 20%
        Assert.Equal(5, lumped.Count(r => r.Program != ProgramAttributor.Others));
        Assert.DoesNotContain(lumped, r => r.Program == ProgramAttributor.Others);
    }
}

public class IdleBaselineTests
{
    [Fact]
    public void KeepsTheLowestWindowSeen()
    {
        var b = new IdleBaseline();
        Assert.Null(b.CpuWatts);
        Assert.True(b.Observe(40, 12));
        Assert.True(b.Observe(30, 15));      // CPU lower, GPU not
        Assert.False(b.Observe(35, 20));     // nothing lower
        Assert.Equal(30, b.CpuWatts);
        Assert.Equal(12, b.GpuWatts);
    }

    [Fact]
    public void IgnoresNonsense()
    {
        var b = new IdleBaseline(30, 10);
        Assert.False(b.Observe(double.NaN, -5));
        Assert.Equal((30, 10), (b.CpuWatts, b.GpuWatts));
    }
}

public class WindowsProgramNameTests
{
    [Theory]
    [InlineData("chrome", "chrome.exe")]
    [InlineData("chrome#3", "chrome.exe")]          // renumbered duplicates are the same program
    [InlineData("svchost#12", "svchost.exe")]
    [InlineData("System", "System")]
    [InlineData("Idle", null)]                       // idle time is not a program
    [InlineData("_Total", null)]
    [InlineData("", null)]
    public void NamesTheProgram(string instance, string? expected)
    {
        if (!OperatingSystem.IsWindows()) return;
        Assert.Equal(expected, WindowsProgramActivitySource.ProgramName(instance));
    }
}

public class ProgramHistoryTests
{
    private static readonly DateTimeOffset T0 = new(2026, 10, 18, 12, 0, 0, TimeSpan.Zero);

    [Fact]
    public void ProgramMinutesAddUpAndNamesMatchIgnoringCase()
    {
        using var h = new TempHistory();
        var m = T0.ToUnixTimeSeconds();
        h.Store.AddProgramMinutes(m, new[] { new ProgramEnergy("Code.exe", 0.001, 0, 10), new ProgramEnergy("game.exe", 0.004, 0.01, 60) });
        h.Store.AddProgramMinutes(m + 60, new[] { new ProgramEnergy("code.exe", 0.002, 0, 20) });

        var rows = h.Store.QueryPrograms(T0, T0.AddMinutes(5));

        Assert.Equal(new[] { "game.exe", "Code.exe" }, rows.Select(r => r.Program));   // biggest first; one row for Code
        Assert.Equal(0.003, rows[1].CpuKwh, precision: 9);
        Assert.Equal(30, rows[1].ActiveSeconds);
    }

    [Fact]
    public void QueryProgramsOnlyCoversThePeriod()
    {
        using var h = new TempHistory();
        var m = T0.ToUnixTimeSeconds();
        h.Store.AddProgramMinutes(m - 3600, new[] { new ProgramEnergy("old.exe", 1, 0, 1) });
        h.Store.AddProgramMinutes(m, new[] { new ProgramEnergy("new.exe", 1, 0, 1) });
        Assert.Equal("new.exe", Assert.Single(h.Store.QueryPrograms(T0, T0.AddMinutes(1))).Program);
    }

    [Fact]
    public void LiveProgramsAndTheIdleBaselineAreKept()
    {
        using var h = new TempHistory();
        Assert.Null(h.Store.GetLivePrograms());
        Assert.Equal((null, null), h.Store.GetIdleBaseline());

        h.Store.SetLivePrograms(T0, new[] { new ProgramLive("game.exe", 8, 70), new ProgramLive("chrome.exe", 3, 0) });
        h.Store.SetIdleBaseline(30, 10);

        var live = h.Store.GetLivePrograms()!.Value;
        Assert.Equal(T0, live.UpdatedAt);
        Assert.Equal(new[] { "game.exe", "chrome.exe" }, live.Rows.Select(r => r.Program));
        Assert.Equal((30.0, 10.0), h.Store.GetIdleBaseline());
    }
}

public class ProgramCollectorTests
{
    private static readonly DateTimeOffset T0 = new(2026, 10, 18, 12, 0, 0, TimeSpan.Zero);

    private sealed class Hardware : IPowerSource
    {
        public double CpuWatts = 30, GpuWatts = 10;
        public string Name => "hardware";
        public string? UnavailableReason => null;
        public IReadOnlyList<PowerReading> Read() => new[]
        {
            new PowerReading("CPU package", CpuWatts, ReadingScope.Component, PowerKind.Cpu),
            new PowerReading("GPU", GpuWatts, ReadingScope.Component, PowerKind.Gpu),
        };
        public void Dispose() { }
    }

    private sealed class Activity : IProgramActivitySource
    {
        public IReadOnlyList<ProgramActivity> Current = Array.Empty<ProgramActivity>();
        public string? Unavailable { get; set; }
        public int Resets { get; private set; }
        public string? UnavailableReason => Unavailable;
        public IReadOnlyList<ProgramActivity> Sample() => Current;
        public void Reset() => Resets++;
        public void Dispose() { }
    }

    private static PowerCollector Make(TempHistory h, Hardware hw, Activity a) =>
        new(h.Store, new PowerSampler(new IPowerSource[] { hw }, maxGap: TimeSpan.FromMinutes(10)), a);

    [Fact]
    public void LoadIsBlamedOnTheProgramsThatCausedItAndIdleIsNot()
    {
        using var h = new TempHistory();
        var hw = new Hardware();
        var activity = new Activity();
        var c = Make(h, hw, activity);
        c.Start(T0);

        // A quiet minute first: the idle baseline is learned from it (CPU 30 W, GPU 10 W).
        for (var i = 0; i <= 60; i++) c.Tick(T0.AddSeconds(i));

        // Then two minutes of load: a game on both CPU and GPU, a browser a little on the CPU.
        hw.CpuWatts = 80;
        hw.GpuWatts = 110;
        activity.Current = new[]
        {
            new ProgramActivity("game.exe", CpuBusy: 60, GpuBusy: 90),
            new ProgramActivity("other.exe", CpuBusy: 20, GpuBusy: 0),
        };
        for (var i = 61; i <= 180; i++) c.Tick(T0.AddSeconds(i));
        c.Stop();

        var end = T0.AddMinutes(4);
        var rows = h.Store.QueryPrograms(T0, end).ToDictionary(r => r.Program);

        // 50 W above idle on the CPU split 3:1, and 100 W above idle on the GPU all to the game, for 120 s.
        const double kwhPerWattSecond = 1 / 3_600_000.0;
        Assert.InRange(rows["game.exe"].TotalKwh, (37.5 + 100) * 120 * kwhPerWattSecond * 0.92, (37.5 + 100) * 120 * kwhPerWattSecond * 1.08);
        Assert.InRange(rows["other.exe"].TotalKwh, 12.5 * 120 * kwhPerWattSecond * 0.85, 12.5 * 120 * kwhPerWattSecond * 1.15);
        Assert.True(rows["game.exe"].ActiveSeconds > 100);
        Assert.Equal(0, rows["other.exe"].GpuKwh);

        // Idle belongs to nobody, and the baseline was kept for the next run.
        Assert.True(rows[ProgramAttributor.Baseline].TotalKwh > 0);
        var (cpuIdle, gpuIdle) = h.Store.GetIdleBaseline();
        Assert.Equal(30, cpuIdle!.Value, precision: 6);
        Assert.Equal(10, gpuIdle!.Value, precision: 6);

        // Nothing lost: the programs and the baseline together are the CPU and GPU energy that was measured
        // (to within the couple of seconds still waiting to be shared out when it stopped).
        var measured = h.Store.Query(T0, end).KilowattHours;
        Assert.InRange(rows.Values.Sum(r => r.TotalKwh), measured * 0.97, measured * 1.001);

        // Right now: the live list leads with the game.
        Assert.Equal("game.exe", h.Store.GetLivePrograms()!.Value.Rows[0].Program);
    }

    [Fact]
    public void WithoutAKnownBaselineNothingIsBlamedOnPrograms()
    {
        using var h = new TempHistory();
        var hw = new Hardware { CpuWatts = 80, GpuWatts = 110 };
        var activity = new Activity { Current = new[] { new ProgramActivity("game.exe", 60, 90) } };
        var c = Make(h, hw, activity);
        c.Start(T0);
        for (var i = 0; i <= 20; i++) c.Tick(T0.AddSeconds(i));   // busy from the very first second, under 30 s of history
        c.Stop();

        var rows = h.Store.QueryPrograms(T0, T0.AddMinutes(2));
        Assert.DoesNotContain(rows, r => r.Program == "game.exe" && r.TotalKwh > 0);
        Assert.True(rows.Single(r => r.Program == ProgramAttributor.Baseline).TotalKwh > 0);
    }

    [Fact]
    public void ABaselineFromAnEarlierRunIsUsedAtOnce()
    {
        using var h = new TempHistory();
        h.Store.SetIdleBaseline(30, 10);
        var hw = new Hardware { CpuWatts = 80, GpuWatts = 110 };
        var activity = new Activity { Current = new[] { new ProgramActivity("game.exe", 60, 90) } };
        var c = Make(h, hw, activity);
        c.Start(T0);
        for (var i = 0; i <= 20; i++) c.Tick(T0.AddSeconds(i));
        c.Stop();

        Assert.True(h.Store.QueryPrograms(T0, T0.AddMinutes(2)).Single(r => r.Program == "game.exe").TotalKwh > 0);
    }

    [Fact]
    public void SleepStartsTheProgramReadingOver()
    {
        using var h = new TempHistory();
        var activity = new Activity();
        var c = Make(h, new Hardware(), activity);
        c.Start(T0);
        for (var i = 0; i <= 10; i++) c.Tick(T0.AddSeconds(i));
        c.Tick(T0.AddHours(8));   // the machine slept
        Assert.Equal(1, activity.Resets);
    }

    [Fact]
    public void AProgramSourceThatCannotRunIsSaidSoAndEnergyRecordingContinues()
    {
        using var h = new TempHistory();
        var activity = new Activity { Unavailable = "Windows does not publish per-program CPU counters on this machine." };
        var c = Make(h, new Hardware(), activity);
        c.Start(T0);
        for (var i = 0; i <= 10; i++) c.Tick(T0.AddSeconds(i));

        Assert.Contains("Per-program power: Windows does not publish", Assert.Single(h.Store.GetLive()!.Notes));
        c.Stop();
        Assert.True(h.Store.Query(T0, T0.AddMinutes(1)).KilowattHours > 0);   // the totals are still recorded
    }
}

public class EnergyFormatTests
{
    [Theory]
    [InlineData(0.0014, "1.4 Wh")]
    [InlineData(0.0, "0 Wh")]
    [InlineData(0.0099, "9.9 Wh")]
    [InlineData(0.01, "0.01 kWh")]
    [InlineData(0.5, "0.5 kWh")]
    [InlineData(12.3456, "12.346 kWh")]
    public void SmallAmountsReadInWattHours(double kwh, string expected)
    {
        CultureInfo.CurrentCulture = CultureInfo.InvariantCulture;
        Assert.Equal(expected, MainWindow.FormatEnergy(kwh));
    }
}

public class ProgramsUiTests
{
    private static void UseInvariantCulture() => CultureInfo.CurrentCulture = CultureInfo.InvariantCulture;

    [AvaloniaFact]
    public async Task TheProgramsTabListsWhoUsedThePowerAndWhatItCost()
    {
        UseInvariantCulture();
        using var t = new TempData();
        t.Services.Database!.SetSetting(KVGuzzler.Data.SettingKeys.RatePerKwh, "0.20");
        t.Services.Database.SetSetting(KVGuzzler.Data.SettingKeys.Currency, "$");
        t.Services.QueryCollectorService = () => ServiceState.Running;

        using (var store = new HistoryStore(Path.Combine(t.HistoryDir, HistoryStore.FileName)))
        {
            var now = DateTimeOffset.UtcNow;
            var minute = now.AddMinutes(-30).ToUnixTimeSeconds() / 60 * 60;
            store.AddProgramMinutes(minute, new[]
            {
                new ProgramEnergy(ProgramAttributor.Baseline, 0.2, 0.1, 0),
                new ProgramEnergy("chrome.exe", 0.05, 0, 600),
                new ProgramEnergy("game.exe", 0.1, 0.4, 3600),
            });
            store.SetLive(now, new[] { new PowerReading("CPU package", 80, ReadingScope.Component, PowerKind.Cpu) }, Array.Empty<string>());
            store.SetLivePrograms(now, new[] { new ProgramLive("game.exe", 37.5, 100), new ProgramLive(ProgramAttributor.Baseline, 30, 10) });
            store.SetIdleBaseline(30, 10);
        }

        var window = new MainWindow(t.Services);
        window.Show();
        await window.RefreshCollectorAsync(includeService: true);

        var rows = Assert.IsAssignableFrom<IEnumerable<HistoryRow>>(window.ProgramsList.ItemsSource).ToList();
        Assert.Equal(new[] { "game.exe: 0.5 kWh, $0.10", "chrome.exe: 0.05 kWh, $0.01", "(system baseline): 0.3 kWh, $0.06" },
            rows.Select(r => r.Title));                                          // programs by energy, the baseline last
        Assert.Equal("GPU 0.4 kWh · CPU 0.1 kWh · active 1 h 0 min", rows[0].Detail);
        Assert.Contains("idle power", rows[2].Detail);

        Assert.Equal("CPU and GPU together: 0.85 kWh, $0.17", window.ProgramsTotalText.Text);
        Assert.Contains("CPU 30 W, GPU 10 W", window.ProgramsBaselineText.Text);

        var live = Assert.IsAssignableFrom<IEnumerable<HistoryRow>>(window.LiveProgramsList.ItemsSource).ToList();
        Assert.Equal("game.exe: 137.5 W", live[0].Title);
        Assert.Equal("GPU 100.0 W · CPU 37.5 W", live[0].Detail);
    }

    [AvaloniaFact]
    public async Task WithoutHistoryItPointsToBackgroundRecording()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();
        await window.RefreshCollectorAsync(includeService: true);
        Assert.Contains("background collector", window.ProgramsNoteText.Text);
    }
}
