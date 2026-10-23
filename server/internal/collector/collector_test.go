package collector

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

type fake struct {
	readings []model.Reading
	err      error
}

func (f *fake) Device() model.Device { return model.Device{ID: "dev", Kind: "refoss", Name: "Fake"} }
func (f *fake) Channels() []model.Channel {
	cap := 5000.0
	return []model.Channel{{ID: "dev/ch-01", DeviceID: "dev", Scope: model.ScopeCircuit, Label: "Dryer", Enabled: true, MaxWatts: &cap}}
}
func (f *fake) Poll(context.Context) ([]model.Reading, error) { return f.readings, f.err }

func setup(t *testing.T) (*Collector, *store.Store, *fake) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	c := New(s, time.Second)
	f := &fake{}
	if err := c.Add(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	return c, s, f
}

var t0 = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

func TestEnergyIsWattsTimesCoveredSeconds(t *testing.T) {
	ctx := context.Background()
	c, s, f := setup(t)
	f.readings = []model.Reading{{ChannelID: "dev/ch-01", Watts: 3600}}
	// Polls at 12:00:00 .. 12:01:00. The first only starts the clock, so 60 s are credited.
	for i := 0; i <= 60; i++ {
		c.PollOnce(ctx, t0.Add(time.Duration(i)*time.Second))
	}
	c.FlushAll(ctx)
	tot, err := s.Totals(ctx, "dev/ch-01", t0, t0.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(tot.KWh-3600.0*60/3.6e6) > 1e-9 {
		t.Errorf("kWh = %v, want %v", tot.KWh, 3600.0*60/3.6e6)
	}
}

func TestImpossibleReadingIsDroppedAndNoted(t *testing.T) {
	ctx := context.Background()
	c, s, f := setup(t)
	c.SetClock(func() time.Time { return t0.Add(10 * time.Second) })

	f.readings = []model.Reading{{ChannelID: "dev/ch-01", Watts: 100}}
	c.PollOnce(ctx, t0)
	c.PollOnce(ctx, t0.Add(time.Second))
	f.readings = []model.Reading{{ChannelID: "dev/ch-01", Watts: 250000}} // NVML-style nonsense
	c.PollOnce(ctx, t0.Add(2*time.Second))
	c.FlushAll(ctx)

	tot, _ := s.Totals(ctx, "dev/ch-01", t0, t0.Add(time.Minute))
	if tot.PeakWatts != 100 {
		t.Errorf("peak = %v, want 100: the impossible reading leaked into the history", tot.PeakWatts)
	}
	notes := c.Notes()
	if len(notes) != 1 || !strings.Contains(notes[0], "Dropped an impossible reading of 250000 W on Dryer") {
		t.Errorf("notes = %v", notes)
	}
	for _, r := range c.LiveReadings() {
		if r.Watts > 5000 {
			t.Errorf("live shows %v W", r.Watts)
		}
	}
}

func TestSilenceIsNotCreditedAndIsReported(t *testing.T) {
	ctx := context.Background()
	c, s, f := setup(t)
	c.SetClock(func() time.Time { return t0.Add(20 * time.Second) })

	f.readings = []model.Reading{{ChannelID: "dev/ch-01", Watts: 1000}}
	c.PollOnce(ctx, t0)
	c.PollOnce(ctx, t0.Add(time.Second)) // 1 s credited
	f.err = errors.New("connection refused")
	c.PollOnce(ctx, t0.Add(2*time.Second))
	if n := c.Notes(); len(n) != 1 || !strings.Contains(n[0], "Fake did not answer") {
		t.Fatalf("notes = %v", n)
	}
	if d := c.Devices(); len(d) != 1 || d[0].Online {
		t.Fatalf("devices = %+v, want offline", d)
	}

	f.err = nil
	c.PollOnce(ctx, t0.Add(17*time.Second)) // back after 15 s of silence: restarts the clock, credits nothing
	c.PollOnce(ctx, t0.Add(18*time.Second)) // 1 s credited
	c.FlushAll(ctx)

	tot, _ := s.Totals(ctx, "dev/ch-01", t0, t0.Add(time.Minute))
	if math.Abs(tot.MonitoredHours*3600-2) > 1e-9 {
		t.Errorf("covered %v s, want 2 s", tot.MonitoredHours*3600)
	}
	if n := c.Notes(); len(n) != 0 {
		t.Errorf("notes after recovery = %v", n)
	}
}

type counting struct {
	fake
	polls atomic.Int32
}

func (c *counting) Poll(ctx context.Context) ([]model.Reading, error) {
	c.polls.Add(1)
	return c.fake.Poll(ctx)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for i := 0; i < 400; i++ {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestSourcesCanBeAddedAndRemovedWhileRunning(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s, err := store.Open(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	c := New(s, 10*time.Millisecond)

	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	src := &counting{fake: fake{readings: []model.Reading{{ChannelID: "dev/ch-01", Watts: 100}}}}
	if err := c.Add(context.Background(), src); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the added source to be polled", func() bool { return src.polls.Load() >= 3 })
	waitFor(t, "a live reading", func() bool { return len(c.LiveReadings()) == 1 })

	if err := c.Add(context.Background(), &counting{}); err == nil {
		t.Error("adding the same device twice should fail")
	}

	if !c.Remove("dev") {
		t.Fatal("Remove said the device was not being polled")
	}
	if c.Remove("dev") {
		t.Error("removing twice should report false")
	}
	if len(c.LiveReadings()) != 0 || len(c.Devices()) != 0 {
		t.Errorf("after removal: live %v, devices %v", c.LiveReadings(), c.Devices())
	}
	time.Sleep(60 * time.Millisecond) // let a loop that was mid-poll finish
	n := src.polls.Load()
	time.Sleep(80 * time.Millisecond)
	if after := src.polls.Load(); after != n {
		t.Errorf("polled %d more times after removal", after-n)
	}
}
