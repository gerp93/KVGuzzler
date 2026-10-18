namespace KVGuzzler.Core.Programs;

/// <summary>One run of a program: from when it was first seen running until none of its processes were left,
/// with the energy it is estimated to have caused in that time.</summary>
public sealed record ProgramRun(
    string Program, DateTimeOffset Start, DateTimeOffset End, double CpuKwh, double GpuKwh, double ActiveSeconds, bool StillRunning)
{
    public double TotalKwh => CpuKwh + GpuKwh;
}

/// <summary>
/// Follows which programs are running and adds up the energy each one caused between its start and its end,
/// so a session can be priced ("this game session: 2 h, 0.31 kWh"). A program is a program name, not one
/// process: a browser with twenty processes is one run, from its first process until its last. A run starts
/// when the program is first seen, which for a program already running when recording began is that moment.
/// Runs that caused almost nothing are not kept.
/// </summary>
public sealed class RunTracker
{
    /// <summary>A program not seen for this long has ended (one missed sample is not an exit).</summary>
    public static readonly TimeSpan Gone = TimeSpan.FromSeconds(10);

    /// <summary>Runs that caused less than this (half a watt-hour) are noise and are not kept.</summary>
    public const double MinKwh = 0.0005;

    private sealed class State
    {
        public required DateTimeOffset Start { get; init; }
        public DateTimeOffset LastSeen { get; set; }
        public double CpuKwh, GpuKwh, ActiveSeconds;
    }

    private readonly Dictionary<string, State> _runs = new(StringComparer.OrdinalIgnoreCase);

    /// <summary>Records one look at what is running and the energy each program caused since the last look.
    /// Returns the runs that ended.</summary>
    /// <param name="running">Every program with a process right now, active or not.</param>
    /// <param name="interval">The energy shared out for the interval (the special baseline rows are ignored).</param>
    public IReadOnlyList<ProgramRun> Update(DateTimeOffset now, IReadOnlyCollection<string> running, IReadOnlyList<ProgramEnergy> interval)
    {
        var present = new HashSet<string>(running, StringComparer.OrdinalIgnoreCase);
        foreach (var row in interval)
            if (!IsSpecial(row.Program)) present.Add(row.Program);

        foreach (var program in present)
        {
            if (IsSpecial(program)) continue;
            if (!_runs.TryGetValue(program, out var state))
                _runs[program] = state = new State { Start = now, LastSeen = now };
            state.LastSeen = now;
        }

        foreach (var row in interval)
        {
            if (IsSpecial(row.Program) || !_runs.TryGetValue(row.Program, out var state)) continue;
            state.CpuKwh += row.CpuKwh;
            state.GpuKwh += row.GpuKwh;
            state.ActiveSeconds += row.ActiveSeconds;
        }

        var finished = new List<ProgramRun>();
        foreach (var (program, state) in _runs.Where(kv => !present.Contains(kv.Key) && now - kv.Value.LastSeen >= Gone).ToList())
        {
            _runs.Remove(program);
            if (Worth(state)) finished.Add(ToRun(program, state, stillRunning: false));
        }
        return finished;
    }

    /// <summary>The runs going on now that have caused enough to be worth showing.</summary>
    public IReadOnlyList<ProgramRun> Current() =>
        _runs.Where(kv => Worth(kv.Value)).Select(kv => ToRun(kv.Key, kv.Value, stillRunning: true)).ToList();

    /// <summary>Ends every run at the last time it was seen (the recording is stopping).</summary>
    public IReadOnlyList<ProgramRun> CloseAll()
    {
        var all = _runs.Where(kv => Worth(kv.Value)).Select(kv => ToRun(kv.Key, kv.Value, stillRunning: false)).ToList();
        _runs.Clear();
        return all;
    }

    private static bool Worth(State s) => s.CpuKwh + s.GpuKwh >= MinKwh;

    private static bool IsSpecial(string program) => program is ProgramAttributor.Baseline or ProgramAttributor.Unattributed or ProgramAttributor.Others;

    private static ProgramRun ToRun(string program, State s, bool stillRunning) =>
        new(program, s.Start, s.LastSeen, s.CpuKwh, s.GpuKwh, s.ActiveSeconds, stillRunning);
}
