package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/baseload"
	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/rates"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

// Summaries of a day or a week, made from figures the server already has: what the house used and cost against the period before, which meters used it, how
// the always-on power moved, what was not recorded, which days were unusual and what raised an alert. A summary is made once, in the morning after its period
// ends (a day at 06:00 local, a week at 06:00 on Monday), kept as the JSON it is shown from, and announced by an alert. Nothing in it is a new figure.

const (
	digestDaily  = "daily"
	digestWeekly = "weekly"
	// digestHour is the local hour a summary is made from.
	digestHour = 6
	// digestTop is how many meters a summary lists.
	digestTop = 5
)

type digestMeter struct {
	MeterID  string   `json:"meter_id"`
	Label    string   `json:"label"`
	KWh      float64  `json:"kwh"`
	Cost     *string  `json:"cost"`
	PrevKWh  float64  `json:"prev_kwh"`
	ChangeKW float64  `json:"change_kwh"`
	Share    *float64 `json:"share"`
}

type digestAlerts struct {
	Info   int `json:"info"`
	Warn   int `json:"warn"`
	Urgent int `json:"urgent"`
}

type digestBody struct {
	Kind string     `json:"kind"`
	From rates.Date `json:"from"`
	To   rates.Date `json:"to"`
	// KWh and Cost are the house's (the mains added) over the period; Prev* the same over the period before it (for a day, the same weekday a week earlier).
	KWh           float64  `json:"kwh"`
	Cost          *string  `json:"cost"`
	CostEstimated bool     `json:"cost_estimated"`
	PrevKWh       *float64 `json:"prev_kwh"`
	PrevCost      *string  `json:"prev_cost"`
	// ChangeShare is the energy against the period before, as a share of it.
	ChangeShare *float64 `json:"change_share"`
	// Coverage is the share of the period the house was recorded for.
	Coverage *float64 `json:"coverage"`
	// Top are the appliances (meters that hold no other meter) that used the most, each against the period before; Biggest is the one that rose the most.
	Top     []digestMeter `json:"top"`
	Biggest *digestMeter  `json:"biggest_rise"`
	// BaseWatts is the house's quietest draw over the period and BasePrevWatts over the one before.
	BaseWatts     *float64     `json:"base_watts"`
	BasePrevWatts *float64     `json:"base_prev_watts"`
	Outages       reconOutages `json:"outages"`
	UnusualDays   int          `json:"unusual_days"`
	Alerts        digestAlerts `json:"alerts"`
}

type digestView struct {
	ID        int64      `json:"id"`
	Kind      string     `json:"kind"`
	From      rates.Date `json:"from"`
	To        rates.Date `json:"to"`
	CreatedAt time.Time  `json:"created_at"`
	Body      digestBody `json:"body"`
}

func (s *Server) digestEnabled(ctx context.Context, kind string) (bool, error) {
	rules, err := s.alertRules(ctx)
	if err != nil {
		return false, err
	}
	for _, r := range rules {
		if r.ID == "digest_"+kind {
			return r.Enabled, nil
		}
	}
	return false, nil
}

// digestPeriod is the latest complete period of the kind as of `now`, once it is time to make it: a day's is yesterday (from 06:00 local), a week's the last
// Monday-to-Sunday (from 06:00 on Monday). False before then.
func (s *Server) digestPeriod(kind string, now time.Time, early bool) (from, to rates.Date, ok bool) {
	local := now.In(s.Loc)
	today := rates.DateOf(now, s.Loc)
	switch kind {
	case digestDaily:
		if !early && local.Hour() < digestHour {
			return rates.Date{}, rates.Date{}, false
		}
		return today.AddDays(-1), today.AddDays(-1), true
	case digestWeekly:
		monday := today.AddDays(-((int(local.Weekday()) + 6) % 7))
		if monday == today && !early && local.Hour() < digestHour {
			monday = monday.AddDays(-7)
		}
		return monday.AddDays(-7), monday.AddDays(-1), true
	}
	return rates.Date{}, rates.Date{}, false
}

// makeDigests makes the summaries that are due and are switched on, and announces each with an alert.
func (s *Server) makeDigests(ctx context.Context, now time.Time) error {
	for _, kind := range []string{digestWeekly, digestDaily} {
		on, err := s.digestEnabled(ctx, kind)
		if err != nil || !on {
			if err != nil {
				return err
			}
			continue
		}
		from, to, ok := s.digestPeriod(kind, now, false)
		if !ok {
			continue
		}
		have, err := s.Store.HasDigest(ctx, kind, from.String())
		if err != nil || have {
			if err != nil {
				return err
			}
			continue
		}
		_, made, err := s.buildDigest(ctx, kind, from, to, now)
		if err != nil {
			return err
		}
		if !made {
			continue
		}
		title := "Summary for " + describePeriod(kind, from, to) + " is ready"
		a, err := s.Store.OpenAlert(ctx, store.Alert{RuleID: "digest_" + kind, Key: "digest:" + kind + ":" + from.String(), Severity: sevInfo, Title: title,
			Detail: "", Link: "#/alerts", Opened: now, Updated: now, Cleared: &now})
		if err != nil {
			return err
		}
		s.notify(ctx, a, nil)
	}
	return nil
}

func describePeriod(kind string, from, to rates.Date) string {
	if kind == digestDaily {
		return from.String()
	}
	return "the week of " + from.String() + " to " + to.String()
}

// buildDigest works out and keeps the summary of [from, to] (local days, inclusive). It reports whether it made one: a period with nothing recorded is not
// summarised.
func (s *Server) buildDigest(ctx context.Context, kind string, from, to rates.Date, now time.Time) (digestView, bool, error) {
	cfg, err := s.rateConfig(ctx)
	if err != nil {
		return digestView{}, false, err
	}
	schedule := cfg.Schedule(s.Loc)
	on, byID, err := s.enabledMeters(ctx)
	if err != nil {
		return digestView{}, false, err
	}
	days := to.DaysSince(from) + 1
	prevFrom := from.AddDays(-days)
	if kind == digestDaily {
		prevFrom = from.AddDays(-7)
	}
	prevTo := prevFrom.AddDays(days - 1)
	start, end := s.scheduleDayStart(from), s.scheduleDayStart(to.AddDays(1))
	pStart, pEnd := s.scheduleDayStart(prevFrom), s.scheduleDayStart(prevTo.AddDays(1))

	b := digestBody{Kind: kind, From: from, To: to, Top: []digestMeter{}}
	mains := []string{}
	for _, m := range on {
		if m.Scope == model.ScopeMains {
			mains = append(mains, m.ID)
		}
	}
	if len(mains) > 0 {
		f, err := s.houseFigures(ctx, on, start, end, schedule)
		if err != nil {
			return digestView{}, false, err
		}
		if f.KWh <= 0 {
			return digestView{}, false, nil
		}
		b.KWh, b.Cost, b.CostEstimated = f.KWh, f.Cost, f.Cost != nil && f.Estimated
		if pf, err := s.houseFigures(ctx, on, pStart, pEnd, schedule); err != nil {
			return digestView{}, false, err
		} else if pf.KWh > 0 {
			prev := pf.KWh
			b.PrevKWh, b.PrevCost = &prev, pf.Cost
			share := (f.KWh - pf.KWh) / pf.KWh
			b.ChangeShare = &share
		}
		var hours float64
		for _, id := range mains {
			t, err := s.Store.Totals(ctx, id, start, end)
			if err != nil {
				return digestView{}, false, err
			}
			hours += t.MonitoredHours
		}
		cov := hours / float64(len(mains)) / end.Sub(start).Hours()
		if cov > 1 {
			cov = 1
		}
		b.Coverage = &cov
		if w, err := s.Store.MinuteWatts(ctx, mains, start, end); err != nil {
			return digestView{}, false, err
		} else if r, ok := baseload.Of(w); ok {
			v := r.Watts
			b.BaseWatts = &v
		}
		if w, err := s.Store.MinuteWatts(ctx, mains, pStart, pEnd); err != nil {
			return digestView{}, false, err
		} else if r, ok := baseload.Of(w); ok {
			v := r.Watts
			b.BasePrevWatts = &v
		}
	}

	// The appliances: meters that hold no other meter and are not the mains.
	holds := map[string]bool{}
	for _, m := range on {
		if m.ParentID != nil {
			holds[*m.ParentID] = true
		}
	}
	var all []digestMeter
	for _, m := range on {
		if m.Scope == model.ScopeMains || holds[m.ID] {
			continue
		}
		cs, _, err := s.meterSummary(ctx, m, start, end, schedule)
		if err != nil {
			return digestView{}, false, err
		}
		ps, _, err := s.meterSummary(ctx, m, pStart, pEnd, schedule)
		if err != nil {
			return digestView{}, false, err
		}
		if cs.KWh <= 0 && ps.KWh <= 0 {
			continue
		}
		dm := digestMeter{MeterID: m.ID, Label: s.meterLabel(byID, m.ID), KWh: cs.KWh, Cost: cs.Cost, PrevKWh: ps.KWh, ChangeKW: cs.KWh - ps.KWh}
		if b.KWh > 0 {
			sh := cs.KWh / b.KWh
			dm.Share = &sh
		}
		all = append(all, dm)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].KWh > all[j].KWh })
	b.Top = append(b.Top, all[:min(digestTop, len(all))]...)
	sort.Slice(all, func(i, j int) bool { return all[i].ChangeKW > all[j].ChangeKW })
	if len(all) > 0 && all[0].ChangeKW >= 0.3 && all[0].ChangeKW >= 0.2*all[0].PrevKWh {
		top := all[0]
		b.Biggest = &top
	}

	if outages, err := s.Store.Outages(ctx, start, 200); err != nil {
		return digestView{}, false, err
	} else {
		b.Outages = outagesWithin(outages, start, end)
	}
	if an, err := s.anomalyDays(ctx, 60, ""); err == nil {
		for _, d := range an.Days {
			if !d.Date.Before(from) && !d.Date.After(to) {
				b.UnusualDays++
			}
		}
	}
	if raised, err := s.Store.RecentAlerts(ctx, start, 500); err == nil {
		for _, a := range raised {
			if a.Opened.Before(start) || !a.Opened.Before(end) || len(a.RuleID) >= 6 && a.RuleID[:6] == "digest" {
				continue
			}
			switch a.Severity {
			case sevUrgent:
				b.Alerts.Urgent++
			case sevWarn:
				b.Alerts.Warn++
			default:
				b.Alerts.Info++
			}
		}
	}

	body, err := json.Marshal(b)
	if err != nil {
		return digestView{}, false, err
	}
	if _, err := s.Store.SaveDigest(ctx, store.Digest{Kind: kind, PeriodStart: from.String(), PeriodEnd: to.String(), Created: now, Body: string(body)}); err != nil {
		return digestView{}, false, err
	}
	return digestView{Kind: kind, From: from, To: to, CreatedAt: now, Body: b}, true, nil
}

func viewOfDigest(d store.Digest) (digestView, bool) {
	var b digestBody
	if err := json.Unmarshal([]byte(d.Body), &b); err != nil {
		return digestView{}, false
	}
	return digestView{ID: d.ID, Kind: d.Kind, From: b.From, To: b.To, CreatedAt: d.Created, Body: b}, true
}

func (s *Server) listDigests(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	list, err := s.Store.Digests(r.Context(), limit)
	if err != nil {
		serverError(w, err)
		return
	}
	out := struct {
		Digests []digestView `json:"digests"`
	}{Digests: []digestView{}}
	for _, d := range list {
		if v, ok := viewOfDigest(d); ok {
			out.Digests = append(out.Digests, v)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// makeDigestNow makes the summary of the latest complete day or week now, rather than waiting for the morning. One already made is left as it is.
func (s *Server) makeDigestNow(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind string `json:"kind"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body); err != nil || (body.Kind != digestDaily && body.Kind != digestWeekly) {
		writeProblem(w, http.StatusBadRequest, "bad_digest", `send {"kind": "daily"} or {"kind": "weekly"}`)
		return
	}
	now := s.now()
	// The latest complete period, without waiting for the morning.
	from, to, ok := s.digestPeriod(body.Kind, now, true)
	if !ok {
		serverError(w, errors.New("no period"))
		return
	}
	if have, err := s.Store.HasDigest(r.Context(), body.Kind, from.String()); err != nil {
		serverError(w, err)
		return
	} else if !have {
		if _, made, err := s.buildDigest(r.Context(), body.Kind, from, to, now); err != nil {
			serverError(w, err)
			return
		} else if !made {
			writeProblem(w, http.StatusConflict, "nothing_recorded", "Nothing was recorded in that period.")
			return
		}
	}
	list, err := s.Store.Digests(r.Context(), 100)
	if err != nil {
		serverError(w, err)
		return
	}
	for _, d := range list {
		if d.Kind == body.Kind && d.PeriodStart == from.String() {
			if v, ok := viewOfDigest(d); ok {
				writeJSON(w, http.StatusOK, v)
				return
			}
		}
	}
	serverError(w, errors.New("the summary was not kept"))
}
