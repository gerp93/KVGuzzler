using System.Globalization;
using System.Net;
using System.Text;
using Avalonia.Controls;
using Avalonia.Headless.XUnit;
using Avalonia.LogicalTree;
using KVGuzzler.Collector;
using KVGuzzler.Core.Billing;
using KVGuzzler.Core.History;
using KVGuzzler.Core.Power;
using KVGuzzler.Data;
using KVGuzzler.Views;
using Xunit;

namespace KVGuzzler.Tests;

/// <summary>Plays the part of OpenEI.</summary>
internal sealed class FakeOpenEi : HttpMessageHandler
{
    public Func<HttpRequestMessage, HttpResponseMessage> Respond { get; set; } = _ => new HttpResponseMessage(HttpStatusCode.OK) { Content = new StringContent(OpenEiJson.Items()) };
    public List<HttpRequestMessage> Requests { get; } = new();

    protected override Task<HttpResponseMessage> SendAsync(HttpRequestMessage request, CancellationToken cancellationToken)
    {
        lock (Requests) Requests.Add(request);
        return Task.FromResult(Respond(request));
    }
}

/// <summary>Builds answers shaped like OpenEI's.</summary>
internal static class OpenEiJson
{
    /// <summary>A 12 by 24 schedule: <paramref name="peakHours"/> use period 1 on weekdays, everything else period 0.</summary>
    public static string Schedule(Func<int, bool> peakHour) =>
        "[" + string.Join(",", Enumerable.Range(0, 12).Select(_ => "[" + string.Join(",", Enumerable.Range(0, 24).Select(h => peakHour(h) ? "1" : "0")) + "]")) + "]";

    public static string Number(double d) => d.ToString("0.#####", CultureInfo.InvariantCulture);

    /// <summary>A residential time-of-use rate: 8 cents off-peak, 20 cents from 5pm to 8pm on weekdays (weekends all off-peak), with
    /// a two-tier first period, a 1 cent adjustment and a fixed charge.</summary>
    public static string TimeOfUse(string label = "tou-1", string name = "Residential Time of Use", string extra = "") => $$"""
        {"label":"{{label}}","utility":"Example Electric","name":"{{name}}","uri":"https://openei.org/apps/USURDB/rate/view/{{label}}",
         "sector":"Residential","servicetype":"Bundled","approved":true,"is_default":false,"startdate":1700000000,
         "fixedchargefirstmeter":6.75,"fixedchargeunits":"$/month",
         "energyratestructure":[[{"rate":0.07,"adj":0.01,"unit":"kWh","max":500},{"rate":0.09,"adj":0.01,"unit":"kWh"}],[{"rate":0.19,"adj":0.01,"unit":"kWh"}]],
         "energyweekdayschedule":{{Schedule(h => h is >= 17 and < 20)}},
         "energyweekendschedule":{{Schedule(_ => false)}}{{extra}}}
        """;

    public static string Flat(string label = "flat-1", string name = "Residential Standard", double rate = 0.12, string extra = "") => $$"""
        {"label":"{{label}}","utility":"Example Electric","name":"{{name}}","sector":"Residential","servicetype":"Bundled","is_default":true,
         "energyratestructure":[[{"rate":{{Number(rate)}},"unit":"kWh"}]]{{extra}}}
        """;

    public static string Items(params string[] items) => """{"items":[""" + string.Join(",", items.Length == 0 ? new[] { TimeOfUse(), Flat() } : items) + "]}";
}

public class OpenEiParsingTests
{
    private static readonly DateTimeOffset Now = new(2026, 10, 19, 15, 0, 0, TimeSpan.Zero);

    private static IReadOnlyList<TariffCandidate> Parse(params string[] items) => OpenEiClient.ParseRates(OpenEiJson.Items(items), Now);

    [Fact]
    public void ATimeOfUseRateBecomesAPriceForEveryMonthDayKindAndHour()
    {
        var c = Assert.Single(Parse(OpenEiJson.TimeOfUse()));
        var t = c.Tariff;
        Assert.Equal("tou-1", t.Label);
        Assert.Equal("Example Electric", t.Utility);
        Assert.Equal(2, t.Periods);
        Assert.Equal(2, t.Tiers);
        Assert.True(c.Approved);
        Assert.False(c.IsDefault);
        Assert.Equal("6.75 $/month", t.FixedCharge);

        Assert.Equal(0.08, t.Price(0, weekend: false, 12), precision: 9);   // off-peak: 0.07 + 0.01, the first tier
        Assert.Equal(0.20, t.Price(5, weekend: false, 18), precision: 9);   // peak: 0.19 + 0.01
        Assert.Equal(0.08, t.Price(5, weekend: true, 18), precision: 9);    // weekends are all off-peak
        Assert.True(t.VariesByTime);
        Assert.Equal(0.08, t.Cheapest, precision: 9);
        Assert.Equal(0.20, t.Dearest, precision: 9);
    }

    [Fact]
    public void AFlatRateNeedsNoSchedule()
    {
        var t = Assert.Single(Parse(OpenEiJson.Flat(rate: 0.1234))).Tariff;
        Assert.False(t.VariesByTime);
        Assert.Equal(0.1234, t.Price(7, true, 3), precision: 9);
        Assert.Equal(1, t.Periods);
        Assert.Contains("$0.1234 per kWh", t.Summary());
    }

    [Fact]
    public void TheMonthsFuelAdjustmentIsAddedAndNumbersMayBeText()
    {
        var fuel = ",\"fueladjustmentsmonthly\":[0.01,0,0,0,0,0,0,0,0,0,0,0.02]";
        var t = Assert.Single(Parse(OpenEiJson.Flat(rate: 0.10, extra: fuel))).Tariff;
        Assert.Equal(0.11, t.Price(0, false, 0), precision: 9);
        Assert.Equal(0.10, t.Price(5, false, 0), precision: 9);
        Assert.Equal(0.12, t.Price(11, true, 23), precision: 9);

        var text = Assert.Single(Parse("""{"label":"x","energyratestructure":[[{"rate":"0.15","unit":"kWh"}]]}""")).Tariff;
        Assert.Equal(0.15, text.Price(0, false, 0), precision: 9);
    }

    [Theory]
    [InlineData("""{"label":"a","sector":"Commercial","energyratestructure":[[{"rate":0.1}]]}""")]            // not residential
    [InlineData("""{"label":"a","servicetype":"Delivery","energyratestructure":[[{"rate":0.1}]]}""")]       // no energy price in it
    [InlineData("""{"label":"a","enddate":1700000000,"energyratestructure":[[{"rate":0.1}]]}""")]            // no longer in force
    [InlineData("""{"label":"a","startdate":1999999999,"energyratestructure":[[{"rate":0.1}]]}""")]          // not yet in force
    [InlineData("""{"label":"a","energyratestructure":[[{"rate":0.1,"unit":"kWh/kW"}]]}""")]                // demand-based
    [InlineData("""{"label":"a","energyratestructure":[[{"rate":0.1,"unit":"MWh"}]]}""")]
    [InlineData("""{"label":"a","energyratestructure":[]}""")]
    [InlineData("""{"label":"a","energyratestructure":[[]]}""")]
    [InlineData("""{"label":"a","energyratestructure":[[{"unit":"kWh"}]]}""")]                              // no price at all
    [InlineData("""{"label":"a","energyratestructure":[[{"rate":0.1}],[{"rate":0.2}]]}""")]                 // two periods and no schedule
    [InlineData("""{"label":"a","energyratestructure":[[{"rate":-1}]]}""")]
    [InlineData("""{"label":"a","energyratestructure":[[{"rate":900}]]}""")]                                 // not a household price
    [InlineData("""{"energyratestructure":[[{"rate":0.1}]]}""")]                                              // no label
    public void ARateThatCannotBePricedPerKwhIsLeftOutNotGuessedAt(string item) => Assert.Empty(Parse(item));

    [Fact]
    public void AScheduleThatPointsAtAMissingPeriodLeavesTheRateOut()
    {
        var bad = OpenEiJson.Schedule(_ => true).Replace("1", "5");   // period 5 of 2
        var item = $$"""{"label":"a","energyratestructure":[[{"rate":0.1}],[{"rate":0.2}]],"energyweekdayschedule":{{bad}}}""";
        Assert.Empty(Parse(item));
    }

    [Fact]
    public void ARateWithoutAWeekendScheduleUsesItsWeekdayOne()
    {
        var item = $$"""{"label":"a","energyratestructure":[[{"rate":0.1}],[{"rate":0.3}]],"energyweekdayschedule":{{OpenEiJson.Schedule(h => h == 18)}}}""";
        var t = Assert.Single(Parse(item)).Tariff;
        Assert.Equal(0.3, t.Price(2, true, 18), precision: 9);
    }

    [Theory]
    [InlineData("")]
    [InlineData("<html>busy</html>")]
    [InlineData("[]")]
    [InlineData("""{"items":5}""")]
    [InlineData("""{"nothing":true}""")]
    public void AnythingThatIsNotAListOfRatesIsAMessage(string answer) =>
        Assert.Contains("not with a list of rates", Assert.Throws<RateLookupException>(() => OpenEiClient.ParseRates(answer, Now)).Message);

    [Fact]
    public void AnErrorFromOpenEiIsPassedOn() =>
        Assert.Contains("OpenEI said: Invalid api key", Assert.Throws<RateLookupException>(() => OpenEiClient.ParseRates("""{"error":"Invalid api key"}""", Now)).Message);

    [Fact]
    public void AnEmptyListIsNoRatesNotAnError() => Assert.Empty(OpenEiClient.ParseRates("""{"items":[]}""", Now));

    [Fact]
    public void ARateKnownByItsLabelIsUsedEvenIfItsDatesHavePassed()
    {
        var expired = """{"label":"old","enddate":1700000000,"energyratestructure":[[{"rate":0.1}]]}""";
        Assert.Empty(OpenEiClient.ParseRates(OpenEiJson.Items(expired), Now));
        Assert.Single(OpenEiClient.ParseRates(OpenEiJson.Items(expired), Now, requireCurrent: false));
    }
}

public class OpenEiClientTests
{
    private static readonly DateTimeOffset Now = new(2026, 10, 19, 15, 0, 0, TimeSpan.Zero);

    [Fact]
    public void TheAddressAndKeyAreEncodedSoNothingTypedCanChangeTheRequest()
    {
        var uri = OpenEiClient.SearchUri("key123&x=1", "80202 & evil=1/../", Now);
        Assert.Equal("api.openei.org", uri.Host);
        Assert.Equal("/utility_rates", uri.AbsolutePath);
        Assert.Contains("api_key=key123%26x%3D1", uri.Query);
        Assert.Contains("address=80202%20%26%20evil%3D1%2F..%2F", uri.Query);
        Assert.Contains("sector=Residential", uri.Query);
        Assert.Contains($"effective_on_date={Now.ToUnixTimeSeconds()}", uri.Query);
        Assert.DoesNotContain("evil=1&", uri.Query);

        var label = OpenEiClient.LabelUri("k", "a b&c");
        Assert.Contains("getpage=a%20b%26c", label.Query);
    }

    [Fact]
    public async Task FindsRatesWithAGetAndPutsTheMostCommonFirst()
    {
        var api = new FakeOpenEi();
        var found = await new OpenEiClient(new HttpClient(api)).FindAsync("abcdefghijklmnop", "80202", Now, TestContext.Current.CancellationToken);

        var request = Assert.Single(api.Requests);
        Assert.Equal(HttpMethod.Get, request.Method);
        Assert.Equal(new[] { "flat-1", "tou-1" }, found.Select(c => c.Tariff.Label));   // the flat one is marked most common
        Assert.Equal(Now, found[0].Tariff.FetchedAt);
    }

    [Fact]
    public async Task GetsOneRateByItsLabelOrNullWhenItIsGone()
    {
        var api = new FakeOpenEi();
        var client = new OpenEiClient(new HttpClient(api));
        Assert.Equal("tou-1", (await client.GetAsync("abcdefghijklmnop", "tou-1", Now, TestContext.Current.CancellationToken))!.Tariff.Label);
        Assert.Null(await client.GetAsync("abcdefghijklmnop", "gone", Now, TestContext.Current.CancellationToken));
    }

    [Theory]
    [InlineData(400, "did not accept the request")]
    [InlineData(401, "did not accept the API key")]
    [InlineData(403, "did not accept the API key")]
    [InlineData(429, "slow down")]
    [InlineData(500, "error (500)")]
    public async Task ExplainsWhatTheErrorMeans(int status, string expected)
    {
        var api = new FakeOpenEi { Respond = _ => new HttpResponseMessage((HttpStatusCode)status) };
        var e = await Assert.ThrowsAsync<RateLookupException>(() => new OpenEiClient(new HttpClient(api)).FindAsync("abcdefghijklmnop", "80202", Now, TestContext.Current.CancellationToken));
        Assert.Contains(expected, e.Message);
    }

    [Fact]
    public async Task AnUnreachableServiceSaysSo()
    {
        var api = new FakeOpenEi { Respond = _ => throw new HttpRequestException("No such host is known") };
        var e = await Assert.ThrowsAsync<RateLookupException>(() => new OpenEiClient(new HttpClient(api)).FindAsync("abcdefghijklmnop", "80202", Now, TestContext.Current.CancellationToken));
        Assert.StartsWith("Could not reach OpenEI", e.Message);
    }
}

public class TariffAndProfileTests
{
    [Fact]
    public void ATariffSurvivesBeingSavedAsText_AndADamagedOneIsNothing()
    {
        var original = Assert.Single(OpenEiClient.ParseRates(OpenEiJson.Items(OpenEiJson.TimeOfUse()), DateTimeOffset.UtcNow)).Tariff;
        var back = Tariff.FromJson(original.ToJson())!;
        Assert.Equal(original.Label, back.Label);
        Assert.Equal(original.Grid, back.Grid);

        Assert.Null(Tariff.FromJson(""));
        Assert.Null(Tariff.FromJson("not json"));
        Assert.Null(Tariff.FromJson("""{"Label":"x","Utility":"u","Name":"n","FetchedAt":"2026-01-01T00:00:00+00:00","Grid":[1,2,3],"Periods":1,"Tiers":1}"""));   // too short
    }

    [Fact]
    public void TheProfileIsTheShareOfUsageInEachHourSeparatelyForWeekdaysAndWeekends()
    {
        var zone = TimeZoneInfo.CreateCustomTimeZone("test", TimeSpan.FromHours(-6), "test", "test");
        // Monday 19 Oct 2026, 18:00 local (= 00:00 UTC on the 20th) and 08:00 local; Saturday 24 Oct 12:00 local.
        long At(int day, int hourLocal) => new DateTimeOffset(2026, 10, day, hourLocal, 0, 0, TimeSpan.FromHours(-6)).ToUnixTimeSeconds();
        var profile = UsageProfile.FromHourly(new[] { (At(19, 18), 3.0), (At(19, 8), 1.0), (At(24, 12), 2.0) }, zone);

        Assert.Equal(0.75, profile.Weekday[18], precision: 9);
        Assert.Equal(0.25, profile.Weekday[8], precision: 9);
        Assert.Equal(1.0, profile.Weekend[12], precision: 9);
        Assert.Equal(1.0, profile.Weekday.Sum(), precision: 9);
    }

    [Fact]
    public void NoUsageMeansAnEvenDay()
    {
        var profile = UsageProfile.FromHourly(Array.Empty<(long, double)>(), TimeZoneInfo.Utc);
        Assert.All(profile.Weekday.Concat(profile.Weekend), w => Assert.Equal(1.0 / 24, w, precision: 9));
        Assert.Equal(1.0, profile.Weekend.Sum(), precision: 9);
        Assert.NotNull(UsageProfile.FromJson(profile.ToJson()));
        Assert.Null(UsageProfile.FromJson("""{"Weekday":[1],"Weekend":[1]}"""));
    }

    private static Tariff Tou() => Assert.Single(OpenEiClient.ParseRates(OpenEiJson.Items(OpenEiJson.TimeOfUse()), DateTimeOffset.UtcNow)).Tariff;

    private static readonly DateOnly Monday = new(2026, 10, 19);
    private static readonly DateOnly Saturday = new(2026, 10, 24);

    [Fact]
    public void ADaysPriceIsTheTariffWeightedByWhenThePowerIsUsed()
    {
        var evening = new UsageProfile(Enumerable.Range(0, 24).Select(h => h == 18 ? 1.0 : 0.0).ToArray(), UsageProfile.Uniform.Weekend);
        var night = new UsageProfile(Enumerable.Range(0, 24).Select(h => h == 3 ? 1.0 : 0.0).ToArray(), UsageProfile.Uniform.Weekend);

        Assert.Equal(0.20m, new RateSchedule(RateMode.Lookup, null, Array.Empty<Bill>(), TimeZoneInfo.Utc, Tou(), evening).RateAt(Monday)!.Value, precision: 6);
        Assert.Equal(0.08m, new RateSchedule(RateMode.Lookup, null, Array.Empty<Bill>(), TimeZoneInfo.Utc, Tou(), night).RateAt(Monday)!.Value, precision: 6);
        // An even day: 3 peak hours at 20 cents, 21 off-peak at 8 cents.
        Assert.Equal((3 * 0.20m + 21 * 0.08m) / 24, new RateSchedule(RateMode.Lookup, null, Array.Empty<Bill>(), TimeZoneInfo.Utc, Tou()).RateAt(Monday)!.Value, precision: 6);
        // A weekend has no peak at all.
        Assert.Equal(0.08m, new RateSchedule(RateMode.Lookup, null, Array.Empty<Bill>(), TimeZoneInfo.Utc, Tou(), evening).RateAt(Saturday)!.Value, precision: 6);
    }

    [Fact]
    public void CostsFollowTheLookedUpPriceDayByDay()
    {
        var evening = new UsageProfile(Enumerable.Range(0, 24).Select(h => h == 18 ? 1.0 : 0.0).ToArray(), UsageProfile.Uniform.Weekend);
        var schedule = new RateSchedule(RateMode.Lookup, null, Array.Empty<Bill>(), TimeZoneInfo.Utc, Tou(), evening);
        var from = new DateTimeOffset(2026, 10, 23, 0, 0, 0, TimeSpan.Zero);   // Friday and Saturday
        var cost = schedule.Cost((a, b) => (b - a).TotalHours / 24, from, from.AddDays(2));   // 1 kWh a day
        Assert.Equal(0.20m + 0.08m, cost.Cost, precision: 6);
        Assert.False(cost.Unpriced);
    }

    [Fact]
    public void LookupWithoutATariffFallsBackOnTheSingleRate_AndOtherModesIgnoreAnyTariff()
    {
        Assert.Equal(0.15m, new RateSchedule(RateMode.Lookup, 0.15m, Array.Empty<Bill>()).RateAt(Monday));
        Assert.Null(new RateSchedule(RateMode.Lookup, null, Array.Empty<Bill>()).RateAt(Monday));
        Assert.Equal(0.15m, new RateSchedule(RateMode.OneRate, 0.15m, Array.Empty<Bill>(), null, Tou()).RateAt(Monday));
    }
}

public class RateLookupUiTests
{
    private static void UseInvariantCulture() => CultureInfo.CurrentCulture = CultureInfo.InvariantCulture;
    private const string Key = "abcdefghij0123456789";

    private sealed class FakeLookup : IRateLookup
    {
        public List<TariffCandidate> Rates { get; set; } = new();
        public Func<string, TariffCandidate?>? ByLabel { get; set; }
        public Exception? Fail { get; set; }
        public List<(string Key, string Address)> Asked { get; } = new();
        public int Refreshes;

        public Task<IReadOnlyList<TariffCandidate>> FindAsync(string apiKey, string address, DateTimeOffset now, CancellationToken cancel = default)
        {
            Asked.Add((apiKey, address));
            if (Fail is not null) throw Fail;
            return Task.FromResult<IReadOnlyList<TariffCandidate>>(Rates);
        }

        public Task<TariffCandidate?> GetAsync(string apiKey, string label, DateTimeOffset now, CancellationToken cancel = default)
        {
            Refreshes++;
            if (Fail is not null) throw Fail;
            return Task.FromResult(ByLabel?.Invoke(label));
        }
    }

    private static TariffCandidate Candidate(string json, DateTimeOffset? fetched = null)
    {
        var c = OpenEiClient.ParseRates(OpenEiJson.Items(json), DateTimeOffset.UtcNow).Single();
        return fetched is { } at ? c with { Tariff = c.Tariff with { FetchedAt = at } } : c;
    }

    [AvaloniaFact]
    public void TheThirdChoiceShowsTheLookupPanelAndIsRemembered()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();
        Assert.False(window.LookupPanel.IsVisible);

        window.RateModeBox.SelectedIndex = 2;
        Assert.True(window.LookupPanel.IsVisible);
        Assert.False(window.OneRatePanel.IsVisible);
        Assert.Equal("lookup", t.Services.Database!.GetSetting(SettingKeys.RateMode));
        Assert.Equal(2, new MainWindow(t.Services).RateModeBox.SelectedIndex);

        window.RateModeBox.SelectedIndex = 0;
        Assert.Equal("one", t.Services.Database.GetSetting(SettingKeys.RateMode));   // fixed rate and bills still work as before
        window.RateModeBox.SelectedIndex = 1;
        Assert.Equal("bills", t.Services.Database.GetSetting(SettingKeys.RateMode));
    }

    [AvaloniaFact]
    public async Task FindShowsTheRatesKeepsTheKeyHiddenAndRefusesBadInputFirst()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var lookup = new FakeLookup { Rates = { Candidate(OpenEiJson.Flat()), Candidate(OpenEiJson.TimeOfUse()) } };
        t.Services.RateLookup = lookup;
        var window = new MainWindow(t.Services);
        window.Show();
        window.RateModeBox.SelectedIndex = 2;

        await window.FindRatesAsync();
        Assert.Contains("Paste your OpenEI API key", window.LookupStatusText.Text);
        window.OpenEiKeyBox.Text = "short";
        await window.FindRatesAsync();
        Assert.Contains("does not look like an API key", window.LookupStatusText.Text);
        window.OpenEiKeyBox.Text = Key;
        window.RateAddressBox.Text = "";
        await window.FindRatesAsync();
        Assert.Contains("Enter your ZIP code", window.LookupStatusText.Text);
        Assert.Empty(lookup.Asked);

        window.RateAddressBox.Text = "80202";
        await window.FindRatesAsync();
        Assert.Equal((Key, "80202"), Assert.Single(lookup.Asked));
        Assert.StartsWith("Found 2 residential rates", window.LookupStatusText.Text);
        Assert.True(window.RateResultsBox.IsVisible);
        Assert.True(window.UseFoundRateButton.IsVisible);
        Assert.Equal(2, ((IEnumerable<string>)window.RateResultsBox.ItemsSource!).Count());
        Assert.Equal(Key, t.Services.Database!.GetSetting(SettingKeys.OpenEiKey));
        Assert.Equal("", window.OpenEiKeyBox.Text);   // never shown again
        Assert.Contains("Key saved", window.OpenEiKeyBox.PlaceholderText);

        // With the key box left empty the saved key is used.
        await window.FindRatesAsync();
        Assert.Equal(Key, lookup.Asked[1].Key);
    }

    [AvaloniaFact]
    public async Task APoorAnswerIsAMessageAndNotAKeptKey()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var lookup = new FakeLookup { Fail = new RateLookupException("OpenEI did not accept the API key. Check it was copied whole.") };
        t.Services.RateLookup = lookup;
        var window = new MainWindow(t.Services);
        window.Show();
        window.OpenEiKeyBox.Text = Key;
        window.RateAddressBox.Text = "80202";

        await window.FindRatesAsync();
        Assert.Contains("did not accept the API key", window.LookupStatusText.Text);
        Assert.Null(t.Services.Database!.GetSetting(SettingKeys.OpenEiKey));   // a key that failed is not saved

        lookup.Fail = new InvalidOperationException("boom");
        await window.FindRatesAsync();
        Assert.StartsWith("Something unexpected went wrong: boom", window.LookupStatusText.Text);

        lookup.Fail = null;
        await window.FindRatesAsync();   // nothing found
        Assert.Contains("no usable residential rate", window.LookupStatusText.Text);
        Assert.False(window.UseFoundRateButton.IsVisible);
    }

    [AvaloniaFact]
    public async Task UsingARateSavesItSwitchesToLookupAndExplainsWhatItCannotDo()
    {
        UseInvariantCulture();
        using var t = new TempData();
        t.Services.RateLookup = new FakeLookup { Rates = { Candidate(OpenEiJson.TimeOfUse()) } };
        var window = new MainWindow(t.Services);
        window.Show();
        window.RateModeBox.SelectedIndex = 2;
        window.OpenEiKeyBox.Text = Key;
        window.RateAddressBox.Text = "80202";
        await window.FindRatesAsync();

        Assert.Contains("changes with the hour", window.RateResultDetailText.Text);
        Assert.Contains("2 usage tiers", window.RateResultDetailText.Text);
        Assert.Contains("fixed charge (6.75 $/month)", window.RateResultDetailText.Text);
        Assert.Contains("Checked by an OpenEI expert", window.RateResultDetailText.Text);

        window.UseFoundRateButton.RaiseEvent(new Avalonia.Interactivity.RoutedEventArgs(Avalonia.Controls.Button.ClickEvent));
        Assert.Equal("Saved. Costs on the Dashboard now use this rate.", window.LookupStatusText.Text);
        Assert.Equal("lookup", t.Services.Database!.GetSetting(SettingKeys.RateMode));
        Assert.Equal("tou-1", Tariff.FromJson(t.Services.Database.GetSetting(SettingKeys.RateLookupTariff))!.Label);
        Assert.Contains("Using: Example Electric: Residential Time of Use", window.LookupCurrentText.Text);
        Assert.Contains("per kWh at the hours you use power", window.LookupCurrentText.Text);
        Assert.True(window.RefreshTariffButton.IsVisible);

        // The schedule everything prices with is now the looked-up one.
        var schedule = t.Services.Database.LoadRateSchedule();
        Assert.Equal(RateMode.Lookup, schedule.Mode);
        Assert.Equal("tou-1", schedule.Tariff!.Label);
    }

    [AvaloniaFact]
    public async Task RefreshReplacesTheSavedRateOrKeepsItWhenOpenEiCannotHelp()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var old = Candidate(OpenEiJson.Flat(rate: 0.10), DateTimeOffset.UtcNow.AddDays(-30));
        t.Services.Database!.SetSetting(SettingKeys.RateLookupTariff, old.Tariff.ToJson());
        t.Services.Database.SetSetting(SettingKeys.RateMode, "lookup");
        t.Services.Database.SetSetting(SettingKeys.OpenEiKey, Key);
        var lookup = new FakeLookup { ByLabel = _ => Candidate(OpenEiJson.Flat(rate: 0.14)) };
        t.Services.RateLookup = lookup;
        var window = new MainWindow(t.Services);
        window.Show();

        await window.RefreshTariffAsync(silent: false);
        Assert.Equal("Updated to the latest version of this rate.", window.LookupStatusText.Text);
        Assert.Equal(0.14, Tariff.FromJson(t.Services.Database.GetSetting(SettingKeys.RateLookupTariff))!.Price(0, false, 0), precision: 9);

        lookup.ByLabel = _ => null;
        await window.RefreshTariffAsync(silent: false);
        Assert.Contains("no longer has this rate", window.LookupStatusText.Text);
        Assert.Equal(0.14, Tariff.FromJson(t.Services.Database.GetSetting(SettingKeys.RateLookupTariff))!.Price(0, false, 0), precision: 9);   // kept

        lookup.Fail = new RateLookupException("Could not reach OpenEI: offline");
        await window.RefreshTariffAsync(silent: false);
        Assert.EndsWith("The saved copy is still being used.", window.LookupStatusText.Text);

        lookup.Fail = null;
        lookup.ByLabel = null;
        window.LookupStatusText.Text = "untouched";
        await window.RefreshTariffAsync(silent: true);   // the background refresh says nothing
        Assert.Equal("untouched", window.LookupStatusText.Text);
    }

    [AvaloniaFact]
    public async Task AWeekOldRateIsRefreshedOnOpenAndAFreshOneIsLeftAlone()
    {
        UseInvariantCulture();
        foreach (var (age, expected) in new[] { (30, 1), (1, 0) })
        {
            using var t = new TempData();
            t.Services.Database!.SetSetting(SettingKeys.RateLookupTariff, Candidate(OpenEiJson.Flat(), DateTimeOffset.UtcNow.AddDays(-age)).Tariff.ToJson());
            t.Services.Database.SetSetting(SettingKeys.OpenEiKey, Key);
            var lookup = new FakeLookup { ByLabel = _ => null };
            t.Services.RateLookup = lookup;
            var window = new MainWindow(t.Services);
            window.Show();
            await Task.Delay(100, TestContext.Current.CancellationToken);
            Assert.Equal(expected, lookup.Refreshes);
        }
    }

    private static async Task<TempData> LookupDashboardAsync(Action<TempData>? beforeShow = null)
    {
        var t = new TempData();
        t.Services.QueryCollectorService = () => ServiceState.Running;
        t.Services.Database!.SetSetting(SettingKeys.RatePerKwh, "0.50");
        t.Services.Database.SetSetting(SettingKeys.Currency, "$");
        beforeShow?.Invoke(t);
        var minute = DateTimeOffset.UtcNow.AddMinutes(-30).ToUnixTimeSeconds() / 60 * 60;
        using var store = new HistoryStore(Path.Combine(t.HistoryDir, HistoryStore.FileName));
        store.AddMinutes(new[] { new MinuteRow(minute, WallMeterSource.ReadingName, ReadingScope.Wall, 1.0, 60, 300) });
        store.SetLive(DateTimeOffset.UtcNow, new List<PowerReading> { new(WallMeterSource.ReadingName, 200, ReadingScope.Wall) }, Array.Empty<string>());
        await Task.CompletedTask;
        return t;
    }

    [AvaloniaFact]
    public async Task TheCostCardSaysWhichLookedUpRateItIsUsing()
    {
        UseInvariantCulture();
        // A flat tariff, so the price does not depend on what hour the test happens to run in.
        using var t = await LookupDashboardAsync(x =>
        {
            x.Services.Database!.SetSetting(SettingKeys.RateMode, "lookup");
            x.Services.Database.SetSetting(SettingKeys.RateLookupTariff, Candidate(OpenEiJson.Flat(rate: 0.12)).Tariff.ToJson());
        });
        var window = new MainWindow(t.Services);
        window.Show();
        await window.RefreshCollectorAsync(includeService: true);

        Assert.True(window.StatRateNote.IsVisible);
        Assert.Equal("Rate: Example Electric, Residential Standard (from OpenEI) · averaging $0.1200 per kWh at the hours you use power"
                     + " · energy charge only: fixed fees, riders and taxes are not in it", window.StatRateNote.Text);
        Assert.Equal("$0.12", window.StatCostText.Text);   // 1 kWh at that rate, so the note and the number agree
    }

    [AvaloniaFact]
    public async Task TheFootnoteIsOnlyForALookedUpRate_AndSaysSoWhenNoneIsChosenYet()
    {
        UseInvariantCulture();
        using var t = await LookupDashboardAsync(x => x.Services.Database!.SetSetting(SettingKeys.RateMode, "lookup"));   // on, but no tariff saved
        var window = new MainWindow(t.Services);
        window.Show();
        await window.RefreshCollectorAsync(includeService: true);
        Assert.True(window.StatRateNote.IsVisible);
        Assert.Contains("no rate has been chosen yet", window.StatRateNote.Text);
        Assert.Contains("single rate is used", window.StatRateNote.Text);

        window.RateModeBox.SelectedIndex = 0;   // the fixed rate: the user's own figure, nothing to footnote
        await window.RefreshCollectorAsync(includeService: false);
        Assert.False(window.StatRateNote.IsVisible);
        window.RateModeBox.SelectedIndex = 1;   // and bills
        await window.RefreshCollectorAsync(includeService: false);
        Assert.False(window.StatRateNote.IsVisible);
    }

    [AvaloniaFact]
    public void TheSettingsPageKeepsTheTwoMeterCardsTogetherWithCarbonAfterThem()
    {
        UseInvariantCulture();
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();

        Border Card(Control c) => c.GetLogicalAncestors().OfType<Border>().First(b => b.Classes.Contains("card"));
        var wall = Card(window.WallHostBox);
        var other = Card(window.PeripheralSettingsList);
        var carbon = Card(window.CarbonSourceBox);
        var cards = ((Panel)wall.Parent!).Children;

        Assert.Same(wall.Parent, other.Parent);
        Assert.Same(wall.Parent, carbon.Parent);
        Assert.Equal(cards.IndexOf(wall) + 1, cards.IndexOf(other));     // the wall meter and the other meters sit side by side
        Assert.True(cards.IndexOf(carbon) > cards.IndexOf(other));       // carbon is not between them
    }

    [AvaloniaFact]
    public async Task TheDashboardPricesWithTheLookedUpRateAndLearnsWhenThePowerIsUsed()
    {
        UseInvariantCulture();
        using var t = new TempData();
        t.Services.QueryCollectorService = () => ServiceState.Running;
        t.Services.Database!.SetSetting(SettingKeys.RatePerKwh, "0.50");
        t.Services.Database.SetSetting(SettingKeys.RateMode, "lookup");
        t.Services.Database.SetSetting(SettingKeys.RateLookupTariff, Candidate(OpenEiJson.TimeOfUse()).Tariff.ToJson());
        t.Services.Database.SetSetting(SettingKeys.Currency, "$");

        // 1 kWh used entirely at 02:00 UTC-local-whatever: always off-peak, never the 5 to 8 pm peak (the zone is the test machine's).
        var local = DateTimeOffset.Now.Date.AddDays(-1).AddHours(3);
        var minute = new DateTimeOffset(local, TimeZoneInfo.Local.GetUtcOffset(local)).ToUnixTimeSeconds() / 60 * 60;
        using (var store = new HistoryStore(Path.Combine(t.HistoryDir, HistoryStore.FileName)))
        {
            store.AddMinutes(new[] { new MinuteRow(minute, WallMeterSource.ReadingName, ReadingScope.Wall, 1.0, 60, 300) });
            store.SetLive(DateTimeOffset.UtcNow, new List<PowerReading> { new(WallMeterSource.ReadingName, 200, ReadingScope.Wall) }, Array.Empty<string>());
        }

        var window = new MainWindow(t.Services);
        window.Show();
        await window.RefreshCollectorAsync(includeService: true);   // learns the profile from the history...
        await window.RefreshCollectorAsync(includeService: false);  // ...and the next read prices with it

        var profile = UsageProfile.FromJson(t.Services.Database.GetSetting(SettingKeys.RateLookupProfile))!;
        Assert.Equal(1.0, profile.Weekday.Concat(profile.Weekend).Max(), precision: 9);   // all of it in one hour
        Assert.NotEqual("$0.50", window.StatCostText.Text);                              // not the single rate
        Assert.StartsWith("$0.08", window.StatCostText.Text);                            // 1 kWh at the off-peak 8 cents
    }
}
