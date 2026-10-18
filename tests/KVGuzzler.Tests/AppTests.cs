using System.Globalization;
using Avalonia;
using Avalonia.Controls;
using Avalonia.Headless;
using Avalonia.Headless.XUnit;
using Avalonia.Interactivity;
using Kvg.Standards;
using KVGuzzler;
using KVGuzzler.Data;
using KVGuzzler.Services;
using KVGuzzler.Views;
using Xunit;

[assembly: AvaloniaTestApplication(typeof(KVGuzzler.Tests.TestAppBuilder))]

namespace KVGuzzler.Tests;

public static class TestSetup
{
    // Velopack's UpdateManager refuses to exist until VelopackApp.Build().Run() has run, as in Program.Main.
    [System.Runtime.CompilerServices.ModuleInitializer]
    public static void InitVelopack() => Velopack.VelopackApp.Build().Run();
}

public static class TestAppBuilder
{
    public static AppBuilder BuildAvaloniaApp() =>
        AppBuilder.Configure<App>().UseHeadless(new AvaloniaHeadlessPlatformOptions());
}

public sealed class TempData : IDisposable
{
    public string Dir { get; } = Path.Combine(Path.GetTempPath(), "kvguzzler-test-" + Guid.NewGuid().ToString("N")[..8]);
    public AppServices Services { get; }

    public TempData()
    {
        Directory.CreateDirectory(Dir);
        Services = new AppServices(Dir, null, new UpdateService(AppServices.RepoUrl));
        Services.OpenDatabase();
    }

    public void Dispose()
    {
        Services.Dispose();
        try { Directory.Delete(Dir, recursive: true); } catch (IOException) { /* best effort */ }
    }
}

public class DatabaseTests
{
    [Fact]
    public void SettingsRoundTripAndUpdate()
    {
        using var t = new TempData();
        Assert.Null(t.Services.Database!.GetSetting("rate_per_kwh"));
        t.Services.Database.SetSetting("rate_per_kwh", "0.15");
        t.Services.Database.SetSetting("rate_per_kwh", "0.20");
        Assert.Equal("0.20", t.Services.Database.GetSetting("rate_per_kwh"));
    }

    [Fact]
    public void DatabaseIsInWalMode()
    {
        using var t = new TempData();
        using var c = new Microsoft.Data.Sqlite.SqliteConnection($"Data Source={t.Services.DbLocation.GetEffectiveDbPath()};Pooling=False");
        c.Open();
        using var cmd = c.CreateCommand();
        cmd.CommandText = "PRAGMA journal_mode;";
        Assert.Equal("wal", cmd.ExecuteScalar());
    }
}

public class UpdateServiceTests
{
    [Fact]
    public async Task ADevelopmentRunReportsInsteadOfThrowing()
    {
        var updates = new UpdateService(AppServices.RepoUrl);
        Assert.False(updates.IsInstalled);
        Assert.Equal("development build", updates.VersionText);
        Assert.Contains("development run", await updates.CheckAsync());
        Assert.False(updates.UpdatePending);
    }
}

public class WindowTests
{
    private static void UseInvariantCulture() => CultureInfo.CurrentCulture = CultureInfo.InvariantCulture;

    [AvaloniaFact]
    public void ShowsTheCostOfTheSavedRate()
    {
        UseInvariantCulture();
        using var t = new TempData();
        t.Services.Database!.SetSetting(SettingKeys.RatePerKwh, "0.15");
        t.Services.Database.SetSetting(SettingKeys.Currency, "$");

        var window = new MainWindow(t.Services);
        window.Show();

        Assert.Equal("0.15", window.RateBox.Text);
        Assert.Contains("Per hour: $0.06", window.EstimateText.Text);      // 400 W x 1 h x $0.15/kWh
        Assert.Contains("Per 30 days: $43.20", window.EstimateText.Text);
    }

    [AvaloniaFact]
    public void WithoutARateItSaysHowToSetOne()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();
        Assert.Contains("Save your electricity rate", window.EstimateText.Text);
    }

    [AvaloniaFact]
    public void SavingValidatesAndPersistsTheRate()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();

        window.RateBox.Text = "abc";
        window.SaveRateButton.RaiseEvent(new RoutedEventArgs(Button.ClickEvent));
        Assert.Contains("not a valid price", window.RateStatusText.Text);
        Assert.Null(t.Services.Database!.GetSetting(SettingKeys.RatePerKwh));

        window.RateBox.Text = "0.15";
        window.SaveRateButton.RaiseEvent(new RoutedEventArgs(Button.ClickEvent));
        Assert.Equal("Saved.", window.RateStatusText.Text);
        Assert.Equal("0.15", t.Services.Database.GetSetting(SettingKeys.RatePerKwh));
        Assert.Contains("Per hour: $0.06", window.EstimateText.Text);
    }

    [AvaloniaFact]
    public void EditingTheRateClearsTheStaleSavedMessage()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();

        window.RateBox.Text = "0.15";
        window.SaveRateButton.RaiseEvent(new RoutedEventArgs(Button.ClickEvent));
        Assert.Equal("Saved.", window.RateStatusText.Text);

        window.RateBox.Text = "0.20";                      // a different, unsaved value
        Avalonia.Threading.Dispatcher.UIThread.RunJobs();
        Assert.Equal("", window.RateStatusText.Text);
    }

    [AvaloniaFact]
    public void ACentsRateIsFlagged()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();

        window.RateBox.Text = "15";
        Avalonia.Threading.Dispatcher.UIThread.RunJobs();
        Assert.Equal("15", window.RateBox.Text);
        Assert.True(window.RateWarningText.IsVisible, "warning text was: " + window.RateExampleText.Text);
        window.RateBox.Text = "0.15";
        Avalonia.Threading.Dispatcher.UIThread.RunJobs();
        Assert.False(window.RateWarningText.IsVisible);
    }

    [AvaloniaFact]
    public void RelocatingTheDatabaseKeepsEveryWriteAndAsksForARestart()
    {
        UseInvariantCulture();
        using var t = new TempData();
        t.Services.Database!.SetSetting(SettingKeys.RatePerKwh, "0.15");
        var window = new MainWindow(t.Services);
        window.Show();

        var target = Path.Combine(t.Dir, "synced", "kvguzzler.db");
        window.RelocateTo(target);

        Assert.True(File.Exists(target));
        Assert.False(t.Services.DbLocation.IsUsingDefaultLocation());
        Assert.True(window.RestartButton.IsVisible);
        Assert.False(window.SaveRateButton.IsEnabled);
        Assert.Equal("0.15", new Database(target).GetSetting(SettingKeys.RatePerKwh));   // the WAL write survived the copy
        Assert.Contains("Restart", window.DbStatusText.Text);
    }

    [AvaloniaFact]
    public void ResettingReturnsToTheDefaultLocation()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();
        window.RelocateTo(Path.Combine(t.Dir, "elsewhere", "kvguzzler.db"));
        window.RelocateTo(null);
        Assert.True(t.Services.DbLocation.IsUsingDefaultLocation());
    }

    [AvaloniaFact]
    public void HelpShowsTheVersionAndTheDevelopmentRunNotice()
    {
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();
        Assert.Equal("Version development build", window.VersionText.Text);
    }

    [AvaloniaFact]
    public void TheMissingDatabaseDialogOffersTheTwoChoices()
    {
        string? chosen = null;
        var window = new MissingDatabaseWindow("/gone/kvguzzler.db", () => chosen = "default", () => chosen = "quit");
        window.Show();
        Assert.Contains("/gone/kvguzzler.db", window.MessageText.Text);
        window.UseDefaultButton.RaiseEvent(new RoutedEventArgs(Button.ClickEvent));
        Assert.Equal("default", chosen);
        window.QuitButton.RaiseEvent(new RoutedEventArgs(Button.ClickEvent));
        Assert.Equal("quit", chosen);
    }
}
