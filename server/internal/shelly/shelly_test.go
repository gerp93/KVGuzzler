package shelly

// Ported from tests/KVGuzzler.Tests/WallMeterTests.cs (ShellyPlugTests): the same answers, including the one captured from a
// real Shelly Plug US Gen4 (firmware 1.7.99), and the same refusals.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/model"
)

const answer = `{"id":0,"source":"init","output":true,"apower":52.1,"voltage":121.3,"current":0.43,"aenergy":{"total":1234.5,"by_minute":[0,0,0],"minute_ts":1792341000}}`

// Captured from a real Shelly Plug US Gen4 with nothing plugged into it: spacing and all.
const real = `{"id":0, "source":"init", "output":false, "apower":0.0, "voltage":122.0, "freq":60.1, "current":0.000, "aenergy":{"total":0.000,"by_minute":[0.000,0.000,0.000],"minute_ts":1792369140}, "ret_aenergy":{"total":0.000,"by_minute":[0.000,0.000,0.000],"minute_ts":1792369140},"temperature":{"tC":35.0, "tF":95.0}}`

func TestParsesWattsAndTheCounter(t *testing.T) {
	st, err := ParseStatus([]byte(answer))
	if err != nil {
		t.Fatal(err)
	}
	if st.Watts != 52.1 || st.EnergyWh == nil || *st.EnergyWh != 1234.5 || st.Volts == nil || *st.Volts != 121.3 || st.Amps == nil || *st.Amps != 0.43 {
		t.Errorf("status = %+v", st)
	}
}

func TestParsesTheRealGen4Answer(t *testing.T) {
	st, err := ParseStatus([]byte(real))
	if err != nil || st.Watts != 0 {
		t.Fatalf("%+v, %v", st, err)
	}
	st, err = ParseStatus([]byte(strings.Replace(real, `"apower":0.0`, `"apower":187.4`, 1)))
	if err != nil || st.Watts != 187.4 {
		t.Fatalf("%+v, %v", st, err)
	}
}

func TestAnythingElseIsReportedAsNotAShelly(t *testing.T) {
	for _, body := range []string{`{"apower":"52.1"}`, `{"apower":-3}`, `{"apower":null}`, `{}`, `[]`, `not json`, ``, `{"apower":1e999}`} {
		if _, err := ParseStatus([]byte(body)); !errors.Is(err, ErrNotAShelly) {
			t.Errorf("ParseStatus(%q) err = %v, want ErrNotAShelly", body, err)
		}
	}
}

func TestABadCounterIsNilButThePowerIsStillGood(t *testing.T) {
	for _, body := range []string{
		`{"apower":10}`, `{"apower":10,"aenergy":{}}`, `{"apower":10,"aenergy":{"total":"x"}}`, `{"apower":10,"aenergy":{"total":-5}}`,
	} {
		st, err := ParseStatus([]byte(body))
		if err != nil || st.Watts != 10 || st.EnergyWh != nil {
			t.Errorf("%s: %+v, %v", body, st, err)
		}
	}
}

func serve(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return newWithBase(srv.URL, "192.168.1.50", srv.Client())
}

func TestReadsOverAPlainGet(t *testing.T) {
	var method, path, query string
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		method, path, query = r.Method, r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte(answer))
	})
	st, err := c.Status(context.Background())
	if err != nil || st.Watts != 52.1 {
		t.Fatalf("%+v, %v", st, err)
	}
	if method != http.MethodGet || path != "/rpc/Switch.GetStatus" || query != "id=0" {
		t.Errorf("request was %s %s?%s", method, path, query)
	}
}

func TestStatusCodesAreReportedInWords(t *testing.T) {
	for code, want := range map[int]string{401: "password", 404: "Shelly plug", 500: "error (500)"} {
		code := code
		c := serve(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) })
		_, err := c.Status(context.Background())
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%d: err = %v, want one containing %q", code, err, want)
		}
	}
}

func TestAHangingPlugTimesOut(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-block }))
	t.Cleanup(func() { close(block); srv.Close() })
	c := newWithBase(srv.URL, "192.168.1.50", &http.Client{Timeout: 100 * time.Millisecond})

	_, err := c.Status(context.Background())
	if err == nil || !strings.Contains(err.Error(), "did not answer") {
		t.Errorf("err = %v", err)
	}
}

func TestACancelledReadIsNotReportedAsATimeout(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-block }))
	t.Cleanup(func() { close(block); srv.Close() })
	c := newWithBase(srv.URL, "192.168.1.50", srv.Client())

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	_, err := c.Status(ctx)
	if err == nil || strings.Contains(err.Error(), "did not answer") {
		t.Errorf("err = %v, want a plain cancellation", err)
	}
}

func TestOnlyAPlainHostIsAccepted(t *testing.T) {
	for host, ok := range map[string]bool{
		"192.168.1.50": true, "shelly-plug.lan": true,
		"192.168.1.50/rpc/Shelly.Reboot": false, // a path: only the plug's own address is ever asked
		"http://192.168.1.50":            false, "192.168.1.50:80": false, "": false,
	} {
		_, err := New(host, nil)
		if (err == nil) != ok {
			t.Errorf("New(%q) err = %v, want ok=%v", host, err, ok)
		}
	}
}

func TestSourceIsOneMeterOfTheRightScope(t *testing.T) {
	var hits atomic.Int32
	c := serve(t, func(w http.ResponseWriter, _ *http.Request) { hits.Add(1); _, _ = w.Write([]byte(answer)) })

	src := newSource(c, "192.168.1.50", "Desktop PC", model.ScopePC)
	if d := src.Device(); d.ID != "shelly-192.168.1.50" || d.Kind != "shelly_plug" || d.Host != "192.168.1.50" {
		t.Errorf("device = %+v", d)
	}
	chs := src.Meters()
	if len(chs) != 1 || chs[0].Scope != model.ScopePC || chs[0].Label != "Desktop PC" || strings.Contains(chs[0].ID, "/") || *chs[0].MaxWatts != MaxWatts {
		t.Errorf("meters = %+v", chs)
	}
	rs, err := src.Poll(context.Background())
	if err != nil || len(rs) != 1 || rs[0].MeterID != chs[0].ID || rs[0].Watts != 52.1 {
		t.Fatalf("readings = %+v, %v", rs, err)
	}
	if hits.Load() != 1 {
		t.Errorf("%d requests for one poll", hits.Load())
	}
}

func TestDeviceIDIsSafeForAUrlSegment(t *testing.T) {
	for host, want := range map[string]string{"192.168.1.50": "shelly-192.168.1.50", "Plug.LAN": "shelly-plug.lan", "fe80::1": "shelly-fe80-1"} {
		if got := DeviceID(host); got != want {
			t.Errorf("DeviceID(%q) = %q, want %q", host, got, want)
		}
	}
}

func TestNameDefaultsToTheHost(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, _ *http.Request) {})
	if got := newSource(c, "10.0.0.7", "", model.ScopePlug).Meters()[0].Label; got != "10.0.0.7" {
		t.Errorf("label = %q", got)
	}
}
