using System.Globalization;
using System.Net;
using Avalonia.Headless.XUnit;
using KVGuzzler.Collector;
using KVGuzzler.Core.Billing;
using KVGuzzler.Core.Carbon;
using KVGuzzler.Core.History;
using KVGuzzler.Core.Power;
using KVGuzzler.Views;
using Xunit;

namespace KVGuzzler.Tests;

/// <summary>A clock the test sets by hand.</summary>
internal sealed class SettableClock : TimeProvider
{
    private DateTimeOffset _now;
    public SettableClock(DateTimeOffset start) => _now = start;
    public override DateTimeOffset GetUtcNow() => _now;
    public void Advance(TimeSpan by) => _now += by;
}

/// <summary>Plays the part of Electricity Maps.</summary>
internal sealed class FakeElectricityMaps : HttpMessageHandler
{
    public const string Answer = """{"zone":"US-NW-PSCO","carbonIntensity":412,"datetime":"2026-10-19T14:00:00.000Z","updatedAt":"2026-10-19T13:55:00.000Z","emissionFactorType":"lifecycle","isEstimated":false,"estimationMethod":null}""";

    public Func<HttpRequestMessage, HttpResponseMessage> Respond { get; set; } =
        _ => new HttpResponseMessage(HttpStatusCode.OK) { Content = new StringContent(Answer) };

    public List<HttpRequestMessage> Requests { get; } = new();
    private readonly List<string?> _tokens = new();
    public IReadOnlyList<string?> Tokens => _tokens;

    protected override Task<HttpResponseMessage> SendAsync(HttpRequestMessage request, CancellationToken cancellationToken)
    {
        lock (Requests)
        {
            Requests.Add(request);
            _tokens.Add(request.Headers.TryGetValues("auth-token", out var v) ? v.Single() : null);
        }
        return Task.FromResult(Respond(request));
    }
}

public class CarbonConfigTests : IDisposable
{
    private readonly string _dir = Path.Combine(Path.GetTempPath(), "kvguzzler-carbon-" + Guid.NewGuid().ToString("N")[..8]);
    private string File_ => Path.Combine(_dir, CarbonConfig.FileName);

    public void Dispose()
    {
        try { Directory.Delete(_dir, recursive: true); } catch (IOException) { /* best effort */ }
    }

    [Theory]
    [InlineData("US-NW-PSCO", true)]
    [InlineData("DE", true)]
    [InlineData("GB", true)]
    [InlineData("39.74, -104.99", true)]
    [InlineData("39.74,-104.99", true)]
    [InlineData("-33.87, 151.21", true)]
    [InlineData("", false)]
    [InlineData("US NW", false)]
    [InlineData("US-NW-PSCO&x=1", false)]       // nothing typed can add to the request
    [InlineData("../../x", false)]
    [InlineData("95, 10", false)]                // not a latitude
    [InlineData("10, 190", false)]               // not a longitude
    [InlineData("1,2,3", false)]
    public void OnlyAZoneOrCoordinatesIsAccepted(string place, bool valid) => Assert.Equal(valid, CarbonConfig.IsValidPlace(place));

    [Theory]
    [InlineData("abcDEF123456", true)]
    [InlineData("a-b_c.d12345", true)]
    [InlineData("short", false)]
    [InlineData("has space 123456", false)]
    [InlineData("quote\"123456", false)]
    [InlineData("", false)]
    public void OnlyAKeyShapedLikeOneIsAccepted(string key, bool valid) => Assert.Equal(valid, CarbonConfig.IsValidKey(key));

    [Fact]
    public void ExplainsWhatIsMissing()
    {
        Assert.Contains("Paste your Electricity Maps API key", CarbonConfig.Problem("", "US-NW-PSCO"));
        Assert.Contains("does not look like an API key", CarbonConfig.Problem("no", "US-NW-PSCO"));
        Assert.Contains("Enter your zone", CarbonConfig.Problem("abcdefgh1234", ""));
        Assert.Contains("zone code like", CarbonConfig.Problem("abcdefgh1234", "not a place!"));
        Assert.Null(CarbonConfig.Problem("abcdefgh1234", "US-NW-PSCO"));
    }

    [Fact]
    public void TheRequestGoesOnlyToElectricityMapsWhateverIsTyped()
    {
        Assert.Equal("https://api.electricitymap.org/v3/carbon-intensity/latest?zone=US-NW-PSCO", new CarbonConfig("abcdefgh1234", "US-NW-PSCO").LatestUri().ToString());
        Assert.Equal("https://api.electricitymap.org/v3/carbon-intensity/latest?lat=39.74&lon=-104.99", new CarbonConfig("abcdefgh1234", "39.74, -104.99").LatestUri().ToString());

        var old = CultureInfo.CurrentCulture;
        CultureInfo.CurrentCulture = new CultureInfo("de-DE");   // a decimal comma must not leak into the address
        try { Assert.Contains("lat=39.74&lon=-104.99", new CarbonConfig("abcdefgh1234", "39.74, -104.99").LatestUri().ToString()); }
        finally { CultureInfo.CurrentCulture = old; }
    }

    [Fact]
    public void SavesLoadsAndRemoves()
    {
        new CarbonConfig("abcdefgh1234", "US-NW-PSCO").Save(File_);
        var loaded = CarbonConfig.Load(File_)!;
        Assert.Equal("abcdefgh1234", loaded.ApiKey);
        Assert.Equal("US-NW-PSCO", loaded.Place);

        CarbonConfig.Remove(File_);
        CarbonConfig.Remove(File_);   // harmless twice
        Assert.Null(CarbonConfig.Load(File_));
    }

    [Fact]
    public void AMissingDamagedOrHandEditedFileMeansNotSetUp()
    {
        Assert.Null(CarbonConfig.Load(File_));
        Directory.CreateDirectory(_dir);
        File.WriteAllText(File_, "not json {");
        Assert.Null(CarbonConfig.Load(File_));
        File.WriteAllText(File_, """{"apiKey":"abcdefgh1234","place":"x&y=1"}""");
        Assert.Null(CarbonConfig.Load(File_));
        File.WriteAllText(File_, """{"apiKey":null,"place":"DE"}""");
        Assert.Null(CarbonConfig.Load(File_));
    }
}

public class ElectricityMapsClientTests
{
    private static readonly DateTimeOffset Now = new(2026, 10, 19, 15, 30, 0, TimeSpan.Zero);
    private static readonly CarbonConfig Config = new("abcdefgh1234", "US-NW-PSCO");

    [Fact]
    public void ReadsTheFigureItsHourAndWhetherItWasModelled()
    {
        var reading = ElectricityMapsClient.ParseLatest(FakeElectricityMaps.Answer, Now);
        Assert.Equal(412, reading.GramsPerKwh);
        Assert.Equal(new DateTimeOffset(2026, 10, 19, 14, 0, 0, TimeSpan.Zero), reading.At);
        Assert.False(reading.IsEstimated);

        Assert.True(ElectricityMapsClient.ParseLatest("""{"carbonIntensity":300.5,"isEstimated":true}""", Now).IsEstimated);
    }

    [Fact]
    public void AMissingTimeMeansNowAndAWrappedAnswerIsStillRead()
    {
        var bare = ElectricityMapsClient.ParseLatest("""{"carbonIntensity":300}""", Now);
        Assert.Equal(Now, bare.At);
        Assert.Equal(250, ElectricityMapsClient.ParseLatest("""{"data":{"carbonIntensity":250,"datetime":"2026-10-19T10:00:00Z"}}""", Now).GramsPerKwh);
    }

    [Theory]
    [InlineData("""{"zone":"DE"}""")]                         // no figure
    [InlineData("""{"carbonIntensity":"412"}""")]            // text, not a number
    [InlineData("""{"carbonIntensity":null}""")]
    [InlineData("""{"carbonIntensity":-5}""")]
    [InlineData("""{"carbonIntensity":999999}""")]            // not a believable grid
    [InlineData("""[1,2]""")]
    [InlineData("<html>login</html>")]
    [InlineData("")]
    public void AnythingElseIsReportedAsNotACarbonIntensity(string answer) =>
        Assert.Contains("not with a carbon intensity", Assert.Throws<CarbonException>(() => ElectricityMapsClient.ParseLatest(answer, Now)).Message);

    [Fact]
    public async Task AsksWithAGetAndTheKeyInTheHeaderOnly()
    {
        var api = new FakeElectricityMaps();
        var reading = await ElectricityMapsClient.ReadLatestAsync(new HttpClient(api), Config, Now, TestContext.Current.CancellationToken);

        Assert.Equal(412, reading.GramsPerKwh);
        var request = Assert.Single(api.Requests);
        Assert.Equal(HttpMethod.Get, request.Method);
        Assert.Equal("api.electricitymap.org", request.RequestUri!.Host);
        Assert.DoesNotContain("abcdefgh1234", request.RequestUri.ToString());   // the key is never in the address
        Assert.Equal("abcdefgh1234", Assert.Single(api.Tokens));
    }

    [Theory]
    [InlineData(401, "did not accept the key", false)]
    [InlineData(403, "did not accept the key", false)]
    [InlineData(404, "does not know that zone", false)]
    [InlineData(429, "slow down", true)]
    [InlineData(500, "error (500)", false)]
    public async Task ExplainsWhatTheErrorMeans(int status, string expected, bool rateLimited)
    {
        var api = new FakeElectricityMaps { Respond = _ => new HttpResponseMessage((HttpStatusCode)status) };
        var e = await Assert.ThrowsAsync<CarbonException>(() => ElectricityMapsClient.ReadLatestAsync(new HttpClient(api), Config, Now, TestContext.Current.CancellationToken));
        Assert.Contains(expected, e.Message);
        Assert.Equal(rateLimited, e.RateLimited);
    }

    [Fact]
    public async Task AnUnreachableServiceSaysSo()
    {
        var api = new FakeElectricityMaps { Respond = _ => throw new HttpRequestException("No such host is known") };
        var e = await Assert.ThrowsAsync<CarbonException>(() => ElectricityMapsClient.ReadLatestAsync(new HttpClient(api), Config, Now, TestContext.Current.CancellationToken));
        Assert.StartsWith("Could not reach Electricity Maps", e.Message);
    }

    [Fact]
    public async Task AServiceThatNeverAnswersTimesOutWithAPlainMessage()
    {
        var e = await Assert.ThrowsAsync<CarbonException>(() => ElectricityMapsClient.ReadLatestAsync(
            new HttpClient(new Hanging()), Config, Now, TestContext.Current.CancellationToken, TimeSpan.FromMilliseconds(100)));
        Assert.Contains("did not answer", e.Message);
    }

    private sealed class Hanging : HttpMessageHandler
    {
        protected override async Task<HttpResponseMessage> SendAsync(HttpRequestMessage request, CancellationToken cancellationToken)
        {
            await Task.Delay(Timeout.Infinite, cancellationToken);
            return new HttpResponseMessage();
        }
    }
}

public class CarbonFeedTests : IDisposable
{
    private static readonly DateTimeOffset T0 = new(2026, 10, 19, 14, 5, 0, TimeSpan.Zero);
    private readonly string _dir = Path.Combine(Path.GetTempPath(), "kvguzzler-carbonfeed-" + Guid.NewGuid().ToString("N")[..8]);
    private string ConfigPath => Path.Combine(_dir, CarbonConfig.FileName);
    private readonly TempHistory _h = new();
    private readonly FakeElectricityMaps _api = new();
    private readonly SettableClock _clock = new(T0);

    public void Dispose()
    {
        _h.Dispose();
        try { Directory.Delete(_dir, recursive: true); } catch (IOException) { /* best effort */ }
    }

    private CarbonFeed Make() => new(_h.Store, ConfigPath, _api, _clock);

    [Fact]
    public async Task WithNothingSetUpItAsksNothingAndSaysNothing()
    {
        var feed = Make();
        Assert.False(await feed.StepAsync(TestContext.Current.CancellationToken));
        Assert.Empty(_api.Requests);
        Assert.Null(_h.Store.GetCarbonStatus());
    }

    [Fact]
    public async Task SavesTheReadingUnderItsHourAndSaysSo()
    {
        new CarbonConfig("abcdefgh1234", "US-NW-PSCO").Save(ConfigPath);
        var feed = Make();
        Assert.True(await feed.StepAsync(TestContext.Current.CancellationToken));

        var hour = new DateTimeOffset(2026, 10, 19, 14, 0, 0, TimeSpan.Zero);
        Assert.Equal(412, _h.Store.QueryCarbon(hour, hour.AddHours(1))[hour.ToUnixTimeSeconds()]);
        var status = _h.Store.GetCarbonStatus()!;
        Assert.True(status.Ok);
        Assert.Equal(412, status.GramsPerKwh);
        Assert.Equal(T0, status.CheckedAt);
    }

    [Fact]
    public async Task AsksAgainOnlyOnceTheIntervalHasPassed()
    {
        new CarbonConfig("abcdefgh1234", "US-NW-PSCO").Save(ConfigPath);
        var feed = Make();
        await feed.StepAsync(TestContext.Current.CancellationToken);

        _clock.Advance(TimeSpan.FromMinutes(10));
        Assert.False(await feed.StepAsync(TestContext.Current.CancellationToken));
        Assert.Single(_api.Requests);

        _clock.Advance(CarbonFeed.Interval);
        Assert.True(await feed.StepAsync(TestContext.Current.CancellationToken));
        Assert.Equal(2, _api.Requests.Count);
    }

    [Fact]
    public async Task ANewKeyOrPlaceIsUsedAtOnceNotAtTheNextInterval()
    {
        new CarbonConfig("abcdefgh1234", "US-NW-PSCO").Save(ConfigPath);
        var feed = Make();
        await feed.StepAsync(TestContext.Current.CancellationToken);

        new CarbonConfig("abcdefgh1234", "DE").Save(ConfigPath);
        _clock.Advance(TimeSpan.FromSeconds(30));
        Assert.True(await feed.StepAsync(TestContext.Current.CancellationToken));
        Assert.Contains("zone=DE", _api.Requests[1].RequestUri!.Query);
    }

    [Fact]
    public async Task AFailureIsSavedWithItsReasonAndRetriedSoonerThanASuccess()
    {
        new CarbonConfig("abcdefgh1234", "US-NW-PSCO").Save(ConfigPath);
        _api.Respond = _ => new HttpResponseMessage(HttpStatusCode.Unauthorized);
        var feed = Make();
        await feed.StepAsync(TestContext.Current.CancellationToken);

        var status = _h.Store.GetCarbonStatus()!;
        Assert.False(status.Ok);
        Assert.Contains("did not accept the key", status.Error);
        Assert.Empty(_h.Store.QueryCarbon(T0.AddDays(-1), T0.AddDays(1)));   // nothing made up

        _clock.Advance(CarbonFeed.RetryAfterError + TimeSpan.FromSeconds(1));
        _api.Respond = _ => new HttpResponseMessage(HttpStatusCode.OK) { Content = new StringContent(FakeElectricityMaps.Answer) };
        Assert.True(await feed.StepAsync(TestContext.Current.CancellationToken));
        Assert.True(_h.Store.GetCarbonStatus()!.Ok);
    }

    [Fact]
    public async Task BeingToldToSlowDownMeansWaitingAGoodWhile()
    {
        new CarbonConfig("abcdefgh1234", "US-NW-PSCO").Save(ConfigPath);
        _api.Respond = _ => new HttpResponseMessage((HttpStatusCode)429);
        var feed = Make();
        await feed.StepAsync(TestContext.Current.CancellationToken);

        _clock.Advance(CarbonFeed.RetryAfterError + TimeSpan.FromMinutes(1));
        Assert.False(await feed.StepAsync(TestContext.Current.CancellationToken));   // not yet: it asked for time
        _clock.Advance(CarbonFeed.RetryAfterLimit);
        Assert.True(await feed.StepAsync(TestContext.Current.CancellationToken));
    }

    [Fact]
    public async Task RemovingTheKeyIsSaidOnceAndStopsTheAsking()
    {
        new CarbonConfig("abcdefgh1234", "US-NW-PSCO").Save(ConfigPath);
        var feed = Make();
        await feed.StepAsync(TestContext.Current.CancellationToken);

        CarbonConfig.Remove(ConfigPath);
        _clock.Advance(TimeSpan.FromHours(2));
        Assert.False(await feed.StepAsync(TestContext.Current.CancellationToken));
        Assert.Equal("Not set up.", _h.Store.GetCarbonStatus()!.Error);
        Assert.Single(_api.Requests);
        Assert.Single(_h.Store.QueryCarbon(T0.AddDays(-1), T0.AddDays(1)));   // the hours already saved are kept
    }
}

public class CarbonCalculatorTests
{
    private static readonly DateTimeOffset T0 = new(2026, 10, 19, 12, 0, 0, TimeSpan.Zero);

    private static TempHistory Seeded()
    {
        var h = new TempHistory();
        var noon = T0.ToUnixTimeSeconds();
        h.Store.AddMinutes(new[]
        {
            new MinuteRow(noon, WallMeterSource.ReadingName, ReadingScope.Wall, 0.100, 60, 300),                 // 12:00, 100 Wh
            new MinuteRow(noon + 3600, WallMeterSource.ReadingName, ReadingScope.Wall, 0.200, 60, 300),          // 13:00, 200 Wh
            new MinuteRow(noon + 7200, WallMeterSource.ReadingName, ReadingScope.Wall, 0.100, 60, 300),          // 14:00, 100 Wh: no figure
            new MinuteRow(noon, "Monitor", ReadingScope.Peripheral, 0.010, 60, 40),
        });
        h.Store.AddCarbon(T0, 400, estimated: false);
        h.Store.AddCarbon(T0.AddHours(1), 200, estimated: true);
        return h;
    }

    [Fact]
    public void EachHoursEnergyIsValuedAtItsOwnHoursIntensity()
    {
        using var h = Seeded();
        var result = CarbonCalculator.Compute(h.Store, T0, T0.AddHours(2).AddMinutes(30))!;

        // 0.1 kWh x 400 g + 0.2 kWh x 200 g + 0.1 kWh at the average of the known hours (300 g) = 40 + 40 + 30 g.
        Assert.Equal(0.110, result.Kg, precision: 9);
        Assert.Equal(0.4, result.Kwh, precision: 9);
        Assert.Equal(0.1, result.EstimatedKwh, precision: 9);
        Assert.Equal(0.25, result.EstimatedShare, precision: 9);
        Assert.Equal(275, result.AverageGramsPerKwh, precision: 6);
    }

    [Fact]
    public void ExtraMetersAreAddedOnTopOrCountedAlone()
    {
        using var h = Seeded();
        var to = T0.AddHours(3);
        var pc = CarbonCalculator.Compute(h.Store, T0, to)!;
        var both = CarbonCalculator.Compute(h.Store, T0, to, includePc: true, new[] { "Monitor" })!;
        var only = CarbonCalculator.Compute(h.Store, T0, to, includePc: false, new[] { "Monitor" })!;

        Assert.Equal(0.010 * 400 / 1000, only.Kg, precision: 9);                  // the monitor's 10 Wh in the 400 g hour
        Assert.Equal(pc.Kg + only.Kg, both.Kg, precision: 9);
        Assert.Equal(0, CarbonCalculator.Compute(h.Store, T0, to, includePc: false)!.Kg);   // nothing counted: nothing
    }

    [Fact]
    public void EnergyAcrossGapsHasNoHourSoItUsesThePeriodAverage()
    {
        using var h = Seeded();
        h.Store.AddGap(new Gap(T0.AddHours(5), T0.AddHours(9), GapReason.Asleep));
        h.Store.SetGapPlugEnergy(T0.AddHours(5), 0.040);                 // the wall plug counted 40 Wh while the PC slept
        h.Store.SetGapPeripheralEnergy(T0.AddHours(5), "Monitor", 0.020);

        var without = CarbonCalculator.Compute(h.Store, T0, T0.AddHours(3), includePc: true, new[] { "Monitor" })!;
        var with = CarbonCalculator.Compute(h.Store, T0, T0.AddHours(10), includePc: true, new[] { "Monitor" })!;
        Assert.Equal(without.Kg + (0.040 + 0.020) * 300 / 1000, with.Kg, precision: 9);   // at 300 g, the average of 400 and 200
        Assert.Equal(without.EstimatedKwh + 0.060, with.EstimatedKwh, precision: 9);
    }

    [Fact]
    public void WithNoIntensityEverSavedThereIsNothingToShow()
    {
        using var h = new TempHistory();
        h.Store.AddMinutes(new[] { new MinuteRow(T0.ToUnixTimeSeconds(), WallMeterSource.ReadingName, ReadingScope.Wall, 0.1, 60, 300) });
        Assert.Null(CarbonCalculator.Compute(h.Store, T0, T0.AddHours(1)));
    }

    [Fact]
    public void APeriodBeforeAnyFigureWasSavedUsesTheAverageOfAllOfThem()
    {
        using var h = new TempHistory();
        h.Store.AddMinutes(new[] { new MinuteRow(T0.AddDays(-30).ToUnixTimeSeconds(), WallMeterSource.ReadingName, ReadingScope.Wall, 0.5, 60, 300) });
        h.Store.AddCarbon(T0, 400, false);
        h.Store.AddCarbon(T0.AddHours(1), 200, false);

        var result = CarbonCalculator.Compute(h.Store, T0.AddDays(-31), T0.AddDays(-29))!;
        Assert.Equal(0.5 * 300 / 1000, result.Kg, precision: 9);
        Assert.Equal(1.0, result.EstimatedShare, precision: 9);   // all of it, and it says so
    }

    [Fact]
    public void AHourWithANewerFigureReplacesTheOlder()
    {
        using var h = new TempHistory();
        h.Store.AddCarbon(T0.AddMinutes(5), 400, false);
        h.Store.AddCarbon(T0.AddMinutes(40), 380, true);   // the same hour, refined
        var only = Assert.Single(h.Store.QueryCarbon(T0, T0.AddHours(1)));
        Assert.Equal(380, only.Value);
    }

    [Fact]
    public void TheStatsCarryTheCarbonOfTheSameEnergy()
    {
        using var h = Seeded();
        var flat = new RateSchedule(RateMode.OneRate, 0.20m, Array.Empty<Bill>(), TimeZoneInfo.Utc);
        var stat = UsageSummary.Compute(h.Store, flat, null, T0.AddHours(3), new[] { "Monitor" });
        Assert.NotNull(stat.Carbon);
        Assert.Equal(stat.Kwh, stat.Carbon!.Kwh, precision: 9);   // the same energy as the headline

        using var none = new TempHistory();
        none.Store.AddMinutes(new[] { new MinuteRow(T0.ToUnixTimeSeconds(), WallMeterSource.ReadingName, ReadingScope.Wall, 0.1, 60, 300) });
        Assert.Null(UsageSummary.Compute(none.Store, flat, null, T0.AddHours(1)).Carbon);
    }

    [Fact]
    public void TheCsvGetsCarbonColumnsOnlyOnceThereIsIntensityData()
    {
        using var plain = new TempHistory();
        plain.Store.AddMinutes(new[] { new MinuteRow(T0.ToUnixTimeSeconds(), WallMeterSource.ReadingName, ReadingScope.Wall, 0.1, 60, 300) });
        var flat = new RateSchedule(RateMode.OneRate, 0.20m, Array.Empty<Bill>(), TimeZoneInfo.Utc);
        Assert.DoesNotContain("co2", HistoryExport.DailyCsv(plain.Store, flat, T0, T0.AddHours(3), TimeZoneInfo.Utc));

        using var h = Seeded();
        var lines = HistoryExport.DailyCsv(h.Store, flat, T0, T0.AddHours(3), TimeZoneInfo.Utc).Split(new[] { "\r\n", "\n" }, StringSplitOptions.RemoveEmptyEntries);
        Assert.EndsWith(",pc_co2_kg,peripherals_co2_kg", lines[0]);
        Assert.EndsWith(",0.11,0.004", lines[1]);   // 110 g for the PC, 10 Wh x 400 g = 4 g for the monitor
    }
}

public class CarbonUiTests
{
    private static void UseInvariantCulture() => CultureInfo.CurrentCulture = CultureInfo.InvariantCulture;
    private static string ConfigPath(TempData t) => Path.Combine(t.HistoryDir, CarbonConfig.FileName);

    private static CarbonReading Reading(double g = 412) => new(g, DateTimeOffset.UtcNow, false);

    [AvaloniaFact]
    public async Task SaveOnlyKeepsAKeyAndPlaceThatGotAReading_AndNeverShowsTheKeyAgain()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();
        window.CarbonKeyBox.Text = "abcdefgh1234";
        window.CarbonPlaceBox.Text = "US-NW-PSCO";

        t.Services.ReadCarbonAsync = _ => throw new CarbonException("Electricity Maps did not accept the key.");
        await window.SaveCarbonAsync();
        Assert.StartsWith("Not saved. Electricity Maps did not accept", window.CarbonStatusText.Text);
        Assert.False(File.Exists(ConfigPath(t)));

        t.Services.ReadCarbonAsync = _ => Task.FromResult(Reading());
        await window.SaveCarbonAsync();
        Assert.StartsWith("Saved.", window.CarbonStatusText.Text);
        Assert.Equal("abcdefgh1234", CarbonConfig.Load(ConfigPath(t))!.ApiKey);
        Assert.Equal("", window.CarbonKeyBox.Text);                                  // the key is not displayed again
        Assert.Contains("Key saved", window.CarbonKeyBox.PlaceholderText);
        Assert.Equal("US-NW-PSCO", window.CarbonPlaceBox.Text);
    }

    [AvaloniaFact]
    public async Task LeavingTheKeyEmptyKeepsTheSavedOneAndChangesOnlyThePlace()
    {
        UseInvariantCulture();
        using var t = new TempData();
        new CarbonConfig("abcdefgh1234", "US-NW-PSCO").Save(ConfigPath(t));
        CarbonConfig? asked = null;
        t.Services.ReadCarbonAsync = c => { asked = c; return Task.FromResult(Reading()); };
        var window = new MainWindow(t.Services);
        window.Show();
        Assert.Equal("US-NW-PSCO", window.CarbonPlaceBox.Text);

        window.CarbonPlaceBox.Text = "DE";
        await window.SaveCarbonAsync();
        Assert.Equal(new CarbonConfig("abcdefgh1234", "DE"), asked);
        Assert.Equal(new CarbonConfig("abcdefgh1234", "DE"), CarbonConfig.Load(ConfigPath(t)));
    }

    [AvaloniaFact]
    public async Task TestShowsTheFigureAndSavesNothing_AndBadInputIsRefusedBeforeAnythingIsSent()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var asked = false;
        t.Services.ReadCarbonAsync = _ => { asked = true; return Task.FromResult(Reading(412.4)); };
        var window = new MainWindow(t.Services);
        window.Show();

        window.CarbonKeyBox.Text = "bad key";
        window.CarbonPlaceBox.Text = "US-NW-PSCO";
        await window.TestCarbonAsync();
        Assert.Contains("does not look like an API key", window.CarbonStatusText.Text);
        Assert.False(asked);

        window.CarbonKeyBox.Text = "abcdefgh1234";
        window.CarbonPlaceBox.Text = "1,2,3";
        await window.SaveCarbonAsync();
        Assert.Contains("zone code like", window.CarbonStatusText.Text);
        Assert.False(asked);

        window.CarbonPlaceBox.Text = "US-NW-PSCO";
        await window.TestCarbonAsync();
        Assert.Equal("Reached Electricity Maps. US-NW-PSCO is at 412 g/kWh right now.", window.CarbonStatusText.Text);
        Assert.False(File.Exists(ConfigPath(t)));
    }

    [AvaloniaFact]
    public async Task AnUnexpectedErrorIsAMessageNotACrash()
    {
        UseInvariantCulture();
        using var t = new TempData();
        t.Services.ReadCarbonAsync = _ => throw new InvalidOperationException("boom");
        var window = new MainWindow(t.Services);
        window.Show();
        window.CarbonKeyBox.Text = "abcdefgh1234";
        window.CarbonPlaceBox.Text = "DE";
        await window.TestCarbonAsync();
        Assert.StartsWith("Something unexpected went wrong: boom", window.CarbonStatusText.Text);
        await window.SaveCarbonAsync();
        Assert.StartsWith("Not saved. Something unexpected", window.CarbonStatusText.Text);
    }

    [AvaloniaFact]
    public async Task TheCostCardShowsTheCarbonAndTheCollectorsLatestWord()
    {
        UseInvariantCulture();
        using var t = new TempData();
        t.Services.QueryCollectorService = () => ServiceState.Running;
        new CarbonConfig("abcdefgh1234", "US-NW-PSCO").Save(ConfigPath(t));
        var hour = DateTimeOffset.UtcNow.AddMinutes(-90);
        using (var store = new HistoryStore(Path.Combine(t.HistoryDir, HistoryStore.FileName)))
        {
            store.AddMinutes(new[] { new MinuteRow(hour.ToUnixTimeSeconds() / 60 * 60, WallMeterSource.ReadingName, ReadingScope.Wall, 1.0, 60, 300) });
            store.AddCarbon(hour, 400, false);
            store.SetCarbonStatus(new CarbonStatus(true, DateTimeOffset.UtcNow, 400, hour));
            store.SetLive(DateTimeOffset.UtcNow, new List<PowerReading> { new(WallMeterSource.ReadingName, 200, ReadingScope.Wall) }, Array.Empty<string>());
        }

        var window = new MainWindow(t.Services);
        window.Show();
        await window.RefreshCollectorAsync(includeService: true);

        Assert.True(window.StatCarbonCaption.IsVisible);
        Assert.Equal("≈ 400 g CO2 · grid average 400 g/kWh", window.StatCarbonCaption.Text);   // 1 kWh at 400 g, all from its own hour
        Assert.StartsWith("Last reading 400 g/kWh", window.CarbonFeedText.Text);

        // The collector could not get a reading: the reason is shown, and the carbon already saved is still used.
        using (var store = new HistoryStore(Path.Combine(t.HistoryDir, HistoryStore.FileName)))
            store.SetCarbonStatus(new CarbonStatus(false, DateTimeOffset.UtcNow, Error: "Could not reach Electricity Maps: offline"));
        await window.RefreshCollectorAsync(includeService: false);
        Assert.Contains("could not get a reading", window.CarbonFeedText.Text);
        Assert.Contains("Could not reach Electricity Maps: offline", window.CarbonFeedText.Text);
        Assert.True(window.StatCarbonCaption.IsVisible);
    }

    [AvaloniaFact]
    public async Task WithoutAnyIntensityTheCardSaysNothingAboutCarbon()
    {
        UseInvariantCulture();
        using var t = new TempData();
        t.Services.QueryCollectorService = () => ServiceState.Running;
        using (var store = new HistoryStore(Path.Combine(t.HistoryDir, HistoryStore.FileName)))
        {
            store.AddMinutes(new[] { new MinuteRow(DateTimeOffset.UtcNow.AddMinutes(-30).ToUnixTimeSeconds() / 60 * 60, WallMeterSource.ReadingName, ReadingScope.Wall, 1.0, 60, 300) });
            store.SetLive(DateTimeOffset.UtcNow, new List<PowerReading> { new(WallMeterSource.ReadingName, 200, ReadingScope.Wall) }, Array.Empty<string>());
        }
        var window = new MainWindow(t.Services);
        window.Show();
        await window.RefreshCollectorAsync(includeService: true);
        Assert.False(window.StatCarbonCaption.IsVisible);
        Assert.False(window.CarbonFeedText.IsVisible);
    }

    [AvaloniaFact]
    public void RemoveForgetsTheKeyAndTheBoxesClear()
    {
        UseInvariantCulture();
        using var t = new TempData();
        new CarbonConfig("abcdefgh1234", "US-NW-PSCO").Save(ConfigPath(t));
        var window = new MainWindow(t.Services);
        window.Show();
        window.RemoveCarbonButton.RaiseEvent(new Avalonia.Interactivity.RoutedEventArgs(Avalonia.Controls.Button.ClickEvent));
        Assert.False(File.Exists(ConfigPath(t)));
        Assert.Equal("", window.CarbonPlaceBox.Text);
        Assert.Contains("hours already saved are kept", window.CarbonStatusText.Text);
    }

    [Theory]
    [InlineData(0.0, "0 g")]
    [InlineData(0.845, "845 g")]
    [InlineData(1.0, "1.0 kg")]
    [InlineData(12.345, "12.35 kg")]
    public void SmallAmountsAreGramsAndBigOnesKilograms(double kg, string expected)
    {
        UseInvariantCulture();
        Assert.Equal(expected, MainWindow.Mass(kg));
    }
}
