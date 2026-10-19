using KVGuzzler.Core.History;

namespace KVGuzzler.Core.Carbon;

/// <summary>
/// Keeps the history's grid carbon intensity up to date. Run by the collector service, so it keeps recording with the window closed:
/// every half hour it asks Electricity Maps for the latest reading and saves it under its hour, and it says in the history what happened
/// (a reading, or why not) so the window can show it. It follows the config file, so setting up, changing or removing the key in the
/// window takes effect within about half a minute without restarting the service.
/// </summary>
/// <remarks>The free key only returns the latest reading and the last 24 hours, so hours the collector was not running for are never
/// filled in afterwards; the calculator values those at the average of the hours it does have.</remarks>
public sealed class CarbonFeed
{
    /// <summary>How often a reading is asked for. The figure is hourly, so this catches each hour's update without using much of a free allowance.</summary>
    public static readonly TimeSpan Interval = TimeSpan.FromMinutes(30);

    /// <summary>How long to wait before trying again after something went wrong.</summary>
    public static readonly TimeSpan RetryAfterError = TimeSpan.FromMinutes(5);

    /// <summary>How long to wait when the service said to slow down.</summary>
    public static readonly TimeSpan RetryAfterLimit = TimeSpan.FromHours(1);

    /// <summary>How often the config file and the clock are looked at.</summary>
    public static readonly TimeSpan CheckEvery = TimeSpan.FromSeconds(30);

    private readonly HistoryStore _store;
    private readonly string _configPath;
    private readonly HttpClient _http;
    private readonly TimeProvider _time;

    private CarbonConfig? _lastConfig;
    private DateTimeOffset _nextAt = DateTimeOffset.MinValue;
    private bool _hadConfig;

    public CarbonFeed(HistoryStore store, string configPath, HttpMessageHandler? handler = null, TimeProvider? time = null)
    {
        _store = store;
        _configPath = configPath;
        _http = handler is null ? new HttpClient() : new HttpClient(handler, disposeHandler: false);
        _time = time ?? TimeProvider.System;
    }

    /// <summary>Looks at the config now and then until stopped.</summary>
    public async Task RunAsync(CancellationToken stop)
    {
        try
        {
            using var timer = new PeriodicTimer(CheckEvery, _time);
            do
            {
                try { await StepAsync(stop); }
                catch (Exception e) when (e is not OperationCanceledException)
                {
                    // An optional feature must never stop the collector: say what happened and carry on.
                    Save(new CarbonStatus(false, _time.GetUtcNow(), Error: "Something unexpected went wrong: " + e.Message));
                }
            }
            while (await timer.WaitForNextTickAsync(stop));
        }
        catch (OperationCanceledException)
        {
            // The service is stopping.
        }
        finally
        {
            _http.Dispose();
        }
    }

    /// <summary>One look: asks for a reading when one is due (or the setup changed). Returns true when it asked.</summary>
    public async Task<bool> StepAsync(CancellationToken stop = default)
    {
        var config = CarbonConfig.Load(_configPath);
        if (config is null)
        {
            // Said once, when it was set up and now is not; with nothing ever set up there is nothing to say.
            if (_hadConfig) _store.SetCarbonStatus(new CarbonStatus(false, _time.GetUtcNow(), Error: "Not set up."));
            _hadConfig = false;
            _lastConfig = null;
            return false;
        }

        var now = _time.GetUtcNow();
        var changed = config != _lastConfig;
        if (!changed && now < _nextAt) return false;
        _hadConfig = true;
        _lastConfig = config;

        try
        {
            var reading = await ElectricityMapsClient.ReadLatestAsync(_http, config, now, stop);
            _store.AddCarbon(reading.At, reading.GramsPerKwh, reading.IsEstimated);
            Save(new CarbonStatus(true, now, reading.GramsPerKwh, reading.At));
            _nextAt = now + Interval;
        }
        catch (CarbonException e)
        {
            Save(new CarbonStatus(false, now, Error: e.Message));
            _nextAt = now + (e.RateLimited ? RetryAfterLimit : RetryAfterError);
        }
        return true;
    }

    private void Save(CarbonStatus status)
    {
        try { _store.SetCarbonStatus(status); }
        catch (Exception e) when (e is Microsoft.Data.Sqlite.SqliteException or IOException) { /* it will say so next time */ }
    }
}
