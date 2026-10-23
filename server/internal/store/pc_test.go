package store

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/pcrelay"
)

func pcMinute(at time.Time, cpuW float64) pcrelay.Minute {
	peak := 300.0
	return pcrelay.Minute{
		Minute: at,
		Readings: []pcrelay.Reading{
			{Name: "CPU package", Scope: "component", Kind: "cpu", AvgWatts: cpuW, MaxWatts: cpuW + 10, Samples: 60},
			{Name: "Wall meter", Scope: "wall", Kind: "other", AvgWatts: 300, MaxWatts: 320, Samples: 60},
		},
		Programs:       []pcrelay.Program{{Name: "ComfyUI", CPUKwh: 0.001, GPUKwh: 0.003, ActiveSeconds: 50}, {Name: "chrome.exe", CPUKwh: 0.0005, ActiveSeconds: 10}},
		TotalPeakWatts: &peak,
	}
}

func TestPcSourceSummaryAddsUpAndASecondPushReplacesNotDoubles(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	start := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	in := pcrelay.Ingest{APIVersion: 1, SourceID: "pc-1", SourceName: "Desktop"}
	for i := 0; i < 10; i++ {
		in.Minutes = append(in.Minutes, pcMinute(start.Add(time.Duration(i)*time.Minute), 100))
	}

	for pass := 0; pass < 2; pass++ { // the same push twice, as after a failed attempt
		n, err := s.IngestPC(ctx, in, start.Add(11*time.Minute))
		if err != nil || n != 10 {
			t.Fatalf("pass %d: %d minutes, %v", pass, n, err)
		}
	}

	src, err := s.PcSourceSummary(ctx, "pc-1", start, start.Add(time.Hour))
	if err != nil || src == nil {
		t.Fatalf("source = %+v, %v", src, err)
	}
	if src.SourceID != "pc-1" || src.Name != "Desktop" || src.LastSeen == nil || !src.LastSeen.Equal(start.Add(11*time.Minute)) {
		t.Errorf("source = %+v", src)
	}

	byName := map[string]float64{}
	for _, c := range src.Components {
		byName[c.Name] = c.KWh
		if c.Name == "CPU package" && (c.AvgWatts == nil || math.Abs(*c.AvgWatts-100) > 1e-9 || *c.PeakWatts != 110 || math.Abs(c.MonitoredHours-600.0/3600) > 1e-12) {
			t.Errorf("cpu = %+v", c)
		}
	}
	if math.Abs(byName["CPU package"]-100.0*600/3.6e6) > 1e-12 || math.Abs(byName["Wall meter"]-300.0*600/3.6e6) > 1e-12 {
		t.Errorf("energies = %v: a resent push must replace, not add", byName)
	}

	if len(src.Programs) != 2 || src.Programs[0].Name != "ComfyUI" || math.Abs(src.Programs[0].KWh-0.04) > 1e-9 || math.Abs(src.Programs[0].ActiveSeconds-500) > 1e-9 {
		t.Errorf("programs = %+v, want ComfyUI first with 10 x 0.004 kWh", src.Programs)
	}
	if src.TotalPeakWatts == nil || *src.TotalPeakWatts != 300 {
		t.Errorf("total peak = %v", src.TotalPeakWatts)
	}
}

func TestPcSourceSummaryOnlyCountsThePeriodAndAnEmptyOneIsStillThere(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	start := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	in := pcrelay.Ingest{APIVersion: 1, SourceID: "pc-1", SourceName: "Desktop", Minutes: []pcrelay.Minute{pcMinute(start, 100), pcMinute(start.Add(time.Hour), 100)}}
	if _, err := s.IngestPC(ctx, in, start); err != nil {
		t.Fatal(err)
	}

	src, _ := s.PcSourceSummary(ctx, "pc-1", start, start.Add(30*time.Minute)) // only the first minute
	cpu := src.Components[len(src.Components)-1]
	if math.Abs(cpu.MonitoredHours-60.0/3600) > 1e-12 {
		t.Errorf("monitored = %v h, want one minute", cpu.MonitoredHours)
	}

	empty, err := s.PcSourceSummary(ctx, "pc-1", start.Add(48*time.Hour), start.Add(49*time.Hour))
	if err != nil || empty == nil || len(empty.Components) != 0 || len(empty.Programs) != 0 || len(empty.Gaps) != 0 || empty.TotalPeakWatts != nil {
		t.Errorf("an empty period: %+v, %v (the PC should still be there, with empty lists)", empty, err)
	}
	if none, err := s.PcSourceSummary(ctx, "never-reported", start, start.Add(time.Hour)); none != nil || err != nil {
		t.Errorf("a collector that never reported: %+v, %v, want nil", none, err)
	}
}

func TestPcGapsAreClippedAndTheirPlugEnergyIsSharedEvenly(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	from := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	plug := 0.4
	in := pcrelay.Ingest{APIVersion: 1, SourceID: "pc-1", Gaps: []pcrelay.Gap{
		{From: from.Add(2 * time.Hour), To: from.Add(6 * time.Hour), Reason: "asleep", PlugKwh: &plug}, // 4 h, 0.4 kWh
		{From: from.Add(10 * time.Hour), To: from.Add(11 * time.Hour), Reason: "off"},
	}}
	if _, err := s.IngestPC(ctx, in, from.Add(12*time.Hour)); err != nil {
		t.Fatal(err)
	}

	// A period that cuts the first gap in half (hours 4 to 8 holds 2 of its 4 hours) and holds none of the second.
	src, _ := s.PcSourceSummary(ctx, "pc-1", from.Add(4*time.Hour), from.Add(8*time.Hour))
	gaps := src.Gaps
	if len(gaps) != 1 || gaps[0].Reason != "asleep" || !gaps[0].From.Equal(from.Add(4*time.Hour)) || !gaps[0].To.Equal(from.Add(6*time.Hour)) {
		t.Fatalf("gaps = %+v", gaps)
	}
	if gaps[0].PlugKwh == nil || math.Abs(*gaps[0].PlugKwh-0.2) > 1e-12 {
		t.Errorf("plug kwh = %v, want half of 0.4", gaps[0].PlugKwh)
	}

	all, _ := s.PcSourceSummary(ctx, "pc-1", from, from.Add(24*time.Hour))
	if len(all.Gaps) != 2 || all.Gaps[1].PlugKwh != nil {
		t.Errorf("gaps = %+v, want both, the second without a plug figure", all.Gaps)
	}
}
