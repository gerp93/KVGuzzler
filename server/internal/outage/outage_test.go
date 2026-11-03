package outage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/source"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

var day = time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)

func at(h, m int) time.Time { return day.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute) }

func boot(id string, when time.Time, cause source.BootCause) source.Boot {
	return source.Boot{DeviceID: id, At: when, Cause: cause}
}

func TestAPowerOutageEndsWhenThePlugsStartedAgain(t *testing.T) {
	// Today: the server's last note at 13:14, the plugs started again at 14:47, the server was back at 16:03.
	got := Classify(at(13, 14), at(16, 3), []source.Boot{boot("a", at(14, 47), source.BootPower), boot("b", at(14, 47).Add(time.Second), source.BootPower)})
	if len(got) != 2 {
		t.Fatalf("%+v, want the outage and the time after it", got)
	}
	if p := got[0]; p.Kind != store.OutagePower || !p.Start.Equal(at(13, 14)) || !p.End.Equal(at(14, 47)) || p.Source != store.OutageFromPlugs {
		t.Errorf("outage %+v, want power from 13:14 to 14:47 found from the plugs", p)
	}
	if p := got[1]; p.Kind != store.OutageServer || !p.Start.Equal(at(14, 47)) || !p.End.Equal(at(16, 3)) {
		t.Errorf("after %+v, want the server off from 14:47 to 16:03", p)
	}
}

func TestTheServerTakingAFewMinutesToBootIsPartOfTheOutageNotTimeOfItsOwn(t *testing.T) {
	// The server was back 90 seconds after the plugs, and then exactly the leeway after: neither is listed apart.
	for _, lag := range []time.Duration{90 * time.Second, Leeway} {
		got := Classify(at(13, 14), at(14, 47).Add(lag), []source.Boot{boot("a", at(14, 47), source.BootPower)})
		if len(got) != 1 || got[0].Kind != store.OutagePower || !got[0].End.Equal(at(14, 47)) {
			t.Errorf("lag %v: %+v, want one power outage ending when the plugs did", lag, got)
		}
	}
	// A minute past the leeway it is time of its own.
	got := Classify(at(13, 14), at(14, 47).Add(Leeway+time.Minute), []source.Boot{boot("a", at(14, 47), source.BootPower)})
	if len(got) != 2 || got[1].Kind != store.OutageServer {
		t.Errorf("past the leeway: %+v, want the outage and server off", got)
	}
}

func TestOnlyAPlugThatStartedInsideTheGapForAPowerReasonShowsTheOutage(t *testing.T) {
	from, to := at(13, 14), at(16, 3)
	cases := []struct {
		name  string
		boots []source.Boot
		want  string    // kind of the only entry
		end   time.Time // where it ends
	}{
		{"started before the gap", []source.Boot{boot("a", at(9, 0), source.BootPower)}, store.OutageServer, to},
		{"restarted by software", []source.Boot{boot("a", at(14, 47), source.BootOther)}, store.OutageServer, to},
		{"started with no reason given", []source.Boot{boot("a", at(16, 0), source.BootUnknown)}, store.OutagePower, at(16, 0)},
		{"none answered", nil, store.OutageUnknown, to},
	}
	for _, c := range cases {
		got := Classify(from, to, c.boots)
		if len(got) != 1 || got[0].Kind != c.want || !got[0].Start.Equal(from) || !got[0].End.Equal(c.end) {
			t.Errorf("%s: %+v, want one %s entry from the start of the gap to %v", c.name, got, c.want, c.end)
		}
	}
	// The earliest start counts: a plug unplugged for something else later does not move the end.
	got := Classify(from, to, []source.Boot{boot("a", at(15, 30), source.BootPower), boot("b", at(14, 47), source.BootPower)})
	if !got[0].End.Equal(at(14, 47)) {
		t.Errorf("the outage ends %v, want 14:47", got[0].End)
	}
}

func TestAShortTimeAwayWithNoPowerLossIsNotListed(t *testing.T) {
	for _, boots := range [][]source.Boot{nil, {boot("a", at(9, 0), source.BootPower)}} {
		if got := Classify(at(13, 14), at(13, 14).Add(Leeway), boots); got != nil {
			t.Errorf("%+v, want nothing for a restart no longer than the leeway", got)
		}
	}
}

func newWatcher(t *testing.T, now time.Time, boots func(context.Context) []source.Boot) (*Watcher, *store.Store, *time.Time) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "house.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	clock := now
	return &Watcher{Store: st, Boots: boots, Now: func() time.Time { return clock }, Sleep: func(context.Context, time.Duration) bool { return true }}, st, &clock
}

func nolog(string, ...any) {}

func TestARestartAfterAnOutageSavesWhatHappenedAndTheNextOneStartsFromNow(t *testing.T) {
	ctx := context.Background()
	w, st, clock := newWatcher(t, at(13, 14), nil)
	// The server runs until 13:14: no gap on its first start.
	if _, ok := w.Begin(ctx, nolog); ok {
		t.Fatal("a first start has nothing before it")
	}
	*clock = at(16, 3)
	w.Boots = func(context.Context) []source.Boot { return []source.Boot{boot("a", at(14, 47), source.BootPower)} }
	pending, ok := w.Begin(ctx, nolog)
	if !ok {
		t.Fatal("2 h 49 min away was not looked into")
	}
	// Until it is worked out the gap is already there, as unknown.
	if list, _ := st.Outages(ctx, day, 10); len(list) != 1 || list[0].Kind != store.OutageUnknown {
		t.Fatalf("while working it out: %+v", list)
	}
	w.Resolve(ctx, pending, nolog)
	list, err := st.Outages(ctx, day, 10)
	if err != nil || len(list) != 2 {
		t.Fatalf("%+v, %v, want the outage and the time after it", list, err)
	}
	if list[1].Kind != store.OutagePower || !list[1].Started.Equal(at(13, 14)) || !list[1].Ended.Equal(at(14, 47)) {
		t.Errorf("the outage is %+v", list[1])
	}
	// The heartbeat moved to now, so another restart a minute later finds nothing.
	*clock = at(16, 4)
	if _, ok := w.Begin(ctx, nolog); ok {
		t.Error("a restart a minute later was looked into")
	}
}

func TestAFastRestartForANewVersionIsNotAnOutage(t *testing.T) {
	ctx := context.Background()
	w, st, clock := newWatcher(t, at(10, 0), nil)
	w.Begin(ctx, nolog)
	*clock = at(10, 0).Add(MinGap - time.Second)
	if _, ok := w.Begin(ctx, nolog); ok {
		t.Error("a restart shorter than the minimum was looked into")
	}
	if list, _ := st.Outages(ctx, day, 10); len(list) != 0 {
		t.Errorf("%+v, want nothing saved", list)
	}
}

func TestPlugsThatAreNotUpYetAreAskedAgainAndGiveUpAfterAWhile(t *testing.T) {
	ctx := context.Background()
	asked := 0
	w, st, clock := newWatcher(t, at(13, 14), func(context.Context) []source.Boot {
		asked++
		if asked < 4 { // the network is not up for the first three looks
			return nil
		}
		return []source.Boot{boot("a", at(14, 47), source.BootPower)}
	})
	w.Begin(ctx, nolog)
	*clock = at(16, 3)
	pending, _ := w.Begin(ctx, nolog)
	w.Resolve(ctx, pending, nolog)
	if asked != 4 {
		t.Errorf("asked %d times, want 4", asked)
	}
	if list, _ := st.Outages(ctx, day, 10); len(list) != 2 {
		t.Errorf("%+v, want the outage and the time after it", list)
	}

	// Plugs that never answer leave the gap as unknown.
	w2, st2, clock2 := newWatcher(t, at(13, 14), func(context.Context) []source.Boot { return nil })
	w2.Begin(ctx, nolog)
	*clock2 = at(16, 3)
	pending, _ = w2.Begin(ctx, nolog)
	w2.Resolve(ctx, pending, nolog)
	if list, _ := st2.Outages(ctx, day, 10); len(list) != 1 || list[0].Kind != store.OutageUnknown || list[0].Source != store.OutageFromGap {
		t.Errorf("%+v, want the gap kept as unknown", list)
	}
}
