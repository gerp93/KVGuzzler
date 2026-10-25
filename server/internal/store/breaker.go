package store

import (
	"context"
	"math"

	"github.com/gerp93/KVGuzzler/server/internal/model"
)

// MaxBreakerAmps is the largest breaker a meter may say it is on: a main disconnect of a big house is 400 A.
const MaxBreakerAmps = 1000

// SetMeterBreaker saves the size of the breaker a meter is on (amps), or (nil) forgets it, and returns the meter. It is only ever used to say how
// much of the breaker a current is; nothing else follows from it.
func (s *Store) SetMeterBreaker(ctx context.Context, id string, amps *float64) (model.Meter, error) {
	if _, err := s.Meter(ctx, id); err != nil {
		return model.Meter{}, err
	}
	var v any
	if amps != nil {
		if math.IsNaN(*amps) || *amps <= 0 || *amps > MaxBreakerAmps {
			return model.Meter{}, invalid("A breaker is between 0 and 1000 amps.")
		}
		v = *amps
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE meters SET breaker_amps = ? WHERE id = ?`, v, id); err != nil {
		return model.Meter{}, err
	}
	return s.Meter(ctx, id)
}
