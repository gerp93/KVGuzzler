package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gerp93/KVGuzzler/server/internal/model"
)

// The two ways a request about computers can be wrong, so the API can answer each with its own status. Their messages are
// fit to show the user.
var (
	ErrInvalid  = errors.New("invalid")
	ErrConflict = errors.New("conflict")
)

type kindError struct {
	kind error
	msg  string
}

func (e *kindError) Error() string        { return e.msg }
func (e *kindError) Is(target error) bool { return target == e.kind }

func invalid(format string, args ...any) error {
	return &kindError{kind: ErrInvalid, msg: fmt.Sprintf(format, args...)}
}

func conflict(format string, args ...any) error {
	return &kindError{kind: ErrConflict, msg: fmt.Sprintf(format, args...)}
}

const computersSchema = `
CREATE TABLE IF NOT EXISTS computers (
  id        TEXT PRIMARY KEY,
  name      TEXT NOT NULL,
  source_id TEXT UNIQUE
);`

const maxComputerName = 60

// querier is what a *sql.DB and a *sql.Tx both offer, so a helper can run inside a transaction or on its own.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func newComputerID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "comp-" + hex.EncodeToString(b)
}

func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxComputerName {
		return "", invalid("A computer's name is 1 to %d characters.", maxComputerName)
	}
	return name, nil
}

func nullable(s *string) any {
	if s == nil || *s == "" {
		return nil
	}
	return *s
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// CreateComputer adds a computer. sourceID, when given, is the PC collector that reports for it; one collector can report
// for only one computer.
func (s *Store) CreateComputer(ctx context.Context, name string, sourceID *string) (model.Computer, error) {
	name, err := cleanName(name)
	if err != nil {
		return model.Computer{}, err
	}
	id := newComputerID()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO computers(id, name, source_id) VALUES (?, ?, ?)`, id, name, nullable(sourceID)); err != nil {
		if isUnique(err) {
			return model.Computer{}, conflict("Another computer already uses that collector.")
		}
		return model.Computer{}, err
	}
	return s.Computer(ctx, id)
}

// ensureComputerForSource makes sure a computer reports through sourceID. The first time a PC collector pushes:
//   - if a computer already reports through it, nothing changes (a rename, or plugs tied to it, are left alone);
//   - if there is exactly one computer in all and it has no collector, the collector adopts it (the user made the computer and
//     tied its plugs first, and this is plainly that PC);
//   - otherwise a computer named after the collector is added, so there is something to tie plugs to. Two computers are never
//     merged on a guess.
func ensureComputerForSource(ctx context.Context, q querier, sourceID, name string) error {
	var have string
	err := q.QueryRowContext(ctx, `SELECT id FROM computers WHERE source_id = ?`, sourceID).Scan(&have)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	var total int
	var lone, loneSource sql.NullString
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*), MIN(id), MIN(source_id) FROM computers`).Scan(&total, &lone, &loneSource); err != nil {
		return err
	}
	if total == 1 && lone.Valid && !loneSource.Valid {
		_, err := q.ExecContext(ctx, `UPDATE computers SET source_id = ? WHERE id = ?`, sourceID, lone.String)
		return err
	}

	if strings.TrimSpace(name) == "" {
		name = sourceID
	}
	if utf8.RuneCountInString(name) > maxComputerName {
		name = string([]rune(name)[:maxComputerName])
	}
	_, err = q.ExecContext(ctx, `INSERT INTO computers(id, name, source_id) VALUES (?, ?, ?)`, newComputerID(), name, sourceID)
	return err
}

// Computers lists every computer with the plugs tied to it, by name.
func (s *Store) Computers(ctx context.Context) ([]model.Computer, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, source_id FROM computers`)
	if err != nil {
		return nil, err
	}
	byID := map[string]*model.Computer{}
	var order []string
	for rows.Next() {
		var c model.Computer
		var source sql.NullString
		if err := rows.Scan(&c.ID, &c.Name, &source); err != nil {
			rows.Close()
			return nil, err
		}
		if source.Valid {
			c.SourceID = &source.String
		}
		c.PeripheralMeterIDs = []string{}
		byID[c.ID] = &c
		order = append(order, c.ID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	chans, err := s.db.QueryContext(ctx, `SELECT computer_id, id, scope FROM meters WHERE computer_id IS NOT NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	for chans.Next() {
		var computerID, meterID, scope string
		if err := chans.Scan(&computerID, &meterID, &scope); err != nil {
			chans.Close()
			return nil, err
		}
		c := byID[computerID]
		if c == nil {
			continue // a plug that still points at a computer that was removed
		}
		id := meterID
		if model.Scope(scope) == model.ScopePC {
			c.WallMeterID = &id
		} else {
			c.PeripheralMeterIDs = append(c.PeripheralMeterIDs, id)
		}
	}
	chans.Close()
	if err := chans.Err(); err != nil {
		return nil, err
	}

	seen, err := s.db.QueryContext(ctx, `SELECT source_id, last_seen FROM pc_sources`)
	if err != nil {
		return nil, err
	}
	lastSeen := map[string]time.Time{}
	for seen.Next() {
		var id string
		var at int64
		if err := seen.Scan(&id, &at); err != nil {
			seen.Close()
			return nil, err
		}
		lastSeen[id] = time.Unix(at, 0).UTC()
	}
	seen.Close()
	if err := seen.Err(); err != nil {
		return nil, err
	}

	out := make([]model.Computer, 0, len(order))
	for _, id := range order {
		c := *byID[id]
		if c.SourceID != nil {
			if t, ok := lastSeen[*c.SourceID]; ok {
				c.LastSeen = &t
			}
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// Computer returns one computer, or ErrNotFound.
func (s *Store) Computer(ctx context.Context, id string) (model.Computer, error) {
	all, err := s.Computers(ctx)
	if err != nil {
		return model.Computer{}, err
	}
	for _, c := range all {
		if c.ID == id {
			return c, nil
		}
	}
	return model.Computer{}, ErrNotFound
}

// UpdateComputer renames a computer and/or changes its collector. Pass setSource to change the collector (a nil sourceID then
// lets it go).
func (s *Store) UpdateComputer(ctx context.Context, id string, name *string, setSource bool, sourceID *string) (model.Computer, error) {
	if _, err := s.Computer(ctx, id); err != nil {
		return model.Computer{}, err
	}
	if name != nil {
		clean, err := cleanName(*name)
		if err != nil {
			return model.Computer{}, err
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE computers SET name = ? WHERE id = ?`, clean, id); err != nil {
			return model.Computer{}, err
		}
	}
	if setSource {
		if _, err := s.db.ExecContext(ctx, `UPDATE computers SET source_id = ? WHERE id = ?`, nullable(sourceID), id); err != nil {
			if isUnique(err) {
				return model.Computer{}, conflict("Another computer already uses that collector.")
			}
			return model.Computer{}, err
		}
	}
	return s.Computer(ctx, id)
}

// DeleteComputer removes a computer. Its plugs stay, as ordinary plugs; what its collector reported is kept.
func (s *Store) DeleteComputer(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE meters SET computer_id = NULL WHERE computer_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM computers WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// Meter returns one meter, or ErrNotFound.
func (s *Store) Meter(ctx context.Context, id string) (model.Meter, error) {
	c, err := scanMeter(s.db.QueryRowContext(ctx, `SELECT `+meterCols+` FROM meters WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Meter{}, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	return c, s.settleTypes(ctx, &c)
}

// AssignMeter ties a plug to a computer, or lets it go (nil). Only a plug's meters can belong to a computer: a `pc`
// meter is its wall plug (one per computer), a `plug` meter is a peripheral.
func (s *Store) AssignMeter(ctx context.Context, meterID string, computerID *string) (model.Meter, error) {
	ch, err := s.Meter(ctx, meterID)
	if err != nil {
		return model.Meter{}, err
	}
	if computerID == nil || *computerID == "" {
		if _, err := s.db.ExecContext(ctx, `UPDATE meters SET computer_id = NULL WHERE id = ?`, meterID); err != nil {
			return model.Meter{}, err
		}
		return s.Meter(ctx, meterID)
	}

	if ch.Scope != model.ScopePC && ch.Scope != model.ScopePlug {
		return model.Meter{}, invalid("Only a smart plug can belong to a computer, not a %s meter.", ch.Scope)
	}
	if ch.GroupID != nil {
		return model.Meter{}, conflict("%s is part of a group. Take it out of the group first.", ch.Label)
	}
	comp, err := s.Computer(ctx, *computerID)
	if errors.Is(err, ErrNotFound) {
		return model.Meter{}, invalid("There is no such computer.")
	}
	if err != nil {
		return model.Meter{}, err
	}
	if ch.Scope == model.ScopePC && comp.WallMeterID != nil && *comp.WallMeterID != meterID {
		return model.Meter{}, conflict("%s is already on a plug that measures the whole PC. A computer has one.", comp.Name)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE meters SET computer_id = ? WHERE id = ?`, *computerID, meterID); err != nil {
		return model.Meter{}, err
	}
	return s.Meter(ctx, meterID)
}
