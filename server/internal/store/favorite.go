package store

import (
	"context"

	"github.com/gerp93/KVGuzzler/server/internal/model"
)

// SetMeterFavorite marks a meter as a favorite (or takes the mark off) and returns it. The mark is the user's alone: nothing is computed
// from it, it only decides which meters the live page offers as a one-click choice.
func (s *Store) SetMeterFavorite(ctx context.Context, id string, favorite bool) (model.Meter, error) {
	if _, err := s.Meter(ctx, id); err != nil {
		return model.Meter{}, err
	}
	v := 0
	if favorite {
		v = 1
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE meters SET favorite = ? WHERE id = ?`, v, id); err != nil {
		return model.Meter{}, err
	}
	return s.Meter(ctx, id)
}
