// Package devices adds, removes and restores the devices the server polls. A device is added only after it has answered
// once, so a typo or the wrong address is refused up front instead of silently never reporting.
package devices

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gerp93/KVGuzzler/server/internal/collector"
	"github.com/gerp93/KVGuzzler/server/internal/hostcheck"
	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/refoss"
	"github.com/gerp93/KVGuzzler/server/internal/shelly"
	"github.com/gerp93/KVGuzzler/server/internal/source"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

// The kinds of failure, so the API can answer each with the right status. Every error returned here is one of them
// (errors.Is) and its text is fit to show the user as it is.
var (
	ErrInvalid     = errors.New("invalid device")
	ErrUnsupported = errors.New("unsupported device")
	ErrUnreachable = errors.New("device did not answer")
	ErrExists      = errors.New("device already added")
	ErrNotFound    = errors.New("no such device")
)

type kindError struct {
	kind error
	msg  string
}

func (e *kindError) Error() string        { return e.msg }
func (e *kindError) Is(target error) bool { return target == e.kind }

func fail(kind error, format string, args ...any) error {
	return &kindError{kind: kind, msg: fmt.Sprintf(format, args...)}
}

// NewDevice is what the user supplies to start polling a device.
type NewDevice struct {
	Kind  string `json:"kind"`
	Host  string `json:"host"`
	Name  string `json:"name"`
	Scope string `json:"scope"`
}

const maxName = 60

type Manager struct {
	Store     *store.Store
	Collector *collector.Collector
	HTTP      *http.Client
	// ProbeTimeout is how long a new device has to answer once; zero means 6 seconds.
	ProbeTimeout time.Duration

	// build makes a source; tests replace it to point at a local server.
	build func(kind, host, name string, scope model.Scope, hc *http.Client) (source.Source, error)

	// detect asks a Refoss monitor what model it is; tests replace it.
	detect func(ctx context.Context, host string, hc *http.Client) (string, error)

	mu     sync.Mutex
	models map[string]string // the model of each Refoss monitor by host, found when it is added and kept with the device
}

func (m *Manager) builder() func(string, string, string, model.Scope, *http.Client) (source.Source, error) {
	if m.build != nil {
		return m.build
	}
	return m.buildDefault
}

// buildDefault is Build, except that a Refoss monitor's source also needs its model and a way to read what the user said about its clamps.
func (m *Manager) buildDefault(kind, host, name string, scope model.Scope, hc *http.Client) (source.Source, error) {
	if kind != "refoss" {
		return Build(kind, host, name, scope, hc)
	}
	m.mu.Lock()
	modelName := m.models[host]
	m.mu.Unlock()
	if modelName == "" {
		return nil, fail(ErrInvalid, "The model of the Refoss monitor at %s is not known.", host)
	}
	src, err := refoss.NewSource(host, name, modelName, m.refossSettings(refoss.DeviceID(host)), hc)
	if err != nil {
		return nil, fail(ErrUnsupported, "%s", sentence(err))
	}
	return src, nil
}

// refossSettings reads, on each poll, how the user said a monitor's clamps are wired (which way round, which legs are one breaker).
func (m *Manager) refossSettings(deviceID string) refoss.SettingsFunc {
	return func(ctx context.Context) (map[string]refoss.Setting, error) {
		chs, err := m.Store.DeviceChannels(ctx, deviceID)
		if err != nil {
			return nil, err
		}
		out := make(map[string]refoss.Setting, len(chs))
		for _, c := range chs {
			s := refoss.Setting{Invert: c.Invert}
			if c.PairWith != nil {
				s.PairWith = *c.PairWith
			}
			out[c.ID] = s
		}
		return out, nil
	}
}

func (m *Manager) setModel(host, modelName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.models == nil {
		m.models = map[string]string{}
	}
	m.models[host] = modelName
}

// detectModel asks a Refoss monitor what it is (a plain, read-only call that needs no login).
func (m *Manager) detectModel(ctx context.Context, host string) (string, error) {
	if m.detect != nil {
		return m.detect(ctx, host, m.HTTP)
	}
	c, err := refoss.New(host, m.HTTP)
	if err != nil {
		return "", err
	}
	info, err := c.DeviceInfo(ctx)
	if err != nil {
		return "", err
	}
	return info.Model, nil
}

// Build makes the source for a device kind. A Refoss monitor is built by the Manager, which knows its model and the user's clamp settings.
func Build(kind, host, name string, scope model.Scope, hc *http.Client) (source.Source, error) {
	switch kind {
	case "shelly_plug":
		return shelly.NewSource(host, name, scope, hc)
	case "refoss":
		return nil, fail(ErrUnsupported, "A Refoss monitor's source needs its model: add it through the manager.")
	default:
		return nil, fail(ErrInvalid, "The kind of device is shelly_plug or refoss.")
	}
}

func (m *Manager) probeTimeout() time.Duration {
	if m.ProbeTimeout > 0 {
		return m.ProbeTimeout
	}
	return 6 * time.Second
}

// Add starts polling a new device. It is read once first; if it does not answer like the kind named, nothing is saved.
func (m *Manager) Add(ctx context.Context, nd NewDevice) (model.Device, error) {
	name := strings.TrimSpace(nd.Name)
	if utf8.RuneCountInString(name) > maxName {
		return model.Device{}, fail(ErrInvalid, "A name is %d characters at most.", maxName)
	}
	scope := model.ScopePlug
	switch {
	case nd.Kind == "refoss":
		// Every input of a monitor is a circuit until the user says otherwise (a monitor on the main panel has its mains inputs set in Settings).
		scope = model.ScopeCircuit
		if nd.Scope != "" {
			return model.Device{}, fail(ErrInvalid, "A Refoss monitor has no scope to choose: set each input in Settings.")
		}
	case nd.Scope == "", nd.Scope == string(model.ScopePlug):
	case nd.Scope == string(model.ScopePC):
		scope = model.ScopePC
	default:
		return model.Device{}, fail(ErrInvalid, "The scope of a plug is plug or pc.")
	}
	if err := hostcheck.Validate(nd.Host); err != nil {
		return model.Device{}, fail(ErrInvalid, "The address must be a plain host name or IP address, not a URL, path or port.")
	}

	if nd.Kind == "refoss" {
		detectCtx, cancel := context.WithTimeout(ctx, m.probeTimeout())
		modelName, err := m.detectModel(detectCtx, nd.Host)
		cancel()
		if err != nil {
			return model.Device{}, fail(ErrUnreachable, "%s", sentence(err))
		}
		if !refoss.SupportedModel(modelName) {
			return model.Device{}, fail(ErrUnsupported, "This is a Refoss %s. Only the EM16P has been tried so far.", strings.ToUpper(modelName))
		}
		m.setModel(nd.Host, modelName)
	}
	src, err := m.builder()(nd.Kind, nd.Host, name, scope, m.HTTP)
	if err != nil {
		return model.Device{}, err
	}
	for _, d := range m.Collector.Devices() {
		if d.ID == src.Device().ID {
			return model.Device{}, fail(ErrExists, "%s is already being polled.", nd.Host)
		}
	}

	probeCtx, cancel := context.WithTimeout(ctx, m.probeTimeout())
	defer cancel()
	if _, err := src.Poll(probeCtx); err != nil {
		return model.Device{}, fail(ErrUnreachable, "%s", sentence(err))
	}
	if err := m.Collector.Add(ctx, src); err != nil {
		return model.Device{}, err
	}
	if a, ok := src.(interface{ Active() []string }); ok {
		// A monitor has many inputs and most have nothing clamped on them: only the ones in use are shown at first.
		if err := m.switchOffIdle(ctx, src, a.Active()); err != nil {
			log.Printf("could not switch off the idle inputs of %s: %v", src.Device().ID, err)
		}
	}
	if scope == model.ScopePC {
		// The device is added either way; if it cannot be tied to a computer now, the user can do it in Settings.
		if err := m.tieToAComputer(ctx, src); err != nil {
			log.Printf("could not tie %s to a computer: %v", src.Device().ID, err)
		}
	}
	d := src.Device()
	d.Online = true
	return d, nil
}

// switchOffIdle hides the channels of a new device that had nothing on them at the first reading (the user switches on the ones they use).
func (m *Manager) switchOffIdle(ctx context.Context, src source.Source, active []string) error {
	live := map[string]bool{}
	for _, id := range active {
		live[id] = true
	}
	off := false
	for _, ch := range src.Channels() {
		if live[ch.ID] {
			continue
		}
		if _, err := m.Store.UpdateChannel(ctx, ch.ID, nil, &off); err != nil {
			return err
		}
	}
	return nil
}

// tieToAComputer makes a PC's wall plug part of a computer, so it is shown with the computer and not on its own: the one
// computer that has no wall plug yet (a PC collector adds one when it first reports), or a new computer named after the plug
// when there is none or more than one to choose from. A plug that already belongs to a computer (added again after being
// removed) is left where it was.
func (m *Manager) tieToAComputer(ctx context.Context, src source.Source) error {
	chs := src.Channels()
	if len(chs) == 0 {
		return nil
	}
	id := chs[0].ID
	if have, err := m.Store.Channel(ctx, id); err == nil && have.ComputerID != nil {
		return nil
	}
	all, err := m.Store.Computers(ctx)
	if err != nil {
		return err
	}
	var free []model.Computer
	for _, c := range all {
		if c.WallChannelID == nil {
			free = append(free, c)
		}
	}
	var target string
	if len(free) == 1 {
		target = free[0].ID
	} else {
		name := src.Device().Name
		if utf8.RuneCountInString(name) > maxName {
			name = string([]rune(name)[:maxName])
		}
		c, err := m.Store.CreateComputer(ctx, name, nil)
		if err != nil {
			return err
		}
		target = c.ID
	}
	_, err = m.Store.AssignChannel(ctx, id, &target)
	return err
}

// Remove stops polling a device. What it recorded stays in the history.
func (m *Manager) Remove(ctx context.Context, id string) error {
	found := m.Collector.Remove(id)
	if err := m.Store.RemoveDevice(ctx, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			if found {
				return nil // polled but never saved: nothing left to forget
			}
			return fail(ErrNotFound, "No such device: %s.", id)
		}
		return err
	}
	return nil
}

// Restore starts polling every saved device again after a restart. A device that is not answering right now is still
// restored (it shows offline until it comes back). One of a kind the server cannot poll is skipped and logged.
func (m *Manager) Restore(ctx context.Context) error {
	saved, err := m.Store.Devices(ctx)
	if err != nil {
		return err
	}
	polling := map[string]bool{}
	for _, d := range m.Collector.Devices() {
		polling[d.ID] = true
	}
	for _, d := range saved {
		if polling[d.ID] {
			continue
		}
		chs, err := m.Store.DeviceChannels(ctx, d.ID)
		if err != nil {
			return err
		}
		scope := model.ScopePlug
		if len(chs) > 0 {
			scope = chs[0].Scope
		}
		if d.Kind == "refoss" {
			m.setModel(d.Host, d.Model)
		}
		src, err := m.builder()(d.Kind, d.Host, d.Name, scope, m.HTTP)
		if err != nil {
			log.Printf("not polling saved device %s: %v", d.ID, err)
			continue
		}
		if err := m.Collector.Add(ctx, src); err != nil {
			return err
		}
	}
	return nil
}

// sentence is an error's text as a sentence starting with a capital letter and ending with a full stop.
func sentence(err error) string {
	s := strings.TrimSpace(err.Error())
	if s == "" {
		return "The device did not answer."
	}
	s = strings.ToUpper(s[:1]) + s[1:]
	if !strings.HasSuffix(s, ".") && !strings.HasSuffix(s, "?") {
		s += "."
	}
	return s
}
