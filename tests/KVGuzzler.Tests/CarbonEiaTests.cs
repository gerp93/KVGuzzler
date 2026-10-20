using System.Globalization;
using System.Net;
using Avalonia.Controls;
using Avalonia.Headless.XUnit;
using KVGuzzler.Collector;
using KVGuzzler.Core.Carbon;
using KVGuzzler.Core.History;
using KVGuzzler.Core.Power;
using KVGuzzler.Views;
using Xunit;

namespace KVGuzzler.Tests;

/// <summary>Builds answers shaped like EIA's hourly generation by fuel (captured from a real MISO answer, with its field names).</summary>
internal static class EiaJson
{
    public static string Row(string period, string fuel, string value) =>
        $$"""{"period":"{{period}}","respondent":"MISO","respondent-name":"Midcontinent Independent System Operator, Inc.","fueltype":"{{fuel}}","type-name":"x","value":"{{value}}","value-units":"megawatthours"}""";

    /// <summary>The hour 04:00 UTC on 19 Oct 2026 as MISO reported it, including battery storage (negative, charging).</summary>
    public static IEnumerable<string> RealHour(string period = "2026-10-19T04") => new[]
    {
        Row(period, "BAT", "-214"), Row(period, "COL", "16146"), Row(period, "NG", "20970"), Row(period, "NUC", "8329"),
        Row(period, "OTH", "536"), Row(period, "SUN", "8"), Row(period, "WAT", "546"), Row(period, "WND", "18383"),
    };

    public static string Answer(params string[] rows) =>
        """{"warnings":[{"warning":"incomplete return","description":"The API can only return 5000 rows."}],"response":{"total":"493927","dateFormat":"YYYY-MM-DD\"T\"HH24","frequency":"hourly","data":[""" + string.Join(",", rows) + "]}}";
}

public class EiaParsingTests
{
    [Fact]
    public void AnHoursIntensityIsEachFuelsLifeCycleEmissionsWeightedByWhatItGenerated()
    {
        var reading = Assert.Single(EiaClient.Parse(EiaJson.Answer(EiaJson.RealHour().ToArray())));
        // (16146 MWh coal x 820 + 20970 gas x 490 + 8329 nuclear x 12 + 536 other x 230 + 8 solar x 45 + 546 hydro x 24 + 18383 wind x 11) / 64918 MWh.
        Assert.Equal(368.99, reading.GramsPerKwh, precision: 2);
        Assert.Equal(new DateTimeOffset(2026, 10, 19, 4, 0, 0, TimeSpan.Zero), reading.At);
        Assert.False(reading.IsEstimated);
    }

    [Fact]
    public void BatteryStorageAndUnknownFuelsAreLeftOutAndANegativeFigureCountsAsNone()
    {
        var plain = EiaClient.Intensity(new[] { ("COL", 100.0), ("NG", 100.0), ("WND", 100.0) })!.Value;
        var withExtras = EiaClient.Intensity(new[] { ("COL", 100.0), ("NG", 100.0), ("WND", 100.0), ("BAT", -500.0), ("BAT", 900.0), ("PS", 300.0), ("UES", 50.0), ("WAT", -40.0) })!.Value;
        Assert.Equal(plain, withExtras, precision: 9);
        Assert.Equal((820 + 490 + 11) / 3.0, plain, precision: 9);
        Assert.Null(EiaClient.Intensity(new[] { ("BAT", 10.0), ("COL", 0.0) }));   // nothing that counts: no figure, not zero
    }

    [Fact]
    public void SeveralHoursComeBackOldestFirstAndAPartialHourIsSkipped()
    {
        var rows = EiaJson.RealHour("2026-10-19T05").Concat(EiaJson.RealHour("2026-10-19T03")).Concat(new[]
        {
            EiaJson.Row("2026-10-19T06", "COL", "16000"), EiaJson.Row("2026-10-19T06", "NG", "21000"),   // the newest hour, only partly reported
        });
        var readings = EiaClient.Parse(EiaJson.Answer(rows.ToArray()));
        Assert.Equal(new[] { 3, 5 }, readings.Select(r => r.At.Hour));
    }

    [Fact]
    public void NumbersMayBeTextOrNumbersAndOddRowsAreIgnored()
    {
        var json = """
            {"response":{"data":[
              {"period":"2026-10-19T04","fueltype":"COL","value":300},
              {"period":"2026-10-19T04","fueltype":"NG","value":"300"},
              {"period":"2026-10-19T04","fueltype":"WND","value":"300.5"},
              {"period":"2026-10-19T04","fueltype":"SUN","value":"n/a"},
              {"period":"2026-10-19T04","fueltype":"NUC","value":null},
              {"period":"bad","fueltype":"COL","value":"1"},
              {"fueltype":"COL","value":"1"},
              {"period":"2026-10-19T04","value":"1"},
              5]}}
            """;
        var reading = Assert.Single(EiaClient.Parse(json));
        Assert.Equal((300 * 820 + 300 * 490 + 300.5 * 11) / 900.5, reading.GramsPerKwh, precision: 6);
    }

    [Theory]
    [InlineData("")]
    [InlineData("<html>maintenance</html>")]
    [InlineData("[]")]
    [InlineData("""{"response":{}}""")]
    [InlineData("""{"response":{"data":5}}""")]
    [InlineData("""{"nothing":1}""")]
    public void AnythingThatIsNotGenerationDataIsAMessage(string answer) =>
        Assert.Contains("not with generation data", Assert.Throws<CarbonException>(() => EiaClient.Parse(answer)).Message);
}

public class EiaClientTests
{
    private static readonly CarbonConfig Config = new("abcdefghijklmnopqrstuvwxyz0123456789ABCD", "MISO") { Source = CarbonSources.Eia };

    private static FakeElectricityMaps Api(string body) =>
        new() { Respond = _ => new HttpResponseMessage(HttpStatusCode.OK) { Content = new StringContent(body) } };

    [Fact]
    public void TheRequestGoesOnlyToEiaWithTheAuthorityAndKeyEncoded()
    {
        var uri = EiaClient.RequestUri(Config with { Place = "MISO", ApiKey = "k&x=1 #y12345678" });
        Assert.Equal("api.eia.gov", uri.Host);
        Assert.Equal("/v2/electricity/rto/fuel-type-data/data/", uri.AbsolutePath);
        Assert.Contains("api_key=k%26x%3D1%20%23y12345678", uri.Query);
        Assert.Contains("facets%5Brespondent%5D%5B%5D=MISO", uri.Query);
        Assert.Contains("frequency=hourly", uri.Query);
        Assert.Contains("length=600", uri.Query);
    }

    [Fact]
    public async Task ReadsTheRecentHoursWithAGetAndNeverTheKeyInAMessage()
    {
        var api = Api(EiaJson.Answer(EiaJson.RealHour("2026-10-19T03").Concat(EiaJson.RealHour("2026-10-19T04")).ToArray()));
        var readings = await EiaClient.ReadAsync(new HttpClient(api), Config, TestContext.Current.CancellationToken);

        Assert.Equal(2, readings.Count);
        Assert.Equal(HttpMethod.Get, Assert.Single(api.Requests).Method);
    }

    [Fact]
    public async Task APlaceEiaKnowsNothingAboutIsSaidPlainly()
    {
        var e = await Assert.ThrowsAsync<CarbonException>(() => EiaClient.ReadAsync(new HttpClient(Api(EiaJson.Answer())), Config with { Place = "NOPE" }, TestContext.Current.CancellationToken));
        Assert.Contains("no recent generation data for \"NOPE\"", e.Message);
    }

    [Theory]
    [InlineData(400, "did not accept the request", false)]
    [InlineData(403, "did not accept the key (error 403)", false)]
    [InlineData(429, "slow down", true)]
    [InlineData(500, "error (500)", false)]
    public async Task ExplainsWhatTheErrorMeansInEiasOwnWords_NeverEchoingTheKey(int status, string expected, bool rateLimited)
    {
        var api = new FakeElectricityMaps
        {
            Respond = _ => new HttpResponseMessage((HttpStatusCode)status)
            {
                Content = new StringContent("{\"error\":{\"code\":\"API_KEY_INVALID\",\"message\":\"Key " + Config.ApiKey + " is invalid.\"}}"),
            },
        };
        var e = await Assert.ThrowsAsync<CarbonException>(() => EiaClient.ReadAsync(new HttpClient(api), Config, TestContext.Current.CancellationToken));
        Assert.Contains(expected, e.Message);
        Assert.Contains("EIA says: Key (key) is invalid.", e.Message);
        Assert.DoesNotContain(Config.ApiKey, e.Message);
        Assert.Equal(rateLimited, e.RateLimited);
    }

    [Theory]
    [InlineData("""{"error":"Bad thing"}""", "Bad thing")]
    [InlineData("""{"error":{"message":"Nested"}}""", "Nested")]
    [InlineData("""{"error":{"code":"X"}}""", null)]
    [InlineData("<html>", null)]
    [InlineData("""{"other":1}""", null)]
    public void OnlyEiasShortMessageIsPassedOn(string body, string? expected) => Assert.Equal(expected, EiaClient.ServiceMessage(body, "abcdefgh1234"));

    [Fact]
    public async Task AnUnreachableServiceSaysSo()
    {
        var api = new FakeElectricityMaps { Respond = _ => throw new HttpRequestException("No such host is known") };
        var e = await Assert.ThrowsAsync<CarbonException>(() => EiaClient.ReadAsync(new HttpClient(api), Config, TestContext.Current.CancellationToken));
        Assert.StartsWith("Could not reach EIA", e.Message);
    }

    [Fact]
    public async Task TheSourceInTheConfigDecidesWhoIsAsked()
    {
        var eia = Api(EiaJson.Answer(EiaJson.RealHour().ToArray()));
        var viaEia = await CarbonProviders.ReadAsync(new HttpClient(eia), Config, DateTimeOffset.UtcNow, TestContext.Current.CancellationToken);
        Assert.Equal("api.eia.gov", Assert.Single(eia.Requests).RequestUri!.Host);
        Assert.Single(viaEia);

        var maps = new FakeElectricityMaps();
        var viaMaps = await CarbonProviders.ReadAsync(new HttpClient(maps), new CarbonConfig("abcdefgh1234", "US-MIDW-MISO"), DateTimeOffset.UtcNow, TestContext.Current.CancellationToken);
        Assert.Equal("api.electricitymaps.com", Assert.Single(maps.Requests).RequestUri!.Host);
        Assert.Equal(412, Assert.Single(viaMaps).GramsPerKwh);
    }
}

public class CarbonSourceConfigTests : IDisposable
{
    private readonly string _dir = Path.Combine(Path.GetTempPath(), "kvguzzler-carbonsrc-" + Guid.NewGuid().ToString("N")[..8]);
    private string File_ => Path.Combine(_dir, CarbonConfig.FileName);

    public void Dispose()
    {
        try { Directory.Delete(_dir, recursive: true); } catch (IOException) { /* best effort */ }
    }

    [Theory]
    [InlineData("MISO", true)]
    [InlineData("PJM", true)]
    [InlineData("ERCO", true)]
    [InlineData("US-MIDW-MISO", false)]     // that is an Electricity Maps zone, not an EIA code
    [InlineData("39.7, -104.9", false)]
    [InlineData("MISO&x=1", false)]
    [InlineData("miso", false)]             // normalised before it is checked
    [InlineData("A", false)]
    [InlineData("", false)]
    public void AnEiaPlaceIsABalancingAuthorityCode(string place, bool valid) => Assert.Equal(valid, CarbonConfig.IsValidPlace(place, CarbonSources.Eia));

    [Fact]
    public void ExplainsWhatIsMissingForEia()
    {
        const string key = "abcdefghijklmnopqrstuvwxyz0123456789ABCD";
        Assert.Contains("Paste your EIA API key", CarbonConfig.Problem("", "MISO", CarbonSources.Eia));
        Assert.Contains("balancing authority's code, like MISO", CarbonConfig.Problem(key, "", CarbonSources.Eia));
        Assert.Contains("letters and digits only", CarbonConfig.Problem(key, "MI SO", CarbonSources.Eia));
        Assert.Null(CarbonConfig.Problem(key, "miso".ToUpperInvariant(), CarbonSources.Eia));
        Assert.Equal("MISO", CarbonConfig.Normalised(" miso ", CarbonSources.Eia));
        Assert.Equal("us-midw-miso", CarbonConfig.Normalised(" us-midw-miso ", CarbonSources.ElectricityMaps));   // a zone is left as typed
    }

    [Fact]
    public void ASavedFileWithNoSourceIsElectricityMaps_AndAnEiaOneKeepsItsSourceAndUpperCasesItsCode()
    {
        Directory.CreateDirectory(_dir);
        File.WriteAllText(File_, """{"apiKey":"abcdefgh1234","place":"US-MIDW-MISO"}""");   // written before there was a choice
        var old = CarbonConfig.Load(File_)!;
        Assert.Equal(CarbonSources.ElectricityMaps, old.Source);

        new CarbonConfig("abcdefghijklmnopqrstuvwxyz0123456789ABCD", "miso") { Source = CarbonSources.Eia }.Save(File_);
        var eia = CarbonConfig.Load(File_)!;
        Assert.Equal(CarbonSources.Eia, eia.Source);
        Assert.Equal("MISO", eia.Place);
    }

    [Fact]
    public void AnUnknownSourceOrAPlaceThatDoesNotFitItsSourceIsNotSetUp()
    {
        Directory.CreateDirectory(_dir);
        File.WriteAllText(File_, """{"apiKey":"abcdefgh1234","place":"MISO","source":"somewhere-else"}""");
        Assert.Null(CarbonConfig.Load(File_));
        File.WriteAllText(File_, """{"apiKey":"abcdefgh1234","place":"US-MIDW-MISO","source":"eia"}""");   // a zone code is not an authority code
        Assert.Null(CarbonConfig.Load(File_));
    }
}

public class CarbonFeedEiaTests : IDisposable
{
    private static readonly DateTimeOffset T0 = new(2026, 10, 19, 6, 5, 0, TimeSpan.Zero);
    private readonly string _dir = Path.Combine(Path.GetTempPath(), "kvguzzler-carboneia-" + Guid.NewGuid().ToString("N")[..8]);
    private readonly TempHistory _h = new();

    public void Dispose()
    {
        _h.Dispose();
        try { Directory.Delete(_dir, recursive: true); } catch (IOException) { /* best effort */ }
    }

    [Fact]
    public async Task EveryHourEiaGivesIsSavedSoHoursMissedWhileOffAreFilledIn_AndTheStatusIsTheLatest()
    {
        var path = Path.Combine(_dir, CarbonConfig.FileName);
        new CarbonConfig("abcdefghijklmnopqrstuvwxyz0123456789ABCD", "MISO") { Source = CarbonSources.Eia }.Save(path);
        var api = new FakeElectricityMaps
        {
            Respond = _ => new HttpResponseMessage(HttpStatusCode.OK)
            {
                Content = new StringContent(EiaJson.Answer(new[] { "2026-10-19T02", "2026-10-19T03", "2026-10-19T04" }.SelectMany(p => EiaJson.RealHour(p)).ToArray())),
            },
        };
        var feed = new CarbonFeed(_h.Store, path, api, new SettableClock(T0));

        Assert.True(await feed.StepAsync(TestContext.Current.CancellationToken));

        var saved = _h.Store.QueryCarbon(new DateTimeOffset(2026, 10, 19, 0, 0, 0, TimeSpan.Zero), T0);
        Assert.Equal(3, saved.Count);
        Assert.All(saved.Values, g => Assert.Equal(368.99, g, precision: 2));
        var status = _h.Store.GetCarbonStatus()!;
        Assert.True(status.Ok);
        Assert.Equal(new DateTimeOffset(2026, 10, 19, 4, 0, 0, TimeSpan.Zero), status.ReadingAt);

        // Energy in a backfilled hour is now valued at that hour's own figure, not at an average.
        _h.Store.AddMinutes(new[] { new MinuteRow(new DateTimeOffset(2026, 10, 19, 3, 0, 0, TimeSpan.Zero).ToUnixTimeSeconds(), WallMeterSource.ReadingName, ReadingScope.Wall, 1.0, 60, 300) });
        var carbon = CarbonCalculator.Compute(_h.Store, new DateTimeOffset(2026, 10, 19, 0, 0, 0, TimeSpan.Zero), T0)!;
        Assert.Equal(0.36899, carbon.Kg, precision: 4);
        Assert.Equal(0, carbon.EstimatedKwh);
    }
}

public class CarbonSourceUiTests
{
    private static void UseInvariantCulture() => CultureInfo.CurrentCulture = CultureInfo.InvariantCulture;
    private static string ConfigPath(TempData t) => Path.Combine(t.HistoryDir, CarbonConfig.FileName);
    private const string EiaKey = "abcdefghijklmnopqrstuvwxyz0123456789ABCD";

    private static IReadOnlyList<CarbonReading> Hours(params int[] hours) =>
        hours.Select(h => new CarbonReading(436.4, new DateTimeOffset(2026, 10, 19, h, 0, 0, TimeSpan.Zero), false)).ToList();

    [AvaloniaFact]
    public void ChoosingEiaChangesWhatTheBoxesAskForAndExplains()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();
        Assert.Equal(0, window.CarbonSourceBox.SelectedIndex);
        Assert.Contains("Electricity Maps API portal", window.CarbonNoteText.Text);
        Assert.Equal("US-MIDW-MISO or 44.98, -93.27", window.CarbonPlaceBox.PlaceholderText);

        window.CarbonSourceBox.SelectedIndex = 1;
        Assert.Contains("Energy Information Administration", window.CarbonNoteText.Text);
        Assert.Contains("MISO for Minnesota", window.CarbonNoteText.Text);
        Assert.Equal("MISO", window.CarbonPlaceBox.PlaceholderText);
    }

    [AvaloniaFact]
    public async Task EiaTestShowsTheLatestHourAndSaveKeepsTheSourceAndAnUpperCaseCode()
    {
        UseInvariantCulture();
        using var t = new TempData();
        CarbonConfig? asked = null;
        t.Services.ReadCarbonAsync = c => { asked = c; return Task.FromResult(Hours(2, 3, 4)); };
        var window = new MainWindow(t.Services);
        window.Show();
        window.CarbonSourceBox.SelectedIndex = 1;
        window.CarbonKeyBox.Text = EiaKey;
        window.CarbonPlaceBox.Text = "miso";

        await window.TestCarbonAsync();
        Assert.StartsWith("Reached EIA. MISO is at 436 g/kWh for ", window.CarbonStatusText.Text);
        Assert.Contains("3 hours found", window.CarbonStatusText.Text);
        Assert.Equal(CarbonSources.Eia, asked!.Source);
        Assert.False(File.Exists(ConfigPath(t)));

        await window.SaveCarbonAsync();
        var saved = CarbonConfig.Load(ConfigPath(t))!;
        Assert.Equal(CarbonSources.Eia, saved.Source);
        Assert.Equal("MISO", saved.Place);
        Assert.Equal(EiaKey, saved.ApiKey);
        Assert.Equal("MISO", window.CarbonPlaceBox.Text);
        Assert.Equal("", window.CarbonKeyBox.Text);
        Assert.Equal(CarbonSources.Eia, CarbonConfig.Load(ConfigPath(t))!.Source);
        Assert.Equal(1, new MainWindow(t.Services).CarbonSourceBox.SelectedIndex);   // a fresh window remembers the source
    }

    [AvaloniaFact]
    public async Task ASavedSetupBelongsToItsSourceAndIsNotOfferedForTheOther()
    {
        UseInvariantCulture();
        using var t = new TempData();
        new CarbonConfig("abcdefgh1234", "US-MIDW-MISO").Save(ConfigPath(t));   // Electricity Maps
        var window = new MainWindow(t.Services);
        window.Show();
        Assert.Equal("US-MIDW-MISO", window.CarbonPlaceBox.Text);
        Assert.Contains("Key saved", window.CarbonKeyBox.PlaceholderText);

        window.CarbonSourceBox.SelectedIndex = 1;   // EIA: nothing saved for it
        Assert.Equal("", window.CarbonPlaceBox.Text);
        Assert.Equal("API key", window.CarbonKeyBox.PlaceholderText);
        window.CarbonPlaceBox.Text = "MISO";
        await window.TestCarbonAsync();             // the Electricity Maps key must not be sent to EIA
        Assert.Contains("Paste your EIA API key", window.CarbonStatusText.Text);

        window.CarbonSourceBox.SelectedIndex = 0;
        Assert.Equal("US-MIDW-MISO", window.CarbonPlaceBox.Text);
    }

    [AvaloniaFact]
    public async Task EiaPlaceMistakesAreExplainedBeforeAnythingIsSent()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var asked = false;
        t.Services.ReadCarbonAsync = _ => { asked = true; return Task.FromResult(Hours(4)); };
        var window = new MainWindow(t.Services);
        window.Show();
        window.CarbonSourceBox.SelectedIndex = 1;
        window.CarbonKeyBox.Text = EiaKey;
        window.CarbonPlaceBox.Text = "US-MIDW-MISO";   // an Electricity Maps zone
        await window.SaveCarbonAsync();
        Assert.Contains("letters and digits only", window.CarbonStatusText.Text);
        Assert.False(asked);
    }
}
