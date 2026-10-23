package carbon

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// These are the cases of CarbonEiaTests and the EIA-independent parts of CarbonTests in the C# test project, one for one (Electricity Maps was
// not carried over).

var now = time.Date(2026, 10, 19, 15, 30, 0, 0, time.UTC)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

const goodKey = "abcdefghijklmnopqrstuvwxyz0123456789ABCD"

func TestOnlyAKeyShapedLikeOneIsAccepted(t *testing.T) {
	for key, valid := range map[string]bool{
		"abcDEF123456": true, "a-b_c.d12345": true, "short": false, "has space 123456": false, `quote"123456`: true, "tab\there1234": false,
		"line\nbreak1234": false, "AbC+dE/fG=hIjKlMn": true, "": false,
	} {
		if got := IsValidKey(key); got != valid {
			t.Errorf("key %q: %v, want %v", key, got, valid)
		}
	}
}

func TestAnEiaPlaceIsABalancingAuthorityCode(t *testing.T) {
	for place, valid := range map[string]bool{
		"MISO": true, "PJM": true, "ERCO": true, "US-MIDW-MISO": false, "39.7, -104.9": false, "MISO&x=1": false, "miso": false, "A": false, "": false,
	} {
		if got := IsValidPlace(place); got != valid {
			t.Errorf("EIA place %q: %v, want %v", place, got, valid)
		}
	}
	if Normalised(" miso ") != "MISO" {
		t.Error("an authority is upper case")
	}
}

func TestExplainsWhatIsMissing(t *testing.T) {
	cases := []struct{ key, place, want string }{
		{"", "MISO", "Paste your EIA API key"},
		{"no", "MISO", "does not look like an API key"},
		{goodKey, "", "balancing authority's code, like MISO"},
		{goodKey, "MI SO", "letters and digits only"},
	}
	for _, c := range cases {
		if got := Problem(c.key, c.place); !strings.Contains(got, c.want) {
			t.Errorf("Problem(%q, %q) = %q, want it to say %q", c.key, c.place, got, c.want)
		}
	}
	if Problem(goodKey, "MISO") != "" {
		t.Error("a good setup should have no problem")
	}
}

func TestASetupIsOnlyUsableWhenItsKeyAndPlaceFitAndItIsEiaOrFromBeforeThereWasAChoice(t *testing.T) {
	good, ok := Config{APIKey: " " + goodKey + " ", Place: "miso"}.Valid()
	if !ok || good.Source != EIA || good.APIKey != goodKey || good.Place != "MISO" {
		t.Errorf("good = %+v, %v", good, ok)
	}
	if _, ok := (Config{Source: EIA, APIKey: goodKey, Place: "PJM"}).Valid(); !ok {
		t.Error("a setup saved as eia should be usable")
	}
	for name, bad := range map[string]Config{
		"saved for Electricity Maps": {Source: "electricitymaps", APIKey: "abcdefgh1234", Place: "US-MIDW-MISO"},
		"unknown source":             {Source: "somewhere-else", APIKey: goodKey, Place: "MISO"},
		"a zone is not an authority": {APIKey: goodKey, Place: "US-MIDW-MISO"},
		"hand-edited place":          {APIKey: goodKey, Place: "x&y=1"},
		"no key":                     {Place: "MISO"},
	} {
		if _, ok := bad.Valid(); ok {
			t.Errorf("%s should not be usable", name)
		}
	}
}

func fakeSource(t *testing.T, respond func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, *[]*http.Request) {
	t.Helper()
	var seen []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r)
		respond(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func eiaRow(period, fuel, value string) string {
	return fmt.Sprintf(`{"period":%q,"respondent":"MISO","respondent-name":"Midcontinent Independent System Operator, Inc.","fueltype":%q,"type-name":"x","value":%q,"value-units":"megawatthours"}`, period, fuel, value)
}

// realHour is the hour 04:00 UTC on 19 Oct 2026 as MISO reported it, including battery storage (negative, charging).
func realHour(period string) []string {
	return []string{
		eiaRow(period, "BAT", "-214"), eiaRow(period, "COL", "16146"), eiaRow(period, "NG", "20970"), eiaRow(period, "NUC", "8329"),
		eiaRow(period, "OTH", "536"), eiaRow(period, "SUN", "8"), eiaRow(period, "WAT", "546"), eiaRow(period, "WND", "18383"),
	}
}

func eiaAnswer(rows ...string) string {
	return `{"warnings":[{"warning":"incomplete return","description":"The API can only return 5000 rows."}],"response":{"total":"493927","dateFormat":"YYYY-MM-DD\"T\"HH24","frequency":"hourly","data":[` + strings.Join(rows, ",") + `]}}`
}

func TestAnHoursIntensityIsEachFuelsLifeCycleEmissionsWeightedByWhatItGenerated(t *testing.T) {
	got, err := ParseEia(eiaAnswer(realHour("2026-10-19T04")...))
	if err != nil || len(got) != 1 {
		t.Fatalf("got %v, %v", got, err)
	}
	// (16146 MWh coal x 820 + 20970 gas x 490 + 8329 nuclear x 12 + 536 other x 230 + 8 solar x 45 + 546 hydro x 24 + 18383 wind x 11) / 64918 MWh.
	if math.Abs(got[0].GramsPerKwh-368.99) > 0.005 || !got[0].At.Equal(time.Date(2026, 10, 19, 4, 0, 0, 0, time.UTC)) || got[0].Estimated {
		t.Errorf("reading = %+v", got[0])
	}
}

func TestBatteryStorageAndUnknownFuelsAreLeftOutAndANegativeFigureCountsAsNone(t *testing.T) {
	plain, _ := Intensity(map[string]float64{"COL": 100, "NG": 100, "WND": 100})
	extras, _ := Intensity(map[string]float64{"COL": 100, "NG": 100, "WND": 100, "BAT": 900, "PS": 300, "UES": 50, "WAT": -40})
	if !near(plain, extras) || !near(plain, (820+490+11)/3.0) {
		t.Errorf("plain %v, with extras %v", plain, extras)
	}
	if _, ok := Intensity(map[string]float64{"BAT": 10, "COL": 0}); ok {
		t.Error("nothing that counts should be no figure, not zero")
	}
}

func TestSeveralHoursComeBackOldestFirstAndAPartialHourIsSkipped(t *testing.T) {
	rows := append(append(realHour("2026-10-19T05"), realHour("2026-10-19T03")...),
		eiaRow("2026-10-19T06", "COL", "16000"), eiaRow("2026-10-19T06", "NG", "21000")) // the newest hour, only partly reported
	got, err := ParseEia(eiaAnswer(rows...))
	if err != nil || len(got) != 2 || got[0].At.Hour() != 3 || got[1].At.Hour() != 5 {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestNumbersMayBeTextOrNumbersAndOddRowsAreIgnored(t *testing.T) {
	body := `{"response":{"data":[
      {"period":"2026-10-19T04","fueltype":"COL","value":300},
      {"period":"2026-10-19T04","fueltype":"NG","value":"300"},
      {"period":"2026-10-19T04","fueltype":"WND","value":"300.5"},
      {"period":"2026-10-19T04","fueltype":"SUN","value":"n/a"},
      {"period":"2026-10-19T04","fueltype":"NUC","value":null},
      {"period":"bad","fueltype":"COL","value":"1"},
      {"fueltype":"COL","value":"1"},
      {"period":"2026-10-19T04","value":"1"},
      5]}}`
	got, err := ParseEia(body)
	if err != nil || len(got) != 1 || math.Abs(got[0].GramsPerKwh-(300*820+300*490+300.5*11)/900.5) > 1e-6 {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestAnythingThatIsNotGenerationDataIsAMessage(t *testing.T) {
	for _, answer := range []string{"", "<html>maintenance</html>", "[]", `{"response":{}}`, `{"response":{"data":5}}`, `{"nothing":1}`} {
		if _, err := ParseEia(answer); err == nil || !strings.Contains(err.Error(), "not with generation data") {
			t.Errorf("%q: err = %v", answer, err)
		}
	}
}

var eiaConfig = Config{Source: EIA, APIKey: goodKey, Place: "MISO"}

func TestTheRequestGoesOnlyToEiaWithTheAuthorityAndKeyEncoded(t *testing.T) {
	raw := EiaURL(EiaBase, Config{Source: EIA, APIKey: "k&x=1 #y12345678", Place: "MISO"})
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Host != "api.eia.gov" || u.Path != "/v2/electricity/rto/fuel-type-data/data/" || q.Get("api_key") != "k&x=1 #y12345678" ||
		q.Get("facets[respondent][]") != "MISO" || q.Get("frequency") != "hourly" || q.Get("length") != "600" || q.Get("x") != "" {
		t.Errorf("request = %s", raw)
	}
	if !strings.Contains(u.RawQuery, "api_key=k%26x%3D1%20%23y12345678") || !strings.Contains(u.RawQuery, "facets%5Brespondent%5D%5B%5D=MISO") {
		t.Errorf("raw query = %s", u.RawQuery)
	}
}

func TestReadsTheRecentHoursWithAGet(t *testing.T) {
	srv, seen := fakeSource(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, eiaAnswer(append(realHour("2026-10-19T03"), realHour("2026-10-19T04")...)...))
	})
	got, err := (&Client{EiaBase: srv.URL + "/data/"}).Read(context.Background(), eiaConfig, now)
	if err != nil || len(got) != 2 || (*seen)[0].Method != http.MethodGet {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestAPlaceEiaKnowsNothingAboutIsSaidPlainly(t *testing.T) {
	srv, _ := fakeSource(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, eiaAnswer()) })
	cfg := eiaConfig
	cfg.Place = "NOPE"
	_, err := (&Client{EiaBase: srv.URL}).Read(context.Background(), cfg, now)
	if err == nil || !strings.Contains(err.Error(), `no recent generation data for "NOPE"`) {
		t.Errorf("err = %v", err)
	}
}

func TestExplainsWhatTheErrorMeansInEiasOwnWordsNeverEchoingTheKey(t *testing.T) {
	for status, c := range map[int]struct {
		want    string
		limited bool
	}{400: {"did not accept the request", false}, 403: {"did not accept the key (error 403)", false}, 429: {"slow down", true}, 500: {"error (500)", false}} {
		srv, _ := fakeSource(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			fmt.Fprint(w, `{"error":{"code":"API_KEY_INVALID","message":"Key `+eiaConfig.APIKey+` is invalid."}}`)
		})
		_, err := (&Client{EiaBase: srv.URL}).Read(context.Background(), eiaConfig, now)
		ce, ok := err.(*Error)
		if !ok || !strings.Contains(ce.Message, c.want) || !strings.Contains(ce.Message, "EIA says: Key (key) is invalid.") || strings.Contains(ce.Message, eiaConfig.APIKey) || ce.RateLimited != c.limited {
			t.Errorf("%d: err = %v", status, err)
		}
	}
	for body, want := range map[string]string{
		`{"error":"Bad thing"}`: "Bad thing", `{"error":{"message":"Nested"}}`: "Nested", `{"error":{"code":"X"}}`: "", "<html>": "", `{"other":1}`: "",
	} {
		if got := eiaMessage(body, "abcdefgh1234"); got != want {
			t.Errorf("%q: %q, want %q", body, got, want)
		}
	}
	if got := eiaMessage(`{"error":"`+strings.Repeat("x", 500)+`"}`, "k"); len(got) != 203 {
		t.Errorf("a long message is %d long, want it cut to 203", len(got))
	}
}

func TestAnUnreachableServiceSaysSoAndAHangingOneTimesOut(t *testing.T) {
	_, err := (&Client{EiaBase: "http://127.0.0.1:1/data"}).Read(context.Background(), eiaConfig, now)
	if err == nil || !strings.HasPrefix(err.Error(), "Could not reach EIA") || strings.Contains(err.Error(), goodKey) {
		t.Errorf("unreachable: %v", err)
	}
	srv, _ := fakeSource(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	_, err = (&Client{EiaBase: srv.URL, EiaLimit: 100 * time.Millisecond}).Read(context.Background(), eiaConfig, now)
	if err == nil || !strings.Contains(err.Error(), "did not answer") {
		t.Errorf("hanging: %v", err)
	}
}

// ---- the calculator

var t0 = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

// seeded: 12:00 100 Wh, 13:00 200 Wh, 14:00 100 Wh with no figure of its own; 12:00 is 400 g and 13:00 is 200 g.
func seeded() (map[int64]float64, []HourKWh) {
	known := map[int64]float64{t0.Unix(): 400, t0.Add(time.Hour).Unix(): 200}
	hours := []HourKWh{{t0.Unix(), 0.100}, {t0.Add(time.Hour).Unix(), 0.200}, {t0.Add(2 * time.Hour).Unix(), 0.100}}
	return known, hours
}

func TestEachHoursEnergyIsValuedAtItsOwnHoursIntensity(t *testing.T) {
	known, hours := seeded()
	r := Value(known, 300, hours)
	// 0.1 kWh x 400 g + 0.2 kWh x 200 g + 0.1 kWh at the average of the known hours (300 g) = 40 + 40 + 30 g.
	if !near(r.Kg, 0.110) || !near(r.KWh, 0.4) || !near(r.EstimatedKWh, 0.1) || !near(r.EstimatedShare(), 0.25) || math.Abs(r.AverageGramsPerKwh()-275) > 1e-6 {
		t.Errorf("result = %+v", r)
	}
}

func TestAPeriodBeforeAnyFigureWasSavedUsesTheAverageOfAllOfThem(t *testing.T) {
	r := Value(map[int64]float64{}, 300, []HourKWh{{t0.Add(-30 * 24 * time.Hour).Unix(), 0.5}})
	if !near(r.Kg, 0.5*300/1000) || !near(r.EstimatedShare(), 1) { // all of it, and it says so
		t.Errorf("result = %+v", r)
	}
	if got := Value(nil, 300, nil); got.Kg != 0 || got.EstimatedShare() != 0 || got.AverageGramsPerKwh() != 0 {
		t.Errorf("no energy = %+v", got)
	}
}

// ---- the feed

type fakeSink struct {
	hours    map[int64]float64
	statuses []Status
}

func (f *fakeSink) AddCarbon(_ context.Context, at time.Time, g float64, _ bool) error {
	if f.hours == nil {
		f.hours = map[int64]float64{}
	}
	f.hours[at.Truncate(time.Hour).Unix()] = g
	return nil
}

func (f *fakeSink) SaveStatus(_ context.Context, s Status) error {
	f.statuses = append(f.statuses, s)
	return nil
}

func (f *fakeSink) last() Status { return f.statuses[len(f.statuses)-1] }

func okEia(w http.ResponseWriter, _ *http.Request) {
	fmt.Fprint(w, eiaAnswer(append(realHour("2026-10-19T13"), realHour("2026-10-19T14")...)...))
}

func feedFor(t *testing.T, cfg **Config, respond func(w http.ResponseWriter, r *http.Request)) (*Feed, *fakeSink, *time.Time, *[]*http.Request) {
	t.Helper()
	srv, seen := fakeSource(t, respond)
	clock := time.Date(2026, 10, 19, 14, 5, 0, 0, time.UTC)
	sink := &fakeSink{}
	return &Feed{
		Client: &Client{EiaBase: srv.URL},
		Load:   func(context.Context) (*Config, error) { return *cfg, nil },
		Sink:   sink,
		Now:    func() time.Time { return clock },
	}, sink, &clock, seen
}

func TestWithNothingSetUpItAsksNothingAndSaysNothing(t *testing.T) {
	var cfg *Config
	f, sink, _, seen := feedFor(t, &cfg, okEia)
	if asked, err := f.Step(context.Background()); asked || err != nil || len(*seen) != 0 || len(sink.statuses) != 0 {
		t.Errorf("asked %v, %v, %d requests, %d statuses", asked, err, len(*seen), len(sink.statuses))
	}
}

func TestSavesEveryHourItGotAndSaysSo(t *testing.T) {
	cfg := &eiaConfig
	f, sink, clock, _ := feedFor(t, &cfg, okEia)
	if asked, _ := f.Step(context.Background()); !asked {
		t.Fatal("it should have asked")
	}
	if len(sink.hours) != 2 {
		t.Errorf("hours = %v", sink.hours)
	}
	s := sink.last()
	if !s.OK || s.GramsPerKwh == nil || math.Abs(*s.GramsPerKwh-368.99) > 0.005 || s.At == nil || s.At.Hour() != 14 || !s.CheckedAt.Equal(*clock) {
		t.Errorf("status = %+v", s)
	}
}

func TestAsksAgainOnlyOnceTheIntervalHasPassed(t *testing.T) {
	cfg := &eiaConfig
	f, _, clock, seen := feedFor(t, &cfg, okEia)
	f.Step(context.Background())
	*clock = clock.Add(10 * time.Minute)
	if asked, _ := f.Step(context.Background()); asked || len(*seen) != 1 {
		t.Errorf("asked early: %v, %d requests", asked, len(*seen))
	}
	*clock = clock.Add(Interval)
	if asked, _ := f.Step(context.Background()); !asked || len(*seen) != 2 {
		t.Errorf("did not ask after the interval: %v, %d requests", asked, len(*seen))
	}
}

func TestANewKeyOrPlaceIsUsedAtOnceNotAtTheNextInterval(t *testing.T) {
	cfg := &eiaConfig
	f, _, clock, seen := feedFor(t, &cfg, okEia)
	f.Step(context.Background())
	changed := Config{Source: EIA, APIKey: goodKey, Place: "PJM"}
	cfg = &changed
	*clock = clock.Add(30 * time.Second)
	if asked, _ := f.Step(context.Background()); !asked || !strings.Contains((*seen)[1].URL.RawQuery, "PJM") {
		t.Errorf("asked %v; %d requests", asked, len(*seen))
	}
}

func TestAFailureIsSavedWithItsReasonAndRetriedSoonerThanASuccess(t *testing.T) {
	cfg := &eiaConfig
	status := 403
	f, sink, clock, _ := feedFor(t, &cfg, func(w http.ResponseWriter, r *http.Request) {
		if status != 200 {
			w.WriteHeader(status)
			return
		}
		okEia(w, r)
	})
	f.Step(context.Background())
	if s := sink.last(); s.OK || !strings.Contains(s.Error, "did not accept the key") || len(sink.hours) != 0 { // nothing made up
		t.Errorf("status = %+v, hours %v", s, sink.hours)
	}
	*clock = clock.Add(RetryAfterError + time.Second)
	status = 200
	if asked, _ := f.Step(context.Background()); !asked || !sink.last().OK {
		t.Errorf("asked %v, status %+v", asked, sink.last())
	}
}

func TestBeingToldToSlowDownMeansWaitingAGoodWhile(t *testing.T) {
	cfg := &eiaConfig
	f, _, clock, _ := feedFor(t, &cfg, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(429) })
	f.Step(context.Background())
	*clock = clock.Add(RetryAfterError + time.Minute)
	if asked, _ := f.Step(context.Background()); asked { // not yet: it asked for time
		t.Error("asked again too soon after being told to slow down")
	}
	*clock = clock.Add(RetryAfterLimit)
	if asked, _ := f.Step(context.Background()); !asked {
		t.Error("did not ask once the wait had passed")
	}
}

func TestRemovingTheKeyIsSaidOnceAndStopsTheAsking(t *testing.T) {
	cfg := &eiaConfig
	f, sink, clock, seen := feedFor(t, &cfg, okEia)
	f.Step(context.Background())
	cfg = nil
	*clock = clock.Add(2 * time.Hour)
	if asked, _ := f.Step(context.Background()); asked || sink.last().Error != "Not set up." || len(*seen) != 1 || len(sink.hours) != 2 { // the hours already saved are kept
		t.Errorf("asked %v, status %+v, %d requests, hours %v", asked, sink.last(), len(*seen), sink.hours)
	}
	n := len(sink.statuses)
	f.Step(context.Background())
	if len(sink.statuses) != n {
		t.Error("it said 'Not set up.' again")
	}
}
