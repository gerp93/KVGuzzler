namespace KVGuzzler.Core;

public static class CostCalculator
{
    /// <summary>Energy in kWh for a constant load held for a duration.</summary>
    public static double KilowattHours(double watts, TimeSpan duration) =>
        watts / 1000.0 * duration.TotalHours;

    /// <summary>Cost of an amount of energy at a flat price per kWh.</summary>
    public static decimal Cost(double kilowattHours, decimal ratePerKwh) =>
        (decimal)kilowattHours * ratePerKwh;

    /// <summary>The estimate shown in Settings next to the rate field, so a mistyped rate
    /// (15 instead of 0.15) is obvious.</summary>
    public static decimal CostOfConstantLoad(double watts, TimeSpan duration, decimal ratePerKwh) =>
        Cost(KilowattHours(watts, duration), ratePerKwh);
}
