using System.Globalization;
using Avalonia;
using Avalonia.Controls;
using Avalonia.Controls.ApplicationLifetimes;
using Avalonia.Platform.Storage;
using Kvg.Standards;
using KVGuzzler.Core;
using KVGuzzler.Data;
using KVGuzzler.Services;
using VisualAssault;

namespace KVGuzzler.Views;

public partial class MainWindow : Window
{
    private readonly AppServices _services;
    private bool _restartRequired;

    // Avalonia's XAML runtime loader and previewer need a public parameterless constructor. The app
    // always creates the window with its services; this one is never called at runtime.
    public MainWindow() => throw new NotSupportedException("Create MainWindow with its AppServices.");

    public MainWindow(AppServices services)
    {
        _services = services;
        InitializeComponent();

        CurrencyBox.Text = _services.Database?.GetSetting(SettingKeys.Currency) ?? "$";
        RateBox.Text = _services.Database?.GetSetting(SettingKeys.RatePerKwh) ?? "";
        VersionText.Text = "Version " + _services.Updates.VersionText;

        WattsBox.TextChanged += (_, _) => RefreshEstimate();
        // Editing makes the on-screen "Saved." stale: the new value is not saved until Save is pressed.
        RateBox.TextChanged += (_, _) => { RateStatusText.Text = ""; RefreshRateHints(); };
        CurrencyBox.TextChanged += (_, _) => { RateStatusText.Text = ""; RefreshRateHints(); };
        SaveRateButton.Click += (_, _) => SaveRate();

        ChooseExistingButton.Click += async (_, _) => await ChooseExistingAsync();
        ChooseNewButton.Click += async (_, _) => await ChooseNewAsync();
        ResetDbButton.Click += (_, _) => RelocateTo(null);
        RestartButton.Click += (_, _) => Restart();

        RepoButton.Click += async (_, _) => await OpenLinkAsync(AppServices.RepoUrl);
        IssuesButton.Click += async (_, _) => await OpenLinkAsync(AppServices.RepoUrl + "/issues");
        CheckUpdatesButton.Click += async (_, _) => await CheckForUpdatesAsync();
        ApplyUpdateButton.Click += async (_, _) => await ApplyUpdateAsync();

        SetUpThemePicker();

        RefreshRateHints();
        RefreshEstimate();
        RefreshDatabaseInfo();
    }

    // ----- Appearance -------------------------------------------------------------------

    private void SetUpThemePicker()
    {
        ThemeBox.ItemsSource = VisualAssaultThemes.All.Select(t => t.Name).ToList();
        ThemeBox.SelectedIndex = IndexOfTheme(ThemeManager.Current?.Id ?? ThemeChoice.DefaultId);
        ThemeDescriptionText.Text = ThemeManager.Current?.Description;
        ThemeBox.SelectionChanged += (_, _) =>
        {
            if (ThemeBox.SelectedIndex < 0) return;
            var theme = VisualAssaultThemes.All[ThemeBox.SelectedIndex];
            ThemeManager.Apply(theme);
            ThemeDescriptionText.Text = theme.Description;
            ThemeChoice.Save(_services.Database, theme.Id);
        };
    }

    private static int IndexOfTheme(string id)
    {
        for (var i = 0; i < VisualAssaultThemes.All.Count; i++)
            if (VisualAssaultThemes.All[i].Id == id) return i;
        return 0;
    }

    // ----- Rate and estimate ------------------------------------------------------------

    private string Currency => string.IsNullOrWhiteSpace(CurrencyBox.Text) ? "$" : CurrencyBox.Text.Trim();

    private static string FormatMoney(decimal value) =>
        value >= 1 ? value.ToString("N2", CultureInfo.CurrentCulture) : value.ToString("0.00##", CultureInfo.CurrentCulture);

    private void RefreshRateHints()
    {
        if (!Rate.TryParse(RateBox.Text, out var rate))
        {
            RateExampleText.Text = "Enter your price per kWh, for example 0.15.";
            RateWarningText.IsVisible = false;
            return;
        }

        var cost = CostCalculator.CostOfConstantLoad(400, TimeSpan.FromHours(1), rate);
        RateExampleText.Text = $"Check: a 400 W load for 1 hour = 0.4 kWh = {Currency}{FormatMoney(cost)} at this rate.";
        RateWarningText.IsVisible = rate > Rate.SuspiciouslyHigh;
        RateWarningText.Text = "That is more than 1.00 per kWh. If your bill says cents, enter 0.15, not 15.";
    }

    private void SaveRate()
    {
        if (_restartRequired || _services.Database is null)
        {
            RateStatusText.Text = "Restart KVGuzzler first: the database location was changed.";
            return;
        }
        if (!Rate.TryParse(RateBox.Text, out var rate))
        {
            RateStatusText.Text = "That is not a valid price. Enter a number such as 0.15.";
            return;
        }

        _services.Database.SetSetting(SettingKeys.RatePerKwh, rate.ToString(CultureInfo.InvariantCulture));
        _services.Database.SetSetting(SettingKeys.Currency, Currency);
        RateStatusText.Text = "Saved.";
        RefreshEstimate();
    }

    private void RefreshEstimate()
    {
        var saved = _services.Database?.GetSetting(SettingKeys.RatePerKwh);
        if (!double.TryParse(WattsBox.Text, NumberStyles.AllowDecimalPoint, CultureInfo.CurrentCulture, out var watts)
            || watts < 0)
        {
            EstimateText.Text = "Enter a number of watts, for example 400.";
            return;
        }
        if (!decimal.TryParse(saved, NumberStyles.AllowDecimalPoint, CultureInfo.InvariantCulture, out var rate))
        {
            EstimateText.Text = $"{watts:0.#} W uses {CostCalculator.KilowattHours(watts, TimeSpan.FromHours(1)):0.###} kWh per hour. " +
                                "Save your electricity rate in Settings to see the cost.";
            return;
        }

        var currency = _services.Database?.GetSetting(SettingKeys.Currency) ?? "$";
        string Line(string label, TimeSpan span) =>
            $"{label}: {currency}{FormatMoney(CostCalculator.CostOfConstantLoad(watts, span, rate))}";
        EstimateText.Text = string.Join(Environment.NewLine,
            Line("Per hour", TimeSpan.FromHours(1)),
            Line("Per day", TimeSpan.FromDays(1)),
            Line("Per 30 days", TimeSpan.FromDays(30)),
            Line("Per year", TimeSpan.FromDays(365)));
    }

    // ----- Database location ------------------------------------------------------------

    private void RefreshDatabaseInfo()
    {
        DbPathText.Text = "Current file: " + _services.DbLocation.GetEffectiveDbPath()
                          + (_services.DbLocation.IsUsingDefaultLocation() ? "  (default location)" : "");
        var size = _services.DbLocation.GetDbFileSizeBytes();
        DbSizeText.Text = "Size: " + (size is null ? "not created yet" : FileSize.Format(size.Value));
    }

    private static readonly FilePickerFileType DatabaseFiles = new("SQLite database") { Patterns = new[] { "*.db" } };

    private async Task ChooseExistingAsync()
    {
        var picked = await StorageProvider.OpenFilePickerAsync(new FilePickerOpenOptions
        {
            Title = "Choose an existing KVGuzzler database",
            AllowMultiple = false,
            FileTypeFilter = new[] { DatabaseFiles },
        });
        var path = picked.FirstOrDefault()?.TryGetLocalPath();
        if (path is not null) RelocateTo(path);
    }

    private async Task ChooseNewAsync()
    {
        var picked = await StorageProvider.SaveFilePickerAsync(new FilePickerSaveOptions
        {
            Title = "Choose where to keep the database (it is copied there)",
            SuggestedFileName = "kvguzzler.db",
            DefaultExtension = "db",
            FileTypeChoices = new[] { DatabaseFiles },
        });
        var path = picked?.TryGetLocalPath();
        if (path is not null) RelocateTo(path);
    }

    /// <summary>Moves the database (or resets to the default when <paramref name="path"/> is null), then
    /// asks for a restart: an open connection cannot be re-pointed.</summary>
    internal void RelocateTo(string? path)
    {
        try
        {
            // Close everything first, or the copy can miss writes still sitting in the -wal file.
            _services.CloseDatabase();
            if (path is null) _services.DbLocation.ResetToDefaultDbPath();
            else _services.DbLocation.SetDbPath(path);

            _restartRequired = true;
            foreach (var b in new[] { ChooseExistingButton, ChooseNewButton, ResetDbButton, SaveRateButton })
                b.IsEnabled = false;
            RestartButton.IsVisible = true;
            DbStatusText.Text = "Location changed. Restart KVGuzzler to use it.";
            RefreshDatabaseInfo();
        }
        catch (Exception e)
        {
            DbStatusText.Text = "Could not change the location: " + e.Message;
            try { _services.OpenDatabase(); } catch { /* the message above already tells the user */ }
        }
    }

    private void Restart()
    {
        _services.StartReplacementProcess();
        (Application.Current?.ApplicationLifetime as IClassicDesktopStyleApplicationLifetime)?.Shutdown();
    }

    // ----- Help and updates -------------------------------------------------------------

    private async Task OpenLinkAsync(string url)
    {
        var launcher = TopLevel.GetTopLevel(this)?.Launcher;
        if (launcher is not null) await launcher.LaunchUriAsync(new Uri(url));
    }

    public async Task CheckForUpdatesAsync()
    {
        UpdateStatusText.Text = "Checking...";
        UpdateStatusText.Text = await _services.Updates.CheckAsync();
        ApplyUpdateButton.IsVisible = _services.Updates.UpdatePending;
    }

    private async Task ApplyUpdateAsync()
    {
        ApplyUpdateButton.IsEnabled = false;
        UpdateStatusText.Text = "Downloading the update...";
        try
        {
            await _services.Updates.DownloadAndRestartAsync(percent => Avalonia.Threading.Dispatcher.UIThread.Post(
                () => UpdateStatusText.Text = $"Downloading the update... {percent}%"));
        }
        catch (Exception e)
        {
            UpdateStatusText.Text = "The update failed: " + e.Message;
            ApplyUpdateButton.IsEnabled = true;
        }
    }
}
