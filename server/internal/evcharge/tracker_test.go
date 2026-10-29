package evcharge

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

const meterID = "d/ev"

type rig struct {
	t     *testing.T
	st    *store.Store
	tr    *Tracker
	now   time.Time
	watts float64
	on    bool // the meter answers
	car   *fakeCar
}

type fakeCar struct {
	asked int
	// next is what the car says; err is what it says instead when set.
	next Reading
	err  error
}

func (c *fakeCar) Battery(_ context.Context, _ string) (Reading, error) {
	c.asked++
	if c.err != nil {
		return Reading{}, c.err
	}
	return c.next, nil
}

func newRig(t *testing.T, withCar bool) *rig {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "house.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	if err := st.UpsertMeter(ctx, model.Meter{ID: meterID, DeviceID: "d", Scope: model.ScopeCircuit, Label: "EV"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetMeterEV(ctx, meterID, true); err != nil {
		t.Fatal(err)
	}
	r := &rig{t: t, st: st, now: time.Date(2026, 10, 29, 20, 0, 0, 0, time.UTC), on: true}
	r.tr = &Tracker{Store: st, Live: func(string) (float64, bool) { return r.watts, r.on }, Now: func() time.Time { return r.now }}
	if withCar {
		r.car = &fakeCar{}
		r.tr.Car = r.car
	}
	return r
}

// look runs one look, then moves the clock on by d.
func (r *rig) look(d time.Duration) {
	r.t.Helper()
	if err := r.tr.Step(context.Background()); err != nil {
		r.t.Fatal(err)
	}
	r.now = r.now.Add(d)
}

// lookFor repeats looks every TickEvery for the given time.
func (r *rig) lookFor(total time.Duration) {
	r.t.Helper()
	for end := r.now.Add(total); r.now.Before(end); {
		r.look(TickEvery)
	}
}

func (r *rig) charges() []model.EvCharge {
	r.t.Helper()
	list, err := r.st.EvCharges(context.Background(), meterID, 50)
	if err != nil {
		r.t.Fatal(err)
	}
	return list
}

func TestOneSpikeIsNotACharge(t *testing.T) {
	r := newRig(t, false)
	r.watts = 3000
	r.look(TickEvery)
	r.watts = 4
	r.lookFor(time.Minute)
	if n := len(r.charges()); n != 0 {
		t.Fatalf("a single look above the start level opened %d charges", n)
	}
}

func TestAChargeOpensAndClosesWhereTheMeterStoppedDrawing(t *testing.T) {
	r := newRig(t, false)
	r.watts = 4
	r.lookFor(time.Minute)
	started := r.now
	r.watts = 7200
	r.lookFor(30 * time.Minute)
	if got := r.charges(); len(got) != 1 || got[0].EndedAt != nil || got[0].StartedAt.Sub(started) > TickEvery {
		t.Fatalf("charges while charging = %+v", got)
	}
	lastDrawing := r.now.Add(-TickEvery) // the look just before the meter stopped
	r.watts = 3
	r.lookFor(RestFor + 30*time.Second)
	got := r.charges()
	if len(got) != 1 || got[0].EndedAt == nil {
		t.Fatalf("charges after resting = %+v", got)
	}
	if !got[0].EndedAt.Equal(lastDrawing) {
		t.Fatalf("ended at %v, want the last look that drew power, %v", got[0].EndedAt, lastDrawing)
	}
}

func TestAPauseInTheMiddleDoesNotSplitACharge(t *testing.T) {
	r := newRig(t, false)
	r.watts = 7200
	r.lookFor(10 * time.Minute)
	r.watts = 0
	r.lookFor(RestFor - TickEvery*2)
	r.watts = 7200
	r.lookFor(10 * time.Minute)
	if got := r.charges(); len(got) != 1 || got[0].EndedAt != nil {
		t.Fatalf("a pause shorter than %v made %+v", RestFor, got)
	}
}

func TestATaperingChargeBetweenTheTwoLevelsStaysOpen(t *testing.T) {
	r := newRig(t, false)
	r.watts = 7200
	r.lookFor(5 * time.Minute)
	r.watts = (StartWatts + RestWatts) / 2 // under the level that opens a charge, over the one that closes it
	r.lookFor(10 * time.Minute)
	if got := r.charges(); len(got) != 1 || got[0].EndedAt != nil {
		t.Fatalf("a charge at %.0f W was closed or split: %+v", r.watts, got)
	}
}

func TestAMeterThatStopsAnsweringClosesTheChargeWhereItWasLastSeen(t *testing.T) {
	r := newRig(t, false)
	r.watts = 7200
	r.lookFor(5 * time.Minute)
	lastSeen := r.now.Add(-TickEvery)
	r.on = false
	r.lookFor(NoAnswerFor - TickEvery)
	if got := r.charges(); got[0].EndedAt != nil {
		t.Fatalf("closed too early: %+v", got)
	}
	r.lookFor(2 * TickEvery)
	got := r.charges()
	if got[0].EndedAt == nil || !got[0].EndedAt.Equal(lastSeen) {
		t.Fatalf("charge = %+v, want it closed at %v", got[0], lastSeen)
	}
}

func TestACarThatIsAskedAtTheStartAndAgainAfterTheEnd(t *testing.T) {
	r := newRig(t, true)
	r.car.err = errors.New("too soon")
	r.watts = 7200
	r.lookFor(2 * TickEvery)
	if got := r.charges(); got[0].StartPct != nil {
		t.Fatalf("a refused look gave a start charge: %+v", got[0])
	}
	// The car answers on a later look; it is asked again after StartRetry.
	r.car.err = nil
	r.car.next = Reading{Percent: 41, At: r.now.Add(-time.Hour)}
	r.lookFor(StartRetry + TickEvery)
	got := r.charges()[0]
	if got.StartPct == nil || *got.StartPct != 41 {
		t.Fatalf("start charge = %v, want 41", got.StartPct)
	}
	asked := r.car.asked
	r.lookFor(10 * time.Minute)
	if r.car.asked != asked {
		t.Fatalf("the car was asked again for the start of a charge that has it (%d -> %d)", asked, r.car.asked)
	}

	// The charge ends. The car first reports from before the end: kept, but not final.
	r.watts = 0
	endOfDrawing := r.now
	r.lookFor(RestFor + EndFirstAfter + 30*time.Second)
	r.car.next = Reading{Percent: 70, At: endOfDrawing.Add(-10 * time.Minute)}
	r.lookFor(EndRetry + TickEvery)
	c := r.charges()[0]
	if c.EndPct == nil || *c.EndPct != 70 || Settled(c) {
		t.Fatalf("early report: %+v settled=%v", c, Settled(c))
	}
	// The next report is from after the end: final, and the car is left alone.
	r.car.next = Reading{Percent: 78, At: r.now}
	r.lookFor(EndRetry + TickEvery)
	c = r.charges()[0]
	if c.EndPct == nil || *c.EndPct != 78 || !Settled(c) {
		t.Fatalf("final report: %+v settled=%v", c, Settled(c))
	}
	asked = r.car.asked
	r.lookFor(time.Hour)
	if r.car.asked != asked {
		t.Fatalf("the car was asked again after the final report (%d -> %d)", asked, r.car.asked)
	}
}

func TestAnOlderReportNeverReplacesANewerOne(t *testing.T) {
	r := newRig(t, true)
	r.car.next = Reading{Percent: 30, At: r.now}
	r.watts = 5000
	r.lookFor(5 * time.Minute)
	r.watts = 0
	r.lookFor(RestFor + EndFirstAfter + 10*time.Second)
	r.car.next = Reading{Percent: 60, At: r.now}
	r.lookFor(EndRetry)
	r.car.next = Reading{Percent: 55, At: r.now.Add(-time.Hour)}
	r.lookFor(EndRetry + TickEvery)
	if c := r.charges()[0]; c.EndPct == nil || *c.EndPct != 60 {
		t.Fatalf("end charge = %v, want the newer 60", c.EndPct)
	}
}

func TestACarThatNeverReportedTheStartIsNotAskedForTheEnd(t *testing.T) {
	r := newRig(t, true)
	r.car.err = ErrNoCar
	r.watts = 5000
	r.lookFor(StartReadFor + time.Minute)
	r.watts = 0
	r.lookFor(RestFor + time.Minute)
	asked := r.car.asked
	r.lookFor(time.Hour)
	if r.car.asked != asked {
		t.Fatalf("asked for the end of a charge with no start (%d -> %d)", asked, r.car.asked)
	}
	if c := r.charges()[0]; c.EndedAt == nil || c.StartPct != nil || c.EndPct != nil {
		t.Fatalf("charge = %+v", c)
	}
}

func TestAFailingCarIsNotAskedAgainQuickly(t *testing.T) {
	r := newRig(t, true)
	r.car.err = slow{}
	r.watts = 5000
	r.lookFor(StartReadFor - time.Minute)
	if r.car.asked != 1 {
		t.Fatalf("a car that asks for an hour's wait was asked %d times in %v", r.car.asked, StartReadFor)
	}
}

type slow struct{}

func (slow) Error() string             { return "failing" }
func (slow) RetryAfter() time.Duration { return time.Hour }

func TestWithoutACarChargesAreStillKept(t *testing.T) {
	r := newRig(t, false)
	r.watts = 5000
	r.lookFor(5 * time.Minute)
	r.watts = 0
	r.lookFor(RestFor + time.Minute)
	if c := r.charges(); len(c) != 1 || c[0].EndedAt == nil || c[0].StartPct != nil {
		t.Fatalf("charges = %+v", c)
	}
}

func TestSettled(t *testing.T) {
	end := time.Date(2026, 10, 29, 22, 0, 0, 0, time.UTC)
	pct := 80.0
	at := func(d time.Duration) *time.Time { t := end.Add(d); return &t }
	cases := []struct {
		name string
		c    model.EvCharge
		want bool
	}{
		{"still going", model.EvCharge{EndPct: &pct, EndPctAt: at(0)}, false},
		{"no report", model.EvCharge{EndedAt: &end}, false},
		{"after the end", model.EvCharge{EndedAt: &end, EndPct: &pct, EndPctAt: at(5 * time.Minute)}, true},
		{"just before the end, within the slack", model.EvCharge{EndedAt: &end, EndPct: &pct, EndPctAt: at(-time.Minute)}, true},
		{"well before the end", model.EvCharge{EndedAt: &end, EndPct: &pct, EndPctAt: at(-10 * time.Minute)}, false},
	}
	for _, c := range cases {
		if got := Settled(c.c); got != c.want {
			t.Errorf("%s: Settled = %v, want %v", c.name, got, c.want)
		}
	}
}
