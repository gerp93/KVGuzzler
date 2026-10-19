using System.Globalization;
using Avalonia.Controls;
using Avalonia.Headless.XUnit;
using Avalonia.Interactivity;
using KVGuzzler.Collector;
using KVGuzzler.Core.History;
using KVGuzzler.Core.Power;
using KVGuzzler.Views;
using Xunit;

namespace KVGuzzler.Tests;

public class WallMeterUiTests
{
    private static void UseInvariantCulture() => CultureInfo.CurrentCulture = CultureInfo.InvariantCulture;

    private static string ConfigPath(TempData t) => Path.Combine(t.HistoryDir, WallMeterConfig.FileName);

    private static void LiveReadings(TempData t, bool withWall)
    {
        using var store = new HistoryStore(Path.Combine(t.HistoryDir, HistoryStore.FileName));
        var readings = new List<PowerReading>
        {
            new("NVIDIA GeForce RTX 5070 Ti", 80, ReadingScope.Component),
            new("CPU package", 65, ReadingScope.Component),
            new("CPU cores", 56, ReadingScope.Breakdown),
        };
        if (withWall) readings.Add(new PowerReading(WallMeterSource.ReadingName, 200, ReadingScope.Wall));
        store.SetLive(DateTimeOffset.UtcNow, readings, Array.Empty<string>());
    }

    [AvaloniaFact]
    public async Task TestShowsWhatThePlugReadsRightNow()
    {
        UseInvariantCulture();
        using var t = new TempData();
        t.Services.ReadWallMeterAsync = _ => Task.FromResult(52.1);
        var window = new MainWindow(t.Services);
        window.Show();

        window.WallHostBox.Text = "192.168.1.50";
        await window.TestWallMeterAsync();

        Assert.Equal("Reached the plug. It reads 52.1 W right now.", window.WallStatusText.Text);
        Assert.False(File.Exists(ConfigPath(t)));   // testing saves nothing
    }

    [AvaloniaFact]
    public async Task SaveOnlyKeepsAnAddressThatAnsweredLikeAPlug()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();
        window.WallHostBox.Text = "192.168.1.50";

        t.Services.ReadWallMeterAsync = _ => throw new WallMeterException("192.168.1.50 did not answer within 3 seconds.");
        await window.SaveWallMeterAsync();
        Assert.StartsWith("Not saved. 192.168.1.50 did not answer", window.WallStatusText.Text);
        Assert.False(File.Exists(ConfigPath(t)));

        t.Services.ReadWallMeterAsync = _ => Task.FromResult(52.1);
        await window.SaveWallMeterAsync();
        Assert.StartsWith("Saved.", window.WallStatusText.Text);
        Assert.Equal("192.168.1.50", WallMeterConfig.Load(ConfigPath(t))!.Host);
    }

    [AvaloniaFact]
    public async Task AUrlOrAPathIsRefusedBeforeAnythingIsAsked()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var asked = false;
        t.Services.ReadWallMeterAsync = _ => { asked = true; return Task.FromResult(0.0); };
        var window = new MainWindow(t.Services);
        window.Show();

        window.WallHostBox.Text = "http://192.168.1.50/rpc/Shelly.Reboot";
        await window.TestWallMeterAsync();
        await window.SaveWallMeterAsync();

        Assert.Contains("Enter just the address", window.WallStatusText.Text);
        Assert.False(asked);
        Assert.False(File.Exists(ConfigPath(t)));
    }

    [AvaloniaFact]
    public async Task AnUnexpectedErrorInTestOrSaveIsAMessageNotACrash()
    {
        UseInvariantCulture();
        using var t = new TempData();
        t.Services.ReadWallMeterAsync = _ => throw new UriFormatException("Invalid URI: The hostname could not be parsed.");
        var window = new MainWindow(t.Services);
        window.Show();
        window.WallHostBox.Text = "192.168.68.106";

        await window.TestWallMeterAsync();
        Assert.StartsWith("Something unexpected went wrong:", window.WallStatusText.Text);

        await window.SaveWallMeterAsync();
        Assert.StartsWith("Not saved. Something unexpected went wrong:", window.WallStatusText.Text);
        Assert.False(File.Exists(ConfigPath(t)));
    }

    [AvaloniaFact]
    public void TheSavedAddressIsShownAgainAndRemoveForgetsIt()
    {
        UseInvariantCulture();
        using var t = new TempData();
        new WallMeterConfig("192.168.1.50").Save(ConfigPath(t));
        var window = new MainWindow(t.Services);
        window.Show();
        Assert.Equal("192.168.1.50", window.WallHostBox.Text);

        window.RemoveWallButton.RaiseEvent(new RoutedEventArgs(Button.ClickEvent));
        Assert.False(File.Exists(ConfigPath(t)));
        Assert.Equal("", window.WallHostBox.Text);
    }

    [AvaloniaFact]
    public async Task TheDashboardShowsTheWholePcAndWhatTheSensorsMiss()
    {
        UseInvariantCulture();
        using var t = new TempData();
        t.Services.QueryCollectorService = () => ServiceState.Running;
        LiveReadings(t, withWall: true);

        var window = new MainWindow(t.Services);
        window.Show();
        await window.RefreshCollectorAsync(includeService: true);

        Assert.True(window.WallPowerText.IsVisible);
        Assert.Equal("Whole PC at the wall: 200.0 W", window.WallPowerText.Text);
        Assert.Equal("Total active: 145.0 W", window.PowerTotalText.Text);                // the wall is not added to the parts
        Assert.Contains("The other 55.0 W is fans, drives", window.WallBreakdownText.Text);
        var rows = Assert.IsAssignableFrom<IEnumerable<ReadingRow>>(window.ReadingsList.ItemsSource).ToList();
        Assert.DoesNotContain(rows, r => r.Name.Contains("Whole PC"));                     // it has its own line
        Assert.Contains("matches your bill", window.PowerInfoText.Text);
    }

    [AvaloniaFact]
    public async Task WithoutAWallMeterTheDashboardPointsToSettings()
    {
        UseInvariantCulture();
        using var t = new TempData();
        t.Services.QueryCollectorService = () => ServiceState.Running;
        LiveReadings(t, withWall: false);

        var window = new MainWindow(t.Services);
        window.Show();
        await window.RefreshCollectorAsync(includeService: true);

        Assert.False(window.WallPowerText.IsVisible);
        Assert.False(window.WallBreakdownText.IsVisible);
        Assert.Contains("Set up a wall meter in Settings", window.PowerInfoText.Text);
    }

    [Fact]
    public void WithAWallMeterTheHistoryHeadlineIsTheWholePc()
    {
        UseInvariantCulture();
        var p = new PeriodReport("Last 24 hours", MeasuredKwh: 2.4, EstimatedKwh: 0.02, Monitored: TimeSpan.FromHours(13),
            Asleep: TimeSpan.FromHours(10), Off: TimeSpan.FromHours(0.5), LostPower: TimeSpan.FromMinutes(3), WallKwh: 4.1);

        Assert.Equal("Last 24 hours: 4.1 kWh whole PC, $0.82", MainWindow.HistoryTitle(p, 0.20m, "$"));
        // The sleep estimate is dropped from the detail: the wall meter measured the sleeping hours itself.
        Assert.Equal("of which CPU, GPU and memory 2.4 kWh · asleep 10 h 0 min · off 30 min · lost power or crashed 3 min",
            MainWindow.HistoryDetail(p));
    }
}
