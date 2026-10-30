// Package gasprice reads the average retail price of gasoline from the US EIA's weekly survey, for an area (the US, a region, a state or a big city),
// so the cost of driving an electric car can be set beside what the same miles would have cost in a gasoline one. It is the same EIA key as the carbon
// footprint's, sent only to EIA.
package gasprice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/rates"
)

// Base is where the weekly retail prices are read. Like the carbon source's address it is fixed here, so the key is only ever sent to EIA.
const Base = "https://api.eia.gov/v2/petroleum/pri/gnd/data/"

const (
	timeout = 30 * time.Second
	maxBody = 4 << 20
	// product is regular gasoline, all formulations: the grade a typical car takes.
	product = "EPMR"
	// maxDollars is a price per gallon no real week comes near: anything above it is not a price.
	maxDollars = 50
)

// Area is a place EIA publishes a weekly gasoline price for.
type Area struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Areas are the places EIA's weekly survey covers (its own codes), the whole country first and then the regions, states and cities by name.
var Areas = []Area{
	{"NUS", "U.S."},
	{"R10", "East Coast (PADD 1)"}, {"R20", "Midwest (PADD 2)"}, {"R30", "Gulf Coast (PADD 3)"}, {"R40", "Rocky Mountain (PADD 4)"}, {"R50", "West Coast (PADD 5)"},
	{"SCA", "California"}, {"SCO", "Colorado"}, {"SFL", "Florida"}, {"SMA", "Massachusetts"}, {"SMN", "Minnesota"}, {"SNY", "New York"}, {"SOH", "Ohio"},
	{"STX", "Texas"}, {"SWA", "Washington"},
	{"YBOS", "Boston"}, {"YORD", "Chicago"}, {"YCLE", "Cleveland"}, {"YDEN", "Denver"}, {"Y44HO", "Houston"}, {"Y05LA", "Los Angeles"}, {"YMIA", "Miami"},
	{"Y35NY", "New York City"}, {"Y05SF", "San Francisco"}, {"Y48SE", "Seattle"},
}

// AreaName is the name of an area by its code; false when EIA has no such area.
func AreaName(id string) (string, bool) {
	for _, a := range Areas {
		if a.ID == id {
			return a.Name, true
		}
	}
	return "", false
}

// Price is the average retail price of a gallon of regular gasoline in a week.
type Price struct {
	// Week is the day EIA dates the week by, a Monday, `2026-10-26`.
	Week string `json:"week"`
	// Dollars is a decimal string per gallon, exact.
	Dollars string `json:"dollars"`
}

// Error is a failure worded to be shown. It never carries the request's address, which holds the key.
type Error struct {
	Message     string
	RateLimited bool
}

func (e *Error) Error() string { return e.Message }

func fail(format string, a ...any) *Error { return &Error{Message: fmt.Sprintf(format, a...)} }

// Client asks EIA for the prices. The base is only changed by tests.
type Client struct {
	HTTP *http.Client
	Base string
}

func (c *Client) base() string {
	if c.Base != "" {
		return c.Base
	}
	return Base
}

// Weekly reads an area's regular gasoline prices from the given day on, oldest first.
func (c *Client) Weekly(ctx context.Context, apiKey, area string, since time.Time) ([]Price, error) {
	if _, ok := AreaName(area); !ok {
		return nil, fail("EIA has no gasoline price for the area %q.", area)
	}
	q := url.Values{}
	q.Set("api_key", apiKey)
	q.Set("frequency", "weekly")
	q.Set("data[0]", "value")
	q.Add("facets[product][]", product)
	q.Add("facets[duoarea][]", area)
	q.Set("start", since.Format("2006-01-02"))
	q.Set("sort[0][column]", "period")
	q.Set("sort[0][direction]", "asc")
	q.Set("length", "5000")

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base()+"?"+q.Encode(), nil)
	if err != nil {
		return nil, fail("Could not make the request to EIA.")
	}
	httpc := c.HTTP
	if httpc == nil {
		httpc = &http.Client{}
	}
	resp, err := httpc.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, err
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fail("EIA did not answer within %.0f seconds. Is this computer online?", timeout.Seconds())
		}
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // the address, which holds the key, stays out
		}
		return nil, fail("Could not reach EIA: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fail("Could not read EIA's answer.")
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		prices, err := ParsePrices(body)
		if err != nil {
			return nil, err
		}
		if len(prices) == 0 {
			return nil, fail("EIA has no gasoline prices for that area since %s.", since.Format("2006-01-02"))
		}
		return prices, nil
	}
	why := message(body, apiKey)
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fail("EIA did not accept the key (error %d). Check it under Carbon.%s", resp.StatusCode, why)
	case http.StatusTooManyRequests:
		return nil, &Error{Message: "EIA asked for a slower pace. It will be tried again later." + why, RateLimited: true}
	}
	return nil, fail("EIA answered with an error (%d).%s", resp.StatusCode, why)
}

// message is EIA's own words for an error, cut short, with any trace of the key taken out.
func message(body []byte, apiKey string) string {
	var root map[string]any
	if json.Unmarshal(body, &root) != nil {
		return ""
	}
	var text string
	switch e := root["error"].(type) {
	case string:
		text = e
	case map[string]any:
		text, _ = e["message"].(string)
	}
	text = strings.TrimSpace(strings.ReplaceAll(text, apiKey, "[key]"))
	if text == "" {
		return ""
	}
	if len(text) > 200 {
		text = text[:200] + "..."
	}
	return " EIA says: " + text
}

// ParsePrices reads EIA's answer: rows of {period, value}. A row whose price is not a believable price is left out, and the rows come back in the
// order of their weeks.
func ParsePrices(body []byte) ([]Price, error) {
	var root struct {
		Response struct {
			Data []struct {
				Period string `json:"period"`
				Value  any    `json:"value"`
			} `json:"data"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fail("EIA's answer was not what was expected.")
	}
	var out []Price
	for _, row := range root.Response.Data {
		if _, err := time.Parse("2006-01-02", row.Period); err != nil {
			continue
		}
		text := ""
		switch v := row.Value.(type) {
		case string:
			text = strings.TrimSpace(v)
		case float64:
			text = fmt.Sprintf("%.3f", v)
		}
		m, err := rates.ParseMoney(text)
		if err != nil || m.Sign() <= 0 || m.Float() > maxDollars {
			continue
		}
		out = append(out, Price{Week: row.Period, Dollars: m.String()})
	}
	// EIA sorts by the request, but nothing downstream may depend on it.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Week < out[j-1].Week; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, nil
}
