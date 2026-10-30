package store

import (
	"context"

	"github.com/gerp93/KVGuzzler/server/internal/gasprice"
)

// The weekly average retail price of regular gasoline in an area, from EIA, kept so the cost of a gasoline car's miles can be set beside an electric
// car's. A price is a decimal string, never a float; a week is the Monday EIA dates it by.
const gasSchema = `
CREATE TABLE IF NOT EXISTS gas_prices (
  area    TEXT NOT NULL,
  week    TEXT NOT NULL,
  dollars TEXT NOT NULL,
  PRIMARY KEY (area, week)
) WITHOUT ROWID;`

// SaveGasPrices keeps an area's weekly prices, replacing a week already kept (EIA revises the latest one).
func (s *Store) SaveGasPrices(ctx context.Context, area string, prices []gasprice.Price) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, p := range prices {
		if _, err := tx.ExecContext(ctx, `INSERT INTO gas_prices(area, week, dollars) VALUES (?, ?, ?)
ON CONFLICT(area, week) DO UPDATE SET dollars = excluded.dollars`, area, p.Week, p.Dollars); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GasPrices lists an area's kept weekly prices, oldest first.
func (s *Store) GasPrices(ctx context.Context, area string) ([]gasprice.Price, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT week, dollars FROM gas_prices WHERE area = ? ORDER BY week`, area)
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
