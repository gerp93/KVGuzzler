using System.Globalization;
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
        SaveWallButton.Click += async (_, _) => await SaveWallMeterAsync();
        RemoveWallButton.Click += (_, _) => RemoveWallMeter();
    }

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
        }
        catch (Exception e) when (e is IOException or UnauthorizedAccessException)
        {
            WallStatusText.Text = "Could not remove it: " + e.Message;
        }
    }
}
