package api

import (
	"testing"

	"github.com/gerp93/KVGuzzler/server/internal/model"
)

func TestAnAllTimeSeriesIsBucketedByTheTimeThatIsRecordedNotByTheYear2000(t *testing.T) {
	srv, _ := houseServer(t) // two hours recorded on 22 Oct 2026
	const from, to = "2000-01-01T00:00:00Z", "2026-10-23T00:00:00Z"

	// Twelve hours from the first minute to the end of the period: hours, not the one-day bucket that 26 years would give (a single point).
	var s model.Series
	get(t, srv.URL+"/api/v1/series?meter_id=dryer&from="+from+"&to="+to, &s)
	if s.Bucket != model.BucketHour || len(s.Points) != 2 {
		t.Errorf("all time: bucket %q with %d points, want hour with 2", s.Bucket, len(s.Points))
	}

	// A bucket asked for is still the one used.
	var day model.Series
	get(t, srv.URL+"/api/v1/series?meter_id=dryer&bucket=day&from="+from+"&to="+to, &day)
	if day.Bucket != model.BucketDay {
		t.Errorf("asked for days, got %q", day.Bucket)
	}

	// A period that is all inside the history is chosen as before.
	var short model.Series
	get(t, srv.URL+"/api/v1/series?meter_id=dryer&from=2026-10-22T12:00:00Z&to=2026-10-22T14:00:00Z", &short)
	if short.Bucket != model.BucketMinute {
		t.Errorf("two hours: bucket %q, want minute", short.Bucket)
	}
}
