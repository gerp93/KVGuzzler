package store

import (
	"context"
	"database/sql"
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
	rows, err := s.db.QueryContext(ctx, `SELECT day, miles FROM odometer_readings WHERE meter_id = ? ORDER BY day`, meterID)
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
