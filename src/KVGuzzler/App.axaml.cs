using Avalonia;
using Avalonia.Controls.ApplicationLifetimes;
using Avalonia.Markup.Xaml;
using KVGuzzler.Views;

namespace KVGuzzler;

public partial class App : Application
{
    public override void Initialize() => AvaloniaXamlLoader.Load(this);

    public override void OnFrameworkInitializationCompleted()
    {
        if (ApplicationLifetime is IClassicDesktopStyleApplicationLifetime desktop)
        {
            var services = Program.Services;

            // A custom database location the user chose has gone missing (drive unplugged, synced
            // folder not mounted). Never quietly create an empty database over it: ask first.
            if (services.DbLocation.IsConfiguredPathMissing())
            {
                desktop.MainWindow = new MissingDatabaseWindow(
                    services.DbLocation.GetConfiguredDbPath()!,
                    useDefault: () =>
                    {
                        services.DbLocation.ResetToDefaultDbPath();
                        ShowMainWindow(desktop);
                    },
                    quit: () => desktop.Shutdown());
            }
            else
            {
                ShowMainWindow(desktop);
            }
        }

        base.OnFrameworkInitializationCompleted();
    }

    private static void ShowMainWindow(IClassicDesktopStyleApplicationLifetime desktop)
    {
        var services = Program.Services;
        services.OpenDatabase();
        var previous = desktop.MainWindow;
        var main = new MainWindow(services);
        desktop.MainWindow = main;
        main.Show();
        previous?.Close();

        // Quietly look for an update on startup; the Help tab shows the result.
        _ = main.CheckForUpdatesAsync();
    }
}
