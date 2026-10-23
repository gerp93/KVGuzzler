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
	"time"
	"unicode/utf8"

	"github.com/gerp93/KVGuzzler/server/internal/collector"
	"github.com/gerp93/KVGuzzler/server/internal/hostcheck"
	"github.com/gerp93/KVGuzzler/server/internal/model"
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
}

func (m *Manager) builder() func(string, string, string, model.Scope, *http.Client) (source.Source, error) {
	if m.build != nil {
		return m.build
	}
	return Build
}

// Build makes the source for a device kind. Only Shelly plugs are supported so far: a Refoss monitor needs its readings
// mapped from a real answer first (see cmd/refoss-probe).
func Build(kind, host, name string, scope model.Scope, hc *http.Client) (source.Source, error) {
	switch kind {
	case "shelly_plug":
		return shelly.NewSource(host, name, scope, hc)
	case "refoss":
		return nil, fail(ErrUnsupported, "Refoss monitors are not supported yet, because the field names of their readings are not confirmed. Run refoss-probe against the monitor first.")
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
	switch nd.Scope {
	case "", string(model.ScopePlug):
	case string(model.ScopePC):
		scope = model.ScopePC
	default:
		return model.Device{}, fail(ErrInvalid, "The scope of a plug is plug or pc.")
	}
	if err := hostcheck.Validate(nd.Host); err != nil {
		return model.Device{}, fail(ErrInvalid, "The address must be a plain host name or IP address, not a URL, path or port.")
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
	d := src.Device()
	d.Online = true
	return d, nil
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
