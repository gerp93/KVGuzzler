using System.Globalization;
using System.Text.Json;
using System.Text.RegularExpressions;

namespace KVGuzzler.Core.Carbon;

/// <summary>Where the grid's carbon intensity comes from.</summary>
public static class CarbonSources
{
    /// <summary>Electricity Maps: a ready-made intensity for a zone. Needs a key for the zone, from a free personal tier.</summary>
    public const string ElectricityMaps = "electricitymaps";

    /// <summary>The US Energy Information Administration's hourly generation by fuel for a balancing authority (like <c>MISO</c>), turned into an
    /// intensity here. Free with a registration key; US only.</summary>
    public const string Eia = "eia";

    public static string Name(string source) => source == Eia ? "EIA" : "Electricity Maps";
}

/// <summary>
/// Where the grid's carbon intensity comes from and how to ask: a source, an API key, and the place (for Electricity Maps a zone code such as
/// <c>US-MIDW-MISO</c> or latitude and longitude as <c>44.98, -93.27</c>; for EIA a balancing authority code such as <c>MISO</c>). Kept in a
/// small file in the machine-wide data folder, because the collector service does the fetching and cannot read a user's settings.
/// </summary>
/// <remarks>The key is stored as plain text in that folder, readable by anyone who can read it. It is a free personal key that can only
/// read public grid data, and it is only ever sent to the address of its own source (<see cref="ApiBase"/> or <see cref="EiaClient.ApiBase"/>).</remarks>
public sealed partial record CarbonConfig(string ApiKey, string Place)
{
    public const string FileName = "carbon.json";

    /// <summary>Electricity Maps' address. Fixed here, not in the file, so a key can never be sent anywhere else.</summary>
    public const string ApiBase = "https://api.electricitymaps.com/v4";

    /// <summary>Which source the key belongs to. Files saved before there was a choice have none, and are Electricity Maps.</summary>
    public string Source { get; init; } = CarbonSources.ElectricityMaps;

    /// <summary>Any visible character, no spaces or control characters: the key goes in a header or a query, so those are the only things to keep out.</summary>
    [GeneratedRegex(@"^[!-~]{8,200}$")]
    private static partial Regex KeyPattern();

    [GeneratedRegex(@"^[A-Za-z0-9]{1,12}(-[A-Za-z0-9]{1,12}){0,4}$")]
    private static partial Regex ZonePattern();

    [GeneratedRegex(@"^[A-Z0-9]{2,10}$")]
    private static partial Regex AuthorityPattern();

    public static bool IsValidKey(string? key) => key is not null && KeyPattern().IsMatch(key);

    /// <summary>A zone code or a latitude and longitude (Electricity Maps), or a balancing authority code (EIA).</summary>
    public static bool IsValidPlace(string? place, string source = CarbonSources.ElectricityMaps) =>
        place is not null && (source == CarbonSources.Eia
            ? AuthorityPattern().IsMatch(place)
            : ZonePattern().IsMatch(place) || TryCoordinates(place, out _, out _));

    /// <summary>"44.98, -93.27" as a latitude and longitude, when it is one.</summary>
    public static bool TryCoordinates(string text, out double latitude, out double longitude)
    {
        latitude = longitude = 0;
        var parts = text.Split(',');
        return parts.Length == 2
            && double.TryParse(parts[0].Trim(), NumberStyles.Float, CultureInfo.InvariantCulture, out latitude)
            && double.TryParse(parts[1].Trim(), NumberStyles.Float, CultureInfo.InvariantCulture, out longitude)
            && latitude is >= -90 and <= 90 && longitude is >= -180 and <= 180;
    }

    /// <summary>The place as it is used: a balancing authority is upper case whatever was typed.</summary>
    public static string Normalised(string place, string source) => source == CarbonSources.Eia ? place.Trim().ToUpperInvariant() : place.Trim();

    /// <summary>Why this cannot be used, in words fit to show, or null when it can.</summary>
    public static string? Problem(string? key, string? place, string source = CarbonSources.ElectricityMaps)
    {
        var eia = source == CarbonSources.Eia;
        if (string.IsNullOrWhiteSpace(key)) return $"Paste your {CarbonSources.Name(source)} API key first.";
        if (!IsValidKey(key.Trim())) return "That does not look like an API key. Copy it whole from the portal, with no spaces.";
        if (string.IsNullOrWhiteSpace(place))
            return eia ? "Enter your balancing authority's code, like MISO (Minnesota), PJM, ERCO (Texas), CISO (California) or PSCO (Colorado)."
                : "Enter your zone (like US-MIDW-MISO) or your latitude and longitude (like 44.98, -93.27).";
        if (!IsValidPlace(Normalised(place, source), source))
            return eia ? "Enter just the balancing authority's code, like MISO: letters and digits only."
                : "Enter a zone code like US-MIDW-MISO, or a latitude and longitude like 44.98, -93.27.";
        return null;
    }

    /// <summary>The address of Electricity Maps' latest reading. The place is encoded, so nothing typed can change where the request goes.</summary>
    public Uri LatestUri() =>
        TryCoordinates(Place, out var lat, out var lon)
            ? new Uri(FormattableString.Invariant($"{ApiBase}/carbon-intensity/latest?lat={lat}&lon={lon}"))
            : new Uri($"{ApiBase}/carbon-intensity/latest?zone={Uri.EscapeDataString(Place)}");

    /// <summary>The saved setup, or null when none is set up or the file is unreadable or not valid.</summary>
    public static CarbonConfig? Load(string path)
    {
        try
        {
            if (!File.Exists(path)) return null;
            var config = JsonSerializer.Deserialize<CarbonConfig>(File.ReadAllText(path), Options);
            if (config is null || config.Source is not (CarbonSources.ElectricityMaps or CarbonSources.Eia)) return null;
            return Problem(config.ApiKey, config.Place, config.Source) is null
                ? config with { ApiKey = config.ApiKey.Trim(), Place = Normalised(config.Place, config.Source) }
                : null;
        }
        catch (Exception e) when (e is IOException or UnauthorizedAccessException or JsonException)
        {
            return null;
        }
    }

    public void Save(string path)
    {
        Directory.CreateDirectory(Path.GetDirectoryName(Path.GetFullPath(path))!);
        File.WriteAllText(path, JsonSerializer.Serialize(this, Options));
    }

    public static void Remove(string path)
    {
        if (File.Exists(path)) File.Delete(path);
    }

    private static readonly JsonSerializerOptions Options = new() { WriteIndented = true, PropertyNamingPolicy = JsonNamingPolicy.CamelCase };
}
