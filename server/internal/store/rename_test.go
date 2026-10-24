package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// A database made when a meter was still called a channel: the table `channels` and the column `minutes.channel_id`, with every column the
// later migrations added.
const oldSchema = `
CREATE TABLE devices (id TEXT PRIMARY KEY, kind TEXT NOT NULL, host TEXT NOT NULL DEFAULT '', name TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '', removed INTEGER NOT NULL DEFAULT 0);
CREATE TABLE channels (
  id TEXT PRIMARY KEY, device_id TEXT NOT NULL, idx INTEGER, scope TEXT NOT NULL, label TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1,
  max_watts REAL, computer_id TEXT, group_id TEXT, invert INTEGER NOT NULL DEFAULT 0, pair_with TEXT, parent_id TEXT
);
CREATE TABLE minutes (channel_id TEXT NOT NULL, minute INTEGER NOT NULL, avg_w REAL NOT NULL, max_w REAL NOT NULL, seconds REAL NOT NULL, PRIMARY KEY (channel_id, minute)) WITHOUT ROWID;
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO meta(key, value) VALUES ('schema_version', '1');
INSERT INTO devices(id, kind, host, name, model) VALUES ('refoss-h', 'refoss', 'h', 'Garage panel', 'em16p');
INSERT INTO channels(id, device_id, idx, scope, label, enabled, max_watts, invert, pair_with, parent_id) VALUES
  ('refoss-h:em1', 'refoss-h', 1, 'circuit', 'Garage panel feed', 1, 30000, 0, 'refoss-h:em7', NULL),
  ('refoss-h:em2', 'refoss-h', 2, 'circuit', 'EV charger', 1, 30000, 1, NULL, 'refoss-h:em1'),
  ('refoss-h:em7', 'refoss-h', 7, 'circuit', 'B1', 0, 30000, 0, NULL, NULL);
INSERT INTO minutes(channel_id, minute, avg_w, max_w, seconds) VALUES
  ('refoss-h:em1', 1000020, 7400, 7500, 60),
  ('refoss-h:em1', 1000080, 7000, 7100, 60),
  ('refoss-h:em2', 1000020, 7300, 7400, 60);`

func rawDB(t *testing.T, path, script string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(script); err != nil {
		t.Fatal(err)
	}
}

func tableNames(t *testing.T, s *Store) map[string]bool {
	t.Helper()
	rows, err := s.db.Query(`SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		out[n] = true
	}
	return out
}

func TestADatabaseFromBeforeTheRenameKeepsEverythingUnderTheNewNames(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "house.db")
	rawDB(t, path, oldSchema)

	// Opening it twice is fine: the second time there is nothing left to move.
	for i := 0; i < 2; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("open %d: %v", i+1, err)
		}
		tables := tableNames(t, s)
		if tables["channels"] || !tables["meters"] {
			t.Errorf("open %d: tables = %v, want meters and no channels", i+1, tables)
		}
		ms, err := s.Meters(ctx)
		if err != nil || len(ms) != 3 {
			t.Fatalf("open %d: %d meters, %v", i+1, len(ms), err)
		}
		ev, err := s.Meter(ctx, "refoss-h:em2")
		if err != nil || ev.Label != "EV charger" || !ev.Invert || ev.ParentID == nil || *ev.ParentID != "refoss-h:em1" {
			t.Errorf("open %d: EV charger = %+v, %v", i+1, ev, err)
		}
		feed, _ := s.Meter(ctx, "refoss-h:em1")
		if feed.PairWith == nil || *feed.PairWith != "refoss-h:em7" || feed.MaxWatts == nil || *feed.MaxWatts != 30000 {
			t.Errorf("open %d: feed = %+v", i+1, feed)
		}
		tot, err := s.Totals(ctx, "refoss-h:em1", time.Unix(1000000, 0), time.Unix(1001000, 0))
		want := 7400.0*60 + 7000.0*60
		if i == 1 {
			want += 100 * 60 // the minute added after the first open
		}
		if err != nil || !near(tot.KWh, want/3.6e6) || tot.PeakWatts != 7500 {
			t.Errorf("open %d: history = %+v, %v", i+1, tot, err)
		}
		// New minutes keep landing in the same table.
		if err := s.AddMinute(ctx, "refoss-h:em1", time.Unix(1000140, 0), 100, 120, 60); err != nil {
			t.Errorf("open %d: %v", i+1, err)
		}
		s.Close()
	}
}

func TestAFreshDatabaseHasTheNewNamesAndNoOldOnes(t *testing.T) {
	s := open(t)
	tables := tableNames(t, s)
	if !tables["meters"] || tables["channels"] {
		t.Errorf("tables = %v", tables)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('minutes') WHERE name = 'meter_id'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("minutes.meter_id: %d, %v", n, err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('minutes') WHERE name = 'channel_id'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("minutes.channel_id: %d, %v", n, err)
	}
}

func TestAnEmptyMetersTableBesideTheOldOneIsReplacedButOneWithRowsIsNeverGuessedAt(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.db")
	rawDB(t, empty, oldSchema+`CREATE TABLE meters (id TEXT PRIMARY KEY, device_id TEXT NOT NULL, idx INTEGER, scope TEXT NOT NULL, label TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1, max_watts REAL);`)
	s, err := Open(empty)
	if err != nil {
		t.Fatalf("an empty meters beside channels: %v", err)
	}
	if ms, err := s.Meters(context.Background()); err != nil || len(ms) != 3 {
		t.Errorf("%d meters, %v", len(ms), err)
	}
	s.Close()

	both := filepath.Join(dir, "both.db")
	rawDB(t, both, oldSchema+`CREATE TABLE meters (id TEXT PRIMARY KEY, device_id TEXT NOT NULL, idx INTEGER, scope TEXT NOT NULL, label TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1, max_watts REAL);
INSERT INTO meters(id, device_id, scope, label) VALUES ('x', 'd', 'circuit', 'X');`)
	if s, err := Open(both); err == nil {
		s.Close()
		t.Error("two tables with rows should be refused, not merged by guesswork")
	}
}
