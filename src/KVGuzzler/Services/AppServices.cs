using System.Diagnostics;
using KVGuzzler.Collector;
using KVGuzzler.Core.History;
using KVGuzzler.Data;
using Kvg.Standards;

namespace KVGuzzler.Services;

/// <summary>Everything the windows share: data folder, database location, database, updater, power monitor.</summary>
public sealed class AppServices : IDisposable
{
    public const string AppName = "KVGuzzler";
    public const string RepoUrl = "https://github.com/gerp93/KVGuzzler";

    private readonly InstanceLock? _instanceLock;

    private HistoryStore? _history;

    /// <param name="historyDirectory">Where the background collector keeps its history. Tests pass a temporary
    /// folder; the app uses the machine-wide folder.</param>
    public AppServices(string dataDir, InstanceLock? instanceLock, UpdateService updates, PowerMonitor? power = null,
        string? historyDirectory = null)
    {
        HistoryDirectory = historyDirectory ?? HistoryStore.DefaultDirectory(isDevelopment: !updates.IsInstalled);
        QueryCollectorService = () => ServiceInstaller.Query(development: !updates.IsInstalled);
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

    /// <summary>The background collector's folder (machine-wide, written by its service).</summary>
    public string HistoryDirectory { get; }

    /// <summary>Asks Windows whether the collector service is installed and running. Replaceable for tests.</summary>
    public Func<ServiceState> QueryCollectorService { get; set; }

    /// <summary>The collector's history, or null when it has not recorded anything yet on this machine (or
    /// cannot be opened). Never creates the file: only the collector does that.</summary>
    public HistoryStore? OpenHistory()
    {
        if (_history is not null) return _history;
        var path = Path.Combine(HistoryDirectory, HistoryStore.FileName);
        if (!File.Exists(path)) return null;
        try { return _history = new HistoryStore(path); }
        catch (Exception e) when (e is Microsoft.Data.Sqlite.SqliteException or IOException or UnauthorizedAccessException) { return null; }
    }

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
        _history?.Dispose();
        CloseDatabase();
        _instanceLock?.Dispose();
    }
}
