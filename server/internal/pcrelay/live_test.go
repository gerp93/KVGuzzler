package pcrelay

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTheLiveSnapshotAndRunsOfTheContractExampleSurviveValidation(t *testing.T) {
	in := example(t)
	if _, err := in.Validate(now); err != nil {
		t.Fatal(err)
	}
	if in.Live == nil || len(in.Live.Readings) != 3 || len(in.Live.Programs) != 1 || len(in.Live.Notes) != 1 ||
		in.Live.IdleCPUWatts == nil || *in.Live.IdleCPUWatts != 18.5 {
		t.Errorf("live = %+v", in.Live)
	}
	if len(in.Runs) != 1 || !in.Runs[0].StillRunning {
		t.Errorf("runs = %+v", in.Runs)
	}
}

func TestImpossibleLiveValuesAreDroppedAndCounted(t *testing.T) {
	in := example(t)
	l := in.Live
	l.Readings = append(l.Readings,
		LiveReading{Name: "GPU nonsense", Scope: "component", Kind: "gpu", Watts: 90000}, // over the sensor cap
		LiveReading{Name: "Plug nonsense", Scope: "wall", Watts: 4500},                   // over the plug cap
		LiveReading{Name: "Negative", Scope: "component", Watts: -3},
		LiveReading{Name: "", Scope: "component", Watts: 5},
		LiveReading{Name: "Odd scope", Scope: "mains", Watts: 5},
	)
	l.Programs = append(l.Programs, LiveProgram{Name: "runaway.exe", CPUWatts: 50000}, LiveProgram{Name: "", CPUWatts: 1})
	huge := 99999.0
	l.IdleGPUWatts = &huge

	notes, err := in.Validate(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Readings) != 3 || len(l.Programs) != 1 || l.IdleGPUWatts != nil || l.IdleCPUWatts == nil {
		t.Errorf("readings %d, programs %d, idle gpu %v: the impossible ones should be gone and the good idle kept", len(l.Readings), len(l.Programs), l.IdleGPUWatts)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "Dropped 7 impossible") {
		t.Errorf("notes = %v", notes)
	}
}

func TestALiveSnapshotWithANonsenseTimeIsDroppedWholeNotTheWholePush(t *testing.T) {
	for name, at := range map[string]time.Time{"far future": now.Add(48 * time.Hour), "long ago": time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)} {
		in := example(t)
		in.Live.At = at
		if _, err := in.Validate(now); err != nil {
			t.Fatalf("%s: a bad look should not refuse the minutes with it: %v", name, err)
		}
		if in.Live != nil {
			t.Errorf("%s: the snapshot should have been dropped", name)
		}
		if len(in.Minutes) != 1 {
			t.Errorf("%s: the minutes were lost with it", name)
		}
	}
}

func TestLiveNotesAreCutAndCounted(t *testing.T) {
	in := example(t)
	in.Live.Notes = []string{strings.Repeat("x", 500), "", "short"}
	for i := 0; i < 80; i++ {
		in.Live.Notes = append(in.Live.Notes, "note")
	}
	if _, err := in.Validate(now); err != nil {
		t.Fatal(err)
	}
	if len(in.Live.Notes) != maxNotes || len([]rune(in.Live.Notes[0])) != maxNoteLength || in.Live.Notes[1] != "short" {
		t.Errorf("notes = %d, first %d characters, second %q", len(in.Live.Notes), len([]rune(in.Live.Notes[0])), in.Live.Notes[1])
	}
}

func TestRunsThatMakeNoSenseRefuseThePush(t *testing.T) {
	cases := map[string]func(*Ingest){
		"too many":              func(in *Ingest) { in.Runs = make([]Run, MaxRuns+1) },
		"no name":               func(in *Ingest) { in.Runs[0].Name = "" },
		"ends before it starts": func(in *Ingest) { in.Runs[0].End = in.Runs[0].Start.Add(-time.Minute) },
		"in the future":         func(in *Ingest) { in.Runs[0].End = now.Add(time.Hour) },
		"negative energy":       func(in *Ingest) { in.Runs[0].GPUKwh = -1 },
		"absurd energy":         func(in *Ingest) { in.Runs[0].CPUKwh = 1e6 },
		"negative seconds":      func(in *Ingest) { in.Runs[0].ActiveSeconds = -5 },
	}
	for name, mutate := range cases {
		in := example(t)
		mutate(&in)
		if _, err := in.Validate(now); !errors.Is(err, ErrBad) {
			t.Errorf("%s: err = %v, want ErrBad", name, err)
		}
	}
}

func TestAPushWithOnlyALiveSnapshotIsFine(t *testing.T) {
	in := example(t)
	in.Minutes, in.Gaps, in.Runs = nil, nil, nil
	if _, err := in.Validate(now); err != nil {
		t.Fatalf("a live-only push should be accepted: %v", err)
	}
}
