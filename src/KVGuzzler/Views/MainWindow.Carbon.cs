using System.Globalization;
using KVGuzzler.Core.Carbon;
using KVGuzzler.Core.History;

namespace KVGuzzler.Views;

// Settings > Carbon footprint: which source (Electricity Maps or EIA), the key and the place. Saved in the machine-wide data folder, where
// the collector service reads it too and picks up a change within about half a minute. The Dashboard shows the carbon of the energy it totals
// (the Cost card).
public partial class MainWindow
{
    private const string ElectricityMapsFreeTierUrl = "https://electricitymaps.com/free-tier";
    private const string EiaRegisterUrl = "https://www.eia.gov/opendata/register.php";

    private string CarbonConfigPath => Path.Combine(_services.HistoryDirectory, CarbonConfig.FileName);

    /// <summary>The source chosen in the box.</summary>
    private string SelectedCarbonSource => CarbonSourceBox.SelectedIndex == 1 ? CarbonSources.Eia : CarbonSources.ElectricityMaps;

    private void StartCarbonUi()
    {
        CarbonSourceBox.SelectedIndex = CarbonConfig.Load(CarbonConfigPath)?.Source == CarbonSources.Eia ? 1 : 0;
        ShowSavedCarbon();
        CarbonSourceBox.SelectionChanged += (_, _) =>
        {
            ShowSavedCarbon();
            CarbonStatusText.Text = "";
        };
        TestCarbonButton.Click += async (_, _) => await TestCarbonAsync();
        SaveCarbonButton.Click += async (_, _) => await SaveCarbonAsync();
        RemoveCarbonButton.Click += (_, _) => RemoveCarbon();
        OpenElectricityMapsButton.Click += async (_, _) =>
            await OpenLinkAsync(SelectedCarbonSource == CarbonSources.Eia ? EiaRegisterUrl : ElectricityMapsFreeTierUrl);
    }

    /// <summary>Only a setup saved for the source in the box is shown: the key is never shown again once saved, the box says it is kept, and
    /// leaving it empty keeps it.</summary>
    private void ShowSavedCarbon()
    {
        var eia = SelectedCarbonSource == CarbonSources.Eia;
        var saved = CarbonConfig.Load(CarbonConfigPath) is { } c && c.Source == SelectedCarbonSource ? c : null;
        CarbonKeyBox.Text = "";
        CarbonKeyBox.PlaceholderText = saved is null ? "API key" : "Key saved. Paste a new one to change it.";
        CarbonPlaceBox.Text = saved?.Place ?? "";
        CarbonPlaceBox.PlaceholderText = eia ? "MISO" : "US-MIDW-MISO or 44.98, -93.27";
        OpenElectricityMapsButton.Content = "Get a free key...";
        CarbonNoteText.Text = eia
            ? "Uses the US Energy Information Administration's hourly generation by fuel for your balancing authority (MISO for Minnesota; also PJM, "
              + "ERCO for Texas, CISO for California, PSCO for Colorado and others) and works the carbon out from the fuel mix. Free: register at the EIA "
              + "site for a key. It covers the US only, runs an hour or more behind, and leaves out power imported from other grids. Only the key and the "
              + "authority's code are sent, and only to EIA. The background collector fetches it every half hour, so this needs background recording. "
              + "The key is saved as plain text in the collector's data folder."
            : "Shows the estimated CO2 of the electricity used, from how clean your grid is each hour. The background collector asks Electricity Maps for "
              + "the grid's carbon intensity every half hour and saves it, so this needs background recording. Get a free personal key from the Electricity "
              + "Maps API portal (it covers one zone: yours), paste it here once, and enter your zone code (like US-MIDW-MISO for Minnesota) or your "
              + "latitude and longitude (like 44.98, -93.27). Only the key and that place are sent, and only to Electricity Maps. The key is saved as "
              + "plain text in the collector's data folder.";
    }

    /// <summary>What is typed, with the saved key standing in when the key box is left empty (and the saved setup is for this source); null (with
    /// the reason in the status line) when it cannot be used.</summary>
    private CarbonConfig? TypedCarbon()
    {
        var source = SelectedCarbonSource;
        var typedKey = CarbonKeyBox.Text?.Trim();
        var saved = CarbonConfig.Load(CarbonConfigPath) is { } c && c.Source == source ? c : null;
        var key = string.IsNullOrEmpty(typedKey) ? saved?.ApiKey : typedKey;
        var place = CarbonPlaceBox.Text is { } p ? CarbonConfig.Normalised(p, source) : null;
        if (CarbonConfig.Problem(key, place, source) is { } problem)
        {
            CarbonStatusText.Text = problem;
            return null;
        }
        return new CarbonConfig(key!, place!) { Source = source };
    }

    internal async Task TestCarbonAsync()
    {
        if (TypedCarbon() is not { } config) return;
        CarbonStatusText.Text = $"Asking {CarbonSources.Name(config.Source)}...";
        try
        {
            var readings = await _services.ReadCarbonAsync(config);
            var latest = readings[^1];
            CarbonStatusText.Text = config.Source == CarbonSources.Eia
                ? $"Reached EIA. {config.Place} is at {Grams(latest.GramsPerKwh)} for {latest.At.ToLocalTime().ToString("g", CultureInfo.CurrentCulture)} "
                  + $"(the latest hour EIA has, worked out from the fuel mix; {readings.Count} hours found)."
                : $"Reached Electricity Maps. {config.Place} is at {Grams(latest.GramsPerKwh)} right now"
                  + (latest.IsEstimated ? " (a modelled figure, not a measurement)." : ".");
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
        CarbonStatusText.Text = $"Checking with {CarbonSources.Name(config.Source)} before saving...";
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
            ? configured ? "The background collector has not asked for the grid's carbon intensity yet. It needs background recording turned on." : ""
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
