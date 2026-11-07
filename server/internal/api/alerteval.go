package api

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/alerts"
	"github.com/gerp93/KVGuzzler/server/internal/baseload"
	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/rates"
	"github.com/gerp93/KVGuzzler/server/internal/runs"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

// The alert engine. Every minute each rule that is switched on looks at the house and says what needs attention (a finding). Each finding is matched to the
// alert that is already open for it, if there is one:
//
//   - a condition (a device not answering, a meter over its breaker) opens an alert when it first holds, keeps it up to date while it does, and closes it when it
//     has stopped holding (on two looks in a row, so a flicker does not close and reopen it). Closing also takes it off the banner: it no longer needs attention.
//   - an event (an outage, an unusual day, a bill that differs) opens an alert once, already closed, which stays on the banner until it is dismissed.
//
// Dismissing an alert only takes it off the banner; it stays in the list while it holds. A rule switched off, or removed, closes what it opened.
// A Notifier is told of each alert as it opens, so a way of reaching the owner outside the dashboard (a push to the phone) can be added without touching a rule.

const (
	// alertsEvery is how often the engine looks.
	alertsEvery = time.Minute
	// alertsFirstWait gives the collector a moment to answer before the first look: a device is not "silent" while the server is still starting.
	alertsFirstWait = 30 * time.Second
	// alertKeep is how long closed alerts are kept.
	alertKeep = 90 * 24 * time.Hour
	// alertsSinceKey is when the engine first looked: events from before it (an old outage) are history, not news.
	alertsSinceKey = "alerts_since"
)

// Notifier is told of an alert the moment it opens (not each time it is updated). It is how an alert reaches the owner outside the dashboard.
type Notifier interface {
	Notify(ctx context.Context, a store.Alert) error
}

// alertsSince is when the engine first looked, remembered across restarts.
func (s *Server) alertsSince(ctx context.Context, now time.Time) (time.Time, error) {
	text, ok, err := s.Store.Meta(ctx, alertsSinceKey)
	if err != nil {
		return time.Time{}, err
	}
	if ok {
		if n, err := strconv.ParseInt(text, 10, 64); err == nil {
			return time.Unix(n, 0).UTC(), nil
		}
	}
	return now, s.Store.SetMeta(ctx, alertsSinceKey, strconv.FormatInt(now.Unix(), 10))
}

// RunAlerts looks every minute, makes the summaries of the day and week when they are due, and forgets old alerts.
func (s *Server) RunAlerts(ctx context.Context, logf func(format string, a ...any)) {
	s.alertStart = s.now()
	select {
	case <-ctx.Done():
		return
	case <-time.After(alertsFirstWait):
	}
	tick := time.NewTicker(alertsEvery)
	defer tick.Stop()
	var lastDigest time.Time
	for {
		now := s.now()
		if err := s.EvaluateAlerts(ctx, now, logf); err != nil && ctx.Err() == nil {
			logf("looking at the alert rules: %v", err)
		}
		if now.Sub(lastDigest) >= time.Hour {
			lastDigest = now
			if err := s.makeDigests(ctx, now); err != nil && ctx.Err() == nil {
				logf("making the summaries: %v", err)
			}
			_ = s.Store.PruneAlerts(ctx, now.Add(-alertKeep))
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// EvaluateAlerts runs every rule that is due once, at `now`.
func (s *Server) EvaluateAlerts(ctx context.Context, now time.Time, logf func(format string, a ...any)) error {
	s.alertMu.Lock()
	defer s.alertMu.Unlock()
	if s.alertLast == nil {
		s.alertLast, s.alertMiss = map[string]time.Time{}, map[string]int{}
	}
	rules, err := s.alertRules(ctx)
	if err != nil {
		return err
	}
	_, byID, err := s.enabledMeters(ctx)
	if err != nil {
		return err
	}
	live := map[string]bool{}
	for _, r := range rules {
		live[r.ID] = true
		if r.eval == nil {
			continue // the summaries are made by makeDigests
		}
		gone := r.MeterID != "" && !byID[r.MeterID].Enabled
		if !r.Enabled || gone {
			if err := s.closeRule(ctx, r.ID, now); err != nil {
				return err
			}
			delete(s.alertLast, r.ID)
			continue
		}
		every := customEvery(r.Kind)
		if r.Spec != nil {
			every = r.Spec.Every
		}
		if last, ok := s.alertLast[r.ID]; ok && now.Sub(last) < every {
			continue
		}
		found, err := r.eval(ctx, r, now)
		if err != nil {
			if logf != nil {
				logf("alert rule %s: %v", r.ID, err)
			}
			continue
		}
		s.alertLast[r.ID] = now
		if err := s.settleRule(ctx, r, found, every, now, logf); err != nil {
			return err
		}
	}
	// A rule that was removed no longer holds anything open.
	open, err := s.Store.OpenAlerts(ctx)
	if err != nil {
		return err
	}
	for _, a := range open {
		if !live[a.RuleID] {
			if err := s.Store.ClearAlert(ctx, a.ID, now); err != nil {
				return err
			}
		}
	}
	return nil
}

// closeRule closes every open alert of a rule.
func (s *Server) closeRule(ctx context.Context, ruleID string, now time.Time) error {
	open, err := s.Store.OpenAlerts(ctx)
	if err != nil {
		return err
	}
	for _, a := range open {
		if a.RuleID == ruleID {
			if err := s.Store.ClearAlert(ctx, a.ID, now); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Server) notify(ctx context.Context, a store.Alert, logf func(format string, a ...any)) {
	for _, n := range s.Notifiers {
		if err := n.Notify(ctx, a); err != nil && logf != nil {
			logf("telling the owner of alert %d: %v", a.ID, err)
		}
	}
}

// settleRule matches what a rule found to the alerts it has open.
func (s *Server) settleRule(ctx context.Context, r alertRule, found []finding, every time.Duration, now time.Time, logf func(format string, a ...any)) error {
	sev := r.severity()
	current := map[string]bool{}
	for _, f := range found {
		fs := f.Severity
		if fs == "" || r.Severity != "" {
			fs = sev
		}
		if f.Event {
			seen, err := s.Store.AlertSeen(ctx, r.ID, f.Key)
			if err != nil {
				return err
			}
			if seen {
				continue
			}
			t := now
			a, err := s.Store.OpenAlert(ctx, store.Alert{RuleID: r.ID, Key: f.Key, Severity: fs, Title: f.Title, Detail: f.Detail, MeterID: f.MeterID, Link: f.Link, Opened: now, Updated: now, Cleared: &t})
			if err != nil {
				return err
			}
			s.notify(ctx, a, logf)
			continue
		}
		current[f.Key] = true
		delete(s.alertMiss, r.ID+"|"+f.Key)
		open, ok, err := s.Store.OpenAlertFor(ctx, r.ID, f.Key)
		if err != nil {
			return err
		}
		if ok {
			if open.Title != f.Title || open.Detail != f.Detail || open.Severity != fs {
				if err := s.Store.UpdateAlert(ctx, open.ID, fs, f.Title, f.Detail, now); err != nil {
					return err
				}
			}
			continue
		}
		a, err := s.Store.OpenAlert(ctx, store.Alert{RuleID: r.ID, Key: f.Key, Severity: fs, Title: f.Title, Detail: f.Detail, MeterID: f.MeterID, Link: f.Link, Opened: now, Updated: now})
		if err != nil {
			return err
		}
		s.notify(ctx, a, logf)
	}
	// What was open and is not found now closes, once it has been missing on two looks (one, for a rule that looks only now and then).
	limit := 2
	if every >= 30*time.Minute {
		limit = 1
	}
	open, err := s.Store.OpenAlerts(ctx)
	if err != nil {
		return err
	}
	for _, a := range open {
		if a.RuleID != r.ID || current[a.Key] {
			continue
		}
		k := r.ID + "|" + a.Key
		s.alertMiss[k]++
		if s.alertMiss[k] >= limit {
			delete(s.alertMiss, k)
			if err := s.Store.ClearAlert(ctx, a.ID, now); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Server) localTime(t time.Time) string { return t.In(s.Loc).Format("Jan 2, 15:04") }

func (s *Server) deviceName(d model.Device) string {
	if d.Name != "" {
		return d.Name
	}
	return d.Host
}

// --- the built-in rules ---

func (s *Server) evalDeviceSilent(_ context.Context, r alertRule, now time.Time) ([]finding, error) {
	after := time.Duration(r.P.Minutes) * time.Minute
	var out []finding
	for _, d := range s.Collector.Devices() {
		if d.Online {
			continue
		}
		var silent time.Duration
		detail := "It has not answered since the server started."
		if d.LastSeen != nil {
			silent = now.Sub(*d.LastSeen)
			detail = "It last answered " + alerts.Humane(silent) + " ago."
		} else if !s.alertStart.IsZero() {
			silent = now.Sub(s.alertStart)
		}
		if silent < after {
			continue
		}
		out = append(out, finding{Key: d.ID, Title: s.deviceName(d) + " is not answering", Detail: detail, Link: "#/connectors"})
	}
	return out, nil
}

func (s *Server) evalOutages(ctx context.Context, _ alertRule, now time.Time) ([]finding, error) {
	since, err := s.alertsSince(ctx, now)
	if err != nil {
		return nil, err
	}
	list, err := s.Store.Outages(ctx, since, 20)
	if err != nil {
		return nil, err
	}
	var out []finding
	for _, o := range list {
		if o.Ended.Before(since) || o.Ended.Sub(o.Started) < time.Minute {
			continue
		}
		length := alerts.Humane(o.Ended.Sub(o.Started))
		title := "Nothing was recorded for " + length
		switch o.Kind {
		case store.OutagePower:
			title = "The power was out for " + length
		case store.OutageServer:
			title = "The server was off for " + length + " with the power on"
		}
		out = append(out, finding{Key: "outage:" + strconv.FormatInt(o.ID, 10), Title: title, Detail: s.localTime(o.Started) + " to " + s.localTime(o.Ended) + ".", Link: "#/history", Event: true})
	}
	return out, nil
}

func (s *Server) evalBreaker(ctx context.Context, _ alertRule, now time.Time) ([]finding, error) {
	on, byID, err := s.enabledMeters(ctx)
	if err != nil {
		return nil, err
	}
	var out []finding
	for _, m := range on {
		if m.BreakerAmps == nil || !(*m.BreakerAmps > 0) {
			continue
		}
		breaker := *m.BreakerAmps
		pts, err := s.Store.MeterMinutes(ctx, m.ID, now.Add(-3*time.Hour), now.Add(time.Minute))
		if err != nil {
			return nil, err
		}
		name := s.meterLabel(byID, m.ID)
		link := "#/hierarchy/meter/" + m.ID
		// Over the breaker's rating in the last few minutes.
		var latest float64
		for _, p := range pts {
			if p.HaveAmps && now.Sub(p.Minute) <= 5*time.Minute {
				latest = math.Max(latest, p.MaxAmps)
			}
		}
		if latest > breaker {
			out = append(out, finding{Key: "over|" + m.ID, Severity: sevUrgent, Title: name + " is over its breaker rating", Detail: fmt.Sprintf("Drew %.1f A on a %g A breaker.", latest, breaker), MeterID: m.ID, Link: link})
		}
		// Three hours at 80% of it or more, which is what a load that runs for hours should stay under.
		var n int
		var ampSeconds, seconds float64
		for _, p := range pts {
			if p.HaveAmps && p.Seconds > 0 {
				n++
				ampSeconds += p.AvgAmps * p.Seconds
				seconds += p.Seconds
			}
		}
		if n >= 150 && seconds > 0 && ampSeconds/seconds >= 0.8*breaker {
			out = append(out, finding{Key: "sustained|" + m.ID, Title: name + " has run near its breaker's limit",
				Detail: fmt.Sprintf("%.0f%% of a %g A breaker, on average, for the last 3 hours.", ampSeconds/seconds/breaker*100, breaker), MeterID: m.ID, Link: link})
		}
	}
	return out, nil
}

func (s *Server) mainsIDs(ctx context.Context) ([]string, error) {
	on, _, err := s.enabledMeters(ctx)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, m := range on {
		if m.Scope == model.ScopeMains {
			ids = append(ids, m.ID)
		}
	}
	return ids, nil
}

func (s *Server) evalBaseLoadUp(ctx context.Context, r alertRule, now time.Time) ([]finding, error) {
	ids, err := s.mainsIDs(ctx)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	recentW, err := s.Store.MinuteWatts(ctx, ids, now.Add(-7*24*time.Hour), now)
	if err != nil {
		return nil, err
	}
	beforeW, err := s.Store.MinuteWatts(ctx, ids, now.Add(-35*24*time.Hour), now.Add(-7*24*time.Hour))
	if err != nil {
		return nil, err
	}
	recent, ok1 := baseload.Of(recentW)
	before, ok2 := baseload.Of(beforeW)
	// The weeks before have to be a real stretch of history, not a day: a few hours say nothing about what is usual.
	if !ok1 || !ok2 || len(beforeW) < 3*24*60/2 {
		return nil, nil
	}
	rise := recent.Watts - before.Watts
	if rise < r.P.Watts || recent.Watts < before.Watts*(1+r.P.Percent/100) {
		return nil, nil
	}
	return []finding{{Key: "house", Title: fmt.Sprintf("Always-on power is up %.0f W", rise),
		Detail: fmt.Sprintf("The house's quietest draw is %.0f W over the last week, %.0f W over the weeks before.", recent.Watts, before.Watts), Link: "#/live"}}, nil
}

func (s *Server) evalUnusualDay(ctx context.Context, _ alertRule, now time.Time) ([]finding, error) {
	ans, err := s.anomalyDays(ctx, 2, "")
	if err != nil {
		return nil, err
	}
	yesterday := rates.DateOf(now, s.Loc).AddDays(-1)
	var out []finding
	for _, d := range ans.Days {
		if d.Date != yesterday {
			continue
		}
		subject, link := "house", "#/live"
		if d.MeterID != nil {
			subject, link = *d.MeterID, "#/hierarchy/meter/"+*d.MeterID
		}
		detail := fmt.Sprintf("Usually about %.1f kWh on this kind of day, %.1f kWh more.", d.UsualKWh, d.ExtraKWh)
		if d.ExtraCost != nil {
			detail += " " + moneyShort(*d.ExtraCost) + " extra."
		}
		if len(d.Causes) > 0 {
			detail += fmt.Sprintf(" Most of it: %s (+%.1f kWh).", d.Causes[0].Label, d.Causes[0].ExtraKWh)
		}
		out = append(out, finding{Key: subject + "|" + d.Date.String(), Title: fmt.Sprintf("Unusual day: %s used %.1f kWh on %s", d.Label, d.KWh, d.Date), Detail: detail, MeterID: derefString(d.MeterID), Link: link, Event: true})
	}
	return out, nil
}

// moneyShort is an amount of money for a sentence: two decimals, the currency's sign left to the dashboard.
func moneyShort(m string) string {
	v, err := rates.ParseMoney(m)
	if err != nil {
		return m
	}
	return v.Fixed(2)
}

func (s *Server) evalBillDiffers(ctx context.Context, r alertRule, now time.Time) ([]finding, error) {
	rec, err := s.reconcileBills(ctx)
	if err != nil {
		return nil, err
	}
	var out []finding
	recentEnough := rates.DateOf(now, s.Loc).AddDays(-60)
	for _, c := range rec.Bills {
		if c.InProgress || c.DiffShare == nil || c.Coverage == nil || *c.Coverage < fullCoverage || c.End.Before(recentEnough) {
			continue
		}
		if math.Abs(*c.DiffShare)*100 < r.P.Percent {
			continue
		}
		out = append(out, finding{Key: "bill:" + c.Start.String() + ":" + c.End.String(),
			Title:  fmt.Sprintf("The bill for %s to %s differs from the recording", c.Start, c.End),
			Detail: fmt.Sprintf("Billed %.0f kWh, recorded %.0f kWh (%+.1f%%).", c.BilledKWh, *c.RecordedKWh, *c.DiffShare*100), Link: "#/history", Event: true})
	}
	return out, nil
}

// --- the owner's own rules ---

// recentPoints are a meter's recorded minutes of the last `window`, and whether the newest is recent enough for the meter to be answering.
func (s *Server) recentPoints(ctx context.Context, meterID string, window time.Duration, now time.Time) ([]runs.Point, bool, error) {
	pts, err := s.Store.MeterMinutes(ctx, meterID, now.Add(-window), now.Add(time.Minute))
	if err != nil {
		return nil, false, err
	}
	var rec []runs.Point
	for _, p := range pts {
		if p.Seconds > 0 {
			rec = append(rec, p)
		}
	}
	fresh := len(rec) > 0 && now.Sub(rec[len(rec)-1].Minute) <= 5*time.Minute
	return rec, fresh, nil
}

func (s *Server) ruleMeter(ctx context.Context, r alertRule) (string, error) {
	_, byID, err := s.enabledMeters(ctx)
	if err != nil {
		return "", err
	}
	return s.meterLabel(byID, r.MeterID), nil
}

func (s *Server) evalLevel(ctx context.Context, r alertRule, now time.Time) ([]finding, error) {
	window := time.Duration(r.P.Minutes) * time.Minute
	pts, fresh, err := s.recentPoints(ctx, r.MeterID, window, now)
	if err != nil || !fresh {
		return nil, err
	}
	var inside []runs.Point
	for _, p := range pts {
		if now.Sub(p.Minute) <= window {
			inside = append(inside, p)
		}
	}
	// Judged only on a window that was mostly recorded, and every recorded minute of it has to be past the level.
	if float64(len(inside)) < 0.8*float64(r.P.Minutes) {
		return nil, nil
	}
	for _, p := range inside {
		if (r.P.Op == "above" && !(p.AvgW > r.P.Watts)) || (r.P.Op == "below" && !(p.AvgW < r.P.Watts)) {
			return nil, nil
		}
	}
	name, err := s.ruleMeter(ctx, r)
	if err != nil {
		return nil, err
	}
	latest := inside[len(inside)-1].AvgW
	return []finding{{Key: "level", Title: fmt.Sprintf("%s is %s %g W", name, r.P.Op, r.P.Watts),
		Detail: fmt.Sprintf("Drawing %.1f W; %s %g W for the last %d min.", latest, r.P.Op, r.P.Watts, r.P.Minutes), MeterID: r.MeterID, Link: "#/hierarchy/meter/" + r.MeterID}}, nil
}

func (s *Server) evalQuiet(ctx context.Context, r alertRule, now time.Time) ([]finding, error) {
	window := time.Duration(r.P.Hours * float64(time.Hour))
	first, ok, err := s.Store.FirstMinute(ctx, r.MeterID)
	if err != nil || !ok || first.After(now.Add(-window)) {
		return nil, err // not enough history to say it has not worked for that long
	}
	pts, fresh, err := s.recentPoints(ctx, r.MeterID, window, now)
	if err != nil || !fresh {
		return nil, err
	}
	// A wait in which the meter was mostly not recorded says nothing about whether it worked.
	if float64(len(pts)) < 0.8*window.Minutes() {
		return nil, nil
	}
	for _, p := range pts {
		if p.MaxW >= r.P.Watts {
			return nil, nil
		}
	}
	name, err := s.ruleMeter(ctx, r)
	if err != nil {
		return nil, err
	}
	return []finding{{Key: "quiet", Title: fmt.Sprintf("%s has not worked for %s", name, alerts.Humane(window)),
		Detail: fmt.Sprintf("Nothing above %g W in that time.", r.P.Watts), MeterID: r.MeterID, Link: "#/hierarchy/meter/" + r.MeterID}}, nil
}

func (s *Server) evalLong(ctx context.Context, r alertRule, now time.Time) ([]finding, error) {
	pts, fresh, err := s.recentPoints(ctx, r.MeterID, time.Duration(r.P.Minutes+30)*time.Minute, now)
	if err != nil || !fresh {
		return nil, err
	}
	found := runs.Detect(pts, r.P.Watts, 5, 1)
	n := len(found)
	if n == 0 || now.Sub(found[n-1].End) > 3*time.Minute || found[n-1].Minutes < r.P.Minutes {
		return nil, nil
	}
	name, err := s.ruleMeter(ctx, r)
	if err != nil {
		return nil, err
	}
	return []finding{{Key: "long", Title: fmt.Sprintf("%s has been working for %s", name, alerts.Humane(time.Duration(found[n-1].Minutes)*time.Minute)),
		Detail: fmt.Sprintf("Above %g W since %s.", r.P.Watts, s.localTime(found[n-1].Start)), MeterID: r.MeterID, Link: "#/hierarchy/meter/" + r.MeterID}}, nil
}

func (s *Server) evalPattern(ctx context.Context, r alertRule, now time.Time) ([]finding, error) {
	if r.P.Watch == nil {
		return nil, nil
	}
	pts, _, err := s.recentPoints(ctx, r.MeterID, time.Duration(r.P.Days)*24*time.Hour, now)
	if err != nil {
		return nil, err
	}
	level := r.P.Watts
	if level <= 0 {
		var ok bool
		if level, ok = alerts.SuggestLevel(pts); !ok {
			return nil, nil
		}
	}
	name, err := s.ruleMeter(ctx, r)
	if err != nil {
		return nil, err
	}
	pat := alerts.Learn(pts, level, now)
	var out []finding
	for _, is := range alerts.Check(pat, pts, now, *r.P.Watch, name) {
		out = append(out, finding{Key: "pattern|" + is.Code, Title: is.Title, Detail: is.Detail, MeterID: r.MeterID, Link: "#/hierarchy/meter/" + r.MeterID})
	}
	return out, nil
}
