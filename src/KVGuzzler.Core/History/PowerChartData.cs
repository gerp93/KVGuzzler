namespace KVGuzzler.Core.History;

/// <summary>One point on a chart line: the average power over a stretch of time, and the highest seen in it.</summary>
public sealed record ChartPoint(DateTimeOffset Time, double Watts, double PeakWatts);

/// <param name="Variant">Which of several lines of the same kind this is (each extra meter is its own line), so they can be drawn apart.</param>
public sealed record ChartSeries(string Name, ChartSeriesKind Kind, IReadOnlyList<ChartPoint> Points, int Variant = 0);

/// <summary>Everything the power chart draws for a period: the lines and the stretches nothing was recorded.</summary>
public sealed record PowerChartData(
    DateTimeOffset From, DateTimeOffset To, TimeSpan Bucket, IReadOnlyList<ChartSeries> Series, IReadOnlyList<Gap> Gaps);

/// <summary>Builds the chart's data from the saved history.</summary>
public static class PowerChart
{
    /// <summary>The periods the Dashboard offers, with a bucket sized to give a few hundred points each.</summary>
    public static readonly IReadOnlyList<(string Label, TimeSpan Span, TimeSpan Bucket)> Periods = new[]
    {
        ("Last hour", TimeSpan.FromHours(1), TimeSpan.FromMinutes(1)),
        ("Last 24 hours", TimeSpan.FromDays(1), TimeSpan.FromMinutes(5)),
        ("Last 7 days", TimeSpan.FromDays(7), TimeSpan.FromMinutes(30)),
        ("Last 30 days", TimeSpan.FromDays(30), TimeSpan.FromHours(2)),
    };

    /// <summary>The whole-PC line (when a wall meter recorded), the CPU and the GPU, as average watts per bucket.
    /// A line with no data in the period is left out.</summary>
    /// <param name="peripherals">Extra meters to draw, each as its own line named by its label; none leaves them out.</param>
    /// <param name="includePc">False to leave the whole-PC, CPU and GPU lines out (the "peripherals only" view).</param>
    public static PowerChartData Build(HistoryStore store, DateTimeOffset from, DateTimeOffset to, TimeSpan bucket,
        IReadOnlyCollection<string>? peripherals = null, bool includePc = true)
    {
        var series = new List<ChartSeries>();
        if (includePc)
            foreach (var (name, kind) in new[] { ("Whole PC", ChartSeriesKind.Wall), ("CPU", ChartSeriesKind.Cpu), ("GPU", ChartSeriesKind.Gpu) })
            {
                var points = Bucketed(store.QuerySeries(from, to, kind), bucket);
                if (points.Count > 0) series.Add(new ChartSeries(name, kind, points));
            }

        var variant = 0;
        foreach (var label in peripherals ?? Array.Empty<string>())
        {
            var points = Bucketed(store.QuerySeries(from, to, ChartSeriesKind.Peripheral, new[] { label }), bucket);
            if (points.Count > 0) series.Add(new ChartSeries(label, ChartSeriesKind.Peripheral, points, variant++));
        }
        return new PowerChartData(from, to, bucket, series, store.Query(from, to).Gaps);
    }

    /// <summary>Averages minutes into buckets. A bucket's watts are its energy over the time it was actually
    /// sampled, so a half-recorded bucket is not drawn as half the power.</summary>
    internal static IReadOnlyList<ChartPoint> Bucketed(IReadOnlyList<SeriesMinute> minutes, TimeSpan bucket)
    {
        var size = Math.Max((long)bucket.TotalSeconds, 60);
        return minutes
            .GroupBy(m => m.MinuteStart / size * size)
            .Select(g =>
            {
                var seconds = g.Sum(m => m.Seconds);
                var watts = seconds > 0 ? g.Sum(m => m.KilowattHours) * 3_600_000.0 / seconds : 0;
                return new ChartPoint(
                    DateTimeOffset.FromUnixTimeSeconds(g.Key + size / 2),
                    watts,
                    Math.Max(g.Max(m => m.PeakWatts), watts));
            })
            .OrderBy(p => p.Time)
            .ToList();
    }
}
