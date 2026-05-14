# Intervención post-auditoría — 2026-05-14

> Rama: `claude/review-go-media-backend-37MDe`
> Audit de referencia: `docs/memory/audit-2026-05-14-go-backend-review.md`
> (inmutable; este doc tracée el trabajo iteración por iteración).

## Protocolo

Cada commit en esta rama referencia el olor por su letra única
(ej. `fix(security): FFF — añade CheckRedirect a imaging.SafeGet`).
Al cerrar un olor, se añade un párrafo de cierre debajo de su entrada
en este doc (no se edita el audit original).

## Estado por iteración

| It. | Estado | Foco | Olores | Notas |
|----:|---|---|---|---|
| 0 | 🔄 en curso | Pre-trabajo: ADRs + conventions.md | — | — |
| 1 | ✅ cerrada | Fixes urgentes seguridad + correctness | FFF, F16-1, RRR-mig, RR, Y, DD, GGGG, AAA, EE, HHH, F16-6, F16-7 | 12 olores cerrados, suite verde |
| 2 | ✅ cerrada | Sub-paquetes de `db/` | B, J, K, T, L | Sesión M.2 (B+J) + sesión M.3 (L) + sesión M.4 (K+T) |
| 3 | ⏳ pendiente | Migración Opción B incremental | M (iptv → auth → library) | — |
| 4 | ⏳ pendiente | Split de god-handlers/services | P, Z, QQ | — |
| 5 | ⏳ pendiente | Refactor estructural `iptv/` | CC | — |
| 6 | ⏳ pendiente | Composition root | G, H, V, Q, LL, JJ | — |
| 7 | ⏳ pendiente | Cosmética + schema | D, X, W, BB, UUU-mig, etc. | — |
| 8 | ⏳ pendiente | Polish de calidad de código | F14-X, F15-X, F16-X | — |
| 9 | ⏳ pendiente | Verificación empírica | `-race`, `goleak`, `govulncheck` | post-merge |

---

## Iteración 0 — Pre-trabajo

Se documentan los ADRs nuevos en `architecture-decisions.md` y se
actualizan convenciones en `conventions.md`. Los ADRs que **aplican
directamente a fixes de Iteración 1** (019 SSRF, 021 EvalSymlinks)
se abren primero; los demás se abren cuando llegue su iteración
correspondiente.

### Cierres

- ✅ **ADR-019** (SSRF guard con `CheckRedirect` obligatorio) —
  añadido al final de `architecture-decisions.md`. Documenta el
  vector, la decisión, la implementación de referencia, y la
  convención hacia futuros HTTP clients outbound.
- ✅ **ADR-021** (Path traversal: `EvalSymlinks` obligatorio antes
  de `filepath.Rel`) — añadido. Documenta el vector CVE-class de
  F16-1, el algoritmo de resolución (incluido "trepar al primer
  componente existente" para destinos no-creados) y los
  trade-offs.

Pendiente: ADR-015 (Opción B), ADR-016 (composition root), ADR-017
(timeouts), ADR-018 (comentarios en español), ADR-020 (up-only),
ADR-022 (clock injection), ADR-023 (error filtering) — se abren
cuando llegue su iteración.

---

## Iteración 1 — Fixes urgentes de seguridad + correctness

Cero cambios de API pública. Cierra los 2 CVE-class + 3 bugs
latentes de drain + violación up-only + leak de ratelimit + 2
olores menores de federation + 1 de audit trail.

### Cierres

- ✅ **FFF** (SSRF redirect bypass en `imaging.SafeGet`) —
  `internal/imaging/safety.go`: `http.Client` ahora setea
  `CheckRedirect` que re-valida la IP en cada hop con la misma
  función `validateOutboundURL` extraída. Patrón paralelo al de
  `iptv/proxy.go`. Test de regresión:
  `TestSafeGet_RejectsRedirectToPrivateIP` monta un httptest
  server que devuelve `302` hacia otro httptest server, con un
  mock de `BlockedIP` contador para verificar que el redirect se
  rechaza con `ErrUnsafeURL`. 4 tests `TestSafeGet_*` verdes;
  suite completa de `internal/imaging/...` sin regresión.
- ✅ **F16-1** (path traversal en `people.isUnderImageDir`) —
  nuevo helper compartido `internal/api/handlers/imagedir.go` con
  `isPathUnderImageDir(imageDir, p)` que: (1) resuelve el
  imageDir con `EvalSymlinks` una vez, (2) intenta `EvalSymlinks`
  sobre el path completo, (3) si falla (destino aún no creado),
  trepa al primer componente existente y resuelve allí, pegando
  el sufijo no-creado. `PeopleHandler.isUnderImageDir` e
  `ImageHandler.isUnderImageDir` ahora delegan en el helper —
  resuelve la duplicación que tenía exactamente el mismo bug.
  Tests nuevos: `TestIsPathUnderImageDir_Symlink`,
  `_TraversalLiteral`, `_SymlinkInParentChain`,
  `_NonExistentTargetUnderRootAccepted`,
  `_NonExistentOutsideRoot`, `_HappyPath`. Suite completa
  `internal/api/handlers/...` sin regresión.

- ✅ **RRR-mig** (política up-only) — eliminados bloques
  `-- +goose Down` de **58 migraciones** (15 SQLite + 43 Postgres).
  Operación mecánica con `sed`. Verificado: cero ficheros con
  `-- +goose Down` restantes; `internal/db` tests siguen verdes
  (las migraciones se cargan correctamente con solo Up).
- ✅ **AAA** (comentario obsoleto `event/bus.go:24-31`) —
  reescrito a tabla en español con productor de cada evento entre
  paréntesis. Sigue siendo no-op cuando nadie está suscrito.
- ✅ **EE** (`StreamProxy.Shutdown` engañoso) — renombrado a
  `ClearRelays`. El nombre ahora alinea con el efecto (vacía el
  map de listeners; el drain real lo hace `http.Server.Shutdown`).
  Caller único en `cmd/hubplay/main.go:614` actualizado, con
  comentario explicativo. Test `proxy_security_test.go` ajustado.
- ✅ **RR** (`loginRateLimiter` goroutine sin Stop) — añadido
  `stopCh` + `Stop()` idempotente con `sync.Once`. La goroutine
  de cleanup ahora sale al cerrar el canal. `auth.Service.
  StopSessionCleaner` invoca `rateLimiter.Stop()` además del
  cleaner de sesiones. Tests nuevos:
  `TestLoginRateLimiter_StopIsIdempotent` y
  `_StopClosesGoroutine`. Cierra el leak documentado en F7.
- ✅ **HHH** (`pathmap.Read` sin validar) — añadido
  `ErrCorruptMapping` y helper `isWellFormedAbsPath` que rechaza
  paths vacíos, relativos o con `..` literal. Defense-in-depth
  por debajo de F16-1: aunque el handler ya valida con
  `EvalSymlinks`, el store ya no devuelve paths manifestamente
  inseguros. Test nuevo `TestRead_RejectsCorruptMapping` con 4
  subcasos (empty, relative, dot-dot embedded, bare dot-dot).
- ✅ **F16-7** (audit trail en `KillSession`) — si `auth.GetClaims`
  retorna nil, el handler ahora responde **401 + log ERROR**
  ("endpoint expuesto sin auth?") en vez de matar sesiones
  anónimamente. Cuando hay claims, el log obligatorio incluye
  `session_id`, `by` (UserID) y `role`. Tests actualizados con
  helper `adminCtx(...)`; test nuevo
  `TestAdminStreams_KillSession_RejectsWithoutClaims` para pinear
  el guard.
- ✅ **F16-6** (filtración de internals en errores de
  `federation_admin.go`) — 3 sitios (`ProbePeer`, `AcceptInvite`,
  `ShareLibrary`) ya no propagan `err.Error()` al cliente. El
  detalle va al log (con IP, status code, etc. si aplica) y el
  cliente recibe un mensaje genérico fijo por categoría
  (`INVITE_EXPIRED`, `PEER_NOT_PAIRED`, etc.). Cierra
  information disclosure.
- ✅ **Y** (`SegmentDetector`/`SegmentFingerprinter` sin drain) —
  ambos structs añaden `bgWG sync.WaitGroup`. El handler del bus
  registra `bgWG.Add(1)` antes de spawn, `defer bgWG.Done()` al
  final. El `unsub` retornado por `Start` ahora envuelve
  `busUnsub()` **y** `bgWG.Wait()` para que el `defer` de
  `main.go` espere a las goroutines en vuelo antes de cerrar la
  DB. Patrón paralelo al de `library.Service`.
- ✅ **DD + GGGG** (detached goroutines en `iptv.Service` y
  `handlers/iptv_admin`) — `iptv.Service` ahora tiene
  `bgCtx/bgCancel/bgWG` propios. Añadidos métodos
  `SpawnBackground(fn func(ctx context.Context))` y
  `BackgroundContext()` al service y al interface `IPTVService`.
  `service_m3u.go` (auto-EPG + auto-probe tras import) y
  `handlers/iptv_admin.go` (refresh M3U async, M3U refresh tras
  public-IPTV create) reemplazan sus `go func() { …
  context.Background() … }` por
  `svc.SpawnBackground(func(bgCtx) { … })`.
  `Service.Shutdown` ahora cancela y drena el WG en lugar de ser
  no-op. Cierra dos olores con un único patrón.

### Verificación final Iteración 1

- `go build ./...` — verde.
- `go test ./internal/... -count=1 -timeout=300s` — **todos los
  paquetes verdes**: api, handlers, auth, db, federation, iptv,
  imaging, library, scanner, stream, etc.
- Nuevos tests añadidos en esta iteración:
  `TestSafeGet_RejectsRedirectToPrivateIP`,
  6× `TestIsPathUnderImageDir_*`,
  `TestLoginRateLimiter_Stop*`,
  4× `TestRead_RejectsCorruptMapping/*`,
  `TestAdminStreams_KillSession_RejectsWithoutClaims`.

### Cierre Iteración 1

12 olores cerrados (2 CVE-class + 10 correctness/seguridad
menores), 58 migraciones limpiadas, 1 helper nuevo compartido
(`isPathUnderImageDir`), 2 métodos nuevos exportados
(`iptv.Service.SpawnBackground` + `BackgroundContext`). Cero
cambios de API HTTP pública. Iteración lista para revisión y
merge.

---

## Iteración 2 — Sub-paquetes de `db/`

Plan original: cerrar olores B + J + K + T + L. Esta sesión (M.2)
cierra **B + J**; K + T + L se difieren a una sub-iteración 2.1
para que el PR sea un refactor estructural puro sin mezclar
cambios de interfaz en `Dependencies`. Cero cambios de API HTTP
pública; firmas de los repos preservadas vía rename de tipo
(`FederationRepository` → `storage.Repository`).

### Cierres

- ✅ **B + J** (inversión de capa `db → federation` + god-fichero
  `federation_repository.go` con 6 responsabilidades) —
  `internal/db/federation_repository.go` (1 474 LOC) eliminado.
  Nuevo paquete `internal/federation/storage/` con split en
  **9 ficheros**:

  | Fichero | Métodos | Responsabilidad |
  |---|---|---|
  | `storage.go` | struct `Repository` + `NewRepository` + `useSQLite` + `caseInsensitiveSort` + `buildSearchSharedItemsSQL` | Construcción + helpers de dialecto |
  | `sql_util.go` | `nullableString` + `toTSQueryPrefix` | Helpers SQL (copias privadas — ver nota) |
  | `identity.go` | `GetIdentity` + `InsertIdentity` + 2 row mappers | Identity Ed25519 del servidor |
  | `invite.go` | 4 métodos + 2 row mappers | Códigos `hp-invite-…` |
  | `peer.go` | 7 métodos + 2 row mappers | Peers linkeados |
  | `audit.go` | `InsertAuditEntry` + `ListAuditEntries` + `PruneAuditBefore` | Cola de audit log |
  | `share.go` | 7 métodos + 2 row mappers + `attachPrimaryImageColors` (raw) + `SearchSharedItems` (FTS dual-dialect raw) | Library shares + shared items + búsqueda federada |
  | `item_cache.go` | `UpsertCachedItems` (tx + raw INSERT) + `ListCachedItems` (raw SELECT) + `PurgeCachedItemsForLibrary` | Item cache cross-peer |
  | `progress.go` | `UpsertProgress` + `GetProgress` + `DeleteProgress` + `ListContinueWatching` | Cross-peer Continue Watching |

  El plan original del audit mencionaba 6 sub-ficheros
  (incluyendo `ratelimit.go` para RatelimitState declarado en
  ADR-012); ese fichero NO se crea porque grep confirmó que
  `federation_repository.go` jamás tuvo métodos de rate limit
  (el RatelimitState está fuera de scope hasta que alguien lo
  implemente). En su lugar el fichero original tenía dos
  responsabilidades sin mención explícita en el audit:
  `share.go` (LibraryShare + SharedLibrary + SharedItem listings
  + búsqueda federada) y `progress.go` (federation_progress de
  la migración 028). El split refleja la realidad del código,
  no la lista mental del auditor — cada fichero hace exactamente
  lo que dice su nombre.

  **Decisiones de implementación**:
  - **Tipo `Repository`** (no `FederationRepository`) — evita
    stutter; el call site queda `federationstorage.NewRepository(...)`.
  - **Constructor sigue en el caller** (`main.go`, `pg-smoke`,
    tests), NO en `federation.NewManager`. El plan del audit
    sugería que `federation.NewManager` construyese el storage
    internamente, pero eso introduciría ciclo
    `federation ↔ federation/storage` (storage importa
    `federation` para los tipos). El paquete `federation`
    consume el repo vía `type Repo interface` (manager.go:34),
    así que mover la construcción al composition root es
    coherente con el resto del proyecto y deja cero ciclo.
  - **Helpers privados copiados en lugar de exportados**:
    `nullableString` (4 LOC) y `toTSQueryPrefix` (~30 LOC) viven
    duplicados en `sql_util.go` para no exponer API pública en
    `db/` por dos call-sites externos. Las versiones en `db/`
    (`session_repository.go:345`, `item_repository.go:941`)
    siguen igual para sus propios callers.
  - **`db.RewritePlaceholders` + `db.IsPostgres` reutilizados**
    (exportados ya antes) para no duplicar la lógica de dialect
    rewrite.
  - **Tests preservados via `git mv`** —
    `internal/db/federation_repository_test.go` (498 LOC, 4
    funciones test, 1 helper `insertTestUser`) movido a
    `internal/federation/storage/repository_test.go` con
    `package storage_test`. El test sigue usando
    `db.NewLibraryRepository` / `db.NewItemRepository` /
    `db.NewImageRepository` para seed (importing `internal/db`),
    sólo cambia `db.NewFederationRepository(...)` →
    `storage.NewRepository(...)`. Git tracea el rename
    automáticamente.
  - **4 callers actualizados**:
    - `cmd/hubplay/main.go:392`:
      `db.NewFederationRepository(...)` →
      `federationstorage.NewRepository(...)` + import añadido.
    - `cmd/pg-smoke/main.go:107`: ídem.
    - `internal/api/handlers/federation_stream_test.go:73,454`:
      ídem (2 sitios) + import añadido.

  **Cierra olor B** porque `internal/db` ya no importa
  `internal/federation` (la inversión de capa única del proyecto
  ha desaparecido). **Cierra olor J** porque el god-fichero de
  1 474 LOC se ha descompuesto en 7 ficheros temáticos de
  150–530 LOC cada uno, cada uno con responsabilidad única.

### Verificación final Iteración 2 (B+J)

- `go build ./...` — verde (exitcode 0 en `golang:1.25` container).
- `go test ./internal/... -count=1 -timeout=300s` — **22
  paquetes verdes** incluyendo el nuevo `hubplay/internal/federation/storage`
  (1.6s, las 4 funciones test movidas pasan). `hubplay/internal/db`
  sigue verde tras la extracción (24.8s); `hubplay/internal/api/handlers`
  sigue verde (16.2s, federation_stream_test.go incluido);
  `hubplay/internal/federation` sigue verde (2.5s).
- Tests pre-existentes preservados: `TestFederationRepository_SearchSharedItems`,
  `TestFederationRepository_SharedItem_ColorsForwarded`,
  `TestFederationRepository_Progress`,
  `TestFederationRepository_Progress_PeerRevokedDropsFromRail`.

### Cierres adicionales (sesiones M.3 + M.4 del 2026-05-14)

- ✅ **L** (sesión M.3, commit `ab0f4c1`) — split textual de
  `internal/db/home_repository.go` (671 LOC) en 4 ficheros por
  carril:

  | Fichero | Contenido |
  |---|---|
  | `home.go` | struct `HomeRepository` + `NewHomeRepository` (constructor con 7 queries SQL pre-rewritten) + `groupConcatExpr` helper + `splitGroupConcat` helper (compartido entre Recommended y BecauseYouWatched) + `IDsFromTrending` utility |
  | `home_trending.go` | `HomeTrendingItem` type + `Trending` method |
  | `home_recommended.go` | `HomeRecommendation` + `HomeBecauseSeed` + `HomeBecauseResult` types + `Recommended` + `BecauseYouWatched` methods |
  | `home_live.go` | `HomeLiveNowChannel` type + `LiveNow` method |

  El plan original del audit decía split en `home_latest.go`,
  `home_trending.go`, `home_live.go`, pero el repo no tiene un
  método `Latest`; la realidad del código son Trending,
  Recommended, BecauseYouWatched, LiveNow. Recommended y
  BecauseYouWatched comparten `splitGroupConcat` y la misma
  estrategia de scoring por género; viven juntos en
  `home_recommended.go`. Cero cambios de SQL, tipos públicos o
  comportamiento — split textual puro.

- ✅ **K + T** (sesión M.4) — interfaces estrechas en
  `Dependencies` + `db.ActivityRepository` para las queries inline
  de `handlers/system.go`. Cambios:

  **Nuevos ficheros en `internal/db/`**:
  - `activity_repository.go` — `ActivityRepository` con
    `DailyWatchActivity(ctx, cutoff)` y
    `TopItems(ctx, cutoff, limit)`. Tipos `DailyWatchBucket` y
    `TopItemRow` exportados. Pattern B raw SQL (mismas queries
    que estaban inline en `system.go:430-540` y `system.go:570-625`,
    pre-rewritten una vez al construir).
  - `maintenance.go` — 3 interfaces estrechas
    (`HealthChecker { PingContext }`, `BackupOperator { VacuumInto }`,
    `PoolStatsReporter { Stats }`) + struct `Maintenance` que
    las implementa todas. `MigrationSource()` retorna el `*sql.DB`
    subyacente exclusivamente para el migrator sqlite→pg (el
    único caller legítimo que necesita acceso arbitrario).

  **Handlers adaptados**:
  - `handlers/system.go` — `db *sql.DB` + `driver string`
    sustituidos por `health db.HealthChecker` + `activity
    *db.ActivityRepository`. `StreamActivity` y `TopItems` ahora
    son thin wrappers de las queries del repo; la lógica de
    coercion de tipos (any → int) que estaba inline ahora vive
    en el repo. Backfill de días vacíos sigue en el handler
    (presentación). Cero cambios de wire format JSON.
  - `handlers/health.go` — `db *sql.DB` → `health
    db.HealthChecker`. `Ping()` → `PingContext(r.Context())`.
  - `handlers/admin_backup.go` — `db *sql.DB` → `backup
    db.BackupOperator`. `ExecContext("VACUUM INTO ...")` →
    `backup.VacuumInto(ctx, path)`.
  - `handlers/admin_db.go` — `liveDB *sql.DB` → `maint
    *db.Maintenance`. `liveDB.Stats()` → `maint.Stats()`;
    `liveDB` pasado al migrator → `maint.MigrationSource()`.

  **`api.Dependencies` reorganizada**:
  - **Eliminado** `Database *sql.DB`.
  - **Añadido** `DB *db.Maintenance` (typed wrapper) y
    `Activity *db.ActivityRepository` (queries tipadas).
  - `internal/api/router.go` ya no importa `database/sql`.
  - 4 call-sites del wireado (`router.go:157,170,546,616,635`)
    actualizados.

  **`main.go`**: añade
  `db.NewMaintenance(driver, database)` +
  `db.NewActivityRepository(driver, database)` al
  `Dependencies` literal. Sin cambios estructurales más allá.

  **Tests ajustados**:
  - `internal/api/handlers/system_test.go` — 10 sitios:
    `DB: database, Driver: testutil.Driver()` →
    `Health: db.NewMaintenance(...), Activity: db.NewActivityRepository(...)`.
  - `internal/api/handlers/health_test.go` — 6 sitios:
    `NewHealthHandler(database, ...)` →
    `NewHealthHandler(db.NewMaintenance(testutil.Driver(), database), ...)`.
    Import nuevo de `hubplay/internal/db`.
  - `internal/api/integration_test.go` +
    `internal/api/stream_integration_test.go` —
    `Database: database` → `DB: db.NewMaintenance(...),
    Activity: db.NewActivityRepository(...)`.

  **Cierra olor K** porque los 4 handlers admin ya no reciben
  `*sql.DB` raw — consumen interfaces de una sola obligación cada
  una. **Cierra olor T** porque `Dependencies.Database *sql.DB`
  desaparece. El único punto que sigue exponiendo el handle crudo
  (`Maintenance.MigrationSource()`) es controlado, documentado, y
  con un único caller legítimo (admin_db migrator).

### Verificación final Iteración 2 (L + K + T)

- `go build ./...` — exitcode 0 en `golang:1.25` container.
- `go test ./internal/... -count=1 -timeout=300s` — **22
  paquetes verdes** incluyendo `internal/api` (9.9s),
  `internal/api/handlers` (17.2s), `internal/db` (26.7s),
  `internal/federation/storage` (3.0s).
- Tests pre-existentes preservados: `system_test.go` (10
  funciones), `health_test.go` (6 funciones), `admin_db_test.go`,
  `admin_backup_test.go`, 2 tests de integración.

### Cierre Iteración 2

5 olores cerrados (B, J, K, T, L) sobre 3 sesiones (M.2 + M.3 +
M.4):

- **Sesión M.2** (commit `dc988ba`, PR pendiente) — B+J:
  `federation_repository.go` movido a `internal/federation/storage/`
  con split en 7 ficheros. Cierra la inversión de capa única +
  el primer god-fichero.
- **Sesión M.3** (commit `ab0f4c1`) — L: split textual de
  `home_repository.go` en 4 ficheros por carril.
- **Sesión M.4** (este commit) — K+T: interfaces estrechas
  (`HealthChecker`, `BackupOperator`, `PoolStatsReporter`) +
  `Maintenance` + `ActivityRepository`. Elimina
  `Dependencies.Database *sql.DB` por completo.

Cero cambios de API HTTP pública. Iteración lista para
revisión y merge.
