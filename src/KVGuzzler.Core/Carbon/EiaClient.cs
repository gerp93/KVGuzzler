using System.Globalization;
using System.Text.Json;

namespace KVGuzzler.Core.Carbon;

/// <summary>
/// Reads the US Energy Information Administration's hourly electricity generation by fuel for one balancing authority and turns each hour
/// into a grid carbon intensity. A plain GET with the free registration key as a query parameter, as EIA documents; nothing else is sent but the
/// authority's code, and only to <see cref="ApiBase"/>.
/// </summary>
/// <remarks>
/// The figure is the generation-weighted average of each fuel's life-cycle emissions (IPCC medians, the same kind of figure Electricity Maps
/// publishes): what the authority's own power stations emit per kWh they produce. It leaves out what is imported from neighbouring grids, and
/// storage (batteries, pumped hydro), which only moves energy that was generated elsewhere. EIA's data runs an hour or more behind, and a
/// request returns about the last two and a half days, so missing recent hours are filled in as they are reported.
/// </remarks>
public static class EiaClient
{
    public const string ApiBase = "https://api.eia.gov/v2/electricity/rto/fuel-type-data/data/";

    public static readonly TimeSpan Timeout = TimeSpan.FromSeconds(30);

    /// <summary>How many rows to ask for: about nine fuels an hour, so roughly the last two and a half days.</summary>
    public const int Rows = 600;

    /// <summary>An hour reported with fewer fuels than this is a partial report, and its mix would mislead.</summary>
    private const int MinimumFuels = 3;

    /// <summary>Grams of CO2 equivalent per kWh over a power station's life, for each of EIA's fuel codes. IPCC medians (coal 820, gas 490,
    /// biomass 230, geothermal 38, hydro 24, nuclear 12, solar 45, wind 11); oil is 650. "Other" (waste, biomass and the like) is taken as biomass.
    /// Storage and unknown sources are left out.</summary>
    public static readonly IReadOnlyDictionary<string, double> GramsPerKwh = new Dictionary<string, double>(StringComparer.OrdinalIgnoreCase)
    {
        ["COL"] = 820, ["NG"] = 490, ["OIL"] = 650, ["NUC"] = 12, ["SUN"] = 45, ["SNB"] = 45, ["WND"] = 11, ["WAT"] = 24,
        ["GEO"] = 38, ["BIO"] = 230, ["OTH"] = 230,
    };

    public static Uri RequestUri(CarbonConfig config) => new(ApiBase
        + $"?api_key={Uri.EscapeDataString(config.ApiKey)}&frequency=hourly&data%5B0%5D=value"
        + $"&facets%5Brespondent%5D%5B%5D={Uri.EscapeDataString(config.Place)}"
        + $"&sort%5B0%5D%5Bcolumn%5D=period&sort%5B0%5D%5Bdirection%5D=desc&length={Rows}");

    /// <summary>The intensity of each complete hour in an answer, oldest first. Hours reported with too few fuels, or with no generation, are left out.</summary>
    public static IReadOnlyList<CarbonReading> Parse(string json)
    {
        var hours = new Dictionary<DateTimeOffset, List<(string Fuel, double Mwh)>>();
        try
        {
            using var doc = JsonDocument.Parse(json);
            if (doc.RootElement.ValueKind != JsonValueKind.Object) throw new CarbonException(NotData);
            if (!doc.RootElement.TryGetProperty("response", out var response) || response.ValueKind != JsonValueKind.Object
                || !response.TryGetProperty("data", out var data) || data.ValueKind != JsonValueKind.Array)
                throw new CarbonException(NotData);

            foreach (var row in data.EnumerateArray())
            {
                if (row.ValueKind != JsonValueKind.Object) continue;
                if (!row.TryGetProperty("period", out var p) || p.ValueKind != JsonValueKind.String
                    || !DateTimeOffset.TryParseExact(p.GetString(), "yyyy-MM-dd'T'HH", CultureInfo.InvariantCulture, DateTimeStyles.AssumeUniversal | DateTimeStyles.AdjustToUniversal, out var hour))
                    continue;
                if (!row.TryGetProperty("fueltype", out var f) || f.ValueKind != JsonValueKind.String || f.GetString() is not { Length: > 0 } fuel) continue;
                if (!row.TryGetProperty("value", out var v)) continue;
                // EIA writes its numbers as text.
                double mwh = 0;
                var ok = false;
                if (v.ValueKind == JsonValueKind.String) ok = double.TryParse(v.GetString(), NumberStyles.Float, CultureInfo.InvariantCulture, out mwh);
                else if (v.ValueKind == JsonValueKind.Number) ok = v.TryGetDouble(out mwh);
                if (!ok || !double.IsFinite(mwh)) continue;

                if (!hours.TryGetValue(hour, out var list)) hours[hour] = list = new List<(string, double)>();
                list.Add((fuel, mwh));
            }
        }
        catch (JsonException)
        {
            throw new CarbonException(NotData);
        }

        var readings = new List<CarbonReading>();
        foreach (var (hour, fuels) in hours.OrderBy(h => h.Key))
            if (fuels.Count >= MinimumFuels && Intensity(fuels) is { } grams)
                readings.Add(new CarbonReading(grams, hour, IsEstimated: false));
        return readings;
    }

    private const string NotData = "EIA answered, but not with generation data. Check the balancing authority's code, or try again later.";

    /// <summary>Grams per kWh of one hour's generation by fuel (code, MWh), or null when nothing that counts was generated. Each fuel is weighted
    /// by what it generated; a negative figure (a data glitch) counts as none, and storage and unknown fuels are left out.</summary>
    public static double? Intensity(IEnumerable<(string Fuel, double Mwh)> fuels)
    {
        double total = 0, grams = 0;
        foreach (var (fuel, mwh) in fuels)
        {
            if (!GramsPerKwh.TryGetValue(fuel, out var factor) || !(mwh > 0)) continue;
            total += mwh;
            grams += mwh * factor;
        }
        return total > 0 ? grams / total : null;
    }

    /// <summary>The recent hours' intensity for a balancing authority, oldest first. Throws <see cref="CarbonException"/> with a message fit to show.</summary>
    public static async Task<IReadOnlyList<CarbonReading>> ReadAsync(HttpClient http, CarbonConfig config, CancellationToken cancel = default,
        TimeSpan? timeoutAfter = null)
    {
        var limit = timeoutAfter ?? Timeout;
        try
        {
            using var timeout = CancellationTokenSource.CreateLinkedTokenSource(cancel);
            timeout.CancelAfter(limit);
            using var response = await http.GetAsync(RequestUri(config), timeout.Token);
            var status = (int)response.StatusCode;
            var body = await response.Content.ReadAsStringAsync(timeout.Token);

            if (status is >= 200 and < 300)
            {
                var readings = Parse(body);
                return readings.Count > 0
                    ? readings
                    : throw new CarbonException($"EIA has no recent generation data for \"{config.Place}\". Check the code (MISO, PJM, ERCO, CISO...) or try again later.");
            }

            var why = ServiceMessage(body, config.ApiKey) is { } m ? $" EIA says: {m}" : "";
            throw status switch
            {
                400 => new CarbonException("EIA did not accept the request. Check the balancing authority's code." + why),
                401 or 403 => new CarbonException($"EIA did not accept the key (error {status}). Check it was copied whole." + why),
                404 => new CarbonException("EIA does not know that address." + why),
                429 => new CarbonException("EIA asked us to slow down. It will be tried again later." + why, rateLimited: true),
                _ => new CarbonException($"EIA answered with an error ({status})." + why),
            };
        }
        catch (OperationCanceledException) when (!cancel.IsCancellationRequested)
        {
            throw new CarbonException($"EIA did not answer within {limit.TotalSeconds:0.#} seconds. Is this computer online?");
        }
        catch (HttpRequestException e)
        {
            throw new CarbonException($"Could not reach EIA: {e.Message}", inner: e);
        }
    }

    /// <summary>EIA's own words for an error (<c>error.message</c>, or <c>error</c> as text), cut short, with any trace of the key taken out.</summary>
    internal static string? ServiceMessage(string body, string apiKey)
    {
        try
        {
            using var doc = JsonDocument.Parse(body);
            if (doc.RootElement.ValueKind != JsonValueKind.Object || !doc.RootElement.TryGetProperty("error", out var error)) return null;
            var text = error.ValueKind switch
            {
                JsonValueKind.String => error.GetString(),
                JsonValueKind.Object when error.TryGetProperty("message", out var m) && m.ValueKind == JsonValueKind.String => m.GetString(),
                _ => null,
            };
            if (string.IsNullOrWhiteSpace(text)) return null;
            text = new string(text.Replace(apiKey, "(key)", StringComparison.Ordinal).Where(c => !char.IsControl(c)).ToArray()).Trim();
            return text.Length > 200 ? text[..200] + "..." : text;
        }
        catch (JsonException)
        {
            return null;
        }
    }
}
