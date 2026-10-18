// KvgDbLocation - copy this file into your app (see README.md); do not edit the copy.
// Source: gerp93/KVG_Standards packages/dotnet/KvgDbLocation. The re-vendor script
// stamps the source tag into the line below; fix bugs upstream, then re-run it.
// Vendored-from: gerp93/KVG_Standards main (b6c10c11912fb0df6f1dc50864878bd8093a8a7d) - do not hand-edit
//
// User-configurable SQLite file location for .NET desktop apps. Mirrors
// packages/python/kvg_dblocation and Sweeper's src/main/dbLocation.ts: a default
// path under the app's own data directory, an optional override the user can point
// elsewhere (for backup/syncing a folder outside the app's own files), and a
// copy-on-relocate so moving the file never loses data. It also carries the two
// "instance isolation" pieces from db-location-versioning.md that need no
// third-party code: dev/packaged data-folder separation and a single-instance lock.
//
// This file only manages paths and bookkeeping. It does not open the database or
// restart the app, and it has no dependencies beyond the BCL.
#nullable enable
using System;
using System.Globalization;
using System.IO;
using System.Text.Json;
using System.Text.Json.Nodes;

namespace Kvg.Standards;

/// <summary>Manages one SQLite file's location: a default path under a data
/// directory, optionally overridden through a small JSON config stored beside it.</summary>
public sealed class DbLocation
{
    private const string ConfigKey = "db_path";   // same key as kvg_dblocation (Python)

    private readonly string _configPath;
    private readonly string _defaultFilename;

    /// <param name="dataDir">Where this app keeps its per-user data. Use
    /// <see cref="ResolveDataDirectory"/> so dev and packaged builds never share a folder.</param>
    /// <param name="defaultFilename">e.g. "kvguzzler.db".</param>
    public DbLocation(string dataDir, string defaultFilename, string configFilename = "db_location.json")
    {
        DataDir = dataDir;
        _defaultFilename = defaultFilename;
        _configPath = Path.Combine(dataDir, configFilename);
    }

    public string DataDir { get; }

    /// <summary>The per-user data folder: <c>%LOCALAPPDATA%\app</c> on Windows,
    /// <c>~/.local/share/app</c> on Linux. A development run gets <c>app-dev</c>, so
    /// it can never open the same database file as an installed copy.</summary>
    /// <param name="isDevelopment">Pass <c>true</c> for anything that is not an installed
    /// build - with Velopack, <c>!UpdateManager.IsInstalled</c>.</param>
    public static string ResolveDataDirectory(string appName, bool isDevelopment)
    {
        // SpecialFolderOption.Create matters: with the default (None), .NET returns "" when the folder
        // does not exist yet (a fresh Linux account has no ~/.local/share), and Path.Combine("", name)
        // is a RELATIVE path - the database would silently be created in the current directory.
        var root = Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData,
                                             Environment.SpecialFolderOption.Create);
        if (string.IsNullOrEmpty(root))
            throw new InvalidOperationException("Could not determine the per-user application data folder.");
        return Path.Combine(root, isDevelopment ? appName + "-dev" : appName);
    }

    public string GetDefaultDbPath() => Path.Combine(DataDir, _defaultFilename);

    /// <summary>The raw user override with no fallback, or null when using the default.
    /// Use it to detect a configured custom path that has gone missing (see
    /// <see cref="IsConfiguredPathMissing"/>).</summary>
    public string? GetConfiguredDbPath()
    {
        var path = (ReadConfig()[ConfigKey] as JsonValue)?.GetValue<string>();
        return string.IsNullOrEmpty(path) ? null : path;
    }

    /// <summary>The database file the app should actually open.</summary>
    public string GetEffectiveDbPath() => GetConfiguredDbPath() ?? GetDefaultDbPath();

    public bool IsUsingDefaultLocation() => GetConfiguredDbPath() is null;

    /// <summary>True when the user chose a custom location and nothing exists there now
    /// (drive unplugged, synced folder not mounted yet). The app must NOT quietly create
    /// an empty database in that case - that is exactly how data "disappears". Show a
    /// Quit / use-default choice instead. False on first run and for the default path.</summary>
    public bool IsConfiguredPathMissing()
    {
        var configured = GetConfiguredDbPath();
        return configured is not null && !File.Exists(configured);
    }

    /// <summary>Size of the effective database file in bytes, or null if it does not
    /// exist. Pair with <see cref="FileSize.Format"/> for the Settings UI.</summary>
    public long? GetDbFileSizeBytes()
    {
        var path = GetEffectiveDbPath();
        return File.Exists(path) ? new FileInfo(path).Length : null;
    }

    /// <summary>Point the app at a different SQLite file. If nothing exists at the new
    /// location, the current database is copied there first so no data is lost; if a file
    /// already exists there it is left alone and adopted as-is.
    ///
    /// Close every connection first. With Microsoft.Data.Sqlite also call
    /// <c>SqliteConnection.ClearAllPools()</c>: the pool keeps the file open, and in WAL
    /// mode recent writes live in the -wal file until the last connection closes and
    /// checkpoints - copying the .db before that silently loses them.</summary>
    public void SetDbPath(string newPath)
    {
        newPath = Path.GetFullPath(newPath);
        var currentPath = GetEffectiveDbPath();

        if (!File.Exists(newPath) && File.Exists(currentPath))
        {
            var directory = Path.GetDirectoryName(newPath);
            if (!string.IsNullOrEmpty(directory)) Directory.CreateDirectory(directory);

            // Copy beside the target and move into place, so a crash mid-copy can never
            // leave a truncated file that the next launch would adopt as the real database.
            var partial = newPath + ".partial";
            File.Copy(currentPath, partial, overwrite: true);
            File.Move(partial, newPath);
        }

        var config = ReadConfig();
        config[ConfigKey] = newPath;
        WriteConfig(config);
    }

    public void ResetToDefaultDbPath()
    {
        var config = ReadConfig();
        config.Remove(ConfigKey);
        WriteConfig(config);
    }

    private JsonObject ReadConfig()
    {
        if (!File.Exists(_configPath)) return new JsonObject();
        try
        {
            return JsonNode.Parse(File.ReadAllText(_configPath)) as JsonObject ?? new JsonObject();
        }
        catch (Exception e) when (e is JsonException or IOException)
        {
            return new JsonObject();
        }
    }

    private void WriteConfig(JsonObject config)
    {
        Directory.CreateDirectory(DataDir);
        File.WriteAllText(_configPath, config.ToJsonString(new JsonSerializerOptions { WriteIndented = true }));
    }
}

/// <summary>Holds the "only one copy of this app per data folder" lock. Two copies
/// sharing one database is how data gets clobbered. A file lock is used instead of a
/// named mutex because it behaves the same on Windows and Linux and is released by
/// the OS if the process crashes.</summary>
public sealed class InstanceLock : IDisposable
{
    private FileStream? _stream;

    private InstanceLock(FileStream stream) => _stream = stream;

    /// <summary>Returns the lock, or null if another copy already holds it. Call it first
    /// thing in <c>Main</c>, before opening any window or the database, and exit if null
    /// (optionally after signalling the running copy to come to the front). Keep the
    /// returned object alive for the life of the process.</summary>
    public static InstanceLock? TryAcquire(string dataDir, string lockFilename = "instance.lock")
    {
        Directory.CreateDirectory(dataDir);
        try
        {
            var stream = new FileStream(Path.Combine(dataDir, lockFilename), FileMode.OpenOrCreate,
                                        FileAccess.ReadWrite, FileShare.None);
            return new InstanceLock(stream);
        }
        catch (IOException)
        {
            return null;
        }
    }

    public void Dispose()
    {
        _stream?.Dispose();
        _stream = null;
    }
}

public static class FileSize
{
    /// <summary>Human size in whichever of KB / MB / GB fits, e.g. "842 KB", "4.1 MB",
    /// "1.3 GB" - the format the Settings UI shows next to the database path.</summary>
    public static string Format(long bytes, IFormatProvider? provider = null)
    {
        provider ??= CultureInfo.CurrentCulture;
        const double kb = 1024, mb = kb * 1024, gb = mb * 1024;
        if (bytes >= gb) return string.Format(provider, "{0:0.0} GB", bytes / gb);
        if (bytes >= mb) return string.Format(provider, "{0:0.0} MB", bytes / mb);
        return string.Format(provider, "{0:0} KB", Math.Round(bytes / kb));
    }
}
