using Avalonia;
using Kvg.Standards;
using KVGuzzler.Collector;
using KVGuzzler.Core.History;
using KVGuzzler.Core.Power;
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
        BuildVelopack().Run();

        var updates = new UpdateService(AppServices.RepoUrl);
        var development = !updates.IsInstalled;

        // Headless modes, before any window or the settings database: the background collector, and the
        // elevated copy that installs or removes its service.
        if (args.Contains(CollectorHost.Argument)) return CollectorHost.Run(args, development);
        if (args.Contains(ServiceInstaller.InstallArgument)) return ServiceInstaller.RunElevated(install: true, development);
        if (args.Contains(ServiceInstaller.UninstallArgument)) return ServiceInstaller.RunElevated(install: false, development);

        // A development run gets its own data folder, so it can never share a database with an installed copy.
        var dataDir = DbLocation.ResolveDataDirectory(AppServices.AppName, isDevelopment: development);

        // Only one copy per data folder; two writers on one database is how data gets lost.
        var instanceLock = InstanceLock.TryAcquire(dataDir);
        if (instanceLock is null) return 0;

        Services = new AppServices(dataDir, instanceLock, updates, new PowerMonitor(() => PowerSources.Detect(
                Path.Combine(HistoryStore.DefaultDirectory(development), WallMeterConfig.FileName))));
        try
        {
            return BuildAvaloniaApp().StartWithClassicDesktopLifetime(args);
        }
        finally
        {
            Services.Dispose();
        }
    }

    /// <summary>Velopack's entry point, with the hooks that keep the background collector service in step with
    /// the install on Windows. A per-machine install runs them elevated; a per-user one cannot change a
    /// service, which <see cref="Quietly"/> absorbs.</summary>
    private static VelopackApp BuildVelopack()
    {
        var app = VelopackApp.Build();
        return OperatingSystem.IsWindows() ? WithServiceHooks(app) : app;
    }

    [System.Runtime.Versioning.SupportedOSPlatform("windows")]
    private static VelopackApp WithServiceHooks(VelopackApp app) => app
        .OnAfterInstallFastCallback(_ => Quietly(() => ServiceInstaller.Install(development: false)))
        .OnBeforeUpdateFastCallback(_ => Quietly(() => ServiceInstaller.StopForUpdate(development: false)))
        .OnAfterUpdateFastCallback(_ => Quietly(() => ServiceInstaller.Install(development: false)))
        .OnBeforeUninstallFastCallback(_ => Quietly(() => ServiceInstaller.Uninstall(development: false)));

    /// <summary>Install hooks must never fail the installer or updater over an optional service.</summary>
    private static void Quietly(Action action)
    {
        try { action(); }
        catch (Exception) { /* the service is optional; the app works without it */ }
    }

    public static AppBuilder BuildAvaloniaApp() =>
        AppBuilder.Configure<App>().UsePlatformDetect();
}
