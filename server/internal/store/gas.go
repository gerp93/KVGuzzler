package store

import (
	"context"

	"github.com/gerp93/KVGuzzler/server/internal/gasprice"
)

// The weekly average retail price of a fuel (regular gasoline, diesel) in an area, from EIA, kept so the cost of a gas or diesel car's miles can be set beside an
// electric car's. A price is a decimal string, never a float; a week is the Monday EIA dates it by. gas_prices is what was kept before there was more than one
// fuel (gasoline only): it is copied into fuel_prices and no longer written.
const gasSchema = `
CREATE TABLE IF NOT EXISTS gas_prices (
  area    TEXT NOT NULL,
  week    TEXT NOT NULL,
  dollars TEXT NOT NULL,
  PRIMARY KEY (area, week)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS fuel_prices (
  product TEXT NOT NULL,
  area    TEXT NOT NULL,
  week    TEXT NOT NULL,
  dollars TEXT NOT NULL,
  PRIMARY KEY (product, area, week)
) WITHOUT ROWID;
INSERT OR IGNORE INTO fuel_prices(product, area, week, dollars) SELECT 'EPMR', area, week, dollars FROM gas_prices;`

// SaveFuelPrices keeps an area's weekly prices of a fuel, replacing a week already kept (EIA revises the latest one).
func (s *Store) SaveFuelPrices(ctx context.Context, fuel gasprice.Product, area string, prices []gasprice.Price) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, p := range prices {
		if _, err := tx.ExecContext(ctx, `INSERT INTO fuel_prices(product, area, week, dollars) VALUES (?, ?, ?, ?)
ON CONFLICT(product, area, week) DO UPDATE SET dollars = excluded.dollars`, string(fuel), area, p.Week, p.Dollars); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// FuelPrices lists an area's kept weekly prices of a fuel, oldest first.
func (s *Store) FuelPrices(ctx context.Context, fuel gasprice.Product, area string) ([]gasprice.Price, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT week, dollars FROM fuel_prices WHERE product = ? AND area = ? ORDER BY week`, string(fuel), area)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []gasprice.Price{}
	for rows.Next() {
		var p gasprice.Price
		if err := rows.Scan(&p.Week, &p.Dollars); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
