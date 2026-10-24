package store

import (
	"context"
	"errors"

	"github.com/gerp93/KVGuzzler/server/internal/model"
)

// A circuit can be the feed of a sub-panel: its figure is everything on that panel, and the circuits clamped inside the panel say they are
// part of it (parent_id). A plug, a computer's wall plug or any other channel that is not the mains says which circuit it is on the same way. The parent already contains its children, so a parent
// and its children are never added together; what the parent has that no child accounts for is shown as the remainder. A feed can itself be a
// circuit of a bigger panel, so the chain is as deep as the wiring is. A circuit with no parent is on the main panel, under the house.

// maxDepth stops a walk up the chain; a real house has two or three levels.
const maxDepth = 16

// SetChannelParent makes the channel part of the circuit parent, or (nil) takes it out again, and returns it.
func (s *Store) SetChannelParent(ctx context.Context, id string, parent *string) (model.Channel, error) {
	c, err := s.Channel(ctx, id)
	if err != nil {
		return model.Channel{}, err
	}
	if parent == nil || *parent == "" {
		if _, err := s.db.ExecContext(ctx, `UPDATE channels SET parent_id = NULL WHERE id = ?`, id); err != nil {
			return model.Channel{}, err
		}
		return s.Channel(ctx, id)
	}
	if c.Scope == model.ScopeMains {
		return model.Channel{}, invalid("The mains is the house itself: it is not inside anything.")
	}
	if *parent == id {
		return model.Channel{}, invalid("A circuit cannot be part of itself.")
	}
	p, err := s.Channel(ctx, *parent)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return model.Channel{}, invalid("There is no such circuit to put it under.")
		}
		return model.Channel{}, err
	}
	if p.Scope != model.ScopeCircuit {
		return model.Channel{}, invalid("A sub-panel's feed is a circuit.")
	}
	for _, x := range []model.Channel{c, p} {
		var owners int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM channels WHERE pair_with = ?`, x.ID).Scan(&owners); err != nil {
			return model.Channel{}, err
		}
		if owners > 0 {
			return model.Channel{}, conflict("%s is the second leg of another circuit, so it is not shown on its own.", x.Label)
		}
	}
	// The new parent may not be this circuit's own child, grandchild...: that would make a loop.
	for at, depth := p, 0; ; depth++ {
		if at.ParentID == nil {
			break
		}
		if *at.ParentID == id || depth > maxDepth {
			return model.Channel{}, conflict("%s is part of %s already, so it cannot hold it.", p.Label, c.Label)
		}
		if at, err = s.Channel(ctx, *at.ParentID); err != nil {
			if errors.Is(err, ErrNotFound) {
				break
			}
			return model.Channel{}, err
		}
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE channels SET parent_id = ? WHERE id = ?`, *parent, id); err != nil {
		return model.Channel{}, err
	}
	return s.Channel(ctx, id)
}

// forgetPlace takes a channel out of the sub-panel tree: it is no one's child and its own children become top-level circuits. Used when it
// stops being a circuit shown on its own (it became the mains, or the second leg of another circuit).
func (s *Store) forgetPlace(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE channels SET parent_id = NULL WHERE id = ? OR parent_id = ?`, id, id)
	return err
}
