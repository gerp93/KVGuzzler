// Package ford reads a Ford vehicle's odometer through FordConnect Query (Ford's developer API) so the odometer reading of an electric vehicle's
// charging meter keeps itself up to date.
//
// The user registers their own application at developer.ford.com (the client ID and secret are theirs and are typed into the dashboard, never
// committed) and links their vehicle once: the dashboard shows Ford's sign-in page, Ford asks the owner to consent and redirects the browser to
// the registered redirect URI with a one-time code, and the user pastes that address back. The app then holds a client ID, a client secret and a
// refresh token, and never the owner's Ford password. Only reading is done: a GET of the vehicle's telemetry.
package ford

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// APIBase is where the vehicle data is read from. It is a constant, never taken from the saved setup, so nothing typed can send the access
	// token somewhere else.
	APIBase = "https://api.vehicle.ford.com"
	// TelemetryPath is the vehicle telemetry endpoint of FordConnect Query.
	TelemetryPath = "/fcon-query/v1/telemetry"

	Kilometers = "km"
	Miles      = "miles"

	kmPerMile = 1.609344
	// MaxOdometerMiles mirrors the store's limit: a larger figure is not a car.
	maxOdometerMiles = 10_000_000
)

// Config is the setup the user typed in. ClientSecret is write-only everywhere it is shown.
type Config struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	// AuthorizeURL is the account-linking address copied from the developer dashboard ("View oAuth URLs"). It already holds the client ID and the
	// registered redirect URI; only its state is replaced, with a fresh value each time.
	AuthorizeURL string `json:"authorize_url"`
	// TokenURL is the address the code and the refresh token are exchanged at (the "B2C Code Exchange" request of Ford's Postman collection).
	TokenURL string `json:"token_url"`
	// OdometerUnit is what Ford's figure is in: "km" or "miles". It is the user's choice, checked against the car's own display, so a figure is never
	// guessed to be one or the other.
	OdometerUnit string `json:"odometer_unit"`
	// MeterID is the electric vehicle's charging meter whose odometer readings are kept up to date.
	MeterID string `json:"meter_id"`
}

// Scope is what the token requests are made for: the client's own ID plus the two OpenID scopes that give a refresh token.
func (c Config) Scope() string { return c.ClientID + " offline_access openid" }

// checkURL says what is wrong with an address the client secret may be sent to (https and a named host, not an IP address; a loopback address is
// allowed for a test server only), or "" with the parsed address when it is fine.
func checkURL(raw, what string) (*url.URL, string) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return nil, what + " is not an address."
	}
	host := u.Hostname()
	loopback := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if u.Scheme != "https" && !(loopback && u.Scheme == "http") {
		return nil, what + " has to start with https://."
	}
	if net.ParseIP(host) != nil && !loopback {
		return nil, what + " has to use a name, not an IP address."
	}
	if u.User != nil {
		return nil, what + " cannot hold a user name or password."
	}
	return u, ""
}

// Problem says what is wrong with a setup, or "" when it is usable. haveSecret says a client secret is already saved, so one may be left out.
func Problem(c Config, haveSecret bool) string {
	switch {
	case strings.TrimSpace(c.ClientID) == "" || strings.ContainsAny(c.ClientID, " \t\r\n"):
		return "Enter the client ID from the Ford developer dashboard."
	case strings.TrimSpace(c.ClientSecret) == "" && !haveSecret:
		return "Enter the client secret from the Ford developer dashboard."
	case strings.TrimSpace(c.MeterID) == "":
		return "Pick the meter that charges the vehicle."
	case c.OdometerUnit != Kilometers && c.OdometerUnit != Miles:
		return "Say whether Ford's odometer is in kilometers or miles (compare it with the car's own display)."
	}
	if _, msg := checkURL(c.AuthorizeURL, "The account-linking URL"); msg != "" {
		return msg
	}
	if _, msg := checkURL(c.TokenURL, "The token URL"); msg != "" {
		return msg
	}
	au, _ := url.Parse(c.AuthorizeURL)
	q := au.Query()
	if q.Get("client_id") != "" && q.Get("client_id") != c.ClientID {
		return "The account-linking URL is for a different client ID."
	}
	if q.Get("redirect_uri") == "" {
		return "The account-linking URL has no redirect_uri: copy it again from the dashboard (View oAuth URLs and Modify Redirect URIs)."
	}
	return ""
}

// NewState is an unpredictable value for one linking attempt, checked when the user comes back (it stops a stray or forged address from linking).
func NewState() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// LinkURL is the account-linking address with its state replaced by the given one.
func LinkURL(c Config, state string) (string, error) {
	u, err := url.Parse(c.AuthorizeURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("state", state)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// RedirectURI is the redirect address the account-linking URL names; the same one has to be given when the code is exchanged.
func RedirectURI(c Config) string {
	u, err := url.Parse(c.AuthorizeURL)
	if err != nil {
		return ""
	}
	return u.Query().Get("redirect_uri")
}

// CodeFromRedirect finds the one-time code in the address the browser was sent to after the owner consented (pasted whole, or just its query),
// and checks the state is the one given out. Ford's own error (the owner declined) is said as it is.
func CodeFromRedirect(pasted, wantState string) (string, error) {
	pasted = strings.TrimSpace(pasted)
	if pasted == "" {
		return "", errors.New("Paste the address the browser went to after you agreed (it starts with the redirect address and has ?code=... in it).")
	}
	raw := pasted
	if i := strings.Index(raw, "?"); i >= 0 {
		raw = raw[i+1:]
	}
	if i := strings.Index(raw, "#"); i >= 0 {
		raw = raw[:i]
	}
	q, err := url.ParseQuery(raw)
	if err != nil {
		return "", errors.New("That is not an address with a code in it.")
	}
	if e := q.Get("error"); e != "" {
		if d := q.Get("error_description"); d != "" {
			return "", fmt.Errorf("Ford said: %s", d)
		}
		return "", fmt.Errorf("Ford said: %s", e)
	}
	if q.Get("state") == "" || q.Get("state") != wantState {
		return "", errors.New("That address is not from the link that was just made (its state differs). Start the link again.")
	}
	code := q.Get("code")
	if code == "" {
		return "", errors.New("There is no code in that address.")
	}
	return code, nil
}

// Tokens are what a code or a refresh token is exchanged for. Ford gives an access token for 20 minutes and a refresh token for 30 days, replaced
// at every refresh.
type Tokens struct {
	Access  string
	Refresh string
}

// Error is something Ford's servers said, worded for the user. Relink means the saved permission is gone (expired, revoked or never given), so
// only the owner linking again helps and nothing more should be tried until then.
type Error struct {
	Message     string
	Relink      bool
	RateLimited bool
}

func (e *Error) Error() string { return e.Message }

// Client talks to Ford. The zero value uses the real server.
type Client struct {
	HTTP *http.Client
	// Base replaces APIBase (tests only).
	Base string
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *Client) base() string {
	if c.Base != "" {
		return c.Base
	}
	return APIBase
}

// Exchange trades the one-time code for tokens.
func (c *Client) Exchange(ctx context.Context, cfg Config, code string) (Tokens, error) {
	return c.token(ctx, cfg, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {RedirectURI(cfg)}})
}

// Refresh trades a refresh token for a new pair of tokens.
func (c *Client) Refresh(ctx context.Context, cfg Config, refresh string) (Tokens, error) {
	return c.token(ctx, cfg, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}})
}

func (c *Client) token(ctx context.Context, cfg Config, form url.Values) (Tokens, error) {
	form.Set("client_id", cfg.ClientID)
	form.Set("client_secret", cfg.ClientSecret)
	form.Set("scope", cfg.Scope())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Tokens{}, &Error{Message: "The token URL is not usable."}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Tokens{}, ctx.Err()
		}
		// The error text of a failed request names the address, never the form, so it holds no secret; it is still cut to what is useful.
		return Tokens{}, &Error{Message: "Could not reach Ford's sign-in service."}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	var answer struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Error        string `json:"error"`
		Description  string `json:"error_description"`
	}
	_ = json.Unmarshal(body, &answer)
	if resp.StatusCode != http.StatusOK || answer.AccessToken == "" {
		switch {
		case answer.Error == "invalid_grant":
			// Expired, revoked or already used (a code is single-use and lasts 10 minutes), or the redirect address differs from the registered one.
			return Tokens{}, &Error{Message: "Ford no longer accepts the saved permission (it expired, was revoked, or the code was used or too old). Link the vehicle again.", Relink: true}
		case resp.StatusCode == http.StatusTooManyRequests:
			return Tokens{}, &Error{Message: "Ford asked to slow down.", RateLimited: true}
		case answer.Error != "":
			return Tokens{}, &Error{Message: "Ford refused the request: " + clip(answer.Error+" "+answer.Description)}
		}
		return Tokens{}, &Error{Message: fmt.Sprintf("Ford's sign-in service answered %d.", resp.StatusCode)}
	}
	t := Tokens{Access: answer.AccessToken, Refresh: answer.RefreshToken}
	return t, nil
}

func clip(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}

// Telemetry reads the vehicle's telemetry (one GET; Ford allows one request per 30 seconds per endpoint).
func (c *Client) Telemetry(ctx context.Context, access string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base()+TelemetryPath, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &Error{Message: "Could not reach Ford's vehicle service."}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	switch {
	case resp.StatusCode == http.StatusOK:
		return body, nil
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, &Error{Message: "Ford asked to slow down.", RateLimited: true}
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, &Error{Message: "Ford did not accept the access token."}
	case resp.StatusCode == http.StatusForbidden:
		return nil, &Error{Message: "Ford says this link has no access to the vehicle's telemetry (insufficient scope): register the application again with that scope and link the vehicle again."}
	}
	return nil, &Error{Message: fmt.Sprintf("Ford's vehicle service answered %d.", resp.StatusCode)}
}

// Odometer is the figure found in a telemetry answer.
type Odometer struct {
	// Value is as Ford gave it, in the unit the user said.
	Value float64
	// Path is where in the answer it was found (like "metrics.odometer.value"), so the user can check it.
	Path string
}

// ParseOdometer finds the odometer in a telemetry answer. The answer's exact shape is not documented anywhere this was written from, so it looks for
// any member named like "odometer" holding a number (or an object with a numeric "value", or a number as text); more than one different figure is an
// error rather than a guess (a second vehicle, say).
func ParseOdometer(body []byte) (Odometer, error) {
	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		return Odometer{}, errors.New("Ford's telemetry answer is not JSON.")
	}
	var found []Odometer
	var walk func(v any, path string, depth int)
	walk = func(v any, path string, depth int) {
		if depth > 12 {
			return
		}
		switch t := v.(type) {
		case map[string]any:
			for k, child := range t {
				p := k
				if path != "" {
					p = path + "." + k
				}
				if strings.Contains(strings.ToLower(k), "odometer") {
					if n, sub, ok := number(child); ok {
						if sub != "" {
							p += "." + sub
						}
						found = append(found, Odometer{Value: n, Path: p})
						continue
					}
				}
				walk(child, p, depth+1)
			}
		case []any:
			for i, child := range t {
				walk(child, fmt.Sprintf("%s[%d]", path, i), depth+1)
			}
		}
	}
	walk(root, "", 0)
	if len(found) == 0 {
		return Odometer{}, errors.New("Ford's telemetry answer has no odometer in it (the link may not include that data).")
	}
	first := found[0]
	for _, f := range found[1:] {
		if f.Value != first.Value {
			return Odometer{}, fmt.Errorf("Ford's telemetry answer has more than one odometer (%s and %s).", first.Path, f.Path)
		}
	}
	return first, nil
}

// number reads a figure from a JSON value: a number, a numeric string, or an object whose "value" is one (sub names the member).
func number(v any) (n float64, sub string, ok bool) {
	switch t := v.(type) {
	case float64:
		return t, "", !math.IsNaN(t)
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f, "", err == nil
	case map[string]any:
		if inner, has := t["value"]; has {
			if n, _, ok := number(inner); ok {
				return n, "value", true
			}
		}
	}
	return 0, "", false
}

// Miles is the odometer in miles, or false when the figure is not a plausible odometer.
func (o Odometer) Miles(unit string) (float64, bool) {
	m := o.Value
	if unit == Kilometers {
		m = o.Value / kmPerMile
	}
	if !(m >= 0 && m <= maxOdometerMiles) {
		return 0, false
	}
	return math.Round(m*10) / 10, true
}
