using KVGuzzler.Core.Power;
using KVGuzzler.Sensors;
using Xunit;

namespace KVGuzzler.Tests;

internal sealed class FakeSource : IPowerSource
{
    private readonly Func<IReadOnlyList<PowerReading>> _read;

    public FakeSource(string name, Func<IReadOnlyList<PowerReading>> read, string? unavailable = null)
    {
        Name = name;
        _read = read;
        UnavailableReason = unavailable;
    }

    public string Name { get; }
    public string? UnavailableReason { get; }
    public bool Disposed { get; private set; }
    public IReadOnlyList<PowerReading> Read() => _read();
    public void Dispose() => Disposed = true;
}

public class PowerSamplerTests
{
    private static readonly DateTimeOffset T0 = new(2026, 10, 18, 12, 0, 0, TimeSpan.Zero);
    private static PowerReading Part(string n, double w) => new(n, w, ReadingScope.Component);
    private static PowerReading Sub(string n, double w) => new(n, w, ReadingScope.Breakdown);

    [Fact]
    public void TotalSumsComponentsAndNeverTheirBreakdown()
    {
        var sampler = new PowerSampler(new IPowerSource[]
        {
            new FakeSource("gpu", () => new[] { Part("GPU", 80) }),
            new FakeSource("cpu", () => new[] { Part("CPU package", 65), Sub("CPU cores", 56), Part("Memory", 5) }),
        });
        var s = sampler.Sample(T0);
        Assert.Equal(150, s.TotalWatts);
        Assert.Equal(4, s.Readings.Count);
    }

    [Fact]
    public void EnergyIntegratesTheTotalOverTime()
    {
        var sampler = new PowerSampler(new[] { new FakeSource("gpu", () => new[] { Part("GPU", 400) }) });
        sampler.Sample(T0);
        var s = sampler.Sample(T0.AddMinutes(1).AddSeconds(-1));   // inside the default 1 minute gap limit
        Assert.Equal(400.0 * 59 / 3_600_000.0, s.SessionKilowattHours, precision: 12);
    }

    [Fact]
    public void AnUnavailableSourceIsReportedNotRead()
    {
        var neverCalled = new FakeSource("nvml", () => throw new InvalidOperationException("must not be read"), "No NVIDIA driver found.");
        var s = new PowerSampler(new[] { neverCalled }).Sample(T0);
        Assert.Empty(s.Readings);
        Assert.Equal("nvml: No NVIDIA driver found.", Assert.Single(s.Notes));
    }

    [Fact]
    public void AFailingSourceDoesNotStopTheOthers()
    {
        var sampler = new PowerSampler(new IPowerSource[]
        {
            new FakeSource("broken", () => throw new IOException("sensor gone")),
            new FakeSource("gpu", () => new[] { Part("GPU", 80) }),
        });
        var s = sampler.Sample(T0);
        Assert.Equal(80, s.TotalWatts);
        Assert.Equal("broken: sensor gone", Assert.Single(s.Notes));
    }

    [Fact]
    public void NonsenseReadingsAreDroppedBeforeTheyReachTheEnergyMeter()
    {
        var sampler = new PowerSampler(new[]
        {
            new FakeSource("odd", () => new[] { Part("NaN", double.NaN), Part("Negative", -5), Part("Fine", 10) }),
        });
        var s = sampler.Sample(T0);
        Assert.Equal("Fine", Assert.Single(s.Readings).Name);
        Assert.Equal(10, s.TotalWatts);
    }

    [Fact]
    public void NothingMeasuredIsNotIntegratedAsZeroWatts()
    {
        var read = true;
        var sampler = new PowerSampler(new[]
        {
            new FakeSource("gpu", () => read ? new[] { Part("GPU", 100) } : Array.Empty<PowerReading>()),
        }, maxGap: TimeSpan.FromMinutes(10));
        sampler.Sample(T0);
        read = false;
        sampler.Sample(T0.AddSeconds(30));    // sensor silent: no zero is invented
        read = true;
        var s = sampler.Sample(T0.AddSeconds(60));
        Assert.Equal(100.0 * 60 / 3_600_000.0, s.SessionKilowattHours, precision: 12);   // bridged at the last known level
    }

    [Fact]
    public void DisposingTheSamplerDisposesItsSources()
    {
        var a = new FakeSource("a", () => Array.Empty<PowerReading>());
        new PowerSampler(new[] { a }).Dispose();
        Assert.True(a.Disposed);
    }
}

public class NvmlPowerSourceTests
{
    private sealed class FakeNvml : INvmlApi
    {
        public int DeviceCount => 2;
        public string DeviceName(int index) => index == 0 ? "NVIDIA GeForce RTX 5070 Ti" : "NVIDIA T400";
        public uint? PowerMilliwatts(int index) => index == 0 ? 94_660u : null;   // the second board reports no power
        public void Dispose() { }
    }

    [Fact]
    public void ReportsMilliwattsAsWattsPerGpuAndSkipsBoardsWithoutASensor()
    {
        var source = new NvmlPowerSource(new FakeNvml());
        Assert.Null(source.UnavailableReason);
        var r = Assert.Single(source.Read());
        Assert.Equal("NVIDIA GeForce RTX 5070 Ti", r.Name);
        Assert.Equal(94.66, r.Watts, precision: 6);
        Assert.Equal(ReadingScope.Component, r.Scope);
    }

    [Fact]
    public void WithoutADriverItSaysSo()
    {
        var source = new NvmlPowerSource(null, "No NVIDIA driver (NVML) found.");
        Assert.Equal("No NVIDIA driver (NVML) found.", source.UnavailableReason);
    }
}

public class WindowsEnergyMeterNameTests
{
    [Theory]
    [InlineData("RAPL_Package0_PKG", 1, "CPU package", ReadingScope.Component)]
    [InlineData("RAPL_Package1_PKG", 2, "CPU package 1", ReadingScope.Component)]
    [InlineData("RAPL_Package0_DRAM", 1, "Memory (DRAM)", ReadingScope.Component)]
    [InlineData("RAPL_Package0_PP0", 1, "CPU cores", ReadingScope.Breakdown)]
    [InlineData("RAPL_Package0_PP1", 1, "CPU graphics / uncore", ReadingScope.Breakdown)]
    [InlineData("rapl_package0_pkg", 1, "CPU package", ReadingScope.Component)]   // counter paths come back lower-cased
    public void NamesTheInstances(string instance, int packages, string label, ReadingScope scope)
    {
        if (!OperatingSystem.IsWindows()) return;
        Assert.Equal((label, scope), WindowsEnergyMeterSource.Describe(instance, packages));
    }

    [Fact]
    public void SkipsTheTotalInstance()
    {
        if (!OperatingSystem.IsWindows()) return;
        Assert.Null(WindowsEnergyMeterSource.Describe("_Total", 1));
    }
}

public class LinuxSourceTests : IDisposable
{
    private readonly string _root = Path.Combine(Path.GetTempPath(), "kvguzzler-sysfs-" + Guid.NewGuid().ToString("N")[..8]);

    public void Dispose()
    {
        try { Directory.Delete(_root, recursive: true); } catch (IOException) { /* best effort */ }
    }

    private string PowercapRoot => Path.Combine(_root, "powercap");
    private string HwmonRoot => Path.Combine(_root, "hwmon");

    private void Write(string relative, string content)
    {
        var path = Path.Combine(_root, relative);
        Directory.CreateDirectory(Path.GetDirectoryName(path)!);
        File.WriteAllText(path, content + "\n");
    }

    private sealed class ManualTime : TimeProvider
    {
        private long _ticks;
        public override long TimestampFrequency => 1_000;   // one tick per millisecond
        public override long GetTimestamp() => _ticks;
        public void Advance(TimeSpan by) => _ticks += (long)by.TotalMilliseconds;
    }

    // Real zone directories are named intel-rapl:0:1; Windows cannot create a colon in a directory name.
    private static readonly char Sep = OperatingSystem.IsWindows() ? '.' : ':';

    private static string ZoneDir(string zone) => zone.Replace(':', Sep);

    private LinuxRaplSource Rapl(TimeProvider? time = null) => new(PowercapRoot, time, Sep);

    private void Zone(string dir, string name, ulong energyUj, ulong maxUj = 262_143_328_850)
    {
        dir = ZoneDir(dir);
        Write($"powercap/{dir}/name", name);
        Write($"powercap/{dir}/energy_uj", energyUj.ToString());
        Write($"powercap/{dir}/max_energy_range_uj", maxUj.ToString());
    }

    [Fact]
    public void RaplPowerIsTheEnergyChangeOverTheElapsedTime()
    {
        Zone("intel-rapl:0", "package-0", 1_000_000);
        Zone("intel-rapl:0:0", "core", 1_000_000);
        Zone("intel-rapl:0:2", "dram", 1_000_000);
        Zone("intel-rapl-mmio:0", "package-0", 1_000_000);   // the same counters again: must be ignored
        var time = new ManualTime();
        var source = Rapl(time);
        Assert.Null(source.UnavailableReason);

        Assert.Empty(source.Read());                         // the first read only sets the baseline

        time.Advance(TimeSpan.FromSeconds(2));
        Write($"powercap/{ZoneDir("intel-rapl:0/energy_uj")}", "131000000");        // +130 J in 2 s = 65 W
        Write($"powercap/{ZoneDir("intel-rapl:0:0/energy_uj")}", "113000000");      // +112 J in 2 s = 56 W
        Write($"powercap/{ZoneDir("intel-rapl:0:2/energy_uj")}", "11000000");       // +10 J in 2 s = 5 W
        var byName = source.Read().ToDictionary(r => r.Name);

        Assert.Equal(3, byName.Count);
        Assert.Equal(65, byName["CPU package"].Watts, precision: 6);
        Assert.Equal(ReadingScope.Component, byName["CPU package"].Scope);
        Assert.Equal(56, byName["CPU cores"].Watts, precision: 6);
        Assert.Equal(ReadingScope.Breakdown, byName["CPU cores"].Scope);
        Assert.Equal(5, byName["Memory (DRAM)"].Watts, precision: 6);
        Assert.Equal(ReadingScope.Component, byName["Memory (DRAM)"].Scope);
    }

    [Fact]
    public void RaplCounterWrapAroundIsHandled()
    {
        Zone("intel-rapl:0", "package-0", 999_000_000, maxUj: 1_000_000_000);
        var time = new ManualTime();
        var source = Rapl(time);
        source.Read();

        time.Advance(TimeSpan.FromSeconds(1));
        Write($"powercap/{ZoneDir("intel-rapl:0/energy_uj")}", "49000000");          // wrapped: 1 J + 49 J = 50 J in 1 s
        Assert.Equal(50, Assert.Single(source.Read()).Watts, precision: 6);
    }

    [Fact]
    public void RaplWithoutAnyZonesSaysSo()
    {
        Directory.CreateDirectory(PowercapRoot);
        Assert.Contains("No RAPL power zones", Rapl().UnavailableReason);
        Assert.Contains("no RAPL", new LinuxRaplSource(Path.Combine(_root, "missing")).UnavailableReason);
    }

    [Fact]
    public void HwmonGpuPowerIsMicrowattsAndCountsTowardTheTotal()
    {
        Write("hwmon/hwmon3/name", "amdgpu");
        Write("hwmon/hwmon3/power1_average", "187000000");          // 187 W
        Write("hwmon/hwmon3/power1_input", "1");                    // average wins over input
        Write("hwmon/hwmon1/name", "coolermod");
        Write("hwmon/hwmon1/power1_input", "12000000");
        Write("hwmon/hwmon1/power1_label", "pump");
        Write("hwmon/hwmon2/name", "nvme");                         // no power file: ignored

        var source = new LinuxHwmonSource(HwmonRoot);
        Assert.Null(source.UnavailableReason);
        var byName = source.Read().ToDictionary(r => r.Name);

        Assert.Equal(187, byName["GPU (amdgpu)"].Watts, precision: 6);
        Assert.Equal(ReadingScope.Component, byName["GPU (amdgpu)"].Scope);
        Assert.Equal(12, byName["coolermod pump"].Watts, precision: 6);
        Assert.Equal(ReadingScope.Breakdown, byName["coolermod pump"].Scope);   // unknown driver: shown, not summed
    }

    [Fact]
    public void HwmonWithoutPowerSensorsSaysSo()
    {
        Write("hwmon/hwmon0/name", "nvme");
        Assert.Contains("No power sensors", new LinuxHwmonSource(HwmonRoot).UnavailableReason);
    }
}
