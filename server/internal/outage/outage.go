// Package outage notes when the house was not being recorded and works out why.
//
// The server writes the time it knew it was running every few seconds (the heartbeat). When it starts and finds the last one a while ago, the time between is
// a gap. The plugs say when they last started (the time they were up for), and a plug that lost power starts again the moment it comes back, so a plug that
// started inside the gap tells when the power returned: the gap up to then is a power outage, and what is left until the server was back is time the power
// was on and nothing was recording. That second part is only called that when it is long enough to be more than the server taking its time to boot after
// the power returned (Leeway); a short one is just the end of the outage.
package outage

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/source"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

const (
	// Beat is how often the server notes that it is running.
	Beat = 15 * time.Second
	// MinGap is the shortest time away that is looked into at all: a restart for a new version takes seconds.
	MinGap = 2 * time.Minute
	// Leeway is how long the server may take to be back after the power returned before the time is called "server off". Plugs start in seconds, the box
	// in a minute or two, and the network a little after that, so anything under this is the outage's own tail and is not listed apart.
	Leeway = 10 * time.Minute
	// AskFor and AskEvery are how long, and how often, the plugs are asked when they have not answered yet (the network may not be up when the server is).
	AskFor   = 5 * time.Minute
	AskEvery = 10 * time.Second
	// bootSlack is how far after the end of the gap a plug's start may fall and still be taken as the same event (the plugs' clocks are worked out from this
	// server's, which is only as exact as the time the question took).
	bootSlack = time.Minute
)

// Entry is an outage found by Classify.
type Entry struct {
	Kind       string
	Start, End time.Time
	Source     string
	Note       string
}

// Classify says what the gap [from, to) was, given the starts the plugs reported afterwards (none when none answered).
//
// A plug that started inside the gap for a power reason (or without saying why) shows the power went out and came back when it started; the earliest such
// start is used, since a later one can be a plug that was unplugged for something else. What is left of the gap after it is "server off" only when it is
// longer than Leeway. With no such plug the gap is the server (or the network) being away when the plugs answered and stayed up, and unknown when they did not
// answer at all; both are only listed when longer than Leeway.
func Classify(from, to time.Time, boots []source.Boot) []Entry {
	var evidence []source.Boot
	for _, b := range boots {
		if b.Cause != source.BootOther && b.At.After(from) && !b.At.After(to.Add(bootSlack)) {
			evidence = append(evidence, b)
		}
	}
	if len(evidence) > 0 {
		sort.Slice(evidence, func(i, j int) bool { return evidence[i].At.Before(evidence[j].At) })
		back := evidence[0].At.Truncate(time.Second)
		if back.After(to) {
			back = to
		}
		out := []Entry{{Kind: store.OutagePower, Start: from, End: back, Source: store.OutageFromPlugs, Note: powerNote(len(evidence))}}
		if to.Sub(back) > Leeway {
			out = append(out, Entry{Kind: store.OutageServer, Start: back, End: to, Source: store.OutageFromPlugs, Note: "The power was back; nothing was recording."})
		}
		return out
	}
	if to.Sub(from) <= Leeway {
		return nil
	}
	if len(boots) > 0 {
		return []Entry{{Kind: store.OutageServer, Start: from, End: to, Source: store.OutageFromPlugs, Note: "The plugs stayed on, so the power was not out; nothing was recording."}}
	}
	return []Entry{{Kind: store.OutageUnknown, Start: from, End: to, Source: store.OutageFromGap, Note: "Nothing was recorded; why could not be told."}}
}

func powerNote(n int) string {
	if n == 1 {
		return "1 plug started again when the power came back."
	}
	return fmt.Sprintf("%d plugs started again when the power came back.", n)
}

// Watcher keeps the heartbeat and, after a restart, works out what the time away was.
type Watcher struct {
	Store *store.Store
	// Boots asks the plugs when they last started; it returns those that answered.
	Boots func(ctx context.Context) []source.Boot
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Sleep waits for d or until ctx ends (it reports false then); nil means a real wait. Tests pass one that does not wait.
	Sleep func(ctx context.Context, d time.Duration) bool
}

func (w *Watcher) now() time.Time {
	if w.Now != nil {
		return w.Now().UTC()
	}
	return time.Now().UTC()
}

func (w *Watcher) sleep(ctx context.Context, d time.Duration) bool {
	if w.Sleep != nil {
		return w.Sleep(ctx, d)
	}
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// Run looks into the time the server was away (if it was), then notes that it is running every Beat until ctx ends. A failure is reported with logf and
// never stops it.
func (w *Watcher) Run(ctx context.Context, logf func(format string, a ...any)) {
	if pending, ok := w.Begin(ctx, logf); ok {
		go w.Resolve(ctx, pending, logf)
	}
	for w.sleep(ctx, Beat) {
		if err := w.Store.SetAlive(ctx, w.now()); err != nil && ctx.Err() == nil {
			logf("outages: heartbeat: %v", err)
		}
	}
}

// Begin compares now with the last heartbeat. When the server was away long enough to matter it saves the gap as an outage of unknown cause and returns it,
// to be worked out by Resolve; either way it then notes that the server is running.
func (w *Watcher) Begin(ctx context.Context, logf func(format string, a ...any)) (store.Outage, bool) {
	now := w.now()
	defer func() {
		if err := w.Store.SetAlive(ctx, now); err != nil && ctx.Err() == nil {
			logf("outages: heartbeat: %v", err)
		}
	}()
	last, had, err := w.Store.Alive(ctx)
	if err != nil {
		logf("outages: %v", err)
		return store.Outage{}, false
	}
	if !had || now.Sub(last) < MinGap {
		return store.Outage{}, false
	}
	o, err := w.Store.AddOutage(ctx, store.Outage{Kind: store.OutageUnknown, Started: last, Ended: now, Source: store.OutageFromGap, Note: "Working out why."})
	if err != nil {
		logf("outages: the time away (%s to %s) could not be saved: %v", last.Format(time.RFC3339), now.Format(time.RFC3339), err)
		return store.Outage{}, false
	}
	return o, true
}

// Resolve asks the plugs when they last started, until one answers or AskFor has passed, and replaces the provisional outage with what Classify makes of the gap.
func (w *Watcher) Resolve(ctx context.Context, pending store.Outage, logf func(format string, a ...any)) {
	var boots []source.Boot
	for waited := time.Duration(0); ; waited += AskEvery {
		if boots = w.Boots(ctx); len(boots) > 0 || waited >= AskFor {
			break
		}
		if !w.sleep(ctx, AskEvery) {
			return
		}
	}
	entries := Classify(pending.Started, pending.Ended, boots)
	with := make([]store.Outage, len(entries))
	for i, e := range entries {
		with[i] = store.Outage{Kind: e.Kind, Started: e.Start, Ended: e.End, Source: e.Source, Note: e.Note}
	}
	if _, err := w.Store.ReplaceOutage(ctx, pending.ID, with); err != nil && ctx.Err() == nil {
		logf("outages: could not save what the time away was: %v", err)
	}
}
