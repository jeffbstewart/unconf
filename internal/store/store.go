// Package store persists unconference state in SQLite.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

// Store is the SQLite-backed persistence layer.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the database at path, enables WAL and
// foreign keys, and applies pending migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	dsn := "file:" + path +
		"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate %s: %w", path, err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// migrate applies every embedded migrations/NNN_*.sql whose number exceeds
// the database's user_version, each in its own transaction.
func (s *Store) migrate(ctx context.Context) error {
	var current int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return err
	}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		version, err := strconv.Atoi(strings.SplitN(path.Base(name), "_", 2)[0])
		if err != nil {
			return fmt.Errorf("migration %s: bad version prefix", name)
		}
		if version <= current {
			continue
		}
		body, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		current = version
	}
	return nil
}

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
