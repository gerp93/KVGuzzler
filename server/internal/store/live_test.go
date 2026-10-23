package store

import (
	"context"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/pcrelay"
)

func live(at time.Time, cpu float64) *pcrelay.Live {
	idle := 18.5
	return &pcrelay.Live{
		At:           at,
		Readings:     []pcrelay.LiveReading{{Name: "CPU package", Scope: "component", Kind: "cpu", Watts: cpu}},
		Programs:     []pcrelay.LiveProgram{{Name: "ComfyUI", CPUWatts: 20, GPUWatts: 150}},
		Notes:        []string{"a note"},
		IdleCPUWatts: &idle,
	}
}

func TestTheNewestLiveLookReplacesTheLastAndAnOlderOneDoesNot(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	t0 := time.Date(2026, 10, 22, 18, 0, 0, 0, time.UTC)
	push := func(l *pcrelay.Live, received time.Time) {
		t.Helper()
		in := pcrelay.Ingest{APIVersion: 1, SourceID: "pc-1", SourceName: "Desktop", Live: l}
		if _, err := s.IngestPC(ctx, in, received); err != nil {
			t.Fatal(err)
		}
	}
	read := func() (watts float64, received time.Time) {
		t.Helper()
		src, err := s.PcSourceSummary(ctx, "pc-1", t0.Add(-time.Hour), t0.Add(time.Hour))
		if err != nil || src == nil || src.Live == nil {
			t.Fatalf("live = %+v, %v", src, err)
		}
		return src.Live.Readings[0].Watts, src.Live.ReceivedAt
	}

	src, _ := s.PcSourceSummary(ctx, "pc-1", t0, t0.Add(time.Hour))
	if src != nil {
		t.Fatalf("a PC that has never reported: %+v", src)
	}

	push(live(t0, 50), t0)
	if w, at := read(); w != 50 || !at.Equal(t0) {
		t.Errorf("first: %v W received %v", w, at)
	}
	push(live(t0.Add(5*time.Second), 60), t0.Add(5*time.Second))
	if w, _ := read(); w != 60 {
		t.Errorf("a newer look should replace it: %v W", w)
	}
	push(live(t0.Add(2*time.Second), 999), t0.Add(6*time.Second)) // arrived late, older than what is held
	if w, _ := read(); w != 60 {
		t.Errorf("an older look must not replace a newer one: %v W", w)
	}

	src, _ = s.PcSourceSummary(ctx, "pc-1", t0, t0.Add(time.Hour))
	l := src.Live
	if len(l.Programs) != 1 || l.Programs[0].GPUWatts != 150 || len(l.Notes) != 1 || l.IdleCPUWatts == nil || *l.IdleCPUWatts != 18.5 || l.IdleGPUWatts != nil {
		t.Errorf("live = %+v", l)
	}
}

func TestARunIsKeptReplacedAsItGrowsAndListedByTheirOverlapWithThePeriod(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	t0 := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	run := func(name string, startMin, endMin int, kwh float64, still bool) pcrelay.Run {
		return pcrelay.Run{Name: name, Start: t0.Add(time.Duration(startMin) * time.Minute), End: t0.Add(time.Duration(endMin) * time.Minute), CPUKwh: kwh, GPUKwh: kwh * 2, ActiveSeconds: 100, StillRunning: still}
	}
	push := func(runs ...pcrelay.Run) {
		t.Helper()
		if _, err := s.IngestPC(ctx, pcrelay.Ingest{APIVersion: 1, SourceID: "pc-1", Runs: runs}, t0.Add(10*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}

	push(run("ComfyUI", 10, 20, 0.01, true), run("chrome.exe", 0, 5, 0.001, false), run("old.exe", -300, -290, 0.5, false))
	push(run("ComfyUI", 10, 40, 0.03, true)) // the same run, grown: replaces it

	src, _ := s.PcSourceSummary(ctx, "pc-1", t0.Add(-time.Hour), t0.Add(time.Hour))
	if len(src.Runs) != 2 {
		t.Fatalf("runs = %+v, want the two that overlap the hour (old.exe ended long before)", src.Runs)
	}
	if src.Runs[0].Name != "ComfyUI" || !src.Runs[0].StillRunning || src.Runs[0].CPUKwh != 0.03 || !src.Runs[0].End.Equal(t0.Add(40*time.Minute)) {
		t.Errorf("first = %+v, want the one still going, as it grew", src.Runs[0])
	}
	if src.Runs[1].Name != "chrome.exe" {
		t.Errorf("second = %+v", src.Runs[1])
	}

	all, _ := s.PcSourceSummary(ctx, "pc-1", t0.Add(-24*time.Hour), t0.Add(time.Hour))
	if len(all.Runs) != 3 {
		t.Errorf("a longer period should list all three: %+v", all.Runs)
	}
}
