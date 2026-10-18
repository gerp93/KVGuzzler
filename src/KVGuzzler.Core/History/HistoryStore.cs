using Microsoft.Data.Sqlite;
using KVGuzzler.Core.Power;

namespace KVGuzzler.Core.History;

/// <summary>Why the collector was not sampling for a stretch of time.</summary>
public enum GapReason
{
    /// <summary>The machine slept or hibernated while the collector was running. It still drew a little power.</summary>
    Asleep = 0,

    /// <summary>The collector was stopped cleanly (shutdown, or someone stopped the service).</summary>
    Off = 1,

    /// <summary>The collector disappeared without stopping cleanly (power loss, crash, hard reset).</summary>
    LostPower = 2,
}

/// <summary>Energy one reading used during one minute (UTC, minute-aligned).</summary>
public sealed record MinuteRow(long MinuteStart, string Name, ReadingScope Scope, double KilowattHours, double Seconds, double PeakWatts);

public sealed record Gap(DateTimeOffset Start, DateTimeOffset End, GapReason Reason);

/// <summary>What the collector saw on its latest sample.</summary>
public sealed record LiveState(DateTimeOffset UpdatedAt, IReadOnlyList<PowerReading> Readings, IReadOnlyList<string> Notes);

/// <summary>
/// The collector's SQLite file: one row per reading per minute, the gaps between sampling, and the
/// latest live readings. The collector service writes it; the window reads it. WAL mode, short-lived
/// pooled connections, as in the settings database.
/// </summary>
public sealed class HistoryStore : IDisposable
{
    public const string FileName = "history.db";

    private const string LastSeenKey = "last_seen_ms";
    private const string CleanStopKey = "clean_stop_ms";
    private const string NotesKey = "notes";

    private readonly string _connectionString;

    private const string WallMinutesOnly = """
        AND minute_start IN (SELECT minute_start FROM samples WHERE scope = 2 AND minute_start >= $f AND minute_start < $t)
        """;

    public HistoryStore(string path)
    {
        Path = path;
        Directory.CreateDirectory(System.IO.Path.GetDirectoryName(System.IO.Path.GetFullPath(path))!);
        _connectionString = new SqliteConnectionStringBuilder { DataSource = path }.ToString();
        using var c = Open();
        Exec(c, "PRAGMA journal_mode=WAL;");
        Exec(c, """
            CREATE TABLE IF NOT EXISTS samples (
                minute_start INTEGER NOT NULL, name TEXT NOT NULL, scope INTEGER NOT NULL,
                kwh REAL NOT NULL, seconds REAL NOT NULL, peak_watts REAL NOT NULL,
                PRIMARY KEY (minute_start, name));
            CREATE TABLE IF NOT EXISTS gaps (
                start_ms INTEGER PRIMARY KEY, end_ms INTEGER NOT NULL, reason INTEGER NOT NULL);
            CREATE TABLE IF NOT EXISTS live (name TEXT PRIMARY KEY, scope INTEGER NOT NULL, watts REAL NOT NULL, position INTEGER NOT NULL);
            CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
            """);
    }

    public string Path { get; }

    /// <summary>The folder the collector keeps its data in. The service runs for every user, so this is the
    /// machine-wide data folder (%ProgramData% on Windows), never a folder in someone's profile. A
    /// development run gets its own folder so it cannot touch an installed collector's history.</summary>
    public static string DefaultDirectory(bool isDevelopment) =>
        System.IO.Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.CommonApplicationData),
            isDevelopment ? "KVGuzzler-dev" : "KVGuzzler");

    // ----- Writing (collector) ----------------------------------------------------------

    /// <summary>Adds energy to the minute rows. Adding rather than replacing means a collector restarted
    /// inside a minute does not lose what was already saved for it.</summary>
    public void AddMinutes(IReadOnlyList<MinuteRow> rows)
    {
        if (rows.Count == 0) return;
        using var c = Open();
        using var tx = c.BeginTransaction();
        foreach (var r in rows)
        {
            using var cmd = c.CreateCommand();
            cmd.Transaction = tx;
            cmd.CommandText = """
                INSERT INTO samples (minute_start, name, scope, kwh, seconds, peak_watts)
                VALUES ($m, $n, $s, $k, $sec, $p)
                ON CONFLICT (minute_start, name) DO UPDATE SET
                    kwh = kwh + excluded.kwh, seconds = seconds + excluded.seconds,
                    peak_watts = MAX(peak_watts, excluded.peak_watts)
                """;
            cmd.Parameters.AddWithValue("$m", r.MinuteStart);
            cmd.Parameters.AddWithValue("$n", r.Name);
            cmd.Parameters.AddWithValue("$s", (int)r.Scope);
            cmd.Parameters.AddWithValue("$k", r.KilowattHours);
            cmd.Parameters.AddWithValue("$sec", r.Seconds);
            cmd.Parameters.AddWithValue("$p", r.PeakWatts);
            cmd.ExecuteNonQuery();
        }
        tx.Commit();
    }

    public void AddGap(Gap gap)
    {
        using var c = Open();
        using var cmd = c.CreateCommand();
        cmd.CommandText = "INSERT OR REPLACE INTO gaps (start_ms, end_ms, reason) VALUES ($s, $e, $r)";
        cmd.Parameters.AddWithValue("$s", gap.Start.ToUnixTimeMilliseconds());
        cmd.Parameters.AddWithValue("$e", gap.End.ToUnixTimeMilliseconds());
        cmd.Parameters.AddWithValue("$r", (int)gap.Reason);
        cmd.ExecuteNonQuery();
    }

    /// <summary>Replaces the live readings and records when the collector last saw the machine.</summary>
    public void SetLive(DateTimeOffset now, IReadOnlyList<PowerReading> readings, IReadOnlyList<string> notes)
    {
        using var c = Open();
        using var tx = c.BeginTransaction();
        Run(c, tx, "DELETE FROM live");
        for (var i = 0; i < readings.Count; i++)
        {
            using var cmd = c.CreateCommand();
            cmd.Transaction = tx;
            cmd.CommandText = "INSERT INTO live (name, scope, watts, position) VALUES ($n, $s, $w, $p) ON CONFLICT (name) DO UPDATE SET watts = excluded.watts";
            cmd.Parameters.AddWithValue("$n", readings[i].Name);
            cmd.Parameters.AddWithValue("$s", (int)readings[i].Scope);
            cmd.Parameters.AddWithValue("$w", readings[i].Watts);
            cmd.Parameters.AddWithValue("$p", i);
            cmd.ExecuteNonQuery();
        }
        SetMeta(c, tx, LastSeenKey, now.ToUnixTimeMilliseconds().ToString());
        SetMeta(c, tx, NotesKey, string.Join("\n", notes));
        tx.Commit();
    }

    /// <summary>Marks that the collector is stopping on purpose, as of the moment it last saw the machine.</summary>
    public void MarkCleanStop()
    {
        using var c = Open();
        using var tx = c.BeginTransaction();
        if (GetMeta(c, LastSeenKey) is { } last) SetMeta(c, tx, CleanStopKey, last);
        tx.Commit();
    }

    // ----- Reading (collector and window) -----------------------------------------------

    public DateTimeOffset? LastSeen()
    {
        using var c = Open();
        return long.TryParse(GetMeta(c, LastSeenKey), out var ms) ? DateTimeOffset.FromUnixTimeMilliseconds(ms) : null;
    }

    /// <summary>True when the last thing the collector wrote was a deliberate stop, so the time since is
    /// "off" rather than "lost power".</summary>
    public bool StoppedCleanly()
    {
        using var c = Open();
        var last = GetMeta(c, LastSeenKey);
        return last is not null && last == GetMeta(c, CleanStopKey);
    }

    public LiveState? GetLive()
    {
        using var c = Open();
        if (!long.TryParse(GetMeta(c, LastSeenKey), out var ms)) return null;

        var readings = new List<PowerReading>();
        using (var cmd = c.CreateCommand())
        {
            cmd.CommandText = "SELECT name, scope, watts FROM live ORDER BY position";
            using var r = cmd.ExecuteReader();
            while (r.Read()) readings.Add(new PowerReading(r.GetString(0), r.GetDouble(2), (ReadingScope)r.GetInt32(1)));
        }

        var notes = (GetMeta(c, NotesKey) ?? "").Split('\n', StringSplitOptions.RemoveEmptyEntries);
        return new LiveState(DateTimeOffset.FromUnixTimeMilliseconds(ms), readings, notes);
    }

    /// <summary>Energy between two instants for one kind of reading (by default the measured components, i.e.
    /// the active energy; <see cref="ReadingScope.Wall"/> for the whole PC), with the time that was covered
    /// and the gaps inside the period (clipped to it). With <paramref name="onlyWhereWallWasRecorded"/>, only the
    /// minutes in which the wall meter also recorded count, so a part can be compared fairly with the whole.</summary>
    public (double KilowattHours, TimeSpan Monitored, IReadOnlyList<Gap> Gaps) Query(
        DateTimeOffset from, DateTimeOffset to, ReadingScope scope = ReadingScope.Component, bool onlyWhereWallWasRecorded = false)
    {
        using var c = Open();
        var fromMinute = from.ToUnixTimeSeconds() / 60 * 60;
        var toSeconds = to.ToUnixTimeSeconds();

        var wallFilter = onlyWhereWallWasRecorded ? WallMinutesOnly : "";

        double kwh, monitored;
        using (var cmd = c.CreateCommand())
        {
            cmd.CommandText = $"""
                SELECT COALESCE(SUM(kwh), 0) FROM samples
                WHERE scope = $scope AND minute_start >= $f AND minute_start < $t {wallFilter}
                """;
            cmd.Parameters.AddWithValue("$scope", (int)scope);
            cmd.Parameters.AddWithValue("$f", fromMinute);
            cmd.Parameters.AddWithValue("$t", toSeconds);
            kwh = Convert.ToDouble(cmd.ExecuteScalar());
        }
        using (var cmd = c.CreateCommand())
        {
            // Time covered = per minute, the longest any one component was sampled.
            cmd.CommandText = $"""
                SELECT COALESCE(SUM(s), 0) FROM (
                    SELECT MAX(seconds) AS s FROM samples
                    WHERE scope = $scope AND minute_start >= $f AND minute_start < $t {wallFilter}
                    GROUP BY minute_start)
                """;
            cmd.Parameters.AddWithValue("$scope", (int)scope);
            cmd.Parameters.AddWithValue("$f", fromMinute);
            cmd.Parameters.AddWithValue("$t", toSeconds);
            monitored = Convert.ToDouble(cmd.ExecuteScalar());
        }

        var gaps = new List<Gap>();
        using (var cmd = c.CreateCommand())
        {
            cmd.CommandText = "SELECT start_ms, end_ms, reason FROM gaps WHERE end_ms > $f AND start_ms < $t ORDER BY start_ms";
            cmd.Parameters.AddWithValue("$f", from.ToUnixTimeMilliseconds());
            cmd.Parameters.AddWithValue("$t", to.ToUnixTimeMilliseconds());
            using var r = cmd.ExecuteReader();
            while (r.Read())
            {
                var start = DateTimeOffset.FromUnixTimeMilliseconds(r.GetInt64(0));
                var end = DateTimeOffset.FromUnixTimeMilliseconds(r.GetInt64(1));
                gaps.Add(new Gap(start < from ? from : start, end > to ? to : end, (GapReason)r.GetInt32(2)));
            }
        }

        return (kwh, TimeSpan.FromSeconds(monitored), gaps);
    }

    /// <summary>Release the file (pooled connections keep it open, and WAL leaves recent writes in the -wal file).</summary>
    public void Dispose() => SqliteConnection.ClearAllPools();

    // ----- Plumbing ---------------------------------------------------------------------

    private SqliteConnection Open()
    {
        var c = new SqliteConnection(_connectionString);
        c.Open();
        return c;
    }

    private static void Exec(SqliteConnection c, string sql)
    {
        using var cmd = c.CreateCommand();
        cmd.CommandText = sql;
        cmd.ExecuteNonQuery();
    }

    private static void Run(SqliteConnection c, SqliteTransaction tx, string sql)
    {
        using var cmd = c.CreateCommand();
        cmd.Transaction = tx;
        cmd.CommandText = sql;
        cmd.ExecuteNonQuery();
    }

    private static string? GetMeta(SqliteConnection c, string key)
    {
        using var cmd = c.CreateCommand();
        cmd.CommandText = "SELECT value FROM meta WHERE key = $k";
        cmd.Parameters.AddWithValue("$k", key);
        return cmd.ExecuteScalar() as string;
    }

    private static void SetMeta(SqliteConnection c, SqliteTransaction tx, string key, string value)
    {
        using var cmd = c.CreateCommand();
        cmd.Transaction = tx;
        cmd.CommandText = "INSERT INTO meta (key, value) VALUES ($k, $v) ON CONFLICT (key) DO UPDATE SET value = excluded.value";
        cmd.Parameters.AddWithValue("$k", key);
        cmd.Parameters.AddWithValue("$v", value);
        cmd.ExecuteNonQuery();
    }
}
