package db

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
	"time"

	"github.com/pressly/goose/v3"
)

// Open creates and configures a database connection. Dispatches on
// `driver` to the SQLite or PostgreSQL implementation. The string
// after the driver is the DSN (postgres) or the filesystem path
// (sqlite) — the caller is expected to pass the appropriate one from
// config.
//
// Both backends apply pool tuning tailored to their concurrency model
// (see openSQLite / openPostgres for the numbers). Both ping before
// returning so a misconfigured DSN fails the boot loudly with a clear
// error rather than producing N "failed to connect" warnings at
// request time.
func Open(driver, dsnOrPath string, logger *slog.Logger) (*sql.DB, error) {
	switch driver {
	case DriverSQLite:
		return openSQLite(dsnOrPath, logger)
	case DriverPostgres:
		return openPostgres(dsnOrPath, logger)
	default:
		return nil, fmt.Errorf("unsupported driver %q (want %q or %q)",
			driver, DriverSQLite, DriverPostgres)
	}
}

// Migrate runs all pending goose migrations using the provided
// filesystem. `driver` selects the dialect (`sqlite3` / `postgres`)
// goose passes to its SQL preprocessor AND the directory inside the
// FS where it looks for migration files (`migrations/sqlite` /
// `migrations/postgres`).
//
// The two migration trees are NOT generated from each other — they
// are maintained side-by-side by the project. Sesión D translated the
// SQLite migrations one by one to the Postgres dialect; future
// migrations land in both directories at the same time.
func Migrate(driver string, database *sql.DB, migrationsFS fs.FS, logger *slog.Logger) error {
	var (
		gooseDialect string
		dir          string
	)
	switch driver {
	case DriverSQLite:
		gooseDialect = "sqlite3"
		dir = "migrations/sqlite"
	case DriverPostgres:
		gooseDialect = "postgres"
		dir = "migrations/postgres"
	default:
		return fmt.Errorf("migrate: unsupported driver %q", driver)
	}

	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())

	if err := goose.SetDialect(gooseDialect); err != nil {
		return fmt.Errorf("setting goose dialect: %w", err)
	}
	if err := goose.Up(database, dir); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	logger.Info("database migrations complete",
		"driver", driver,
		"dialect", gooseDialect,
		"dir", dir)
	return nil
}

// Optimize is the dialect-aware planner-stats refresher. Drives
// `PRAGMA optimize` + FTS5 segment merge on SQLite; a no-op on
// Postgres (autovacuum + the trigger-maintained tsvector index keep
// stats current without explicit help). Best-effort: SQLite logs and
// continues on failure.
func Optimize(ctx context.Context, driver string, database *sql.DB, logger *slog.Logger) {
	if driver != DriverSQLite {
		// Postgres: autovacuum handles ANALYZE on its schedule.
		// Operators tuning a busy DB can run ANALYZE / REINDEX out
		// of band; we don't push that on from the application.
		return
	}
	optimizeSQLite(ctx, database, logger)
}

// optimizeInterval is how often the background tick fires. Six hours
// is a balance: scans burst writes for ~minutes, then go quiet for
// hours; running a planner refresh during the quiet window costs
// nothing and keeps stats current. Daily would also be fine; sub-hour
// is wasteful.
const optimizeInterval = 6 * time.Hour

// StartPeriodicOptimize fires `Optimize` every `optimizeInterval`
// until `ctx` is cancelled. The first tick fires after the interval —
// never at startup — so app boot doesn't pay the optimize cost on top
// of the cold-cache overhead it already has.
//
// Returns a stop function the caller can defer for clean shutdown
// (idempotent if the context is already cancelled). When `driver` is
// not SQLite the returned stop is a no-op closer over an already-
// stopped goroutine; the caller's defer pattern stays the same.
func StartPeriodicOptimize(ctx context.Context, driver string, database *sql.DB, logger *slog.Logger) func() {
	stop := make(chan struct{})
	if driver != DriverSQLite {
		// Postgres autovacuum already does this. Return a no-op
		// stopper so call sites don't need to branch on driver.
		close(stop)
		return func() {}
	}
	tick := time.NewTicker(optimizeInterval)
	go func() {
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-tick.C:
				// Bounded ctx so a stuck Optimize can't pile up forever
				// on top of the next scheduled tick.
				optCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				Optimize(optCtx, driver, database, logger)
				cancel()
			}
		}
	}()
	return func() {
		select {
		case <-stop:
		default:
			close(stop)
		}
	}
}
