package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/gerp93/KVGuzzler/server/internal/model"
)

// How a Refoss monitor's clamps are wired is the user's to say, because the monitor cannot know: which way a clamp sits (its sign), and which
// two inputs are the two legs of one 240 V breaker (one circuit, the two added). A channel that has a partner reports both legs; the partner
// is switched off so it is not shown or counted on its own.

func (s *Store) isRefossChannel(ctx context.Context, ch model.Channel) (bool, error) {
	var kind string
	err := s.db.QueryRowContext(ctx, `SELECT kind FROM devices WHERE id = ?`, ch.DeviceID).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return kind == "refoss", err
}

// WiringChange is what to change about a Refoss channel; nil leaves a thing alone.
type WiringChange struct {
	Invert *bool
	// SetPair says PairWith is meant: a nil PairWith then unpairs.
	SetPair  bool
	PairWith *string
	// Scope is circuit or mains: a monitor on the main panel measures the mains, one on a sub-panel measures circuits.
	Scope *model.Scope
}

// SetChannelWiring changes the sign, the pairing and the scope of a Refoss channel and returns it.
func (s *Store) SetChannelWiring(ctx context.Context, id string, ch WiringChange) (model.Channel, error) {
	c, err := s.Channel(ctx, id)
	if err != nil {
		return model.Channel{}, err
	}
	isRefoss, err := s.isRefossChannel(ctx, c)
	if err != nil {
		return model.Channel{}, err
	}
	if !isRefoss {
		return model.Channel{}, invalid("Only a Refoss monitor's channels can have their clamp flipped, paired or given a scope.")
	}

	if ch.Scope != nil {
		if *ch.Scope != model.ScopeCircuit && *ch.Scope != model.ScopeMains {
			return model.Channel{}, invalid("A Refoss channel is a circuit or the mains.")
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE channels SET scope = ? WHERE id = ?`, string(*ch.Scope), id); err != nil {
			return model.Channel{}, err
		}
		if *ch.Scope == model.ScopeMains {
			if err := s.forgetPlace(ctx, id); err != nil {
				return model.Channel{}, err
			}
		}
	}
	if ch.Invert != nil {
		v := 0
		if *ch.Invert {
			v = 1
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE channels SET invert = ? WHERE id = ?`, v, id); err != nil {
			return model.Channel{}, err
		}
	}
	if ch.SetPair {
		if err := s.setPair(ctx, c, ch.PairWith); err != nil {
			return model.Channel{}, err
		}
	}
	return s.Channel(ctx, id)
}

func (s *Store) setPair(ctx context.Context, c model.Channel, with *string) error {
	// Let go of the old partner first: it comes back as a channel of its own.
	if c.PairWith != nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE channels SET enabled = 1 WHERE id = ?`, *c.PairWith); err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE channels SET pair_with = NULL WHERE id = ?`, c.ID); err != nil {
			return err
		}
	}
	if with == nil || *with == "" {
		return nil
	}
	if *with == c.ID {
		return invalid("A channel cannot be paired with itself.")
	}
	other, err := s.Channel(ctx, *with)
	if errors.Is(err, ErrNotFound) {
		return invalid("There is no such channel to pair with.")
	}
	if err != nil {
		return err
	}
	if other.DeviceID != c.DeviceID {
		return invalid("The two legs of a breaker must be on the same monitor.")
	}
	if other.PairWith != nil {
		return conflict("%s is already paired with another input. Unpair it first.", other.Label)
	}
	var owners int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM channels WHERE pair_with = ? AND id <> ?`, other.ID, c.ID).Scan(&owners); err != nil {
		return err
	}
	if owners > 0 {
		return conflict("%s is already the second leg of another circuit. Unpair that one first.", other.Label)
	}
	var mine int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM channels WHERE pair_with = ?`, c.ID).Scan(&mine); err != nil {
		return err
	}
	if mine > 0 {
		return conflict("%s is already the second leg of another circuit, so it cannot have a partner of its own.", c.Label)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE channels SET pair_with = ? WHERE id = ?`, other.ID, c.ID); err != nil {
		return err
	}
	if _, err = s.db.ExecContext(ctx, `UPDATE channels SET enabled = 0 WHERE id = ?`, other.ID); err != nil {
		return err
	}
	return s.forgetPlace(ctx, other.ID) // the second leg is no longer a circuit of its own
}
