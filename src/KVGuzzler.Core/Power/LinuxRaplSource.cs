using System.Text.RegularExpressions;

namespace KVGuzzler.Core.Power;

/// <summary>
/// CPU (and memory) power on Linux from the RAPL energy counters under /sys/class/powercap. The kernel
/// exposes a running energy total in microjoules; power is the change in that total divided by the
/// time between two reads, so the first read only records a starting point and reports nothing.
/// </summary>
/// <remarks>
/// Since the Platypus side-channel fix (2020) <c>energy_uj</c> is readable by root only, so without
/// elevation this reports itself unavailable and says why. Intel CPUs, and AMD Zen CPUs on recent
/// kernels, expose these zones. Written against the kernel's documented layout and tested with a fake
/// tree; not yet confirmed on real Linux hardware.
/// </remarks>
public sealed class LinuxRaplSource : IPowerSource
{
    private sealed class Zone
    {
        public required string EnergyFile { get; init; }
        public required string Label { get; init; }
        public required ReadingScope Scope { get; init; }
        public ulong MaxRange { get; init; }
        public ulong? LastEnergy { get; set; }
        public long LastTimestamp { get; set; }
    }

    private readonly List<Zone> _zones = new();
    private readonly TimeProvider _time;
    private readonly char _separator;

    public LinuxRaplSource(string root = "/sys/class/powercap") : this(root, null, ':') { }

    /// <param name="separator">The ':' in zone directory names like intel-rapl:0:1. Tests pass another
    /// character because Windows cannot create a directory with a colon in its name.</param>
    internal LinuxRaplSource(string root, TimeProvider? time, char separator = ':')
    {
        _time = time ?? TimeProvider.System;
        _separator = separator;

        // intel-rapl:0 (a package), intel-rapl:0:1 (a subzone). The intel-rapl-mmio:* zones are the same
        // counters reached another way; skipping them avoids counting the package twice.
        var s = Regex.Escape(separator.ToString());
        var zoneDirectory = new Regex($@"^intel-rapl{s}\d+({s}\d+)*$");

        if (!Directory.Exists(root))
        {
            UnavailableReason = "This system has no RAPL power counters (/sys/class/powercap).";
            return;
        }

        var dirs = Directory.GetDirectories(root)
            .Where(d => zoneDirectory.IsMatch(Path.GetFileName(d)))
            .OrderBy(d => Path.GetFileName(d), StringComparer.Ordinal)
            .ToList();
        var names = dirs.ToDictionary(d => d, d => ReadText(Path.Combine(d, "name")) ?? "");
        var packageCount = names.Values.Count(n => n.StartsWith("package", StringComparison.Ordinal));

        var denied = false;
        foreach (var dir in dirs)
        {
            var energyFile = Path.Combine(dir, "energy_uj");
            if (!File.Exists(energyFile)) continue;
            try { _ = File.ReadAllText(energyFile); }
            catch (UnauthorizedAccessException) { denied = true; continue; }
            catch (IOException) { continue; }

            var (label, scope) = Describe(names[dir], packageCount, ZonePackageNumber(dir));
            _ = ulong.TryParse(ReadText(Path.Combine(dir, "max_energy_range_uj")), out var max);
            _zones.Add(new Zone { EnergyFile = energyFile, Label = label, Scope = scope, MaxRange = max });
        }

        if (_zones.Count == 0)
        {
            UnavailableReason = denied
                ? "Reading CPU power needs elevated rights on Linux (the energy counters are root-only)."
                : "No RAPL power zones found (needs an Intel CPU, or a recent AMD CPU and kernel).";
        }
    }

    public string Name => "CPU (RAPL)";
    public string? UnavailableReason { get; }

    public IReadOnlyList<PowerReading> Read()
    {
        var readings = new List<PowerReading>();
        foreach (var zone in _zones)
        {
            if (!ulong.TryParse(ReadText(zone.EnergyFile), out var energy)) continue;
            var now = _time.GetTimestamp();

            if (zone.LastEnergy is { } last)
            {
                var seconds = _time.GetElapsedTime(zone.LastTimestamp, now).TotalSeconds;
                // The counter wraps at max_energy_range_uj; without a known range, skip that one interval.
                var delta = energy >= last ? energy - last : zone.MaxRange > 0 ? zone.MaxRange - last + energy : 0;
                if (seconds > 0 && (energy >= last || zone.MaxRange > 0))
                    readings.Add(new PowerReading(zone.Label, delta / 1_000_000.0 / seconds, zone.Scope,
                        zone.Label.StartsWith("CPU package", StringComparison.Ordinal) ? PowerKind.Cpu : PowerKind.Other));
            }

            zone.LastEnergy = energy;
            zone.LastTimestamp = now;
        }
        return readings;
    }

    public void Dispose() { }

    /// <summary>Maps a RAPL zone name to what the user sees and whether it is a separate component.</summary>
    internal static (string Label, ReadingScope Scope) Describe(string zoneName, int packageCount, int packageNumber) =>
        zoneName switch
        {
            _ when zoneName.StartsWith("package", StringComparison.Ordinal) =>
                (packageCount > 1 ? $"CPU package {packageNumber}" : "CPU package", ReadingScope.Component),
            "dram" => ("Memory (DRAM)", ReadingScope.Component),   // not included in the package figure
            "core" => ("CPU cores", ReadingScope.Breakdown),
            "uncore" => ("CPU graphics / uncore", ReadingScope.Breakdown),
            "psys" => ("Platform (psys)", ReadingScope.Breakdown),  // overlaps the package, so never summed
            _ => (zoneName.Length == 0 ? "RAPL zone" : zoneName, ReadingScope.Breakdown),
        };

    // intel-rapl:1:0 belongs to package 1.
    private int ZonePackageNumber(string dir) =>
        int.TryParse(Path.GetFileName(dir).Split(_separator)[1], out var n) ? n : 0;

    private static string? ReadText(string path)
    {
        try { return File.ReadAllText(path).Trim(); }
        catch (Exception e) when (e is IOException or UnauthorizedAccessException) { return null; }
    }
}
