// Package pcrelay is what the PC collector pushes to the house server (the contract's PcIngest) and the checks that
// body must pass before anything is stored.
//
// The rules mirror the desktop app's: a reading above the cap for its kind is dropped, not averaged (the same
// MaxComponentWatts / MaxMeterWatts), and nothing the PC says is ever added to the house or circuit figures.
package pcrelay

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"time"
	"unicode/utf8"
)

// APIVersion is the contract's major version this server speaks.
const APIVersion = 1

const (
	MaxMinutes = 1440
	MaxGaps    = 1000

	// MaxComponentWatts and MaxMeterWatts are the desktop app's caps: no sensor reads above the first, no plug above the second.
	MaxComponentWatts = 2000.0
	MaxMeterWatts     = 4000.0

	// maxProgramKwhPerMinute is 6 kW for one minute: more than any single program can have been given.
	maxProgramKwhPerMinute = 0.1
	maxTotalPeakWatts      = 10000.0
	maxName                = 80
	MaxRuns                = 200
	maxLiveReadings        = 100
	maxLivePrograms        = 200
	maxNotes               = 50
	maxNoteLength          = 300
	maxRunKwh              = 1000.0
	maxRunSeconds          = 31 * 24 * 3600.0
	maxSourceName          = 60
	maxGap                 = 366 * 24 * time.Hour
	clockSkew              = 5 * time.Minute
)

// ErrBad is a body that cannot be accepted (400). ErrVersion is a different major version (426).
var (
	ErrBad     = errors.New("bad ingest")
	ErrVersion = errors.New("api version mismatch")
)

type Reading struct {
	Name     string  `json:"name"`
	Scope    string  `json:"scope"`
	Kind     string  `json:"kind"`
	AvgWatts float64 `json:"avg_watts"`
	MaxWatts float64 `json:"max_watts"`
	Samples  int     `json:"samples"`
}

type Program struct {
	Name          string  `json:"name"`
	CPUKwh        float64 `json:"cpu_kwh"`
	GPUKwh        float64 `json:"gpu_kwh"`
	ActiveSeconds float64 `json:"active_seconds"`
}

type Minute struct {
	Minute         time.Time `json:"minute"`
	Readings       []Reading `json:"readings"`
	Programs       []Program `json:"programs"`
	TotalPeakWatts *float64  `json:"total_peak_watts"`
}

type Gap struct {
	From    time.Time `json:"from"`
	To      time.Time `json:"to"`
	Reason  string    `json:"reason"`
	PlugKwh *float64  `json:"plug_kwh"`
}

// LiveReading is one reading as of the PC's last look; LiveProgram what one program is estimated to be causing right now.
type LiveReading struct {
	Name  string  `json:"name"`
	Scope string  `json:"scope"`
	Kind  string  `json:"kind"`
	Watts float64 `json:"watts"`
}

type LiveProgram struct {
	Name     string  `json:"name"`
	CPUWatts float64 `json:"cpu_watts"`
	GPUWatts float64 `json:"gpu_watts"`
}

// Live is the PC's latest readings: not history, the newest replaces the last.
type Live struct {
	At           time.Time     `json:"at"`
	Readings     []LiveReading `json:"readings"`
	Programs     []LiveProgram `json:"programs"`
	Notes        []string      `json:"notes"`
	IdleCPUWatts *float64      `json:"idle_cpu_watts"`
	IdleGPUWatts *float64      `json:"idle_gpu_watts"`
}

// Run is what a program is estimated to have caused from when it started until it closed.
type Run struct {
	Name          string    `json:"name"`
	Start         time.Time `json:"start"`
	End           time.Time `json:"end"`
	CPUKwh        float64   `json:"cpu_kwh"`
	GPUKwh        float64   `json:"gpu_kwh"`
	ActiveSeconds float64   `json:"active_seconds"`
	StillRunning  bool      `json:"still_running"`
}

type Ingest struct {
	APIVersion int      `json:"api_version"`
	SourceID   string   `json:"source_id"`
	SourceName string   `json:"source_name"`
	Minutes    []Minute `json:"minutes"`
	Gaps       []Gap    `json:"gaps"`
	Live       *Live    `json:"live"`
	Runs       []Run    `json:"runs"`
}

var (
	sourceIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	scopes          = map[string]float64{"component": MaxComponentWatts, "breakdown": MaxComponentWatts, "wall": MaxMeterWatts, "peripheral": MaxMeterWatts}
	kinds           = map[string]bool{"": true, "other": true, "cpu": true, "gpu": true}
	reasons         = map[string]bool{"asleep": true, "off": true, "lost_power": true}
)

func bad(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrBad, fmt.Sprintf(format, args...))
}

func finite(vs ...float64) bool {
	for _, v := range vs {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

// Validate checks the body and cleans it in place: a reading, program or gap energy that is impossible is dropped and
// said so in the returned notes; anything that makes the body itself wrong is an error. now is the server's clock.
func (in *Ingest) Validate(now time.Time) (notes []string, err error) {
	if in.APIVersion != APIVersion {
		return nil, fmt.Errorf("%w: this server speaks API version %d and the PC sent %d; update the older one", ErrVersion, APIVersion, in.APIVersion)
	}
	if !sourceIDPattern.MatchString(in.SourceID) {
		return nil, bad("source_id is 1 to 64 letters, digits, dots, dashes or underscores")
	}
	if utf8.RuneCountInString(in.SourceName) > maxSourceName {
		return nil, bad("source_name is %d characters at most", maxSourceName)
	}
	if len(in.Minutes) > MaxMinutes {
		return nil, bad("at most %d minutes in one push", MaxMinutes)
	}
	if len(in.Gaps) > MaxGaps {
		return nil, bad("at most %d gaps in one push", MaxGaps)
	}

	dropped := 0
	latest := now.Add(clockSkew)
	earliest := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range in.Minutes {
		m := &in.Minutes[i]
		m.Minute = m.Minute.UTC()
		if m.Minute.Unix()%60 != 0 {
			return nil, bad("minute %s is not on a whole minute", m.Minute.Format(time.RFC3339))
		}
		if m.Minute.After(latest) || m.Minute.Before(earliest) {
			return nil, bad("minute %s is outside the plausible range", m.Minute.Format(time.RFC3339))
		}

		kept := m.Readings[:0]
		seen := map[string]int{}
		for _, r := range m.Readings {
			if r.Name == "" || utf8.RuneCountInString(r.Name) > maxName {
				return nil, bad("a reading name is 1 to %d characters", maxName)
			}
			limit, ok := scopes[r.Scope]
			if !ok {
				return nil, bad("reading scope %q is component, breakdown, wall or peripheral", r.Scope)
			}
			if !kinds[r.Kind] {
				return nil, bad("reading kind %q is other, cpu or gpu", r.Kind)
			}
			if r.Samples < 1 || r.Samples > 120 {
				return nil, bad("samples is 1 to 120")
			}
			if !finite(r.AvgWatts, r.MaxWatts) || r.AvgWatts < 0 || r.MaxWatts < 0 {
				return nil, bad("watts must be finite and not negative")
			}
			if r.AvgWatts > limit || r.MaxWatts > limit {
				dropped++
				continue // an impossible reading is dropped, not averaged in
			}
			key := r.Scope + "\x00" + r.Name
			if at, dup := seen[key]; dup {
				kept[at] = r // the later one wins
				continue
			}
			seen[key] = len(kept)
			kept = append(kept, r)
		}
		m.Readings = kept

		keptP := m.Programs[:0]
		seenP := map[string]int{}
		for _, p := range m.Programs {
			if p.Name == "" || utf8.RuneCountInString(p.Name) > maxName {
				return nil, bad("a program name is 1 to %d characters", maxName)
			}
			if !finite(p.CPUKwh, p.GPUKwh, p.ActiveSeconds) || p.CPUKwh < 0 || p.GPUKwh < 0 || p.ActiveSeconds < 0 || p.ActiveSeconds > 120 {
				return nil, bad("a program's energy and active seconds must be finite, not negative, and at most 120 seconds")
			}
			if p.CPUKwh > maxProgramKwhPerMinute || p.GPUKwh > maxProgramKwhPerMinute {
				dropped++
				continue
			}
			if at, dup := seenP[p.Name]; dup {
				keptP[at] = p
				continue
			}
			seenP[p.Name] = len(keptP)
			keptP = append(keptP, p)
		}
		m.Programs = keptP

		if m.TotalPeakWatts != nil {
			if !finite(*m.TotalPeakWatts) || *m.TotalPeakWatts < 0 {
				return nil, bad("total_peak_watts must be finite and not negative")
			}
			if *m.TotalPeakWatts > maxTotalPeakWatts {
				m.TotalPeakWatts = nil
				dropped++
			}
		}
	}

	for i := range in.Gaps {
		g := &in.Gaps[i]
		g.From, g.To = g.From.UTC(), g.To.UTC()
		if !g.To.After(g.From) || g.To.Sub(g.From) > maxGap || g.To.After(latest) || g.From.Before(earliest) {
			return nil, bad("a gap must end after it starts, last at most a year, and not be in the future")
		}
		if !reasons[g.Reason] {
			return nil, bad("gap reason %q is asleep, off or lost_power", g.Reason)
		}
		if g.PlugKwh != nil {
			kwh := *g.PlugKwh
			hours := g.To.Sub(g.From).Hours()
			if !finite(kwh) || kwh < 0 {
				return nil, bad("plug_kwh must be finite and not negative")
			}
			if kwh/hours*1000 > MaxMeterWatts { // more than a plug can carry: the counter was not trustworthy
				g.PlugKwh = nil
				dropped++
			}
		}
	}

	dropped += in.validateLive(latest, earliest)
	if err := in.validateRuns(latest, earliest); err != nil {
		return nil, err
	}

	if dropped > 0 {
		notes = append(notes, fmt.Sprintf("Dropped %d impossible reading(s) from the PC.", dropped))
	}
	return notes, nil
}

// validateLive cleans the live snapshot in place and returns how many impossible values it dropped. A snapshot that is itself wrong
// (a time that makes no sense) is dropped whole: it is only the newest look, and the next one replaces it.
func (in *Ingest) validateLive(latest, earliest time.Time) int {
	l := in.Live
	if l == nil {
		return 0
	}
	l.At = l.At.UTC()
	if l.At.After(latest) || l.At.Before(earliest) {
		in.Live = nil
		return 0
	}
	dropped := 0

	readings := l.Readings[:0]
	for _, r := range l.Readings {
		limit, ok := scopes[r.Scope]
		if len(readings) >= maxLiveReadings || r.Name == "" || utf8.RuneCountInString(r.Name) > maxName || !ok || !kinds[r.Kind] || !finite(r.Watts) || r.Watts < 0 {
			dropped++
			continue
		}
		if r.Watts > limit {
			dropped++
			continue
		}
		readings = append(readings, r)
	}
	l.Readings = readings

	programs := l.Programs[:0]
	for _, p := range l.Programs {
		if len(programs) >= maxLivePrograms || p.Name == "" || utf8.RuneCountInString(p.Name) > maxName ||
			!finite(p.CPUWatts, p.GPUWatts) || p.CPUWatts < 0 || p.GPUWatts < 0 || p.CPUWatts > MaxComponentWatts || p.GPUWatts > MaxComponentWatts {
			dropped++
			continue
		}
		programs = append(programs, p)
	}
	l.Programs = programs

	notes := make([]string, 0, len(l.Notes))
	for _, n := range l.Notes {
		if len(notes) >= maxNotes {
			break
		}
		if r := []rune(n); len(r) > maxNoteLength {
			n = string(r[:maxNoteLength])
		}
		if n != "" {
			notes = append(notes, n)
		}
	}
	l.Notes = notes

	for _, w := range []**float64{&l.IdleCPUWatts, &l.IdleGPUWatts} {
		if *w != nil && (!finite(**w) || **w < 0 || **w > MaxComponentWatts) {
			*w = nil
		}
	}
	return dropped
}

func (in *Ingest) validateRuns(latest, earliest time.Time) error {
	if len(in.Runs) > MaxRuns {
		return bad("at most %d runs in one push", MaxRuns)
	}
	for i := range in.Runs {
		r := &in.Runs[i]
		r.Start, r.End = r.Start.UTC(), r.End.UTC()
		if r.Name == "" || utf8.RuneCountInString(r.Name) > maxName {
			return bad("a run's program name is 1 to %d characters", maxName)
		}
		if r.End.Before(r.Start) || r.End.After(latest) || r.Start.Before(earliest) {
			return bad("a run must end after it starts and not be in the future")
		}
		if !finite(r.CPUKwh, r.GPUKwh, r.ActiveSeconds) || r.CPUKwh < 0 || r.GPUKwh < 0 || r.ActiveSeconds < 0 ||
			r.CPUKwh > maxRunKwh || r.GPUKwh > maxRunKwh || r.ActiveSeconds > maxRunSeconds {
			return bad("a run's energy and active seconds must be finite, not negative and plausible")
		}
	}
	return nil
}
