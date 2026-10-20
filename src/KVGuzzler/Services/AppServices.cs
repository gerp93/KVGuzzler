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
        StopCollectorForUpdateAsync = () => ServiceInstaller.EnsureStoppedForUpdateAsync(development: !updates.IsInstalled);
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

    /// <summary>One reading from a wall meter at the given address, in watts; throws
    /// <see cref="KVGuzzler.Core.Power.WallMeterException"/> with a message fit to show. Replaceable for tests.</summary>
    public Func<string, Task<double>> ReadWallMeterAsync { get; set; } =
        host => KVGuzzler.Core.Power.ShellyPlug.ReadWattsAsync(WallMeterHttp, host);

    private static readonly HttpClient WallMeterHttp = new();

    /// <summary>The recent grid carbon intensity (oldest first) from the source, key and place in the config; throws
    /// <see cref="KVGuzzler.Core.Carbon.CarbonException"/> with a message fit to show. Replaceable for tests.</summary>
    public Func<KVGuzzler.Core.Carbon.CarbonConfig, Task<IReadOnlyList<KVGuzzler.Core.Carbon.CarbonReading>>> ReadCarbonAsync { get; set; } =
        config => KVGuzzler.Core.Carbon.CarbonProviders.ReadAsync(CarbonHttp, config, DateTimeOffset.UtcNow);

    private static readonly HttpClient CarbonHttp = new();

    /// <summary>Finds the electricity rates for a place (OpenEI). Replaceable for tests.</summary>
    public KVGuzzler.Core.Billing.IRateLookup RateLookup { get; set; } = new KVGuzzler.Core.Billing.OpenEiClient(OpenEiHttp);

    private static readonly HttpClient OpenEiHttp = new();

    /// <summary>Asks Windows whether the collector service is installed and running. Replaceable for tests.</summary>
    public Func<ServiceState> QueryCollectorService { get; set; }

    /// <summary>Gets the collector service out of the way of an update; null when the program files are free to replace, otherwise
    /// what to tell the user. Replaceable for tests.</summary>
    public Func<Task<string?>> StopCollectorForUpdateAsync { get; set; }

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
