using System.Text.Json;

namespace KVGuzzler.Core.Billing;

/// <summary>
/// A utility's published electricity price, reduced to what KVGuzzler can use: the price of a kWh for each month, for weekdays and
/// weekends, for each hour of the day. The energy charge only (the first tier's rate plus its adjustment and the month's fuel
/// adjustment); fixed monthly charges and demand charges are not part of a price per kWh, as with a bill's flat fees.
/// </summary>
/// <param name="Label">The rate's page label in the OpenEI Utility Rate Database, which finds the same rate again later.</param>
/// <param name="Grid">12 months x weekday/weekend x 24 hours of dollars per kWh, month-major: index <c>(month * 2 + (weekend ? 1 : 0)) * 24 + hour</c>.</param>
/// <param name="Periods">How many time-of-use periods the tariff has (1 is a flat rate by time of day).</param>
/// <param name="Tiers">The most usage tiers any period has. Tiers depend on a whole house's monthly usage, which a PC's share cannot tell, so
/// the first tier is used and the Settings page says so when there is more than one.</param>
public sealed record Tariff(string Label, string Utility, string Name, string? Uri, DateTimeOffset FetchedAt, IReadOnlyList<double> Grid,
    int Periods, int Tiers, string? FixedCharge = null)
{
    public const int GridSize = 12 * 2 * 24;

    public static int Index(int month0, bool weekend, int hour) => (month0 * 2 + (weekend ? 1 : 0)) * 24 + hour;

    public double Price(int month0, bool weekend, int hour) => Grid[Index(month0, weekend, hour)];

    public double Cheapest => Grid.Min();
    public double Dearest => Grid.Max();

    /// <summary>True when the price changes with the hour or the season, so the hours used matter.</summary>
    public bool VariesByTime => Grid.Max() - Grid.Min() > 1e-9;

    /// <summary>A line for a list: who, what, and what it costs.</summary>
    public string Summary(string currency = "$") =>
        $"{Utility}: {Name} · " + (VariesByTime
            ? $"{currency}{Cheapest:0.0000} to {currency}{Dearest:0.0000} per kWh, {Periods} time-of-use period{(Periods == 1 ? "" : "s")}"
            : $"{currency}{Cheapest:0.0000} per kWh");

    public string ToJson() => JsonSerializer.Serialize(this);

    /// <summary>A saved tariff, or null when the text is empty, damaged or not a usable grid.</summary>
    public static Tariff? FromJson(string? json)
    {
        if (string.IsNullOrWhiteSpace(json)) return null;
        try
        {
            var t = JsonSerializer.Deserialize<Tariff>(json);
            return t is { Grid.Count: GridSize } && t.Grid.All(p => double.IsFinite(p) && p >= 0) ? t : null;
        }
        catch (JsonException)
        {
            return null;
        }
    }
}

/// <summary>
/// When the electricity is used: the share of usage in each hour of a weekday and of a weekend day, learned from the saved history. It
/// turns a time-of-use tariff into one price for a day (weighted by when you really use power), which is what the rest of the app
/// prices with.
/// </summary>
public sealed record UsageProfile(IReadOnlyList<double> Weekday, IReadOnlyList<double> Weekend)
{
    public static UsageProfile Uniform { get; } = new(Enumerable.Repeat(1.0 / 24, 24).ToArray(), Enumerable.Repeat(1.0 / 24, 24).ToArray());

    /// <summary>The profile of some hourly energy: (the unix second an hour starts, its kWh). A kind of day with no usage is spread evenly.</summary>
    public static UsageProfile FromHourly(IEnumerable<(long Hour, double Kwh)> hours, TimeZoneInfo zone)
    {
        var weekday = new double[24];
        var weekend = new double[24];
        foreach (var (hour, kwh) in hours)
        {
            if (!(kwh > 0) || !double.IsFinite(kwh)) continue;
            var local = TimeZoneInfo.ConvertTime(DateTimeOffset.FromUnixTimeSeconds(hour), zone);
            (local.DayOfWeek is DayOfWeek.Saturday or DayOfWeek.Sunday ? weekend : weekday)[local.Hour] += kwh;
        }
        return new UsageProfile(Normalised(weekday), Normalised(weekend));
    }

    private static double[] Normalised(double[] energy)
    {
        var total = energy.Sum();
        return total > 0 ? energy.Select(e => e / total).ToArray() : Enumerable.Repeat(1.0 / 24, 24).ToArray();
    }

    public string ToJson() => JsonSerializer.Serialize(this);

    public static UsageProfile? FromJson(string? json)
    {
        if (string.IsNullOrWhiteSpace(json)) return null;
        try
        {
            var p = JsonSerializer.Deserialize<UsageProfile>(json);
            return p is { Weekday.Count: 24, Weekend.Count: 24 } && p.Weekday.Concat(p.Weekend).All(w => double.IsFinite(w) && w >= 0) ? p : null;
        }
        catch (JsonException)
        {
            return null;
        }
    }
}
