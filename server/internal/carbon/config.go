// Package carbon turns the grid's carbon intensity into a carbon footprint for the energy the house used. The intensity comes from the US EIA's
// hourly generation by fuel for a balancing authority, turned into an intensity here; each hour's energy is valued at that hour's intensity. A
// port of KVGuzzler.Core.Carbon without its Electricity Maps source (a free key for it did not give the data wanted, so it was not carried over).
//
// It is the average intensity of the whole grid (location based), the usual way of reporting it, not what one more kWh would cause.
package carbon

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// EIA is the only source: the US Energy Information Administration's hourly generation by fuel for a balancing authority (like MISO), turned into
// an intensity here. Free with a registration key; US only.
const EIA = "eia"

// Config is where the grid's carbon intensity comes from: an API key and the place, a balancing authority's code such as MISO. The key goes only
// to EIA's address.
type Config struct {
	// Source is kept in what is saved so a setup made when there was a choice still reads: only "eia" (or nothing) is usable.
	Source string `json:"source,omitempty"`
	APIKey string `json:"api_key"`
	Place  string `json:"place"`
}

var (
	// Any visible character, no spaces or control characters: the key goes in a query, so those are the only things to keep out.
	keyPattern       = regexp.MustCompile(`^[!-~]{8,200}$`)
	authorityPattern = regexp.MustCompile(`^[A-Z0-9]{2,10}$`)
)

func IsValidKey(key string) bool { return keyPattern.MatchString(key) }

// IsValidPlace is a balancing authority code.
func IsValidPlace(place string) bool { return authorityPattern.MatchString(place) }

// Normalised is the place as it is used: a balancing authority is upper case whatever was typed.
func Normalised(place string) string { return strings.ToUpper(strings.TrimSpace(place)) }

// Problem says why this cannot be used, in words fit to show, or returns "" when it can.
func Problem(key, place string) string {
	if strings.TrimSpace(key) == "" {
		return "Paste your EIA API key first."
	}
	if !IsValidKey(strings.TrimSpace(key)) {
		return "That does not look like an API key. Copy it whole from the portal, with no spaces."
	}
	if strings.TrimSpace(place) == "" {
		return "Enter your balancing authority's code, like MISO (Minnesota), PJM, ERCO (Texas), CISO (California) or PSCO (Colorado)."
	}
	if !IsValidPlace(Normalised(place)) {
		return "Enter just the balancing authority's code, like MISO: letters and digits only."
	}
	return ""
}

// Valid is the configuration as it will be used (the key trimmed, the place normalised), or false when it cannot be used. A setup saved for
// any other source (Electricity Maps was one, once) is not usable.
func (c Config) Valid() (Config, bool) {
	if c.Source != "" && c.Source != EIA {
		return Config{}, false
	}
	if Problem(c.APIKey, c.Place) != "" {
		return Config{}, false
	}
	return Config{Source: EIA, APIKey: strings.TrimSpace(c.APIKey), Place: Normalised(c.Place)}, true
}

// esc percent-encodes everything but letters, digits and - _ . ~, so a typed value can never add or change a parameter of the request.
func esc(s string) string { return strings.ReplaceAll(url.QueryEscape(s), "+", "%20") }

// Reading is the grid's carbon intensity for one hour.
type Reading struct {
	// GramsPerKwh is grams of CO2 equivalent per kWh (a life-cycle figure for the whole grid, an average).
	GramsPerKwh float64
	// At is the hour the figure is for.
	At time.Time
	// Estimated is true when the source had no measurement and modelled it (EIA's figures are computed from reported generation, so never).
	Estimated bool
}

// Error is a problem reaching or understanding the carbon source, worded so it can be shown to the user as is.
type Error struct {
	Message string
	// RateLimited is true when the service said to slow down, so the caller should wait a good while before asking again.
	RateLimited bool
}

func (e *Error) Error() string { return e.Message }

func fail(format string, a ...any) *Error { return &Error{Message: fmt.Sprintf(format, a...)} }
