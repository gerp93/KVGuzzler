package api

import (
	"context"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/model"
)

func sp(day, h, m int, pct float64) socPoint {
	return socPoint{time.Date(2026, 10, day, h, m, 0, 0, time.UTC), pct}
}

func TestTheBatterysChargeIsWorkedOutBetweenTheCarsReports(t *testing.T) {
	pts := []socPoint{sp(20, 0, 0, 80), sp(20, 10, 0, 60), sp(22, 0, 0, 65), sp(22, 6, 0, 65.5)}
	at := func(day, h int) time.Time { return time.Date(2026, 10, day, h, 0, 0, 0, time.UTC) }

	if v, ok := socAt(pts, at(20, 5)); !ok || math.Abs(v-70) > 1e-9 {
		t.Errorf("halfway between 80 and 60 = %v, %v, want 70", v, ok)
	}
	if v, ok := socAt(pts, at(20, 0)); !ok || v != 80 {
		t.Errorf("at the first report = %v, %v, want 80", v, ok)
	}
	if _, ok := socAt(pts, at(19, 23)); ok {
		t.Error("before the first report there is nothing to say")
	}
	// A day and more apart is too long to say when the car was driven, unless the battery hardly moved (a parked car).
	if _, ok := socAt(pts, at(21, 0)); ok {
		t.Error("a long gap that the battery moved across should not be filled in")
	}
	pts = []socPoint{sp(22, 0, 0, 61), sp(25, 0, 0, 60.5)}
	if v, ok := socAt(pts, at(23, 12)); !ok || v < 60.5 || v > 61 {
		t.Errorf("parked across a long gap = %v, %v, want about 60.7", v, ok)
	}
	// After the last report only a short while is held.
	if v, ok := socAt(pts, sp(25, 6, 0, 0).At); !ok || v != 60.5 {
		t.Errorf("a few hours after the last report = %v, %v, want 60.5", v, ok)
	}
	if _, ok := socAt(pts, sp(26, 6, 0, 0).At); ok {
		t.Error("a day after the last report should be unknown")
	}
	if _, ok := socAt(nil, at(20, 0)); ok {
		t.Error("no reports, no charge")
	}
}

func TestAChargeAndTheCarsOwnReportsAreOneTimeline(t *testing.T) {
	start, end := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC), time.Date(2026, 10, 21, 11, 0, 0, 0, time.UTC)
	a, b := 40.0, 60.0
	charges := []model.EvCharge{{StartPct: &a, StartPctAt: &start, EndPct: &b, EndPctAt: &end}, {StartPct: &b}} // the second has no time: not a report
	reads := []model.EvBatteryRead{{At: start, Percent: 40}, {At: end.Add(-time.Hour * 3), Percent: 45}}
	got := socPointsOf(reads, charges)
	if len(got) != 3 || got[0].Pct != 45 || got[1].Pct != 40 || got[2].Pct != 60 {
		t.Fatalf("timeline = %+v, want 45, 40, 60 with the report both a look and a charge made counted once", got)
	}
}

// bankBody is the part of the odometer answer these tests read.
type bankBody struct {
	Intervals []struct {
		Miles       float64  `json:"miles"`
		KWh         float64  `json:"kwh"`
		Cost        *string  `json:"cost"`
		CostPerMile *string  `json:"cost_per_mile"`
		MilesPerKWh *float64 `json:"miles_per_kwh"`
		BankedPct   *float64 `json:"banked_pct"`
		UsedKWh     *float64 `json:"used_kwh"`
		UsedCost    *string  `json:"used_cost"`
	} `json:"intervals"`
	Totals *struct {
		Miles       float64  `json:"miles"`
		KWh         float64  `json:"kwh"`
		Cost        *string  `json:"cost"`
		CostPerMile *string  `json:"cost_per_mile"`
		MilesPerKWh *float64 `json:"miles_per_kwh"`
		Adjusted    bool     `json:"adjusted"`
		FromDay     *string  `json:"from_day"`
	} `json:"totals"`
}

func TestEnergyStillInTheBatteryIsNotCountedAgainstTheMilesDriven(t *testing.T) {
	f := newFordTest(t)
	ctx := context.Background()
	base := f.srv.URL + "/api/v1/meters/" + f.meter
	patch(t, base, `{"ev":true}`)
	put(t, f.srv.URL+"/api/v1/rates", `{"mode":"one","fixed_rate":"0.20","currency":"USD"}`)

	day := func(d, h, m int) time.Time { return time.Date(2026, 10, d, h, m, 0, 0, time.UTC) }
	// Two odometer days, 20 miles apart. In between the car was charged 7.2 kWh (20 % of the battery), so 0.36 kWh for every percent.
	if err := f.st.SetOdometer(ctx, f.meter, "2026-10-19", 1000); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetOdometer(ctx, f.meter, "2026-10-21", 1020); err != nil {
		t.Fatal(err)
	}
	f.draw(t, day(20, 10, 0), 60, 7200)
	f.charge(t, day(20, 10, 0), day(20, 10, 59), pct(40), pct(60), day(20, 9, 50), day(20, 11, 5))

	// Before the car's reports are saved, the stretch has all 7.2 kWh against 20 miles: 7.2 cents a mile.
	var raw bankBody
	get(t, base+"/ev", &raw)
	if raw.Intervals[0].BankedPct != nil || raw.Intervals[0].CostPerMile == nil || *raw.Intervals[0].CostPerMile != "0.072" {
		t.Fatalf("without reports the stretch keeps its raw figures: %+v", raw.Intervals[0])
	}
	if raw.Totals == nil || raw.Totals.Adjusted {
		t.Fatalf("without reports the totals are not adjusted: %+v", raw.Totals)
	}

	// The battery held 45 % when the stretch began and 60 % when it ended: 15 of the 20 percent charged is still there, 5.4 kWh of the 7.2.
	for _, r := range []struct {
		at  time.Time
		pct float64
	}{{day(19, 0, 0), 45}, {day(19, 12, 0), 42}, {day(20, 0, 0), 41}, {day(20, 23, 0), 60}} {
		if err := f.st.SaveEvBatteryRead(ctx, f.meter, r.at, r.pct); err != nil {
			t.Fatal(err)
		}
	}
	var got bankBody
	if r := get(t, base+"/ev", &got); r.StatusCode != http.StatusOK {
		t.Fatalf("ev: %d", r.StatusCode)
	}
	iv := got.Intervals[0]
	if iv.BankedPct == nil || math.Abs(*iv.BankedPct-15) > 1e-6 {
		t.Fatalf("banked = %v, want 15", iv.BankedPct)
	}
	if iv.UsedKWh == nil || math.Abs(*iv.UsedKWh-1.8) > 1e-6 || iv.UsedCost == nil || *iv.UsedCost != "0.36" {
		t.Errorf("used = %v kWh, %v, want 1.8 kWh and 0.36", iv.UsedKWh, iv.UsedCost)
	}
	if iv.KWh < 7.19 || iv.KWh > 7.21 || iv.Cost == nil || *iv.Cost != "1.44" {
		t.Errorf("what the meter measured stays: %v kWh, %v", iv.KWh, iv.Cost)
	}
	if iv.CostPerMile == nil || *iv.CostPerMile != "0.018" {
		t.Errorf("cost per mile = %v, want 0.018 on what the miles used", iv.CostPerMile)
	}
	if iv.MilesPerKWh == nil || math.Abs(*iv.MilesPerKWh-20/1.8) > 1e-6 {
		t.Errorf("miles per kWh = %v, want %v", iv.MilesPerKWh, 20/1.8)
	}
	if got.Totals == nil || !got.Totals.Adjusted || got.Totals.FromDay == nil || *got.Totals.FromDay != "2026-10-19" || got.Totals.CostPerMile == nil || *got.Totals.CostPerMile != "0.018" {
		t.Errorf("totals = %+v, want adjusted from 2026-10-19 at 0.018 a mile", got.Totals)
	}
}

func TestADriveOnEnergyBoughtEarlierCostsWhatItUsedFromTheBattery(t *testing.T) {
	f := newFordTest(t)
	ctx := context.Background()
	base := f.srv.URL + "/api/v1/meters/" + f.meter
	patch(t, base, `{"ev":true}`)
	put(t, f.srv.URL+"/api/v1/rates", `{"mode":"one","fixed_rate":"0.20","currency":"USD"}`)
	day := func(d, h, m int) time.Time { return time.Date(2026, 10, d, h, m, 0, 0, time.UTC) }

	// A charge (0.36 kWh a percent) on the 18th, then two days of driving with nothing charged: the battery went from 90 % to 80 %.
	f.draw(t, day(18, 10, 0), 60, 7200)
	f.charge(t, day(18, 10, 0), day(18, 10, 59), pct(40), pct(60), day(18, 9, 50), day(18, 11, 5))
	for s, miles := range map[string]float64{"2026-10-19": 1000, "2026-10-21": 1030} {
		if err := f.st.SetOdometer(ctx, f.meter, s, miles); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []struct {
		at  time.Time
		pct float64
	}{{day(19, 0, 0), 90}, {day(19, 10, 0), 87}, {day(20, 0, 0), 84}, {day(20, 12, 0), 82}, {day(21, 0, 0), 80}} {
		if err := f.st.SaveEvBatteryRead(ctx, f.meter, r.at, r.pct); err != nil {
			t.Fatal(err)
		}
	}
	var got bankBody
	get(t, base+"/ev", &got)
	iv := got.Intervals[0]
	// 10 % was drawn from the battery: 3.6 kWh at the 20 cents it was bought for, for 30 miles, though nothing was charged in the stretch.
	if iv.KWh != 0 || iv.BankedPct == nil || *iv.BankedPct != -10 {
		t.Fatalf("stretch = %+v, want no energy and a battery down 10", iv)
	}
	if iv.UsedKWh == nil || math.Abs(*iv.UsedKWh-3.6) > 1e-6 || iv.UsedCost == nil || *iv.UsedCost != "0.72" {
		t.Errorf("used = %v kWh, %v, want 3.6 kWh and 0.72", iv.UsedKWh, iv.UsedCost)
	}
}
