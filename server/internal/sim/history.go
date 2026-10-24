package sim

import (
	"context"
	"time"
)

// MinuteRow is one minute of one meter, as the store keeps it.
type MinuteRow struct {
	MeterID string
	Minute  time.Time
	AvgW    float64
	MaxW    float64
	Seconds float64
}

// History is what the simulated house would have recorded each minute from `days` days before `end` up to `end` (the start of a minute), so a demo has
// a few weeks behind it instead of nothing. The same day plans drive it as the live readings, so the past and the present look alike.
func History(ctx context.Context, end time.Time, days int, add func([]MinuteRow) error) error {
	end = end.Truncate(time.Minute)
	start := end.Add(-time.Duration(days) * 24 * time.Hour)
	var batch []MinuteRow
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		err := add(batch)
		batch = batch[:0]
		return err
	}
	for t := start; t.Before(end); t = t.Add(time.Minute) {
		if err := ctx.Err(); err != nil {
			return err
		}
		// The middle of the minute stands for it.
		for _, r := range at(t.Add(30 * time.Second)) {
			batch = append(batch, MinuteRow{MeterID: r.MeterID, Minute: t, AvgW: r.Watts, MaxW: r.Watts * 1.03, Seconds: 60})
		}
		if len(batch) >= 6000 {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return flush()
}
