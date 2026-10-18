using System.Globalization;

namespace KVGuzzler.Core;

/// <summary>The flat electricity price the user enters, with input validation.</summary>
public static class Rate
{
    /// <summary>Above this per kWh the number is almost certainly cents typed as dollars.</summary>
    public const decimal SuspiciouslyHigh = 1.00m;

    public static bool TryParse(string? text, out decimal rate)
    {
        rate = 0;
        if (string.IsNullOrWhiteSpace(text)) return false;
        // Plain decimals only (no thousands separators): with NumberStyles.Number, "0.15" would
        // parse as 15 where '.' is the group separator (de-DE). Accept the user's own decimal
        // separator first, then a plain '.'.
        const NumberStyles style = NumberStyles.AllowDecimalPoint;
        return (decimal.TryParse(text.Trim(), style, CultureInfo.CurrentCulture, out rate)
                || decimal.TryParse(text.Trim(), style, CultureInfo.InvariantCulture, out rate))
               && rate >= 0;
    }
}
