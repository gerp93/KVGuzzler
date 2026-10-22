// Package sim is a made-up house, so the server and the dashboard can run with no hardware. Its device is named
// "Simulated house" and nothing it reports is a measurement.
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

type appliance struct {
	label string
	volts float64
	max   float64
	// watts is the draw at hour (0 to 24, fractional) of the day, plus a little noise.
	watts func(hour float64, r *rand.Rand) float64
}

func between(h, from, to float64) bool { return h >= from && h < to }

var appliances = []appliance{
	{"Dryer", 240, 6000, func(h float64, r *rand.Rand) float64 {
		if between(h, 18, 19.2) {
			return 5200 + r.Float64()*300
		}
		return 0
	}},
	{"Range", 240, 9600, func(h float64, r *rand.Rand) float64 {
		if between(h, 17.5, 18.5) {
			return 2400 + r.Float64()*600
		}
		return 0
	}},
	{"EV charger", 240, 9600, func(h float64, r *rand.Rand) float64 {
		if h >= 23 || h < 4 {
			return 7150 + r.Float64()*100
		}
		return 0
	}},
	{"HVAC", 240, 6000, func(h float64, r *rand.Rand) float64 {
		// Runs about half of every hour.
		if math.Mod(h, 1) < 0.5 {
			return 3300 + r.Float64()*200
		}
		return 40
	}},
	{"Water heater", 240, 5500, func(h float64, r *rand.Rand) float64 {
		if between(h, 6, 6.7) || between(h, 19.5, 20) {
			return 4400 + r.Float64()*100
		}
		return 0
	}},
	{"Fridge", 120, 1500, func(h float64, r *rand.Rand) float64 {
		if math.Mod(h*6, 1) < 0.4 {
			return 140 + r.Float64()*20
		}
		return 4
	}},
	{"Lights", 120, 1800, func(h float64, r *rand.Rand) float64 {
		if between(h, 17, 23) {
			return 220 + r.Float64()*30
		}
		return 15
	}},
	{"Office", 120, 1800, func(h float64, r *rand.Rand) float64 {
		if between(h, 8, 17) {
			return 180 + r.Float64()*60
		}
		return 35
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
func circuitID(i int) string    { return fmt.Sprintf("%s:ch-%02d", deviceID, i+1) }
func f64(v float64) *float64    { return &v }
func idx(i int) *int            { return &i }

func (s *Source) Channels() []model.Channel {
	chs := []model.Channel{
		{ID: mainsID("a"), DeviceID: deviceID, Scope: model.ScopeMains, Label: "Mains A", Enabled: true, MaxWatts: f64(26000)},
		{ID: mainsID("b"), DeviceID: deviceID, Scope: model.ScopeMains, Label: "Mains B", Enabled: true, MaxWatts: f64(26000)},
	}
	for i, a := range appliances {
		chs = append(chs, model.Channel{
			ID: circuitID(i), DeviceID: deviceID, Index: idx(i + 1), Scope: model.ScopeCircuit,
			Label: a.label, Enabled: true, MaxWatts: f64(a.max),
		})
	}
	return chs
}

func (s *Source) Poll(_ context.Context) ([]model.Reading, error) {
	t := s.now()
	hour := float64(t.Hour()) + float64(t.Minute())/60 + float64(t.Second())/3600
	r := rand.New(rand.NewPCG(7, uint64(t.Unix())))

	readings := make([]model.Reading, 0, len(appliances)+2)
	total := baseWatts
	for i, a := range appliances {
		w := a.watts(hour, r)
		total += w
		readings = append(readings, model.Reading{ChannelID: circuitID(i), Watts: w, Volts: f64(a.volts), Amps: f64(w / a.volts)})
	}
	// The two legs share the load; the 240 V loads draw equally from both.
	legA, legB := total/2*1.02, total/2*0.98
	readings = append(readings,
		model.Reading{ChannelID: mainsID("a"), Watts: legA, Volts: f64(121), Amps: f64(legA / 121)},
		model.Reading{ChannelID: mainsID("b"), Watts: legB, Volts: f64(120), Amps: f64(legB / 120)},
	)
	return readings, nil
}
