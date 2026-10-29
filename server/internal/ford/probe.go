package ford

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"
)

// Endpoint is one read-only call of FordConnect Query that the owner can look at.
type Endpoint struct {
	Name string
	Path string
}

// Endpoints are the calls a probe makes, every one a plain GET with no parameters. Each has its own limit at Ford (one request per 30 seconds), so
// they do not use up each other's. The charging-station activity call is made too, once the wallbox names a station (see Probe).
var Endpoints = []Endpoint{
	{"telemetry", TelemetryPath},
	{"garage", "/fcon-query/v1/garage"},
	{"vehicle-health/alerts", "/fcon-query/v1/vehicle-health/alerts"},
	{"electric/charge-schedules", "/fcon-query/v1/electric/charge-schedules"},
	{"electric/departure-times", "/fcon-query/v1/electric/departure-times"},
	{"wallbox", "/fcon-query/v1/wallbox"},
}

const fccsPath = "/fcon-query/v1/fccs"

// probeKept says which members of a probe's answers have their value shown: what is about the vehicle's kind, the battery, charging, schedules and
// alerts. Names, the vehicle's identity and where it is are never copied (the allow-list does not match them).
var probeKept = regexp.MustCompile(`(?i)battery|charg|plug|range|energy|soc|odometer|engineType|modelYear|modelName|\.make$|vehicleType|enabled|price|providerName|days|alert|severity|\.type$|\.status$|totalActivities|authorization|tcu|event`)

// ProbeResult is what one endpoint said.
type ProbeResult struct {
	Endpoint string `json:"endpoint"`
	// Status is the HTTP status; 0 when the call was not made or Ford was not reached (Message says why).
	Status  int      `json:"status"`
	OK      bool     `json:"ok"`
	Message string   `json:"message,omitempty"`
	Signals []Signal `json:"signals,omitempty"`
}

// Probe is what a look at every endpoint said.
type Probe struct {
	CheckedAt *time.Time    `json:"checked_at"`
	Results   []ProbeResult `json:"results"`
}

// Fetch is one GET of a FordConnect Query path. It answers the status and the body whatever the status is (the probe shows what Ford said); an
// error is only for not reaching Ford at all. Only the paths Ford documents are called, with the access token that can only go to Ford.
func (c *Client) Fetch(ctx context.Context, access, path string, query url.Values, header map[string]string) (int, []byte, error) {
	target := c.base() + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("Accept", "application/json")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := c.http().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil, ctx.Err()
		}
		return 0, nil, &Error{Message: "Could not reach Ford's vehicle service."}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return resp.StatusCode, body, nil
}

// said is what Ford's complaint comes to in words: its own message when it sent one, else a plain sentence for the usual statuses, else the start
// of what it sent.
func said(status int, body []byte) string {
	var e struct {
		Message string `json:"message"`
		Error   string `json:"errorMessage"`
	}
	if json.Unmarshal(body, &e) == nil {
		if e.Message != "" {
			return clip(e.Message)
		}
		if e.Error != "" {
			return clip(e.Error)
		}
	}
	switch status {
	case http.StatusTooManyRequests:
		return "Ford asked to slow down."
	case http.StatusForbidden:
		return "Ford says this link has no access to it."
	case http.StatusNotFound:
		return "Ford has nothing there for this vehicle."
	}
	return clip(string(body))
}

// stationID is the first charging station the wallbox answer lists, or "".
func stationID(body []byte) string {
	var w struct {
		Result struct {
			IDs []string `json:"associatedChargingStationIds"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &w) != nil || len(w.Result.IDs) == 0 {
		return ""
	}
	return w.Result.IDs[0]
}

func result(name string, status int, body []byte) ProbeResult {
	r := ProbeResult{Endpoint: name, Status: status, OK: status == http.StatusOK}
	if r.OK {
		r.Signals = describe(body, probeKept)
		if len(r.Signals) == 0 {
			r.Message = "Ford answered with nothing in it."
		}
		return r
	}
	r.Message = said(status, body)
	return r
}

// call makes one GET and turns what came back into a result; an error is only returned for something that is not Ford's (a cancelled request).
func (f *Feed) call(ctx context.Context, access, name, path string, query url.Values, header map[string]string) (ProbeResult, []byte, error) {
	f.stamp(f.now())
	status, body, err := f.Client.Fetch(ctx, access, path, query, header)
	if err != nil {
		var fe *Error
		if errors.As(err, &fe) {
			return ProbeResult{Endpoint: name, Message: fe.Message}, nil, nil
		}
		return ProbeResult{}, nil, err
	}
	return result(name, status, body), body, nil
}

// Probe asks every endpoint once and says what each answered, so the owner (and whoever builds on it) can see what this link can read. It saves
// nothing but the new refresh token, is refused inside the 30-second wait like any look, and calls each endpoint once.
func (f *Feed) Probe(ctx context.Context) (Probe, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cfg, err := f.Sink.Config(ctx)
	if err != nil {
		return Probe{}, err
	}
	if cfg == nil {
		return Probe{}, ErrNotSetUp
	}
	if err := f.guard(ctx); err != nil {
		return Probe{}, err
	}
	refresh, ok, err := f.Sink.Refresh(ctx)
	if err != nil {
		return Probe{}, err
	}
	if !ok {
		return Probe{}, &Error{Message: "The vehicle is not linked yet.", Relink: true}
	}
	f.stamp(f.now())
	tok, err := f.Client.Refresh(ctx, *cfg, refresh)
	if err != nil {
		if _, ferr := f.fail(ctx, err); ferr != nil {
			return Probe{}, ferr
		}
		return Probe{}, err
	}
	// The new refresh token is saved before anything else is asked of Ford: the old one no longer works.
	if tok.Refresh != "" && tok.Refresh != refresh {
		if err := f.Sink.SaveRefresh(ctx, tok.Refresh); err != nil {
			return Probe{}, err
		}
	}
	out := Probe{Results: []ProbeResult{}}
	var station string
	for _, e := range Endpoints {
		r, body, err := f.call(ctx, tok.Access, e.Name, e.Path, nil, nil)
		if err != nil {
			return Probe{}, err
		}
		if e.Name == "wallbox" && r.OK {
			station = stationID(body)
		}
		out.Results = append(out.Results, r)
	}
	// Charging-station activity needs a station to ask about, and only a Ford wallbox has one.
	if station == "" {
		out.Results = append(out.Results, ProbeResult{Endpoint: "fccs", Message: "Not asked: the wallbox lists no charging station (only a Ford wallbox has one)."})
	} else {
		now := f.now().In(f.loc())
		q := url.Values{"startDate": {now.AddDate(0, 0, -7).Format("2006-01-02")}, "endDate": {now.Format("2006-01-02")}}
		r, _, err := f.call(ctx, tok.Access, "fccs", fccsPath, q, map[string]string{"chargingStationId": station})
		if err != nil {
			return Probe{}, err
		}
		out.Results = append(out.Results, r)
	}
	at := f.now().UTC()
	out.CheckedAt = &at
	return out, nil
}
