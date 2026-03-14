package testutil

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	hubplay "hubplay"
	"hubplay/internal/db"

	"log/slog"
)

// NewTestDB creates a unique temporary SQLite database with all migrations applied.
// Each call creates a separate database file to prevent state leaking between tests.
// The database is automatically closed and removed when the test finishes.
func NewTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, fmt.Sprintf("test_%d.db", os.Getpid()))

	database, err := db.Open("sqlite", dbPath, slog.Default())
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}

	if err := db.Migrate(database, hubplay.SQLiteMigrations, slog.Default()); err != nil {
		_ = database.Close()
		t.Fatalf("migrating test db: %v", err)
	}

	t.Cleanup(func() { _ = database.Close() })
	return database
}

// NewTestRepos creates repositories backed by an in-memory database.
func NewTestRepos(t *testing.T) *db.Repositories {
	t.Helper()
	return db.NewRepositories(NewTestDB(t))
}

// NopLogger returns a logger that discards output.
func NopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

// TestLogger returns a logger suitable for tests (discards below error).
func TestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}
