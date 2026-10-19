using System.Globalization;
using System.Text.Json;
using System.Text.RegularExpressions;

namespace KVGuzzler.Core.Carbon;

/// <summary>
/// Where the grid's carbon intensity comes from: an Electricity Maps API key and the place (a zone code such as <c>US-NW-PSCO</c>, or
/// latitude and longitude as <c>39.74, -104.99</c>). Kept in a small file in the machine-wide data folder, because the collector service
/// does the fetching and cannot read a user's settings.
/// </summary>
/// <remarks>The key is stored as plain text in that folder, readable by anyone who can read it. It is a free personal key that can only
/// read public grid data, and it is only ever sent to <see cref="ApiBase"/>.</remarks>
public sealed partial record CarbonConfig(string ApiKey, string Place)
{
    public const string FileName = "carbon.json";

    /// <summary>Fixed here, not in the file, so a key can never be sent anywhere else.</summary>
    public const string ApiBase = "https://api.electricitymap.org/v3";

    [GeneratedRegex(@"^[A-Za-z0-9_\-\.]{8,200}$")]
    private static partial Regex KeyPattern();

    [GeneratedRegex(@"^[A-Za-z0-9]{1,12}(-[A-Za-z0-9]{1,12}){0,4}$")]
    private static partial Regex ZonePattern();

    public static bool IsValidKey(string? key) => key is not null && KeyPattern().IsMatch(key);

    /// <summary>A zone code, or a latitude and longitude.</summary>
    public static bool IsValidPlace(string? place) => place is not null && (ZonePattern().IsMatch(place) || TryCoordinates(place, out _, out _));

    /// <summary>"39.74, -104.99" as a latitude and longitude, when it is one.</summary>
    public static bool TryCoordinates(string text, out double latitude, out double longitude)
    {
        latitude = longitude = 0;
        var parts = text.Split(',');
        return parts.Length == 2
            && double.TryParse(parts[0].Trim(), NumberStyles.Float, CultureInfo.InvariantCulture, out latitude)
            && double.TryParse(parts[1].Trim(), NumberStyles.Float, CultureInfo.InvariantCulture, out longitude)
            && latitude is >= -90 and <= 90 && longitude is >= -180 and <= 180;
    }

    /// <summary>Why this cannot be used, in words fit to show, or null when it can.</summary>
    public static string? Problem(string? key, string? place)
    {
        if (string.IsNullOrWhiteSpace(key)) return "Paste your Electricity Maps API key first.";
        if (!IsValidKey(key.Trim())) return "That does not look like an API key. Copy it whole from the Electricity Maps portal, with no spaces.";
        if (string.IsNullOrWhiteSpace(place)) return "Enter your zone (like US-NW-PSCO) or your latitude and longitude (like 39.74, -104.99).";
        if (!IsValidPlace(place.Trim())) return "Enter a zone code like US-NW-PSCO, or a latitude and longitude like 39.74, -104.99.";
        return null;
    }

    /// <summary>The address of the latest reading. The place is encoded, so nothing typed can change where the request goes.</summary>
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
            return config is not null && Problem(config.ApiKey, config.Place) is null ? config with { ApiKey = config.ApiKey.Trim(), Place = config.Place.Trim() } : null;
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
