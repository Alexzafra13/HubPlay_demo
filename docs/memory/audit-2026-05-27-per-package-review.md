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

¿Por cuál empezamos?
