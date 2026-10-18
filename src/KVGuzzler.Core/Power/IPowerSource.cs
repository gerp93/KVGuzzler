namespace KVGuzzler.Core.Power;

/// <summary>
/// One way of reading power from this machine (NVML, the Windows Energy Meter counters, Linux RAPL...).
/// A source works out in its constructor whether it can run here; it never throws for "this hardware
/// or permission is not present", it says so in <see cref="UnavailableReason"/>.
/// </summary>
public interface IPowerSource : IDisposable
{
    /// <summary>Short name shown to the user, such as "NVIDIA GPU (NVML)".</summary>
    string Name { get; }

    /// <summary>Null when the source works; otherwise one plain sentence on why it cannot report here.</summary>
    string? UnavailableReason { get; }

    /// <summary>The current readings. May be empty (nothing to report yet). Only called when
    /// <see cref="UnavailableReason"/> is null.</summary>
    IReadOnlyList<PowerReading> Read();
}
