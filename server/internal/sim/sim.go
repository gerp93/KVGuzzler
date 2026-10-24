// Package sim is a made-up house, so the server and the dashboard can run with no hardware. Its device is named
// "Simulated house" and nothing it reports is a measurement.
//
// Each day has its own plan (the dryer runs on some days, the car charges on most nights, the air conditioner works harder on a hot
// day, the office is quiet at the weekend), worked out from the date alone, so the same day always looks the same and history made for
// the past matches what the live readings do today.
package sim

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/model"
)

const deviceID = "sim-em16p"

// baseWatts is what the house draws that no circuit clamp sees (so the mains always read more than the circuits).
const baseWatts = 310.0

// plan is how one day goes.
type plan struct {
	weekday bool

	dryerOn           bool
	dryerStart        float64
	rangeStart        float64
	rangeLen          float64
	evOn              bool
	evStart, evLen    float64
	hvacDuty          float64
	heaterEvening     float64
	pcBurst           bool
	pcBurstStart      float64
	pcBurstLen        float64
	lightsStart       float64
	officeLongerToday bool
}

// planFor works out a day's plan from its date: no clock, no state.
func planFor(day time.Time) plan {
	r := rand.New(rand.NewPCG(uint64(day.Year())*1000+uint64(day.YearDay()), 99))
	wd := day.Weekday()
	p := plan{weekday: wd != time.Saturday && wd != time.Sunday}
	p.dryerOn = r.Float64() < 0.55
	p.dryerStart = 17 + r.Float64()*3.5
	p.rangeStart = 17 + r.Float64()*1.5
	p.rangeLen = 0.5 + r.Float64()*0.7
	p.evOn = r.Float64() < 0.7
	p.evStart = 22 + r.Float64()*2.5 // may run past midnight
	p.evLen = 2.5 + r.Float64()*3.5
	p.hvacDuty = 0.2 + r.Float64()*0.5
	p.heaterEvening = 19 + r.Float64()*1.5
	p.pcBurst = r.Float64() < 0.5
	p.pcBurstStart = 19 + r.Float64()*2
	p.pcBurstLen = 1 + r.Float64()*3
	p.lightsStart = 16.5 + r.Float64()
	p.officeLongerToday = r.Float64() < 0.2
	if !p.weekday {
		p.pcBurst = r.Float64() < 0.8
		p.pcBurstStart = 13 + r.Float64()*6
		p.pcBurstLen = 2 + r.Float64()*4
	}
	return p
}

// window reports whether hour h (0 to 24) is in [start, start+length), allowing the window to run past midnight.
func window(h, start, length float64) bool {
	d := math.Mod(h-start+48, 24)
	return d < length
}

type appliance struct {
	label string
	scope model.Scope
	volts float64
	max   float64
	// in is the label of what it is inside or on (a sub-panel's feed, the office circuit): its power is already in that one's reading, so
	// the mains do not add it a second time.
	in string
	// feed makes the appliance the sub-panel's feed: it reads the sum of what is inside it plus a little of its own.
	feed bool
	// watts is the draw at hour (0 to 24, fractional) of a day with plan p, plus a little noise.
	watts func(p plan, h float64, r *rand.Rand) float64
}

func between(h, from, to float64) bool { return h >= from && h < to }

// circuits are clamped on the panel; they are what the mains cover. The PC and the monitor are plugs on the office circuit, shown beside it.
var appliances = []appliance{
	{"Dryer", model.ScopeCircuit, 240, 6000, "", false, func(p plan, h float64, r *rand.Rand) float64 {
		if p.dryerOn && window(h, p.dryerStart, 1.1) {
			return 5200 + r.Float64()*300
		}
		return 0
	}},
	{"Range", model.ScopeCircuit, 240, 9600, "", false, func(p plan, h float64, r *rand.Rand) float64 {
		if window(h, p.rangeStart, p.rangeLen) {
			return 2400 + r.Float64()*600
		}
		return 0
	}},
	{"EV charger", model.ScopeCircuit, 240, 9600, "Garage panel", false, func(p plan, h float64, r *rand.Rand) float64 {
		if p.evOn && window(h, p.evStart, p.evLen) {
			// The last quarter of an hour tapers off, as a charger does when the battery is nearly full.
			if !window(h, p.evStart, p.evLen-0.25) {
				return 3200 + r.Float64()*200
			}
			return 7150 + r.Float64()*100
		}
		return 0
	}},
	{"HVAC", model.ScopeCircuit, 240, 6000, "", false, func(p plan, h float64, r *rand.Rand) float64 {
		// Works in half-hour cycles; how much of each depends on the day.
		if math.Mod(h*2, 1) < p.hvacDuty {
			return 3300 + r.Float64()*200
		}
		return 40
	}},
	{"Water heater", model.ScopeCircuit, 240, 5500, "", false, func(p plan, h float64, r *rand.Rand) float64 {
		if between(h, 6, 6.7) || window(h, p.heaterEvening, 0.5) {
			return 4400 + r.Float64()*100
		}
		return 0
	}},
	{"Fridge", model.ScopeCircuit, 120, 1500, "", false, func(p plan, h float64, r *rand.Rand) float64 {
		if math.Mod(h*6, 1) < 0.4 {
			return 140 + r.Float64()*20
		}
		return 4
	}},
	{"Lights", model.ScopeCircuit, 120, 1800, "", false, func(p plan, h float64, r *rand.Rand) float64 {
		if window(h, p.lightsStart, 6.5) {
			return 220 + r.Float64()*30
		}
		return 15
	}},
	{"Office", model.ScopeCircuit, 120, 1800, "", false, func(p plan, h float64, r *rand.Rand) float64 {
		end := 17.0
		if p.officeLongerToday {
			end = 20
		}
		if p.weekday && between(h, 8, end) {
			return 180 + r.Float64()*60
		}
		return 35
	}},
	{"Desk PC", model.ScopePC, 120, 1200, "Office", false, func(p plan, h float64, r *rand.Rand) float64 {
		if window(h, p.pcBurstStart, p.pcBurstLen) && p.pcBurst {
			return 300 + r.Float64()*80 // a game or a render
		}
		if between(h, 7, 23.5) {
			return 62 + r.Float64()*18
		}
		return 3
	}},
	{"Monitor", model.ScopePlug, 120, 300, "Office", false, func(p plan, h float64, r *rand.Rand) float64 {
		if between(h, 7, 23.5) {
			return 31 + r.Float64()*3
		}
		return 0.5
	}},
	{"Workshop", model.ScopeCircuit, 240, 4800, "Garage panel", false, func(p plan, h float64, r *rand.Rand) float64 {
		// Tools on a weekend morning, now and then on a weekday evening.
		if (!p.weekday && between(h, 9, 13)) || (p.weekday && p.pcBurst && window(h, 18, 1.5)) {
			return 900 + r.Float64()*1400*math.Abs(math.Sin(h*7))
		}
		return 6
	}},
	{"Garage panel", model.ScopeCircuit, 240, 12000, "", true, func(p plan, h float64, r *rand.Rand) float64 {
		return 45 + r.Float64()*10 // what the panel itself carries: openers, a charger in the wall
	}},
}

type Source struct {
	now func() time.Time
}

// New returns a simulated house that follows the wall clock.
func New() *Source { return &Source{now: time.Now} }

// NewAt returns one that reads its clock from now, for tests.
func NewAt(now func() time.Time) *Source { return &Source{now: now} }

func (s *Source) Device() model.Device {
	return model.Device{ID: deviceID, Kind: "refoss", Name: "Simulated house", Model: "sim"}
}

func mainsID(leg string) string { return deviceID + ":mains-" + leg }
func f64(v float64) *float64    { return &v }
func idx(i int) *int            { return &i }

// meterID is the id of the i-th appliance: the circuits are numbered, the PC and the monitor are named.
func meterID(i int) string {
	switch appliances[i].scope {
	case model.ScopePC:
		return deviceID + ":pc"
	case model.ScopePlug:
		return deviceID + ":monitor"
	}
	return fmt.Sprintf("%s:ch-%02d", deviceID, i+1)
}

func (s *Source) Meters() []model.Meter {
	chs := []model.Meter{
		{ID: mainsID("a"), DeviceID: deviceID, Scope: model.ScopeMains, Label: "Mains A", Enabled: true, MaxWatts: f64(26000)},
		{ID: mainsID("b"), DeviceID: deviceID, Scope: model.ScopeMains, Label: "Mains B", Enabled: true, MaxWatts: f64(26000)},
	}
	for i, a := range appliances {
		m := model.Meter{ID: meterID(i), DeviceID: deviceID, Scope: a.scope, Label: a.label, Enabled: true, MaxWatts: f64(a.max)}
		if a.scope == model.ScopeCircuit {
			m.Index = idx(i + 1)
		}
		chs = append(chs, m)
	}
	return chs
}

// at is every reading of the house at one moment: what each appliance draws, and the two legs of the mains, which carry the circuits and
// the part nothing clamps.
func at(t time.Time) []model.Reading {
	hour := float64(t.Hour()) + float64(t.Minute())/60 + float64(t.Second())/3600
	p := planFor(t)
	r := rand.New(rand.NewPCG(7, uint64(t.Unix())))

	readings := make([]model.Reading, 0, len(appliances)+2)
	watts := make([]float64, len(appliances))
	for i, a := range appliances {
		watts[i] = a.watts(p, hour, r)
	}
	// A sub-panel's feed carries everything inside it.
	for i, a := range appliances {
		if !a.feed {
			continue
		}
		for j, b := range appliances {
			if b.in == a.label {
				watts[i] += watts[j]
			}
		}
	}
	total := baseWatts
	for i, a := range appliances {
		// Only what is not inside another counts towards the mains: the feed already holds its circuits, the office circuit its plugs.
		if a.scope == model.ScopeCircuit && a.in == "" {
			total += watts[i]
		}
		readings = append(readings, model.Reading{MeterID: meterID(i), Watts: watts[i], Volts: f64(a.volts), Amps: f64(watts[i] / a.volts)})
	}
	// The two legs share the load; the 240 V loads draw equally from both.
	legA, legB := total/2*1.02, total/2*0.98
	return append(readings,
		model.Reading{MeterID: mainsID("a"), Watts: legA, Volts: f64(121), Amps: f64(legA / 121)},
		model.Reading{MeterID: mainsID("b"), Watts: legB, Volts: f64(120), Amps: f64(legB / 120)},
	)
}

// Inside says what each meter is inside (the id of the meter whose reading already holds it): the EV charger and the workshop are inside the
// garage panel's feed, the PC and the monitor are on the office circuit. A demo uses it to lay the meters out the way the house is wired.
func (s *Source) Inside() map[string]string {
	ids := map[string]string{}
	for i, a := range appliances {
		ids[a.label] = meterID(i)
	}
	out := map[string]string{}
	for i, a := range appliances {
		if a.in != "" {
			out[meterID(i)] = ids[a.in]
		}
	}
	return out
}

func (s *Source) Poll(_ context.Context) ([]model.Reading, error) {
	return at(s.now()), nil
}
