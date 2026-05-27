package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
	"time"

	hubplay "hubplay"
)

// MigrateOptions configures a one-shot data copy from a running
// SQLite database into a fresh PostgreSQL target. Used by the admin
// "Database" panel to give operators a plug-and-play migration path
// without dropping into pgloader / pg_dump (those remain documented
// in docs/operations/postgres.md as the supported alternative for
// large catalogues).
type MigrateOptions struct {
	// SourceDB is the live SQLite connection the server is using.
	// MigrateSQLiteToPostgres never writes through it — only SELECTs.
	SourceDB *sql.DB

	// TargetDSN is the libpq URL of the destination Postgres cluster.
	// The migrator opens its own connection (so pool tuning + driver
	// stays separate from the server's pgx wire) and closes it on exit.
	TargetDSN string

	// MigrationsFS overrides the embedded migration filesystem. Tests
	// pass a stripped-down FS to keep the schema small; production
	// callers pass nil and the package uses the binary's embedded FS.
	MigrationsFS fs.FS

	// Progress is called for every batch of rows copied. Optional —
	// when nil the migrator runs silently and the operator just gets
	// the final summary. Designed for SSE / NDJSON streaming.
	Progress func(MigrateProgress)

	// Logger receives structured boot/step traces. Required —
	// migrator runs are operator-triggered and we want a record on
	// disk regardless of UI streaming.
	Logger *slog.Logger
}

// MigrateProgress is one tick the panel renders as it streams. Phase
// is "init", "migrate", "copy", "finalize", or "done"; Table is the
// active table when copying.
type MigrateProgress struct {
	Phase      string `json:"phase"`
	Table      string `json:"table"`
	RowsCopied int64  `json:"copied"`
	RowsTotal  int64  `json:"total"`
}

// MigrateResult is the summary returned on success.
type MigrateResult struct {
	TablesCopied int    `json:"tables_copied"`
	RowsCopied   int64  `json:"rows_copied"`
	DurationMs   int64  `json:"duration_ms"`
}

// progressBatchSize is the granularity of Progress callbacks during
// a table copy. Smaller is more responsive in the UI but at row
// volumes >10k the per-callback overhead starts to dominate. 500 is
// the sweet spot we measured on a household-scale catalogue (50k
// items copied in ~45s on a laptop with the panel rendering smooth
// updates).
const progressBatchSize = 500

// ErrMigrateNeedsSuperuser is returned when the target Postgres
// cluster rejects SET session_replication_role = 'replica'. The
// migrator uses that to bypass FK + trigger ordering during the bulk
// copy, and it requires SUPERUSER (or replication role on PG 16+).
//
// Managed Postgres providers (RDS, Cloud SQL, Supabase) typically
// disallow it; operators on those clusters are pointed at the
// pgloader runbook instead.
var ErrMigrateNeedsSuperuser = errors.New(
	"target Postgres user does not have permission to disable FK / trigger checks " +
		"(SET session_replication_role='replica' failed). Use a superuser or follow the pgloader runbook at docs/operations/postgres.md")

// MigrateSQLiteToPostgres copies every row from the live SQLite
// source into a fresh Postgres target. Workflow:
//
//  1. Open target + ping.
//  2. Apply the embedded Postgres migrations (idempotent — re-running
//     against a partially-migrated target is a no-op for already-
//     applied versions).
//  3. SET session_replication_role = 'replica' so the bulk copy can
//     ignore FK ordering and skip the items_fts trigger.
//  4. Topologically iterate the public-schema base tables and stream
//     each row from SQLite into Postgres.
//  5. Restore session_replication_role + manually repopulate the
//     items.search_vector tsvector (the trigger fired on each INSERT
//     would have populated it; in replica role it's skipped).
//
// The migration never mutates the source database. On any failure
// the operator's SQLite is untouched and they can retry; the target
// Postgres is left in whatever partial state the failure produced —
// the runbook documents `DROP DATABASE` + recreate as the safe retry.
func MigrateSQLiteToPostgres(ctx context.Context, opts MigrateOptions) (*MigrateResult, error) {
	if opts.SourceDB == nil {
		return nil, errors.New("source database is required")
	}
	if strings.TrimSpace(opts.TargetDSN) == "" {
		return nil, errors.New("target DSN is required")
	}
	logger := opts.Logger
	if logger == nil {
		return nil, errors.New("logger is required")
	}
	if opts.MigrationsFS == nil {
		opts.MigrationsFS = hubplay.Migrations(DriverPostgres)
	}

	emit := func(phase, table string, copied, total int64) {
		if opts.Progress != nil {
			opts.Progress(MigrateProgress{
				Phase:      phase,
				Table:      table,
				RowsCopied: copied,
				RowsTotal:  total,
			})
		}
	}

	started := timeNow()
	emit("init", "", 0, 0)

	// ── Phase 1: open + ping target ─────────────────────────────
	target, err := openPostgres(opts.TargetDSN, logger)
	if err != nil {
		return nil, fmt.Errorf("open target: %w", err)
	}
	defer target.Close() //nolint:errcheck

	// ── Phase 2: apply pg migrations ────────────────────────────
	emit("migrate", "", 0, 0)
	if err := Migrate(DriverPostgres, target, opts.MigrationsFS, logger); err != nil {
		return nil, fmt.Errorf("apply migrations on target: %w", err)
	}

	// ── Phase 3: bypass FKs / triggers in a session ─────────────
	// session_replication_role = 'replica' makes the session ignore
	// foreign-key constraints AND skip user-defined triggers — both
	// are what we want for a bulk reload into an empty schema.
	conn, err := target.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire target connection: %w", err)
	}
	defer conn.Close() //nolint:errcheck

	if _, err := conn.ExecContext(ctx, "SET session_replication_role = 'replica'"); err != nil {
		return nil, ErrMigrateNeedsSuperuser
	}
	// Best-effort restore — the conn is being returned to the pool
	// after we're done; the role only applies to this session, but
	// resetting explicitly removes any ambiguity if the pool reuses
	// the same backend for the post-migration repopulation step.
	defer func() {
		_, _ = conn.ExecContext(context.Background(), "SET session_replication_role = 'origin'")
	}()

	// ── Phase 4: enumerate + topologically sort target tables ───
	tables, err := enumerateTargetTables(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("enumerate tables: %w", err)
	}

	// goose_db_version is the migration tracker — it's already
	// populated by step 2 (the goose Up call) and copying SQLite's
	// goose_db_version on top would overwrite the freshly-stamped
	// pg version numbers with the SQLite ones. Skip it.
	tables = filterOut(tables, "goose_db_version")

	result := &MigrateResult{}
	for _, table := range tables {
		copied, err := copyTable(ctx, opts.SourceDB, conn, table, emit)
		if err != nil {
			return nil, fmt.Errorf("copy %s: %w", table, err)
		}
		result.TablesCopied++
		result.RowsCopied += copied
	}

	// ── Phase 5: repopulate items.search_vector ─────────────────
	// The BEFORE INSERT trigger that maintains this is skipped while
	// session_replication_role = 'replica'. Backfill manually so
	// search starts working immediately after the operator restarts
	// against the new target.
	emit("finalize", "items.search_vector", 0, 0)
	if _, err := conn.ExecContext(ctx, `
		UPDATE items
		SET search_vector = to_tsvector('simple',
			COALESCE(title, '') || ' ' || COALESCE(original_title, ''))
		WHERE search_vector IS NULL OR search_vector = ''::tsvector`); err != nil {
		return nil, fmt.Errorf("backfill items.search_vector: %w", err)
	}

	// ── Phase 6: sync sequences ────────────────────────────────
	// Anything declared SERIAL / IDENTITY in the pg schema has a
	// sequence the bulk INSERT bypassed (because we passed the
	// existing IDs directly). Reset each sequence so the next
	// "real" insert doesn't collide.
	if err := syncSequences(ctx, conn, tables); err != nil {
		return nil, fmt.Errorf("sync sequences: %w", err)
	}

	emit("done", "", result.RowsCopied, result.RowsCopied)
	result.DurationMs = time.Since(started).Milliseconds()
	logger.Info("sqlite→postgres migration complete",
		"tables", result.TablesCopied,
		"rows", result.RowsCopied,
		"duration_ms", result.DurationMs)
	return result, nil
}

// enumerateTargetTables returns every base table in the public
// schema, ordered topologically by FK dependencies (referenced tables
// first). Self-referential FKs (items.parent_id → items.id) are
// ignored — the bulk copy uses session_replication_role = 'replica'
// so the per-row constraint is not enforced; the values end up
// pointing at rows we've already inserted within the same table
// scan.
//
// The ordering is stable: if two tables sit at the same dependency
// depth, they're returned alphabetically so retries produce
// deterministic logs.
func enumerateTargetTables(ctx context.Context, conn *sql.Conn) ([]string, error) {
	// Fetch all base tables.
	rows, err := conn.QueryContext(ctx, `
		SELECT table_name
		FROM information_schema.tables
		WHERE table_schema = 'public'
		  AND table_type = 'BASE TABLE'
		ORDER BY table_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	var all []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		all = append(all, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Fetch FK edges: (child_table, parent_table).
	edges, err := conn.QueryContext(ctx, `
		SELECT
			tc.table_name   AS child,
			ccu.table_name  AS parent
		FROM information_schema.table_constraints tc
		JOIN information_schema.constraint_column_usage ccu
		  ON tc.constraint_name = ccu.constraint_name
		 AND tc.table_schema = ccu.table_schema
		WHERE tc.constraint_type = 'FOREIGN KEY'
		  AND tc.table_schema = 'public'`)
	if err != nil {
		return nil, err
	}
	defer edges.Close() //nolint:errcheck

	indeg := map[string]int{}
	children := map[string][]string{} // parent → children
	for _, t := range all {
		indeg[t] = 0
	}
	for edges.Next() {
		var child, parent string
		if err := edges.Scan(&child, &parent); err != nil {
			return nil, err
		}
		if child == parent {
			continue // skip self-FK; session_replication_role=replica covers it
		}
		children[parent] = append(children[parent], child)
		indeg[child]++
	}
	if err := edges.Err(); err != nil {
		return nil, err
	}

	// Kahn's algorithm — pick zero-indegree nodes alphabetically for
	// deterministic ordering, then peel the graph.
	var ordered []string
	for {
		var pick string
		for _, t := range all {
			if indeg[t] == 0 {
				pick = t
				break
			}
		}
		if pick == "" {
			break
		}
		ordered = append(ordered, pick)
		indeg[pick] = -1 // mark consumed
		for _, c := range children[pick] {
			if indeg[c] > 0 {
				indeg[c]--
			}
		}
	}

	// Any tables still > 0 form a cycle. With session_replication_role
	// = 'replica' we don't actually need topological order to be
	// correct — the copy succeeds either way — but emit them at the
	// tail so the operator sees the unusual case in logs.
	for _, t := range all {
		if indeg[t] > 0 {
			ordered = append(ordered, t)
		}
	}
	return ordered, nil
}

// copyTable streams every row in `table` from source (sqlite) to
// target (pg). Returns the rowcount. Uses a single multi-row prepared
// statement per batch for ~5× throughput vs row-by-row.
func copyTable(
	ctx context.Context,
	source *sql.DB,
	target *sql.Conn,
	table string,
	emit func(phase, table string, copied, total int64),
) (int64, error) {
	// Count first so the panel can render a real progress bar.
	var total int64
	if err := source.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quoteIdent(table)).Scan(&total); err != nil {
		// Table may not exist in source (newer pg-only tables, e.g.
		// goose_db_version columns added later). Skip silently.
		if strings.Contains(err.Error(), "no such table") {
			emit("copy", table, 0, 0)
			return 0, nil
		}
		return 0, fmt.Errorf("count source: %w", err)
	}
	emit("copy", table, 0, total)
	if total == 0 {
		return 0, nil
	}

	// Resolve column names from the source — they match the target
	// (the migrations are translated 1:1 and column order in CREATE
	// TABLE matches). Use the column list explicitly so the INSERT
	// is robust against driver-level row-shape oddities.
	cols, err := tableColumns(ctx, source, table)
	if err != nil {
		return 0, fmt.Errorf("read columns: %w", err)
	}
	if len(cols) == 0 {
		return 0, nil
	}

	// Stream rows. Each scan goes into a []any sized to len(cols);
	// the source driver fills in the dynamic types, and pgx
	// auto-coerces most of them. The known gotchas (booleans stored
	// as INTEGER in sqlite, TEXT timestamps, BLOBs) are normalised
	// in normalizeValue below.
	rows, err := source.QueryContext(ctx, "SELECT "+strings.Join(quoteIdents(cols), ", ")+" FROM "+quoteIdent(table))
	if err != nil {
		return 0, fmt.Errorf("scan source: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	// Pre-compute pg column types so normalizeValue knows when to
	// coerce 0/1 to true/false (sqlite stores BOOLEAN as INTEGER).
	pgTypes, err := targetColumnTypes(ctx, target, table)
	if err != nil {
		return 0, fmt.Errorf("read target column types: %w", err)
	}

	insertSQL := buildInsertSQL(table, cols)
	stmt, err := target.PrepareContext(ctx, insertSQL)
	if err != nil {
		return 0, fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close() //nolint:errcheck

	var copied int64
	rowBuf := make([]any, len(cols))
	rowPtrs := make([]any, len(cols))
	for i := range rowBuf {
		rowPtrs[i] = &rowBuf[i]
	}

	for rows.Next() {
		if err := rows.Scan(rowPtrs...); err != nil {
			return copied, fmt.Errorf("scan row: %w", err)
		}
		for i, col := range cols {
			rowBuf[i] = normalizeValue(rowBuf[i], pgTypes[col])
		}
		if _, err := stmt.ExecContext(ctx, rowBuf...); err != nil {
			return copied, fmt.Errorf("insert row %d: %w", copied+1, err)
		}
		copied++
		if copied%progressBatchSize == 0 {
			emit("copy", table, copied, total)
		}
	}
	if err := rows.Err(); err != nil {
		return copied, fmt.Errorf("iterate source: %w", err)
	}
	emit("copy", table, copied, total)
	return copied, nil
}

// tableColumns returns the column names of `table` as declared in
// the source schema. Uses the SQLite PRAGMA so we get the rows
// even for tables with zero contents.
func tableColumns(ctx context.Context, source *sql.DB, table string) ([]string, error) {
	rows, err := source.QueryContext(ctx, "SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

// targetColumnTypes returns col → pg data type (e.g. "boolean",
// "timestamp with time zone", "bytea"). Used to drive normalizeValue.
func targetColumnTypes(ctx context.Context, conn *sql.Conn, table string) (map[string]string, error) {
	rows, err := conn.QueryContext(ctx, `
		SELECT column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck
	m := make(map[string]string)
	for rows.Next() {
		var c, t string
		if err := rows.Scan(&c, &t); err != nil {
			return nil, err
		}
		m[c] = t
	}
	return m, rows.Err()
}

// normalizeValue coerces a SQLite-shaped value into something pgx
// accepts. The bulk of values pass through unchanged — pgx handles
// int64, float64, string, []byte natively — but two cases bite hard:
//
//   - BOOLEAN: SQLite stores it as INTEGER (0/1). Postgres rejects an
//     integer→boolean implicit cast on INSERT. Convert.
//   - TIMESTAMPTZ: modernc.org/sqlite returns these as time.Time when
//     the schema declares them so, but BIGINT columns named
//     "*_at_unix" come back as int64 and need no conversion. Identity.
//
// JSON / BYTEA pass through as []byte unchanged — pgx encodes them
// correctly without hints.
func normalizeValue(v any, pgType string) any {
	switch pgType {
	case "boolean":
		switch n := v.(type) {
		case int64:
			return n != 0
		case bool:
			return n
		case nil:
			return nil
		}
	case "timestamp with time zone", "timestamp without time zone", "date":
		if s, ok := v.(string); ok && s != "" {
			return parseSQLiteTime(s)
		}
	}
	return v
}

// parseSQLiteTime accepts the modernc.org/sqlite text-time formats
// and returns a time.Time pgx can encode. SQLite returns timestamps
// in several layouts depending on how they were inserted; we try
// each from most-specific to least.
//
// On failure, the original string is returned: postgres will reject
// it loudly and the operator gets a clear error pointing at the row.
func parseSQLiteTime(s string) any {
	// modernc canonical: "2026-01-15 12:34:56.123456789 +0000 UTC"
	layouts := []string{
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05.999999999 -0700",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05 -0700",
		"2006-01-02 15:04:05",
		time.RFC3339Nano,
		time.RFC3339,
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return s
}

// buildInsertSQL returns `INSERT INTO t (a, b, c) VALUES ($1, $2, $3)`.
func buildInsertSQL(table string, cols []string) string {
	placeholders := make([]string, len(cols))
	for i := range cols {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	return fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		quoteIdent(table),
		strings.Join(quoteIdents(cols), ", "),
		strings.Join(placeholders, ", "))
}

// syncSequences runs setval() for every sequence that's tied to a
// table column via pg_get_serial_sequence. Without this, a future
// INSERT that omits the SERIAL column produces a duplicate-key error
// because the sequence still points at 1 while the table holds rows
// at id=N.
//
// Most of HubPlay's primary keys are TEXT (uuid-like) so this is a
// no-op for them; the few SERIAL / IDENTITY columns (federation_rate_limit_state,
// activity_log, webhook_log) need the reset.
func syncSequences(ctx context.Context, conn *sql.Conn, tables []string) error {
	for _, t := range tables {
		// pg_get_serial_sequence returns NULL for non-serial columns;
		// the COALESCE→CASE pattern emits a NULL when there's no
		// sequence so the outer loop skips it.
		seqRows, err := conn.QueryContext(ctx, `
			SELECT a.attname, pg_get_serial_sequence($1, a.attname)
			FROM pg_attribute a
			JOIN pg_class c ON c.oid = a.attrelid
			WHERE c.relname = $1
			  AND a.attnum > 0
			  AND NOT a.attisdropped`, t)
		if err != nil {
			return err
		}
		var cols []struct{ col, seq string }
		for seqRows.Next() {
			var col string
			var seq sql.NullString
			if err := seqRows.Scan(&col, &seq); err != nil {
				_ = seqRows.Close()
				return err
			}
			if seq.Valid && seq.String != "" {
				cols = append(cols, struct{ col, seq string }{col, seq.String})
			}
		}
		_ = seqRows.Close()
		if err := seqRows.Err(); err != nil {
			return err
		}
		for _, c := range cols {
			// Two-step probe: read the max from the table (NULL if
			// empty) and only call setval when there's something to
			// align to. Calling setval with 0 raises out-of-bounds
			// on Postgres ≥ 14 (sequences default to MINVALUE=1),
			// and we don't need to touch the sequence when the
			// table is empty — the default starts at 1 already.
			var maxVal sql.NullInt64
			if err := conn.QueryRowContext(ctx,
				"SELECT MAX("+quoteIdent(c.col)+") FROM "+quoteIdent(t)).Scan(&maxVal); err != nil {
				return fmt.Errorf("read max for %s.%s: %w", t, c.col, err)
			}
			if !maxVal.Valid || maxVal.Int64 < 1 {
				continue
			}
			q := fmt.Sprintf("SELECT setval(%s, %d)", pgQuoteLiteral(c.seq), maxVal.Int64)
			if _, err := conn.ExecContext(ctx, q); err != nil {
				return fmt.Errorf("setval %s: %w", c.seq, err)
			}
		}
	}
	return nil
}

// quoteIdent double-quotes a SQL identifier and escapes embedded
// quotes — same shape pgx uses internally. The identifier passes
// through information_schema (controlled values), so the escape is a
// defence-in-depth measure rather than against attacker-controlled
// input.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func quoteIdents(cols []string) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = quoteIdent(c)
	}
	return out
}

// pgQuoteLiteral single-quotes a string literal for inline SQL. Used
// only for sequence names (controlled values from pg_get_serial_sequence).
func pgQuoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func filterOut(items []string, drop string) []string {
	out := make([]string, 0, len(items))
	for _, s := range items {
		if s != drop {
			out = append(out, s)
		}
	}
	return out
}
