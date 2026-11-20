package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/model"
)

// An electric vehicle is a meter the user says charges one (a circuit or a plug, never the mains): the meter keeps being measured exactly as
// before, and the user adds the car's odometer now and then. Miles driven between two readings and the energy the meter used over the same
// stretch give the cost per mile. The odometer is the user's own figure; nothing is read from the car.
const evSchema = `
CREATE TABLE IF NOT EXISTS odometer_readings (
  meter_id TEXT NOT NULL,
  day      TEXT NOT NULL,
  miles    REAL NOT NULL,
  PRIMARY KEY (meter_id, day)
) WITHOUT ROWID;

-- What the car said its traction battery held, each time the Ford feed looked (at is when the car reported it, Unix seconds). With the charges' own
-- start and end reports it says how much of the energy charged is still in the battery rather than driven.
CREATE TABLE IF NOT EXISTS ev_battery_reads (
  meter_id TEXT NOT NULL,
  at       INTEGER NOT NULL,
  percent  REAL NOT NULL,
  PRIMARY KEY (meter_id, at)
) WITHOUT ROWID;`

// MaxOdometerMiles is the largest odometer reading accepted: a typo, not a car.
const MaxOdometerMiles = 10_000_000

// SetMeterEV marks a meter as one that charges an electric vehicle (or takes the mark off) and returns it. Taking it off keeps the odometer
// readings, so marking it again brings them back.
func (s *Store) SetMeterEV(ctx context.Context, id string, ev bool) (model.Meter, error) {
	m, err := s.Meter(ctx, id)
	if err != nil {
		return model.Meter{}, err
	}
	if ev && m.Scope != model.ScopeCircuit && m.Scope != model.ScopePlug {
		return model.Meter{}, invalid("Only a circuit or a plug can charge an electric vehicle.")
	}
	v := 0
	if ev {
		v = 1
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE meters SET ev = ? WHERE id = ?`, v, id); err != nil {
		return model.Meter{}, err
	}
	return s.Meter(ctx, id)
}

// Odometer lists a meter's odometer readings, oldest first.
func (s *Store) Odometer(ctx context.Context, meterID string) ([]model.OdometerReading, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT day, miles FROM odometer_readings WHERE meter_id = ? AND day >= ? ORDER BY day`, meterID, s.floorDay())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.OdometerReading{}
	for rows.Next() {
		var r model.OdometerReading
		if err := rows.Scan(&r.Day, &r.Miles); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetOdometer saves the odometer reading of a local calendar day (replacing that day's). An odometer only goes up: a reading below the one before
// it, or above the one after it, is refused, because it would give a stretch of negative miles.
func (s *Store) SetOdometer(ctx context.Context, meterID, day string, miles float64) error {
	m, err := s.Meter(ctx, meterID)
	if err != nil {
		return err
	}
	if !m.EV {
		return invalid("This meter is not marked as charging an electric vehicle.")
	}
	if _, err := time.Parse("2006-01-02", day); err != nil {
		return invalid("A reading's day is a date like 2026-10-26.")
	}
	if !(miles >= 0 && miles <= MaxOdometerMiles) {
		return invalid("An odometer reading is between 0 and %d miles.", MaxOdometerMiles)
	}
	var before, after sql.NullFloat64
	if err := s.db.QueryRowContext(ctx, `SELECT miles FROM odometer_readings WHERE meter_id = ? AND day < ? ORDER BY day DESC LIMIT 1`, meterID, day).Scan(&before); err != nil && err != sql.ErrNoRows {
		return err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT miles FROM odometer_readings WHERE meter_id = ? AND day > ? ORDER BY day LIMIT 1`, meterID, day).Scan(&after); err != nil && err != sql.ErrNoRows {
		return err
	}
	if before.Valid && miles < before.Float64 {
		return invalid("The reading before this one is %.0f miles: an odometer only goes up.", before.Float64)
	}
	if after.Valid && miles > after.Float64 {
		return invalid("The reading after this one is %.0f miles: an odometer only goes up.", after.Float64)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO odometer_readings(meter_id, day, miles) VALUES (?, ?, ?)
ON CONFLICT(meter_id, day) DO UPDATE SET miles = excluded.miles`, meterID, day, miles)
	return err
}

// PendingOdometer is the latest odometer the Ford feed looked at that is not yet a reading: the last look of a local day, kept until the day is over,
// because only then is it the odometer at the start of the next day (which is what a reading's day means). False when there is none.
func (s *Store) PendingOdometer(ctx context.Context, meterID string) (day string, miles float64, ok bool, err error) {
	v, ok, err := s.Meta(ctx, "odometer_pending:"+meterID)
	if err != nil || !ok {
		return "", 0, false, err
	}
	d, m, found := strings.Cut(v, " ")
	if !found {
		return "", 0, false, nil
	}
	miles, perr := strconv.ParseFloat(m, 64)
	if perr != nil {
		return "", 0, false, nil
	}
	return d, miles, true, nil
}

// SetPendingOdometer keeps the odometer the feed has just looked at, for the local day it was seen on (see PendingOdometer).
func (s *Store) SetPendingOdometer(ctx context.Context, meterID, day string, miles float64) error {
	return s.SetMeta(ctx, "odometer_pending:"+meterID, day+" "+strconv.FormatFloat(miles, 'f', -1, 64))
}

// ShiftOdometerDays moves a meter's readings from `since` on one day later, for readings the Ford feed saved when a day's reading was still the last look
// of that day (the odometer at the day's end, not its start). A reading dated `today` or later is a look of a day that is not over: it is removed and
// returned as the pending look instead. It returns how many readings moved.
func (s *Store) ShiftOdometerDays(ctx context.Context, meterID, since, today string) (moved int, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT day, miles FROM odometer_readings WHERE meter_id = ? AND day >= ? ORDER BY day DESC`, meterID, since)
	if err != nil {
		return 0, err
	}
	type row struct {
		day   string
		miles float64
	}
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.day, &r.miles); err != nil {
			rows.Close()
			return 0, err
		}
		all = append(all, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	var intraday *row
	for _, r := range all { // newest first, so a day's new date is never one that still holds a reading
		if r.day >= today {
			if _, err := tx.ExecContext(ctx, `DELETE FROM odometer_readings WHERE meter_id = ? AND day = ?`, meterID, r.day); err != nil {
				return 0, err
			}
			if r.day == today {
				c := r
				intraday = &c
			}
			continue
		}
		t, err := time.Parse("2006-01-02", r.day)
		if err != nil {
			continue
		}
		next := t.AddDate(0, 0, 1).Format("2006-01-02")
		if _, err := tx.ExecContext(ctx, `UPDATE odometer_readings SET day = ? WHERE meter_id = ? AND day = ?`, next, meterID, r.day); err != nil {
			return 0, err
		}
		moved++
	}
	if intraday != nil {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO meta(key, value) VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value`, "odometer_pending:"+meterID, intraday.day+" "+strconv.FormatFloat(intraday.miles, 'f', -1, 64)); err != nil {
			return 0, err
		}
	}
	return moved, tx.Commit()
}

// DeleteOdometer removes the reading of a day; ErrNotFound when there is none.
func (s *Store) DeleteOdometer(ctx context.Context, meterID, day string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM odometer_readings WHERE meter_id = ? AND day = ?`, meterID, day)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SaveEvBatteryRead keeps one report of the traction battery's charge (0 to 100) the car made at `at`. A report already kept for that moment stays.
func (s *Store) SaveEvBatteryRead(ctx context.Context, meterID string, at time.Time, percent float64) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO ev_battery_reads(meter_id, at, percent) VALUES (?, ?, ?)`, meterID, at.Unix(), percent)
	return err
}

// EvBatteryReads lists the battery reports kept for a meter, oldest first.
func (s *Store) EvBatteryReads(ctx context.Context, meterID string) ([]model.EvBatteryRead, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT at, percent FROM ev_battery_reads WHERE meter_id = ? ORDER BY at`, meterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.EvBatteryRead{}
	for rows.Next() {
		var at int64
		var r model.EvBatteryRead
		if err := rows.Scan(&at, &r.Percent); err != nil {
			return nil, err
		}
		r.At = time.Unix(at, 0).UTC()
		out = append(out, r)
	}
	return out, rows.Err()
}
