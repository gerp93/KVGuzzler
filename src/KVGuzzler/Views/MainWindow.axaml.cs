using System.Globalization;
using Avalonia;
using Avalonia.Controls;
using Avalonia.Controls.ApplicationLifetimes;
using Avalonia.Media;
using Avalonia.Platform.Storage;
using Avalonia.Threading;
using Kvg.Standards;
using KVGuzzler.Core;
using KVGuzzler.Core.Power;
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
        StartPowerDisplay();
        StartCollectorDisplay();
        StartWallMeterSettings();
        StartBillsUi();
        StartStatsUi();
    }

    // ----- Live power -------------------------------------------------------------------

    private void StartPowerDisplay()
    {
        if (_services.Power is not { } power)
        {
            PowerStatusText.Text = "Power sensors are not running.";
            return;
        }

        // While the collector service is publishing readings, those are shown instead.
        void OnUpdated(PowerSnapshot s) => Dispatcher.UIThread.Post(() => { if (!_collectorAlive) ShowPower(s); });
        // Subscribe only while the window is open; Start is a no-op after the first call.
        Opened += (_, _) =>
        {
            power.Updated += OnUpdated;
            power.Start();
            if (power.Latest is { } latest) ShowPower(latest);
        };
        Closed += (_, _) => power.Updated -= OnUpdated;
    }

    private static string Watts(double w) => w.ToString("0.0", CultureInfo.CurrentCulture) + " W";

    /// <summary>The colour a component's bar and chart line share: CPU blue, GPU green, anything else muted. The collector's
    /// live readings carry no hardware kind, so it falls back on the name.</summary>
    private static IBrush ComponentBrush(PowerReading r)
    {
        var name = r.Name;
        if (r.Kind == PowerKind.Cpu || name.StartsWith("CPU", StringComparison.OrdinalIgnoreCase)) return UiColors.Cpu;
        if (r.Kind == PowerKind.Gpu || new[] { "GPU", "NVIDIA", "GeForce", "Radeon", "Arc" }.Any(k => name.Contains(k, StringComparison.OrdinalIgnoreCase)))
            return UiColors.Gpu;
        return UiColors.Brush(VaToken.AccentMuted);
    }

    private void ShowPower(PowerSnapshot s, bool showSession = true)
    {
        // The wall meter is the whole PC, so it is shown on its own line below, not in the list of parts.
        var wall = s.Readings.FirstOrDefault(r => r.Scope == ReadingScope.Wall);
        var hasComponents = s.Readings.Any(r => r.Scope == ReadingScope.Component);

        // Every component is shown as a share of the whole PC at the wall. With no wall meter the only whole there is
        // is the sum of the sensors, so the shares are of that, and the header says so.
        var whole = wall is { Watts: > 0 } ? wall.Watts : s.TotalWatts;
        var rest = wall is not null && hasComponents ? wall.Watts - s.TotalWatts : -1;
        double ShareOf(double watts) => whole > 0 ? Math.Clamp(watts / whole, 0, 1) : 0;

        var rows = s.Readings
            .Where(r => r.Scope != ReadingScope.Wall)
            .OrderBy(r => r.Scope)   // components first, their breakdown after
            .Select(r => new ReadingRow(
                r.Name,
                Watts(r.Watts),
                r.Scope == ReadingScope.Breakdown ? new Thickness(20, 0, 0, 0) : new Thickness(0),
                r.Scope == ReadingScope.Breakdown ? 0.7 : 1,
                ShareOf(r.Watts),
                whole > 0 ? UiColors.Percent(ShareOf(r.Watts)) : "",
                r.Scope == ReadingScope.Breakdown ? UiColors.Rest : ComponentBrush(r)))
            .ToList();
        if (rest > 0.05)
            rows.Add(new ReadingRow("Everything else", Watts(rest), new Thickness(0), 1, ShareOf(rest), UiColors.Percent(ShareOf(rest)), UiColors.Rest));
        ReadingsList.ItemsSource = rows;
        ReadingsShareHeader.Text = wall is not null ? "% of PC" : "% of sensors";
        ReadingsHeader.IsVisible = rows.Count > 0;

        var parts = s.Readings.Where(r => r.Scope == ReadingScope.Component)
            .Select(r => new SplitBar.Part(r.Name, r.Watts, ComponentBrush(r))).ToList();
        if (rest > 0.05) parts.Add(new SplitBar.Part("Everything else", rest, UiColors.Rest));
        SplitBar.Fill(LiveSplitBar, LiveSplitLegend, parts);

        PowerStatusText.Text = hasComponents || wall is not null ? "Measured every second." : "No power sensors are reporting on this machine.";
        PowerTotalText.Text = hasComponents ? $"Total active: {Watts(s.TotalWatts)}" : "";

        var session = $"Active energy since KVGuzzler started: {s.SessionKilowattHours.ToString("0.0000", CultureInfo.CurrentCulture)} kWh";
        if (CurrentRate() is { } rate)
        {
            var currency = _services.Database?.GetSetting(SettingKeys.Currency) ?? "$";
            session += $", {currency}{FormatMoney(CostCalculator.Cost(s.SessionKilowattHours, rate))}";
        }
        PowerSessionText.Text = hasComponents ? session : "";

        WallPowerText.IsVisible = wall is not null;
        WallBreakdownText.IsVisible = false;
        if (wall is not null)
        {
            WallPowerText.Text = $"Whole PC at the wall: {Watts(wall.Watts)}";
            // Only say what the rest is when the sums make sense: the plug and the sensors are read a moment apart.
            var other = wall.Watts - s.TotalWatts;
            WallBreakdownText.Text = hasComponents && other >= 0
                ? $"The sensors above account for {Watts(s.TotalWatts)}. The other {Watts(other)} is fans, drives, the motherboard and power-supply losses."
                : "";
            WallBreakdownText.IsVisible = WallBreakdownText.Text.Length > 0;
        }

        // The "Right now" card: the whole PC when there is a wall meter, otherwise the sensors.
        if (wall is not null)
        {
            NowLabelText.Text = "Right now · whole PC at the wall";
            NowWattsText.Text = Watts(wall.Watts);
            NowCaptionText.Text = hasComponents && whole > 0
                ? $"The sensors account for {UiColors.Percent(ShareOf(s.TotalWatts))} of it; the other {UiColors.Percent(ShareOf(Math.Max(rest, 0)))} is fans, drives and power-supply losses."
                : "";
        }
        else if (hasComponents)
        {
            NowLabelText.Text = "Right now · CPU, GPU and memory";
            NowWattsText.Text = Watts(s.TotalWatts);
            NowCaptionText.Text = "Add a wall meter in Settings to see the whole PC.";
        }
        else
        {
            NowLabelText.Text = "Right now";
            NowWattsText.Text = "-";
            NowCaptionText.Text = PowerStatusText.Text ?? "";
        }

        PowerInfoText.Text = wall is not null
            ? "Active power is what the CPU, GPU and memory draw, read from their own sensors. The wall meter measures the whole PC, which is the figure that matches your bill."
            : "Active power is what the CPU, GPU and memory draw while the PC is on, read from their own sensors. It leaves out fans, drives, the motherboard, power-supply losses and any time the PC sleeps. Set up a wall meter in Settings for the whole-PC figure.";

        PowerNotesText.Text = s.Notes.Count == 0 ? "" : "Not reporting:" + Environment.NewLine + string.Join(Environment.NewLine, s.Notes);
        PowerNotesText.IsVisible = s.Notes.Count > 0;
        PowerTotalText.IsVisible = hasComponents;
        PowerSessionText.IsVisible = hasComponents && showSession;
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

    /// <summary>What a kWh costs today: the single rate, or the price of the bill covering today (or the latest bill's).</summary>
    private decimal? CurrentRate() => _services.Database?.LoadRateSchedule().RateAt(DateOnly.FromDateTime(DateTime.Now));

    private void RefreshEstimate()
    {
        if (!double.TryParse(WattsBox.Text, NumberStyles.AllowDecimalPoint, CultureInfo.CurrentCulture, out var watts)
            || watts < 0)
        {
            EstimateText.Text = "Enter a number of watts, for example 400.";
            return;
        }
        if (CurrentRate() is not { } rate)
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
        // The background recorder runs from the install folder: while it runs, the update cannot replace the files and fails
        // after the window has closed, leaving the old version. Pause it first (Windows may ask for permission).
        UpdateStatusText.Text = "Pausing background recording so the update can replace its files...";
        if (await _services.StopCollectorForUpdateAsync() is { } problem)
        {
            UpdateStatusText.Text = problem;
            ApplyUpdateButton.IsEnabled = true;
            return;
        }
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
