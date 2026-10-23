package devices

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/collector"
	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/shelly"
	"github.com/gerp93/KVGuzzler/server/internal/source"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

const plugAnswer = `{"id":0,"output":true,"apower":52.1,"voltage":121.3,"current":0.43,"aenergy":{"total":1234.5}}`

// fixture is a store, a collector and a manager whose plugs are answered by one local test server.
type fixture struct {
	st    *store.Store
	srv   *httptest.Server
	code  *int
	build func(kind, host, name string, scope model.Scope, hc *http.Client) (source.Source, error)
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		_, _ = w.Write([]byte(plugAnswer))
	}))
	t.Cleanup(srv.Close)

	f := &fixture{st: st, srv: srv, code: &status}
	f.build = func(kind, host, name string, scope model.Scope, hc *http.Client) (source.Source, error) {
		if kind != "shelly_plug" {
			return Build(kind, host, name, scope, hc)
		}
		return shelly.NewSourceWithBase(srv.URL, host, name, scope, srv.Client()), nil
	}
	return f
}

// manager returns a manager over a fresh collector, as after a restart.
func (f *fixture) manager() (*Manager, *collector.Collector) {
	c := collector.New(f.st, time.Hour) // never ticks by itself: nothing here runs the collector
	return &Manager{Store: f.st, Collector: c, build: f.build, ProbeTimeout: time.Second}, c
}

func TestAddReadsTheDeviceOnceAndKeepsIt(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	m, c := f.manager()

	d, err := m.Add(ctx, NewDevice{Kind: "shelly_plug", Host: "192.168.1.50", Name: "  Desktop PC ", Scope: "pc"})
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != "shelly-192.168.1.50" || d.Name != "Desktop PC" || !d.Online || d.Kind != "shelly_plug" {
		t.Errorf("device = %+v", d)
	}
	if got := c.Devices(); len(got) != 1 || got[0].ID != d.ID {
		t.Errorf("collector devices = %+v", got)
	}
	chs, _ := f.st.DeviceChannels(ctx, d.ID)
	if len(chs) != 1 || chs[0].Scope != model.ScopePC || chs[0].Label != "Desktop PC" {
		t.Errorf("channels = %+v", chs)
	}
}

func TestAddRefusesWhatItShould(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	m, c := f.manager()

	cases := []struct {
		name string
		nd   NewDevice
		want error
	}{
		{"host with a path", NewDevice{Kind: "shelly_plug", Host: "192.168.1.50/rpc/Shelly.Reboot"}, ErrInvalid},
		{"host as a URL", NewDevice{Kind: "shelly_plug", Host: "http://192.168.1.50"}, ErrInvalid},
		{"empty host", NewDevice{Kind: "shelly_plug"}, ErrInvalid},
		{"bad scope", NewDevice{Kind: "shelly_plug", Host: "192.168.1.50", Scope: "mains"}, ErrInvalid},
		{"name too long", NewDevice{Kind: "shelly_plug", Host: "192.168.1.50", Name: strings.Repeat("x", 61)}, ErrInvalid},
		{"unknown kind", NewDevice{Kind: "toaster", Host: "192.168.1.50"}, ErrInvalid},
		{"refoss is not mapped yet", NewDevice{Kind: "refoss", Host: "192.168.1.51"}, ErrUnsupported},
	}
	for _, tc := range cases {
		if _, err := m.Add(ctx, tc.nd); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	if len(c.Devices()) != 0 {
		t.Errorf("a refused device was started: %+v", c.Devices())
	}
	if saved, _ := f.st.Devices(ctx); len(saved) != 0 {
		t.Errorf("a refused device was saved: %+v", saved)
	}
}

func TestAnUnreachableOrWrongDeviceIsNotSaved(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	m, c := f.manager()

	*f.code = http.StatusNotFound // answers, but not like a Shelly
	_, err := m.Add(ctx, NewDevice{Kind: "shelly_plug", Host: "192.168.1.50"})
	if !errors.Is(err, ErrUnreachable) || !strings.Contains(err.Error(), "Shelly") {
		t.Errorf("err = %v, want ErrUnreachable saying it is not a Shelly", err)
	}
	if msg := err.Error(); strings.Contains(msg, "did not answer:") || !strings.HasSuffix(msg, ".") || msg[0] < 'A' || msg[0] > 'Z' {
		t.Errorf("message %q should be a clean sentence for the user, with no internal prefix", msg)
	}
	if saved, _ := f.st.Devices(ctx); len(saved) != 0 || len(c.Devices()) != 0 {
		t.Errorf("saved %v, polled %v: nothing should be kept", saved, c.Devices())
	}

	*f.code = http.StatusOK // the same address works once it answers
	if _, err := m.Add(ctx, NewDevice{Kind: "shelly_plug", Host: "192.168.1.50"}); err != nil {
		t.Errorf("after it started answering: %v", err)
	}
}

func TestAddingTheSameDeviceTwice(t *testing.T) {
	ctx := context.Background()
	m, _ := newFixture(t).manager()
	nd := NewDevice{Kind: "shelly_plug", Host: "192.168.1.50"}
	if _, err := m.Add(ctx, nd); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Add(ctx, nd); !errors.Is(err, ErrExists) {
		t.Errorf("err = %v, want ErrExists", err)
	}
}

func TestRemoveAndRestore(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	m, _ := f.manager()
	pc, err := m.Add(ctx, NewDevice{Kind: "shelly_plug", Host: "192.168.1.50", Name: "Desktop PC", Scope: "pc"})
	if err != nil {
		t.Fatal(err)
	}
	gone, err := m.Add(ctx, NewDevice{Kind: "shelly_plug", Host: "192.168.1.60", Name: "Old fridge"})
	if err != nil {
		t.Fatal(err)
	}
	// The user renames the PC's channel; a restart must not undo that.
	label := "My desktop"
	if _, err := f.st.UpdateChannel(ctx, pc.ID+":plug", &label, nil); err != nil {
		t.Fatal(err)
	}

	if err := m.Remove(ctx, gone.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.Remove(ctx, gone.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("removing twice: %v, want ErrNotFound", err)
	}
	if err := m.Remove(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("removing unknown: %v, want ErrNotFound", err)
	}

	// A restart: a new collector over the same database.
	m2, c2 := f.manager()
	if err := m2.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	got := c2.Devices()
	if len(got) != 1 || got[0].ID != pc.ID {
		t.Fatalf("restored devices = %+v, want only the PC plug", got)
	}
	chs, _ := f.st.DeviceChannels(ctx, pc.ID)
	if len(chs) != 1 || chs[0].Scope != model.ScopePC || chs[0].Label != "My desktop" {
		t.Errorf("channel after restore = %+v, want scope pc and the user's label", chs)
	}
	if old, _ := f.st.DeviceChannels(ctx, gone.ID); len(old) != 1 {
		t.Error("a removed device's channel (its history) was lost")
	}

	// Restoring again changes nothing.
	if err := m2.Restore(ctx); err != nil || len(c2.Devices()) != 1 {
		t.Errorf("second restore: %v, %d devices", err, len(c2.Devices()))
	}
}

func TestRestoreSkipsWhatItCannotPoll(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	if err := f.st.UpsertDevice(ctx, model.Device{ID: "sim-em16p", Kind: "refoss", Name: "Simulated house"}); err != nil {
		t.Fatal(err)
	}
	m, c := f.manager()
	if err := m.Restore(ctx); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if len(c.Devices()) != 0 {
		t.Errorf("devices = %+v", c.Devices())
	}
}

func TestADeviceThatIsOfflineAtRestartIsStillRestored(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	m, _ := f.manager()
	if _, err := m.Add(ctx, NewDevice{Kind: "shelly_plug", Host: "192.168.1.50"}); err != nil {
		t.Fatal(err)
	}
	*f.code = http.StatusInternalServerError // down at restart

	m2, c2 := f.manager()
	if err := m2.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	if got := c2.Devices(); len(got) != 1 || got[0].Online {
		t.Errorf("devices = %+v, want one that is not (yet) online", got)
	}
}

func TestAPcPlugJoinsTheOneComputerWithoutAWallPlugOtherwiseGetsItsOwn(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	m, _ := f.manager()
	wallOf := func(computerID string) *string {
		c, err := f.st.Computer(ctx, computerID)
		if err != nil {
			t.Fatal(err)
		}
		return c.WallChannelID
	}

	// A collector has already added a computer: the plug joins it.
	desk, err := f.st.CreateComputer(ctx, "Desktop", nil)
	if err != nil {
		t.Fatal(err)
	}
	pc, err := m.Add(ctx, NewDevice{Kind: "shelly_plug", Host: "192.168.1.50", Name: "PC", Scope: "pc"})
	if err != nil {
		t.Fatal(err)
	}
	if w := wallOf(desk.ID); w == nil || *w != pc.ID+":plug" {
		t.Errorf("the desktop's wall plug = %v, want %s", w, pc.ID+":plug")
	}

	// No computer is free any more: another PC plug gets a computer of its own, named after it.
	if _, err := m.Add(ctx, NewDevice{Kind: "shelly_plug", Host: "192.168.1.51", Name: "Laptop", Scope: "pc"}); err != nil {
		t.Fatal(err)
	}
	all, _ := f.st.Computers(ctx)
	if len(all) != 2 {
		t.Fatalf("computers = %+v, want the desktop and a new laptop", all)
	}
	var laptop *model.Computer
	for i := range all {
		if all[i].Name == "Laptop" {
			laptop = &all[i]
		}
	}
	if laptop == nil || laptop.WallChannelID == nil {
		t.Errorf("laptop = %+v", laptop)
	}

	// An ordinary plug does not create or join a computer.
	if _, err := m.Add(ctx, NewDevice{Kind: "shelly_plug", Host: "192.168.1.60", Name: "Fridge"}); err != nil {
		t.Fatal(err)
	}
	if after, _ := f.st.Computers(ctx); len(after) != 2 {
		t.Errorf("an ordinary plug changed the computers: %+v", after)
	}
}

func TestAPcPlugAddedAgainAfterBeingRemovedKeepsItsComputer(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	m, _ := f.manager()
	pc, err := m.Add(ctx, NewDevice{Kind: "shelly_plug", Host: "192.168.1.50", Name: "PC", Scope: "pc"})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := f.st.Computers(ctx)
	if len(before) != 1 {
		t.Fatalf("computers = %+v", before)
	}
	if err := m.Remove(ctx, pc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Add(ctx, NewDevice{Kind: "shelly_plug", Host: "192.168.1.50", Name: "PC", Scope: "pc"}); err != nil {
		t.Fatal(err)
	}
	after, _ := f.st.Computers(ctx)
	if len(after) != 1 || after[0].ID != before[0].ID || after[0].WallChannelID == nil {
		t.Errorf("computers = %+v, want the same one with its wall plug still tied", after)
	}
}
