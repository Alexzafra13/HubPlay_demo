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

### Reaffirmation 2026-04-25 — sqlc sweep + reglas duras

Tras el ADR original migramos los 16 repos heredados a sqlc, pero las
4 tablas nuevas que aparecieron en branches posteriores
(`library_epg_sources` mig 007, `channel_overrides` mig 009,
`iptv_scheduled_jobs` mig 011, `channel_watch_history` mig 012)
volvieron a escribirse en raw SQL. El comentario inline ("the sqlc
adapter isn't regenerated as part of this change") justificaba un
atajo individual, pero el patrón se propagó: cada nueva tabla seguía
el precedente raw. El review senior de 2026-04-25 lo identifica como
el debt-compound oculto más serio del proyecto — una predicción
concreta: "alguien renombra una columna en `iptv_scheduled_jobs`,
ejecuta `make sqlc` (no-op para el repo raw), tests pasan en
fixtures, runtime falla en producción".

Estado tras el sweep (commit `<<this-commit>>`):
- Las 4 tablas tienen su `internal/db/queries/<tabla>.sql`.
- Los 4 repos son ahora adaptadores delgados sobre `*sqlc.Queries`,
  con la misma interface pública que antes (cero cambio en callers).
- Tests existentes pasan sin modificación.
- Quedan 0 raw repos post-ADR.

Reglas duras a partir de aquí (a la vista en este ADR para que el
próximo PR las cumpla sin debate):

1. **Toda tabla nueva exige su query file en `internal/db/queries/`
   y su repo como adaptador sqlc.** No se aceptan `r.db.QueryContext`
   crudos en repos nuevos.
2. **Excepción explícita y documentada**, NO precedente. Si un repo
   necesita raw SQL (TX multi-paso, EXPLAIN, FTS5 dinámico), se
   añade un comentario al método indicando exactamente por qué sqlc
   no aplica AHÍ — y solo ahí.
3. **`make sqlc` corre antes de cada PR que toque schema o
   queries.** El target ya existe; el desarrollador es responsable
   de regenerar y commitear `internal/db/sqlc/`.
4. **Caracteres no-ASCII en query files rompen el parser de sqlc
   v1.29** (em-dashes, ∈, …). Mantener queries y comentarios en
   ASCII puro. Bug aprendido durante el sweep.
5. **`COALESCE(MAX(x), -1)` en agregados** que sqlc no puede
   tipar — el wrapper devuelve `interface{}`, el repo casts a
   int64 y normaliza el sentinel "-1 → no rows yet". Documentado
   en `library_epg_sources_repository.go` para que el patrón sea
   reusable.

Sentinel-to-AppError mapping sigue siendo job del repo
(`ErrEPGSourceAlreadyAttached`, `ErrIPTVScheduledJobNotFound`,
`ErrChannelNotFound`). sqlc no lo hace por nosotros.
