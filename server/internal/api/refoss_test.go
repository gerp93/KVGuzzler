package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gerp93/KVGuzzler/server/internal/model"
)

func TestARefossClampsSignPairAndScopeAreSetThroughTheMeterPatch(t *testing.T) {
	srv, st := computerServer(t)
	ctx := t.Context()
	if err := st.UpsertDevice(ctx, model.Device{ID: "refoss-h", Kind: "refoss", Host: "h", Name: "Garage", Model: "em16p"}); err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"refoss-h:em2", "refoss-h:em8", "refoss-h:em3"} {
		idx := i
		if err := st.UpsertMeter(ctx, model.Meter{ID: id, DeviceID: "refoss-h", Index: &idx, Scope: model.ScopeCircuit, Label: id}); err != nil {
			t.Fatal(err)
		}
	}
	patch := func(id, body string) (*http.Response, string) {
		return send(t, http.MethodPatch, srv.URL+"/api/v1/meters/"+id, body)
	}

	r, body := patch("refoss-h:em2", `{"invert":true}`)
	if ch := decode[model.Meter](t, body); r.StatusCode != 200 || !ch.Invert {
		t.Fatalf("invert: %d %s", r.StatusCode, body)
	}
	r, body = patch("refoss-h:em2", `{"pair_with":"refoss-h:em8"}`)
	ch := decode[model.Meter](t, body)
	if r.StatusCode != 200 || ch.PairWith == nil || *ch.PairWith != "refoss-h:em8" || !ch.Invert {
		t.Fatalf("pair: %d %s", r.StatusCode, body)
	}
	var chs struct{ Meters []model.Meter }
	get(t, srv.URL+"/api/v1/meters", &chs)
	for _, c := range chs.Meters {
		if c.ID == "refoss-h:em8" && c.Enabled {
			t.Error("the second leg should be switched off")
		}
	}
	if r, _ := patch("refoss-h:em3", `{"pair_with":"refoss-h:em8"}`); r.StatusCode != http.StatusConflict {
		t.Errorf("a leg already used: %d, want 409", r.StatusCode)
	}
	if r, _ := patch("refoss-h:em3", `{"pair_with":"nope"}`); r.StatusCode != http.StatusBadRequest {
		t.Errorf("no such leg: %d, want 400", r.StatusCode)
	}
	if r, body := patch("refoss-h:em3", `{"type":"house"}`); r.StatusCode != 200 || decode[model.Meter](t, body).Scope != model.ScopeMains || decode[model.Meter](t, body).Type != model.TypeHouse {
		t.Errorf("scope: %d %s", r.StatusCode, body)
	}
	if r, _ := patch("refoss-h:em3", `{"type":"device"}`); r.StatusCode != http.StatusBadRequest {
		t.Errorf("a device type: %d, want 400", r.StatusCode)
	}
	r, body = patch("refoss-h:em2", `{"pair_with":null}`)
	if ch := decode[model.Meter](t, body); r.StatusCode != 200 || ch.PairWith != nil {
		t.Errorf("unpair: %d %s", r.StatusCode, body)
	}

	// A circuit can be part of a sub-panel's feed, and the feed is not counted twice against the house.
	r, body = patch("refoss-h:em8", `{"parent_id":"refoss-h:em2"}`)
	if ch := decode[model.Meter](t, body); r.StatusCode != 200 || ch.ParentID == nil || *ch.ParentID != "refoss-h:em2" {
		t.Fatalf("parent: %d %s", r.StatusCode, body)
	}
	if r, _ := patch("refoss-h:em2", `{"parent_id":"refoss-h:em8"}`); r.StatusCode != http.StatusConflict {
		t.Errorf("a loop: %d, want 409", r.StatusCode)
	}
	if r, _ := patch("refoss-h:em8", `{"parent_id":"nope"}`); r.StatusCode != http.StatusBadRequest {
		t.Errorf("no such parent: %d, want 400", r.StatusCode)
	}
	if r, body := patch("refoss-h:em8", `{"parent_id":null}`); r.StatusCode != 200 || decode[model.Meter](t, body).ParentID != nil {
		t.Errorf("out of the panel: %d %s", r.StatusCode, body)
	}

	// Any meter can be a favorite, and the mark is kept on the server.
	if r, body := patch("refoss-h:em2", `{"favorite":true}`); r.StatusCode != 200 || !decode[model.Meter](t, body).Favorite {
		t.Errorf("favorite: %d %s", r.StatusCode, body)
	}
	var listed struct{ Meters []model.Meter }
	get(t, srv.URL+"/api/v1/meters", &listed)
	for _, m := range listed.Meters {
		if m.ID == "refoss-h:em2" && !m.Favorite {
			t.Error("the favorite mark was not kept")
		}
	}

	// A plug is not a monitor: it has no clamp to flip.
	if err := st.UpsertDevice(ctx, model.Device{ID: "shelly-x", Kind: "shelly_plug", Host: "x", Model: "plug"}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertMeter(ctx, model.Meter{ID: "shelly-x:plug", DeviceID: "shelly-x", Scope: model.ScopePlug, Label: "Plug"}); err != nil {
		t.Fatal(err)
	}
	if r, body := patch("shelly-x:plug", `{"invert":true}`); r.StatusCode != http.StatusBadRequest || !strings.Contains(body, "Refoss") {
		t.Errorf("a plug: %d %s", r.StatusCode, body)
	}
}
