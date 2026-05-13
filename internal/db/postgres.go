package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// pgxPoolMaxOpenConns caps concurrent PostgreSQL connections. Postgres
// tolerates many more concurrent connections than SQLite (a connection
// is a backend process, but pgx's pool is lightweight on the client
// side). 25 is a safe default for a self-hosted server: leaves
// headroom under the typical `max_connections = 100` on small
// instances even when other clients (psql, pgbouncer, BI tools) share
// the cluster.
const pgxPoolMaxOpenConns = 25

// pgxPoolMaxIdleConns matches MaxOpenConns so a steady-state workload
// never reopens a TCP+TLS handshake. Postgres connection setup is
// expensive (1–10 ms each on a remote DB) — reusing the pool matters
// more than for SQLite.
const pgxPoolMaxIdleConns = 25

// pgxPoolConnMaxIdleTime caps how long an idle pooled connection
// stays around. After this it's closed. Five minutes balances
// "warm pool for bursty traffic" against "don't pin backend processes
// on the server for an idle client".
const pgxPoolConnMaxIdleTime = 5 * time.Minute

// pgxPoolConnMaxLifetime caps the total lifetime of a pooled
// connection. Set to one hour so a slow leak (server-side memory per
// backend, growing prepared-statement cache, etc.) eventually gets
// recycled. The pool reopens transparently on the next checkout.
const pgxPoolConnMaxLifetime = 1 * time.Hour

// openPostgres creates and configures a PostgreSQL connection pool.
// Driver-specific arm of db.Open; the dispatcher in database.go picks
// this when cfg.Database.Driver == "postgres".
//
// The DSN is passed through unchanged. Standard libpq format is
// supported (postgres://user:pass@host:port/dbname?sslmode=...) plus
// pgx's extended forms — see jackc/pgx docs for the full list.
//
// Pgx is registered as a database/sql driver via the
// jackc/pgx/v5/stdlib side-effect import, so the rest of the codebase
// keeps using `*sql.DB` and `database/sql` semantics unchanged.
func openPostgres(dsn string, logger *slog.Logger) (*sql.DB, error) {
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening postgres: %w", err)
	}

	database.SetMaxOpenConns(pgxPoolMaxOpenConns)
	database.SetMaxIdleConns(pgxPoolMaxIdleConns)
	database.SetConnMaxIdleTime(pgxPoolConnMaxIdleTime)
	database.SetConnMaxLifetime(pgxPoolConnMaxLifetime)

	// Bounded ping — a slow DNS / unreachable host shouldn't block
	// boot for more than a handful of seconds. The operator gets a
	// clear error message and the server exits non-zero rather than
	// hanging the supervisor.
	pingCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := database.PingContext(pingCtx); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("pinging postgres: %w", err)
	}

	logger.Info("database opened",
		"driver", DriverPostgres,
		"dsn", redactPostgresDSN(dsn),
		"pool_max_open", pgxPoolPoolSize(pgxPoolMaxOpenConns),
		"conn_max_lifetime", pgxPoolConnMaxLifetime.String())
	return database, nil
}

// pgxPoolPoolSize wraps an int so it shows up neatly in structured
// logs without needing a custom Marshaller. Trivial helper kept here
// (rather than a slog.Attr) because the boot log is the only call site.
func pgxPoolPoolSize(n int) int { return n }

// redactPostgresDSN strips the password from a Postgres URL so the
// boot log doesn't leak credentials. Handles the `postgres://` URL
// form (user:password@host); the `key=value` libpq form is a different
// shape and we don't redact it (logging that whole string would still
// expose `password=...` in plain — operators using key=value DSNs
// should rely on env var injection or a secrets file).
//
// Falls back to the raw value if parsing fails: better to log an
// imperfect string than a misleading one.
func redactPostgresDSN(dsn string) string {
	// postgres://user:password@host:port/db?sslmode=...
	if !startsWith(dsn, "postgres://") && !startsWith(dsn, "postgresql://") {
		return dsn
	}
	at := lastIndex(dsn, "@")
	if at < 0 {
		return dsn
	}
	// Find the first '://' to skip the scheme.
	schemeEnd := indexOf(dsn, "://")
	if schemeEnd < 0 || schemeEnd >= at {
		return dsn
	}
	userInfo := dsn[schemeEnd+3 : at]
	colon := indexOf(userInfo, ":")
	if colon < 0 {
		// No password to hide.
		return dsn
	}
	return dsn[:schemeEnd+3] + userInfo[:colon] + ":***@" + dsn[at+1:]
}

// Tiny strings helpers — kept private to avoid an explicit `strings`
// import in a file that otherwise has none. The few call sites here
// are easier to read than the strings package equivalents.
func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func lastIndex(s, sub string) int {
	last := -1
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			last = i
		}
	}
	return last
}
