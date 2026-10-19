using System.Text.Json;

namespace KVGuzzler.Core.Power;

/// <summary>A problem reaching or understanding the wall meter, worded so it can be shown to the user as is.</summary>
public sealed class WallMeterException : Exception
{
    public WallMeterException(string message, Exception? inner = null) : base(message, inner) { }
}

/// <summary>
/// Reads a Shelly plug (second generation and newer, including the Plug US Gen4) over the local network.
/// One plain HTTP GET, <c>/rpc/Switch.GetStatus?id=0</c>, answers with the current power in watts
/// (<c>apower</c>). It only ever reads: nothing here can switch the plug, and the PC is plugged into it.
/// </summary>
/// <remarks>Written from Shelly's documented Gen2+ interface and tested with made-up answers; not yet
/// checked against a real plug.</remarks>
public static class ShellyPlug
{
    public static readonly TimeSpan Timeout = TimeSpan.FromSeconds(3);

    public static Uri StatusUri(string host) => new($"http://{host}/rpc/Switch.GetStatus?id=0");

    /// <summary>The current power in watts from a Switch.GetStatus answer.</summary>
    public static double ParseWatts(string json)
    {
        try
        {
            using var doc = JsonDocument.Parse(json);
            if (doc.RootElement.ValueKind == JsonValueKind.Object
                && doc.RootElement.TryGetProperty("apower", out var apower)
                && apower.ValueKind == JsonValueKind.Number)
            {
                var watts = apower.GetDouble();
                if (double.IsFinite(watts) && watts >= 0) return watts;
            }
        }
        catch (JsonException)
        {
            // Falls through to the message below: whatever answered is not a Shelly.
        }
        throw new WallMeterException("The device answered, but not with a power reading. Is this address the Shelly plug?");
    }

    public static async Task<double> ReadWattsAsync(HttpClient http, string host, CancellationToken cancel = default, TimeSpan? timeoutAfter = null)
    {
        var limit = timeoutAfter ?? Timeout;
        try
        {
            using var timeout = CancellationTokenSource.CreateLinkedTokenSource(cancel);
            timeout.CancelAfter(limit);
            using var response = await http.GetAsync(StatusUri(host), timeout.Token);

            return (int)response.StatusCode switch
            {
                401 => throw new WallMeterException("The plug asks for a password. In the Shelly app, turn off authentication for this plug."),
                404 => throw new WallMeterException("That device does not answer like a Shelly plug (second generation or newer)."),
                >= 200 and < 300 => ParseWatts(await response.Content.ReadAsStringAsync(timeout.Token)),
                var code => throw new WallMeterException($"The plug answered with an error ({code})."),
            };
        }
        catch (OperationCanceledException) when (!cancel.IsCancellationRequested)
        {
            throw new WallMeterException($"{host} did not answer within {limit.TotalSeconds:0.#} seconds. Is the plug on and on the same network?");
        }
        catch (HttpRequestException e)
        {
            throw new WallMeterException($"Could not reach {host}: {e.Message}", e);
        }
        catch (UriFormatException)
        {
            throw new WallMeterException($"\"{host}\" is not a valid address. Enter the plug's address like 192.168.68.106.");
        }
    }
}
