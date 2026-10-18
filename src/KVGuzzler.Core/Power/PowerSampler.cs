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
                    readings.Add(r);
                }
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

    public void Dispose()
    {
        foreach (var s in _sources) s.Dispose();
    }
}
