using System.Globalization;
using Avalonia.Controls;
using Avalonia.Headless.XUnit;
using KVGuzzler.Collector;
using KVGuzzler.Core.Billing;
using KVGuzzler.Core.History;
using KVGuzzler.Core.Power;
using KVGuzzler.Data;
using KVGuzzler.Views;
using Xunit;

namespace KVGuzzler.Tests;

public class UsageSummaryTests
{
    private static readonly DateTimeOffset Now = new(2026, 10, 19, 12, 0, 0, TimeSpan.Zero);
    private static readonly RateSchedule Flat20 = new(RateMode.OneRate, 0.20m, Array.Empty<Bill>(), TimeZoneInfo.Utc);

    private static MinuteRow Row(DateTimeOffset at, ReadingScope scope, double kwh) =>
        new(at.ToUnixTimeSeconds() / 60 * 60, scope == ReadingScope.Wall ? "Whole PC (wall meter)" : "CPU package", scope, kwh, 60, 100,
            scope == ReadingScope.Wall ? PowerKind.Other : PowerKind.Cpu);

    [Fact]
    public void TheStoreSumsEnergyAndKnowsWhenRecordingBegan()
    {
        using var h = new TempHistory();
        Assert.Null(h.Store.FirstRecorded());
        Assert.Equal(0, h.Store.EnergyKwh(Now.AddDays(-1), Now));

        h.Store.AddMinutes(new[]
        {
            Row(Now.AddDays(-3), ReadingScope.Component, 0.5),
            Row(Now.AddMinutes(-30), ReadingScope.Component, 0.25),
            Row(Now.AddMinutes(-20), ReadingScope.Wall, 0.75),
        });

        Assert.Equal(0.25, h.Store.EnergyKwh(Now.AddHours(1).AddHours(-2), Now), precision: 9);                  // the sensors in the last two hours
        Assert.Equal(0.75, h.Store.EnergyKwh(Now.AddHours(-2), Now, ReadingScope.Wall), precision: 9);
        Assert.Equal(Now.AddDays(-3).ToUnixTimeSeconds() / 60 * 60, h.Store.FirstRecorded()!.Value.ToUnixTimeSeconds());
        Assert.Equal(Now.AddMinutes(-20).ToUnixTimeSeconds() / 60 * 60, h.Store.FirstRecorded(ReadingScope.Wall)!.Value.ToUnixTimeSeconds());
    }

    [Fact]
    public void WithoutAWallMeterItIsTheSensorsEnergyAndItsCost()
    {
        using var h = new TempHistory();
        h.Store.AddMinutes(new[]
        {
            Row(Now.AddMinutes(-10), ReadingScope.Component, 0.01),
            Row(Now.AddMinutes(-30), ReadingScope.Component, 0.01),
            Row(Now.AddHours(-3), ReadingScope.Component, 0.01),
        });

        var hour = UsageSummary.Compute(h.Store, Flat20, TimeSpan.FromHours(1), Now);
        Assert.Equal(0.02, hour.Kwh, precision: 9);
        Assert.Equal(0.004m, hour.Cost);                      // 0.02 kWh at $0.20
        Assert.False(hour.WholePc);
        Assert.Null(hour.CountedSince);

        var all = UsageSummary.Compute(h.Store, Flat20, null, Now);       // everything ever recorded
        Assert.Equal(0.03, all.Kwh, precision: 9);
        Assert.Equal(Now.AddHours(-3).ToUnixTimeSeconds() / 60 * 60, all.From.ToUnixTimeSeconds());
    }

    [Fact]
    public void WithAWallMeterItIsTheWholePcAndTheSensorsAreNotAddedToIt()
    {
        using var h = new TempHistory();
        h.Store.AddMinutes(new[]
        {
            Row(Now.AddDays(-5), ReadingScope.Component, 1.0),             // before the plug was connected
            Row(Now.AddMinutes(-40), ReadingScope.Component, 0.01),
            Row(Now.AddMinutes(-40), ReadingScope.Wall, 0.03),
            Row(Now.AddMinutes(-20), ReadingScope.Component, 0.01),
            Row(Now.AddMinutes(-20), ReadingScope.Wall, 0.03),
        });

        var week = UsageSummary.Compute(h.Store, Flat20, TimeSpan.FromDays(7), Now);

        Assert.True(week.WholePc);
        Assert.Equal(0.06, week.Kwh, precision: 9);                        // the wall's 0.06, not 0.06 + 0.02 + 1.0
        Assert.Equal(0.012m, week.Cost);
        Assert.Equal(Now.AddMinutes(-40).ToUnixTimeSeconds() / 60 * 60, week.CountedSince!.Value.ToUnixTimeSeconds());   // the meter began part-way through
    }

    [Fact]
    public void WhenTheWallMeterCoversTheWholePeriodThereIsNoCountedSinceNote()
    {
        using var h = new TempHistory();
        h.Store.AddMinutes(new[] { Row(Now.AddMinutes(-59), ReadingScope.Wall, 0.03) });
        Assert.Null(UsageSummary.Compute(h.Store, Flat20, TimeSpan.FromHours(1), Now).CountedSince);
    }

    [Fact]
    public void NothingRecordedIsZeroEnergy()
    {
        using var h = new TempHistory();
        var all = UsageSummary.Compute(h.Store, Flat20, null, Now);
        Assert.Equal(0, all.Kwh);
        Assert.Equal(0m, all.Cost);
    }

    [Fact]
    public void TheAverageDayIsWhatWasReallyUsedOverTheTimeRecorded()
    {
        using var h = new TempHistory();
        h.Store.AddMinutes(new[]
        {
            Row(Now.AddDays(-2), ReadingScope.Component, 1.0),
            Row(Now.AddMinutes(-1), ReadingScope.Component, 1.0),
        });

        // A week was asked for but only two days exist: the average is over those two days, not diluted over seven.
        var week = UsageSummary.AverageDay(h.Store, TimeSpan.FromDays(7), Now)!;
        Assert.Equal(2.0, week.Kwh, precision: 9);
        Assert.Equal(2.0, week.Days, precision: 6);
        Assert.Equal(1.0, week.KwhPerDay, precision: 6);
        Assert.Equal(1000.0 / 24, week.AverageWatts, precision: 4);
        Assert.False(week.WholePc);

        // The last hour holds nothing, and less than an hour of data is too little to project from.
        Assert.Null(UsageSummary.AverageDay(h.Store, TimeSpan.FromMinutes(30), Now));
    }

    [Fact]
    public void TheAverageDayUsesTheWholePcOnceTheWallMeterHasRecorded()
    {
        using var h = new TempHistory();
        h.Store.AddMinutes(new[]
        {
            Row(Now.AddDays(-1), ReadingScope.Component, 0.5),
            Row(Now.AddDays(-1), ReadingScope.Wall, 0.9),
            Row(Now.AddMinutes(-1), ReadingScope.Component, 0.5),
            Row(Now.AddMinutes(-1), ReadingScope.Wall, 0.9),
        });

        var use = UsageSummary.AverageDay(h.Store, null, Now)!;
        Assert.True(use.WholePc);
        Assert.Equal(1.8, use.Kwh, precision: 9);                      // the plug's figure; the sensors are not added to it
        Assert.Null(UsageSummary.AverageDay(new TempHistory().Store, null, Now));   // nothing recorded yet
    }

    [Fact]
    public void WithNoPriceTheCostIsUnknownNotZero()
    {
        using var h = new TempHistory();
        h.Store.AddMinutes(new[] { Row(Now.AddMinutes(-10), ReadingScope.Component, 0.01) });
        var noRate = new RateSchedule(RateMode.OneRate, null, Array.Empty<Bill>(), TimeZoneInfo.Utc);
        var stat = UsageSummary.Compute(h.Store, noRate, TimeSpan.FromHours(1), Now);
        Assert.Equal(0.01, stat.Kwh, precision: 9);
        Assert.Null(stat.Cost);
    }

    [Fact]
    public void EachDayIsPricedByItsOwnBillAndLaterDaysAreAnEstimate()
    {
        using var h = new TempHistory();
        var old = new Bill(1, new DateOnly(2026, 9, 1), new DateOnly(2026, 9, 30), 200m, 1000, Array.Empty<FixedFee>());     // $0.20
        var recent = new Bill(2, new DateOnly(2026, 10, 1), new DateOnly(2026, 10, 15), 90m, 500, Array.Empty<FixedFee>());   // $0.18
        var schedule = new RateSchedule(RateMode.FromBills, 0.15m, new[] { old, recent }, TimeZoneInfo.Utc);
        h.Store.AddMinutes(new[]
        {
            Row(new DateTimeOffset(2026, 9, 10, 12, 0, 0, TimeSpan.Zero), ReadingScope.Component, 1.0),    // in the first bill
            Row(new DateTimeOffset(2026, 10, 10, 12, 0, 0, TimeSpan.Zero), ReadingScope.Component, 1.0),   // in the second
            Row(new DateTimeOffset(2026, 10, 18, 12, 0, 0, TimeSpan.Zero), ReadingScope.Component, 1.0),   // after the latest bill
        });

        var stat = UsageSummary.Compute(h.Store, schedule, null, Now);

        Assert.Equal(3.0, stat.Kwh, precision: 9);
        Assert.Equal(0.20m + 0.18m + 0.18m, stat.Cost);
        Assert.True(stat.PriceEstimated);
    }
}

public class StatsCardsUiTests
{
    private static void UseInvariantCulture() => CultureInfo.CurrentCulture = CultureInfo.InvariantCulture;

    private static void Seed(TempData t)
    {
        var now = DateTimeOffset.UtcNow;
        using var store = new HistoryStore(Path.Combine(t.HistoryDir, HistoryStore.FileName));
        store.AddMinutes(new[]
        {
            new MinuteRow(now.AddMinutes(-10).ToUnixTimeSeconds() / 60 * 60, "CPU package", ReadingScope.Component, 0.5, 60, 100, PowerKind.Cpu),
            new MinuteRow(now.AddDays(-3).ToUnixTimeSeconds() / 60 * 60, "CPU package", ReadingScope.Component, 2.0, 60, 100, PowerKind.Cpu),
        });
        store.SetLive(now, new[] { new PowerReading("CPU package", 50, ReadingScope.Component, PowerKind.Cpu) }, Array.Empty<string>());
    }

    private static IReadOnlyList<RadioButton> Cards(MainWindow w) => w.StatsPeriodPanel.Children.OfType<RadioButton>().ToList();

    [AvaloniaFact]
    public void ThePeriodPickerSitsInTheHeaderOfTheDashboardAndProgramsPages()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();

        Assert.Same(window.DashboardPeriodHost, window.PeriodBar.Parent);       // never over the tab strip
        window.Tabs.SelectedIndex = 1;
        Avalonia.Threading.Dispatcher.UIThread.RunJobs();
        Assert.Same(window.ProgramsPeriodHost, window.PeriodBar.Parent);
        Assert.Empty(window.DashboardPeriodHost.Children);
        window.Tabs.SelectedIndex = 2;                                          // Settings has nothing to filter: it stays where it was
        Avalonia.Threading.Dispatcher.UIThread.RunJobs();
        Assert.Same(window.ProgramsPeriodHost, window.PeriodBar.Parent);
        window.Tabs.SelectedIndex = 0;
        Avalonia.Threading.Dispatcher.UIThread.RunJobs();
        Assert.Same(window.DashboardPeriodHost, window.PeriodBar.Parent);
    }

    private static void ChooseEstimate(MainWindow w, string mode)
    {
        w.EstimateModePanel.Children.OfType<RadioButton>().Single(b => b.Content!.ToString() == mode).IsChecked = true;
        Avalonia.Threading.Dispatcher.UIThread.RunJobs();
    }

    [AvaloniaFact]
    public async Task TheEstimatorCanUseWhatThePcDrawsRightNow()
    {
        UseInvariantCulture();
        using var t = new TempData();
        t.Services.Database!.SetSetting(SettingKeys.RatePerKwh, "0.20");
        t.Services.Database.SetSetting(SettingKeys.Currency, "$");
        t.Services.QueryCollectorService = () => ServiceState.Running;
        Seed(t);                                                // the collector's live reading is 50 W

        var window = new MainWindow(t.Services);
        window.Show();
        await window.RefreshCollectorAsync(includeService: true);
        ChooseEstimate(window, "Right now");

        Assert.False(window.EstimateConstantPanel.IsVisible);
        Assert.Contains("50.0 W", window.EstimateBasisText.Text);
        Assert.Contains("Per day: $0.24 · 1.2 kWh", window.EstimateText.Text);        // 50 W x 24 h = 1.2 kWh at $0.20
        Assert.Contains("Per year: $87.60", window.EstimateText.Text);
    }

    [AvaloniaFact]
    public async Task TheEstimatorCanProjectFromWhatWasReallyUsed()
    {
        UseInvariantCulture();
        using var t = new TempData();
        t.Services.Database!.SetSetting(SettingKeys.RatePerKwh, "0.20");
        t.Services.Database.SetSetting(SettingKeys.Currency, "$");
        t.Services.QueryCollectorService = () => ServiceState.Running;
        Seed(t);                                                // 2.5 kWh over the 3 days since recording began

        var window = new MainWindow(t.Services);
        window.Show();
        ChooseEstimate(window, "My usage");
        await window.RefreshCollectorAsync(includeService: true);

        Assert.True(window.EstimateHistoryPanel.IsVisible);
        Assert.Contains("0.833 kWh a day", window.EstimateBasisText.Text);
        Assert.Contains("counting the time it was off or asleep", window.EstimateBasisText.Text);
        Assert.DoesNotContain("Per hour", window.EstimateText.Text);                    // an average day has no "per hour"
        Assert.Contains("Per 30 days: $5.00", window.EstimateText.Text);                // 0.8333 kWh x 30 x $0.20

        // A window longer than the data says what it really covered.
        window.EstimateWindowBox.SelectedIndex = 3;
        await window.RefreshCollectorAsync(includeService: false);
        Assert.Contains("recorded so far", window.EstimateBasisText.Text);
    }

    [AvaloniaFact]
    public async Task WithTooLittleHistoryTheUsageEstimateSaysSo()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();
        ChooseEstimate(window, "My usage");
        await window.RefreshCollectorAsync(includeService: true);
        Assert.Contains("Not enough history", window.EstimateText.Text);
    }

    [AvaloniaFact]
    public void ThereAreSixBlocksAndAllTimeIsChosenFirst()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();

        Assert.Equal(new[] { "Last hour", "Last 24 hours", "Last 7 days", "Last 30 days", "Last year", "All time" },
            Cards(window).Select(c => c.Content!.ToString()));
        Assert.Equal("All time", Cards(window).Single(c => c.IsChecked == true).Content);
    }

    [AvaloniaFact]
    public async Task TheTwoBigNumbersFollowTheChosenBlock()
    {
        UseInvariantCulture();
        using var t = new TempData();
        t.Services.Database!.SetSetting(SettingKeys.RatePerKwh, "0.20");
        t.Services.Database.SetSetting(SettingKeys.Currency, "$");
        t.Services.QueryCollectorService = () => ServiceState.Running;
        Seed(t);

        var window = new MainWindow(t.Services);
        window.Show();
        await window.RefreshCollectorAsync(includeService: true);

        Assert.Equal("2.5 kWh", window.StatEnergyText.Text);                            // all time: 0.5 + 2.0
        Assert.Equal("$0.50", window.StatCostText.Text);                                // at $0.20
        Assert.Contains("All time", window.StatEnergyCaption.Text);
        Assert.Contains("CPU, GPU and memory sensors", window.StatEnergyCaption.Text);

        Cards(window).Single(c => c.Content!.ToString() == "Last 24 hours").IsChecked = true;
        Avalonia.Threading.Dispatcher.UIThread.RunJobs();
        await window.RefreshCollectorAsync(includeService: false);

        Assert.Equal("0.5 kWh", window.StatEnergyText.Text);                            // only the recent minute
        Assert.Equal("$0.10", window.StatCostText.Text);
        Assert.Contains("Last 24 hours", window.StatEnergyCaption.Text);
    }

    [AvaloniaFact]
    public void TheChosenBlockIsRememberedNextTime()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();

        Cards(window).Single(c => c.Content!.ToString() == "Last 7 days").IsChecked = true;
        Avalonia.Threading.Dispatcher.UIThread.RunJobs();
        Assert.Equal("2", t.Services.Database!.GetSetting(SettingKeys.StatsPeriod));

        var reopened = new MainWindow(t.Services);
        reopened.Show();
        Assert.Equal("Last 7 days", Cards(reopened).Single(c => c.IsChecked == true).Content);
    }

    [AvaloniaFact]
    public async Task WithNoHistoryTheNumbersAreDashesAndTheCaptionSaysWhy()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();
        await window.RefreshCollectorAsync(includeService: true);

        Assert.Equal("-", window.StatEnergyText.Text);
        Assert.Equal("-", window.StatCostText.Text);
        Assert.Contains("No history yet", window.StatEnergyCaption.Text);
    }

    [AvaloniaFact]
    public async Task WithoutAPriceTheCostCardPointsToSettings()
    {
        UseInvariantCulture();
        using var t = new TempData();
        t.Services.QueryCollectorService = () => ServiceState.Running;
        Seed(t);
        var window = new MainWindow(t.Services);
        window.Show();
        await window.RefreshCollectorAsync(includeService: true);

        Assert.Equal("2.5 kWh", window.StatEnergyText.Text);
        Assert.Equal("-", window.StatCostText.Text);
        Assert.Contains("Settings", window.StatCostCaption.Text);
    }
}
