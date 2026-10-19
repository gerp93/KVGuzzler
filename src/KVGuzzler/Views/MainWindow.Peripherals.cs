using System.Globalization;
using Avalonia;
using Avalonia.Controls;
using Avalonia.Layout;
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

    private void StartPeripheralsUi()
    {
        _peripherals = PeripheralMetersConfig.Load(PeripheralsConfigPath);
        TestPeripheralButton.Click += async (_, _) => await TestPeripheralAsync();
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

            var remove = new Button { Content = "Remove", VerticalAlignment = VerticalAlignment.Center };
            remove.Click += (_, _) => RemovePeripheral(meter);
            var row = new Grid { ColumnDefinitions = new ColumnDefinitions("Auto,*,Auto"), ColumnSpacing = 12 };
            row.Children.Add(new TextBlock { Text = meter.Label, FontWeight = Avalonia.Media.FontWeight.SemiBold, VerticalAlignment = VerticalAlignment.Center });
            var host = new TextBlock { Text = meter.Host, Classes = { "caption" }, VerticalAlignment = VerticalAlignment.Center };
            Grid.SetColumn(host, 1);
            row.Children.Add(host);
            Grid.SetColumn(remove, 2);
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
    private IReadOnlyList<PowerReading> ShowPeripheralsLive(IReadOnlyList<PowerReading> readings)
    {
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
            var peak = _peripheralPeriod?.FirstOrDefault(u => string.Equals(u.Name, m.Label, StringComparison.OrdinalIgnoreCase))?.PeakWatts;
            var detail = new List<string>
            {
                live is null ? "no reading right now" : "now " + Watts(live.Watts),
            };
            if (peak is { } p && p > 0) detail.Add("peak " + Watts(p));
            return new HistoryRow(m.Label, FormatEnergy(kwh),
                _peripheralRate is { } rate ? $"{currency}{FormatMoney(CostCalculator.Cost(kwh, rate))}" : "",
                string.Join(" · ", detail),
                Note: counted.Contains(m.Label) ? "" : "not counted above",
                Share: biggest > 0 ? kwh / biggest : 0,
                Bar: UiColors.Brush(VaToken.TextMuted, 0.8));
        }).ToList();
        PeripheralsPeriodText.Text = period + " · each meter on its own. Tick the ones to count on top of the PC in the figures above.";
    }
}
