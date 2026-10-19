using KVGuzzler.Core.Power;

namespace KVGuzzler.Core.History;

/// <summary>One period of history, with measured and estimated energy kept apart.</summary>
/// <param name="MeasuredKwh">What the sensors measured while the collector was sampling.</param>
/// <param name="EstimatedKwh">Sleep time at the user's assumed sleep wattage. An estimate, never shown as a reading.</param>
/// <param name="Monitored">Time actually covered by measurements.</param>
/// <param name="Asleep">Time the machine slept while the collector ran.</param>
/// <param name="Off">Time the collector was stopped on purpose (the PC was off, or the service stopped). Counted as 0 W.</param>
/// <param name="LostPower">Time the collector vanished without a clean stop. Counted as 0 W, but could have been a crash with the PC on.</param>
/// <param name="WallKwh">What the wall meter measured for the whole PC in the period, or null if it recorded nothing.
/// When present this is the headline figure; <paramref name="MeasuredKwh"/> is then the part the CPU, GPU and
/// memory sensors account for.</param>
public sealed record PeriodReport(
    string Label,
    double MeasuredKwh,
    double EstimatedKwh,
    TimeSpan Monitored,
    TimeSpan Asleep,
    TimeSpan Off,
    TimeSpan LostPower,
    double? WallKwh = null)
{
    public double TotalKwh => MeasuredKwh + EstimatedKwh;
}

public static class HistoryReport
{
    public static PeriodReport Build(HistoryStore store, string label, DateTimeOffset from, DateTimeOffset to, double sleepWatts)
    {
        var wall = store.Query(from, to, ReadingScope.Wall);
        var hasWall = wall.Monitored > TimeSpan.Zero;
        // With a wall meter, the sensors' share is counted only over the minutes the wall meter also recorded:
        // sensors that ran before the plug was set up would otherwise look like more than the whole PC.
        var (kwh, monitored, gaps) = store.Query(from, to, ReadingScope.Component, onlyWhereWallWasRecorded: hasWall);

        TimeSpan Sum(GapReason reason) =>
            gaps.Where(g => g.Reason == reason).Aggregate(TimeSpan.Zero, (t, g) => t + (g.End - g.Start));

        var asleep = Sum(GapReason.Asleep);
        return new PeriodReport(label, kwh, CostCalculator.KilowattHours(sleepWatts, asleep),
            monitored, asleep, Sum(GapReason.Off), Sum(GapReason.LostPower),
            hasWall ? wall.KilowattHours : null);
    }

    /// <summary>The rolling periods the Dashboard shows.</summary>
    public static IReadOnlyList<PeriodReport> Standard(HistoryStore store, DateTimeOffset now, double sleepWatts) => new[]
    {
        Build(store, "Last hour", now.AddHours(-1), now, sleepWatts),
        Build(store, "Last 24 hours", now.AddDays(-1), now, sleepWatts),
        Build(store, "Last 7 days", now.AddDays(-7), now, sleepWatts),
        Build(store, "Last 30 days", now.AddDays(-30), now, sleepWatts),
    };
}
