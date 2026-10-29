package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/evcharge"
	"github.com/gerp93/KVGuzzler/server/internal/ford"
)

// chargesBody is the part of the charges answer the tests read.
type chargesBody struct {
	CarLinked   bool `json:"car_linked"`
	Consistency *struct {
		Charges   int     `json:"charges"`
		KWhPerPct float64 `json:"kwh_per_pct"`
		Lowest    float64 `json:"lowest"`
		Highest   float64 `json:"highest"`
	} `json:"consistency"`
	Charges []struct {
		ID              int64    `json:"id"`
		Active          bool     `json:"active"`
		DurationSeconds int      `json:"duration_seconds"`
		KWh             float64  `json:"kwh"`
		Cost            *string  `json:"cost"`
		Coverage        float64  `json:"coverage"`
		GainPct         *float64 `json:"gain_pct"`
		PctStatus       string   `json:"pct_status"`
		KWhPerPct       *float64 `json:"kwh_per_pct"`
	} `json:"charges"`
}

func at(h, m int) time.Time { return time.Date(2026, 10, 22, h, m, 0, 0, time.UTC) }

// charge keeps a closed charge of the test meter with the car's two reports (a nil report is none).
func (f *fordTest) charge(t *testing.T, start, end time.Time, startPct, endPct *float64, startAt, endAt time.Time) int64 {
	t.Helper()
	ctx := context.Background()
	c, err := f.st.StartEvCharge(ctx, f.meter, start)
	if err != nil {
		t.Fatal(err)
	}
	if end.IsZero() {
		return c.ID
	}
	if err := f.st.TouchEvCharge(ctx, c.ID, end); err != nil {
		t.Fatal(err)
	}
	if err := f.st.EndEvCharge(ctx, c.ID, end); err != nil {
		t.Fatal(err)
	}
	if startPct != nil {
		if err := f.st.SetEvChargeStart(ctx, c.ID, *startPct, startAt); err != nil {
			t.Fatal(err)
		}
	}
	if endPct != nil {
		if err := f.st.SetEvChargeEnd(ctx, c.ID, *endPct, endAt); err != nil {
			t.Fatal(err)
		}
	}
	return c.ID
}

// draw records a meter drawing the given watts every minute from start for the given minutes.
func (f *fordTest) draw(t *testing.T, start time.Time, minutes int, watts float64) {
	t.Helper()
	for i := 0; i < minutes; i++ {
		if err := f.st.AddMinute(context.Background(), f.meter, start.Add(time.Duration(i)*time.Minute), watts, watts, 60); err != nil {
			t.Fatal(err)
		}
	}
}

func pct(v float64) *float64 { return &v }

func TestChargesHaveTheEnergyTheCostAndTheEnergyPerPercentOfTheirOwnMinutes(t *testing.T) {
	f := newFordTest(t)
	base := f.srv.URL + "/api/v1/meters/" + f.meter + "/ev/charges"
	if r, _ := send(t, http.MethodGet, base, ""); r.StatusCode != http.StatusConflict {
		t.Fatalf("charges of a meter not marked as charging: %d, want 409", r.StatusCode)
	}
	if r, _ := send(t, http.MethodGet, f.srv.URL+"/api/v1/meters/nope/ev/charges", ""); r.StatusCode != http.StatusNotFound {
		t.Fatalf("charges of no meter: %d, want 404", r.StatusCode)
	}
	patch(t, f.srv.URL+"/api/v1/meters/"+f.meter, `{"ev":true}`)
	put(t, f.srv.URL+"/api/v1/rates", `{"mode":"one","fixed_rate":"0.20","currency":"USD"}`)

	// Two good charges: 7.2 kWh for 20 % and 4 kWh for 10 %, each with the car's report after it ended.
	f.draw(t, at(10, 0), 60, 7200)
	f.charge(t, at(10, 0).Add(20*time.Second), at(10, 59).Add(50*time.Second), pct(40), pct(60), at(9, 50), at(11, 5))
	f.draw(t, at(12, 0), 60, 4000)
	f.charge(t, at(12, 0), at(12, 59), pct(50), pct(60), at(11, 55), at(13, 2))
	// A charge that gained nothing the car can show (power drawn, battery still at 60).
	f.draw(t, at(13, 10), 10, 1000)
	small := f.charge(t, at(13, 10), at(13, 19), pct(60), pct(60.5), at(13, 5), at(13, 30))
	// One whose end the car has not reported yet, and one whose start it never reported.
	f.charge(t, at(14, 0), at(14, 10), pct(61), nil, at(13, 55), time.Time{})
	f.charge(t, at(14, 20), at(14, 30), nil, nil, time.Time{}, time.Time{})
	// One going on now (the clock is 15:00).
	f.draw(t, at(14, 40), 20, 7000)
	active := f.charge(t, at(14, 40), time.Time{}, nil, nil, time.Time{}, time.Time{})

	var got chargesBody
	if r := get(t, base, &got); r.StatusCode != http.StatusOK {
		t.Fatalf("charges: %d", r.StatusCode)
	}
	if len(got.Charges) != 6 {
		t.Fatalf("%d charges, want 6", len(got.Charges))
	}
	if got.Charges[0].ID != active || !got.Charges[0].Active || got.Charges[0].PctStatus != "active" {
		t.Errorf("newest = %+v, want the active one first", got.Charges[0])
	}
	if got.Charges[0].DurationSeconds != 20*60 {
		t.Errorf("active duration = %d s, want it counted to now", got.Charges[0].DurationSeconds)
	}
	if kwh := got.Charges[0].KWh; kwh < 2.33 || kwh > 2.34 { // 20 minutes at 7 kW
		t.Errorf("active energy = %v", kwh)
	}
	byStatus := map[string]int{}
	for _, c := range got.Charges {
		byStatus[c.PctStatus]++
	}
	if byStatus["ok"] != 2 || byStatus["small_gain"] != 1 || byStatus["waiting"] != 1 || byStatus["none"] == 0 {
		t.Errorf("statuses = %v", byStatus)
	}
	for _, c := range got.Charges {
		switch {
		case c.ID == small:
			if c.PctStatus != "small_gain" || c.KWhPerPct != nil || c.GainPct == nil || *c.GainPct != 0.5 {
				t.Errorf("small gain: %+v", c)
			}
		case c.PctStatus == "ok" && c.KWh > 7:
			if c.KWh < 7.19 || c.KWh > 7.21 || c.KWhPerPct == nil || *c.KWhPerPct < 0.359 || *c.KWhPerPct > 0.361 {
				t.Errorf("first good charge: %+v", c)
			}
			if c.Cost == nil || !strings.HasPrefix(*c.Cost, "1.44") || c.Coverage < 0.99 {
				t.Errorf("its cost %v / coverage %v, want 7.2 kWh at 0.20 = 1.44 and the meter measured the whole time", c.Cost, c.Coverage)
			}
		case c.PctStatus == "ok":
			if c.KWhPerPct == nil || *c.KWhPerPct < 0.399 || *c.KWhPerPct > 0.401 {
				t.Errorf("second good charge: %+v", c)
			}
		}
	}
	// 11.2 kWh for 30 %: the charges weigh by their size, and the extremes are single charges.
	c := got.Consistency
	if c == nil || c.Charges != 2 || c.KWhPerPct < 0.373 || c.KWhPerPct > 0.374 || c.Lowest < 0.359 || c.Lowest > 0.361 || c.Highest < 0.399 || c.Highest > 0.401 {
		t.Errorf("consistency = %+v", c)
	}
	if got.CarLinked {
		t.Error("car_linked before Ford was set up")
	}
}

func TestACarsFinalReportMustComeAfterTheChargeEndedOrTheEnergyPerPercentIsWithheld(t *testing.T) {
	f := newFordTest(t)
	patch(t, f.srv.URL+"/api/v1/meters/"+f.meter, `{"ev":true}`)
	f.draw(t, at(1, 0), 60, 7200)
	// Reported ten minutes before the charge ended and never again: the gain is short by the last of the charge.
	start, end := at(1, 0), at(1, 59)
	f.charge(t, start, end, pct(20), pct(30), at(0, 50), at(1, 49))
	var got chargesBody
	get(t, f.srv.URL+"/api/v1/meters/"+f.meter+"/ev/charges", &got)
	// The clock is 15:00, past the time the tracker gives up, so this is final.
	if len(got.Charges) != 1 || got.Charges[0].PctStatus != "early" || got.Charges[0].KWhPerPct != nil || got.Consistency != nil {
		t.Fatalf("charges = %+v / %+v", got.Charges, got.Consistency)
	}
	// The same report a minute after the tracker started trying is still waiting for a better one.
	*f.clock = at(2, 30)
	get(t, f.srv.URL+"/api/v1/meters/"+f.meter+"/ev/charges", &got)
	if got.Charges[0].PctStatus != "waiting" {
		t.Errorf("status while the tracker still asks = %q, want waiting", got.Charges[0].PctStatus)
	}
}

func TestAChargeCanBeRemovedOnceItHasEnded(t *testing.T) {
	f := newFordTest(t)
	patch(t, f.srv.URL+"/api/v1/meters/"+f.meter, `{"ev":true}`)
	closed := f.charge(t, at(10, 0), at(10, 30), nil, nil, time.Time{}, time.Time{})
	active := f.charge(t, at(14, 0), time.Time{}, nil, nil, time.Time{}, time.Time{})
	base := f.srv.URL + "/api/v1/meters/" + f.meter + "/ev/charges/"
	if r, _ := send(t, http.MethodDelete, base+strconv.FormatInt(active, 10), ""); r.StatusCode != http.StatusConflict {
		t.Errorf("removing the charge in progress: %d, want 409", r.StatusCode)
	}
	if r, _ := send(t, http.MethodDelete, base+strconv.FormatInt(closed, 10), ""); r.StatusCode != http.StatusNoContent {
		t.Errorf("removing an ended charge: %d, want 204", r.StatusCode)
	}
	if r, _ := send(t, http.MethodDelete, base+strconv.FormatInt(closed, 10), ""); r.StatusCode != http.StatusNotFound {
		t.Errorf("removing it again: %d, want 404", r.StatusCode)
	}
	if r, _ := send(t, http.MethodDelete, base+"abc", ""); r.StatusCode != http.StatusNotFound {
		t.Errorf("removing charge abc: %d, want 404", r.StatusCode)
	}
	var got chargesBody
	get(t, f.srv.URL+"/api/v1/meters/"+f.meter+"/ev/charges", &got)
	if len(got.Charges) != 1 || got.Charges[0].ID != active {
		t.Errorf("charges left = %+v", got.Charges)
	}
}

// link finishes linking the fake vehicle, so the car can be asked.
func (f *fordTest) link(t *testing.T) {
	t.Helper()
	base := f.srv.URL + "/api/v1/ford"
	patch(t, f.srv.URL+"/api/v1/meters/"+f.meter, `{"ev":true}`)
	if r, body := send(t, http.MethodPut, base, f.setup("km")); r.StatusCode != http.StatusOK {
		t.Fatalf("setup: %d %s", r.StatusCode, body)
	}
	_, body := send(t, http.MethodPost, base+"/link", "")
	var link struct {
		URL string `json:"url"`
	}
	_ = json.Unmarshal([]byte(body), &link)
	u, _ := url.Parse(link.URL)
	if r, body := send(t, http.MethodPost, base+"/link/complete", `{"redirect":"http://localhost:8000/callback?code=GOODCODE&state=`+u.Query().Get("state")+`"}`); r.StatusCode != http.StatusOK {
		t.Fatalf("complete: %d %s", r.StatusCode, body)
	}
}

func TestTheCarIsAskedForTheTractionBatteryAndOnlyForTheMeterTheVehicleIsLinkedTo(t *testing.T) {
	f := newFordTest(t)
	car := fordCar{f.api}
	ctx := context.Background()
	if _, err := car.Battery(ctx, f.meter); !errors.Is(err, evcharge.ErrNoCar) {
		t.Fatalf("before anything is set up: %v, want no car", err)
	}
	f.link(t)
	var got chargesBody
	get(t, f.srv.URL+"/api/v1/meters/"+f.meter+"/ev/charges", &got)
	if !got.CarLinked {
		t.Error("car_linked is false once the vehicle is linked to the meter")
	}

	// Linking just asked Ford: it is not asked again inside 30 seconds, and the tracker is told to try later.
	var tooSoon *ford.TooSoon
	if _, err := car.Battery(ctx, f.meter); !errors.As(err, &tooSoon) {
		t.Fatalf("a look right after linking: %v, want the 30-second wait", err)
	}
	*f.clock = f.clock.Add(time.Minute)
	r, err := car.Battery(ctx, f.meter)
	if err != nil {
		t.Fatal(err)
	}
	// The traction battery, not the 12 V one (91) or the similarly named estimate (63).
	if r.Percent != 61.5 || !r.At.Equal(time.Date(2026, 10, 22, 14, 50, 0, 0, time.UTC)) {
		t.Errorf("battery = %+v, want 61.5 percent reported at 14:50", r)
	}
	if _, err := car.Battery(ctx, "some/other-meter"); !errors.Is(err, evcharge.ErrNoCar) {
		t.Errorf("another meter: %v, want no car", err)
	}

	// Ford answers, but without the figure (the link lacks the permission): a long wait, not a quick retry.
	*f.clock = f.clock.Add(time.Minute)
	f.ford.noBattery = true
	_, err = car.Battery(ctx, f.meter)
	var wait interface{ RetryAfter() time.Duration }
	if !errors.As(err, &wait) || wait.RetryAfter() < time.Hour || !strings.Contains(err.Error(), "evData") {
		t.Errorf("an answer without the battery: %v", err)
	}
}
