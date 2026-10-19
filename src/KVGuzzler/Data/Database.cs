using System.Globalization;
using KVGuzzler.Core.Billing;
using Microsoft.Data.Sqlite;

namespace KVGuzzler.Data;

public static class SettingKeys
{
    public const string RatePerKwh = "rate_per_kwh";
    public const string Currency = "currency_symbol";
    public const string Theme = "theme_id";
    /// <summary>Watts a sleeping PC is assumed to draw, for estimating the time the collector saw it asleep.</summary>
    public const string SleepWatts = "sleep_watts";
    /// <summary>"one" (a single price per kWh) or "bills" (the price comes from each bill's dates).</summary>
    public const string RateMode = "rate_mode";
    /// <summary>Which period the two big numbers at the top of the Dashboard show (an index; 5 is "All time").</summary>
    public const string StatsPeriod = "stats_period";
    /// <summary>The extra meters (monitor, speakers) the user switched off in the Dashboard's "Also count" filter, one label per line.
    /// Kept as the ones left out, so a meter added later counts until it is switched off.</summary>
    public const string PeripheralsOff = "peripherals_off";
    /// <summary>"1" when the PC itself is unticked in the Dashboard's "Count" row, so only the extra meters are totalled.
    /// Only applies while there are extra meters.</summary>
    public const string PcOff = "pc_off";
    /// <summary>The OpenEI API key used to look up the utility's rates (saved once a search worked; never shown again).</summary>
    public const string OpenEiKey = "openei_key";
    /// <summary>The chosen looked-up tariff, as <see cref="Core.Billing.Tariff"/> JSON.</summary>
    public const string RateLookupTariff = "rate_lookup_tariff";
    /// <summary>When the hours-of-use profile was last worked out, and the profile itself (<see cref="Core.Billing.UsageProfile"/> JSON).</summary>
    public const string RateLookupProfileAt = "rate_lookup_profile_at";
    public const string RateLookupProfile = "rate_lookup_profile";
}

/// <summary>The app's SQLite file, in WAL mode. Short-lived pooled connections per call.</summary>
public sealed class Database : IDisposable
{
    private readonly string _connectionString;

    public Database(string path)
    {
        Directory.CreateDirectory(Path.GetDirectoryName(Path.GetFullPath(path))!);
        _connectionString = new SqliteConnectionStringBuilder { DataSource = path }.ToString();
        using var c = Open();
        Exec(c, "PRAGMA journal_mode=WAL;");
        Exec(c, "CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);");
        // Bills: the dates they cover, the total and the kWh. Amounts are kept as text so a price is never a rounded float.
        Exec(c, """
            CREATE TABLE IF NOT EXISTS bills (
                id INTEGER PRIMARY KEY AUTOINCREMENT, start_day TEXT NOT NULL, end_day TEXT NOT NULL,
                total TEXT NOT NULL, kwh REAL NOT NULL);
            CREATE TABLE IF NOT EXISTS bill_fees (
                bill_id INTEGER NOT NULL REFERENCES bills(id) ON DELETE CASCADE, position INTEGER NOT NULL,
                label TEXT NOT NULL, amount TEXT NOT NULL, PRIMARY KEY (bill_id, position));
            """);
    }

    public string? GetSetting(string key)
    {
        using var c = Open();
        using var cmd = c.CreateCommand();
        cmd.CommandText = "SELECT value FROM settings WHERE key = $k";
        cmd.Parameters.AddWithValue("$k", key);
        return cmd.ExecuteScalar() as string;
    }

    public void SetSetting(string key, string value)
    {
        using var c = Open();
        using var cmd = c.CreateCommand();
        cmd.CommandText = "INSERT INTO settings(key, value) VALUES ($k, $v) ON CONFLICT(key) DO UPDATE SET value = excluded.value";
        cmd.Parameters.AddWithValue("$k", key);
        cmd.Parameters.AddWithValue("$v", value);
        cmd.ExecuteNonQuery();
    }

    // ----- Bills ------------------------------------------------------------------------

    /// <summary>Every saved bill, oldest first, with its flat fees (zero or more) in the order they were entered.</summary>
    public IReadOnlyList<Bill> GetBills()
    {
        using var c = Open();
        var fees = new Dictionary<int, List<FixedFee>>();
        using (var cmd = c.CreateCommand())
        {
            cmd.CommandText = "SELECT bill_id, label, amount FROM bill_fees ORDER BY bill_id, position";
            using var r = cmd.ExecuteReader();
            while (r.Read())
            {
                var id = r.GetInt32(0);
                if (!fees.TryGetValue(id, out var list)) fees[id] = list = new List<FixedFee>();
                list.Add(new FixedFee(r.GetString(1), decimal.Parse(r.GetString(2), CultureInfo.InvariantCulture)));
            }
        }

        var bills = new List<Bill>();
        using (var cmd = c.CreateCommand())
        {
            cmd.CommandText = "SELECT id, start_day, end_day, total, kwh FROM bills ORDER BY start_day";
            using var r = cmd.ExecuteReader();
            while (r.Read())
            {
                var id = r.GetInt32(0);
                bills.Add(new Bill(id, DateOnly.ParseExact(r.GetString(1), "yyyy-MM-dd", CultureInfo.InvariantCulture),
                    DateOnly.ParseExact(r.GetString(2), "yyyy-MM-dd", CultureInfo.InvariantCulture),
                    decimal.Parse(r.GetString(3), CultureInfo.InvariantCulture), r.GetDouble(4),
                    fees.TryGetValue(id, out var f) ? f : new List<FixedFee>()));
            }
        }
        return bills;
    }

    /// <summary>Adds a bill (Id 0) or replaces the one with that Id, fees included. Returns its Id. The caller has already
    /// checked it with <see cref="BillValidator"/>.</summary>
    public int SaveBill(Bill bill)
    {
        using var c = Open();
        using var tx = c.BeginTransaction();
        int id;
        using (var cmd = c.CreateCommand())
        {
            cmd.Transaction = tx;
            cmd.Parameters.AddWithValue("$s", bill.Start.ToString("yyyy-MM-dd", CultureInfo.InvariantCulture));
            cmd.Parameters.AddWithValue("$e", bill.End.ToString("yyyy-MM-dd", CultureInfo.InvariantCulture));
            cmd.Parameters.AddWithValue("$t", bill.Total.ToString(CultureInfo.InvariantCulture));
            cmd.Parameters.AddWithValue("$k", bill.Kwh);
            if (bill.Id == 0)
            {
                cmd.CommandText = "INSERT INTO bills (start_day, end_day, total, kwh) VALUES ($s, $e, $t, $k); SELECT last_insert_rowid();";
                id = Convert.ToInt32(cmd.ExecuteScalar());
            }
            else
            {
                cmd.CommandText = "UPDATE bills SET start_day = $s, end_day = $e, total = $t, kwh = $k WHERE id = $id";
                cmd.Parameters.AddWithValue("$id", bill.Id);
                if (cmd.ExecuteNonQuery() == 0) throw new InvalidOperationException("That bill no longer exists.");
                id = bill.Id;
            }
        }

        Run(c, tx, "DELETE FROM bill_fees WHERE bill_id = " + id.ToString(CultureInfo.InvariantCulture));
        for (var i = 0; i < bill.FixedFees.Count; i++)
        {
            using var cmd = c.CreateCommand();
            cmd.Transaction = tx;
            cmd.CommandText = "INSERT INTO bill_fees (bill_id, position, label, amount) VALUES ($b, $p, $l, $a)";
            cmd.Parameters.AddWithValue("$b", id);
            cmd.Parameters.AddWithValue("$p", i);
            cmd.Parameters.AddWithValue("$l", bill.FixedFees[i].Label);
            cmd.Parameters.AddWithValue("$a", bill.FixedFees[i].Amount.ToString(CultureInfo.InvariantCulture));
            cmd.ExecuteNonQuery();
        }
        tx.Commit();
        return id;
    }

    public void DeleteBill(int id)
    {
        using var c = Open();
        using var tx = c.BeginTransaction();
        Run(c, tx, "DELETE FROM bill_fees WHERE bill_id = " + id.ToString(CultureInfo.InvariantCulture));
        Run(c, tx, "DELETE FROM bills WHERE id = " + id.ToString(CultureInfo.InvariantCulture));
        tx.Commit();
    }

    /// <summary>The prices to use for each day: one saved rate, the bills, or the looked-up tariff, as the user chose.</summary>
    public RateSchedule LoadRateSchedule()
    {
        var mode = GetSetting(SettingKeys.RateMode) switch
        {
            "bills" => Core.Billing.RateMode.FromBills,
            "lookup" => Core.Billing.RateMode.Lookup,
            _ => Core.Billing.RateMode.OneRate,
        };
        decimal? flat = decimal.TryParse(GetSetting(SettingKeys.RatePerKwh), NumberStyles.AllowDecimalPoint, CultureInfo.InvariantCulture, out var r) ? r : null;
        return new RateSchedule(mode, flat, GetBills(), null, Core.Billing.Tariff.FromJson(GetSetting(SettingKeys.RateLookupTariff)),
            Core.Billing.UsageProfile.FromJson(GetSetting(SettingKeys.RateLookupProfile)));
    }

    /// <summary>Release the file. Required before relocating it: the connection pool keeps it open,
    /// and in WAL mode recent writes stay in the -wal file until the last connection closes.</summary>
    public void Dispose() => SqliteConnection.ClearAllPools();

    private SqliteConnection Open()
    {
        var c = new SqliteConnection(_connectionString);
        c.Open();
        return c;
    }

    private static void Run(SqliteConnection c, SqliteTransaction tx, string sql)
    {
        using var cmd = c.CreateCommand();
        cmd.Transaction = tx;
        cmd.CommandText = sql;
        cmd.ExecuteNonQuery();
    }

    private static void Exec(SqliteConnection c, string sql)
    {
        using var cmd = c.CreateCommand();
        cmd.CommandText = sql;
        cmd.ExecuteNonQuery();
    }
}
