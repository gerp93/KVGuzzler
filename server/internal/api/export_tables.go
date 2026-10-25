package api

import (
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/rates"
)

// treeRow is one meter in the order the Hierarchy tab shows them, with where it sits.
type treeRow struct {
	Meter model.Meter
	// Path is the labels from the top to the meter itself: "Garage panel > EV charger".
	Path []string
	// Inside is how many (enabled) meters sit directly inside it.
	Inside int
}

// treeOrder lays the enabled meters out as the Hierarchy tab does: the mains first, then everything else from the top down, each meter followed by
// the ones inside it. A meter whose parent is not shown is at the top. It cannot loop: a meter is listed once.
func treeOrder(meters []model.Meter) []treeRow {
	shown := map[string]bool{}
	for _, m := range meters {
		shown[m.ID] = true
	}
	kids := map[string][]model.Meter{}
	var top []model.Meter
	for _, m := range meters {
		switch {
		case m.Scope != model.ScopeMains && m.ParentID != nil && shown[*m.ParentID]:
			kids[*m.ParentID] = append(kids[*m.ParentID], m)
		default:
			top = append(top, m)
		}
	}
	var mains, others []model.Meter
	for _, m := range top {
		if m.Scope == model.ScopeMains {
			mains = append(mains, m)
		} else {
			others = append(others, m)
		}
	}
	var out []treeRow
	seen := map[string]bool{}
	var walk func(m model.Meter, path []string, depth int)
	walk = func(m model.Meter, path []string, depth int) {
		if seen[m.ID] || depth > 16 {
			return
		}
		seen[m.ID] = true
		here := append(append([]string(nil), path...), m.Label)
		out = append(out, treeRow{Meter: m, Path: here, Inside: len(kids[m.ID])})
		for _, k := range kids[m.ID] {
			walk(k, here, depth+1)
		}
	}
	for _, m := range append(mains, others...) {
		walk(m, nil, 0)
	}
	return out
}

func safeName(s string) string {
	s = strings.Trim(unsafeFileChars.ReplaceAllString(s, "-"), "-")
	if s == "" {
		return "meter"
	}
	return s
}

func sendCSV(w http.ResponseWriter, name, body string) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(name)+`"`)
	_, _ = w.Write([]byte(body))
}

// runsExport is a meter's runs over a period as a CSV download: one row per run, all of them (the JSON answer lists the newest 500).
func (s *Server) runsExport(w http.ResponseWriter, r *http.Request) {
	id, from, to, minWatts, ok := s.runsRequest(w, r)
	if !ok {
		return
	}
	got, err := s.computeRuns(r.Context(), id, from, to, minWatts, 0)
	if err != nil {
		serverError(w, err)
		return
	}
	_, byID, err := s.enabledMeters(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	opt := func(f *float64) string {
		if f == nil {
			return ""
		}
		return number(*f)
	}
	var b strings.Builder
	b.WriteString(csvLine([]string{"start", "end", "minutes", "kwh", "avg_watts", "peak_watts", "avg_amps", "peak_amps", "cost"}))
	const layout = "2006-01-02 15:04"
	for _, run := range got.Runs {
		cost := ""
		if run.Cost != nil {
			if m, err := rates.ParseMoney(*run.Cost); err == nil {
				cost = m.Fixed(4)
			}
		}
		b.WriteString(csvLine([]string{
			run.Start.In(s.Loc).Format(layout), run.End.In(s.Loc).Format(layout), number(float64(run.Minutes)), number(run.KWh), number(run.AvgWatts),
			number(run.PeakWatts), opt(run.AvgAmps), opt(run.PeakAmps), cost,
		}))
	}
	name := "kvguzzler-runs-" + safeName(byID[id].Label) + "-" + rates.DateOf(from, s.Loc).String() + "-to-" + rates.DateOf(to.Add(-time.Nanosecond), s.Loc).String() + ".csv"
	sendCSV(w, name, b.String())
}

// treeExport is every shown meter over a period as a CSV download, in the order of the Hierarchy tab: what each used and cost, its highest power and
// current, its breaker, where it sits and how many meters are inside it. A meter's own reading already contains the ones inside it, so the rows are
// never meant to be added: `inside_count` says which rows hold others.
func (s *Server) treeExport(w http.ResponseWriter, r *http.Request) {
	from, to, err := parsePeriod(r)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "bad_period", err.Error())
		return
	}
	on, _, err := s.enabledMeters(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	cfg, err := s.rateConfig(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	schedule := cfg.Schedule(s.Loc)
	devices := map[string]string{}
	for _, d := range s.Collector.Devices() {
		devices[d.ID] = d.Name
	}
	var b strings.Builder
	b.WriteString(csvLine([]string{"meter", "id", "type", "scope", "device", "path", "inside_count", "kwh", "cost", "peak_watts", "peak_amps", "breaker_amps", "monitored_hours"}))
	for _, row := range treeOrder(on) {
		m := row.Meter
		cs, _, err := s.meterSummary(r.Context(), m, from, to, schedule)
		if err != nil {
			serverError(w, err)
			return
		}
		cost, peakW, peakA, breaker := "", "", "", ""
		if cs.Cost != nil {
			if mo, err := rates.ParseMoney(*cs.Cost); err == nil {
				cost = mo.Fixed(4)
			}
		}
		if cs.PeakWatts != nil {
			peakW = number(*cs.PeakWatts)
		}
		if cs.PeakAmps != nil {
			peakA = number(*cs.PeakAmps)
		}
		if m.BreakerAmps != nil {
			breaker = number(*m.BreakerAmps)
		}
		b.WriteString(csvLine([]string{
			m.Label, m.ID, string(m.Type), string(m.Scope), devices[m.DeviceID], strings.Join(row.Path, " > "), number(float64(row.Inside)),
			number(cs.KWh), cost, peakW, peakA, breaker, number(cs.MonitoredHours),
		}))
	}
	name := "kvguzzler-meters-" + rates.DateOf(from, s.Loc).String() + "-to-" + rates.DateOf(to.Add(-time.Nanosecond), s.Loc).String() + ".csv"
	sendCSV(w, name, b.String())
}
