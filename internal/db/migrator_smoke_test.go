package db_test

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hubplay "hubplay"
	"hubplay/internal/db"
	"hubplay/internal/testutil"
)

// TestMigrateSQLiteToPostgres_EndToEnd is the end-to-end smoke for
// the admin "Migrate sqlite → postgres" button. Gated by the same
// HUBPLAY_TEST_POSTGRES_DSN env var the rest of the pg-aware tests
// use (Sesión G) — runs locally with `docker run postgres:16-alpine`
// and on the CI matrix lane that has the postgres service wired.
//
// The test:
//
//  1. Builds a fresh SQLite source with a couple of users + libraries
//     so we have realistic rows to copy (FK ordering, BOOLEAN, etc.).
//  2. Spins a fresh per-test Postgres database next to the env DSN
//     (same database-per-test scheme testutil.NewTestPostgresDB uses).
//  3. Invokes MigrateSQLiteToPostgres against it.
//  4. Asserts the rowcount survived the trip on both side of the
//     FK boundary (users → library_access → libraries).
func TestMigrateSQLiteToPostgres_EndToEnd(t *testing.T) {
	if os.Getenv("HUBPLAY_TEST_POSTGRES_DSN") == "" {
		t.Skip("HUBPLAY_TEST_POSTGRES_DSN not set — skipping pg migrator smoke")
	}

	logger := testutil.NopLogger()

	// ── Build the source SQLite (forced sqlite regardless of
	// HUBPLAY_TEST_DRIVER — the migrator's source is always SQLite).
	srcPath := filepath.Join(t.TempDir(), "src.db")
	src, err := db.Open(db.DriverSQLite, srcPath, logger)
	if err != nil {
		t.Fatalf("open source sqlite: %v", err)
	}
	defer src.Close() //nolint:errcheck

	if err := db.Migrate(db.DriverSQLite, src, hubplay.Migrations(db.DriverSQLite), logger); err != nil {
		t.Fatalf("migrate source: %v", err)
	}

	// Seed a couple of rows.
	ctx := context.Background()
	if _, err := src.ExecContext(ctx, `
		INSERT INTO users (id, username, display_name, password_hash, role, is_active, created_at)
		VALUES ('u1', 'admin', 'Admin', '$2a$10$abcdefghijklmnopqrstuv', 'admin', 1, datetime('now'))`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := src.ExecContext(ctx, `
		INSERT INTO libraries (id, name, content_type, created_at)
		VALUES ('lib1', 'Movies', 'movies', datetime('now'))`); err != nil {
		t.Fatalf("seed library: %v", err)
	}
	if _, err := src.ExecContext(ctx, `
		INSERT INTO library_access (user_id, library_id)
		VALUES ('u1', 'lib1')`); err != nil {
		t.Fatalf("seed access: %v", err)
	}

	// ── Prepare a fresh per-test target database.
	targetDSN := newPgDBForTest(t)

	// ── Run the migrator.
	result, err := db.MigrateSQLiteToPostgres(ctx, db.MigrateOptions{
		SourceDB:  src,
		TargetDSN: targetDSN,
		Logger:    logger,
	})
	if err != nil {
		t.Fatalf("MigrateSQLiteToPostgres: %v", err)
	}
	if result.RowsCopied < 3 {
		t.Errorf("rows copied = %d, want at least 3", result.RowsCopied)
	}
	if result.TablesCopied < 3 {
		t.Errorf("tables copied = %d, want at least 3", result.TablesCopied)
	}

	// ── Verify the target.
	target, err := db.Open(db.DriverPostgres, targetDSN, logger)
	if err != nil {
		t.Fatalf("open target pg: %v", err)
	}
	defer target.Close() //nolint:errcheck

	var users, libs, access int
	if err := target.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&users); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if err := target.QueryRowContext(ctx, "SELECT COUNT(*) FROM libraries").Scan(&libs); err != nil {
		t.Fatalf("count libraries: %v", err)
	}
	if err := target.QueryRowContext(ctx, "SELECT COUNT(*) FROM library_access").Scan(&access); err != nil {
		t.Fatalf("count library_access: %v", err)
	}
	if users != 1 || libs != 1 || access != 1 {
		t.Errorf("counts = users:%d libs:%d access:%d, want 1/1/1", users, libs, access)
	}
}

// newPgDBForTest creates a fresh per-test Postgres database next to
// the cluster pointed at by HUBPLAY_TEST_POSTGRES_DSN. The database
// is dropped on test cleanup. Mirrors testutil.newTestPostgresDB's
// scheme without re-exporting it.
func newPgDBForTest(t *testing.T) string {
	t.Helper()
	admin := os.Getenv("HUBPLAY_TEST_POSTGRES_DSN")
	adminDB, err := sql.Open("pgx", admin)
	if err != nil {
		t.Fatalf("open admin pg: %v", err)
	}
	defer adminDB.Close() //nolint:errcheck

	name := "hubplay_migrator_" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "_"))
	if len(name) > 60 {
		name = name[:60]
	}

	if _, err := adminDB.ExecContext(context.Background(),
		"DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
		t.Fatalf("drop pre-existing %s: %v", name, err)
	}
	if _, err := adminDB.ExecContext(context.Background(),
		"CREATE DATABASE "+name); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	t.Cleanup(func() {
		// Use a fresh admin connection — the test may have already
		// closed/leaked others by now.
		ac, err := sql.Open("pgx", admin)
		if err != nil {
			return
		}
		defer ac.Close() //nolint:errcheck
		_, _ = ac.ExecContext(context.Background(),
			"DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})

	// Rewrite the env DSN to point at our new db.
	u, err := url.Parse(admin)
	if err != nil {
		t.Fatalf("parse admin DSN: %v", err)
	}
	u.Path = "/" + name
	return u.String()
}
