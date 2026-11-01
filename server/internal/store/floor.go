package store

import (
	"context"
	"database/sql"
	"strconv"
	"time"
)

// History starts on a day the owner picks: nothing recorded before it is looked at, whatever period is asked for (even "all time"), and its first moment
// counts as the start of history. The data is not deleted, so moving the day earlier or taking it off brings it all back. It is kept as the day (what the
// owner typed, `history_start` in the meta table) and as the instant that day starts in the server's zone (`history_start_at`, Unix seconds), which is what
// every read is cut at; the zone is the API's to know, so it works the instant out and hands both here.
const (
	historyStartKey   = "history_start"
	historyStartAtKey = "history_start_at"
)

type historyFloor struct {
	day string
	at  int64
}

// loadFloor reads the saved cutoff when the database is opened.
func (s *Store) loadFloor(ctx context.Context) error {
	day, ok, err := s.Meta(ctx, historyStartKey)
	if err != nil || !ok || day == "" {
		return err
	}
	text, ok, err := s.Meta(ctx, historyStartAtKey)
	if err != nil || !ok {
		return err
	}
	at, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return nil // a value that is not a time is no cutoff
	}
	s.floor.Store(&historyFloor{day: day, at: at})
	return nil
}

// HistoryStart is the day history starts on and the instant that day begins, or false when history is not cut.
func (s *Store) HistoryStart() (day string, at time.Time, ok bool) {
	f := s.floor.Load()
	if f == nil {
		return "", time.Time{}, false
	}
	return f.day, time.Unix(f.at, 0).UTC(), true
}

// SetHistoryStart cuts history at the instant `at`, the start of local day `day`. An empty day takes the cutoff off.
func (s *Store) SetHistoryStart(ctx context.Context, day string, at time.Time) error {
	if day == "" {
		if err := s.SetMeta(ctx, historyStartKey, ""); err != nil {
			return err
		}
		s.floor.Store(nil)
		return nil
	}
	if err := s.SetMeta(ctx, historyStartAtKey, strconv.FormatInt(at.Unix(), 10)); err != nil {
		return err
	}
	if err := s.SetMeta(ctx, historyStartKey, day); err != nil {
		return err
	}
	s.floor.Store(&historyFloor{day: day, at: at.Unix()})
	return nil
}

// clip is `from`, or the start of history when that is later: a period never reaches back before it.
func (s *Store) clip(from time.Time) time.Time {
	if f := s.floor.Load(); f != nil && from.Unix() < f.at {
		return time.Unix(f.at, 0).UTC()
	}
	return from
}

// floorUnix is the start of history in Unix seconds, 0 when it is not cut (every recorded minute is later).
func (s *Store) floorUnix() int64 {
	if f := s.floor.Load(); f != nil {
		return f.at
	}
	return 0
}

// floorDay is the day history starts on, "" when it is not cut (every day sorts after it).
func (s *Store) floorDay() string {
	if f := s.floor.Load(); f != nil {
		return f.day
	}
	return ""
}

// FirstDataEver is the first minute anything was recorded whatever the cutoff says, or false when nothing was.
func (s *Store) FirstDataEver(ctx context.Context) (time.Time, bool, error) {
	var first sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MIN(m) FROM (SELECT MIN(minute) AS m FROM minutes UNION ALL SELECT MIN(minute) FROM pc_minutes)`).Scan(&first); err != nil {
		return time.Time{}, false, err
	}
	if !first.Valid {
		return time.Time{}, false, nil
	}
	return time.Unix(first.Int64, 0).UTC(), true, nil
}
