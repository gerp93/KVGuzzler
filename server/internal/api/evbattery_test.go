package api

import (
	"math"
	"net/http"
	"testing"
)

type lossBody struct {
	BatteryKWh *float64 `json:"battery_kwh"`
	Losses     *struct {
		Charges    int     `json:"charges"`
		WallKWh    float64 `json:"wall_kwh"`
		BatteryKWh float64 `json:"battery_kwh"`
		LossKWh    float64 `json:"loss_kwh"`
		Efficiency float64 `json:"efficiency"`
		Lowest     float64 `json:"lowest"`
		Highest    float64 `json:"highest"`
		LossCost   *string `json:"loss_cost"`
	} `json:"losses"`
	Charges []struct {
		ID         int64    `json:"id"`
		PctStatus  string   `json:"pct_status"`
		KWh        float64  `json:"kwh"`
		BatteryKWh *float64 `json:"battery_kwh"`
		LossKWh    *float64 `json:"loss_kwh"`
		Efficiency *float64 `json:"efficiency"`
		LossCost   *string  `json:"loss_cost"`
	} `json:"charges"`
}

func within(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestWhatChargingLostIsTheWallsEnergyLessWhatReachedTheBattery(t *testing.T) {
	f := newFordTest(t)
	base := f.srv.URL + "/api/v1/meters/" + f.meter + "/ev/"
	patch(t, f.srv.URL+"/api/v1/meters/"+f.meter, `{"ev":true}`)
	put(t, f.srv.URL+"/api/v1/rates", `{"mode":"one","fixed_rate":"0.20","currency":"USD"}`)

	// 7.2 kWh for 20 percent, and 4 kWh for 10 percent, each with the car's report after it ended.
	f.draw(t, at(10, 0), 60, 7200)
	first := f.charge(t, at(10, 0), at(10, 59), pct(40), pct(60), at(9, 50), at(11, 5))
	f.draw(t, at(12, 0), 60, 4000)
	second := f.charge(t, at(12, 0), at(12, 59), pct(50), pct(60), at(11, 55), at(13, 2))
	// A charge the battery gained 20 percent in, but that the meter measured only half of: its wall energy is too low to say anything about the loss.
	f.draw(t, at(5, 0), 30, 7200)
	half := f.charge(t, at(5, 0), at(5, 59), pct(20), pct(40), at(4, 50), at(6, 5))
	// A charge that gained nothing the car can show.
	f.draw(t, at(13, 10), 10, 1000)
	small := f.charge(t, at(13, 10), at(13, 19), pct(60), pct(60.5), at(13, 5), at(13, 30))

	// Without the battery's size there is no loss, however good the charges are.
	var got lossBody
	get(t, base+"charges", &got)
	if got.BatteryKWh != nil || got.Losses != nil {
		t.Fatalf("losses without a battery size: %+v", got)
	}
	for _, c := range got.Charges {
		if c.Efficiency != nil || c.LossKWh != nil || c.BatteryKWh != nil || c.LossCost != nil {
			t.Errorf("charge %d has loss figures without a battery size: %+v", c.ID, c)
		}
	}

	// A battery size is a believable number of kWh.
	for name, body := range map[string]string{"none": `{}`, "too small": `{"kwh":3}`, "too big": `{"kwh":900}`, "an extra field": `{"kwh":30,"x":1}`} {
		if r, _ := send(t, http.MethodPut, base+"battery", body); r.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", name, r.StatusCode)
		}
	}
	if r, body := send(t, http.MethodPut, base+"battery", `{"kwh":30}`); r.StatusCode != http.StatusOK {
		t.Fatalf("battery: %d %s", r.StatusCode, body)
	}

	get(t, base+"charges", &got)
	if got.BatteryKWh == nil || *got.BatteryKWh != 30 {
		t.Fatalf("battery size = %v", got.BatteryKWh)
	}
	for _, c := range got.Charges {
		switch c.ID {
		case first: // 20 percent of 30 kWh is 6; the wall gave 7.2
			if c.Efficiency == nil || !within(*c.BatteryKWh, 6) || !within(*c.LossKWh, 1.2) || !within(*c.Efficiency, 6/7.2) || c.LossCost == nil || *c.LossCost != "0.24" {
				t.Errorf("first charge = %+v / %v", c, c.LossCost)
			}
		case second: // 10 percent is 3; the wall gave 4
			if c.Efficiency == nil || !within(*c.BatteryKWh, 3) || !within(*c.LossKWh, 1) || !within(*c.Efficiency, 0.75) || *c.LossCost != "0.20" {
				t.Errorf("second charge = %+v / %v", c, c.LossCost)
			}
		case half:
			if c.Efficiency != nil || c.LossKWh != nil {
				t.Errorf("a charge measured half the time has a loss: %+v", c)
			}
		case small:
			if c.Efficiency != nil || c.PctStatus != "small_gain" {
				t.Errorf("a charge with no gain has a loss: %+v", c)
			}
		}
	}
	// 9 of 11.2 kWh reached the battery; a bigger charge weighs more than a small one, and the extremes are single charges.
	l := got.Losses
	if l == nil || l.Charges != 2 || !within(l.WallKWh, 11.2) || !within(l.BatteryKWh, 9) || !within(l.LossKWh, 2.2) || !within(l.Efficiency, 9/11.2) || !within(l.Lowest, 0.75) || !within(l.Highest, 6/7.2) {
		t.Fatalf("losses = %+v", l)
	}
	if l.LossCost == nil || *l.LossCost != "0.44" {
		t.Errorf("the lost energy's price = %v, want 0.44", l.LossCost)
	}

	// Taking the size off takes the loss figures with it.
	if r, _ := send(t, http.MethodDelete, base+"battery", ""); r.StatusCode != http.StatusNoContent {
		t.Errorf("delete battery: %d", r.StatusCode)
	}
	get(t, base+"charges", &got)
	if got.BatteryKWh != nil || got.Losses != nil {
		t.Errorf("after taking the size off: %+v", got)
	}
}

func TestAnImpossibleBatterySizeShowsAsMoreThanFullEfficiencyNotAsALoss(t *testing.T) {
	f := newFordTest(t)
	base := f.srv.URL + "/api/v1/meters/" + f.meter + "/ev/"
	patch(t, f.srv.URL+"/api/v1/meters/"+f.meter, `{"ev":true}`)
	put(t, f.srv.URL+"/api/v1/rates", `{"mode":"one","fixed_rate":"0.20","currency":"USD"}`)
	f.draw(t, at(10, 0), 60, 7200)
	f.charge(t, at(10, 0), at(10, 59), pct(40), pct(60), at(9, 50), at(11, 5))
	send(t, http.MethodPut, base+"battery", `{"kwh":88}`) // 20 percent of 88 kWh is more than the wall gave

	var got lossBody
	get(t, base+"charges", &got)
	c := got.Charges[0]
	if c.Efficiency == nil || *c.Efficiency <= 1 || *c.LossKWh >= 0 {
		t.Fatalf("a battery size the numbers cannot match = %+v", c)
	}
	if c.LossCost != nil {
		t.Errorf("an impossible negative loss was priced: %v", *c.LossCost)
	}
}

func TestOnlyAMeterThatChargesAVehicleHasABatterySize(t *testing.T) {
	f := newFordTest(t)
	base := f.srv.URL + "/api/v1/meters/" + f.meter + "/ev/battery"
	if r, _ := send(t, http.MethodGet, base, ""); r.StatusCode != http.StatusConflict {
		t.Errorf("GET battery on a meter not marked as charging: %d, want 409", r.StatusCode)
	}
	if r, _ := send(t, http.MethodPut, base, `{"kwh":30}`); r.StatusCode != http.StatusConflict {
		t.Errorf("PUT battery on a meter not marked as charging: %d, want 409", r.StatusCode)
	}
}
