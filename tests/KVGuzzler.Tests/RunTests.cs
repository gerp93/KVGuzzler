using System.Globalization;
using KVGuzzler.Core.History;
using KVGuzzler.Core.Power;
using KVGuzzler.Core.Programs;
using KVGuzzler.Views;
using Xunit;

namespace KVGuzzler.Tests;

public class RunTrackerTests
{
    private static readonly DateTimeOffset T0 = new(2026, 10, 18, 20, 0, 0, TimeSpan.Zero);
    private static ProgramEnergy Used(string name, double kwh, double active = 2) => new(name, kwh, 0, active);
    private static readonly string[] Nothing = Array.Empty<string>();

    [Fact]
    public void ARunIsEverythingBetweenWhenItStartedAndWhenItWasGone()
    {
        var t = new RunTracker();
        for (var i = 0; i < 100; i++)
            Assert.Empty(t.Update(T0.AddSeconds(i * 2), new[] { "game.exe" }, new[] { Used("game.exe", 0.0001) }));

        var ended = t.Update(T0.AddSeconds(200 + 12), Nothing, Array.Empty<ProgramEnergy>());   // gone for over ten seconds

        var run = Assert.Single(ended);
        Assert.Equal("game.exe", run.Program);
        Assert.Equal(T0, run.Start);
        Assert.Equal(T0.AddSeconds(198), run.End);                    // the last time it was seen, not when it was noticed missing
        Assert.Equal(0.01, run.TotalKwh, precision: 9);
        Assert.Equal(200, run.ActiveSeconds, precision: 6);
        Assert.False(run.StillRunning);
        Assert.Empty(t.Current());
    }

    [Fact]
    public void AnOpenButIdleProgramIsStillOneRun()
    {
        var t = new RunTracker();
        t.Update(T0, new[] { "game.exe" }, new[] { Used("game.exe", 0.001) });
        for (var i = 1; i <= 50; i++)
            t.Update(T0.AddSeconds(i * 2), new[] { "game.exe" }, Array.Empty<ProgramEnergy>());   // running, doing nothing

        var run = Assert.Single(t.Current());
        Assert.Equal(T0, run.Start);
        Assert.True(run.StillRunning);
    }

    [Fact]
    public void OneMissedSampleIsNotAnExit()
    {
        var t = new RunTracker();
        t.Update(T0, new[] { "game.exe" }, new[] { Used("game.exe", 0.001) });
        Assert.Empty(t.Update(T0.AddSeconds(4), Nothing, Array.Empty<ProgramEnergy>()));   // not seen for 4 s
        t.Update(T0.AddSeconds(6), new[] { "game.exe" }, new[] { Used("game.exe", 0.001) });
        Assert.Equal(T0, Assert.Single(t.Current()).Start);                                 // same run
    }

    [Fact]
    public void StartingAgainIsANewRun()
    {
        var t = new RunTracker();
        t.Update(T0, new[] { "game.exe" }, new[] { Used("game.exe", 0.001) });
        var first = Assert.Single(t.Update(T0.AddSeconds(30), Nothing, Array.Empty<ProgramEnergy>()));
        t.Update(T0.AddMinutes(10), new[] { "game.exe" }, new[] { Used("game.exe", 0.002) });

        var second = Assert.Single(t.Current());
        Assert.Equal(T0, first.Start);
        Assert.Equal(T0.AddMinutes(10), second.Start);
        Assert.Equal(0.002, second.TotalKwh, precision: 9);          // nothing carried over from the first run
    }

    [Fact]
    public void RunsThatCausedAlmostNothingAreNotKept()
    {
        var t = new RunTracker();
        t.Update(T0, new[] { "notepad.exe" }, new[] { Used("notepad.exe", 0.00001) });
        Assert.Empty(t.Update(T0.AddSeconds(60), Nothing, Array.Empty<ProgramEnergy>()));
        Assert.Empty(t.Current());
    }

    [Fact]
    public void ProcessesOfOneProgramAreOneRunWhateverTheCase()
    {
        var t = new RunTracker();
        t.Update(T0, new[] { "Chrome.exe", "chrome.exe" }, new[] { Used("Chrome.exe", 0.0004), Used("chrome.exe", 0.0004) });
        Assert.Equal(0.0008, Assert.Single(t.Current()).TotalKwh, precision: 9);
    }

    [Fact]
    public void TheBaselineAndLeftoversAreNotPrograms()
    {
        var t = new RunTracker();
        t.Update(T0, Nothing, new[]
        {
            new ProgramEnergy(ProgramAttributor.Baseline, 1, 1, 0),
            new ProgramEnergy(ProgramAttributor.Unattributed, 1, 1, 0),
            new ProgramEnergy(ProgramAttributor.Others, 1, 1, 0),
        });
        Assert.Empty(t.Current());
        Assert.Empty(t.CloseAll());
    }

    [Fact]
    public void StoppingEndsEveryRunWhereItWasLastSeen()
    {
        var t = new RunTracker();
        t.Update(T0, new[] { "a.exe", "b.exe" }, new[] { Used("a.exe", 0.001), Used("b.exe", 0.001) });
        t.Update(T0.AddSeconds(2), new[] { "a.exe" }, new[] { Used("a.exe", 0.001) });

        var closed = t.CloseAll();

        Assert.Equal(2, closed.Count);
        Assert.All(closed, r => Assert.False(r.StillRunning));
        Assert.Equal(T0.AddSeconds(2), closed.Single(r => r.Program == "a.exe").End);
        Assert.Equal(T0, closed.Single(r => r.Program == "b.exe").End);
        Assert.Empty(t.Current());
    }
}

public class ProgramRunStoreAndCollectorTests
{
    private static readonly DateTimeOffset T0 = new(2026, 10, 18, 20, 0, 0, TimeSpan.Zero);

    [Fact]
    public void RunsAreSavedAgainAsTheyGrowAndListedStillRunningFirst()
    {
        using var h = new TempHistory();
        h.Store.SaveRun(new ProgramRun("old.exe", T0, T0.AddHours(1), 0.1, 0, 3600, StillRunning: false));
        h.Store.SaveRun(new ProgramRun("game.exe", T0.AddHours(2), T0.AddHours(2).AddMinutes(5), 0.01, 0.02, 300, StillRunning: true));
        h.Store.SaveRun(new ProgramRun("game.exe", T0.AddHours(2), T0.AddHours(3), 0.1, 0.3, 3600, StillRunning: true));   // the same run, grown
        h.Store.SaveRun(new ProgramRun("newer.exe", T0.AddHours(4), T0.AddHours(5), 0.2, 0, 3600, StillRunning: false));

        var runs = h.Store.GetRecentRuns(10);

        Assert.Equal(new[] { "game.exe", "newer.exe", "old.exe" }, runs.Select(r => r.Program));
        Assert.Equal(0.4, runs[0].TotalKwh, precision: 9);
        Assert.True(runs[0].StillRunning);
        Assert.Single(h.Store.GetRecentRuns(1));
    }

    [Fact]
    public void RunsLeftOpenByADeadCollectorAreEnded()
    {
        using var h = new TempHistory();
        h.Store.SaveRun(new ProgramRun("game.exe", T0, T0.AddMinutes(5), 0.1, 0, 300, StillRunning: true));
        h.Store.EndStaleRuns();
        Assert.False(h.Store.GetRecentRuns(5).Single().StillRunning);
    }

    private sealed class Hardware : IPowerSource
    {
        public string Name => "hardware";
        public string? UnavailableReason => null;
        public IReadOnlyList<PowerReading> Read() => new[]
        {
            new PowerReading("CPU package", 80, ReadingScope.Component, PowerKind.Cpu),
            new PowerReading("GPU", 110, ReadingScope.Component, PowerKind.Gpu),
        };
        public void Dispose() { }
    }

    private sealed class Games : IProgramActivitySource
    {
        public bool GameOpen;
        public string? UnavailableReason => null;
        public IReadOnlyCollection<string> RunningPrograms => GameOpen ? new[] { "game.exe", "explorer.exe" } : new[] { "explorer.exe" };
        public IReadOnlyList<ProgramActivity> Sample() =>
            GameOpen ? new[] { new ProgramActivity("game.exe", 60, 90) } : Array.Empty<ProgramActivity>();
        public void Reset() { }
        public void Dispose() { }
    }

    [Fact]
    public void ASessionIsPricedFromStartToExit()
    {
        using var h = new TempHistory();
        h.Store.SetIdleBaseline(30, 10);   // known from an earlier run, so attribution starts at once
        var games = new Games();
        var c = new PowerCollector(h.Store, new PowerSampler(new IPowerSource[] { new Hardware() }, maxGap: TimeSpan.FromMinutes(10)), games);
        c.Start(T0);

        games.GameOpen = true;
        for (var i = 0; i <= 40; i++) c.Tick(T0.AddSeconds(i));

        var midway = Assert.Single(h.Store.GetRecentRuns(5));     // visible and still running while it is going on
        Assert.True(midway.StillRunning);
        Assert.Equal("game.exe", midway.Program);

        for (var i = 41; i <= 60; i++) c.Tick(T0.AddSeconds(i));
        games.GameOpen = false;                                    // the game is closed
        for (var i = 61; i <= 90; i++) c.Tick(T0.AddSeconds(i));
        c.Stop();

        var run = Assert.Single(h.Store.GetRecentRuns(5));
        Assert.False(run.StillRunning);
        Assert.InRange((run.End - run.Start).TotalSeconds, 50, 62);
        // 150 W above idle (50 CPU + 100 GPU) for about a minute.
        Assert.InRange(run.TotalKwh, 150 * 55 / 3_600_000.0, 150 * 62 / 3_600_000.0);
    }
}

public class RunsUiTests
{
    [Avalonia.Headless.XUnit.AvaloniaFact]
    public async Task TheProgramsTabListsRecentRuns()
    {
        CultureInfo.CurrentCulture = CultureInfo.InvariantCulture;
        using var t = new TempData();
        t.Services.Database!.SetSetting(KVGuzzler.Data.SettingKeys.RatePerKwh, "0.20");
        t.Services.QueryCollectorService = () => KVGuzzler.Collector.ServiceState.Running;
        var now = DateTimeOffset.UtcNow;
        using (var store = new HistoryStore(Path.Combine(t.HistoryDir, HistoryStore.FileName)))
        {
            store.SetLive(now, new[] { new PowerReading("CPU package", 80, ReadingScope.Component, PowerKind.Cpu) }, Array.Empty<string>());
            store.AddProgramMinutes(now.AddMinutes(-5).ToUnixTimeSeconds() / 60 * 60, new[] { new ProgramEnergy("game.exe", 0.1, 0.2, 300) });
            store.SaveRun(new ProgramRun("game.exe", now.AddHours(-2), now.AddMinutes(-5), 0.1, 0.2, 7000, StillRunning: false));
        }

        var window = new MainWindow(t.Services);
        window.Show();
        await window.RefreshCollectorAsync(includeService: true);

        var rows = Assert.IsAssignableFrom<IEnumerable<HistoryRow>>(window.RunsList.ItemsSource).ToList();
        Assert.Equal("game.exe: 0.3 kWh, $0.06", Assert.Single(rows).Title);
        Assert.False(window.RunsNoteText.IsVisible);
    }

    [Avalonia.Headless.XUnit.AvaloniaFact]
    public async Task WithNoRunsItSaysSo()
    {
        CultureInfo.CurrentCulture = CultureInfo.InvariantCulture;
        using var t = new TempData();
        t.Services.QueryCollectorService = () => KVGuzzler.Collector.ServiceState.Running;
        using (var store = new HistoryStore(Path.Combine(t.HistoryDir, HistoryStore.FileName)))
            store.SetLive(DateTimeOffset.UtcNow, new[] { new PowerReading("CPU package", 80, ReadingScope.Component, PowerKind.Cpu) }, Array.Empty<string>());

        var window = new MainWindow(t.Services);
        window.Show();
        await window.RefreshCollectorAsync(includeService: true);

        Assert.Equal("No runs recorded yet.", window.RunsNoteText.Text);
    }
}

public class ProgramListTests
{
    [Fact]
    public void ProgramsThatCausedAlmostNothingAreSummedIntoOneLine()
    {
        CultureInfo.CurrentCulture = CultureInfo.InvariantCulture;
        var totals = new List<ProgramEnergy>
        {
            new("game.exe", 0.1, 0.4, 3600),
            new(ProgramAttributor.Baseline, 0.2, 0.1, 0),
        };
        for (var i = 0; i < 40; i++) totals.Add(new ProgramEnergy($"tiny{i}.exe", 0.00001, 0, 1));   // 0.01 Wh each

        var rows = MainWindow.ProgramRows(totals, 0.20m, "$");

        Assert.Equal(new[] { "game.exe: 0.5 kWh, $0.10", "40 smaller programs: 0.4 Wh, $0.0001", "(system baseline): 0.3 kWh, $0.06" },
            rows.Select(r => r.Title));
    }

    [Fact]
    public void AtMostTwentyFiveProgramsAreListedOnTheirOwn()
    {
        CultureInfo.CurrentCulture = CultureInfo.InvariantCulture;
        var totals = Enumerable.Range(0, 30).Select(i => new ProgramEnergy($"p{i}.exe", 0.01 + i * 0.001, 0, 60)).ToList();

        var rows = MainWindow.ProgramRows(totals, null, "$");

        Assert.Equal(26, rows.Count);                                        // 25 programs and one line for the other five
        Assert.StartsWith("p29.exe", rows[0].Title);                         // biggest first
        Assert.StartsWith("5 smaller programs", rows[25].Title);
    }

    [Fact]
    public void ASingleSmallProgramIsNotPluralised()
    {
        CultureInfo.CurrentCulture = CultureInfo.InvariantCulture;
        var rows = MainWindow.ProgramRows(new[] { new ProgramEnergy("one.exe", 0.00001, 0, 1) }, null, "$");
        Assert.StartsWith("1 smaller program:", Assert.Single(rows).Title);
    }
}

public class RunRowsTests
{
    [Fact]
    public void ARunReadsAsWhatItCostAndHowLong()
    {
        CultureInfo.CurrentCulture = CultureInfo.InvariantCulture;
        var start = new DateTimeOffset(2026, 10, 18, 20, 12, 0, TimeSpan.Zero);
        var rows = MainWindow.RunRows(new[]
        {
            new ProgramRun("game.exe", start, start.AddHours(2).AddMinutes(10), 0.03, 0.28, 7500, StillRunning: false),
            new ProgramRun("render.exe", start, start.AddMinutes(20), 0.002, 0.004, 1200, StillRunning: true),
        }, 0.20m, "$");

        Assert.Equal("game.exe: 0.31 kWh, $0.062", rows[0].Title);
        Assert.Contains("ran 2 h 10 min", rows[0].Detail);
        Assert.Contains("GPU 0.28 kWh · CPU 0.03 kWh", rows[0].Detail);
        Assert.Equal("render.exe: 6 Wh, $0.0012 (still running)", rows[1].Title);
        Assert.Contains("running for 20 min", rows[1].Detail);
    }
}
