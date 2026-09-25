// Package schema evolves the SQLite schema through numbered SQL fragments.
//
// The only schema this code creates itself is the schema_version table.
// Every other table comes from a fragment file in this directory named
// NNN_description.sql (three-digit number, lower-case words), embedded in
// the binary. Fragments are applied in order, each in its own transaction
// together with a schema_version row recording its number, file name, and
// SHA-256.
//
// On every start the recorded hashes are compared with the embedded files.
// A mismatch means an applied fragment was edited after the fact, and the
// database must not be opened. The same goes for a fragment the database has
// applied but the binary lacks (an older binary on a newer database).
// Schema changes are therefore always new fragments, never edits.
package schema

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"time"
)

// Fragments holds the schema fragments shipped in this binary.
//
//go:embed *.sql
var Fragments embed.FS

const createVersionTable = `CREATE TABLE IF NOT EXISTS schema_version (
  version    INTEGER PRIMARY KEY,
  name       TEXT NOT NULL,
  sha256     TEXT NOT NULL,
  applied_at TEXT NOT NULL
)`

var fragmentName = regexp.MustCompile(`^(\d{3})_[a-z0-9]+(?:_[a-z0-9]+)*\.sql$`)

// Fragment is one numbered schema revision.
type Fragment struct {
	Version int
	Name    string // file name, e.g. "002_events_users.sql"
	SHA256  string // hex digest of the file's bytes
	SQL     string
}

// Load reads and validates the fragments at the root of fsys. Every file
// must be a correctly named fragment, and the numbers must run 001, 002, ...
// with no gaps or duplicates.
func Load(fsys fs.FS) ([]Fragment, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var frags []Fragment
	for _, e := range entries {
		m := fragmentName.FindStringSubmatch(e.Name())
		if e.IsDir() || m == nil {
			return nil, fmt.Errorf("schema: %q is not a fragment (want NNN_lower_words.sql)", e.Name())
		}
		body, err := fs.ReadFile(fsys, e.Name())
		if err != nil {
			return nil, err
		}
		version, _ := strconv.Atoi(m[1])
		sum := sha256.Sum256(body)
		frags = append(frags, Fragment{
			Version: version,
			Name:    e.Name(),
			SHA256:  hex.EncodeToString(sum[:]),
			SQL:     string(body),
		})
	}
	sort.Slice(frags, func(i, j int) bool { return frags[i].Version < frags[j].Version })
	for i, f := range frags {
		if f.Version != i+1 {
			return nil, fmt.Errorf("schema: fragments must be numbered 001, 002, ... without gaps or duplicates; found %s at position %d", f.Name, i+1)
		}
	}
	return frags, nil
}

// Applied is a schema_version row.
type Applied struct {
	Version   int
	Name      string
	SHA256    string
	AppliedAt string
}

// Apply verifies the database against the fragments in fsys and applies
// any that are pending. It returns an error, having changed nothing, if
// the fragments are malformed, a recorded hash or name differs from the
// embedded fragment, or the database has fragments the binary lacks.
func Apply(ctx context.Context, db *sql.DB, fsys fs.FS) error {
	frags, err := Load(fsys)
	if err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, createVersionTable); err != nil {
		return fmt.Errorf("schema: create schema_version: %w", err)
	}
	applied, err := AppliedVersions(ctx, db)
	if err != nil {
		return err
	}
	if err := verify(frags, applied); err != nil {
		return err
	}
	for _, f := range frags[len(applied):] {
		if err := applyOne(ctx, db, f); err != nil {
			return err
		}
	}
	return nil
}

// verify checks that the applied rows are exactly the embedded fragments
// 1..n, byte for byte.
func verify(frags []Fragment, applied []Applied) error {
	for i, a := range applied {
		if a.Version != i+1 {
			return fmt.Errorf("schema: database records fragment %03d but not %03d; schema_version is inconsistent", a.Version, i+1)
		}
		if i >= len(frags) {
			return fmt.Errorf("schema: database has fragment %s applied, which this binary does not include (is the binary older than the database?)", a.Name)
		}
		f := frags[i]
		if a.Name != f.Name || a.SHA256 != f.SHA256 {
			return fmt.Errorf("schema: fragment %03d does not match the database: database applied %s (sha256 %s), binary has %s (sha256 %s); applied fragments must never be edited, add a new fragment instead",
				f.Version, a.Name, a.SHA256, f.Name, f.SHA256)
		}
	}
	return nil
}

func applyOne(ctx context.Context, db *sql.DB, f Fragment) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Another process may have applied this fragment since we looked.
	var sum string
	switch err := tx.QueryRowContext(ctx, "SELECT sha256 FROM schema_version WHERE version = ?", f.Version).Scan(&sum); {
	case err == nil && sum == f.SHA256:
		return nil
	case err == nil:
		return fmt.Errorf("schema: fragment %s was concurrently applied with a different hash", f.Name)
	case err != sql.ErrNoRows:
		return err
	}

	if _, err := tx.ExecContext(ctx, f.SQL); err != nil {
		return fmt.Errorf("schema: apply %s: %w", f.Name, err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO schema_version (version, name, sha256, applied_at) VALUES (?, ?, ?, ?)",
		f.Version, f.Name, f.SHA256, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	return tx.Commit()
}

// AppliedVersions lists the schema_version rows in version order.
func AppliedVersions(ctx context.Context, db *sql.DB) ([]Applied, error) {
	rows, err := db.QueryContext(ctx, "SELECT version, name, sha256, applied_at FROM schema_version ORDER BY version")
	if err != nil {
		return nil, fmt.Errorf("schema: read schema_version: %w", err)
	}
	defer rows.Close()
	var out []Applied
	for rows.Next() {
		var a Applied
		if err := rows.Scan(&a.Version, &a.Name, &a.SHA256, &a.AppliedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
