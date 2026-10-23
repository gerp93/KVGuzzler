package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"strings"

	"github.com/gerp93/KVGuzzler/server/internal/model"
)

// An appliance group is several plugs shown as one appliance, for something that runs through more than one (an entertainment
// center on two power strips). It changes only how they are shown: each plug still records its own energy, and a group's figure
// is the sum of its plugs' (all of one scope, so adding them is sound).
const groupsSchema = `
CREATE TABLE IF NOT EXISTS appliance_groups (
  id   TEXT PRIMARY KEY,
  name TEXT NOT NULL
);`

func newGroupID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "grp-" + hex.EncodeToString(b)
}

// CreateGroup adds an empty group.
func (s *Store) CreateGroup(ctx context.Context, name string) (model.ApplianceGroup, error) {
	name, err := cleanName(name)
	if err != nil {
		return model.ApplianceGroup{}, invalid("A group's name is 1 to %d characters.", maxComputerName)
	}
	id := newGroupID()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO appliance_groups(id, name) VALUES (?, ?)`, id, name); err != nil {
		return model.ApplianceGroup{}, err
	}
	return s.Group(ctx, id)
}

// Groups lists every group with its plugs, by name.
func (s *Store) Groups(ctx context.Context) ([]model.ApplianceGroup, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name FROM appliance_groups`)
	if err != nil {
		return nil, err
	}
	byID := map[string]*model.ApplianceGroup{}
	var order []string
	for rows.Next() {
		var g model.ApplianceGroup
		if err := rows.Scan(&g.ID, &g.Name); err != nil {
			rows.Close()
			return nil, err
		}
		g.ChannelIDs = []string{}
		byID[g.ID] = &g
		order = append(order, g.ID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	chans, err := s.db.QueryContext(ctx, `SELECT group_id, id FROM channels WHERE group_id IS NOT NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer chans.Close()
	for chans.Next() {
		var groupID, channelID string
		if err := chans.Scan(&groupID, &channelID); err != nil {
			return nil, err
		}
		if g := byID[groupID]; g != nil {
			g.ChannelIDs = append(g.ChannelIDs, channelID)
		}
	}
	if err := chans.Err(); err != nil {
		return nil, err
	}

	out := make([]model.ApplianceGroup, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// Group returns one group, or ErrNotFound.
func (s *Store) Group(ctx context.Context, id string) (model.ApplianceGroup, error) {
	all, err := s.Groups(ctx)
	if err != nil {
		return model.ApplianceGroup{}, err
	}
	for _, g := range all {
		if g.ID == id {
			return g, nil
		}
	}
	return model.ApplianceGroup{}, ErrNotFound
}

// RenameGroup changes a group's name.
func (s *Store) RenameGroup(ctx context.Context, id, name string) (model.ApplianceGroup, error) {
	if _, err := s.Group(ctx, id); err != nil {
		return model.ApplianceGroup{}, err
	}
	name, err := cleanName(name)
	if err != nil {
		return model.ApplianceGroup{}, invalid("A group's name is 1 to %d characters.", maxComputerName)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE appliance_groups SET name = ? WHERE id = ?`, name, id); err != nil {
		return model.ApplianceGroup{}, err
	}
	return s.Group(ctx, id)
}

// DeleteGroup removes a group. Its plugs stay, as ordinary plugs; what they recorded is untouched.
func (s *Store) DeleteGroup(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE channels SET group_id = NULL WHERE group_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM appliance_groups WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// AssignChannelToGroup puts a plug in a group, or takes it out (nil). Only an ordinary plug can be in a group (not a PC's own plug, a
// circuit or the mains), and not one that already belongs to a computer: a plug is shown in one place.
func (s *Store) AssignChannelToGroup(ctx context.Context, channelID string, groupID *string) (model.Channel, error) {
	ch, err := s.Channel(ctx, channelID)
	if err != nil {
		return model.Channel{}, err
	}
	if groupID == nil || *groupID == "" {
		if _, err := s.db.ExecContext(ctx, `UPDATE channels SET group_id = NULL WHERE id = ?`, channelID); err != nil {
			return model.Channel{}, err
		}
		return s.Channel(ctx, channelID)
	}

	if ch.Scope != model.ScopePlug {
		return model.Channel{}, invalid("Only an ordinary smart plug can be in a group, not a %s channel.", ch.Scope)
	}
	if ch.ComputerID != nil {
		return model.Channel{}, conflict("%s belongs to a computer. Take it out of the computer first.", ch.Label)
	}
	if _, err := s.Group(ctx, *groupID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return model.Channel{}, invalid("There is no such group.")
		}
		return model.Channel{}, err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE channels SET group_id = ? WHERE id = ?`, *groupID, channelID); err != nil {
		return model.Channel{}, err
	}
	return s.Channel(ctx, channelID)
}
