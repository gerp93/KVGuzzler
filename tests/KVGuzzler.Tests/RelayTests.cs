using System.Net;
using System.Text.Json;
using KVGuzzler.Core.History;
using KVGuzzler.Core.Power;
using KVGuzzler.Core.Programs;
using KVGuzzler.Core.Relay;
using Xunit;

namespace KVGuzzler.Tests;

/// <summary>Plays the part of the house server's push endpoint.</summary>
internal sealed class FakeHouse : HttpMessageHandler
{
    public List<(Uri Uri, string Body)> Requests { get; } = new();

    /// <summary>The answers to give, in order; once they run out the server says OK.</summary>
    public Queue<HttpStatusCode> Answers { get; } = new();

    protected override async Task<HttpResponseMessage> SendAsync(HttpRequestMessage request, CancellationToken cancellationToken)
    {
        Requests.Add((request.RequestUri!, await request.Content!.ReadAsStringAsync(cancellationToken)));
        var code = Answers.Count > 0 ? Answers.Dequeue() : HttpStatusCode.OK;
        var body = code == HttpStatusCode.BadRequest ? """{"code":"bad_ingest","message":"minute is odd"}""" : "{}";
        return new HttpResponseMessage(code) { Content = new StringContent(body) };
    }

    public JsonDocument Sent(int index) => JsonDocument.Parse(Requests[index].Body);
}

public class RelayBatchTests
{
    private static readonly DateTimeOffset T0 = new(2026, 10, 22, 12, 0, 0, TimeSpan.Zero);
    private static long Minute(int i) => T0.AddMinutes(i).ToUnixTimeSeconds();

    [Fact]
    public void RowsBecomeAverageWattsOverTheSecondsSampledWithTheContractsNames()
    {
        using var h = new TempHistory();
        h.Store.AddMinutes(new[]
        {
            new MinuteRow(Minute(0), "CPU package", ReadingScope.Component, 0.001, 60, 120, PowerKind.Cpu),     // 1 Wh in 60 s = 60 W
            new MinuteRow(Minute(0), "CPU cores", ReadingScope.Breakdown, 0.0008, 60, 90, PowerKind.Cpu),
            new MinuteRow(Minute(0), "Wall meter", ReadingScope.Wall, 0.005, 60, 330),                          // 300 W
            new MinuteRow(Minute(0), "Monitor", ReadingScope.Peripheral, 0.0005, 60, 31),
            new MinuteRow(Minute(0), "RTX", ReadingScope.Component, 0.002, 60, 200, PowerKind.Gpu),
        });

        var batch = RelayBatchBuilder.Build(h.Store, "pc-1", "Desktop", Minute(-1), Minute(0))!;

        Assert.Equal((1, "pc-1", "Desktop"), (batch.ApiVersion, batch.SourceId, batch.SourceName));
        var m = Assert.Single(batch.Minutes);
        Assert.Equal(T0, m.Minute);
        var by = m.Readings.ToDictionary(r => r.Name);
        Assert.Equal(60.0, by["CPU package"].AvgWatts, precision: 9);
        Assert.Equal((120.0, 60, "component", "cpu"), (by["CPU package"].MaxWatts, by["CPU package"].Samples, by["CPU package"].Scope, by["CPU package"].Kind));
        Assert.Equal(("breakdown", "cpu"), (by["CPU cores"].Scope, by["CPU cores"].Kind));
        Assert.Equal(("wall", "other", 300.0), (by["Wall meter"].Scope, by["Wall meter"].Kind, Math.Round(by["Wall meter"].AvgWatts, 6)));
        Assert.Equal("peripheral", by["Monitor"].Scope);
        Assert.Equal("gpu", by["RTX"].Kind);
    }

    [Fact]
    public void APeakBelowTheAverageIsRaisedToItAndSecondsAreClampedToWhatTheServerAccepts()
    {
        using var h = new TempHistory();
        h.Store.AddMinutes(new[]
        {
            new MinuteRow(Minute(0), "A", ReadingScope.Component, 0.001, 60, 10),     // average 60 W, peak says 10
            new MinuteRow(Minute(0), "B", ReadingScope.Component, 0.001, 200, 500),   // 200 s: more than the 120 the server allows
            new MinuteRow(Minute(0), "C", ReadingScope.Component, 0.001, 0.4, 500),   // under a second still counts as one sample
        });

        var m = Assert.Single(RelayBatchBuilder.Build(h.Store, "pc-1", "D", Minute(-1), Minute(0))!.Minutes);
        var by = m.Readings.ToDictionary(r => r.Name);
        Assert.Equal(60.0, by["A"].MaxWatts, precision: 9);
        Assert.Equal(120, by["B"].Samples);
        Assert.Equal(1, by["C"].Samples);
    }

    [Fact]
    public void RowsThatCannotBeSentAreLeftOutAndLongNamesAreCut()
    {
        using var h = new TempHistory();
        h.Store.AddMinutes(new[]
        {
            new MinuteRow(Minute(0), "no seconds", ReadingScope.Component, 0.001, 0, 10),
            new MinuteRow(Minute(0), "negative", ReadingScope.Component, -0.001, 60, 10),
            new MinuteRow(Minute(0), new string('x', 200), ReadingScope.Component, 0.001, 60, 100),
            new MinuteRow(Minute(0), "good", ReadingScope.Component, 0.001, 60, 100),
        });

        var m = Assert.Single(RelayBatchBuilder.Build(h.Store, "pc-1", "D", Minute(-1), Minute(0))!.Minutes);
        Assert.Equal(2, m.Readings.Count);
        Assert.Contains(m.Readings, r => r.Name.Length == RelayBatchBuilder.MaxNameLength);
        Assert.Contains(m.Readings, r => r.Name == "good");
    }

    [Fact]
    public void ProgramsPeaksAndGapsComeAlongWithTheirMinutes()
    {
        using var h = new TempHistory();
        h.Store.AddMinutes(new[] { new MinuteRow(Minute(1), "GPU", ReadingScope.Component, 0.002, 60, 150, PowerKind.Gpu) });
        h.Store.AddProgramMinutes(Minute(1), new[]
        {
            new ProgramEnergy("ComfyUI", 0.001, 0.0019, 55),
            new ProgramEnergy("runaway.exe", 0.0001, 0, 900),   // more active seconds than a minute holds
        });
        h.Store.AddTotalPeak(Minute(1), 410);
        h.Store.AddGap(new Gap(T0.AddMinutes(-400), T0.AddMinutes(-200), GapReason.Asleep, 0.07));   // ended long before the window
        h.Store.AddGap(new Gap(T0.AddMinutes(-20), T0.AddMinutes(-10), GapReason.LostPower));
        h.Store.AddGap(new Gap(T0.AddMinutes(5), T0.AddMinutes(9), GapReason.Off));   // after this batch: not yet

        var batch = RelayBatchBuilder.Build(h.Store, "pc-1", "D", Minute(-30), Minute(1))!;

        var m = Assert.Single(batch.Minutes);
        Assert.Equal(410, m.TotalPeakWatts);
        Assert.Equal(new[] { "ComfyUI", "runaway.exe" }, m.Programs.Select(p => p.Name).ToArray());
        Assert.Equal((0.001, 0.0019, 55.0), (m.Programs[0].CpuKwh, m.Programs[0].GpuKwh, m.Programs[0].ActiveSeconds));
        Assert.Equal(RelayBatchBuilder.MaxActiveSeconds, m.Programs[1].ActiveSeconds);
        Assert.Equal(new[] { "lost_power" }, batch.Gaps.Select(g => g.Reason).ToArray());   // only gaps from the last hour before the window are sent along
    }

    [Fact]
    public void AMinuteWithOnlyProgramEstimatesIsStillSent()
    {
        using var h = new TempHistory();
        h.Store.AddProgramMinutes(Minute(3), new[] { new ProgramEnergy("ComfyUI", 0.001, 0.001, 30) });

        var m = Assert.Single(RelayBatchBuilder.Build(h.Store, "pc-1", "D", Minute(0), Minute(10))!.Minutes);
        Assert.Equal(T0.AddMinutes(3), m.Minute);
        Assert.Empty(m.Readings);
        Assert.Single(m.Programs);
    }

    [Fact]
    public void OnlyMinutesInTheWindowAreSentAndABatchHasAtMostTheRequestedNumber()
    {
        using var h = new TempHistory();
        h.Store.AddMinutes(Enumerable.Range(0, 10).Select(i => new MinuteRow(Minute(i), "GPU", ReadingScope.Component, 0.001, 60, 100)).ToList());

        Assert.Null(RelayBatchBuilder.Build(h.Store, "pc-1", "D", Minute(9), Minute(20)));                 // nothing after the last
        Assert.Null(RelayBatchBuilder.Build(h.Store, "pc-1", "D", Minute(-5), Minute(-1)));                // nothing in the window

        var first = RelayBatchBuilder.Build(h.Store, "pc-1", "D", Minute(1), Minute(8), maxMinutes: 3)!;
        Assert.Equal(new[] { Minute(2), Minute(3), Minute(4) }, first.Minutes.Select(m => m.Minute.ToUnixTimeSeconds()).ToArray());
        Assert.Equal(Minute(4), first.LastMinute);                                                          // so the next one starts at 5
        var next = RelayBatchBuilder.Build(h.Store, "pc-1", "D", first.LastMinute, Minute(8), maxMinutes: 3)!;
        Assert.Equal(Minute(5), next.Minutes[0].Minute.ToUnixTimeSeconds());
    }

    private static DateTimeOffset LiveAt => T0.AddMinutes(30);

    /// <summary>A history with a latest look, a fresh per-program split, an idle baseline and two runs, as a collector leaves it.</summary>
    private static TempHistory HistoryWithALook()
    {
        var h = new TempHistory();
        h.Store.AddMinutes(new[]
        {
            new MinuteRow(Minute(29), "CPU package", ReadingScope.Component, 0.001, 60, 120, PowerKind.Cpu),
            new MinuteRow(Minute(29), "RTX", ReadingScope.Component, 0.002, 60, 200, PowerKind.Gpu),
        });
        h.Store.SetLive(LiveAt, new[]
        {
            new PowerReading("CPU package", 61.5, ReadingScope.Component),
            new PowerReading("RTX", 120, ReadingScope.Component),
            new PowerReading("Fans", 7, ReadingScope.Breakdown),
            new PowerReading("Wall meter", 300, ReadingScope.Wall),
            new PowerReading("Negative", -4, ReadingScope.Component),
        }, new[] { "GPU read a nonsense value and was ignored.", new string('n', 400) });
        h.Store.SetLivePrograms(LiveAt, new[] { new ProgramLive("ComfyUI", 20, 110), new ProgramLive("chrome.exe", 5, 0) });
        h.Store.SetIdleBaseline(18.5, null);
        h.Store.SaveRun(new ProgramRun("ComfyUI", LiveAt.AddMinutes(-20), LiveAt, 0.01, 0.04, 1200, true));
        h.Store.SaveRun(new ProgramRun("old.exe", LiveAt.AddHours(-3), LiveAt.AddHours(-2), 0.002, 0, 3600, false));
        return h;
    }

    [Fact]
    public void TheLiveLookCarriesReadingsKindsProgramsIdleBaselineNotesAndRuns()
    {
        using var h = HistoryWithALook();

        var batch = RelayBatchBuilder.BuildLive(h.Store, "pc-1", "Desktop", LiveAt)!;

        Assert.Empty(batch.Minutes);
        Assert.Empty(batch.Gaps);
        var live = batch.Live!;
        Assert.Equal(LiveAt, live.At);
        // The negative reading is left out; the kind comes from what the minutes were saved with.
        Assert.Equal(new[] { "CPU package", "RTX", "Fans", "Wall meter" }, live.Readings.Select(r => r.Name).ToArray());
        Assert.Equal(("component", "cpu", 61.5), (live.Readings[0].Scope, live.Readings[0].Kind, live.Readings[0].Watts));
        Assert.Equal("gpu", live.Readings[1].Kind);
        Assert.Equal(("breakdown", "other"), (live.Readings[2].Scope, live.Readings[2].Kind));
        Assert.Equal("wall", live.Readings[3].Scope);
        Assert.Equal(new[] { "ComfyUI", "chrome.exe" }, live.Programs.Select(p => p.Name).ToArray());
        Assert.Equal((20.0, 110.0), (live.Programs[0].CpuWatts, live.Programs[0].GpuWatts));
        Assert.Equal(18.5, live.IdleCpuWatts);
        Assert.Null(live.IdleGpuWatts);
        Assert.Equal(2, live.Notes.Count);
        Assert.Equal(RelayBatchBuilder.MaxNoteLength, live.Notes[1].Length);

        var runs = batch.Runs!;
        Assert.Equal(new[] { "ComfyUI", "old.exe" }, runs.Select(r => r.Name).ToArray());
        Assert.True(runs[0].StillRunning);
        Assert.Equal((0.01, 0.04, 1200.0), (runs[0].CpuKwh, runs[0].GpuKwh, runs[0].ActiveSeconds));
    }

    [Fact]
    public void ProgramPowerFromACollectorThatStoppedSharingItIsLeftOutAndRunsNeverEndInTheFuture()
    {
        using var h = HistoryWithALook();
        h.Store.SetLivePrograms(LiveAt - RelayBatchBuilder.LiveProgramsFresh - TimeSpan.FromSeconds(1), new[] { new ProgramLive("ComfyUI", 20, 110) });

        var batch = RelayBatchBuilder.BuildLive(h.Store, "pc-1", "D", LiveAt.AddMinutes(-5))!;   // the PC's clock stepped back
        Assert.Empty(batch.Live!.Programs);
        Assert.All(batch.Runs!, r => Assert.True(r.End <= LiveAt.AddMinutes(-5) && r.Start <= r.End));
    }

    [Fact]
    public void WithNoLookSavedThereIsNothingLiveToSend()
    {
        using var h = new TempHistory();
        Assert.Null(RelayBatchBuilder.BuildLive(h.Store, "pc-1", "D", LiveAt));
    }

    [Fact]
    public void ALiveLookUsesOnlyFieldNamesTheContractDefinesAndAMinutesPushLeavesLiveOut()
    {
        using var h = HistoryWithALook();
        var live = RelayBatchBuilder.BuildLive(h.Store, "pc-1", "Desktop", LiveAt)!;

        using var ours = JsonDocument.Parse(JsonSerializer.Serialize(live, RelayBatch.Json));
        using var theirs = JsonDocument.Parse(File.ReadAllText(ContractExample("ingest-pc.json")));
        var missing = new List<string>();
        Compare(ours.RootElement, theirs.RootElement, "$", missing);
        Assert.True(missing.Count == 0, "Not in the contract's example: " + string.Join(", ", missing));

        var minutes = RelayBatchBuilder.Build(h.Store, "pc-1", "Desktop", Minute(28), Minute(29))!;
        using var plain = JsonDocument.Parse(JsonSerializer.Serialize(minutes, RelayBatch.Json));
        Assert.False(plain.RootElement.TryGetProperty("live", out _));   // the contract has no null for it: it is left out
        Assert.False(plain.RootElement.TryGetProperty("runs", out _));
    }

    /// <summary>The push's field names must be the contract's: every property this collector writes has to exist in the contract's own example.</summary>
    [Fact]
    public void EveryFieldNameIsOneTheContractDefines()
    {
        using var h = new TempHistory();
        h.Store.AddMinutes(new[] { new MinuteRow(Minute(0), "CPU package", ReadingScope.Component, 0.001, 60, 100, PowerKind.Cpu) });
        h.Store.AddProgramMinutes(Minute(0), new[] { new ProgramEnergy("ComfyUI", 0.001, 0.001, 30) });
        h.Store.AddTotalPeak(Minute(0), 300);
        h.Store.AddGap(new Gap(T0.AddMinutes(-20), T0.AddMinutes(-10), GapReason.Asleep, 0.07));
        var batch = RelayBatchBuilder.Build(h.Store, "pc-1", "Desktop", Minute(-1), Minute(0))!;

        using var ours = JsonDocument.Parse(JsonSerializer.Serialize(batch, RelayBatch.Json));
        using var theirs = JsonDocument.Parse(File.ReadAllText(ContractExample("ingest-pc.json")));
        var missing = new List<string>();
        Compare(ours.RootElement, theirs.RootElement, "$", missing);
        Assert.True(missing.Count == 0, "Not in the contract's example: " + string.Join(", ", missing));
    }

    private static void Compare(JsonElement ours, JsonElement contract, string path, List<string> missing)
    {
        switch (ours.ValueKind)
        {
            case JsonValueKind.Object:
                foreach (var p in ours.EnumerateObject())
                {
                    if (contract.ValueKind != JsonValueKind.Object || !contract.TryGetProperty(p.Name, out var other)) missing.Add($"{path}.{p.Name}");
                    else Compare(p.Value, other, $"{path}.{p.Name}", missing);
                }
                break;
            case JsonValueKind.Array when ours.GetArrayLength() > 0 && contract.ValueKind == JsonValueKind.Array && contract.GetArrayLength() > 0:
                Compare(ours[0], contract[0], path + "[]", missing);
                break;
        }
    }

    internal static string ContractExample(string name)
    {
        for (var dir = new DirectoryInfo(AppContext.BaseDirectory); dir is not null; dir = dir.Parent)
        {
            var candidate = Path.Combine(dir.FullName, "contract", "examples", name);
            if (File.Exists(candidate)) return candidate;
        }
        throw new FileNotFoundException("contract/examples/" + name + " not found above " + AppContext.BaseDirectory);
    }
}

public class RelayFeedTests : IDisposable
{
    private static readonly DateTimeOffset T0 = new(2026, 10, 22, 12, 0, 0, TimeSpan.Zero);
    private static long Minute(int i) => T0.AddMinutes(i).ToUnixTimeSeconds();

    private readonly TempHistory _h = new();
    private readonly FakeHouse _house = new();
    private readonly SettableClock _clock = new(T0.AddMinutes(30));
    private string ConfigPath => Path.Combine(_h.Dir, RelayConfig.FileName);

    public void Dispose() => _h.Dispose();

    private RelayFeed Feed(string machine = "KGERP") => new(_h.Store, ConfigPath, machine, _house, _clock);

    private void AddMinutes(int from, int to) =>
        _h.Store.AddMinutes(Enumerable.Range(from, to - from + 1).Select(i => new MinuteRow(Minute(i), "GPU", ReadingScope.Component, 0.001, 60, 100)).ToList());

    private static long UnixOf(JsonElement minute) => DateTimeOffset.Parse(minute.GetProperty("minute").GetString()!).ToUnixTimeSeconds();

    [Fact]
    public async Task WithNoConfigNothingIsSent()
    {
        AddMinutes(0, 5);
        Assert.Equal(0, await Feed().StepAsync(TestContext.Current.CancellationToken));
        Assert.Empty(_house.Requests);
    }

    [Fact]
    public async Task ABadAddressInTheConfigIsTreatedAsNoConfig()
    {
        AddMinutes(0, 5);
        File.WriteAllText(ConfigPath, """{"host":"http://evil.example/steal"}""");
        Assert.Equal(0, await Feed().StepAsync(TestContext.Current.CancellationToken));
        Assert.Empty(_house.Requests);
    }

    [Fact]
    public async Task SendsOnlyToTheHouseServersPushAddressWithItsDefaultPort()
    {
        new RelayConfig("house-box").Save(ConfigPath);
        AddMinutes(0, 5);
        await Feed().StepAsync(TestContext.Current.CancellationToken);
        Assert.Equal("http://house-box:8080/api/v1/ingest/pc", _house.Requests[0].Uri.ToString());

        new RelayConfig("192.168.1.9:9000").Save(ConfigPath);
        AddMinutes(6, 8);
        await Feed().StepAsync(TestContext.Current.CancellationToken);
        Assert.Equal("http://192.168.1.9:9000/api/v1/ingest/pc", _house.Requests[1].Uri.ToString());
    }

    [Fact]
    public async Task SendsNewWholeMinutesOnceAndRemembersWhereItGot()
    {
        new RelayConfig("house-box").Save(ConfigPath);
        AddMinutes(0, 5);

        var feed = Feed("KGERP_PC");
        Assert.Equal(6, await feed.StepAsync(TestContext.Current.CancellationToken));
        using (var first = _house.Sent(0))
        {
            Assert.Equal(1, first.RootElement.GetProperty("api_version").GetInt32());
            Assert.Equal("KGERP_PC", first.RootElement.GetProperty("source_name").GetString());
            Assert.Equal(6, first.RootElement.GetProperty("minutes").GetArrayLength());
        }
        Assert.Equal(0, await feed.StepAsync(TestContext.Current.CancellationToken));   // nothing new
        Assert.Single(_house.Requests);

        AddMinutes(6, 7);
        Assert.Equal(2, await Feed().StepAsync(TestContext.Current.CancellationToken)); // a new feed picks up from the saved watermark
        using var second = _house.Sent(1);
        Assert.Equal(new[] { Minute(6), Minute(7) }, second.RootElement.GetProperty("minutes").EnumerateArray().Select(UnixOf).ToArray());
    }

    [Fact]
    public async Task TheNewestMinutesAreLeftAloneUntilTheyAreSafe()
    {
        new RelayConfig("house-box").Save(ConfigPath);
        AddMinutes(25, 30);   // the clock is at minute 30: the last two minutes may still be written to

        await Feed().StepAsync(TestContext.Current.CancellationToken);
        using var sent = _house.Sent(0);
        Assert.Equal(Minute(28), sent.RootElement.GetProperty("minutes").EnumerateArray().Select(UnixOf).Max());

        _clock.Advance(TimeSpan.FromMinutes(5));
        await Feed().StepAsync(TestContext.Current.CancellationToken);
        using var later = _house.Sent(1);
        Assert.Equal(new[] { Minute(29), Minute(30) }, later.RootElement.GetProperty("minutes").EnumerateArray().Select(UnixOf).ToArray());
    }

    [Fact]
    public async Task TheFirstTimeOnlyAWeekOfHistoryIsSent()
    {
        new RelayConfig("house-box").Save(ConfigPath);
        var old = T0.AddMinutes(30).AddDays(-10).ToUnixTimeSeconds() / 60 * 60;
        var recent = T0.AddMinutes(30).AddDays(-1).ToUnixTimeSeconds() / 60 * 60;
        _h.Store.AddMinutes(new[]
        {
            new MinuteRow(old, "GPU", ReadingScope.Component, 0.001, 60, 100),
            new MinuteRow(recent, "GPU", ReadingScope.Component, 0.001, 60, 100),
        });

        Assert.Equal(1, await Feed().StepAsync(TestContext.Current.CancellationToken));
        using var sent = _house.Sent(0);
        Assert.Equal(recent, UnixOf(sent.RootElement.GetProperty("minutes")[0]));
    }

    [Fact]
    public async Task AServerThatIsAwayLosesNothingAndGetsTheBacklogWhenItReturns()
    {
        new RelayConfig("house-box").Save(ConfigPath);
        AddMinutes(0, 5);
        _house.Answers.Enqueue(HttpStatusCode.InternalServerError);

        var feed = Feed();
        Assert.Equal(0, await feed.StepAsync(TestContext.Current.CancellationToken));
        Assert.Contains("error (500)", feed.Status);
        Assert.Contains("error (500)", _h.Store.GetMeta(RelayFeed.StatusKey));

        Assert.Equal(0, await feed.StepAsync(TestContext.Current.CancellationToken));   // still waiting out the failure
        Assert.Single(_house.Requests);

        _clock.Advance(RelayFeed.RetryAfterError + TimeSpan.FromSeconds(1));
        AddMinutes(6, 7);
        Assert.Equal(8, await feed.StepAsync(TestContext.Current.CancellationToken));   // everything, from the start
        Assert.Equal(2, _house.Requests.Count);
        using var retry = _house.Sent(1);
        Assert.Equal(Minute(0), UnixOf(retry.RootElement.GetProperty("minutes")[0]));
        Assert.StartsWith("Sent 8 minute(s)", feed.Status);
    }

    [Fact]
    public async Task ADifferentVersionWaitsALongTimeAndSaysWhy()
    {
        new RelayConfig("house-box").Save(ConfigPath);
        AddMinutes(0, 5);
        _house.Answers.Enqueue(HttpStatusCode.UpgradeRequired);

        var feed = Feed();
        await feed.StepAsync(TestContext.Current.CancellationToken);
        Assert.Contains("different version", feed.Status);

        _clock.Advance(TimeSpan.FromMinutes(5));
        await feed.StepAsync(TestContext.Current.CancellationToken);
        Assert.Single(_house.Requests);   // still waiting
        _clock.Advance(RelayFeed.RetryAfterVersion);
        await feed.StepAsync(TestContext.Current.CancellationToken);
        Assert.Equal(2, _house.Requests.Count);
    }

    [Fact]
    public async Task ABatchTheServerKeepsRefusingIsSkippedSoItCannotHoldUpTheRest()
    {
        new RelayConfig("house-box").Save(ConfigPath);
        AddMinutes(0, 2);
        for (var i = 0; i < RelayFeed.RefusalsBeforeSkipping; i++) _house.Answers.Enqueue(HttpStatusCode.BadRequest);

        var feed = Feed();
        for (var attempt = 1; attempt <= RelayFeed.RefusalsBeforeSkipping; attempt++)
        {
            await feed.StepAsync(TestContext.Current.CancellationToken);
            Assert.Contains(attempt < RelayFeed.RefusalsBeforeSkipping ? "minute is odd" : "skipping", feed.Status);
            _clock.Advance(RelayFeed.RetryAfterError + TimeSpan.FromSeconds(1));
        }
        Assert.Equal(RelayFeed.RefusalsBeforeSkipping, _house.Requests.Count);

        AddMinutes(3, 4);   // new minutes after the skipped ones go through
        Assert.Equal(2, await feed.StepAsync(TestContext.Current.CancellationToken));
        using var last = _house.Sent(_house.Requests.Count - 1);
        Assert.Equal(new[] { Minute(3), Minute(4) }, last.RootElement.GetProperty("minutes").EnumerateArray().Select(UnixOf).ToArray());
    }

    [Fact]
    public async Task TheSourceIdIsMadeOnceAndKept()
    {
        new RelayConfig("house-box").Save(ConfigPath);
        AddMinutes(0, 2);
        await Feed().StepAsync(TestContext.Current.CancellationToken);
        AddMinutes(3, 4);
        await Feed().StepAsync(TestContext.Current.CancellationToken);

        using var a = _house.Sent(0);
        using var b = _house.Sent(1);
        var id = a.RootElement.GetProperty("source_id").GetString()!;
        Assert.Matches("^pc-[0-9a-f]{8}$", id);
        Assert.Equal(id, b.RootElement.GetProperty("source_id").GetString());
        Assert.Equal(id, _h.Store.GetMeta(RelayFeed.SourceIdKey));
    }

    private void SaveLook(DateTimeOffset at, double cpuWatts) =>
        _h.Store.SetLive(at, new[] { new PowerReading("CPU package", cpuWatts, ReadingScope.Component, PowerKind.Cpu) }, Array.Empty<string>());

    [Fact]
    public async Task ALiveLookIsSentOnlyWhenTheCollectorSavedANewOneAndLeavesTheHistoryAlone()
    {
        new RelayConfig("house-box").Save(ConfigPath);
        var feed = Feed();
        Assert.False(await feed.LiveStepAsync(TestContext.Current.CancellationToken));   // nothing saved yet
        Assert.Empty(_house.Requests);

        SaveLook(_clock.GetUtcNow(), 50);
        Assert.True(await feed.LiveStepAsync(TestContext.Current.CancellationToken));
        using (var first = _house.Sent(0))
        {
            Assert.Equal(0, first.RootElement.GetProperty("minutes").GetArrayLength());
            Assert.Equal(50, first.RootElement.GetProperty("live").GetProperty("readings")[0].GetProperty("watts").GetDouble());
        }

        Assert.False(await feed.LiveStepAsync(TestContext.Current.CancellationToken));  // the same look is not sent twice
        SaveLook(_clock.GetUtcNow().AddSeconds(1), 70);
        Assert.True(await feed.LiveStepAsync(TestContext.Current.CancellationToken));
        Assert.Equal(2, _house.Requests.Count);

        Assert.Null(_h.Store.GetMeta(RelayFeed.WatermarkKey));   // a look is not history: nothing was marked as sent
    }

    [Fact]
    public async Task AFailedLiveLookIsWaitedOutAndNeverReplacesTheHistoryStatus()
    {
        new RelayConfig("house-box").Save(ConfigPath);
        SaveLook(_clock.GetUtcNow(), 50);
        _house.Answers.Enqueue(HttpStatusCode.InternalServerError);

        var feed = Feed();
        Assert.False(await feed.LiveStepAsync(TestContext.Current.CancellationToken));
        Assert.Contains("error", feed.LiveStatus);
        Assert.Null(feed.Status);

        Assert.False(await feed.LiveStepAsync(TestContext.Current.CancellationToken));   // still waiting out the minute
        Assert.Single(_house.Requests);

        _clock.Advance(RelayFeed.RetryAfterError + TimeSpan.FromSeconds(1));
        SaveLook(_clock.GetUtcNow(), 60);
        Assert.True(await feed.LiveStepAsync(TestContext.Current.CancellationToken));
        Assert.Null(feed.LiveStatus);
    }

    [Fact]
    public async Task WithNoConfigNoLiveLookIsSent()
    {
        SaveLook(_clock.GetUtcNow(), 50);
        Assert.False(await Feed().LiveStepAsync(TestContext.Current.CancellationToken));
        Assert.Empty(_house.Requests);
    }
}

public class RelayConfigTests
{
    [Theory]
    [InlineData("house-box", true)]
    [InlineData("192.168.68.110", true)]
    [InlineData("192.168.68.110:8080", true)]
    [InlineData("house-box.lan", true)]
    [InlineData("http://house-box", false)]
    [InlineData("house-box/api", false)]
    [InlineData("192.168.68..110", false)]
    [InlineData("", false)]
    public void OnlyAPlainHostIsAccepted(string host, bool ok) => Assert.Equal(ok, RelayConfig.IsValidHost(host));

    [Fact]
    public void TheConfigRoundTripsAndABrokenFileMeansNoConfig()
    {
        var path = Path.Combine(Path.GetTempPath(), "kvg-relay-" + Guid.NewGuid().ToString("N")[..8] + ".json");
        try
        {
            Assert.Null(RelayConfig.Load(path));
            new RelayConfig("house-box:8081").Save(path);
            Assert.Equal("house-box:8081", RelayConfig.Load(path)!.Host);
            Assert.Equal("http://house-box:8081/api/v1/ingest/pc", RelayConfig.Load(path)!.PushUri.ToString());

            File.WriteAllText(path, "not json");
            Assert.Null(RelayConfig.Load(path));
            RelayConfig.Remove(path);
            Assert.False(File.Exists(path));
        }
        finally
        {
            if (File.Exists(path)) File.Delete(path);
        }
    }
}
