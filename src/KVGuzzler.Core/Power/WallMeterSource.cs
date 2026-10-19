namespace KVGuzzler.Core.Power;

/// <summary>
/// The whole PC's power at the wall, from a smart plug. Polls the plug on its own timer, so a plug that
/// is slow or switched off never holds up the once-a-second sampling of the other sources. It follows
/// the config file, so setting up, changing or removing the meter in the window takes effect within a few
/// seconds without restarting the collector service.
/// </summary>
/// <remarks>The reading has <see cref="ReadingScope.Wall"/>: it already includes everything the CPU, GPU and
/// memory sensors measure plus the rest of the PC, so it is never added to them.</remarks>
public sealed class WallMeterSource : IPowerSource, IWallEnergyCounter
{
    public const string ReadingName = "Whole PC (wall meter)";

    /// <summary>A reading older than this is not shown as current.</summary>
    public static readonly TimeSpan Fresh = TimeSpan.FromSeconds(5);

    private static readonly TimeSpan ConfigCheck = TimeSpan.FromSeconds(2);

    private readonly string _configPath;
    private readonly HttpClient _http;
    private readonly TimeSpan _interval;
    private readonly TimeProvider _time;
    private readonly object _gate = new();

    private WallMeterConfig? _config;
    private DateTime _configStamp = DateTime.MinValue;
    private long? _configCheckedAt;
    private CancellationTokenSource? _polling;
    private double _watts;
    private long? _readAt;
    private string? _error;
    private WallCounter? _counter;

    public WallMeterSource(string configPath, HttpMessageHandler? handler = null, TimeSpan? pollInterval = null, TimeProvider? time = null)
    {
        _configPath = configPath;
        _http = handler is null ? new HttpClient() : new HttpClient(handler, disposeHandler: false);
        _interval = pollInterval ?? TimeSpan.FromSeconds(1);
        _time = time ?? TimeProvider.System;
    }

    public string Name => "Wall meter (Shelly plug)";

    public string? UnavailableReason
    {
        get
        {
            lock (_gate)
            {
                FollowConfig();
                if (_config is null) return "Not set up. Enter your smart plug's address in Settings.";
                if (IsFresh()) return null;
                return _error ?? "Waiting for the first reading from the plug.";
            }
        }
    }

    public WallCounter? LatestCounter
    {
        get
        {
            lock (_gate) return _counter;
        }
    }

    public IReadOnlyList<PowerReading> Read()
    {
        lock (_gate)
        {
            return IsFresh() ? new[] { new PowerReading(ReadingName, _watts, ReadingScope.Wall) } : Array.Empty<PowerReading>();
        }
    }

    public void Dispose()
    {
        lock (_gate)
        {
            _polling?.Cancel();
            _polling = null;
        }
        _http.Dispose();
    }

    private bool IsFresh() => _readAt is { } at && _time.GetElapsedTime(at) < Fresh;

    /// <summary>Re-reads the config file when it changed (checked at most every couple of seconds) and
    /// restarts polling for the new address. Caller holds the lock.</summary>
    private void FollowConfig()
    {
        var now = _time.GetTimestamp();
        if (_configCheckedAt is { } checkedAt && _time.GetElapsedTime(checkedAt, now) < ConfigCheck) return;
        _configCheckedAt = now;

        var stamp = File.Exists(_configPath) ? File.GetLastWriteTimeUtc(_configPath) : DateTime.MinValue;
        if (stamp == _configStamp && (_config is not null || stamp == DateTime.MinValue)) return;
        _configStamp = stamp;

        var loaded = WallMeterConfig.Load(_configPath);
        if (loaded == _config) return;

        _polling?.Cancel();
        _polling = null;
        _config = loaded;
        _readAt = null;   // a reading from the old address says nothing about the new one
        _counter = null;
        _error = null;

        if (loaded is null) return;
        var polling = new CancellationTokenSource();
        _polling = polling;
        _ = Task.Run(() => PollAsync(loaded.Host, polling.Token));
    }

    private async Task PollAsync(string host, CancellationToken cancel)
    {
        try
        {
            using var timer = new PeriodicTimer(_interval);
            do
            {
                try
                {
                    var status = await ShellyPlug.ReadStatusAsync(_http, host, cancel);
                    lock (_gate)
                    {
                        if (cancel.IsCancellationRequested) return;   // the address changed while this was in flight
                        _watts = status.Watts;
                        _readAt = _time.GetTimestamp();
                        _counter = status.EnergyWh is { } wh ? new WallCounter(host, wh, _time.GetUtcNow()) : null;
                        _error = null;
                    }
                }
                catch (WallMeterException e)
                {
                    lock (_gate)
                    {
                        if (!cancel.IsCancellationRequested) _error = e.Message;
                    }
                }
            }
            while (await timer.WaitForNextTickAsync(cancel));
        }
        catch (OperationCanceledException)
        {
            // The meter was changed, removed, or the app is closing.
        }
    }
}
