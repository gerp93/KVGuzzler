package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Alerts: things that need attention, raised by rules that look at the house's own history. Rules (the built-in ones with their settings, and the owner's own
// per meter) and the alerts they raise are kept here, and so are the summaries (digests) made of a day or a week. None of it is part of the history: no figure
// is read from it.
const alertSchema = `
CREATE TABLE IF NOT EXISTS alert_rules (
  id         TEXT PRIMARY KEY,
  builtin    INTEGER NOT NULL DEFAULT 0,
  kind       TEXT NOT NULL,
  name       TEXT NOT NULL DEFAULT '',
  enabled    INTEGER NOT NULL DEFAULT 1,
  meter_id   TEXT NOT NULL DEFAULT '',
  params     TEXT NOT NULL DEFAULT '{}',
  severity   TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS alerts (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  rule_id    TEXT NOT NULL,
  key        TEXT NOT NULL,
  severity   TEXT NOT NULL,
  title      TEXT NOT NULL,
  detail     TEXT NOT NULL DEFAULT '',
  meter_id   TEXT NOT NULL DEFAULT '',
  link       TEXT NOT NULL DEFAULT '',
  opened_at  INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  cleared_at INTEGER,
  acked_at   INTEGER
);
CREATE INDEX IF NOT EXISTS alerts_by_rule ON alerts(rule_id, key);
CREATE INDEX IF NOT EXISTS alerts_by_open ON alerts(cleared_at, opened_at);
CREATE TABLE IF NOT EXISTS digests (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  kind         TEXT NOT NULL,
  period_start TEXT NOT NULL,
  period_end   TEXT NOT NULL,
  created_at   INTEGER NOT NULL,
  body         TEXT NOT NULL,
  UNIQUE(kind, period_start)
);`

// AlertRule is one rule as saved: a built-in one (only its switch and settings are kept) or one the owner made for a meter.
type AlertRule struct {
	ID       string
	Builtin  bool
	Kind     string
	Name     string
	Enabled  bool
	MeterID  string
	Params   string // JSON
	Severity string
	Created  time.Time
}

// Alert is one thing that needs attention. It is open until its condition has passed (Cleared) and is dismissed from the banner by being acknowledged.
type Alert struct {
	ID       int64
	RuleID   string
	Key      string
	Severity string
	Title    string
	Detail   string
	MeterID  string
	Link     string
	Opened   time.Time
	Updated  time.Time
	Cleared  *time.Time
	Acked    *time.Time
}

// Digest is a summary of a day or a week, kept as the JSON it is shown from.
type Digest struct {
	ID          int64
	Kind        string
	PeriodStart string
	PeriodEnd   string
	Created     time.Time
	Body        string
}

// AlertRules lists every saved rule, the built-in ones first.
func (s *Store) AlertRules(ctx context.Context) ([]AlertRule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, builtin, kind, name, enabled, meter_id, params, severity, created_at FROM alert_rules ORDER BY builtin DESC, created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AlertRule{}
	for rows.Next() {
		var r AlertRule
		var builtin, enabled, created int64
		if err := rows.Scan(&r.ID, &builtin, &r.Kind, &r.Name, &enabled, &r.MeterID, &r.Params, &r.Severity, &created); err != nil {
			return nil, err
		}
		r.Builtin, r.Enabled, r.Created = builtin != 0, enabled != 0, time.Unix(created, 0).UTC()
		out = append(out, r)
	}
	return out, rows.Err()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// SaveAlertRule adds a rule or replaces the one with the same id.
func (s *Store) SaveAlertRule(ctx context.Context, r AlertRule) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO alert_rules(id, builtin, kind, name, enabled, meter_id, params, severity, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET name = excluded.name, enabled = excluded.enabled, meter_id = excluded.meter_id, params = excluded.params, severity = excluded.severity`,
		r.ID, b2i(r.Builtin), r.Kind, r.Name, b2i(r.Enabled), r.MeterID, r.Params, r.Severity, r.Created.Unix())
	return err
}

// DeleteAlertRule removes one of the owner's own rules (a built-in one cannot be removed, only switched off), with the alerts it raised. It reports whether
// there was one.
func (s *Store) DeleteAlertRule(ctx context.Context, id string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM alert_rules WHERE id = ? AND builtin = 0`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM alerts WHERE rule_id = ?`, id); err != nil {
			return false, err
		}
	}
	return n > 0, tx.Commit()
}

const alertColumns = `id, rule_id, key, severity, title, detail, meter_id, link, opened_at, updated_at, cleared_at, acked_at`

func scanAlert(r rowScanner) (Alert, error) {
	var a Alert
	var opened, updated int64
	var cleared, acked sql.NullInt64
	if err := r.Scan(&a.ID, &a.RuleID, &a.Key, &a.Severity, &a.Title, &a.Detail, &a.MeterID, &a.Link, &opened, &updated, &cleared, &acked); err != nil {
		return Alert{}, err
	}
	a.Opened, a.Updated = time.Unix(opened, 0).UTC(), time.Unix(updated, 0).UTC()
	if cleared.Valid {
		t := time.Unix(cleared.Int64, 0).UTC()
		a.Cleared = &t
	}
	if acked.Valid {
		t := time.Unix(acked.Int64, 0).UTC()
		a.Acked = &t
	}
	return a, nil
}

// OpenAlert saves a new alert (open unless `cleared` is set, for one that is only an event) and returns it with its id.
func (s *Store) OpenAlert(ctx context.Context, a Alert) (Alert, error) {
	var cleared any
	if a.Cleared != nil {
		cleared = a.Cleared.Unix()
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO alerts(rule_id, key, severity, title, detail, meter_id, link, opened_at, updated_at, cleared_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.RuleID, a.Key, a.Severity, a.Title, a.Detail, a.MeterID, a.Link, a.Opened.Unix(), a.Updated.Unix(), cleared)
	if err != nil {
		return Alert{}, err
	}
	a.ID, err = res.LastInsertId()
	return a, err
}

// UpdateAlert changes what an open alert says as its condition changes.
func (s *Store) UpdateAlert(ctx context.Context, id int64, severity, title, detail string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE alerts SET severity = ?, title = ?, detail = ?, updated_at = ? WHERE id = ?`, severity, title, detail, at.Unix(), id)
	return err
}

// ClearAlert closes an alert whose condition has passed. It no longer needs attention, so it also leaves the banner.
func (s *Store) ClearAlert(ctx context.Context, id int64, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE alerts SET cleared_at = ?, updated_at = ?, acked_at = COALESCE(acked_at, ?) WHERE id = ? AND cleared_at IS NULL`, at.Unix(), at.Unix(), at.Unix(), id)
	return err
}

// OpenAlertFor is the open alert of a rule with the key, if there is one.
func (s *Store) OpenAlertFor(ctx context.Context, ruleID, key string) (Alert, bool, error) {
	a, err := scanAlert(s.db.QueryRowContext(ctx, `SELECT `+alertColumns+` FROM alerts WHERE rule_id = ? AND key = ? AND cleared_at IS NULL ORDER BY id DESC LIMIT 1`, ruleID, key))
	if errors.Is(err, sql.ErrNoRows) {
		return Alert{}, false, nil
	}
	return a, err == nil, err
}

// AlertSeen says whether an alert with the key was ever raised by the rule, open or not: an event is raised once.
func (s *Store) AlertSeen(ctx context.Context, ruleID, key string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM alerts WHERE rule_id = ? AND key = ?`, ruleID, key).Scan(&n)
	return n > 0, err
}

// OpenAlerts lists the alerts whose condition still holds, newest first.
func (s *Store) OpenAlerts(ctx context.Context) ([]Alert, error) {
	return s.queryAlerts(ctx, `SELECT `+alertColumns+` FROM alerts WHERE cleared_at IS NULL ORDER BY opened_at DESC, id DESC`)
}

// RecentAlerts lists the alerts opened since `since` (open or not), newest first, at most limit.
func (s *Store) RecentAlerts(ctx context.Context, since time.Time, limit int) ([]Alert, error) {
	return s.queryAlerts(ctx, `SELECT `+alertColumns+` FROM alerts WHERE opened_at >= ? OR cleared_at IS NULL ORDER BY opened_at DESC, id DESC LIMIT ?`, since.Unix(), limit)
}

// UnackedAlerts lists the alerts that are still on the banner: open, or an event, and not dismissed.
func (s *Store) UnackedAlerts(ctx context.Context, since time.Time) ([]Alert, error) {
	return s.queryAlerts(ctx, `SELECT `+alertColumns+` FROM alerts WHERE acked_at IS NULL AND opened_at >= ? ORDER BY opened_at DESC, id DESC`, since.Unix())
}

func (s *Store) queryAlerts(ctx context.Context, q string, args ...any) ([]Alert, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Alert{}
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AckAlert dismisses one alert from the banner. It reports whether there was one not dismissed already.
func (s *Store) AckAlert(ctx context.Context, id int64, at time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE alerts SET acked_at = ? WHERE id = ? AND acked_at IS NULL`, at.Unix(), id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// AckAllAlerts dismisses every alert from the banner and returns how many there were.
func (s *Store) AckAllAlerts(ctx context.Context, at time.Time) (int, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE alerts SET acked_at = ? WHERE acked_at IS NULL`, at.Unix())
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// PruneAlerts removes alerts that closed before `before`.
func (s *Store) PruneAlerts(ctx context.Context, before time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM alerts WHERE cleared_at IS NOT NULL AND cleared_at < ?`, before.Unix())
	return err
}

// SaveDigest keeps a summary; one for the same kind and period is left as it is. It reports whether it was new.
func (s *Store) SaveDigest(ctx context.Context, d Digest) (bool, error) {
	res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO digests(kind, period_start, period_end, created_at, body) VALUES (?, ?, ?, ?, ?)`,
		d.Kind, d.PeriodStart, d.PeriodEnd, d.Created.Unix(), d.Body)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// HasDigest says whether a summary of the kind and period is kept.
func (s *Store) HasDigest(ctx context.Context, kind, periodStart string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM digests WHERE kind = ? AND period_start = ?`, kind, periodStart).Scan(&n)
	return n > 0, err
}

// Digests lists the kept summaries, newest first.
func (s *Store) Digests(ctx context.Context, limit int) ([]Digest, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind, period_start, period_end, created_at, body FROM digests ORDER BY period_start DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Digest{}
	for rows.Next() {
		var d Digest
		var created int64
		if err := rows.Scan(&d.ID, &d.Kind, &d.PeriodStart, &d.PeriodEnd, &created, &d.Body); err != nil {
			return nil, err
		}
		d.Created = time.Unix(created, 0).UTC()
		out = append(out, d)
	}
	return out, rows.Err()
}
