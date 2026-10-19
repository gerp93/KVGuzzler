

namespace KVGuzzler.Core.Power;

/// <summary>Chooses which power sources to try on this operating system. Every source that cannot run
/// here stays in the list and says why, so the Dashboard can tell the user what is missing.</summary>
public static class PowerSources
{
    /// <param name="wallMeterConfigPath">The wall-meter config file, or null to leave the wall meter out. The
    /// source is always added when a path is given: it says "not set up" until the user sets one up.</param>
    public static IReadOnlyList<IPowerSource> Detect(string? wallMeterConfigPath = null)
    {
        var sources = new List<IPowerSource> { new NvmlPowerSource() };

        if (OperatingSystem.IsWindows())
        {
            sources.Add(new WindowsEnergyMeterSource());
        }
        else if (OperatingSystem.IsLinux())
        {
            sources.Add(new LinuxRaplSource());
            sources.Add(new LinuxHwmonSource());
        }

        if (wallMeterConfigPath is not null) sources.Add(new WallMeterSource(wallMeterConfigPath));

        return sources;
    }
}
