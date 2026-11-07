package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/collector"
	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

// alertWorld is a house with a mains meter drawing a steady 1000 W and a septic pump that rests at 2 W and runs for 4 minutes at 800 W every 34 to 59 hours,
// from 8 Oct 2026 for 40 days. The server's clock is set by the test.
type alertWorld struct {
	srv   *httptest.Server
	api   *Server
	now   time.Time
	lastH float64 // hours after the start when the pump last ran, in its pattern
	// runAgain is when it ran after a wait longer than any before.
	runAgain float64
}

var worldStart = time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)

func newAlertWorld(t *testing.T) *alertWorld {
	t.Helper()
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.UpsertDevice(ctx, model.Device{ID: "d", Kind: "refoss", Name: "d"}); err != nil {
		t.Fatal(err)
	}
	for id, sc := range map[string]model.Scope{"ma": model.ScopeMains, "septic": model.ScopeCircuit, "ev": model.ScopeCircuit} {
		if err := s.UpsertMeter(ctx, model.Meter{ID: id, DeviceID: "d", Scope: sc, Label: map[string]string{"ma": "Main", "septic": "Septic", "ev": "EV charger"}[id]}); err != nil {
			t.Fatal(err)
		}
	}
	w := &alertWorld{}
	gaps := []float64{44, 58, 37, 47, 52, 35, 41, 55, 38, 49, 43, 36, 57, 40}
	runAt := map[int]bool{}
	h := 10.0
	runAt[int(h*60)] = true
	for _, g := range gaps {
		h += g
		runAt[int(h*60)] = true
		w.lastH = h
	}
	// After the last run of the pattern the pump waits 76 hours, longer than it ever has, and then runs.
	w.runAgain = w.lastH + 76
	runAt[int(w.runAgain*60)] = true
	n := 40 * 24 * 60
	rows := make([]store.MinuteRow, 0, 2*n)
	left := 0
	for i := 0; i < n; i++ {
		at := worldStart.Add(time.Duration(i) * time.Minute)
		if runAt[i] {
			left = 4
		}
		avg, max := 2.0, 2.2
		if left > 0 {
			avg, max = 800, 1000
			left--
		}
		rows = append(rows, store.MinuteRow{MeterID: "septic", Minute: at, AvgW: avg, MaxW: max, Seconds: 60},
			store.MinuteRow{MeterID: "ma", Minute: at, AvgW: 1000, MaxW: 1000, Seconds: 60})
	}
	if err := s.AddMinutes(ctx, rows); err != nil {
		t.Fatal(err)
	}
	w.now = worldStart.Add(time.Duration(w.lastH*float64(time.Hour)) + 10*time.Hour)
	w.api = &Server{Store: s, Collector: collector.New(s, time.Second), Loc: time.UTC, Version: "test", Now: func() time.Time { return w.now }}
	w.srv = httptest.NewServer(w.api.Handler())
	t.Cleanup(w.srv.Close)
	return w
}

func (w *alertWorld) look(t *testing.T) {
	t.Helper()
	if err := w.api.EvaluateAlerts(context.Background(), w.now, nil); err != nil {
		t.Fatal(err)
	}
}

func (w *alertWorld) alerts(t *testing.T) alertsAnswer {
	t.Helper()
	var a alertsAnswer
	get(t, w.srv.URL+"/api/v1/alerts", &a)
	return a
}

func (w *alertWorld) active(t *testing.T) []alertView {
	t.Helper()
	var out []alertView
	for _, a := range w.alerts(t).Alerts {
		if a.Active {
			out = append(out, a)
		}
	}
	return out
}

func (w *alertWorld) patternRule(t *testing.T) alertRuleView {
	t.Helper()
	var v alertRuleView
	r := sendJSON(t, "POST", w.srv.URL+"/api/v1/alert-rules", `{"kind":"pattern","meter_id":"septic","params":{"watts":0,"days":30,"watch":{"overdue":true,"too_often":true,"too_long":true,"rest":true}}}`, &v)
	if r.StatusCode != http.StatusCreated {
		t.Fatalf("status %d", r.StatusCode)
	}
	return v
}

func TestAPatternRuleRaisesAnAlertWhenThePumpIsOverdueAndClearsItWhenItRuns(t *testing.T) {
	w := newAlertWorld(t)
	rule := w.patternRule(t)
	if rule.Name != "Septic: usual pattern" || !rule.Enabled || rule.Severity != sevWarn {
		t.Fatalf("rule %+v", rule)
	}
	// Ten hours after the last run is normal.
	w.look(t)
	if got := w.active(t); len(got) != 0 {
		t.Fatalf("a pump that is keeping its pattern raised %+v", got)
	}

	// Three days after the last run is longer than it has ever waited.
	w.now = worldStart.Add(time.Duration((w.lastH + 4.0/60 + 74) * float64(time.Hour)))
	w.look(t)
	got := w.active(t)
	if len(got) != 1 || !strings.HasPrefix(got[0].Title, "Septic has not run for 3.1 days") || got[0].Severity != sevWarn || got[0].RuleID != rule.ID {
		t.Fatalf("alerts %+v", got)
	}
	// Looking again later says the same alert, not a second one.
	w.now = w.now.Add(15 * time.Minute)
	w.look(t)
	if again := w.active(t); len(again) != 1 || again[0].ID != got[0].ID {
		t.Errorf("a second look: %+v", again)
	}
	a := w.alerts(t)
	if len(a.Banner) != 1 || a.Worst != sevWarn || a.ActiveCount != 1 {
		t.Errorf("banner %+v worst %q active %d", a.Banner, a.Worst, a.ActiveCount)
	}

	// The pump runs after 76 hours: the condition stops holding, and the alert closes on the second look that does not find it.
	w.now = worldStart.Add(time.Duration((w.runAgain + 1) * float64(time.Hour)))
	w.look(t)
	if still := w.active(t); len(still) != 1 {
		t.Fatalf("one look missing the condition does not close it: %+v", still)
	}
	w.now = w.now.Add(11 * time.Minute)
	w.look(t)
	if still := w.active(t); len(still) != 0 {
		t.Fatalf("closed after it stopped holding: %+v", still)
	}
	if a := w.alerts(t); len(a.Banner) != 0 || len(a.Alerts) != 1 || a.Alerts[0].Active {
		t.Errorf("closed alerts leave the banner but stay in the list: %+v", a)
	}
}

func TestSwitchingARuleOffOrRemovingItClosesWhatItRaised(t *testing.T) {
	w := newAlertWorld(t)
	rule := w.patternRule(t)
	w.now = worldStart.Add(time.Duration((w.lastH + 4.0/60 + 74) * float64(time.Hour)))
	w.look(t)
	if len(w.active(t)) != 1 {
		t.Fatal("expected an alert")
	}
	var v alertRuleView
	if r := sendJSON(t, "PATCH", w.srv.URL+"/api/v1/alert-rules/"+rule.ID, `{"enabled":false}`, &v); r.StatusCode != 200 || v.Enabled {
		t.Fatalf("switch off: %d %+v", r.StatusCode, v)
	}
	if len(w.active(t)) != 0 {
		t.Error("switching a rule off closes its alerts at once")
	}
	w.look(t)
	if len(w.active(t)) != 0 {
		t.Error("an off rule raises nothing")
	}
	sendJSON(t, "PATCH", w.srv.URL+"/api/v1/alert-rules/"+rule.ID, `{"enabled":true}`, &v)
	w.look(t)
	if len(w.active(t)) != 1 {
		t.Error("switched on, it looks again at once")
	}
	if r := sendJSON(t, "DELETE", w.srv.URL+"/api/v1/alert-rules/"+rule.ID, "", nil); r.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", r.StatusCode)
	}
	if got := w.alerts(t); len(got.Alerts) != 0 {
		t.Errorf("removing a rule removes its alerts: %+v", got.Alerts)
	}
}

type countNotifier struct {
	mu    sync.Mutex
	opens []store.Alert
}

func (c *countNotifier) Notify(_ context.Context, a store.Alert) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.opens = append(c.opens, a)
	return nil
}

func TestANotifierIsToldOnceWhenAnAlertOpens(t *testing.T) {
	w := newAlertWorld(t)
	n := &countNotifier{}
	w.api.Notifiers = []Notifier{n}
	w.patternRule(t)
	w.now = worldStart.Add(time.Duration((w.lastH + 4.0/60 + 74) * float64(time.Hour)))
	for i := 0; i < 4; i++ {
		w.look(t)
		w.now = w.now.Add(15 * time.Minute)
	}
	if len(n.opens) != 1 || !strings.Contains(n.opens[0].Title, "has not run") {
		t.Errorf("notified %+v", n.opens)
	}
}

func TestAnOutageIsRaisedOnceAndStaysUntilDismissed(t *testing.T) {
	w := newAlertWorld(t)
	w.look(t) // the first look: an outage from before it is history
	old := store.Outage{Kind: store.OutagePower, Started: w.now.Add(-48 * time.Hour), Ended: w.now.Add(-47 * time.Hour), Source: store.OutageManual}
	if _, err := w.api.Store.AddOutage(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	w.now = w.now.Add(2 * time.Minute)
	w.look(t)
	if a := w.alerts(t); len(a.Alerts) != 0 {
		t.Fatalf("an outage that ended before the engine first looked is not news: %+v", a.Alerts)
	}
	fresh := store.Outage{Kind: store.OutagePower, Started: w.now.Add(-93 * time.Minute), Ended: w.now, Source: store.OutageFromPlugs}
	if _, err := w.api.Store.AddOutage(context.Background(), fresh); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		w.now = w.now.Add(2 * time.Minute)
		w.look(t)
	}
	a := w.alerts(t)
	if len(a.Alerts) != 1 || a.Alerts[0].Active || !strings.HasPrefix(a.Alerts[0].Title, "The power was out for 1.6 hours") || len(a.Banner) != 1 {
		t.Fatalf("alerts %+v banner %+v", a.Alerts, a.Banner)
	}
	if r := sendJSON(t, "POST", w.srv.URL+"/api/v1/alerts/"+strconv.FormatInt(a.Banner[0].ID, 10)+"/ack", "", nil); r.StatusCode != http.StatusNoContent {
		t.Fatalf("ack: %d", r.StatusCode)
	}
	w.now = w.now.Add(2 * time.Minute)
	w.look(t)
	if a := w.alerts(t); len(a.Banner) != 0 || len(a.Alerts) != 1 {
		t.Errorf("dismissed stays dismissed and is not raised again: %+v", a)
	}
}

func TestARuleThatIsAskedForTooMuchOrTooLittleIsRefused(t *testing.T) {
	w := newAlertWorld(t)
	post := func(body string) int {
		return sendJSON(t, "POST", w.srv.URL+"/api/v1/alert-rules", body, nil).StatusCode
	}
	for name, body := range map[string]string{
		"no meter":                `{"kind":"level","params":{"op":"above","watts":5,"minutes":60}}`,
		"unknown meter":           `{"kind":"level","meter_id":"nope","params":{"op":"above","watts":5,"minutes":60}}`,
		"bad kind":                `{"kind":"magic","meter_id":"septic","params":{}}`,
		"level with no side":      `{"kind":"level","meter_id":"septic","params":{"watts":5,"minutes":60}}`,
		"level for no time":       `{"kind":"level","meter_id":"septic","params":{"op":"above","watts":5}}`,
		"quiet with no watts":     `{"kind":"quiet","meter_id":"septic","params":{"hours":24}}`,
		"pattern watches nothing": `{"kind":"pattern","meter_id":"septic","params":{"days":30,"watch":{}}}`,
		"pattern too short":       `{"kind":"pattern","meter_id":"septic","params":{"days":2,"watch":{"overdue":true}}}`,
		"bad severity":            `{"kind":"quiet","meter_id":"septic","severity":"loud","params":{"watts":5,"hours":24}}`,
	} {
		if code := post(body); code != http.StatusBadRequest {
			t.Errorf("%s: %d", name, code)
		}
	}
	if code := post(`{"kind":"quiet","meter_id":"septic","params":{"watts":50,"hours":72}}`); code != http.StatusCreated {
		t.Errorf("a good rule: %d", code)
	}
	// The built-in rules: their numbers are kept inside their limits, they cannot be removed, only switched off.
	if r := sendJSON(t, "PATCH", w.srv.URL+"/api/v1/alert-rules/device_silent", `{"params":{"minutes":0}}`, nil); r.StatusCode != http.StatusBadRequest {
		t.Errorf("device_silent 0 min: %d", r.StatusCode)
	}
	var v alertRuleView
	if r := sendJSON(t, "PATCH", w.srv.URL+"/api/v1/alert-rules/device_silent", `{"params":{"minutes":30},"severity":"urgent"}`, &v); r.StatusCode != 200 || v.Params.Minutes != 30 || v.Severity != sevUrgent {
		t.Errorf("device_silent 30 min: %d %+v", r.StatusCode, v)
	}
	if r := sendJSON(t, "DELETE", w.srv.URL+"/api/v1/alert-rules/device_silent", "", nil); r.StatusCode != http.StatusNotFound {
		t.Errorf("a built-in rule cannot be removed: %d", r.StatusCode)
	}
	var list struct{ Rules []alertRuleView }
	get(t, w.srv.URL+"/api/v1/alert-rules", &list)
	ids := map[string]bool{}
	for _, r := range list.Rules {
		ids[r.ID] = true
		if r.ID == "digest_daily" && r.Enabled {
			t.Error("the daily summary starts off")
		}
	}
	for _, want := range []string{"device_silent", "outage", "breaker", "baseload_up", "unusual_day", "bill_differs", "digest_weekly", "digest_daily"} {
		if !ids[want] {
			t.Errorf("missing built-in rule %s", want)
		}
	}
}

func TestPatternPreviewShowsWhatWasLearned(t *testing.T) {
	w := newAlertWorld(t)
	var p patternAnswer
	if r := get(t, w.srv.URL+"/api/v1/alert-rules/pattern?meter_id=septic", &p); r.StatusCode != 200 {
		t.Fatalf("status %d", r.StatusCode)
	}
	if !p.Ok || !p.Suggested || p.Runs < 10 || p.RunMedianMinutes != 4 || p.RestWatts < 1.9 || p.RestWatts > 2.1 || p.GapMaxHours < 57.9 || p.GapMaxHours > 58.1 || p.LastRunAt == nil {
		t.Errorf("%+v", p)
	}
	if abs(p.OverdueAfterHours-p.GapMaxHours*1.25) > 0.01 {
		t.Errorf("overdue after %v", p.OverdueAfterHours)
	}
	// A meter that never rises has nothing to learn.
	var flat patternAnswer
	get(t, w.srv.URL+"/api/v1/alert-rules/pattern?meter_id=ma", &flat)
	if flat.Ok || flat.Why != "no_spikes" {
		t.Errorf("%+v", flat)
	}
	if r := get(t, w.srv.URL+"/api/v1/alert-rules/pattern?meter_id=nope", nil); r.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown meter: %d", r.StatusCode)
	}
}

func TestALevelRuleHoldsThePowerPastALevelForAWhile(t *testing.T) {
	w := newAlertWorld(t)
	// The septic circuit rests at 2 W: "below 5 W for 30 minutes" is true, "above 5 W" is not, and a rule needs the whole window to agree.
	var below, above alertRuleView
	sendJSON(t, "POST", w.srv.URL+"/api/v1/alert-rules", `{"kind":"level","meter_id":"septic","name":"Pump has no power","params":{"op":"below","watts":5,"minutes":30}}`, &below)
	sendJSON(t, "POST", w.srv.URL+"/api/v1/alert-rules", `{"kind":"level","meter_id":"septic","params":{"op":"above","watts":5,"minutes":30}}`, &above)
	w.look(t)
	got := w.active(t)
	if len(got) != 1 || got[0].RuleID != below.ID || got[0].Title != "Septic is below 5 W" {
		t.Fatalf("%+v", got)
	}
}

func TestABreakerOverItsRatingIsUrgent(t *testing.T) {
	w := newAlertWorld(t)
	ctx := context.Background()
	amps := 20.0
	if _, err := w.api.Store.SetMeterBreaker(ctx, "ev", &amps); err != nil {
		t.Fatal(err)
	}
	hi, avg := 21.5, 21.0
	var rows []store.MinuteRow
	for i := 0; i < 200; i++ {
		rows = append(rows, store.MinuteRow{MeterID: "ev", Minute: w.now.Add(-time.Duration(i) * time.Minute), AvgW: 4800, MaxW: 5000, Seconds: 60, MaxA: &hi, AvgA: &avg})
	}
	if err := w.api.Store.AddMinutes(ctx, rows); err != nil {
		t.Fatal(err)
	}
	w.look(t)
	var urgent, warn bool
	for _, a := range w.active(t) {
		if a.Severity == sevUrgent && strings.Contains(a.Title, "EV charger is over its breaker") && strings.Contains(a.Detail, "21.5 A on a 20 A breaker") {
			urgent = true
		}
		if a.Severity == sevWarn && strings.Contains(a.Title, "near its breaker's limit") {
			warn = true
		}
	}
	if !urgent || !warn {
		t.Errorf("urgent %v, sustained %v: %+v", urgent, warn, w.active(t))
	}
}

func TestADeviceThatStopsAnsweringIsSaid(t *testing.T) {
	w := newAlertWorld(t)
	src := &flakySource{}
	if err := w.api.Collector.Add(context.Background(), src); err != nil {
		t.Fatal(err)
	}
	t0 := w.now
	w.api.Collector.PollOnce(context.Background(), t0) // answers
	src.down = true
	w.api.Collector.PollOnce(context.Background(), t0.Add(time.Minute))
	w.now = t0.Add(5 * time.Minute)
	w.look(t)
	if len(w.active(t)) != 0 {
		t.Fatal("five minutes is not long enough")
	}
	w.now = t0.Add(12 * time.Minute)
	w.look(t)
	got := w.active(t)
	if len(got) != 1 || got[0].Title != "Garage plug is not answering" || got[0].Detail != "It last answered 12 min ago." || got[0].Link != "#/connectors" {
		t.Fatalf("%+v", got)
	}
	src.down = false
	w.api.Collector.PollOnce(context.Background(), w.now.Add(time.Minute))
	for i := 0; i < 2; i++ {
		w.now = w.now.Add(2 * time.Minute)
		w.look(t)
	}
	if len(w.active(t)) != 0 {
		t.Error("it answers again, so the alert closes")
	}
}

// flakySource answers one reading, or fails when down.
type flakySource struct{ down bool }

func (f *flakySource) Device() model.Device {
	return model.Device{ID: "plug-1", Kind: "shelly", Host: "192.0.2.9", Name: "Garage plug"}
}
func (f *flakySource) Meters() []model.Meter {
	return []model.Meter{{ID: "plug-1:plug", DeviceID: "plug-1", Scope: model.ScopePlug, Label: "Garage plug", Enabled: true}}
}
func (f *flakySource) Poll(context.Context) ([]model.Reading, error) {
	if f.down {
		return nil, http.ErrHandlerTimeout
	}
	return []model.Reading{{MeterID: "plug-1:plug", Watts: 10}}, nil
}

func TestTheWeeklySummaryIsMadeOnMondayMorningOnceAndAnnounced(t *testing.T) {
	w := newAlertWorld(t) // 1 kW steady on the mains: 24 kWh a day
	ctx := context.Background()
	// Sunday evening: the week that has just ended is not over until Monday, so the latest complete week is the one before it (made at once, the first time).
	w.now = time.Date(2026, 11, 8, 20, 0, 0, 0, time.UTC)
	if err := w.api.makeDigests(ctx, w.now); err != nil {
		t.Fatal(err)
	}
	if list, _ := w.api.Store.Digests(ctx, 10); len(list) != 1 || list[0].PeriodStart != "2026-10-26" {
		t.Fatalf("on Sunday: %+v", list)
	}
	// Monday 07:00: the week of Monday 2 Nov to Sunday 8 Nov.
	w.now = time.Date(2026, 11, 9, 7, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ { // the second time makes nothing new
		if err := w.api.makeDigests(ctx, w.now); err != nil {
			t.Fatal(err)
		}
	}
	var got struct{ Digests []digestView }
	get(t, w.srv.URL+"/api/v1/digests", &got)
	if len(got.Digests) != 2 {
		t.Fatalf("%+v", got)
	}
	d := got.Digests[0] // newest first
	if d.Kind != "weekly" || d.From.String() != "2026-11-02" || d.To.String() != "2026-11-08" {
		t.Fatalf("%+v", d)
	}
	if abs(d.Body.KWh-168) > 0.01 || d.Body.PrevKWh == nil || abs(*d.Body.PrevKWh-168) > 0.01 || d.Body.ChangeShare == nil || abs(*d.Body.ChangeShare) > 0.001 {
		t.Errorf("energy %+v", d.Body)
	}
	if d.Body.Coverage == nil || *d.Body.Coverage < 0.999 || d.Body.BaseWatts == nil || abs(*d.Body.BaseWatts-1000) > 1 {
		t.Errorf("coverage %v base %v", d.Body.Coverage, d.Body.BaseWatts)
	}
	// The one appliance is the septic pump (the mains are the house, not an appliance).
	if len(d.Body.Top) == 0 || d.Body.Top[0].Label != "Septic" {
		t.Errorf("top %+v", d.Body.Top)
	}
	a := w.alerts(t)
	if len(a.Banner) != 2 || a.Banner[0].Title != "Summary for the week of 2026-11-02 to 2026-11-08 is ready" || a.Banner[0].Link != "#/alerts" || a.Banner[0].Severity != sevInfo {
		t.Errorf("banner %+v", a.Banner)
	}
	// The daily summary is off until it is switched on.
	if err := w.api.makeDigests(ctx, w.now); err != nil {
		t.Fatal(err)
	}
	for _, x := range w.alerts(t).Alerts {
		if strings.HasPrefix(x.Title, "Summary for 2026-") {
			t.Errorf("daily summary made while off: %+v", x)
		}
	}
	// Asking for one now makes yesterday's, and asking again gives the same one.
	var one, again digestView
	if r := sendJSON(t, "POST", w.srv.URL+"/api/v1/digests", `{"kind":"daily"}`, &one); r.StatusCode != 200 || one.From.String() != "2026-11-08" || abs(one.Body.KWh-24) > 0.01 {
		t.Fatalf("%d %+v", r.StatusCode, one)
	}
	sendJSON(t, "POST", w.srv.URL+"/api/v1/digests", `{"kind":"daily"}`, &again)
	if again.ID != one.ID {
		t.Errorf("a second ask made another: %d and %d", one.ID, again.ID)
	}
	if r := sendJSON(t, "POST", w.srv.URL+"/api/v1/digests", `{"kind":"hourly"}`, nil); r.StatusCode != http.StatusBadRequest {
		t.Errorf("bad kind: %d", r.StatusCode)
	}
}
