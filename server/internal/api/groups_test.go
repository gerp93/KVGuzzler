package api

import (
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/model"
)

func TestGroupsCombineSeveralPlugsIntoOneApplianceAndFollowTheRules(t *testing.T) {
	srv, st := computerServer(t)
	ctx := t.Context()
	at := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

	for _, ch := range []model.Meter{
		{ID: "strip-1", DeviceID: "d1", Scope: model.ScopePlug, Label: "TV strip"},
		{ID: "strip-2", DeviceID: "d2", Scope: model.ScopePlug, Label: "Audio strip"},
		{ID: "strip-3", DeviceID: "d3", Scope: model.ScopePlug, Label: "Hidden strip"},
		{ID: "pc-wall", DeviceID: "d4", Scope: model.ScopePC, Label: "PC"},
		{ID: "desk-plug", DeviceID: "d5", Scope: model.ScopePlug, Label: "Monitor"},
	} {
		if err := st.UpsertMeter(ctx, ch); err != nil {
			t.Fatal(err)
		}
	}
	// One minute each: 200 W, 100 W, 40 W (hidden), and the monitor.
	for id, w := range map[string]float64{"strip-1": 200, "strip-2": 100, "strip-3": 40, "desk-plug": 50} {
		if err := st.AddMinute(ctx, id, at, w, w, 60); err != nil {
			t.Fatal(err)
		}
	}
	send(t, http.MethodPut, srv.URL+"/api/v1/rates", `{"mode":"one","fixed_rate":"0.20"}`)

	base := srv.URL + "/api/v1/groups"
	resp, body := send(t, http.MethodPost, base, `{"name":"  Entertainment center "}`)
	if resp.StatusCode != 201 {
		t.Fatalf("add: %d %s", resp.StatusCode, body)
	}
	grp := decode[model.ApplianceGroup](t, body)
	if grp.Name != "Entertainment center" || grp.MeterIDs == nil {
		t.Errorf("group = %+v", grp)
	}
	quoted := `"` + grp.ID + `"`
	put := func(meter, group string) int {
		r, _ := send(t, http.MethodPatch, srv.URL+"/api/v1/meters/"+meter, `{"group_id":`+group+`}`)
		return r.StatusCode
	}

	for _, ch := range []string{"strip-1", "strip-2", "strip-3"} {
		if code := put(ch, quoted); code != 200 {
			t.Fatalf("put %s in the group: %d", ch, code)
		}
	}
	if code := put("pc-wall", quoted); code != 400 {
		t.Errorf("a PC's plug in a group: %d, want 400", code)
	}
	if code := put("strip-1", `"grp-nope"`); code != 400 {
		t.Errorf("an unknown group: %d, want 400", code)
	}
	if code := put("strip-1", `5`); code != 400 {
		t.Errorf("a number for the group: %d, want 400", code)
	}

	// A plug is shown in one place: a grouped plug cannot join a computer, and a computer's plug cannot join a group.
	_, cbody := send(t, http.MethodPost, srv.URL+"/api/v1/computers", `{"name":"Desktop"}`)
	comp := decode[model.Computer](t, cbody)
	if r, _ := send(t, http.MethodPatch, srv.URL+"/api/v1/meters/strip-1", `{"computer_id":"`+comp.ID+`"}`); r.StatusCode != 409 {
		t.Errorf("a grouped plug on a computer: %d, want 409", r.StatusCode)
	}
	send(t, http.MethodPatch, srv.URL+"/api/v1/meters/desk-plug", `{"computer_id":"`+comp.ID+`"}`)
	if code := put("desk-plug", quoted); code != 409 {
		t.Errorf("a computer's plug in a group: %d, want 409", code)
	}

	send(t, http.MethodPatch, srv.URL+"/api/v1/meters/strip-3", `{"enabled":false}`)
	var d model.GroupDetail
	get(t, base+"/"+grp.ID+"/detail"+period, &d)
	wantKwh := (200.0 + 100) * 60 / 3.6e6
	if len(d.Members) != 2 || math.Abs(d.TotalKWh-wantKwh) > 1e-12 {
		t.Fatalf("members %d, total %v kWh, want the two shown plugs and %v (the hidden one is not counted)", len(d.Members), d.TotalKWh, wantKwh)
	}
	if got, want := parseMoney(t, d.TotalCost), wantKwh*0.20; math.Abs(got-want) > 1e-5 {
		t.Errorf("total cost %v, want %v", got, want)
	}
	for _, m := range d.Members {
		if m.GroupID == nil || *m.GroupID != grp.ID {
			t.Errorf("a member's summary should say which group it is in: %+v", m)
		}
	}

	// The house summary carries each plug's group, so a client can show the group once.
	var sum model.Summary
	get(t, srv.URL+"/api/v1/summary"+period, &sum)
	inGroup := 0
	for _, c := range sum.Meters {
		if c.GroupID != nil && *c.GroupID == grp.ID {
			inGroup++
		}
	}
	if inGroup != 2 {
		t.Errorf("%d summary meters carry the group, want the 2 shown plugs", inGroup)
	}

	// Taking a plug out, renaming, listing, removing.
	if code := put("strip-2", `null`); code != 200 {
		t.Errorf("take a plug out: %d", code)
	}
	for _, c := range []struct {
		name, method, url, body string
		want                    int
	}{
		{"rename", http.MethodPatch, base + "/" + grp.ID, `{"name":"Living room"}`, 200},
		{"blank name", http.MethodPatch, base + "/" + grp.ID, `{"name":" "}`, 400},
		{"unknown field", http.MethodPost, base, `{"name":"X","colour":"red"}`, 400},
		{"unknown group", http.MethodPatch, base + "/grp-nope", `{"name":"X"}`, 404},
		{"detail of an unknown group", http.MethodGet, base + "/grp-nope/detail" + period, ``, 404},
		{"detail with a bad period", http.MethodGet, base + "/" + grp.ID + "/detail?from=x&to=y", ``, 400},
		{"remove", http.MethodDelete, base + "/" + grp.ID, ``, 204},
		{"remove again", http.MethodDelete, base + "/" + grp.ID, ``, 404},
	} {
		if r, b := send(t, c.method, c.url, c.body); r.StatusCode != c.want {
			t.Errorf("%s: %d, want %d (%s)", c.name, r.StatusCode, c.want, b)
		}
	}
	var list struct{ Groups []model.ApplianceGroup }
	get(t, base, &list)
	if list.Groups == nil || len(list.Groups) != 0 {
		t.Errorf("groups = %v, want an empty (not null) list", list.Groups)
	}
	if chs, _ := st.Meter(ctx, "strip-1"); chs.GroupID != nil {
		t.Error("a plug still belongs to a removed group")
	}
}
