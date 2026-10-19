using System.Globalization;
using KVGuzzler.Core.Carbon;
using KVGuzzler.Core.History;

namespace KVGuzzler.Views;

// Settings > Carbon footprint: the Electricity Maps key and place. Saved in the machine-wide data folder, where the collector service
// reads it too and picks up a change within about half a minute. The Dashboard shows the carbon of the energy it totals (the Cost card).
public partial class MainWindow
{
    private const string ElectricityMapsFreeTierUrl = "https://electricitymaps.com/free-tier";

    private string CarbonConfigPath => Path.Combine(_services.HistoryDirectory, CarbonConfig.FileName);

    private void StartCarbonUi()
    {
        ShowSavedCarbon();
        TestCarbonButton.Click += async (_, _) => await TestCarbonAsync();
        SaveCarbonButton.Click += async (_, _) => await SaveCarbonAsync();
        RemoveCarbonButton.Click += (_, _) => RemoveCarbon();
        OpenElectricityMapsButton.Click += async (_, _) => await OpenLinkAsync(ElectricityMapsFreeTierUrl);
    }

    /// <summary>The key is never shown again once saved: the box says it is kept, and leaving it empty keeps it.</summary>
    private void ShowSavedCarbon()
    {
        var saved = CarbonConfig.Load(CarbonConfigPath);
        CarbonKeyBox.Text = "";
        CarbonKeyBox.PlaceholderText = saved is null ? "API key" : "Key saved. Paste a new one to change it.";
        CarbonPlaceBox.Text = saved?.Place ?? "";
    }

    /// <summary>What is typed, with the saved key standing in when the key box is left empty; null (with the reason in the status line)
    /// when it cannot be used.</summary>
    private CarbonConfig? TypedCarbon()
    {
        var typedKey = CarbonKeyBox.Text?.Trim();
        var key = string.IsNullOrEmpty(typedKey) ? CarbonConfig.Load(CarbonConfigPath)?.ApiKey : typedKey;
        var place = CarbonPlaceBox.Text?.Trim();
        if (CarbonConfig.Problem(key, place) is { } problem)
        {
            CarbonStatusText.Text = problem;
            return null;
        }
        return new CarbonConfig(key!, place!);
    }

    internal async Task TestCarbonAsync()
    {
        if (TypedCarbon() is not { } config) return;
        CarbonStatusText.Text = "Asking Electricity Maps...";
        try
        {
            var reading = await _services.ReadCarbonAsync(config);
            CarbonStatusText.Text = $"Reached Electricity Maps. {config.Place} is at {Grams(reading.GramsPerKwh)} right now"
                                    + (reading.IsEstimated ? " (a modelled figure, not a measurement)." : ".");
        }
        catch (CarbonException e)
        {
            CarbonStatusText.Text = e.Message;
        }
        catch (Exception e) when (e is not OperationCanceledException)
        {
            // An optional feature must never close the app: say what happened instead.
            CarbonStatusText.Text = "Something unexpected went wrong: " + e.Message;
        }
    }

    /// <summary>Saves only a key and place that got a reading, so a typo is caught here and not as a silently missing figure later.</summary>
    internal async Task SaveCarbonAsync()
    {
        if (TypedCarbon() is not { } config) return;
        CarbonStatusText.Text = "Checking with Electricity Maps before saving...";
        try
        {
            await _services.ReadCarbonAsync(config);
        }
        catch (CarbonException e)
        {
            CarbonStatusText.Text = "Not saved. " + e.Message;
            return;
        }
        catch (Exception e) when (e is not OperationCanceledException)
        {
            CarbonStatusText.Text = "Not saved. Something unexpected went wrong: " + e.Message;
            return;
        }

        try
        {
            config.Save(CarbonConfigPath);
            ShowSavedCarbon();
            CarbonStatusText.Text = "Saved. The background collector starts saving the grid's carbon intensity within a minute; "
                                    + "carbon figures appear on the Dashboard once it has.";
        }
        catch (Exception e) when (e is IOException or UnauthorizedAccessException)
        {
            CarbonStatusText.Text = "Could not save: " + e.Message;
        }
    }

    private void RemoveCarbon()
    {
        try
        {
            CarbonConfig.Remove(CarbonConfigPath);
            ShowSavedCarbon();
            CarbonStatusText.Text = "Removed. The key is gone and nothing more is fetched; the hours already saved are kept and still used.";
        }
        catch (Exception e) when (e is IOException or UnauthorizedAccessException)
        {
            CarbonStatusText.Text = "Could not remove it: " + e.Message;
        }
    }

    /// <summary>What the collector found out the last time it asked: the latest figure, or why it could not get one.</summary>
    private void ShowCarbonFeed(CarbonStatus? status)
    {
        var configured = CarbonConfig.Load(CarbonConfigPath) is not null;
        CarbonFeedText.IsVisible = configured || status is not null;
        CarbonFeedText.Text = status is null
            ? configured ? "The background collector has not asked Electricity Maps yet. It needs background recording turned on." : ""
            : status.Ok
                ? $"Last reading {Grams(status.GramsPerKwh ?? 0)} for {status.ReadingAt?.ToLocalTime().ToString("g", CultureInfo.CurrentCulture)}, "
                  + $"checked {status.CheckedAt.ToLocalTime().ToString("t", CultureInfo.CurrentCulture)}."
                : $"The collector could not get a reading at {status.CheckedAt.ToLocalTime().ToString("t", CultureInfo.CurrentCulture)}: {status.Error}";
    }

    /// <summary>The carbon line under the Cost card: kilograms, the average intensity behind it, and how much was only an estimate.</summary>
    private void ShowCarbon(CarbonResult? carbon)
    {
        StatCarbonCaption.IsVisible = carbon is not null;
        if (carbon is null) return;

        var text = $"≈ {Mass(carbon.Kg)} CO2 · grid average {Grams(carbon.AverageGramsPerKwh)}";
        if (carbon.EstimatedShare >= 0.1)
            text += $" · {UiColors.Percent(carbon.EstimatedShare)} of the energy had no hourly figure, so it uses the average of the hours that did";
        StatCarbonCaption.Text = text;
    }

    internal static string Grams(double gramsPerKwh) => gramsPerKwh.ToString("0", CultureInfo.CurrentCulture) + " g/kWh";

    /// <summary>"12.3 kg", or "845 g" below a kilogram.</summary>
    internal static string Mass(double kg) =>
        Math.Abs(kg) < 1 ? (kg * 1000).ToString("0", CultureInfo.CurrentCulture) + " g" : kg.ToString("0.0#", CultureInfo.CurrentCulture) + " kg";
}
