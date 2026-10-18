namespace KVGuzzler.Core.Power;

/// <summary>Whether a reading counts toward the total.</summary>
public enum ReadingScope
{
    /// <summary>A separate piece of hardware (a GPU, the CPU package, memory). These are summed.</summary>
    Component,

    /// <summary>A part of a component already counted elsewhere (CPU cores inside the CPU package).
    /// Shown for information, never added to the total, so nothing is counted twice.</summary>
    Breakdown,
}

/// <summary>One power reading from one piece of hardware.</summary>
public sealed record PowerReading(string Name, double Watts, ReadingScope Scope);
