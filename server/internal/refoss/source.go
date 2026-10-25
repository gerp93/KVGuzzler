package refoss

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/gerp93/KVGuzzler/server/internal/model"
)

// What the EM16P's Refoss.Status.Get really answers (a captured answer is internal/refoss/testdata/status-em16p.json): one object per
// input, "em:1" to "em:18", each with current (A), voltage (V), power (W, signed: a clamp on backwards reads negative on a load), pf and
// running energies. The inputs come in three groups of six, named by the phase whose voltage they use: A1-A6 (em:1-6), B1-B6 (em:7-12)
// and C1-C6 (em:13-18). On a US split-phase panel the A and B groups read about 118 V and the C group sees no voltage at all. A 240 V
// breaker has a clamp on each of its two legs, one on an A input and one on a B input, and its power is the two added.

const (
	// MaxWatts is the most one input can carry: a reading above it is nonsense and is dropped (a 200 A service is under 25 kW a leg).
	MaxWatts = 30000.0

	// minVolts: an input whose voltage reference reads less than this has no phase wired to it and is not reported.
	minVolts = 50.0

	// A negative reading smaller than this (in watts) is noise on an idle clamp, not a backwards one.
	backwardsWatts = 1.0
)

// ErrNotARefoss means the device answered but not with Refoss readings.
var ErrNotARefoss = errors.New("the device answered, but not like a Refoss energy monitor. Is this the monitor's address?")

// Input is one input's reading from Refoss.Status.Get.
type Input struct {
	N       int
	Current float64
	Voltage float64
	Power   float64
	PF      float64
}

// ParseStatus reads a Refoss.Status.Get answer into its inputs, by number. Anything that is not an "em:N" object with a numeric power is
// ignored (the answer also carries wifi, cloud and system sections); an answer with no input at all is not a Refoss's.
func ParseStatus(body []byte) (map[int]Input, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, ErrNotARefoss
	}
	out := map[int]Input{}
	for key, v := range raw {
		n, ok := inputNumber(key)
		if !ok {
			continue
		}
		var f struct {
			Current *float64 `json:"current"`
			Voltage *float64 `json:"voltage"`
			Power   *float64 `json:"power"`
			PF      *float64 `json:"pf"`
		}
		if json.Unmarshal(v, &f) != nil || f.Power == nil || f.Voltage == nil {
			continue
		}
		in := Input{N: n, Power: *f.Power, Voltage: *f.Voltage}
		if f.Current != nil {
			in.Current = *f.Current
		}
		if f.PF != nil {
			in.PF = *f.PF
		}
		out[n] = in
	}
	if len(out) == 0 {
		return nil, ErrNotARefoss
	}
	return out, nil
}

// inputNumber is 7 for "em:7".
func inputNumber(key string) (int, bool) {
	rest, ok := strings.CutPrefix(key, "em:")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	return n, err == nil && n >= 1 && n <= 64
}

// InputLabel is the name an input has on the monitor itself: A1 to A6, B1 to B6, C1 to C6.
func InputLabel(n int) string {
	return fmt.Sprintf("%c%d", 'A'+rune((n-1)/6), (n-1)%6+1)
}

// SupportedModel says whether the readings of this model have been seen and mapped. Only the EM16P has been.
func SupportedModel(model string) bool { return strings.EqualFold(model, "em16p") }

const inputsOf = 18

// Setting is what the user has chosen for one meter.
type Setting struct {
	// Invert flips the sign of the clamp's reading: for a clamp put on the other way round.
	Invert bool
	// PairWith is the meter of the other leg of the same 240 V breaker. The meter that has it reports both legs added; the other
	// leg is then not reported on its own.
	PairWith string
}

// SettingsFunc gives the meter settings by meter id. It is asked on every poll, so a change takes effect at once.
type SettingsFunc func(ctx context.Context) (map[string]Setting, error)

// Source polls one monitor. It only ever reads.
type Source struct {
	host, name, model string
	id                string // the device id: made from the address when the monitor is added, and kept if the address changes
	client            *Client
	settings          SettingsFunc

	mu     sync.Mutex
	notes  []string
	active []string
}

// NewSource is a source for the monitor at host (a plain host or IP). model comes from DeviceInfo and must be one that is supported.
func NewSource(host, name, model string, settings SettingsFunc, hc *http.Client) (*Source, error) {
	if !SupportedModel(model) {
		return nil, fmt.Errorf("Refoss %s is not supported yet: only the EM16P has been tried", strings.ToUpper(model))
	}
	c, err := New(host, hc)
	if err != nil {
		return nil, err
	}
	return &Source{id: DeviceID(host), host: host, name: name, model: strings.ToLower(model), client: c, settings: settings}, nil
}

// NewSourceWithBase is NewSource for a monitor at an address that is not a plain host (a test server with a port). Production code uses
// NewSource, which only accepts a plain host or IP.
func NewSourceWithBase(base, host, name, model string, settings SettingsFunc, hc *http.Client) *Source {
	return &Source{id: DeviceID(host), host: host, name: name, model: strings.ToLower(model), client: NewWithBase(base, hc), settings: settings}
}

// DeviceID is the id a monitor at host has.
func DeviceID(host string) string { return "refoss-" + host }

// SetIdentity gives the source the device id it was first saved under, and the settings of that device. A monitor's id is made from its
// address when it is added; if it later moves to another address it keeps that id, so its meters, their wiring and their history stay what they were.
func (s *Source) SetIdentity(id string, settings SettingsFunc) {
	s.id = id
	s.settings = settings
}

func (s *Source) meterID(n int) string { return fmt.Sprintf("%s:em%d", s.id, n) }

func (s *Source) Device() model.Device {
	name := s.name
	if name == "" {
		name = "Refoss " + strings.ToUpper(s.model)
	}
	return model.Device{ID: s.id, Kind: "refoss", Host: s.host, Name: name, Model: s.model}
}

// Meters are every input of the monitor, named as the monitor names them. The ones nothing is clamped on are switched off when the
// device is added, and the user switches on the ones they use.
func (s *Source) Meters() []model.Meter {
	max := MaxWatts
	out := make([]model.Meter, 0, inputsOf)
	for n := 1; n <= inputsOf; n++ {
		idx := n
		out = append(out, model.Meter{
			ID: s.meterID(n), DeviceID: s.id, Index: &idx, Scope: model.ScopeCircuit, Label: InputLabel(n), Enabled: true, MaxWatts: &max,
		})
	}
	return out
}

// Poll reads every input once. A meter's power is its clamp's, with its sign flipped if the user said so, and with the other leg added
// when it is paired. A reading that is still negative after that is dropped and said so in the notes: nothing here has solar.
func (s *Source) Poll(ctx context.Context) ([]model.Reading, error) {
	body, err := s.client.StatusRaw(ctx)
	if err != nil {
		return nil, err
	}
	inputs, err := ParseStatus(body)
	if err != nil {
		return nil, err
	}
	var settings map[string]Setting
	if s.settings != nil {
		if settings, err = s.settings(ctx); err != nil {
			settings = nil // a settings problem must not stop the readings: they are then shown as the clamp gives them
		}
	}

	// A leg that is paired into another meter is reported there, not on its own.
	partner := map[string]bool{}
	for _, st := range settings {
		if st.PairWith != "" {
			partner[st.PairWith] = true
		}
	}
	watts := func(n int) (float64, bool) {
		in, ok := inputs[n]
		if !ok || in.Voltage < minVolts {
			return 0, false
		}
		w := in.Power
		if settings[s.meterID(n)].Invert {
			w = -w
		}
		return w, true
	}

	// amps and volts of an input: its clamp's current (never negative) and the voltage it is measured against.
	var readings []model.Reading
	var notes, active []string
	for n := 1; n <= inputsOf; n++ {
		id := s.meterID(n)
		// An input with a load on it, whichever way round its clamp is: a little current, or a little power either way.
		if in, ok := inputs[n]; ok && in.Voltage >= minVolts && (math.Abs(in.Power) >= backwardsWatts || in.Current >= 0.05) {
			active = append(active, id)
		}
		if partner[id] {
			continue
		}
		w, ok := watts(n)
		if !ok {
			continue
		}
		label := InputLabel(n)
		volts, amps := inputs[n].Voltage, math.Abs(inputs[n].Current)
		if other := settings[id].PairWith; other != "" {
			if m, ok := inputOfMeter(other); ok {
				if w2, ok := watts(m); ok {
					w += w2
					label += "+" + InputLabel(m)
					// One 240 V breaker carries the same current through both of its legs: the current is the larger leg's, never the two added,
					// while the voltage across the load is the two legs' (each is measured against its own phase) added.
					volts += inputs[m].Voltage
					amps = math.Max(amps, math.Abs(inputs[m].Current))
				}
			}
		}
		if w < 0 {
			if w <= -backwardsWatts {
				notes = append(notes, fmt.Sprintf("%s on the Refoss reads %.0f W: a load should read positive. Flip that clamp, or switch on \"Flip sign\" for it in Settings.", label, w))
			}
			continue
		}
		readings = append(readings, model.Reading{MeterID: id, Watts: w, Volts: &volts, Amps: &amps})
	}
	s.mu.Lock()
	s.notes, s.active = notes, active
	s.mu.Unlock()
	return readings, nil
}

// Notes are what the last poll found that needs attention (a clamp on backwards).
func (s *Source) Notes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.notes...)
}

// Active are the meters that had something clamped on them at the last poll (a load on them, whichever way round the clamp is). The
// others are switched off when the monitor is added, so the dashboard does not list a row of zeros.
func (s *Source) Active() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.active...)
}

// inputOfMeter is 8 for "refoss-192.168.68.88:em8".
func inputOfMeter(meterID string) (int, bool) {
	i := strings.LastIndex(meterID, ":em")
	if i < 0 {
		return 0, false
	}
	n, err := strconv.Atoi(meterID[i+3:])
	return n, err == nil && n >= 1 && n <= inputsOf
}
