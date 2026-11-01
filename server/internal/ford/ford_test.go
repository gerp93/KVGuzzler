package ford

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestTheOdometerIsFoundWhereverTheAnswerKeepsIt(t *testing.T) {
	cases := []struct {
		name, body, path string
		value            float64
		wantErr          bool
	}{
		{"object with a value", `{"metrics":{"odometer":{"value":20000.5,"updateTime":"2026-10-22T00:00:00Z"}}}`, "metrics.odometer.value", 20000.5, false},
		{"plain number", `{"vehicle":{"odometer":1234}}`, "vehicle.odometer", 1234, false},
		{"number as text", `{"data":[{"Odometer":"98765"}]}`, "data[0].Odometer", 98765, false},
		{"the same figure twice is one figure", `{"a":{"odometer":5},"b":{"vehicleOdometer":{"value":5}}}`, "", 5, false},
		{"two vehicles", `{"a":{"odometer":5},"b":{"odometer":6}}`, "", 0, true},
		{"none", `{"metrics":{"speed":{"value":3}}}`, "", 0, true},
		{"a name with no figure under it", `{"odometer":{"unit":"km"}}`, "", 0, true},
		{"not json", `<html>`, "", 0, true},
	}
	for _, c := range cases {
		got, err := ParseOdometer([]byte(c.body))
		if c.wantErr != (err != nil) {
			t.Errorf("%s: err = %v, want error %v", c.name, err, c.wantErr)
			continue
		}
		if err == nil && (got.Value != c.value || (c.path != "" && got.Path != c.path)) {
			t.Errorf("%s: got %+v, want %v at %q", c.name, got, c.value, c.path)
		}
	}
}

func TestKilometersBecomeMilesAndNonsenseIsRefused(t *testing.T) {
	if m, ok := (Odometer{Value: 16093.44}).Miles(Kilometers); !ok || m != 10000 {
		t.Errorf("16093.44 km = %v miles (%v), want 10000", m, ok)
	}
	if m, ok := (Odometer{Value: 5000}).Miles(Miles); !ok || m != 5000 {
		t.Errorf("5000 miles = %v (%v)", m, ok)
	}
	for _, v := range []float64{-1, 1e12} {
		if _, ok := (Odometer{Value: v}).Miles(Miles); ok {
			t.Errorf("%v miles was accepted as an odometer", v)
		}
	}
}

func goodConfig() Config {
	return Config{
		ClientID:     "0f243454-a043-4230-bff1-406358da7f2d",
		ClientSecret: "s3cret",
		AuthorizeURL: "https://login.example.com/authorize?client_id=0f243454-a043-4230-bff1-406358da7f2d&redirect_uri=http%3A%2F%2Flocalhost%3A8000%2Fcallback&state=placeholder",
		TokenURL:     "https://login.example.com/token",
		OdometerUnit: Kilometers,
		MeterID:      "m1",
	}
}

func TestASetupIsRefusedUnlessTheSecretOnlyGoesWhereItShould(t *testing.T) {
	if msg := Problem(goodConfig(), false); msg != "" {
		t.Fatalf("a good setup was refused: %s", msg)
	}
	bad := map[string]func(c *Config){
		"no client id":      func(c *Config) { c.ClientID = "" },
		"no secret":         func(c *Config) { c.ClientSecret = "" },
		"no meter":          func(c *Config) { c.MeterID = "" },
		"no unit":           func(c *Config) { c.OdometerUnit = "" },
		"plain http token":  func(c *Config) { c.TokenURL = "http://login.example.com/token" },
		"ip address token":  func(c *Config) { c.TokenURL = "https://10.1.2.3/token" },
		"token with a user": func(c *Config) { c.TokenURL = "https://u:p@login.example.com/token" },
		"not an address":    func(c *Config) { c.TokenURL = "login" },
		"other client":      func(c *Config) { c.ClientID = "someone-else" },
		"no redirect": func(c *Config) {
			c.AuthorizeURL = "https://login.example.com/authorize?client_id=0f243454-a043-4230-bff1-406358da7f2d"
		},
	}
	for name, f := range bad {
		c := goodConfig()
		f(&c)
		if Problem(c, false) == "" {
			t.Errorf("%s was accepted", name)
		}
	}
	// A secret already saved may be left out.
	c := goodConfig()
	c.ClientSecret = ""
	if msg := Problem(c, true); msg != "" {
		t.Errorf("a saved secret left out was refused: %s", msg)
	}
}

func TestTheLinkAddressKeepsTheRegisteredPartsAndGetsAFreshState(t *testing.T) {
	link, err := LinkURL(goodConfig(), "abc123")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(link)
	q := u.Query()
	if q.Get("state") != "abc123" || q.Get("client_id") != goodConfig().ClientID || q.Get("redirect_uri") != "http://localhost:8000/callback" {
		t.Errorf("link = %s", link)
	}
	if RedirectURI(goodConfig()) != "http://localhost:8000/callback" {
		t.Errorf("redirect = %q", RedirectURI(goodConfig()))
	}
	a, _ := NewState()
	b, _ := NewState()
	if len(a) < 32 || a == b {
		t.Errorf("states %q and %q are not unpredictable", a, b)
	}
}

func TestOnlyTheAddressFromTheLinkJustMadeGivesACode(t *testing.T) {
	cases := []struct {
		name, pasted, want string
		wantErr            string
	}{
		{"whole address", "http://localhost:8000/callback?code=CODE1&state=S", "CODE1", ""},
		{"just the query", "?code=CODE2&state=S", "CODE2", ""},
		{"with spaces and a fragment", "  http://localhost:8000/callback?code=C3&state=S#x ", "C3", ""},
		{"another state", "http://localhost:8000/callback?code=C&state=OTHER", "", "state differs"},
		{"no state", "http://localhost:8000/callback?code=C", "", "state differs"},
		{"no code", "http://localhost:8000/callback?state=S", "", "no code"},
		{"declined", "http://localhost:8000/callback?error=access_denied&error_description=The+owner+said+no&state=S", "", "The owner said no"},
		{"empty", "", "", "Paste the address"},
	}
	for _, c := range cases {
		got, err := CodeFromRedirect(c.pasted, "S")
		if c.wantErr == "" && (err != nil || got != c.want) {
			t.Errorf("%s: %q, %v; want %q", c.name, got, err, c.want)
		}
		if c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)) {
			t.Errorf("%s: err = %v, want it to say %q", c.name, err, c.wantErr)
		}
	}
}

func TestTheCodeAndTheRefreshTokenAreTradedWithTheClientsOwnCredentials(t *testing.T) {
	var forms []url.Values
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f, _ := url.ParseQuery(string(b))
		forms = append(forms, f)
		if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("content type %q", r.Header.Get("Content-Type"))
		}
		switch f.Get("grant_type") {
		case "authorization_code":
			_, _ = w.Write([]byte(`{"access_token":"A1","refresh_token":"R1","expires_in":1200}`))
		default:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"AADB2C90080: The provided grant has expired."}`))
		}
	}))
	defer fake.Close()
	cfg := goodConfig()
	cfg.TokenURL = fake.URL + "/token"
	c := &Client{}

	tok, err := c.Exchange(context.Background(), cfg, "CODE")
	if err != nil || tok.Access != "A1" || tok.Refresh != "R1" {
		t.Fatalf("exchange = %+v, %v", tok, err)
	}
	f := forms[0]
	if f.Get("code") != "CODE" || f.Get("redirect_uri") != "http://localhost:8000/callback" || f.Get("client_id") != cfg.ClientID ||
		f.Get("client_secret") != "s3cret" || f.Get("scope") != cfg.ClientID+" offline_access openid" {
		t.Errorf("code exchange form = %v", f)
	}

	_, err = c.Refresh(context.Background(), cfg, "OLD")
	if f := forms[1]; f.Get("grant_type") != "refresh_token" || f.Get("refresh_token") != "OLD" || f.Get("redirect_uri") != "http://localhost:8000/callback" || f.Get("client_secret") != "s3cret" {
		t.Errorf("refresh form = %v", f)
	}
	var fe *Error
	if !errors.As(err, &fe) || !fe.Relink {
		t.Fatalf("a refused refresh token = %v, want a relink error", err)
	}
	if strings.Contains(fe.Message, "s3cret") || strings.Contains(fe.Message, "OLD") {
		t.Errorf("the message holds a secret: %q", fe.Message)
	}
}

func TestTelemetryIsOneGetWithTheAccessTokenAndForbiddenSaysWhy(t *testing.T) {
	status := http.StatusOK
	var seen []*http.Request
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"metrics":{"odometer":{"value":1}}}`))
	}))
	defer fake.Close()
	c := &Client{Base: fake.URL}
	if _, err := c.Telemetry(context.Background(), "TOKEN"); err != nil {
		t.Fatal(err)
	}
	if r := seen[0]; r.Method != http.MethodGet || r.URL.Path != TelemetryPath || r.Header.Get("Authorization") != "Bearer TOKEN" {
		t.Errorf("request = %s %s %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
	}
	status = http.StatusForbidden
	_, err := c.Telemetry(context.Background(), "TOKEN")
	if err == nil || !strings.Contains(err.Error(), "scope") {
		t.Errorf("403 = %v, want it to say the scope is missing", err)
	}
	status = http.StatusTooManyRequests
	var fe *Error
	if _, err := c.Telemetry(context.Background(), "TOKEN"); !errors.As(err, &fe) || !fe.RateLimited {
		t.Errorf("429 = %v, want a rate-limit error", err)
	}
}

// fakeSink keeps everything in memory.
type fakeSink struct {
	cfg      *Config
	refresh  string
	status   []Status
	readings map[string]float64
	order    []string
	refuse   error
	batts    []Battery
}

func (s *fakeSink) Config(context.Context) (*Config, error) { return s.cfg, nil }
func (s *fakeSink) Refresh(context.Context) (string, bool, error) {
	return s.refresh, s.refresh != "", nil
}
func (s *fakeSink) SaveRefresh(_ context.Context, t string) error {
	s.refresh = t
	s.order = append(s.order, "save-refresh:"+t)
	return nil
}
func (s *fakeSink) ClearRefresh(context.Context) error { s.refresh = ""; return nil }
func (s *fakeSink) SaveStatus(_ context.Context, st Status) error {
	s.status = append(s.status, st)
	return nil
}
func (s *fakeSink) SaveBattery(_ context.Context, _ string, b Battery) error {
	s.batts = append(s.batts, b)
	return nil
}
func (s *fakeSink) SaveOdometer(_ context.Context, meter, day string, miles float64) (bool, error) {
	if s.refuse != nil {
		return false, s.refuse
	}
	if s.readings == nil {
		s.readings = map[string]float64{}
	}
	s.readings[meter+" "+day] = miles
	return true, nil
}

// fordServer answers the token and telemetry calls: every refresh hands out the next refresh token, and a refresh token it did not hand out last is refused.
type fordServer struct {
	*httptest.Server
	issued    int
	last      string
	odometer  string
	telemetry int
	// battery is a member added to the telemetry answer (empty: none).
	battery string
	order   *[]string
	// extra answers other paths with a status and a body; fccs records what it was asked.
	extra map[string]fakeAnswer
	fccs  *http.Request
}

type fakeAnswer struct {
	status int
	body   string
}

func newFordServer(t *testing.T, sink *fakeSink) *fordServer {
	t.Helper()
	fs := &fordServer{odometer: `20000`, order: &sink.order}
	fs.last = "R0"
	fs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			b, _ := io.ReadAll(r.Body)
			f, _ := url.ParseQuery(string(b))
			if f.Get("grant_type") == "refresh_token" && f.Get("refresh_token") != fs.last {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			fs.issued++
			fs.last = "R" + string(rune('0'+fs.issued))
			_, _ = w.Write([]byte(`{"access_token":"A` + string(rune('0'+fs.issued)) + `","refresh_token":"` + fs.last + `"}`))
		case TelemetryPath:
			fs.telemetry++
			*fs.order = append(*fs.order, "telemetry")
			_, _ = w.Write([]byte(`{"metrics":{"odometer":{"value":` + fs.odometer + `}` + fs.battery + `}}`))
		default:
			if r.URL.Path == "/fcon-query/v1/fccs" {
				fs.fccs = r.Clone(context.Background())
			}
			if a, ok := fs.extra[r.URL.Path]; ok {
				w.WriteHeader(a.status)
				_, _ = w.Write([]byte(a.body))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(fs.Close)
	return fs
}

func feedFor(t *testing.T) (*Feed, *fakeSink, *fordServer, *time.Time) {
	t.Helper()
	cfg := goodConfig()
	sink := &fakeSink{cfg: &cfg, refresh: "R0"}
	fs := newFordServer(t, sink)
	sink.cfg.TokenURL = fs.URL + "/token"
	now := time.Date(2026, 10, 22, 23, 30, 0, 0, time.UTC)
	f := &Feed{Client: &Client{Base: fs.URL}, Sink: sink, Loc: time.UTC, Now: func() time.Time { return now }}
	return f, sink, fs, &now
}

func TestTheFeedSavesTheNewRefreshTokenBeforeAskingForTelemetryAndReadsFourTimesADayAtMost(t *testing.T) {
	f, sink, fs, now := feedFor(t)
	ctx := context.Background()

	if did, err := f.Step(ctx); err != nil || !did {
		t.Fatalf("first step = %v, %v; want a read", did, err)
	}
	if got := sink.order; len(got) != 2 || got[0] != "save-refresh:R1" || got[1] != "telemetry" {
		t.Errorf("order = %v: the rotated refresh token must be kept before telemetry is asked for", got)
	}
	if m := sink.readings["m1 2026-10-22"]; m < 12427 || m > 12428 {
		t.Errorf("20000 km saved as %v miles, want about 12427.4", m)
	}
	st := sink.status[len(sink.status)-1]
	if !st.OK || st.Found != "metrics.odometer.value" || st.Raw == nil || *st.Raw != 20000 {
		t.Errorf("status = %+v", st)
	}

	if did, _ := f.Step(ctx); did {
		t.Error("a second look right away read again")
	}
	*now = now.Add(Interval + time.Minute)
	fs.odometer = `20100`
	if did, err := f.Step(ctx); err != nil || !did {
		t.Fatalf("a step after the interval = %v, %v", did, err)
	}
	if fs.telemetry != 2 || sink.refresh != "R2" {
		t.Errorf("telemetry calls %d, refresh token %q; want 2 and R2", fs.telemetry, sink.refresh)
	}
}

func TestARefusedPermissionIsSaidOnceAndNothingIsTriedUntilLinkedAgain(t *testing.T) {
	f, sink, fs, now := feedFor(t)
	ctx := context.Background()
	sink.refresh = "STALE"

	if did, err := f.Step(ctx); err != nil || !did {
		t.Fatalf("step = %v, %v", did, err)
	}
	st := sink.status[len(sink.status)-1]
	if st.OK || !st.NeedsLink || sink.refresh != "" {
		t.Fatalf("status %+v, refresh %q: want it to ask for linking and drop the token", st, sink.refresh)
	}
	for i := 0; i < 5; i++ {
		*now = now.Add(2 * time.Hour)
		if did, _ := f.Step(ctx); did {
			t.Fatal("it kept asking Ford with a permission Ford refused")
		}
	}
	if fs.telemetry != 0 {
		t.Errorf("telemetry was asked for %d times", fs.telemetry)
	}

	// Linking again: the code is traded and the odometer read at once.
	if _, err := f.Complete(ctx, "CODE"); err != nil {
		t.Fatal(err)
	}
	if sink.refresh == "" || len(sink.readings) != 1 {
		t.Errorf("after linking: refresh %q, readings %v", sink.refresh, sink.readings)
	}
}

func TestAFailureWaitsAnHourAndAnOdometerThatWouldGoDownIsSaidNotSaved(t *testing.T) {
	f, sink, _, now := feedFor(t)
	ctx := context.Background()
	sink.refuse = &Error{Message: "Not saved: the reading before this one is 15000 miles: an odometer only goes up."}

	if did, err := f.Step(ctx); err != nil || !did {
		t.Fatalf("step = %v, %v", did, err)
	}
	st := sink.status[len(sink.status)-1]
	if st.OK || st.NeedsLink || !strings.Contains(st.Error, "only goes up") {
		t.Fatalf("status = %+v", st)
	}
	*now = now.Add(30 * time.Minute)
	if did, _ := f.Step(ctx); did {
		t.Error("it looked again within the hour after a failure")
	}
	*now = now.Add(31 * time.Minute)
	sink.refuse = nil
	if did, _ := f.Step(ctx); !did {
		t.Error("it did not look again after the hour")
	}
}

func TestChangingTheSetupLooksAtOnce(t *testing.T) {
	f, sink, _, now := feedFor(t)
	ctx := context.Background()
	_, _ = f.Step(ctx)
	if did, _ := f.Step(ctx); did {
		t.Fatal("read twice")
	}
	c := *sink.cfg
	c.OdometerUnit = Miles
	sink.cfg = &c
	if did, _ := f.Step(ctx); did {
		t.Error("a changed setup was looked at within a minute of the last look: Ford would say to slow down")
	}
	*now = now.Add(MinGap + time.Second)
	if did, _ := f.Step(ctx); !did {
		t.Error("a changed setup was not looked at once the gap had passed")
	}
}

func TestLinkingReadsOnceAndTheFeedDoesNotReadAgainRightAfter(t *testing.T) {
	f, sink, fs, now := feedFor(t)
	ctx := context.Background()
	sink.refresh = ""
	if did, _ := f.Step(ctx); did { // not linked: the feed notes that
		t.Fatal("an unlinked vehicle was read")
	}
	if _, err := f.Complete(ctx, "CODE"); err != nil {
		t.Fatal(err)
	}
	if fs.telemetry != 1 {
		t.Fatalf("linking read telemetry %d times, want 1", fs.telemetry)
	}
	for _, wait := range []time.Duration{0, 10 * time.Second, 40 * time.Second, 2 * time.Minute} {
		*now = now.Add(wait)
		if did, _ := f.Step(ctx); did {
			t.Fatalf("the feed read again %v after linking", wait)
		}
	}
	if fs.telemetry != 1 {
		t.Errorf("telemetry was asked for %d times after linking, want 1", fs.telemetry)
	}
}

func TestWithNoSetupOrNoLinkNothingIsAsked(t *testing.T) {
	f, sink, fs, _ := feedFor(t)
	ctx := context.Background()
	sink.refresh = ""
	if did, _ := f.Step(ctx); did {
		t.Error("an unlinked vehicle was read")
	}
	sink.cfg = nil
	if did, _ := f.Step(ctx); did {
		t.Error("with no setup something was read")
	}
	if _, err := f.Read(ctx); !errors.Is(err, ErrNotSetUp) {
		t.Errorf("Read with no setup = %v", err)
	}
	if fs.telemetry != 0 || len(sink.status) != 0 {
		t.Errorf("telemetry %d, statuses %d", fs.telemetry, len(sink.status))
	}
}

func TestNoLookIsMadeLessThan30SecondsAfterTheLastWhoeverAsksAndLinkingKeepsItsCode(t *testing.T) {
	f, sink, fs, now := feedFor(t)
	ctx := context.Background()

	if _, err := f.Read(ctx); err != nil {
		t.Fatalf("first read: %v", err)
	}
	if w := f.Wait(); w != MinCallGap {
		t.Errorf("wait right after a look = %v, want %v", w, MinCallGap)
	}
	*now = now.Add(10 * time.Second)
	var soon *TooSoon
	if _, err := f.Read(ctx); !errors.As(err, &soon) || soon.Wait != 20*time.Second {
		t.Fatalf("read 10 s later = %v, want to be told to wait 20 s", err)
	}
	if fs.telemetry != 1 || fs.issued != 1 {
		t.Errorf("Ford was asked during the wait: %d telemetry, %d tokens", fs.telemetry, fs.issued)
	}
	// Linking inside the wait is refused before the one-time code is traded, so the code is not used up.
	if _, err := f.Complete(ctx, "CODE"); !errors.As(err, &soon) {
		t.Errorf("linking 10 s after a look = %v, want to be told to wait", err)
	}
	if fs.issued != 1 {
		t.Errorf("the code was traded during the wait (%d tokens issued)", fs.issued)
	}
	// The failed tries do not push the time back: 30 s after the look it works again.
	*now = now.Add(20 * time.Second)
	if _, err := f.Read(ctx); err != nil {
		t.Errorf("read 30 s after a look: %v", err)
	}
	if fs.telemetry != 2 || len(sink.status) != 2 {
		t.Errorf("telemetry %d, statuses %d; want 2 and 2 (a refused look saves no outcome)", fs.telemetry, len(sink.status))
	}
}

// lookSink remembers when Ford was last asked, like the server does across a restart.
type lookSink struct {
	*fakeSink
	at time.Time
}

func (s *lookSink) LastLook(context.Context) time.Time { return s.at }

func TestALookMadeBeforeARestartStillCounts(t *testing.T) {
	f, sink, fs, now := feedFor(t)
	f.Sink = &lookSink{fakeSink: sink, at: now.Add(-10 * time.Second)}
	ctx := context.Background()

	var soon *TooSoon
	if _, err := f.Read(ctx); !errors.As(err, &soon) || soon.Wait != 20*time.Second {
		t.Fatalf("read after a restart 10 s after a look = %v, want to wait 20 s", err)
	}
	if did, _ := f.Step(ctx); did {
		t.Error("the feed started a look right after a restart inside the wait")
	}
	if fs.telemetry != 0 {
		t.Errorf("Ford was asked %d times", fs.telemetry)
	}
	*now = now.Add(MinGap)
	if did, err := f.Step(ctx); err != nil || !did {
		t.Errorf("step after the wait = %v, %v; want a read", did, err)
	}
}

func TestDescribeNamesEverythingButOnlyShowsTheBatteryAndOdometerValues(t *testing.T) {
	body := `{"metrics":{"odometer":{"value":93672,"updateTime":"2026-10-28T10:00:00Z"},"xevBatteryStateOfCharge":{"value":63.5},` +
		`"vin":"1FAKEVIN123","position":{"latitude":44.9,"longitude":-93.2},"ignition":"OFF","charging":true,"gone":null}}`
	got := map[string]Signal{}
	for _, s := range Describe([]byte(body)) {
		got[s.Path] = s
	}
	want := map[string]Signal{
		"metrics.odometer.value":                {Path: "metrics.odometer.value", Kind: "number", Value: "93672"},
		"metrics.odometer.updateTime":           {Path: "metrics.odometer.updateTime", Kind: "string", Value: "2026-10-28T10:00:00Z"},
		"metrics.xevBatteryStateOfCharge.value": {Path: "metrics.xevBatteryStateOfCharge.value", Kind: "number", Value: "63.5"},
		"metrics.vin":                           {Path: "metrics.vin", Kind: "string"},
		"metrics.position.latitude":             {Path: "metrics.position.latitude", Kind: "number"},
		"metrics.position.longitude":            {Path: "metrics.position.longitude", Kind: "number"},
		"metrics.ignition":                      {Path: "metrics.ignition", Kind: "string"},
		"metrics.charging":                      {Path: "metrics.charging", Kind: "boolean", Value: "true"},
		"metrics.gone":                          {Path: "metrics.gone", Kind: "null"},
	}
	if len(got) != len(want) {
		t.Errorf("got %d signals, want %d: %v", len(got), len(want), got)
	}
	for p, w := range want {
		if got[p] != w {
			t.Errorf("%s = %+v, want %+v", p, got[p], w)
		}
	}
	if Describe([]byte("nope")) != nil {
		t.Error("an answer that is not JSON described something")
	}
}

func TestAProbeAsksEveryEndpointOnceAndSaysWhatEachAnsweredWithoutCopyingWhoOrWhere(t *testing.T) {
	f, sink, fs, now := feedFor(t)
	fs.extra = map[string]fakeAnswer{
		"/fcon-query/v1/garage":                    {200, `{"vin":"1FAKEVIN123","nickName":"Mach","engineType":"BEV","modelYear":"2021","modelName":"Mustang Mach-E"}`},
		"/fcon-query/v1/electric/charge-schedules": {200, `{"chargeSchedules":[{"vin":"1FAKEVIN123","location":{"name":"Home","latitude":44.9,"type":"SAVED"},"chargeProfile":{"chargeMode":"VALUE_CHARGE","targetSoc":90}}]}`},
		"/fcon-query/v1/electric/departure-times":  {403, `{"error":403,"message":"insufficient_scope"}`},
		"/fcon-query/v1/vehicle-health/alerts":     {404, `{"message":"no alerts"}`},
		"/fcon-query/v1/wallbox":                   {200, `{"result":{"associatedChargingStationIds":["00000000-1111-2222-3333-444444444444"]}}`},
		"/fcon-query/v1/fccs":                      {200, `{"result":{"totalActivities":0,"activity":[]}}`},
	}
	p, err := f.Probe(context.Background())
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if got := sink.order; len(got) == 0 || got[0] != "save-refresh:R1" {
		t.Errorf("order = %v: the rotated refresh token must be kept before anything else is read", got)
	}
	by := map[string]ProbeResult{}
	for _, r := range p.Results {
		by[r.Endpoint] = r
	}
	if len(p.Results) != 7 || p.CheckedAt == nil {
		t.Fatalf("results = %+v", p.Results)
	}
	if r := by["electric/departure-times"]; r.OK || r.Status != 403 || r.Message != "insufficient_scope" {
		t.Errorf("a refused endpoint = %+v", r)
	}
	if r := by["vehicle-health/alerts"]; r.OK || r.Status != 404 || r.Message != "no alerts" {
		t.Errorf("an empty endpoint = %+v", r)
	}
	var kind, target, name, lat string
	for _, s := range by["garage"].Signals {
		if s.Path == "engineType" {
			kind = s.Value
		}
		if s.Path == "nickName" || s.Path == "vin" {
			name += s.Value
		}
	}
	for _, s := range by["electric/charge-schedules"].Signals {
		if s.Path == "chargeSchedules[0].chargeProfile.targetSoc" {
			target = s.Value
		}
		if s.Path == "chargeSchedules[0].location.latitude" || s.Path == "chargeSchedules[0].location.name" {
			lat += s.Value
		}
	}
	if kind != "BEV" || target != "90" || name != "" || lat != "" {
		t.Errorf("engine type %q, target %q (want BEV and 90); identity %q and place %q must not be copied", kind, target, name, lat)
	}
	// The charging-station call names the station and asks for the last week.
	if fs.fccs == nil || fs.fccs.Header.Get("chargingStationId") != "00000000-1111-2222-3333-444444444444" || fs.fccs.URL.Query().Get("endDate") != "2026-10-22" || fs.fccs.URL.Query().Get("startDate") != "2026-10-15" {
		t.Errorf("fccs request = %+v", fs.fccs)
	}
	if fs.telemetry != 1 {
		t.Errorf("telemetry asked %d times", fs.telemetry)
	}
	// Probing again inside the wait asks nothing; later it works.
	*now = now.Add(5 * time.Second)
	var soon *TooSoon
	if _, err := f.Probe(context.Background()); !errors.As(err, &soon) {
		t.Errorf("a second probe 5 s later = %v", err)
	}
	if fs.telemetry != 1 {
		t.Errorf("Ford was asked during the wait")
	}
}

func TestAProbeSkipsTheStationCallWhenTheWallboxListsNone(t *testing.T) {
	f, _, fs, _ := feedFor(t)
	fs.extra = map[string]fakeAnswer{"/fcon-query/v1/wallbox": {200, `{"result":{"associatedChargingStationIds":[]}}`}}
	p, err := f.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	last := p.Results[len(p.Results)-1]
	if last.Endpoint != "fccs" || last.Status != 0 || fs.fccs != nil {
		t.Errorf("fccs = %+v, asked %v", last, fs.fccs)
	}
}
