namespace KVGuzzler.Core.Power;

/// <summary>The smart plug's own running energy total at one moment. The plug keeps counting while the PC is asleep or off,
/// so two of these either side of a stretch nothing was recording give the energy the PC used in it.</summary>
/// <param name="Host">The plug's address. A counter only means anything next to another from the same plug.</param>
/// <param name="TotalWh">Watt-hours since the plug last restarted; it drops back to zero when the plug does.</param>
/// <param name="ReadAt">When the plug was asked (UTC).</param>
public sealed record WallCounter(string Host, double TotalWh, DateTimeOffset ReadAt);

/// <summary>A source that can tell the energy totals of several plugs, by the label the user gave each (see <see cref="WallCounter"/>).</summary>
public interface IPeripheralEnergyCounters
{
    /// <summary>The counter from each meter's latest answer. Meters that have not answered, or answered without a counter, are left out.</summary>
    IReadOnlyDictionary<string, WallCounter> LatestCounters { get; }
}

/// <summary>A source with things to tell the user that are not "I cannot run here": one meter out of several that is not answering.
/// Shown with the notes of the sources that are unavailable.</summary>
public interface IPowerSourceNotes
{
    IReadOnlyList<string> Notes { get; }
}

/// <summary>A source that can also tell the plug's energy total (see <see cref="WallCounter"/>).</summary>
public interface IWallEnergyCounter
{
    /// <summary>The counter from the plug's latest answer, or null when the plug has not answered, answered without a counter,
    /// or the meter is not set up.</summary>
    WallCounter? LatestCounter { get; }
}
