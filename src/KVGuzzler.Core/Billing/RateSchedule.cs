namespace KVGuzzler.Core.Billing;

public enum RateMode
{
    /// <summary>One price per kWh, used for everything.</summary>
    OneRate,

    /// <summary>Each bill gives the price for the dates it covers.</summary>
    FromBills,

    /// <summary>A utility's published tariff, looked up from OpenEI, gives the price of each hour; a day's price is that, weighted by
    /// the hours the power is really used.</summary>
    Lookup,
}

/// <summary>A stretch of time with one price. <see cref="Rate"/> is null when no price is known at all.</summary>
public sealed record RateSegment(DateTimeOffset From, DateTimeOffset To, decimal? Rate, bool Estimate);

/// <summary>The cost of some energy, how much energy there was, and whether any part was priced by estimate or not priced.</summary>
public sealed record RatedCost(decimal Cost, double Kwh, bool Estimated, bool Unpriced);

/// <summary>
/// Answers "what did a kWh cost on this day?". With one rate that is always the same. With bills, a day inside a bill
/// has that bill's price; a day after the latest bill, or in a gap between bills, uses the most recent bill's price
/// as an estimate (it is corrected the moment the next bill is entered); a day before the first bill uses the saved
/// single rate, or the first bill's price if there is none.
/// </summary>
public sealed class RateSchedule
{
    private readonly TimeZoneInfo _zone;

    /// <param name="tariff">The looked-up tariff, used in <see cref="RateMode.Lookup"/>. Without one, that mode falls back on the single rate.</param>
    /// <param name="profile">When the power is used, to weight a time-of-use tariff into one price per day; even through the day when null.</param>
    public RateSchedule(RateMode mode, decimal? flatRate, IEnumerable<Bill> bills, TimeZoneInfo? zone = null, Tariff? tariff = null,
        UsageProfile? profile = null)
    {
        Mode = mode;
        FlatRate = flatRate;
        Bills = bills.OrderBy(b => b.Start).ToList();
        _zone = zone ?? TimeZoneInfo.Local;
        Tariff = tariff;
        Profile = profile ?? UsageProfile.Uniform;
    }

    public RateMode Mode { get; }
    public decimal? FlatRate { get; }
    public IReadOnlyList<Bill> Bills { get; }
    public Tariff? Tariff { get; }
    public UsageProfile Profile { get; }

    private bool UsesBills => Mode == RateMode.FromBills && Bills.Count > 0;
    private bool UsesTariff => Mode == RateMode.Lookup && Tariff is not null;

    /// <summary>The price of a day under the looked-up tariff: each hour's price times the share of the day's usage that falls in that hour.</summary>
    private decimal TariffRate(DateOnly day)
    {
        var weekend = day.DayOfWeek is DayOfWeek.Saturday or DayOfWeek.Sunday;
        var share = weekend ? Profile.Weekend : Profile.Weekday;
        var price = 0.0;
        for (var hour = 0; hour < 24; hour++) price += share[hour] * Tariff!.Price(day.Month - 1, weekend, hour);
        return (decimal)price;
    }

    public decimal? RateAt(DateOnly day)
    {
        if (UsesTariff) return TariffRate(day);
        if (!UsesBills) return FlatRate;

        if (Bills.FirstOrDefault(b => b.Start <= day && day <= b.End) is { } inside) return inside.PricePerKwh;
        if (Bills.LastOrDefault(b => b.End < day) is { } before) return before.PricePerKwh;
        return FlatRate ?? Bills[0].PricePerKwh;   // a day before the first bill
    }

    /// <summary>True when the price for this day is a stand-in rather than the price of a bill that covers it.</summary>
    public bool IsEstimate(DateOnly day) =>
        UsesBills && !Bills.Any(b => b.Start <= day && day <= b.End);

    /// <summary>The instant a local calendar day begins.</summary>
    public DateTimeOffset StartOf(DateOnly day)
    {
        var local = day.ToDateTime(TimeOnly.MinValue);
        return new DateTimeOffset(local, _zone.GetUtcOffset(local));
    }

    private DateOnly DayOf(DateTimeOffset t) => DateOnly.FromDateTime(TimeZoneInfo.ConvertTime(t, _zone).DateTime);

    /// <summary>Splits a period at local midnights into stretches of one price each (neighbours with the same price are joined).</summary>
    public IReadOnlyList<RateSegment> Segments(DateTimeOffset from, DateTimeOffset to)
    {
        var segments = new List<RateSegment>();
        if (to <= from) return segments;

        for (var day = DayOf(from); StartOf(day) < to; day = day.AddDays(1))
        {
            var start = StartOf(day) < from ? from : StartOf(day);
            var end = StartOf(day.AddDays(1)) > to ? to : StartOf(day.AddDays(1));
            if (end <= start) continue;

            var rate = RateAt(day);
            var estimate = IsEstimate(day);
            if (segments.Count > 0 && segments[^1].Rate == rate && segments[^1].Estimate == estimate)
                segments[^1] = segments[^1] with { To = end };
            else
                segments.Add(new RateSegment(start, end, rate, estimate));
        }
        return segments;
    }

    /// <summary>Prices the energy in a period: each stretch's energy times that stretch's price.</summary>
    /// <param name="kwhBetween">The energy used between two instants (read from the history).</param>
    public RatedCost Cost(Func<DateTimeOffset, DateTimeOffset, double> kwhBetween, DateTimeOffset from, DateTimeOffset to)
    {
        decimal cost = 0;
        double kwh = 0;
        bool estimated = false, unpriced = false;
        foreach (var s in Segments(from, to))
        {
            var energy = kwhBetween(s.From, s.To);
            kwh += energy;
            if (s.Rate is not { } rate) { if (energy > 0) unpriced = true; continue; }
            cost += CostCalculator.Cost(energy, rate);
            estimated |= s.Estimate && energy > 0;
        }
        return new RatedCost(cost, kwh, estimated, unpriced);
    }

    /// <summary>The price over a period weighted by time, for pricing many small amounts (a program's share) with one number.</summary>
    public decimal? AverageRate(DateTimeOffset from, DateTimeOffset to)
    {
        double weighted = 0, seconds = 0;
        foreach (var s in Segments(from, to))
        {
            if (s.Rate is not { } rate) continue;
            var length = (s.To - s.From).TotalSeconds;
            weighted += (double)rate * length;
            seconds += length;
        }
        return seconds > 0 ? (decimal)(weighted / seconds) : RateAt(DayOf(to));
    }

    /// <summary>The instants a bill's dates cover: from the start of its first day to the end of its last.</summary>
    public (DateTimeOffset From, DateTimeOffset To) Span(Bill bill) => (StartOf(bill.Start), StartOf(bill.End.AddDays(1)));
}
