package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// Secrets (API keys) are kept in their own file beside the database, readable by the server's user alone (mode 0600), and not in the database:
// the database file is copied around (the nightly snapshots, a backup folder), and a key should not travel with the history. The server needs a
// key in the clear to use it, so this is not encryption; it is about who can read the file and what gets copied.
//
// A key saved by an earlier version lives in the meta table: the first time it is asked for it is moved here, and the meta row is emptied.

type secretFile struct {
	mu   sync.Mutex
	path string
}

func (f *secretFile) read() (map[string]string, error) {
	b, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// write replaces the file whole: a temporary file made private first, then renamed over it, so a crash never leaves a half-written key file
// and the contents are never readable by anyone else even for a moment.
func (f *secretFile) write(m map[string]string) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(f.path), ".secrets-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil && !errors.Is(err, os.ErrPermission) {
		// Windows has no such modes; anywhere with them a failure here matters.
		if runtime.GOOS != "windows" {
			tmp.Close()
			return err
		}
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, f.path)
}

// Secret is a saved secret, or false when there is none. A key still in the meta table from an earlier version is moved to the secrets file.
func (s *Store) Secret(ctx context.Context, key string) (string, bool, error) {
	s.secrets.mu.Lock()
	defer s.secrets.mu.Unlock()
	m, err := s.secrets.read()
	if err != nil {
		return "", false, err
	}
	if v, ok := m[key]; ok && v != "" {
		return v, true, nil
	}
	legacy, ok, err := s.Meta(ctx, key)
	if err != nil || !ok || legacy == "" {
		return "", false, err
	}
	m[key] = legacy
	if err := s.secrets.write(m); err != nil {
		return "", false, err
	}
	if err := s.SetMeta(ctx, key, ""); err != nil {
		return "", false, err
	}
	return legacy, true, nil
}

// SetSecret saves a secret; an empty value removes it (and any copy an earlier version left in the meta table).
func (s *Store) SetSecret(ctx context.Context, key, value string) error {
	s.secrets.mu.Lock()
	defer s.secrets.mu.Unlock()
	m, err := s.secrets.read()
	if err != nil {
		return err
	}
	if value == "" {
		delete(m, key)
	} else {
		m[key] = value
	}
	if err := s.secrets.write(m); err != nil {
		return err
	}
	return s.SetMeta(ctx, key, "")
}

// DataDir is the folder holding the database and the secrets file.
func (s *Store) DataDir() string { return filepath.Dir(s.secrets.path) }
