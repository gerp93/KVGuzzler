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
	order     *[]string
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
			_, _ = w.Write([]byte(`{"metrics":{"odometer":{"value":` + fs.odometer + `}}}`))
		default:
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
	f, sink, _, _ := feedFor(t)
	ctx := context.Background()
	_, _ = f.Step(ctx)
	if did, _ := f.Step(ctx); did {
		t.Fatal("read twice")
	}
	c := *sink.cfg
	c.OdometerUnit = Miles
	sink.cfg = &c
	if did, _ := f.Step(ctx); !did {
		t.Error("a changed setup was not looked at")
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
