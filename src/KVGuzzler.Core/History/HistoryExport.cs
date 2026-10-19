using System.Globalization;
using System.Text;
using KVGuzzler.Core.Billing;
using KVGuzzler.Core.Power;

namespace KVGuzzler.Core.History;

/// <summary>Writes the saved history out as a spreadsheet-ready CSV, one row per local calendar day.</summary>
public static class HistoryExport
{
    /// <summary>
    /// One row per local day between two instants: the whole PC at the wall (when the wall meter recorded; it includes time the PC slept
    /// or was off where the plug's counter knew), what the CPU, GPU and memory sensors measured, then a column for each extra meter that
    /// recorded anything, and the day's cost of the PC and of the extra meters at the price on that day (blank when no price is known).
    /// Numbers use a dot and no thousands separator whatever the computer's language, so any spreadsheet reads them.
    /// </summary>
    /// <param name="schedule">The prices, or null for no cost columns' figures.</param>
    /// <param name="timeZone">Decides where each day starts; the local zone in the app.</param>
    public static string DailyCsv(HistoryStore store, RateSchedule? schedule, DateTimeOffset from, DateTimeOffset to, TimeZoneInfo timeZone)
    {
        var meters = store.QueryPeripherals(from, to).Select(u => u.Name).ToList();
        var hasWall = store.EnergyKwh(from, to, ReadingScope.Wall) > 0;

        var csv = new StringBuilder();
        var header = new List<string> { "date", "whole_pc_kwh", "sensors_kwh" };
        header.AddRange(meters.Select(m => m + " (kwh)"));
        header.AddRange(new[] { "pc_cost", "peripherals_cost" });
        csv.AppendLine(string.Join(",", header.Select(Cell)));

        var day = DateOnly.FromDateTime(TimeZoneInfo.ConvertTime(from, timeZone).DateTime);
        var last = DateOnly.FromDateTime(TimeZoneInfo.ConvertTime(to, timeZone).DateTime);
        for (; day <= last; day = day.AddDays(1))
        {
            var start = StartOf(day, timeZone);
            var end = StartOf(day.AddDays(1), timeZone);
            var a = start < from ? from : start;
            var b = end > to ? to : end;
            if (b <= a) continue;

            var wall = hasWall ? store.EnergyKwh(a, b, ReadingScope.Wall) : (double?)null;
            var sensors = store.EnergyKwh(a, b, ReadingScope.Component);
            var perMeter = meters.Select(m => store.PeripheralKwh(a, b, new[] { m })).ToList();

            var rate = schedule?.RateAt(day);
            string Cost(double kwh) => rate is { } r ? CostCalculator.Cost(kwh, r).ToString("0.0000", CultureInfo.InvariantCulture) : "";

            var row = new List<string> { day.ToString("yyyy-MM-dd", CultureInfo.InvariantCulture), wall is { } w ? Number(w) : "", Number(sensors) };
            row.AddRange(perMeter.Select(Number));
            row.Add(Cost(wall ?? sensors));
            row.Add(Cost(perMeter.Sum()));
            csv.AppendLine(string.Join(",", row.Select(Cell)));
        }
        return csv.ToString();
    }

    private static DateTimeOffset StartOf(DateOnly day, TimeZoneInfo zone)
    {
        var local = day.ToDateTime(TimeOnly.MinValue);
        // A day that starts in a clock change (a missing hour at midnight) starts at the first real time after it.
        while (zone.IsInvalidTime(local)) local = local.AddMinutes(30);
        return new DateTimeOffset(local, zone.GetUtcOffset(local));
    }

    private static string Number(double v) => v.ToString("0.######", CultureInfo.InvariantCulture);

    /// <summary>A CSV cell, quoted when it has a comma, quote or line break. A name that a spreadsheet would run as a formula
    /// (starting with = + - or @) gets a leading apostrophe, so a meter label can never do anything but show up as text.</summary>
    internal static string Cell(string text)
    {
        if (text.Length > 0 && text[0] is '=' or '+' or '@' || text.Length > 1 && text[0] == '-' && !double.TryParse(text, NumberStyles.Float, CultureInfo.InvariantCulture, out _))
            text = "'" + text;
        return text.IndexOfAny(new[] { ',', '"', '\n', '\r' }) >= 0 ? "\"" + text.Replace("\"", "\"\"") + "\"" : text;
    }
}
