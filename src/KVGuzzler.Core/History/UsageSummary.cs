using KVGuzzler.Core.Billing;
using KVGuzzler.Core.Power;

namespace KVGuzzler.Core.History;

/// <summary>The two headline numbers for a period: the energy used and what it cost.</summary>
/// <param name="Kwh">Energy used in the period.</param>
/// <param name="Cost">What it cost at the price on each day, or null when no price is known yet.</param>
/// <param name="WholePc">True when the energy is the whole PC at the wall; false when it is only the CPU, GPU and memory sensors.</param>
/// <param name="CountedSince">When the wall meter started recording, if that was after the period began (so the figure
/// covers less than the whole period); null otherwise.</param>
/// <param name="PriceEstimated">Part of the cost used a stand-in price (days after your latest bill).</param>
/// <param name="Peak">The highest power drawn in the period (whole PC at the wall when <paramref name="WholePc"/>, otherwise the sensors'
/// total), or null when nothing was recorded.</param>
/// <param name="PeripheralKwh">The part of <paramref name="Kwh"/> that came from extra meters (monitor, speakers) the user chose to include; zero when none.
/// Those meters only record while the collector runs, so unlike the wall meter nothing is added for time the PC slept or was off.</param>
/// <param name="PeripheralCount">How many extra meters are included (the peak is then only an upper bound).</param>
public sealed record UsageStat(double Kwh, decimal? Cost, bool WholePc, DateTimeOffset? CountedSince, bool PriceEstimated, DateTimeOffset From,
    PeakWatts? Peak = null, double PeripheralKwh = 0, int PeripheralCount = 0);

/// <summary>What the PC actually used per day, from real history, for projecting a bill.</summary>
/// <param name="Kwh">Energy used over the stretch measured.</param>
/// <param name="Days">How long that stretch is (it starts when recording did, if that was after the period began).</param>
/// <param name="WholePc">True when the energy is the whole PC at the wall; false when it is only the CPU, GPU and memory sensors.</param>
/// <param name="PeripheralCount">How many extra meters are added into <paramref name="Kwh"/>.</param>
public sealed record AverageUse(double Kwh, double Days, bool WholePc, DateTimeOffset From, int PeripheralCount = 0)
{
    public double KwhPerDay => Days > 0 ? Kwh / Days : 0;

    /// <summary>The average power over the stretch, counting the time the PC was off or asleep as zero.</summary>
    public double AverageWatts => KwhPerDay * 1000 / 24;
}

public static class UsageSummary
{
    /// <summary>Less recorded time than this gives an average too noisy to project from.</summary>
    public static readonly TimeSpan MinimumForAverage = TimeSpan.FromHours(1);

    /// <summary>What a day costs on average over the last <paramref name="span"/> (or all recorded time when null), counting
    /// the time the PC was off or asleep, so it projects what a bill would really be. Null when less than an hour has been
    /// recorded. The whole PC is used if the wall meter recorded anything in the period, otherwise the sensors.</summary>
    public static AverageUse? AverageDay(HistoryStore store, TimeSpan? span, DateTimeOffset now, IReadOnlyCollection<string>? peripherals = null)
    {
        peripherals ??= Array.Empty<string>();
        var from = span is { } s ? now - s : DateTimeOffset.UnixEpoch;
        var wholePc = store.EnergyKwh(from, now, ReadingScope.Wall) > 0;
        var scope = wholePc ? ReadingScope.Wall : ReadingScope.Component;

        // Recording may have started partway through the period: average over the time that was actually recorded.
        if (store.FirstRecorded(scope) is not { } first) return null;
        if (first > from) from = first;
        if (now - from < MinimumForAverage) return null;
        return new AverageUse(store.EnergyKwh(from, now, scope) + store.PeripheralKwh(from, now, peripherals),
            (now - from).TotalDays, wholePc, from, peripherals.Count);
    }

    /// <summary>Energy and cost over the last <paramref name="span"/>, or over everything ever recorded when it is null.
    /// The whole PC from the wall meter is used if it recorded anything in the period, otherwise the sensors.</summary>
    /// <param name="peripherals">The extra meters (monitor, speakers) to count on top of the PC: their energy is added to the energy and
    /// the cost, and their peaks to the peak. None (the default) is the PC alone.</param>
    public static UsageStat Compute(HistoryStore store, RateSchedule schedule, TimeSpan? span, DateTimeOffset now,
        IReadOnlyCollection<string>? peripherals = null)
    {
        peripherals ??= Array.Empty<string>();
        var from = span is { } s ? now - s : store.FirstRecorded() ?? now;
        if (from > now) from = now;

        var wholePc = store.EnergyKwh(from, now, ReadingScope.Wall) > 0;
        var scope = wholePc ? ReadingScope.Wall : ReadingScope.Component;

        // The wall meter may have been connected partway through the period: say so rather than imply it covers all of it.
        DateTimeOffset? since = null;
        if (wholePc && store.FirstRecorded(ReadingScope.Wall) is { } firstWall && firstWall > from.AddMinutes(10)) since = firstWall;

        // The extra meters are priced like the PC, day by day, so a period that spans two bills is right for them too.
        var cost = schedule.Cost((a, b) => store.EnergyKwh(a, b, scope) + store.PeripheralKwh(a, b, peripherals), from, now);

        var peak = store.QueryPeak(from, now, scope);
        if (store.QueryPeripheralPeak(from, now, peripherals) is { } extra)
            peak = peak is null
                ? extra
                : new PeakWatts(peak.Watts + extra.Watts, peak.At, Exact: false);   // they do not all peak in the same second

        return new UsageStat(cost.Kwh, cost.Unpriced ? null : cost.Cost, wholePc, since, cost.Estimated, from, peak,
            store.PeripheralKwh(from, now, peripherals), peripherals.Count);
    }
}
