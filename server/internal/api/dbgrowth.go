package api

import (
	"context"
	"math"
	"net/http"
	"os"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/store"
)

// How fast the history database grows, for working out how much data a year comes to. The sizes are the server's own file, noted every few hours (the nightly
// snapshots are copies of it, so they are never read). The estimate is a straight line through the readings of the
// last two weeks; it is only an estimate (a new meter or a PC that reports its insides makes the real rate higher, and the file does not shrink when data is
// deleted, only when it is vacuumed).
const (
	sizeLogEvery = 6 * time.Hour
	// growthWindow is how far back the readings the estimate is made from go: long enough to smooth a busy day, short enough to follow a change.
	growthWindow = 14 * 24 * time.Hour
	// growthMinSpan is the least time the readings must cover before a rate is given: a day or two says nothing about a month.
	growthMinSpan = 3 * 24 * time.Hour
	growthMinN    = 3
	monthSeconds  = 30.4375 * 24 * 3600
	yearSeconds   = 365.25 * 24 * 3600
)

type dbSizePoint struct {
	At    time.Time `json:"at"`
	Bytes int64     `json:"bytes"`
}

type dbGrowth struct {
	// Points are every size recorded, oldest first.
	Points []dbSizePoint `json:"points"`
	// BytesNow is the database file's size now (without its write-ahead log); null when the server does not know where the file is.
	BytesNow *int64 `json:"bytes_now"`
	// PerMonthBytes is the estimated growth in a month, and InAYearBytes the size a year from now at that rate; null until the readings cover a few days.
	PerMonthBytes *int64 `json:"per_month_bytes"`
	InAYearBytes  *int64 `json:"in_a_year_bytes"`
	// Readings and BasisDays say what the estimate rests on: how many readings, over how many days.
	Readings  int      `json:"readings"`
	BasisDays *float64 `json:"basis_days"`
}

// growthRate is the slope, in bytes per second, of a straight line through the readings of the window ending at the latest one, and how many readings and
// what span of time it is made from. False when there are too few readings, or they cover too little time.
func growthRate(points []store.DBSize) (perSecond float64, n int, span time.Duration, ok bool) {
	if len(points) == 0 {
		return 0, 0, 0, false
	}
	last := points[len(points)-1].At
	var use []store.DBSize
	for _, p := range points {
		if !p.At.Before(last.Add(-growthWindow)) {
			use = append(use, p)
		}
	}
	n = len(use)
	if n < growthMinN {
		return 0, n, 0, false
	}
	span = use[n-1].At.Sub(use[0].At)
	if span < growthMinSpan {
		return 0, n, span, false
	}
	// Least squares of bytes on time, with time counted from the first reading so the sums stay small.
	var sx, sy, sxx, sxy float64
	for _, p := range use {
		x, y := p.At.Sub(use[0].At).Seconds(), float64(p.Bytes)
		sx, sy, sxx, sxy = sx+x, sy+y, sxx+x*x, sxy+x*y
	}
	d := float64(n)*sxx - sx*sx
	if d == 0 {
		return 0, n, span, false
	}
	return (float64(n)*sxy - sx*sy) / d, n, span, true
}

func (s *Server) dbGrowth(ctx context.Context) (dbGrowth, error) {
	sizes, err := s.Store.DBSizes(ctx)
	if err != nil {
		return dbGrowth{}, err
	}
	out := dbGrowth{Points: make([]dbSizePoint, 0, len(sizes))}
	for _, p := range sizes {
		out.Points = append(out.Points, dbSizePoint{At: p.At, Bytes: p.Bytes})
	}
	if s.DatabasePath != "" {
		if info, err := os.Stat(s.DatabasePath); err == nil {
			n := info.Size()
			out.BytesNow = &n
		}
	}
	rate, n, span, ok := growthRate(sizes)
	out.Readings = n
	if span > 0 {
		days := span.Hours() / 24
		out.BasisDays = &days
	}
	if !ok {
		return out, nil
	}
	// A file that shrank (it was vacuumed) is not negative growth.
	rate = math.Max(rate, 0)
	month := int64(math.Round(rate * monthSeconds))
	out.PerMonthBytes = &month
	now := sizes[len(sizes)-1].Bytes
	if out.BytesNow != nil {
		now = *out.BytesNow
	}
	year := now + int64(math.Round(rate*yearSeconds))
	out.InAYearBytes = &year
	return out, nil
}

func (s *Server) getDatabaseGrowth(w http.ResponseWriter, r *http.Request) {
	g, err := s.dbGrowth(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

// recordDBSize notes the database file's size now.
func (s *Server) recordDBSize(ctx context.Context) error {
	if s.DatabasePath == "" {
		return nil
	}
	info, err := os.Stat(s.DatabasePath)
	if err != nil {
		return nil // a file that cannot be read now is no reading
	}
	return s.Store.SaveDBSize(ctx, s.now(), info.Size())
}

// RunSizeLog records the database's size shortly after the server starts and every few hours after, until ctx ends. A failure is
// reported with logf and never stops it.
func (s *Server) RunSizeLog(ctx context.Context, logf func(format string, a ...any)) {
	wait := 5 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = sizeLogEvery
		if err := s.recordDBSize(ctx); err != nil && ctx.Err() == nil {
			logf("database sizes: %v", err)
		}
	}
}
