# PostgreSQL query files

Parallel tree to `internal/db/queries/`. Each `.sql` file here is a
dialect-translated sibling of its SQLite counterpart.

**Translation guide**: `docs/architecture/postgres-migration.md`.

**Source of truth**: the SQLite versions. When the SQLite query
changes, the Postgres version must change to match. Until the dual-
dialect migration is complete (sessions B–E in the guide), this
directory is intentionally empty so sqlc generates an empty Queries
package without errors. Add files one repo at a time.

**Function-name parity is mandatory**: the `-- name: ListByItem :many`
comment must match between the two dialects so the generated Go
`Querier` interfaces have the same method set. The repo layer relies
on this parity to swap implementations transparently.
