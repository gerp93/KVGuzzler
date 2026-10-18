using System.Globalization;
using Avalonia;
using Avalonia.Threading;
using KVGuzzler.Collector;
using KVGuzzler.Core;
using KVGuzzler.Core.History;
using KVGuzzler.Core.Power;
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
                await RefreshCollectorAsync(includeService: _ticks % 10 == 0);
            };
            _collectorTimer.Start();
        };
        Closed += (_, _) => _collectorTimer?.Stop();
    }

    private double SleepWatts =>
        double.TryParse(_services.Database?.GetSetting(SettingKeys.SleepWatts), NumberStyles.Float, CultureInfo.InvariantCulture, out var w) && w >= 0
            ? w : DefaultSleepWatts;

    private sealed record CollectorRead(LiveState? Live, IReadOnlyList<PeriodReport>? Periods, ServiceState? Service, string? Error);

    internal async Task RefreshCollectorAsync(bool includeService)
    {
        var sleepWatts = SleepWatts;
        var read = await Task.Run(() => ReadCollector(includeService, sleepWatts));
        ApplyCollector(read);
    }

    private CollectorRead ReadCollector(bool includeService, double sleepWatts)
    {
        LiveState? live = null;
        IReadOnlyList<PeriodReport>? periods = null;
        string? error = null;

        if (_services.OpenHistory() is { } store)
        {
            try
            {
                live = store.GetLive();
                periods = HistoryReport.Standard(store, DateTimeOffset.UtcNow, sleepWatts);
            }
            catch (Exception e) when (e is Microsoft.Data.Sqlite.SqliteException or IOException or UnauthorizedAccessException)
            {
                error = "Could not read the saved history: " + e.Message;
            }
        }

        return new CollectorRead(live, periods, includeService ? _services.QueryCollectorService() : null, error);
    }

    private void ApplyCollector(CollectorRead read)
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

        var rate = decimal.TryParse(_services.Database?.GetSetting(SettingKeys.RatePerKwh), NumberStyles.AllowDecimalPoint,
            CultureInfo.InvariantCulture, out var r) ? r : (decimal?)null;
        var currency = _services.Database?.GetSetting(SettingKeys.Currency) ?? "$";

        HistoryList.ItemsSource = read.Periods.Select(p => new HistoryRow(
            HistoryTitle(p, rate, currency), HistoryDetail(p))).ToList();
        HistoryNoteText.Text = rate is null ? "Save your electricity rate in Settings to see costs." : "";
        HistoryNoteText.IsVisible = rate is null;
    }

    /// <summary>The headline is the active (sensor-measured) energy only. Sleep is an estimate and sits on the
    /// detail line, never in this figure: the whole-PC total is the wall meter's job.</summary>
    internal static string HistoryTitle(PeriodReport p, decimal? rate, string currency)
    {
        var title = $"{p.Label}: {p.MeasuredKwh.ToString("0.###", CultureInfo.CurrentCulture)} kWh active";
        if (rate is { } r) title += $", {currency}{FormatMoney(CostCalculator.Cost(p.MeasuredKwh, r))}";
        return title;
    }

    /// <summary>The second line: how long the active figure covers, and every stretch that was not sampled,
    /// by reason, so nothing is silently missing.</summary>
    internal static string HistoryDetail(PeriodReport p)
    {
        var parts = new List<string> { $"active energy over {FormatSpan(p.Monitored)} recorded" };
        if (p.Asleep > TimeSpan.Zero)
            parts.Add($"asleep {FormatSpan(p.Asleep)} (about {p.EstimatedKwh.ToString("0.###", CultureInfo.CurrentCulture)} kWh at the sleep wattage, not counted above)");
        if (p.Off > TimeSpan.Zero) parts.Add($"off {FormatSpan(p.Off)}");
        if (p.LostPower > TimeSpan.Zero) parts.Add($"lost power or crashed {FormatSpan(p.LostPower)}");
        return string.Join(" · ", parts);
    }

    internal static string FormatSpan(TimeSpan t) =>
        t.TotalDays >= 1 ? $"{(int)t.TotalDays} d {t.Hours} h"
        : t.TotalHours >= 1 ? $"{(int)t.TotalHours} h {t.Minutes} min"
        : t.TotalMinutes >= 1 ? $"{(int)t.TotalMinutes} min"
        : "under a minute";
}
