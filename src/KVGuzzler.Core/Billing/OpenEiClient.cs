using System.Globalization;
using System.Text.Json;

namespace KVGuzzler.Core.Billing;

/// <summary>A problem reaching or understanding OpenEI, worded so it can be shown to the user as is.</summary>
public sealed class RateLookupException : Exception
{
    public RateLookupException(string message, Exception? inner = null) : base(message, inner) { }
}

/// <summary>One rate found for a place, ready to be chosen.</summary>
/// <param name="IsDefault">OpenEI marks it as the most common rate for its sector and service type.</param>
/// <param name="Approved">An OpenEI expert has checked it.</param>
public sealed record TariffCandidate(Tariff Tariff, bool IsDefault, bool Approved);

/// <summary>Finds the electricity rates that apply to a place. Replaceable, so the window can be tested without the network.</summary>
public interface IRateLookup
{
    /// <summary>The residential rates in force now around an address or ZIP code, the likeliest first. Empty when none was usable.</summary>
    Task<IReadOnlyList<TariffCandidate>> FindAsync(string apiKey, string address, DateTimeOffset now, CancellationToken cancel = default);

    /// <summary>The current version of one rate by its label, or null when OpenEI no longer has it.</summary>
    Task<TariffCandidate?> GetAsync(string apiKey, string label, DateTimeOffset now, CancellationToken cancel = default);
}

/// <summary>
/// Reads electricity rates from the OpenEI Utility Rate Database (<c>GET https://api.openei.org/utility_rates</c>), a public collection of
/// utilities' published tariffs. A plain GET with the free API key as a query parameter, as that service documents; nothing else is sent
/// but the address typed or the rate's label.
/// </summary>
/// <remarks>Written from OpenEI's documented field list and tested with made-up answers; not yet checked against the real service. The
/// answer's shape (a month by hour schedule pointing at tiered periods) is reduced to a <see cref="Tariff"/>; a rate that cannot be reduced
/// (a demand-based or unit-less structure, a missing schedule) is left out rather than guessed at.</remarks>
public sealed class OpenEiClient : IRateLookup
{
    public const string Endpoint = "https://api.openei.org/utility_rates";

    /// <summary>A "full detail" answer is big and OpenEI warns it can be slow.</summary>
    public static readonly TimeSpan Timeout = TimeSpan.FromSeconds(60);

    /// <summary>How many rates one search asks for. A page of full detail is large, so this stays modest.</summary>
    public const int SearchLimit = 60;

    private readonly HttpClient _http;

    public OpenEiClient(HttpClient? http = null) => _http = http ?? new HttpClient();

    /// <summary>The address of a search. The key and everything typed are encoded, so nothing can change where the request goes.</summary>
    public static Uri SearchUri(string apiKey, string address, DateTimeOffset now) => new(Endpoint
        + $"?version=latest&format=json&detail=full&sector=Residential&limit={SearchLimit}"
        + $"&effective_on_date={now.ToUnixTimeSeconds()}"
        + $"&address={Uri.EscapeDataString(address)}&api_key={Uri.EscapeDataString(apiKey)}");

    public static Uri LabelUri(string apiKey, string label) => new(Endpoint
        + $"?version=latest&format=json&detail=full&limit=1&getpage={Uri.EscapeDataString(label)}&api_key={Uri.EscapeDataString(apiKey)}");

    public async Task<IReadOnlyList<TariffCandidate>> FindAsync(string apiKey, string address, DateTimeOffset now, CancellationToken cancel = default) =>
        ParseRates(await GetJsonAsync(SearchUri(apiKey, address, now), cancel), now)
            .OrderByDescending(c => c.IsDefault).ThenByDescending(c => c.Approved).ThenBy(c => c.Tariff.Utility).ThenBy(c => c.Tariff.Name)
            .ToList();

    public async Task<TariffCandidate?> GetAsync(string apiKey, string label, DateTimeOffset now, CancellationToken cancel = default) =>
        ParseRates(await GetJsonAsync(LabelUri(apiKey, label), cancel), now, requireCurrent: false)
            .FirstOrDefault(c => string.Equals(c.Tariff.Label, label, StringComparison.OrdinalIgnoreCase));

    private async Task<string> GetJsonAsync(Uri uri, CancellationToken cancel)
    {
        try
        {
            using var timeout = CancellationTokenSource.CreateLinkedTokenSource(cancel);
            timeout.CancelAfter(Timeout);
            using var response = await _http.GetAsync(uri, timeout.Token);
            return (int)response.StatusCode switch
            {
                >= 200 and < 300 => await response.Content.ReadAsStringAsync(timeout.Token),
                400 => throw new RateLookupException("OpenEI did not accept the request. Check the address or ZIP code and the API key."),
                401 or 403 => throw new RateLookupException("OpenEI did not accept the API key. Check it was copied whole."),
                429 => throw new RateLookupException("OpenEI asked us to slow down. Try again in a little while."),
                var code => throw new RateLookupException($"OpenEI answered with an error ({code}). It can be slow or busy: try again later."),
            };
        }
        catch (OperationCanceledException) when (!cancel.IsCancellationRequested)
        {
            throw new RateLookupException($"OpenEI did not answer within {Timeout.TotalSeconds:0} seconds. It can be slow: try again, or try a ZIP code instead of a full address.");
        }
        catch (HttpRequestException e)
        {
            throw new RateLookupException($"Could not reach OpenEI: {e.Message}", e);
        }
    }

    /// <summary>The usable residential rates in an answer. Rates that are not in force now, are delivery-only, or cannot be turned into a price per
    /// kWh by hour are left out.</summary>
    /// <param name="requireCurrent">False when asking for one known rate: it is used even if its dates have passed.</param>
    public static IReadOnlyList<TariffCandidate> ParseRates(string json, DateTimeOffset now, bool requireCurrent = true)
    {
        var found = new List<TariffCandidate>();
        try
        {
            using var doc = JsonDocument.Parse(json);
            if (doc.RootElement.ValueKind != JsonValueKind.Object) throw new RateLookupException(NotRates);
            if (doc.RootElement.TryGetProperty("error", out var error))
                throw new RateLookupException("OpenEI said: " + (error.ValueKind == JsonValueKind.String ? error.GetString() : error.ToString()));
            if (!doc.RootElement.TryGetProperty("items", out var items) || items.ValueKind != JsonValueKind.Array) throw new RateLookupException(NotRates);

            foreach (var item in items.EnumerateArray())
                if (item.ValueKind == JsonValueKind.Object && TryRate(item, now, requireCurrent) is { } candidate)
                    found.Add(candidate);
        }
        catch (JsonException)
        {
            throw new RateLookupException(NotRates);
        }
        return found;
    }

    private const string NotRates = "OpenEI answered, but not with a list of rates. Try again later.";

    private static TariffCandidate? TryRate(JsonElement item, DateTimeOffset now, bool requireCurrent)
    {
        var label = Text(item, "label");
        if (label is null) return null;

        var sector = Text(item, "sector");
        if (sector is not null && !sector.Equals("Residential", StringComparison.OrdinalIgnoreCase)) return null;
        if (Text(item, "servicetype") is { } service && service.StartsWith("Delivery", StringComparison.OrdinalIgnoreCase)) return null;   // no energy price in it
        if (requireCurrent && (Number(item, "startdate") is { } start && start > now.ToUnixTimeSeconds()
                               || Number(item, "enddate") is { } end && end <= now.ToUnixTimeSeconds())) return null;

        if (!item.TryGetProperty("energyratestructure", out var structure) || structure.ValueKind != JsonValueKind.Array || structure.GetArrayLength() == 0) return null;

        // Each period's first tier: its price per kWh. A tier whose unit is not plain kWh (a demand-based "kWh/kW") has no price per kWh to use.
        var periods = new List<double>();
        var tiers = 0;
        foreach (var period in structure.EnumerateArray())
        {
            if (period.ValueKind != JsonValueKind.Array || period.GetArrayLength() == 0) return null;
            tiers = Math.Max(tiers, period.GetArrayLength());
            var tier = period[0];
            if (tier.ValueKind != JsonValueKind.Object) return null;
            if (Text(tier, "unit") is { } unit && (!unit.Contains("kWh", StringComparison.OrdinalIgnoreCase) || unit.Contains("/kW", StringComparison.OrdinalIgnoreCase))) return null;
            var rate = Number(tier, "rate");
            var adj = Number(tier, "adj");
            if (rate is null && adj is null) return null;
            periods.Add((rate ?? 0) + (adj ?? 0));
        }

        var weekday = Schedule(item, "energyweekdayschedule", periods.Count) ?? (periods.Count == 1 ? Flat(periods.Count) : null);
        if (weekday is null) return null;
        var weekend = Schedule(item, "energyweekendschedule", periods.Count) ?? weekday;

        var fuel = new double[12];
        if (item.TryGetProperty("fueladjustmentsmonthly", out var adjustments) && adjustments.ValueKind == JsonValueKind.Array && adjustments.GetArrayLength() == 12)
        {
            var i = 0;
            foreach (var a in adjustments.EnumerateArray()) fuel[i++] = a.ValueKind == JsonValueKind.Number && a.TryGetDouble(out var d) && double.IsFinite(d) ? d : 0;
        }

        var grid = new double[Tariff.GridSize];
        for (var m = 0; m < 12; m++)
            for (var h = 0; h < 24; h++)
            {
                grid[Tariff.Index(m, false, h)] = periods[weekday[m][h]] + fuel[m];
                grid[Tariff.Index(m, true, h)] = periods[weekend[m][h]] + fuel[m];
            }
        // A price below zero or beyond anything a household pays is a mistake in the data, not a tariff.
        if (grid.Any(p => !double.IsFinite(p) || p < 0 || p > 10)) return null;

        string? fixedCharge = Number(item, "fixedchargefirstmeter") is { } fc && fc > 0
            ? $"{fc.ToString("0.##", CultureInfo.InvariantCulture)} {Text(item, "fixedchargeunits") ?? ""}".Trim() : null;
        var tariff = new Tariff(label, Text(item, "utility") ?? "Unknown utility", Text(item, "name") ?? label, Text(item, "uri"), now, grid,
            periods.Count, tiers, fixedCharge);
        return new TariffCandidate(tariff, Bool(item, "is_default"), Bool(item, "approved"));
    }

    private static int[][] Flat(int periods) => Enumerable.Range(0, 12).Select(_ => new int[24]).ToArray();

    /// <summary>A 12 by 24 table of period numbers, or null when it is missing or points at a period that does not exist.</summary>
    private static int[][]? Schedule(JsonElement item, string name, int periods)
    {
        if (!item.TryGetProperty(name, out var table) || table.ValueKind != JsonValueKind.Array || table.GetArrayLength() != 12) return null;
        var result = new int[12][];
        var m = 0;
        foreach (var month in table.EnumerateArray())
        {
            if (month.ValueKind != JsonValueKind.Array || month.GetArrayLength() != 24) return null;
            result[m] = new int[24];
            var h = 0;
            foreach (var cell in month.EnumerateArray())
            {
                if (cell.ValueKind != JsonValueKind.Number || !cell.TryGetInt32(out var period) || period < 0 || period >= periods) return null;
                result[m][h++] = period;
            }
            m++;
        }
        return result;
    }

    private static string? Text(JsonElement e, string name) =>
        e.TryGetProperty(name, out var v) && v.ValueKind == JsonValueKind.String && !string.IsNullOrWhiteSpace(v.GetString()) ? v.GetString() : null;

    /// <summary>A number, whether the service wrote it as a number or as text.</summary>
    private static double? Number(JsonElement e, string name)
    {
        if (!e.TryGetProperty(name, out var v)) return null;
        double d = 0;
        var ok = false;
        if (v.ValueKind == JsonValueKind.Number) ok = v.TryGetDouble(out d);
        else if (v.ValueKind == JsonValueKind.String) ok = double.TryParse(v.GetString(), NumberStyles.Float, CultureInfo.InvariantCulture, out d);
        return ok && double.IsFinite(d) ? d : null;
    }

    private static bool Bool(JsonElement e, string name) => e.TryGetProperty(name, out var v) && v.ValueKind == JsonValueKind.True;
}
