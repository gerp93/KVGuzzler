package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/collector"
	"github.com/gerp93/KVGuzzler/server/internal/ford"
	"github.com/gerp93/KVGuzzler/server/internal/sim"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

const (
	fordClientID = "0f243454-a043-4230-bff1-406358da7f2d"
	fordSecret   = "client-secret-xyz"
)

// fakeFord stands in for Ford's sign-in and vehicle services: every refresh hands out the next refresh token, and a refresh token that is not the
// latest one handed out is refused (a replaced token is dead).
type fakeFord struct {
	*httptest.Server
	latest   string
	issued   int
	odometer string
	forms    []url.Values
}

func newFakeFord(t *testing.T) *fakeFord {
	t.Helper()
	f := &fakeFord{odometer: "20000"}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			b, _ := io.ReadAll(r.Body)
			form, _ := url.ParseQuery(string(b))
			f.forms = append(f.forms, form)
			if form.Get("client_secret") != fordSecret {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			switch form.Get("grant_type") {
			case "authorization_code":
				if form.Get("code") != "GOODCODE" {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
					return
				}
			case "refresh_token":
				if form.Get("refresh_token") != f.latest {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
					return
				}
			}
			f.issued++
			f.latest = "REFRESH-" + string(rune('0'+f.issued))
			_, _ = w.Write([]byte(`{"access_token":"ACCESS-` + string(rune('0'+f.issued)) + `","refresh_token":"` + f.latest + `"}`))
		case ford.TelemetryPath:
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ACCESS-") {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"metrics":{"odometer":{"value":` + f.odometer + `},"xevBatteryStateOfCharge":{"value":63,"updateTime":"2026-10-22T14:55:00Z"},"vin":"1FAKEVIN123","position":{"latitude":44.9}}}`))
		case "/fcon-query/v1/garage":
			_, _ = w.Write([]byte(`{"vin":"1FAKEVIN123","engineType":"BEV","modelYear":"2021"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

type fordTest struct {
	srv   *httptest.Server
	st    *store.Store
	ford  *fakeFord
	clock *time.Time
	meter string
}

func newFordTest(t *testing.T) *fordTest {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ff := newFakeFord(t)
	clock := time.Date(2026, 10, 22, 15, 0, 0, 0, time.UTC)
	c := collector.New(st, time.Second)
	// A circuit to mark as the car's charger.
	if err := c.Add(t.Context(), sim.NewAt(func() time.Time { return clock })); err != nil {
		t.Fatal(err)
	}
	api := &Server{Store: st, Collector: c, Loc: time.UTC, Version: "test", Now: func() time.Time { return clock }, Ford: &ford.Client{Base: ff.URL}}
	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)
	return &fordTest{srv: srv, st: st, ford: ff, clock: &clock, meter: "sim-em16p:ch-01"}
}

func (f *fordTest) setup(unit string) string {
	return `{"client_id":"` + fordClientID + `","client_secret":"` + fordSecret + `","authorize_url":"https://login.example.com/authorize?client_id=` + fordClientID +
		`&redirect_uri=http%3A%2F%2Flocalhost%3A8000%2Fcallback&state=placeholder","token_url":"` + f.ford.URL + `/token","odometer_unit":"` + unit + `","meter_id":"` + f.meter + `"}`
}

func TestLinkingAVehicleReadsItsOdometerIntoTheChargingMeterAndKeepsTheSecretsPrivate(t *testing.T) {
	f := newFordTest(t)
	base := f.srv.URL + "/api/v1/ford"
	evURL := f.srv.URL + "/api/v1/meters/" + f.meter + "/ev"

	// Nothing set up: an empty view, and nothing to link.
	var v struct {
		Configured bool    `json:"configured"`
		Linked     bool    `json:"linked"`
		NeedsLink  bool    `json:"needs_link"`
		ClientID   string  `json:"client_id"`
		LastMiles  float64 `json:"last_miles"`
		Outcome    *string `json:"last_outcome"`
	}
	if get(t, base, &v); v.Configured {
		t.Fatalf("view before setup = %+v", v)
	}
	if r, _ := send(t, http.MethodPost, base+"/link", ""); r.StatusCode != http.StatusConflict {
		t.Errorf("link before setup: %d, want 409", r.StatusCode)
	}
	if r, _ := send(t, http.MethodPost, base+"/read", ""); r.StatusCode != http.StatusConflict {
		t.Errorf("read before setup: %d, want 409", r.StatusCode)
	}

	// The meter must be one that charges a vehicle.
	if r, _ := send(t, http.MethodPut, base, f.setup("km")); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("setup on a meter not marked as charging: %d, want 400", r.StatusCode)
	}
	if r := patch(t, f.srv.URL+"/api/v1/meters/"+f.meter, `{"ev":true}`); r.StatusCode != http.StatusOK {
		t.Fatalf("mark: %d", r.StatusCode)
	}
	if r, body := send(t, http.MethodPut, base, strings.Replace(f.setup("km"), `"odometer_unit":"km"`, `"odometer_unit":""`, 1)); r.StatusCode != http.StatusBadRequest || !strings.Contains(body, "kilometers or miles") {
		t.Errorf("setup with no unit: %d %s", r.StatusCode, body)
	}
	r, body := send(t, http.MethodPut, base, f.setup("km"))
	if r.StatusCode != http.StatusOK {
		t.Fatalf("setup: %d %s", r.StatusCode, body)
	}
	if strings.Contains(body, fordSecret) {
		t.Errorf("the setup's answer holds the client secret: %s", body)
	}
	_ = json.Unmarshal([]byte(body), &v)
	if !v.Configured || v.Linked || v.ClientID != fordClientID {
		t.Errorf("view after setup = %+v", v)
	}
	// Linked is not true until the owner agrees: reading now says so.
	if r, _ := send(t, http.MethodPost, base+"/read", ""); r.StatusCode != http.StatusOK {
		t.Fatalf("read when not linked: %d", r.StatusCode)
	}
	get(t, base, &v)
	if !v.NeedsLink || v.Outcome == nil || !strings.Contains(*v.Outcome, "not linked") {
		t.Errorf("unlinked read left %+v / %v", v, v.Outcome)
	}

	// Start linking: the address to open carries a fresh state and the registered redirect.
	r, body = send(t, http.MethodPost, base+"/link", "")
	var link struct {
		URL         string `json:"url"`
		RedirectURI string `json:"redirect_uri"`
	}
	_ = json.Unmarshal([]byte(body), &link)
	u, _ := url.Parse(link.URL)
	state := u.Query().Get("state")
	if r.StatusCode != http.StatusOK || len(state) < 32 || state == "placeholder" || link.RedirectURI != "http://localhost:8000/callback" {
		t.Fatalf("link = %d %s", r.StatusCode, body)
	}

	// A stray address (wrong state) links nothing; the right one does.
	if r, _ := send(t, http.MethodPost, base+"/link/complete", `{"redirect":"http://localhost:8000/callback?code=GOODCODE&state=forged"}`); r.StatusCode != http.StatusBadRequest {
		t.Errorf("wrong state: %d, want 400", r.StatusCode)
	}
	if r, _ := send(t, http.MethodPost, base+"/link/complete", `{"redirect":"http://localhost:8000/callback?code=BADCODE&state=`+state+`"}`); r.StatusCode != http.StatusBadGateway {
		t.Errorf("a code Ford refuses: %d, want 502", r.StatusCode)
	}
	// The attempt is over once a code was tried (a code is single-use): start again, once Ford may be asked again.
	*f.clock = f.clock.Add(time.Minute)
	_, body = send(t, http.MethodPost, base+"/link", "")
	_ = json.Unmarshal([]byte(body), &link)
	u, _ = url.Parse(link.URL)
	state = u.Query().Get("state")
	r, body = send(t, http.MethodPost, base+"/link/complete", `{"redirect":"http://localhost:8000/callback?code=GOODCODE&state=`+state+`"}`)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("complete: %d %s", r.StatusCode, body)
	}
	for _, secret := range []string{fordSecret, "REFRESH-", "ACCESS-"} {
		if strings.Contains(body, secret) {
			t.Errorf("the answer holds %q: %s", secret, body)
		}
	}
	var done struct {
		Linked    bool     `json:"linked"`
		LastMiles *float64 `json:"last_miles"`
		LastRaw   *float64 `json:"last_raw"`
		LastDay   *string  `json:"last_day"`
	}
	_ = json.Unmarshal([]byte(body), &done)
	if !done.Linked || done.LastMiles == nil || *done.LastMiles != 12427.4 || *done.LastRaw != 20000 || *done.LastDay != "2026-10-22" {
		t.Errorf("after linking: %s", body)
	}
	var ev evBody
	get(t, evURL, &ev)
	if len(ev.Readings) != 1 || ev.Readings[0].Day != "2026-10-22" || ev.Readings[0].Miles != 12427.4 {
		t.Fatalf("readings = %+v, want the odometer in miles", ev.Readings)
	}
	// The code exchange used the client's own credentials and the registered redirect.
	form := f.ford.forms[len(f.ford.forms)-1]
	if form.Get("grant_type") != "authorization_code" || form.Get("redirect_uri") != "http://localhost:8000/callback" || form.Get("scope") != fordClientID+" offline_access openid" {
		t.Errorf("code exchange = %v", form)
	}
	// The secrets are in the private file, not the database.
	if b, err := os.ReadFile(filepath.Join(f.st.DataDir(), "secrets.json")); err != nil || !strings.Contains(string(b), fordSecret) || !strings.Contains(string(b), "REFRESH-") {
		t.Errorf("the secrets file (%v) does not hold the client secret and the refresh token", err)
	}
	if b, err := os.ReadFile(filepath.Join(f.st.DataDir(), "h.db")); err != nil || bytes.Contains(b, []byte(fordSecret)) || bytes.Contains(b, []byte("REFRESH-")) {
		t.Errorf("the database file holds a secret (%v)", err)
	}

	// The next day the odometer moved: a second reading, and the refresh token was replaced and the new one used.
	*f.clock = f.clock.Add(24 * time.Hour)
	f.ford.odometer = "20100"
	if r, body := send(t, http.MethodPost, base+"/read", ""); r.StatusCode != http.StatusOK {
		t.Fatalf("read: %d %s", r.StatusCode, body)
	}
	get(t, evURL, &ev)
	if want, _ := (ford.Odometer{Value: 20100}).Miles(ford.Kilometers); len(ev.Readings) != 2 || ev.Readings[1].Day != "2026-10-23" || ev.Readings[1].Miles != want {
		t.Errorf("readings = %+v", ev.Readings)
	}

	// A parked car adds no reading.
	*f.clock = f.clock.Add(24 * time.Hour)
	send(t, http.MethodPost, base+"/read", "")
	get(t, evURL, &ev)
	if len(ev.Readings) != 2 {
		t.Errorf("an unmoved odometer added a reading: %+v", ev.Readings)
	}

	// An odometer lower than one saved is said, not saved.
	*f.clock = f.clock.Add(time.Minute)
	f.ford.odometer = "100"
	send(t, http.MethodPost, base+"/read", "")
	get(t, base, &v)
	if v.Outcome == nil || !strings.Contains(*v.Outcome, "only goes up") {
		t.Errorf("a lower odometer: %v", v.Outcome)
	}
	get(t, evURL, &ev)
	if len(ev.Readings) != 2 {
		t.Errorf("a lower odometer was saved: %+v", ev.Readings)
	}

	// Ford revoking the permission asks for linking again and stops trying.
	*f.clock = f.clock.Add(time.Minute)
	f.ford.latest = "SOMETHING-ELSE"
	send(t, http.MethodPost, base+"/read", "")
	get(t, base, &v)
	if v.Linked || !v.NeedsLink {
		t.Errorf("after a revoked permission: %+v", v)
	}

	// Removing it keeps the readings.
	if r := del(t, base); r.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", r.StatusCode)
	}
	get(t, base, &v)
	if v.Configured {
		t.Errorf("still configured after removing: %+v", v)
	}
	get(t, evURL, &ev)
	if len(ev.Readings) != 2 {
		t.Errorf("removing the link removed readings: %+v", ev.Readings)
	}
}

func TestChangingTheClientDropsTheSavedPermissionButChangingTheUnitKeepsIt(t *testing.T) {
	f := newFordTest(t)
	base := f.srv.URL + "/api/v1/ford"
	patch(t, f.srv.URL+"/api/v1/meters/"+f.meter, `{"ev":true}`)
	send(t, http.MethodPut, base, f.setup("km"))
	_, body := send(t, http.MethodPost, base+"/link", "")
	var link struct{ URL string }
	_ = json.Unmarshal([]byte(body), &link)
	u, _ := url.Parse(link.URL)
	if r, b := send(t, http.MethodPost, base+"/link/complete", `{"redirect":"?code=GOODCODE&state=`+u.Query().Get("state")+`"}`); r.StatusCode != http.StatusOK {
		t.Fatalf("complete: %d %s", r.StatusCode, b)
	}

	// The secret may be left out to keep the saved one; the unit changes without losing the link.
	miles := strings.Replace(f.setup("miles"), `"client_secret":"`+fordSecret+`",`, "", 1)
	var v struct{ Linked bool }
	if r, b := send(t, http.MethodPut, base, miles); r.StatusCode != http.StatusOK {
		t.Fatalf("unit change: %d %s", r.StatusCode, b)
	} else {
		_ = json.Unmarshal([]byte(b), &v)
	}
	if !v.Linked {
		t.Error("changing the unit lost the link")
	}
	// A different client ID is a different permission.
	other := strings.ReplaceAll(miles, fordClientID, "another-client-id")
	if r, b := send(t, http.MethodPut, base, other); r.StatusCode != http.StatusOK {
		t.Fatalf("client change: %d %s", r.StatusCode, b)
	} else {
		_ = json.Unmarshal([]byte(b), &v)
	}
	if v.Linked {
		t.Error("a new client ID kept the old permission")
	}
}

func TestFordIsNeverAskedTwiceWithin30SecondsAndTheAnswerShowsWhatItHeld(t *testing.T) {
	f := newFordTest(t)
	base := f.srv.URL + "/api/v1/ford"
	patch(t, f.srv.URL+"/api/v1/meters/"+f.meter, `{"ev":true}`)
	send(t, http.MethodPut, base, f.setup("km"))
	_, body := send(t, http.MethodPost, base+"/link", "")
	var link struct{ URL string }
	_ = json.Unmarshal([]byte(body), &link)
	u, _ := url.Parse(link.URL)
	if r, b := send(t, http.MethodPost, base+"/link/complete", `{"redirect":"?code=GOODCODE&state=`+u.Query().Get("state")+`"}`); r.StatusCode != http.StatusOK {
		t.Fatalf("complete: %d %s", r.StatusCode, b)
	}
	asked := len(f.ford.forms)

	// "Read now" right after linking is refused without asking Ford, and the setup says how long is left.
	*f.clock = f.clock.Add(10 * time.Second)
	r, body := send(t, http.MethodPost, base+"/read", "")
	if r.StatusCode != http.StatusTooManyRequests || r.Header.Get("Retry-After") != "20" || !strings.Contains(body, "try again in 20") {
		t.Errorf("read 10 s after a look: %d (Retry-After %q) %s", r.StatusCode, r.Header.Get("Retry-After"), body)
	}
	if len(f.ford.forms) != asked {
		t.Errorf("Ford was asked during the wait: %d requests, want %d", len(f.ford.forms), asked)
	}
	var v struct {
		Wait    int `json:"read_wait_seconds"`
		Signals []struct {
			Path  string `json:"path"`
			Kind  string `json:"kind"`
			Value string `json:"value"`
		} `json:"last_signals"`
	}
	_, body = send(t, http.MethodGet, base, "")
	_ = json.Unmarshal([]byte(body), &v)
	if v.Wait != 20 {
		t.Errorf("read_wait_seconds = %d, want 20", v.Wait)
	}
	// What the answer held is shown: the battery with its value, everything else named only.
	var battery, vin bool
	for _, s := range v.Signals {
		if s.Path == "metrics.xevBatteryStateOfCharge.value" && s.Kind == "number" && s.Value == "63" {
			battery = true
		}
		if s.Path == "metrics.vin" && s.Kind == "string" && s.Value == "" {
			vin = true
		}
	}
	if !battery || !vin || strings.Contains(body, "1FAKEVIN123") || strings.Contains(body, "44.9") {
		t.Errorf("signals = %+v (the battery should have its value; the vehicle's identity and position must not): %s", v.Signals, body)
	}

	// Past the 30 seconds it goes through.
	*f.clock = f.clock.Add(21 * time.Second)
	if r, body := send(t, http.MethodPost, base+"/read", ""); r.StatusCode != http.StatusOK {
		t.Errorf("read 31 s after a look: %d %s", r.StatusCode, body)
	}
}

func TestProbingFordShowsWhatEachEndpointSaidAndKeepsTheLastOne(t *testing.T) {
	f := newFordTest(t)
	base := f.srv.URL + "/api/v1/ford"
	patch(t, f.srv.URL+"/api/v1/meters/"+f.meter, `{"ev":true}`)
	send(t, http.MethodPut, base, f.setup("km"))

	// Nothing kept before a probe is made.
	var last struct {
		CheckedAt *string `json:"checked_at"`
		Results   []struct {
			Endpoint string `json:"endpoint"`
			Status   int    `json:"status"`
			OK       bool   `json:"ok"`
			Message  string `json:"message"`
			Signals  []struct {
				Path  string `json:"path"`
				Value string `json:"value"`
			} `json:"signals"`
		} `json:"results"`
	}
	_, body := send(t, http.MethodGet, base+"/probe", "")
	_ = json.Unmarshal([]byte(body), &last)
	if last.CheckedAt != nil || last.Results == nil || len(last.Results) != 0 {
		t.Errorf("probe before any = %s", body)
	}

	_, body = send(t, http.MethodPost, base+"/link", "")
	var link struct{ URL string }
	_ = json.Unmarshal([]byte(body), &link)
	u, _ := url.Parse(link.URL)
	if r, b := send(t, http.MethodPost, base+"/link/complete", `{"redirect":"?code=GOODCODE&state=`+u.Query().Get("state")+`"}`); r.StatusCode != http.StatusOK {
		t.Fatalf("complete: %d %s", r.StatusCode, b)
	}
	// Too soon after linking: refused without asking Ford.
	*f.clock = f.clock.Add(5 * time.Second)
	if r, _ := send(t, http.MethodPost, base+"/probe", ""); r.StatusCode != http.StatusTooManyRequests {
		t.Errorf("probe 5 s after a look: %d, want 429", r.StatusCode)
	}

	*f.clock = f.clock.Add(time.Minute)
	r, body := send(t, http.MethodPost, base+"/probe", "")
	if r.StatusCode != http.StatusOK {
		t.Fatalf("probe: %d %s", r.StatusCode, body)
	}
	for _, secret := range []string{fordSecret, "REFRESH-", "ACCESS-", "1FAKEVIN123"} {
		if strings.Contains(body, secret) {
			t.Errorf("the probe holds %q: %s", secret, body)
		}
	}
	_ = json.Unmarshal([]byte(body), &last)
	by := map[string]int{}
	var engine string
	for _, res := range last.Results {
		by[res.Endpoint] = res.Status
		for _, s := range res.Signals {
			if res.Endpoint == "garage" && s.Path == "engineType" {
				engine = s.Value
			}
		}
	}
	if len(last.Results) != 7 || by["garage"] != 200 || by["telemetry"] != 200 || by["wallbox"] != 404 || by["fccs"] != 0 || engine != "BEV" {
		t.Errorf("results = %s", body)
	}
	// The last probe is kept, and removing the setup forgets it.
	_, kept := send(t, http.MethodGet, base+"/probe", "")
	if !strings.Contains(kept, `"engineType"`) {
		t.Errorf("the last probe was not kept: %s", kept)
	}
	if r := del(t, base); r.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", r.StatusCode)
	}
	_, after := send(t, http.MethodGet, base+"/probe", "")
	if strings.Contains(after, "engineType") {
		t.Errorf("a removed setup left its probe: %s", after)
	}
}
