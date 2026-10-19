using System.Runtime.Versioning;
using System.Security.AccessControl;
using System.Security.Principal;
using Kvg.Standards;
using KVGuzzler.Core.History;
using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.Hosting;
using Microsoft.Extensions.Logging;

namespace KVGuzzler.Collector;

/// <summary>
/// Runs the collector without any window: as a Windows service when started by the service manager,
/// as a plain console process otherwise (<c>KVGuzzler.exe --collector</c>, stop with Ctrl+C).
/// </summary>
public static class CollectorHost
{
    public const string Argument = "--collector";

    public static int Run(string[] args, bool development)
    {
        var dataDir = HistoryStore.DefaultDirectory(development);
        Directory.CreateDirectory(dataDir);

        // One collector per history folder: two writers would count every second twice.
        using var instanceLock = InstanceLock.TryAcquire(dataDir, "collector.lock");
        if (instanceLock is null) return 0;

        if (OperatingSystem.IsWindows()) AllowUsersToWrite(dataDir);

        var builder = Host.CreateApplicationBuilder(args);
        builder.Services.AddWindowsService(o => o.ServiceName = ServiceInstaller.ServiceName(development));
        builder.Services.AddHostedService(sp => new CollectorWorker(dataDir, sp.GetRequiredService<ILogger<CollectorWorker>>()));
        builder.Build().Run();
        return 0;
    }

    /// <summary>The service runs as the system account, so it owns the folder; a window running as an
    /// ordinary user still has to open the history file (SQLite needs to write its -shm file even to read
    /// a WAL database). Everyone on this machine can therefore modify the history, which is accepted for
    /// a personal power log.</summary>
    [SupportedOSPlatform("windows")]
    private static void AllowUsersToWrite(string dir)
    {
        try
        {
            var info = new DirectoryInfo(dir);
            var security = info.GetAccessControl();
            security.AddAccessRule(new FileSystemAccessRule(
                new SecurityIdentifier(WellKnownSidType.BuiltinUsersSid, null),
                FileSystemRights.Modify,
                InheritanceFlags.ContainerInherit | InheritanceFlags.ObjectInherit,
                PropagationFlags.None,
                AccessControlType.Allow));
            info.SetAccessControl(security);
        }
        catch (Exception e) when (e is UnauthorizedAccessException or IOException or InvalidOperationException)
        {
            // Not fatal: the collector still records. The window may not be able to read it until this is fixed.
            File.AppendAllText(Path.Combine(dir, "collector.log"), $"{DateTimeOffset.Now:u} Could not share the history folder: {e.Message}{Environment.NewLine}");
        }
    }
}
