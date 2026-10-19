namespace KVGuzzler.Core.Programs;

/// <summary>How busy one program was over an interval. The units are only meaningful relative to the other
/// programs' numbers: CPU is a percentage of the whole CPU's capacity, GPU a percentage summed over the
/// engines the program used.</summary>
public sealed record ProgramActivity(string Program, double CpuBusy, double GpuBusy);

/// <summary>Estimated energy one program caused over some time.</summary>
public sealed record ProgramEnergy(string Program, double CpuKwh, double GpuKwh, double ActiveSeconds)
{
    public double TotalKwh => CpuKwh + GpuKwh;
}

/// <summary>Where per-program activity comes from (Windows performance counters today).</summary>
public interface IProgramActivitySource : IDisposable
{
    /// <summary>Null when it works; otherwise one sentence on why it cannot report here.</summary>
    string? UnavailableReason { get; }

    /// <summary>Every program that had a process at the last <see cref="Sample"/>, busy or not. Used to follow
    /// runs: a program that is open but idle is still running.</summary>
    IReadOnlyCollection<string> RunningPrograms { get; }

    /// <summary>What each program did since the previous call. The first call (and the first after
    /// <see cref="Reset"/>) returns nothing: a rate needs two readings.</summary>
    IReadOnlyList<ProgramActivity> Sample();

    /// <summary>Forget the previous reading, because time passed that must not be averaged over (sleep).</summary>
    void Reset();
}

/// <summary>
/// Shares the CPU's and GPU's measured energy out among the programs that were running. This is an
/// estimate: the hardware reports only its total power, and the operating system reports each program's
/// share of CPU time and GPU time. The power the hardware draws doing nothing (its idle baseline) belongs to
/// no program; only the power above it is divided, in proportion to each program's share of the busy time.
/// Nothing is lost or invented: the rows add up to exactly the energy that went in.
/// </summary>
public static class ProgramAttributor
{
    /// <summary>The CPU and GPU idle draw: not any program's doing.</summary>
    public const string Baseline = "(system baseline)";

    /// <summary>Power above idle in an interval when no program reported any activity.</summary>
    public const string Unattributed = "(not attributed)";

    /// <summary>Many tiny programs merged so the history stays small.</summary>
    public const string Others = "(other small programs)";

    /// <summary>A program counts as active in an interval above this much CPU or GPU use (percent).</summary>
    public const double ActiveCpuPercent = 0.5;
    public const double ActiveGpuPercent = 1.0;

    public static IReadOnlyList<ProgramEnergy> Attribute(
        double cpuKwh, double cpuIdleKwh, double gpuKwh, double gpuIdleKwh,
        double seconds, IReadOnlyList<ProgramActivity> activity)
    {
        var cpuBase = Math.Clamp(cpuIdleKwh, 0, Math.Max(cpuKwh, 0));
        var gpuBase = Math.Clamp(gpuIdleKwh, 0, Math.Max(gpuKwh, 0));
        var cpuDynamic = Math.Max(cpuKwh, 0) - cpuBase;
        var gpuDynamic = Math.Max(gpuKwh, 0) - gpuBase;

        var cpuTotal = activity.Sum(a => Math.Max(a.CpuBusy, 0));
        var gpuTotal = activity.Sum(a => Math.Max(a.GpuBusy, 0));

        var rows = new List<ProgramEnergy> { new(Baseline, cpuBase, gpuBase, 0) };

        foreach (var a in activity)
        {
            var cpu = cpuTotal > 0 ? cpuDynamic * Math.Max(a.CpuBusy, 0) / cpuTotal : 0;
            var gpu = gpuTotal > 0 ? gpuDynamic * Math.Max(a.GpuBusy, 0) / gpuTotal : 0;
            var active = a.CpuBusy >= ActiveCpuPercent || a.GpuBusy >= ActiveGpuPercent ? seconds : 0;
            if (cpu > 0 || gpu > 0 || active > 0) rows.Add(new ProgramEnergy(a.Program, cpu, gpu, active));
        }

        // Power above idle that no program claimed (nothing reported busy, e.g. work done by the driver itself).
        var lostCpu = cpuTotal > 0 ? 0 : cpuDynamic;
        var lostGpu = gpuTotal > 0 ? 0 : gpuDynamic;
        if (lostCpu > 0 || lostGpu > 0) rows.Add(new ProgramEnergy(Unattributed, lostCpu, lostGpu, 0));

        return rows;
    }

    /// <summary>Keeps the biggest programs and anything with a meaningful share, and merges the rest into
    /// <see cref="Others"/>. The two special rows are always kept as they are.</summary>
    public static IReadOnlyList<ProgramEnergy> Lump(IReadOnlyCollection<ProgramEnergy> rows, int keepTop = 15, double minShare = 0.01)
    {
        var special = rows.Where(r => r.Program is Baseline or Unattributed).ToList();
        var programs = rows.Where(r => r.Program is not (Baseline or Unattributed)).OrderByDescending(r => r.TotalKwh).ToList();
        var total = programs.Sum(r => r.TotalKwh);

        var kept = new List<ProgramEnergy>();
        double cpu = 0, gpu = 0, active = 0;
        var merged = false;
        for (var i = 0; i < programs.Count; i++)
        {
            var p = programs[i];
            if (i < keepTop || (total > 0 && p.TotalKwh / total >= minShare)) kept.Add(p);
            else
            {
                cpu += p.CpuKwh;
                gpu += p.GpuKwh;
                active = Math.Max(active, p.ActiveSeconds);   // programs ran side by side: this is not a sum of their times
                merged = true;
            }
        }
        if (merged) kept.Add(new ProgramEnergy(Others, cpu, gpu, active));
        return special.Concat(kept).ToList();
    }
}
