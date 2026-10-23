// Package openei reads electricity rates from the OpenEI Utility Rate Database (GET https://api.openei.org/utility_rates), a public
// collection of utilities' published tariffs. A plain GET with the free API key as a query parameter, as that service documents; nothing
// else is sent but the address typed or the rate's label. A port of KVGuzzler.Core.Billing.OpenEiClient.
//
// Written from OpenEI's documented field list and tested with made-up answers; not checked against the real service. The answer's shape (a
// month by hour schedule pointing at tiered periods) is reduced to a rates.Tariff; a rate that cannot be reduced (a demand-based or unit-less
// structure, a missing schedule) is left out rather than guessed at.
package openei

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/rates"
)

// Endpoint is where OpenEI answers. The key is only ever sent here: it is not read from anywhere a caller can change.
const Endpoint = "https://api.openei.org/utility_rates"

// Timeout is how long one request may take: a "full detail" answer is big and OpenEI warns it can be slow.
const Timeout = 60 * time.Second

// SearchLimit is how many rates one search asks for. A page of full detail is large, so this stays modest.
const SearchLimit = 60

// maxBody keeps a runaway answer from filling memory.
const maxBody = 32 << 20

// Error is a problem reaching or understanding OpenEI, worded so it can be shown to the user as is.
type Error struct{ Message string }

func (e *Error) Error() string { return e.Message }

func fail(format string, a ...any) *Error { return &Error{Message: fmt.Sprintf(format, a...)} }

// Candidate is one rate found for a place, ready to be chosen.
type Candidate struct {
	Tariff *rates.Tariff
	// IsDefault: OpenEI marks it as the most common rate for its sector and service type.
	IsDefault bool
	// Approved: an OpenEI expert has checked it.
	Approved bool
}

// Lookup finds the electricity rates that apply to a place. An interface, so the server can be tested without the network.
type Lookup interface {
	// Find is the residential rates in force now around an address or ZIP code, the likeliest first. Empty when none was usable.
	Find(ctx context.Context, apiKey, address string, now time.Time) ([]Candidate, error)
	// Get is the current version of one rate by its label, or nil when OpenEI no longer has it.
	Get(ctx context.Context, apiKey, label string, now time.Time) (*Candidate, error)
}

// Client talks to OpenEI over HTTP.
type Client struct {
	HTTP *http.Client
	// Base is the endpoint; empty means Endpoint. Only tests set it.
	Base string
}

func (c *Client) base() string {
	if c.Base != "" {
		return c.Base
	}
	return Endpoint
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{}
}

// esc percent-encodes everything but letters, digits and - _ . ~, so a typed value can never add or change a parameter of the request.
func esc(s string) string { return strings.ReplaceAll(url.QueryEscape(s), "+", "%20") }

// SearchURL is the address of a search. The key and everything typed are encoded, so nothing can change where the request goes.
func SearchURL(base, apiKey, address string, now time.Time) string {
	return base + "?version=latest&format=json&detail=full&sector=Residential&limit=" + strconv.Itoa(SearchLimit) +
		"&effective_on_date=" + strconv.FormatInt(now.Unix(), 10) +
		"&address=" + esc(address) + "&api_key=" + esc(apiKey)
}

// LabelURL is the address of a request for one known rate.
func LabelURL(base, apiKey, label string) string {
	return base + "?version=latest&format=json&detail=full&limit=1&getpage=" + esc(label) + "&api_key=" + esc(apiKey)
}

func (c *Client) Find(ctx context.Context, apiKey, address string, now time.Time) ([]Candidate, error) {
	body, err := c.get(ctx, SearchURL(c.base(), apiKey, address, now))
	if err != nil {
		return nil, err
	}
	found, err := ParseRates(body, now, true)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(found, func(i, j int) bool {
		a, b := found[i], found[j]
		switch {
		case a.IsDefault != b.IsDefault:
			return a.IsDefault
		case a.Approved != b.Approved:
			return a.Approved
		case a.Tariff.Utility != b.Tariff.Utility:
			return a.Tariff.Utility < b.Tariff.Utility
		}
		return a.Tariff.Name < b.Tariff.Name
	})
	return found, nil
}

func (c *Client) Get(ctx context.Context, apiKey, label string, now time.Time) (*Candidate, error) {
	body, err := c.get(ctx, LabelURL(c.base(), apiKey, label))
	if err != nil {
		return nil, err
	}
	found, err := ParseRates(body, now, false)
	if err != nil {
		return nil, err
	}
	for i := range found {
		if strings.EqualFold(found[i].Tariff.Label, label) {
			return &found[i], nil
		}
	}
	return nil, nil
}

func (c *Client) get(ctx context.Context, address string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", fail("Could not make the request to OpenEI.")
	}
	resp, err := c.client().Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "", fail("OpenEI did not answer within %d seconds. It can be slow: try again, or try a ZIP code instead of a full address.", int(Timeout.Seconds()))
		}
		if errors.Is(err, context.Canceled) {
			return "", err
		}
		// The error text carries the address (with the key) of the request; only its cause is shown.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return "", fail("Could not reach OpenEI: %v", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
		if err != nil {
			return "", fail("Could not read OpenEI's answer.")
		}
		return string(b), nil
	case resp.StatusCode == 400:
		return "", fail("OpenEI did not accept the request. Check the address or ZIP code and the API key.")
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return "", fail("OpenEI did not accept the API key. Check it was copied whole.")
	case resp.StatusCode == 429:
		return "", fail("OpenEI asked us to slow down. Try again in a little while.")
	default:
		return "", fail("OpenEI answered with an error (%d). It can be slow or busy: try again later.", resp.StatusCode)
	}
}

const notRates = "OpenEI answered, but not with a list of rates. Try again later."

// ParseRates is the usable residential rates in an answer. Rates that are not in force now, are delivery-only, or cannot be turned into a
// price per kWh by hour are left out. requireCurrent is false when asking for one known rate: it is used even if its dates have passed.
func ParseRates(body string, now time.Time, requireCurrent bool) ([]Candidate, error) {
	var root any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		return nil, fail("%s", notRates)
	}
	obj, ok := root.(map[string]any)
	if !ok {
		return nil, fail("%s", notRates)
	}
	if e, has := obj["error"]; has {
		if s, ok := e.(string); ok {
			return nil, fail("OpenEI said: %s", s)
		}
		b, _ := json.Marshal(e)
		return nil, fail("OpenEI said: %s", b)
	}
	items, ok := obj["items"].([]any)
	if !ok {
		return nil, fail("%s", notRates)
	}
	found := []Candidate{}
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			if c := tryRate(m, now, requireCurrent); c != nil {
				found = append(found, *c)
			}
		}
	}
	return found, nil
}

func tryRate(item map[string]any, now time.Time, requireCurrent bool) *Candidate {
	label, ok := text(item, "label")
	if !ok {
		return nil
	}
	if sector, ok := text(item, "sector"); ok && !strings.EqualFold(sector, "Residential") {
		return nil
	}
	if service, ok := text(item, "servicetype"); ok && strings.HasPrefix(strings.ToLower(service), "delivery") {
		return nil // no energy price in it
	}
	if requireCurrent {
		if start, ok := number(item, "startdate"); ok && start > float64(now.Unix()) {
			return nil
		}
		if end, ok := number(item, "enddate"); ok && end <= float64(now.Unix()) {
			return nil
		}
	}

	structure, ok := item["energyratestructure"].([]any)
	if !ok || len(structure) == 0 {
		return nil
	}

	// Each period's first tier: its price per kWh. A tier whose unit is not plain kWh (a demand-based "kWh/kW") has no price per kWh to use.
	periods := make([]float64, 0, len(structure))
	tiers := 0
	for _, p := range structure {
		period, ok := p.([]any)
		if !ok || len(period) == 0 {
			return nil
		}
		if len(period) > tiers {
			tiers = len(period)
		}
		tier, ok := period[0].(map[string]any)
		if !ok {
			return nil
		}
		if unit, ok := text(tier, "unit"); ok {
			lower := strings.ToLower(unit)
			if !strings.Contains(lower, "kwh") || strings.Contains(lower, "/kw") {
				return nil
			}
		}
		rate, hasRate := number(tier, "rate")
		adj, hasAdj := number(tier, "adj")
		if !hasRate && !hasAdj {
			return nil
		}
		periods = append(periods, rate+adj)
	}

	weekday := schedule(item, "energyweekdayschedule", len(periods))
	if weekday == nil && len(periods) == 1 {
		weekday = flat()
	}
	if weekday == nil {
		return nil
	}
	weekend := schedule(item, "energyweekendschedule", len(periods))
	if weekend == nil {
		weekend = weekday
	}

	var fuel [12]float64
	if adj, ok := item["fueladjustmentsmonthly"].([]any); ok && len(adj) == 12 {
		for i, a := range adj {
			if f, ok := a.(float64); ok && !math.IsInf(f, 0) && !math.IsNaN(f) {
				fuel[i] = f
			}
		}
	}

	grid := make([]float64, rates.GridSize)
	for m := 0; m < 12; m++ {
		for h := 0; h < 24; h++ {
			grid[rates.GridIndex(m, false, h)] = periods[weekday[m][h]] + fuel[m]
			grid[rates.GridIndex(m, true, h)] = periods[weekend[m][h]] + fuel[m]
		}
	}
	// A price below zero or beyond anything a household pays is a mistake in the data, not a tariff.
	for _, p := range grid {
		if math.IsNaN(p) || math.IsInf(p, 0) || p < 0 || p > 10 {
			return nil
		}
	}

	var fixed *string
	if fc, ok := number(item, "fixedchargefirstmeter"); ok && fc > 0 {
		units, _ := text(item, "fixedchargeunits")
		s := strings.TrimSpace(strconv.FormatFloat(fc, 'f', -1, 64) + " " + units)
		fixed = &s
	}
	utility, ok := text(item, "utility")
	if !ok {
		utility = "Unknown utility"
	}
	name, ok := text(item, "name")
	if !ok {
		name = label
	}
	var uri *string
	if u, ok := text(item, "uri"); ok {
		uri = &u
	}
	t := &rates.Tariff{Label: label, Utility: utility, Name: name, URI: uri, FetchedAt: now.UTC(), Grid: grid, Periods: len(periods), Tiers: tiers, FixedCharge: fixed}
	isDefault, _ := item["is_default"].(bool)
	approved, _ := item["approved"].(bool)
	return &Candidate{Tariff: t, IsDefault: isDefault, Approved: approved}
}

func flat() [][]int {
	out := make([][]int, 12)
	for i := range out {
		out[i] = make([]int, 24)
	}
	return out
}

// schedule is a 12 by 24 table of period numbers, or nil when it is missing or points at a period that does not exist.
func schedule(item map[string]any, name string, periods int) [][]int {
	table, ok := item[name].([]any)
	if !ok || len(table) != 12 {
		return nil
	}
	out := make([][]int, 12)
	for m, month := range table {
		hours, ok := month.([]any)
		if !ok || len(hours) != 24 {
			return nil
		}
		out[m] = make([]int, 24)
		for h, cell := range hours {
			f, ok := cell.(float64)
			if !ok || f != math.Trunc(f) || f < 0 || f >= float64(periods) {
				return nil
			}
			out[m][h] = int(f)
		}
	}
	return out
}

func text(m map[string]any, name string) (string, bool) {
	s, ok := m[name].(string)
	if !ok || strings.TrimSpace(s) == "" {
		return "", false
	}
	return s, true
}

// number is a number, whether the service wrote it as a number or as text.
func number(m map[string]any, name string) (float64, bool) {
	switch v := m[name].(type) {
	case float64:
		return v, !math.IsInf(v, 0) && !math.IsNaN(v)
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f, err == nil && !math.IsInf(f, 0) && !math.IsNaN(f)
	}
	return 0, false
}
