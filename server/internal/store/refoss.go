package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/gerp93/KVGuzzler/server/internal/model"
)

// How a Refoss monitor's clamps are wired is the user's to say, because the monitor cannot know: which way a clamp sits (its sign), and which
// two inputs are the two legs of one 240 V breaker (one circuit, the two added). A meter that has a partner reports both legs; the partner
// is switched off so it is not shown or counted on its own.

func (s *Store) isRefossMeter(ctx context.Context, ch model.Meter) (bool, error) {
	var kind string
	err := s.db.QueryRowContext(ctx, `SELECT kind FROM devices WHERE id = ?`, ch.DeviceID).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return kind == "refoss", err
}

// WiringChange is what to change about a Refoss meter; nil leaves a thing alone.
type WiringChange struct {
	Invert *bool
	// SetPair says PairWith is meant: a nil PairWith then unpairs.
	SetPair  bool
	PairWith *string
	// Type is what the meter is: the house (a monitor on the main panel measures the mains), a sub-panel's feed, or one circuit.
	Type *model.MeterType
}

// SetMeterWiring changes the sign, the pairing and the type of a Refoss meter and returns it.
func (s *Store) SetMeterWiring(ctx context.Context, id string, ch WiringChange) (model.Meter, error) {
	c, err := s.Meter(ctx, id)
	if err != nil {
		return model.Meter{}, err
	}
	isRefoss, err := s.isRefossMeter(ctx, c)
	if err != nil {
		return model.Meter{}, err
	}
	if !isRefoss {
		return model.Meter{}, invalid("Only a Refoss monitor's meters can have their clamp flipped, paired or given a type.")
	}

	if ch.Type != nil {
		// The house is the mains; a sub-panel's feed and a circuit are both circuits, told apart by the saved type.
		scope, saved := model.ScopeCircuit, any(string(*ch.Type))
		switch *ch.Type {
		case model.TypeHouse:
			scope, saved = model.ScopeMains, nil
		case model.TypeSubPanel, model.TypeCircuit:
		default:
			return model.Meter{}, invalid("A Refoss meter is the house, a sub-panel or a circuit.")
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE meters SET scope = ?, type = ? WHERE id = ?`, string(scope), saved, id); err != nil {
			return model.Meter{}, err
		}
		if scope == model.ScopeMains {
			if err := s.forgetPlace(ctx, id); err != nil {
				return model.Meter{}, err
			}
		}
	}
	if ch.Invert != nil {
		v := 0
		if *ch.Invert {
			v = 1
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE meters SET invert = ? WHERE id = ?`, v, id); err != nil {
			return model.Meter{}, err
		}
	}
	if ch.SetPair {
		if err := s.setPair(ctx, c, ch.PairWith); err != nil {
			return model.Meter{}, err
		}
	}
	return s.Meter(ctx, id)
}

func (s *Store) setPair(ctx context.Context, c model.Meter, with *string) error {
	// Let go of the old partner first: it comes back as a meter of its own.
	if c.PairWith != nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE meters SET enabled = 1 WHERE id = ?`, *c.PairWith); err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE meters SET pair_with = NULL WHERE id = ?`, c.ID); err != nil {
			return err
		}
	}
	if with == nil || *with == "" {
		return nil
	}
	if *with == c.ID {
		return invalid("A meter cannot be paired with itself.")
	}
	other, err := s.Meter(ctx, *with)
	if errors.Is(err, ErrNotFound) {
		return invalid("There is no such meter to pair with.")
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
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM meters WHERE pair_with = ? AND id <> ?`, other.ID, c.ID).Scan(&owners); err != nil {
		return err
	}
	if owners > 0 {
		return conflict("%s is already the second leg of another circuit. Unpair that one first.", other.Label)
	}
	var mine int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM meters WHERE pair_with = ?`, c.ID).Scan(&mine); err != nil {
		return err
	}
	if mine > 0 {
		return conflict("%s is already the second leg of another circuit, so it cannot have a partner of its own.", c.Label)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE meters SET pair_with = ? WHERE id = ?`, other.ID, c.ID); err != nil {
		return err
	}
	if _, err = s.db.ExecContext(ctx, `UPDATE meters SET enabled = 0 WHERE id = ?`, other.ID); err != nil {
		return err
	}
	return s.forgetPlace(ctx, other.ID) // the second leg is no longer a circuit of its own
}
