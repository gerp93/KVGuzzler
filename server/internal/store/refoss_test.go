package store

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/model"
)

func monitor(t *testing.T, s *Store, deviceID string, n int) []string {
	t.Helper()
	ctx := context.Background()
	if err := s.UpsertDevice(ctx, model.Device{ID: deviceID, Kind: "refoss", Host: "h", Name: "Panel", Model: "em16p"}); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for i := 1; i <= n; i++ {
		idx := i
		id := deviceID + ":em" + string(rune('0'+i))
		if err := s.UpsertMeter(ctx, model.Meter{ID: id, DeviceID: deviceID, Index: &idx, Scope: model.ScopeCircuit, Label: "In " + string(rune('0'+i))}); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestAClampsSignAndScopeAreSetAndKept(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	ids := monitor(t, s, "refoss-a", 3)
	yes, house := true, model.TypeHouse

	ch, err := s.SetMeterWiring(ctx, ids[0], WiringChange{Invert: &yes, Type: &house})
	if err != nil || !ch.Invert || ch.Scope != model.ScopeMains {
		t.Fatalf("meter = %+v, %v", ch, err)
	}
	// Registering the meter again (a restart) does not undo what the user set.
	if err := s.UpsertMeter(ctx, model.Meter{ID: ids[0], DeviceID: "refoss-a", Scope: model.ScopeCircuit, Label: "In 1"}); err == nil {
		again, _ := s.Meter(ctx, ids[0])
		if !again.Invert {
			t.Errorf("the sign was lost when the meter was registered again: %+v", again)
		}
		if again.Scope != model.ScopeMains || again.Type != model.TypeHouse {
			t.Errorf("the whole house was lost when the meter was registered again (a restart): scope %q, type %q", again.Scope, again.Type)
		}
	}
	bad := model.TypeDevice
	if _, err := s.SetMeterWiring(ctx, ids[0], WiringChange{Type: &bad}); !errors.Is(err, ErrInvalid) {
		t.Errorf("a device type: %v, want invalid", err)
	}
	if _, err := s.SetMeterWiring(ctx, "nope", WiringChange{Invert: &yes}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown meter: %v", err)
	}
}

func TestOnlyARefossMeterCanBeFlippedOrPaired(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	if err := s.UpsertDevice(ctx, model.Device{ID: "shelly-x", Kind: "shelly_plug", Host: "x", Model: "plug"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertMeter(ctx, model.Meter{ID: "shelly-x:plug", DeviceID: "shelly-x", Scope: model.ScopePlug, Label: "Plug"}); err != nil {
		t.Fatal(err)
	}
	yes := true
	if _, err := s.SetMeterWiring(ctx, "shelly-x:plug", WiringChange{Invert: &yes}); !errors.Is(err, ErrInvalid) {
		t.Errorf("a plug: %v, want invalid", err)
	}
}

func TestPairingMakesOneMeterOfTwoLegsAndUnpairingBringsTheLegBack(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	ids := monitor(t, s, "refoss-a", 4)
	other := monitor(t, s, "refoss-b", 1)
	pair := func(a string, with *string) (model.Meter, error) {
		return s.SetMeterWiring(ctx, a, WiringChange{SetPair: true, PairWith: with})
	}

	ch, err := pair(ids[0], &ids[1])
	if err != nil || ch.PairWith == nil || *ch.PairWith != ids[1] {
		t.Fatalf("pair = %+v, %v", ch, err)
	}
	if leg, _ := s.Meter(ctx, ids[1]); leg.Enabled {
		t.Error("the second leg should be switched off")
	}

	for name, c := range map[string]struct {
		a    string
		with string
		want error
	}{
		"itself":                     {ids[2], ids[2], ErrInvalid},
		"a meter that is not there":  {ids[2], "nope", ErrInvalid},
		"a leg of another monitor":   {ids[2], other[0], ErrInvalid},
		"a leg that is already one":  {ids[2], ids[1], ErrConflict},
		"a meter that has a partner": {ids[2], ids[0], ErrConflict},
		"a leg taking a partner":     {ids[1], ids[3], ErrConflict},
	} {
		w := c.with
		if _, err := pair(c.a, &w); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}

	// Moving the pair to another leg lets the old one go.
	if _, err := pair(ids[0], &ids[2]); err != nil {
		t.Fatal(err)
	}
	if old, _ := s.Meter(ctx, ids[1]); !old.Enabled {
		t.Error("the leg let go of should be back")
	}
	if next, _ := s.Meter(ctx, ids[2]); next.Enabled {
		t.Error("the new leg should be switched off")
	}
	if ch, err = pair(ids[0], nil); err != nil || ch.PairWith != nil {
		t.Fatalf("unpair = %+v, %v", ch, err)
	}
	if back, _ := s.Meter(ctx, ids[2]); !back.Enabled {
		t.Error("an unpaired leg comes back")
	}
}

func TestPairingTwoClampsAddsWhatEachRecordedBeforeAndNeverTwice(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	ids := monitor(t, s, "refoss-p", 3)
	a, b := ids[0], ids[1]
	t0 := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)

	// Apart, each clamp recorded one leg: 3 kW for an hour (60 minutes), the second leg also for a minute the first did not record.
	for i := 0; i < 60; i++ {
		if err := s.AddMinute(ctx, a, t0.Add(time.Duration(i)*time.Minute), 3000, 3100, 60); err != nil {
			t.Fatal(err)
		}
		if err := s.AddMinute(ctx, b, t0.Add(time.Duration(i)*time.Minute), 3000, 3050, 60); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AddMinute(ctx, b, t0.Add(-time.Minute), 3000, 3000, 60); err != nil {
		t.Fatal(err)
	}
	total := func(id string) float64 {
		tot, err := s.Totals(ctx, id, t0.Add(-time.Hour), t0.Add(2*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		return tot.KWh
	}
	if total(a) != 3 || total(b) < 3.04 {
		t.Fatalf("before pairing: %v and %v kWh", total(a), total(b))
	}

	if _, err := s.SetMeterWiring(ctx, a, WiringChange{SetPair: true, PairWith: &b}); err != nil {
		t.Fatal(err)
	}
	want := 6.05 // 3 + 3 for the hour both recorded, and the 0.05 kWh minute only the second recorded
	if got := total(a); math.Abs(got-want) > 1e-9 {
		t.Errorf("the circuit has %v kWh, want %v: both legs, added", got, want)
	}
	tot, _ := s.Totals(ctx, a, t0, t0.Add(time.Hour))
	if tot.PeakWatts != 6150 {
		t.Errorf("peak %v W, want the two legs' highest added (6150)", tot.PeakWatts)
	}
	if got := total(b); got != 0 {
		t.Errorf("the second leg still has %v kWh of its own", got)
	}

	// Unpairing and pairing again must not add the same history a second time.
	if _, err := s.SetMeterWiring(ctx, a, WiringChange{SetPair: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetMeterWiring(ctx, a, WiringChange{SetPair: true, PairWith: &b}); err != nil {
		t.Fatal(err)
	}
	if got := total(a); math.Abs(got-want) > 1e-9 {
		t.Errorf("after pairing again: %v kWh, want it unchanged at %v", got, want)
	}

	// A minute both recorded partly (the moment of pairing) adds up exactly: 20 s of 100 W + 20 s of 50 W on the leg, 40 s of 100 W on the circuit.
	ids2 := monitor(t, s, "refoss-q", 2)
	if err := s.AddMinute(ctx, ids2[0], t0, 100, 100, 40); err != nil {
		t.Fatal(err)
	}
	if err := s.AddMinute(ctx, ids2[1], t0, 50, 50, 20); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetMeterWiring(ctx, ids2[0], WiringChange{SetPair: true, PairWith: &ids2[1]}); err != nil {
		t.Fatal(err)
	}
	tot, _ = s.Totals(ctx, ids2[0], t0, t0.Add(time.Minute))
	if wantKwh := (100.0*40 + 50.0*20) / 3.6e6; math.Abs(tot.KWh-wantKwh) > 1e-12 {
		t.Errorf("a minute both recorded: %v kWh, want %v", tot.KWh, wantKwh)
	}
}
