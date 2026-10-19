using System.Globalization;
using Microsoft.Data.Sqlite;
using KVGuzzler.Core.Power;
using KVGuzzler.Core.Programs;

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
public sealed record MinuteRow(
    long MinuteStart, string Name, ReadingScope Scope, double KilowattHours, double Seconds, double PeakWatts,
    PowerKind Kind = PowerKind.Other);

/// <summary>Which line of the power chart a set of saved rows belongs to.</summary>
public enum ChartSeriesKind
{
    /// <summary>The whole PC, from the wall meter.</summary>
    Wall,
    Cpu,
    Gpu,

    /// <summary>The extra meters the user chose to include, added together.</summary>
    Peripheral,
}

/// <summary>What one extra meter (a monitor's plug) recorded in a period.</summary>
/// <param name="PeakWatts">The highest once-a-second reading.</param>
public sealed record PeripheralUse(string Name, double KilowattHours, double PeakWatts);

/// <summary>One minute of one chart series: its energy, how many seconds it covers, and its peak.</summary>
public sealed record SeriesMinute(long MinuteStart, double KilowattHours, double Seconds, double PeakWatts);

/// <summary>The highest power drawn in a period, and the minute it happened in.</summary>
/// <param name="Exact">False when it is only an upper bound: some minutes were saved before the total's peak was tracked, and
/// the best that can be said for them is the sum of each sensor's own peak, which never all happen in the same second.</param>
public sealed record PeakWatts(double Watts, DateTimeOffset At, bool Exact);

/// <summary>A stretch nothing was sampling.</summary>
/// <param name="PlugKwh">The whole-PC energy the smart plug's own counter says was used in the stretch (it keeps counting while the PC is
/// asleep or off), or null when that is not known. Not a sampled figure, and never part of the sensors' energy. When the gap is
/// clipped to a period this is the clipped share, spread evenly over the gap.</param>
public sealed record Gap(DateTimeOffset Start, DateTimeOffset End, GapReason Reason, double? PlugKwh = null);

/// <summary>What one program is estimated to be drawing right now.</summary>
public sealed record ProgramLive(string Program, double CpuWatts, double GpuWatts);

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
    private const string LivePrograms = "live_programs_ms";
    private const string IdleCpuKey = "idle_cpu_w";
    private const string IdleGpuKey = "idle_gpu_w";
    private const string WallCounterKey = "wall_counter";

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
        // The chart needs to know which saved rows are the CPU and which the GPU. Files from before this column
        // existed are upgraded in place (their old rows are read as "unknown" and found by name instead).
        if (!HasColumn(c, "samples", "kind") && TableExists(c, "samples"))
            Exec(c, "ALTER TABLE samples ADD COLUMN kind INTEGER NOT NULL DEFAULT 0;");
        if (!HasColumn(c, "gaps", "plug_kwh") && TableExists(c, "gaps"))
            Exec(c, "ALTER TABLE gaps ADD COLUMN plug_kwh REAL;");
        Exec(c, """
            CREATE TABLE IF NOT EXISTS samples (
                minute_start INTEGER NOT NULL, name TEXT NOT NULL, scope INTEGER NOT NULL,
                kwh REAL NOT NULL, seconds REAL NOT NULL, peak_watts REAL NOT NULL, kind INTEGER NOT NULL DEFAULT 0,
                PRIMARY KEY (minute_start, name));
            CREATE TABLE IF NOT EXISTS total_peaks (minute_start INTEGER PRIMARY KEY, watts REAL NOT NULL);
            CREATE TABLE IF NOT EXISTS gaps (
                start_ms INTEGER PRIMARY KEY, end_ms INTEGER NOT NULL, reason INTEGER NOT NULL, plug_kwh REAL);
            CREATE TABLE IF NOT EXISTS live (name TEXT PRIMARY KEY, scope INTEGER NOT NULL, watts REAL NOT NULL, position INTEGER NOT NULL);
            CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
            CREATE TABLE IF NOT EXISTS program_samples (
                minute_start INTEGER NOT NULL, program TEXT NOT NULL COLLATE NOCASE,
                cpu_kwh REAL NOT NULL, gpu_kwh REAL NOT NULL, active_seconds REAL NOT NULL,
                PRIMARY KEY (minute_start, program));
            CREATE TABLE IF NOT EXISTS program_runs (
                program TEXT NOT NULL COLLATE NOCASE, start_ms INTEGER NOT NULL, end_ms INTEGER NOT NULL,
                cpu_kwh REAL NOT NULL, gpu_kwh REAL NOT NULL, active_seconds REAL NOT NULL, still_running INTEGER NOT NULL,
                PRIMARY KEY (program, start_ms));
            CREATE TABLE IF NOT EXISTS live_programs (
                program TEXT PRIMARY KEY COLLATE NOCASE, cpu_w REAL NOT NULL, gpu_w REAL NOT NULL, position INTEGER NOT NULL);
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
                INSERT INTO samples (minute_start, name, scope, kwh, seconds, peak_watts, kind)
                VALUES ($m, $n, $s, $k, $sec, $p, $kind)
                ON CONFLICT (minute_start, name) DO UPDATE SET
                    kwh = kwh + excluded.kwh, seconds = seconds + excluded.seconds,
                    peak_watts = MAX(peak_watts, excluded.peak_watts), kind = excluded.kind
                """;
            cmd.Parameters.AddWithValue("$kind", (int)r.Kind);
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

    /// <summary>Keeps the highest the sensors' total reached in a minute (the per-sensor peaks cannot give it: they do not
    /// all happen in the same second).</summary>
    public void AddTotalPeak(long minuteStart, double watts)
    {
        using var c = Open();
        using var cmd = c.CreateCommand();
        cmd.CommandText = """
            INSERT INTO total_peaks (minute_start, watts) VALUES ($m, $w)
            ON CONFLICT (minute_start) DO UPDATE SET watts = MAX(watts, excluded.watts)
            """;
        cmd.Parameters.AddWithValue("$m", minuteStart);
        cmd.Parameters.AddWithValue("$w", watts);
        cmd.ExecuteNonQuery();
    }

    public void AddGap(Gap gap)
    {
        using var c = Open();
        using var cmd = c.CreateCommand();
        cmd.CommandText = "INSERT OR REPLACE INTO gaps (start_ms, end_ms, reason, plug_kwh) VALUES ($s, $e, $r, $k)";
        cmd.Parameters.AddWithValue("$s", gap.Start.ToUnixTimeMilliseconds());
        cmd.Parameters.AddWithValue("$e", gap.End.ToUnixTimeMilliseconds());
        cmd.Parameters.AddWithValue("$r", (int)gap.Reason);
        cmd.Parameters.AddWithValue("$k", gap.PlugKwh is { } k ? k : DBNull.Value);
        cmd.ExecuteNonQuery();
    }

    /// <summary>Records the energy the plug's own counter measured across a gap that was saved without it.</summary>
    public void SetGapPlugEnergy(DateTimeOffset gapStart, double kwh)
    {
        using var c = Open();
        using var cmd = c.CreateCommand();
        cmd.CommandText = "UPDATE gaps SET plug_kwh = $k WHERE start_ms = $s";
        cmd.Parameters.AddWithValue("$k", kwh);
        cmd.Parameters.AddWithValue("$s", gapStart.ToUnixTimeMilliseconds());
        cmd.ExecuteNonQuery();
    }

    /// <summary>Replaces the live readings and records when the collector last saw the machine.</summary>
    /// <param name="wallCounter">The plug's energy counter as last read, kept so that after a restart the collector can still
    /// tell how much the plug counted while it was away. Left as it was when null.</param>
    public void SetLive(DateTimeOffset now, IReadOnlyList<PowerReading> readings, IReadOnlyList<string> notes, WallCounter? wallCounter = null)
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
        if (wallCounter is not null)
            SetMeta(c, tx, WallCounterKey, string.Create(CultureInfo.InvariantCulture,
                $"{wallCounter.Host}|{wallCounter.TotalWh:R}|{wallCounter.ReadAt.ToUnixTimeMilliseconds()}"));
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

    /// <summary>The plug's energy counter as last saved by <see cref="SetLive"/>, or null if none was.</summary>
    public WallCounter? GetWallCounter()
    {
        using var c = Open();
        var parts = GetMeta(c, WallCounterKey)?.Split('|');
        return parts is { Length: 3 }
            && double.TryParse(parts[1], NumberStyles.Float, CultureInfo.InvariantCulture, out var wh)
            && long.TryParse(parts[2], out var ms)
            ? new WallCounter(parts[0], wh, DateTimeOffset.FromUnixTimeMilliseconds(ms))
            : null;
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
        // The whole PC also used power while nothing was sampling; where the plug's own counter knew how much, it counts.
        if (scope == ReadingScope.Wall) kwh += GapPlugKwh(c, from, to);
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
            cmd.CommandText = "SELECT start_ms, end_ms, reason, plug_kwh FROM gaps WHERE end_ms > $f AND start_ms < $t ORDER BY start_ms";
            cmd.Parameters.AddWithValue("$f", from.ToUnixTimeMilliseconds());
            cmd.Parameters.AddWithValue("$t", to.ToUnixTimeMilliseconds());
            using var r = cmd.ExecuteReader();
            while (r.Read())
            {
                var start = DateTimeOffset.FromUnixTimeMilliseconds(r.GetInt64(0));
                var end = DateTimeOffset.FromUnixTimeMilliseconds(r.GetInt64(1));
                var clippedStart = start < from ? from : start;
                var clippedEnd = end > to ? to : end;
                double? plug = r.IsDBNull(3) ? null : r.GetDouble(3) * ShareInside(start, end, from, to);
                gaps.Add(new Gap(clippedStart, clippedEnd, (GapReason)r.GetInt32(2), plug));
            }
        }

        return (kwh, TimeSpan.FromSeconds(monitored), gaps);
    }

    // ----- Peak -------------------------------------------------------------------------

    /// <summary>The highest power between two instants: of the whole PC at the wall for <see cref="ReadingScope.Wall"/>, otherwise of
    /// the sensors' total (CPU, GPU and memory together). Null when nothing was recorded. Highest of the once-a-second readings, so a
    /// spike shorter than that is not seen.</summary>
    public PeakWatts? QueryPeak(DateTimeOffset from, DateTimeOffset to, ReadingScope scope = ReadingScope.Component)
    {
        using var c = Open();
        var f = from.ToUnixTimeSeconds() / 60 * 60;
        var t = to.ToUnixTimeSeconds();

        PeakWatts? Top(string sql, bool exact)
        {
            using var cmd = c.CreateCommand();
            cmd.CommandText = sql;
            cmd.Parameters.AddWithValue("$scope", (int)scope);
            cmd.Parameters.AddWithValue("$f", f);
            cmd.Parameters.AddWithValue("$t", t);
            using var r = cmd.ExecuteReader();
            return r.Read() && !r.IsDBNull(1) ? new PeakWatts(r.GetDouble(1), DateTimeOffset.FromUnixTimeSeconds(r.GetInt64(0)), exact) : null;
        }

        if (scope == ReadingScope.Wall)
            return Top("""
                SELECT minute_start, MAX(peak_watts) FROM samples
                WHERE scope = $scope AND minute_start >= $f AND minute_start < $t
                GROUP BY minute_start ORDER BY 2 DESC LIMIT 1
                """, exact: true);

        var recorded = Top("""
            SELECT minute_start, watts FROM total_peaks
            WHERE minute_start >= $f AND minute_start < $t
            ORDER BY watts DESC LIMIT 1
            """, exact: true);
        // Minutes saved before the total's peak was tracked: the sum of the sensors' own peaks is the most they can have reached.
        var bound = Top("""
            SELECT minute_start, SUM(peak_watts) FROM samples
            WHERE scope = $scope AND minute_start >= $f AND minute_start < $t
              AND minute_start NOT IN (SELECT minute_start FROM total_peaks)
            GROUP BY minute_start ORDER BY 2 DESC LIMIT 1
            """, exact: false);
        return bound is null ? recorded : recorded is null || bound.Watts > recorded.Watts ? bound : recorded;
    }

    // ----- Totals -----------------------------------------------------------------------

    /// <summary>Energy of one kind of reading between two instants: just the sum, so it stays quick over a year of minutes.</summary>
    public double EnergyKwh(DateTimeOffset from, DateTimeOffset to, ReadingScope scope = ReadingScope.Component)
    {
        using var c = Open();
        using var cmd = c.CreateCommand();
        cmd.CommandText = "SELECT COALESCE(SUM(kwh), 0) FROM samples WHERE scope = $scope AND minute_start >= $f AND minute_start < $t";
        cmd.Parameters.AddWithValue("$scope", (int)scope);
        cmd.Parameters.AddWithValue("$f", from.ToUnixTimeSeconds() / 60 * 60);
        cmd.Parameters.AddWithValue("$t", to.ToUnixTimeSeconds());
        var kwh = Convert.ToDouble(cmd.ExecuteScalar());
        return scope == ReadingScope.Wall ? kwh + GapPlugKwh(c, from, to) : kwh;
    }

    // ----- Extra meters -----------------------------------------------------------------

    /// <summary>What each extra meter recorded between two instants, biggest first. Only what was sampled: unlike the wall meter,
    /// nothing is filled in for the time the PC was asleep or off.</summary>
    public IReadOnlyList<PeripheralUse> QueryPeripherals(DateTimeOffset from, DateTimeOffset to)
    {
        using var c = Open();
        using var cmd = c.CreateCommand();
        cmd.CommandText = """
            SELECT name, SUM(kwh), MAX(peak_watts) FROM samples
            WHERE scope = 3 AND minute_start >= $f AND minute_start < $t
            GROUP BY name ORDER BY SUM(kwh) DESC
            """;
        cmd.Parameters.AddWithValue("$f", from.ToUnixTimeSeconds() / 60 * 60);
        cmd.Parameters.AddWithValue("$t", to.ToUnixTimeSeconds());
        var rows = new List<PeripheralUse>();
        using var r = cmd.ExecuteReader();
        while (r.Read()) rows.Add(new PeripheralUse(r.GetString(0), r.GetDouble(1), r.GetDouble(2)));
        return rows;
    }

    /// <summary>The energy the named extra meters recorded between two instants. No names is no energy.</summary>
    public double PeripheralKwh(DateTimeOffset from, DateTimeOffset to, IReadOnlyCollection<string> names)
    {
        if (names.Count == 0) return 0;
        using var c = Open();
        using var cmd = c.CreateCommand();
        cmd.CommandText = $"SELECT COALESCE(SUM(kwh), 0) FROM samples WHERE scope = 3 AND minute_start >= $f AND minute_start < $t AND {NameIn(cmd, names)}";
        cmd.Parameters.AddWithValue("$f", from.ToUnixTimeSeconds() / 60 * 60);
        cmd.Parameters.AddWithValue("$t", to.ToUnixTimeSeconds());
        return Convert.ToDouble(cmd.ExecuteScalar());
    }

    /// <summary>The most the named extra meters could have drawn together: per minute, the sum of each one's own peak, which
    /// they never all reach in the same second, so it is only an upper bound. Null when none of them recorded anything.</summary>
    public PeakWatts? QueryPeripheralPeak(DateTimeOffset from, DateTimeOffset to, IReadOnlyCollection<string> names)
    {
        if (names.Count == 0) return null;
        using var c = Open();
        using var cmd = c.CreateCommand();
        cmd.CommandText = $"""
            SELECT minute_start, SUM(peak_watts) FROM samples
            WHERE scope = 3 AND minute_start >= $f AND minute_start < $t AND {NameIn(cmd, names)}
            GROUP BY minute_start ORDER BY 2 DESC LIMIT 1
            """;
        cmd.Parameters.AddWithValue("$f", from.ToUnixTimeSeconds() / 60 * 60);
        cmd.Parameters.AddWithValue("$t", to.ToUnixTimeSeconds());
        using var r = cmd.ExecuteReader();
        return r.Read() && !r.IsDBNull(1)
            ? new PeakWatts(r.GetDouble(1), DateTimeOffset.FromUnixTimeSeconds(r.GetInt64(0)), Exact: names.Count == 1)
            : null;
    }

    /// <summary>"name IN ($n0, $n1, ...)" with the names bound as parameters, never pasted into the text.</summary>
    private static string NameIn(SqliteCommand cmd, IReadOnlyCollection<string> names)
    {
        var markers = new List<string>();
        foreach (var name in names)
        {
            var marker = "$n" + markers.Count;
            markers.Add(marker);
            cmd.Parameters.AddWithValue(marker, name);
        }
        return $"name IN ({string.Join(", ", markers)})";
    }

    /// <summary>The whole-PC energy between two instants that the plug's own counter measured across gaps (time nothing was
    /// sampling: asleep, off, or lost power). Each gap's energy is spread evenly over its length, so a period that cuts a gap
    /// gets its share. This is part of <see cref="ReadingScope.Wall"/> energy, never of the sensors'.</summary>
    public double PlugGapKwh(DateTimeOffset from, DateTimeOffset to)
    {
        using var c = Open();
        return GapPlugKwh(c, from, to);
    }

    private static double GapPlugKwh(SqliteConnection c, DateTimeOffset from, DateTimeOffset to)
    {
        using var cmd = c.CreateCommand();
        cmd.CommandText = "SELECT start_ms, end_ms, plug_kwh FROM gaps WHERE plug_kwh IS NOT NULL AND end_ms > $f AND start_ms < $t";
        cmd.Parameters.AddWithValue("$f", from.ToUnixTimeMilliseconds());
        cmd.Parameters.AddWithValue("$t", to.ToUnixTimeMilliseconds());
        using var r = cmd.ExecuteReader();
        var total = 0.0;
        while (r.Read())
            total += r.GetDouble(2) * ShareInside(
                DateTimeOffset.FromUnixTimeMilliseconds(r.GetInt64(0)), DateTimeOffset.FromUnixTimeMilliseconds(r.GetInt64(1)), from, to);
        return total;
    }

    /// <summary>The fraction of a gap that lies inside a period.</summary>
    private static double ShareInside(DateTimeOffset start, DateTimeOffset end, DateTimeOffset from, DateTimeOffset to)
    {
        var length = (end - start).TotalMilliseconds;
        if (length <= 0) return 0;
        var inside = ((end < to ? end : to) - (start > from ? start : from)).TotalMilliseconds;
        return Math.Clamp(inside / length, 0, 1);
    }

    /// <summary>When the earliest saved reading is (of one kind, or of any), or null if nothing was recorded.</summary>
    public DateTimeOffset? FirstRecorded(ReadingScope? scope = null)
    {
        using var c = Open();
        using var cmd = c.CreateCommand();
        cmd.CommandText = scope is null ? "SELECT MIN(minute_start) FROM samples" : "SELECT MIN(minute_start) FROM samples WHERE scope = $scope";
        if (scope is { } s) cmd.Parameters.AddWithValue("$scope", (int)s);
        return cmd.ExecuteScalar() is long seconds ? DateTimeOffset.FromUnixTimeSeconds(seconds) : null;
    }

    // ----- Chart series -----------------------------------------------------------------

    /// <summary>The minutes of one chart series between two instants, oldest first. A series can be several saved
    /// readings (two GPUs): their energy and peaks add, and the time covered is the longest any of them was sampled.</summary>
    /// <param name="peripherals">For <see cref="ChartSeriesKind.Peripheral"/>, which extra meters to add up (none gives an empty series).</param>
    public IReadOnlyList<SeriesMinute> QuerySeries(DateTimeOffset from, DateTimeOffset to, ChartSeriesKind kind,
        IReadOnlyCollection<string>? peripherals = null)
    {
        using var c = Open();
        using var cmd = c.CreateCommand();

        // Fixed fragments chosen by the enum, never text from outside.
        var filter = kind switch
        {
            ChartSeriesKind.Wall => "scope = 2",
            ChartSeriesKind.Peripheral => peripherals is { Count: > 0 } ? $"scope = 3 AND {NameIn(cmd, peripherals)}" : "0",
            ChartSeriesKind.Gpu => "scope = 0 AND kind = 2",
            // Rows saved before the kind column existed have kind 0: the CPU is still found by its name.
            _ => "scope = 0 AND (kind = 1 OR (kind = 0 AND name LIKE 'CPU package%'))",
        };

        cmd.CommandText = $"""
            SELECT minute_start, SUM(kwh), MAX(seconds), SUM(peak_watts) FROM samples
            WHERE {filter} AND minute_start >= $f AND minute_start < $t
            GROUP BY minute_start ORDER BY minute_start
            """;
        cmd.Parameters.AddWithValue("$f", from.ToUnixTimeSeconds() / 60 * 60);
        cmd.Parameters.AddWithValue("$t", to.ToUnixTimeSeconds());
        var rows = new List<SeriesMinute>();
        using var r = cmd.ExecuteReader();
        while (r.Read()) rows.Add(new SeriesMinute(r.GetInt64(0), r.GetDouble(1), r.GetDouble(2), r.GetDouble(3)));
        return rows;
    }

    // ----- Per-program estimates --------------------------------------------------------

    /// <summary>Adds estimated energy per program for one minute (adding, so saving a minute in pieces is fine).</summary>
    public void AddProgramMinutes(long minuteStart, IEnumerable<ProgramEnergy> rows)
    {
        using var c = Open();
        using var tx = c.BeginTransaction();
        foreach (var r in rows)
        {
            using var cmd = c.CreateCommand();
            cmd.Transaction = tx;
            cmd.CommandText = """
                INSERT INTO program_samples (minute_start, program, cpu_kwh, gpu_kwh, active_seconds)
                VALUES ($m, $p, $c, $g, $a)
                ON CONFLICT (minute_start, program) DO UPDATE SET
                    cpu_kwh = cpu_kwh + excluded.cpu_kwh, gpu_kwh = gpu_kwh + excluded.gpu_kwh,
                    active_seconds = active_seconds + excluded.active_seconds
                """;
            cmd.Parameters.AddWithValue("$m", minuteStart);
            cmd.Parameters.AddWithValue("$p", r.Program);
            cmd.Parameters.AddWithValue("$c", r.CpuKwh);
            cmd.Parameters.AddWithValue("$g", r.GpuKwh);
            cmd.Parameters.AddWithValue("$a", r.ActiveSeconds);
            cmd.ExecuteNonQuery();
        }
        tx.Commit();
    }

    /// <summary>Estimated energy per program over a period, biggest first. Program names are matched ignoring case.</summary>
    public IReadOnlyList<ProgramEnergy> QueryPrograms(DateTimeOffset from, DateTimeOffset to)
    {
        using var c = Open();
        using var cmd = c.CreateCommand();
        cmd.CommandText = """
            SELECT program, SUM(cpu_kwh), SUM(gpu_kwh), SUM(active_seconds) FROM program_samples
            WHERE minute_start >= $f AND minute_start < $t
            GROUP BY program ORDER BY SUM(cpu_kwh) + SUM(gpu_kwh) DESC
            """;
        cmd.Parameters.AddWithValue("$f", from.ToUnixTimeSeconds() / 60 * 60);
        cmd.Parameters.AddWithValue("$t", to.ToUnixTimeSeconds());
        var rows = new List<ProgramEnergy>();
        using var r = cmd.ExecuteReader();
        while (r.Read()) rows.Add(new ProgramEnergy(r.GetString(0), r.GetDouble(1), r.GetDouble(2), r.GetDouble(3)));
        return rows;
    }

    /// <summary>Saves a run, replacing what was saved for it before (a run still going is saved again as it grows).</summary>
    public void SaveRun(ProgramRun run)
    {
        using var c = Open();
        using var cmd = c.CreateCommand();
        cmd.CommandText = """
            INSERT OR REPLACE INTO program_runs (program, start_ms, end_ms, cpu_kwh, gpu_kwh, active_seconds, still_running)
            VALUES ($p, $s, $e, $c, $g, $a, $r)
            """;
        cmd.Parameters.AddWithValue("$p", run.Program);
        cmd.Parameters.AddWithValue("$s", run.Start.ToUnixTimeMilliseconds());
        cmd.Parameters.AddWithValue("$e", run.End.ToUnixTimeMilliseconds());
        cmd.Parameters.AddWithValue("$c", run.CpuKwh);
        cmd.Parameters.AddWithValue("$g", run.GpuKwh);
        cmd.Parameters.AddWithValue("$a", run.ActiveSeconds);
        cmd.Parameters.AddWithValue("$r", run.StillRunning ? 1 : 0);
        cmd.ExecuteNonQuery();
    }

    /// <summary>Runs left marked "still running" by a collector that is no longer there were cut off with it.</summary>
    public void EndStaleRuns()
    {
        using var c = Open();
        using var cmd = c.CreateCommand();
        cmd.CommandText = "UPDATE program_runs SET still_running = 0 WHERE still_running = 1";
        cmd.ExecuteNonQuery();
    }

    /// <summary>The most recent runs, ones still going first.</summary>
    public IReadOnlyList<ProgramRun> GetRecentRuns(int limit)
    {
        using var c = Open();
        using var cmd = c.CreateCommand();
        cmd.CommandText = """
            SELECT program, start_ms, end_ms, cpu_kwh, gpu_kwh, active_seconds, still_running FROM program_runs
            ORDER BY still_running DESC, end_ms DESC LIMIT $n
            """;
        cmd.Parameters.AddWithValue("$n", limit);
        var runs = new List<ProgramRun>();
        using var r = cmd.ExecuteReader();
        while (r.Read())
            runs.Add(new ProgramRun(r.GetString(0), DateTimeOffset.FromUnixTimeMilliseconds(r.GetInt64(1)),
                DateTimeOffset.FromUnixTimeMilliseconds(r.GetInt64(2)), r.GetDouble(3), r.GetDouble(4), r.GetDouble(5), r.GetInt32(6) == 1));
        return runs;
    }

    public void SetLivePrograms(DateTimeOffset now, IReadOnlyList<ProgramLive> rows)
    {
        using var c = Open();
        using var tx = c.BeginTransaction();
        Run(c, tx, "DELETE FROM live_programs");
        for (var i = 0; i < rows.Count; i++)
        {
            using var cmd = c.CreateCommand();
            cmd.Transaction = tx;
            cmd.CommandText = "INSERT INTO live_programs (program, cpu_w, gpu_w, position) VALUES ($p, $c, $g, $i) ON CONFLICT (program) DO UPDATE SET cpu_w = excluded.cpu_w, gpu_w = excluded.gpu_w";
            cmd.Parameters.AddWithValue("$p", rows[i].Program);
            cmd.Parameters.AddWithValue("$c", rows[i].CpuWatts);
            cmd.Parameters.AddWithValue("$g", rows[i].GpuWatts);
            cmd.Parameters.AddWithValue("$i", i);
            cmd.ExecuteNonQuery();
        }
        SetMeta(c, tx, LivePrograms, now.ToUnixTimeMilliseconds().ToString());
        tx.Commit();
    }

    public (DateTimeOffset UpdatedAt, IReadOnlyList<ProgramLive> Rows)? GetLivePrograms()
    {
        using var c = Open();
        if (!long.TryParse(GetMeta(c, LivePrograms), out var ms)) return null;
        var rows = new List<ProgramLive>();
        using var cmd = c.CreateCommand();
        cmd.CommandText = "SELECT program, cpu_w, gpu_w FROM live_programs ORDER BY position";
        using var r = cmd.ExecuteReader();
        while (r.Read()) rows.Add(new ProgramLive(r.GetString(0), r.GetDouble(1), r.GetDouble(2)));
        return (DateTimeOffset.FromUnixTimeMilliseconds(ms), rows);
    }

    /// <summary>The CPU's and GPU's idle draw learned so far (null until a quiet window has been seen).</summary>
    public (double? Cpu, double? Gpu) GetIdleBaseline()
    {
        using var c = Open();
        static double? Parse(string? v) => double.TryParse(v, System.Globalization.NumberStyles.Float, System.Globalization.CultureInfo.InvariantCulture, out var d) ? d : null;
        return (Parse(GetMeta(c, IdleCpuKey)), Parse(GetMeta(c, IdleGpuKey)));
    }

    public void SetIdleBaseline(double? cpu, double? gpu)
    {
        using var c = Open();
        using var tx = c.BeginTransaction();
        if (cpu is { } a) SetMeta(c, tx, IdleCpuKey, a.ToString(System.Globalization.CultureInfo.InvariantCulture));
        if (gpu is { } b) SetMeta(c, tx, IdleGpuKey, b.ToString(System.Globalization.CultureInfo.InvariantCulture));
        tx.Commit();
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

    private static bool TableExists(SqliteConnection c, string table)
    {
        using var cmd = c.CreateCommand();
        cmd.CommandText = "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = $n";
        cmd.Parameters.AddWithValue("$n", table);
        return Convert.ToInt32(cmd.ExecuteScalar()) > 0;
    }

    private static bool HasColumn(SqliteConnection c, string table, string column)
    {
        using var cmd = c.CreateCommand();
        cmd.CommandText = $"SELECT COUNT(*) FROM pragma_table_info('{table}') WHERE name = $n";
        cmd.Parameters.AddWithValue("$n", column);
        return Convert.ToInt32(cmd.ExecuteScalar()) > 0;
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
