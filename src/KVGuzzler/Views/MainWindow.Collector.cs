using System.Globalization;
using Avalonia;
using Avalonia.Threading;
using KVGuzzler.Collector;
using KVGuzzler.Core;
using KVGuzzler.Core.Billing;
using KVGuzzler.Core.History;
using KVGuzzler.Core.Power;
using KVGuzzler.Core.Programs;
using KVGuzzler.Data;

namespace KVGuzzler.Views;

/// <summary>One period of history on the Dashboard: a headline and a smaller line of detail.</summary>
public sealed record HistoryRow(string Title, string Detail);

// The Dashboard's view of the background collector: whether it is running, the readings it publishes,
// and the history it has saved.
public partial class MainWindow
{
    /// <summary>The collector counts as running if it saved a reading this recently.</summary>
    private static readonly TimeSpan Fresh = TimeSpan.FromSeconds(5);

    /// <summary>Watts a sleeping desktop is assumed to draw until the user says otherwise.</summary>
    internal const double DefaultSleepWatts = 2;

    private DispatcherTimer? _collectorTimer;
    private int _ticks;
    private bool _collectorAlive;
    private ServiceState? _serviceState;

    private void StartCollectorDisplay()
    {
        SleepWattsBox.Text = _services.Database?.GetSetting(SettingKeys.SleepWatts) ?? DefaultSleepWatts.ToString(CultureInfo.InvariantCulture);
        SaveSleepButton.Click += (_, _) => SaveSleepWatts();
        ProgramPeriodBox.SelectionChanged += (_, _) => _ = RefreshCollectorAsync(includeService: false, includeChart: false);
        ChartPeriodBox.SelectionChanged += (_, _) => _ = RefreshCollectorAsync(includeService: false, includeChart: true);
        InstallCollectorButton.Click += async (_, _) => await ChangeCollectorAsync(install: true);
        RemoveCollectorButton.Click += async (_, _) => await ChangeCollectorAsync(install: false);

        Opened += async (_, _) =>
        {
            await RefreshCollectorAsync(includeService: true);
            _collectorTimer = new DispatcherTimer { Interval = TimeSpan.FromSeconds(1) };
            _collectorTimer.Tick += async (_, _) =>
            {
                // History sums over up to 30 days and asking Windows about the service starts a process,
                // so those run less often than the one-second live readings.
                _ticks++;
                // The chart reads up to 30 days of history, so it is redrawn every ten seconds, not every second.
                await RefreshCollectorAsync(includeService: _ticks % 10 == 0, includeChart: _ticks % 10 == 0);
            };
            _collectorTimer.Start();
        };
        Closed += (_, _) => _collectorTimer?.Stop();
    }

    private double SleepWatts =>
        double.TryParse(_services.Database?.GetSetting(SettingKeys.SleepWatts), NumberStyles.Float, CultureInfo.InvariantCulture, out var w) && w >= 0
            ? w : DefaultSleepWatts;

    private sealed record ProgramsRead(
        IReadOnlyList<ProgramEnergy> Totals,
        (DateTimeOffset UpdatedAt, IReadOnlyList<ProgramLive> Rows)? Live,
        (double? Cpu, double? Gpu) Baseline,
        IReadOnlyList<ProgramRun> Runs);

    /// <summary>The prices for what was read: each rolling period's effective price per kWh (its energy priced day by day, so a
    /// period that spans two bills is right), one average price for the Programs period, and the schedule for pricing runs.</summary>
    private sealed record RateInfo(RateSchedule Schedule, IReadOnlyList<(decimal? Rate, bool Estimated)> PeriodRates, decimal? ProgramRate);

    private sealed record CollectorRead(
        LiveState? Live, IReadOnlyList<PeriodReport>? Periods, ServiceState? Service, string? Error, ProgramsRead? Programs,
        PowerChartData? Chart, RateInfo? Rates, UsageStat? Stats);

    /// <summary>How long each choice in the Programs tab looks back.</summary>
    private static readonly TimeSpan[] ProgramPeriods =
        { TimeSpan.FromHours(1), TimeSpan.FromDays(1), TimeSpan.FromDays(7), TimeSpan.FromDays(30) };

    internal async Task RefreshCollectorAsync(bool includeService, bool includeChart = true)
    {
        try
        {
            var sleepWatts = SleepWatts;
            var lookBack = ProgramPeriods[Math.Clamp(ProgramPeriodBox.SelectedIndex, 0, ProgramPeriods.Length - 1)];
            var chartPeriod = PowerChart.Periods[Math.Clamp(ChartPeriodBox.SelectedIndex, 0, PowerChart.Periods.Count - 1)];
            var schedule = _services.Database?.LoadRateSchedule();
            var statsSpan = StatsPeriods[Math.Clamp(SelectedStatsIndex, 0, StatsPeriods.Length - 1)].Span;
            var read = await Task.Run(() => ReadCollector(includeService, sleepWatts, lookBack, includeChart ? chartPeriod : null, schedule,
                includeChart ? statsSpan : null, includeChart));
            ApplyCollector(read, includeChart);
        }
        catch (Exception e) when (e is not OperationCanceledException)
        {
            // This runs every second from a timer: a problem reading the history must show as a message, not close the app.
            CollectorResultText.Text = "Could not refresh the Dashboard: " + e.Message;
            CollectorResultText.IsVisible = true;
        }
    }

    private CollectorRead ReadCollector(bool includeService, double sleepWatts, TimeSpan programLookBack,
        (string Label, TimeSpan Span, TimeSpan Bucket)? chartPeriod, RateSchedule? schedule, TimeSpan? statsSpan, bool includeStats)
    {
        UsageStat? stats = null;
        RateInfo? rates = null;
        LiveState? live = null;
        IReadOnlyList<PeriodReport>? periods = null;
        ProgramsRead? programs = null;
        PowerChartData? chart = null;
        string? error = null;

        if (_services.OpenHistory() is { } store)
        {
            try
            {
                var now = DateTimeOffset.UtcNow;
                live = store.GetLive();
                periods = HistoryReport.Standard(store, now, sleepWatts);
                if (schedule is not null) rates = PriceThePeriods(store, schedule, periods, now, programLookBack);
                programs = new ProgramsRead(store.QueryPrograms(now - programLookBack, now), store.GetLivePrograms(), store.GetIdleBaseline(),
                    store.GetRecentRuns(20));
                if (chartPeriod is { } period) chart = PowerChart.Build(store, now - period.Span, now, period.Bucket);
                if (includeStats && schedule is not null) stats = UsageSummary.Compute(store, schedule, statsSpan, now);
            }
            catch (Exception e) when (e is Microsoft.Data.Sqlite.SqliteException or IOException or UnauthorizedAccessException)
            {
                error = "Could not read the saved history: " + e.Message;
            }
        }

        return new CollectorRead(live, periods, includeService ? _services.QueryCollectorService() : null, error, programs, chart, rates, stats);
    }

    /// <summary>Prices each rolling period's headline energy day by day, and gives the Programs period one average price.</summary>
    private static RateInfo PriceThePeriods(HistoryStore store, RateSchedule schedule, IReadOnlyList<PeriodReport> periods,
        DateTimeOffset now, TimeSpan programLookBack)
    {
        var spans = new[] { TimeSpan.FromHours(1), TimeSpan.FromDays(1), TimeSpan.FromDays(7), TimeSpan.FromDays(30) };
        var today = DateOnly.FromDateTime(DateTime.Now);
        var periodRates = new List<(decimal? Rate, bool Estimated)>();
        for (var i = 0; i < periods.Count; i++)
        {
            var wall = periods[i].WallKwh is not null;
            var cost = schedule.Cost((a, b) => wall ? store.Query(a, b, ReadingScope.Wall).KilowattHours : store.Query(a, b).KilowattHours,
                now - spans[Math.Min(i, spans.Length - 1)], now);
            // The effective price is the cost over the energy, so energy x price is exactly the priced total.
            decimal? rate = cost.Unpriced ? null : cost.Kwh > 0 ? cost.Cost / (decimal)cost.Kwh : schedule.RateAt(today);
            periodRates.Add((rate, cost.Estimated));
        }
        return new RateInfo(schedule, periodRates, schedule.AverageRate(now - programLookBack, now));
    }

    private void ApplyCollector(CollectorRead read, bool includeChart)
    {
        if (read.Service is { } state) _serviceState = state;
        _collectorAlive = read.Live is { } l && DateTimeOffset.UtcNow - l.UpdatedAt < Fresh;

        if (_collectorAlive)
        {
            var live = read.Live!;
            var total = live.Readings.Where(r => r.Scope == ReadingScope.Component).Sum(r => r.Watts);
            ShowPower(new PowerSnapshot(live.UpdatedAt, live.Readings, total, 0, live.Notes), showSession: false);
            PowerStatusText.Text = "Active power, measured every second by the background collector.";
        }

        ShowCollectorStatus(read.Live);
        ShowHistory(read);
        ShowPrograms(read.Programs, read.Rates);
        if (includeChart)
        {
            ShowChart(read);
            ShowStats(read.Stats);
        }
    }

    private void ShowChart(CollectorRead read)
    {
        PowerChartView.Data = read.Chart;
        ChartNoteText.Text = read.Chart is null
            ? "No history yet. The chart fills in once the background collector has recorded some."
            : read.Chart.Series.Count == 0
                ? "Nothing was recorded in this period."
                : read.Chart.Series.All(s => s.Kind != ChartSeriesKind.Wall)
                    ? "CPU and GPU are the sensors' active power. Set up a wall meter in Settings to add the whole-PC line."
                    : "";
        ChartNoteText.IsVisible = ChartNoteText.Text.Length > 0;
    }

    // ----- Programs ---------------------------------------------------------------------

    private void ShowPrograms(ProgramsRead? read, RateInfo? rates)
    {
        if (read is null)
        {
            LiveProgramsList.ItemsSource = Array.Empty<HistoryRow>();
            ProgramsList.ItemsSource = Array.Empty<HistoryRow>();
            ProgramsTotalText.Text = "";
            ProgramsBaselineText.Text = "";
            RunsList.ItemsSource = Array.Empty<HistoryRow>();
            RunsNoteText.Text = "";
            ProgramsNoteText.Text = "No program history yet. It is recorded by the background collector (turn it on from the Dashboard).";
            ProgramsNoteText.IsVisible = true;
            return;
        }

        var currency = _services.Database?.GetSetting(SettingKeys.Currency) ?? "$";
        var rate = rates?.ProgramRate;

        // Right now: only while the collector is publishing, so a stale list is never shown as current.
        var fresh = read.Live is { } l && DateTimeOffset.UtcNow - l.UpdatedAt < TimeSpan.FromSeconds(10);
        LiveProgramsList.ItemsSource = fresh ? LiveRows(read.Live!.Value.Rows) : Array.Empty<HistoryRow>();
        ProgramsNoteText.IsVisible = !fresh;
        ProgramsNoteText.Text = fresh ? "" : "Nothing is being recorded right now.";

        ProgramsList.ItemsSource = read.Totals.Count == 0 ? Array.Empty<HistoryRow>() : ProgramRows(read.Totals, rate, currency);
        var all = read.Totals.Sum(p => p.TotalKwh);
        ProgramsTotalText.Text = read.Totals.Count == 0
            ? "Nothing recorded in this period yet."
            : $"CPU and GPU together: {FormatEnergy(all)}"
              + (rate is { } rt ? $", {currency}{FormatMoney(CostCalculator.Cost(all, rt))}" : "");

        // A run is priced at the price on the day it ended.
        RunsList.ItemsSource = RunRows(read.Runs, run => rates?.Schedule.RateAt(DateOnly.FromDateTime(run.End.ToLocalTime().DateTime)), currency);
        RunsNoteText.Text = read.Runs.Count == 0 ? "No runs recorded yet." : "";
        RunsNoteText.IsVisible = read.Runs.Count == 0;

        ProgramsBaselineText.Text = read.Baseline is ({ } cpu, { } gpu)
            ? $"Idle power learned so far: CPU {cpu.ToString("0.#", CultureInfo.CurrentCulture)} W, GPU {gpu.ToString("0.#", CultureInfo.CurrentCulture)} W (the lowest 30 second average seen). Only power above this is shared out."
            : "Still learning the idle power. Until a quiet moment has been seen, everything counts as baseline.";
    }

    /// <summary>One line per run: what it cost, when it started and how long it ran.</summary>
    internal static IReadOnlyList<HistoryRow> RunRows(IReadOnlyList<ProgramRun> runs, decimal? rate, string currency) =>
        RunRows(runs, _ => rate, currency);

    /// <summary>One line per run, each priced at the price on the day it ended.</summary>
    internal static IReadOnlyList<HistoryRow> RunRows(IReadOnlyList<ProgramRun> runs, Func<ProgramRun, decimal?> rateOf, string currency) =>
        runs.Select(r => new HistoryRow(
            $"{r.Program}: {FormatEnergy(r.TotalKwh)}"
            + (rateOf(r) is { } rt ? $", {currency}{FormatMoney(CostCalculator.Cost(r.TotalKwh, rt))}" : "")
            + (r.StillRunning ? " (still running)" : ""),
            $"started {r.Start.ToLocalTime().ToString("g", CultureInfo.CurrentCulture)} · "
            + (r.StillRunning ? "running for " : "ran ") + FormatSpan(r.End - r.Start)
            + $" · GPU {FormatEnergy(r.GpuKwh)} · CPU {FormatEnergy(r.CpuKwh)}")).ToList();

    internal static IReadOnlyList<HistoryRow> LiveRows(IReadOnlyList<ProgramLive> live) =>
        live.Select(p => new HistoryRow(
            $"{p.Program}: {(p.CpuWatts + p.GpuWatts).ToString("0.0", CultureInfo.CurrentCulture)} W",
            p.Program is ProgramAttributor.Baseline
                ? "idle power of the CPU and GPU, not any program's doing"
                : $"GPU {p.GpuWatts.ToString("0.0", CultureInfo.CurrentCulture)} W · CPU {p.CpuWatts.ToString("0.0", CultureInfo.CurrentCulture)} W")).ToList();

    /// <summary>A program that caused less than this (0.05 Wh) over the period is not listed on its own.</summary>
    internal const double MinListedKwh = 0.00005;
    internal const int MaxListedPrograms = 25;

    /// <summary>Programs biggest first, then the two rows that belong to no program, so the list reads as "who used it".</summary>
    internal static IReadOnlyList<HistoryRow> ProgramRows(IReadOnlyList<ProgramEnergy> totals, decimal? rate, string currency)
    {
        static bool Special(ProgramEnergy p) => p.Program is ProgramAttributor.Baseline or ProgramAttributor.Unattributed;
        string Title(ProgramEnergy p) =>
            $"{p.Program}: {FormatEnergy(p.TotalKwh)}"
            + (rate is { } r ? $", {currency}{FormatMoney(CostCalculator.Cost(p.TotalKwh, r))}" : "");
        string Detail(ProgramEnergy p) => p.Program switch
        {
            ProgramAttributor.Baseline => "the CPU's and GPU's idle power, not any program's doing",
            ProgramAttributor.Unattributed => "power above idle that no program reported using",
            ProgramAttributor.Others => $"many small programs together · active up to {FormatSpan(TimeSpan.FromSeconds(p.ActiveSeconds))}",
            _ => $"GPU {FormatEnergy(p.GpuKwh)} · CPU {FormatEnergy(p.CpuKwh)}"
                 + (p.ActiveSeconds > 0 ? $" · active {FormatSpan(TimeSpan.FromSeconds(p.ActiveSeconds))}" : ""),
        };

        // Dozens of programs each caused a fraction of a watt-hour: list the ones that matter and sum the rest.
        var programs = totals.Where(p => !Special(p)).OrderByDescending(p => p.TotalKwh).ToList();
        var shown = programs.Where(p => p.TotalKwh >= MinListedKwh).Take(MaxListedPrograms).ToList();
        var rest = programs.Skip(shown.Count).ToList();

        var rows = shown.Select(p => new HistoryRow(Title(p), Detail(p))).ToList();
        if (rest.Count > 0)
        {
            var restKwh = rest.Sum(p => p.TotalKwh);
            rows.Add(new HistoryRow(
                $"{rest.Count} smaller program{(rest.Count == 1 ? "" : "s")}: {FormatEnergy(restKwh)}"
                + (rate is { } rr ? $", {currency}{FormatMoney(CostCalculator.Cost(restKwh, rr))}" : ""),
                "none of them caused enough to list on its own"));
        }
        rows.AddRange(totals.Where(Special).Select(p => new HistoryRow(Title(p), Detail(p))));
        return rows;
    }

    private void ShowCollectorStatus(LiveState? live)
    {
        var state = _serviceState;
        var (text, canInstall, canRemove) = (state, _collectorAlive) switch
        {
            (ServiceState.Unsupported, _) => ("Background recording is Windows only for now. KVGuzzler records while this window is open.", false, false),
            (_, true) => ("On. The collector service is recording, even when nobody is logged in.", false, state is ServiceState.Running or ServiceState.Stopped or ServiceState.Other),
            (ServiceState.Running, false) => ("The collector service is installed and starting. Waiting for its first reading.", false, true),
            (ServiceState.Stopped, false) => ("The collector service is installed but stopped. Nothing is being recorded.", true, true),
            (ServiceState.NotInstalled, false) => ("Off. KVGuzzler only records while this window is open, so time when it is closed is not counted.", true, false),
            _ => ("Checking the collector service...", false, false),
        };

        CollectorStatusText.Text = text;
        InstallCollectorButton.IsVisible = canInstall;
        InstallCollectorButton.Content = state == ServiceState.Stopped ? "Start background recording" : "Turn on background recording";
        RemoveCollectorButton.IsVisible = canRemove;
        CollectorButtons.IsVisible = canInstall || canRemove;
        CollectorLastSeenText.IsVisible = !_collectorAlive && live is not null;
        if (live is not null && !_collectorAlive)
            CollectorLastSeenText.Text = "The collector last recorded at " + live.UpdatedAt.ToLocalTime().ToString("g", CultureInfo.CurrentCulture) + ".";
    }

    private async Task ChangeCollectorAsync(bool install)
    {
        InstallCollectorButton.IsEnabled = RemoveCollectorButton.IsEnabled = false;
        CollectorResultText.Text = "Waiting for Windows to ask whether KVGuzzler may make this change...";
        CollectorResultText.IsVisible = true;
        try
        {
            var (_, message) = await ServiceInstaller.RequestElevatedAsync(install, development: !_services.Updates.IsInstalled);
            CollectorResultText.Text = message;
        }
        catch (Exception e)
        {
            CollectorResultText.Text = "Could not start the change: " + e.Message;
        }
        finally
        {
            CollectorResultText.IsVisible = true;
            InstallCollectorButton.IsEnabled = RemoveCollectorButton.IsEnabled = true;
        }
        await RefreshCollectorAsync(includeService: true);
    }

    private void SaveSleepWatts()
    {
        if (_services.Database is null) return;
        if (!double.TryParse(SleepWattsBox.Text, NumberStyles.Float, CultureInfo.CurrentCulture, out var watts)
            && !double.TryParse(SleepWattsBox.Text, NumberStyles.Float, CultureInfo.InvariantCulture, out watts)
            || watts < 0 || watts > 100)
        {
            SleepStatusText.Text = "Enter a number of watts between 0 and 100, for example 2.";
            return;
        }

        _services.Database.SetSetting(SettingKeys.SleepWatts, watts.ToString(CultureInfo.InvariantCulture));
        SleepStatusText.Text = "Saved.";
        _ = RefreshCollectorAsync(includeService: false);
    }

    // ----- History ----------------------------------------------------------------------

    private void ShowHistory(CollectorRead read)
    {
        HistoryNoteText.IsVisible = true;
        if (read.Error is not null)
        {
            HistoryList.ItemsSource = Array.Empty<HistoryRow>();
            HistoryNoteText.Text = read.Error;
            return;
        }
        if (read.Periods is null)
        {
            HistoryList.ItemsSource = Array.Empty<HistoryRow>();
            HistoryNoteText.Text = "No history yet. It is saved by the background collector.";
            return;
        }

        var currency = _services.Database?.GetSetting(SettingKeys.Currency) ?? "$";
        var priced = read.Rates?.PeriodRates;

        HistoryList.ItemsSource = read.Periods.Select((p, i) => new HistoryRow(
            HistoryTitle(p, priced?[i].Rate, currency, priced?[i].Estimated ?? false), HistoryDetail(p))).ToList();
        var anyPrice = priced is not null && priced.Any(x => x.Rate is not null);
        HistoryNoteText.Text = anyPrice ? "" : "Save your electricity rate (or a bill) in Settings to see costs.";
        HistoryNoteText.IsVisible = !anyPrice;
    }

    /// <summary>With a wall meter the headline is the whole PC (what the bill is made of). Without one it is the
    /// active energy the sensors measured. Sleep is an estimate and never goes in either figure.</summary>
    internal static string HistoryTitle(PeriodReport p, decimal? rate, string currency, bool estimated = false)
    {
        var (kwh, what) = p.WallKwh is { } wall ? (wall, "whole PC") : (p.MeasuredKwh, "active");
        var title = $"{p.Label}: {kwh.ToString("0.###", CultureInfo.CurrentCulture)} kWh {what}";
        if (rate is { } r) title += $", {currency}{FormatMoney(CostCalculator.Cost(kwh, r))}" + (estimated ? " (price estimated)" : "");
        return title;
    }

    /// <summary>The second line: how long the figure covers, the part the sensors account for when there is a
    /// wall meter, and every stretch that was not sampled, by reason, so nothing is silently missing.</summary>
    internal static string HistoryDetail(PeriodReport p)
    {
        var parts = new List<string>();
        if (p.WallKwh is not null)
            parts.Add($"of which CPU, GPU and memory {p.MeasuredKwh.ToString("0.###", CultureInfo.CurrentCulture)} kWh");
        else
            parts.Add($"active energy over {FormatSpan(p.Monitored)} recorded");
        if (p.Asleep > TimeSpan.Zero)
            parts.Add($"asleep {FormatSpan(p.Asleep)}" + (p.WallKwh is null
                ? $" (about {p.EstimatedKwh.ToString("0.###", CultureInfo.CurrentCulture)} kWh at the sleep wattage, not counted above)"
                : ""));
        if (p.Off > TimeSpan.Zero) parts.Add($"off {FormatSpan(p.Off)}");
        if (p.LostPower > TimeSpan.Zero) parts.Add($"lost power or crashed {FormatSpan(p.LostPower)}");
        return string.Join(" · ", parts);
    }

    /// <summary>Energy in kWh, or in Wh when it is small, so a program that used a little still reads as something
    /// ("1.4 Wh") instead of "0 kWh".</summary>
    internal static string FormatEnergy(double kwh) =>
        Math.Abs(kwh) < 0.01
            ? $"{(kwh * 1000).ToString("0.#", CultureInfo.CurrentCulture)} Wh"
            : $"{kwh.ToString("0.###", CultureInfo.CurrentCulture)} kWh";

    internal static string FormatSpan(TimeSpan t) =>
        t.TotalDays >= 1 ? $"{(int)t.TotalDays} d {t.Hours} h"
        : t.TotalHours >= 1 ? $"{(int)t.TotalHours} h {t.Minutes} min"
        : t.TotalMinutes >= 1 ? $"{(int)t.TotalMinutes} min"
        : "under a minute";
}
