using KVGuzzler.Core.History;
using KVGuzzler.Core.Power;
using Microsoft.Extensions.Hosting;
using Microsoft.Extensions.Logging;

namespace KVGuzzler.Collector;

/// <summary>The collector's loop: sample once a second, forever, until the service is told to stop.</summary>
internal sealed class CollectorWorker : BackgroundService
{
    private readonly string _dataDir;
    private readonly ILogger<CollectorWorker> _log;

    public CollectorWorker(string dataDir, ILogger<CollectorWorker> log)
    {
        _dataDir = dataDir;
        _log = log;
    }

    protected override async Task ExecuteAsync(CancellationToken stop)
    {
        using var store = new HistoryStore(Path.Combine(_dataDir, HistoryStore.FileName));
        using var sampler = new PowerSampler(PowerSources.Detect());
        var collector = new PowerCollector(store, sampler);

        collector.Start(DateTimeOffset.UtcNow);
        _log.LogInformation("KVGuzzler collector started. History: {Path}", store.Path);

        try
        {
            using var timer = new PeriodicTimer(TimeSpan.FromSeconds(1));
            do
            {
                try
                {
                    collector.Tick(DateTimeOffset.UtcNow);
                }
                catch (Exception e) when (e is not OperationCanceledException)
                {
                    // One bad sample (a locked database, a sensor that threw) must not end the recording.
                    _log.LogWarning(e, "A sample failed; carrying on.");
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
            collector.Stop();
            _log.LogInformation("KVGuzzler collector stopped.");
        }
    }
}
