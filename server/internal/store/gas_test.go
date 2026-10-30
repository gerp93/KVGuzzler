package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gerp93/KVGuzzler/server/internal/gasprice"
)

func TestFuelPricesAreKeptApartByFuelAndAreaAndAWeekIsRevised(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	if err := s.SaveFuelPrices(ctx, gasprice.Gasoline, "SMN", []gasprice.Price{{Week: "2026-10-05", Dollars: "4.00"}, {Week: "2026-10-12", Dollars: "4.50"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveFuelPrices(ctx, gasprice.Diesel, "SMN", []gasprice.Price{{Week: "2026-10-12", Dollars: "5.20"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveFuelPrices(ctx, gasprice.Gasoline, "NUS", []gasprice.Price{{Week: "2026-10-12", Dollars: "3.60"}}); err != nil {
		t.Fatal(err)
	}
	// EIA revises the newest week: the same week saved again replaces it.
	if err := s.SaveFuelPrices(ctx, gasprice.Gasoline, "SMN", []gasprice.Price{{Week: "2026-10-12", Dollars: "4.55"}}); err != nil {
		t.Fatal(err)
	}
	gas, _ := s.FuelPrices(ctx, gasprice.Gasoline, "SMN")
	diesel, _ := s.FuelPrices(ctx, gasprice.Diesel, "SMN")
	us, _ := s.FuelPrices(ctx, gasprice.Gasoline, "NUS")
	if len(gas) != 2 || gas[0].Dollars != "4.00" || gas[1].Dollars != "4.55" {
		t.Errorf("gasoline in Minnesota = %v", gas)
	}
	if len(diesel) != 1 || diesel[0].Dollars != "5.20" {
		t.Errorf("diesel in Minnesota = %v", diesel)
	}
	if len(us) != 1 || us[0].Dollars != "3.60" {
		t.Errorf("gasoline in the US = %v", us)
	}
}

func TestGasolinePricesKeptBeforeThereWasDieselAreStillThere(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "house.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// What an older server kept: gasoline in the table that held only gasoline.
	if _, err := s.db.ExecContext(ctx, `INSERT INTO gas_prices(area, week, dollars) VALUES ('SMN', '2026-10-05', '4.00'), ('SMN', '2026-10-12', '4.50')`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	gas, err := s.FuelPrices(ctx, gasprice.Gasoline, "SMN")
	if err != nil || len(gas) != 2 || gas[1].Dollars != "4.50" {
		t.Fatalf("gasoline prices after reopening = %v, %v", gas, err)
	}
	if d, _ := s.FuelPrices(ctx, gasprice.Diesel, "SMN"); len(d) != 0 {
		t.Errorf("diesel prices appeared from nowhere: %v", d)
	}
}
