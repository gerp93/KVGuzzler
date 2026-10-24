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
		if err := st.UpsertChannel(ctx, model.Channel{ID: id, DeviceID: "refoss-h", Index: &idx, Scope: model.ScopeCircuit, Label: id}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.UpsertChannel(ctx, model.Channel{ID: "mains", DeviceID: "refoss-h", Scope: model.ScopeMains, Label: "mains"}); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestACircuitCanBePartOfASubPanelsFeedAndTheChainHasNoLoops(t *testing.T) {
	st := panelFixture(t)
	ctx := t.Context()
	feed, ev := "feed", "ev"

	c, err := st.SetChannelParent(ctx, "ev", &feed)
	if err != nil || c.ParentID == nil || *c.ParentID != "feed" {
		t.Fatalf("ev under feed: %+v %v", c, err)
	}
	// Deeper: the rest of the garage under the feed, and a branch under that.
	if _, err := st.SetChannelParent(ctx, "rest", &feed); err != nil {
		t.Fatal(err)
	}
	rest := "rest"
	if _, err := st.SetChannelParent(ctx, "leg", &rest); err != nil {
		t.Fatal(err)
	}
	// A loop: the feed cannot go under its own child, grandchild, or itself.
	if _, err := st.SetChannelParent(ctx, "feed", &ev); !errors.Is(err, ErrConflict) {
		t.Errorf("a loop: %v, want a conflict", err)
	}
	leg := "leg"
	if _, err := st.SetChannelParent(ctx, "feed", &leg); !errors.Is(err, ErrConflict) {
		t.Errorf("a longer loop: %v, want a conflict", err)
	}
	if _, err := st.SetChannelParent(ctx, "feed", &feed); !errors.Is(err, ErrInvalid) {
		t.Errorf("itself: %v, want invalid", err)
	}
	// Only circuits take part, and the parent must exist.
	mains, nope := "mains", "nope"
	if _, err := st.SetChannelParent(ctx, "ev", &mains); !errors.Is(err, ErrInvalid) {
		t.Errorf("under the mains: %v, want invalid", err)
	}
	if _, err := st.SetChannelParent(ctx, "mains", &feed); !errors.Is(err, ErrInvalid) {
		t.Errorf("the mains under a circuit: %v, want invalid", err)
	}
	if _, err := st.SetChannelParent(ctx, "ev", &nope); !errors.Is(err, ErrInvalid) {
		t.Errorf("no such parent: %v, want invalid", err)
	}
	// A plug can be put on a circuit, but a plug cannot hold anything and the PC's wall plug is no circuit.
	if err := st.UpsertChannel(ctx, model.Channel{ID: "fridge", DeviceID: "refoss-h", Scope: model.ScopePlug, Label: "fridge"}); err != nil {
		t.Fatal(err)
	}
	if c, err := st.SetChannelParent(ctx, "fridge", &rest); err != nil || c.ParentID == nil || *c.ParentID != "rest" {
		t.Errorf("a plug on a circuit: %+v %v", c, err)
	}
	fridge := "fridge"
	if _, err := st.SetChannelParent(ctx, "ev", &fridge); !errors.Is(err, ErrInvalid) {
		t.Errorf("a circuit inside a plug: %v, want invalid", err)
	}
	// Taking it out puts it back at the top level.
	if c, err := st.SetChannelParent(ctx, "ev", nil); err != nil || c.ParentID != nil {
		t.Errorf("taken out: %+v %v", c, err)
	}
}

func TestAChannelThatStopsBeingACircuitLeavesTheTreeAndItsChildrenGoToTheTop(t *testing.T) {
	st := panelFixture(t)
	ctx := t.Context()
	feed := "feed"
	for _, id := range []string{"ev", "rest"} {
		if _, err := st.SetChannelParent(ctx, id, &feed); err != nil {
			t.Fatal(err)
		}
	}
	mains := model.ScopeMains
	if _, err := st.SetChannelWiring(ctx, "feed", WiringChange{Scope: &mains}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ev", "rest"} {
		if c, _ := st.Channel(ctx, id); c.ParentID != nil {
			t.Errorf("%s is still under a feed that is the mains now: %v", id, *c.ParentID)
		}
	}

	// A second leg is not a circuit of its own: it cannot be a parent or a child, and pairing it takes it out of the tree.
	ev := "ev"
	if _, err := st.SetChannelParent(ctx, "leg", &ev); err != nil {
		t.Fatal(err)
	}
	leg := "leg"
	if _, err := st.SetChannelWiring(ctx, "ev", WiringChange{SetPair: true, PairWith: &leg}); err != nil {
		t.Fatal(err)
	}
	if c, _ := st.Channel(ctx, "leg"); c.ParentID != nil {
		t.Errorf("a second leg kept its place: %v", *c.ParentID)
	}
	if _, err := st.SetChannelParent(ctx, "rest", &leg); !errors.Is(err, ErrConflict) {
		t.Errorf("under a second leg: %v, want a conflict", err)
	}
}
