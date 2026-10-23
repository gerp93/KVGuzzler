// Package carbon turns the grid's carbon intensity into a carbon footprint for the energy the house used. The intensity comes from one of two
// sources (Electricity Maps, or the US EIA's hourly generation by fuel turned into an intensity here); each hour's energy is valued at that
// hour's intensity. A port of KVGuzzler.Core.Carbon.
//
// It is the average intensity of the whole grid (location based), the usual way of reporting it, not what one more kWh would cause.
package carbon

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Sources the intensity can come from.
const (
	// ElectricityMaps gives a ready-made intensity for a zone. Needs a key for the zone, from a free personal tier.
	ElectricityMaps = "electricitymaps"
	// EIA is the US Energy Information Administration's hourly generation by fuel for a balancing authority (like MISO), turned into an
	// intensity here. Free with a registration key; US only.
	EIA = "eia"
)

// SourceName is how a source is named to a person.
func SourceName(source string) string {
	if source == EIA {
		return "EIA"
	}
	return "Electricity Maps"
}

// MapsBase is Electricity Maps' address. Fixed here, not saved with the key, so a key can never be sent anywhere else.
const MapsBase = "https://api.electricitymaps.com/v4"

// Config is where the grid's carbon intensity comes from and how to ask: a source, an API key, and the place (for Electricity Maps a zone code
// such as US-MIDW-MISO or latitude and longitude as "44.98, -93.27"; for EIA a balancing authority code such as MISO). The key goes only to the
// address of its own source.
type Config struct {
	Source string `json:"source"`
	APIKey string `json:"api_key"`
	Place  string `json:"place"`
}

var (
	// Any visible character, no spaces or control characters: the key goes in a header or a query, so those are the only things to keep out.
	keyPattern       = regexp.MustCompile(`^[!-~]{8,200}$`)
	zonePattern      = regexp.MustCompile(`^[A-Za-z0-9]{1,12}(-[A-Za-z0-9]{1,12}){0,4}$`)
	authorityPattern = regexp.MustCompile(`^[A-Z0-9]{2,10}$`)
)

func IsValidKey(key string) bool { return keyPattern.MatchString(key) }

// IsValidPlace is a zone code or a latitude and longitude (Electricity Maps), or a balancing authority code (EIA).
func IsValidPlace(place, source string) bool {
	if source == EIA {
		return authorityPattern.MatchString(place)
	}
	if zonePattern.MatchString(place) {
		return true
	}
	_, _, ok := Coordinates(place)
	return ok
}

// Coordinates reads "44.98, -93.27" as a latitude and longitude, when it is one.
func Coordinates(text string) (lat, lon float64, ok bool) {
	parts := strings.Split(text, ",")
	if len(parts) != 2 {
		return 0, 0, false
	}
	lat, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	lon, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err1 != nil || err2 != nil || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return 0, 0, false
	}
	return lat, lon, true
}

// Normalised is the place as it is used: a balancing authority is upper case whatever was typed.
func Normalised(place, source string) string {
	if source == EIA {
		return strings.ToUpper(strings.TrimSpace(place))
	}
	return strings.TrimSpace(place)
}

// Problem says why this cannot be used, in words fit to show, or returns "" when it can.
func Problem(key, place, source string) string {
	eia := source == EIA
	if strings.TrimSpace(key) == "" {
		return fmt.Sprintf("Paste your %s API key first.", SourceName(source))
	}
	if !IsValidKey(strings.TrimSpace(key)) {
		return "That does not look like an API key. Copy it whole from the portal, with no spaces."
	}
	if strings.TrimSpace(place) == "" {
		if eia {
			return "Enter your balancing authority's code, like MISO (Minnesota), PJM, ERCO (Texas), CISO (California) or PSCO (Colorado)."
		}
		return "Enter your zone (like US-MIDW-MISO) or your latitude and longitude (like 44.98, -93.27)."
	}
	if !IsValidPlace(Normalised(place, source), source) {
		if eia {
			return "Enter just the balancing authority's code, like MISO: letters and digits only."
		}
		return "Enter a zone code like US-MIDW-MISO, or a latitude and longitude like 44.98, -93.27."
	}
	return ""
}

// Valid is the configuration as it will be used (the key trimmed, the place normalised), or false when it cannot be used. A source that is
// not known is not valid; an empty one is Electricity Maps (a setup saved before there was a choice).
func (c Config) Valid() (Config, bool) {
	if c.Source == "" {
		c.Source = ElectricityMaps
	}
	if c.Source != ElectricityMaps && c.Source != EIA {
		return Config{}, false
	}
	if Problem(c.APIKey, c.Place, c.Source) != "" {
		return Config{}, false
	}
	c.APIKey, c.Place = strings.TrimSpace(c.APIKey), Normalised(c.Place, c.Source)
	return c, true
}

// LatestURL is the address of Electricity Maps' latest reading. The place is encoded, so nothing typed can change where the request goes.
func (c Config) LatestURL(base string) string {
	if lat, lon, ok := Coordinates(c.Place); ok {
		return fmt.Sprintf("%s/carbon-intensity/latest?lat=%s&lon=%s", base, strconv.FormatFloat(lat, 'f', -1, 64), strconv.FormatFloat(lon, 'f', -1, 64))
	}
	return base + "/carbon-intensity/latest?zone=" + esc(c.Place)
}

// esc percent-encodes everything but letters, digits and - _ . ~, so a typed value can never add or change a parameter of the request.
func esc(s string) string { return strings.ReplaceAll(url.QueryEscape(s), "+", "%20") }

// Reading is the grid's carbon intensity for one hour.
type Reading struct {
	// GramsPerKwh is grams of CO2 equivalent per kWh (a life-cycle figure for the whole grid, an average).
	GramsPerKwh float64
	// At is the hour the figure is for.
	At time.Time
	// Estimated: Electricity Maps had no measurement and modelled it.
	Estimated bool
}

// Error is a problem reaching or understanding a carbon source, worded so it can be shown to the user as is.
type Error struct {
	Message string
	// RateLimited is true when the service said to slow down, so the caller should wait a good while before asking again.
	RateLimited bool
}

func (e *Error) Error() string { return e.Message }

func fail(format string, a ...any) *Error { return &Error{Message: fmt.Sprintf(format, a...)} }
