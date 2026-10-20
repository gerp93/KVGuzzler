using System.Globalization;
using System.Text.Json;

namespace KVGuzzler.Core.Carbon;

/// <summary>A problem reaching or understanding Electricity Maps, worded so it can be shown to the user as is.</summary>
public sealed class CarbonException : Exception
{
    /// <param name="rateLimited">True when the service said to slow down, so the caller should wait a good while before asking again.</param>
    public CarbonException(string message, bool rateLimited = false, Exception? inner = null) : base(message, inner) => RateLimited = rateLimited;

    public bool RateLimited { get; }
}

/// <summary>The grid's carbon intensity for one hour.</summary>
/// <param name="GramsPerKwh">Grams of CO2 equivalent per kWh of electricity (a life-cycle figure for the whole grid, an average, not what
/// one more kWh would cause).</param>
/// <param name="At">The hour the figure is for.</param>
/// <param name="IsEstimated">Electricity Maps had no measurement and modelled it.</param>
public sealed record CarbonReading(double GramsPerKwh, DateTimeOffset At, bool IsEstimated);

/// <summary>
/// Reads the latest grid carbon intensity from Electricity Maps (<c>GET /v3/carbon-intensity/latest</c>, key in the <c>auth-token</c>
/// header). A plain GET: nothing here sends anything but the key and the place, and only to <see cref="CarbonConfig.ApiBase"/>.
/// </summary>
/// <remarks>Uses the v4 interface at <see cref="CarbonConfig.ApiBase"/> (the address and header that Electricity Maps' own portal shows).
/// Tested with made-up answers; the host, path and header were checked to answer (401 without a key) but a real key has not been tried
/// here. The free key covers one zone, the latest reading and 24 hours of history.</remarks>
public static class ElectricityMapsClient
{
    public static readonly TimeSpan Timeout = TimeSpan.FromSeconds(10);

    /// <summary>The figure and its hour from a "latest" answer. Tolerant of a missing time (it says now) and of the figure being wrapped in a
    /// <c>data</c> object; anything that is not a believable number of grams is refused.</summary>
    public static CarbonReading ParseLatest(string json, DateTimeOffset now)
    {
        try
        {
            using var doc = JsonDocument.Parse(json);
            var root = doc.RootElement;
            if (root.ValueKind == JsonValueKind.Object && root.TryGetProperty("data", out var data) && data.ValueKind == JsonValueKind.Object
                && !root.TryGetProperty("carbonIntensity", out _))
                root = data;

            if (root.ValueKind == JsonValueKind.Object
                && root.TryGetProperty("carbonIntensity", out var value) && value.ValueKind == JsonValueKind.Number
                && value.TryGetDouble(out var grams) && double.IsFinite(grams) && grams is >= 0 and <= 3000)
            {
                var at = root.TryGetProperty("datetime", out var dt) && dt.ValueKind == JsonValueKind.String
                    && DateTimeOffset.TryParse(dt.GetString(), CultureInfo.InvariantCulture, DateTimeStyles.AssumeUniversal, out var parsed)
                    ? parsed : now;
                var estimated = root.TryGetProperty("isEstimated", out var est) && est.ValueKind == JsonValueKind.True;
                return new CarbonReading(grams, at.ToUniversalTime(), estimated);
            }
        }
        catch (JsonException)
        {
            // Falls through to the message below: whatever answered is not Electricity Maps.
        }
        throw new CarbonException("Electricity Maps answered, but not with a carbon intensity. Check the zone, or try again later.");
    }

    /// <summary>The <c>message</c> (or <c>error</c>) of an error answer, cut short, with any trace of the key taken out; null if there is none.</summary>
    internal static string? ServiceMessage(string body, string apiKey)
    {
        try
        {
            using var doc = JsonDocument.Parse(body);
            if (doc.RootElement.ValueKind != JsonValueKind.Object) return null;
            foreach (var name in new[] { "message", "error" })
                if (doc.RootElement.TryGetProperty(name, out var v) && v.ValueKind == JsonValueKind.String && !string.IsNullOrWhiteSpace(v.GetString()))
                {
                    var text = v.GetString()!.Replace(apiKey, "(key)", StringComparison.Ordinal);
                    text = new string(text.Where(c => !char.IsControl(c)).ToArray()).Trim();
                    return text.Length > 200 ? text[..200] + "..." : text;
                }
        }
        catch (JsonException)
        {
            // Not JSON: nothing worth passing on.
        }
        return null;
    }

    public static async Task<CarbonReading> ReadLatestAsync(HttpClient http, CarbonConfig config, DateTimeOffset now,
        CancellationToken cancel = default, TimeSpan? timeoutAfter = null)
    {
        var limit = timeoutAfter ?? Timeout;
        try
        {
            using var timeout = CancellationTokenSource.CreateLinkedTokenSource(cancel);
            timeout.CancelAfter(limit);
            using var request = new HttpRequestMessage(HttpMethod.Get, config.LatestUri());
            request.Headers.Add("auth-token", config.ApiKey);
            using var response = await http.SendAsync(request, timeout.Token);

            var status = (int)response.StatusCode;
            var body = await response.Content.ReadAsStringAsync(timeout.Token);
            if (status is >= 200 and < 300) return ParseLatest(body, now);

            // Electricity Maps says why in its own words ("Invalid auth-token", a zone the key does not cover): pass that on.
            var why = ServiceMessage(body, config.ApiKey) is { } m ? $" Electricity Maps says: {m}" : "";
            throw status switch
            {
                401 or 403 => new CarbonException($"Electricity Maps did not accept the key (error {status}). "
                    + "The free key covers one zone: use the zone it was set up for." + why),
                404 => new CarbonException("Electricity Maps does not know that zone. Check it on the Electricity Maps map." + why),
                429 => new CarbonException("Electricity Maps asked us to slow down. It will be tried again later." + why, rateLimited: true),
                _ => new CarbonException($"Electricity Maps answered with an error ({status})." + why),
            };
        }
        catch (OperationCanceledException) when (!cancel.IsCancellationRequested)
        {
            throw new CarbonException($"Electricity Maps did not answer within {limit.TotalSeconds:0.#} seconds. Is this computer online?");
        }
        catch (HttpRequestException e)
        {
            throw new CarbonException($"Could not reach Electricity Maps: {e.Message}", inner: e);
        }
    }
}
