package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gerp93/KVGuzzler/server/internal/model"
)

func TestAGroupIsALogicalGroupingOfAnyMeterExceptTheMains(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	addMeter(t, s, "strip-1", model.ScopePlug)
	addMeter(t, s, "strip-2", model.ScopePlug)
	addMeter(t, s, "desk-plug", model.ScopePlug)
	addMeter(t, s, "pc-wall", model.ScopePC)
	addMeter(t, s, "dryer", model.ScopeCircuit)
	addMeter(t, s, "mains-a", model.ScopeMains)

	tv, err := s.CreateGroup(ctx, "  Entertainment center ")
	if err != nil || tv.Name != "Entertainment center" || !strings.HasPrefix(tv.ID, "grp-") || tv.MeterIDs == nil || len(tv.MeterIDs) != 0 {
		t.Fatalf("group = %+v, %v", tv, err)
	}
	for _, ch := range []string{"strip-1", "strip-2"} {
		got, err := s.AssignMeterToGroup(ctx, ch, &tv.ID)
		if err != nil || got.GroupID == nil || *got.GroupID != tv.ID {
			t.Fatalf("assigning %s: %+v, %v", ch, got, err)
		}
	}
	if _, err := s.AssignMeterToGroup(ctx, "strip-1", &tv.ID); err != nil {
		t.Errorf("putting a plug in the group it is already in: %v", err)
	}
	// Any meter can be grouped, whatever it is: a circuit, a computer's plug. Only the mains is the house itself.
	for _, ch := range []string{"pc-wall", "dryer"} {
		if _, err := s.AssignMeterToGroup(ctx, ch, &tv.ID); err != nil {
			t.Errorf("%s in a group: %v", ch, err)
		}
	}
	if _, err := s.AssignMeterToGroup(ctx, "mains-a", &tv.ID); !errors.Is(err, ErrInvalid) {
		t.Errorf("the mains in a group: %v, want ErrInvalid", err)
	}
	if _, err := s.AssignMeterToGroup(ctx, "strip-1", str("grp-nope")); !errors.Is(err, ErrInvalid) {
		t.Errorf("unknown group: %v, want ErrInvalid", err)
	}
	if _, err := s.AssignMeterToGroup(ctx, "nope", &tv.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown meter: %v, want ErrNotFound", err)
	}

	// A group is only a grouping: a computer's plug can be in one, and a grouped plug can belong to a computer.
	desk, _ := s.CreateComputer(ctx, "Desktop", nil)
	if _, err := s.AssignMeter(ctx, "desk-plug", &desk.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AssignMeterToGroup(ctx, "desk-plug", &tv.ID); err != nil {
		t.Errorf("a computer's plug in a group: %v", err)
	}
	if _, err := s.AssignMeter(ctx, "strip-1", &desk.ID); err != nil {
		t.Errorf("a grouped plug on a computer: %v", err)
	}

	groups, _ := s.Groups(ctx)
	if len(groups) != 1 || len(groups[0].MeterIDs) != 5 {
		t.Errorf("groups = %+v, want one group of the five", groups)
	}

	// Taking a plug out, renaming, and removing the group: its plugs stay, as ordinary plugs.
	if _, err := s.AssignMeterToGroup(ctx, "strip-2", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RenameGroup(ctx, tv.ID, "Living room"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RenameGroup(ctx, tv.ID, "   "); !errors.Is(err, ErrInvalid) {
		t.Errorf("a blank name: %v", err)
	}
	if err := s.DeleteGroup(ctx, tv.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteGroup(ctx, tv.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting twice: %v", err)
	}
	if c, _ := s.Meter(ctx, "strip-1"); c.GroupID != nil {
		t.Error("a plug still belongs to a removed group")
	}
}

func TestOpeningADatabaseFromBeforePlugsCouldBeGrouped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`CREATE TABLE meters (id TEXT PRIMARY KEY, device_id TEXT NOT NULL, idx INTEGER, scope TEXT NOT NULL, label TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1, max_watts REAL, computer_id TEXT);
INSERT INTO meters(id, device_id, scope, label) VALUES ('shelly-1:plug', 'shelly-1', 'plug', 'Strip');`); err != nil {
		t.Fatal(err)
	}
	old.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	g, _ := s.CreateGroup(context.Background(), "TV")
	if _, err := s.AssignMeterToGroup(context.Background(), "shelly-1:plug", &g.ID); err != nil {
		t.Errorf("grouping on a migrated database: %v", err)
	}
}
