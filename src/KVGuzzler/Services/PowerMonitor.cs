using KVGuzzler.Core.Power;

namespace KVGuzzler.Services;

/// <summary>
/// Samples the power sources on a background timer and publishes each snapshot. Detecting sources can be
/// slow (loading drivers, listing performance counters), so that happens on the background thread too:
/// the window never waits for hardware.
/// </summary>
public sealed class PowerMonitor : IDisposable
{
    private readonly Func<IReadOnlyList<IPowerSource>> _detect;
    private readonly TimeSpan _interval;
    private readonly CancellationTokenSource _stop = new();
    private Task? _loop;
    private PowerSampler? _sampler;

    public PowerMonitor(Func<IReadOnlyList<IPowerSource>> detect, TimeSpan? interval = null)
    {
        _detect = detect;
        _interval = interval ?? TimeSpan.FromSeconds(1);
    }

    /// <summary>The most recent snapshot, or null before the first one.</summary>
    public PowerSnapshot? Latest { get; private set; }

    /// <summary>Raised on a background thread after every sample.</summary>
    public event Action<PowerSnapshot>? Updated;

    /// <summary>Starts sampling. Calling it again does nothing.</summary>
    public void Start() => _loop ??= Task.Run(() => RunAsync(_stop.Token));

    private async Task RunAsync(CancellationToken stop)
    {
        try
        {
            _sampler = new PowerSampler(_detect());
            using var timer = new PeriodicTimer(_interval);
            do
            {
                var snapshot = _sampler.Sample(DateTimeOffset.UtcNow);
                Latest = snapshot;
                Updated?.Invoke(snapshot);
            }
            while (await timer.WaitForNextTickAsync(stop));
        }
        catch (OperationCanceledException)
        {
            // Shutting down.
        }
        catch (Exception e)
        {
            // Never take the app down over an optional feature: show what went wrong instead.
            var snapshot = new PowerSnapshot(DateTimeOffset.UtcNow, Array.Empty<PowerReading>(), 0, 0,
                new[] { "Power monitoring stopped: " + e.Message });
            Latest = snapshot;
            Updated?.Invoke(snapshot);
        }
    }

    public void Dispose()
    {
        _stop.Cancel();
        // Wait for the loop before disposing the sources it may be reading.
        try { _loop?.Wait(TimeSpan.FromSeconds(3)); }
        catch (AggregateException) { /* the loop reports its own failures */ }
        _sampler?.Dispose();
        _stop.Dispose();
    }
}
