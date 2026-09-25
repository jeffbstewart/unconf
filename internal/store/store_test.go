package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jeffbstewart/unconf/internal/domain"
)

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func TestMigrateCreatesSchemaAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s, path := openTemp(t)
	var version, tables int
	s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version)
	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table'").Scan(&tables)
	if version != 1 || tables != 14 {
		t.Fatalf("user_version=%d tables=%d", version, tables)
	}
	s.Close()

	again, err := Open(ctx, path) // re-running migrations must be a no-op
	if err != nil {
		t.Fatal(err)
	}
	again.Close()
}

func TestPragmas(t *testing.T) {
	s, _ := openTemp(t)
	var mode string
	var fk int
	s.db.QueryRow("PRAGMA journal_mode").Scan(&mode)
	s.db.QueryRow("PRAGMA foreign_keys").Scan(&fk)
	if mode != "wal" || fk != 1 {
		t.Fatalf("journal_mode=%s foreign_keys=%d", mode, fk)
	}
}

func TestMeta(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)
	if _, err := s.Meta(ctx, "k"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	v, err := s.MetaOrInit(ctx, "k", func() string { return "first" })
	if err != nil || v != "first" {
		t.Fatalf("got %q, %v", v, err)
	}
	v, _ = s.MetaOrInit(ctx, "k", func() string { return "second" })
	if v != "first" {
		t.Fatalf("MetaOrInit overwrote: %q", v)
	}
	s.SetMeta(ctx, "k", "third")
	if v, _ := s.Meta(ctx, "k"); v != "third" {
		t.Fatalf("SetMeta: %q", v)
	}
}

func TestEnsureDefaultEventPersists(t *testing.T) {
	ctx := context.Background()
	s, path := openTemp(t)
	e, err := s.EnsureDefaultEvent(ctx, "Camp")
	if err != nil {
		t.Fatal(err)
	}
	if e.Name != "Camp" || e.Lifecycle != "setup" || e.VotesPerUser != 5 || e.VotingOpen {
		t.Fatalf("unexpected defaults: %+v", e)
	}
	s.Close()

	s2, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	e2, err := s2.EnsureDefaultEvent(ctx, "Renamed")
	if err != nil || e2.ID != e.ID || e2.Name != "Camp" {
		t.Fatalf("second run should reuse event: %+v, %v", e2, err)
	}
}

func TestUsers(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)
	ev, _ := s.EnsureDefaultEvent(ctx, "E")
	u := User{ID: domain.NewID(), EventID: ev.ID, Name: "Ada", Role: domain.RoleParticipant,
		CreatedAt: domain.Timestamp(time.Now())}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	dup := u
	dup.ID, dup.Name = domain.NewID(), "ADA"
	if err := s.CreateUser(ctx, dup); !errors.Is(err, ErrConflict) {
		t.Fatalf("case-insensitive duplicate: want ErrConflict, got %v", err)
	}

	got, err := s.UserByName(ctx, ev.ID, "aDa")
	if err != nil || got.ID != u.ID || got.Email != "" {
		t.Fatalf("UserByName: %+v, %v", got, err)
	}
	if err := s.SetUserEmail(ctx, u.ID, "ada@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUserRole(ctx, u.ID, domain.RoleOrganizer); err != nil {
		t.Fatal(err)
	}
	got, _ = s.UserByID(ctx, u.ID)
	if got.Email != "ada@example.com" || got.Role != domain.RoleOrganizer {
		t.Fatalf("after updates: %+v", got)
	}
	if _, err := s.UserByID(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := s.SetUserRole(ctx, "missing", domain.RoleModerator); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if n, err := s.VotesCast(ctx, u.ID); err != nil || n != 0 {
		t.Fatalf("VotesCast: %d, %v", n, err)
	}
}
