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

// fakeEIA answers the weekly prices of any area with two weeks, gasoline at 4.00 then 4.50 and diesel at 4.80 then 5.20, and remembers what it was asked.
// With noDiesel it has no diesel prices (it answers 404 for them), as for an area EIA does not publish them for.
func fakeEIA(t *testing.T, noDiesel bool) (*httptest.Server, *[]string) {
	t.Helper()
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.RawQuery)
		if r.URL.Query().Get("api_key") != eiaKey {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.URL.Query().Get("facets[product][]") == string(gasprice.Diesel) {
			if noDiesel {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(`{"response":{"data":[{"period":"2026-10-12","value":"5.20"},{"period":"2026-10-05","value":"4.80"}]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"response":{"data":[{"period":"2026-10-12","value":"4.50"},{"period":"2026-10-05","value":"4.00"}]}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &asked
}

type savingsGas struct {
	Fuel    string  `json:"fuel"`
	MPG     float64 `json:"mpg"`
	Gallons float64 `json:"gallons"`
	GasCost *string `json:"gas_cost"`
	Savings *string `json:"savings"`
}

type savingsBody struct {
	Configured     bool    `json:"configured"`
	AreaName       *string `json:"area_name"`
	DieselAreaName *string `json:"diesel_area_name"`
	Comparisons    []struct {
		Fuel   string  `json:"fuel"`
		MPG    float64 `json:"mpg"`
		Custom bool    `json:"custom"`
	} `json:"comparisons"`
	Prices []struct {
		Week    string `json:"week"`
		Dollars string `json:"dollars"`
	} `json:"prices"`
	DieselPrices []struct {
		Week    string `json:"week"`
		Dollars string `json:"dollars"`
	} `json:"diesel_prices"`
	Stretches []struct {
		FromDay     string       `json:"from_day"`
		GasPrice    *string      `json:"gas_price"`
		DieselPrice *string      `json:"diesel_price"`
		EvCost      *string      `json:"ev_cost"`
		Gas         []savingsGas `json:"gas"`
	} `json:"stretches"`
	Totals *struct {
		Stretches     int     `json:"stretches"`
		Miles         float64 `json:"miles"`
		EvCost        string  `json:"ev_cost"`
		EvCostPerMile *string `json:"ev_cost_per_mile"`
		Gas           []struct {
			Fuel           string  `json:"fuel"`
			MPG            float64 `json:"mpg"`
			Gallons        float64 `json:"gallons"`
			GasCost        string  `json:"gas_cost"`
			Savings        string  `json:"savings"`
			GasCostPerMile *string `json:"gas_cost_per_mile"`
		} `json:"gas"`
	} `json:"totals"`
}

// mpgs are the miles per gallon of the cars compared in a fuel, in the order answered.
func (b savingsBody) mpgs(fuel string) []float64 {
	out := []float64{}
	for _, c := range b.Comparisons {
		if c.Fuel == fuel {
			out = append(out, c.MPG)
		}
	}
	return out
}

type compareBody struct {
	AreaID            *string   `json:"area_id"`
	AreaName          *string   `json:"area_name"`
	DieselAreaName    *string   `json:"diesel_area_name"`
	CustomMPG         *float64  `json:"custom_mpg"`
	Presets           []float64 `json:"presets"`
	KeySaved          bool      `json:"key_saved"`
	LastOutcome       *string   `json:"last_outcome"`
	LatestWeek        *string   `json:"latest_week"`
	LatestPrice       *string   `json:"latest_price"`
	LatestDieselPrice *string   `json:"latest_diesel_price"`
	Areas             []struct {
		ID string `json:"id"`
	} `json:"areas"`
}

func (f *fordTest) gasSetup(t *testing.T, withKey bool) (*[]string, string) {
	return f.fuelSetup(t, withKey, false)
}

func (f *fordTest) fuelSetup(t *testing.T, withKey, noDiesel bool) (*[]string, string) {
	t.Helper()
	eia, asked := fakeEIA(t, noDiesel)
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

func gasFor(t *testing.T, g []savingsGas, fuel string, mpg float64) savingsGas {
	t.Helper()
	for _, x := range g {
		if x.Fuel == fuel && x.MPG == mpg {
			return x
		}
	}
	t.Fatalf("no %s comparison for %v mpg in %+v", fuel, mpg, g)
	return savingsGas{}
}

func TestEachCarsCostIsWhatTheSameMilesWouldHaveCostLessWhatChargingCost(t *testing.T) {
	f := newFordTest(t)
	asked, base := f.gasSetup(t, true)

	// Nothing set up yet: the key is there, the area is not, and the presets are known.
	var c compareBody
	get(t, base+"compare", &c)
	if c.AreaID != nil || !c.KeySaved || len(c.Areas) < 20 || len(c.Presets) != 6 || c.Presets[0] != 15 || c.Presets[5] != 40 {
		t.Fatalf("compare before setup = %+v", c)
	}
	var none savingsBody
	get(t, base+"savings", &none)
	if none.Configured || len(none.Stretches) != 0 || none.Totals != nil || len(none.Comparisons) != 0 {
		t.Fatalf("savings before setup = %+v", none)
	}

	// A setup EIA or the user would refuse is refused, and so is the old one-gas-car form.
	for name, body := range map[string]string{
		"too few miles per gallon": `{"area_id":"SMN","custom_mpg":2}`,
		"too many":                 `{"area_id":"SMN","custom_mpg":500}`,
		"unknown area":             `{"area_id":"ZZ"}`,
		"no area":                  `{"custom_mpg":25}`,
		"the old form":             `{"area_id":"SMN","mpg":25}`,
		"an extra field":           `{"area_id":"SMN","key":"x"}`,
	} {
		if r, _ := send(t, http.MethodPut, base+"compare", body); r.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", name, r.StatusCode)
		}
	}
	if len(*asked) != 0 {
		t.Errorf("EIA was asked for a refused setup: %v", *asked)
	}

	// The area alone is enough: the presets are always compared. Setting it up reads the area's prices of both fuels at once.
	r, body := send(t, http.MethodPut, base+"compare", `{"area_id":"SMN"}`)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("compare: %d %s", r.StatusCode, body)
	}
	_ = json.Unmarshal([]byte(body), &c)
	if c.AreaName == nil || *c.AreaName != "Minnesota" || c.CustomMPG != nil || c.LatestWeek == nil || *c.LatestWeek != "2026-10-12" || c.LatestPrice == nil || *c.LatestPrice != "4.50" {
		t.Errorf("compare after setup = %s", body)
	}
	if c.LatestDieselPrice == nil || *c.LatestDieselPrice != "5.20" || c.LastOutcome == nil || *c.LastOutcome != "Prices saved." {
		t.Errorf("diesel after setup = %v / %v", deref(c.LatestDieselPrice), deref(c.LastOutcome))
	}
	if len(*asked) != 2 || !strings.Contains((*asked)[0], "EPMR") || !strings.Contains((*asked)[1], "EPD2D") {
		t.Errorf("EIA was asked %v, want regular gasoline then No. 2 diesel", *asked)
	}
	// Gasoline is read for Minnesota; EIA publishes no diesel for a state, so diesel is read for the Midwest, the region Minnesota is in.
	if !strings.Contains((*asked)[0], "facets%5Bduoarea%5D%5B%5D=SMN") || !strings.Contains((*asked)[1], "facets%5Bduoarea%5D%5B%5D=R20") {
		t.Errorf("the requests %v, want gasoline for Minnesota and diesel for the Midwest", *asked)
	}
	if c.DieselAreaName == nil || *c.DieselAreaName != "Midwest (PADD 2)" {
		t.Errorf("diesel area = %v", deref(c.DieselAreaName))
	}
	if strings.Contains(body, eiaKey) {
		t.Errorf("the answer holds the EIA key: %s", body)
	}

	// Four odometer readings: the first stretch is before EIA's first week, so it has no price of either fuel.
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
	want := []float64{15, 20, 25, 30, 35, 40}
	for _, fuel := range []string{"gas", "diesel"} {
		if m := got.mpgs(fuel); len(m) != 6 || m[0] != want[0] || m[5] != want[5] {
			t.Fatalf("%s comparisons = %v, want the six presets", fuel, m)
		}
	}
	if len(got.Comparisons) != 12 || got.Comparisons[0].Fuel != "gas" || got.Comparisons[6].Fuel != "diesel" {
		t.Fatalf("comparisons = %+v, want the gas cars then the diesel cars", got.Comparisons)
	}
	for _, comp := range got.Comparisons {
		if comp.Custom {
			t.Errorf("%v mpg %s is marked custom", comp.MPG, comp.Fuel)
		}
	}
	if len(got.DieselPrices) != 2 || got.DieselPrices[1].Dollars != "5.20" || got.DieselAreaName == nil || *got.DieselAreaName != "Midwest (PADD 2)" {
		t.Errorf("diesel prices = %+v for %v", got.DieselPrices, deref(got.DieselAreaName))
	}
	first, second, third := got.Stretches[0], got.Stretches[1], got.Stretches[2]
	// The stretch before EIA's first week has an electric cost and no fuel figures, for every car.
	if first.GasPrice != nil || first.DieselPrice != nil || first.EvCost == nil || len(first.Gas) != 12 {
		t.Fatalf("the stretch before EIA's first week = %+v", first)
	}
	for _, g := range first.Gas {
		if g.GasCost != nil || g.Savings != nil || g.Gallons <= 0 {
			t.Errorf("a fuel figure before the first price: %+v", g)
		}
	}
	// Oct 10 and 11 are in the week of Oct 5, Oct 12 in the week of Oct 12: gasoline averages 4.00, 4.00, 4.50 and diesel 4.80, 4.80, 5.20. 90 miles.
	if second.GasPrice == nil || *second.GasPrice != "4.166667" || deref(second.DieselPrice) != "4.933333" || *second.EvCost != "2.00" {
		t.Fatalf("second stretch = %v %v %v", deref(second.GasPrice), deref(second.DieselPrice), deref(second.EvCost))
	}
	for _, w := range []struct {
		fuel          string
		mpg           float64
		cost, savings string
	}{{"gas", 15, "25.00", "23.00"}, {"gas", 25, "15.00", "13.00"}, {"gas", 40, "9.375", "7.375"}, {"diesel", 25, "17.76", "15.76"}, {"diesel", 40, "11.10", "9.10"}} {
		g := gasFor(t, second.Gas, w.fuel, w.mpg)
		if deref(g.GasCost) != w.cost || deref(g.Savings) != w.savings {
			t.Errorf("second stretch at %v mpg %s = cost %v savings %v, want %v and %v", w.mpg, w.fuel, deref(g.GasCost), deref(g.Savings), w.cost, w.savings)
		}
	}
	if g := gasFor(t, second.Gas, "gas", 25); g.Gallons != 3.6 {
		t.Errorf("90 miles at 25 per gallon is %v gallons", g.Gallons)
	}
	// Oct 13 to 16 are all in the week of Oct 12: 100 miles at 25 per gallon is 4 gallons, at 4.50 for gasoline and 5.20 for diesel.
	if *third.GasPrice != "4.50" || deref(third.DieselPrice) != "5.20" || deref(gasFor(t, third.Gas, "gas", 25).GasCost) != "18.00" || deref(gasFor(t, third.Gas, "gas", 25).Savings) != "16.56" ||
		deref(gasFor(t, third.Gas, "diesel", 25).GasCost) != "20.80" || deref(gasFor(t, third.Gas, "diesel", 25).Savings) != "19.36" {
		t.Errorf("third stretch = %v %v %+v %+v", deref(third.GasPrice), deref(third.DieselPrice), gasFor(t, third.Gas, "gas", 25), gasFor(t, third.Gas, "diesel", 25))
	}

	// Only stretches with every price and the electric cost are added up, the same ones for every car.
	tot := got.Totals
	if tot == nil || tot.Stretches != 2 || tot.Miles != 190 || tot.EvCost != "3.44" || len(tot.Gas) != 12 {
		t.Fatalf("totals = %+v", tot)
	}
	if tot.EvCostPerMile == nil || *tot.EvCostPerMile != "0.018105" {
		t.Errorf("electric cost per mile = %v", deref(tot.EvCostPerMile))
	}
	for _, w := range []struct {
		fuel                  string
		mpg                   float64
		gas, savings, perMile string
	}{{"gas", 15, "55.00", "51.56", "0.289474"}, {"gas", 25, "33.00", "29.56", "0.173684"}, {"gas", 40, "20.625", "17.185", "0.108553"}, {"diesel", 25, "38.56", "35.12", "0.202947"}} {
		var found bool
		for _, g := range tot.Gas {
			if g.Fuel == w.fuel && g.MPG == w.mpg {
				found = true
				if g.GasCost != w.gas || g.Savings != w.savings || deref(g.GasCostPerMile) != w.perMile {
					t.Errorf("totals at %v mpg %s = %+v, want %v / %v / %v", w.mpg, w.fuel, g, w.gas, w.savings, w.perMile)
				}
			}
		}
		if !found {
			t.Errorf("no totals for %v mpg %s", w.mpg, w.fuel)
		}
	}
	// The chart's prices start a little before the first reading, so both weeks are in.
	if len(got.Prices) != 2 || got.Prices[1].Dollars != "4.50" {
		t.Errorf("prices = %+v", got.Prices)
	}

	// Taking the vehicle's own car off leaves the house's area, so the comparison stands.
	if r, _ := send(t, http.MethodDelete, base+"compare", ""); r.StatusCode != http.StatusNoContent {
		t.Errorf("delete compare: %d", r.StatusCode)
	}
	get(t, base+"savings", &got)
	if !got.Configured || got.Totals == nil {
		t.Errorf("savings after taking the car off = %+v", got)
	}
	// Taking the house's area off takes every comparison off.
	fuel := strings.TrimSuffix(base, "/meters/"+f.meter+"/ev/") + "/fuel-prices"
	if r, _ := send(t, http.MethodDelete, fuel, ""); r.StatusCode != http.StatusNoContent {
		t.Errorf("delete fuel prices: %d", r.StatusCode)
	}
	get(t, base+"savings", &got)
	if got.Configured || got.Totals != nil || len(got.Comparisons) != 0 {
		t.Errorf("savings after taking the area off = %+v", got)
	}
}

func TestTheGasAreaIsTheHousesAndEveryVehicleUsesIt(t *testing.T) {
	f := newFordTest(t)
	asked, base := f.gasSetup(t, true)
	fuel := f.srv.URL + "/api/v1/fuel-prices"

	// No area yet: the places to choose from and the key are known, and a vehicle's own car cannot be saved before there is somewhere to price it.
	var v compareBody
	get(t, fuel, &v)
	if v.AreaID != nil || !v.KeySaved || len(v.Areas) < 20 {
		t.Fatalf("fuel prices before setup = %+v", v)
	}
	if r, _ := send(t, http.MethodPut, base+"compare", `{"custom_mpg":27.5}`); r.StatusCode != http.StatusBadRequest {
		t.Errorf("a car with no area: %d, want 400", r.StatusCode)
	}
	for name, body := range map[string]string{"unknown area": `{"area_id":"ZZ"}`, "no area": `{}`, "an extra field": `{"area_id":"SMN","mpg":25}`} {
		if r, _ := send(t, http.MethodPut, fuel, body); r.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", name, r.StatusCode)
		}
	}
	if len(*asked) != 0 {
		t.Errorf("EIA was asked for a refused area: %v", *asked)
	}

	// Saving the area reads its prices of both fuels at once, and the vehicle sees the area without being told.
	r, body := send(t, http.MethodPut, fuel, `{"area_id":"SMN"}`)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("fuel prices: %d %s", r.StatusCode, body)
	}
	_ = json.Unmarshal([]byte(body), &v)
	if v.AreaName == nil || *v.AreaName != "Minnesota" || v.LatestPrice == nil || *v.LatestPrice != "4.50" || v.LatestDieselPrice == nil || *v.LatestDieselPrice != "5.20" {
		t.Errorf("fuel prices after setup = %s", body)
	}
	if len(*asked) != 2 {
		t.Errorf("EIA was asked %v, want gasoline then diesel", *asked)
	}
	var c compareBody
	get(t, base+"compare", &c)
	if c.AreaName == nil || *c.AreaName != "Minnesota" || c.LatestPrice == nil || *c.LatestPrice != "4.50" {
		t.Errorf("the vehicle's comparison does not see the house's area: %+v", c)
	}

	// The vehicle keeps only its own car, and saving it does not touch the area or ask EIA again.
	r, body = send(t, http.MethodPut, base+"compare", `{"custom_mpg":27.5}`)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("own car: %d %s", r.StatusCode, body)
	}
	_ = json.Unmarshal([]byte(body), &c)
	if c.CustomMPG == nil || *c.CustomMPG != 27.5 || c.AreaName == nil || *c.AreaName != "Minnesota" {
		t.Errorf("own car = %s", body)
	}
	if len(*asked) != 2 {
		t.Errorf("EIA was asked again for a car: %v", *asked)
	}

	// An area chosen again replaces it for the house, whatever the vehicle did.
	if r, body := send(t, http.MethodPut, fuel, `{"area_id":"NUS"}`); r.StatusCode != http.StatusOK {
		t.Fatalf("another area: %d %s", r.StatusCode, body)
	}
	get(t, base+"compare", &c)
	if c.AreaID == nil || *c.AreaID != "NUS" || c.CustomMPG == nil || *c.CustomMPG != 27.5 {
		t.Errorf("after another area = %+v", c)
	}

	// Taking the area off keeps the car for when there is an area again.
	if r, _ := send(t, http.MethodDelete, fuel, ""); r.StatusCode != http.StatusNoContent {
		t.Errorf("delete: %d", r.StatusCode)
	}
	get(t, fuel, &v)
	get(t, base+"compare", &c)
	if v.AreaID != nil || c.AreaID != nil || c.CustomMPG == nil || *c.CustomMPG != 27.5 {
		t.Errorf("after taking the area off = %+v / %+v", v, c)
	}
}

func TestAnAreaFromAnOlderVehicleSetupIsTheHousesUntilOneIsSaved(t *testing.T) {
	f := newFordTest(t)
	_, base := f.gasSetup(t, true)
	if err := f.st.SetMeta(context.Background(), evCompareKey+f.meter, `{"area_id":"SMN","custom_mpg":27.5}`); err != nil {
		t.Fatal(err)
	}
	fuel := f.srv.URL + "/api/v1/fuel-prices"
	var v compareBody
	get(t, fuel, &v)
	if v.AreaName == nil || *v.AreaName != "Minnesota" {
		t.Errorf("the house's area from the older setup = %+v", v)
	}
	// Taking the area off takes it off the older setup too, or it would come back.
	if r, _ := send(t, http.MethodDelete, fuel, ""); r.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", r.StatusCode)
	}
	var c compareBody
	get(t, base+"compare", &c)
	if c.AreaID != nil || c.CustomMPG == nil || *c.CustomMPG != 27.5 {
		t.Errorf("after taking the older area off = %+v", c)
	}
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

func TestWithoutDieselPricesOnlyTheGasCarsAreComparedAndTheOutcomeSaysSo(t *testing.T) {
	f := newFordTest(t)
	_, base := f.fuelSetup(t, true, true)
	r, body := send(t, http.MethodPut, base+"compare", `{"area_id":"SMN"}`)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("compare: %d %s", r.StatusCode, body)
	}
	var c compareBody
	_ = json.Unmarshal([]byte(body), &c)
	if c.LatestPrice == nil || c.LatestDieselPrice != nil || c.LastOutcome == nil || !strings.Contains(*c.LastOutcome, "Diesel not read") {
		t.Errorf("compare without diesel = %s", body)
	}
	f.odometer(t, base, "2026-10-10", 1000)
	f.odometer(t, base, "2026-10-13", 1090)
	var got savingsBody
	get(t, base+"savings", &got)
	if len(got.Comparisons) != 6 || len(got.DieselPrices) != 0 || got.Stretches[0].DieselPrice != nil {
		t.Errorf("savings without diesel = %+v", got.Comparisons)
	}
	for _, comp := range got.Comparisons {
		if comp.Fuel != "gas" {
			t.Errorf("a %s comparison without diesel prices", comp.Fuel)
		}
	}
	if got.Totals == nil || len(got.Totals.Gas) != 6 {
		t.Errorf("the gas cars' totals stand without diesel: %+v", got.Totals)
	}
}

func TestACarOfTheOwnersOwnTakesItsPlaceAmongThePresetsInEachFuel(t *testing.T) {
	f := newFordTest(t)
	_, base := f.gasSetup(t, true)
	_, body := send(t, http.MethodPut, base+"compare", `{"area_id":"SMN","custom_mpg":27.5}`)
	var c compareBody
	_ = json.Unmarshal([]byte(body), &c)
	if c.CustomMPG == nil || *c.CustomMPG != 27.5 {
		t.Fatalf("compare = %s", body)
	}
	f.odometer(t, base, "2026-10-10", 1000)
	f.odometer(t, base, "2026-10-13", 1090)
	var got savingsBody
	get(t, base+"savings", &got)
	for _, fuel := range []string{"gas", "diesel"} {
		if m := got.mpgs(fuel); len(m) != 7 || m[0] != 15 || m[3] != 27.5 || m[4] != 30 || m[6] != 40 {
			t.Fatalf("%s comparisons = %v, want the presets with 27.5 between 25 and 30", fuel, m)
		}
	}
	for _, comp := range got.Comparisons {
		if comp.Custom != (comp.MPG == 27.5) {
			t.Errorf("%v mpg %s has custom = %v", comp.MPG, comp.Fuel, comp.Custom)
		}
	}
	if len(got.Stretches[0].Gas) != 14 || got.Stretches[0].Gas[3].MPG != 27.5 || got.Stretches[0].Gas[10].Fuel != "diesel" || got.Stretches[0].Gas[10].MPG != 27.5 {
		t.Errorf("a stretch's costs are not in the comparisons' order: %+v", got.Stretches[0].Gas)
	}

	// A custom car that is a preset is not a second line.
	_, body = send(t, http.MethodPut, base+"compare", `{"area_id":"SMN","custom_mpg":25}`)
	_ = json.Unmarshal([]byte(body), &c)
	get(t, base+"savings", &got)
	if c.CustomMPG != nil || len(got.Comparisons) != 12 {
		t.Errorf("a custom 25 mpg: custom %v, %d comparisons", c.CustomMPG, len(got.Comparisons))
	}
}

func TestASetupSavedWithOneGasCarStillReadsAndKeepsThatCar(t *testing.T) {
	f := newFordTest(t)
	_, base := f.gasSetup(t, true)
	ctx := context.Background()
	// What was saved before the presets: one miles per gallon and the area. The prices are read with the first comparison asked for.
	if r, body := send(t, http.MethodPut, base+"compare", `{"area_id":"SMN"}`); r.StatusCode != http.StatusOK {
		t.Fatalf("compare: %d %s", r.StatusCode, body)
	}
	if err := f.st.SetMeta(ctx, evCompareKey+f.meter, `{"mpg":28,"area_id":"SMN"}`); err != nil {
		t.Fatal(err)
	}
	var got savingsBody
	get(t, base+"savings", &got)
	if !got.Configured || len(got.Comparisons) != 14 {
		t.Fatalf("an older setup with 28 mpg: %+v", got.Comparisons)
	}
	for _, comp := range got.Comparisons {
		if comp.Custom != (comp.MPG == 28) {
			t.Errorf("%v mpg %s has custom = %v", comp.MPG, comp.Fuel, comp.Custom)
		}
	}
	// One that was a preset is already there.
	if err := f.st.SetMeta(ctx, evCompareKey+f.meter, `{"mpg":30,"area_id":"SMN"}`); err != nil {
		t.Fatal(err)
	}
	get(t, base+"savings", &got)
	if len(got.Comparisons) != 12 {
		t.Errorf("an older setup with 30 mpg: %d comparisons, want the presets of both fuels", len(got.Comparisons))
	}
}

func TestTheComparisonIsKeptWithoutTheEIAKeyAndSaysWhatIsMissing(t *testing.T) {
	f := newFordTest(t)
	asked, base := f.gasSetup(t, false)
	r, body := send(t, http.MethodPut, base+"compare", `{"area_id":"NUS"}`)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("compare: %d %s", r.StatusCode, body)
	}
	var c compareBody
	_ = json.Unmarshal([]byte(body), &c)
	if c.AreaID == nil || *c.AreaID != "NUS" || c.KeySaved || c.LastOutcome == nil || !strings.Contains(*c.LastOutcome, "key") || c.LatestPrice != nil {
		t.Errorf("compare without a key = %s", body)
	}
	if len(*asked) != 0 {
		t.Errorf("EIA was asked without a key: %v", *asked)
	}
	// Saved, but with no prices the savings have the cars' gallons and no cost.
	f.odometer(t, base, "2026-10-10", 1000)
	f.odometer(t, base, "2026-10-13", 1090)
	var got savingsBody
	get(t, base+"savings", &got)
	if !got.Configured || len(got.Stretches) != 1 || len(got.Stretches[0].Gas) != 6 || got.Stretches[0].Gas[0].GasCost != nil || got.Totals != nil {
		t.Errorf("savings without prices = %+v", got)
	}
}

func TestAKeyEIARefusesIsSaidAndNeverShown(t *testing.T) {
	f := newFordTest(t)
	asked, base := f.gasSetup(t, false)
	if err := f.st.SetSecret(context.Background(), carbonConfigKey, `{"api_key":"WRONG-KEY-99999","place":"MISO"}`); err != nil {
		t.Fatal(err)
	}
	_, body := send(t, http.MethodPut, base+"compare", `{"area_id":"SMN"}`)
	var c compareBody
	_ = json.Unmarshal([]byte(body), &c)
	if c.LastOutcome == nil || !strings.Contains(*c.LastOutcome, "did not accept the key") || strings.Contains(body, "WRONG-KEY-99999") {
		t.Errorf("a refused key: %s", body)
	}
	// A key refused for gasoline is not asked again for diesel.
	if len(*asked) != 1 {
		t.Errorf("EIA was asked %d times with a key it refused, want once: %v", len(*asked), *asked)
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
	if r, _ := send(t, http.MethodPut, base+"compare", `{"area_id":"SMN"}`); r.StatusCode != http.StatusConflict {
		t.Errorf("PUT compare on a meter not marked as charging: %d, want 409", r.StatusCode)
	}
}
