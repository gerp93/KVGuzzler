package store

import (
	"context"
	"errors"
	"testing"

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
	yes, mains := true, model.ScopeMains

	ch, err := s.SetMeterWiring(ctx, ids[0], WiringChange{Invert: &yes, Scope: &mains})
	if err != nil || !ch.Invert || ch.Scope != model.ScopeMains {
		t.Fatalf("meter = %+v, %v", ch, err)
	}
	// Registering the meter again (a restart) does not undo what the user set.
	if err := s.UpsertMeter(ctx, model.Meter{ID: ids[0], DeviceID: "refoss-a", Scope: model.ScopeCircuit, Label: "In 1"}); err == nil {
		again, _ := s.Meter(ctx, ids[0])
		if !again.Invert {
			t.Errorf("the sign was lost when the meter was registered again: %+v", again)
		}
	}
	bad := model.Scope("plug")
	if _, err := s.SetMeterWiring(ctx, ids[0], WiringChange{Scope: &bad}); !errors.Is(err, ErrInvalid) {
		t.Errorf("a plug scope: %v, want invalid", err)
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
