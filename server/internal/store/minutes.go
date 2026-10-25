package store

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/runs"
)

// MeterMinutes is every recorded minute of one meter in [from, to), in time order, for finding the times it was working (package runs).
func (s *Store) MeterMinutes(ctx context.Context, meterID string, from, to time.Time) ([]runs.Point, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT minute, avg_w, max_w, seconds, avg_a, max_a FROM minutes
WHERE meter_id = ? AND minute >= ? AND minute < ? ORDER BY minute`, meterID, from.Unix(), to.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []runs.Point
	for rows.Next() {
		var t int64
		var p runs.Point
		var avgA, maxA sql.NullFloat64
		if err := rows.Scan(&t, &p.AvgW, &p.MaxW, &p.Seconds, &avgA, &maxA); err != nil {
			return nil, err
		}
		p.Minute = time.Unix(t, 0).UTC()
		if avgA.Valid {
			p.AvgAmps, p.HaveAmps = avgA.Float64, true
			p.MaxAmps = maxA.Float64
			if !maxA.Valid {
				p.MaxAmps = avgA.Float64
			}
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// MinuteWatts is, for each minute of [from, to) in which every one of the given meters reported at least half the minute, those meters' average
// watts added together (a meter alone, or the two mains legs together), in time order. A minute where one of them is missing is left out, so the
// sum is never one leg short.
func (s *Store) MinuteWatts(ctx context.Context, meterIDs []string, from, to time.Time) ([]float64, error) {
	if len(meterIDs) == 0 {
		return nil, nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(meterIDs)), ",")
	args := make([]any, 0, len(meterIDs)+3)
	for _, id := range meterIDs {
		args = append(args, id)
	}
	args = append(args, from.Unix(), to.Unix(), len(meterIDs))
	rows, err := s.db.QueryContext(ctx, `SELECT SUM(avg_w) FROM minutes
WHERE meter_id IN (`+marks+`) AND minute >= ? AND minute < ? AND seconds >= 30
GROUP BY minute HAVING COUNT(*) = ? ORDER BY minute`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []float64
	for rows.Next() {
		var w float64
		if err := rows.Scan(&w); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}
