using System.Globalization;
using Avalonia.Controls;
using Avalonia.Headless.XUnit;
using KVGuzzler.Collector;
using KVGuzzler.Core.History;
using KVGuzzler.Core.Power;
using KVGuzzler.Views;
using Xunit;

namespace KVGuzzler.Tests;

public class PeripheralUiTests
{
    private static void UseInvariantCulture() => CultureInfo.CurrentCulture = CultureInfo.InvariantCulture;

    private static string ConfigPath(TempData t) => Path.Combine(t.HistoryDir, PeripheralMetersConfig.FileName);

    private static void Live(TempData t)
    {
        using var store = new HistoryStore(Path.Combine(t.HistoryDir, HistoryStore.FileName));
        store.SetLive(DateTimeOffset.UtcNow, new List<PowerReading>
        {
            new("CPU package", 65, ReadingScope.Component),
            new(WallMeterSource.ReadingName, 200, ReadingScope.Wall),
            new("Monitor", 30, ReadingScope.Peripheral),
            new("Speakers", 10, ReadingScope.Peripheral),
        }, Array.Empty<string>());
    }

    [AvaloniaFact]
    public async Task AMeterIsAddedOnlyAfterItAnswersAndShowsUpInTheFilter()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();
        Assert.False(window.PeripheralsCard.IsVisible);   // nothing to show until a meter exists

        window.PeripheralLabelBox.Text = "Monitor";
        window.PeripheralHostBox.Text = "192.168.1.51";
        t.Services.ReadWallMeterAsync = _ => throw new WallMeterException("192.168.1.51 did not answer within 3 seconds.");
        await window.AddPeripheralAsync();
        Assert.StartsWith("Not saved. 192.168.1.51 did not answer", window.PeripheralStatusText.Text);
        Assert.False(File.Exists(ConfigPath(t)));

        t.Services.ReadWallMeterAsync = _ => Task.FromResult(31.5);
        await window.AddPeripheralAsync();
        Assert.StartsWith("Saved.", window.PeripheralStatusText.Text);
        Assert.Equal("Monitor", Assert.Single(PeripheralMetersConfig.Load(ConfigPath(t)).Meters).Label);
        Assert.True(window.PeripheralsCard.IsVisible);
        var box = Assert.Single(window.PeripheralFilterPanel.Children.OfType<CheckBox>());
        Assert.Equal("Monitor", box.Content);
        Assert.True(box.IsChecked);   // a new meter counts until it is switched off
        Assert.Equal("", window.PeripheralLabelBox.Text);
    }

    [AvaloniaFact]
    public async Task ABadNameOrAddressOrADuplicateIsRefusedBeforeAnythingIsAsked()
    {
        UseInvariantCulture();
        using var t = new TempData();
        new WallMeterConfig("192.168.1.50").Save(Path.Combine(t.HistoryDir, WallMeterConfig.FileName));
        new PeripheralMetersConfig(new[] { new PeripheralMeter("Monitor", "192.168.1.51") }).Save(ConfigPath(t));
        var asked = false;
        t.Services.ReadWallMeterAsync = _ => { asked = true; return Task.FromResult(0.0); };
        var window = new MainWindow(t.Services);
        window.Show();

        async Task<string?> Try(string label, string host)
        {
            window.PeripheralLabelBox.Text = label;
            window.PeripheralHostBox.Text = host;
            await window.AddPeripheralAsync();
            return window.PeripheralStatusText.Text;
        }

        Assert.Contains("Give the meter a name", await Try("", "192.168.1.52"));
        Assert.Contains("without http://", await Try("Speakers", "http://192.168.1.52/x"));
        Assert.Contains("already a meter called", await Try("monitor", "192.168.1.52"));
        Assert.Contains("already set up at that address", await Try("Speakers", "192.168.1.51"));
        Assert.Contains("wall meter for the whole PC", await Try("Speakers", "192.168.1.50"));
        Assert.False(asked);
        Assert.Single(PeripheralMetersConfig.Load(ConfigPath(t)).Meters);
    }

    [AvaloniaFact]
    public void RemovingAMeterForgetsItButKeepsItsHistory()
    {
        UseInvariantCulture();
        using var t = new TempData();
        new PeripheralMetersConfig(new[] { new PeripheralMeter("Monitor", "192.168.1.51"), new PeripheralMeter("Speakers", "192.168.1.52") }).Save(ConfigPath(t));
        var window = new MainWindow(t.Services);
        window.Show();
        Assert.Equal(2, window.PeripheralFilterPanel.Children.Count);

        var first = window.PeripheralSettingsList.Children.OfType<Grid>().First().Children.OfType<Button>().Single();
        first.RaiseEvent(new Avalonia.Interactivity.RoutedEventArgs(Button.ClickEvent));

        Assert.Equal("Speakers", Assert.Single(PeripheralMetersConfig.Load(ConfigPath(t)).Meters).Label);
        Assert.Equal("Speakers", Assert.Single(window.PeripheralFilterPanel.Children.OfType<CheckBox>()).Content);
        Assert.Contains("stays in the history", window.PeripheralStatusText.Text);
    }

    [AvaloniaFact]
    public async Task RightNowCountsOnlyTheMetersThatAreTicked()
    {
        UseInvariantCulture();
        using var t = new TempData();
        t.Services.QueryCollectorService = () => ServiceState.Running;
        new PeripheralMetersConfig(new[] { new PeripheralMeter("Monitor", "192.168.1.51"), new PeripheralMeter("Speakers", "192.168.1.52") }).Save(ConfigPath(t));
        Live(t);
        var window = new MainWindow(t.Services);
        window.Show();
        await window.RefreshCollectorAsync(includeService: true);

        // Both are counted to begin with: the PC's 200 W at the wall plus 30 W and 10 W.
        Assert.Equal("240.0 W", window.NowWattsText.Text);
        Assert.Contains("+ peripherals", window.NowLabelText.Text);
        Assert.Contains("Monitor 30.0 W", window.NowCaptionText.Text);
        Assert.Equal("Whole PC at the wall: 200.0 W", window.WallPowerText.Text);   // the PC's own line is never changed by them

        // They are not lines among the PC's parts.
        var rows = Assert.IsAssignableFrom<IEnumerable<ReadingRow>>(window.ReadingsList.ItemsSource).ToList();
        Assert.DoesNotContain(rows, r => r.Name is "Monitor" or "Speakers");
        Assert.Equal("Total active: 65.0 W", window.PowerTotalText.Text);

        // Untick the speakers.
        window.PeripheralFilterPanel.Children.OfType<CheckBox>().Single(b => (string?)b.Content == "Speakers").IsChecked = false;
        await window.RefreshCollectorAsync(includeService: false);
        Assert.Equal("230.0 W", window.NowWattsText.Text);

        // Untick the monitor too: the card is the PC alone again.
        window.PeripheralFilterPanel.Children.OfType<CheckBox>().Single(b => (string?)b.Content == "Monitor").IsChecked = false;
        await window.RefreshCollectorAsync(includeService: false);
        Assert.Equal("200.0 W", window.NowWattsText.Text);
        Assert.DoesNotContain("peripherals", window.NowLabelText.Text);

        // The choice is remembered by a fresh window.
        var again = new MainWindow(t.Services);
        Assert.All(again.PeripheralFilterPanel.Children.OfType<CheckBox>(), b => Assert.False(b.IsChecked));
    }

    [AvaloniaFact]
    public async Task TheHeadlineStatsAddTheTickedMetersAndSaySo()
    {
        UseInvariantCulture();
        using var t = new TempData();
        t.Services.QueryCollectorService = () => ServiceState.Running;
        new PeripheralMetersConfig(new[] { new PeripheralMeter("Monitor", "192.168.1.51") }).Save(ConfigPath(t));
        Live(t);
        using (var store = new HistoryStore(Path.Combine(t.HistoryDir, HistoryStore.FileName)))
        {
            var minute = DateTimeOffset.UtcNow.AddMinutes(-30).ToUnixTimeSeconds() / 60 * 60;
            store.AddMinutes(new[]
            {
                new MinuteRow(minute, WallMeterSource.ReadingName, ReadingScope.Wall, 0.100, 60, 300),
                new MinuteRow(minute, "Monitor", ReadingScope.Peripheral, 0.020, 60, 40),
            });
        }

        var window = new MainWindow(t.Services);
        window.Show();
        await window.RefreshCollectorAsync(includeService: true);
        Assert.Equal("0.12 kWh", window.StatEnergyText.Text);
        Assert.Contains("plus 0.02 kWh from 1 peripheral meter", window.StatEnergyCaption.Text);
        Assert.Contains("+ peripherals", window.StatMaxLabel.Text);
        Assert.StartsWith("≤", window.StatMaxText.Text);   // the PC's peak plus the monitor's own: only an upper bound
        var row = Assert.Single(Assert.IsAssignableFrom<IEnumerable<HistoryRow>>(window.PeripheralsList.ItemsSource));
        Assert.Equal("Monitor", row.Name);
        Assert.Contains("now 30.0 W", row.Detail);

        window.PeripheralFilterPanel.Children.OfType<CheckBox>().Single().IsChecked = false;
        await window.RefreshCollectorAsync(includeService: false);
        Assert.Equal("0.1 kWh", window.StatEnergyText.Text);
        Assert.DoesNotContain("peripheral", window.StatEnergyCaption.Text);
        Assert.Equal("300.0 W", window.StatMaxText.Text);
    }
}
