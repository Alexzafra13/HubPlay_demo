package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

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

// sqlitePoolMaxOpenConns caps concurrent SQLite connections. SQLite
// with WAL allows multiple readers + one writer; capping helps avoid
// goroutine pile-ups but the real serialisation is in SQLite itself.
// 8 is enough headroom for two scanners + a UI + the IPTV transmux's
// EPG queries without queuing.
const sqlitePoolMaxOpenConns = 8

// sqlitePoolMaxIdleConns matches MaxOpenConns so a steady-state
// workload never reopens a connection. Default would be 2, which
// causes repeated open/close churn on a server that does sustained
// traffic.
const sqlitePoolMaxIdleConns = 8

// sqlitePoolConnMaxIdleTime caps how long an idle connection stays in
// the pool. After this it's closed. Prevents stale connections from
// hanging onto deleted WAL pages or holding the DB open against admin
// operations like VACUUM.
const sqlitePoolConnMaxIdleTime = 5 * time.Minute

// openSQLite creates and configures a SQLite database connection.
// Driver-specific arm of db.Open; the dispatcher in database.go picks
// this when cfg.Database.Driver == "sqlite".
func openSQLite(path string, logger *slog.Logger) (*sql.DB, error) {
	dsn := path + "?" + sqlitePragmas
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite: %w", err)
	}

	database.SetMaxOpenConns(sqlitePoolMaxOpenConns)
	database.SetMaxIdleConns(sqlitePoolMaxIdleConns)
	database.SetConnMaxIdleTime(sqlitePoolConnMaxIdleTime)

	if err := database.Ping(); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("pinging sqlite: %w", err)
	}

	logger.Info("database opened",
		"driver", DriverSQLite,
		"path", path,
		"pragmas", sqlitePragmas,
		"pool_max_open", sqlitePoolMaxOpenConns)
	return database, nil
}

// optimizeSQLite runs `PRAGMA optimize` plus an FTS5 inverted-index
// merge. Both refresh data structures the query planner and search
// rely on:
//
//   - `PRAGMA optimize` recomputes per-table statistics (which
//     indexes are useful, how skewed the data is). After many INSERTs
//     and UPDATEs, stale stats produce sub-optimal plans (full scans
//     where an index lookup would do). Cheap and idempotent — a
//     no-op for tables that haven't changed since last run.
//
//   - `INSERT INTO items_fts(items_fts) VALUES('optimize')` merges
//     the FTS5 inverted-index segments. Each scan-time INSERT/UPDATE
//     of an `items` row appends a new segment; without periodic
//     merging the index slowly fragments and search latency grows
//     O(segments). The merge is incremental — bounded by the
//     `analysis_limit` so a 50k-row index doesn't block for seconds.
//
// `analysis_limit=400` caps work per index to ~400 rows sampled — the
// SQLite docs recommend this for predictable latency on large DBs.
// Without it, optimize can spend O(table_size) time on tables that
// have grown a lot since last analysis.
//
// Best-effort: any failure logs and continues. Stale stats / a
// fragmented FTS at worst cost a few µs per query until the next
// successful run; nothing operational depends on this completing.
//
// Postgres equivalents (ANALYZE, REINDEX) are not driven from here —
// Postgres auto-vacuum keeps statistics current and FTS uses the
// regular tsvector index that the trigger maintains on every row
// write. See Optimize() for the dispatcher.
func optimizeSQLite(ctx context.Context, database *sql.DB, logger *slog.Logger) {
	stmts := []string{
		"PRAGMA analysis_limit=400",
		"PRAGMA optimize",
		"INSERT INTO items_fts(items_fts) VALUES('optimize')",
	}
	for _, s := range stmts {
		if _, err := database.ExecContext(ctx, s); err != nil {
			logger.Warn("sqlite optimize",
				"stmt", trimPragma(s),
				"err", err)
			return
		}
	}
	logger.Info("sqlite optimize: stats refreshed + FTS merged")
}

func trimPragma(s string) string {
	if rest, ok := strings.CutPrefix(s, "PRAGMA "); ok {
		return rest
	}
	return s
}
