// Package store keeps the server's history in SQLite (WAL mode, pure Go driver, so the server cross-compiles).
//
// History is per-minute rows per meter: the average and highest watts and how many seconds of the minute were
// actually covered. Energy is average watts x covered seconds, so a minute only partly covered counts only what was seen.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/gerp93/KVGuzzler/server/internal/model"
)

var ErrNotFound = errors.New("not found")

const joulesPerKWh = 3.6e6

type Store struct {
	db      *sql.DB
	secrets secretFile
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
	return &Store{db: db, secrets: secretFile{path: filepath.Join(filepath.Dir(path), "secrets.json")}}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// renameChannelsToMeters moves a database made when a meter was still called a channel: the table `channels` becomes `meters` and the
// column `minutes.channel_id` becomes `meter_id`. It runs before anything else, so the CREATE TABLE IF NOT EXISTS below never makes an empty
// `meters` beside the old table, and it does nothing on a database that has been moved already (or was made after the rename).
func renameChannelsToMeters(db *sql.DB) error {
	count := func(q string, args ...any) (int, error) {
		var n int
		err := db.QueryRow(q, args...).Scan(&n)
		return n, err
	}
	hasTable := func(name string) (bool, error) {
		n, err := count(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name)
		return n > 0, err
	}
	oldTable, err := hasTable("channels")
	if err != nil {
		return err
	}
	oldColumn := false
	if minutes, err := hasTable("minutes"); err != nil {
		return err
	} else if minutes {
		n, err := count(`SELECT COUNT(*) FROM pragma_table_info('minutes') WHERE name = 'channel_id'`)
		if err != nil {
			return err
		}
		oldColumn = n > 0
	}
	if !oldTable && !oldColumn {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if oldTable {
		var meters int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'meters'`).Scan(&meters); err != nil {
			return err
		}
		if meters > 0 {
			// Both exist: only an empty `meters` (made by mistake) may be thrown away; never guess between two with rows in them.
			var rows int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM meters`).Scan(&rows); err != nil {
				return err
			}
			if rows > 0 {
				return errors.New("both a channels table and a meters table have rows")
			}
			if _, err := tx.Exec(`DROP TABLE meters`); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`ALTER TABLE channels RENAME TO meters`); err != nil {
			return err
		}
	}
	if oldColumn {
		if _, err := tx.Exec(`ALTER TABLE minutes RENAME COLUMN channel_id TO meter_id`); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func migrate(db *sql.DB) error {
	if err := renameChannelsToMeters(db); err != nil {
		return fmt.Errorf("store: migrate: channels to meters: %w", err)
	}
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS devices (
  id    TEXT PRIMARY KEY,
  kind  TEXT NOT NULL,
  host  TEXT NOT NULL DEFAULT '',
  name  TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL DEFAULT '',
  removed INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS meters (
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
  meter_id TEXT NOT NULL,
  minute     INTEGER NOT NULL,
  avg_w      REAL NOT NULL,
  max_w      REAL NOT NULL,
  seconds    REAL NOT NULL,
  PRIMARY KEY (meter_id, minute)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
INSERT OR IGNORE INTO meta(key, value) VALUES ('schema_version', '1');` + pcSchema + computersSchema + groupsSchema + carbonSchema + evSchema)
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
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('meters') WHERE name = 'computer_id'`).Scan(&has); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	if has == 0 {
		if _, err := db.Exec(`ALTER TABLE meters ADD COLUMN computer_id TEXT`); err != nil {
			return fmt.Errorf("store: migrate: %w", err)
		}
	}
	// And before a clamp's sign could be flipped and its two legs paired.
	// And before a circuit could be part of a sub-panel's feed.
	for _, col := range []string{"invert INTEGER NOT NULL DEFAULT 0", "pair_with TEXT", "parent_id TEXT", "type TEXT", "favorite INTEGER NOT NULL DEFAULT 0", "ev INTEGER NOT NULL DEFAULT 0"} {
		name := col[:strings.IndexByte(col, ' ')]
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('meters') WHERE name = ?`, name).Scan(&has); err != nil {
			return fmt.Errorf("store: migrate: %w", err)
		}
		if has == 0 {
			if _, err := db.Exec(`ALTER TABLE meters ADD COLUMN ` + col); err != nil {
				return fmt.Errorf("store: migrate: %w", err)
			}
		}
	}
	// And before the highest current of each minute was kept next to its highest power.
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('minutes') WHERE name = 'max_a'`).Scan(&has); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	if has == 0 {
		if _, err := db.Exec(`ALTER TABLE minutes ADD COLUMN max_a REAL`); err != nil {
			return fmt.Errorf("store: migrate: %w", err)
		}
	}
	// And before the average current of each minute was kept (a run's average current is worked out from it).
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('minutes') WHERE name = 'avg_a'`).Scan(&has); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	if has == 0 {
		if _, err := db.Exec(`ALTER TABLE minutes ADD COLUMN avg_a REAL`); err != nil {
			return fmt.Errorf("store: migrate: %w", err)
		}
	}
	// And before a meter could say how big its breaker is.
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('meters') WHERE name = 'breaker_amps'`).Scan(&has); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	if has == 0 {
		if _, err := db.Exec(`ALTER TABLE meters ADD COLUMN breaker_amps REAL`); err != nil {
			return fmt.Errorf("store: migrate: %w", err)
		}
	}
	// And before plugs could be grouped.
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('meters') WHERE name = 'group_id'`).Scan(&has); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	if has == 0 {
		if _, err := db.Exec(`ALTER TABLE meters ADD COLUMN group_id TEXT`); err != nil {
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

// UpsertMeter records a meter. The label and the enabled switch are the user's and survive re-registering.
func (s *Store) UpsertMeter(ctx context.Context, c model.Meter) error {
	var idx any
	if c.Index != nil {
		idx = *c.Index
	}
	var max any
	if c.MaxWatts != nil {
		max = *c.MaxWatts
	}
	// A device registers its meters every time the server starts, and a Refoss input always says "circuit": a meter the user made the house (mains)
	// stays the house, or every restart would turn the whole-house figure off.
	_, err := s.db.ExecContext(ctx, `
INSERT INTO meters(id, device_id, idx, scope, label, enabled, max_watts) VALUES (?, ?, ?, ?, ?, 1, ?)
ON CONFLICT(id) DO UPDATE SET device_id = excluded.device_id, idx = excluded.idx, max_watts = excluded.max_watts,
  scope = CASE WHEN meters.scope = 'mains' AND excluded.scope = 'circuit' THEN meters.scope ELSE excluded.scope END`,
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

func scanMeter(sc interface{ Scan(...any) error }) (model.Meter, error) {
	var c model.Meter
	var idx sql.NullInt64
	var max, breaker sql.NullFloat64
	var computer, group, pair, parent, typ sql.NullString
	var scope string
	var enabled, invert, favorite, ev int
	if err := sc.Scan(&c.ID, &c.DeviceID, &idx, &scope, &c.Label, &enabled, &max, &computer, &group, &invert, &pair, &parent, &typ, &favorite, &breaker, &ev); err != nil {
		return c, err
	}
	c.Invert = invert == 1
	if breaker.Valid {
		c.BreakerAmps = &breaker.Float64
	}
	c.Favorite = favorite == 1
	c.EV = ev == 1
	c.Type = model.MeterType(typ.String) // as saved; settleTypes turns it into what the meter is
	if parent.Valid {
		c.ParentID = &parent.String
	}
	if pair.Valid {
		c.PairWith = &pair.String
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

const meterCols = `id, device_id, idx, scope, label, enabled, max_watts, computer_id, group_id, invert, pair_with, parent_id, type, favorite, breaker_amps, ev`

// settleTypes fills in each meter's Type: the mains is the house, plugs are devices, and a circuit is a sub-panel when circuits sit inside it
// (or the user said so) and a circuit otherwise. The user's own choice, kept in the type column, wins for a circuit.
func (s *Store) settleTypes(ctx context.Context, list ...*model.Meter) error {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT parent_id FROM meters WHERE parent_id IS NOT NULL AND scope = 'circuit'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	holds := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		holds[id] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, c := range list {
		switch {
		case c.Scope == model.ScopeMains:
			c.Type = model.TypeHouse
		case c.Scope == model.ScopePC && c.ComputerID != nil:
			c.Type = model.TypeComputer
		case c.Scope == model.ScopePlug || c.Scope == model.ScopePC:
			c.Type = model.TypeDevice
		case c.Type == model.TypeSubPanel || c.Type == model.TypeCircuit:
		case holds[c.ID]:
			c.Type = model.TypeSubPanel
		default:
			c.Type = model.TypeCircuit
		}
	}
	return nil
}

func (s *Store) Meters(ctx context.Context) ([]model.Meter, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+meterCols+` FROM meters ORDER BY device_id, idx IS NULL, idx, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Meter
	for rows.Next() {
		c, err := scanMeter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	ptrs := make([]*model.Meter, len(out))
	for i := range out {
		ptrs[i] = &out[i]
	}
	if err := s.settleTypes(ctx, ptrs...); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateMeter changes the user's label and/or enabled switch of a meter and returns it.
func (s *Store) UpdateMeter(ctx context.Context, id string, label *string, enabled *bool) (model.Meter, error) {
	if label != nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE meters SET label = ? WHERE id = ?`, *label, id); err != nil {
			return model.Meter{}, err
		}
	}
	if enabled != nil {
		v := 0
		if *enabled {
			v = 1
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE meters SET enabled = ? WHERE id = ?`, v, id); err != nil {
			return model.Meter{}, err
		}
	}
	c, err := scanMeter(s.db.QueryRowContext(ctx, `SELECT `+meterCols+` FROM meters WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Meter{}, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	return c, s.settleTypes(ctx, &c)
}

// AddMinute adds covered time to a meter's minute. Adding to a minute that already has rows merges them (the
// averages are weighted by seconds), so a restart in the middle of a minute loses nothing.
func (s *Store) AddMinute(ctx context.Context, meterID string, minute time.Time, avgW, maxW, seconds float64) error {
	return s.AddMinuteAmps(ctx, meterID, minute, avgW, maxW, nil, seconds)
}

// upsertMinute adds covered time to a minute that may already have rows. The highest current is kept like the highest power, and stays null for
// a meter that never reported one (a minute with a current in one half and none in the other keeps the one there is).
const upsertMinute = `
INSERT INTO minutes(meter_id, minute, avg_w, max_w, seconds, max_a, avg_a) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(meter_id, minute) DO UPDATE SET
  avg_w   = (avg_w * seconds + excluded.avg_w * excluded.seconds) / (seconds + excluded.seconds),
  max_w   = max(max_w, excluded.max_w),
  max_a   = CASE WHEN max_a IS NULL THEN excluded.max_a WHEN excluded.max_a IS NULL THEN max_a ELSE max(max_a, excluded.max_a) END,
  avg_a   = CASE WHEN avg_a IS NULL THEN excluded.avg_a WHEN excluded.avg_a IS NULL THEN avg_a
            ELSE (avg_a * seconds + excluded.avg_a * excluded.seconds) / (seconds + excluded.seconds) END,
  seconds = seconds + excluded.seconds`

// AddMinuteAmps is AddMinute with the highest current (amps) seen in the covered time; nil when the meter does not report one.
func (s *Store) AddMinuteAmps(ctx context.Context, meterID string, minute time.Time, avgW, maxW float64, maxA *float64, seconds float64) error {
	return s.AddMinuteCurrent(ctx, meterID, minute, avgW, maxW, nil, maxA, seconds)
}

// AddMinuteCurrent is AddMinute with the average and the highest current (amps) seen in the covered time; nil for one the meter does not report.
func (s *Store) AddMinuteCurrent(ctx context.Context, meterID string, minute time.Time, avgW, maxW float64, avgA, maxA *float64, seconds float64) error {
	if seconds <= 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx, upsertMinute, meterID, minute.Unix(), avgW, maxW, seconds, nullFloat(maxA), nullFloat(avgA))
	return err
}

func nullFloat(f *float64) any {
	if f == nil {
		return nil
	}
	return *f
}

// MinuteRow is one minute of one meter's history, for writing many at once.
type MinuteRow struct {
	MeterID string
	Minute  time.Time
	AvgW    float64
	MaxW    float64
	Seconds float64
	// MaxA and AvgA are the highest and the average current (amps) in the minute; nil when the meter does not report one.
	MaxA *float64
	AvgA *float64
}

// AddMinutes writes many minutes in one transaction, merging into any minute that already has rows exactly as AddMinute does.
func (s *Store) AddMinutes(ctx context.Context, rows []MinuteRow) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, upsertMinute)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range rows {
		if r.Seconds <= 0 {
			continue
		}
		if _, err := stmt.ExecContext(ctx, r.MeterID, r.Minute.Unix(), r.AvgW, r.MaxW, r.Seconds, nullFloat(r.MaxA), nullFloat(r.AvgA)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type Totals struct {
	KWh            float64
	PeakWatts      float64
	MonitoredHours float64
	// PeakAmps is the highest current in the period; nil when the meter reported none.
	PeakAmps *float64
}

// FirstMinute is the first minute a meter has any energy recorded, or false when it has none.
func (s *Store) FirstMinute(ctx context.Context, meterID string) (time.Time, bool, error) {
	var first sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MIN(minute) FROM minutes WHERE meter_id = ?`, meterID).Scan(&first); err != nil {
		return time.Time{}, false, err
	}
	if !first.Valid {
		return time.Time{}, false, nil
	}
	return time.Unix(first.Int64, 0).UTC(), true, nil
}

// Totals is a meter's energy, highest reading and covered time in [from, to).
func (s *Store) Totals(ctx context.Context, meterID string, from, to time.Time) (Totals, error) {
	var ws, peak, secs float64
	var amps sql.NullFloat64
	err := s.db.QueryRowContext(ctx, `
SELECT COALESCE(SUM(avg_w * seconds), 0), COALESCE(MAX(max_w), 0), COALESCE(SUM(seconds), 0), MAX(max_a)
FROM minutes WHERE meter_id = ? AND minute >= ? AND minute < ?`,
		meterID, from.Unix(), to.Unix()).Scan(&ws, &peak, &secs, &amps)
	if err != nil {
		return Totals{}, err
	}
	t := Totals{KWh: ws / joulesPerKWh, PeakWatts: peak, MonitoredHours: secs / 3600}
	if amps.Valid {
		t.PeakAmps = &amps.Float64
	}
	return t, nil
}

type agg struct {
	ws, max, secs float64
	amps          sql.NullFloat64
}

// Series is a meter's power over [from, to), bucketed. A "day" is a day of loc.
func (s *Store) Series(ctx context.Context, meterID string, from, to time.Time, bucket model.Bucket, loc *time.Location) ([]model.Point, error) {
	q := `SELECT minute, avg_w * seconds, max_w, seconds, max_a FROM minutes
WHERE meter_id = ? AND minute >= ? AND minute < ? ORDER BY minute`
	if bucket != model.BucketMinute {
		q = `SELECT (minute / 3600) * 3600, SUM(avg_w * seconds), MAX(max_w), SUM(seconds), MAX(max_a) FROM minutes
WHERE meter_id = ? AND minute >= ? AND minute < ? GROUP BY 1 ORDER BY 1`
	}
	rows, err := s.db.QueryContext(ctx, q, meterID, from.Unix(), to.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	buckets := map[int64]*agg{}
	for rows.Next() {
		var t int64
		var a agg
		if err := rows.Scan(&t, &a.ws, &a.max, &a.secs, &a.amps); err != nil {
			return nil, err
		}
		key := bucketStart(time.Unix(t, 0), bucket, loc).Unix()
		if b := buckets[key]; b != nil {
			b.ws += a.ws
			b.secs += a.secs
			b.max = maxF(b.max, a.max)
			if a.amps.Valid && (!b.amps.Valid || a.amps.Float64 > b.amps.Float64) {
				b.amps = a.amps
			}
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
		p := model.Point{T: time.Unix(k, 0).UTC(), AvgWatts: b.ws / b.secs, MaxWatts: b.max, KWh: b.ws / joulesPerKWh}
		if b.amps.Valid {
			p.MaxAmps = &b.amps.Float64
		}
		points = append(points, p)
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

// RemoveDevice marks a device as no longer polled. Its meters and history stay.
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

// DeviceMeters returns the meters of one device.
func (s *Store) DeviceMeters(ctx context.Context, deviceID string) ([]model.Meter, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+meterCols+` FROM meters WHERE device_id = ? ORDER BY idx IS NULL, idx, id`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Meter
	for rows.Next() {
		c, err := scanMeter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// HourlyKWh is the energy the given meters together used in each hour of [from, to), by the unix second the hour starts. Only the
// meters of one scope should be given (never the house and its circuits together: that would count the same power twice).
func (s *Store) HourlyKWh(ctx context.Context, meterIDs []string, from, to time.Time) (map[int64]float64, error) {
	out := map[int64]float64{}
	if len(meterIDs) == 0 {
		return out, nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(meterIDs)), ",")
	args := make([]any, 0, len(meterIDs)+2)
	for _, id := range meterIDs {
		args = append(args, id)
	}
	args = append(args, from.Unix(), to.Unix())
	rows, err := s.db.QueryContext(ctx, `SELECT (minute / 3600) * 3600, SUM(avg_w * seconds) FROM minutes WHERE meter_id IN (`+marks+`) AND minute >= ? AND minute < ? GROUP BY 1`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
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
