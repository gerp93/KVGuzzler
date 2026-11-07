package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/alerts"
	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/store"
)

// Alert rules. A rule looks at the house's own history and says what needs attention. There are two kinds of rule:
//
//   - the built-in ones (about the house as a whole, or about every meter of some kind): each is switched on or off, and has settings with sensible
//     defaults. Only its switch and its settings are saved; what it does is in the code.
//   - the owner's own, each about one meter: a level it should (not) stay above or below, how long it may go without working, how long it may work, or the
//     meter's usual pattern learned from its own history (package alerts). What a rule means is fixed by its kind; the numbers are the owner's.

// The severities, least to most.
const (
	sevInfo   = "info"
	sevWarn   = "warn"
	sevUrgent = "urgent"
)

func validSeverity(s string) bool { return s == sevInfo || s == sevWarn || s == sevUrgent }

func severityRank(s string) int {
	switch s {
	case sevUrgent:
		return 3
	case sevWarn:
		return 2
	}
	return 1
}

// AlertParams are the numbers a rule is given. Which of them a rule uses is fixed by its kind; the others stay empty.
type AlertParams struct {
	// Op is "above" or "below" (level).
	Op string `json:"op,omitempty"`
	// Watts is a power: the level (level), the power above which the meter is working (quiet, long), or the least rise that counts (baseload). For a pattern, 0 is
	// "from the meter's own minutes".
	Watts   float64 `json:"watts,omitempty"`
	Minutes int     `json:"minutes,omitempty"`
	Hours   float64 `json:"hours,omitempty"`
	Percent float64 `json:"percent,omitempty"`
	// Days is how many days a pattern is learned from.
	Days int `json:"days,omitempty"`
	// Watch is which departures from a pattern are watched.
	Watch *alerts.Watch `json:"watch,omitempty"`
}

// paramField describes one setting of a built-in rule, so the dashboard can show it without knowing the rule.
type paramField struct {
	Key   string  `json:"key"`
	Label string  `json:"label"`
	Unit  string  `json:"unit"`
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
	Step  float64 `json:"step"`
}

// The kinds of the owner's own rules.
const (
	kindLevel   = "level"
	kindQuiet   = "quiet"
	kindLong    = "long"
	kindPattern = "pattern"
)

// ruleEval looks and returns what needs attention under the rule now.
type ruleEval func(ctx context.Context, r alertRule, now time.Time) ([]finding, error)

// finding is one thing a rule says needs attention. Event is something that happened (an outage, an unusual day): it is raised once and is over at once, only
// waiting to be dismissed; otherwise it is a condition that is open until it no longer holds.
type finding struct {
	Key      string
	Severity string
	Title    string
	Detail   string
	MeterID  string
	Link     string
	Event    bool
}

type builtinSpec struct {
	ID       string
	Name     string
	Help     string
	Severity string
	Enabled  bool
	Params   AlertParams
	Fields   []paramField
	// Every is how often it looks: a rule that reads weeks of history does not need to look every minute.
	Every time.Duration
	eval  ruleEval
}

// builtinSpecs are the built-in rules, in the order they are listed.
func (s *Server) builtinSpecs() []builtinSpec {
	return []builtinSpec{
		{ID: "device_silent", Name: "Device not answering", Help: "A plug or monitor has not answered for a while.", Severity: sevWarn, Enabled: true,
			Params: AlertParams{Minutes: 10}, Fields: []paramField{{Key: "minutes", Label: "Silent for", Unit: "min", Min: 2, Max: 1440, Step: 1}},
			Every: time.Minute, eval: s.evalDeviceSilent},
		{ID: "outage", Name: "Power or recording outage", Help: "Nothing was recorded for a while: the power was out, or the server was off.", Severity: sevWarn, Enabled: true,
			Every: time.Minute, eval: s.evalOutages},
		{ID: "breaker", Name: "Close to a breaker's limit", Help: "A circuit with a breaker size set is over it, or has run at 80% of it for 3 hours.", Severity: sevWarn, Enabled: true,
			Every: 5 * time.Minute, eval: s.evalBreaker},
		{ID: "baseload_up", Name: "Always-on power went up", Help: "What the house draws at its quietest is well above what it was over the weeks before.", Severity: sevInfo, Enabled: true,
			Params: AlertParams{Percent: 25, Watts: 30},
			Fields: []paramField{{Key: "percent", Label: "Up by at least", Unit: "%", Min: 5, Max: 300, Step: 5}, {Key: "watts", Label: "and at least", Unit: "W", Min: 5, Max: 2000, Step: 5}},
			Every:  time.Hour, eval: s.evalBaseLoadUp},
		{ID: "unusual_day", Name: "Unusual day", Help: "Yesterday used far more than the same kind of day usually does.", Severity: sevInfo, Enabled: true,
			Every: time.Hour, eval: s.evalUnusualDay},
		{ID: "bill_differs", Name: "Bill differs from the recording", Help: "A bill's energy is well away from what the house recorded over its days.", Severity: sevInfo, Enabled: true,
			Params: AlertParams{Percent: 6}, Fields: []paramField{{Key: "percent", Label: "Differs by at least", Unit: "%", Min: 1, Max: 50, Step: 1}},
			Every: time.Hour, eval: s.evalBillDiffers},
		{ID: "digest_weekly", Name: "Weekly summary", Help: "A summary of last week, made on Monday morning.", Severity: sevInfo, Enabled: true},
		{ID: "digest_daily", Name: "Daily summary", Help: "A summary of yesterday, made each morning.", Severity: sevInfo, Enabled: false},
	}
}

// alertRule is a rule as it runs: the saved row (or the built-in one's defaults) with its settings read.
type alertRule struct {
	store.AlertRule
	P    AlertParams
	Spec *builtinSpec
	eval ruleEval
}

func (r alertRule) severity() string {
	if r.Severity != "" {
		return r.Severity
	}
	if r.Spec != nil {
		return r.Spec.Severity
	}
	return sevWarn
}

// customEvals is what each kind of the owner's own rules does.
func (s *Server) customEval(kind string) ruleEval {
	switch kind {
	case kindLevel:
		return s.evalLevel
	case kindQuiet:
		return s.evalQuiet
	case kindLong:
		return s.evalLong
	case kindPattern:
		return s.evalPattern
	}
	return nil
}

// customEvery is how often the owner's rules look.
func customEvery(kind string) time.Duration {
	if kind == kindPattern {
		return 10 * time.Minute
	}
	return time.Minute
}

// alertRules is every rule: the built-in ones with what was saved over their defaults, then the owner's own.
func (s *Server) alertRules(ctx context.Context) ([]alertRule, error) {
	saved, err := s.Store.AlertRules(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[string]store.AlertRule{}
	for _, r := range saved {
		byID[r.ID] = r
	}
	var out []alertRule
	specs := s.builtinSpecs()
	for i := range specs {
		spec := &specs[i]
		r := alertRule{AlertRule: store.AlertRule{ID: spec.ID, Builtin: true, Kind: spec.ID, Name: spec.Name, Enabled: spec.Enabled}, P: spec.Params, Spec: spec, eval: spec.eval}
		if row, ok := byID[spec.ID]; ok {
			r.Enabled, r.Severity = row.Enabled, row.Severity
			if row.Params != "" {
				if err := json.Unmarshal([]byte(row.Params), &r.P); err != nil {
					r.P = spec.Params
				}
			}
		}
		out = append(out, r)
	}
	for _, row := range saved {
		if row.Builtin {
			continue
		}
		var p AlertParams
		if err := json.Unmarshal([]byte(row.Params), &p); err != nil {
			continue
		}
		out = append(out, alertRule{AlertRule: row, P: p, eval: s.customEval(row.Kind)})
	}
	return out, nil
}

var errBadRule = errors.New("bad rule")

func badRule(format string, a ...any) error {
	return fmt.Errorf("%w: %s", errBadRule, fmt.Sprintf(format, a...))
}

// checkCustom says why the owner's rule cannot be saved as it is, or returns nil, and fills in what was left out.
func checkCustom(kind string, p *AlertParams) error {
	switch kind {
	case kindLevel:
		if p.Op != "above" && p.Op != "below" {
			return badRule("Choose above or below.")
		}
		if p.Watts < 0 || p.Watts > 100000 {
			return badRule("The level is from 0 to 100000 W.")
		}
		if p.Minutes < 1 || p.Minutes > 1440 {
			return badRule("It has to last from 1 minute to 24 hours.")
		}
	case kindQuiet:
		if !(p.Watts > 0) || p.Watts > 100000 {
			return badRule("Enter the power above which it is working, in watts.")
		}
		if p.Hours < 0.5 || p.Hours > 720 {
			return badRule("The wait is from half an hour to 30 days.")
		}
	case kindLong:
		if !(p.Watts > 0) || p.Watts > 100000 {
			return badRule("Enter the power above which it is working, in watts.")
		}
		if p.Minutes < 5 || p.Minutes > 2880 {
			return badRule("A run is from 5 minutes to 2 days.")
		}
	case kindPattern:
		if p.Watts < 0 || p.Watts > 100000 {
			return badRule("The level is from 0 W (from the meter's own minutes) to 100000 W.")
		}
		if p.Days == 0 {
			p.Days = 30
		}
		if p.Days < 7 || p.Days > 90 {
			return badRule("Learn from 7 to 90 days.")
		}
		if p.Watch == nil || !(p.Watch.Overdue || p.Watch.TooOften || p.Watch.TooLong || p.Watch.Rest) {
			return badRule("Choose what to watch for.")
		}
		if p.Watch.Margin != 0 && (p.Watch.Margin < 1 || p.Watch.Margin > 5) {
			return badRule("The margin is from 1 to 5.")
		}
	default:
		return badRule("The kind is level, quiet, long or pattern.")
	}
	return nil
}

// ruleName is what a rule of the owner's is called when it was given no name.
func ruleName(kind, meter string) string {
	switch kind {
	case kindLevel:
		return meter + ": level"
	case kindQuiet:
		return meter + ": not working"
	case kindLong:
		return meter + ": working too long"
	}
	return meter + ": usual pattern"
}

func (s *Server) meterLabel(byID map[string]model.Meter, id string) string {
	if m, ok := byID[id]; ok && strings.TrimSpace(m.Label) != "" {
		return m.Label
	}
	return id
}
