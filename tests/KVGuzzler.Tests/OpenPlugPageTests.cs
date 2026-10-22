using System.Globalization;
using Avalonia.Controls;
using Avalonia.Headless.XUnit;
using Avalonia.Interactivity;
using KVGuzzler.Core.Power;
using KVGuzzler.Views;
using Xunit;

namespace KVGuzzler.Tests;

public class OpenPlugPageTests
{
    private static void UseInvariantCulture() => CultureInfo.CurrentCulture = CultureInfo.InvariantCulture;

    [Fact]
    public void ThePageIsThePlugsPlainAddressAndNothingElse() =>
        Assert.Equal("http://192.168.1.50/", MainWindow.PlugPageUrl("192.168.1.50"));

    [AvaloniaFact]
    public async Task TheWallMeterButtonOpensTheTypedAddressInTheBrowser()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();
        var opened = new List<Uri>();
        window.UriLauncher = uri => { opened.Add(uri); return Task.CompletedTask; };

        window.WallHostBox.Text = " 192.168.1.50 ";
        await window.OpenWallMeterPageAsync();

        Assert.Equal("http://192.168.1.50/", Assert.Single(opened).ToString());
        Assert.StartsWith("Opened http://192.168.1.50/ in your browser", window.WallStatusText.Text);
        Assert.False(File.Exists(Path.Combine(t.HistoryDir, WallMeterConfig.FileName)));   // looking at the page saves nothing
    }

    [AvaloniaFact]
    public async Task NothingIsOpenedForAMissingOrBadAddress_SoNoOtherPageCanBeReached()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();
        var opened = new List<Uri>();
        window.UriLauncher = uri => { opened.Add(uri); return Task.CompletedTask; };

        window.WallHostBox.Text = "";
        await window.OpenWallMeterPageAsync();
        Assert.Contains("Enter the plug's address first", window.WallStatusText.Text);

        foreach (var bad in new[] { "http://192.168.1.50", "192.168.1.50/rpc/Shelly.Reboot", "evil.example/x?y=1", "user:pass@192.168.1.50" })
        {
            window.WallHostBox.Text = bad;
            await window.OpenWallMeterPageAsync();
            Assert.Contains("Enter just the address", window.WallStatusText.Text);
        }
        Assert.Empty(opened);
    }

    [AvaloniaFact]
    public async Task AProblemOpeningTheBrowserIsAMessageNotACrash()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();
        window.UriLauncher = _ => throw new InvalidOperationException("no default browser");
        window.WallHostBox.Text = "192.168.1.50";

        await window.OpenWallMeterPageAsync();

        Assert.Equal("Could not open the browser: no default browser", window.WallStatusText.Text);
    }

    [AvaloniaFact]
    public async Task EachOtherMeterAndTheBoxForANewOneCanBeOpenedToo()
    {
        UseInvariantCulture();
        using var t = new TempData();
        new PeripheralMetersConfig(new[] { new PeripheralMeter("Monitor", "192.168.1.51") }).Save(Path.Combine(t.HistoryDir, PeripheralMetersConfig.FileName));
        var window = new MainWindow(t.Services);
        window.Show();
        var opened = new List<Uri>();
        window.UriLauncher = uri => { opened.Add(uri); return Task.CompletedTask; };

        var row = window.PeripheralSettingsList.Children.OfType<Grid>().Single();
        row.Children.OfType<Button>().Single(b => (string?)b.Content == "Open in browser").RaiseEvent(new RoutedEventArgs(Button.ClickEvent));
        await Task.Delay(50, TestContext.Current.CancellationToken);
        Assert.Equal("http://192.168.1.51/", Assert.Single(opened).ToString());
        Assert.StartsWith("Opened http://192.168.1.51/", window.PeripheralStatusText.Text);

        window.PeripheralHostBox.Text = "";
        await window.OpenTypedPeripheralPageAsync();
        Assert.Contains("Enter just the plug's address first", window.PeripheralStatusText.Text);
        window.PeripheralHostBox.Text = "http://evil.example/";
        await window.OpenTypedPeripheralPageAsync();
        Assert.Single(opened);   // still only the one

        window.PeripheralHostBox.Text = "192.168.1.52";
        await window.OpenTypedPeripheralPageAsync();
        Assert.Equal("http://192.168.1.52/", opened[1].ToString());
    }
}
