package main

import (
	"database/sql"
	"fmt"
	"log/slog"

	hubplay "hubplay"
	"hubplay/internal/db"
)

// openDatabase aplica un restore pendiente (SQLite-only), abre la
// conexión, ejecuta migraciones y construye los repos. El caller
// es responsable de cerrar la DB.
func openDatabase(cfg databaseConfig, logger *slog.Logger) (*sql.DB, *db.Repositories, error) {
	if cfg.Driver == db.DriverSQLite {
		if err := db.ApplyPendingRestoreIfAny(cfg.Path, logger); err != nil {
			return nil, nil, fmt.Errorf("applying pending DB restore: %w", err)
		}
	}

	dsnOrPath := cfg.Path
	if cfg.Driver == db.DriverPostgres {
		dsnOrPath = cfg.DSN
	}

	database, err := db.Open(cfg.Driver, dsnOrPath, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("opening database: %w", err)
	}

	// Red de seguridad: snapshot SQLite antes de aplicar migraciones
	// pendientes (up-only, sin rollback). Best-effort, no bloquea el boot.
	backupBeforeMigrate(database, cfg, hubplay.Migrations(cfg.Driver), logger)

	if err := db.Migrate(cfg.Driver, database, hubplay.Migrations(cfg.Driver), logger); err != nil {
		database.Close() //nolint:errcheck
		return nil, nil, fmt.Errorf("running migrations: %w", err)
	}

	repos := db.NewRepositories(cfg.Driver, database)
	return database, repos, nil
}

// databaseConfig agrupa los campos de config relevantes para abrir la DB.
type databaseConfig struct {
	Driver string
	Path   string
	DSN    string
}
