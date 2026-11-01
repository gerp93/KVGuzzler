package store

import (
	"context"
	"time"
)

// The size of the history database over time, kept so how fast it grows can be told (and what a year of it comes to). The server notes its own file's size
// every few hours; a snapshot of the database is that size at the moment it is taken, so the snapshot files themselves are never read. The table is not cut
// by the history start, and nothing in it is the history itself.
const dbSizeSchema = `
CREATE TABLE IF NOT EXISTS db_sizes (
  at    INTEGER PRIMARY KEY,
  bytes INTEGER NOT NULL
);`

// DBSize is the size of the database file at one moment.
type DBSize struct {
	At    time.Time
	Bytes int64
}

// SaveDBSize records a size at a moment. A size already recorded for that second stays.
func (s *Store) SaveDBSize(ctx context.Context, at time.Time, bytes int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO db_sizes(at, bytes) VALUES (?, ?)`, at.Unix(), bytes)
	return err
}

// DBSizes lists every recorded size, oldest first.
func (s *Store) DBSizes(ctx context.Context) ([]DBSize, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT at, bytes FROM db_sizes ORDER BY at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DBSize{}
	for rows.Next() {
		var at int64
		var d DBSize
		if err := rows.Scan(&at, &d.Bytes); err != nil {
			return nil, err
		}
		d.At = time.Unix(at, 0).UTC()
		out = append(out, d)
	}
	return out, rows.Err()
}
