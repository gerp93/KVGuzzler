package ford

import (
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
