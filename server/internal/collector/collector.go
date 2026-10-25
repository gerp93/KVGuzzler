// Package collector polls every source on a timer, keeps the latest reading of each meter for the live view,
// and folds the readings into per-minute rows in the store.
//
// A reading above its meter's sanity cap is dropped, like a missing one, and said so in the notes: it is never
// averaged in. Time a source was not answering is not credited: a gap in the history is a gap.
package collector

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/source"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

const (
	liveFreshness = 15 * time.Second
	noteLifetime  = time.Hour
)

type acc struct {
	ws, max, secs float64
	// maxA is the highest current seen in the minute; haveA says whether the meter reported one at all.
	maxA  float64
	haveA bool
}

type state struct {
	src      source.Source
	caps     map[string]float64
	labels   map[string]string
	cancel   context.CancelFunc // stops this source's loop; nil until the collector is running
	online   bool
	lastSeen time.Time
	lastPoll time.Time
	minute   int64
	acc      map[string]*acc
}

type liveReading struct {
	model.Reading
	at time.Time
}

type note struct {
	text  string
	until time.Time
}

type Collector struct {
	store    *store.Store
	interval time.Duration
	now      func() time.Time

	mu      sync.Mutex
	sources []*state
	live    map[string]liveReading
	notes   map[string]note

	runCtx  context.Context // set while Run is running, so sources added later start at once
	running sync.WaitGroup
}

func New(s *store.Store, interval time.Duration) *Collector {
	return &Collector{store: s, interval: interval, now: time.Now, live: map[string]liveReading{}, notes: map[string]note{}}
}

// Add registers a source: its device and meters are written to the store (keeping any label the user gave them).
func (c *Collector) Add(ctx context.Context, src source.Source) error {
	d := src.Device()
	if err := c.store.UpsertDevice(ctx, d); err != nil {
		return err
	}
	st := &state{src: src, caps: map[string]float64{}, labels: map[string]string{}, acc: map[string]*acc{}}
	for _, ch := range src.Meters() {
		if err := c.store.UpsertMeter(ctx, ch); err != nil {
			return err
		}
		st.labels[ch.ID] = ch.Label
		if ch.MaxWatts != nil {
			st.caps[ch.ID] = *ch.MaxWatts
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, other := range c.sources {
		if other.src.Device().ID == d.ID {
			return fmt.Errorf("device %s is already being polled", d.ID)
		}
	}
	c.sources = append(c.sources, st)
	if c.runCtx != nil {
		c.startLocked(st)
	}
	return nil
}

// Remove stops polling a device and drops its live readings. What it recorded stays in the history. It reports whether
// the device was being polled.
func (c *Collector) Remove(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, st := range c.sources {
		if st.src.Device().ID != id {
			continue
		}
		c.sources = append(c.sources[:i], c.sources[i+1:]...)
		if st.cancel != nil {
			st.cancel() // the loop writes the partial minute it holds, then ends
		}
		for chID := range st.labels {
			delete(c.live, chID)
		}
		delete(c.notes, "device:"+id)
		for k := range c.notes {
			if strings.HasPrefix(k, "src:"+id+":") {
				delete(c.notes, k)
			}
		}
		return true
	}
	return false
}

// startLocked runs a source's polling loop until its context ends. The caller holds c.mu and the collector is running.
func (c *Collector) startLocked(st *state) {
	ctx, cancel := context.WithCancel(c.runCtx)
	st.cancel = cancel
	c.running.Add(1)
	go func() {
		defer c.running.Done()
		tick := time.NewTicker(c.interval)
		defer tick.Stop()
		for {
			c.step(ctx, st, c.now())
			select {
			case <-ctx.Done():
				c.flush(context.Background(), st)
				return
			case <-tick.C:
			}
		}
	}()
}

// Run polls every source until ctx ends, then writes the partial minute each one still holds. Sources added while it
// runs start polling at once.
func (c *Collector) Run(ctx context.Context) {
	c.mu.Lock()
	c.runCtx = ctx
	for _, st := range c.sources {
		c.startLocked(st)
	}
	c.mu.Unlock()

	<-ctx.Done()
	c.running.Wait()
}

// step polls one source at time t and processes the answer.
func (c *Collector) step(ctx context.Context, st *state, t time.Time) {
	readings, err := st.src.Poll(ctx)
	if ctx.Err() != nil {
		return
	}
	dev := st.src.Device()

	c.mu.Lock()
	defer c.mu.Unlock()
	key := "device:" + dev.ID
	if err != nil {
		st.online = false
		st.lastPoll = time.Time{} // time without an answer is not credited
		c.notes[key] = note{text: fmt.Sprintf("%s did not answer: %v", deviceName(dev), err), until: t.Add(noteLifetime)}
		return
	}
	st.online, st.lastSeen = true, t
	delete(c.notes, key)
	if n, ok := st.src.(source.Noter); ok {
		// What the source says now replaces what it said before, so a fixed problem stops being reported at once.
		for k := range c.notes {
			if strings.HasPrefix(k, "src:"+dev.ID+":") {
				delete(c.notes, k)
			}
		}
		for i, text := range n.Notes() {
			c.notes[fmt.Sprintf("src:%s:%d", dev.ID, i)] = note{text: text, until: t.Add(noteLifetime)}
		}
	}

	// Credit the time since the previous answer, but never more than two polling intervals of it.
	var dt float64
	if !st.lastPoll.IsZero() {
		d := t.Sub(st.lastPoll)
		if d > 2*c.interval {
			d = 2 * c.interval
		}
		dt = d.Seconds()
	}
	st.lastPoll = t

	minute := t.Unix() / 60 * 60
	if st.minute != 0 && minute != st.minute {
		c.flushLocked(ctx, st)
	}
	st.minute = minute

	for _, r := range readings {
		if cap, ok := st.caps[r.MeterID]; ok && r.Watts > cap {
			c.notes["cap:"+r.MeterID] = note{
				text:  fmt.Sprintf("Dropped an impossible reading of %.0f W on %s (cap %.0f W).", r.Watts, st.labels[r.MeterID], cap),
				until: t.Add(noteLifetime),
			}
			continue
		}
		if r.Watts < 0 {
			continue
		}
		c.live[r.MeterID] = liveReading{Reading: r, at: t}
		a := st.acc[r.MeterID]
		if a == nil {
			a = &acc{}
			st.acc[r.MeterID] = a
		}
		a.ws += r.Watts * dt
		a.secs += dt
		if r.Watts > a.max {
			a.max = r.Watts
		}
		if r.Amps != nil && *r.Amps >= 0 {
			a.maxA, a.haveA = math.Max(a.maxA, *r.Amps), true
		}
	}
}

func (c *Collector) flush(ctx context.Context, st *state) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.flushLocked(ctx, st)
}

func (c *Collector) flushLocked(ctx context.Context, st *state) {
	for id, a := range st.acc {
		if a.secs > 0 {
			// A failed write is not retried: the minute is lost rather than counted twice.
			var amps *float64
			if a.haveA {
				amps = &a.maxA
			}
			_ = c.store.AddMinuteAmps(ctx, id, time.Unix(st.minute, 0), a.ws/a.secs, a.max, amps, a.secs)
		}
	}
	st.acc = map[string]*acc{}
}

// Devices is every registered device with whether it answered on its last poll.
func (c *Collector) Devices() []model.Device {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]model.Device, 0, len(c.sources))
	for _, st := range c.sources {
		d := st.src.Device()
		d.Online = st.online
		if !st.lastSeen.IsZero() {
			t := st.lastSeen
			d.LastSeen = &t
		}
		out = append(out, d)
	}
	return out
}

// LiveReadings is the latest reading of every meter that answered recently.
func (c *Collector) LiveReadings() []model.Reading {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	out := make([]model.Reading, 0, len(c.live))
	for _, r := range c.live {
		if now.Sub(r.at) <= liveFreshness {
			out = append(out, r.Reading)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MeterID < out[j].MeterID })
	return out
}

// Notes are the plain-language things that need attention, oldest problems first dropped after an hour.
func (c *Collector) Notes() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	out := []string{}
	for k, n := range c.notes {
		if now.After(n.until) {
			delete(c.notes, k)
			continue
		}
		out = append(out, n.text)
	}
	sort.Strings(out)
	return out
}

func deviceName(d model.Device) string {
	if d.Name != "" {
		return d.Name
	}
	return d.ID
}

// SetClock replaces the wall clock. It is for tests.
func (c *Collector) SetClock(now func() time.Time) {
	c.mu.Lock()
	c.now = now
	c.mu.Unlock()
}

// PollOnce polls every source once as if it were time t, without the timers. It is for tests.
func (c *Collector) PollOnce(ctx context.Context, t time.Time) {
	c.mu.Lock()
	sources := append([]*state(nil), c.sources...)
	c.mu.Unlock()
	for _, st := range sources {
		c.step(ctx, st, t)
	}
}

// FlushAll writes the partial minute each source still holds. It is for tests and shutdown.
func (c *Collector) FlushAll(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, st := range c.sources {
		c.flushLocked(ctx, st)
	}
}
