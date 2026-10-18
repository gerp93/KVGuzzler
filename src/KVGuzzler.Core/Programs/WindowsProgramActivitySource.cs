using System.Collections;
using System.Diagnostics;
using System.Runtime.Versioning;
using System.Text.RegularExpressions;

namespace KVGuzzler.Core.Programs;

/// <summary>
/// Per-program CPU and GPU use on Windows, from the performance counters Task Manager itself uses:
/// "Process" (CPU time per process) and "GPU Engine" (time each process kept each GPU engine busy). No
/// administrator rights and no driver are needed. Processes are grouped by program name, so all of one
/// browser's processes count as one program.
/// </summary>
[SupportedOSPlatform("windows")]
public sealed partial class WindowsProgramActivitySource : IProgramActivitySource
{
    [GeneratedRegex(@"#\d+$")]
    private static partial Regex InstanceSuffix();

    [GeneratedRegex(@"^pid_(\d+)_", RegexOptions.IgnoreCase)]
    private static partial Regex GpuPid();

    private sealed record Snapshot(
        Dictionary<int, (string Program, CounterSample Cpu)> Processes,
        Dictionary<string, (int Pid, CounterSample Gpu)> Gpu);

    private readonly int _processors = Math.Max(Environment.ProcessorCount, 1);
    private Snapshot? _previous;

    public WindowsProgramActivitySource()
    {
        try
        {
            if (!PerformanceCounterCategory.Exists("Process"))
                UnavailableReason = "Windows does not publish per-program CPU counters on this machine.";
        }
        catch (Exception e)
        {
            UnavailableReason = "Could not read the Windows per-program counters: " + e.Message;
        }
    }

    public string? UnavailableReason { get; }

    public void Reset() => _previous = null;

    public IReadOnlyList<ProgramActivity> Sample()
    {
        if (UnavailableReason is not null) return Array.Empty<ProgramActivity>();

        var current = Read();
        var previous = _previous;
        _previous = current;
        if (previous is null) return Array.Empty<ProgramActivity>();

        var byProgram = new Dictionary<string, (double Cpu, double Gpu)>(StringComparer.OrdinalIgnoreCase);
        var casing = new Dictionary<string, string>(StringComparer.OrdinalIgnoreCase);

        foreach (var (pid, (program, cpu)) in current.Processes)
        {
            // Matched by process id, not by instance name: names like chrome#3 renumber when another process exits.
            if (!previous.Processes.TryGetValue(pid, out var before) || before.Program != program) continue;
            var percent = Math.Clamp(CounterSample.Calculate(before.Cpu, cpu) / _processors, 0, 100);
            if (percent <= 0) continue;
            casing.TryAdd(program, program);
            var so = byProgram.GetValueOrDefault(program);
            byProgram[program] = (so.Cpu + percent, so.Gpu);
        }

        // One counter instance per process and engine (pid_1234_luid_..._engtype_3D); the full name is stable, so
        // each engine is compared only with itself.
        foreach (var (engine, (pid, now)) in current.Gpu)
        {
            if (!current.Processes.TryGetValue(pid, out var process) || !previous.Gpu.TryGetValue(engine, out var before)) continue;
            var percent = Math.Clamp(CounterSample.Calculate(before.Gpu, now), 0, 100);
            if (percent <= 0) continue;
            casing.TryAdd(process.Program, process.Program);
            var so = byProgram.GetValueOrDefault(process.Program);
            byProgram[process.Program] = (so.Cpu, so.Gpu + percent);
        }

        return byProgram.Select(kv => new ProgramActivity(casing[kv.Key], kv.Value.Cpu, kv.Value.Gpu)).ToList();
    }

    private Snapshot Read()
    {
        var processes = new Dictionary<int, (string, CounterSample)>();
        var process = new PerformanceCounterCategory("Process").ReadCategory();
        var ids = process["ID Process"];
        var cpu = process["% Processor Time"];
        foreach (DictionaryEntry entry in ids)
        {
            var instance = (string)entry.Key;
            if (ProgramName(instance) is not { } name) continue;
            var pid = (int)((InstanceData)entry.Value!).Sample.RawValue;
            if (pid <= 0 || !cpu.Contains(instance)) continue;
            processes[pid] = (name, cpu[instance].Sample);
        }

        var gpu = new Dictionary<string, (int, CounterSample)>();
        if (PerformanceCounterCategory.Exists("GPU Engine"))
        {
            var engines = new PerformanceCounterCategory("GPU Engine").ReadCategory();
            if (engines.Contains("Utilization Percentage"))
            {
                foreach (DictionaryEntry entry in engines["Utilization Percentage"])
                {
                    if (GpuPid().Match((string)entry.Key) is { Success: true } m)
                        gpu[(string)entry.Key] = (int.Parse(m.Groups[1].Value), ((InstanceData)entry.Value!).Sample);
                }
            }
        }
        return new Snapshot(processes, gpu);
    }

    /// <summary>The program name for a "Process" counter instance (<c>chrome#3</c> becomes <c>chrome.exe</c>),
    /// or null for the entries that are not programs (the idle time and the total).</summary>
    internal static string? ProgramName(string instance)
    {
        var name = InstanceSuffix().Replace(instance, "");
        if (name.Length == 0 || name.Equals("_Total", StringComparison.OrdinalIgnoreCase) || name.Equals("Idle", StringComparison.OrdinalIgnoreCase))
            return null;
        return name.Equals("System", StringComparison.OrdinalIgnoreCase) ? "System" : name + ".exe";
    }

    public void Dispose() { }
}
