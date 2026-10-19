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
/// <remarks>Written from Electricity Maps' documented v3 interface and tested with made-up answers; not yet checked against the real
/// service. The free key covers one zone, the latest reading and 24 hours of history.</remarks>
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

            return (int)response.StatusCode switch
            {
                401 or 403 => throw new CarbonException("Electricity Maps did not accept the key, or the key is not allowed to read this zone. "
                    + "The free key covers one zone: use the zone it was set up for."),
                404 => throw new CarbonException("Electricity Maps does not know that zone. Check it on the Electricity Maps map."),
                429 => throw new CarbonException("Electricity Maps asked us to slow down. It will be tried again later.", rateLimited: true),
                >= 200 and < 300 => ParseLatest(await response.Content.ReadAsStringAsync(timeout.Token), now),
                var code => throw new CarbonException($"Electricity Maps answered with an error ({code})."),
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
