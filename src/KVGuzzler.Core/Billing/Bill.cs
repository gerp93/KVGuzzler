namespace KVGuzzler.Core.Billing;

/// <summary>A charge on a bill that does not depend on how much electricity was used (the basic service charge,
/// a monthly credit). Left out when working out the price per kWh. A credit is a negative amount.</summary>
public sealed record FixedFee(string Label, decimal Amount);

/// <summary>
/// One electricity bill: the dates it covers, what it totalled, the kWh it billed, and any flat fees to leave out.
/// The price per kWh is what is left after those fees, divided by the kWh: with no fees it is simply the total
/// divided by the kWh, the all-in price.
/// </summary>
/// <param name="Id">Zero until the bill is stored.</param>
/// <param name="Start">First day covered (inclusive), a calendar date in local time.</param>
/// <param name="End">Last day covered (inclusive).</param>
public sealed record Bill(int Id, DateOnly Start, DateOnly End, decimal Total, double Kwh, IReadOnlyList<FixedFee> FixedFees)
{
    public decimal FixedTotal => FixedFees.Sum(f => f.Amount);

    /// <summary>What the bill charged for the electricity itself, after the flat fees are taken out.</summary>
    public decimal Variable => Total - FixedTotal;

    public decimal PricePerKwh => Kwh > 0 ? Variable / (decimal)Kwh : 0;

    public int Days => End.DayNumber - Start.DayNumber + 1;
}

public static class BillValidator
{
    /// <summary>The longest a single bill can cover. Bills are about a month; this catches a year typed wrongly.</summary>
    public const int MaxDays = 366;

    /// <summary>Why a bill cannot be saved, in words fit to show, or null if it is fine. Past bills are welcome in any
    /// order; what is refused is dates in the wrong order and two bills claiming the same day (it would be priced twice).</summary>
    public static string? Validate(Bill bill, IEnumerable<Bill> existing)
    {
        if (bill.End < bill.Start) return "The end date is before the start date.";
        if (bill.Days > MaxDays) return "That covers more than a year. A bill usually covers about a month.";
        if (!(bill.Kwh > 0) || double.IsInfinity(bill.Kwh)) return "Enter the kWh the bill charged for (more than 0).";
        if (bill.Total <= 0) return "Enter the bill's total (more than 0).";
        if (bill.FixedFees.Any(f => f.Label.Length > 60)) return "A flat fee's name is too long (60 characters at most).";
        if (bill.Variable <= 0) return "After leaving out the flat fees nothing is left to price. Check the fees and the total.";

        var clash = existing.FirstOrDefault(b => b.Id != bill.Id && b.Start <= bill.End && bill.Start <= b.End);
        if (clash is not null)
            return $"These dates overlap another bill ({clash.Start:yyyy-MM-dd} to {clash.End:yyyy-MM-dd}). Each day can only be on one bill.";
        return null;
    }
}
