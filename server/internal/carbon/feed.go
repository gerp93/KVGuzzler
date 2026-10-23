package carbon

import (
	"context"
	"errors"
	"math"
	"time"
)

// HourKWh is the energy used in one hour (the unix second the hour starts).
type HourKWh struct {
	Hour int64
	KWh  float64
}

// Result is an estimate of the carbon the grid emitted to make the electricity used in a period.
type Result struct {
	// Kg is kilograms of CO2 equivalent.
	Kg float64
	// KWh is the energy it was worked out for.
	KWh float64
	// EstimatedKWh is the part of KWh valued at an average because its own hour had no saved intensity (the server was not running then, or the
	// feed was not set up yet).
	EstimatedKWh float64
}

// AverageGramsPerKwh is the energy-weighted average intensity over the period.
func (r Result) AverageGramsPerKwh() float64 {
	if r.KWh > 0 {
		return r.Kg * 1000 / r.KWh
	}
	return 0
}

// EstimatedShare is how much of the energy was valued at an average rather than its own hour's figure (0 to 1).
func (r Result) EstimatedShare() float64 {
	if r.KWh > 0 {
		return math.Min(1, math.Max(0, r.EstimatedKWh/r.KWh))
	}
	return 0
}

// Value turns hourly energy into carbon: each hour's kWh times that hour's grid intensity (known: hour to grams per kWh). Energy in an hour with
// no intensity of its own (the free key gives no history, so anything from before the feed was set up or while the server was off) is valued at
// the average of the hours that do have one, or at everAverage (the average of every hour ever saved) when none in the period does, so a
// period never shows zero just for missing data, and the result says how much was valued that way.
func Value(known map[int64]float64, everAverage float64, hours []HourKWh) Result {
	periodAverage := everAverage
	if len(known) > 0 {
		var sum float64
		for _, g := range known {
			sum += g
		}
		periodAverage = sum / float64(len(known))
	}
	var r Result
	for _, h := range hours {
		grams, own := known[h.Hour]
		if !own {
			grams = periodAverage
			r.EstimatedKWh += h.KWh
		}
		r.Kg += h.KWh * grams / 1000
		r.KWh += h.KWh
	}
	return r
}

// Status is what the last fetch said: a reading, or why not.
type Status struct {
	OK          bool       `json:"ok"`
	CheckedAt   time.Time  `json:"checked_at"`
	GramsPerKwh *float64   `json:"grams_per_kwh,omitempty"`
	At          *time.Time `json:"at,omitempty"`
	Error       string     `json:"error,omitempty"`
}

// Sink is where the feed puts what it reads.
type Sink interface {
	// AddCarbon saves an hour's intensity; a newer figure for the same hour replaces the older.
	AddCarbon(ctx context.Context, at time.Time, gramsPerKwh float64, estimated bool) error
	SaveStatus(ctx context.Context, s Status) error
}

const (
	// Interval is how often a reading is asked for. The figure is hourly, so this catches each hour's update without using much of a free allowance.
	Interval = 30 * time.Minute
	// RetryAfterError is how long to wait before trying again after something went wrong.
	RetryAfterError = 5 * time.Minute
	// RetryAfterLimit is how long to wait when the service said to slow down.
	RetryAfterLimit = time.Hour
	// CheckEvery is how often the config and the clock are looked at.
	CheckEvery = 30 * time.Second
)

// Feed keeps the saved grid carbon intensity up to date: every half hour it asks the chosen source for the recent intensity and saves each hour,
// and it says what happened (a reading, or why not) so the dashboard can show it. It follows the saved configuration, so setting up, changing or
// removing the key takes effect within about half a minute.
//
// Electricity Maps' free key only returns the latest reading, so hours the server was not running for are not filled in afterwards with it; EIA
// returns the last couple of days, so it fills what it can.
type Feed struct {
	Client *Client
	// Load is the saved configuration, or nil when none is set up (or it is not usable).
	Load func(ctx context.Context) (*Config, error)
	Sink Sink
	Now  func() time.Time

	last      *Config
	nextAt    time.Time
	hadConfig bool
}

func (f *Feed) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

// Step is one look: it asks for a reading when one is due (or the setup changed) and returns whether it asked.
func (f *Feed) Step(ctx context.Context) (bool, error) {
	cfg, err := f.Load(ctx)
	if err != nil {
		return false, err
	}
	now := f.now()
	if cfg == nil {
		// Said once, when it was set up and now is not; with nothing ever set up there is nothing to say.
		if f.hadConfig {
			_ = f.Sink.SaveStatus(ctx, Status{CheckedAt: now.UTC(), Error: "Not set up."})
		}
		f.hadConfig, f.last = false, nil
		return false, nil
	}
	changed := f.last == nil || *f.last != *cfg
	if !changed && now.Before(f.nextAt) {
		return false, nil
	}
	f.hadConfig = true
	c := *cfg
	f.last = &c

	readings, err := f.Client.Read(ctx, *cfg, now)
	if err != nil {
		var ce *Error
		if !errors.As(err, &ce) {
			return true, err // not a source's complaint (the request was cancelled): nothing to report
		}
		_ = f.Sink.SaveStatus(ctx, Status{CheckedAt: now.UTC(), Error: ce.Message})
		if ce.RateLimited {
			f.nextAt = now.Add(RetryAfterLimit)
		} else {
			f.nextAt = now.Add(RetryAfterError)
		}
		return true, nil
	}
	// Every hour the source gave, so hours missed while the server was off are filled in where the source has them (EIA does).
	for _, r := range readings {
		if err := f.Sink.AddCarbon(ctx, r.At, r.GramsPerKwh, r.Estimated); err != nil {
			return true, err
		}
	}
	latest := readings[len(readings)-1]
	g, at := latest.GramsPerKwh, latest.At
	_ = f.Sink.SaveStatus(ctx, Status{OK: true, CheckedAt: now.UTC(), GramsPerKwh: &g, At: &at})
	f.nextAt = now.Add(Interval)
	return true, nil
}

// Run looks at the config now and then until ctx ends. A failure is reported with logf and never stops it: an optional feature must not stop
// the server.
func (f *Feed) Run(ctx context.Context, logf func(format string, a ...any)) {
	tick := time.NewTicker(CheckEvery)
	defer tick.Stop()
	for {
		if _, err := f.Step(ctx); err != nil && ctx.Err() == nil {
			logf("carbon feed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
