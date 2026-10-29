package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/model"
)

// A charge is a stretch in which the meter that charges an electric vehicle drew power: it starts when the meter goes from resting to drawing and
// ends when it has rested for a while. The tracker (internal/evcharge) opens and closes them as it watches the meter; the energy and cost of a charge
// are not kept here, they are read from the meter's minutes when asked, so they follow a corrected price. What is kept is what only the car can say:
// its battery's charge when the charge began and when it ended, and when the car reported each. Times are Unix seconds.
const evChargeSchema = `
CREATE TABLE IF NOT EXISTS ev_charges (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  meter_id     TEXT NOT NULL,
  started_at   INTEGER NOT NULL,
  last_above   INTEGER NOT NULL,
  ended_at     INTEGER,
  start_pct    REAL,
  start_pct_at INTEGER,
  end_pct      REAL,
  end_pct_at   INTEGER
);
CREATE INDEX IF NOT EXISTS ev_charges_by_meter ON ev_charges(meter_id, started_at);`

const evChargeColumns = `id, meter_id, started_at, last_above, ended_at, start_pct, start_pct_at, end_pct, end_pct_at`

func scanEvCharge(row interface{ Scan(...any) error }) (model.EvCharge, error) {
	var c model.EvCharge
	var started, above int64
	var ended, startAt, endAt sql.NullInt64
	var startPct, endPct sql.NullFloat64
	if err := row.Scan(&c.ID, &c.MeterID, &started, &above, &ended, &startPct, &startAt, &endPct, &endAt); err != nil {
		return model.EvCharge{}, err
	}
	c.StartedAt, c.LastAbove = time.Unix(started, 0).UTC(), time.Unix(above, 0).UTC()
	timeOf := func(n sql.NullInt64) *time.Time {
		if !n.Valid {
			return nil
		}
		t := time.Unix(n.Int64, 0).UTC()
		return &t
	}
	c.EndedAt, c.StartPctAt, c.EndPctAt = timeOf(ended), timeOf(startAt), timeOf(endAt)
	if startPct.Valid {
		c.StartPct = &startPct.Float64
	}
	if endPct.Valid {
		c.EndPct = &endPct.Float64
	}
	return c, nil
}

// EVMeterIDs lists the meters marked as charging an electric vehicle.
func (s *Store) EVMeterIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM meters WHERE ev = 1 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// OpenEvCharge is the charge a meter is in the middle of, or nil when there is none.
func (s *Store) OpenEvCharge(ctx context.Context, meterID string) (*model.EvCharge, error) {
	c, err := scanEvCharge(s.db.QueryRowContext(ctx, `SELECT `+evChargeColumns+` FROM ev_charges WHERE meter_id = ? AND ended_at IS NULL ORDER BY started_at DESC LIMIT 1`, meterID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// LatestClosedEvCharge is the newest charge of a meter that has ended, or nil.
func (s *Store) LatestClosedEvCharge(ctx context.Context, meterID string) (*model.EvCharge, error) {
	c, err := scanEvCharge(s.db.QueryRowContext(ctx, `SELECT `+evChargeColumns+` FROM ev_charges WHERE meter_id = ? AND ended_at IS NOT NULL ORDER BY started_at DESC LIMIT 1`, meterID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// StartEvCharge opens a charge that began at the given time.
func (s *Store) StartEvCharge(ctx context.Context, meterID string, at time.Time) (model.EvCharge, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO ev_charges(meter_id, started_at, last_above) VALUES (?, ?, ?)`, meterID, at.Unix(), at.Unix())
	if err != nil {
		return model.EvCharge{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return model.EvCharge{}, err
	}
	return scanEvCharge(s.db.QueryRowContext(ctx, `SELECT `+evChargeColumns+` FROM ev_charges WHERE id = ?`, id))
}

// TouchEvCharge notes that the meter was still drawing power at the given time.
func (s *Store) TouchEvCharge(ctx context.Context, id int64, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE ev_charges SET last_above = ? WHERE id = ? AND ended_at IS NULL AND last_above < ?`, at.Unix(), id, at.Unix())
	return err
}

// EndEvCharge closes a charge at the given time.
func (s *Store) EndEvCharge(ctx context.Context, id int64, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE ev_charges SET ended_at = ? WHERE id = ? AND ended_at IS NULL`, at.Unix(), id)
	return err
}

// SetEvChargeStart keeps the battery's charge the car reported when the charge began. at is when the car reported it; the zero time means it did not say.
func (s *Store) SetEvChargeStart(ctx context.Context, id int64, pct float64, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE ev_charges SET start_pct = ?, start_pct_at = ? WHERE id = ?`, pct, unixOrNull(at), id)
	return err
}

// SetEvChargeEnd keeps the battery's charge the car reported after the charge ended, replacing an earlier (older) report of the same charge.
func (s *Store) SetEvChargeEnd(ctx context.Context, id int64, pct float64, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE ev_charges SET end_pct = ?, end_pct_at = ? WHERE id = ?`, pct, unixOrNull(at), id)
	return err
}

func unixOrNull(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.Unix()
}

// EvCharges lists a meter's charges, newest first, up to limit.
func (s *Store) EvCharges(ctx context.Context, meterID string, limit int) ([]model.EvCharge, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+evChargeColumns+` FROM ev_charges WHERE meter_id = ? ORDER BY started_at DESC, id DESC LIMIT ?`, meterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.EvCharge{}
	for rows.Next() {
		c, err := scanEvCharge(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeleteEvCharge removes one of a meter's charges (a false one, say); ErrNotFound when there is none.
func (s *Store) DeleteEvCharge(ctx context.Context, meterID string, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM ev_charges WHERE id = ? AND meter_id = ?`, id, meterID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
