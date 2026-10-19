using KVGuzzler.Core.History;
using KVGuzzler.Core.Power;
using Xunit;

namespace KVGuzzler.Tests;

public sealed class TempHistory : IDisposable
{
    public string Dir { get; } = Path.Combine(Path.GetTempPath(), "kvguzzler-hist-" + Guid.NewGuid().ToString("N")[..8]);
    public HistoryStore Store { get; }

    public TempHistory() => Store = new HistoryStore(Path.Combine(Dir, HistoryStore.FileName));

    public void Dispose()
    {
        Store.Dispose();
        try { Directory.Delete(Dir, recursive: true); } catch (IOException) { /* best effort */ }
    }
}

public class HistoryStoreTests
{
    private static readonly DateTimeOffset T0 = new(2026, 10, 18, 12, 0, 0, TimeSpan.Zero);

    [Fact]
    public void MinutesAddUpAndOnlyMeasuredComponentsAreCounted()
    {
        using var h = new TempHistory();
        var m = T0.ToUnixTimeSeconds();
        h.Store.AddMinutes(new[]
        {
            new MinuteRow(m, "GPU", ReadingScope.Component, 0.001, 60, 90),
            new MinuteRow(m, "CPU package", ReadingScope.Component, 0.0005, 60, 50),
            new MinuteRow(m, "CPU cores", ReadingScope.Breakdown, 0.0004, 60, 40),   // part of the package: not summed
        });
        h.Store.AddMinutes(new[] { new MinuteRow(m, "GPU", ReadingScope.Component, 0.001, 30, 120) });   // same minute again: added

        var (kwh, monitored, gaps) = h.Store.Query(T0, T0.AddMinutes(1));
        Assert.Equal(0.0025, kwh, precision: 9);
        Assert.Equal(TimeSpan.FromSeconds(90), monitored);   // the longest any one component was sampled: 60 + 30
        Assert.Empty(gaps);
    }

    [Fact]
    public void QueryOnlyCoversTheAskedPeriod()
    {
        using var h = new TempHistory();
        var m = T0.ToUnixTimeSeconds();
        h.Store.AddMinutes(new[]
        {
            new MinuteRow(m - 120, "GPU", ReadingScope.Component, 1, 60, 1),    // before
            new MinuteRow(m, "GPU", ReadingScope.Component, 2, 60, 1),          // inside
            new MinuteRow(m + 600, "GPU", ReadingScope.Component, 4, 60, 1),    // after
        });
        Assert.Equal(2, h.Store.Query(T0, T0.AddMinutes(5)).KilowattHours);
    }

    [Fact]
    public void GapsAreClippedToThePeriod()
    {
        using var h = new TempHistory();
        h.Store.AddGap(new Gap(T0.AddHours(-2), T0.AddMinutes(30), GapReason.Asleep));   // starts before the period
        var (_, _, gaps) = h.Store.Query(T0, T0.AddHours(1));
        var g = Assert.Single(gaps);
        Assert.Equal(T0, g.Start);
        Assert.Equal(T0.AddMinutes(30), g.End);
    }

    [Fact]
    public void LiveReadingsKeepTheirOrderAndNotes()
    {
        using var h = new TempHistory();
        Assert.Null(h.Store.GetLive());

        h.Store.SetLive(T0, new[]
        {
            new PowerReading("GPU", 80, ReadingScope.Component),
            new PowerReading("CPU package", 65, ReadingScope.Component),
            new PowerReading("CPU cores", 56, ReadingScope.Breakdown),
        }, new[] { "NVIDIA GPU (NVML): No NVIDIA driver (NVML) found." });

        var live = h.Store.GetLive()!;
        Assert.Equal(T0, live.UpdatedAt);
        Assert.Equal(new[] { "GPU", "CPU package", "CPU cores" }, live.Readings.Select(r => r.Name));
        Assert.Equal(ReadingScope.Breakdown, live.Readings[2].Scope);
        Assert.Single(live.Notes);
    }

    [Fact]
    public void ReopeningTheFileKeepsEverything()
    {
        using var h = new TempHistory();
        h.Store.AddMinutes(new[] { new MinuteRow(T0.ToUnixTimeSeconds(), "GPU", ReadingScope.Component, 1, 60, 1) });
        h.Store.Dispose();   // releases the pool, as the collector does on exit
        using var again = new HistoryStore(h.Store.Path);
        Assert.Equal(1, again.Query(T0, T0.AddMinutes(1)).KilowattHours);
    }
}

public class PowerCollectorTests
{
    private static readonly DateTimeOffset T0 = new(2026, 10, 18, 12, 0, 0, TimeSpan.Zero);

    private sealed class Steady : IPowerSource
    {
        public double GpuWatts = 100;
        public string Name => "fake";
        public string? UnavailableReason => null;
        public IReadOnlyList<PowerReading> Read() => new[]
        {
            new PowerReading("GPU", GpuWatts, ReadingScope.Component),
            new PowerReading("CPU package", 50, ReadingScope.Component),
            new PowerReading("CPU cores", 40, ReadingScope.Breakdown),
        };
        public void Dispose() { }
    }

    private static PowerCollector Make(TempHistory h, Steady source) =>
        new(h.Store, new PowerSampler(new IPowerSource[] { source }, maxGap: TimeSpan.FromMinutes(5)));

    private static void Run(PowerCollector c, DateTimeOffset from, int seconds)
    {
        for (var i = 0; i <= seconds; i++) c.Tick(from.AddSeconds(i));
    }

    [Fact]
    public void ASteadyHourIsSavedAsEnergyPerMinute()
    {
        using var h = new TempHistory();
        var c = Make(h, new Steady());
        c.Start(T0);
        Run(c, T0, 3600);
        c.Stop();

        // 150 W of components for one hour = 0.15 kWh; the cores are part of the package and not counted.
        var (kwh, monitored, gaps) = h.Store.Query(T0, T0.AddHours(1).AddSeconds(1));
        Assert.Equal(0.15, kwh, precision: 6);
        Assert.InRange(monitored.TotalSeconds, 3590, 3601);
        Assert.Empty(gaps);
    }

    [Fact]
    public void ChangingLoadIsIntegratedNotAveragedAway()
    {
        using var h = new TempHistory();
        var source = new Steady { GpuWatts = 100 };
        var c = Make(h, source);
        c.Start(T0);
        Run(c, T0, 60);
        source.GpuWatts = 300;
        Run(c, T0.AddSeconds(61), 59);
        c.Stop();

        var (kwh, _, _) = h.Store.Query(T0, T0.AddMinutes(3));
        // about 150 W for a minute, then 350 W for a minute, give or take the one-second ramp between them
        Assert.InRange(kwh, 150.0 / 60000 + 350.0 / 60000 - 0.0001, 150.0 / 60000 + 350.0 / 60000 + 0.0001);
    }

    [Fact]
    public void ALongSilenceWhileRunningIsASleepGapAndIsNotBilled()
    {
        using var h = new TempHistory();
        var c = Make(h, new Steady());
        c.Start(T0);
        Run(c, T0, 30);
        var wake = T0.AddHours(8);                  // the timer resumes eight hours later
        Run(c, wake, 30);
        c.Stop();

        var (kwh, _, gaps) = h.Store.Query(T0, wake.AddMinutes(1));
        var g = Assert.Single(gaps);
        Assert.Equal(GapReason.Asleep, g.Reason);
        Assert.Equal(T0.AddSeconds(30), g.Start);
        Assert.Equal(wake, g.End);
        Assert.Equal(150.0 * 60 / 3_600_000.0, kwh, precision: 4);   // only the 60 s actually sampled
    }

    [Fact]
    public void RestartAfterACleanStopIsOffAndAfterNoStopIsLostPower()
    {
        using var h = new TempHistory();
        var first = Make(h, new Steady());
        first.Start(T0);
        Run(first, T0, 30);
        first.Stop();

        var second = Make(h, new Steady());
        second.Start(T0.AddHours(2));               // clean stop, two hours later
        Run(second, T0.AddHours(2), 30);
        // no Stop(): the power was cut

        var third = Make(h, new Steady());
        third.Start(T0.AddHours(5));
        Run(third, T0.AddHours(5), 5);

        var gaps = h.Store.Query(T0, T0.AddHours(6)).Gaps;
        Assert.Equal(new[] { GapReason.Off, GapReason.LostPower }, gaps.Select(g => g.Reason));
        Assert.Equal(T0.AddSeconds(30), gaps[0].Start);
        Assert.Equal(T0.AddHours(2), gaps[0].End);
    }

    [Fact]
    public void TheFirstEverStartHasNoGap()
    {
        using var h = new TempHistory();
        Make(h, new Steady()).Start(T0);
        Assert.Empty(h.Store.Query(T0.AddDays(-1), T0.AddDays(1)).Gaps);
    }

    [Fact]
    public void TwoIdenticalGpusKeepSeparateRows()
    {
        using var h = new TempHistory();
        var twin = new TwinGpus();
        var c = new PowerCollector(h.Store, new PowerSampler(new IPowerSource[] { twin }));
        c.Start(T0);
        Run(c, T0, 10);
        Assert.Equal(new[] { "GPU", "GPU 2" }, h.Store.GetLive()!.Readings.Select(r => r.Name));
        c.Stop();
        Assert.Equal(2 * 100.0 * 10 / 3_600_000.0, h.Store.Query(T0, T0.AddMinutes(1)).KilowattHours, precision: 9);
    }

    private sealed class TwinGpus : IPowerSource
    {
        public string Name => "twin";
        public string? UnavailableReason => null;
        public IReadOnlyList<PowerReading> Read() => new[]
        {
            new PowerReading("GPU", 100, ReadingScope.Component),
            new PowerReading("GPU", 100, ReadingScope.Component),
        };
        public void Dispose() { }
    }
}

public class HistoryReportTests
{
    private static readonly DateTimeOffset T0 = new(2026, 10, 18, 12, 0, 0, TimeSpan.Zero);

    [Fact]
    public void SleepIsEstimatedAtTheAssumedWattageAndKeptApartFromMeasured()
    {
        using var h = new TempHistory();
        h.Store.AddMinutes(new[] { new MinuteRow(T0.ToUnixTimeSeconds(), "GPU", ReadingScope.Component, 0.5, 60, 300) });
        h.Store.AddGap(new Gap(T0.AddHours(1), T0.AddHours(11), GapReason.Asleep));
        h.Store.AddGap(new Gap(T0.AddHours(12), T0.AddHours(14), GapReason.Off));
        h.Store.AddGap(new Gap(T0.AddHours(15), T0.AddHours(15).AddMinutes(30), GapReason.LostPower));

        var r = HistoryReport.Build(h.Store, "test", T0, T0.AddDays(1), sleepWatts: 2);

        Assert.Equal(0.5, r.MeasuredKwh, precision: 9);
        Assert.Equal(0.02, r.EstimatedKwh, precision: 9);   // 2 W x 10 h
        Assert.Equal(0.52, r.TotalKwh, precision: 9);
        Assert.Equal(TimeSpan.FromHours(10), r.Asleep);
        Assert.Equal(TimeSpan.FromHours(2), r.Off);
        Assert.Equal(TimeSpan.FromMinutes(30), r.LostPower);
    }

    [Fact]
    public void StandardPeriodsAreTheFourRollingWindows()
    {
        using var h = new TempHistory();
        var periods = HistoryReport.Standard(h.Store, T0, sleepWatts: 2);
        Assert.Equal(new[] { "Last hour", "Last 24 hours", "Last 7 days", "Last 30 days" }, periods.Select(p => p.Label));
    }
}

public class ServiceInstallerTests
{
    [Fact]
    public void RunsTheSameProgramInCollectorMode()
    {
        var cmds = KVGuzzler.Collector.ServiceInstaller.CreateCommands("KVGuzzlerCollector", @"C:\Program Files\KVGuzzler\current\KVGuzzler.exe", exists: false);
        Assert.Equal("create", cmds[0][0]);
        Assert.Equal(@"""C:\Program Files\KVGuzzler\current\KVGuzzler.exe"" --collector", cmds[0][Array.IndexOf(cmds[0], "binPath=") + 1]);
        Assert.Equal("auto", cmds[0][Array.IndexOf(cmds[0], "start=") + 1]);
        Assert.Contains(cmds, c => c[0] == "failure");   // restarts itself if it dies
    }

    [Fact]
    public void TheLoggedInUserMayStopAndStartTheServiceSoAnUpdateCanReplaceItsFiles()
    {
        var cmds = KVGuzzler.Collector.ServiceInstaller.CreateCommands("KVGuzzlerCollector", @"C:\x\KVGuzzler.exe", exists: false);
        var sdset = Assert.Single(cmds, c => c[0] == "sdset");
        Assert.Equal(KVGuzzler.Collector.ServiceInstaller.ServiceSecurity, sdset[2]);

        // The text must be valid SDDL, and the interactive user must hold start and stop but not delete or reconfigure.
        if (OperatingSystem.IsWindows()) CheckInteractiveUserRights();
    }

    [System.Runtime.Versioning.SupportedOSPlatform("windows")]
    private static void CheckInteractiveUserRights()
    {
        var descriptor = new System.Security.AccessControl.RawSecurityDescriptor(KVGuzzler.Collector.ServiceInstaller.ServiceSecurity);
        var interactive = new System.Security.Principal.SecurityIdentifier(System.Security.Principal.WellKnownSidType.InteractiveSid, null);
        var rule = descriptor.DiscretionaryAcl!.OfType<System.Security.AccessControl.CommonAce>().Single(a => a.SecurityIdentifier == interactive);
        Assert.Equal(0x0010 | 0x0020, rule.AccessMask & (0x0010 | 0x0020));   // SERVICE_START | SERVICE_STOP
        Assert.Equal(0, rule.AccessMask & 0x0002);                              // not SERVICE_CHANGE_CONFIG
        Assert.Equal(0, rule.AccessMask & 0x00010000);                          // not DELETE
    }

    [Fact]
    public void AnExistingServiceIsReconfiguredNotRecreated()
    {
        var cmds = KVGuzzler.Collector.ServiceInstaller.CreateCommands("KVGuzzlerCollector", @"C:\x\KVGuzzler.exe", exists: true);
        Assert.Equal("config", cmds[0][0]);
    }

    [Theory]
    [InlineData(1060, "", KVGuzzler.Collector.ServiceState.NotInstalled)]
    [InlineData(0, "SERVICE_NAME: x\n        TYPE               : 10  WIN32_OWN_PROCESS\n        STATE              : 4  RUNNING\n", KVGuzzler.Collector.ServiceState.Running)]
    [InlineData(0, "        STATE              : 1  STOPPED\n", KVGuzzler.Collector.ServiceState.Stopped)]
    [InlineData(0, "        STATE              : 4  EN COURS D'EXECUTION\n", KVGuzzler.Collector.ServiceState.Running)]   // translated word, same number
    [InlineData(0, "        STATE              : 2  START_PENDING\n", KVGuzzler.Collector.ServiceState.Other)]
    public void ReadsTheServiceStateFromScOutput(int exit, string output, KVGuzzler.Collector.ServiceState expected) =>
        Assert.Equal(expected, KVGuzzler.Collector.ServiceInstaller.ParseState(exit, output));
}

public class PowerCollectorSavingTests
{
    private static readonly DateTimeOffset T0 = new(2026, 10, 18, 12, 0, 0, TimeSpan.Zero);

    private sealed class Flat : IPowerSource
    {
        public string Name => "flat";
        public string? UnavailableReason => null;
        public IReadOnlyList<PowerReading> Read() => new[] { new PowerReading("GPU", 100, ReadingScope.Component) };
        public void Dispose() { }
    }

    [Fact]
    public void EnergyIsSavedWithinSecondsNotOnlyAtTheEndOfTheMinute()
    {
        using var h = new TempHistory();
        var c = new PowerCollector(h.Store, new PowerSampler(new IPowerSource[] { new Flat() }));
        c.Start(T0);
        for (var i = 0; i <= 40; i++) c.Tick(T0.AddSeconds(i));   // killed here: no Stop()

        var saved = h.Store.Query(T0, T0.AddMinutes(1)).KilowattHours;
        Assert.True(saved > 100.0 * 30 / 3_600_000.0, $"only {saved} kWh was saved after 40 s");
    }

    [Fact]
    public void SavingInPiecesGivesTheSameTotalAsSavingOnce()
    {
        using var h = new TempHistory();
        var c = new PowerCollector(h.Store, new PowerSampler(new IPowerSource[] { new Flat() }));
        c.Start(T0);
        for (var i = 0; i <= 55; i++) c.Tick(T0.AddSeconds(i));
        c.Stop();
        Assert.Equal(100.0 * 55 / 3_600_000.0, h.Store.Query(T0, T0.AddMinutes(1)).KilowattHours, precision: 9);
    }
}
