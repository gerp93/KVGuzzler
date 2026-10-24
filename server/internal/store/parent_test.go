package store

import (
	"errors"
	"testing"

	"github.com/gerp93/KVGuzzler/server/internal/model"
)

func panelFixture(t *testing.T) *Store {
	t.Helper()
	st := open(t)
	ctx := t.Context()
	if err := st.UpsertDevice(ctx, model.Device{ID: "refoss-h", Kind: "refoss", Host: "h", Model: "em16p"}); err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"feed", "ev", "rest", "leg"} {
		idx := i + 1
		if err := st.UpsertMeter(ctx, model.Meter{ID: id, DeviceID: "refoss-h", Index: &idx, Scope: model.ScopeCircuit, Label: id}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.UpsertMeter(ctx, model.Meter{ID: "mains", DeviceID: "refoss-h", Scope: model.ScopeMains, Label: "mains"}); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestACircuitCanBePartOfASubPanelsFeedAndTheChainHasNoLoops(t *testing.T) {
	st := panelFixture(t)
	ctx := t.Context()
	feed, ev := "feed", "ev"

	c, err := st.SetMeterParent(ctx, "ev", &feed)
	if err != nil || c.ParentID == nil || *c.ParentID != "feed" {
		t.Fatalf("ev under feed: %+v %v", c, err)
	}
	// Deeper: the rest of the garage under the feed, and a branch under that.
	if _, err := st.SetMeterParent(ctx, "rest", &feed); err != nil {
		t.Fatal(err)
	}
	rest := "rest"
	if _, err := st.SetMeterParent(ctx, "leg", &rest); err != nil {
		t.Fatal(err)
	}
	// A loop: the feed cannot go under its own child, grandchild, or itself.
	if _, err := st.SetMeterParent(ctx, "feed", &ev); !errors.Is(err, ErrConflict) {
		t.Errorf("a loop: %v, want a conflict", err)
	}
	leg := "leg"
	if _, err := st.SetMeterParent(ctx, "feed", &leg); !errors.Is(err, ErrConflict) {
		t.Errorf("a longer loop: %v, want a conflict", err)
	}
	if _, err := st.SetMeterParent(ctx, "feed", &feed); !errors.Is(err, ErrInvalid) {
		t.Errorf("itself: %v, want invalid", err)
	}
	// Only circuits take part, and the parent must exist.
	mains, nope := "mains", "nope"
	if _, err := st.SetMeterParent(ctx, "ev", &mains); !errors.Is(err, ErrInvalid) {
		t.Errorf("under the mains: %v, want invalid", err)
	}
	if _, err := st.SetMeterParent(ctx, "mains", &feed); !errors.Is(err, ErrInvalid) {
		t.Errorf("the mains under a circuit: %v, want invalid", err)
	}
	if _, err := st.SetMeterParent(ctx, "ev", &nope); !errors.Is(err, ErrInvalid) {
		t.Errorf("no such parent: %v, want invalid", err)
	}
	// A plug, or the PC's wall plug, can be put on a circuit; a plug cannot hold anything.
	if err := st.UpsertMeter(ctx, model.Meter{ID: "pcwall", DeviceID: "refoss-h", Scope: model.ScopePC, Label: "PC"}); err != nil {
		t.Fatal(err)
	}
	if c, err := st.SetMeterParent(ctx, "pcwall", &rest); err != nil || c.ParentID == nil {
		t.Errorf("the PC's plug on a circuit: %+v %v", c, err)
	}
	if err := st.UpsertMeter(ctx, model.Meter{ID: "fridge", DeviceID: "refoss-h", Scope: model.ScopePlug, Label: "fridge"}); err != nil {
		t.Fatal(err)
	}
	if c, err := st.SetMeterParent(ctx, "fridge", &rest); err != nil || c.ParentID == nil || *c.ParentID != "rest" {
		t.Errorf("a plug on a circuit: %+v %v", c, err)
	}
	fridge := "fridge"
	if _, err := st.SetMeterParent(ctx, "ev", &fridge); !errors.Is(err, ErrInvalid) {
		t.Errorf("a circuit inside a plug: %v, want invalid", err)
	}
	// Taking it out puts it back at the top level.
	if c, err := st.SetMeterParent(ctx, "ev", nil); err != nil || c.ParentID != nil {
		t.Errorf("taken out: %+v %v", c, err)
	}
}

func TestAMeterThatStopsBeingACircuitLeavesTheTreeAndItsChildrenGoToTheTop(t *testing.T) {
	st := panelFixture(t)
	ctx := t.Context()
	feed := "feed"
	for _, id := range []string{"ev", "rest"} {
		if _, err := st.SetMeterParent(ctx, id, &feed); err != nil {
			t.Fatal(err)
		}
	}
	house := model.TypeHouse
	if _, err := st.SetMeterWiring(ctx, "feed", WiringChange{Type: &house}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ev", "rest"} {
		if c, _ := st.Meter(ctx, id); c.ParentID != nil {
			t.Errorf("%s is still under a feed that is the mains now: %v", id, *c.ParentID)
		}
	}

	// A second leg is not a circuit of its own: it cannot be a parent or a child, and pairing it takes it out of the tree.
	ev := "ev"
	if _, err := st.SetMeterParent(ctx, "leg", &ev); err != nil {
		t.Fatal(err)
	}
	leg := "leg"
	if _, err := st.SetMeterWiring(ctx, "ev", WiringChange{SetPair: true, PairWith: &leg}); err != nil {
		t.Fatal(err)
	}
	if c, _ := st.Meter(ctx, "leg"); c.ParentID != nil {
		t.Errorf("a second leg kept its place: %v", *c.ParentID)
	}
	if _, err := st.SetMeterParent(ctx, "rest", &leg); !errors.Is(err, ErrConflict) {
		t.Errorf("under a second leg: %v, want a conflict", err)
	}
}

func TestAMetersTypeFollowsItsScopeAndWhatIsInsideItUntilTheUserSaysOtherwise(t *testing.T) {
	st := panelFixture(t)
	ctx := t.Context()
	typeOf := func(id string) model.MeterType {
		t.Helper()
		m, err := st.Meter(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return m.Type
	}
	if err := st.UpsertMeter(ctx, model.Meter{ID: "fridge", DeviceID: "refoss-h", Scope: model.ScopePlug, Label: "fridge"}); err != nil {
		t.Fatal(err)
	}
	if typeOf("mains") != model.TypeHouse || typeOf("fridge") != model.TypeDevice || typeOf("feed") != model.TypeCircuit {
		t.Errorf("house %s, plug %s, feed with nothing inside %s", typeOf("mains"), typeOf("fridge"), typeOf("feed"))
	}
	feed := "feed"
	if _, err := st.SetMeterParent(ctx, "ev", &feed); err != nil {
		t.Fatal(err)
	}
	if typeOf("feed") != model.TypeSubPanel {
		t.Errorf("a circuit with a circuit inside it is %s, want sub_panel", typeOf("feed"))
	}
	// The user's own choice wins, and the same list read together agrees.
	circuit := model.TypeCircuit
	if _, err := st.SetMeterWiring(ctx, "feed", WiringChange{Type: &circuit}); err != nil {
		t.Fatal(err)
	}
	all, _ := st.Meters(ctx)
	for _, m := range all {
		if m.ID == "feed" && m.Type != model.TypeCircuit {
			t.Errorf("a saved circuit is %s", m.Type)
		}
	}
	asHouse := model.TypeHouse
	if m, err := st.SetMeterWiring(ctx, "rest", WiringChange{Type: &asHouse}); err != nil || m.Scope != model.ScopeMains || m.Type != model.TypeHouse {
		t.Errorf("made the house: %+v %v", m, err)
	}
	panel := model.TypeSubPanel
	if m, err := st.SetMeterWiring(ctx, "rest", WiringChange{Type: &panel}); err != nil || m.Scope != model.ScopeCircuit || m.Type != model.TypeSubPanel {
		t.Errorf("made a sub-panel again: %+v %v", m, err)
	}
}
