namespace KVGuzzler.Core.Programs;

/// <summary>
/// Learns how much power the CPU and the GPU draw when nothing is asking anything of them: the lowest
/// average over any window (30 s) the collector has seen. Only the power above that is shared among
/// programs. It starts unknown, which means "treat everything as baseline" so a first reading taken while
/// the PC was busy cannot make a program look worse than it was; it can only come down as quieter moments
/// are seen, and is kept between runs.
/// </summary>
public sealed class IdleBaseline
{
    /// <summary>How long a stretch is averaged before it is compared with the lowest seen so far.</summary>
    public static readonly TimeSpan Window = TimeSpan.FromSeconds(30);

    public IdleBaseline(double? cpuWatts = null, double? gpuWatts = null)
    {
        CpuWatts = cpuWatts;
        GpuWatts = gpuWatts;
    }

    public double? CpuWatts { get; private set; }
    public double? GpuWatts { get; private set; }

    /// <summary>Compares a window's average power with the lowest so far. True if the baseline changed.</summary>
    public bool Observe(double cpuWatts, double gpuWatts)
    {
        var changed = false;
        if (double.IsFinite(cpuWatts) && cpuWatts >= 0 && (CpuWatts is null || cpuWatts < CpuWatts)) { CpuWatts = cpuWatts; changed = true; }
        if (double.IsFinite(gpuWatts) && gpuWatts >= 0 && (GpuWatts is null || gpuWatts < GpuWatts)) { GpuWatts = gpuWatts; changed = true; }
        return changed;
    }
}
