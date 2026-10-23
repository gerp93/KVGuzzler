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

func TestGroupsHoldOrdinaryPlugsAndAPlugIsShownInOnePlace(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	addChannel(t, s, "strip-1", model.ScopePlug)
	addChannel(t, s, "strip-2", model.ScopePlug)
	addChannel(t, s, "desk-plug", model.ScopePlug)
	addChannel(t, s, "pc-wall", model.ScopePC)
	addChannel(t, s, "dryer", model.ScopeCircuit)

	tv, err := s.CreateGroup(ctx, "  Entertainment center ")
	if err != nil || tv.Name != "Entertainment center" || !strings.HasPrefix(tv.ID, "grp-") || tv.ChannelIDs == nil || len(tv.ChannelIDs) != 0 {
		t.Fatalf("group = %+v, %v", tv, err)
	}
	for _, ch := range []string{"strip-1", "strip-2"} {
		got, err := s.AssignChannelToGroup(ctx, ch, &tv.ID)
		if err != nil || got.GroupID == nil || *got.GroupID != tv.ID {
			t.Fatalf("assigning %s: %+v, %v", ch, got, err)
		}
	}
	if _, err := s.AssignChannelToGroup(ctx, "strip-1", &tv.ID); err != nil {
		t.Errorf("putting a plug in the group it is already in: %v", err)
	}
	for _, ch := range []string{"pc-wall", "dryer"} {
		if _, err := s.AssignChannelToGroup(ctx, ch, &tv.ID); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v, want ErrInvalid (only an ordinary plug can be in a group)", ch, err)
		}
	}
	if _, err := s.AssignChannelToGroup(ctx, "strip-1", str("grp-nope")); !errors.Is(err, ErrInvalid) {
		t.Errorf("unknown group: %v, want ErrInvalid", err)
	}
	if _, err := s.AssignChannelToGroup(ctx, "nope", &tv.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown channel: %v, want ErrNotFound", err)
	}

	// A plug is shown in one place: a computer's plug cannot join a group, and a grouped plug cannot join a computer.
	desk, _ := s.CreateComputer(ctx, "Desktop", nil)
	if _, err := s.AssignChannel(ctx, "desk-plug", &desk.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AssignChannelToGroup(ctx, "desk-plug", &tv.ID); !errors.Is(err, ErrConflict) {
		t.Errorf("a computer's plug in a group: %v, want ErrConflict", err)
	}
	if _, err := s.AssignChannel(ctx, "strip-1", &desk.ID); !errors.Is(err, ErrConflict) {
		t.Errorf("a grouped plug on a computer: %v, want ErrConflict", err)
	}

	groups, _ := s.Groups(ctx)
	if len(groups) != 1 || len(groups[0].ChannelIDs) != 2 || groups[0].ChannelIDs[0] != "strip-1" {
		t.Errorf("groups = %+v", groups)
	}

	// Taking a plug out, renaming, and removing the group: its plugs stay, as ordinary plugs.
	if _, err := s.AssignChannelToGroup(ctx, "strip-2", nil); err != nil {
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
	if c, _ := s.Channel(ctx, "strip-1"); c.GroupID != nil {
		t.Error("a plug still belongs to a removed group")
	}
}

func TestOpeningADatabaseFromBeforePlugsCouldBeGrouped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`CREATE TABLE channels (id TEXT PRIMARY KEY, device_id TEXT NOT NULL, idx INTEGER, scope TEXT NOT NULL, label TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1, max_watts REAL, computer_id TEXT);
INSERT INTO channels(id, device_id, scope, label) VALUES ('shelly-1:plug', 'shelly-1', 'plug', 'Strip');`); err != nil {
		t.Fatal(err)
	}
	old.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	g, _ := s.CreateGroup(context.Background(), "TV")
	if _, err := s.AssignChannelToGroup(context.Background(), "shelly-1:plug", &g.ID); err != nil {
		t.Errorf("grouping on a migrated database: %v", err)
	}
}
