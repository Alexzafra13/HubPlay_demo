# Revisión arquitectónica — Go backend (2026-05-27, continuación)

> Rama: `claude/go-backend-arch-review-UDzqB` · Base: `ca4a572`
> Método: análisis top-down (estructura → dependencias → flujo) y luego
> revisión por paquete. Continúa donde dejaron los audits
> [`2026-05-14`](audit-2026-05-14-go-backend-review.md) (110+ olores, casi
> 100% cerrado) y [`2026-05-27-macro`](audit-2026-05-27-architecture-macro.md)
> (6 olores estructurales MM–RR identificados, 0/8 paquetes revisados).
>
> **Este documento es el análisis profundo pedido por el usuario.** Los
> hallazgos nuevos se marcan con prefijo `SS-`, `TT-`, etc., siguiendo la
> serie del audit anterior. Los hallazgos que refuerzan olores ya
> catalogados se referencian por su letra existente.

---

## Índice

| § | Sección |
|---|---------|
| 1 | [Estructura de carpetas](#1-estructura-de-carpetas) |
| 2 | [Dependencias entre paquetes](#2-dependencias-entre-paquetes) |
| 3 | [Flujo general del sistema](#3-flujo-general-del-sistema) |
| 4 | [Hallazgos nuevos consolidados](#4-hallazgos-nuevos-consolidados) |
| 5 | [Próximos pasos](#5-próximos-pasos) |

---

## 1. Estructura de carpetas

### 1.1 Inventario actual (verificado contra código)

**29 paquetes** en `internal/`, contando sub-paquetes:

```
internal/
├── api/                    # Router + middleware + sub-paquete handlers/
│   ├── handlers/           # 72 ficheros prod, ~25k LoC — MEGA-PAQUETE (OO)
│   └── apperror/           # Cut-set para romper ciclo auth ↔ handlers
├── audit/                  # Fire-and-forget audit service (PR5)
├── auth/                   # JWT + sesiones + rate-limit + device codes
│   └── model/              # User, Session, SigningKey, DeviceCode (leaf puro)
├── blurhash/               # Encoder blurhash — leaf, 0 deps internas
├── clock/                  # Seam time.Now — leaf, 0 deps internas
├── config/                 # YAML + env override + preflight
├── db/                     # Repos sqlc-generated + infra (Open/Migrate)
│   ├── sqlc/               # Generated SQLite
│   └── sqlc_pg/            # Generated Postgres
├── domain/                 # AppError + Kind sentinel — leaf, 332 LoC
├── event/                  # Pub/sub in-proc — leaf, 0 deps internas
├── federation/             # P2P: manager + client + pairing + shares
│   └── storage/            # Repos federation (9 ficheros) — rompe inversión db→fed
├── imaging/                # Thumbnails, blurhash, safety (SSRF-safe fetch)
│   └── pathmap/            # Mapa path→ID de imágenes en disco
├── iptv/                   # Live TV: service + proxy + transmux + EPG + orden
│   └── model/              # Channel, EPGProgram, etc. (leaf puro)
├── library/                # Scanner scheduling + service + segmentos + watcher
│   └── model/              # Item, MediaStream, Image, etc. (leaf puro)
├── logging/                # Wrapper slog + buffer para admin panel
├── mdns/                   # Anuncio mDNS LAN
├── notification/           # Inbox de notificaciones (migration 049)
├── observability/          # Prometheus metrics + sinks por feature
├── probe/                  # Wrapper ffprobe
├── provider/               # TMDb, Fanart, OpenSubtitles (pluggable)
├── retention/              # Purgado periódico EPG + audit log
├── scanner/                # Walker filesystem + enrich + persist
├── setup/                  # Wizard primera ejecución
├── stream/                 # Manager transcoding + decision tree + sesiones
├── sysmetrics/             # CPU/RAM/GPU sampling para admin panel
├── testutil/               # Helpers de test (DB, fixtures, etc.)
├── updates/                # GitHub release checker background
├── upload/                 # tusd integration + staging + GC
└── user/                   # CRUD usuarios + avatares + permisos
```

### 1.2 Lo que está bien

**Layout package-por-dominio, no por tipo.** No hay `service/`, `repository/`,
`dto/`, `interfaces/` como paquetes globales. Cada feature owns sus tipos,
su servicio y sus tests. Es el layout idiomático de Go que proyectos como
`kubernetes/kubernetes`, `hashicorp/consul` o `sourcegraph/sourcegraph`
validan.

**8 paquetes leaf con 0 dependencias internas:**
`domain`, `event`, `clock`, `logging`, `observability`, `probe`, `blurhash`,
`sysmetrics`. Esto da un "suelo" estable: cambios en estos paquetes no
propagan.

**Sub-paquetes `model/` bien aplicados.** `library/model`, `iptv/model`,
`auth/model` rompen el ciclo handlers ↔ servicio ↔ tipos sin introducir
paquetes-tipo artificiales. Son leaf puros (0 imports internos).

**`federation/storage/` valida el patrón** de que cada feature puede
poseer su repo. Rompió la única inversión de capa del proyecto (`db →
federation`).

**Paquetes nuevos (`notification`, `audit`, `upload`, `updates`, `mdns`,
`retention`) no rompieron la disciplina.** Cada uno tiene 1 responsabilidad
clara, tamaño acotado (65–526 LoC productivas) y contratos estrechos.

### 1.3 Lo que necesita atención

#### 1.3.1 `handlers/` — mega-paquete de 72 ficheros (refuerza OO)

**Dónde:** `internal/api/handlers/` — 72 ficheros de producción, ~25k LoC.

**Contexto del problema:** Conviven 15 dominios en un solo `package handlers`:
auth (3 ficheros), items (6), library (1), iptv (12), federation (5),
me_* (7), admin_* (6), system (4), colecciones, studios, personas,
preferencias, providers, notificaciones, uploads, permisos, auditoría,
actualizaciones, CORS, openapi.

**Síntomas verificados:**
- Prefijos defensivos en tipos: `IPTVPersonalisationHandler`,
  `MePeerImageHandler`, `AdminStreamsHandler`. Si fueran sub-paquetes,
  serían `personalisation.Handler`, `peerimage.Handler`, `streams.Handler`.
- `interfaces.go` (449 LoC) + `deps_repos.go` (216 LoC) centralizan
  contratos de 15 dominios. Cambiar un método de `IPTVService` toca un
  fichero compartido con `AuthService`, `LibraryService`, etc.
- Los helpers compartidos (`responses.go`, `cache_control.go`,
  `streaming_deadline.go`, `client_ip.go`, `imagedir.go`) no tienen
  dueño claro — viven en el mismo paquete que todo lo demás.

**Principio violado:** SRP a nivel paquete, Interface Segregation
(contratos gigantes centralizados).

**Impacto futuro:** Cada feature nueva añade ficheros aquí. El paquete
crece linealmente. IDE navigation degrada. Refactors atómicos imposibles
(tocar `iptv_channels.go` puede romper un test de `federation_admin_test.go`
que comparte un helper).

**Gravedad:** Media (OO — ya catalogado).

**Refactor:** Sub-paquetes por dominio con un `common/` para helpers
compartidos. Ver propuesta en
[`audit-2026-05-27-architecture-macro.md` §2 OO](audit-2026-05-27-architecture-macro.md).

---

#### 1.3.2 `main.run()` — 630 LoC monolíticas (refuerza QQ)

**Dónde:** `cmd/hubplay/main.go:82-713`.

**Lo que hace bien:** El orden de las 7 fases es correcto. `lifecycle`
reemplazó el god-struct `runtime`. Los módulos `library.New()` e
`iptv.New()` encapsulan wiring complejo.

**Lo que no escala:**
- Lectura de `app_settings` inline (líneas 293-317) — lógica de
  negocio (parsear overrides de streaming) en el composition root.
- Bloque de uploads (líneas 502-561) con `os.Exit(1)` en medio del
  `run()` — no usa `return fmt.Errorf(...)` como el resto.
- Bloque de federation (líneas 420-469) con 6 `defer` sueltos que
  NO pasan por `lifecycle` (federation.Close, stopPendingSweeper,
  stopNotifSweeper).
- Construcción de `Dependencies` (líneas 605-685) ocupa 80 LoC de
  asignación tediosa campo-a-campo.

**Principio violado:** SRP. `run()` mezcla wiring, configuración
runtime, y lógica de negocio (parseo de `app_settings`).

**Gravedad:** Media (QQ — ya catalogado).

---

#### 1.3.3 Tipos `db.X` que fugan a handlers (refuerza PP)

**Verificado:** 35 imports de `internal/db` desde `handlers/**`.

Tipos concretos que cruzan la frontera:
- `db.UserData`, `db.ContinueWatchingItem`, `db.FavoriteItem`,
  `db.NextUpItem` — usados en `interfaces.go:363-373`, `progress.go`,
  `items.go`, y 5+ ficheros de test.
- `db.HomeTrendingItem`, `db.HomeRecommendation`,
  `db.HomeLiveNowChannel`, `db.HomeBecauseResult` — en `me_home.go:63-66`.
- `db.UserPreference` — en `preferences.go:31-32`.
- `db.ProviderConfig` — en `interfaces.go:432-434`, `setup.go`.
- `db.UploadAuditRow` — en `interfaces.go:394`, `upload_browse.go`.

**Contraste:** `notification` es el gold standard — importa `sqlc`/`sqlc_pg`
directamente, define sus propios tipos, y convierte en la frontera del
repo. Los handlers nunca ven un tipo de persistencia de `notification`.

**Gravedad:** Media (PP — ya catalogado).

---

## 2. Dependencias entre paquetes

### 2.1 Grafo verificado (producción, sin `_test.go`)

```
── Hojas (0 deps internas) ──
domain          → ∅
event           → ∅
clock           → ∅
logging         → ∅
blurhash        → ∅
probe           → ∅
sysmetrics      → ∅
auth/model      → ∅
library/model   → ∅
iptv/model      → ∅

── Infra ligera ──
config          → logging
imaging         → blurhash
imaging/pathmap → (ninguna interna fuera de imaging)
testutil        → db

── Features ──
setup           → config
retention       → clock, config
updates         → (sólo stdlib)
mdns            → (sólo stdlib)
user            → auth/model, db, domain, imaging
provider        → db
notification    → clock, db/sqlc, db/sqlc_pg, domain, event
audit           → auth, db

── Features pesadas ──
auth            → api/apperror, auth/model, clock, config, db, domain, event
scanner         → clock, db, event, imaging, imaging/pathmap, library/model,
                  probe, provider
library         → clock, db, domain, event, imaging, imaging/pathmap,
                  library/model, probe, provider, scanner
stream          → clock, config, db, domain, event, library/model
iptv            → clock, db, event, imaging, iptv/model, library/model
federation      → clock, domain, event, imaging
federation/storage → auth/model, db, domain, federation, iptv/model,
                     library/model
upload          → auth/model, clock, db, domain, event, library/model, probe

── Composition roots ──
api             → 20+ paquetes (router HTTP)
cmd/hubplay     → 20+ paquetes (proceso principal)
```

### 2.2 Lo que está bien

**Cero ciclos directos.** Verificado por CI (`golangci-lint`) y por
grep cruzado. La única inversión de capa histórica (`db → federation`)
fue cerrada por el refactor `federation/storage/`.

**Fan-out vertical, no horizontal.** Las features pesadas (`library`,
`iptv`, `stream`, `federation`) no se importan entre sí. La comunicación
cross-feature va por `event.Bus` (pub/sub in-proc) o por los tipos
compartidos en `*/model/`.

**Sink pattern bien aplicado.** `observability` no aparece como import
directo en `stream`, `handlers`, `iptv`, ni `federation`. Cada paquete
define una interface local (`MetricsSink`, `TransmuxMetrics`,
`FederationSink`) con `noopSink{}` default. Rompe ciclos potenciales
y deja el paquete testeable sin Prometheus.

**`api/apperror` como cut-set documentado** para romper `auth ↔ handlers`.
Pequeño (1 fichero), estable, sin tendencia a crecer.

### 2.3 Hallazgos nuevos

#### SS-1 — `notification` importa `db/sqlc` y `db/sqlc_pg` directamente

**Dónde:** `internal/notification/storage.go:13-14`.

**Por qué es problema:** El patrón canónico del proyecto es que los
repos viven en `internal/db/` y se construyen vía `db.NewRepositories()`.
`notification` se salta este contrato e importa los paquetes generated
directamente. Funciona, pero introduce un acoplamiento con el shape del
código generado por sqlc que otros paquetes no tienen.

**Principio violado:** Consistency (no SRP ni DRY). El patrón no es
incorrecto per se, pero es la excepción solitaria.

**Impacto futuro:** Si se regenera sqlc con otra herramienta o se cambia
el schema de `notifications`, `notification/storage.go` rompe directamente.
Los demás repos están aislados por `db.NewRepositories()`.

**Gravedad:** Baja.

**Refactor sugerido:** Dos opciones:
1. Mover el repo a `internal/db/notification_repository.go` siguiendo
   el patrón del resto (Pattern A/B).
2. Aceptar que `notification` sigue el patrón de `federation/storage`
   (feature owns su repo) y documentarlo como decisión explícita.

La opción 2 es más coherente con la dirección Opción B (cada feature
owns sus tipos y repo). **Documentar en `conventions.md`** y no tocar
código.

---

#### SS-2 — `upload` y `audit` filtran tipos `db.X` en sus interfaces

**Dónde:**
- `internal/upload/service.go:42-44`: `AuditStore.Insert(ctx, db.UploadAuditRow)`
- `internal/audit/service.go:41-43`: `Store.Insert(ctx, db.AuditLogRow)`

**Por qué es problema:** Ambos paquetes definen interfaces locales
("consumer-side", bien) pero el tipo del parámetro es `db.X` (no bien).
Un mock de `upload.AuditStore` debe importar `internal/db` sólo para
construir un `db.UploadAuditRow`. Los tests del paquete ya lo hacen
(`upload/service_test.go`, `upload/clock_internal_test.go`).

**Contraste:** `notification` define `notification.Notification` como
tipo propio y convierte en la frontera. No filtra `db.X`.

**Principio violado:** Dependency Inversion (la interface del dominio
depende de un tipo de la capa de persistencia).

**Gravedad:** Baja (ambos paquetes son pequeños y estables).

**Refactor:** Definir `upload.AuditRow` y `audit.LogRow` propios
(structs espejo) y convertir en el repo. Mecánico, ~30 LoC por paquete.
Hacerlo cuando se cierre PP (movimiento general de tipos fuera de `db`).

---

#### SS-3 — `defer` fuera de `lifecycle` en main.go

**Dónde:** `cmd/hubplay/main.go`:
- Línea 281: `defer stopOptimize()`
- Línea 453: `defer federationManager.Close()`
- Línea 468: `defer stopPendingSweeper()`
- Línea 476: `defer stopNotifSweeper()`

**Por qué es problema:** El sistema tiene un mecanismo de shutdown
por fases (`lifecycle` con workers → HTTP drain → services LIFO). Estos
4 `defer` se ejecutan DESPUÉS de `waitForShutdown` (cuando `run()`
retorna), no dentro del orden de fases. Concretamente:

1. `federationManager.Close()` se ejecuta DESPUÉS de `database.Close()`.
   Si el auditor async tiene un write en vuelo, escribe contra una DB
   cerrada.
2. `stopNotifSweeper()` y `stopPendingSweeper()` cancelan goroutines
   periódicos DESPUÉS de cerrar la DB. Si justo estaban en medio de un
   `DELETE FROM notifications WHERE ...`, el query falla.
3. `stopOptimize()` (el periódico de PRAGMA optimize) se ejecuta DESPUÉS
   del PRAGMA optimize explícito del shutdown (línea 770-772 de
   `waitForShutdown`). Orden invertido pero idempotente — no es un bug,
   pero sí confuso.

**Principio violado:** Lifecycle management consistente. El contrato
implícito es "todo lo que tiene lifecycle pasa por `lc.AddWorker/
AddService`".

**Gravedad:** Media.

**Impacto futuro:** Ruido en logs durante shutdown ("database is closed"
para el auditor federation). En escenarios patológicos, escrituras
parciales del sweeper de notificaciones.

**Refactor:**
```go
// Registrar como workers (fase 1, antes de HTTP drain)
lc.AddWorker("federation close", func(context.Context) error {
    federationManager.Close()
    return nil
})
lc.AddWorker("pending request sweeper", func(context.Context) error {
    stopPendingSweeper()
    return nil
})
lc.AddWorker("notification sweeper", func(context.Context) error {
    stopNotifSweeper()
    return nil
})
lc.AddWorker("periodic optimize", func(context.Context) error {
    stopOptimize()
    return nil
})
```
Eliminar los 4 `defer`. El orden de los workers (add-order) garantiza
que paran ANTES del HTTP drain y ANTES de cerrar la DB.

---

#### SS-4 — `os.Exit(1)` en medio de `run()` (upload)

**Dónde:** `cmd/hubplay/main.go:507-508, 528-530`.

```go
if err != nil {
    logger.Error("upload staging dir setup failed", "error", err)
    os.Exit(1)   // ← no ejecuta defers
}
```

**Por qué es problema:** `os.Exit(1)` no ejecuta las funciones `defer`
registradas hasta ese punto (database.Close, stopOptimize, lifecycle
hooks). El resto de `run()` usa `return fmt.Errorf(...)` consistentemente,
excepto aquí.

**Principio violado:** KISS, consistency.

**Gravedad:** Baja (sólo afecta al boot; si falla staging dir, no hay
transacciones in-flight que perder).

**Refactor:**
```go
if err != nil {
    return fmt.Errorf("upload staging dir: %w", err)
}
```

---

## 3. Flujo general del sistema

### 3.1 Bootstrap

`cmd/hubplay/main.go::run()` — 7 fases bien marcadas con comentarios
`═══ Phase N: X ═══`. Orden lógico:

1. **Foundation** (config + logger + clock + PATH + preflight)
2. **Database** (restore + open + migrate + repos)
3. **Infrastructure** (event bus + observability)
4. **Core Services**:
   - 4a Library Module (9 componentes, `library.New()`)
   - 4b Streaming (manager + runtime overrides de app_settings)
   - 4c IPTV Module (6 componentes, `iptv.New()`)
   - 4d Providers (TMDb, Fanart, OpenSubtitles)
   - 4e Setup service
5. **HTTP Server** (federation + uploads + audit + updates + mdns +
   CORS registry + router + `http.Server`)
6. **Start** (`go server.ListenAndServe()`)
7. **Shutdown** (`waitForShutdown`)

**Lo que está bien:**
- Feature modules (`library.New()`, `iptv.New()`) encapsulan wiring
  complejo y cross-wiring interno. `main.run()` no conoce los
  sub-componentes.
- `lifecycle` dirige shutdown en 3 fases con semánticas claras.
- `WriteTimeout: 30s` default + opt-out explícito por handler streaming.
- Federation fail-soft (nil-safe en el router, gating en registration).
- `singleflight.Group` en `stream.Manager.StartSession` colapsa init
  burst sin contención.

### 3.2 Comunicación

**Event bus** (`internal/event/bus.go`): pub/sub in-proc, `Subscribe`
devuelve `func()` para unsub. Productores: scanner, auth, iptv,
federation, upload, notification. Consumidores: SegmentDetector,
SegmentFingerprinter, SSE handlers, notification hooks.

El contrato "unsub obligatorio con lifecycle" (ADR-008) se cumple en
los 3 puntos principales:
- `library.Module.RegisterWith(lc)` captura unsubs de detector y
  fingerprinter.
- SSE handlers ligan unsub al `context.Done()` del request.
- Federation notifications registradas en `registerFederationNotifications`
  con lifetime del proceso.

**Background workers** (9+ goroutines long-lived):
| Worker | Lifecycle | Drain |
|--------|-----------|-------|
| session cleaner (auth) | `lc.AddWorker` | `StopSessionCleaner()` |
| scan scheduler | `library.Module.RegisterWith(lc)` | `Stop()` |
| image refresh scheduler | `library.Module.RegisterWith(lc)` | `Stop()` |
| fs watcher | `library.Module.RegisterWith(lc)` | `Stop()` |
| segment detector | `library.Module.RegisterWith(lc)` | unsub + `bgWG.Wait()` |
| segment fingerprinter | `library.Module.RegisterWith(lc)` | unsub + `bgWG.Wait()` |
| iptv scheduler | `iptv.Module.RegisterWith(lc)` | `Stop()` |
| iptv prober | `iptv.Module.RegisterWith(lc)` | `Stop()` |
| stream cleanup loop | goroutine en `NewManager` | `m.stopClean` channel |
| transmux reap loop | goroutine en `NewTransmuxManager` | `m.stop` + `<-m.stopped` |
| retention runner | `lc.AddWorker` | `Stop()` |
| host metrics sampler | goroutine en `Start(ctx)` | ctx cancellation |
| update checker | goroutine en `Start(ctx)` | ctx cancellation |
| periodic optimize | goroutine | `defer stopOptimize()` (**fuera de lc**, ver SS-3) |
| fed pending sweeper | goroutine | `defer stopPendingSweeper()` (**fuera de lc**, ver SS-3) |
| notif read sweeper | goroutine | `defer stopNotifSweeper()` (**fuera de lc**, ver SS-3) |

### 3.3 Concurrencia — análisis profundo

#### stream.Manager

**Locking:** `m.mu` protege `m.sessions` (map). Secciones críticas
breves (~100μs para lookup/insert). `ms.restartMu` per-sesión para
restart de ffmpeg (~2s). **No hay inversión de lock order** — `m.mu`
siempre se suelta antes de tomar `ms.restartMu`.

**singleflight:** Serializa slow-path de `StartSession` por session key.
Se ejecuta FUERA del lock (`m.mu` liberado antes de `Do()`). Correcto.

**cleanupLoop:** Spawn en `NewManager` sin WaitGroup. `Shutdown()` cierra
`m.stopClean` y retorna sin esperar a que la goroutine salga. **Riesgo
bajo:** la goroutine es fire-and-forget (sólo hace deletes del map y
stop de sesiones), pero en tests con `-race` podría producir un data
race si el test destruye el Manager y accede al map simultáneamente.

**Recomendación:** Añadir `<-m.cleanDone` en `Shutdown()` (pattern de
`transmux.go:895`). ~5 LoC.

#### iptv.TransmuxManager

**Reap loop:** Correcto — `Shutdown()` cierra `m.stop` y bloquea en
`<-m.stopped`. La goroutine señaliza `stopped` al salir. **Textbook.**

**Per-session goroutines** (processWatcher, readyWatcher, stderrTail):
No tienen WaitGroup explícito, pero se drenan implícitamente:
`terminate()` llama `s.cancel()` → ffmpeg exit → pipe closes → goroutines
salen. El timeout de 5s (`terminate` línea 1047) acota el caso de ffmpeg
colgado. **Aceptable** para deployment doméstico; en producción expuesta
sería prudente enviar SIGKILL tras el timeout.

#### federation.Manager

**Stream sweeper:** Correcto — `Close()` espera `<-m.sweepDone`.

**Goroutines best-effort** (pairing callbacks, líneas 294 y 447): No
tracked. Bounded por `HTTPTimeout` (~15s). **Aceptable** para
notificaciones fire-and-forget; documentar que no completan en shutdown
inmediato.

**Fan-out en `BrowseAllPeerLibraries`:** Sin per-peer timeout. Si un
peer cuelga, la llamada completa se bloquea hasta que el contexto del
request expire. **`SearchAllPeers` sí tiene per-peer timeout** (bien).
La inconsistencia es un bug latente.

**Recomendación:** Añadir `context.WithTimeout(ctx, perPeerTimeout)` en
`BrowseAllPeerLibraries`, misma técnica que `SearchAllPeers`. ~5 LoC.

### 3.4 Shutdown

```
SIGINT/SIGTERM
  │
  ▼
waitForShutdown
  │
  ├─ Fase 1: lc.stopWorkers (add-order)
  │    session cleaner, scan scheduler, image refresh,
  │    fs watcher, iptv scheduler, iptv prober,
  │    retention runner, stream manager(*)
  │
  ├─ Fase 2: server.Shutdown (HTTP drain, 30s budget)
  │
  ├─ Fase 3: lc.stopServices (LIFO)
  │    segment detector, segment fingerprinter,
  │    library service, iptv service, iptv proxy,
  │    iptv transmux
  │
  ├─ cancel() (root context)
  │
  ├─ PRAGMA optimize (5s budget)
  │
  └─ database.Close()

  [DESPUÉS de run() retorna — ver SS-3]
  defer stopOptimize()         ← ya ejecutado arriba, idempotente
  defer stopPendingSweeper()   ← puede escribir contra DB cerrada
  defer federationManager.Close() ← puede escribir contra DB cerrada
  defer stopNotifSweeper()     ← puede escribir contra DB cerrada
  defer database.Close()       ← ya cerrada, no-op
```

El problema de SS-3 es visible en este diagrama: los defers se ejecutan
DESPUÉS de `database.Close()`, no dentro de las fases del lifecycle.

---

## 4. Hallazgos nuevos consolidados

### Tabla resumen

| # | Olor | Severidad | Dónde | Principio | Coste fix |
|---|------|-----------|-------|-----------|-----------|
| **SS-3** | `defer` fuera de `lifecycle` (federation, sweepers, optimize) | 🟡 Media | `main.go:281,453,468,476` | Lifecycle management | ~20 LoC |
| **SS-1** | `notification` importa `db/sqlc` directo | 🟢 Baja | `notification/storage.go:13-14` | Consistency | documentar |
| **SS-2** | `upload` y `audit` filtran `db.X` en interfaces | 🟢 Baja | `upload/service.go:42`, `audit/service.go:41` | Dependency Inversion | ~30 LoC c/u |
| **SS-4** | `os.Exit(1)` en `run()` (upload) | 🟢 Baja | `main.go:507,528` | Consistency | ~4 LoC |
| **SS-5** | `BrowseAllPeerLibraries` sin per-peer timeout | 🟡 Media | `federation/manager_browse.go:58` | Robustness | ~5 LoC |
| **SS-6** | `cleanupLoop` sin drain explícito en `Shutdown` | 🟢 Baja | `stream/manager.go:192,761` | Lifecycle management | ~5 LoC |
| — | `Dependencies` 77 campos (MM) | 🔴 Alta | `api/router.go:35-214` | SRP, ISP | sesión grande |
| — | Interfaces de servicio gigantes (NN) | 🔴 Alta | `handlers/interfaces.go` | ISP, Go idiom | sesión grande |
| — | `handlers/` mega-paquete (OO) | 🟡 Media | `handlers/` 72 ficheros | SRP paquete | sesión grande |
| — | Tipos `db.X` fugan a handlers (PP) | 🟡 Media | 35 imports | Dep. Inversion | mecánico |
| — | `main.run()` 630 LoC (QQ) | 🟡 Media | `main.go:82-713` | SRP | sesión mediana |

### Relación con olores previos

Los hallazgos nuevos (SS-1 a SS-6) son **satélites de baja gravedad**
alrededor de los 6 olores macro (MM–RR) ya catalogados. La prioridad
sigue siendo:

1. **NN** (interfaces gigantes → micro en consumer) — desbloquea F15-5
   y OO.
2. **OO** (split handlers en sub-paquetes) — desbloquea PP y reduce
   el blast radius de cambios.
3. **MM** (split Dependencies) — se simplifica naturalmente al cerrar
   NN + OO.
4. **PP** (mover tipos `db.X` restantes) — mecánico, hacer después de
   OO.
5. **QQ** (split main.run) — sintomático, hacer después de NN+OO+PP.

Los SS-3 a SS-6 se pueden cerrar **oportunísticamente** en cualquier
sesión sin bloquear otros refactors.

---

## 5. Próximos pasos

La estructura, el grafo de dependencias y el flujo general están
analizados. Los problemas fundamentales son **shape problems**, no
bugs: el código funciona, los tests pasan, la concurrencia es correcta
en lo macro.

**Siguiente paso natural:** bajar a revisión por paquete, empezando por:

1. **`internal/api` + `internal/api/handlers`** — es donde convergen
   los 3 olores altos (MM, NN, OO). Resolver aquí desbloquea todo lo
   demás.
2. **`internal/iptv`** — 8.9k LoC, IPTVService con ~50 métodos,
   transmux.go 1107 LoC, proxy.go 804 LoC.
3. **`internal/stream`** — Manager 823 LoC, concurrencia, sesiones.

Empezamos por el paquete 1.

---

## 6. Revisión por paquete: `internal/api` + `internal/api/handlers`

### 6.1 Inventario

**`internal/api/` (7 ficheros de producción, ~2.2k LoC):**

| Fichero | LoC | Responsabilidad |
|---------|----:|-----------------|
| `router.go` | 519 | `Dependencies` struct (77 campos) + `NewRouter` + `fillFromConfig` + CORS helpers |
| `mount_media.go` | 434 | Streaming + Libraries + Items + IPTV |
| `mount_admin_system.go` | 247 | Admin system (stats, settings, backup, db, cors, logs, audit, updates, streams, storage) |
| `mount_federation.go` | 238 | Federation public + peer-auth |
| `mount_me.go` | 132 | Auth protected + SSE + me identity + notifications + preferences |
| `mount_users.go` | 98 | Users admin |
| `mount_public.go` | 84 | Health + OpenAPI + setup wizard |
| `mount_uploads.go` | 48 | Upload tus surface |

**`internal/api/handlers/` (72 ficheros de producción, ~25k LoC):**

- **46 handler structs** (verificado por grep).
- **27 interfaces** en `interfaces.go` (449 LoC).
- **19 interfaces** en `deps_repos.go` (216 LoC).
- **~20 interfaces locales** dispersas en handler files individuales.
- Helpers compartidos: `responses.go`, `cache_control.go`, `streaming_deadline.go`, `client_ip.go`, `imagedir.go`, `sse_limiter.go`, `iprate_middleware.go`.

### 6.2 Lo que está bien

**El split por `mount_*.go` es sano.** Antes `NewRouter` era un monolito
de ~1100 LoC. Ahora son 7 funciones con scope acotado. Cada una recibe
`Dependencies` (o un subset) y monta rutas de un dominio. Modelo a
preservar.

**`ItemHandler` como facade sobre 5 sub-handlers es correcto.** El refactor
del olor P (god-handler de 1186 LoC, 13 deps) dejó un facade limpio por
embedding de puntero. Cada sub-handler tiene sus propias deps estrechas.
La firma pública de `NewItemHandler` no cambió — los tests siguen sin
modificar. Pattern exportable al iptv handler.

**Helpers `respondData`/`requireParam` eliminaron boilerplate.** 115 sites
de `map[string]any{"data": ...}` reemplazados por una función. 53 sites
de `chi.URLParam` + check vacío reemplazados. Código más conciso.

**`handleServiceError` centraliza el mapeo error→HTTP.** En lugar de que
cada handler mapee `domain.AppError` independientemente, hay un punto
central que convierte `.Kind` a status code. Reduce duplicación y garantiza
consistencia.

**Interfaces locales en handlers individuales ya existen.** `system.go`
define 5 interfaces locales (`SystemStatsProvider`, `HostInfoProvider`,
`LibraryStatsProvider`, `activityRepo`, `SettingsReader`). `me_home.go`
define 3 (`homeRepo`, `HomeLibraryLister`, `HomeMetadataRepo`).
`settings.go` define 1 (`settingsStore`). Esto demuestra que **el patrón
correcto ya se aplica en varios handlers** — sólo falta generalizarlo.

### 6.3 Hallazgos (ordenados por impacto)

---

#### TT-1 — `IPTVService` con ~50 métodos: Interface Segregation rota (refuerza NN)

**Dónde:** `handlers/interfaces.go:146-260` (114 LoC sólo para esta interface).

**Datos verificados — qué usa cada consumidor:**

| Fichero handler | Métodos que usa | Ratio usados/total |
|-----------------|----------------:|-------------------:|
| `iptv_channels.go` | 11 | 11/50 (22%) |
| `iptv_health.go` | 7 | 7/50 (14%) |
| `iptv_favorites.go` | 7 | 7/50 (14%) |
| `iptv_admin.go` | 7 | 7/50 (14%) |
| `iptv_epg.go` | 5 | 5/50 (10%) |
| `iptv_channel_logo.go` | 5 | 5/50 (10%) |
| `iptv_personalisation.go` | 4 | 4/50 (8%) |
| `iptv_admin_channel_order.go` | 4 | 4/50 (8%) |
| `iptv_playback_failure.go` | 2 | 2/50 (4%) |

**Ningún handler usa más del 22% de la interface.** El más estrecho
(`iptv_playback_failure.go`) necesita 2 métodos de 50.

**Impacto concreto en tests:** `iptv_test.go` define `iptvFakeService`
con **50+ métodos stub**. Cada test del paquete construye un fake que
implementa 50 métodos para poder testar 2-7. Un cambio de firma en
cualquier método (ej. `GetChannels` añade un parámetro) rompe TODOS
los tests — los 50 métodos del fake deben compilar.

**Ejemplo de cómo debería ser:**

```go
// iptv_playback_failure.go (necesita 2 métodos)
type playbackFailureReporter interface {
    GetChannel(ctx context.Context, id string) (*iptvmodel.Channel, error)
    RecordProbeFailure(ctx context.Context, channelID string, err error)
}
```

```go
// iptv_epg.go (necesita 5 métodos)
type epgManager interface {
    PublicEPGCatalog() []iptv.PublicEPGSource
    ListEPGSources(ctx context.Context, libraryID string) ([]*iptvmodel.LibraryEPGSource, error)
    AddEPGSource(ctx context.Context, libraryID, catalogID, customURL string) (*iptvmodel.LibraryEPGSource, error)
    RemoveEPGSource(ctx context.Context, libraryID, sourceID string) error
    ReorderEPGSources(ctx context.Context, libraryID string, orderedIDs []string) error
}
```

El `*iptv.Service` concreto satisface ambas interfaces sin cambios.
**`interfaces.go` pierde 114 LoC** cuando todos los handlers migran.
Los tests pasan de un fake de 50 métodos a fakes de 2-7 métodos.

**Principio violado:** Interface Segregation Principle (literalmente),
Go idiom ("accept interfaces, return structs" — interfaces en consumer,
1-3 métodos).

**Gravedad:** Alta (NN — ya catalogado). Es el bloqueo más importante
del paquete.

---

#### TT-2 — `LibraryService` con 25 métodos: misma enfermedad, menor grado

**Dónde:** `handlers/interfaces.go:71-114` (44 LoC).

**Datos verificados:**

| Fichero handler | Métodos que usa | Ratio |
|-----------------|----------------:|------:|
| `library.go` | 12 | 48% |
| `item_detail_handler.go` | 5 | 20% |
| `item_search_handler.go` | 1 | 4% |
| `item_recommendations_handler.go` | 0 de lib (usa externalIDs y providers) | 0% |
| `auth.go` | usa `LibraryService` pero sólo `ListForUser` | 4% |
| `setup.go` | usa `LibraryService` pero sólo `Create` + `Scan` | 8% |

**`SearchHandler` necesita 1 método de 25 (`ListItems`).** El fake para
testarlo debe implementar 25. `RecommendationsHandler` recibe
`LibraryService` pero **no usa ningún método de ella directamente** —
hereda la dependencia del facade `ItemHandler` y sólo la pasa.

**Refactor: interfaces micro en consumer.**

```go
// item_search_handler.go
type itemSearcher interface {
    ListItems(ctx context.Context, filter librarymodel.ItemFilter) ([]*librarymodel.Item, int, error)
}
```

```go
// item_detail_handler.go
type itemFetcher interface {
    GetItem(ctx context.Context, id string) (*librarymodel.Item, error)
    GetItemChildren(ctx context.Context, id string) ([]*librarymodel.Item, error)
    GetItemChildCounts(ctx context.Context, parentIDs []string) (map[string]int, error)
    GetItemStreams(ctx context.Context, itemID string) ([]*librarymodel.MediaStream, error)
    GetItemImages(ctx context.Context, itemID string) ([]*librarymodel.Image, error)
}
```

**Gravedad:** Alta (NN — ya catalogado).

---

#### TT-3 — `Dependencies` como god-struct de 77 campos pasa a CADA mount

**Dónde:** `api/router.go:35-214`.

**Lo que pasa hoy:** Cada `mountXxx` recibe `Dependencies` entero.
`mountStreaming` necesita 7 campos pero recibe 77.
`mountMeNotificationsAndPreferences` necesita 5 pero recibe 77.

**Cada mount file tiene que importar `api` para el tipo `Dependencies`.**
Cuando `Dependencies` cambia (nuevo campo = nueva feature), TODOS los
mount files recompilan aunque no les toque.

**Pero la urgencia real es otra.** El impacto práctico hoy es bajo
porque Go compila rápido y los mounts están en el mismo paquete. El
problema de MM es más estético/de claridad que de correctness. **NN
importa más que MM.** Resolver NN primero y OO después; MM se simplifica
como consecuencia porque cada sub-paquete handler definirá su propio
"deps" struct con 3-7 campos.

**Gravedad:** Alta (MM — ya catalogado), pero **prioridad 3** (después
de NN y OO).

---

#### TT-4 — `deps_repos.go` duplica contratos de `interfaces.go` (refuerza RR)

**Dónde:**
- `interfaces.go:288-307` declara `ItemRepository` (2 métodos: GetByID, List).
- `deps_repos.go:35-45` declara `ItemsRepo` (9 métodos: incluye GetByID, List + 7 más).

Dos interfaces para el mismo repo (`db.ItemRepository`), en el mismo
paquete, con nombres parecidos, contratos solapados. El comentario de
`deps_repos.go` admite el problema sin resolverlo:

> *"el handler sigue consumiendo el contrato estrecho que ya conocía"*

En la práctica **NO hay composición ni embedding entre ellas**. Son
copias independientes que hay que mantener en sincronía manual.

**Refactor:** Desaparece naturalmente al cerrar NN — cada handler define
SU interface micro; `deps_repos.go` desaparece; `Dependencies` pasa los
repos como concretos (o como interfaces "broad" de 1 nivel, si el sub-
paquete las necesita).

**Gravedad:** Baja (RR — ya catalogado). Se resuelve sola con NN.

---

#### TT-5 — `SetErrorRecorder` es estado global mutable

**Dónde:** `handlers/responses.go:47`.

```go
func SetErrorRecorder(fn func(code string)) {
    apperror.SetRecorder(fn)
}
```

`router.go:245` lo llama en `NewRouter`:
```go
if deps.Metrics != nil {
    handlers.SetErrorRecorder(func(code string) {
        deps.Metrics.HTTPErrors.WithLabelValues(code).Inc()
    })
}
```

**Por qué es problema:** Es un package-level global mutable compartido
por toda la vida del proceso. En tests, dos tests paralelos que llamen
`SetErrorRecorder` con fakes distintos se pisan. El cleanup
(`t.Cleanup(func() { SetErrorRecorder(nil) })` en
`responses_test.go:213`) funciona sólo si los tests NO corren en
paralelo — y lo hacen (`t.Parallel()` en la mayoría).

**Principio violado:** Shared mutable state, testability.

**Impacto:** Hoy no se manifiesta como flake porque `responses_test.go`
es el único que setea el recorder en tests. Si mañana otro test lo
setea, hay un data race.

**Gravedad:** Baja (funciona hoy, pero es una bomba de tiempo).

**Refactor:** Pasar el recorder como campo del handler que emite errores
(no como global). Alternativa: aceptar el patrón stdlib (`var timeNow`)
documentando que el global no se toca en paralelo (sólo en init del
proceso). Ambas opciones son 5-10 LoC.

---

#### TT-6 — `IPTVHandler` con 10 deps consume `IPTVService` completo

**Dónde:** `handlers/iptv.go:38-70`.

```go
type IPTVHandler struct {
    svc       IPTVService           // 50 métodos
    proxy     IPTVStreamProxyService
    transmux  IPTVTransmuxer
    logoCache *iptv.LogoCache
    imageDir  string
    libraries LibraryRepository
    access    LibraryAccessService
    audit     AuditEmitter
    bus       EventBusPublisher
    logger    *slog.Logger
}
```

Un solo `IPTVHandler` sirve 12 ficheros (`iptv_channels.go`,
`iptv_favorites.go`, `iptv_admin.go`, `iptv_health.go`, etc.) mediante
receiver `(h *IPTVHandler)`. Todos comparten las 10 deps aunque cada
fichero use 2-4.

**Es el "pre-split P" del IPTV.** El audit 2026-05-14 detectó el patrón
en `ItemHandler` (13 deps, 4 responsabilidades) y lo resolvió con el
facade de 5 sub-handlers. `IPTVHandler` es el mismo olor sin resolver.

**Refactor (siguiendo el patrón exitoso de ItemHandler):**

```
IPTVHandler (facade por embedding)
├── IPTVChannelHandler    (channels, groups, schedule, bulk schedule, now playing)
├── IPTVFavoritesHandler  (add/remove/list favorites, continue watching, record watch)
├── IPTVAdminHandler      (refresh M3U/EPG, preflight check, spawn background)
├── IPTVHealthHandler     (unhealthy channels, health summary, channel active/reset)
├── IPTVEPGHandler        (EPG sources CRUD)
├── IPTVPersonalisationHandler  (per-user channel order, visibility)
├── IPTVAdminOrderHandler (library-level channel order, visibility)
├── IPTVLogoHandler       (channel logo CRUD, iptv-org refresh)
└── IPTVPlaybackFailureHandler  (record probe failure)
```

Cada sub-handler define su micro-interface de `svc` (2-11 métodos).
El facade `IPTVHandler` los embebe y `NewIPTVHandler` distribuye las
deps.

**Gravedad:** Media (nuevo, TT-6). Es el mismo patrón que P pero para
IPTV.

---

#### TT-7 — `NewItemHandler` con 15 parámetros posicionales

**Dónde:** `handlers/items.go:50`.

```go
func NewItemHandler(lib LibraryService, images ImageRepository,
    metadata MetadataRepository, userData UserDataRepository,
    users UserService, chapters ChapterRepository,
    segments EpisodeSegmentRepository,
    externalIDs ExternalIDsRepository,
    people PeopleRepoForItems,
    collections CollectionRepoForItems,
    providers ProviderManager,
    identifier MetadataIdentifier,
    trickplayDir string, audit AuditEmitter,
    logger *slog.Logger) *ItemHandler
```

15 parámetros posicionales. Si alguien reordena dos del mismo tipo
(`ImageRepository` y `MetadataRepository` → ambos interfaces), compila
pero el handler hace cosas raras.

**Principio violado:** KISS, error-proneness.

**Refactor:** Struct `ItemHandlerDeps` con campos nombrados. Ya existe
precedente en `stream.Deps`, `library.Deps`, `iptv.Deps`.

```go
type ItemHandlerDeps struct {
    Lib         LibraryService
    Images      ImageRepository
    Metadata    MetadataRepository
    // ...
}
```

**Gravedad:** Baja (funciona, pero es el constructor más frágil del repo).

---

#### TT-8 — Comentarios en inglés en handlers

**Dónde:** ~40% de los comentarios en `handlers/` siguen en inglés.

Ejemplos:
- `interfaces.go:83-92` — bloques de comentario en inglés para
  `GetItemChildCounts`, `LatestItems`, `ListGenres`.
- `deps_repos.go:1-21` — doc block completo en inglés.
- `responses.go:43-48` — `SetErrorRecorder` doc en inglés.
- `streaming_deadline.go` — doc completo en inglés.
- `cache_control.go` — doc en inglés.

La convención del proyecto dice "comentarios en español, técnicos,
concisos". `library.go`, `system.go`, `auth.go` ya están en español.
La inconsistencia es visible.

**Gravedad:** Baja (cosmética). Hacer incrementalmente al tocar cada
fichero por refactors de NN/OO.

---

### 6.4 Plan de ataque para este paquete

Orden optimizado por valor/desbloqueo/bajo blast radius:

| Paso | Qué | Cierra | Coste | Desbloquea |
|------|-----|--------|-------|------------|
| **1** | Micro-interfaces en cada handler file (empezar por IPTV, luego library, luego auth) | NN | 1 sesión grande | F15-5, OO, tests limpios |
| **2** | Split `IPTVHandler` en sub-handlers (patrón P/ItemHandler) | TT-6 | 1 sesión mediana | Tests IPTV más ligeros |
| **3** | Borrar `interfaces.go` y `deps_repos.go` | RR | mecánico tras paso 1 | Reduce 665 LoC de contratos |
| **4** | Sub-paquetes por dominio en `handlers/` | OO | 1 sesión grande | Blast radius, clarity |
| **5** | Split `Dependencies` en sub-structs por mount | MM | 1 sesión mediana | Claridad composition root |
| **6** | Struct params para `NewItemHandler` et al. | TT-7 | mecánico | Robustez constructores |

Los pasos 1-3 son el núcleo y se pueden hacer sin mover ficheros de
sitio. Los pasos 4-6 son cambios de organización física que dependen
de que los contratos estén resueltos.

---

### 6.5 Estado de cierre (actualizado post-implementación)

#### LibraryService — **cerrado al 100%**

| Consumer | Micro-interface | Métodos |
|----------|----------------|--------:|
| SearchHandler | `itemSearcher` | 1 |
| RecommendationsHandler | `itemGetter` | 1 |
| ItemDetailHandler | `itemDetailFetcher` | 5 |
| LibraryHandler | `libraryOps` | 12 |
| AuthHandler | `authLibraryOps` | 2 |
| SetupHandler | `setupLibraryOps` | 2 |

#### IPTVService — **cerrado al 100% (9/9 sub-handlers)**

| Sub-handler | Micro-interface | Métodos |
|-------------|----------------|--------:|
| `iptvChannelHandler` | `channelBrowseOps` | 11 |
| `iptvPlaybackFailureHandler` | `playbackFailureReporter` | 2 |
| `iptvEPGHandler` | `epgManager` | 5 |
| `iptvPersonalisationHandler` | `channelPersonaliser` | 4 |
| `iptvAdminOrderHandler` | `adminChannelOrderManager` | 4 |
| `iptvAdminHandler` | `iptvAdminOps` | 7 |
| `iptvHealthHandler` | `channelHealthOps` | 7 |
| `iptvFavoritesHandler` | `channelFavoritesOps` | 7 |
| `iptvLogoHandler` | `channelLogoOps` | 5 |

`IPTVHandler` es ahora un facade puro (0 campos directos, 9 sub-handlers
embebidos). `IPTVService` en `interfaces.go` puede eliminarse cuando se
migren los tests al fake por sub-handler.

#### Otros fixes implementados

| Olor | Estado |
|------|--------|
| SS-1 (notification importa db/sqlc directo) | ✅ cerrado por doc — sabor C añadido en `conventions.md` |
| SS-3 (defer fuera de lifecycle) | ✅ cerrado |
| SS-4 (os.Exit en run) | ✅ cerrado |
| SS-5 (browse sin per-peer timeout) | ✅ cerrado |
| SS-6 (cleanupLoop sin drain en Shutdown) | ✅ cerrado — `m.cleanDone` channel ([PR #481](https://github.com/Alexzafra13/HubPlay_demo/pull/481)) |
