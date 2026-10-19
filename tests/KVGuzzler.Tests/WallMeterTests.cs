using System.Net;
using KVGuzzler.Core.History;
using KVGuzzler.Core.Power;
using Xunit;

namespace KVGuzzler.Tests;

/// <summary>A clock the test moves by hand.</summary>
internal sealed class FakeClock : TimeProvider
{
    private long _ticks;
    public override long TimestampFrequency => 1_000;   // one tick per millisecond
    public override long GetTimestamp() => Interlocked.Read(ref _ticks);
    public void Advance(TimeSpan by) => Interlocked.Add(ref _ticks, (long)by.TotalMilliseconds);
}

/// <summary>Plays the part of the plug: answers with whatever the test says and remembers what was asked.</summary>
internal sealed class FakePlug : HttpMessageHandler
{
    private readonly object _gate = new();
    public const string Answer = """{"id":0,"source":"init","output":true,"apower":52.1,"voltage":121.3,"current":0.43,"aenergy":{"total":1234.5,"by_minute":[0,0,0],"minute_ts":1792341000}}""";

    public Func<HttpRequestMessage, HttpResponseMessage> Respond { get; set; } =
        _ => new HttpResponseMessage(HttpStatusCode.OK) { Content = new StringContent(Answer) };

    public List<(HttpMethod Method, Uri Uri)> Requests { get; } = new();

    protected override Task<HttpResponseMessage> SendAsync(HttpRequestMessage request, CancellationToken cancellationToken)
    {
        lock (_gate) Requests.Add((request.Method, request.RequestUri!));
        return Task.FromResult(Respond(request));
    }

    public Uri? LastUri { get { lock (_gate) return Requests.LastOrDefault().Uri; } }
}

public class ShellyPlugTests
{
    private static HttpClient Client(HttpMessageHandler h) => new(h);

    [Fact]
    public void ReadsTheWattsFromARealisticAnswer() =>
        Assert.Equal(52.1, ShellyPlug.ParseWatts(FakePlug.Answer));

    [Fact]
    public void ReadsTheAnswerOfARealPlugUsGen4()
    {
        // Captured from a real Shelly Plug US Gen4 (firmware 1.7.99), with nothing plugged into it: spacing and all.
        const string real = """{"id":0, "source":"init", "output":false, "apower":0.0, "voltage":122.0, "freq":60.1, "current":0.000, "aenergy":{"total":0.000,"by_minute":[0.000,0.000,0.000],"minute_ts":1792369140}, "ret_aenergy":{"total":0.000,"by_minute":[0.000,0.000,0.000],"minute_ts":1792369140},"temperature":{"tC":35.0, "tF":95.0}}""";
        Assert.Equal(0.0, ShellyPlug.ParseWatts(real));
        Assert.Equal(187.4, ShellyPlug.ParseWatts(real.Replace("\"apower\":0.0", "\"apower\":187.4")));
    }

    [Theory]
    [InlineData("""{"id":0,"output":true}""")]            // no power reading
    [InlineData("""{"apower":"52.1"}""")]                 // a string, not a number
    [InlineData("""{"apower":-3}""")]
    [InlineData("""[1,2,3]""")]
    [InlineData("<html>router login</html>")]
    [InlineData("")]
    public void AnythingElseIsReportedAsNotAShelly(string answer) =>
        Assert.Contains("Is this address the Shelly plug?", Assert.Throws<WallMeterException>(() => ShellyPlug.ParseWatts(answer)).Message);

    [Fact]
    public async Task AsksOnlyAndAlwaysWithAReadOfTheStatus()
    {
        var plug = new FakePlug();
        var watts = await ShellyPlug.ReadWattsAsync(Client(plug), "192.168.1.50", TestContext.Current.CancellationToken);

        Assert.Equal(52.1, watts);
        var (method, uri) = Assert.Single(plug.Requests);
        Assert.Equal(HttpMethod.Get, method);                    // it can never switch the plug
        Assert.Equal("http://192.168.1.50/rpc/Switch.GetStatus?id=0", uri.ToString());
    }

    [Theory]
    [InlineData(401, "password")]
    [InlineData(404, "second generation or newer")]
    [InlineData(500, "error (500)")]
    public async Task ExplainsWhatTheErrorMeans(int status, string expected)
    {
        var plug = new FakePlug { Respond = _ => new HttpResponseMessage((HttpStatusCode)status) };
        var e = await Assert.ThrowsAsync<WallMeterException>(() => ShellyPlug.ReadWattsAsync(Client(plug), "192.168.1.50", TestContext.Current.CancellationToken));
        Assert.Contains(expected, e.Message);
    }

    [Fact]
    public async Task AnUnreachablePlugSaysSo()
    {
        var plug = new FakePlug { Respond = _ => throw new HttpRequestException("No route to host") };
        var e = await Assert.ThrowsAsync<WallMeterException>(() => ShellyPlug.ReadWattsAsync(Client(plug), "192.168.1.50", TestContext.Current.CancellationToken));
        Assert.StartsWith("Could not reach 192.168.1.50", e.Message);
    }

    [Fact]
    public async Task APlugThatNeverAnswersTimesOutWithAPlainMessage()
    {
        var plug = new HangingHandler();
        var e = await Assert.ThrowsAsync<WallMeterException>(
            () => ShellyPlug.ReadWattsAsync(Client(plug), "192.168.1.50", TestContext.Current.CancellationToken, TimeSpan.FromMilliseconds(100)));
        Assert.Contains("did not answer", e.Message);
    }

    [Theory]
    [InlineData("192.168.68..106")]
    [InlineData("a..b")]
    [InlineData("not a host")]
    public async Task ABadAddressIsAMessageAndNeverAnException(string host)
    {
        // Even if something skips the address check, the reader itself must not throw a UriFormatException.
        var e = await Assert.ThrowsAsync<WallMeterException>(
            () => ShellyPlug.ReadWattsAsync(Client(new FakePlug()), host, TestContext.Current.CancellationToken));
        Assert.Contains("not a valid address", e.Message);
    }

    [Fact]
    public async Task ShuttingDownIsNotMistakenForATimeout()
    {
        using var cancel = new CancellationTokenSource();
        cancel.Cancel();
        await Assert.ThrowsAnyAsync<OperationCanceledException>(
            () => ShellyPlug.ReadWattsAsync(Client(new HangingHandler()), "192.168.1.50", cancel.Token));
    }

    private sealed class HangingHandler : HttpMessageHandler
    {
        protected override async Task<HttpResponseMessage> SendAsync(HttpRequestMessage request, CancellationToken cancellationToken)
        {
            await Task.Delay(Timeout.Infinite, cancellationToken);
            return new HttpResponseMessage();
        }
    }
}

public class WallMeterConfigTests : IDisposable
{
    private readonly string _dir = Path.Combine(Path.GetTempPath(), "kvguzzler-wall-" + Guid.NewGuid().ToString("N")[..8]);
    private string File_ => Path.Combine(_dir, WallMeterConfig.FileName);

    public void Dispose()
    {
        try { Directory.Delete(_dir, recursive: true); } catch (IOException) { /* best effort */ }
    }

    [Theory]
    [InlineData("192.168.1.50", true)]
    [InlineData("192.168.1.50:8080", true)]
    [InlineData("shelly-pc.lan", true)]
    [InlineData("shellyplug-ABC123", true)]
    [InlineData("", false)]
    [InlineData("http://192.168.1.50", false)]            // a URL, not an address
    [InlineData("192.168.1.50/rpc/Shelly.Reboot", false)] // a path: only the plug's own address is ever asked
    [InlineData("192.168.1.50 ", false)]
    [InlineData("user:pass@192.168.1.50", false)]
    [InlineData("-bad.example", false)]
    [InlineData("192.168.68..106", false)]                // a doubled dot: the pattern allows it, the URL parser does not. This crashed the app
    [InlineData("a..b", false)]
    [InlineData("192.168..", false)]
    [InlineData("192.168.68.106:99999", false)]           // not a port
    public void OnlyAPlainAddressIsAccepted(string host, bool valid) =>
        Assert.Equal(valid, WallMeterConfig.IsValidHost(host));

    [Fact]
    public void SavesAndLoadsBack()
    {
        new WallMeterConfig("192.168.1.50").Save(File_);
        var loaded = WallMeterConfig.Load(File_);
        Assert.Equal("192.168.1.50", loaded!.Host);
        Assert.Equal("shelly", loaded.Type);
    }

    [Fact]
    public void AMissingOrDamagedFileMeansNoMeter()
    {
        Assert.Null(WallMeterConfig.Load(File_));
        Directory.CreateDirectory(_dir);
        File.WriteAllText(File_, "not json {");
        Assert.Null(WallMeterConfig.Load(File_));
        File.WriteAllText(File_, """{"host":"http://evil.example/x"}""");   // hand-edited into something that is not an address
        Assert.Null(WallMeterConfig.Load(File_));
    }

    [Fact]
    public void RemovingForgetsTheMeterAndIsHarmlessTwice()
    {
        new WallMeterConfig("192.168.1.50").Save(File_);
        WallMeterConfig.Remove(File_);
        WallMeterConfig.Remove(File_);
        Assert.Null(WallMeterConfig.Load(File_));
    }
}

public class WallMeterSourceTests : IDisposable
{
    private readonly string _dir = Path.Combine(Path.GetTempPath(), "kvguzzler-wallsrc-" + Guid.NewGuid().ToString("N")[..8]);
    private string ConfigPath => Path.Combine(_dir, WallMeterConfig.FileName);
    private readonly FakePlug _plug = new();
    private readonly FakeClock _clock = new();
    private readonly List<IDisposable> _toDispose = new();

    public void Dispose()
    {
        foreach (var d in _toDispose) d.Dispose();
        try { Directory.Delete(_dir, recursive: true); } catch (IOException) { /* best effort */ }
    }

    private WallMeterSource Make()
    {
        var source = new WallMeterSource(ConfigPath, _plug, TimeSpan.FromMilliseconds(20), _clock);
        _toDispose.Add(source);
        return source;
    }

    private static async Task<bool> Eventually(Func<bool> condition)
    {
        for (var i = 0; i < 200; i++)
        {
            if (condition()) return true;
            await Task.Delay(20, TestContext.Current.CancellationToken);
        }
        return false;
    }

    [Fact]
    public void WithoutAMeterItSaysWhereToSetOneUp()
    {
        var source = Make();
        Assert.Contains("Settings", source.UnavailableReason);
        Assert.Empty(_plug.Requests);   // nothing is asked of the network until a plug is configured
    }

    [Fact]
    public async Task ReadsTheConfiguredPlugAsTheWholePc()
    {
        new WallMeterConfig("192.168.1.50").Save(ConfigPath);
        var source = Make();
        _ = source.UnavailableReason;   // looks at the config and starts polling

        Assert.True(await Eventually(() => source.UnavailableReason is null), "no reading arrived");
        var reading = Assert.Single(source.Read());
        Assert.Equal(WallMeterSource.ReadingName, reading.Name);
        Assert.Equal(52.1, reading.Watts);
        Assert.Equal(ReadingScope.Wall, reading.Scope);
        Assert.All(_plug.Requests, r => Assert.Equal(HttpMethod.Get, r.Method));
    }

    [Fact]
    public async Task AnUnreachablePlugIsReportedWithItsReason()
    {
        _plug.Respond = _ => throw new HttpRequestException("No route to host");
        new WallMeterConfig("192.168.1.50").Save(ConfigPath);
        var source = Make();
        _ = source.UnavailableReason;

        Assert.True(await Eventually(() => source.UnavailableReason?.StartsWith("Could not reach") == true));
        Assert.Empty(source.Read());
    }

    [Fact]
    public async Task AReadingGoesStaleWhenThePlugStopsAnswering()
    {
        new WallMeterConfig("192.168.1.50").Save(ConfigPath);
        var source = Make();
        _ = source.UnavailableReason;
        Assert.True(await Eventually(() => source.UnavailableReason is null));

        _plug.Respond = _ => throw new HttpRequestException("gone");
        Assert.True(await Eventually(() => _plug.Requests.Count > 3));   // a few failed polls go by
        _clock.Advance(WallMeterSource.Fresh + TimeSpan.FromSeconds(1));

        Assert.Empty(source.Read());   // an old watt figure is never shown as current
        Assert.Contains("gone", source.UnavailableReason);
    }

    [Fact]
    public async Task FollowsAChangeOfAddressAndRemoval()
    {
        new WallMeterConfig("192.168.1.50").Save(ConfigPath);
        var source = Make();
        _ = source.UnavailableReason;
        Assert.True(await Eventually(() => source.UnavailableReason is null));

        _clock.Advance(TimeSpan.FromSeconds(3));   // past the pause between looks at the config file
        new WallMeterConfig("192.168.1.77").Save(ConfigPath);
        File.SetLastWriteTimeUtc(ConfigPath, DateTime.UtcNow.AddSeconds(5));   // a coarse file clock must not hide the edit
        _ = source.UnavailableReason;
        Assert.True(await Eventually(() => _plug.LastUri?.Host == "192.168.1.77"), "still asking the old address");

        _clock.Advance(TimeSpan.FromSeconds(3));
        WallMeterConfig.Remove(ConfigPath);
        Assert.Contains("Settings", source.UnavailableReason);
        Assert.Empty(source.Read());
    }
}

public class WallMeterHistoryTests
{
    private static readonly DateTimeOffset T0 = new(2026, 10, 18, 12, 0, 0, TimeSpan.Zero);

    private sealed class PcAndWall : IPowerSource
    {
        public string Name => "pc and wall";
        public string? UnavailableReason => null;
        public IReadOnlyList<PowerReading> Read() => new[]
        {
            new PowerReading("GPU", 100, ReadingScope.Component),
            new PowerReading("CPU package", 50, ReadingScope.Component),
            new PowerReading(WallMeterSource.ReadingName, 200, ReadingScope.Wall),
        };
        public void Dispose() { }
    }

    [Fact]
    public void TheWallFigureIsNeverAddedToTheComponents()
    {
        var sampler = new PowerSampler(new IPowerSource[] { new PcAndWall() });
        var s = sampler.Sample(T0);
        Assert.Equal(150, s.TotalWatts);                 // not 350
        Assert.Equal(3, s.Readings.Count);
    }

    [Fact]
    public void WallEnergyIsSavedAndReportedApartFromTheActiveEnergy()
    {
        using var h = new TempHistory();
        var c = new PowerCollector(h.Store, new PowerSampler(new IPowerSource[] { new PcAndWall() }, maxGap: TimeSpan.FromMinutes(5)));
        c.Start(T0);
        for (var i = 0; i <= 3600; i++) c.Tick(T0.AddSeconds(i));
        c.Stop();

        var end = T0.AddHours(1).AddSeconds(1);
        Assert.Equal(0.15, h.Store.Query(T0, end).KilowattHours, precision: 6);                       // active: 150 W for an hour
        Assert.Equal(0.2, h.Store.Query(T0, end, ReadingScope.Wall).KilowattHours, precision: 6);      // whole PC: 200 W for an hour

        var report = HistoryReport.Build(h.Store, "Last hour", T0, end, sleepWatts: 2);
        Assert.Equal(0.15, report.MeasuredKwh, precision: 6);
        Assert.Equal(0.2, report.WallKwh!.Value, precision: 6);
    }

    [Fact]
    public void WithoutAWallMeterThereIsNoWallFigure()
    {
        using var h = new TempHistory();
        h.Store.AddMinutes(new[] { new MinuteRow(T0.ToUnixTimeSeconds(), "GPU", ReadingScope.Component, 1, 60, 1) });
        Assert.Null(HistoryReport.Build(h.Store, "x", T0, T0.AddMinutes(5), 2).WallKwh);
    }
}

public class WallMeterComparisonTests
{
    private static readonly DateTimeOffset T0 = new(2026, 10, 18, 12, 0, 0, TimeSpan.Zero);

    [Fact]
    public void TheSensorsShareIsOnlyCountedWhereTheWallMeterAlsoRecorded()
    {
        using var h = new TempHistory();
        var m = T0.ToUnixTimeSeconds();
        // The sensors recorded for ten minutes (0.01 kWh each); the plug was only set up for the last five (0.03 kWh each).
        for (var i = 0; i < 10; i++)
            h.Store.AddMinutes(new[] { new MinuteRow(m + i * 60, "GPU", ReadingScope.Component, 0.01, 60, 100) });
        for (var i = 5; i < 10; i++)
            h.Store.AddMinutes(new[] { new MinuteRow(m + i * 60, WallMeterSource.ReadingName, ReadingScope.Wall, 0.03, 60, 300) });

        var report = HistoryReport.Build(h.Store, "test", T0, T0.AddMinutes(10), sleepWatts: 2);

        Assert.Equal(0.15, report.WallKwh!.Value, precision: 9);
        Assert.Equal(0.05, report.MeasuredKwh, precision: 9);            // not 0.10: the first five minutes had no wall reading
        Assert.Equal(TimeSpan.FromMinutes(5), report.Monitored);
    }

    [Fact]
    public void WithoutAWallMeterAllTheSensorEnergyCounts()
    {
        using var h = new TempHistory();
        var m = T0.ToUnixTimeSeconds();
        for (var i = 0; i < 10; i++)
            h.Store.AddMinutes(new[] { new MinuteRow(m + i * 60, "GPU", ReadingScope.Component, 0.01, 60, 100) });

        var report = HistoryReport.Build(h.Store, "test", T0, T0.AddMinutes(10), sleepWatts: 2);
        Assert.Null(report.WallKwh);
        Assert.Equal(0.10, report.MeasuredKwh, precision: 9);
    }
}
