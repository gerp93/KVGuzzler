package store

import (
	"context"
	"time"
)

// The price of a kWh each finished local day was saved with. Pricing is worked out when it is asked for, from the settings, so without this a change of
// the price source (one rate, bills, a published rate), or a tariff being read again, would change what every day in the history cost. A day is saved
// once it is over and never overwritten by the server: only an explicit re-price removes it. Today and the days to come are not here; they follow the settings.
const dayPriceSchema = `
CREATE TABLE IF NOT EXISTS day_prices (
  day       TEXT PRIMARY KEY,
  rate      TEXT NOT NULL,
  source    TEXT NOT NULL,
  saved_at  INTEGER NOT NULL
) WITHOUT ROWID;`

// DayPrice is one saved day: the local calendar day (YYYY-MM-DD), the price per kWh as an exact decimal, and which source gave it.
type DayPrice struct {
	Day    string
	Rate   string
	Source string
}

// DayPrices lists every saved day, oldest first.
func (s *Store) DayPrices(ctx context.Context) ([]DayPrice, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT day, rate, source FROM day_prices ORDER BY day`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DayPrice{}
	for rows.Next() {
		var d DayPrice
		if err := rows.Scan(&d.Day, &d.Rate, &d.Source); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// SaveDayPrices saves days that are not saved yet. A day already saved is left exactly as it is, so saving can be repeated freely.
func (s *Store) SaveDayPrices(ctx context.Context, days []DayPrice, at time.Time) (int, error) {
	if len(days) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	added := 0
	for _, d := range days {
		res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO day_prices(day, rate, source, saved_at) VALUES (?, ?, ?, ?)`, d.Day, d.Rate, d.Source, at.Unix())
		if err != nil {
			return 0, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			added++
		}
	}
	return added, tx.Commit()
}

// ForgetDayPrices removes the saved days from `from` to `to` inclusive (YYYY-MM-DD; an empty `to` is no end), so they follow the settings again until
// they are saved again. It returns how many there were.
func (s *Store) ForgetDayPrices(ctx context.Context, from, to string) (int, error) {
	q, args := `DELETE FROM day_prices WHERE day >= ?`, []any{from}
	if to != "" {
		q, args = q+` AND day <= ?`, append(args, to)
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}
