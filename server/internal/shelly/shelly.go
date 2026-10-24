// Package shelly reads a Shelly smart plug (second generation and newer, including the Plug US Gen4) over the local
// network. It is a port of KVGuzzler.Core's ShellyPlug and Source.
//
// One plain GET, /rpc/Switch.GetStatus?id=0, answers with the power now (apower) and the plug's own energy counter
// (aenergy.total). Nothing here can switch the plug: there is exactly one request and it is a read. A plug often has the PC
// or the fridge plugged into it.
package shelly

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/hostcheck"
	"github.com/gerp93/KVGuzzler/server/internal/model"
)

const (
	// MaxWatts is the most a plug can carry; a reading above it is nonsense and is dropped (the desktop app's MaxMeterWatts).
	MaxWatts = 4000.0

	statusPath = "/rpc/Switch.GetStatus?id=0"
	maxBody    = 1 << 20
	timeout    = 3 * time.Second
)

// Status is one Switch.GetStatus answer.
type Status struct {
	Watts float64
	// EnergyWh is the plug's running total in watt-hours, which it keeps counting while whatever is plugged in is off.
	// It starts again from zero when the plug restarts. Nil when the answer has no usable counter.
	EnergyWh *float64
	Volts    *float64
	Amps     *float64
}

// ErrNotAShelly means the device answered but not with a power reading.
var ErrNotAShelly = errors.New("the device answered, but not with a power reading. Is this address the Shelly plug?")

// ParseStatus reads a Switch.GetStatus answer. A missing, non-numeric or negative counter, voltage or current is simply nil:
// the power reading is still good.
func ParseStatus(body []byte) (Status, error) {
	// Decoded into generic values so a JSON string such as "52.1" is plainly a string and never passes for a number.
	var raw map[string]any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return Status{}, ErrNotAShelly
	}
	watts, ok := number(raw["apower"])
	if !ok || watts < 0 {
		return Status{}, ErrNotAShelly
	}
	st := Status{Watts: watts}
	if aenergy, ok := raw["aenergy"].(map[string]any); ok {
		if wh, ok := number(aenergy["total"]); ok && wh >= 0 {
			st.EnergyWh = &wh
		}
	}
	if v, ok := number(raw["voltage"]); ok && v >= 0 {
		st.Volts = &v
	}
	if a, ok := number(raw["current"]); ok && a >= 0 {
		st.Amps = &a
	}
	return st, nil
}

// number is v as a finite float64 when it is a JSON number, and not when it is anything else (a string, null, a missing key).
func number(v any) (float64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	f, err := n.Float64()
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

type Client struct {
	base string
	http *http.Client
	host string
}

// New returns a client for the plug at host, which must be a plain host name or IP address.
func New(host string, hc *http.Client) (*Client, error) {
	if err := hostcheck.Validate(host); err != nil {
		return nil, fmt.Errorf("shelly host %q: %w", host, err)
	}
	return newWithBase("http://"+host, host, hc), nil
}

func newWithBase(base, host string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: timeout}
	}
	return &Client{base: base, host: host, http: hc}
}

// Status asks the plug for its status. A plain GET: nothing here can switch the plug.
func (c *Client) Status(ctx context.Context) (Status, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+statusPath, nil)
	if err != nil {
		return Status{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() == nil && isTimeout(err) {
			return Status{}, fmt.Errorf("%s did not answer within %.0f seconds. Is the plug on and on the same network?", c.host, timeout.Seconds())
		}
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // the request's own URL is noise here: the host is already named
		}
		return Status{}, fmt.Errorf("could not reach %s: %w", c.host, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return Status{}, errors.New("the plug asks for a password. In the Shelly app, turn off authentication for this plug.")
	case resp.StatusCode == http.StatusNotFound:
		return Status{}, errors.New("that device does not answer like a Shelly plug (second generation or newer).")
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return Status{}, fmt.Errorf("the plug answered with an error (%d).", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return Status{}, fmt.Errorf("could not read the plug's answer: %w", err)
	}
	return ParseStatus(body)
}

func isTimeout(err error) bool {
	var t interface{ Timeout() bool }
	return errors.As(err, &t) && t.Timeout()
}

// Source is a plug as the collector's source: one meter, the plug itself.
type Source struct {
	device model.Device
	meter  model.Meter
	client *Client
}

var idUnsafe = regexp.MustCompile(`[^a-z0-9.-]+`)

// DeviceID is the stable id of the plug at host (it has no slash, since ids are URL path segments).
func DeviceID(host string) string {
	return "shelly-" + idUnsafe.ReplaceAllString(strings.ToLower(host), "-")
}

// NewSource makes a source for the plug at host. scope is model.ScopePlug for an appliance, or model.ScopePC for the PC's
// own wall plug (a slice of a circuit, never added to anything). name labels the meter; empty means the host.
func NewSource(host, name string, scope model.Scope, hc *http.Client) (*Source, error) {
	c, err := New(host, hc)
	if err != nil {
		return nil, err
	}
	return newSource(c, host, name, scope), nil
}

func newSource(c *Client, host, name string, scope model.Scope) *Source {
	if name == "" {
		name = host
	}
	id := DeviceID(host)
	max := MaxWatts
	return &Source{
		device: model.Device{ID: id, Kind: "shelly_plug", Host: host, Name: name, Model: "shelly"},
		meter:  model.Meter{ID: id + ":plug", DeviceID: id, Scope: scope, Label: name, Enabled: true, MaxWatts: &max},
		client: c,
	}
}

func (s *Source) Device() model.Device  { return s.device }
func (s *Source) Meters() []model.Meter { return []model.Meter{s.meter} }

func (s *Source) Poll(ctx context.Context) ([]model.Reading, error) {
	st, err := s.client.Status(ctx)
	if err != nil {
		return nil, err
	}
	return []model.Reading{{MeterID: s.meter.ID, Watts: st.Watts, Volts: st.Volts, Amps: st.Amps}}, nil
}

// NewSourceWithBase is NewSource for an address that is not a plain host (a test server with a port). Production code uses
// NewSource, which only accepts a plain host or IP.
func NewSourceWithBase(base, host, name string, scope model.Scope, hc *http.Client) *Source {
	return newSource(newWithBase(base, host, hc), host, name, scope)
}
