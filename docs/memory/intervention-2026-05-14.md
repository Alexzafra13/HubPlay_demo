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
| 2 | ⏳ pendiente | Sub-paquetes de `db/` | B, J, K, T, L | — |
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
