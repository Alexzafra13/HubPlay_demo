package testutil

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	hubplay "hubplay"
	"hubplay/internal/db"

	"log/slog"
)

// envTestDriver selects the backend tests run against. Default
// (unset / "sqlite") keeps the original SQLite-per-test behaviour;
// "postgres" routes through NewTestDB to a real Postgres cluster
// pointed at by HUBPLAY_TEST_POSTGRES_DSN.
//
// The CI matrix sets this so every PR runs the same suite against
// both backends. Local devs can opt in by running:
//
//	docker run --rm -d --name pg -e POSTGRES_PASSWORD=test -p 5432:5432 postgres:16-alpine
//	HUBPLAY_TEST_DRIVER=postgres \
//	  HUBPLAY_TEST_POSTGRES_DSN="postgres://postgres:test@127.0.0.1:5432/postgres?sslmode=disable" \
//	  go test ./internal/db/...
const (
	envTestDriver     = "HUBPLAY_TEST_DRIVER"
	envTestPostgresDSN = "HUBPLAY_TEST_POSTGRES_DSN"
)

// NewTestDB creates a unique temporary database with all migrations
// applied. Each call creates a fresh isolated database so state never
// leaks between tests. The database is automatically dropped/removed
// when the test finishes.
//
// Backend is selected by the HUBPLAY_TEST_DRIVER env var. Default is
// SQLite (file-per-test under t.TempDir); set to "postgres" to use
// the Postgres path (database-per-test against the cluster at
// HUBPLAY_TEST_POSTGRES_DSN).
func NewTestDB(t *testing.T) *sql.DB {
	t.Helper()

	if os.Getenv(envTestDriver) == db.DriverPostgres {
		return newTestPostgresDB(t)
	}
	return newTestSQLiteDB(t)
}

// NewTestRepos creates repositories backed by a fresh test database.
// Driver is the one NewTestDB picked (sqlite by default, postgres
// when HUBPLAY_TEST_DRIVER=postgres).
func NewTestRepos(t *testing.T) *db.Repositories {
	t.Helper()
	return db.NewRepositories(Driver(), NewTestDB(t))
}

// Driver returns whichever backend NewTestDB will use this run.
// Useful as the first argument to `db.NewXxxRepository(driver, ...)`
// so the same test file exercises both backends in the matrix CI run.
// Defaults to SQLite when HUBPLAY_TEST_DRIVER is unset.
func Driver() string {
	if d := os.Getenv(envTestDriver); d == db.DriverPostgres {
		return db.DriverPostgres
	}
	return db.DriverSQLite
}

// Exec runs a raw SQL fixture statement against the test DB,
// translating `?` placeholders to `$N` when the active driver is
// Postgres. Lets a test that seeds rows with a literal
// `INSERT INTO foo VALUES (?, ?, ?)` work unchanged in both
// matrix runs.
//
// The Postgres-specific result is identical to ExecContext's: any
// error fails the test directly so the caller does not have to wrap
// `if err != nil { t.Fatal }` around every fixture.
func Exec(t *testing.T, database *sql.DB, query string, args ...any) {
	t.Helper()
	q := db.RewritePlaceholders(Driver(), query)
	if _, err := database.ExecContext(context.Background(), q, args...); err != nil {
		t.Fatalf("testutil.Exec: %v\nquery: %s", err, q)
	}
}

// SkipIfPostgres lets a test bail out cleanly when it would only
// pass against SQLite (raw SQL with `?` placeholders, `PRAGMA`
// inspection, sqlite-specific helpers, etc.). Centralised so a
// future migration to dual-dialect for that test removes the call
// in one place.
func SkipIfPostgres(t *testing.T, reason string) {
	t.Helper()
	if Driver() == db.DriverPostgres {
		t.Skipf("skipping under HUBPLAY_TEST_DRIVER=postgres: %s", reason)
	}
}

// newTestSQLiteDB is the legacy path: a fresh file under t.TempDir
// with WAL + pragmas via db.Open.
func newTestSQLiteDB(t *testing.T) *sql.DB {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, fmt.Sprintf("test_%d.db", os.Getpid()))

	database, err := db.Open(db.DriverSQLite, dbPath, slog.Default())
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}

	if err := db.Migrate(db.DriverSQLite, database, hubplay.SQLiteMigrations, slog.Default()); err != nil {
		_ = database.Close()
		t.Fatalf("migrating test db: %v", err)
	}

	t.Cleanup(func() { _ = database.Close() })
	return database
}

// ─── postgres test infrastructure ──────────────────────────────────

// Singleton "admin" pool that points at the cluster's `postgres`
// database (or whatever DB the DSN points at). Used only to
// CREATE / DROP per-test databases — never holds application data.
var (
	pgAdminOnce  sync.Once
	pgAdminDB    *sql.DB
	pgBaseURL    *url.URL
	pgAdminError error
	pgTestCount  atomic.Int64
)

func newTestPostgresDB(t *testing.T) *sql.DB {
	t.Helper()
	pgAdminOnce.Do(initPostgresAdmin)
	if pgAdminError != nil {
		t.Fatalf("postgres test setup: %v", pgAdminError)
	}

	// One database per test. The combination of test PID and an atomic
	// counter keeps names unique across parallel test binaries on the
	// same cluster (e.g. `go test ./...` runs each package in its own
	// process; multiple packages can race on naming).
	dbName := fmt.Sprintf("hubplay_test_%d_%d", os.Getpid(), pgTestCount.Add(1))

	if _, err := pgAdminDB.Exec(fmt.Sprintf(`CREATE DATABASE %q`, dbName)); err != nil {
		t.Fatalf("create test database %q: %v", dbName, err)
	}

	// Build a DSN that points at the new database. url.URL.Path takes
	// the leading slash; whatever query (sslmode etc.) was on the
	// admin DSN survives intact.
	testURL := *pgBaseURL
	testURL.Path = "/" + dbName

	testDB, err := db.Open(db.DriverPostgres, testURL.String(), slog.Default())
	if err != nil {
		_, _ = pgAdminDB.Exec(fmt.Sprintf(`DROP DATABASE IF EXISTS %q`, dbName))
		t.Fatalf("open test database %q: %v", dbName, err)
	}

	if err := db.Migrate(db.DriverPostgres, testDB, hubplay.PostgresMigrations, slog.Default()); err != nil {
		_ = testDB.Close()
		_, _ = pgAdminDB.Exec(fmt.Sprintf(`DROP DATABASE IF EXISTS %q WITH (FORCE)`, dbName))
		t.Fatalf("migrate test database %q: %v", dbName, err)
	}

	t.Cleanup(func() {
		_ = testDB.Close()
		// WITH (FORCE) terminates any leftover backends from the test
		// (Postgres 13+). Without it a misbehaving test that leaks a
		// connection would block the DROP and leave the database
		// behind for the next run.
		if _, err := pgAdminDB.Exec(fmt.Sprintf(`DROP DATABASE IF EXISTS %q WITH (FORCE)`, dbName)); err != nil {
			// Cleanup failure isn't a test failure — log via t.Logf so
			// it shows up but the test verdict reflects its own
			// assertions.
			t.Logf("warning: drop test database %q: %v", dbName, err)
		}
	})

	return testDB
}

func initPostgresAdmin() {
	raw := os.Getenv(envTestPostgresDSN)
	if raw == "" {
		pgAdminError = errors.New("HUBPLAY_TEST_POSTGRES_DSN is required when HUBPLAY_TEST_DRIVER=postgres")
		return
	}
	u, err := url.Parse(raw)
	if err != nil {
		pgAdminError = fmt.Errorf("parse HUBPLAY_TEST_POSTGRES_DSN: %w", err)
		return
	}
	pgBaseURL = u
	pgAdminDB, pgAdminError = db.Open(db.DriverPostgres, raw, slog.Default())
}

// NopLogger returns a logger that discards output.
func NopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

// TestLogger returns a logger suitable for tests (discards below error).
func TestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}
