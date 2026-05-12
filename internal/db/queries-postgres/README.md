# queries-postgres/

Tree paralelo a `internal/db/queries/`. Cada `.sql` aquí es un
hermano traducido al dialecto de su homólogo SQLite.

Guía de traducción: `docs/architecture/postgres-migration.md`.

Hasta que las sesiones B–E del plan estén hechas, este directorio
está vacío a propósito. El bloque postgres en `sqlc.yaml` está
comentado para que `sqlc generate` no falle en dir vacío. Al añadir
el primer `.sql` aquí, descomentar el bloque y regenerar.

**Paridad de nombres de función**: el comentario `-- name: ListByItem :many`
debe coincidir entre los dos dialectos para que las interfaces
`Querier` generadas tengan el mismo method set. La capa de repo
depende de esa paridad para swappear backends sin tocar call sites.
