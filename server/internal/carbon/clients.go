package carbon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// EiaBase is where EIA's hourly generation by fuel is read. Like MapsBase it is fixed here, so a key is only ever sent to its own source.
const EiaBase = "https://api.eia.gov/v2/electricity/rto/fuel-type-data/data/"

const (
	mapsTimeout = 10 * time.Second
	eiaTimeout  = 30 * time.Second
	// EiaRows is how many rows to ask for: about nine fuels an hour, so roughly the last two and a half days.
	EiaRows = 600
	// minimumFuels: an hour reported with fewer fuels than this is a partial report, and its mix would mislead.
	minimumFuels = 3
	maxBody      = 8 << 20
)

// GramsPerKwh is grams of CO2 equivalent per kWh over a power station's life, for each of EIA's fuel codes. IPCC medians (coal 820, gas 490,
// biomass 230, geothermal 38, hydro 24, nuclear 12, solar 45, wind 11); oil is 650. "Other" (waste, biomass and the like) is taken as biomass.
// Storage and unknown sources are left out.
var GramsPerKwh = map[string]float64{
	"COL": 820, "NG": 490, "OIL": 650, "NUC": 12, "SUN": 45, "SNB": 45, "WND": 11, "WAT": 24, "GEO": 38, "BIO": 230, "OTH": 230,
}

// Client asks the sources for the grid's carbon intensity. The bases are only changed by tests.
type Client struct {
	HTTP      *http.Client
	MapsBase  string
	EiaBase   string
	MapsLimit time.Duration
	EiaLimit  time.Duration
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{}
}

func (c *Client) mapsBase() string {
	if c.MapsBase != "" {
		return c.MapsBase
	}
	return MapsBase
}

func (c *Client) eiaBase() string {
	if c.EiaBase != "" {
		return c.EiaBase
	}
	return EiaBase
}

// Read asks whichever source the config names for the recent hours, oldest first: Electricity Maps gives the latest hour, EIA the last couple
// of days (so hours missed while the server was off can be filled in). The error, when it is a *Error, is worded to be shown.
func (c *Client) Read(ctx context.Context, cfg Config, now time.Time) ([]Reading, error) {
	if cfg.Source == EIA {
		return c.readEia(ctx, cfg)
	}
	r, err := c.readLatest(ctx, cfg, now)
	if err != nil {
		return nil, err
	}
	return []Reading{r}, nil
}

// ---- Electricity Maps

// ParseLatest is the figure and its hour from a "latest" answer. Tolerant of a missing time (it says now) and of the figure being wrapped in a
// data object; anything that is not a believable number of grams is refused.
func ParseLatest(body string, now time.Time) (Reading, error) {
	var root any
	if err := json.Unmarshal([]byte(body), &root); err == nil {
		if obj, ok := root.(map[string]any); ok {
			if data, ok := obj["data"].(map[string]any); ok {
				if _, has := obj["carbonIntensity"]; !has {
					obj = data
				}
			}
			if grams, ok := obj["carbonIntensity"].(float64); ok && !math.IsNaN(grams) && !math.IsInf(grams, 0) && grams >= 0 && grams <= 3000 {
				at := now
				if s, ok := obj["datetime"].(string); ok {
					if t, err := time.Parse(time.RFC3339, s); err == nil {
						at = t
					}
				}
				estimated, _ := obj["isEstimated"].(bool)
				return Reading{GramsPerKwh: grams, At: at.UTC(), Estimated: estimated}, nil
			}
		}
	}
	return Reading{}, fail("Electricity Maps answered, but not with a carbon intensity. Check the zone, or try again later.")
}

func (c *Client) readLatest(ctx context.Context, cfg Config, now time.Time) (Reading, error) {
	limit := c.MapsLimit
	if limit == 0 {
		limit = mapsTimeout
	}
	status, body, err := c.get(ctx, cfg.LatestURL(c.mapsBase()), limit, map[string]string{"auth-token": cfg.APIKey}, "Electricity Maps")
	if err != nil {
		return Reading{}, err
	}
	if status >= 200 && status < 300 {
		return ParseLatest(body, now)
	}
	why := ""
	if m := mapsMessage(body, cfg.APIKey); m != "" {
		why = " Electricity Maps says: " + m
	}
	switch status {
	case 401, 403:
		return Reading{}, fail("Electricity Maps did not accept the key (error %d). The free key covers one zone: use the zone it was set up for.%s", status, why)
	case 404:
		return Reading{}, fail("Electricity Maps does not know that zone. Check it on the Electricity Maps map.%s", why)
	case 429:
		return Reading{}, &Error{Message: "Electricity Maps asked us to slow down. It will be tried again later." + why, RateLimited: true}
	}
	return Reading{}, fail("Electricity Maps answered with an error (%d).%s", status, why)
}

// mapsMessage is the message (or error) of an error answer, cut short, with any trace of the key taken out; "" if there is none.
func mapsMessage(body, apiKey string) string {
	var root map[string]any
	if json.Unmarshal([]byte(body), &root) != nil {
		return ""
	}
	for _, name := range []string{"message", "error"} {
		if s, ok := root[name].(string); ok && strings.TrimSpace(s) != "" {
			return clean(s, apiKey)
		}
	}
	return ""
}

func clean(text, apiKey string) string {
	if apiKey != "" {
		text = strings.ReplaceAll(text, apiKey, "(key)")
	}
	text = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, text))
	if r := []rune(text); len(r) > 200 {
		text = string(r[:200]) + "..."
	}
	return text
}

// ---- EIA

// EiaURL is the address of EIA's request for a balancing authority. The key and the code are encoded.
func EiaURL(base string, cfg Config) string {
	return base + "?api_key=" + esc(cfg.APIKey) + "&frequency=hourly&data%5B0%5D=value" +
		"&facets%5Brespondent%5D%5B%5D=" + esc(cfg.Place) +
		"&sort%5B0%5D%5Bcolumn%5D=period&sort%5B0%5D%5Bdirection%5D=desc&length=" + strconv.Itoa(EiaRows)
}

const notGeneration = "EIA answered, but not with generation data. Check the balancing authority's code, or try again later."

type fuelMwh struct {
	fuel string
	mwh  float64
}

// ParseEia is the intensity of each complete hour in an answer, oldest first. Hours reported with too few fuels, or with no generation, are
// left out.
func ParseEia(body string) ([]Reading, error) {
	var root map[string]any
	if json.Unmarshal([]byte(body), &root) != nil {
		return nil, fail("%s", notGeneration)
	}
	response, ok := root["response"].(map[string]any)
	if !ok {
		return nil, fail("%s", notGeneration)
	}
	rows, ok := response["data"].([]any)
	if !ok {
		return nil, fail("%s", notGeneration)
	}
	hours := map[time.Time][]fuelMwh{}
	for _, r := range rows {
		row, ok := r.(map[string]any)
		if !ok {
			continue
		}
		period, ok := row["period"].(string)
		if !ok {
			continue
		}
		hour, err := time.Parse("2006-01-02T15", period)
		if err != nil {
			continue
		}
		fuel, ok := row["fueltype"].(string)
		if !ok || fuel == "" {
			continue
		}
		var mwh float64
		switch v := row["value"].(type) { // EIA writes its numbers as text
		case string:
			f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err != nil {
				continue
			}
			mwh = f
		case float64:
			mwh = v
		default:
			continue
		}
		if math.IsNaN(mwh) || math.IsInf(mwh, 0) {
			continue
		}
		hours[hour.UTC()] = append(hours[hour.UTC()], fuelMwh{fuel, mwh})
	}

	keys := make([]time.Time, 0, len(hours))
	for h := range hours {
		keys = append(keys, h)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Before(keys[j]) })
	var readings []Reading
	for _, h := range keys {
		fuels := hours[h]
		if len(fuels) < minimumFuels {
			continue
		}
		if g, ok := intensity(fuels); ok {
			readings = append(readings, Reading{GramsPerKwh: g, At: h})
		}
	}
	return readings, nil
}

// Intensity is grams per kWh of one hour's generation by fuel (code, MWh), or false when nothing that counts was generated. Each fuel is
// weighted by what it generated; a negative figure (a data glitch) counts as none, and storage and unknown fuels are left out.
func Intensity(fuels map[string]float64) (float64, bool) {
	list := make([]fuelMwh, 0, len(fuels))
	for f, m := range fuels {
		list = append(list, fuelMwh{f, m})
	}
	return intensity(list)
}

func intensity(fuels []fuelMwh) (float64, bool) {
	var total, grams float64
	for _, f := range fuels {
		factor, ok := GramsPerKwh[strings.ToUpper(f.fuel)]
		if !ok || !(f.mwh > 0) {
			continue
		}
		total += f.mwh
		grams += f.mwh * factor
	}
	if total > 0 {
		return grams / total, true
	}
	return 0, false
}

func (c *Client) readEia(ctx context.Context, cfg Config) ([]Reading, error) {
	limit := c.EiaLimit
	if limit == 0 {
		limit = eiaTimeout
	}
	status, body, err := c.get(ctx, EiaURL(c.eiaBase(), cfg), limit, nil, "EIA")
	if err != nil {
		return nil, err
	}
	if status >= 200 && status < 300 {
		readings, err := ParseEia(body)
		if err != nil {
			return nil, err
		}
		if len(readings) == 0 {
			return nil, fail("EIA has no recent generation data for \"%s\". Check the code (MISO, PJM, ERCO, CISO...) or try again later.", cfg.Place)
		}
		return readings, nil
	}
	why := ""
	if m := eiaMessage(body, cfg.APIKey); m != "" {
		why = " EIA says: " + m
	}
	switch status {
	case 400:
		return nil, fail("EIA did not accept the request. Check the balancing authority's code.%s", why)
	case 401, 403:
		return nil, fail("EIA did not accept the key (error %d). Check it was copied whole.%s", status, why)
	case 404:
		return nil, fail("EIA does not know that address.%s", why)
	case 429:
		return nil, &Error{Message: "EIA asked us to slow down. It will be tried again later." + why, RateLimited: true}
	}
	return nil, fail("EIA answered with an error (%d).%s", status, why)
}

// eiaMessage is EIA's own words for an error (error.message, or error as text), cut short, with any trace of the key taken out.
func eiaMessage(body, apiKey string) string {
	var root map[string]any
	if json.Unmarshal([]byte(body), &root) != nil {
		return ""
	}
	var text string
	switch e := root["error"].(type) {
	case string:
		text = e
	case map[string]any:
		text, _ = e["message"].(string)
	}
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return clean(text, apiKey)
}

// ---- both

// get sends a GET and returns the status and body. A failure to reach the source is an *Error that never carries the request's address (the
// key may be in it).
func (c *Client) get(ctx context.Context, address string, limit time.Duration, headers map[string]string, who string) (int, string, error) {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return 0, "", fail("Could not make the request to %s.", who)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return 0, "", fail("%s did not answer within %.0f seconds. Is this computer online?", who, limit.Seconds())
		}
		if errors.Is(err, context.Canceled) {
			return 0, "", err
		}
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return 0, "", fail("Could not reach %s: %v", who, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return 0, "", fail("Could not read %s's answer.", who)
	}
	return resp.StatusCode, string(b), nil
}
