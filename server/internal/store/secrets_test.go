package store

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestASecretIsSavedAndRemovedAndNeverGoesIntoTheDatabase(t *testing.T) {
	ctx := t.Context()
	s := open(t)
	if _, ok, err := s.Secret(ctx, "k"); ok || err != nil {
		t.Fatalf("a fresh store has no secret: %v %v", ok, err)
	}
	if err := s.SetSecret(ctx, "k", "s3cr3t-value"); err != nil {
		t.Fatal(err)
	}
	if v, ok, _ := s.Secret(ctx, "k"); !ok || v != "s3cr3t-value" {
		t.Errorf("secret = %q %v", v, ok)
	}
	if v, ok, _ := s.Meta(ctx, "k"); ok && v != "" {
		t.Errorf("the database has it: %q", v)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(s.DataDir(), "secrets.json"))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("secrets file: %v, mode %v, want 0600", err, info.Mode())
		}
	}
	if err := s.SetSecret(ctx, "k", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Secret(ctx, "k"); ok {
		t.Error("an emptied secret should be gone")
	}
}

func TestAKeyLeftInTheDatabaseByAnEarlierVersionIsMovedOutTheFirstTimeItIsRead(t *testing.T) {
	ctx := t.Context()
	s := open(t)
	if err := s.SetMeta(ctx, "openei_key", "legacy-key-12345"); err != nil {
		t.Fatal(err)
	}
	if v, ok, err := s.Secret(ctx, "openei_key"); err != nil || !ok || v != "legacy-key-12345" {
		t.Fatalf("legacy = %q %v %v", v, ok, err)
	}
	if v, _, _ := s.Meta(ctx, "openei_key"); v != "" {
		t.Errorf("the old copy is still in the database: %q", v)
	}
	b, _ := os.ReadFile(filepath.Join(s.DataDir(), "secrets.json"))
	if !strings.Contains(string(b), "legacy-key-12345") {
		t.Errorf("secrets file = %s", b)
	}
	if v, ok, _ := s.Secret(ctx, "openei_key"); !ok || v != "legacy-key-12345" { // and it still reads from there
		t.Errorf("after the move: %q %v", v, ok)
	}
}

func TestSeveralSecretsKeepApartAndADamagedFileIsAnErrorNotASilentLoss(t *testing.T) {
	ctx := t.Context()
	s := open(t)
	_ = s.SetSecret(ctx, "a", "one-value-1")
	_ = s.SetSecret(ctx, "b", "two-value-2")
	_ = s.SetSecret(ctx, "a", "")
	if v, ok, _ := s.Secret(ctx, "b"); !ok || v != "two-value-2" {
		t.Errorf("b = %q %v", v, ok)
	}
	if err := os.WriteFile(filepath.Join(s.DataDir(), "secrets.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Secret(ctx, "b"); err == nil {
		t.Error("a damaged secrets file should be reported, not read as empty")
	}
	if err := s.SetSecret(ctx, "c", "three-value-3"); err == nil {
		t.Error("saving over a damaged file must not quietly throw away what it held")
	}
}
