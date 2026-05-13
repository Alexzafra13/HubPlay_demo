package db_test

import (
	"sort"
	"strings"
	"testing"

	hubplay "hubplay"
)

// TestMigrationParity catches the most common source of dual-dialect
// drift: someone adds a SQLite migration and forgets the Postgres
// twin (or vice versa). The two trees are maintained side-by-side and
// MUST list the same filenames — every schema change lives in both
// or in neither.
//
// What this test does NOT verify: that the SQL inside two same-named
// files actually expresses the same schema change. Translating
// `INSERT OR IGNORE` to `ON CONFLICT DO NOTHING` or `INTEGER` to
// `BIGINT` is judgement work that lives in the migration author's
// head; this test only catches "you forgot a file" — the cheap class
// of mistake.
//
// Run order in either tree is set by the leading numeric prefix
// (goose convention); the parity check therefore reduces to "same
// sorted filename list". A file present in one tree without a twin
// in the other is the failure.
func TestMigrationParity(t *testing.T) {
	sqliteFiles := listMigrationFilenames(t, "sqlite")
	postgresFiles := listMigrationFilenames(t, "postgres")

	missingFromPg := setDifference(sqliteFiles, postgresFiles)
	missingFromSq := setDifference(postgresFiles, sqliteFiles)

	if len(missingFromPg) > 0 {
		t.Errorf("migrations/sqlite has files with no migrations/postgres twin: %v\n"+
			"Add the Postgres version (translated from SQLite — see docs/architecture/postgres-migration.md "+
			"for the gotchas: BIGINT vs INTEGER, ON CONFLICT, tsvector vs FTS5, etc.)",
			missingFromPg)
	}
	if len(missingFromSq) > 0 {
		t.Errorf("migrations/postgres has files with no migrations/sqlite twin: %v\n"+
			"Add the SQLite version (the project's primary backend — every postgres-only "+
			"migration would break sqlite operators on the next boot).",
			missingFromSq)
	}
}

func listMigrationFilenames(t *testing.T, dialect string) []string {
	t.Helper()
	fs := hubplay.SQLiteMigrations
	if dialect == "postgres" {
		fs = hubplay.PostgresMigrations
	}
	entries, err := fs.ReadDir("migrations/" + dialect)
	if err != nil {
		t.Fatalf("read migrations/%s: %v", dialect, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// setDifference returns elements of `a` not present in `b`.
func setDifference(a, b []string) []string {
	in := make(map[string]struct{}, len(b))
	for _, s := range b {
		in[s] = struct{}{}
	}
	var diff []string
	for _, s := range a {
		if _, ok := in[s]; !ok {
			diff = append(diff, s)
		}
	}
	return diff
}
