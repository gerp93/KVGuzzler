namespace KVGuzzler.Core.Power;

/// <summary>Whether a reading counts toward the total.</summary>
public enum ReadingScope
{
    /// <summary>A separate piece of hardware (a GPU, the CPU package, memory). These are summed.</summary>
    Component,

    /// <summary>A part of a component already counted elsewhere (CPU cores inside the CPU package).
    /// Shown for information, never added to the total, so nothing is counted twice.</summary>
    Breakdown,

    /// <summary>The whole PC measured at the wall by a smart plug. It already includes every component
    /// and everything the sensors cannot see, so it is reported next to them and never added to them.</summary>
    Wall,
}

/// <summary>What kind of hardware a reading is, so the per-program estimate knows which power to share out.</summary>
public enum PowerKind
{
    Other,
    Cpu,
    Gpu,
}

/// <summary>One power reading from one piece of hardware.</summary>
public sealed record PowerReading(string Name, double Watts, ReadingScope Scope, PowerKind Kind = PowerKind.Other);
