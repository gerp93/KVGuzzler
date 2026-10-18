using KVGuzzler.Core.Power;

namespace KVGuzzler.Core.History;

/// <summary>
/// The collector's logic, separate from any timer or service so it can be tested with made-up clocks.
/// Call <see cref="Start"/> once, <see cref="Tick"/> about once a second, and <see cref="Stop"/> on the
/// way out. It saves energy per reading per minute, publishes the live readings, and records a
/// <see cref="Gap"/> with its reason whenever sampling was not happening.
/// </summary>
public sealed class PowerCollector
{
    /// <summary>A silence longer than this between ticks means the machine was asleep (the timer
    /// resumes with the wall clock far ahead), not that one tick was merely slow.</summary>
    public static readonly TimeSpan GapThreshold = TimeSpan.FromSeconds(10);

    /// <summary>Pending energy is saved at least this often, so a power cut loses seconds, not a minute.
    /// Saving adds to the minute's row, so saving a minute in pieces gives the same total.</summary>
    public static readonly TimeSpan FlushInterval = TimeSpan.FromSeconds(15);

    private readonly HistoryStore _store;
    private readonly PowerSampler _sampler;

    private DateTimeOffset? _last;
    private Dictionary<string, double> _lastWatts = new();
    private long _bucketMinute = -1;
    private DateTimeOffset _lastFlush;
    private readonly Dictionary<string, MinuteRow> _bucket = new();

    public PowerCollector(HistoryStore store, PowerSampler sampler)
    {
        _store = store;
        _sampler = sampler;
    }

    /// <summary>Records the time since the previous run as a gap: "off" if it stopped on purpose,
    /// otherwise "lost power" (it did not get to say goodbye).</summary>
    public void Start(DateTimeOffset now)
    {
        if (_store.LastSeen() is { } lastSeen && now - lastSeen > GapThreshold)
            _store.AddGap(new Gap(lastSeen, now, _store.StoppedCleanly() ? GapReason.Off : GapReason.LostPower));
    }

    public PowerSnapshot Tick(DateTimeOffset now)
    {
        var snapshot = _sampler.Sample(now);
        var readings = Unique(snapshot.Readings);

        var slept = _last is { } previous && now - previous > GapThreshold;
        if (slept)
        {
            // Energy counters answer with their average over the whole sleep, which is meaningless as a
            // current reading: close the books at the last good sample and start fresh from this one.
            Flush(now);
            _store.AddGap(new Gap(_last!.Value, now, GapReason.Asleep));
            _lastWatts.Clear();
        }
        else if (_last is { } before && now > before)
        {
            Integrate(before, now, readings);
            if (now - _lastFlush >= FlushInterval) Flush(now);
        }

        _lastWatts = readings.ToDictionary(r => r.Name, r => r.Watts);
        _last = now;
        _store.SetLive(now, readings, snapshot.Notes);
        return snapshot with { Readings = readings };
    }

    /// <summary>Saves what is pending and marks this as a deliberate stop.</summary>
    public void Stop()
    {
        Flush(_last ?? DateTimeOffset.UtcNow);
        _store.MarkCleanStop();
    }

    private void Integrate(DateTimeOffset from, DateTimeOffset to, IReadOnlyList<PowerReading> readings)
    {
        var minute = to.ToUnixTimeSeconds() / 60 * 60;
        if (minute != _bucketMinute)
        {
            Flush(to);
            _bucketMinute = minute;
        }

        var seconds = (to - from).TotalSeconds;
        foreach (var r in readings)
        {
            // A reading that was not there on the previous tick has nothing to average against yet.
            if (!_lastWatts.TryGetValue(r.Name, out var before)) continue;

            var kwh = (before + r.Watts) / 2.0 * seconds / 3_600_000.0;
            _bucket[r.Name] = _bucket.TryGetValue(r.Name, out var row)
                ? row with { KilowattHours = row.KilowattHours + kwh, Seconds = row.Seconds + seconds, PeakWatts = Math.Max(row.PeakWatts, r.Watts) }
                : new MinuteRow(minute, r.Name, r.Scope, kwh, seconds, Math.Max(before, r.Watts));
        }
    }

    private void Flush(DateTimeOffset now)
    {
        if (_bucket.Count > 0) _store.AddMinutes(_bucket.Values.ToList());
        _bucket.Clear();
        _lastFlush = now;
    }

    /// <summary>Two identical GPUs are both called "NVIDIA GeForce ..."; the second becomes "... 2" so each
    /// keeps its own history row instead of one overwriting the other.</summary>
    private static IReadOnlyList<PowerReading> Unique(IReadOnlyList<PowerReading> readings)
    {
        var seen = new Dictionary<string, int>();
        var result = new List<PowerReading>(readings.Count);
        foreach (var r in readings)
        {
            seen[r.Name] = seen.TryGetValue(r.Name, out var n) ? n + 1 : 1;
            result.Add(seen[r.Name] == 1 ? r : r with { Name = $"{r.Name} {seen[r.Name]}" });
        }
        return result;
    }
}
