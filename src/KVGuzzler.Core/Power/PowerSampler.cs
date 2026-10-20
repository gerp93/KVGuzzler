namespace KVGuzzler.Core.Power;

/// <summary>Everything known at one sampling instant.</summary>
/// <param name="Readings">All readings from all working sources.</param>
/// <param name="TotalWatts">Sum of the <see cref="ReadingScope.Component"/> readings: what the measured
/// hardware draws. It is not the whole machine (fans, drives, motherboard and PSU losses are not measured).</param>
/// <param name="SessionKilowattHours">Energy of the measured components since the sampler started.</param>
/// <param name="Notes">Sources that are unavailable or failed on this sample, one sentence each.</param>
public sealed record PowerSnapshot(
    DateTimeOffset Time,
    IReadOnlyList<PowerReading> Readings,
    double TotalWatts,
    double SessionKilowattHours,
    IReadOnlyList<string> Notes);

/// <summary>Polls a set of power sources and integrates the measured total into energy.</summary>
public sealed class PowerSampler : IDisposable
{
    /// <summary>No single sensor (a CPU package, a GPU board) draws more than this; a bigger answer is a driver glitch.
    /// NVML in particular answers with nonsense (tens to hundreds of kilowatts) on the first read after the PC wakes.</summary>
    public const double MaxComponentWatts = 2000;

    /// <summary>The same for a smart plug (the most it can switch), for the wall meter and the extra meters.</summary>
    public const double MaxMeterWatts = 4000;

    private readonly IReadOnlyList<IPowerSource> _sources;
    private readonly EnergyMeter _total;

    public PowerSampler(IEnumerable<IPowerSource> sources, TimeSpan? maxGap = null)
    {
        _sources = sources.ToList();
        _total = new EnergyMeter(maxGap);
    }

    public IReadOnlyList<IPowerSource> Sources => _sources;

    public PowerSnapshot Sample(DateTimeOffset now)
    {
        var readings = new List<PowerReading>();
        var notes = new List<string>();

        foreach (var source in _sources)
        {
            if (source.UnavailableReason is { } why)
            {
                notes.Add($"{source.Name}: {why}");
                continue;
            }

            try
            {
                foreach (var r in source.Read())
                {
                    // A broken sensor must not poison the total or throw out of the energy meter.
                    if (double.IsNaN(r.Watts) || double.IsInfinity(r.Watts) || r.Watts < 0) continue;
                    // An impossible answer is dropped like a missing one, so nothing averages against it.
                    if (r.Watts > MaxWattsFor(r.Scope))
                    {
                        notes.Add($"{r.Name}: ignored an impossible reading ({r.Watts:0} W).");
                        continue;
                    }
                    readings.Add(r);
                }
                if (source is IPowerSourceNotes extra) notes.AddRange(extra.Notes);
            }
            catch (Exception e)
            {
                notes.Add($"{source.Name}: {e.Message}");
            }
        }

        var total = readings.Where(r => r.Scope == ReadingScope.Component).Sum(r => r.Watts);
        // With nothing measured there is nothing to integrate; a gap is better than invented zeros.
        if (readings.Any(r => r.Scope == ReadingScope.Component)) _total.AddSample(now, total);

        return new PowerSnapshot(now, readings, total, _total.KilowattHours, notes);
    }

    /// <summary>The most a reading of this kind can plausibly be: the plugs for the wall and extra meters, a sensor otherwise.</summary>
    public static double MaxWattsFor(ReadingScope scope) =>
        scope is ReadingScope.Wall or ReadingScope.Peripheral ? MaxMeterWatts : MaxComponentWatts;

    public void Dispose()
    {
        foreach (var s in _sources) s.Dispose();
    }
}
