package devices

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/collector"
	"github.com/gerp93/KVGuzzler/server/internal/hostcheck"
	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/refoss"
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

	refossStatus, err := os.ReadFile("../refoss/testdata/status-em16p.json") // what a real EM16P answered
	if err != nil {
		t.Fatal(err)
	}
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		if r.URL.Path == "/rpc/Refoss.Status.Get" {
			_, _ = w.Write(refossStatus)
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
	m := &Manager{Store: f.st, Collector: c, ProbeTimeout: time.Second}
	m.detect = func(context.Context, string, *http.Client) (string, error) { return "em16p", nil }
	m.build = func(kind, host, name string, scope model.Scope, hc *http.Client) (source.Source, error) {
		if kind == "refoss" {
			if err := hostcheck.Validate(host); err != nil { // as the real builder would: the simulated house is saved as a Refoss with no address
				return nil, fail(ErrInvalid, "bad address")
			}
			return refoss.NewSourceWithBase(f.srv.URL, host, name, "em16p", m.refossSettings(refoss.DeviceID(host)), f.srv.Client()), nil
		}
		return f.build(kind, host, name, scope, hc)
	}
	return m, c
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
	chs, _ := f.st.DeviceMeters(ctx, d.ID)
	if len(chs) != 1 || chs[0].Scope != model.ScopePC || chs[0].Label != "Desktop PC" {
		t.Errorf("meters = %+v", chs)
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
		{"a monitor has no scope to choose", NewDevice{Kind: "refoss", Host: "192.168.1.51", Scope: "pc"}, ErrInvalid},
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
	// The user renames the PC's meter; a restart must not undo that.
	label := "My desktop"
	if _, err := f.st.UpdateMeter(ctx, pc.ID+":plug", &label, nil); err != nil {
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
	chs, _ := f.st.DeviceMeters(ctx, pc.ID)
	if len(chs) != 1 || chs[0].Scope != model.ScopePC || chs[0].Label != "My desktop" {
		t.Errorf("meter after restore = %+v, want scope pc and the user's label", chs)
	}
	if old, _ := f.st.DeviceMeters(ctx, gone.ID); len(old) != 1 {
		t.Error("a removed device's meter (its history) was lost")
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
		return c.WallMeterID
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
	if laptop == nil || laptop.WallMeterID == nil {
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
	if len(after) != 1 || after[0].ID != before[0].ID || after[0].WallMeterID == nil {
		t.Errorf("computers = %+v, want the same one with its wall plug still tied", after)
	}
}

// ---- a Refoss energy monitor (the answers are what a real EM16P gave: A2 and B2, the EV charger, are clamped on backwards)

func refossReadings(t *testing.T, c *collector.Collector, id string) map[string]float64 {
	t.Helper()
	c.PollOnce(context.Background(), time.Now())
	out := map[string]float64{}
	for _, r := range c.LiveReadings() {
		out[r.MeterID] = r.Watts
	}
	_ = id
	return out
}

func TestAddingARefossMonitorKeepsItsInputsAndSwitchesOffTheOnesWithNothingOnThem(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	m, c := f.manager()

	d, err := m.Add(ctx, NewDevice{Kind: "refoss", Host: "192.168.68.88", Name: "Garage panel"})
	if err != nil || d.ID != "refoss-192.168.68.88" || d.Kind != "refoss" || d.Model != "em16p" || d.Name != "Garage panel" {
		t.Fatalf("device = %+v, err %v", d, err)
	}
	chs, _ := f.st.DeviceMeters(ctx, d.ID)
	if len(chs) != 18 {
		t.Fatalf("%d meters, want the 18 inputs", len(chs))
	}
	on := map[string]bool{}
	for _, ch := range chs {
		if ch.Enabled {
			on[ch.Label] = true
		}
		if ch.Scope != model.ScopeCircuit {
			t.Errorf("%s is a %s, want circuit", ch.Label, ch.Scope)
		}
	}
	// A1, A2, A3 and B1, B2, B3 have something on them (A2 and B2 backwards); the rest nothing, and C has no voltage at all.
	want := map[string]bool{"A1": true, "A2": true, "A3": true, "B1": true, "B2": true, "B3": true}
	if len(on) != len(want) {
		t.Errorf("switched on: %v, want %v", on, want)
	}
	for l := range want {
		if !on[l] {
			t.Errorf("%s should be on", l)
		}
	}

	if _, err := m.Add(ctx, NewDevice{Kind: "refoss", Host: "192.168.68.88"}); !errors.Is(err, ErrExists) {
		t.Errorf("adding it again: %v, want ErrExists", err)
	}
	_ = c
}

func TestARefossMonitorOfAModelThatWasNeverSeenOrThatDoesNotAnswerIsRefused(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	m, c := f.manager()

	m.detect = func(context.Context, string, *http.Client) (string, error) { return "em06p", nil }
	if _, err := m.Add(ctx, NewDevice{Kind: "refoss", Host: "192.168.68.88"}); !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "EM06P") {
		t.Errorf("an EM06P: %v, want unsupported saying so", err)
	}
	m.detect = func(context.Context, string, *http.Client) (string, error) {
		return "", errors.New("connection refused")
	}
	if _, err := m.Add(ctx, NewDevice{Kind: "refoss", Host: "192.168.68.88"}); !errors.Is(err, ErrUnreachable) {
		t.Errorf("no answer: %v, want unreachable", err)
	}
	if len(c.Devices()) != 0 {
		t.Errorf("a refused monitor was started: %+v", c.Devices())
	}
}

func TestABackwardsClampIsSaidSoAndFixedByFlippingItsSignOrPairingTheLegs(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	m, c := f.manager()
	if _, err := m.Add(ctx, NewDevice{Kind: "refoss", Host: "192.168.68.88"}); err != nil {
		t.Fatal(err)
	}
	id := func(n int) string { return "refoss-192.168.68.88:em" + string(rune('0'+n)) }

	// As the clamps are: the EV charger's two legs (inputs 2 and 8) read negative, so they report nothing and the collector says why.
	got := refossReadings(t, c, "")
	if got[id(1)] < 3700 || got[id(3)] < 18 || got[id(3)] > 19 {
		t.Errorf("readings = %v", got)
	}
	if _, ok := got[id(2)]; ok {
		t.Errorf("a backwards clamp reported %v W", got[id(2)])
	}
	notes := strings.Join(c.Notes(), " | ")
	if !strings.Contains(notes, "A2+B2") && !strings.Contains(notes, "A2 on the Refoss reads -3668") {
		t.Errorf("notes = %q, want a clamp reading backwards said plainly", notes)
	}

	// Flip the sign of input 2: it reads the 3.7 kW it really draws.
	yes := true
	if _, err := f.st.SetMeterWiring(ctx, id(2), store.WiringChange{Invert: &yes}); err != nil {
		t.Fatal(err)
	}
	if got = refossReadings(t, c, ""); got[id(2)] < 3660 || got[id(2)] > 3680 {
		t.Errorf("flipped input 2 = %v, want about 3668", got[id(2)])
	}

	// Flip input 8 too and pair the two legs: one circuit of 7.3 kW on input 2, and input 8 is no meter of its own any more.
	if _, err := f.st.SetMeterWiring(ctx, id(8), store.WiringChange{Invert: &yes}); err != nil {
		t.Fatal(err)
	}
	partner := id(8)
	if _, err := f.st.SetMeterWiring(ctx, id(2), store.WiringChange{SetPair: true, PairWith: &partner}); err != nil {
		t.Fatal(err)
	}
	got = refossReadings(t, c, "")
	if w := got[id(2)]; w < 7320 || w > 7350 {
		t.Errorf("the paired EV circuit = %v, want about 7334 (3668 + 3666)", w)
	}
	if _, ok := got[id(8)]; ok {
		t.Error("the second leg is still reported on its own")
	}
	if ch, _ := f.st.Meter(ctx, id(8)); ch.Enabled {
		t.Error("the second leg should be switched off once it is part of a circuit")
	}
	if n := strings.Join(c.Notes(), ""); strings.Contains(n, "Refoss reads") {
		t.Errorf("there is nothing backwards any more but the notes say %q", n)
	}

	// Unpairing brings the leg back as a meter of its own.
	if _, err := f.st.SetMeterWiring(ctx, id(2), store.WiringChange{SetPair: true}); err != nil {
		t.Fatal(err)
	}
	if ch, _ := f.st.Meter(ctx, id(8)); !ch.Enabled || ch.PairWith != nil {
		t.Errorf("after unpairing: %+v", ch)
	}
}

func TestARestartKeepsARefossMonitorAndItsModel(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	m, _ := f.manager()
	if _, err := m.Add(ctx, NewDevice{Kind: "refoss", Host: "192.168.68.88"}); err != nil {
		t.Fatal(err)
	}
	m2, c2 := f.manager() // as after a restart: nothing remembered but what is saved
	if err := m2.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	if ds := c2.Devices(); len(ds) != 1 || ds[0].ID != "refoss-192.168.68.88" || ds[0].Model != "em16p" {
		t.Errorf("restored = %+v", ds)
	}
}

// ---- changing a device's address or name

func TestAPlugThatMovesToANewAddressKeepsItsIdItsMeterAndItsHistory(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	m, c := f.manager()
	if _, err := m.Add(ctx, NewDevice{Kind: "shelly_plug", Host: "192.168.1.50", Name: "Desk"}); err != nil {
		t.Fatal(err)
	}
	if err := f.st.AddMinute(ctx, "shelly-192.168.1.50:plug", time.Now().Truncate(time.Minute), 52, 60, 60); err != nil {
		t.Fatal(err)
	}

	host, name := "192.168.1.99", "Desk lamp"
	d, err := m.Update(ctx, "shelly-192.168.1.50", UpdateDevice{Host: &host, Name: &name})
	if err != nil || d.ID != "shelly-192.168.1.50" || d.Host != host || d.Name != name {
		t.Fatalf("device = %+v, err %v", d, err)
	}
	saved, _ := f.st.Devices(ctx)
	if len(saved) != 1 || saved[0].ID != "shelly-192.168.1.50" || saved[0].Host != host || saved[0].Name != name {
		t.Errorf("saved = %+v", saved)
	}
	meters, _ := f.st.DeviceMeters(ctx, "shelly-192.168.1.50")
	if len(meters) != 1 || meters[0].ID != "shelly-192.168.1.50:plug" {
		t.Errorf("meters = %+v: the id must not follow the address", meters)
	}
	if ds := c.Devices(); len(ds) != 1 || ds[0].ID != "shelly-192.168.1.50" || ds[0].Host != host {
		t.Errorf("polled = %+v", ds)
	}
	c.PollOnce(ctx, time.Now())
	got := false
	for _, r := range c.LiveReadings() {
		got = got || r.MeterID == "shelly-192.168.1.50:plug"
	}
	if !got {
		t.Error("the plug at its new address is not read under its old meter id")
	}
	if tot, _ := f.st.Totals(ctx, "shelly-192.168.1.50:plug", time.Now().Add(-time.Hour), time.Now().Add(time.Hour)); tot.KWh <= 0 {
		t.Error("the history was lost")
	}

	// A restart brings it back at the new address, under the same id.
	m2, c2 := f.manager()
	if err := m2.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	if ds := c2.Devices(); len(ds) != 1 || ds[0].ID != "shelly-192.168.1.50" || ds[0].Host != host {
		t.Errorf("restored = %+v", ds)
	}
}

func TestAnAddressNothingAnswersAtIsRefusedAndTheOldOneKeepsBeingRead(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	m, c := f.manager()
	if _, err := m.Add(ctx, NewDevice{Kind: "shelly_plug", Host: "192.168.1.50"}); err != nil {
		t.Fatal(err)
	}
	*f.code = http.StatusInternalServerError
	host := "192.168.1.77"
	if _, err := m.Update(ctx, "shelly-192.168.1.50", UpdateDevice{Host: &host}); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("update: %v, want unreachable", err)
	}
	*f.code = http.StatusOK
	if ds := c.Devices(); len(ds) != 1 || ds[0].Host != "192.168.1.50" {
		t.Errorf("polled = %+v: the old address should still be read", ds)
	}
	saved, _ := f.st.Devices(ctx)
	if len(saved) != 1 || saved[0].Host != "192.168.1.50" {
		t.Errorf("saved = %+v", saved)
	}
}

func TestChangingADeviceChecksTheAddressTheNameAndWhatIsAlreadyPolled(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	m, _ := f.manager()
	for _, h := range []string{"192.168.1.50", "192.168.1.51"} {
		if _, err := m.Add(ctx, NewDevice{Kind: "shelly_plug", Host: h}); err != nil {
			t.Fatal(err)
		}
	}
	bad, taken, empty, long := "http://x/y", "192.168.1.51", " ", strings.Repeat("x", 61)
	for _, c := range []struct {
		u    UpdateDevice
		want error
	}{
		{UpdateDevice{Host: &bad}, ErrInvalid},
		{UpdateDevice{Host: &taken}, ErrExists},
		{UpdateDevice{Name: &empty}, ErrInvalid},
		{UpdateDevice{Name: &long}, ErrInvalid},
	} {
		if _, err := m.Update(ctx, "shelly-192.168.1.50", c.u); !errors.Is(err, c.want) {
			t.Errorf("%+v: %v, want %v", c.u, err, c.want)
		}
	}
	if _, err := m.Update(ctx, "shelly-nope", UpdateDevice{Name: &taken}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown device: %v", err)
	}
	// Changing only the name does not read the device again and does not move it.
	name := "Lamp"
	d, err := m.Update(ctx, "shelly-192.168.1.50", UpdateDevice{Name: &name})
	if err != nil || d.Host != "192.168.1.50" || d.Name != "Lamp" {
		t.Errorf("rename: %+v %v", d, err)
	}
}

func TestAMonitorThatMovesKeepsItsInputsAndTheirWiring(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	m, c := f.manager()
	if _, err := m.Add(ctx, NewDevice{Kind: "refoss", Host: "192.168.68.88", Name: "Garage panel"}); err != nil {
		t.Fatal(err)
	}
	yes := true
	if _, err := f.st.SetMeterWiring(ctx, "refoss-192.168.68.88:em2", store.WiringChange{Invert: &yes}); err != nil {
		t.Fatal(err)
	}
	host := "192.168.68.89"
	if _, err := m.Update(ctx, "refoss-192.168.68.88", UpdateDevice{Host: &host}); err != nil {
		t.Fatal(err)
	}
	if ds := c.Devices(); len(ds) != 1 || ds[0].ID != "refoss-192.168.68.88" || ds[0].Host != host || ds[0].Model != "em16p" {
		t.Fatalf("polled = %+v", ds)
	}
	c.PollOnce(ctx, time.Now())
	got := map[string]float64{}
	for _, r := range c.LiveReadings() {
		got[r.MeterID] = r.Watts
	}
	if w := got["refoss-192.168.68.88:em2"]; w < 3660 || w > 3680 {
		t.Errorf("A2 = %v: its flipped sign should still apply under the old id (readings %v)", w, got)
	}
}

func TestRenamingAPlugRenamesItsMeterToo(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	m, _ := f.manager()
	if _, err := m.Add(ctx, NewDevice{Kind: "shelly_plug", Host: "192.168.1.50", Name: "Desk"}); err != nil {
		t.Fatal(err)
	}
	name := "Standing desk"
	if _, err := m.Update(ctx, "shelly-192.168.1.50", UpdateDevice{Name: &name}); err != nil {
		t.Fatal(err)
	}
	if mt, err := f.st.Meter(ctx, "shelly-192.168.1.50:plug"); err != nil || mt.Label != name {
		t.Errorf("the plug's meter is %q (%v), want it to follow the device's name", mt.Label, err)
	}
	// A monitor has many meters, each named on its own: renaming the device leaves them alone.
	if _, err := m.Add(ctx, NewDevice{Kind: "refoss", Host: "192.168.68.88", Name: "Garage panel"}); err != nil {
		t.Fatal(err)
	}
	rename := "Garage"
	if _, err := m.Update(ctx, "refoss-192.168.68.88", UpdateDevice{Name: &rename}); err != nil {
		t.Fatal(err)
	}
	if mt, _ := f.st.Meter(ctx, "refoss-192.168.68.88:em2"); mt.Label != "A2" {
		t.Errorf("an input's name is %q, want it left alone", mt.Label)
	}
}
