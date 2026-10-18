using KVGuzzler.Core.Power;
using KVGuzzler.Core.Programs;

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

    /// <summary>How often runs still going are saved.</summary>
    public static readonly TimeSpan RunSaveInterval = TimeSpan.FromSeconds(30);

    /// <summary>How often the CPU's and GPU's energy is shared out among programs.</summary>
    public static readonly TimeSpan AttributionInterval = TimeSpan.FromSeconds(2);

    private const double JoulesPerKwh = 3_600_000.0;

    private readonly HistoryStore _store;
    private readonly PowerSampler _sampler;
    private readonly IProgramActivitySource? _programs;
    private readonly IdleBaseline _baseline = new();
    private readonly RunTracker _runs = new();
    private DateTimeOffset _lastRunSave = DateTimeOffset.MinValue;

    // Energy of the CPU and GPU since programs were last looked at, and the window the idle baseline is learned in.
    private double _pendCpuKwh, _pendGpuKwh, _pendSeconds;
    private double _windowCpuKwh, _windowGpuKwh, _windowSeconds;
    private DateTimeOffset _lastAttribution = DateTimeOffset.MinValue;
    private bool _programsPrimed;
    private string? _programNote;
    private readonly Dictionary<string, ProgramEnergy> _programBucket = new(StringComparer.OrdinalIgnoreCase);

    private DateTimeOffset? _last;
    private Dictionary<string, double> _lastWatts = new();
    private long _bucketMinute = -1;
    private DateTimeOffset _lastFlush;
    private readonly Dictionary<string, MinuteRow> _bucket = new();

    /// <param name="programs">Per-program activity, or null to skip the per-program estimate.</param>
    public PowerCollector(HistoryStore store, PowerSampler sampler, IProgramActivitySource? programs = null)
    {
        _store = store;
        _sampler = sampler;
        _programs = programs;
        if (programs is not null)
        {
            var (cpu, gpu) = store.GetIdleBaseline();
            _baseline = new IdleBaseline(cpu, gpu);
        }
    }

    /// <summary>Records the time since the previous run as a gap: "off" if it stopped on purpose,
    /// otherwise "lost power" (it did not get to say goodbye).</summary>
    public void Start(DateTimeOffset now)
    {
        _store.EndStaleRuns();
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
            RestartPrograms();
        }
        else if (_last is { } before && now > before)
        {
            Integrate(before, now, readings);
            if (_programs is not null && now - _lastAttribution >= AttributionInterval) AttributePrograms(now);
            if (now - _lastFlush >= FlushInterval) Flush(now);
        }

        _lastWatts = readings.ToDictionary(r => r.Name, r => r.Watts);
        _last = now;
        var notes = _programNote is null ? snapshot.Notes : snapshot.Notes.Append(_programNote).ToList();
        _store.SetLive(now, readings, notes);
        return snapshot with { Readings = readings, Notes = notes };
    }

    /// <summary>Saves what is pending and marks this as a deliberate stop.</summary>
    public void Stop()
    {
        foreach (var run in _runs.CloseAll()) _store.SaveRun(run);
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
        _pendSeconds += seconds;
        foreach (var r in readings)
        {
            // A reading that was not there on the previous tick has nothing to average against yet.
            if (!_lastWatts.TryGetValue(r.Name, out var before)) continue;

            var kwh = (before + r.Watts) / 2.0 * seconds / JoulesPerKwh;
            if (r.Scope == ReadingScope.Component)
            {
                if (r.Kind == PowerKind.Cpu) _pendCpuKwh += kwh;
                else if (r.Kind == PowerKind.Gpu) _pendGpuKwh += kwh;
            }
            _bucket[r.Name] = _bucket.TryGetValue(r.Name, out var row)
                ? row with { KilowattHours = row.KilowattHours + kwh, Seconds = row.Seconds + seconds, PeakWatts = Math.Max(row.PeakWatts, r.Watts) }
                : new MinuteRow(minute, r.Name, r.Scope, kwh, seconds, Math.Max(before, r.Watts));
        }
    }

    private void Flush(DateTimeOffset now)
    {
        if (_bucket.Count > 0) _store.AddMinutes(_bucket.Values.ToList());
        _bucket.Clear();
        if (_programBucket.Count > 0) _store.AddProgramMinutes(_bucketMinute, ProgramAttributor.Lump(_programBucket.Values));
        _programBucket.Clear();
        _lastFlush = now;
    }

    // ----- Per-program estimate ---------------------------------------------------------

    /// <summary>Shares the CPU's and GPU's energy since the last look out among the programs that were busy, keeping
    /// the idle baseline apart, and learns the baseline from the quietest 30 second stretch seen so far.</summary>
    private void AttributePrograms(DateTimeOffset now)
    {
        if (_programs!.UnavailableReason is { } why)
        {
            _programNote = "Per-program power: " + why;
            return;
        }

        IReadOnlyList<ProgramActivity> activity;
        try
        {
            activity = _programs.Sample();
            _programNote = null;
        }
        catch (Exception e)
        {
            // Optional extra: a failing counter must not stop the energy recording itself.
            _programNote = "Per-program power: " + e.Message;
            return;
        }

        _lastAttribution = now;
        if (!_programsPrimed)
        {
            // The first reading only sets a starting point; the energy up to here has no matching activity.
            _programsPrimed = true;
            ClearPending();
            return;
        }
        if (_pendSeconds <= 0) return;

        var seconds = _pendSeconds;
        // Until a baseline is known everything counts as baseline: better to under-credit programs for a
        // moment than to blame them for power the idle machine draws.
        var cpuIdle = _baseline.CpuWatts is { } cw ? cw * seconds / JoulesPerKwh : _pendCpuKwh;
        var gpuIdle = _baseline.GpuWatts is { } gw ? gw * seconds / JoulesPerKwh : _pendGpuKwh;
        var rows = ProgramAttributor.Attribute(_pendCpuKwh, cpuIdle, _pendGpuKwh, gpuIdle, seconds, activity);

        foreach (var row in rows)
        {
            _programBucket[row.Program] = _programBucket.TryGetValue(row.Program, out var old)
                ? new ProgramEnergy(row.Program, old.CpuKwh + row.CpuKwh, old.GpuKwh + row.GpuKwh, old.ActiveSeconds + row.ActiveSeconds)
                : row;
        }

        _store.SetLivePrograms(now, rows
            .Select(r => new ProgramLive(r.Program, r.CpuKwh * JoulesPerKwh / seconds, r.GpuKwh * JoulesPerKwh / seconds))
            .Where(r => r.CpuWatts + r.GpuWatts >= 0.05)
            .OrderByDescending(r => r.CpuWatts + r.GpuWatts)
            .Take(13)
            .ToList());

        TrackRuns(now, rows);
        LearnBaseline(_pendCpuKwh, _pendGpuKwh, seconds);
        ClearPending();
    }

    /// <summary>Follows each program from its start to its end. Runs that ended are saved at once; runs still
    /// going are saved every so often, so a crash or power cut leaves them with what they had.</summary>
    private void TrackRuns(DateTimeOffset now, IReadOnlyList<ProgramEnergy> interval)
    {
        foreach (var finished in _runs.Update(now, _programs!.RunningPrograms, interval)) _store.SaveRun(finished);
        if (now - _lastRunSave < RunSaveInterval) return;
        _lastRunSave = now;
        foreach (var current in _runs.Current()) _store.SaveRun(current);
    }

    private void LearnBaseline(double cpuKwh, double gpuKwh, double seconds)
    {
        _windowCpuKwh += cpuKwh;
        _windowGpuKwh += gpuKwh;
        _windowSeconds += seconds;
        if (_windowSeconds < IdleBaseline.Window.TotalSeconds) return;

        if (_baseline.Observe(_windowCpuKwh * JoulesPerKwh / _windowSeconds, _windowGpuKwh * JoulesPerKwh / _windowSeconds))
            _store.SetIdleBaseline(_baseline.CpuWatts, _baseline.GpuWatts);
        _windowCpuKwh = _windowGpuKwh = _windowSeconds = 0;
    }

    private void ClearPending() => _pendCpuKwh = _pendGpuKwh = _pendSeconds = 0;

    /// <summary>After a gap the counters' rates would average over the sleep: start the per-program reading over.</summary>
    private void RestartPrograms()
    {
        _programs?.Reset();
        _programsPrimed = false;
        ClearPending();
        _windowCpuKwh = _windowGpuKwh = _windowSeconds = 0;
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
