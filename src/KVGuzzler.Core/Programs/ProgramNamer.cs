namespace KVGuzzler.Core.Programs;

/// <summary>
/// Gives each running process its display name (<see cref="ProgramLabeler"/>) and remembers it, so the
/// operating system is asked about a process once, not at every sample. Only processes with a generic
/// name (an interpreter) are ever looked up.
/// </summary>
public sealed class ProgramNamer(IProcessInspector? inspector)
{
    private readonly Dictionary<int, (string Raw, string Label)> _known = new();

    /// <param name="pid">The process.</param>
    /// <param name="raw">Its executable name, e.g. <c>python.exe</c>.</param>
    /// <param name="programOf">The executable name of a process id, or null when it is not running.</param>
    public string Name(int pid, string raw, Func<int, string?> programOf)
    {
        if (!ProgramLabeler.IsGeneric(raw)) return raw;
        // A process id can be reused by a different program, so the remembered label is kept only while the name matches.
        if (_known.TryGetValue(pid, out var known) && known.Raw == raw) return known.Label;

        ProcessDetails? details = null;
        try { details = inspector?.Inspect(pid); }
        catch (Exception) { /* a process that cannot be inspected keeps its plain name */ }

        var parent = details?.ParentPid is { } p ? programOf(p) : null;
        var label = ProgramLabeler.Label(raw, details, parent);
        _known[pid] = (raw, label);
        return label;
    }

    /// <summary>Forget processes that are gone.</summary>
    public void Retain(IReadOnlyCollection<int> running)
    {
        if (_known.Count == 0) return;
        var alive = running as ISet<int> ?? running.ToHashSet();
        foreach (var pid in _known.Keys.Where(k => !alive.Contains(k)).ToList()) _known.Remove(pid);
    }
}
