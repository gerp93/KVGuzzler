using System.Text.RegularExpressions;

namespace KVGuzzler.Core.Power;

/// <summary>
/// Power sensors that Linux drivers publish under /sys/class/hwmon (microwatts), most usefully AMD and
/// Intel GPUs (amdgpu, i915, xe). Readable without elevation. NVIDIA GPUs go through NVML instead.
/// Written against the kernel's documented layout and tested with a fake tree; not yet confirmed on
/// real Linux hardware.
/// </summary>
public sealed partial class LinuxHwmonSource : IPowerSource
{
    [GeneratedRegex(@"^power(\d+)_(average|input)$")]
    private static partial Regex PowerFile();

    private static readonly HashSet<string> GpuDrivers = new(StringComparer.Ordinal) { "amdgpu", "radeon", "i915", "xe" };

    private sealed record Sensor(string File, string Label, ReadingScope Scope);

    private readonly List<Sensor> _sensors = new();

    public LinuxHwmonSource(string root = "/sys/class/hwmon")
    {
        if (!Directory.Exists(root))
        {
            UnavailableReason = "This system has no hardware monitor sensors (/sys/class/hwmon).";
            return;
        }

        var gpuDirs = Directory.GetDirectories(root).Count(d => GpuDrivers.Contains(ReadText(Path.Combine(d, "name")) ?? ""));
        var gpuNumber = 0;

        foreach (var dir in Directory.GetDirectories(root).OrderBy(d => d, StringComparer.Ordinal))
        {
            var driver = ReadText(Path.Combine(dir, "name")) ?? Path.GetFileName(dir);
            var isGpu = GpuDrivers.Contains(driver);
            if (isGpu) gpuNumber++;

            // power1_average is smoothed by the driver; power1_input is the same quantity under the other
            // name newer kernels use. Prefer average, one file per sensor number.
            var files = Directory.GetFiles(dir)
                .Select(f => (File: f, Match: PowerFile().Match(Path.GetFileName(f))))
                .Where(x => x.Match.Success)
                .GroupBy(x => x.Match.Groups[1].Value)
                .Select(g => g.OrderBy(x => x.Match.Groups[2].Value == "average" ? 0 : 1).First())
                .OrderBy(x => int.Parse(x.Match.Groups[1].Value));

            foreach (var (file, match) in files)
            {
                if (ReadMicrowatts(file) is null) continue;   // unreadable or unsupported: skip this sensor
                var number = match.Groups[1].Value;
                var sensorLabel = ReadText(Path.Combine(dir, $"power{number}_label"));
                _sensors.Add(isGpu
                    ? new Sensor(file, gpuDirs > 1 ? $"GPU {gpuNumber} ({driver})" : $"GPU ({driver})", ReadingScope.Component)
                    // Unknown drivers might overlap something counted elsewhere (a CPU module next to RAPL),
                    // so they are shown but not summed.
                    : new Sensor(file, $"{driver} {sensorLabel ?? "power" + number}", ReadingScope.Breakdown));
            }
        }

        if (_sensors.Count == 0) UnavailableReason = "No power sensors found in the hardware monitor (hwmon).";
    }

    public string Name => "Hardware monitor (hwmon)";
    public string? UnavailableReason { get; }

    public IReadOnlyList<PowerReading> Read()
    {
        var readings = new List<PowerReading>();
        foreach (var s in _sensors)
        {
            if (ReadMicrowatts(s.File) is { } uw) readings.Add(new PowerReading(s.Label, uw / 1_000_000.0, s.Scope));
        }
        return readings;
    }

    public void Dispose() { }

    private static double? ReadMicrowatts(string path) =>
        double.TryParse(ReadText(path), System.Globalization.NumberStyles.Float, System.Globalization.CultureInfo.InvariantCulture, out var v) ? v : null;

    private static string? ReadText(string path)
    {
        try { return File.ReadAllText(path).Trim(); }
        catch (Exception e) when (e is IOException or UnauthorizedAccessException) { return null; }
    }
}
