package db

import (
	"context"
	"database/sql"
	"fmt"
)

// HealthChecker reports whether the underlying database connection
// pool is reachable. Single-method interface for handlers that need
// to gate liveness on a DB ping (admin /system/stats, /health/db,
// admin /db Test).
//
// El interfaz cierra el olor K de la auditoría 2026-05-14: handlers
// que sólo necesitan pinguear la DB no deben recibir `*sql.DB` raw —
// reciben este contrato estrecho y cero acceso a Query/Exec.
type HealthChecker interface {
	PingContext(ctx context.Context) error
}

// BackupOperator runs the SQLite-only `VACUUM INTO` to produce a
// hot-backup file. Postgres is out of scope (`pg_dump` is the
// orthodox tool — out of process). Implementations should error
// cleanly when the underlying driver isn't SQLite.
//
// Cierra el olor K: admin_backup ya no recibe `*sql.DB` ni invoca
// ExecContext con SQL ad-hoc.
type BackupOperator interface {
	VacuumInto(ctx context.Context, destPath string) error
}

// PoolStatsReporter exposes the connection-pool metrics the admin
// Database panel reads. Reads are zero-cost — sql.DB.Stats() is
// in-memory.
//
// Cierra el olor K: admin_db ya no toca `*sql.DB.Stats()` directo.
type PoolStatsReporter interface {
	Stats() sql.DBStats
}

// Maintenance wraps the live `*sql.DB` and exposes the typed slice
// of operations admin handlers need: ping, pool stats, backup,
// connection-source for the sqlite→postgres migrator. Implements
// HealthChecker + BackupOperator + PoolStatsReporter.
//
// Una sola instancia por proceso; el composition root (`main.go`)
// la construye después de `db.Open()` y la inyecta vía Dependencies.
// `Database *sql.DB` en Dependencies queda eliminado — sólo este
// wrapper viaja por el grafo, y `MigrationSource()` es el único
// hueco controlado por donde aún se expone el raw handle (sólo el
// migrator necesita acceso arbitrario a la conexión origen).
type Maintenance struct {
	db     *sql.DB
	driver string
}

// NewMaintenance binds the wrapper to a live connection. The
// driver string is captured so BackupOperator can refuse VACUUM
// against a Postgres connection without forcing the caller to
// re-derive the dialect from config.
func NewMaintenance(driver string, database *sql.DB) *Maintenance {
	return &Maintenance{db: database, driver: driver}
}

// PingContext implements HealthChecker.
func (m *Maintenance) PingContext(ctx context.Context) error {
	if m == nil || m.db == nil {
		return fmt.Errorf("db.Maintenance: no live connection")
	}
	return m.db.PingContext(ctx)
}

// Stats implements PoolStatsReporter. Zero-value for a nil wrapper
// so callers can call it unconditionally during teardown / tests.
func (m *Maintenance) Stats() sql.DBStats {
	if m == nil || m.db == nil {
		return sql.DBStats{}
	}
	return m.db.Stats()
}

// VacuumInto implements BackupOperator. SQLite-only; returns an
// error against Postgres pointing the operator at the right tool
// (`pg_dump`) rather than producing a half-broken artefact.
//
// destPath must be a filesystem path on the same host (SQLite
// resolves it relative to the process cwd). Caller is responsible
// for placing it inside a writable directory and cleaning up on
// failure.
func (m *Maintenance) VacuumInto(ctx context.Context, destPath string) error {
	if m == nil || m.db == nil {
		return fmt.Errorf("db.Maintenance: no live connection")
	}
	if IsPostgres(m.driver) {
		return fmt.Errorf("db.Maintenance.VacuumInto: not supported for Postgres (use pg_dump)")
	}
	// `VACUUM INTO` is parsed by SQLite at the SQL level; using %q
	// would escape with double-quotes (an SQL identifier quote),
	// which SQLite accepts for the path literal. Caller-supplied
	// path is already constrained to a server-controlled directory
	// by the admin_backup handler.
	if _, err := m.db.ExecContext(ctx, fmt.Sprintf("VACUUM INTO %q", destPath)); err != nil {
		return fmt.Errorf("vacuum into: %w", err)
	}
	return nil
}

// MigrationSource returns the underlying `*sql.DB` for the
// sqlite→postgres migrator. **Use sparingly** — this is the only
// legitimate caller; every other handler should consume the typed
// interfaces above instead of grabbing the raw handle.
//
// Devuelto sin envolver porque `db.MigrateSQLiteToPostgres` hace
// queries arbitrarias contra el origen (catálogo + row dumps por
// tabla); envolver eso en una interfaz tipada no aporta valor
// (sería literalmente reimplementar `*sql.DB`).
func (m *Maintenance) MigrationSource() *sql.DB {
	if m == nil {
		return nil
	}
	return m.db
}
