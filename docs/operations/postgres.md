# Running HubPlay on Postgres

HubPlay ships with two supported database backends: **SQLite** (the
default, zero-config) and **PostgreSQL 13+** (alternative for larger
installs). The same binary works against either; the choice lives in
`hubplay.yaml`.

This document is for operators. For the internal design of the
dual-dialect refactor see `docs/architecture/postgres-migration.md`.

---

## When to pick Postgres

SQLite covers the overwhelming majority of self-hosted setups. Reach
for Postgres only when one of these is true:

- Your catalogue exceeds a few hundred thousand items and search /
  browse latency under SQLite stops feeling instant.
- You want the database on a separate host from the HubPlay process
  (e.g. shared with another app, or you already manage Postgres for
  other services).
- You need point-in-time recovery, streaming replicas, or another
  Postgres-specific operational feature.

There is no functional difference: every HubPlay feature works the
same on both backends.

---

## Fresh install on Postgres

1. Provision a database and a role that owns it:

   ```sql
   CREATE ROLE hubplay LOGIN PASSWORD 'change-me';
   CREATE DATABASE hubplay OWNER hubplay;
   ```

2. Set the driver + DSN in `hubplay.yaml`:

   ```yaml
   database:
     driver: "postgres"
     dsn: "postgres://hubplay:change-me@localhost:5432/hubplay?sslmode=disable"
   ```

   `sslmode=require` (or `verify-full` with a cert) is recommended
   when the DB is on a separate host.

3. Start the server. It runs the embedded Postgres migrations
   (`migrations/postgres/*.sql`) on first boot — no manual `goose up`
   needed.

The setup wizard pages render the same as on SQLite.

---

## Migrating an existing SQLite install to Postgres

HubPlay does not include a built-in data migrator: a self-hosted
sqlite → postgres move is a once-per-install operation and the
ecosystem has a mature tool that handles every gotcha (BLOB encoding,
sequence autoincrements, timestamp zones, etc.):

[**pgloader**](https://pgloader.io) (apt `pgloader`, brew `pgloader`, Docker
`dimitri/pgloader`).

### One-shot migration

```bash
# 1. Stop HubPlay so the SQLite file isn't being written to.
systemctl stop hubplay   # or docker compose stop hubplay

# 2. Create the target Postgres role + empty database (as above).

# 3. Apply the Postgres schema with goose so the columns line up
#    with what HubPlay expects. The Docker image bundles goose and
#    the migrations; run it once pointed at the empty DB:
docker run --rm --network host -v "$PWD:/app" -w /app \
  ghcr.io/pressly/goose:latest \
  -dir migrations/postgres postgres \
  "postgres://hubplay:change-me@localhost:5432/hubplay?sslmode=disable" up

# 4. Run pgloader. The minimal config copies every table from the
#    SQLite file into the equivalent Postgres tables, skipping the
#    schema (we already applied migrations).
cat > migrate.load <<'EOF'
LOAD DATABASE
  FROM   sqlite:///path/to/hubplay.db
  INTO   postgresql://hubplay:change-me@localhost:5432/hubplay
WITH    data only,
        truncate
SET     search_path TO 'public';
EOF
pgloader migrate.load

# 5. Edit hubplay.yaml: driver: "postgres" + dsn: "..."
# 6. Start HubPlay against Postgres. Spot-check the home page,
#    a library, and play one item before declaring success.
```

`pgloader` reports any row it couldn't migrate; review the summary
before declaring done. The SQLite file is left untouched — keep it as
a rollback artefact for at least one week.

### Rollback to SQLite

If something goes wrong, revert `hubplay.yaml` to the SQLite block and
restart. The original `hubplay.db` is unchanged; any progress / new
items recorded against Postgres after the cutover would be lost, but
the catalogue itself is intact.

---

## Backup and restore on Postgres

The admin "Copia de seguridad" surface in the UI is **SQLite-only**.
On Postgres both endpoints return `501 Not Implemented` with a
message pointing here. This is intentional: orchestrating
`pg_dump`/`pg_restore` from the application would need cluster
credentials, network access, and the operator's storage choices —
all of which a self-hosted app should not assume.

Use the Postgres native tools out-of-band:

```bash
# Daily backup to a file (run from cron / systemd timer):
pg_dump --format=custom --file=hubplay-$(date +%F).dump \
  "postgres://hubplay:change-me@localhost:5432/hubplay"

# Restore into a fresh empty database:
pg_restore --clean --if-exists \
  -d "postgres://hubplay:change-me@localhost:5432/hubplay" \
  hubplay-2026-05-13.dump
```

For point-in-time recovery, use Postgres' WAL archiving — same as for
any other Postgres app you operate.

---

## Operational notes

- **Pool tuning**: HubPlay opens up to 25 connections against
  Postgres (`pgxPoolMaxOpenConns` in `internal/db/postgres.go`).
  Each connection is a backend process on the server side; on a
  shared cluster with `max_connections = 100` that leaves
  headroom for other clients. Tune up if you run a large household
  with many concurrent streamers.
- **Auto-vacuum**: HubPlay does NOT issue manual `VACUUM` /
  `ANALYZE`. Postgres' built-in autovacuum is sufficient at the
  scale a HubPlay install produces (writes are slow / bursty).
- **FTS**: search uses the `tsvector` column maintained by the
  trigger in `migrations/postgres/002_fts_search.sql`. No external
  search service needed.
- **Schema changes**: new HubPlay releases ship matching
  `migrations/sqlite/<n>.sql` AND `migrations/postgres/<n>.sql`. The
  binary auto-applies the right tree on boot. A drift-guard test
  (`internal/db/migrations_parity_test.go`) prevents a release that
  ships an unmatched migration.
- **Monitoring**: HubPlay exposes `/metrics` on Prometheus format;
  the standard Postgres exporters (`postgres_exporter`,
  `pg_stat_statements`) cover the DB-side picture.

---

## Known limitations

- The admin **backup download** + **upload restore** endpoints
  return 501 on Postgres. Use `pg_dump` / `pg_restore` (see above).
- The setup **wizard** still asks for a SQLite path on the first
  install screen; selecting Postgres there is currently a manual
  yaml edit (`hubplay.yaml` after the wizard completes). Tracked
  as a follow-up.
- There is no in-app **driver migration tool**. Use `pgloader` (see
  above) for the one-shot sqlite → postgres path.
