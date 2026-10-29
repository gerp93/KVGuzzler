// Package evcharge finds the charges of an electric vehicle: it watches the meter that charges it, opens a charge when the meter goes from resting to
// drawing power and closes it when the meter has rested a while, and asks the car for its battery's charge at each end. The meter says how much energy
// went in; only the car can say how much the battery gained, and the two together give the energy it takes per percent.
package evcharge

import (
	"context"
	"errors"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

const (
	// StartWatts is the power at which a resting meter counts as charging, seen on StartSamples looks in a row so one spike is not a charge. A charger
	// that is plugged in but not charging draws a few watts, well under it.
	StartWatts   = 200.0
	StartSamples = 2
	// RestWatts is the power under which a charging meter is resting. It is lower than StartWatts so a charge tapering off at the top does not open and
	// close again.
	RestWatts = 100.0
	// RestFor is how long the meter must rest before the charge is over: a pause in the middle (the car re-checking, a schedule) does not split it.
	RestFor = 2 * time.Minute
	// NoAnswerFor is how long a meter that stopped answering keeps a charge open. Time it did not answer has no energy in it either way.
	NoAnswerFor = 10 * time.Minute
	// TickEvery is how often the meter is looked at.
	TickEvery = 10 * time.Second

	// StartReadFor is how long after a charge began the car is still asked for the battery's charge at its start (the car may need to wake, and Ford
	// answers one look every 30 seconds), StartRetry how often.
	StartReadFor = 15 * time.Minute
	StartRetry   = 45 * time.Second
	// EndFirstAfter is how long after a charge ended the car is first asked: the car reports a little after it stops. It is then asked every EndRetry for
	// EndFastFor, then every EndSlowRetry, until EndGiveUp.
	EndFirstAfter = time.Minute
	EndRetry      = 3 * time.Minute
	EndFastFor    = 30 * time.Minute
	EndSlowRetry  = 30 * time.Minute
	EndGiveUp     = 6 * time.Hour
	// SettledSlack is how much earlier than the end of the charge the car's report may be and still count as the final one: a report from before the
	// charge ended misses the last of it, which would make the energy per percent look worse than it is.
	SettledSlack = 2 * time.Minute
)

// Reading is the battery's charge as the car reported it.
type Reading struct {
	Percent float64
	// At is when the car reported it; zero when it did not say.
	At time.Time
}

// ErrNoCar says there is nothing to ask for this meter (no vehicle is linked to it).
var ErrNoCar = errors.New("no vehicle to ask")

// Car is where the battery's charge comes from. It may refuse a look (the vehicle service allows one every 30 seconds); the tracker asks again later.
// An error that has a RetryAfter method asks for a longer wait than the tracker's own (a service that is failing is not asked again quickly).
type Car interface {
	Battery(ctx context.Context, meterID string) (Reading, error)
}

// retryIn is how long to wait after the car refused a look: def, or longer when the error asks for it.
func retryIn(err error, def time.Duration) time.Duration {
	var r interface{ RetryAfter() time.Duration }
	if errors.As(err, &r) && r.RetryAfter() > def {
		return r.RetryAfter()
	}
	return def
}

// Tracker follows every meter marked as charging a vehicle.
type Tracker struct {
	Store *store.Store
	// Live is the meter's latest reading in watts; false when it has not answered lately.
	Live func(meterID string) (float64, bool)
	// Car may be nil: charges are then recorded with their energy and cost but no battery figures.
	Car Car
	Now func() time.Time

	meters map[string]*meterState
}

type meterState struct {
	// above counts the looks in a row at or above StartWatts while no charge is open.
	above int
	// nextCar is the earliest time the car is asked again.
	nextCar time.Time
}

func (t *Tracker) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

// Settled says whether a closed charge has the car's final report: one made after the charge ended (give or take SettledSlack).
func Settled(c model.EvCharge) bool {
	return c.EndedAt != nil && c.EndPct != nil && c.EndPctAt != nil && !c.EndPctAt.Before(c.EndedAt.Add(-SettledSlack))
}

// Step looks at every charging meter once.
func (t *Tracker) Step(ctx context.Context) error {
	ids, err := t.Store.EVMeterIDs(ctx)
	if err != nil {
		return err
	}
	if t.meters == nil {
		t.meters = map[string]*meterState{}
	}
	var first error
	for _, id := range ids {
		st := t.meters[id]
		if st == nil {
			st = &meterState{}
			t.meters[id] = st
		}
		if err := t.stepMeter(ctx, id, st); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (t *Tracker) stepMeter(ctx context.Context, id string, st *meterState) error {
	now := t.now()
	watts, answered := t.Live(id)
	open, err := t.Store.OpenEvCharge(ctx, id)
	if err != nil {
		return err
	}

	if open == nil {
		switch {
		case answered && watts >= StartWatts:
			st.above++
		case answered:
			st.above = 0
		}
		if st.above >= StartSamples {
			c, err := t.Store.StartEvCharge(ctx, id, now)
			if err != nil {
				return err
			}
			open, st.above, st.nextCar = &c, 0, now
		}
	} else {
		switch {
		case answered && watts >= RestWatts:
			if err := t.Store.TouchEvCharge(ctx, open.ID, now); err != nil {
				return err
			}
			open.LastAbove = now
		case (answered && now.Sub(open.LastAbove) >= RestFor) || (!answered && now.Sub(open.LastAbove) >= NoAnswerFor):
			if err := t.Store.EndEvCharge(ctx, open.ID, open.LastAbove); err != nil {
				return err
			}
			st.nextCar = now.Add(EndFirstAfter)
			open = nil
		}
	}
	if t.Car == nil || now.Before(st.nextCar) {
		return nil
	}
	if open != nil {
		return t.readStart(ctx, id, st, *open, now)
	}
	return t.readEnd(ctx, id, st, now)
}

// readStart asks the car for the battery's charge of a charge that has just begun, until it answers or StartReadFor has passed.
func (t *Tracker) readStart(ctx context.Context, id string, st *meterState, c model.EvCharge, now time.Time) error {
	if c.StartPct != nil || now.Sub(c.StartedAt) >= StartReadFor {
		return nil
	}
	r, err := t.Car.Battery(ctx, id)
	if err != nil {
		st.nextCar = now.Add(retryIn(err, StartRetry))
		return nil
	}
	if r.At.IsZero() {
		r.At = now
	}
	return t.Store.SetEvChargeStart(ctx, c.ID, r.Percent, r.At)
}

// readEnd asks the car for the battery's charge after the latest charge ended, until the car has reported one made after the charge was over or
// EndGiveUp has passed. A charge whose start was never read has no use for an end, so the car is not asked.
func (t *Tracker) readEnd(ctx context.Context, id string, st *meterState, now time.Time) error {
	c, err := t.Store.LatestClosedEvCharge(ctx, id)
	if err != nil || c == nil || c.StartPct == nil || Settled(*c) {
		return err
	}
	since := now.Sub(*c.EndedAt)
	if since >= EndGiveUp {
		return nil
	}
	r, err := t.Car.Battery(ctx, id)
	if err != nil {
		st.nextCar = now.Add(retryIn(err, retryAfterEnd(since)))
		return nil
	}
	if r.At.IsZero() {
		r.At = now
	}
	// An older report never replaces a newer one already kept.
	if c.EndPctAt == nil || r.At.After(*c.EndPctAt) {
		if err := t.Store.SetEvChargeEnd(ctx, c.ID, r.Percent, r.At); err != nil {
			return err
		}
		c.EndPct, c.EndPctAt = &r.Percent, &r.At
	}
	if !Settled(*c) {
		st.nextCar = now.Add(retryAfterEnd(since))
	}
	return nil
}

func retryAfterEnd(sinceEnd time.Duration) time.Duration {
	if sinceEnd < EndFastFor {
		return EndRetry
	}
	return EndSlowRetry
}

// Run looks at the charging meters until ctx ends. A failure is reported with logf and never stops it: an optional feature must not stop the server.
func (t *Tracker) Run(ctx context.Context, logf func(format string, a ...any)) {
	tick := time.NewTicker(TickEvery)
	defer tick.Stop()
	for {
		if err := t.Step(ctx); err != nil && ctx.Err() == nil {
			logf("ev charges: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
