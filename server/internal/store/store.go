// Package store keeps the server's history in SQLite (WAL mode, pure Go driver, so the server cross-compiles).
//
// History is per-minute rows per channel: the average and highest watts and how many seconds of the minute were
// actually covered. Energy is average watts x covered seconds, so a minute only partly covered counts only what was seen.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	_ "modernc.org/sqlite"

	"github.com/gerp93/KVGuzzler/server/internal/model"
)

var ErrNotFound = errors.New("not found")

const joulesPerKWh = 3.6e6

type Store struct {
	db *sql.DB
}

// Open opens (creating it if needed) the database file at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// One connection, so the pragmas below hold for every statement.
	db.SetMaxOpenConns(1)
	for _, q := range []string{
		`PRAGMA journal_mode = WAL`,
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA foreign_keys = ON`,
	} {
		if _, err := db.Exec(q); err != nil {
			db.Close()
			return nil, fmt.Errorf("store: %s: %w", q, err)
		}
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS devices (
  id    TEXT PRIMARY KEY,
  kind  TEXT NOT NULL,
  host  TEXT NOT NULL DEFAULT '',
  name  TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL DEFAULT '',
  removed INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS channels (
  id        TEXT PRIMARY KEY,
  device_id TEXT NOT NULL,
  idx       INTEGER,
  scope     TEXT NOT NULL,
  label     TEXT NOT NULL,
  enabled   INTEGER NOT NULL DEFAULT 1,
  max_watts REAL,
  computer_id TEXT,
  group_id TEXT
);
CREATE TABLE IF NOT EXISTS minutes (
  channel_id TEXT NOT NULL,
  minute     INTEGER NOT NULL,
  avg_w      REAL NOT NULL,
  max_w      REAL NOT NULL,
  seconds    REAL NOT NULL,
  PRIMARY KEY (channel_id, minute)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
INSERT OR IGNORE INTO meta(key, value) VALUES ('schema_version', '1');` + pcSchema + computersSchema + groupsSchema)
	if err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	// Databases made before devices could be removed have no such column.
	var has int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('devices') WHERE name = 'removed'`).Scan(&has); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	if has == 0 {
		if _, err := db.Exec(`ALTER TABLE devices ADD COLUMN removed INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("store: migrate: %w", err)
		}
	}
	// Databases made before plugs could belong to a computer have no such column.
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('channels') WHERE name = 'computer_id'`).Scan(&has); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	if has == 0 {
		if _, err := db.Exec(`ALTER TABLE channels ADD COLUMN computer_id TEXT`); err != nil {
			return fmt.Errorf("store: migrate: %w", err)
		}
	}
	// And before plugs could be grouped.
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('channels') WHERE name = 'group_id'`).Scan(&has); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	if has == 0 {
		if _, err := db.Exec(`ALTER TABLE channels ADD COLUMN group_id TEXT`); err != nil {
			return fmt.Errorf("store: migrate: %w", err)
		}
	}
	return nil
}

// UpsertDevice records a device. Its identity fields are refreshed; nothing the user set is touched.
func (s *Store) UpsertDevice(ctx context.Context, d model.Device) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO devices(id, kind, host, name, model) VALUES (?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET kind = excluded.kind, host = excluded.host, name = excluded.name, model = excluded.model, removed = 0`,
		d.ID, d.Kind, d.Host, d.Name, d.Model)
	return err
}

// UpsertChannel records a channel. The label and the enabled switch are the user's and survive re-registering.
func (s *Store) UpsertChannel(ctx context.Context, c model.Channel) error {
	var idx any
	if c.Index != nil {
		idx = *c.Index
	}
	var max any
	if c.MaxWatts != nil {
		max = *c.MaxWatts
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO channels(id, device_id, idx, scope, label, enabled, max_watts) VALUES (?, ?, ?, ?, ?, 1, ?)
ON CONFLICT(id) DO UPDATE SET device_id = excluded.device_id, idx = excluded.idx, scope = excluded.scope, max_watts = excluded.max_watts`,
		c.ID, c.DeviceID, idx, string(c.Scope), c.Label, max)
	return err
}

func (s *Store) Devices(ctx context.Context) ([]model.Device, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind, host, name, model FROM devices WHERE removed = 0 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Device
	for rows.Next() {
		var d model.Device
		if err := rows.Scan(&d.ID, &d.Kind, &d.Host, &d.Name, &d.Model); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func scanChannel(sc interface{ Scan(...any) error }) (model.Channel, error) {
	var c model.Channel
	var idx sql.NullInt64
	var max sql.NullFloat64
	var computer, group sql.NullString
	var scope string
	var enabled int
	if err := sc.Scan(&c.ID, &c.DeviceID, &idx, &scope, &c.Label, &enabled, &max, &computer, &group); err != nil {
		return c, err
	}
	if computer.Valid {
		c.ComputerID = &computer.String
	}
	if group.Valid {
		c.GroupID = &group.String
	}
	c.Scope = model.Scope(scope)
	c.Enabled = enabled != 0
	if idx.Valid {
		v := int(idx.Int64)
		c.Index = &v
	}
	if max.Valid {
		c.MaxWatts = &max.Float64
	}
	return c, nil
}

const channelCols = `id, device_id, idx, scope, label, enabled, max_watts, computer_id, group_id`

func (s *Store) Channels(ctx context.Context) ([]model.Channel, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+channelCols+` FROM channels ORDER BY device_id, idx IS NULL, idx, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Channel
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpdateChannel changes the user's label and/or enabled switch of a channel and returns it.
func (s *Store) UpdateChannel(ctx context.Context, id string, label *string, enabled *bool) (model.Channel, error) {
	if label != nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE channels SET label = ? WHERE id = ?`, *label, id); err != nil {
			return model.Channel{}, err
		}
	}
	if enabled != nil {
		v := 0
		if *enabled {
			v = 1
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE channels SET enabled = ? WHERE id = ?`, v, id); err != nil {
			return model.Channel{}, err
		}
	}
	c, err := scanChannel(s.db.QueryRowContext(ctx, `SELECT `+channelCols+` FROM channels WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Channel{}, ErrNotFound
	}
	return c, err
}

// AddMinute adds covered time to a channel's minute. Adding to a minute that already has rows merges them (the
// averages are weighted by seconds), so a restart in the middle of a minute loses nothing.
func (s *Store) AddMinute(ctx context.Context, channelID string, minute time.Time, avgW, maxW, seconds float64) error {
	if seconds <= 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO minutes(channel_id, minute, avg_w, max_w, seconds) VALUES (?, ?, ?, ?, ?)
ON CONFLICT(channel_id, minute) DO UPDATE SET
  avg_w   = (avg_w * seconds + excluded.avg_w * excluded.seconds) / (seconds + excluded.seconds),
  max_w   = max(max_w, excluded.max_w),
  seconds = seconds + excluded.seconds`,
		channelID, minute.Unix(), avgW, maxW, seconds)
	return err
}

type Totals struct {
	KWh            float64
	PeakWatts      float64
	MonitoredHours float64
}

// Totals is a channel's energy, highest reading and covered time in [from, to).
func (s *Store) Totals(ctx context.Context, channelID string, from, to time.Time) (Totals, error) {
	var ws, peak, secs float64
	err := s.db.QueryRowContext(ctx, `
SELECT COALESCE(SUM(avg_w * seconds), 0), COALESCE(MAX(max_w), 0), COALESCE(SUM(seconds), 0)
FROM minutes WHERE channel_id = ? AND minute >= ? AND minute < ?`,
		channelID, from.Unix(), to.Unix()).Scan(&ws, &peak, &secs)
	if err != nil {
		return Totals{}, err
	}
	return Totals{KWh: ws / joulesPerKWh, PeakWatts: peak, MonitoredHours: secs / 3600}, nil
}

type agg struct{ ws, max, secs float64 }

// Series is a channel's power over [from, to), bucketed. A "day" is a day of loc.
func (s *Store) Series(ctx context.Context, channelID string, from, to time.Time, bucket model.Bucket, loc *time.Location) ([]model.Point, error) {
	q := `SELECT minute, avg_w * seconds, max_w, seconds FROM minutes
WHERE channel_id = ? AND minute >= ? AND minute < ? ORDER BY minute`
	if bucket != model.BucketMinute {
		q = `SELECT (minute / 3600) * 3600, SUM(avg_w * seconds), MAX(max_w), SUM(seconds) FROM minutes
WHERE channel_id = ? AND minute >= ? AND minute < ? GROUP BY 1 ORDER BY 1`
	}
	rows, err := s.db.QueryContext(ctx, q, channelID, from.Unix(), to.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	buckets := map[int64]*agg{}
	for rows.Next() {
		var t int64
		var a agg
		if err := rows.Scan(&t, &a.ws, &a.max, &a.secs); err != nil {
			return nil, err
		}
		key := bucketStart(time.Unix(t, 0), bucket, loc).Unix()
		if b := buckets[key]; b != nil {
			b.ws += a.ws
			b.secs += a.secs
			b.max = maxF(b.max, a.max)
		} else {
			buckets[key] = &a
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	keys := make([]int64, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	points := make([]model.Point, 0, len(keys))
	for _, k := range keys {
		b := buckets[k]
		points = append(points, model.Point{T: time.Unix(k, 0).UTC(), AvgWatts: b.ws / b.secs, MaxWatts: b.max, KWh: b.ws / joulesPerKWh})
	}
	return points, nil
}

func bucketStart(t time.Time, bucket model.Bucket, loc *time.Location) time.Time {
	switch bucket {
	case model.BucketMinute:
		return t.Truncate(time.Minute)
	case model.BucketHour:
		return t.Truncate(time.Hour)
	default:
		l := t.In(loc)
		return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, loc)
	}
}

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// Meta returns a small saved value by key (settings kept as text), and whether it exists.
func (s *Store) Meta(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

// SetMeta saves a small value by key, replacing any earlier one.
func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO meta(key, value) VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// RemoveDevice marks a device as no longer polled. Its channels and history stay.
func (s *Store) RemoveDevice(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE devices SET removed = 1 WHERE id = ? AND removed = 0`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeviceChannels returns the channels of one device.
func (s *Store) DeviceChannels(ctx context.Context, deviceID string) ([]model.Channel, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+channelCols+` FROM channels WHERE device_id = ? ORDER BY idx IS NULL, idx, id`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Channel
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
