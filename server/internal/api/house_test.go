package api

import (
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

// houseServer has two mains clamps (500 W and 300 W) and a circuit (200 W), two hours each from 12:00 to 14:00, on a clock at 19:00 on
// 22 Oct 2026, and a price of 20 cents.
func houseServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	srv, st := computerServer(t)
	ctx := t.Context()
	for _, ch := range []model.Meter{
		{ID: "mains-a", DeviceID: "d", Scope: model.ScopeMains, Label: "Mains A"},
		{ID: "mains-b", DeviceID: "d", Scope: model.ScopeMains, Label: "Mains B"},
		{ID: "dryer", DeviceID: "d", Scope: model.ScopeCircuit, Label: "Dryer"},
	} {
		if err := st.UpsertMeter(ctx, ch); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 120; i++ {
		at := time.Date(2026, 10, 22, 12, i, 0, 0, time.UTC)
		for id, w := range map[string]float64{"mains-a": 500, "mains-b": 300, "dryer": 200} {
			if err := st.AddMinute(ctx, id, at, w, w+10, 60); err != nil {
				t.Fatal(err)
			}
		}
	}
	if r, b := send(t, http.MethodPut, srv.URL+"/api/v1/rates", `{"mode":"one","fixed_rate":"0.20"}`); r.StatusCode != 200 {
		t.Fatalf("rates: %d %s", r.StatusCode, b)
	}
	return srv, st
}

func TestTheSummaryCarriesTheHousesHighestDrawAndCarbonAddedFromTheMainsOnly(t *testing.T) {
	srv, st := houseServer(t)
	_ = st.AddCarbon(t.Context(), time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC), 400, false)
	var sum model.Summary
	get(t, srv.URL+"/api/v1/summary"+period, &sum)
	// 0.8 kW for 2 h = 1.6 kWh; the peaks 510 + 310 added (an upper bound); the circuit is no part of the house figure.
	if sum.HouseKWh == nil || math.Abs(*sum.HouseKWh-1.6) > 1e-9 || sum.HousePeakWatts == nil || *sum.HousePeakWatts != 820 {
		t.Fatalf("house = %v kWh, peak %v", sum.HouseKWh, sum.HousePeakWatts)
	}
	// 12:00 has its own 400 g/kWh (0.8 kWh -> 320 g); 13:00 is valued at the average of the known hours (also 400): 640 g in all.
	if sum.HouseCarbonG == nil || math.Abs(*sum.HouseCarbonG-640) > 1e-6 || sum.HouseCarbonEstimatedKWh == nil || math.Abs(*sum.HouseCarbonEstimatedKWh-0.8) > 1e-9 {
		t.Errorf("house carbon = %v g, estimated %v kWh", sum.HouseCarbonG, sum.HouseCarbonEstimatedKWh)
	}
}

func TestTheHouseHistoryHasARowPerPeriodOfTheMainsAlone(t *testing.T) {
	srv, _ := houseServer(t)
	var h model.HouseHistory
	if r := get(t, srv.URL+"/api/v1/history", &h); r.StatusCode != 200 {
		t.Fatalf("history: %d", r.StatusCode)
	}
	if len(h.Periods) != 6 || h.Periods[0].Key != "1h" || h.Periods[5].Key != "all" {
		t.Fatalf("periods = %+v", h.Periods)
	}
	all := h.Periods[5]
	if math.Abs(all.KWh-1.6) > 1e-9 || all.Cost == nil || !strings.HasPrefix(*all.Cost, "0.32") || all.PeakWatts == nil || *all.PeakWatts != 820 {
		t.Errorf("all = %+v", all)
	}
	if h.Periods[0].KWh != 0 { // nothing in the last hour: the clock is at 19:00
		t.Errorf("last hour = %v kWh", h.Periods[0].KWh)
	}
}

func TestTheHouseEstimateCountsEveryHourOfTheTimeRecorded(t *testing.T) {
	srv, _ := houseServer(t)
	var e model.ComputerEstimate
	get(t, srv.URL+"/api/v1/estimate?window=24h", &e)
	// 1.6 kWh over the 7 h since recording began (12:00 to 19:00).
	if e.Rate == nil || *e.Rate != "0.20" || e.Usage == nil || math.Abs(e.Usage.Days-7.0/24) > 1e-9 || math.Abs(e.Usage.KWhPerDay-1.6/(7.0/24)) > 1e-9 {
		t.Errorf("estimate = %+v, usage %+v", e, e.Usage)
	}
	if r := get(t, srv.URL+"/api/v1/estimate?window=fortnight", nil); r.StatusCode != http.StatusBadRequest {
		t.Errorf("bad window: %d", r.StatusCode)
	}
}

func TestTheHouseExportIsADailyCsvOfTheHouseAndEachCircuit(t *testing.T) {
	srv, _ := houseServer(t)
	resp, err := http.Get(srv.URL + "/api/v1/export.csv" + period)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	rows := lines(string(b))
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/csv") || len(rows) != 2 {
		t.Fatalf("status %d, rows %q", resp.StatusCode, rows)
	}
	if rows[0] != "date,house_kwh,Dryer (kwh),house_cost" || rows[1] != "2026-10-22,1.6,0.4,0.3200" {
		t.Errorf("rows = %q", rows)
	}
	if r := get(t, srv.URL+"/api/v1/export.csv?from=nope&to=x", nil); r.StatusCode != http.StatusBadRequest {
		t.Errorf("bad period: %d", r.StatusCode)
	}
}
