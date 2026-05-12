# PostgreSQL backend — migration & translation guide

This document is a recipe for porting the SQLite source-of-truth schema
+ queries to a parallel PostgreSQL tree. Read top to bottom before
touching anything — most decisions are already made; you're following
a path, not blazing one.

**Audience**: contributors picking up the dual-dialect work across
multiple sessions. Each session should leave the tree in a state where
`go build` + SQLite tests still pass.

---

## Architecture in one paragraph

HubPlay keeps SQLite as the default backend (zero-ops self-hosted
sweet spot). PostgreSQL is the opt-in production-scale alternative.
Both backends are accessed through identical Go repository interfaces;
the implementation under the hood is selected at boot via
`cfg.Database.Driver`. sqlc generates two parallel packages
(`internal/db/sqlc/` for SQLite, `internal/db/sqlc_pg/` for Postgres);
the repo layer holds whichever was constructed. The driver bridge is
`pgx/v5/stdlib` so we can keep using `*sql.DB` across the codebase
without rewriting every repo for pgx's native API.

---

## Directory layout

```
migrations/
  sqlite/           ← source of truth (41 files)
  postgres/         ← parallel translations (this guide is how)

internal/db/
  queries/          ← SQLite query files, sqlc input
  queries-postgres/ ← Postgres query files, sqlc input
  sqlc/             ← generated SQLite bindings
  sqlc_pg/          ← generated Postgres bindings
  *_repository.go   ← repo layer — interface unchanged, ctor branches
```

`sqlc.yaml` has two `sql` entries (one per engine). Run `make sqlc`
and both packages regenerate.

---

## Translation rules — schema (.sql in migrations/)

The vast majority of SQLite syntax is identical in Postgres. The
deltas:

### Types

| SQLite | PostgreSQL | When | Note |
|---|---|---|---|
| `DATETIME` | `TIMESTAMPTZ` | always | TZ-aware is the safe default. Code reads into `time.Time` regardless. |
| `BOOLEAN DEFAULT 0` | `BOOLEAN DEFAULT FALSE` | always | SQLite stores bool as 0/1; Postgres requires the keyword. |
| `BOOLEAN DEFAULT 1` | `BOOLEAN DEFAULT TRUE` | always | Same |
| `REAL` | `DOUBLE PRECISION` | always | SQLite's REAL is 8-byte float; equivalent. |
| `INTEGER` (small) | `INTEGER` | always | 32-bit int. |
| `INTEGER` (tick / size / large) | `BIGINT` | when storing `*_ticks` or `size` fields | SQLite INTEGER is dynamically sized, Postgres is fixed. Anything that could exceed `2^31` (movie tick counts, file sizes) MUST be BIGINT. |
| `TEXT` | `TEXT` | always | Identical. |
| `BLOB` | `BYTEA` | if any | None in HubPlay so far. |

**Quick scan rule**: in every `CREATE TABLE`, find columns named
`*_ticks`, `size`, `duration_ticks`, `position_ticks`, `bytes`. These
must be `BIGINT` in Postgres.

### Constraints & defaults

| SQLite | PostgreSQL |
|---|---|
| `PRIMARY KEY` | `PRIMARY KEY` (unchanged) |
| `PRIMARY KEY (a, b)` | `PRIMARY KEY (a, b)` (unchanged) |
| `REFERENCES … ON DELETE CASCADE` | (unchanged) |
| `REFERENCES … ON DELETE SET NULL` | (unchanged) |
| `UNIQUE(a, b)` | (unchanged) |
| `CHECK (col IN ('a','b'))` | (unchanged) |
| `DEFAULT CURRENT_TIMESTAMP` | (unchanged) |
| `STRICT` table modifier | omit — Postgres is strict by default |
| `WITHOUT ROWID` | omit |

### Indexes

`CREATE INDEX …` syntax is identical. **Opportunity** (do NOT do
blindly — wait for the perf pass):
- Partial indexes: `WHERE deleted_at IS NULL` etc.
- BRIN indexes for time-series columns (epg_programs.start_time,
  federation_audit_log.created_at) — much smaller than B-tree.
- Covering indexes with INCLUDE clauses.

These are NOT in the 1:1 translation. Tackle them in a separate
performance PR after the dual-dialect compiles.

### FTS (full-text search)

`migrations/sqlite/002_fts_search.sql` uses SQLite's `fts5` extension.
This is the **biggest divergence**. Postgres equivalent is
`tsvector` + GIN index. Both shapes do "search text, get items
ranked by relevance", but the API is dialect-specific.

For initial translation:
1. The Postgres version of `002_fts_search.sql` should NOT use `fts5`
   syntax (it doesn't exist). Replace with:
   ```sql
   ALTER TABLE items ADD COLUMN search_vector tsvector;
   CREATE INDEX idx_items_fts ON items USING GIN(search_vector);
   -- Plus a trigger to populate search_vector from title +
   -- original_title + (metadata.overview joined) on insert/update.
   ```
2. The SearchItems query needs a Postgres-specific variant using
   `WHERE search_vector @@ plainto_tsquery('english', $1)
    ORDER BY ts_rank(search_vector, plainto_tsquery('english', $1)) DESC`.
3. Multilingual: consider `tsvector('simple', ...)` for accent
   tolerance, or load language-specific dictionaries.

This is one of the harder migrations. Allocate ~half a day for it.

### UPSERT patterns

The SQLite codebase uses several upsert shapes. Translation table:

| SQLite | Postgres |
|---|---|
| `INSERT OR IGNORE INTO t (a,b) VALUES (?,?)` | `INSERT INTO t (a,b) VALUES ($1,$2) ON CONFLICT DO NOTHING` |
| `INSERT OR REPLACE INTO t (a,b) VALUES (?,?)` | `INSERT INTO t (a,b) VALUES ($1,$2) ON CONFLICT (a) DO UPDATE SET b = EXCLUDED.b` |
| `INSERT … ON CONFLICT(col) DO UPDATE SET …` | (unchanged — both support this) |
| `INSERT … ON CONFLICT(col) DO NOTHING` | (unchanged) |

**Important**: Postgres requires you to NAME the conflict target
explicitly (the constraint or column list). SQLite's `INSERT OR
IGNORE` infers it from any constraint violation. Be deliberate when
translating: a row that violates two constraints behaves differently.

### Date / time arithmetic

| SQLite | Postgres |
|---|---|
| `datetime('now', '-7 days')` | `NOW() - INTERVAL '7 days'` |
| `strftime('%Y-%m-%d', col)` | `TO_CHAR(col, 'YYYY-MM-DD')` |
| `SUBSTR(col, 1, 10)` | (unchanged) — both support SUBSTR |
| `EXTRACT(YEAR FROM col)` | (unchanged in both, but more idiomatic in Postgres) |

The home repository's daily-bucket queries (used by
`internal/api/handlers/system.go:StreamActivity` and trending) lean
heavily on date arithmetic. Translate carefully — there is room for
silent semantic drift here.

### Placeholders

| SQLite | Postgres |
|---|---|
| `?` (positional, anonymous) | `$1`, `$2`, … (positional, numbered) |

sqlc handles this automatically when the `engine` is set in
`sqlc.yaml`. You don't write `$N` by hand; you write `?` in the
query file and sqlc rewrites for postgres. **However**, if you reuse
the same parameter twice in a query, SQLite repeats `?` while
Postgres references the same `$N`. sqlc handles this with
`sqlc.arg(name)` syntax if you need clarity.

---

## Translation rules — queries (.sql in queries-postgres/)

For each file in `internal/db/queries/`, create a sibling in
`internal/db/queries-postgres/` with the same name. The Go function
names emitted by `-- name: FunctionName :return-shape` MUST be
identical so the generated `Queries` interfaces match.

Most query files will be **near-identical** between the two — just
substitute UPSERT shapes and date math where needed. Run a diff
between the two files; if it's more than ~5 lines different,
double-check the semantics match.

### Things that DO need attention per query

1. **Boolean parameters**: SQLite accepts `0/1` for boolean; Postgres
   wants `TRUE/FALSE`. sqlc usually handles this via Go types.
2. **NULL ordering**: `ORDER BY col` puts NULLs LAST in Postgres,
   FIRST in SQLite. Add `NULLS LAST` explicitly where the order
   matters semantically.
3. **`COALESCE` with strings**: SQLite often returns empty string
   from `COALESCE(col, '')`; Postgres requires consistent typing.
   No-op for HubPlay's existing queries, just be aware.
4. **`LIMIT` with subqueries**: Postgres requires subqueries to be
   parenthesized in some contexts SQLite doesn't.
5. **The raw-SQL holdouts**: `internal/db/library_repository.go` and
   a few others have raw SQL bypassing sqlc due to the 1.31.1
   parser bug (see `docs/memory/architecture-decisions.md`). These
   need TWO versions: one for SQLite (existing), one for Postgres.
   Branch in the repo method.

---

## Translation rules — repo layer (Go)

Today each repo is a struct with a `*sql.DB` and `*sqlc.Queries`
embedded. Pattern post-migration:

```go
type SettingsRepository struct {
    db *sql.DB
    // Generated query handles. Exactly one is non-nil based on
    // the driver chosen at construction. The interface emitted
    // by sqlc (Querier) is the same for both, so callers see
    // a single method set.
    sqlite *sqlc.Queries
    pg     *sqlc_pg.Queries
}

func NewSettingsRepository(driver string, db *sql.DB) *SettingsRepository {
    r := &SettingsRepository{db: db}
    switch driver {
    case "postgres":
        r.pg = sqlc_pg.New(db)
    default:
        r.sqlite = sqlc.New(db)
    }
    return r
}

// Each method picks the right backend transparently.
func (r *SettingsRepository) Get(ctx context.Context, key string) (string, error) {
    if r.pg != nil {
        return r.pg.GetSetting(ctx, key)
    }
    return r.sqlite.GetSetting(ctx, key)
}
```

For repos with many methods (most), this branching becomes
boilerplate. Two cleaner alternatives:

**Option A — internal interface (recommended)**:
```go
type settingsQueries interface {
    GetSetting(ctx context.Context, key string) (string, error)
    SetSetting(ctx context.Context, args SetSettingParams) error
    // ...
}

type SettingsRepository struct {
    db *sql.DB
    q  settingsQueries  // *sqlc.Queries or *sqlc_pg.Queries
}

func NewSettingsRepository(driver string, db *sql.DB) *SettingsRepository {
    r := &SettingsRepository{db: db}
    if driver == "postgres" {
        r.q = sqlc_pg.New(db)
    } else {
        r.q = sqlc.New(db)
    }
    return r
}
```

The interface is hand-rolled (kept in the repo file). Both generated
`Queries` types satisfy it because they expose the same method
signatures (we made sure of that by keeping query names + return
shapes identical). This is the cleanest and what we'll go with.

**Option B — generics with a type parameter**: more elegant in theory
but requires Go's type inference to handle methods on `*Queries`
pointers; gets ugly with method sets. Avoid.

---

## Step-by-step plan for the dual-dialect work (multi-session)

### Session A — Foundation (~1 day) ✅ DONE in current PR

- [x] Add postgres engine to `sqlc.yaml`
- [x] Create `migrations/postgres/` with `001_initial_schema.sql` translated
- [x] Create `internal/db/queries-postgres/` directory
- [x] Add `github.com/jackc/pgx/v5` to `go.mod`
- [x] Write this guide

### Session B — Schema translation (~1 day)

- [ ] Translate `migrations/sqlite/002_fts_search.sql` to tsvector +
      GIN (the hardest one)
- [ ] Translate `003_add_indexes.sql` through `019_app_settings.sql`
- [ ] Run `sqlc generate` and confirm both packages emit identical
      `Querier` interfaces (you'll see compile errors when they don't)
- [ ] Add a smoke test: spin a Postgres container via testcontainers
      and run `goose up` against `migrations/postgres/` — must succeed

### Session C — Schema translation cont. (~1 day)

- [ ] Translate migrations 020 through 041 (federation, IPTV
      extensions, household model, etc.)
- [ ] Smoke test ALL migrations via testcontainers
- [ ] Add a CI job that runs migrations both for SQLite (existing)
      and Postgres (new)

### Session D — Query files (~1 day)

- [ ] Copy every file from `queries/` to `queries-postgres/`
- [ ] Apply UPSERT translations + date math + booleans per the
      tables above
- [ ] `sqlc generate` and confirm interface parity
- [ ] Add a test that `len(sqlc.Querier methods) == len(sqlc_pg.Querier methods)`

### Session E — Repo layer (~2 days)

- [ ] Refactor each repo to use the internal-interface pattern
      (Option A above). 14 repos total: settings, users, sessions,
      libraries, items, media_streams, images, metadata, user_data,
      home, providers, federation, channels, epg_programs.
- [ ] Update `main.go` to construct repos with the driver string.
- [ ] All existing SQLite tests must still pass with no changes.
- [ ] Add parallel `_postgres_test.go` files using testcontainers
      for the critical repos (settings, users, items, user_data).

### Session F — Connection wiring + pgx (~half day)

- [ ] In `main.go`, when `cfg.Database.Driver == "postgres"`, register
      the `pgx/v5/stdlib` driver and use the DSN.
- [ ] Configure `pgxpool` parameters (MaxConns, MinConns,
      MaxConnLifetime) from a new config block.
- [ ] Add health-check probe in admin panel that distinguishes
      SQLite ping vs Postgres ping.
- [ ] Document the connection-string format in `hubplay.example.yaml`.

### Session G — Migration CLI (`hubplay migrate-db`) + admin UI (~2 days)

- [ ] New CLI subcommand using `pgloader` under the hood (pull image
      `dimitri/pgloader` or vendor a static binary).
- [ ] Admin panel section "Base de datos" with "Probar conexión" +
      copy-paste instructions for the CLI command.
- [ ] After-migration marker file + admin-panel "Migración completa"
      banner that disappears when the marker is removed.

### Session H — Worker pool with backend-aware auto-tune (~3 days)

- [ ] New `internal/workerpool/` package with the pipeline-stages
      pattern described in the conversation (this conversation's
      Postgres tools section).
- [ ] `AutoTuneWorkers(driver)`: SQLite → 1 writer; Postgres → cores
      writers.
- [ ] Migrate scanner / image processor / provider fetcher to use
      worker-pool stages instead of inline goroutines.
- [ ] Add `LISTEN/NOTIFY` channel for cross-instance events when
      Postgres is selected (graceful no-op on SQLite).

### Session I — Extensions + indexes for Postgres (~1 day)

- [ ] Migration that runs `CREATE EXTENSION IF NOT EXISTS pg_trgm`
      and adds a GIN index on items.title for fuzzy search.
- [ ] Migration that enables `pg_stat_statements` (operator must
      add to `shared_preload_libraries`; document this).
- [ ] Add the partial indexes + BRIN indexes identified in the perf
      audit (only after Session H).

### Session J — Production hardening (~1 day)

- [ ] `postgres_exporter` example docker-compose for Prometheus
- [ ] `pg_dump`-based backup that replaces the SQLite backup endpoint
      when Postgres is selected
- [ ] Operations doc: how to upgrade Postgres version safely, how to
      restore from a `pg_dump`, recommended `postgresql.conf` tunings
      for self-hosted

---

## Anti-patterns to avoid

1. **Don't use ORMs**: GORM / ent etc. hide what's executing. The
   project has chosen sqlc for typed, visible queries. Stay there.

2. **Don't drop SQLite support**: SQLite is the zero-ops sweet spot
   for ~95% of self-hosted users. The dual-dialect work makes
   Postgres available, NOT mandatory.

3. **Don't introduce Redis as a cache layer**: Postgres with good
   indexes + the existing in-process caches are enough. Redis adds
   operational complexity (one more thing to back up, monitor, fail).

4. **Don't run goose against both schemas in the same call**: the
   schema versions might drift if a migration only applies to one
   dialect. Use the driver to pick the migrations directory at boot.

5. **Don't use pgx's native API in repos**: stick with `database/sql`
   + `pgx/v5/stdlib` so the repo code is dialect-agnostic. Drop down
   to pgx native only in specific hot paths (worker pool's bulk
   inserts via `COPY FROM`).

6. **Don't add Postgres-specific features lightly**: every time you
   write a query that LISTEN/NOTIFYs or uses RETURNING…INTO etc.,
   you create a divergence the SQLite path can't follow. Use them
   only where there's clear value (worker pool, event bus).

---

## Testing strategy

- **SQLite tests** stay as-is. They run fast (in-memory) and cover
  the default path 99% of users hit.
- **Postgres tests** use `testcontainers-go`. Slower (~2 s/test
  initial container) but cached after that. Add only for critical
  repos + migrations + queries with dialect-specific syntax.
- **CI** runs both. Use Github Actions matrix:
  ```yaml
  strategy:
    matrix:
      database: [sqlite, postgres]
  ```
- **Migration tests**: a single test per dialect that runs
  `goose up` from empty against the full migration tree. Catches
  syntax errors early.

---

## Where to ask questions while doing this

- sqlc bugs / dialect oddities: search the project memory for
  documented holdouts (`docs/memory/architecture-decisions.md`).
- Postgres-specific behavior questions: official docs are
  excellent — https://www.postgresql.org/docs/
- pgx specifics: https://github.com/jackc/pgx
- testcontainers-go: https://golang.testcontainers.org/

---

## Estimated total effort

| Session | Hours | Cumulative |
|---|---:|---:|
| A — Foundation (done) | 3 | 3 |
| B — Schema part 1 | 7 | 10 |
| C — Schema part 2 | 7 | 17 |
| D — Queries | 7 | 24 |
| E — Repos | 14 | 38 |
| F — Wiring | 3 | 41 |
| G — Migration CLI + UI | 14 | 55 |
| H — Worker pool | 21 | 76 |
| I — Extensions / indexes | 7 | 83 |
| J — Production hardening | 7 | 90 |

**~90 hours of focused work**, distributed across 10 sessions. The
project doesn't need to do all of them — sessions A through F deliver
"functional Postgres mode", and G–J are the productionizing layer.

A pragmatic v1 = A through F + G (migration UX). H–J can land later.
