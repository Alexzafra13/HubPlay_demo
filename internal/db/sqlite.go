package db

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
	"time"

	"github.com/pressly/goose/v3"

	_ "modernc.org/sqlite"
)

// sqlitePragmas is the connect-string suffix that configures every
// SQLite connection HubPlay opens. The ordering inside the string is
// not significant — modernc.org/sqlite applies them sequentially as
// it parses the DSN — but I list them roughly by category for human
// reading.
//
// Each pragma is justified below; numbers were picked for a self-
// hosted media server profile (read-heavy with bursty writes during
// scans, single-process, low-thousands of items, single-digit users).
//
//   - journal_mode=WAL: the only sane choice for any concurrent
//     workload. Writers don't block readers.
//
//   - synchronous=NORMAL: the documented sweet spot for WAL. FULL is
//     paranoia (fsync per commit), OFF risks corruption on crash.
//     NORMAL fsyncs the WAL but lets the checkpoint be lazy.
//
//   - busy_timeout=5000: 5s grace for any write contender to wait on
//     a lock instead of failing immediately. Combined with WAL,
//     contention is rare; 5s absorbs scan-burst peaks.
//
//   - foreign_keys=ON: SQLite ships with FKs OFF by default for legacy
//     reasons. We have ON DELETE CASCADE in several places (federation
//     peers → shares, libraries → items, etc.); turning this OFF would
//     leak orphan rows silently. Always ON.
//
//   - cache_size=-65536: page cache size in KiB (negative number = KiB,
//     positive = page count). 64 MiB. Default is 2 MiB. For our DB
//     size (a household library is < 50 MiB on disk for thousands of
//     items) this often holds the entire schema + hot indexes in RAM.
//     Browse + search go from "every query touches disk" to "every
//     query is a memcpy".
//
//   - temp_store=MEMORY: ORDER BY without a covering index, GROUP BY,
//     and CTEs spill intermediate results to a temp store. Default is
//     "FILE" — disk. MEMORY keeps them in RAM. Worst-case extra RSS
//     is the size of the largest intermediate, which is at most a few
//     MiB given our row sizes.
//
//   - mmap_size=268435456: 256 MiB of memory-mapped I/O. SQLite reads
//     pages via mmap when this is non-zero, eliminating one syscall
//     per page-fault. Big wins on FTS searches and JOINs across
//     items / metadata / images. The OS only commits actual pages
//     touched, so on a low-memory host the cost is bounded by
//     working-set size, not the 256 MiB ceiling.
//
//   - wal_autocheckpoint=1000: explicit at the default. Pinned here
//     because a future PRAGMA edit might silently shift it.
const sqlitePragmas = "" +
	"_pragma=journal_mode(WAL)" +
	"&_pragma=synchronous(NORMAL)" +
	"&_pragma=busy_timeout(5000)" +
	"&_pragma=foreign_keys(ON)" +
	"&_pragma=cache_size(-65536)" +
	"&_pragma=temp_store(MEMORY)" +
	"&_pragma=mmap_size(268435456)" +
	"&_pragma=wal_autocheckpoint(1000)"

// poolMaxOpenConns caps concurrent SQLite connections. SQLite with
// WAL allows multiple readers + one writer; capping helps avoid
// goroutine pile-ups but the real serialisation is in SQLite itself.
// 8 is enough headroom for two scanners + a UI + the IPTV transmux's
// EPG queries without queuing.
const poolMaxOpenConns = 8

// poolMaxIdleConns matches MaxOpenConns so a steady-state workload
// never reopens a connection. Default would be 2, which causes
// repeated open/close churn on a server that does sustained traffic.
const poolMaxIdleConns = 8

// poolConnMaxIdleTime caps how long an idle connection stays in the
// pool. After this it's closed. Prevents stale connections from
// hanging onto deleted WAL pages or holding the DB open against
// admin operations like VACUUM.
const poolConnMaxIdleTime = 5 * time.Minute

// Open creates and configures a SQLite database connection.
func Open(driver, path string, logger *slog.Logger) (*sql.DB, error) {
	if driver != "sqlite" {
		return nil, fmt.Errorf("unsupported driver %q (only sqlite supported in v1)", driver)
	}

	dsn := path + "?" + sqlitePragmas
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite: %w", err)
	}

	database.SetMaxOpenConns(poolMaxOpenConns)
	database.SetMaxIdleConns(poolMaxIdleConns)
	database.SetConnMaxIdleTime(poolConnMaxIdleTime)

	if err := database.Ping(); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("pinging sqlite: %w", err)
	}

	logger.Info("database opened",
		"driver", driver,
		"path", path,
		"pragmas", sqlitePragmas,
		"pool_max_open", poolMaxOpenConns)
	return database, nil
}

// Optimize runs `PRAGMA optimize` to refresh the query planner's
// statistics. Recommended by the SQLite docs for long-running
// processes; cheap to call (it's a no-op for tables that haven't
// changed since the last analysis).
//
// Wired into the graceful-shutdown path in main.go so every clean
// stop persists fresh statistics for the next start. Run periodically
// (every few hours) for very long-lived processes; we do it on
// shutdown which is sufficient for HubPlay's restart cadence.
//
// `analysis_limit=400` caps work per index to ~400 rows sampled —
// the SQLite docs recommend this for predictable latency on large
// DBs. Without it, optimize can spend O(table_size) time on tables
// that have grown a lot since last analysis.
func Optimize(ctx context.Context, database *sql.DB, logger *slog.Logger) {
	stmts := []string{
		"PRAGMA analysis_limit=400",
		"PRAGMA optimize",
	}
	for _, s := range stmts {
		if _, err := database.ExecContext(ctx, s); err != nil {
			// Best-effort. A failure here doesn't mean the DB is
			// broken — at worst the next process starts with stale
			// stats, costing a few µs per query until the next run.
			logger.Warn("sqlite optimize",
				"stmt", strings.TrimPrefix(s, "PRAGMA "),
				"err", err)
			return
		}
	}
	logger.Info("sqlite optimize: stats refreshed")
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
