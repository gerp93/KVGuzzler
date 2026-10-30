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

type savingsGas struct {
	MPG     float64 `json:"mpg"`
	Gallons float64 `json:"gallons"`
	GasCost *string `json:"gas_cost"`
	Savings *string `json:"savings"`
}

type savingsBody struct {
	Configured  bool    `json:"configured"`
	AreaName    *string `json:"area_name"`
	Comparisons []struct {
		MPG    float64 `json:"mpg"`
		Custom bool    `json:"custom"`
	} `json:"comparisons"`
	Prices []struct {
		Week    string `json:"week"`
		Dollars string `json:"dollars"`
	} `json:"prices"`
	Stretches []struct {
		FromDay  string       `json:"from_day"`
		GasPrice *string      `json:"gas_price"`
		EvCost   *string      `json:"ev_cost"`
		Gas      []savingsGas `json:"gas"`
	} `json:"stretches"`
	Totals *struct {
		Stretches     int     `json:"stretches"`
		Miles         float64 `json:"miles"`
		EvCost        string  `json:"ev_cost"`
		EvCostPerMile *string `json:"ev_cost_per_mile"`
		Gas           []struct {
			MPG            float64 `json:"mpg"`
			Gallons        float64 `json:"gallons"`
			GasCost        string  `json:"gas_cost"`
			Savings        string  `json:"savings"`
			GasCostPerMile *string `json:"gas_cost_per_mile"`
		} `json:"gas"`
	} `json:"totals"`
}

func (b savingsBody) mpgs() []float64 {
	out := []float64{}
	for _, c := range b.Comparisons {
		out = append(out, c.MPG)
	}
	return out
}

type compareBody struct {
	AreaID      *string   `json:"area_id"`
	AreaName    *string   `json:"area_name"`
	CustomMPG   *float64  `json:"custom_mpg"`
	Presets     []float64 `json:"presets"`
	KeySaved    bool      `json:"key_saved"`
	LastOutcome *string   `json:"last_outcome"`
	LatestWeek  *string   `json:"latest_week"`
	LatestPrice *string   `json:"latest_price"`
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

func gasFor(t *testing.T, g []savingsGas, mpg float64) savingsGas {
	t.Helper()
	for _, x := range g {
		if x.MPG == mpg {
			return x
		}
	}
	t.Fatalf("no comparison for %v mpg in %+v", mpg, g)
	return savingsGas{}
}

func TestEachGasCarsCostIsWhatTheSameMilesWouldHaveCostLessWhatChargingCost(t *testing.T) {
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

	// The area alone is enough: the presets are always compared. Setting it up reads the area's prices at once.
	r, body := send(t, http.MethodPut, base+"compare", `{"area_id":"SMN"}`)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("compare: %d %s", r.StatusCode, body)
	}
	_ = json.Unmarshal([]byte(body), &c)
	if c.AreaName == nil || *c.AreaName != "Minnesota" || c.CustomMPG != nil || c.LatestWeek == nil || *c.LatestWeek != "2026-10-12" || c.LatestPrice == nil || *c.LatestPrice != "4.50" {
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
	want := []float64{15, 20, 25, 30, 35, 40}
	if m := got.mpgs(); len(m) != 6 || m[0] != want[0] || m[5] != want[5] {
		t.Fatalf("comparisons = %v, want the six presets", m)
	}
	for _, comp := range got.Comparisons {
		if comp.Custom {
			t.Errorf("%v mpg is marked custom", comp.MPG)
		}
	}
	first, second, third := got.Stretches[0], got.Stretches[1], got.Stretches[2]
	// The stretch before EIA's first week has an electric cost and no gas figures, for every gas car.
	if first.GasPrice != nil || first.EvCost == nil || len(first.Gas) != 6 {
		t.Fatalf("the stretch before EIA's first week = %+v", first)
	}
	for _, g := range first.Gas {
		if g.GasCost != nil || g.Savings != nil || g.Gallons <= 0 {
			t.Errorf("a gas figure before the first price: %+v", g)
		}
	}
	// Oct 10 and 11 are in the week of Oct 5 (4.00), Oct 12 in the week of Oct 12 (4.50): their average. 90 miles.
	if second.GasPrice == nil || *second.GasPrice != "4.166667" || *second.EvCost != "2.00" {
		t.Fatalf("second stretch = %v %v", deref(second.GasPrice), deref(second.EvCost))
	}
	for mpg, want := range map[float64][3]string{15: {"25.00", "23.00", ""}, 25: {"15.00", "13.00", ""}, 40: {"9.375", "7.375", ""}} {
		g := gasFor(t, second.Gas, mpg)
		if deref(g.GasCost) != want[0] || deref(g.Savings) != want[1] {
			t.Errorf("second stretch at %v mpg = cost %v savings %v, want %v and %v", mpg, deref(g.GasCost), deref(g.Savings), want[0], want[1])
		}
	}
	if g := gasFor(t, second.Gas, 25); g.Gallons != 3.6 {
		t.Errorf("90 miles at 25 per gallon is %v gallons", g.Gallons)
	}
	// Oct 13 to 16 are all in the week of Oct 12 (4.50): 100 miles at 25 per gallon is 4 gallons.
	if *third.GasPrice != "4.50" || deref(gasFor(t, third.Gas, 25).GasCost) != "18.00" || deref(gasFor(t, third.Gas, 25).Savings) != "16.56" {
		t.Errorf("third stretch = %v %+v", deref(third.GasPrice), gasFor(t, third.Gas, 25))
	}

	// Only stretches with both costs are added up, the same ones for every gas car.
	tot := got.Totals
	if tot == nil || tot.Stretches != 2 || tot.Miles != 190 || tot.EvCost != "3.44" || len(tot.Gas) != 6 {
		t.Fatalf("totals = %+v", tot)
	}
	if tot.EvCostPerMile == nil || *tot.EvCostPerMile != "0.018105" {
		t.Errorf("electric cost per mile = %v", deref(tot.EvCostPerMile))
	}
	for _, w := range []struct {
		mpg                   float64
		gas, savings, perMile string
	}{{15, "55.00", "51.56", "0.289474"}, {25, "33.00", "29.56", "0.173684"}, {40, "20.625", "17.185", "0.108553"}} {
		var found bool
		for _, g := range tot.Gas {
			if g.MPG == w.mpg {
				found = true
				if g.GasCost != w.gas || g.Savings != w.savings || deref(g.GasCostPerMile) != w.perMile {
					t.Errorf("totals at %v mpg = %+v, want %v / %v / %v", w.mpg, g, w.gas, w.savings, w.perMile)
				}
			}
		}
		if !found {
			t.Errorf("no totals for %v mpg", w.mpg)
		}
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
	if got.Configured || got.Totals != nil || len(got.Comparisons) != 0 {
		t.Errorf("savings after taking the comparison off = %+v", got)
	}
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

func TestAGasCarOfTheOwnersOwnTakesItsPlaceAmongThePresets(t *testing.T) {
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
	if m := got.mpgs(); len(m) != 7 || m[0] != 15 || m[3] != 27.5 || m[4] != 30 || m[6] != 40 {
		t.Fatalf("comparisons = %v, want the presets with 27.5 between 25 and 30", m)
	}
	for _, comp := range got.Comparisons {
		if comp.Custom != (comp.MPG == 27.5) {
			t.Errorf("%v mpg has custom = %v", comp.MPG, comp.Custom)
		}
	}
	if len(got.Stretches[0].Gas) != 7 || got.Stretches[0].Gas[3].MPG != 27.5 {
		t.Errorf("a stretch's gas costs are not in the comparisons' order: %+v", got.Stretches[0].Gas)
	}

	// A custom car that is a preset is not a second line.
	_, body = send(t, http.MethodPut, base+"compare", `{"area_id":"SMN","custom_mpg":25}`)
	_ = json.Unmarshal([]byte(body), &c)
	get(t, base+"savings", &got)
	if c.CustomMPG != nil || len(got.Comparisons) != 6 {
		t.Errorf("a custom 25 mpg: custom %v, %d comparisons", c.CustomMPG, len(got.Comparisons))
	}
}

func TestASetupSavedWithOneGasCarStillReadsAndKeepsThatCar(t *testing.T) {
	f := newFordTest(t)
	_, base := f.gasSetup(t, true)
	ctx := context.Background()
	// What was saved before the presets: one miles per gallon and the area.
	if err := f.st.SetMeta(ctx, evCompareKey+f.meter, `{"mpg":28,"area_id":"SMN"}`); err != nil {
		t.Fatal(err)
	}
	var got savingsBody
	get(t, base+"savings", &got)
	if !got.Configured || len(got.Comparisons) != 7 {
		t.Fatalf("an older setup with 28 mpg: %+v", got.Comparisons)
	}
	for _, comp := range got.Comparisons {
		if comp.Custom != (comp.MPG == 28) {
			t.Errorf("%v mpg has custom = %v", comp.MPG, comp.Custom)
		}
	}
	// One that was a preset is already there.
	if err := f.st.SetMeta(ctx, evCompareKey+f.meter, `{"mpg":30,"area_id":"SMN"}`); err != nil {
		t.Fatal(err)
	}
	get(t, base+"savings", &got)
	if len(got.Comparisons) != 6 {
		t.Errorf("an older setup with 30 mpg: %d comparisons, want the six presets", len(got.Comparisons))
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
	// Saved, but with no prices the savings have the gas cars' gallons and no gas cost.
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
	_, base := f.gasSetup(t, false)
	if err := f.st.SetSecret(context.Background(), carbonConfigKey, `{"api_key":"WRONG-KEY-99999","place":"MISO"}`); err != nil {
		t.Fatal(err)
	}
	_, body := send(t, http.MethodPut, base+"compare", `{"area_id":"SMN"}`)
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
	if r, _ := send(t, http.MethodPut, base+"compare", `{"area_id":"SMN"}`); r.StatusCode != http.StatusConflict {
		t.Errorf("PUT compare on a meter not marked as charging: %d, want 409", r.StatusCode)
	}
}
