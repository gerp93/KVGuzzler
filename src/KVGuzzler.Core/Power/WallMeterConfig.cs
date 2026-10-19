using System.Text.Json;
using System.Text.RegularExpressions;

namespace KVGuzzler.Core.Power;

/// <summary>
/// Where the wall meter (a smart plug that measures what the whole PC draws from the wall) is on the
/// network. Kept in a small file in the machine-wide data folder, because both the window and the
/// collector service have to know it and the service cannot read a user's settings.
/// </summary>
public sealed partial record WallMeterConfig(string Host)
{
    public const string FileName = "wall-meter.json";
    public const string ShellyType = "shelly";

    [GeneratedRegex(@"^[A-Za-z0-9]([A-Za-z0-9.\-]*[A-Za-z0-9])?(:\d{1,5})?$")]
    private static partial Regex HostPattern();

    /// <summary>The kind of meter. Only Shelly plugs are supported so far; the field leaves room for others.</summary>
    public string Type { get; init; } = ShellyType;

    /// <summary>A name or IPv4 address, optionally with a port: <c>192.168.1.50</c>, <c>shelly-pc.lan</c>.
    /// Not a URL: the program builds the address itself, so only the plug's own address can be reached.</summary>
    public static bool IsValidHost(string? host) =>
        host is not null && host.Length <= 253 && HostPattern().IsMatch(host)
        // The pattern allows things like 192.168.68.300 that look right but are not an address: only keep what
        // the URL parser really accepts as a plain host (and port), so the plug is never asked for with a bad one.
        && Uri.TryCreate($"http://{host}/", UriKind.Absolute, out var uri)
        && uri.PathAndQuery == "/" && uri.Host.Length > 0;

    /// <summary>The saved meter, or null when none is set up or the file is unreadable.</summary>
    public static WallMeterConfig? Load(string path)
    {
        try
        {
            if (!File.Exists(path)) return null;
            var config = JsonSerializer.Deserialize<WallMeterConfig>(File.ReadAllText(path), Options);
            return config is not null && IsValidHost(config.Host) ? config : null;
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
