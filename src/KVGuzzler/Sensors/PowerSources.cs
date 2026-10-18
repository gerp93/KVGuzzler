using KVGuzzler.Core.Power;

namespace KVGuzzler.Sensors;

/// <summary>Chooses which power sources to try on this operating system. Every source that cannot run
/// here stays in the list and says why, so the Dashboard can tell the user what is missing.</summary>
public static class PowerSources
{
    public static IReadOnlyList<IPowerSource> Detect()
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

        return sources;
    }
}
