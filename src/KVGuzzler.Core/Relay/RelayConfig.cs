using System.Text.Json;
using KVGuzzler.Core.Power;

namespace KVGuzzler.Core.Relay;

/// <summary>
/// Where the house server is, so the collector can send it what this PC recorded. Kept in a small file in the machine-wide data
/// folder beside the other settings, because the collector service reads it and cannot read a user's settings. Without the file
/// the collector sends nothing.
/// </summary>
/// <param name="Host">The house server's address on the network, with an optional port: <c>192.168.1.50</c>, <c>house-box:8080</c>.
/// Not a URL: the program builds the address itself, so only the server's own push address can ever be reached.</param>
public sealed record RelayConfig(string Host)
{
    public const string FileName = "relay.json";

    /// <summary>The port the house server listens on unless told otherwise.</summary>
    public const int DefaultPort = 8080;

    /// <summary>A name or IPv4 address, optionally with a port. The same rule as the wall meter's address.</summary>
    public static bool IsValidHost(string? host) => WallMeterConfig.IsValidHost(host);

    /// <summary>The one address anything is ever sent to.</summary>
    public Uri PushUri => new($"http://{(Host.Contains(':') ? Host : $"{Host}:{DefaultPort}")}/api/v1/ingest/pc");

    /// <summary>The saved setting, or null when none is set up or the file is unreadable or holds something that is not an address.</summary>
    public static RelayConfig? Load(string path)
    {
        try
        {
            if (!File.Exists(path)) return null;
            var config = JsonSerializer.Deserialize<RelayConfig>(File.ReadAllText(path), Options);
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
