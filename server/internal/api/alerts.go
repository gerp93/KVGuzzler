package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/alerts"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

const (
	// alertListDays is how far back the list of alerts goes; bannerDays how long an alert nobody dismissed stays on the banner.
	alertListDays = 30
	bannerDays    = 7
	alertListMax  = 300
)

type alertView struct {
	ID        int64      `json:"id"`
	RuleID    string     `json:"rule_id"`
	Severity  string     `json:"severity"`
	Title     string     `json:"title"`
	Detail    string     `json:"detail"`
	MeterID   string     `json:"meter_id,omitempty"`
	Link      string     `json:"link,omitempty"`
	OpenedAt  time.Time  `json:"opened_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	ClearedAt *time.Time `json:"cleared_at"`
	AckedAt   *time.Time `json:"acked_at"`
	// Active says the condition still holds. An event (an outage, an unusual day) is never active: it happened.
	Active bool `json:"active"`
}

func viewOfAlert(a store.Alert) alertView {
	return alertView{ID: a.ID, RuleID: a.RuleID, Severity: a.Severity, Title: a.Title, Detail: a.Detail, MeterID: a.MeterID, Link: a.Link,
		OpenedAt: a.Opened, UpdatedAt: a.Updated, ClearedAt: a.Cleared, AckedAt: a.Acked, Active: a.Cleared == nil}
}

type alertsAnswer struct {
	// Banner is what is on the banner: alerts not dismissed yet, worst first.
	Banner []alertView `json:"banner"`
	// Alerts is the list of the last month, newest first, open ones included.
	Alerts []alertView `json:"alerts"`
	// ActiveCount is how many conditions hold right now.
	ActiveCount int `json:"active_count"`
	// Worst is the highest severity on the banner; empty with none.
	Worst string `json:"worst"`
}

func (s *Server) listAlerts(w http.ResponseWriter, r *http.Request) {
	now := s.now()
	recent, err := s.Store.RecentAlerts(r.Context(), now.Add(-alertListDays*24*time.Hour), alertListMax)
	if err != nil {
		serverError(w, err)
		return
	}
	unacked, err := s.Store.UnackedAlerts(r.Context(), now.Add(-bannerDays*24*time.Hour))
	if err != nil {
		serverError(w, err)
		return
	}
	out := alertsAnswer{Banner: []alertView{}, Alerts: []alertView{}}
	for _, a := range recent {
		out.Alerts = append(out.Alerts, viewOfAlert(a))
		if a.Cleared == nil {
			out.ActiveCount++
		}
	}
	for _, a := range unacked {
		out.Banner = append(out.Banner, viewOfAlert(a))
		if severityRank(a.Severity) > severityRank(out.Worst) {
			out.Worst = a.Severity
		}
	}
	// Worst first, newest first within a severity.
	for i := 1; i < len(out.Banner); i++ {
		for j := i; j > 0 && severityRank(out.Banner[j].Severity) > severityRank(out.Banner[j-1].Severity); j-- {
			out.Banner[j], out.Banner[j-1] = out.Banner[j-1], out.Banner[j]
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) ackAlert(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("alertId"), 10, 64)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "unknown_alert", "no such alert")
		return
	}
	if _, err := s.Store.AckAlert(r.Context(), id, s.now()); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) ackAllAlerts(w http.ResponseWriter, r *http.Request) {
	if _, err := s.Store.AckAllAlerts(r.Context(), s.now()); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- rules ---

type alertRuleView struct {
	ID         string       `json:"id"`
	Builtin    bool         `json:"builtin"`
	Kind       string       `json:"kind"`
	Name       string       `json:"name"`
	Help       string       `json:"help,omitempty"`
	Enabled    bool         `json:"enabled"`
	MeterID    string       `json:"meter_id,omitempty"`
	MeterLabel string       `json:"meter_label,omitempty"`
	Severity   string       `json:"severity"`
	Params     AlertParams  `json:"params"`
	Fields     []paramField `json:"fields,omitempty"`
	// Open is how many alerts of the rule hold now.
	Open int `json:"open"`
}

func (s *Server) ruleViews(r *http.Request) ([]alertRuleView, error) {
	rules, err := s.alertRules(r.Context())
	if err != nil {
		return nil, err
	}
	_, byID, err := s.enabledMeters(r.Context())
	if err != nil {
		return nil, err
	}
	open, err := s.Store.OpenAlerts(r.Context())
	if err != nil {
		return nil, err
	}
	count := map[string]int{}
	for _, a := range open {
		count[a.RuleID]++
	}
	out := []alertRuleView{}
	for _, rule := range rules {
		v := alertRuleView{ID: rule.ID, Builtin: rule.Builtin, Kind: rule.Kind, Name: rule.Name, Enabled: rule.Enabled, MeterID: rule.MeterID, Severity: rule.severity(), Params: rule.P, Open: count[rule.ID]}
		if rule.Spec != nil {
			v.Help, v.Fields = rule.Spec.Help, rule.Spec.Fields
		}
		if rule.MeterID != "" {
			v.MeterLabel = s.meterLabel(byID, rule.MeterID)
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Server) listAlertRules(w http.ResponseWriter, r *http.Request) {
	views, err := s.ruleViews(r)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Rules []alertRuleView `json:"rules"`
	}{views})
}

type ruleBody struct {
	Kind     *string      `json:"kind"`
	MeterID  *string      `json:"meter_id"`
	Name     *string      `json:"name"`
	Enabled  *bool        `json:"enabled"`
	Severity *string      `json:"severity"`
	Params   *AlertParams `json:"params"`
}

func readRuleBody(w http.ResponseWriter, r *http.Request) (ruleBody, bool) {
	var b ruleBody
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		writeProblem(w, http.StatusBadRequest, "bad_rule", "send kind, meter_id, params and optionally name, severity and enabled")
		return b, false
	}
	return b, true
}

func newRuleID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return "custom-" + hex.EncodeToString(b[:])
}

func (s *Server) ruleProblem(w http.ResponseWriter, err error) {
	if errors.Is(err, errBadRule) {
		writeProblem(w, http.StatusBadRequest, "bad_rule", strings.TrimPrefix(err.Error(), errBadRule.Error()+": "))
		return
	}
	serverError(w, err)
}

// addAlertRule makes one of the owner's own rules, about one meter.
func (s *Server) addAlertRule(w http.ResponseWriter, r *http.Request) {
	b, ok := readRuleBody(w, r)
	if !ok {
		return
	}
	if b.Kind == nil || b.MeterID == nil || b.Params == nil {
		writeProblem(w, http.StatusBadRequest, "bad_rule", "A rule needs a kind, a meter and its settings.")
		return
	}
	_, byID, err := s.enabledMeters(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	if m, ok := byID[*b.MeterID]; !ok || !m.Enabled {
		writeProblem(w, http.StatusBadRequest, "bad_rule", "No such meter.")
		return
	}
	p := *b.Params
	if err := checkCustom(*b.Kind, &p); err != nil {
		s.ruleProblem(w, err)
		return
	}
	row := store.AlertRule{ID: newRuleID(), Kind: *b.Kind, MeterID: *b.MeterID, Enabled: b.Enabled == nil || *b.Enabled, Created: s.now()}
	row.Name = strings.TrimSpace(derefString(b.Name))
	if row.Name == "" {
		row.Name = ruleName(row.Kind, s.meterLabel(byID, row.MeterID))
	}
	if len(row.Name) > 80 {
		writeProblem(w, http.StatusBadRequest, "bad_rule", "The name is at most 80 characters.")
		return
	}
	if sev := derefString(b.Severity); sev != "" {
		if !validSeverity(sev) {
			writeProblem(w, http.StatusBadRequest, "bad_rule", "The severity is info, warn or urgent.")
			return
		}
		row.Severity = sev
	}
	enc, err := json.Marshal(p)
	if err != nil {
		serverError(w, err)
		return
	}
	row.Params = string(enc)
	if err := s.Store.SaveAlertRule(r.Context(), row); err != nil {
		serverError(w, err)
		return
	}
	s.writeRuleBack(w, r, row.ID, http.StatusCreated)
}

func (s *Server) writeRuleBack(w http.ResponseWriter, r *http.Request, id string, status int) {
	views, err := s.ruleViews(r)
	if err != nil {
		serverError(w, err)
		return
	}
	for _, v := range views {
		if v.ID == id {
			writeJSON(w, status, v)
			return
		}
	}
	writeProblem(w, http.StatusNotFound, "unknown_rule", "no such rule")
}

// updateAlertRule changes a rule: a built-in one's switch, severity and settings, or the owner's own rule's anything.
func (s *Server) updateAlertRule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("ruleId")
	b, ok := readRuleBody(w, r)
	if !ok {
		return
	}
	rules, err := s.alertRules(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	var rule *alertRule
	for i := range rules {
		if rules[i].ID == id {
			rule = &rules[i]
		}
	}
	if rule == nil {
		writeProblem(w, http.StatusNotFound, "unknown_rule", "no such rule")
		return
	}
	row := rule.AlertRule
	if row.Created.IsZero() {
		row.Created = s.now()
	}
	if b.Enabled != nil {
		row.Enabled = *b.Enabled
	}
	if b.Severity != nil {
		if *b.Severity != "" && !validSeverity(*b.Severity) {
			writeProblem(w, http.StatusBadRequest, "bad_rule", "The severity is info, warn or urgent.")
			return
		}
		row.Severity = *b.Severity
	}
	p := rule.P
	if b.Params != nil {
		p = *b.Params
		if rule.Builtin {
			if err := checkBuiltin(rule.Spec, p); err != nil {
				s.ruleProblem(w, err)
				return
			}
		} else if err := checkCustom(rule.Kind, &p); err != nil {
			s.ruleProblem(w, err)
			return
		}
	}
	if !rule.Builtin {
		if b.Name != nil {
			name := strings.TrimSpace(*b.Name)
			if name == "" || len(name) > 80 {
				writeProblem(w, http.StatusBadRequest, "bad_rule", "The name is from 1 to 80 characters.")
				return
			}
			row.Name = name
		}
		if b.MeterID != nil {
			_, byID, err := s.enabledMeters(r.Context())
			if err != nil {
				serverError(w, err)
				return
			}
			if m, ok := byID[*b.MeterID]; !ok || !m.Enabled {
				writeProblem(w, http.StatusBadRequest, "bad_rule", "No such meter.")
				return
			}
			row.MeterID = *b.MeterID
		}
	}
	enc, err := json.Marshal(p)
	if err != nil {
		serverError(w, err)
		return
	}
	row.Params = string(enc)
	if err := s.Store.SaveAlertRule(r.Context(), row); err != nil {
		serverError(w, err)
		return
	}
	// A rule that was changed looks again at once, and one switched off closes what it opened.
	s.alertMu.Lock()
	delete(s.alertLast, id)
	s.alertMu.Unlock()
	if !row.Enabled {
		if err := s.closeRule(r.Context(), id, s.now()); err != nil {
			serverError(w, err)
			return
		}
	}
	s.writeRuleBack(w, r, id, http.StatusOK)
}

// checkBuiltin keeps a built-in rule's settings inside the limits its fields state.
func checkBuiltin(spec *builtinSpec, p AlertParams) error {
	for _, f := range spec.Fields {
		var v float64
		switch f.Key {
		case "minutes":
			v = float64(p.Minutes)
		case "percent":
			v = p.Percent
		case "watts":
			v = p.Watts
		case "hours":
			v = p.Hours
		}
		if v < f.Min || v > f.Max {
			return badRule("%s is from %g to %g %s.", f.Label, f.Min, f.Max, f.Unit)
		}
	}
	return nil
}

func (s *Server) deleteAlertRule(w http.ResponseWriter, r *http.Request) {
	ok, err := s.Store.DeleteAlertRule(r.Context(), r.PathValue("ruleId"))
	switch {
	case err != nil:
		serverError(w, err)
	case !ok:
		writeProblem(w, http.StatusNotFound, "unknown_rule", "no such rule, or a built-in one (those are switched off, not removed)")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// patternAnswer is what a meter's usual pattern looks like now, for the rule that holds it to that pattern to show before it is saved.
type patternAnswer struct {
	// Ok says there is a pattern; Why says what is missing when there is not ("no_data", "no_spikes", "too_few_runs").
	Ok  bool   `json:"ok"`
	Why string `json:"why,omitempty"`
	// LevelWatts is the power above which a minute counts as working; Suggested says it was picked from the meter's own minutes.
	LevelWatts float64 `json:"level_watts"`
	Suggested  bool    `json:"suggested"`
	RestWatts  float64 `json:"rest_watts"`
	Runs       int     `json:"runs"`
	Days       float64 `json:"days"`
	// GapMedianHours and GapMaxHours are the time from one run's start to the next's; OverdueAfterHours is how long a wait has to be to be said (the longest, with the margin).
	GapMedianHours    float64 `json:"gap_median_hours"`
	GapMaxHours       float64 `json:"gap_max_hours"`
	OverdueAfterHours float64 `json:"overdue_after_hours"`
	RunMedianMinutes  int     `json:"run_median_minutes"`
	RunMaxMinutes     int     `json:"run_max_minutes"`
	PeakMedianWatts   float64 `json:"peak_median_watts"`
	MostPerDay        int     `json:"most_per_day"`
	// LastRunAt and HoursSinceLastRun say when it last worked; null with no run.
	LastRunAt         *time.Time `json:"last_run_at"`
	HoursSinceLastRun *float64   `json:"hours_since_last_run"`
}

// alertPattern shows what a meter's pattern looks like from its last `days` days (default 30), with `watts` as the level or, without it, the one picked from its minutes.
func (s *Server) alertPattern(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id := q.Get("meter_id")
	_, byID, err := s.enabledMeters(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	if m, ok := byID[id]; !ok || !m.Enabled {
		writeProblem(w, http.StatusBadRequest, "unknown_meter", "no such meter")
		return
	}
	days, watts := 30, 0.0
	if v := q.Get("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 7 || n > 90 {
			writeProblem(w, http.StatusBadRequest, "bad_days", "days is from 7 to 90")
			return
		}
		days = n
	}
	if v := q.Get("watts"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f < 0 || f > 100000 {
			writeProblem(w, http.StatusBadRequest, "bad_watts", "watts is from 0 to 100000")
			return
		}
		watts = f
	}
	now := s.now()
	pts, _, err := s.recentPoints(r.Context(), id, time.Duration(days)*24*time.Hour, now)
	if err != nil {
		serverError(w, err)
		return
	}
	out := patternAnswer{}
	level, suggested := watts, false
	if level <= 0 {
		var ok bool
		if level, ok = alerts.SuggestLevel(pts); !ok {
			out.Why = "no_spikes"
			writeJSON(w, http.StatusOK, out)
			return
		}
		suggested = true
	}
	p := alerts.Learn(pts, level, now)
	out.Ok, out.Why, out.LevelWatts, out.Suggested, out.RestWatts, out.Runs = p.Ok, p.Why, p.Level, suggested, p.RestWatts, p.Runs
	out.Days = p.Span.Hours() / 24
	out.GapMedianHours, out.GapMaxHours = p.GapMedian.Hours(), p.GapMax.Hours()
	out.OverdueAfterHours = p.GapMax.Hours() * alerts.DefaultMargin
	out.RunMedianMinutes, out.RunMaxMinutes, out.PeakMedianWatts, out.MostPerDay = p.RunMedianMinutes, p.RunMaxMinutes, p.PeakMedianWatts, p.MostPerDay
	if !p.LastEnd.IsZero() {
		t := p.LastStart
		h := now.Sub(p.LastEnd).Hours()
		out.LastRunAt, out.HoursSinceLastRun = &t, &h
	}
	writeJSON(w, http.StatusOK, out)
}
