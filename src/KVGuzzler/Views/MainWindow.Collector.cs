using System.Globalization;
using Avalonia;
using Avalonia.Controls;
using Avalonia.Threading;
using KVGuzzler.Collector;
using KVGuzzler.Core;
using KVGuzzler.Core.Billing;
using KVGuzzler.Core.History;
using KVGuzzler.Core.Power;
using KVGuzzler.Core.Programs;
using KVGuzzler.Data;
using VisualAssault;

namespace KVGuzzler.Views;

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
        OpenRecordingSettingsButton.Click += (_, _) => Tabs.SelectedItem = Tabs.Items.OfType<TabItem>().First(t => (string?)t.Header == "Settings");
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

    /// <param name="Totals">What each program caused over the chosen period; null when not read this time (the long periods are
    /// only read now and then), in which case the lists already on screen stay.</param>
    /// <param name="WholeKwh">The whole PC at the wall over the same period, when the wall meter covered all of it; null otherwise.</param>
    private sealed record ProgramsRead(
        IReadOnlyList<ProgramEnergy>? Totals, double? WholeKwh,
        (DateTimeOffset UpdatedAt, IReadOnlyList<ProgramLive> Rows)? Live,
        (double? Cpu, double? Gpu) Baseline,
        IReadOnlyList<ProgramRun> Runs);

    /// <summary>The prices for what was read: each rolling period's effective price per kWh (its energy priced day by day, so a
    /// period that spans two bills is right), one average price for the Programs period, and the schedule for pricing runs.</summary>
    private sealed record RateInfo(RateSchedule Schedule, IReadOnlyList<(decimal? Rate, bool Estimated)> PeriodRates, decimal? ProgramRate);

    private sealed record CollectorRead(
        LiveState? Live, IReadOnlyList<PeriodReport>? Periods, ServiceState? Service, string? Error, ProgramsRead? Programs,
        PowerChartData? Chart, RateInfo? Rates, UsageStat? Stats);

    /// <summary>The chart shows at most the last 30 days, so the two longest choices show that and say so.</summary>
    private static (string Label, TimeSpan Span, TimeSpan Bucket) ChartPeriodFor(int periodIndex) =>
        PowerChart.Periods[Math.Clamp(periodIndex, 0, PowerChart.Periods.Count - 1)];

    /// <summary>Adding up the programs over a year reads millions of rows, so the long periods are read once a minute at most.</summary>
    private static readonly TimeSpan LongProgramRefresh = TimeSpan.FromMinutes(1);
    private DateTimeOffset _programsReadAt;
    private int _programsReadPeriod = -1;

    internal async Task RefreshCollectorAsync(bool includeService, bool includeChart = true)
    {
        try
        {
            var sleepWatts = SleepWatts;
            var periodIndex = Math.Clamp(SelectedStatsIndex, 0, StatsPeriods.Length - 1);
            var statsSpan = StatsPeriods[periodIndex].Span;
            var chartPeriod = ChartPeriodFor(periodIndex);
            var schedule = _services.Database?.LoadRateSchedule();
            var longPeriod = statsSpan is null || statsSpan > TimeSpan.FromDays(7);
            var readTotals = includeChart
                && (periodIndex != _programsReadPeriod || !longPeriod || DateTimeOffset.UtcNow - _programsReadAt > LongProgramRefresh);
            var read = await Task.Run(() => ReadCollector(includeService, sleepWatts, statsSpan, includeChart ? chartPeriod : null, schedule,
                includeChart ? statsSpan : null, includeChart, readTotals));
            if (read.Programs?.Totals is not null)
            {
                _programsReadAt = DateTimeOffset.UtcNow;
                _programsReadPeriod = periodIndex;
            }
            ApplyCollector(read, includeChart);
        }
        catch (Exception e) when (e is not OperationCanceledException)
        {
            // This runs every second from a timer: a problem reading the history must show as a message, not close the app.
            CollectorResultText.Text = "Could not refresh the Dashboard: " + e.Message;
            CollectorResultText.IsVisible = true;
        }
    }

    private CollectorRead ReadCollector(bool includeService, double sleepWatts, TimeSpan? programLookBack,
        (string Label, TimeSpan Span, TimeSpan Bucket)? chartPeriod, RateSchedule? schedule, TimeSpan? statsSpan, bool includeStats,
        bool readProgramTotals)
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
                // "All time" for the programs starts at the first thing recorded.
                var programFrom = programLookBack is { } back ? now - back : store.FirstRecorded() ?? now;
                if (schedule is not null) rates = PriceThePeriods(store, schedule, periods, now, programFrom);
                IReadOnlyList<ProgramEnergy>? totals = null;
                double? whole = null;
                if (readProgramTotals)
                {
                    totals = store.QueryPrograms(programLookBack is null ? DateTimeOffset.UnixEpoch : programFrom, now);
                    // The whole PC is the yardstick for the programs' shares, but only when the wall meter covers the whole period.
                    if (store.FirstRecorded(ReadingScope.Wall) is { } firstWall && firstWall <= programFrom.AddMinutes(10)
                        && store.EnergyKwh(programFrom, now, ReadingScope.Wall) is > 0 and var wallKwh)
                        whole = wallKwh;
                }
                programs = new ProgramsRead(totals, whole, store.GetLivePrograms(), store.GetIdleBaseline(), store.GetRecentRuns(20));
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
        DateTimeOffset now, DateTimeOffset programFrom)
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
        return new RateInfo(schedule, periodRates, schedule.AverageRate(programFrom, now));
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
            PowerStatusText.Text = "Measured every second by the background collector.";
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
        var period = StatsPeriods[Math.Clamp(SelectedStatsIndex, 0, StatsPeriods.Length - 1)].Label;
        // The chart is built from minute data and goes back 30 days at most; the two longest choices show that and say so.
        ChartPeriodText.Text = SelectedStatsIndex >= PowerChart.Periods.Count ? "Last 30 days (the chart does not go back further)" : period;
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

    /// <summary>How many programs the Dashboard lists before rolling the rest into one line; the Programs tab lists more.</summary>
    internal const int DashboardPrograms = 5;

    private void ShowPrograms(ProgramsRead? read, RateInfo? rates)
    {
        if (read is null)
        {
            var none = Array.Empty<HistoryRow>();
            LiveProgramsList.ItemsSource = DashboardLiveProgramsList.ItemsSource = none;
            ProgramsList.ItemsSource = DashboardProgramsList.ItemsSource = none;
            ProgramsTotalText.Text = "Nothing recorded yet.";
            DashboardProgramsSummaryText.Text = "";
            ProgramsBaselineText.Text = "";
            SplitBar.Fill(ProgramsSplitBar, ProgramsSplitLegend, Array.Empty<SplitBar.Part>());
            SplitBar.Fill(DashboardProgramsSplitBar, DashboardProgramsSplitLegend, Array.Empty<SplitBar.Part>());
            RunsList.ItemsSource = none;
            RunsNoteText.Text = "";
            ProgramsNoteText.Text = DashboardProgramsNoteText.Text = "No program history yet. It is recorded by the background collector (turn it on from the Dashboard).";
            ProgramsNoteText.IsVisible = DashboardProgramsNoteText.IsVisible = true;
            return;
        }

        var currency = _services.Database?.GetSetting(SettingKeys.Currency) ?? "$";
        var rate = rates?.ProgramRate;
        var period = StatsPeriods[Math.Clamp(SelectedStatsIndex, 0, StatsPeriods.Length - 1)].Label;

        ProgramsPeriodText.Text = period;

        // Right now: only while the collector is publishing, so a stale list is never shown as current.
        var fresh = read.Live is { } l && DateTimeOffset.UtcNow - l.UpdatedAt < TimeSpan.FromSeconds(10);
        var live = fresh ? LiveRows(read.Live!.Value.Rows) : Array.Empty<HistoryRow>();
        LiveProgramsList.ItemsSource = live;
        DashboardLiveProgramsList.ItemsSource = live.Take(DashboardPrograms).ToList();
        DashboardLiveProgramsHeader.IsVisible = live.Count > 0;
        ProgramsNoteText.IsVisible = !fresh;
        ProgramsNoteText.Text = fresh ? "" : "Nothing is being recorded right now.";

        // The totals are not read every second (the long periods are costly), so they are only redrawn when they were read.
        if (read.Totals is { } totals)
        {
            ProgramsList.ItemsSource = totals.Count == 0 ? Array.Empty<HistoryRow>() : ProgramRows(totals, rate, currency, MaxListedPrograms, read.WholeKwh);
            DashboardProgramsList.ItemsSource = totals.Count == 0 ? Array.Empty<HistoryRow>() : ProgramRows(totals, rate, currency, DashboardPrograms, read.WholeKwh);

            var all = totals.Sum(p => p.TotalKwh);
            ProgramsTotalText.Text = totals.Count == 0
                ? "Nothing recorded in this period yet."
                : $"CPU and GPU together: {FormatEnergy(all)}"
                  + (rate is { } rt ? $", {currency}{FormatMoney(CostCalculator.Cost(all, rt))}" : "");
            DashboardProgramsSummaryText.Text = period + " · " + (totals.Count == 0 ? "nothing recorded yet" : ProgramsTotalText.Text);

            var split = SplitParts(totals, read.WholeKwh);
            SplitBar.Fill(ProgramsSplitBar, ProgramsSplitLegend, split);
            SplitBar.Fill(DashboardProgramsSplitBar, DashboardProgramsSplitLegend, split);
            var yardstick = read.WholeKwh is null ? "Percentages are of the energy the CPU and GPU used." : "Percentages are of the whole PC at the wall.";
            ProgramsShareNoteText.Text = DashboardProgramsShareNoteText.Text = totals.Count == 0 ? "" : yardstick;
            DashboardProgramsNoteText.IsVisible = totals.Count == 0;
            DashboardProgramsNoteText.Text = totals.Count == 0 ? "Nothing recorded in this period yet." : "";
        }

        // A run is priced at the price on the day it ended.
        RunsList.ItemsSource = RunRows(read.Runs, run => rates?.Schedule.RateAt(DateOnly.FromDateTime(run.End.ToLocalTime().DateTime)), currency);
        RunsNoteText.Text = read.Runs.Count == 0 ? "No runs recorded yet." : "";
        RunsNoteText.IsVisible = read.Runs.Count == 0;

        ProgramsBaselineText.Text = read.Baseline is ({ } cpu, { } gpu)
            ? $"Idle power learned so far: CPU {cpu.ToString("0.#", CultureInfo.CurrentCulture)} W, GPU {gpu.ToString("0.#", CultureInfo.CurrentCulture)} W (the lowest 30 second average seen). Only power above this is shared out."
            : "Still learning the idle power. Until a quiet moment has been seen, everything counts as baseline.";
    }

    private static bool IsSpecial(ProgramEnergy p) => p.Program is ProgramAttributor.Baseline or ProgramAttributor.Unattributed;

    /// <summary>The CPU and GPU energy split into the programs, the idle baseline and what no program claimed. With the whole
    /// PC known, the part of it that is neither CPU nor GPU (fans, drives, memory, power-supply losses) is the last piece.</summary>
    internal static IReadOnlyList<SplitBar.Part> SplitParts(IReadOnlyList<ProgramEnergy> totals, double? wholeKwh)
    {
        var programs = totals.Where(p => !IsSpecial(p)).Sum(p => p.TotalKwh);
        var baseline = totals.Where(p => p.Program == ProgramAttributor.Baseline).Sum(p => p.TotalKwh);
        var unattributed = totals.Where(p => p.Program == ProgramAttributor.Unattributed).Sum(p => p.TotalKwh);
        var parts = new List<SplitBar.Part>
        {
            new("Programs", programs, UiColors.Programs),
            new("Idle baseline", baseline, UiColors.Brush(VaToken.AccentMuted)),
            new("Not attributed", unattributed, UiColors.NoProgram),
        };
        // The sensors and the plug are read a moment apart, so the rest can come out a hair negative: show it only when it is real.
        if (wholeKwh is { } whole && whole - (programs + baseline + unattributed) is > 0 and var rest)
            parts.Add(new SplitBar.Part("Everything else", rest, UiColors.Rest));
        return parts;
    }

    /// <summary>One line per run: what it cost, when it started and how long it ran.</summary>
    internal static IReadOnlyList<HistoryRow> RunRows(IReadOnlyList<ProgramRun> runs, decimal? rate, string currency) =>
        RunRows(runs, _ => rate, currency);

    /// <summary>One line per run, each priced at the price on the day it ended.</summary>
    internal static IReadOnlyList<HistoryRow> RunRows(IReadOnlyList<ProgramRun> runs, Func<ProgramRun, decimal?> rateOf, string currency) =>
        runs.Select(r => new HistoryRow(
            r.Program,
            FormatEnergy(r.TotalKwh),
            rateOf(r) is { } rt ? $"{currency}{FormatMoney(CostCalculator.Cost(r.TotalKwh, rt))}" : "",
            $"started {r.Start.ToLocalTime().ToString("g", CultureInfo.CurrentCulture)} · "
            + (r.StillRunning ? "running for " : "ran ") + FormatSpan(r.End - r.Start)
            + $" · GPU {FormatEnergy(r.GpuKwh)} · CPU {FormatEnergy(r.CpuKwh)}",
            Note: r.StillRunning ? "still running" : "")).ToList();

    /// <summary>What each program is drawing right now, with its part of the CPU and GPU power at this moment.</summary>
    internal static IReadOnlyList<HistoryRow> LiveRows(IReadOnlyList<ProgramLive> live)
    {
        var total = live.Sum(p => p.CpuWatts + p.GpuWatts);
        return live.Select(p => new HistoryRow(
            p.Program,
            (p.CpuWatts + p.GpuWatts).ToString("0.0", CultureInfo.CurrentCulture) + " W",
            "",
            p.Program is ProgramAttributor.Baseline
                ? "idle power of the CPU and GPU, not any program's doing"
                : $"GPU {p.GpuWatts.ToString("0.0", CultureInfo.CurrentCulture)} W · CPU {p.CpuWatts.ToString("0.0", CultureInfo.CurrentCulture)} W",
            Share: total > 0 ? (p.CpuWatts + p.GpuWatts) / total : 0,
            Bar: p.Program is ProgramAttributor.Baseline ? UiColors.Brush(VaToken.AccentMuted) : UiColors.Programs)).ToList();
    }

    /// <summary>A program that caused less than this (0.05 Wh) over the period is not listed on its own.</summary>
    internal const double MinListedKwh = 0.00005;
    internal const int MaxListedPrograms = 25;

    /// <summary>Programs biggest first, then the two rows that belong to no program, so the list reads as "who used it".
    /// Each row's share is of the whole PC when <paramref name="wholeKwh"/> is known, otherwise of the CPU and GPU energy.</summary>
    internal static IReadOnlyList<HistoryRow> ProgramRows(IReadOnlyList<ProgramEnergy> totals, decimal? rate, string currency,
        int maxPrograms = MaxListedPrograms, double? wholeKwh = null)
    {
        var yardstick = wholeKwh is > 0 ? wholeKwh.Value : totals.Sum(p => p.TotalKwh);
        double ShareOf(double kwh) => yardstick > 0 ? Math.Clamp(kwh / yardstick, 0, 1) : 0;
        string Cost(double kwh) => rate is { } r ? $"{currency}{FormatMoney(CostCalculator.Cost(kwh, r))}" : "";
        HistoryRow Row(ProgramEnergy p) => new(
            p.Program, FormatEnergy(p.TotalKwh), Cost(p.TotalKwh),
            p.Program switch
            {
                ProgramAttributor.Baseline => "the CPU's and GPU's idle power, not any program's doing",
                ProgramAttributor.Unattributed => "power above idle that no program reported using",
                ProgramAttributor.Others => $"many small programs together · active up to {FormatSpan(TimeSpan.FromSeconds(p.ActiveSeconds))}",
                _ => $"GPU {FormatEnergy(p.GpuKwh)} · CPU {FormatEnergy(p.CpuKwh)}"
                     + (p.ActiveSeconds > 0 ? $" · active {FormatSpan(TimeSpan.FromSeconds(p.ActiveSeconds))}" : ""),
            },
            Share: ShareOf(p.TotalKwh),
            Bar: p.Program == ProgramAttributor.Baseline ? UiColors.Brush(VaToken.AccentMuted) : IsSpecial(p) ? UiColors.NoProgram : UiColors.Programs);

        // Dozens of programs each caused a fraction of a watt-hour: list the ones that matter and sum the rest.
        var programs = totals.Where(p => !IsSpecial(p)).OrderByDescending(p => p.TotalKwh).ToList();
        var shown = programs.Where(p => p.TotalKwh >= MinListedKwh).Take(maxPrograms).ToList();
        var rest = programs.Skip(shown.Count).ToList();

        var rows = shown.Select(Row).ToList();
        if (rest.Count > 0)
        {
            var restKwh = rest.Sum(p => p.TotalKwh);
            rows.Add(new HistoryRow(
                $"{rest.Count} smaller program{(rest.Count == 1 ? "" : "s")}", FormatEnergy(restKwh), Cost(restKwh),
                "none of them caused enough to list on its own", Share: ShareOf(restKwh), Bar: UiColors.Programs));
        }
        rows.AddRange(totals.Where(IsSpecial).Select(Row));
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
        // The controls are in Settings; the Dashboard only points there while nothing is recording in the background.
        RecordingHint.IsVisible = !_collectorAlive && state is not (ServiceState.Unsupported or null);
        RecordingHintText.Text = state == ServiceState.Stopped
            ? "Background recording is stopped, so nothing is being counted. Start it in Settings."
            : "Background recording is off, so time when this window is closed is not counted. Turn it on in Settings.";
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
            var (_, message) = await ServiceInstaller.RequestElevatedAsync(install ? ServiceChange.Install : ServiceChange.Uninstall, development: !_services.Updates.IsInstalled);
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

        HistoryList.ItemsSource = read.Periods.Select((p, i) => HistoryRowFor(
            p, priced?[i].Rate, currency, priced?[i].Estimated ?? false)).ToList();
        var anyPrice = priced is not null && priced.Any(x => x.Rate is not null);
        HistoryNoteText.Text = anyPrice ? "" : "Save your electricity rate (or a bill) in Settings to see costs.";
        HistoryNoteText.IsVisible = !anyPrice;
    }

    /// <summary>With a wall meter the headline is the whole PC (what the bill is made of). Without one it is the
    /// active energy the sensors measured. Sleep is an estimate and never goes in either figure.</summary>
    internal static HistoryRow HistoryRowFor(PeriodReport p, decimal? rate, string currency, bool estimated = false)
    {
        var (kwh, what) = p.WallKwh is { } wall ? (wall, "whole PC") : (p.MeasuredKwh, "active");
        return new HistoryRow(p.Label, $"{kwh.ToString("0.###", CultureInfo.CurrentCulture)} kWh",
            rate is { } r ? $"{currency}{FormatMoney(CostCalculator.Cost(kwh, r))}" : "",
            HistoryDetail(p), Tag: what, Note: rate is not null && estimated ? "price estimated" : "");
    }

    internal static string HistoryTitle(PeriodReport p, decimal? rate, string currency, bool estimated = false) =>
        HistoryRowFor(p, rate, currency, estimated).Title;

    /// <summary>The second line: how long the figure covers, the part the sensors account for when there is a
    /// wall meter, and every stretch that was not sampled, by reason, so nothing is silently missing.</summary>
    internal static string HistoryDetail(PeriodReport p)
    {
        var parts = new List<string>();
        if (p.WallKwh is not null)
            parts.Add($"of which CPU, GPU and memory {p.MeasuredKwh.ToString("0.###", CultureInfo.CurrentCulture)} kWh"
                      + (p.WallKwh > 0 ? $" ({UiColors.Percent(p.MeasuredKwh / p.WallKwh.Value)} of the whole PC)" : ""));
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
