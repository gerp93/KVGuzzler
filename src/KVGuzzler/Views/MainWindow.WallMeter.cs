using System.Globalization;
using Avalonia.Controls;
using KVGuzzler.Core.Power;

namespace KVGuzzler.Views;

// Settings > Wall meter: where the smart plug is. Saved in the machine-wide data folder, where the
// collector service finds it too and picks up a change within a few seconds.
public partial class MainWindow
{
    private string WallConfigPath => Path.Combine(_services.HistoryDirectory, WallMeterConfig.FileName);

    private void StartWallMeterSettings()
    {
        WallHostBox.Text = WallMeterConfig.Load(WallConfigPath)?.Host ?? "";
        TestWallButton.Click += async (_, _) => await TestWallMeterAsync();
        OpenWallButton.Click += async (_, _) => await OpenWallMeterPageAsync();
        SaveWallButton.Click += async (_, _) => await SaveWallMeterAsync();
        RemoveWallButton.Click += (_, _) => RemoveWallMeter();
        RefreshSleepCard();
    }

    /// <summary>The sleep wattage only fills the history lines when there is no wall meter (with one, the headline is the plug's
    /// own figure and the estimate is not used), so the setting is only offered without one.</summary>
    private void RefreshSleepCard() => SleepCard.IsVisible = WallMeterConfig.Load(WallConfigPath) is null;

    /// <summary>The address typed in the box, or null (with the reason in the status line) if it is not one.</summary>
    private string? TypedHost()
    {
        var host = WallHostBox.Text?.Trim();
        if (string.IsNullOrEmpty(host))
        {
            WallStatusText.Text = "Enter the plug's address first, for example 192.168.1.50.";
            return null;
        }
        if (!WallMeterConfig.IsValidHost(host))
        {
            WallStatusText.Text = "Enter just the address (like 192.168.1.50 or shelly-pc.lan), without http:// or a path.";
            return null;
        }
        return host;
    }

    /// <summary>The page of a plug: the plain address in a browser, so the plug can be looked at (and set up) on the network. Only ever
    /// <c>http://</c> and the host that passed <see cref="WallMeterConfig.IsValidHost"/>, never a path.</summary>
    internal static string PlugPageUrl(string host) => $"http://{host}/";

    internal async Task OpenWallMeterPageAsync()
    {
        if (TypedHost() is not { } host) return;
        await OpenPlugPageAsync(host, WallStatusText);
    }

    /// <summary>Opens a plug's own page in the system browser, and says so (or why not) in <paramref name="status"/>.</summary>
    private async Task OpenPlugPageAsync(string host, TextBlock status)
    {
        try
        {
            await OpenLinkAsync(PlugPageUrl(host));
            status.Text = $"Opened http://{host}/ in your browser. It only loads from a computer on the same network as the plug.";
        }
        catch (Exception e) when (e is not OperationCanceledException)
        {
            // An optional convenience must never close the app: say what happened instead.
            status.Text = "Could not open the browser: " + e.Message;
        }
    }

    internal async Task TestWallMeterAsync()
    {
        if (TypedHost() is not { } host) return;
        WallStatusText.Text = "Asking the plug...";
        try
        {
            var watts = await _services.ReadWallMeterAsync(host);
            WallStatusText.Text = $"Reached the plug. It reads {watts.ToString("0.0", CultureInfo.CurrentCulture)} W right now.";
        }
        catch (WallMeterException e)
        {
            WallStatusText.Text = e.Message;
        }
        catch (Exception e) when (e is not OperationCanceledException)
        {
            // An optional feature must never close the app: say what happened instead.
            WallStatusText.Text = "Something unexpected went wrong: " + e.Message;
        }
    }

    /// <summary>Saves only an address that answered like a plug, so a typo is caught here and not as a
    /// silently missing wall figure later.</summary>
    internal async Task SaveWallMeterAsync()
    {
        if (TypedHost() is not { } host) return;
        WallStatusText.Text = "Checking the plug before saving...";
        try
        {
            await _services.ReadWallMeterAsync(host);
        }
        catch (WallMeterException e)
        {
            WallStatusText.Text = "Not saved. " + e.Message;
            return;
        }
        catch (Exception e) when (e is not OperationCanceledException)
        {
            WallStatusText.Text = "Not saved. Something unexpected went wrong: " + e.Message;
            return;
        }

        try
        {
            new WallMeterConfig(host).Save(WallConfigPath);
            WallStatusText.Text = "Saved. The whole-PC reading appears on the Dashboard within a few seconds.";
            RefreshSleepCard();
        }
        catch (Exception e) when (e is IOException or UnauthorizedAccessException)
        {
            WallStatusText.Text = "Could not save: " + e.Message;
        }
    }

    private void RemoveWallMeter()
    {
        try
        {
            WallMeterConfig.Remove(WallConfigPath);
            WallHostBox.Text = "";
            WallStatusText.Text = "Removed. KVGuzzler no longer reads a wall meter.";
            RefreshSleepCard();
        }
        catch (Exception e) when (e is IOException or UnauthorizedAccessException)
        {
            WallStatusText.Text = "Could not remove it: " + e.Message;
        }
    }
}
