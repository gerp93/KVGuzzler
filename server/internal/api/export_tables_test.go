package api

import (
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/model"
)

func strp(s string) *string { return &s }

func meterOf(id, label string, scope model.Scope, parent *string) model.Meter {
	return model.Meter{ID: id, Label: label, Scope: scope, Enabled: true, ParentID: parent}
}

func TestTheTreeIsListedMainsFirstThenFromTheTopDownEachFollowedByWhatIsInsideIt(t *testing.T) {
	rows := treeOrder([]model.Meter{
		meterOf("ev", "EV charger", model.ScopeCircuit, strp("garage")),
		meterOf("fridge", "Fridge", model.ScopeCircuit, nil),
		meterOf("garage", "Garage panel", model.ScopeCircuit, nil),
		meterOf("ma", "Mains A", model.ScopeMains, nil),
		meterOf("shop", "Workshop", model.ScopeCircuit, strp("garage")),
		meterOf("lamp", "Lamp", model.ScopePlug, strp("hidden")), // its parent is not shown: it is at the top
	})
	var got []string
	for _, r := range rows {
		got = append(got, strings.Join(r.Path, ">")+fmt.Sprintf("(%d)", r.Inside))
	}
	if len(rows) != 6 || rows[0].Meter.ID != "ma" {
		t.Fatalf("rows = %v", got)
	}
	order := []string{"ma", "fridge", "garage", "ev", "shop", "lamp"}
	for i, id := range order {
		if rows[i].Meter.ID != id {
			t.Errorf("row %d is %s, want %s (%v)", i, rows[i].Meter.ID, id, got)
		}
	}
	if strings.Join(rows[3].Path, " > ") != "Garage panel > EV charger" || rows[2].Inside != 2 || rows[3].Inside != 0 {
		t.Errorf("garage rows: %+v %+v", rows[2], rows[3])
	}
}

func TestTheTreeNeverLoopsAndTheMainsIsNeverInsideAnything(t *testing.T) {
	rows := treeOrder([]model.Meter{
		meterOf("a", "A", model.ScopeCircuit, strp("b")),
		meterOf("b", "B", model.ScopeCircuit, strp("a")),
		meterOf("m", "Mains", model.ScopeMains, strp("a")),
	})
	// A loop cannot be saved, but the export must survive one: nothing is listed twice, and the mains is at the top whatever it says.
	seen := map[string]int{}
	for _, r := range rows {
		seen[r.Meter.ID]++
	}
	if seen["m"] != 1 || rows[0].Meter.ID != "m" || seen["a"] > 1 || seen["b"] > 1 {
		t.Errorf("rows = %+v", rows)
	}
}

func readCSV(t *testing.T, resp *http.Response) [][]string {
	t.Helper()
	defer resp.Body.Close()
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/csv") || !strings.Contains(resp.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("status %d, type %q, disposition %q", resp.StatusCode, resp.Header.Get("Content-Type"), resp.Header.Get("Content-Disposition"))
	}
	body, _ := io.ReadAll(resp.Body)
	rows, err := csv.NewReader(strings.NewReader(string(body))).ReadAll()
	if err != nil {
		t.Fatalf("not a CSV: %v\n%s", err, body)
	}
	return rows
}

func TestRunsCanBeDownloadedAsACSVWithEveryRun(t *testing.T) {
	srv, start := runsServer(t)
	put(t, srv.URL+"/api/v1/rates", `{"mode":"one","fixed_rate":"0.25","currency":"USD"}`)
	url := strings.Replace(runsURL(srv, "charger", start, start.Add(72*time.Hour), ""), "/runs?", "/runs.csv?", 1)
	r, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	if cd := r.Header.Get("Content-Disposition"); !strings.Contains(cd, "kvguzzler-runs-charger-2026-10-20-to-2026-10-22.csv") {
		t.Errorf("disposition = %q", cd)
	}
	rows := readCSV(t, r)
	if strings.Join(rows[0], ",") != "start,end,minutes,kwh,avg_watts,peak_watts,avg_amps,peak_amps,cost" {
		t.Fatalf("header = %v", rows[0])
	}
	if len(rows) != 4 { // the header and three charges
		t.Fatalf("%d rows, want a header and the three charges", len(rows))
	}
	first := rows[1] // the newest: the charge on the evening of 22 October, 18:00 for two hours
	if first[0] != "2026-10-22 18:00" || first[1] != "2026-10-22 20:00" || first[2] != "120" || first[3] != "14.4" || first[4] != "7200" || first[6] != "30" || first[8] != "3.6000" {
		t.Errorf("first run = %v", first)
	}
	// Bad asks are refused like the JSON answer's.
	bad, _ := http.Get(strings.Replace(runsURL(srv, "nope", start, start.Add(time.Hour), ""), "/nope/runs?", "/nope/runs.csv?", 1))
	bad.Body.Close()
	if bad.StatusCode != http.StatusNotFound {
		t.Errorf("unknown meter: %d", bad.StatusCode)
	}
}

func TestTheMetersCanBeDownloadedAsACSVInTreeOrderWithWhatIsInsideAndALabelThatCannotRunAsAFormula(t *testing.T) {
	srv, start := runsServer(t)
	if r := patch(t, srv.URL+"/api/v1/meters/fridge", `{"parent_id":"charger","label":"=HYPERLINK(1)"}`); r.StatusCode != 200 {
		t.Fatalf("setup: %d", r.StatusCode)
	}
	if r := patch(t, srv.URL+"/api/v1/meters/charger", `{"breaker_amps":40}`); r.StatusCode != 200 {
		t.Fatalf("breaker: %d", r.StatusCode)
	}
	r, err := http.Get(fmt.Sprintf("%s/api/v1/tree.csv?from=%s&to=%s", srv.URL, start.Format(time.RFC3339), start.Add(72*time.Hour).Format(time.RFC3339)))
	if err != nil {
		t.Fatal(err)
	}
	rows := readCSV(t, r)
	if strings.Join(rows[0], ",") != "meter,id,type,scope,device,path,inside_count,kwh,cost,peak_watts,peak_amps,breaker_amps,monitored_hours" {
		t.Fatalf("header = %v", rows[0])
	}
	var ids []string
	byID := map[string][]string{}
	for _, row := range rows[1:] {
		ids = append(ids, row[1])
		byID[row[1]] = row
	}
	if strings.Join(ids, ",") != "ma,mb,charger,fridge" {
		t.Fatalf("order = %v, want the mains, then the charger and what is inside it", ids)
	}
	if got := byID["fridge"][0]; got != "'=HYPERLINK(1)" {
		t.Errorf("meter = %q: a label that starts with = must be written as text", got)
	}
	if got := byID["fridge"][5]; got != "charger > =HYPERLINK(1)" {
		t.Errorf("path = %q", got)
	}
	if byID["charger"][6] != "1" || byID["fridge"][6] != "0" || byID["charger"][11] != "40" {
		t.Errorf("charger = %v", byID["charger"])
	}
	// The energy is each meter's own: the charger's 43.2 kWh of charging is not added to anything.
	if kwh, _ := strconv.ParseFloat(byID["charger"][7], 64); kwh < 43.2 || kwh > 43.5 {
		t.Errorf("charger kWh = %v, want its own 43.2 plus the 3 W at rest", byID["charger"][7])
	}
}
