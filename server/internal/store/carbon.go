package store

import (
	"context"
	"database/sql"
	"time"
)

const carbonSchema = `
CREATE TABLE IF NOT EXISTS carbon_intensity (
  hour      INTEGER PRIMARY KEY,
  grams     REAL NOT NULL,
  estimated INTEGER NOT NULL DEFAULT 0
);`

// AddCarbon saves the grid's carbon intensity (grams of CO2 equivalent per kWh) for the hour `at` falls in. A newer figure for the same hour
// replaces the older: the source refines it.
func (s *Store) AddCarbon(ctx context.Context, at time.Time, gramsPerKwh float64, estimated bool) error {
	hour := at.Unix() / 3600 * 3600
	flag := 0
	if estimated {
		flag = 1
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO carbon_intensity(hour, grams, estimated) VALUES (?, ?, ?)
ON CONFLICT(hour) DO UPDATE SET grams = excluded.grams, estimated = excluded.estimated`, hour, gramsPerKwh, flag)
	return err
}

// QueryCarbon is the intensity of each hour that starts inside [from, to) or is the hour `from` falls in, by the unix second the hour starts.
func (s *Store) QueryCarbon(ctx context.Context, from, to time.Time) (map[int64]float64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT hour, grams FROM carbon_intensity WHERE hour >= ? AND hour < ?`, from.Unix()/3600*3600, to.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]float64{}
	for rows.Next() {
		var hour int64
		var grams float64
		if err := rows.Scan(&hour, &grams); err != nil {
			return nil, err
		}
		out[hour] = grams
	}
	return out, rows.Err()
}

// AverageCarbonEver is the average of every hour's intensity ever saved, or false when none was: the carbon footprint has nothing to show.
func (s *Store) AverageCarbonEver(ctx context.Context) (float64, bool, error) {
	var avg sql.NullFloat64
	if err := s.db.QueryRowContext(ctx, `SELECT AVG(grams) FROM carbon_intensity`).Scan(&avg); err != nil {
		return 0, false, err
	}
	return avg.Float64, avg.Valid, nil
}

// PcHourlyKWh is the energy the PC's sensors measured in each hour of [from, to), by the unix second the hour starts.
func (s *Store) PcHourlyKWh(ctx context.Context, sourceID string, from, to time.Time) (map[int64]float64, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT (minute / 3600) * 3600, SUM(avg_w * seconds) FROM pc_minutes
WHERE source_id = ? AND scope = 'component' AND minute >= ? AND minute < ? GROUP BY 1`, sourceID, from.Unix(), to.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]float64{}
	for rows.Next() {
		var hour int64
		var ws float64
		if err := rows.Scan(&hour, &ws); err != nil {
			return nil, err
		}
		out[hour] = ws / joulesPerKWh
	}
	return out, rows.Err()
}
