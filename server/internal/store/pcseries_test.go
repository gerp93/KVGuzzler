package store

import (
	"context"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/pcrelay"
)

func TestPcSeriesGivesOneLinePerReadingExceptPartsOfASensorAndBucketsThem(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	start := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	in := pcrelay.Ingest{APIVersion: 1, SourceID: "pc-1", SourceName: "Desktop"}
	for i := 0; i < 120; i++ {
		m := pcMinute(start.Add(time.Duration(i)*time.Minute), 100)
		m.Readings = append(m.Readings,
			pcrelay.Reading{Name: "CPU cores", Scope: "breakdown", Kind: "cpu", AvgWatts: 80, MaxWatts: 90, Samples: 60},
			pcrelay.Reading{Name: "Monitor", Scope: "peripheral", Kind: "other", AvgWatts: 30, MaxWatts: 31, Samples: 60},
		)
		in.Minutes = append(in.Minutes, m)
	}
	in.Gaps = []pcrelay.Gap{{From: start.Add(30 * time.Minute), To: start.Add(40 * time.Minute), Reason: "asleep"}}
	if _, err := s.IngestPC(ctx, in, start.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}

	got, err := s.PcSeries(ctx, "pc-1", start, start.Add(2*time.Hour), model.BucketHour, time.UTC)
	if err != nil || got == nil {
		t.Fatalf("series = %+v, %v", got, err)
	}
	names := []string{}
	for _, l := range got.Lines {
		names = append(names, l.Scope+"/"+l.Name)
	}
	want := []string{"wall/Wall meter", "component/CPU package", "peripheral/Monitor"}
	if len(names) != 3 || names[0] != want[0] || names[1] != want[1] || names[2] != want[2] {
		t.Fatalf("lines = %v, want %v (the wall first, no CPU cores: it is a part of a sensor)", names, want)
	}
	cpu := got.Lines[1]
	if cpu.Kind != "cpu" || len(cpu.Points) != 2 || !near(cpu.Points[0].AvgWatts, 100) || cpu.Points[0].MaxWatts != 110 || !near(cpu.Points[0].KWh, 100.0/1000) {
		t.Errorf("cpu = %+v", cpu)
	}
	if !cpu.Points[1].T.Equal(start.Add(time.Hour)) {
		t.Errorf("second bucket at %v", cpu.Points[1].T)
	}
	if len(got.Gaps) != 1 || got.Gaps[0].Reason != "asleep" {
		t.Errorf("gaps = %+v", got.Gaps)
	}

	minute, _ := s.PcSeries(ctx, "pc-1", start, start.Add(10*time.Minute), model.BucketMinute, time.UTC)
	if len(minute.Lines[1].Points) != 10 || len(minute.Gaps) != 0 {
		t.Errorf("minute buckets: %d points, %d gaps", len(minute.Lines[1].Points), len(minute.Gaps))
	}
	if none, err := s.PcSeries(ctx, "pc-unknown", start, start.Add(time.Hour), model.BucketHour, time.UTC); err != nil || none != nil {
		t.Errorf("a PC that never reported: %+v, %v", none, err)
	}
}
