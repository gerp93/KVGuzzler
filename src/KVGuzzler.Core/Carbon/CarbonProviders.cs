namespace KVGuzzler.Core.Carbon;

/// <summary>Asks whichever source the config names for the recent grid carbon intensity.</summary>
public static class CarbonProviders
{
    /// <summary>The hours the source has, oldest first: Electricity Maps gives the latest hour, EIA the last couple of days (so hours missed while the
    /// collector was off can be filled in). Throws <see cref="CarbonException"/> with a message fit to show.</summary>
    public static async Task<IReadOnlyList<CarbonReading>> ReadAsync(HttpClient http, CarbonConfig config, DateTimeOffset now, CancellationToken cancel = default)
    {
        if (config.Source == CarbonSources.Eia) return await EiaClient.ReadAsync(http, config, cancel);
        return new[] { await ElectricityMapsClient.ReadLatestAsync(http, config, now, cancel) };
    }
}
