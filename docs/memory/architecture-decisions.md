# Architecture Decision Records

> ADRs cortos para HubPlay. Formato: **Contexto → Decisión → Consecuencias → Alternativas**.
> Un ADR se añade; no se edita. Si la decisión cambia, se crea uno nuevo que supersede.
> Las "decisiones de no hacer" también cuentan — si algo se descartó con razón, registrar aquí ahorra el debate en dos meses.

---

## ADR-001 — Query layer: sqlc sobre `database/sql`

- **Fecha**: 2026-04-15
- **Estado**: Aceptado
- **Supersede**: —
- **Contexto de descubrimiento**: Auditoría `audit-2026-04-15.md`, §2.1

### Contexto

HubPlay tenía `sqlc.yaml` + `make sqlc` + una línea en `CLAUDE.md` declarando `sqlc (generated)`, pero los 16 repos en `internal/db/` estaban escritos a mano con `database/sql`, `Scan()` manual, helpers `nullStr()` duplicados, y el directorio `internal/db/queries/` no existía. Estado inconsistente detectado durante la auditoría del 2026-04-15.

Requisitos del proyecto:
- Un binario, pocas dependencias, self-hosted.
- SQLite como DB por defecto (usando `modernc.org/sqlite`, pure-Go, sin CGO).
- Goose para migraciones.
- FTS5 para búsqueda.
- Pre-launch → ventana abierta para romper APIs internas.

### Decisión

Adoptar **`sqlc`** como única fuente para la capa de consultas.

- Las queries viven en `internal/db/queries/<dominio>.sql`.
- El código generado se emite a `internal/db/sqlc/` (ya configurado en `sqlc.yaml`).
- Los repositorios en `internal/db/*_repository.go` se convierten en **adaptadores delgados** que:
  - Envuelven `*sqlc.Queries`.
  - Mapean errores SQL a sentinels/`AppError` del paquete `domain`.
  - Convierten `sql.Null*` ↔ tipos del dominio cuando añade claridad.
  - Preservan las interfaces estrechas que ya consumen servicios (ej. `signingKeyRepo` en `internal/auth/keystore.go`) para no tocar llamadas arriba.
- `emit_interface: true` en `sqlc.yaml` genera un `Querier` que los tests pueden mockear.
- Migraciones siguen gestionadas por **`goose`**; `sqlc` solo lee el schema para inferir tipos.

### Driver

Se mantiene **`modernc.org/sqlite`** (pure-Go). La penalización de rendimiento vs `mattn/go-sqlite3` (CGO) es aceptable para el perfil de carga de un servidor self-hosted, y a cambio:
- Binario único, sin toolchain C.
- Cross-compile trivial (amd64/arm64 nativos).
- `Dockerfile` multi-arch limpio.
- No complica el target `hwaccel`.

### Consecuencias

**Positivas**
- Type-safety compile-time en todas las queries (un cambio de schema rompe `go build`, no runtime).
- ~40% menos código en `internal/db/` esperado tras migrar los 16 repos.
- `nullStr()` y otros helpers duplicados desaparecen.
- Nuevas queries se añaden en SQL, no en boilerplate Go.
- Mockeo trivial en tests vía el `Querier` interface.

**Negativas / trade-offs**
- `sqlc` requiere un paso extra en el flujo: `make sqlc` tras cambiar `.sql`. Mitigación: target de Makefile ya existe; `sqlc-check` en CI bloquea PRs con queries no regeneradas.
- FTS5 `MATCH` y joins con parámetros dinámicos requieren patrones específicos (nullable params, `COALESCE`, o raw queries como último recurso). Se documentará caso por caso en `conventions.md` según vayan apareciendo.
- Añade dependencia de build: developers necesitan `sqlc` instalado (`go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest` o Docker `sqlc/sqlc`). Se documenta en README del paquete y en `docs/architecture/tooling.md`.

### Alternativas descartadas

- **`sqlx`**: buen middle-ground (structs con tags, sin codegen), pero no da compile-time safety sobre SQL. Descartada por perder la garantía más valiosa de `sqlc`.
- **`ent`**: ORM schema-first potente, pero pesado para un schema que ya está gobernado por migraciones a mano. Introduce una segunda fuente de verdad del schema.
- **`GORM`**: ORM completo con reflection runtime. Incompatible con el criterio "limpio, eficiente, comprensible" del proyecto.
- **`ncruces/go-sqlite3` (driver WASM)**: prometedor pero menos batallado que `modernc.org/sqlite`. Revisitable en el futuro si el perfil de rendimiento lo pide.
- **`mattn/go-sqlite3`**: más rápido pero requiere CGO. Rompe distribución como binario único.
- **Mantener `database/sql` + aceptar el boilerplate**: rechazado. El schema tiene tamaño suficiente (28+ tablas) para que la repetición sea una fuente real de bugs.

### Plan de migración

Incremental, una tabla por commit. Orden propuesto (de menos a más tocado arriba):

1. **Piloto**: `signing_keys` (6 queries, sin callers complejos). Valida patrón.
2. `sessions`, `api_keys`, `providers`, `webhook_configs` (tablas pequeñas, independientes).
3. `users`, `libraries`, `library_paths`, `library_access`.
4. `channels`, `epg_programs`, `media_segments`, `trickplay_info`.
5. `items` + `ancestor_ids` + `metadata` + `external_ids` + `people` + `item_people` + `item_values` + `item_value_map` + `media_streams` + `chapters` (bloque de biblioteca, máxima superficie).
6. `images`, `user_data`, `activity_log`.
7. Una vez todos migrados: borrar `scan_helpers.go`, helpers `nullStr()`, y cualquier import de `database/sql` residual en repos.

Cada fase mantiene la interfaz consumida por servicios estable → no hay cambio en handlers ni en packages arriba.

### Verificación

- `go build ./...` compila tras cada commit.
- Tests existentes pasan sin modificarse (cuando los haya).
- `golangci-lint run ./...` limpio.
- Cobertura ≥ nivel actual.

### Referencias

- [sqlc docs](https://docs.sqlc.dev/)
- `sqlc.yaml` en raíz del repo
- `internal/db/queries/signing_keys.sql` (piloto)
