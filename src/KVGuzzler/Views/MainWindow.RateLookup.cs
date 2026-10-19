using System.Globalization;
using System.Text.RegularExpressions;
using Avalonia.Controls;
using KVGuzzler.Core.Billing;
using KVGuzzler.Core.History;
using KVGuzzler.Core.Power;
using KVGuzzler.Data;

namespace KVGuzzler.Views;

// Settings > Electricity rate > "Looked up (OpenEI)": the price comes from a utility's published tariff, found by ZIP code or address in the
// OpenEI Utility Rate Database. The tariff is saved in the settings database; how the power is spread through the day (to weight a
// time-of-use tariff into one price per day) is learned from the saved history in the background.
public partial class MainWindow
{
    private const string OpenEiSignupUrl = "https://openei.org/services/api/signup/";

    /// <summary>A saved tariff older than this is fetched again when the window opens (utilities change their rates).</summary>
    internal static readonly TimeSpan TariffMaxAge = TimeSpan.FromDays(7);

    /// <summary>How often the hours-of-use profile is worked out again.</summary>
    private static readonly TimeSpan ProfileEvery = TimeSpan.FromHours(1);

    private IReadOnlyList<TariffCandidate> _foundRates = Array.Empty<TariffCandidate>();

    private static readonly Regex OpenEiKeyPattern = new(@"^[A-Za-z0-9_\-]{16,100}$", RegexOptions.Compiled);

    private void StartRateLookupUi()
    {
        ShowSavedOpenEiKey();
        ShowCurrentTariff();
        FindRatesButton.Click += async (_, _) => await FindRatesAsync();
        UseFoundRateButton.Click += (_, _) => UseFoundRate();
        RefreshTariffButton.Click += async (_, _) => await RefreshTariffAsync(silent: false);
        OpenEiKeyLinkButton.Click += async (_, _) => await OpenLinkAsync(OpenEiSignupUrl);
        RateResultsBox.SelectionChanged += (_, _) => ShowFoundDetail();
        // Tariffs change: a week-old one is fetched again in the background, and the old one stays if that fails.
        Opened += async (_, _) =>
        {
            if (Tariff() is { } t && DateTimeOffset.UtcNow - t.FetchedAt > TariffMaxAge) await RefreshTariffAsync(silent: true);
        };
    }

    private Tariff? Tariff() => Core.Billing.Tariff.FromJson(_services.Database?.GetSetting(SettingKeys.RateLookupTariff));

    private string? SavedOpenEiKey() => _services.Database?.GetSetting(SettingKeys.OpenEiKey) is { Length: > 0 } k ? k : null;

    /// <summary>The key is never shown again once saved: the box says it is kept, and leaving it empty keeps it.</summary>
    private void ShowSavedOpenEiKey()
    {
        OpenEiKeyBox.Text = "";
        OpenEiKeyBox.PlaceholderText = SavedOpenEiKey() is null ? "OpenEI API key" : "Key saved. Paste a new one to change it.";
    }

    /// <summary>The key typed, or the saved one when the box is empty; null (with the reason in the status line) when there is none usable.</summary>
    private string? TypedOpenEiKey()
    {
        var typed = OpenEiKeyBox.Text?.Trim();
        var key = string.IsNullOrEmpty(typed) ? SavedOpenEiKey() : typed;
        if (string.IsNullOrEmpty(key)) { LookupStatusText.Text = "Paste your OpenEI API key first (the button beside it gets a free one)."; return null; }
        if (!OpenEiKeyPattern.IsMatch(key)) { LookupStatusText.Text = "That does not look like an API key. Copy it whole, with no spaces."; return null; }
        return key;
    }

    // ----- Finding and choosing ---------------------------------------------------------

    internal async Task FindRatesAsync()
    {
        if (TypedOpenEiKey() is not { } key) return;
        var address = RateAddressBox.Text?.Trim() ?? "";
        if (address.Length < 3 || address.Length > 120 || address.Any(char.IsControl))
        {
            LookupStatusText.Text = "Enter your ZIP code or address, for example 80202.";
            return;
        }

        LookupStatusText.Text = "Asking OpenEI (it can take a little while)...";
        try
        {
            var found = await _services.RateLookup.FindAsync(key, address, DateTimeOffset.UtcNow);
            _services.Database?.SetSetting(SettingKeys.OpenEiKey, key);   // it worked, so it is a good key
            ShowSavedOpenEiKey();
            _foundRates = found;
            RateResultsBox.ItemsSource = found.Select(c => c.Tariff.Summary(Currency) + (c.IsDefault ? " (most common)" : "")).ToList();
            RateResultsBox.IsVisible = UseFoundRateButton.IsVisible = found.Count > 0;
            if (found.Count > 0) RateResultsBox.SelectedIndex = 0;
            ShowFoundDetail();
            LookupStatusText.Text = found.Count == 0
                ? "OpenEI had no usable residential rate for that place. Try a ZIP code, or use My bills."
                : $"Found {found.Count} residential rate{(found.Count == 1 ? "" : "s")}. Pick yours (the name is on your bill) and use it.";
        }
        catch (RateLookupException e)
        {
            LookupStatusText.Text = e.Message;
        }
        catch (Exception e) when (e is not OperationCanceledException)
        {
            // An optional feature must never close the app: say what happened instead.
            LookupStatusText.Text = "Something unexpected went wrong: " + e.Message;
        }
    }

    private TariffCandidate? SelectedFound =>
        RateResultsBox.SelectedIndex is >= 0 and var i && i < _foundRates.Count ? _foundRates[i] : null;

    private void ShowFoundDetail() =>
        RateResultDetailText.Text = SelectedFound is { } c ? Describe(c.Tariff) + (c.Approved ? " Checked by an OpenEI expert." : "") : "";

    /// <summary>What a tariff is, including the things KVGuzzler cannot do with it.</summary>
    internal string Describe(Tariff t)
    {
        var lines = new List<string>();
        if (t.VariesByTime) lines.Add("The price changes with the hour and the season: KVGuzzler weights it by when you use power.");
        if (t.Tiers > 1) lines.Add($"It has {t.Tiers} usage tiers. Which tier applies depends on your whole house's monthly usage, so the first tier's price is used; "
                                   + "your bills (My bills) are exact if this is a poor match.");
        if (t.FixedCharge is { } fixedCharge) lines.Add($"Its fixed charge ({fixedCharge}) is not part of a price per kWh and is left out.");
        return string.Join(" ", lines);
    }

    private void UseFoundRate()
    {
        if (SelectedFound is not { } candidate || _services.Database is null) return;
        _services.Database.SetSetting(SettingKeys.RateLookupTariff, candidate.Tariff.ToJson());
        _services.Database.SetSetting(SettingKeys.RateMode, "lookup");
        _services.Database.SetSetting(SettingKeys.Currency, Currency);
        ShowCurrentTariff();
        LookupStatusText.Text = "Saved. Costs on the Dashboard now use this rate.";
        RefreshEstimate();
        _ = RefreshCollectorAsync(includeService: false, includeChart: true, showBusy: true);
    }

    /// <summary>The rate in use, and the price it works out to today at the hours the power is used.</summary>
    internal void ShowCurrentTariff()
    {
        var tariff = Tariff();
        RefreshTariffButton.IsVisible = tariff is not null;
        if (tariff is null)
        {
            LookupCurrentText.Text = "No rate chosen yet. Until one is, prices fall back on the single rate.";
            return;
        }

        var today = _services.Database?.LoadRateSchedule().RateAt(DateOnly.FromDateTime(DateTime.Now));
        LookupCurrentText.Text = $"Using: {tariff.Summary(Currency)}."
            + (today is { } price ? $" Today that works out to {Currency}{price.ToString("0.0000", CultureInfo.CurrentCulture)} per kWh at the hours you use power." : "")
            + $" Fetched {tariff.FetchedAt.ToLocalTime().ToString("d", CultureInfo.CurrentCulture)}. " + Describe(tariff);
    }

    // ----- Keeping it current -----------------------------------------------------------

    /// <summary>Fetches the saved rate again by its label. The old one is kept when OpenEI cannot be reached or no longer has it.</summary>
    internal async Task RefreshTariffAsync(bool silent)
    {
        if (Tariff() is not { } current || SavedOpenEiKey() is not { } key) return;
        if (!silent) LookupStatusText.Text = "Asking OpenEI for the latest version of this rate...";
        try
        {
            var latest = await _services.RateLookup.GetAsync(key, current.Label, DateTimeOffset.UtcNow);
            if (latest is null)
            {
                if (!silent) LookupStatusText.Text = "OpenEI no longer has this rate. The saved copy is still being used: find your rate again if it changed.";
                return;
            }
            _services.Database?.SetSetting(SettingKeys.RateLookupTariff, latest.Tariff.ToJson());
            ShowCurrentTariff();
            if (!silent) LookupStatusText.Text = "Updated to the latest version of this rate.";
            _ = RefreshCollectorAsync(includeService: false, includeChart: true);
        }
        catch (RateLookupException e)
        {
            if (!silent) LookupStatusText.Text = e.Message + " The saved copy is still being used.";
        }
        catch (Exception e) when (e is not OperationCanceledException)
        {
            if (!silent) LookupStatusText.Text = "Something unexpected went wrong: " + e.Message;
        }
    }

    /// <summary>Works out when the power is used (the share in each hour of a weekday and of a weekend day) from the last two months of saved
    /// history, so a time-of-use tariff can be turned into one price per day. Only while looking up, and at most once an hour. Runs on the
    /// background read.</summary>
    private void RefreshUsageProfile(HistoryStore store, DateTimeOffset now)
    {
        if (_services.Database is not { } db || db.GetSetting(SettingKeys.RateMode) != "lookup") return;
        if (Tariff() is not { VariesByTime: true }) return;   // a flat tariff does not care when
        if (long.TryParse(db.GetSetting(SettingKeys.RateLookupProfileAt), out var ms) && now - DateTimeOffset.FromUnixTimeMilliseconds(ms) < ProfileEvery) return;

        var from = now.AddDays(-60);
        var wholePc = store.EnergyKwh(from, now, ReadingScope.Wall) > 0;
        var hours = store.HourlyKwh(from, now, wholePc ? ReadingScope.Wall : ReadingScope.Component).ToList();
        var meters = store.QueryPeripherals(from, now).Select(u => u.Name).ToList();
        hours.AddRange(store.HourlyKwh(from, now, ReadingScope.Peripheral, meters));
        if (hours.Count == 0) return;   // nothing recorded yet: an even day until there is

        db.SetSetting(SettingKeys.RateLookupProfile, UsageProfile.FromHourly(hours, TimeZoneInfo.Local).ToJson());
        db.SetSetting(SettingKeys.RateLookupProfileAt, now.ToUnixTimeMilliseconds().ToString(CultureInfo.InvariantCulture));
    }
}
