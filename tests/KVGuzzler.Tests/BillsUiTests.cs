using System.Globalization;
using Avalonia.Controls;
using Avalonia.Headless.XUnit;
using Avalonia.Interactivity;
using KVGuzzler.Collector;
using KVGuzzler.Core.Billing;
using KVGuzzler.Core.History;
using KVGuzzler.Core.Power;
using KVGuzzler.Data;
using KVGuzzler.Views;
using Xunit;

namespace KVGuzzler.Tests;

public class BillStoreTests
{
    private static Bill Make(string start, string end, decimal total, double kwh, params FixedFee[] fees) =>
        new(0, DateOnly.Parse(start), DateOnly.Parse(end), total, kwh, fees);

    [Fact]
    public void ABillComesBackExactlyWithItsFeesInOrder()
    {
        using var t = new TempData();
        var db = t.Services.Database!;
        var id = db.SaveBill(Make("2026-08-20", "2026-09-21", 206.32m, 1036,
            new FixedFee("Basic service", 6.00m), new FixedFee("Res Savers credit", -10.00m)));

        var bill = Assert.Single(db.GetBills());
        Assert.Equal(id, bill.Id);
        Assert.Equal((DateOnly.Parse("2026-08-20"), DateOnly.Parse("2026-09-21"), 206.32m, 1036.0), (bill.Start, bill.End, bill.Total, bill.Kwh));
        Assert.Equal(new[] { "Basic service", "Res Savers credit" }, bill.FixedFees.Select(f => f.Label));
        Assert.Equal(new[] { 6.00m, -10.00m }, bill.FixedFees.Select(f => f.Amount));
    }

    [Fact]
    public void ABillWithNoFeesHasNone()
    {
        using var t = new TempData();
        t.Services.Database!.SaveBill(Make("2026-01-01", "2026-01-31", 100m, 500));
        Assert.Empty(t.Services.Database.GetBills().Single().FixedFees);
    }

    [Fact]
    public void BillsComeBackOldestFirstWhateverTheOrderTheyWereEntered()
    {
        using var t = new TempData();
        var db = t.Services.Database!;
        db.SaveBill(Make("2026-09-22", "2026-10-23", 180m, 1000));
        db.SaveBill(Make("2026-03-01", "2026-03-31", 100m, 500));       // a past bill entered afterwards
        Assert.Equal(new[] { "2026-03-01", "2026-09-22" }, db.GetBills().Select(b => b.Start.ToString("yyyy-MM-dd")));
    }

    [Fact]
    public void EditingReplacesTheBillAndItsFeesInsteadOfAddingAnother()
    {
        using var t = new TempData();
        var db = t.Services.Database!;
        var id = db.SaveBill(Make("2026-08-20", "2026-09-21", 206.32m, 1036, new FixedFee("a", 1), new FixedFee("b", 2)));

        db.SaveBill(Make("2026-08-20", "2026-09-21", 210m, 1040, new FixedFee("only", 3)) with { Id = id });

        var bill = Assert.Single(db.GetBills());
        Assert.Equal((210m, 1040.0), (bill.Total, bill.Kwh));
        Assert.Equal("only", Assert.Single(bill.FixedFees).Label);
    }

    [Fact]
    public void DeletingRemovesTheBillAndItsFees()
    {
        using var t = new TempData();
        var db = t.Services.Database!;
        var keep = db.SaveBill(Make("2026-01-01", "2026-01-31", 100m, 500, new FixedFee("x", 1)));
        var gone = db.SaveBill(Make("2026-02-01", "2026-02-28", 100m, 500, new FixedFee("y", 1)));

        db.DeleteBill(gone);

        Assert.Equal(keep, Assert.Single(db.GetBills()).Id);
    }

    [Fact]
    public void EditingABillThatWasDeletedIsAnErrorNotASilentNewBill()
    {
        using var t = new TempData();
        Assert.Throws<InvalidOperationException>(() => t.Services.Database!.SaveBill(Make("2026-01-01", "2026-01-31", 100m, 500) with { Id = 999 }));
    }

    [Fact]
    public void TheScheduleFollowsTheChosenModeAndTheSavedRate()
    {
        using var t = new TempData();
        var db = t.Services.Database!;
        db.SetSetting(SettingKeys.RatePerKwh, "0.15");
        db.SaveBill(Make("2026-08-20", "2026-09-21", 200m, 1000));
        var day = DateOnly.Parse("2026-09-01");

        Assert.Equal(0.15m, db.LoadRateSchedule().RateAt(day));          // the default is one rate
        db.SetSetting(SettingKeys.RateMode, "bills");
        Assert.Equal(0.20m, db.LoadRateSchedule().RateAt(day));
        db.SetSetting(SettingKeys.RateMode, "one");
        Assert.Equal(0.15m, db.LoadRateSchedule().RateAt(day));
    }

    [Fact]
    public void BillsSurviveMovingTheDatabase()
    {
        using var t = new TempData();
        t.Services.Database!.SaveBill(Make("2026-08-20", "2026-09-21", 206.32m, 1036, new FixedFee("Basic service", 6m)));
        var target = Path.Combine(t.Dir, "elsewhere", "kvguzzler.db");
        t.Services.CloseDatabase();
        Directory.CreateDirectory(Path.GetDirectoryName(target)!);
        File.Copy(t.Services.DbLocation.GetEffectiveDbPath(), target);
        using var moved = new Database(target);
        Assert.Equal(206.32m, Assert.Single(moved.GetBills()).Total);
    }
}

public class BillsUiTests
{
    private static void UseInvariantCulture() => CultureInfo.CurrentCulture = CultureInfo.InvariantCulture;

    private static (TextBox Label, TextBox Amount) FeeRow(MainWindow w, int i)
    {
        var row = (StackPanel)w.FeesPanel.Children[i];
        return ((TextBox)row.Children[0], (TextBox)row.Children[1]);
    }

    private static Button RowButton(MainWindow w, int bill, string text) =>
        ((StackPanel)((Grid)w.BillsList.Children[bill]).Children[1]).Children.OfType<Button>().First(b => b.Content?.ToString() == text);

    private static void FillForm(MainWindow w, string start, string end, string total, string kwh)
    {
        w.BillStartPicker.SelectedDate = DateTime.Parse(start);
        w.BillEndPicker.SelectedDate = DateTime.Parse(end);
        w.BillTotalBox.Text = total;
        w.BillKwhBox.Text = kwh;
        Avalonia.Threading.Dispatcher.UIThread.RunJobs();
    }

    [AvaloniaFact]
    public void TheModeSwitchShowsTheRightControlsAndRemembersTheChoice()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();
        Assert.True(window.OneRatePanel.IsVisible);
        Assert.False(window.BillsPanel.IsVisible);

        window.RateModeBox.SelectedIndex = 1;
        Assert.False(window.OneRatePanel.IsVisible);
        Assert.True(window.BillsPanel.IsVisible);
        Assert.Equal("bills", t.Services.Database!.GetSetting(SettingKeys.RateMode));

        var reopened = new MainWindow(t.Services);                    // the choice is kept
        reopened.Show();
        Assert.Equal(1, reopened.RateModeBox.SelectedIndex);
        Assert.True(reopened.BillsPanel.IsVisible);

        window.RateModeBox.SelectedIndex = 0;
        Assert.Equal("one", t.Services.Database.GetSetting(SettingKeys.RateMode));
    }

    [AvaloniaFact]
    public async Task AddingABillWorksOutThePriceAndListsIt()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();
        window.RateModeBox.SelectedIndex = 1;

        FillForm(window, "2026-08-20", "2026-09-21", "206.32", "1036");
        Assert.Equal("Price per kWh: $0.1992 (the total divided by the kWh)", window.BillPriceText.Text);

        window.AddFeeButton.RaiseEvent(new RoutedEventArgs(Button.ClickEvent));
        window.AddFeeButton.RaiseEvent(new RoutedEventArgs(Button.ClickEvent));
        window.AddFeeButton.RaiseEvent(new RoutedEventArgs(Button.ClickEvent));   // a third, left empty: ignored
        (FeeRow(window, 0).Label.Text, FeeRow(window, 0).Amount.Text) = ("Basic service", "6.00");
        (FeeRow(window, 1).Label.Text, FeeRow(window, 1).Amount.Text) = ("Res Savers credit", "-10.00");
        Avalonia.Threading.Dispatcher.UIThread.RunJobs();
        Assert.Equal("Price per kWh: $0.2030 (after leaving out 2 flat fees)", window.BillPriceText.Text);

        await window.SaveBillAsync();

        Assert.Equal("Bill saved.", window.BillStatusText.Text);
        var bill = Assert.Single(t.Services.Database!.GetBills());
        Assert.Equal(2, bill.FixedFees.Count);
        Assert.Equal(-4.00m, bill.FixedTotal);
        Assert.Single(window.BillsList.Children);
        var title = ((TextBlock)((StackPanel)((Grid)window.BillsList.Children[0]).Children[0]).Children[0]).Text;
        Assert.Equal("2026-08-20 to 2026-09-21: $206.32, 1036 kWh, $0.2030 per kWh", title);
        Assert.Empty(window.FeesPanel.Children);                      // the form is cleared for the next bill
        Assert.Equal("", window.BillTotalBox.Text);
    }

    [AvaloniaFact]
    public async Task ProblemsAreExplainedAndNothingIsSaved()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();
        window.RateModeBox.SelectedIndex = 1;

        await window.SaveBillAsync();
        Assert.Contains("Choose the first and last day", window.BillStatusText.Text);

        FillForm(window, "2026-09-21", "2026-08-20", "206.32", "1036");           // the wrong way round
        await window.SaveBillAsync();
        Assert.Contains("end date is before the start date", window.BillStatusText.Text);

        FillForm(window, "2026-08-20", "2026-09-21", "lots", "1036");
        await window.SaveBillAsync();
        Assert.Contains("total as a plain number", window.BillStatusText.Text);

        FillForm(window, "2026-08-20", "2026-09-21", "206.32", "1,036");           // a thousands separator is not accepted
        await window.SaveBillAsync();
        Assert.Contains("kWh as a plain number", window.BillStatusText.Text);

        FillForm(window, "2026-08-20", "2026-09-21", "206.32", "1036");
        window.AddFeeButton.RaiseEvent(new RoutedEventArgs(Button.ClickEvent));
        FeeRow(window, 0).Label.Text = "Basic service";                            // a name with no amount
        await window.SaveBillAsync();
        Assert.Contains("Basic service", window.BillStatusText.Text);
        Assert.Contains("amount", window.BillStatusText.Text);

        Assert.Empty(t.Services.Database!.GetBills());
    }

    [AvaloniaFact]
    public async Task TwoBillsCannotClaimTheSameDayButPastBillsAreWelcome()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();
        window.RateModeBox.SelectedIndex = 1;

        FillForm(window, "2026-08-20", "2026-09-21", "206.32", "1036");
        await window.SaveBillAsync();

        FillForm(window, "2026-09-21", "2026-10-23", "150", "900");               // shares 9/21
        await window.SaveBillAsync();
        Assert.Contains("overlap another bill", window.BillStatusText.Text);
        Assert.Single(t.Services.Database!.GetBills());

        FillForm(window, "2026-07-18", "2026-08-19", "190", "950");               // an earlier month, added afterwards
        await window.SaveBillAsync();
        Assert.Equal("Bill saved.", window.BillStatusText.Text);
        Assert.Equal(2, t.Services.Database.GetBills().Count);
        Assert.Equal(2, window.BillsList.Children.Count);
    }

    [AvaloniaFact]
    public async Task EditingLoadsTheBillAndSavesChangesToIt()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();
        window.RateModeBox.SelectedIndex = 1;
        FillForm(window, "2026-08-20", "2026-09-21", "206.32", "1036");
        window.AddFeeButton.RaiseEvent(new RoutedEventArgs(Button.ClickEvent));
        (FeeRow(window, 0).Label.Text, FeeRow(window, 0).Amount.Text) = ("Basic service", "6.00");
        await window.SaveBillAsync();

        RowButton(window, 0, "Edit").RaiseEvent(new RoutedEventArgs(Button.ClickEvent));

        Assert.Equal("206.32", window.BillTotalBox.Text);
        Assert.Equal("1036", window.BillKwhBox.Text);
        Assert.Equal("Basic service", FeeRow(window, 0).Label.Text);
        Assert.Equal("Save changes", window.SaveBillButton.Content);
        window.BillTotalBox.Text = "210.00";
        await window.SaveBillAsync();

        Assert.Equal("Bill updated.", window.BillStatusText.Text);
        var bill = Assert.Single(t.Services.Database!.GetBills());                 // changed in place, not duplicated
        Assert.Equal(210.00m, bill.Total);
        Assert.Equal("Basic service", Assert.Single(bill.FixedFees).Label);
    }

    [AvaloniaFact]
    public async Task DeletingNeedsASecondPress()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();
        window.RateModeBox.SelectedIndex = 1;
        FillForm(window, "2026-08-20", "2026-09-21", "206.32", "1036");
        await window.SaveBillAsync();

        RowButton(window, 0, "Delete").RaiseEvent(new RoutedEventArgs(Button.ClickEvent));
        await window.RefreshBillsAsync();
        Assert.Single(t.Services.Database!.GetBills());                           // one press only asks
        var confirm = RowButton(window, 0, "Really delete?");

        confirm.RaiseEvent(new RoutedEventArgs(Button.ClickEvent));
        await window.RefreshBillsAsync();
        Assert.Empty(t.Services.Database.GetBills());
        Assert.Contains("No bills yet", window.BillsNoteText.Text);
    }

    [AvaloniaFact]
    public async Task HistoryCostsUseThePriceOfTheBillCoveringThoseDays()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var db = t.Services.Database!;
        db.SetSetting(SettingKeys.RatePerKwh, "0.15");
        db.SetSetting(SettingKeys.RateMode, "bills");
        var today = DateOnly.FromDateTime(DateTime.Now);
        db.SaveBill(new Bill(0, today.AddDays(-20), today.AddDays(10), 30m, 100, Array.Empty<FixedFee>()));   // $0.30 per kWh around today
        t.Services.QueryCollectorService = () => ServiceState.Running;
        var now = DateTimeOffset.UtcNow;
        using (var store = new HistoryStore(Path.Combine(t.HistoryDir, HistoryStore.FileName)))
        {
            store.AddMinutes(new[] { new MinuteRow(now.AddMinutes(-10).ToUnixTimeSeconds() / 60 * 60, "CPU package", ReadingScope.Component, 0.1, 60, 100, PowerKind.Cpu) });
            store.SetLive(now, new[] { new PowerReading("CPU package", 50, ReadingScope.Component, PowerKind.Cpu) }, Array.Empty<string>());
        }

        var window = new MainWindow(t.Services);
        window.Show();
        await window.RefreshCollectorAsync(includeService: true);

        var rows = Assert.IsAssignableFrom<IEnumerable<HistoryRow>>(window.HistoryList.ItemsSource).ToList();
        Assert.Equal("Last hour: 0.1 kWh active, $0.03", rows[0].Title);          // 0.1 kWh at the bill's $0.30, not the saved $0.15
    }

    [AvaloniaFact]
    public void TheEstimatorUsesTodaysPrice()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var db = t.Services.Database!;
        db.SetSetting(SettingKeys.RatePerKwh, "0.15");
        db.SetSetting(SettingKeys.RateMode, "bills");
        var today = DateOnly.FromDateTime(DateTime.Now);
        db.SaveBill(new Bill(0, today.AddDays(-5), today.AddDays(5), 40m, 100, Array.Empty<FixedFee>()));    // $0.40

        var window = new MainWindow(t.Services);
        window.Show();
        Assert.Contains("Per hour: $0.16", window.EstimateText.Text);             // 400 W for an hour is 0.4 kWh
    }
}

public class BillCheckTests
{
    private static void UseInvariantCulture() => CultureInfo.CurrentCulture = new CultureInfo("en-US");

    private static (Bill Bill, RateSchedule Schedule) OneDayBill()
    {
        var bill = new Bill(1, new DateOnly(2026, 9, 1), new DateOnly(2026, 9, 1), 5m, 20, Array.Empty<FixedFee>());   // $0.25 per kWh
        return (bill, new RateSchedule(RateMode.FromBills, null, new[] { bill }));
    }

    private static void Record(HistoryStore store, RateSchedule schedule, Bill bill, ReadingScope scope, double watts, int minutes)
    {
        var (from, _) = schedule.Span(bill);
        for (var i = 0; i < minutes; i++)
            store.AddMinutes(new[] { new MinuteRow(from.AddMinutes(i + 10).ToUnixTimeSeconds() / 60 * 60, scope == ReadingScope.Wall ? "Whole PC (wall meter)" : "CPU package",
                scope, watts * 60 / 3_600_000.0, 60, watts, scope == ReadingScope.Wall ? PowerKind.Other : PowerKind.Cpu) });
    }

    [Fact]
    public void TheWallMeterShareOfABillIsShownWithWhatItCost()
    {
        UseInvariantCulture();
        using var h = new TempHistory();
        var (bill, schedule) = OneDayBill();
        Record(h.Store, schedule, bill, ReadingScope.Wall, watts: 200, minutes: 600);       // 10 hours at 200 W = 2 kWh of 20

        var text = MainWindow.CheckText(bill, schedule, h.Store, "$");

        Assert.Contains("wall meter measured 2 kWh of the bill's 20 kWh (10.0%)", text);
        Assert.Contains("about $0.50 of the bill", text);                                      // 2 kWh at $0.25
        Assert.Contains("only recording for 42% of these dates", text);                       // 600 of 1440 minutes
    }

    [Fact]
    public void WithoutAWallMeterTheSensorsShareIsShownAsActiveOnly()
    {
        UseInvariantCulture();
        using var h = new TempHistory();
        var (bill, schedule) = OneDayBill();
        Record(h.Store, schedule, bill, ReadingScope.Component, watts: 100, minutes: 1430);   // all but ten minutes of the day

        var text = MainWindow.CheckText(bill, schedule, h.Store, "$");

        Assert.Contains("CPU and GPU sensors measured 2.4 kWh (11.9% of the bill's kWh), active power only", text);
        Assert.Contains("a wall meter would show the whole PC", text);
        Assert.DoesNotContain("only recording", text);                                         // 99% of the day counts as covered
    }

    [Fact]
    public void WithNothingRecordedItSaysSo()
    {
        UseInvariantCulture();
        using var h = new TempHistory();
        var (bill, schedule) = OneDayBill();
        Assert.Equal("KVGuzzler has no recording for these dates.", MainWindow.CheckText(bill, schedule, h.Store, "$"));
        Assert.Equal("KVGuzzler has no recording for these dates.", MainWindow.CheckText(bill, schedule, null, "$"));
    }
}
