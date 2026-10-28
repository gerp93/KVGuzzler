package ford

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Status is what the last look said: an odometer, or why not.
type Status struct {
	OK        bool      `json:"ok"`
	CheckedAt time.Time `json:"checked_at"`
	// Raw is the figure as Ford gave it (in the unit chosen in the setup), Found where in the answer it was, Miles what it came to and Day the local
	// day it was saved for. Saved is false when the odometer had not moved since the last reading, so nothing new was written.
	Raw   *float64 `json:"raw,omitempty"`
	Found string   `json:"found,omitempty"`
	Miles *float64 `json:"miles,omitempty"`
	Day   string   `json:"day,omitempty"`
	Saved bool     `json:"saved,omitempty"`
	// NeedsLink says the saved permission is gone and the owner has to link the vehicle again.
	NeedsLink bool   `json:"needs_link,omitempty"`
	Error     string `json:"error,omitempty"`
}

// ErrNotSetUp is what a read or a link says when no setup is saved.
var ErrNotSetUp = errors.New("not set up")

// Sink is where the feed gets its setup and puts what it reads.
type Sink interface {
	// Config is the saved setup, or nil when none is set up.
	Config(ctx context.Context) (*Config, error)
	// Refresh is the saved refresh token; false when the vehicle is not linked.
	Refresh(ctx context.Context) (string, bool, error)
	SaveRefresh(ctx context.Context, token string) error
	ClearRefresh(ctx context.Context) error
	// SaveOdometer keeps the reading of a local day for the meter and says whether it wrote one (false when the odometer had not moved).
	SaveOdometer(ctx context.Context, meterID, day string, miles float64) (bool, error)
	SaveStatus(ctx context.Context, s Status) error
}

const (
	// Interval is how often the odometer is read after a good read: four times a day, so one failed look never skips a day, and a refresh token
	// (30 days) is always renewed long before it lapses.
	Interval = 6 * time.Hour
	// RetryAfterError is how long to wait after something went wrong. Ford may disable a client that retries hard, so it is slow.
	RetryAfterError = time.Hour
	// CheckEvery is how often the setup and the clock are looked at.
	CheckEvery = 30 * time.Second
	// MinGap is the shortest time between two looks the feed starts by itself: Ford answers one request per 30 seconds per endpoint, so a look
	// started right after another (linking, then the feed noticing the link) would only be told to slow down.
	MinGap = time.Minute
)

// Feed keeps the odometer of an electric vehicle's charging meter up to date from Ford. It follows the saved setup, so linking, changing or
// removing it takes effect within half a minute. One lock covers every call: Ford replaces the refresh token at each refresh, so two refreshes at
// once would lose one of them.
type Feed struct {
	Client *Client
	Sink   Sink
	Loc    *time.Location
	Now    func() time.Time

	mu      sync.Mutex
	last    *Config
	nextAt  time.Time
	hadLink bool
	readAt  time.Time
}

func (f *Feed) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

func (f *Feed) loc() *time.Location {
	if f.Loc != nil {
		return f.Loc
	}
	return time.Local
}

// Step is one look: it reads the odometer when one is due (or the setup changed) and returns whether it did.
func (f *Feed) Step(ctx context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cfg, err := f.Sink.Config(ctx)
	if err != nil || cfg == nil {
		f.last = nil
		return false, err
	}
	_, linked, err := f.Sink.Refresh(ctx)
	if err != nil {
		return false, err
	}
	changed := f.last == nil || *f.last != *cfg || linked != f.hadLink
	// Too soon after the last look: nothing is noted as seen, so a changed setup is still looked at once the gap has passed.
	if linked && f.now().Sub(f.readAt) < MinGap {
		return false, nil
	}
	c := *cfg
	f.last, f.hadLink = &c, linked
	if !linked || (!changed && f.now().Before(f.nextAt)) {
		return false, nil
	}
	st, err := f.read(ctx, *cfg)
	if err != nil {
		return true, err
	}
	if st.OK {
		f.nextAt = f.now().Add(Interval)
	} else {
		f.nextAt = f.now().Add(RetryAfterError)
	}
	return true, nil
}

// Read looks now, whenever it is called, and says what happened.
func (f *Feed) Read(ctx context.Context) (Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cfg, err := f.Sink.Config(ctx)
	if err != nil {
		return Status{}, err
	}
	if cfg == nil {
		return Status{}, ErrNotSetUp
	}
	st, err := f.read(ctx, *cfg)
	if err == nil {
		if st.OK {
			f.nextAt = f.now().Add(Interval)
		} else {
			f.nextAt = f.now().Add(RetryAfterError)
		}
	}
	return st, err
}

// Complete finishes linking: it trades the one-time code for tokens, keeps the refresh token and reads the odometer once.
func (f *Feed) Complete(ctx context.Context, code string) (Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cfg, err := f.Sink.Config(ctx)
	if err != nil {
		return Status{}, err
	}
	if cfg == nil {
		return Status{}, ErrNotSetUp
	}
	tok, err := f.Client.Exchange(ctx, *cfg, code)
	if err != nil {
		return Status{}, err
	}
	if tok.Refresh == "" {
		return Status{}, &Error{Message: "Ford gave no refresh token: the application needs the offline_access scope."}
	}
	if err := f.Sink.SaveRefresh(ctx, tok.Refresh); err != nil {
		return Status{}, err
	}
	// The link was just made and is read below: the feed must not take it for news and read again at once.
	c := *cfg
	f.last, f.hadLink = &c, true
	st, err := f.readWith(ctx, *cfg, tok.Access)
	if err == nil {
		f.nextAt = f.now().Add(Interval)
		if !st.OK {
			f.nextAt = f.now().Add(RetryAfterError)
		}
	}
	return st, err
}

// read refreshes the tokens, reads the odometer and saves it, and saves and returns what happened. An error is only returned for something that is
// not Ford's doing (the database, a cancelled request); Ford's complaints are in the Status.
func (f *Feed) read(ctx context.Context, cfg Config) (Status, error) {
	refresh, ok, err := f.Sink.Refresh(ctx)
	if err != nil {
		return Status{}, err
	}
	if !ok {
		return f.fail(ctx, &Error{Message: "The vehicle is not linked yet.", Relink: true})
	}
	f.readAt = f.now()
	tok, err := f.Client.Refresh(ctx, cfg, refresh)
	if err != nil {
		return f.fail(ctx, err)
	}
	// The new refresh token is saved before anything else is asked of Ford: the old one no longer works.
	if tok.Refresh != "" && tok.Refresh != refresh {
		if err := f.Sink.SaveRefresh(ctx, tok.Refresh); err != nil {
			return Status{}, err
		}
	}
	return f.readWith(ctx, cfg, tok.Access)
}

func (f *Feed) readWith(ctx context.Context, cfg Config, access string) (Status, error) {
	f.readAt = f.now()
	body, err := f.Client.Telemetry(ctx, access)
	if err != nil {
		return f.fail(ctx, err)
	}
	odo, err := ParseOdometer(body)
	if err != nil {
		return f.fail(ctx, &Error{Message: err.Error()})
	}
	miles, ok := odo.Miles(cfg.OdometerUnit)
	if !ok {
		return f.fail(ctx, &Error{Message: "Ford's odometer is not a believable figure in the unit chosen: check the unit in the setup."})
	}
	now := f.now()
	day := now.In(f.loc()).Format("2006-01-02")
	saved, err := f.Sink.SaveOdometer(ctx, cfg.MeterID, day, miles)
	if err != nil {
		// A sink that refuses the reading with an Error (the odometer would go down, the meter is no longer marked as charging a vehicle) has it said,
		// not retried hard; anything else is the server's own trouble.
		return f.fail(ctx, err)
	}
	raw := odo.Value
	st := Status{OK: true, CheckedAt: now.UTC(), Raw: &raw, Found: odo.Path, Miles: &miles, Day: day, Saved: saved}
	_ = f.Sink.SaveStatus(ctx, st)
	return st, nil
}

// fail saves and returns what went wrong. A permission Ford no longer accepts also removes the refresh token, so nothing is tried again until the
// owner links the vehicle again.
func (f *Feed) fail(ctx context.Context, err error) (Status, error) {
	var fe *Error
	if !errors.As(err, &fe) {
		return Status{}, err // not Ford's complaint (the request was cancelled): nothing to report
	}
	st := Status{CheckedAt: f.now().UTC(), Error: fe.Message, NeedsLink: fe.Relink}
	if fe.Relink {
		_ = f.Sink.ClearRefresh(ctx)
	}
	_ = f.Sink.SaveStatus(ctx, st)
	return st, nil
}

// Run looks at the setup now and then until ctx ends. A failure is reported with logf and never stops it: an optional feature must not stop the
// server.
func (f *Feed) Run(ctx context.Context, logf func(format string, a ...any)) {
	tick := time.NewTicker(CheckEvery)
	defer tick.Stop()
	for {
		if _, err := f.Step(ctx); err != nil && ctx.Err() == nil {
			logf("ford feed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
