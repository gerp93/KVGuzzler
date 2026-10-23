using System.Net;
using System.Net.Http.Json;
using KVGuzzler.Core.History;

namespace KVGuzzler.Core.Relay;

/// <summary>
/// Sends what this PC recorded to the house server. Run by the collector service, so it keeps going with the window closed. It replays the
/// collector's own history: a saved watermark says which minute was sent last, and every look sends the whole minutes after it (never the
/// last couple, which may still be written to). Nothing is kept for a server that is away, because the history is the backlog; when it
/// answers again the rest is sent. Sending a minute twice is harmless, the server replaces it. It follows <c>relay.json</c>, so setting it
/// up or removing it takes effect within about half a minute without restarting the service.
/// </summary>
public sealed class RelayFeed
{
    /// <summary>How often the config file and the clock are looked at.</summary>
    public static readonly TimeSpan CheckEvery = TimeSpan.FromSeconds(30);

    /// <summary>How long to wait before trying again when the server did not answer or answered with an error.</summary>
    public static readonly TimeSpan RetryAfterError = TimeSpan.FromMinutes(1);

    /// <summary>How long to wait when the server speaks a different version of the contract (nothing short of an update fixes that).</summary>
    public static readonly TimeSpan RetryAfterVersion = TimeSpan.FromMinutes(10);

    /// <summary>The newest minutes are left alone: the collector can still add to a minute it has just finished.</summary>
    public static readonly TimeSpan SafetyMargin = TimeSpan.FromMinutes(2);

    /// <summary>The first time, only this much history is sent; the rest stays on the PC.</summary>
    public const int BackfillDays = 7;

    public const int MaxBatchesPerStep = 24;

    /// <summary>A batch the server refuses this many times in a row is skipped, so one bad minute cannot hold up everything after it.
    /// What was skipped stays in this PC's own history.</summary>
    public const int RefusalsBeforeSkipping = 3;

    public const string WatermarkKey = "relay_watermark";
    public const string SourceIdKey = "relay_source_id";
    public const string StatusKey = "relay_status";

    private readonly HistoryStore _store;
    private readonly string _configPath;
    private readonly string _machineName;
    private readonly HttpClient _http;
    private readonly TimeProvider _time;

    private DateTimeOffset _nextAt = DateTimeOffset.MinValue;
    private long _refusedAfter = -1;
    private int _refusals;

    public RelayFeed(HistoryStore store, string configPath, string machineName, HttpMessageHandler? handler = null, TimeProvider? time = null)
    {
        _store = store;
        _configPath = configPath;
        _machineName = machineName;
        _http = handler is null ? new HttpClient() : new HttpClient(handler, disposeHandler: false);
        _http.Timeout = TimeSpan.FromSeconds(20);
        _time = time ?? TimeProvider.System;
    }

    /// <summary>What happened on the last look, in words ("sent 12 minutes", or why not). Also kept in the history for the window to show.</summary>
    public string? Status { get; private set; }

    /// <summary>Raised when <see cref="Status"/> changes, so the host can log it.</summary>
    public event Action<string>? StatusChanged;

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
                    SetStatus("Something unexpected went wrong sending to the house server: " + e.Message);
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

    /// <summary>One look: sends what is new, if the house server is set up and not waiting out a failure. Returns how many minutes were sent.</summary>
    public async Task<int> StepAsync(CancellationToken stop = default)
    {
        var config = RelayConfig.Load(_configPath);
        if (config is null) return 0;

        var now = _time.GetUtcNow();
        if (now < _nextAt) return 0;

        var through = Floor((now - SafetyMargin).ToUnixTimeSeconds());
        var watermark = Watermark(now);
        var sourceId = SourceId();

        var sent = 0;
        for (var i = 0; i < MaxBatchesPerStep; i++)
        {
            var batch = RelayBatchBuilder.Build(_store, sourceId, _machineName, watermark, through);
            if (batch is null) break;

            var outcome = await PostAsync(config, batch, stop);
            if (outcome.Kind == Kind.Accepted)
            {
                watermark = batch.LastMinute;
                _store.SetMeta(WatermarkKey, watermark.ToString());
                _refusals = 0;
                sent += batch.Minutes.Count;
                continue;
            }

            if (outcome.Kind == Kind.Refused)
            {
                _refusals = _refusedAfter == watermark ? _refusals + 1 : 1;
                _refusedAfter = watermark;
                if (_refusals >= RefusalsBeforeSkipping)
                {
                    SetStatus($"The house server refused {batch.Minutes.Count} minute(s) {_refusals} times ({outcome.Message}); skipping them. They stay in this PC's history.");
                    watermark = batch.LastMinute;
                    _store.SetMeta(WatermarkKey, watermark.ToString());
                    _refusals = 0;
                    continue;
                }
            }

            _nextAt = now + (outcome.Kind == Kind.WrongVersion ? RetryAfterVersion : RetryAfterError);
            SetStatus(outcome.Message);
            return sent;
        }

        if (sent > 0) SetStatus($"Sent {sent} minute(s) to the house server.");
        return sent;
    }

    private enum Kind { Accepted, Refused, WrongVersion, Failed }

    private readonly record struct Outcome(Kind Kind, string Message);

    private async Task<Outcome> PostAsync(RelayConfig config, RelayBatch batch, CancellationToken stop)
    {
        try
        {
            using var response = await _http.PostAsJsonAsync(config.PushUri, batch, RelayBatch.Json, stop);
            switch (response.StatusCode)
            {
                case HttpStatusCode.OK:
                    return new Outcome(Kind.Accepted, "");
                case HttpStatusCode.UpgradeRequired:
                    return new Outcome(Kind.WrongVersion, "The house server speaks a different version than this collector. Update the older one.");
                case HttpStatusCode.BadRequest:
                    return new Outcome(Kind.Refused, await Reason(response, stop));
                default:
                    return new Outcome(Kind.Failed, $"The house server answered with an error ({(int)response.StatusCode}).");
            }
        }
        catch (OperationCanceledException) when (!stop.IsCancellationRequested)
        {
            return new Outcome(Kind.Failed, $"The house server at {config.Host} did not answer in time.");
        }
        catch (HttpRequestException e)
        {
            return new Outcome(Kind.Failed, $"Could not reach the house server at {config.Host}: {e.Message}");
        }
    }

    /// <summary>The server's own explanation of a refusal (its problem answer carries one); never the batch itself.</summary>
    private static async Task<string> Reason(HttpResponseMessage response, CancellationToken stop)
    {
        try
        {
            var body = await response.Content.ReadAsStringAsync(stop);
            using var doc = System.Text.Json.JsonDocument.Parse(body);
            if (doc.RootElement.TryGetProperty("message", out var m) && m.ValueKind == System.Text.Json.JsonValueKind.String)
                return m.GetString()!;
        }
        catch (Exception e) when (e is System.Text.Json.JsonException or HttpRequestException)
        {
            // Not a problem answer: fall through.
        }
        return "no reason given";
    }

    /// <summary>The last minute sent; the first time, a week ago (or when the history starts).</summary>
    private long Watermark(DateTimeOffset now)
    {
        if (long.TryParse(_store.GetMeta(WatermarkKey), out var saved)) return saved;
        var start = Floor((now - TimeSpan.FromDays(BackfillDays)).ToUnixTimeSeconds());
        _store.SetMeta(WatermarkKey, start.ToString());
        return start;
    }

    /// <summary>A name for this PC that stays the same: made once and kept in the history.</summary>
    private string SourceId()
    {
        var id = _store.GetMeta(SourceIdKey);
        if (!string.IsNullOrEmpty(id)) return id;
        id = "pc-" + Guid.NewGuid().ToString("N")[..8];
        _store.SetMeta(SourceIdKey, id);
        return id;
    }

    private static long Floor(long unixSeconds) => unixSeconds / 60 * 60;

    private void SetStatus(string status)
    {
        if (status == Status) return;
        Status = status;
        _store.SetMeta(StatusKey, $"{_time.GetUtcNow():u} {status}");
        StatusChanged?.Invoke(status);
    }
}
