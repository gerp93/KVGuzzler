package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func sendJSON(t *testing.T, method, url, body string, into any) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if into != nil && resp.StatusCode < 300 {
		if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
			t.Fatal(err)
		}
	}
	return resp
}

func TestOutagesAreTypedInCorrectedListedAndRemoved(t *testing.T) {
	f := newFordTest(t) // its clock is 2026-10-22 15:00 UTC
	url := f.srv.URL + "/api/v1/outages"

	var empty outagesAnswer
	if r := get(t, url, &empty); r.StatusCode != http.StatusOK || len(empty.Outages) != 0 || empty.LeewaySeconds != 600 {
		t.Fatalf("with none: %d %+v", r.StatusCode, empty)
	}

	var made outageView
	if r := sendJSON(t, "POST", url, `{"kind":"power","started_at":"2026-10-22T13:14:00Z","ended_at":"2026-10-22T14:47:00Z","note":" from the Ting app "}`, &made); r.StatusCode != http.StatusCreated {
		t.Fatalf("typing one in: %d", r.StatusCode)
	}
	if made.Kind != "power" || made.Seconds != 93*60 || made.Source != "manual" || made.Note != "from the Ting app" {
		t.Errorf("saved as %+v", made)
	}

	var list outagesAnswer
	get(t, url, &list)
	if len(list.Outages) != 1 || list.PowerSeconds != 93*60 || list.ServerSeconds != 0 {
		t.Errorf("listed %+v", list)
	}

	// A correction keeps what was not sent, and the entry becomes the owner's.
	var fixed outageView
	if r := sendJSON(t, "PATCH", url+"/"+itoa(made.ID), `{"ended_at":"2026-10-22T14:50:00Z"}`, &fixed); r.StatusCode != http.StatusOK || fixed.Seconds != 96*60 || fixed.Kind != "power" {
		t.Errorf("correcting: %d %+v", r.StatusCode, fixed)
	}

	if r := sendJSON(t, "DELETE", url+"/"+itoa(made.ID), "", nil); r.StatusCode != http.StatusNoContent {
		t.Errorf("removing: %d", r.StatusCode)
	}
	if r := sendJSON(t, "DELETE", url+"/"+itoa(made.ID), "", nil); r.StatusCode != http.StatusNotFound {
		t.Errorf("removing it again: %d, want 404", r.StatusCode)
	}
	if r := sendJSON(t, "PATCH", url+"/zzz", `{}`, nil); r.StatusCode != http.StatusNotFound {
		t.Errorf("a bad id: %d, want 404", r.StatusCode)
	}
}

func TestAnOutageThatMakesNoSenseIsRefused(t *testing.T) {
	f := newFordTest(t)
	url := f.srv.URL + "/api/v1/outages"
	sendJSON(t, "POST", url, `{"kind":"power","started_at":"2026-10-22T10:00:00Z","ended_at":"2026-10-22T11:00:00Z"}`, nil)
	for name, c := range map[string]struct {
		body string
		want int
	}{
		"an unknown kind":     {`{"kind":"flood","started_at":"2026-10-22T12:00:00Z","ended_at":"2026-10-22T13:00:00Z"}`, 400},
		"ending before start": {`{"kind":"power","started_at":"2026-10-22T13:00:00Z","ended_at":"2026-10-22T12:00:00Z"}`, 400},
		"in the future":       {`{"kind":"power","started_at":"2026-10-22T14:00:00Z","ended_at":"2026-10-22T18:00:00Z"}`, 400},
		"over a month":        {`{"kind":"unknown","started_at":"2026-08-01T00:00:00Z","ended_at":"2026-10-01T00:00:00Z"}`, 400},
		"no end":              {`{"kind":"power","started_at":"2026-10-22T12:00:00Z"}`, 400},
		"another field":       {`{"kind":"power","started_at":"2026-10-22T12:00:00Z","ended_at":"2026-10-22T13:00:00Z","who":"me"}`, 400},
		"overlapping":         {`{"kind":"server","started_at":"2026-10-22T10:30:00Z","ended_at":"2026-10-22T12:00:00Z"}`, 409},
	} {
		if r := sendJSON(t, "POST", url, c.body, nil); r.StatusCode != c.want {
			t.Errorf("%s: %d, want %d", name, r.StatusCode, c.want)
		}
	}
	var list outagesAnswer
	get(t, url, &list)
	if len(list.Outages) != 1 {
		t.Errorf("%d saved, want only the first", len(list.Outages))
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
