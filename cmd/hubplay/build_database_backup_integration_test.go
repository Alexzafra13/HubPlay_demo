package main

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hubplay "hubplay"
	"hubplay/internal/db"
)

// TestBackupBeforeMigrate_Integration ejercita el camino real: una DB
// SQLite a una versión vieja (con migraciones pendientes) produce un
// snapshot en backups/; una DB fresca no.
func TestBackupBeforeMigrate_Integration(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	t.Run("con migraciones pendientes crea backup", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "hubplay.db")
		database, err := db.Open(db.DriverSQLite, path, logger)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		t.Cleanup(func() { _ = database.Close() })

		// Simula una DB en una versión vieja (1) → target (57+) > 1.
		seedGooseVersion(t, database, 1)

		cfg := databaseConfig{Driver: db.DriverSQLite, Path: path}
		backupBeforeMigrate(database, cfg, hubplay.Migrations(db.DriverSQLite), logger)

		if n := countBackups(filepath.Join(dir, "backups")); n != 1 {
			t.Fatalf("esperaba 1 backup pre-migración, vi %d", n)
		}
	})

	t.Run("DB fresca (sin versión) no crea backup", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "hubplay.db")
		database, err := db.Open(db.DriverSQLite, path, logger)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		t.Cleanup(func() { _ = database.Close() })

		cfg := databaseConfig{Driver: db.DriverSQLite, Path: path}
		backupBeforeMigrate(database, cfg, hubplay.Migrations(db.DriverSQLite), logger)

		if n := countBackups(filepath.Join(dir, "backups")); n != 0 {
			t.Fatalf("DB fresca no debería generar backup, vi %d", n)
		}
	})
}

// seedGooseVersion crea la tabla de versión de goose y marca `version`
// como aplicada, simulando una DB en un estado anterior.
func seedGooseVersion(t *testing.T, database *sql.DB, version int64) {
	t.Helper()
	ctx := context.Background()
	if _, err := database.ExecContext(ctx, `CREATE TABLE goose_db_version (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		version_id INTEGER NOT NULL,
		is_applied INTEGER NOT NULL,
		tstamp TIMESTAMP DEFAULT (datetime('now'))
	)`); err != nil {
		t.Fatalf("create goose table: %v", err)
	}
	if _, err := database.ExecContext(ctx,
		`INSERT INTO goose_db_version (version_id, is_applied) VALUES (0, 1), (?, 1)`, version); err != nil {
		t.Fatalf("seed goose version: %v", err)
	}
}

func countBackups(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "hubplay-pre-migrate-") {
			n++
		}
	}
	return n
}
