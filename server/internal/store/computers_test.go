package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gerp93/KVGuzzler/server/internal/model"
	"github.com/gerp93/KVGuzzler/server/internal/pcrelay"
)

func str(s string) *string { return &s }

func addMeter(t *testing.T, s *Store, id string, scope model.Scope) {
	t.Helper()
	if err := s.UpsertMeter(context.Background(), model.Meter{ID: id, DeviceID: "d-" + id, Scope: scope, Label: id}); err != nil {
		t.Fatal(err)
	}
}

func TestComputersHaveOneWallPlugAndAnyNumberOfPeripherals(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	addMeter(t, s, "wall-1", model.ScopePC)
	addMeter(t, s, "wall-2", model.ScopePC)
	addMeter(t, s, "monitor", model.ScopePlug)
	addMeter(t, s, "speakers", model.ScopePlug)
	addMeter(t, s, "house-a", model.ScopeMains)
	addMeter(t, s, "dryer", model.ScopeCircuit)

	desk, err := s.CreateComputer(ctx, "  Desktop ", nil)
	if err != nil || desk.Name != "Desktop" || !strings.HasPrefix(desk.ID, "comp-") || desk.PeripheralMeterIDs == nil {
		t.Fatalf("desk = %+v, %v", desk, err)
	}
	laptop, err := s.CreateComputer(ctx, "Laptop", nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, ch := range []string{"wall-1", "monitor", "speakers"} {
		if _, err := s.AssignMeter(ctx, ch, &desk.ID); err != nil {
			t.Fatalf("assigning %s: %v", ch, err)
		}
	}
	if _, err := s.AssignMeter(ctx, "wall-2", &desk.ID); !errors.Is(err, ErrConflict) {
		t.Errorf("a second wall plug on one computer: %v, want ErrConflict", err)
	}
	if _, err := s.AssignMeter(ctx, "wall-2", &laptop.ID); err != nil {
		t.Errorf("the other computer's wall plug: %v", err)
	}
	if _, err := s.AssignMeter(ctx, "wall-1", &desk.ID); err != nil {
		t.Errorf("re-assigning the same wall plug to its own computer: %v", err)
	}
	for _, ch := range []string{"house-a", "dryer"} {
		if _, err := s.AssignMeter(ctx, ch, &desk.ID); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v, want ErrInvalid (only plugs can belong to a computer)", ch, err)
		}
	}
	if _, err := s.AssignMeter(ctx, "monitor", str("comp-nope")); !errors.Is(err, ErrInvalid) {
		t.Errorf("unknown computer: %v, want ErrInvalid", err)
	}
	if _, err := s.AssignMeter(ctx, "nope", &desk.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown meter: %v, want ErrNotFound", err)
	}

	all, _ := s.Computers(ctx)
	if len(all) != 2 || all[0].Name != "Desktop" || all[1].Name != "Laptop" {
		t.Fatalf("computers = %+v, want Desktop then Laptop", all)
	}
	got := all[0]
	if got.WallMeterID == nil || *got.WallMeterID != "wall-1" || len(got.PeripheralMeterIDs) != 2 || got.PeripheralMeterIDs[0] != "monitor" {
		t.Errorf("desktop = %+v", got)
	}
	ch, _ := s.Meter(ctx, "monitor")
	if ch.ComputerID == nil || *ch.ComputerID != desk.ID {
		t.Errorf("the meter should say which computer it belongs to: %+v", ch)
	}

	// Letting a plug go, and removing a computer: its plugs stay, as ordinary plugs.
	if _, err := s.AssignMeter(ctx, "speakers", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteComputer(ctx, desk.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteComputer(ctx, desk.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting twice: %v", err)
	}
	for _, id := range []string{"wall-1", "monitor", "speakers"} {
		if c, _ := s.Meter(ctx, id); c.ComputerID != nil {
			t.Errorf("%s still belongs to a removed computer", id)
		}
	}
}

func TestComputerNamesAndCollectorsAreChecked(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	for _, name := range []string{"", "   ", strings.Repeat("x", 61)} {
		if _, err := s.CreateComputer(ctx, name, nil); !errors.Is(err, ErrInvalid) {
			t.Errorf("name %q: %v, want ErrInvalid", name, err)
		}
	}
	a, err := s.CreateComputer(ctx, "A", str("pc-1"))
	if err != nil || a.SourceID == nil || *a.SourceID != "pc-1" {
		t.Fatalf("%+v, %v", a, err)
	}
	if _, err := s.CreateComputer(ctx, "B", str("pc-1")); !errors.Is(err, ErrConflict) {
		t.Errorf("a second computer on the same collector: %v, want ErrConflict", err)
	}
	b, _ := s.CreateComputer(ctx, "B", nil)
	if _, err := s.UpdateComputer(ctx, b.ID, nil, true, str("pc-1")); !errors.Is(err, ErrConflict) {
		t.Errorf("taking another computer's collector: %v, want ErrConflict", err)
	}
	renamed, err := s.UpdateComputer(ctx, a.ID, str("Desktop"), true, nil) // rename and let go of the collector
	if err != nil || renamed.Name != "Desktop" || renamed.SourceID != nil {
		t.Errorf("%+v, %v", renamed, err)
	}
	if _, err := s.UpdateComputer(ctx, b.ID, nil, true, str("pc-1")); err != nil {
		t.Errorf("the freed collector should be usable by another computer: %v", err)
	}
	if _, err := s.UpdateComputer(ctx, "comp-nope", str("x"), false, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown computer: %v", err)
	}
}

func TestAFirstPushAddsAComputerOnceAndKeepsAUsersEdits(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	at := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	in := pcrelay.Ingest{APIVersion: 1, SourceID: "pc-7f3a", SourceName: "Desktop", Minutes: []pcrelay.Minute{pcMinute(at, 100)}}

	for i := 0; i < 3; i++ {
		if _, err := s.IngestPC(ctx, in, at.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	all, _ := s.Computers(ctx)
	if len(all) != 1 || all[0].Name != "Desktop" || all[0].SourceID == nil || *all[0].SourceID != "pc-7f3a" || all[0].LastSeen == nil {
		t.Fatalf("computers = %+v, want one, named after the collector, with a last-seen time", all)
	}

	// The user renames it; the next push must not undo that or add another.
	if _, err := s.UpdateComputer(ctx, all[0].ID, str("Gaming PC"), false, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.IngestPC(ctx, in, at.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	all, _ = s.Computers(ctx)
	if len(all) != 1 || all[0].Name != "Gaming PC" {
		t.Errorf("computers = %+v", all)
	}

	// A collector with no name is added under its id.
	anon := pcrelay.Ingest{APIVersion: 1, SourceID: "pc-anon"}
	if _, err := s.IngestPC(ctx, anon, at); err != nil {
		t.Fatal(err)
	}
	all, _ = s.Computers(ctx)
	if len(all) != 2 {
		t.Fatalf("computers = %+v", all)
	}
	found := false
	for _, c := range all {
		found = found || c.Name == "pc-anon"
	}
	if !found {
		t.Errorf("an unnamed collector should be added under its id: %+v", all)
	}
}

func TestOpeningADatabaseFromBeforePlugsCouldBelongToAComputer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`CREATE TABLE meters (id TEXT PRIMARY KEY, device_id TEXT NOT NULL, idx INTEGER, scope TEXT NOT NULL, label TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1, max_watts REAL);
INSERT INTO meters(id, device_id, scope, label) VALUES ('shelly-1:plug', 'shelly-1', 'pc', 'PC');`); err != nil {
		t.Fatal(err)
	}
	old.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	chs, err := s.Meters(context.Background())
	if err != nil || len(chs) != 1 || chs[0].ComputerID != nil {
		t.Fatalf("meters = %+v, %v", chs, err)
	}
	comp, _ := s.CreateComputer(context.Background(), "Desktop", nil)
	if _, err := s.AssignMeter(context.Background(), "shelly-1:plug", &comp.ID); err != nil {
		t.Errorf("assigning on a migrated database: %v", err)
	}
}

func TestAFirstPushAdoptsTheOnlyComputerThatHasNoCollectorButNeverGuessesBetweenTwo(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	push := func(s *Store, id, name string) {
		t.Helper()
		in := pcrelay.Ingest{APIVersion: 1, SourceID: id, SourceName: name, Minutes: []pcrelay.Minute{pcMinute(at, 100)}}
		if _, err := s.IngestPC(ctx, in, at.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}

	// The user made a computer and tied its plugs: the collector for that PC adopts it, and nothing is duplicated.
	s := open(t)
	addMeter(t, s, "wall", model.ScopePC)
	mine, _ := s.CreateComputer(ctx, "kgerp_PC", nil)
	if _, err := s.AssignMeter(ctx, "wall", &mine.ID); err != nil {
		t.Fatal(err)
	}
	push(s, "pc-1", "KGERP-PC")
	all, _ := s.Computers(ctx)
	if len(all) != 1 || all[0].ID != mine.ID || all[0].SourceID == nil || *all[0].SourceID != "pc-1" || all[0].Name != "kgerp_PC" || all[0].WallMeterID == nil {
		t.Fatalf("computers = %+v, want the user's computer, now reporting through pc-1, with its name and plug kept", all)
	}

	// A second PC's collector must not take it over: it gets a computer of its own.
	push(s, "pc-2", "Laptop")
	all, _ = s.Computers(ctx)
	if len(all) != 2 {
		t.Fatalf("computers = %+v, want a new one for the second collector", all)
	}

	// Two computers with no collector: never guess which one a collector is.
	s2 := open(t)
	if _, err := s2.CreateComputer(ctx, "A", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.CreateComputer(ctx, "B", nil); err != nil {
		t.Fatal(err)
	}
	push(s2, "pc-9", "Whatever")
	all, _ = s2.Computers(ctx)
	if len(all) != 3 {
		t.Errorf("computers = %+v, want a third, named after the collector", all)
	}
	for _, c := range all {
		if (c.Name == "A" || c.Name == "B") && c.SourceID != nil {
			t.Errorf("%s was adopted on a guess", c.Name)
		}
	}
}
