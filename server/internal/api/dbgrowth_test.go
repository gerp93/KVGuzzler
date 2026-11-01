package api

import (
	"context"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/store"
)

const mb = 1 << 20

// sizesFrom makes one reading a day from `start`, each of the given sizes.
func sizesFrom(start time.Time, bytes ...int64) []store.DBSize {
	out := make([]store.DBSize, len(bytes))
	for i, b := range bytes {
		out[i] = store.DBSize{At: start.AddDate(0, 0, i), Bytes: b}
	}
	return out
}

func TestGrowthIsTheStraightLineThroughTheLastTwoWeeksOfSizes(t *testing.T) {
	start := time.Date(2026, 10, 1, 3, 30, 0, 0, time.UTC)
	// 2 MB a day, for ten days.
	var b []int64
	for i := 0; i < 10; i++ {
		b = append(b, int64(10+2*i)*mb)
	}
	rate, n, span, ok := growthRate(sizesFrom(start, b...))
	if !ok || n != 10 || span != 9*24*time.Hour {
		t.Fatalf("rate ok=%v n=%d span=%v, want ok over 10 readings and 9 days", ok, n, span)
	}
	if perDay := rate * 86400; math.Abs(perDay-2*mb) > 1 {
		t.Errorf("%v bytes a day, want %d", perDay, 2*mb)
	}

	// Only the last two weeks count: a long flat stretch and then growth gives the growth, not the average.
	var old []int64
	for i := 0; i < 30; i++ {
		old = append(old, 100*mb)
	}
	for i := 0; i < 15; i++ {
		old = append(old, int64(100+3*i)*mb)
	}
	rate, n, _, ok = growthRate(sizesFrom(start, old...))
	if !ok || n != 15 || math.Abs(rate*86400-3*mb) > 1 {
		t.Errorf("recent growth: ok=%v n=%d %v bytes a day, want 15 readings at %d", ok, n, rate*86400, 3*mb)
	}
}

func TestGrowthIsNotGuessedFromAFewReadingsOrAFewDays(t *testing.T) {
	start := time.Date(2026, 10, 1, 3, 30, 0, 0, time.UTC)
	if _, _, _, ok := growthRate(nil); ok {
		t.Error("no readings gave a rate")
	}
	if _, n, _, ok := growthRate(sizesFrom(start, 10*mb, 20*mb)); ok || n != 2 {
		t.Errorf("two readings: ok=%v n=%d, want no rate", ok, n)
	}
	// Plenty of readings but only a day and a half.
	var close []store.DBSize
	for i := 0; i < 8; i++ {
		close = append(close, store.DBSize{At: start.Add(time.Duration(i) * 5 * time.Hour), Bytes: int64(10+i) * mb})
	}
	if _, n, _, ok := growthRate(close); ok || n != 8 {
		t.Errorf("a day and a half: ok=%v n=%d, want no rate", ok, n)
	}
}

func TestTheDatabasesOwnSizeIsRecordedOncePerMoment(t *testing.T) {
	f := newFordTest(t)
	url := f.srv.URL + "/api/v1/database/growth"

	var empty dbGrowth
	if r := get(t, url, &empty); r.StatusCode != http.StatusOK || len(empty.Points) != 0 || empty.PerMonthBytes != nil || empty.InAYearBytes != nil || empty.BytesNow != nil {
		t.Fatalf("with nothing recorded: %d %+v, want no points and no estimate", r.StatusCode, empty)
	}

	f.api.DatabasePath = filepath.Join(t.TempDir(), "house.db")
	if err := os.WriteFile(f.api.DatabasePath, make([]byte, 5*mb), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ { // a second note at the same moment changes nothing
		if err := f.api.recordDBSize(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	var one dbGrowth
	get(t, url, &one)
	if len(one.Points) != 1 || one.Points[0].Bytes != 5*mb || one.BytesNow == nil || *one.BytesNow != 5*mb || one.PerMonthBytes != nil {
		t.Fatalf("one reading: %+v, want it listed, the size now, and no estimate yet", one)
	}
}

func TestTheGrowthComesToAMonthAndToTheSizeInAYear(t *testing.T) {
	f := newFordTest(t)
	ctx := context.Background()
	url := f.srv.URL + "/api/v1/database/growth"

	// Ten days at 2 MB a day, then the file as it is now (the test clock is half a day on), still on the same line.
	start := time.Date(2026, 10, 12, 3, 30, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		if err := f.st.SaveDBSize(ctx, start.AddDate(0, 0, i), int64(5+2*i)*mb); err != nil {
			t.Fatal(err)
		}
	}
	f.api.DatabasePath = filepath.Join(t.TempDir(), "house.db")
	if err := os.WriteFile(f.api.DatabasePath, make([]byte, 26*mb), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := f.api.recordDBSize(ctx); err != nil {
		t.Fatal(err)
	}
	var got dbGrowth
	get(t, url, &got)
	if got.PerMonthBytes == nil || got.InAYearBytes == nil || got.BasisDays == nil || got.Readings != 11 {
		t.Fatalf("after ten days: %+v, want an estimate from 11 readings", got)
	}
	if want := 2 * mb * 30.4375; math.Abs(float64(*got.PerMonthBytes)-want) > 0.03*want {
		t.Errorf("a month = %d bytes, want about %.0f", *got.PerMonthBytes, want)
	}
	if want := 26*mb + 2*mb*365.25; math.Abs(float64(*got.InAYearBytes)-want) > 0.03*want {
		t.Errorf("in a year = %d bytes, want about %.0f", *got.InAYearBytes, want)
	}
}
