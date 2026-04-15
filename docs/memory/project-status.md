# Estado del proyecto

> Snapshot: **2026-04-15** · Rama: `claude/sqlc-sessions` · HEAD base: `7419d74`

## Resumen ejecutivo

Iteración en curso: **migración incremental a sqlc** según ADR-001 (ver
`architecture-decisions.md`). Cada repo de `internal/db/` pasa a ser un
adaptador delgado sobre `*sqlc.Queries`. Las interfaces consumidas por
servicios arriba (`internal/auth/`, handlers, etc.) no cambian → ripple
cero fuera de `internal/db/`.

## Lo hecho hoy

1. **Auditoría exhaustiva** → `audit-2026-04-15.md`. 20 deudas priorizadas.
2. **ADR-001 sqlc** → `architecture-decisions.md`. Driver `modernc.org/sqlite`
   confirmado; `sqlx`/`ent`/`GORM` descartados con razón.
3. **Piloto `signing_keys`** (commit `7419d74`, en `main`):
   - 107 → 49 líneas de repo (-54%).
   - Alias `type SigningKey = sqlc.JwtSigningKey` porque los campos
     coincidían 1:1.
   - Keystore, JWT y service intactos.
4. **`sessions` migrada** (rama actual):
   - 148 → ~140 líneas de repo, pero con la mitad del ruido y tipos
     verificados por el compilador.
   - Struct `db.Session` **conservado** (no alias) porque `ip_address`
     es nullable en SQL y `IPAddress` es `string` en el dominio.
   - Adapter con helpers `sessionFromRow()` y `nullableString()` — el
     patrón que aplicaremos siempre que schema ↔ dominio no coincidan.
   - 10 queries. `DeleteOldestSessionByUser` necesitó alias de tabla
     (`sessions s`) por ambigüedad; anotado en el `.sql`.
   - Tests de `auth`, `db`, `api`, `api/handlers` pasan.

## Progreso de migración sqlc

| Tabla | Estado | Estrategia | Commit |
|-------|--------|------------|--------|
| `jwt_signing_keys` | ✅ | Alias (fields 1:1) | `7419d74` |
| `sessions` | ✅ | Adapter + conversión null | (pendiente) |
| `api_keys` | ⏳ | Adapter probablemente | — |
| `providers` | ⏳ | — | — |
| `webhook_configs`, `webhook_log` | ⏳ | — | — |
| `users` | ⏳ | — | — |
| `libraries`, `library_paths`, `library_access` | ⏳ | — | — |
| `channels`, `epg_programs`, `media_segments`, `trickplay_info` | ⏳ | — | — |
| `items` + familia (ancestor_ids, metadata, external_ids, people, item_people, item_values, item_value_map, media_streams, chapters) | ⏳ | Bloque grande, ir al final (FTS5 aquí) | — |
| `images`, `user_data`, `activity_log` | ⏳ | — | — |

Cleanup al final: borrar `scan_helpers.go` si queda huérfano, eliminar
`nullStr()` duplicados, revisar import de `database/sql` en cada repo.

## Deuda técnica adicional descubierta al migrar

- **Makefile `test` usa `-race`**: no funciona en Windows sin CGO (el
  driver elegido es puro Go por diseño). Split sugerido: `make test`
  (portable) + `make test-race` (Linux/CGO para CI).
- **`internal/stream/transcode_test.go:104`**: hardcodea `/tmp/...` y
  compara contra `filepath.Join` → falla en Windows. Usar `filepath.ToSlash`
  o `filepath.Join` para construir el expected.

## Verificado contra código

| Item | Estado | Ubicación |
|------|--------|-----------|
| Piloto signing_keys | ✅ | `internal/db/signing_key_repository.go`, `internal/db/queries/signing_keys.sql` |
| Piloto sessions | ✅ | `internal/db/session_repository.go`, `internal/db/queries/sessions.sql` |
| sqlc generado | ✅ | `internal/db/sqlc/` (db.go, models.go, querier.go, signing_keys.sql.go, sessions.sql.go) |
| ADR-001 | ✅ | `docs/memory/architecture-decisions.md` |

## Contexto crítico para la próxima sesión

- **Regenerar sqlc** después de tocar `internal/db/queries/*.sql` **o**
  `migrations/sqlite/*.sql`. Comando:
  ```powershell
  docker run --rm -v "${PWD}:/src" -w /src sqlc/sqlc generate
  ```
  (o `sqlc generate` si tienes el binario local).
- **Dos patrones válidos** para migrar un repo — ver `conventions.md`:
  1. **Type alias** cuando los campos `db.X` coincidan 1:1 con
     `sqlc.X`. Ripple cero.
  2. **Adapter con struct propio** cuando haya diferencia (nullable vs
     not, casing, tipos). Ejemplo canónico: `session_repository.go`.
- **Convención de ramas**: trabajar en rama con nombre `claude/...`,
  fast-forward a `main` tras verificar build + tests.
- **`-race` no en Windows** por el driver pure-Go. Para CI local en
  Windows usar `go test -count=1 ./...` sin el flag.

## Próximo paso

Migrar `api_keys` siguiendo el mismo patrón. Tabla pequeña (4–5 queries),
uso limitado, validará que el adapter funcione con tipos adicionales
(hay `last_used_at` nullable). Luego `providers` + `webhook_configs`
que son de complejidad similar.
