package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"
)

// Outages are stretches when nothing in the house was recorded, with what is known about why. They describe the history and are never part of it: no
// figure is changed by one. The server itself keeps the time it last knew it was running (the heartbeat, in the meta table) so that after a restart it
// can tell how long it was away.
const outageSchema = `
CREATE TABLE IF NOT EXISTS outages (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  kind       TEXT NOT NULL,
  started_at INTEGER NOT NULL,
  ended_at   INTEGER NOT NULL,
  source     TEXT NOT NULL,
  note       TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS outages_by_start ON outages(started_at);`

// The kinds of outage.
const (
	// OutagePower: the power was out. Nothing was drawing, so the stretch really used nothing.
	OutagePower = "power"
	// OutageServer: the power was on but the server was not recording, so what the house used then is missing from the figures.
	OutageServer = "server"
	// OutageUnknown: nothing was recorded and what the cause was could not be told.
	OutageUnknown = "unknown"
)

// Where an outage came from.
const (
	// OutageFromPlugs: found by the server itself, from when its plugs started again.
	OutageFromPlugs = "plugs"
	// OutageFromGap: found by the server itself, cause not told.
	OutageFromGap = "gap"
	// OutageManual: typed in.
	OutageManual = "manual"
)

const aliveKey = "alive_at"

// Outage is one stretch of time with nothing recorded.
type Outage struct {
	ID      int64
	Kind    string
	Started time.Time
	Ended   time.Time
	Source  string
	Note    string
}

// ErrOutageOverlap means the stretch overlaps one already saved.
var ErrOutageOverlap = errors.New("store: overlaps another outage")

// SetAlive saves the moment the server knew it was running.
func (s *Store) SetAlive(ctx context.Context, at time.Time) error {
	return s.SetMeta(ctx, aliveKey, strconv.FormatInt(at.Unix(), 10))
}

// Alive is the last moment the server knew it was running, and whether it ever noted one.
func (s *Store) Alive(ctx context.Context) (time.Time, bool, error) {
	v, ok, err := s.Meta(ctx, aliveKey)
	if err != nil || !ok {
		return time.Time{}, false, err
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return time.Time{}, false, nil
	}
	return time.Unix(n, 0).UTC(), true, nil
}

// AddOutage saves an outage and returns it with its id. A stretch that overlaps another is refused.
func (s *Store) AddOutage(ctx context.Context, o Outage) (Outage, error) {
	out, err := s.ReplaceOutage(ctx, 0, []Outage{o})
	if err != nil {
		return Outage{}, err
	}
	return out[0], nil
}

// ReplaceOutage removes the outage `id` (0 for none) and saves `with` in its place, in one step. Nothing in `with` is a plain removal. A stretch that overlaps
// another outage is refused and nothing changes.
func (s *Store) ReplaceOutage(ctx context.Context, id int64, with []Outage) ([]Outage, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM outages WHERE id = ?`, id); err != nil {
		return nil, err
	}
	out := make([]Outage, 0, len(with))
	for _, o := range with {
		if err := outageFree(ctx, tx, o.Started, o.Ended, 0); err != nil {
			return nil, err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO outages(kind, started_at, ended_at, source, note) VALUES (?, ?, ?, ?, ?)`,
			o.Kind, o.Started.Unix(), o.Ended.Unix(), o.Source, o.Note)
		if err != nil {
			return nil, err
		}
		if o.ID, err = res.LastInsertId(); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, tx.Commit()
}

// outageFree is nil when no outage other than `except` overlaps [from, to).
func outageFree(ctx context.Context, tx *sql.Tx, from, to time.Time, except int64) error {
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM outages WHERE id != ? AND started_at < ? AND ended_at > ?`, except, to.Unix(), from.Unix()).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrOutageOverlap
	}
	return nil
}

// UpdateOutage replaces an outage's kind, times, source and note. It reports whether there was one.
func (s *Store) UpdateOutage(ctx context.Context, o Outage) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if err := outageFree(ctx, tx, o.Started, o.Ended, o.ID); err != nil {
		return false, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE outages SET kind = ?, started_at = ?, ended_at = ?, source = ?, note = ? WHERE id = ?`,
		o.Kind, o.Started.Unix(), o.Ended.Unix(), o.Source, o.Note, o.ID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, tx.Commit()
}

// DeleteOutage removes one. It reports whether there was one.
func (s *Store) DeleteOutage(ctx context.Context, id int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM outages WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Outage is one by id.
func (s *Store) Outage(ctx context.Context, id int64) (Outage, bool, error) {
	o, err := scanOutage(s.db.QueryRowContext(ctx, `SELECT id, kind, started_at, ended_at, source, note FROM outages WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Outage{}, false, nil
	}
	return o, err == nil, err
}

// Outages lists the ones that end after `from` (cut at the start of history, like every read of the history), newest first, at most limit.
func (s *Store) Outages(ctx context.Context, from time.Time, limit int) ([]Outage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind, started_at, ended_at, source, note FROM outages WHERE ended_at > ? ORDER BY started_at DESC, id DESC LIMIT ?`,
		s.clip(from).Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Outage{}
	for rows.Next() {
		o, err := scanOutage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

type rowScanner interface{ Scan(dest ...any) error }

func scanOutage(r rowScanner) (Outage, error) {
	var o Outage
	var from, to int64
	if err := r.Scan(&o.ID, &o.Kind, &from, &to, &o.Source, &o.Note); err != nil {
		return Outage{}, err
	}
	o.Started, o.Ended = time.Unix(from, 0).UTC(), time.Unix(to, 0).UTC()
	return o, nil
}
