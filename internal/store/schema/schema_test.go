package schema

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	_ "modernc.org/sqlite"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "s.db")+"?_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func frags(files map[string]string) fstest.MapFS {
	m := fstest.MapFS{}
	for name, body := range files {
		m[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return m
}

var v1 = map[string]string{
	"001_a.sql": "CREATE TABLE a (id INTEGER);",
	"002_b.sql": "CREATE TABLE b (id INTEGER);",
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", name).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

func mustApply(t *testing.T, db *sql.DB, files map[string]string) {
	t.Helper()
	if err := Apply(context.Background(), db, frags(files)); err != nil {
		t.Fatal(err)
	}
}

func wantApplyError(t *testing.T, db *sql.DB, files map[string]string, contains string) {
	t.Helper()
	err := Apply(context.Background(), db, frags(files))
	if err == nil || !strings.Contains(err.Error(), contains) {
		t.Fatalf("want error containing %q, got %v", contains, err)
	}
}

func TestEmptyCreatesOnlyVersionTable(t *testing.T) {
	db := openDB(t)
	mustApply(t, db, nil)
	var n int
	db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table'").Scan(&n)
	if n != 1 || !tableExists(t, db, "schema_version") {
		t.Fatalf("want only schema_version, got %d tables", n)
	}
}

func TestApplyRecordsFragmentsAndHashes(t *testing.T) {
	db := openDB(t)
	mustApply(t, db, v1)
	if !tableExists(t, db, "a") || !tableExists(t, db, "b") {
		t.Fatal("fragments not applied")
	}
	applied, err := AppliedVersions(context.Background(), db)
	if err != nil || len(applied) != 2 {
		t.Fatalf("applied: %+v, %v", applied, err)
	}
	loaded, _ := Load(frags(v1))
	for i, a := range applied {
		if a.Version != i+1 || a.Name != loaded[i].Name || a.SHA256 != loaded[i].SHA256 || len(a.SHA256) != 64 || a.AppliedAt == "" {
			t.Errorf("row %d: %+v", i, a)
		}
	}
	// Re-applying is a no-op (would fail with "table a already exists" otherwise).
	mustApply(t, db, v1)
}

func TestApplyAddsNewFragments(t *testing.T) {
	db := openDB(t)
	mustApply(t, db, v1)
	v2 := map[string]string{"003_c.sql": "CREATE TABLE c (id INTEGER); INSERT INTO a VALUES (1);"}
	for k, v := range v1 {
		v2[k] = v
	}
	mustApply(t, db, v2)
	if !tableExists(t, db, "c") {
		t.Fatal("003 not applied")
	}
	var n int
	db.QueryRow("SELECT COUNT(*) FROM a").Scan(&n)
	if n != 1 {
		t.Fatal("multi-statement fragment not fully applied")
	}
}

func TestRefusesEditedFragment(t *testing.T) {
	db := openDB(t)
	mustApply(t, db, v1)
	edited := map[string]string{
		"001_a.sql": "CREATE TABLE a (id INTEGER, extra TEXT);",
		"002_b.sql": v1["002_b.sql"],
		"003_c.sql": "CREATE TABLE c (id INTEGER);",
	}
	wantApplyError(t, db, edited, "fragment 001 does not match")
	if tableExists(t, db, "c") {
		t.Fatal("nothing may be applied after a hash mismatch")
	}
	// Even a whitespace-only change is refused.
	ws := map[string]string{"001_a.sql": v1["001_a.sql"] + "\n", "002_b.sql": v1["002_b.sql"]}
	wantApplyError(t, db, ws, "does not match")
}

func TestRefusesRenamedFragment(t *testing.T) {
	db := openDB(t)
	mustApply(t, db, v1)
	renamed := map[string]string{"001_a.sql": v1["001_a.sql"], "002_bee.sql": v1["002_b.sql"]}
	wantApplyError(t, db, renamed, "002_bee.sql")
}

func TestRefusesDatabaseNewerThanBinary(t *testing.T) {
	db := openDB(t)
	mustApply(t, db, v1)
	wantApplyError(t, db, map[string]string{"001_a.sql": v1["001_a.sql"]}, "binary does not include")
}

func TestRefusesInconsistentHistory(t *testing.T) {
	db := openDB(t)
	mustApply(t, db, v1)
	db.Exec("DELETE FROM schema_version WHERE version = 1")
	wantApplyError(t, db, v1, "inconsistent")
}

func TestFailedFragmentRollsBack(t *testing.T) {
	db := openDB(t)
	bad := map[string]string{
		"001_a.sql": v1["001_a.sql"],
		"002_b.sql": "CREATE TABLE b (id INTEGER); CREATE TABLE oops (;",
	}
	wantApplyError(t, db, bad, "apply 002_b.sql")
	if !tableExists(t, db, "a") || tableExists(t, db, "b") {
		t.Fatal("001 should be committed and 002 rolled back entirely")
	}
	applied, _ := AppliedVersions(context.Background(), db)
	if len(applied) != 1 {
		t.Fatalf("only 001 should be recorded: %+v", applied)
	}
	mustApply(t, db, v1) // a fixed 002 applies on the next start
}

func TestLoadValidatesNames(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"two digits":      {"01_a.sql": ""},
		"dash":            {"001-a.sql": ""},
		"no description":  {"001.sql": ""},
		"upper case":      {"001_A.sql": ""},
		"wrong extension": {"001_a.sql": "", "002_b.txt": ""},
		"stray file":      {"001_a.sql": "", "README.md": ""},
		"gap":             {"001_a.sql": "", "003_c.sql": ""},
		"duplicate":       {"001_a.sql": "", "001_b.sql": ""},
		"not from 001":    {"002_b.sql": ""},
	} {
		if _, err := Load(frags(files)); err == nil {
			t.Errorf("%s: Load accepted %v", name, files)
		}
	}
	if _, err := Load(fstest.MapFS{"001_a.sql": {}, "sub": {Mode: 0o755 | 1<<31}}); err == nil {
		t.Error("directories must be rejected")
	}
}

func TestShippedFragmentsAreValid(t *testing.T) {
	loaded, err := Load(Fragments)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) == 0 {
		t.Fatal("no fragments embedded")
	}
	db := openDB(t)
	if err := Apply(context.Background(), db, Fragments); err != nil {
		t.Fatal(err)
	}
}
