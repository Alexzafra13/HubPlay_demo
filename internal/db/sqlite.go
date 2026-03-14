package db

import (
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/pressly/goose/v3"

	_ "modernc.org/sqlite"
)

// Open creates and configures a SQLite database connection.
func Open(driver, path string, logger *slog.Logger) (*sql.DB, error) {
	if driver != "sqlite" {
		return nil, fmt.Errorf("unsupported driver %q (only sqlite supported in v1)", driver)
	}

	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)"
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite: %w", err)
	}

	database.SetMaxOpenConns(4)

	if err := database.Ping(); err != nil {
		database.Close()
		return nil, fmt.Errorf("pinging sqlite: %w", err)
	}

	logger.Info("database opened", "driver", driver, "path", path)
	return database, nil
}

// Migrate runs all pending goose migrations using the provided filesystem.
func Migrate(database *sql.DB, migrationsFS fs.FS, logger *slog.Logger) error {
	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())

	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("setting goose dialect: %w", err)
	}

	if err := goose.Up(database, "migrations/sqlite"); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	logger.Info("database migrations complete")
	return nil
}
