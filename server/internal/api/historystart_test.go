package api

import (
	"net/http"
	"testing"
	"time"
)

func TestTheDayHistoryStartsOnIsSavedShownAndTakenOff(t *testing.T) {
	srv, _ := newServer(t, 120)
	url := srv.URL + "/api/v1/history-start"

	var v historyStartView
	if r := get(t, url, &v); r.StatusCode != http.StatusOK || v.Day != nil || v.RecordedSince == nil {
		t.Fatalf("at first: %d day=%v recorded=%v, want no day and the first recorded moment", r.StatusCode, v.Day, v.RecordedSince)
	}
	first := *v.RecordedSince

	for name, body := range map[string]string{"no day": `{}`, "not a date": `{"day":"yesterday"}`, "in the future": `{"day":"2999-01-01"}`, "unknown field": `{"day":"2026-10-21","x":1}`} {
		if r, _ := put(t, url, body); r.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", name, r.StatusCode)
		}
	}

	// The day the data begins on keeps all of it.
	if r, _ := put(t, url, `{"day":"2026-10-22"}`); r.StatusCode != http.StatusOK {
		t.Fatalf("put: %d", r.StatusCode)
	}
	var after historyStartView
	get(t, url, &after)
	if after.Day == nil || *after.Day != "2026-10-22" || after.RecordedSince == nil || !after.RecordedSince.Equal(first) {
		t.Errorf("after = %+v, want the day saved and the first recorded moment unchanged", after)
	}
	var status struct {
		DataSince time.Time `json:"data_since"`
	}
	get(t, srv.URL+"/api/v1/status", &status)
	if !status.DataSince.Equal(first) {
		t.Errorf("data_since = %v, want %v: a day the data is all after changes nothing", status.DataSince, first)
	}

	if r := del(t, url); r.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", r.StatusCode)
	}
	get(t, url, &after)
	if after.Day != nil {
		t.Errorf("after delete the day is %v, want none", *after.Day)
	}
}
