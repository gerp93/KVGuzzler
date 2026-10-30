package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/gasprice"
)

const eiaKey = "EIA-KEY-12345"

// fakeEIA answers the weekly gasoline prices of any area with two weeks, and remembers what it was asked.
func fakeEIA(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.RawQuery)
		if r.URL.Query().Get("api_key") != eiaKey {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`{"response":{"data":[{"period":"2026-10-12","value":"4.50"},{"period":"2026-10-05","value":"4.00"}]}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &asked
}

type savingsBody struct {
	Configured bool     `json:"configured"`
	MPG        *float64 `json:"mpg"`
	AreaName   *string  `json:"area_name"`
	Prices     []struct {
		Week    string `json:"week"`
		Dollars string `json:"dollars"`
	} `json:"prices"`
	Stretches []struct {
		FromDay  string  `json:"from_day"`
		Gallons  float64 `json:"gallons"`
		GasPrice *string `json:"gas_price"`
		GasCost  *string `json:"gas_cost"`
		EvCost   *string `json:"ev_cost"`
		Savings  *string `json:"savings"`
	} `json:"stretches"`
	Totals *struct {
		Stretches      int     `json:"stretches"`
		Miles          float64 `json:"miles"`
		GasCost        string  `json:"gas_cost"`
		EvCost         string  `json:"ev_cost"`
		Savings        string  `json:"savings"`
		GasCostPerMile *string `json:"gas_cost_per_mile"`
		EvCostPerMile  *string `json:"ev_cost_per_mile"`
	} `json:"totals"`
}

type compareBody struct {
	MPG         *float64 `json:"mpg"`
	AreaID      *string  `json:"area_id"`
	AreaName    *string  `json:"area_name"`
	KeySaved    bool     `json:"key_saved"`
	LastOutcome *string  `json:"last_outcome"`
	LatestWeek  *string  `json:"latest_week"`
	LatestPrice *string  `json:"latest_price"`
	Areas       []struct {
		ID string `json:"id"`
	} `json:"areas"`
}

func (f *fordTest) gasSetup(t *testing.T, withKey bool) (*[]string, string) {
	t.Helper()
	eia, asked := fakeEIA(t)
	f.api.Gas = &gasprice.Client{Base: eia.URL}
	if withKey {
		if err := f.st.SetSecret(context.Background(), carbonConfigKey, `{"api_key":"`+eiaKey+`","place":"MISO"}`); err != nil {
			t.Fatal(err)
		}
	}
	patch(t, f.srv.URL+"/api/v1/meters/"+f.meter, `{"ev":true}`)
	put(t, f.srv.URL+"/api/v1/rates", `{"mode":"one","fixed_rate":"0.20","currency":"USD"}`)
	return asked, f.srv.URL + "/api/v1/meters/" + f.meter + "/ev/"
}

func (f *fordTest) odometer(t *testing.T, base, day string, miles float64) {
	t.Helper()
	body, _ := json.Marshal(map[string]float64{"miles": miles})
	if r, text := send(t, http.MethodPut, base+"odometer/"+day, string(body)); r.StatusCode != http.StatusOK {
		t.Fatalf("odometer %s: %d %s", day, r.StatusCode, text)
	}
}

func TestTheSavingsAreWhatTheSameMilesWouldHaveCostInAGasCarLessWhatChargingCost(t *testing.T) {
	f := newFordTest(t)
	asked, base := f.gasSetup(t, true)

	// Nothing set up yet: the key is there, the comparison is not.
	var c compareBody
	get(t, base+"compare", &c)
	if c.MPG != nil || !c.KeySaved || len(c.Areas) < 20 {
		t.Fatalf("compare before setup = %+v", c)
	}
	var none savingsBody
	get(t, base+"savings", &none)
	if none.Configured || len(none.Stretches) != 0 || none.Totals != nil {
		t.Fatalf("savings before setup = %+v", none)
	}

	// A comparison EIA or the user would refuse is refused.
	for name, body := range map[string]string{
		"too few miles per gallon": `{"mpg":2,"area_id":"SMN"}`,
		"too many":                 `{"mpg":500,"area_id":"SMN"}`,
		"unknown area":             `{"mpg":25,"area_id":"ZZ"}`,
		"no number":                `{"area_id":"SMN"}`,
		"an extra field":           `{"mpg":25,"area_id":"SMN","key":"x"}`,
	} {
		if r, _ := send(t, http.MethodPut, base+"compare", body); r.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", name, r.StatusCode)
		}
	}
	if len(*asked) != 0 {
		t.Errorf("EIA was asked for a refused setup: %v", *asked)
	}

	// Setting it up reads the area's prices at once.
	r, body := send(t, http.MethodPut, base+"compare", `{"mpg":25,"area_id":"SMN"}`)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("compare: %d %s", r.StatusCode, body)
	}
	_ = json.Unmarshal([]byte(body), &c)
	if c.MPG == nil || *c.MPG != 25 || c.AreaName == nil || *c.AreaName != "Minnesota" || c.LatestWeek == nil || *c.LatestWeek != "2026-10-12" || c.LatestPrice == nil || *c.LatestPrice != "4.50" {
		t.Errorf("compare after setup = %s", body)
	}
	if len(*asked) != 1 || !strings.Contains((*asked)[0], "facets%5Bduoarea%5D%5B%5D=SMN") || !strings.Contains((*asked)[0], "EPMR") {
		t.Errorf("EIA was asked %v, want regular gasoline for Minnesota", *asked)
	}
	if strings.Contains(body, eiaKey) {
		t.Errorf("the answer holds the EIA key: %s", body)
	}

	// Four odometer readings: the first stretch is before EIA's first week, so it has no gas price.
	f.odometer(t, base, "2026-09-28", 900)
	f.odometer(t, base, "2026-10-10", 1000)
	f.odometer(t, base, "2026-10-13", 1090)
	f.odometer(t, base, "2026-10-17", 1190)
	f.draw(t, time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC), 100, 6000) // 10 kWh in the second stretch
	f.draw(t, time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC), 60, 7200)  // 7.2 kWh in the third

	var got savingsBody
	get(t, base+"savings", &got)
	if !got.Configured || len(got.Stretches) != 3 {
		t.Fatalf("savings = %+v", got)
	}
	first, second, third := got.Stretches[0], got.Stretches[1], got.Stretches[2]
	if first.GasPrice != nil || first.GasCost != nil || first.Savings != nil || first.EvCost == nil {
		t.Errorf("the stretch before EIA's first week = %+v, want an electric cost and no gas figures", first)
	}
	// Oct 10 and 11 are in the week of Oct 5 (4.00), Oct 12 in the week of Oct 12 (4.50): their average. 90 miles at 25 per gallon is 3.6 gallons.
	if second.GasPrice == nil || *second.GasPrice != "4.166667" || second.Gallons != 3.6 || *second.GasCost != "15.00" || *second.EvCost != "2.00" || *second.Savings != "13.00" {
		t.Errorf("second stretch = %+v / %v %v %v", second, deref(second.GasPrice), deref(second.GasCost), deref(second.Savings))
	}
	// Oct 13 to 16 are all in the week of Oct 12 (4.50): 100 miles is 4 gallons.
	if *third.GasPrice != "4.50" || *third.GasCost != "18.00" || *third.EvCost != "1.44" || *third.Savings != "16.56" {
		t.Errorf("third stretch = %v %v %v %v", deref(third.GasPrice), deref(third.GasCost), deref(third.EvCost), deref(third.Savings))
	}
	// Only stretches with both costs are added up.
	tot := got.Totals
	if tot == nil || tot.Stretches != 2 || tot.Miles != 190 || tot.GasCost != "33.00" || tot.EvCost != "3.44" || tot.Savings != "29.56" {
		t.Fatalf("totals = %+v", tot)
	}
	if tot.GasCostPerMile == nil || *tot.GasCostPerMile != "0.173684" || *tot.EvCostPerMile != "0.018105" {
		t.Errorf("per mile = %v / %v", deref(tot.GasCostPerMile), deref(tot.EvCostPerMile))
	}
	// The chart's prices start a little before the first reading, so both weeks are in.
	if len(got.Prices) != 2 || got.Prices[1].Dollars != "4.50" {
		t.Errorf("prices = %+v", got.Prices)
	}

	// Taking the comparison off.
	if r, _ := send(t, http.MethodDelete, base+"compare", ""); r.StatusCode != http.StatusNoContent {
		t.Errorf("delete compare: %d", r.StatusCode)
	}
	get(t, base+"savings", &got)
	if got.Configured || got.Totals != nil {
		t.Errorf("savings after taking the comparison off = %+v", got)
	}
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

func TestTheComparisonIsKeptWithoutTheEIAKeyAndSaysWhatIsMissing(t *testing.T) {
	f := newFordTest(t)
	asked, base := f.gasSetup(t, false)
	r, body := send(t, http.MethodPut, base+"compare", `{"mpg":28,"area_id":"NUS"}`)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("compare: %d %s", r.StatusCode, body)
	}
	var c compareBody
	_ = json.Unmarshal([]byte(body), &c)
	if c.MPG == nil || *c.MPG != 28 || c.KeySaved || c.LastOutcome == nil || !strings.Contains(*c.LastOutcome, "key") || c.LatestPrice != nil {
		t.Errorf("compare without a key = %s", body)
	}
	if len(*asked) != 0 {
		t.Errorf("EIA was asked without a key: %v", *asked)
	}
	// Saved, but with no prices the savings have the gas car's gallons and no gas cost.
	f.odometer(t, base, "2026-10-10", 1000)
	f.odometer(t, base, "2026-10-13", 1090)
	var got savingsBody
	get(t, base+"savings", &got)
	if !got.Configured || len(got.Stretches) != 1 || got.Stretches[0].GasCost != nil || got.Totals != nil {
		t.Errorf("savings without prices = %+v", got)
	}
}

func TestAKeyEIARefusesIsSaidAndNeverShown(t *testing.T) {
	f := newFordTest(t)
	_, base := f.gasSetup(t, false)
	if err := f.st.SetSecret(context.Background(), carbonConfigKey, `{"api_key":"WRONG-KEY-99999","place":"MISO"}`); err != nil {
		t.Fatal(err)
	}
	_, body := send(t, http.MethodPut, base+"compare", `{"mpg":25,"area_id":"SMN"}`)
	var c compareBody
	_ = json.Unmarshal([]byte(body), &c)
	if c.LastOutcome == nil || !strings.Contains(*c.LastOutcome, "did not accept the key") || strings.Contains(body, "WRONG-KEY-99999") {
		t.Errorf("a refused key: %s", body)
	}
}

func TestOnlyAMeterThatChargesAVehicleHasAComparison(t *testing.T) {
	f := newFordTest(t)
	base := f.srv.URL + "/api/v1/meters/" + f.meter + "/ev/"
	for _, path := range []string{"compare", "savings"} {
		if r, _ := send(t, http.MethodGet, base+path, ""); r.StatusCode != http.StatusConflict {
			t.Errorf("GET %s on a meter not marked as charging: %d, want 409", path, r.StatusCode)
		}
	}
	if r, _ := send(t, http.MethodPut, base+"compare", `{"mpg":25,"area_id":"SMN"}`); r.StatusCode != http.StatusConflict {
		t.Errorf("PUT compare on a meter not marked as charging: %d, want 409", r.StatusCode)
	}
}
