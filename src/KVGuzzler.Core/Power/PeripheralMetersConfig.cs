using System.Text.Json;

namespace KVGuzzler.Core.Power;

/// <summary>One smart plug measuring something next to the PC, with the name it is shown and filed under.</summary>
/// <param name="Label">Its name ("Monitor"). It is also the key its history is saved under, so changing it later starts a new line.</param>
/// <param name="Host">The plug's address, as for the wall meter.</param>
public sealed record PeripheralMeter(string Label, string Host)
{
    public const int MaxLabelLength = 40;

    /// <summary>Why a label cannot be used, in words fit to show, or null when it is fine. The wall meter's own reading name
    /// is taken: the history files readings by name, so two would overwrite each other.</summary>
    public static string? LabelProblem(string? label)
    {
        var text = label?.Trim();
        if (string.IsNullOrEmpty(text)) return "Give the meter a name, for example Monitor.";
        if (text.Length > MaxLabelLength) return $"Use a name of at most {MaxLabelLength} characters.";
        if (text.Any(char.IsControl)) return "The name cannot contain control characters.";
        if (string.Equals(text, WallMeterSource.ReadingName, StringComparison.OrdinalIgnoreCase))
            return "That name is used by the wall meter. Pick another.";
        return null;
    }
}

/// <summary>
/// The extra meters: smart plugs for a monitor, speakers or anything else that is not the PC itself. Kept beside the wall meter's
/// file in the machine-wide data folder, so the collector service reads the same list the window edits.
/// </summary>
public sealed record PeripheralMetersConfig(IReadOnlyList<PeripheralMeter> Meters)
{
    public const string FileName = "peripheral-meters.json";

    /// <summary>More than this is almost certainly a mistake, and each one is polled every second.</summary>
    public const int MaxMeters = 12;

    public static PeripheralMetersConfig Empty { get; } = new(Array.Empty<PeripheralMeter>());

    /// <summary>The saved meters. Entries that are not valid (a bad address, a name already taken, the same plug twice) are
    /// left out rather than failing the whole file; a missing or unreadable file is no meters.</summary>
    public static PeripheralMetersConfig Load(string path)
    {
        try
        {
            if (!File.Exists(path)) return Empty;
            var file = JsonSerializer.Deserialize<FileShape>(File.ReadAllText(path), Options);
            return new PeripheralMetersConfig(Clean(file?.Meters ?? new List<PeripheralMeter>()));
        }
        catch (Exception e) when (e is IOException or UnauthorizedAccessException or JsonException)
        {
            return Empty;
        }
    }

    public void Save(string path)
    {
        Directory.CreateDirectory(Path.GetDirectoryName(Path.GetFullPath(path))!);
        File.WriteAllText(path, JsonSerializer.Serialize(new FileShape { Meters = Meters.ToList() }, Options));
    }

    /// <summary>Why <paramref name="meter"/> cannot be added to this list, or null when it can.</summary>
    public string? ProblemAdding(PeripheralMeter meter, string? wallHost = null)
    {
        if (LabelProblem(meter.Label) is { } label) return label;
        if (!WallMeterConfig.IsValidHost(meter.Host))
            return "Enter just the address (like 192.168.1.51 or shelly-monitor.lan), without http:// or a path.";
        if (Meters.Count >= MaxMeters) return $"That is the most meters supported ({MaxMeters}).";
        if (Meters.Any(m => string.Equals(m.Label, meter.Label, StringComparison.OrdinalIgnoreCase)))
            return $"There is already a meter called {meter.Label}.";
        if (Meters.Any(m => SameHost(m.Host, meter.Host)))
            return "A meter is already set up at that address. Counting one plug twice would double its energy.";
        if (wallHost is not null && SameHost(wallHost, meter.Host))
            return "That is the address of the wall meter for the whole PC. Use a different plug.";
        return null;
    }

    private static string? LabelProblem(string label) => PeripheralMeter.LabelProblem(label);

    private static bool SameHost(string a, string b) => string.Equals(a, b, StringComparison.OrdinalIgnoreCase);

    private static IReadOnlyList<PeripheralMeter> Clean(IEnumerable<PeripheralMeter> raw)
    {
        var kept = new PeripheralMetersConfig(Array.Empty<PeripheralMeter>());
        foreach (var m in raw)
        {
            if (m is null || m.Label is null || m.Host is null) continue;
            var trimmed = m with { Label = m.Label.Trim(), Host = m.Host.Trim() };
            if (kept.ProblemAdding(trimmed) is null) kept = new PeripheralMetersConfig(kept.Meters.Append(trimmed).ToList());
        }
        return kept.Meters;
    }

    private sealed class FileShape
    {
        public List<PeripheralMeter> Meters { get; set; } = new();
    }

    private static readonly JsonSerializerOptions Options = new() { WriteIndented = true, PropertyNamingPolicy = JsonNamingPolicy.CamelCase };
}
