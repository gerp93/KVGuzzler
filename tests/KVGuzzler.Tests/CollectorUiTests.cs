using System.Globalization;
using Avalonia.Controls;
using Avalonia.Headless.XUnit;
using Avalonia.Interactivity;
using KVGuzzler.Collector;
using KVGuzzler.Core.History;
using KVGuzzler.Core.Power;
using KVGuzzler.Data;
using KVGuzzler.Views;
using Xunit;

namespace KVGuzzler.Tests;

public class CollectorUiTests
{
    private static void UseInvariantCulture() => CultureInfo.CurrentCulture = CultureInfo.InvariantCulture;

    /// <summary>Writes what a collector would have saved: readings just now, and an hour of history.</summary>
    private static void FakeCollectorHistory(TempData t, DateTimeOffset liveAt)
    {
        using var store = new HistoryStore(Path.Combine(t.HistoryDir, HistoryStore.FileName));
        var now = DateTimeOffset.UtcNow;
        var minute = now.AddMinutes(-30).ToUnixTimeSeconds() / 60 * 60;
        store.AddMinutes(new[] { new MinuteRow(minute, "GPU", ReadingScope.Component, 0.1, 60, 300) });
        store.AddGap(new Gap(now.AddMinutes(-50), now.AddMinutes(-40), GapReason.Asleep));
        store.SetLive(liveAt, new[]
        {
            new PowerReading("NVIDIA GeForce RTX 5070 Ti", 80, ReadingScope.Component),
            new PowerReading("CPU package", 65, ReadingScope.Component),
            new PowerReading("CPU cores", 56, ReadingScope.Breakdown),
        }, Array.Empty<string>());
    }

    [AvaloniaFact]
    public async Task WithoutTheCollectorItSaysRecordingIsOffAndOffersToTurnItOn()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();
        await window.RefreshCollectorAsync(includeService: true);

        Assert.StartsWith("Off.", window.CollectorStatusText.Text);
        Assert.True(window.InstallCollectorButton.IsVisible);
        Assert.False(window.RemoveCollectorButton.IsVisible);
        Assert.Contains("No history yet", window.HistoryNoteText.Text);
    }

    [AvaloniaFact]
    public async Task ARunningCollectorsReadingsAndHistoryAreShown()
    {
        UseInvariantCulture();
        using var t = new TempData();
        t.Services.Database!.SetSetting(SettingKeys.RatePerKwh, "0.20");
        t.Services.Database.SetSetting(SettingKeys.Currency, "$");
        t.Services.QueryCollectorService = () => ServiceState.Running;
        FakeCollectorHistory(t, liveAt: DateTimeOffset.UtcNow);

        var window = new MainWindow(t.Services);
        window.Show();
        await window.RefreshCollectorAsync(includeService: true);

        Assert.StartsWith("On.", window.CollectorStatusText.Text);
        Assert.False(window.InstallCollectorButton.IsVisible);
        Assert.True(window.RemoveCollectorButton.IsVisible);
        Assert.Equal("Total active: 145.0 W", window.PowerTotalText.Text);   // the breakdown line is not added
        Assert.False(window.PowerSessionText.IsVisible);                        // "since started" belongs to the window's own sampling

        var rows = Assert.IsAssignableFrom<IEnumerable<HistoryRow>>(window.HistoryList.ItemsSource).ToList();
        Assert.Equal(4, rows.Count);
        Assert.Equal("Last hour: 0.1 kWh active, $0.02", rows[0].Title);   // the 10 minute sleep is not added to the active figure
        Assert.Contains("asleep 10 min", rows[0].Detail);
    }

    [AvaloniaFact]
    public async Task AStoppedCollectorIsReportedWithWhenItLastRecorded()
    {
        UseInvariantCulture();
        using var t = new TempData();
        t.Services.QueryCollectorService = () => ServiceState.Stopped;
        FakeCollectorHistory(t, liveAt: DateTimeOffset.UtcNow.AddMinutes(-20));

        var window = new MainWindow(t.Services);
        window.Show();
        await window.RefreshCollectorAsync(includeService: true);

        Assert.Contains("stopped", window.CollectorStatusText.Text);
        Assert.True(window.InstallCollectorButton.IsVisible);
        Assert.Equal("Start background recording", window.InstallCollectorButton.Content);
        Assert.True(window.CollectorLastSeenText.IsVisible);
        Assert.StartsWith("The collector last recorded at", window.CollectorLastSeenText.Text);
    }

    [AvaloniaFact]
    public void SleepWattsAreValidatedAndSaved()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();
        Assert.Equal("2", window.SleepWattsBox.Text);

        window.SleepWattsBox.Text = "lots";
        window.SaveSleepButton.RaiseEvent(new RoutedEventArgs(Button.ClickEvent));
        Assert.Contains("between 0 and 100", window.SleepStatusText.Text);
        Assert.Null(t.Services.Database!.GetSetting(SettingKeys.SleepWatts));

        window.SleepWattsBox.Text = "3.5";
        window.SaveSleepButton.RaiseEvent(new RoutedEventArgs(Button.ClickEvent));
        Assert.Equal("Saved.", window.SleepStatusText.Text);
        Assert.Equal("3.5", t.Services.Database.GetSetting(SettingKeys.SleepWatts));
    }

    [Fact]
    public void HistoryLinesSayWhatWasMeasuredEstimatedAndMissing()
    {
        UseInvariantCulture();
        var p = new PeriodReport("Last 24 hours", MeasuredKwh: 2.4, EstimatedKwh: 0.02, Monitored: TimeSpan.FromHours(13).Add(TimeSpan.FromMinutes(20)),
            Asleep: TimeSpan.FromHours(10), Off: TimeSpan.FromHours(0.5), LostPower: TimeSpan.FromMinutes(3));

        Assert.Equal("Last 24 hours: 2.4 kWh active, $0.48", MainWindow.HistoryTitle(p, 0.20m, "$"));
        Assert.Equal("Last 24 hours: 2.4 kWh active", MainWindow.HistoryTitle(p, null, "$"));
        Assert.Equal("active energy over 13 h 20 min recorded · asleep 10 h 0 min (about 0.02 kWh at the sleep wattage, not counted above) · off 30 min · lost power or crashed 3 min",
            MainWindow.HistoryDetail(p));
    }

    [Theory]
    [InlineData(30, "under a minute")]
    [InlineData(300, "5 min")]
    [InlineData(5400, "1 h 30 min")]
    [InlineData(90000, "1 d 1 h")]
    public void SpansReadNaturally(int seconds, string expected) =>
        Assert.Equal(expected, MainWindow.FormatSpan(TimeSpan.FromSeconds(seconds)));
}
