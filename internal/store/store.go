// Package store persists unconference state in SQLite.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/jeffbstewart/unconf/internal/store/schema"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

// Store is the SQLite-backed persistence layer.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the database at path, enables WAL and
// foreign keys, verifies the applied schema fragments, and applies any
// pending ones (see package schema). It refuses to open a database whose
// recorded fragment hashes differ from the ones embedded in this binary.
func Open(ctx context.Context, path string) (*Store, error) {
	dsn := "file:" + path +
		"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := schema.Apply(ctx, db, schema.Fragments); err != nil {
		db.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Meta returns the value stored under key, or ErrNotFound.
func (s *Store) Meta(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = ?", key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return v, err
}

// SetMeta stores value under key, replacing any previous value.
func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value",
		key, value)
	return err
}

// MetaOrInit returns the value under key, first storing init() if the key
// is absent. Concurrent callers all observe the same stored value.
func (s *Store) MetaOrInit(ctx context.Context, key string, init func() string) (string, error) {
	if _, err := s.db.ExecContext(ctx,
		"INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT (key) DO NOTHING", key, init()); err != nil {
		return "", err
	}
	return s.Meta(ctx, key)
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
