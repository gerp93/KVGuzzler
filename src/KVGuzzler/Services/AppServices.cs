using System.Diagnostics;
using KVGuzzler.Data;
using Kvg.Standards;

namespace KVGuzzler.Services;

/// <summary>Everything the windows share: data folder, database location, database, updater, power monitor.</summary>
public sealed class AppServices : IDisposable
{
    public const string AppName = "KVGuzzler";
    public const string RepoUrl = "https://github.com/gerp93/KVGuzzler";

    private readonly InstanceLock? _instanceLock;

    public AppServices(string dataDir, InstanceLock? instanceLock, UpdateService updates, PowerMonitor? power = null)
    {
        DataDir = dataDir;
        _instanceLock = instanceLock;
        Updates = updates;
        Power = power;
        DbLocation = new DbLocation(dataDir, "kvguzzler.db");
    }

    public string DataDir { get; }
    public DbLocation DbLocation { get; }
    public UpdateService Updates { get; }

    /// <summary>Live power readings. Null where there is no hardware to read (the UI tests).</summary>
    public PowerMonitor? Power { get; }
    public Database? Database { get; private set; }

    public void OpenDatabase() => Database = new Database(DbLocation.GetEffectiveDbPath());

    public void CloseDatabase()
    {
        Database?.Dispose();
        Database = null;
    }

    /// <summary>Starts a fresh copy of the app, then returns so the caller can shut down. The lock and
    /// database are released first: the new copy would otherwise find the lock held and exit.</summary>
    public void StartReplacementProcess()
    {
        Dispose();
        var host = Environment.ProcessPath!;
        var psi = new ProcessStartInfo(host) { UseShellExecute = false };
        // `dotnet run` / `dotnet KVGuzzler.dll`: the host is the dotnet muxer, which needs the dll back.
        if (Path.GetFileNameWithoutExtension(host) == "dotnet") psi.ArgumentList.Add(Environment.GetCommandLineArgs()[0]);
        Process.Start(psi);
    }

    public void Dispose()
    {
        Power?.Dispose();
        CloseDatabase();
        _instanceLock?.Dispose();
    }
}
