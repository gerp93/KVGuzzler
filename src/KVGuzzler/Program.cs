using Avalonia;
using Kvg.Standards;
using KVGuzzler.Sensors;
using KVGuzzler.Services;
using Velopack;

namespace KVGuzzler;

public static class Program
{
    public static AppServices Services { get; private set; } = null!;

    [STAThread]
    public static int Main(string[] args)
    {
        // Must be the first statement: handles install/uninstall/update hooks and may exit the process.
        VelopackApp.Build().Run();

        var updates = new UpdateService(AppServices.RepoUrl);

        // A development run gets its own data folder, so it can never share a database with an installed copy.
        var dataDir = DbLocation.ResolveDataDirectory(AppServices.AppName, isDevelopment: !updates.IsInstalled);

        // Only one copy per data folder; two writers on one database is how data gets lost.
        var instanceLock = InstanceLock.TryAcquire(dataDir);
        if (instanceLock is null) return 0;

        Services = new AppServices(dataDir, instanceLock, updates, new PowerMonitor(PowerSources.Detect));
        try
        {
            return BuildAvaloniaApp().StartWithClassicDesktopLifetime(args);
        }
        finally
        {
            Services.Dispose();
        }
    }

    public static AppBuilder BuildAvaloniaApp() =>
        AppBuilder.Configure<App>().UsePlatformDetect();
}
