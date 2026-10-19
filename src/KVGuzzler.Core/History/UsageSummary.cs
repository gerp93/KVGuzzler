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
public sealed record UsageStat(double Kwh, decimal? Cost, bool WholePc, DateTimeOffset? CountedSince, bool PriceEstimated, DateTimeOffset From);

public static class UsageSummary
{
    /// <summary>Energy and cost over the last <paramref name="span"/>, or over everything ever recorded when it is null.
    /// The whole PC from the wall meter is used if it recorded anything in the period, otherwise the sensors.</summary>
    public static UsageStat Compute(HistoryStore store, RateSchedule schedule, TimeSpan? span, DateTimeOffset now)
    {
        var from = span is { } s ? now - s : store.FirstRecorded() ?? now;
        if (from > now) from = now;

        var wholePc = store.EnergyKwh(from, now, ReadingScope.Wall) > 0;
        var scope = wholePc ? ReadingScope.Wall : ReadingScope.Component;

        // The wall meter may have been connected partway through the period: say so rather than imply it covers all of it.
        DateTimeOffset? since = null;
        if (wholePc && store.FirstRecorded(ReadingScope.Wall) is { } firstWall && firstWall > from.AddMinutes(10)) since = firstWall;

        var cost = schedule.Cost((a, b) => store.EnergyKwh(a, b, scope), from, now);
        return new UsageStat(cost.Kwh, cost.Unpriced ? null : cost.Cost, wholePc, since, cost.Estimated, from);
    }
}
