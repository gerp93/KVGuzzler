using System.Globalization;
using Avalonia.Headless.XUnit;
using KVGuzzler.Collector;
using KVGuzzler.Core.History;
using KVGuzzler.Core.Power;
using KVGuzzler.Views;
using Microsoft.Data.Sqlite;
using Xunit;

namespace KVGuzzler.Tests;

public class ChartLayoutTests
{
    [Theory]
    [InlineData(0, 10)]
    [InlineData(-5, 10)]
    [InlineData(7, 10)]
    [InlineData(10, 10)]
    [InlineData(11, 20)]
    [InlineData(130, 200)]
    [InlineData(250, 500)]
    [InlineData(501, 1000)]
    [InlineData(1200, 2000)]
    public void TheScaleTopIsOneTwoFiveOrTenTimesAPowerOfTen(double max, double expected) =>
        Assert.Equal(expected, ChartLayout.NiceMax(max));

    [Fact]
    public void TheWattsScaleIsEvenlySpacedFromZero() =>
        Assert.Equal(new[] { 0.0, 50, 100, 150, 200 }, ChartLayout.YTicks(200));

    [Theory]
    [InlineData(1)]
    [InlineData(24)]
    [InlineData(24 * 7)]
    [InlineData(24 * 30)]
    public void TimeTicksAreFewEvenAndOnRoundTimes(int hours)
    {
        var to = new DateTimeOffset(2026, 10, 18, 20, 37, 12, DateTimeOffset.Now.Offset);
        var from = to.AddHours(-hours);

        var ticks = ChartLayout.TimeTicks(from, to);

        Assert.InRange(ticks.Count, 2, 7);
        Assert.All(ticks, t => Assert.InRange(t, from, to));
        var steps = ticks.Zip(ticks.Skip(1), (a, b) => b - a).Distinct().ToList();
        Assert.Single(steps);                                                    // evenly spaced
        Assert.All(ticks, t => Assert.Equal(0, t.ToLocalTime().Second));         // on a minute boundary at least
        if (steps[0] < TimeSpan.FromHours(1))
            Assert.All(ticks, t => Assert.Equal(0, t.ToLocalTime().Minute % (int)steps[0].TotalMinutes));
        else if (steps[0] < TimeSpan.FromDays(1))
            Assert.All(ticks, t => Assert.Equal(0, t.ToLocalTime().Minute));      // whole hours
    }

    [Fact]
    public void TimeLabelsSuitTheSpan()
    {
        CultureInfo.CurrentCulture = CultureInfo.InvariantCulture;
        var t = new DateTimeOffset(2026, 10, 18, 20, 5, 0, DateTimeOffset.Now.Offset).ToUniversalTime();
        var local = t.ToLocalTime();
        Assert.Equal(local.ToString("HH:mm"), ChartLayout.TimeLabel(t, TimeSpan.FromHours(24)));
        Assert.Equal(local.ToString("ddd HH:mm"), ChartLayout.TimeLabel(t, TimeSpan.FromDays(7)));
        Assert.Equal(local.ToString("MMM d"), ChartLayout.TimeLabel(t, TimeSpan.FromDays(30)));
    }

    private static readonly DateTimeOffset T0 = new(2026, 10, 18, 12, 0, 0, TimeSpan.Zero);

    [Fact]
    public void ALineBreaksWhereNothingWasRecorded()
    {
        var minute = TimeSpan.FromMinutes(1);
        var points = new List<ChartPoint>();
        for (var i = 0; i < 5; i++) points.Add(new ChartPoint(T0.AddMinutes(i), 100, 100));
        for (var i = 30; i < 33; i++) points.Add(new ChartPoint(T0.AddMinutes(i), 100, 100));   // after a long gap
        points.Add(new ChartPoint(T0.AddMinutes(60), 100, 100));                                // alone

        var segments = ChartLayout.Segments(points, minute);

        Assert.Equal(new[] { 5, 3, 1 }, segments.Select(s => s.Count));
        Assert.Equal(new[] { 0, 1, 2, 3, 4 }, segments[0]);
    }

    [Fact]
    public void AMissedBucketOrTwoIsNotAGap()
    {
        var points = new[] { new ChartPoint(T0, 1, 1), new ChartPoint(T0.AddMinutes(1), 1, 1), new ChartPoint(T0.AddMinutes(2.4), 1, 1) };
        Assert.Single(ChartLayout.Segments(points, TimeSpan.FromMinutes(1)));    // 1.4 buckets apart: still one line
    }

    [Fact]
    public void NearestFindsThePointClosestInTime()
    {
        var points = new[] { new ChartPoint(T0, 1, 1), new ChartPoint(T0.AddMinutes(10), 2, 2), new ChartPoint(T0.AddMinutes(20), 3, 3) };
        Assert.Equal(1, ChartLayout.Nearest(points, T0.AddMinutes(8)));
        Assert.Equal(2, ChartLayout.Nearest(points, T0.AddHours(5)));
        Assert.Equal(-1, ChartLayout.Nearest(Array.Empty<ChartPoint>(), T0));
    }
}

public class PowerChartDataTests
{
    private static readonly DateTimeOffset T0 = new(2026, 10, 18, 12, 0, 0, TimeSpan.Zero);

    /// <summary>A minute at constant watts, as the collector would have saved it.</summary>
    private static MinuteRow Minute(DateTimeOffset at, string name, ReadingScope scope, PowerKind kind, double watts, double peak = 0) =>
        new(at.ToUnixTimeSeconds() / 60 * 60, name, scope, watts * 60 / 3_600_000.0, 60, peak > 0 ? peak : watts, kind);

    [Fact]
    public void EachLineIsAverageWattsPerBucketAndTwoGpusAdd()
    {
        using var h = new TempHistory();
        for (var i = 0; i < 10; i++)
        {
            var at = T0.AddMinutes(i);
            h.Store.AddMinutes(new[]
            {
                Minute(at, "CPU package", ReadingScope.Component, PowerKind.Cpu, 50),
                Minute(at, "GPU A", ReadingScope.Component, PowerKind.Gpu, 100, peak: 140),
                Minute(at, "GPU B", ReadingScope.Component, PowerKind.Gpu, 20),
                Minute(at, "CPU cores", ReadingScope.Breakdown, PowerKind.Other, 40),       // part of the package: no line of its own
                Minute(at, "Whole PC (wall meter)", ReadingScope.Wall, PowerKind.Other, 230),
            });
        }

        var chart = PowerChart.Build(h.Store, T0, T0.AddMinutes(10), TimeSpan.FromMinutes(5));

        Assert.Equal(new[] { "Whole PC", "CPU", "GPU" }, chart.Series.Select(s => s.Name));
        Assert.All(chart.Series, s => Assert.Equal(2, s.Points.Count));                      // ten minutes in five-minute buckets
        Assert.Equal(230, chart.Series[0].Points[0].Watts, precision: 6);
        Assert.Equal(50, chart.Series[1].Points[0].Watts, precision: 6);
        Assert.Equal(120, chart.Series[2].Points[0].Watts, precision: 6);                    // 100 + 20
        Assert.Equal(160, chart.Series[2].Points[0].PeakWatts, precision: 6);                // 140 + 20
        Assert.Equal(T0.AddSeconds(150), chart.Series[1].Points[0].Time);                    // the middle of its bucket
    }

    [Fact]
    public void ALineWithNoDataIsLeftOut()
    {
        using var h = new TempHistory();
        h.Store.AddMinutes(new[] { Minute(T0, "CPU package", ReadingScope.Component, PowerKind.Cpu, 50) });
        var chart = PowerChart.Build(h.Store, T0, T0.AddMinutes(5), TimeSpan.FromMinutes(1));
        Assert.Equal("CPU", Assert.Single(chart.Series).Name);   // no wall meter, no GPU
    }

    [Fact]
    public void ABucketOnlyPartlyRecordedKeepsItsRealPower()
    {
        using var h = new TempHistory();
        // Only two of the five minutes were recorded, at 80 W: the bucket is 80 W, not 32.
        for (var i = 0; i < 2; i++) h.Store.AddMinutes(new[] { Minute(T0.AddMinutes(i), "CPU package", ReadingScope.Component, PowerKind.Cpu, 80) });
        var chart = PowerChart.Build(h.Store, T0, T0.AddMinutes(5), TimeSpan.FromMinutes(5));
        Assert.Equal(80, Assert.Single(chart.Series[0].Points).Watts, precision: 6);
    }

    [Fact]
    public void GapsComeWithTheChartSoTheyCanBeShaded()
    {
        using var h = new TempHistory();
        h.Store.AddGap(new Gap(T0.AddMinutes(2), T0.AddMinutes(4), GapReason.Asleep));
        var chart = PowerChart.Build(h.Store, T0, T0.AddMinutes(10), TimeSpan.FromMinutes(1));
        Assert.Equal(GapReason.Asleep, Assert.Single(chart.Gaps).Reason);
    }

    [Fact]
    public void HistoryFromBeforeTheKindColumnIsUpgradedAndTheCpuStillFound()
    {
        var dir = Path.Combine(Path.GetTempPath(), "kvguzzler-old-" + Guid.NewGuid().ToString("N")[..8]);
        Directory.CreateDirectory(dir);
        var path = Path.Combine(dir, HistoryStore.FileName);
        try
        {
            using (var c = new SqliteConnection($"Data Source={path};Pooling=False"))
            {
                c.Open();
                using var cmd = c.CreateCommand();
                cmd.CommandText = """
                    CREATE TABLE samples (minute_start INTEGER NOT NULL, name TEXT NOT NULL, scope INTEGER NOT NULL,
                        kwh REAL NOT NULL, seconds REAL NOT NULL, peak_watts REAL NOT NULL, PRIMARY KEY (minute_start, name));
                    """;
                cmd.ExecuteNonQuery();
                cmd.CommandText = $"""
                    INSERT INTO samples VALUES ({T0.ToUnixTimeSeconds()}, 'CPU package', 0, {50 * 60 / 3_600_000.0:R}, 60, 50);
                    INSERT INTO samples VALUES ({T0.ToUnixTimeSeconds()}, 'NVIDIA GeForce', 0, {80 * 60 / 3_600_000.0:R}, 60, 80);
                    """;
                cmd.ExecuteNonQuery();
            }

            using var store = new HistoryStore(path);                       // opening upgrades it
            var chart = PowerChart.Build(store, T0, T0.AddMinutes(5), TimeSpan.FromMinutes(5));

            Assert.Equal("CPU", Assert.Single(chart.Series).Name);          // found by name; the GPU's kind was never saved
            Assert.Equal(50, chart.Series[0].Points[0].Watts, precision: 6);
            store.AddMinutes(new[] { Minute(T0.AddMinutes(1), "NVIDIA GeForce", ReadingScope.Component, PowerKind.Gpu, 90) });   // new rows carry the kind
            Assert.Contains(PowerChart.Build(store, T0, T0.AddMinutes(5), TimeSpan.FromMinutes(5)).Series, s => s.Name == "GPU");
        }
        finally
        {
            SqliteConnection.ClearAllPools();
            try { Directory.Delete(dir, recursive: true); } catch (IOException) { /* best effort */ }
        }
    }

    private sealed class Hardware : IPowerSource
    {
        public string Name => "hardware";
        public string? UnavailableReason => null;
        public IReadOnlyList<PowerReading> Read() => new[]
        {
            new PowerReading("CPU package", 60, ReadingScope.Component, PowerKind.Cpu),
            new PowerReading("GPU", 120, ReadingScope.Component, PowerKind.Gpu),
        };
        public void Dispose() { }
    }

    [Fact]
    public void TheCollectorSavesWhichRowsAreTheCpuAndTheGpu()
    {
        using var h = new TempHistory();
        var c = new PowerCollector(h.Store, new PowerSampler(new IPowerSource[] { new Hardware() }));
        c.Start(T0);
        for (var i = 0; i <= 120; i++) c.Tick(T0.AddSeconds(i));
        c.Stop();

        var chart = PowerChart.Build(h.Store, T0, T0.AddMinutes(5), TimeSpan.FromMinutes(1));
        Assert.Equal(60, chart.Series.Single(s => s.Name == "CPU").Points[0].Watts, precision: 3);
        Assert.Equal(120, chart.Series.Single(s => s.Name == "GPU").Points[0].Watts, precision: 3);
    }
}

public class ChartUiTests
{
    [AvaloniaFact]
    public async Task TheDashboardChartFillsFromTheHistoryAndFollowsThePeriod()
    {
        CultureInfo.CurrentCulture = CultureInfo.InvariantCulture;
        using var t = new TempData();
        t.Services.QueryCollectorService = () => ServiceState.Running;
        var now = DateTimeOffset.UtcNow;
        using (var store = new HistoryStore(Path.Combine(t.HistoryDir, HistoryStore.FileName)))
        {
            for (var i = 1; i <= 30; i++)
                store.AddMinutes(new[]
                {
                    new MinuteRow(now.AddMinutes(-i).ToUnixTimeSeconds() / 60 * 60, "CPU package", ReadingScope.Component, 50.0 * 60 / 3_600_000, 60, 60, PowerKind.Cpu),
                    new MinuteRow(now.AddMinutes(-i).ToUnixTimeSeconds() / 60 * 60, "GPU", ReadingScope.Component, 100.0 * 60 / 3_600_000, 60, 120, PowerKind.Gpu),
                });
            store.SetLive(now, new[] { new PowerReading("CPU package", 50, ReadingScope.Component, PowerKind.Cpu) }, Array.Empty<string>());
        }

        var window = new MainWindow(t.Services);
        window.Show();
        await window.RefreshCollectorAsync(includeService: true);

        var data = window.PowerChartView.Data!;
        Assert.Equal(new[] { "CPU", "GPU" }, data.Series.Select(s => s.Name));
        Assert.Equal(TimeSpan.FromDays(1), data.To - data.From);                       // the default is the last 24 hours
        Assert.Contains("Set up a wall meter", window.ChartNoteText.Text);              // no whole-PC line yet

        window.ChartPeriodBox.SelectedIndex = 0;                                        // last hour
        await window.RefreshCollectorAsync(includeService: false);
        Assert.Equal(TimeSpan.FromHours(1), window.PowerChartView.Data!.To - window.PowerChartView.Data.From);
        Assert.Equal(TimeSpan.FromMinutes(1), window.PowerChartView.Data.Bucket);
    }

    [AvaloniaFact]
    public async Task WithoutHistoryTheChartSaysWhy()
    {
        CultureInfo.CurrentCulture = CultureInfo.InvariantCulture;
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();
        await window.RefreshCollectorAsync(includeService: true);

        Assert.Null(window.PowerChartView.Data);
        Assert.Contains("No history yet", window.ChartNoteText.Text);
    }
}
