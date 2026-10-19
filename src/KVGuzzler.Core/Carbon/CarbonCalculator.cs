using KVGuzzler.Core.History;
using KVGuzzler.Core.Power;

namespace KVGuzzler.Core.Carbon;

/// <summary>An estimate of the carbon the grid emitted to make the electricity used in a period.</summary>
/// <param name="Kg">Kilograms of CO2 equivalent.</param>
/// <param name="Kwh">The energy it was worked out for.</param>
/// <param name="EstimatedKwh">The part of <paramref name="Kwh"/> valued at an average because its own hour had no saved intensity (the
/// collector was not running then, or the feed was not set up yet), and the energy the plugs' counters measured across gaps.</param>
public sealed record CarbonResult(double Kg, double Kwh, double EstimatedKwh)
{
    /// <summary>The energy-weighted average intensity over the period.</summary>
    public double AverageGramsPerKwh => Kwh > 0 ? Kg * 1000 / Kwh : 0;

    /// <summary>How much of the energy was valued at an average rather than its own hour's figure (0 to 1).</summary>
    public double EstimatedShare => Kwh > 0 ? Math.Clamp(EstimatedKwh / Kwh, 0, 1) : 0;
}

/// <summary>
/// Turns saved energy into carbon: each hour's kWh times that hour's grid intensity. Energy in an hour with no intensity of its own
/// (the free key gives no history, so anything from before the feed was set up or while the collector was off) is valued at the average of
/// the hours that do have one, so a period never shows zero just for missing data, and says how much was valued that way.
/// </summary>
/// <remarks>This is the average intensity of the whole grid (location based), the usual way of reporting it. It is not what one more
/// kWh would cause, which is usually higher.</remarks>
public static class CarbonCalculator
{
    /// <summary>The carbon for the PC and/or the named extra meters between two instants, or null when no intensity has ever been saved
    /// (the feed is not set up, or has not got a reading yet).</summary>
    /// <param name="includePc">The PC: the whole PC at the wall when the wall meter recorded in the period, otherwise the sensors.</param>
    /// <param name="peripherals">The extra meters to add on top (or alone when <paramref name="includePc"/> is false).</param>
    public static CarbonResult? Compute(HistoryStore store, DateTimeOffset from, DateTimeOffset to, bool includePc = true,
        IReadOnlyCollection<string>? peripherals = null)
    {
        peripherals ??= Array.Empty<string>();
        if (store.AverageCarbonEver() is not { } everAverage) return null;

        var known = store.QueryCarbon(from, to);
        var periodAverage = known.Count > 0 ? known.Values.Average() : everAverage;

        double kg = 0, kwh = 0, estimated = 0;

        void Add(IEnumerable<(long Hour, double Kwh)> hours)
        {
            foreach (var (hour, energy) in hours)
            {
                var own = known.TryGetValue(hour, out var grams);
                kg += energy * (own ? grams : periodAverage) / 1000.0;
                kwh += energy;
                if (!own) estimated += energy;
            }
        }

        // Energy the plugs' own counters measured across gaps has no hour of its own: it is valued at the period's average.
        void AddUnhoured(double energy)
        {
            if (energy <= 0) return;
            kg += energy * periodAverage / 1000.0;
            kwh += energy;
            estimated += energy;
        }

        if (includePc)
        {
            var wholePc = store.EnergyKwh(from, to, ReadingScope.Wall) > 0;
            Add(store.HourlyKwh(from, to, wholePc ? ReadingScope.Wall : ReadingScope.Component));
            if (wholePc) AddUnhoured(store.PlugGapKwh(from, to));
        }
        if (peripherals.Count > 0)
        {
            Add(store.HourlyKwh(from, to, ReadingScope.Peripheral, peripherals));
            AddUnhoured(store.PeripheralGapKwh(from, to, peripherals));
        }
        return new CarbonResult(kg, kwh, estimated);
    }
}
