using System.Diagnostics;
using System.Runtime.Versioning;
using System.Text.RegularExpressions;


namespace KVGuzzler.Core.Power;

/// <summary>
/// CPU and memory power on Windows from the built-in "Energy Meter" performance counters, which Windows
/// fills from the processor's RAPL energy registers. No driver and no admin rights are needed, which
/// fits the per-user Velopack install. Instances look like RAPL_Package0_PKG (whole CPU package),
/// _PP0 (cores), _PP1 (graphics / uncore) and _DRAM (memory).
/// </summary>
/// <remarks>
/// The "Power" counter is reported in milliwatts (checked against the counter's raw energy readings).
/// A processor or Windows version that does not publish the counters has no "Energy Meter" category,
/// and the source says so. AMD CPUs and GPUs that are not NVIDIA are not covered here; they need a
/// vendor driver (see TODO.md).
/// </remarks>
[SupportedOSPlatform("windows")]
public sealed partial class WindowsEnergyMeterSource : IPowerSource
{
    private const string Category = "Energy Meter";

    [GeneratedRegex(@"^RAPL_Package(\d+)_(.+)$", RegexOptions.IgnoreCase)]
    private static partial Regex InstanceName();

    private sealed class Instance
    {
        public required PerformanceCounter Counter { get; init; }
        public required string Label { get; init; }
        public required ReadingScope Scope { get; init; }
        /// <summary>A counter that has only ever read zero is a domain this CPU does not measure (desktop
        /// memory is the usual one), so it stays hidden until it reports something.</summary>
        public bool EverNonZero { get; set; }
    }

    private readonly List<Instance> _instances = new();

    public WindowsEnergyMeterSource()
    {
        try
        {
            if (!PerformanceCounterCategory.Exists(Category))
            {
                UnavailableReason = "Windows does not publish CPU energy counters on this machine.";
                return;
            }

            var names = new PerformanceCounterCategory(Category).GetInstanceNames();
            var packageCount = names.Select(n => InstanceName().Match(n)).Where(m => m.Success)
                .Select(m => m.Groups[1].Value).Distinct().Count();

            foreach (var name in names.OrderBy(n => n, StringComparer.OrdinalIgnoreCase))
            {
                if (Describe(name, packageCount) is not { } d) continue;
                var counter = new PerformanceCounter(Category, "Power", name, readOnly: true);
                counter.NextValue();   // a rate counter needs a first read to measure against
                _instances.Add(new Instance { Counter = counter, Label = d.Label, Scope = d.Scope });
            }

            if (_instances.Count == 0) UnavailableReason = "Windows lists no CPU energy counters on this machine.";
        }
        catch (Exception e)
        {
            // Missing category, counters disabled or corrupt: optional hardware, so report instead of crashing.
            UnavailableReason = "Could not read the Windows CPU energy counters: " + e.Message;
            Dispose();
        }
    }

    public string Name => "CPU (Windows energy counters)";
    public string? UnavailableReason { get; }

    public IReadOnlyList<PowerReading> Read()
    {
        var readings = new List<PowerReading>();
        foreach (var i in _instances)
        {
            var watts = i.Counter.NextValue() / 1000.0;   // milliwatts
            if (watts > 0) i.EverNonZero = true;
            if (i.EverNonZero) readings.Add(new PowerReading(i.Label, watts, i.Scope, i.Label.StartsWith("CPU package", StringComparison.Ordinal) ? PowerKind.Cpu : PowerKind.Other));
        }
        return readings;
    }

    public void Dispose()
    {
        foreach (var i in _instances) i.Counter.Dispose();
        _instances.Clear();
    }

    /// <summary>Maps a counter instance name to what the user sees; null for instances that cannot be
    /// classified (such as _Total), which are skipped.</summary>
    internal static (string Label, ReadingScope Scope)? Describe(string instance, int packageCount)
    {
        var m = InstanceName().Match(instance);
        if (!m.Success) return null;

        var package = packageCount > 1 ? " " + m.Groups[1].Value : "";
        return m.Groups[2].Value.ToUpperInvariant() switch
        {
            "PKG" => ($"CPU package{package}", ReadingScope.Component),
            "DRAM" => ($"Memory (DRAM){package}", ReadingScope.Component),   // not part of the package figure
            "PP0" => ($"CPU cores{package}", ReadingScope.Breakdown),
            "PP1" => ($"CPU graphics / uncore{package}", ReadingScope.Breakdown),
            _ => ($"{instance}", ReadingScope.Breakdown),
        };
    }
}
