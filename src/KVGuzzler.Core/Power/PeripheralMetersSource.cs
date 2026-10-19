namespace KVGuzzler.Core.Power;

/// <summary>
/// The extra meters (a monitor's plug, a speakers' plug): one reading per plug that answered lately, each with
/// <see cref="ReadingScope.Peripheral"/> and named by the label the user gave it. Every plug is polled on its own timer, so one
/// that is slow or unplugged never holds up the once-a-second sampling. It follows the config file like
/// <see cref="WallMeterSource"/>, so adding or removing a meter in the window takes effect within a few seconds.
/// </summary>
/// <remarks>Never added to the sensors or the wall figure; see <see cref="ReadingScope.Peripheral"/>. A plug that does not answer
/// is simply missing from the readings, and nothing is recorded for it, rather than a made-up zero. It is named in
/// <see cref="Notes"/> so the Dashboard can say which one is silent.</remarks>
public sealed class PeripheralMetersSource : IPowerSource, IPeripheralEnergyCounters, IPowerSourceNotes
{
    /// <summary>A reading older than this is not shown as current.</summary>
    public static readonly TimeSpan Fresh = TimeSpan.FromSeconds(5);

    private static readonly TimeSpan ConfigCheck = TimeSpan.FromSeconds(2);

    private readonly string _configPath;
    private readonly HttpClient _http;
    private readonly TimeSpan _interval;
    private readonly TimeProvider _time;
    private readonly object _gate = new();
    private readonly Dictionary<PeripheralMeter, Polled> _polled = new();

    private DateTime _configStamp = DateTime.MinValue;
    private long? _configCheckedAt;

    private sealed class Polled
    {
        public CancellationTokenSource Cancel { get; } = new();
        public double Watts;
        public long? ReadAt;
        public string? Error;
        public WallCounter? Counter;
    }

    public PeripheralMetersSource(string configPath, HttpMessageHandler? handler = null, TimeSpan? pollInterval = null, TimeProvider? time = null)
    {
        _configPath = configPath;
        _http = handler is null ? new HttpClient() : new HttpClient(handler, disposeHandler: false);
        _interval = pollInterval ?? TimeSpan.FromSeconds(1);
        _time = time ?? TimeProvider.System;
    }

    public string Name => "Other meters (Shelly plugs)";

    /// <summary>Never unavailable: having no extra meters is normal, and one that does not answer is only left out of the readings.</summary>
    public string? UnavailableReason => null;

    /// <summary>What each plug that answered lately reads, in the order they were set up.</summary>
    public IReadOnlyList<PowerReading> Read()
    {
        lock (_gate)
        {
            FollowConfig();
            return _polled
                .Where(p => IsFresh(p.Value))
                .Select(p => new PowerReading(p.Key.Label, p.Value.Watts, ReadingScope.Peripheral))
                .ToList();
        }
    }

    /// <summary>One line for each meter that has stopped answering and why ("Monitor (192.168.1.51): Could not reach ..."). A meter
    /// still waiting for its first answer, or whose last good answer is recent, is not listed: a single missed poll is not worth a notice.</summary>
    public IReadOnlyList<string> Notes
    {
        get
        {
            lock (_gate)
            {
                return _polled
                    .Where(p => p.Value.Error is not null && !IsFresh(p.Value))
                    .Select(p => $"{p.Key.Label} ({p.Key.Host}): {p.Value.Error}")
                    .ToList();
            }
        }
    }

    /// <summary>Why a meter is not reading right now (its last error), or null when it is, or is not set up.</summary>
    public string? ProblemWith(string label)
    {
        lock (_gate)
        {
            var found = _polled.FirstOrDefault(p => string.Equals(p.Key.Label, label, StringComparison.OrdinalIgnoreCase));
            return found.Value?.Error;
        }
    }

    public IReadOnlyDictionary<string, WallCounter> LatestCounters
    {
        get
        {
            lock (_gate)
            {
                return _polled
                    .Where(p => p.Value.Counter is not null)
                    .ToDictionary(p => p.Key.Label, p => p.Value.Counter!, StringComparer.OrdinalIgnoreCase);
            }
        }
    }

    public void Dispose()
    {
        lock (_gate)
        {
            foreach (var p in _polled.Values) p.Cancel.Cancel();
            _polled.Clear();
        }
        _http.Dispose();
    }

    private bool IsFresh(Polled p) => p.ReadAt is { } at && _time.GetElapsedTime(at) < Fresh;

    /// <summary>Re-reads the config file when it changed (checked at most every couple of seconds), stops polling meters that are
    /// gone or changed, and starts polling new ones. Caller holds the lock.</summary>
    private void FollowConfig()
    {
        var now = _time.GetTimestamp();
        if (_configCheckedAt is { } checkedAt && _time.GetElapsedTime(checkedAt, now) < ConfigCheck) return;
        _configCheckedAt = now;

        var stamp = File.Exists(_configPath) ? File.GetLastWriteTimeUtc(_configPath) : DateTime.MinValue;
        if (stamp == _configStamp && (_polled.Count > 0 || stamp == DateTime.MinValue)) return;
        _configStamp = stamp;

        var wanted = PeripheralMetersConfig.Load(_configPath).Meters.ToHashSet();
        foreach (var gone in _polled.Keys.Where(m => !wanted.Contains(m)).ToList())
        {
            _polled[gone].Cancel.Cancel();   // a reading from the old address says nothing about the new one
            _polled.Remove(gone);
        }
        foreach (var meter in wanted.Where(m => !_polled.ContainsKey(m)))
        {
            var state = new Polled();
            _polled[meter] = state;
            _ = Task.Run(() => PollAsync(meter, state));
        }
    }

    private async Task PollAsync(PeripheralMeter meter, Polled state)
    {
        var cancel = state.Cancel.Token;
        try
        {
            using var timer = new PeriodicTimer(_interval);
            do
            {
                try
                {
                    var status = await ShellyPlug.ReadStatusAsync(_http, meter.Host, cancel);
                    lock (_gate)
                    {
                        if (cancel.IsCancellationRequested) return;
                        state.Watts = status.Watts;
                        state.ReadAt = _time.GetTimestamp();
                        state.Counter = status.EnergyWh is { } wh ? new WallCounter(meter.Host, wh, _time.GetUtcNow()) : null;
                        state.Error = null;
                    }
                }
                catch (WallMeterException e)
                {
                    lock (_gate)
                    {
                        if (!cancel.IsCancellationRequested) state.Error = e.Message;
                    }
                }
            }
            while (await timer.WaitForNextTickAsync(cancel));
        }
        catch (OperationCanceledException)
        {
            // The meter was changed or removed, or the app is closing.
        }
    }
}
