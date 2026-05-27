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

// envTestDriver: selecciona backend. Vacío/"sqlite" = file-per-test;
// "postgres" = DB-per-test contra HUBPLAY_TEST_POSTGRES_DSN. CI lo usa
// para correr toda la suite contra ambos backends en cada PR. En local:
//
//	docker run --rm -d --name pg -e POSTGRES_PASSWORD=test -p 5432:5432 postgres:16-alpine
//	HUBPLAY_TEST_DRIVER=postgres \
//	  HUBPLAY_TEST_POSTGRES_DSN="postgres://postgres:test@127.0.0.1:5432/postgres?sslmode=disable" \
//	  go test ./internal/db/...
const (
	envTestDriver     = "HUBPLAY_TEST_DRIVER"
	envTestPostgresDSN = "HUBPLAY_TEST_POSTGRES_DSN"
)

// NewTestDB: DB temporal aislada con todas las migraciones aplicadas.
// Limpieza automática al terminar el test. Backend según HUBPLAY_TEST_DRIVER
// (sqlite default = file-per-test; postgres = DB-per-test contra el cluster).
func NewTestDB(tb testing.TB) *sql.DB {
	tb.Helper()

	if os.Getenv(envTestDriver) == db.DriverPostgres {
		return newTestPostgresDB(tb)
	}
	return newTestSQLiteDB(tb)
}

// NewTestRepos: repos sobre una DB fresca con el driver que toque.
func NewTestRepos(tb testing.TB) *db.Repositories {
	tb.Helper()
	return db.NewRepositories(Driver(), NewTestDB(tb))
}

// Driver: backend que usará NewTestDB en este run. Default SQLite.
func Driver() string {
	if d := os.Getenv(envTestDriver); d == db.DriverPostgres {
		return db.DriverPostgres
	}
	return db.DriverSQLite
}

// Exec: SQL fixture traduciendo `?` → `$N` cuando el driver es Postgres,
// así un INSERT con `?` corre en ambas matrices sin cambios. Error → tb.Fatal
// directo (evita el `if err != nil { t.Fatal }` en cada fixture).
func Exec(tb testing.TB, database *sql.DB, query string, args ...any) {
	tb.Helper()
	q := db.RewritePlaceholders(Driver(), query)
	if _, err := database.ExecContext(context.Background(), q, args...); err != nil {
		tb.Fatalf("testutil.Exec: %v\nquery: %s", err, q)
	}
}

// SkipIfPostgres: salida limpia para tests sqlite-only (PRAGMA, `?` literal,
// helpers específicos). Centralizado para que migrar a dual-dialect quite
// el skip en un solo sitio.
func SkipIfPostgres(tb testing.TB, reason string) {
	tb.Helper()
	if Driver() == db.DriverPostgres {
		tb.Skipf("skipping under HUBPLAY_TEST_DRIVER=postgres: %s", reason)
	}
}

// migrateMu serializa las llamadas a `db.Migrate` desde tests porque
// goose muta globales (`SetBaseFS`, `SetDialect`, `SetLogger`) sin
// protección. En producción Migrate corre una sola vez al boot; aquí
// con t.Parallel() pueden solaparse varios `NewTestDB` y disparar el
// race detector aunque cada uno opere sobre su propia DB.
var migrateMu sync.Mutex

// newTestSQLiteDB: fichero fresco en t.TempDir con WAL + pragmas vía db.Open.
func newTestSQLiteDB(tb testing.TB) *sql.DB {
	tb.Helper()

	dir := tb.TempDir()
	dbPath := filepath.Join(dir, fmt.Sprintf("test_%d.db", os.Getpid()))

	database, err := db.Open(db.DriverSQLite, dbPath, slog.Default())
	if err != nil {
		tb.Fatalf("opening test db: %v", err)
	}

	migrateMu.Lock()
	err = db.Migrate(db.DriverSQLite, database, hubplay.SQLiteMigrations, slog.Default())
	migrateMu.Unlock()
	if err != nil {
		_ = database.Close()
		tb.Fatalf("migrating test db: %v", err)
	}

	tb.Cleanup(func() { _ = database.Close() })
	return database
}

// ─── postgres test infrastructure ──────────────────────────────────

// Pool admin singleton: apunta a la DB del DSN ("postgres" típicamente).
// Sólo se usa para CREATE/DROP DB por test; nunca contiene datos de la app.
var (
	pgAdminOnce  sync.Once
	pgAdminDB    *sql.DB
	pgBaseURL    *url.URL
	pgAdminError error
	pgTestCount  atomic.Int64
)

func newTestPostgresDB(tb testing.TB) *sql.DB {
	tb.Helper()
	pgAdminOnce.Do(initPostgresAdmin)
	if pgAdminError != nil {
		tb.Fatalf("postgres test setup: %v", pgAdminError)
	}

	// PID + contador atómico evita colisiones entre binarios paralelos
	// (`go test ./...` corre cada paquete en su propio proceso).
	dbName := fmt.Sprintf("hubplay_test_%d_%d", os.Getpid(), pgTestCount.Add(1))

	if _, err := pgAdminDB.Exec(fmt.Sprintf(`CREATE DATABASE %q`, dbName)); err != nil {
		tb.Fatalf("create test database %q: %v", dbName, err)
	}

	// url.URL.Path lleva el slash; el query (sslmode etc.) del DSN admin se preserva.
	testURL := *pgBaseURL
	testURL.Path = "/" + dbName

	testDB, err := db.Open(db.DriverPostgres, testURL.String(), slog.Default())
	if err != nil {
		_, _ = pgAdminDB.Exec(fmt.Sprintf(`DROP DATABASE IF EXISTS %q`, dbName))
		tb.Fatalf("open test database %q: %v", dbName, err)
	}

	migrateMu.Lock()
	err = db.Migrate(db.DriverPostgres, testDB, hubplay.PostgresMigrations, slog.Default())
	migrateMu.Unlock()
	if err != nil {
		_ = testDB.Close()
		_, _ = pgAdminDB.Exec(fmt.Sprintf(`DROP DATABASE IF EXISTS %q WITH (FORCE)`, dbName))
		tb.Fatalf("migrate test database %q: %v", dbName, err)
	}

	tb.Cleanup(func() {
		_ = testDB.Close()
		// WITH (FORCE) (pg 13+) corta backends huérfanos; sin esto un test
		// que filtre conexión bloquea el DROP y deja la DB residual.
		if _, err := pgAdminDB.Exec(fmt.Sprintf(`DROP DATABASE IF EXISTS %q WITH (FORCE)`, dbName)); err != nil {
			tb.Logf("warning: drop test database %q: %v", dbName, err)
		}
	})

	return testDB
}

// closePostgresAdmin cierra el singleton admin pool si está inicializado.
// Llamado desde RunWithGoleak antes de verificar leaks. No-op en runs
// SQLite (el singleton nunca se inicializa).
func closePostgresAdmin() {
	if pgAdminDB != nil {
		_ = pgAdminDB.Close()
		pgAdminDB = nil
	}
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

func NopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

func TestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}
