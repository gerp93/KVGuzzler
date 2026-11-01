package ford

import (
	"context"
	"testing"
	"time"
)

func TestParseBatteryTakesTheTractionBatteryByItsWholeName(t *testing.T) {
	// The shape is the real answer's: the 12 V battery's charge sits beside the traction battery's.
	b, ok := ParseBattery([]byte(`{"metrics":{
		"batteryStateOfCharge":{"value":91,"vehicleBattery":"PRIMARY_BATTERY","updateTime":"2026-10-29T21:19:50Z"},
		"xevBatteryStateOfCharge":{"value":63,"updateTime":"2026-10-29T21:00:00Z"},
		"xevBatteryActualStateOfCharge":{"value":61.129997,"updateTime":"2026-10-29T21:29:49Z"}}}`))
	if !ok || b.Percent != 61.129997 || !b.At.Equal(time.Date(2026, 10, 29, 21, 29, 49, 0, time.UTC)) {
		t.Fatalf("battery = %+v, %v; want 61.129997 reported at 21:29:49", b, ok)
	}
}

func TestParseBatteryReadsAFigureWithoutATimeAndRefusesWhatIsNotAPercentage(t *testing.T) {
	if b, ok := ParseBattery([]byte(`{"metrics":{"xevBatteryActualStateOfCharge":{"value":"88"}}}`)); !ok || b.Percent != 88 || !b.At.IsZero() {
		t.Errorf("text figure without a time = %+v, %v", b, ok)
	}
	for name, body := range map[string]string{
		"none":          `{"metrics":{"batteryStateOfCharge":{"value":91}}}`,
		"over 100":      `{"metrics":{"xevBatteryActualStateOfCharge":{"value":101}}}`,
		"negative":      `{"metrics":{"xevBatteryActualStateOfCharge":{"value":-1}}}`,
		"no value":      `{"metrics":{"xevBatteryActualStateOfCharge":{"updateTime":"2026-10-29T21:29:49Z"}}}`,
		"not a number":  `{"metrics":{"xevBatteryActualStateOfCharge":{"value":"full"}}}`,
		"not an object": `{"metrics":{"xevBatteryActualStateOfCharge":5}}`,
		"not json":      `nope`,
	} {
		if b, ok := ParseBattery([]byte(body)); ok {
			t.Errorf("%s: read %+v", name, b)
		}
	}
}

func TestEveryLookKeepsTheBatterysChargeWithTheTimeTheCarReportedIt(t *testing.T) {
	f, sink, fs, now := feedFor(t)
	fs.battery = `,"xevBatteryActualStateOfCharge":{"value":61.5,"updateTime":"2026-10-29T21:29:49Z"}`
	if _, err := f.Read(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(sink.batts) != 1 || sink.batts[0].Percent != 61.5 || !sink.batts[0].At.Equal(time.Date(2026, 10, 29, 21, 29, 49, 0, time.UTC)) {
		t.Fatalf("kept %+v, want 61.5 at the car's own time", sink.batts)
	}
	// A car that gave no time is kept at the moment it was asked, and an answer without a battery keeps nothing.
	*now = now.Add(7 * time.Hour)
	fs.battery = `,"xevBatteryActualStateOfCharge":{"value":50}`
	if _, err := f.Read(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(sink.batts) != 2 || sink.batts[1].Percent != 50 || !sink.batts[1].At.Equal(now.UTC()) {
		t.Fatalf("kept %+v, want the second at the time of the look", sink.batts)
	}
	*now = now.Add(7 * time.Hour)
	fs.battery = ""
	if _, err := f.Read(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(sink.batts) != 2 {
		t.Errorf("an answer with no battery kept one: %+v", sink.batts)
	}
}
