namespace KVGuzzler.Core;

/// <summary>
/// Integrates power readings over time into energy: kWh = sum(watts x seconds) / 3.6e6.
/// Uses the real timestamp between readings, not a nominal sampling interval, and refuses
/// to bridge a gap longer than <see cref="MaxGap"/> (the machine was asleep, or the app
/// was not sampling) so a pause is never billed as if the load had continued.
/// </summary>
public sealed class EnergyMeter
{
    private DateTimeOffset? _lastTime;
    private double _lastWatts;

    public EnergyMeter(TimeSpan? maxGap = null) => MaxGap = maxGap ?? TimeSpan.FromMinutes(1);

    public TimeSpan MaxGap { get; }

    public double KilowattHours { get; private set; }

    /// <summary>Total time covered by readings that were integrated (gaps excluded).</summary>
    public TimeSpan Covered { get; private set; }

    public void AddSample(DateTimeOffset time, double watts)
    {
        if (double.IsNaN(watts) || double.IsInfinity(watts) || watts < 0)
            throw new ArgumentOutOfRangeException(nameof(watts), watts, "Power must be a finite, non-negative number of watts.");

        if (_lastTime is { } last)
        {
            var elapsed = time - last;
            // Out-of-order samples and long gaps add nothing.
            if (elapsed > TimeSpan.Zero && elapsed <= MaxGap)
            {
                // Trapezoid between the two readings.
                KilowattHours += (_lastWatts + watts) / 2.0 * elapsed.TotalSeconds / 3_600_000.0;
                Covered += elapsed;
            }
        }

        _lastTime = time;
        _lastWatts = watts;
    }
}
