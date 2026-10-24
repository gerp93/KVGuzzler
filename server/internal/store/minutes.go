package store

import (
	"context"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/runs"
)

// MeterMinutes is every recorded minute of one meter in [from, to), in time order, for finding the times it was working (package runs).
func (s *Store) MeterMinutes(ctx context.Context, meterID string, from, to time.Time) ([]runs.Point, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT minute, avg_w, max_w, seconds FROM minutes
WHERE meter_id = ? AND minute >= ? AND minute < ? ORDER BY minute`, meterID, from.Unix(), to.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []runs.Point
	for rows.Next() {
		var t int64
		var p runs.Point
		if err := rows.Scan(&t, &p.AvgW, &p.MaxW, &p.Seconds); err != nil {
			return nil, err
		}
		p.Minute = time.Unix(t, 0).UTC()
		out = append(out, p)
	}
	return out, rows.Err()
}
