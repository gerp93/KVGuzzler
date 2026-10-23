using System.Text.Json;
using System.Text.Json.Serialization;
using KVGuzzler.Core.History;
using KVGuzzler.Core.Power;
using KVGuzzler.Core.Programs;

namespace KVGuzzler.Core.Relay;

// The wire shapes of contract/openapi.yaml's PcIngest. The names are written out so a rename in C# can never change the contract.

public sealed record RelayReading(
    [property: JsonPropertyName("name")] string Name,
    [property: JsonPropertyName("scope")] string Scope,
    [property: JsonPropertyName("kind")] string Kind,
    [property: JsonPropertyName("avg_watts")] double AvgWatts,
    [property: JsonPropertyName("max_watts")] double MaxWatts,
    [property: JsonPropertyName("samples")] int Samples);

public sealed record RelayProgram(
    [property: JsonPropertyName("name")] string Name,
    [property: JsonPropertyName("cpu_kwh")] double CpuKwh,
    [property: JsonPropertyName("gpu_kwh")] double GpuKwh,
    [property: JsonPropertyName("active_seconds")] double ActiveSeconds);

public sealed record RelayMinute(
    [property: JsonPropertyName("minute")] DateTimeOffset Minute,
    [property: JsonPropertyName("readings")] IReadOnlyList<RelayReading> Readings,
    [property: JsonPropertyName("programs")] IReadOnlyList<RelayProgram> Programs,
    [property: JsonPropertyName("total_peak_watts")] double? TotalPeakWatts);

public sealed record RelayGap(
    [property: JsonPropertyName("from")] DateTimeOffset From,
    [property: JsonPropertyName("to")] DateTimeOffset To,
    [property: JsonPropertyName("reason")] string Reason,
    [property: JsonPropertyName("plug_kwh")] double? PlugKwh);

public sealed record RelayLiveReading(
    [property: JsonPropertyName("name")] string Name,
    [property: JsonPropertyName("scope")] string Scope,
    [property: JsonPropertyName("kind")] string Kind,
    [property: JsonPropertyName("watts")] double Watts);

public sealed record RelayLiveProgram(
    [property: JsonPropertyName("name")] string Name,
    [property: JsonPropertyName("cpu_watts")] double CpuWatts,
    [property: JsonPropertyName("gpu_watts")] double GpuWatts);

/// <summary>The PC's readings as of its last look: not history, the newest replaces the last on the server.</summary>
public sealed record RelayLive(
    [property: JsonPropertyName("at")] DateTimeOffset At,
    [property: JsonPropertyName("readings")] IReadOnlyList<RelayLiveReading> Readings,
    [property: JsonPropertyName("programs")] IReadOnlyList<RelayLiveProgram> Programs,
    [property: JsonPropertyName("notes")] IReadOnlyList<string> Notes,
    [property: JsonPropertyName("idle_cpu_watts")] double? IdleCpuWatts,
    [property: JsonPropertyName("idle_gpu_watts")] double? IdleGpuWatts);

public sealed record RelayRun(
    [property: JsonPropertyName("name")] string Name,
    [property: JsonPropertyName("start")] DateTimeOffset Start,
    [property: JsonPropertyName("end")] DateTimeOffset End,
    [property: JsonPropertyName("cpu_kwh")] double CpuKwh,
    [property: JsonPropertyName("gpu_kwh")] double GpuKwh,
    [property: JsonPropertyName("active_seconds")] double ActiveSeconds,
    [property: JsonPropertyName("still_running")] bool StillRunning);

/// <summary>One push to the house server: some whole minutes of what this PC recorded, and the gaps around them; or, with no minutes,
/// just the PC's latest look and its recent program runs.</summary>
public sealed record RelayBatch(
    [property: JsonPropertyName("api_version")] int ApiVersion,
    [property: JsonPropertyName("source_id")] string SourceId,
    [property: JsonPropertyName("source_name")] string SourceName,
    [property: JsonPropertyName("minutes")] IReadOnlyList<RelayMinute> Minutes,
    [property: JsonPropertyName("gaps")] IReadOnlyList<RelayGap> Gaps,
    [property: JsonPropertyName("live"), JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)] RelayLive? Live = null,
    [property: JsonPropertyName("runs"), JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)] IReadOnlyList<RelayRun>? Runs = null)
{
    /// <summary>The contract's major version this collector speaks.</summary>
    public const int Version = 1;

    /// <summary>The newest minute in the batch (unix seconds): everything up to it has been sent once the server accepts the batch.</summary>
    [JsonIgnore]
    public long LastMinute { get; init; }

    public static readonly JsonSerializerOptions Json = new()
    {
        DefaultIgnoreCondition = JsonIgnoreCondition.Never,
        NumberHandling = JsonNumberHandling.Strict,
    };
}

/// <summary>Turns what the collector saved in its history into a push. Reading the history back means nothing new has to be kept for a
/// server that is away: whatever it has not been sent is still here, and sending a minute again is harmless (the server replaces it).</summary>
public static class RelayBatchBuilder
{
    /// <summary>The server's own limits, so a push is never refused for being too big or for a name that is too long.</summary>
    public const int MaxNameLength = 80;
    public const int MaxSamples = 120;
    public const double MaxActiveSeconds = 120;

    /// <summary>The next batch of whole minutes after <paramref name="afterMinute"/> and no later than <paramref name="throughMinute"/>
    /// (both unix seconds on a minute), at most <paramref name="maxMinutes"/> of them; null when there is nothing new.</summary>
    public static RelayBatch? Build(HistoryStore store, string sourceId, string sourceName, long afterMinute, long throughMinute, int maxMinutes = 360)
    {
        var minutes = store.NextMinutes(afterMinute, throughMinute, maxMinutes);
        if (minutes.Count == 0) return null;
        var last = minutes[^1];

        var rows = store.QueryMinuteRows(afterMinute, last).ToLookup(r => r.MinuteStart);
        var programs = store.QueryProgramRows(afterMinute, last).ToLookup(p => p.MinuteStart, p => p.Row);
        var peaks = store.QueryTotalPeaks(afterMinute, last);

        var batchMinutes = new List<RelayMinute>(minutes.Count);
        foreach (var minute in minutes)
        {
            var readings = rows[minute].Select(ToReading).OfType<RelayReading>().ToList();
            var progs = programs[minute].Select(ToProgram).OfType<RelayProgram>().ToList();
            batchMinutes.Add(new RelayMinute(
                DateTimeOffset.FromUnixTimeSeconds(minute), readings, progs,
                peaks.TryGetValue(minute, out var peak) && double.IsFinite(peak) && peak >= 0 ? peak : null));
        }

        // A gap's plug energy can be filled in a little after the gap ends, so the gaps of the last hour are sent along each time.
        var from = DateTimeOffset.FromUnixTimeSeconds(afterMinute) - TimeSpan.FromHours(1);
        var through = DateTimeOffset.FromUnixTimeSeconds(last).AddMinutes(1);
        var gaps = store.QueryGapsEndingBetween(from, through)
            .Where(g => g.End > g.Start)
            .Select(g => new RelayGap(g.Start, g.End, ReasonName(g.Reason), g.PlugKwh is { } k && double.IsFinite(k) && k >= 0 ? k : null))
            .ToList();

        return new RelayBatch(RelayBatch.Version, sourceId, sourceName, batchMinutes, gaps) { LastMinute = last };
    }

    public const int MaxLiveReadings = 100;
    public const int MaxLivePrograms = 200;
    public const int MaxNotes = 50;
    public const int MaxNoteLength = 300;
    public const int MaxRuns = 50;
    public const double MaxRunKwh = 1000;
    public static readonly TimeSpan MaxRunLength = TimeSpan.FromDays(31);

    /// <summary>Per-program power older than this next to the readings is from a collector that stopped sharing it, and is left out.</summary>
    public static readonly TimeSpan LiveProgramsFresh = TimeSpan.FromSeconds(30);

    /// <summary>The PC's latest look and its recent program runs as a push with no minutes; null until the collector has saved a look.
    /// Nothing here is history, so nothing is replayed: the next look replaces it on the server.</summary>
    public static RelayBatch? BuildLive(HistoryStore store, string sourceId, string sourceName, DateTimeOffset now)
    {
        var live = store.GetLive();
        if (live is null) return null;

        // The live table does not keep a reading's kind, but the minutes do: use the kind a name was last saved with.
        var through = now.ToUnixTimeSeconds() / 60 * 60;
        var kinds = new Dictionary<(string, ReadingScope), PowerKind>();
        foreach (var row in store.QueryMinuteRows(through - 600, through))
            kinds[(row.Name, row.Scope)] = row.Kind;

        var readings = new List<RelayLiveReading>();
        foreach (var r in live.Readings)
        {
            var name = Clip(r.Name);
            if (name.Length == 0 || !double.IsFinite(r.Watts) || r.Watts < 0 || readings.Count >= MaxLiveReadings) continue;
            readings.Add(new RelayLiveReading(name, ScopeName(r.Scope), KindName(kinds.GetValueOrDefault((r.Name, r.Scope), r.Kind)), r.Watts));
        }

        var programs = new List<RelayLiveProgram>();
        if (store.GetLivePrograms() is { } lp && live.UpdatedAt - lp.UpdatedAt < LiveProgramsFresh)
        {
            foreach (var p in lp.Rows)
            {
                var name = Clip(p.Program);
                if (name.Length == 0 || !double.IsFinite(p.CpuWatts) || !double.IsFinite(p.GpuWatts) || programs.Count >= MaxLivePrograms) continue;
                programs.Add(new RelayLiveProgram(name, Math.Max(0, p.CpuWatts), Math.Max(0, p.GpuWatts)));
            }
        }

        var notes = live.Notes.Select(n => n.Length <= MaxNoteLength ? n : n[..MaxNoteLength]).Where(n => n.Length > 0).Take(MaxNotes).ToList();
        var (idleCpu, idleGpu) = store.GetIdleBaseline();
        var look = new RelayLive(live.UpdatedAt, readings, programs, notes,
            idleCpu is { } c && double.IsFinite(c) && c >= 0 ? c : null,
            idleGpu is { } g && double.IsFinite(g) && g >= 0 ? g : null);

        var runs = new List<RelayRun>();
        foreach (var r in store.GetRecentRuns(MaxRuns))
        {
            var name = Clip(r.Program);
            // A run's end is the last time it was seen, so it is never after now; clamp anyway so a clock step cannot get the push refused.
            var end = r.End > now ? now : r.End;
            var start = r.Start > end ? end : r.Start;
            if (name.Length == 0 || !double.IsFinite(r.CpuKwh) || !double.IsFinite(r.GpuKwh) || !double.IsFinite(r.ActiveSeconds)) continue;
            runs.Add(new RelayRun(name, start, end, Math.Clamp(r.CpuKwh, 0, MaxRunKwh), Math.Clamp(r.GpuKwh, 0, MaxRunKwh),
                Math.Clamp(r.ActiveSeconds, 0, MaxRunLength.TotalSeconds), r.StillRunning));
        }

        return new RelayBatch(RelayBatch.Version, sourceId, sourceName, Array.Empty<RelayMinute>(), Array.Empty<RelayGap>(), look, runs);
    }

    /// <summary>The average watts over the seconds actually sampled, as the contract wants it; null for a row that cannot be sent.</summary>
    private static RelayReading? ToReading(MinuteRow r)
    {
        var name = Clip(r.Name);
        if (name.Length == 0 || !(r.Seconds > 0) || !double.IsFinite(r.KilowattHours) || r.KilowattHours < 0) return null;
        var avg = r.KilowattHours * 3_600_000.0 / r.Seconds;
        if (!double.IsFinite(avg)) return null;
        var max = double.IsFinite(r.PeakWatts) && r.PeakWatts >= avg ? r.PeakWatts : avg;
        var samples = (int)Math.Clamp(Math.Round(r.Seconds), 1, MaxSamples);
        return new RelayReading(name, ScopeName(r.Scope), KindName(r.Kind), avg, max, samples);
    }

    private static RelayProgram? ToProgram(ProgramEnergy p)
    {
        var name = Clip(p.Program);
        if (name.Length == 0 || !double.IsFinite(p.CpuKwh) || !double.IsFinite(p.GpuKwh) || !double.IsFinite(p.ActiveSeconds)) return null;
        return new RelayProgram(name, Math.Max(0, p.CpuKwh), Math.Max(0, p.GpuKwh), Math.Clamp(p.ActiveSeconds, 0, MaxActiveSeconds));
    }

    private static string Clip(string name)
    {
        name = name.Trim();
        return name.Length <= MaxNameLength ? name : name[..MaxNameLength];
    }

    private static string ScopeName(ReadingScope scope) => scope switch
    {
        ReadingScope.Component => "component",
        ReadingScope.Breakdown => "breakdown",
        ReadingScope.Wall => "wall",
        ReadingScope.Peripheral => "peripheral",
        _ => "component",
    };

    private static string KindName(PowerKind kind) => kind switch
    {
        PowerKind.Cpu => "cpu",
        PowerKind.Gpu => "gpu",
        _ => "other",
    };

    private static string ReasonName(GapReason reason) => reason switch
    {
        GapReason.Asleep => "asleep",
        GapReason.Off => "off",
        _ => "lost_power",
    };
}
