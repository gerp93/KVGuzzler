package pcrelay

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 22, 19, 0, 0, 0, time.UTC)

// example is the contract's own ingest example, so the code and the contract cannot drift apart.
func example(t *testing.T) Ingest {
	t.Helper()
	body, err := os.ReadFile("../../../contract/examples/ingest-pc.json")
	if err != nil {
		t.Fatal(err)
	}
	var in Ingest
	if err := json.Unmarshal(body, &in); err != nil {
		t.Fatal(err)
	}
	return in
}

func TestTheContractExamplePasses(t *testing.T) {
	in := example(t)
	notes, err := in.Validate(now)
	if err != nil || len(notes) != 0 {
		t.Fatalf("notes %v, err %v", notes, err)
	}
	if len(in.Minutes) != 1 || len(in.Minutes[0].Readings) != 5 || len(in.Minutes[0].Programs) != 2 || len(in.Gaps) != 1 {
		t.Errorf("the example lost something in validation: %+v", in)
	}
}

func TestADifferentMajorVersionIsRefused(t *testing.T) {
	in := example(t)
	in.APIVersion = 2
	if _, err := in.Validate(now); !errors.Is(err, ErrVersion) {
		t.Errorf("err = %v, want ErrVersion", err)
	}
}

func TestStructurallyWrongBodiesAreRefused(t *testing.T) {
	cases := map[string]func(*Ingest){
		"empty source id":        func(in *Ingest) { in.SourceID = "" },
		"source id with a slash": func(in *Ingest) { in.SourceID = "pc/1" },
		"long source name":       func(in *Ingest) { in.SourceName = strings.Repeat("x", 61) },
		"too many minutes":       func(in *Ingest) { in.Minutes = make([]Minute, MaxMinutes+1) },
		"minute off the minute":  func(in *Ingest) { in.Minutes[0].Minute = in.Minutes[0].Minute.Add(7 * time.Second) },
		"minute in the future":   func(in *Ingest) { in.Minutes[0].Minute = now.Add(time.Hour).Truncate(time.Minute) },
		"reading with no name":   func(in *Ingest) { in.Minutes[0].Readings[0].Name = "" },
		"unknown scope":          func(in *Ingest) { in.Minutes[0].Readings[0].Scope = "mains" },
		"unknown kind":           func(in *Ingest) { in.Minutes[0].Readings[0].Kind = "fpga" },
		"zero samples":           func(in *Ingest) { in.Minutes[0].Readings[0].Samples = 0 },
		"negative watts":         func(in *Ingest) { in.Minutes[0].Readings[0].AvgWatts = -1 },
		"program active > 120 s": func(in *Ingest) { in.Minutes[0].Programs[0].ActiveSeconds = 500 },
		"negative program kwh":   func(in *Ingest) { in.Minutes[0].Programs[0].CPUKwh = -0.1 },
		"program with no name":   func(in *Ingest) { in.Minutes[0].Programs[0].Name = "" },
		"gap reversed":           func(in *Ingest) { in.Gaps[0].From, in.Gaps[0].To = in.Gaps[0].To, in.Gaps[0].From },
		"gap reason":             func(in *Ingest) { in.Gaps[0].Reason = "napping" },
		"negative plug kwh":      func(in *Ingest) { k := -1.0; in.Gaps[0].PlugKwh = &k },
		"gap in the future":      func(in *Ingest) { in.Gaps[0].To = now.Add(time.Hour) },
	}
	for name, mutate := range cases {
		in := example(t)
		mutate(&in)
		if _, err := in.Validate(now); !errors.Is(err, ErrBad) {
			t.Errorf("%s: err = %v, want ErrBad", name, err)
		}
	}
}

func TestImpossibleValuesAreDroppedNotAveragedAndSaid(t *testing.T) {
	in := example(t)
	m := &in.Minutes[0]
	m.Readings = append(m.Readings,
		Reading{Name: "GPU nonsense", Scope: "component", Kind: "gpu", AvgWatts: 2500, MaxWatts: 2500, Samples: 60}, // over the sensor cap
		Reading{Name: "Plug nonsense", Scope: "wall", AvgWatts: 4500, MaxWatts: 4500, Samples: 60},                  // over the plug cap
		Reading{Name: "Peak nonsense", Scope: "component", AvgWatts: 100, MaxWatts: 50000, Samples: 60},             // good average, impossible peak
	)
	m.Programs = append(m.Programs, Program{Name: "runaway.exe", CPUKwh: 0.5})
	huge := 99999.0
	m.TotalPeakWatts = &huge
	hugeKwh := 50.0
	in.Gaps[0].PlugKwh = &hugeKwh // 50 kWh over 4.5 hours is far more than a plug can carry

	notes, err := in.Validate(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Readings) != 5 || len(m.Programs) != 2 || m.TotalPeakWatts != nil || in.Gaps[0].PlugKwh != nil {
		t.Errorf("readings %d, programs %d, peak %v, plug %v: the impossible ones should be gone", len(m.Readings), len(m.Programs), m.TotalPeakWatts, in.Gaps[0].PlugKwh)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "Dropped 6 impossible") {
		t.Errorf("notes = %v", notes)
	}
}

func TestADuplicateNameInOneMinuteKeepsTheLaterOne(t *testing.T) {
	in := example(t)
	m := &in.Minutes[0]
	m.Readings = append(m.Readings, Reading{Name: "CPU package", Scope: "component", Kind: "cpu", AvgWatts: 99, MaxWatts: 120, Samples: 60})
	m.Programs = append(m.Programs, Program{Name: "chrome.exe", CPUKwh: 0.009})
	if _, err := in.Validate(now); err != nil {
		t.Fatal(err)
	}
	var cpu []Reading
	for _, r := range m.Readings {
		if r.Name == "CPU package" {
			cpu = append(cpu, r)
		}
	}
	if len(cpu) != 1 || cpu[0].AvgWatts != 99 {
		t.Errorf("CPU readings = %+v", cpu)
	}
	var chrome []Program
	for _, p := range m.Programs {
		if p.Name == "chrome.exe" {
			chrome = append(chrome, p)
		}
	}
	if len(chrome) != 1 || chrome[0].CPUKwh != 0.009 {
		t.Errorf("chrome = %+v", chrome)
	}
}

func TestAMinuteJustAheadOfTheClockIsToleratedForSkew(t *testing.T) {
	in := example(t)
	in.Minutes[0].Minute = now.Add(2 * time.Minute).Truncate(time.Minute)
	if _, err := in.Validate(now); err != nil {
		t.Errorf("a PC a couple of minutes ahead should be accepted: %v", err)
	}
}
