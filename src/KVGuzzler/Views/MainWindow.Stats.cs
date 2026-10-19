using System.Globalization;
using Avalonia.Controls;
using KVGuzzler.Core.History;
using KVGuzzler.Data;

namespace KVGuzzler.Views;

// The period picker shared by the Dashboard and Programs tabs, and the two big numbers at the top of the Dashboard (energy used,
// and what it cost) for the chosen period. The chart and the program lists follow the same choice.
public partial class MainWindow
{
    /// <summary>The choices, in the order of the buttons. A null span means everything ever recorded.</summary>
    internal static readonly (string Label, TimeSpan? Span)[] StatsPeriods =
    {
        ("Last hour", TimeSpan.FromHours(1)),
        ("Last 24 hours", TimeSpan.FromDays(1)),
        ("Last 7 days", TimeSpan.FromDays(7)),
        ("Last 30 days", TimeSpan.FromDays(30)),
        ("Last year", TimeSpan.FromDays(365)),
        ("All time", null),
    };

    private IReadOnlyList<RadioButton> StatsButtons => StatsPeriodPanel.Children.OfType<RadioButton>().ToList();

    private int SelectedStatsIndex
    {
        get
        {
            var i = StatsButtons.ToList().FindIndex(b => b.IsChecked == true);
            return i < 0 ? StatsPeriods.Length - 1 : i;
        }
    }

    private void StartStatsUi()
    {
        // The period applies to the Dashboard and Programs: the one picker sits in the header of whichever is showing.
        Tabs.SelectionChanged += (_, _) =>
        {
            var host = Tabs.SelectedIndex switch { 0 => DashboardPeriodHost, 1 => ProgramsPeriodHost, _ => null };
            if (host is null || ReferenceEquals(PeriodBar.Parent, host)) return;
            (PeriodBar.Parent as Panel)?.Children.Remove(PeriodBar);
            host.Children.Add(PeriodBar);
        };

        var buttons = StatsButtons;
        // The choice is remembered between runs; the first time it is "All time".
        if (int.TryParse(_services.Database?.GetSetting(SettingKeys.StatsPeriod), out var saved) && saved >= 0 && saved < buttons.Count)
            buttons[saved].IsChecked = true;

        for (var i = 0; i < buttons.Count; i++)
        {
            var index = i;
            buttons[i].IsCheckedChanged += (_, _) =>
            {
                if (buttons[index].IsChecked != true) return;
                _services.Database?.SetSetting(SettingKeys.StatsPeriod, index.ToString(CultureInfo.InvariantCulture));
                _ = RefreshCollectorAsync(includeService: false, includeChart: true);
            };
        }
    }

    private void ShowStats(UsageStat? stat)
    {
        if (stat is null)
        {
            StatEnergyText.Text = "-";
            StatCostText.Text = "-";
            StatEnergyCaption.Text = "No history yet. It is recorded by the background collector.";
            StatCostCaption.Text = "";
            return;
        }

        var period = StatsPeriods[Math.Clamp(SelectedStatsIndex, 0, StatsPeriods.Length - 1)].Label;
        var currency = _services.Database?.GetSetting(SettingKeys.Currency) ?? "$";

        StatEnergyText.Text = FormatEnergy(stat.Kwh);
        StatEnergyCaption.Text = period + " · " + (stat.WholePc
            ? "whole PC at the wall" + (stat.CountedSince is { } since ? $", counted since the meter was connected ({since.ToLocalTime().ToString("d", CultureInfo.CurrentCulture)})" : "")
            : "CPU, GPU and memory sensors (active power; a wall meter would show the whole PC)");

        StatCostText.Text = stat.Cost is { } cost ? currency + FormatMoney(cost) : "-";
        StatCostCaption.Text = stat.Cost is null ? "Set your electricity rate (or a bill) in Settings."
            : stat.PriceEstimated ? "Part of this uses an estimated price, until you enter your next bill."
            : "Priced at the rate for each day.";
    }
}
