using System.Globalization;
using System.Text;
using Avalonia;
using Avalonia.Controls;
using Avalonia.Layout;
using Avalonia.Platform.Storage;
using KVGuzzler.Core;
using KVGuzzler.Core.Billing;
using KVGuzzler.Core.History;
using KVGuzzler.Core.Power;
using KVGuzzler.Data;
using VisualAssault;

namespace KVGuzzler.Views;

// Extra meters: smart plugs for a monitor, speakers or anything else next to the PC. Set up in Settings (saved in the machine-wide data
// folder, where the collector service reads it too), listed in the Dashboard's Peripherals card, and optionally counted on top of the PC
// in the Dashboard's figures with the "Also count" checkboxes.
public partial class MainWindow
{
    private string PeripheralsConfigPath => Path.Combine(_services.HistoryDirectory, PeripheralMetersConfig.FileName);

    private PeripheralMetersConfig _peripherals = PeripheralMetersConfig.Empty;
    private IReadOnlyList<PowerReading> _peripheralLive = Array.Empty<PowerReading>();
    private IReadOnlyList<PeripheralUse>? _peripheralPeriod;
    private decimal? _peripheralRate;
    private IReadOnlyList<string> _peripheralNotes = Array.Empty<string>();

    /// <summary>Why a meter is silent, from the collector's notes ("Monitor (192.168.1.51): Could not reach ..."), or null.</summary>
    internal static string? ProblemText(IEnumerable<string> notes, string label)
    {
        var note = notes.FirstOrDefault(n => n.StartsWith(label + " (", StringComparison.OrdinalIgnoreCase));
        var at = note?.IndexOf("): ", StringComparison.Ordinal) ?? -1;
        return at < 0 ? null : note![(at + 3)..];
    }

    private void StartPeripheralsUi()
    {
        _peripherals = PeripheralMetersConfig.Load(PeripheralsConfigPath);
        CountPcBox.IsChecked = _services.Database?.GetSetting(SettingKeys.PcOff) != "1";
        CountPcBox.IsCheckedChanged += (_, _) =>
        {
            _services.Database?.SetSetting(SettingKeys.PcOff, CountPcBox.IsChecked == true ? "0" : "1");
            _ = RefreshCollectorAsync(includeService: false, includeChart: true, showBusy: true);
        };
        ExportCsvButton.Click += async (_, _) => await ChooseExportAsync();
        TestPeripheralButton.Click += async (_, _) => await TestPeripheralAsync();
        OpenPeripheralButton.Click += async (_, _) => await OpenTypedPeripheralPageAsync();
        AddPeripheralButton.Click += async (_, _) => await AddPeripheralAsync();
        RebuildPeripherals();
    }

    /// <summary>The extra meters the user has not switched off in the "Also count" filter. A meter added later counts until it is switched off.</summary>
    private IReadOnlyList<string> CountedPeripherals()
    {
        var off = (_services.Database?.GetSetting(SettingKeys.PeripheralsOff) ?? "")
            .Split('\n', StringSplitOptions.RemoveEmptyEntries).ToHashSet(StringComparer.OrdinalIgnoreCase);
        return _peripherals.Meters.Select(m => m.Label).Where(l => !off.Contains(l)).ToList();
    }

    /// <summary>False when the PC is unticked, so the figures show the extra meters alone. Only meaningful while there are extra meters:
    /// without any, the PC is all there is to count.</summary>
    private bool IncludePc() => _peripherals.Meters.Count == 0 || CountPcBox.IsChecked != false;

    private void SaveCountedPeripherals()
    {
        var on = FilterBoxes().Where(b => b.IsChecked == true).Select(b => (string)b.Content!).ToHashSet(StringComparer.OrdinalIgnoreCase);
        var off = _peripherals.Meters.Select(m => m.Label).Where(l => !on.Contains(l));
        _services.Database?.SetSetting(SettingKeys.PeripheralsOff, string.Join("\n", off));
    }

    private IEnumerable<CheckBox> FilterBoxes() => PeripheralFilterPanel.Children.OfType<CheckBox>();

    /// <summary>Redraws everything that lists the configured meters: the Settings list and the Dashboard's checkboxes and card.</summary>
    private void RebuildPeripherals()
    {
        var counted = CountedPeripherals().ToHashSet(StringComparer.OrdinalIgnoreCase);
        PeripheralFilterPanel.Children.Clear();
        PeripheralSettingsList.Children.Clear();

        foreach (var meter in _peripherals.Meters)
        {
            var box = new CheckBox { Content = meter.Label, IsChecked = counted.Contains(meter.Label), Margin = new Thickness(0, 0, 16, 0) };
            box.IsCheckedChanged += (_, _) =>
            {
                SaveCountedPeripherals();
                _ = RefreshCollectorAsync(includeService: false, includeChart: true, showBusy: true);
            };
            PeripheralFilterPanel.Children.Add(box);

            var name = new TextBox { Text = meter.Label, Width = 180, MaxLength = PeripheralMeter.MaxLabelLength, VerticalAlignment = VerticalAlignment.Center };
            var rename = new Button { Content = "Rename", VerticalAlignment = VerticalAlignment.Center };
            rename.Click += (_, _) => RenamePeripheral(meter, name.Text);
            var open = new Button { Content = "Open in browser", VerticalAlignment = VerticalAlignment.Center };
            open.Click += async (_, _) => await OpenPlugPageAsync(meter.Host, PeripheralStatusText);
            var remove = new Button { Content = "Remove", VerticalAlignment = VerticalAlignment.Center };
            remove.Click += (_, _) => RemovePeripheral(meter);
            var row = new Grid { ColumnDefinitions = new ColumnDefinitions("Auto,Auto,*,Auto,Auto"), ColumnSpacing = 12 };
            row.Children.Add(name);
            Grid.SetColumn(rename, 1);
            row.Children.Add(rename);
            var host = new TextBlock { Text = meter.Host, Classes = { "caption" }, VerticalAlignment = VerticalAlignment.Center };
            Grid.SetColumn(host, 2);
            row.Children.Add(host);
            Grid.SetColumn(open, 3);
            row.Children.Add(open);
            Grid.SetColumn(remove, 4);
            row.Children.Add(remove);
            PeripheralSettingsList.Children.Add(row);
        }

        PeripheralsCard.IsVisible = _peripherals.Meters.Count > 0;
        PeripheralSettingsEmptyText.IsVisible = _peripherals.Meters.Count == 0;
        RenderPeripherals();
    }

    // ----- Settings ---------------------------------------------------------------------

    /// <summary>The meter typed in the boxes, or null (with the reason in the status line) when it cannot be added.</summary>
    private PeripheralMeter? TypedPeripheral()
    {
        var meter = new PeripheralMeter(PeripheralLabelBox.Text?.Trim() ?? "", PeripheralHostBox.Text?.Trim() ?? "");
        var wallHost = WallMeterConfig.Load(WallConfigPath)?.Host;
        if (_peripherals.ProblemAdding(meter, wallHost) is { } problem)
        {
            PeripheralStatusText.Text = problem;
            return null;
        }
        return meter;
    }

    /// <summary>Opens the plug at the address typed in the box (before it is added), so it can be looked at on the network.</summary>
    internal async Task OpenTypedPeripheralPageAsync()
    {
        var host = PeripheralHostBox.Text?.Trim();
        if (string.IsNullOrEmpty(host) || !WallMeterConfig.IsValidHost(host))
        {
            PeripheralStatusText.Text = "Enter just the plug's address first (like 192.168.1.51), without http:// or a path.";
            return;
        }
        await OpenPlugPageAsync(host, PeripheralStatusText);
    }

    internal async Task TestPeripheralAsync()
    {
        if (TypedPeripheral() is not { } meter) return;
        PeripheralStatusText.Text = "Asking the plug...";
        try
        {
            var watts = await _services.ReadWallMeterAsync(meter.Host);
            PeripheralStatusText.Text = $"Reached the plug. It reads {watts.ToString("0.0", CultureInfo.CurrentCulture)} W right now.";
        }
        catch (WallMeterException e)
        {
            PeripheralStatusText.Text = e.Message;
        }
        catch (Exception e) when (e is not OperationCanceledException)
        {
            PeripheralStatusText.Text = "Something unexpected went wrong: " + e.Message;
        }
    }

    /// <summary>Adds the meter only if the address answered like a plug, so a typo is caught here and not as a silently missing line later.</summary>
    internal async Task AddPeripheralAsync()
    {
        if (TypedPeripheral() is not { } meter) return;
        PeripheralStatusText.Text = "Checking the plug before saving...";
        try
        {
            await _services.ReadWallMeterAsync(meter.Host);
        }
        catch (WallMeterException e)
        {
            PeripheralStatusText.Text = "Not saved. " + e.Message;
            return;
        }
        catch (Exception e) when (e is not OperationCanceledException)
        {
            PeripheralStatusText.Text = "Not saved. Something unexpected went wrong: " + e.Message;
            return;
        }

        try
        {
            var updated = new PeripheralMetersConfig(_peripherals.Meters.Append(meter).ToList());
            updated.Save(PeripheralsConfigPath);
            _peripherals = updated;
            PeripheralLabelBox.Text = PeripheralHostBox.Text = "";
            PeripheralStatusText.Text = $"Saved. {meter.Label} appears on the Dashboard within a few seconds.";
            RebuildPeripherals();
            _ = RefreshCollectorAsync(includeService: false, includeChart: true, showBusy: true);
        }
        catch (Exception e) when (e is IOException or UnauthorizedAccessException)
        {
            PeripheralStatusText.Text = "Could not save: " + e.Message;
        }
    }

    /// <summary>Renames a meter. Everything already recorded under the old name is moved to the new one, so the history carries on as one
    /// line. The collector follows the config within a few seconds and may save a last few seconds under the old name, so the move is run
    /// again a little later.</summary>
    internal void RenamePeripheral(PeripheralMeter meter, string? typed)
    {
        var label = typed?.Trim() ?? "";
        if (label == meter.Label) { PeripheralStatusText.Text = "That is already its name."; return; }

        var others = new PeripheralMetersConfig(_peripherals.Meters.Where(m => m != meter).ToList());
        var renamed = meter with { Label = label };
        if (others.ProblemAdding(renamed, WallMeterConfig.Load(WallConfigPath)?.Host) is { } problem)
        {
            PeripheralStatusText.Text = problem;
            return;
        }

        try
        {
            var updated = new PeripheralMetersConfig(_peripherals.Meters.Select(m => m == meter ? renamed : m).ToList());
            updated.Save(PeripheralsConfigPath);
            _peripherals = updated;
        }
        catch (Exception e) when (e is IOException or UnauthorizedAccessException)
        {
            PeripheralStatusText.Text = "Could not rename it: " + e.Message;
            return;
        }

        // A meter that was switched off stays switched off under its new name.
        var off = (_services.Database?.GetSetting(SettingKeys.PeripheralsOff) ?? "").Split('\n', StringSplitOptions.RemoveEmptyEntries)
            .Select(l => string.Equals(l, meter.Label, StringComparison.OrdinalIgnoreCase) ? label : l);
        _services.Database?.SetSetting(SettingKeys.PeripheralsOff, string.Join("\n", off));

        var store = _services.OpenHistory();
        if (store is not null)
            _ = Task.Run(async () =>
            {
                MoveHistory(store, meter.Label, label);
                await Task.Delay(RenameSweepDelay);
                MoveHistory(store, meter.Label, label);
            });

        PeripheralStatusText.Text = $"Renamed {meter.Label} to {label}. Its history was moved with it.";
        RebuildPeripherals();
        _ = RefreshCollectorAsync(includeService: false, includeChart: true, showBusy: true);
    }

    /// <summary>How long after a rename the history is moved a second time, to catch what the collector saved under the old name before it
    /// followed the change.</summary>
    internal static TimeSpan RenameSweepDelay { get; set; } = TimeSpan.FromSeconds(25);

    private static void MoveHistory(HistoryStore store, string oldLabel, string newLabel)
    {
        try { store.RenamePeripheral(oldLabel, newLabel); }
        catch (Exception e) when (e is Microsoft.Data.Sqlite.SqliteException or ObjectDisposedException or IOException) { /* it will be tried again */ }
    }

    private void RemovePeripheral(PeripheralMeter meter)
    {
        try
        {
            var updated = new PeripheralMetersConfig(_peripherals.Meters.Where(m => m != meter).ToList());
            updated.Save(PeripheralsConfigPath);
            _peripherals = updated;
            PeripheralStatusText.Text = $"Removed {meter.Label}. What it already recorded stays in the history but is no longer counted.";
            RebuildPeripherals();
            _ = RefreshCollectorAsync(includeService: false, includeChart: true, showBusy: true);
        }
        catch (Exception e) when (e is IOException or UnauthorizedAccessException)
        {
            PeripheralStatusText.Text = "Could not remove it: " + e.Message;
        }
    }

    // ----- Dashboard --------------------------------------------------------------------

    /// <summary>Takes the live readings of the extra meters (drawn in the Peripherals card) and gives back the ones that are counted
    /// on top of the PC.</summary>
    private IReadOnlyList<PowerReading> ShowPeripheralsLive(IReadOnlyList<PowerReading> readings, IReadOnlyList<string> notes)
    {
        _peripheralNotes = notes;
        _peripheralLive = readings.Where(r => r.Scope == ReadingScope.Peripheral).ToList();
        RenderPeripherals();
        var counted = CountedPeripherals().ToHashSet(StringComparer.OrdinalIgnoreCase);
        return _peripheralLive.Where(r => counted.Contains(r.Name)).ToList();
    }

    /// <summary>Takes what each extra meter recorded in the chosen period (not read every second) and redraws the card.</summary>
    private void ShowPeripheralsPeriod(IReadOnlyList<PeripheralUse>? use, RateSchedule? schedule)
    {
        _peripheralPeriod = use;
        _peripheralRate = _services.Database is null ? null : schedule?.RateAt(DateOnly.FromDateTime(DateTime.Now));
        RenderPeripherals();
    }

    private void RenderPeripherals()
    {
        var currency = _services.Database?.GetSetting(SettingKeys.Currency) ?? "$";
        var counted = CountedPeripherals().ToHashSet(StringComparer.OrdinalIgnoreCase);
        var period = StatsPeriods[Math.Clamp(SelectedStatsIndex, 0, StatsPeriods.Length - 1)].Label;

        double KwhOf(string label) => _peripheralPeriod?.FirstOrDefault(u => string.Equals(u.Name, label, StringComparison.OrdinalIgnoreCase))?.KilowattHours ?? 0;
        var biggest = _peripherals.Meters.Select(m => KwhOf(m.Label)).DefaultIfEmpty(0).Max();

        PeripheralsList.ItemsSource = _peripherals.Meters.Select(m =>
        {
            var kwh = KwhOf(m.Label);
            var live = _peripheralLive.FirstOrDefault(r => string.Equals(r.Name, m.Label, StringComparison.OrdinalIgnoreCase));
            var found = _peripheralPeriod?.FirstOrDefault(u => string.Equals(u.Name, m.Label, StringComparison.OrdinalIgnoreCase));
            var peak = found?.PeakWatts;
            var detail = new List<string>
            {
                live is null ? "no reading right now" : "now " + Watts(live.Watts),
            };
            if (peak is { } p && p > 0) detail.Add("peak " + Watts(p));
            if (found is { GapKwh: > 0 }) detail.Add($"{FormatEnergy(found.GapKwh)} of it counted by the plug while the PC was asleep or off");
            if (ProblemText(_peripheralNotes, m.Label) is { } problem) detail.Add(problem);
            return new HistoryRow(m.Label, FormatEnergy(kwh),
                _peripheralRate is { } rate ? $"{currency}{FormatMoney(CostCalculator.Cost(kwh, rate))}" : "",
                string.Join(" · ", detail),
                Note: counted.Contains(m.Label) ? "" : "not counted above",
                Share: biggest > 0 ? kwh / biggest : 0,
                Bar: UiColors.Brush(VaToken.TextMuted, 0.8));
        }).ToList();
        PeripheralsPeriodText.Text = period + " · each meter on its own. Tick the ones to count on top of the PC in the figures above.";
    }
    // ----- Export -----------------------------------------------------------------------

    private static readonly FilePickerFileType CsvFiles = new("CSV spreadsheet") { Patterns = new[] { "*.csv" } };

    private async Task ChooseExportAsync()
    {
        var picked = await StorageProvider.SaveFilePickerAsync(new FilePickerSaveOptions
        {
            Title = "Export the history for the chosen period",
            SuggestedFileName = "kvguzzler-history.csv",
            DefaultExtension = "csv",
            FileTypeChoices = new[] { CsvFiles },
        });
        if (picked?.TryGetLocalPath() is { } path) await ExportHistoryAsync(path);
    }

    /// <summary>Writes one row per day of the chosen period to a CSV file, and says so (or why not) under the button.</summary>
    internal async Task ExportHistoryAsync(string path)
    {
        if (_services.OpenHistory() is not { } store)
        {
            ExportStatusText.Text = "There is no history to export yet.";
            return;
        }

        ExportStatusText.Text = "Exporting...";
        var span = StatsPeriods[Math.Clamp(SelectedStatsIndex, 0, StatsPeriods.Length - 1)].Span;
        var schedule = _services.Database?.LoadRateSchedule();
        try
        {
            var csv = await Task.Run(() =>
            {
                var now = DateTimeOffset.UtcNow;
                var from = span is { } s ? now - s : store.FirstRecorded() ?? now;
                return HistoryExport.DailyCsv(store, schedule, from, now, TimeZoneInfo.Local);
            });
            await File.WriteAllTextAsync(path, csv, new UTF8Encoding(encoderShouldEmitUTF8Identifier: true));
            ExportStatusText.Text = "Saved " + Path.GetFileName(path) + ": one row per day, in kWh.";
        }
        catch (Exception e) when (e is IOException or UnauthorizedAccessException or Microsoft.Data.Sqlite.SqliteException)
        {
            ExportStatusText.Text = "Could not export: " + e.Message;
        }
    }
}
