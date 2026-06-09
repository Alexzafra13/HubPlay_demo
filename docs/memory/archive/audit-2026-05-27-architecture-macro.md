# Auditoría arquitectónica macro — 2026-05-27 (vivo)

> Rama: `main` · Base: `e5f96fb` (commit del cleanup project-status)
> Método: review macro top-down (estructura → grafo de deps → flujo) +
> revisión por paquete (8 paquetes pendientes). Cada hallazgo verificado
> contra código con ruta y línea. Documento vivo — crece a medida que se
> revisa cada paquete.

> Diferencia con [`audit-2026-05-14-go-backend-review.md`](audit-2026-05-14-go-backend-review.md):
> aquel audit (110+ olores, 16 fases) cubrió hasta nivel de función y queda
> casi al 100% cerrado (6/6 olores altos + medium + F16 medium + bajas).
> Este audit se enfoca en los **olores estructurales que emergieron por
> agregación** después de cerrar lo táctico: god-structs de wiring,
> interfaces de servicio gigantes, fuga de tipos de persistencia, y
> división del mega-paquete `handlers/`. Son problemas de **shape**, no
> de bugs.

---

## Índice

| § | Sección | Propósito |
|---|---|---|
| 1 | [Veredicto general](#1-veredicto-general) | Qué está bien, qué no, contexto |
| 2 | [Hallazgos macro](#2-hallazgos-macro) | 6 problemas con ubicación, principio, refactor |
| 3 | [Estructura de carpetas](#3-estructura-de-carpetas) | Tamaños, sub-packages, sub-domains |
| 4 | [Grafo de dependencias](#4-grafo-de-dependencias) | Imports cross-package, ciclos, sink pattern |
| 5 | [Flujo general](#5-flujo-general-del-sistema) | main.run, lifecycle, fases de boot |
| 6 | [Cola de revisión por paquete](#6-cola-de-revisión-por-paquete) | **Por dónde seguir** — 8 paquetes ordenados |
| 7 | [Decisiones pendientes](#7-decisiones-pendientes) | Preguntas al owner antes de tocar código |

---

## 1. Veredicto general

El backend está **más maduro de lo que sugiere su tamaño**. 6 audits previos,
F14–F16 cerrados, ADRs documentados, sink pattern aplicado, ciclos rotos.
Donde uno esperaría ver olores típicos hay decisiones explícitas:

- Observability no contamina downstream (sink pattern con `noopSink{}` default).
- Scanner no importa library (acíclico). Library consume scanner, no al revés.
- Modelos extraídos a `library/model`, `iptv/model`, `auth/model` precisamente para romper el ciclo handlers ↔ servicio ↔ tipos.
- Lifecycle reemplaza el god-struct `runtime` con 3 fases (workers add-order, HTTP drain, services LIFO).

**Lo que sigue NO es feedback de "novato Go disfrazando Java"** porque eso no
es lo que veo. Lo que veo es el problema opuesto: **un codebase Go honesto
que ha estado luchando contra su propia ancho de API**. Cada nueva feature
ha añadido un campo a `Dependencies` y un método a `IPTVService` sin reabrir
la pregunta de si la abstracción sigue valiendo. Los olores listados abajo
son los que han ido **emergiendo por agregación**, no por mal diseño inicial.

### Métricas verificadas (2026-05-27)

- **28 paquetes** en `internal/`.
- **46k LoC producción** internal/, **191** ficheros `_test.go`.
- **77 campos** en `api.Dependencies` ([router.go:35-214](../../internal/api/router.go#L35)).
- **60+ ficheros** en `internal/api/handlers/` (un solo package).
- **450 LoC** en `handlers/interfaces.go` (contratos de servicio).
- **217 LoC** en `handlers/deps_repos.go` (contratos de repos).
- **35 imports** de `internal/db` desde `handlers/**` (fuga de tipos sqlc).

---

## 2. Hallazgos macro

### Resumen por gravedad

| Severidad | # | Olores |
|---|---:|---|
| 🔴 Alta | 2 | MM (Dependencies god-struct), NN (interfaces de servicio gigantes) |
| 🟡 Media | 3 | OO (handlers mega-package), PP (tipos db fugan a handlers), QQ (main.run de 630 LoC) |
| 🟢 Baja | 1 | RR (duplicación interfaces.go ↔ deps_repos.go — se resuelve cerrando NN) |

Letras siguen la convención del audit anterior (A..LLLL ya usadas). Estos son los nuevos.

---

### 🔴 MM — `api.Dependencies` con 77 campos (god-struct de wiring)

**Dónde:** [internal/api/router.go:35-214](../../internal/api/router.go#L35).

**Síntomas verificados:**
- 77 campos. El propio struct mezcla tres tipos de cosa distintos:
  1. Servicios concretos (`*auth.Service`, `*stream.Manager`, `*iptv.Service`, ...): **12 campos**.
  2. Interfaces "broad" de repos (`handlers.ItemsRepo`, `handlers.ImagesRepo`, ...): **18 campos**.
  3. Primitivos derivados de config (`DataDir`, `DatabasePath`, `ServerAddr`, `ServerBaseURL`, `MetricsEnabled`, `MDNSEnabled`, `HWAccelDefault`, `AllowedOrigins`, `TrustedProxies`, ...): **12 campos**.
  4. Más `Config *config.Config` (live, mutable) + `fillFromConfig()` que rellena los primitivos si vienen a cero.
- `fillFromConfig()` ([router.go:435-479](../../internal/api/router.go#L435)) admite el problema:
  > *"main.go los pasa siempre explícitos; retro-compat con tests minimalistas que sólo pasan Config: cfg"*.
- Nil-safe checks proliferan dentro del router ("nil = handlers caen a 503", "nil deshabilita endpoints", ...). Cada campo opcional es una rama.

**Principio que viola:** SRP (struct con tres responsabilidades), Interface Segregation (cada handler recibe `Dependencies` entero aunque toque 3 campos), KISS.

**Impacto futuro:** Mientras se mantenga así, no hay arquitectura defensiva contra el growth. Añadir features → añadir campos → más cohesión a la baja del paquete `api`. Hoy 77, en 6 meses 90+.

**Refactor (gradual):**

1. Romper `Dependencies` en agrupaciones que ya viven en los `mount_*.go`:
   ```go
   type Dependencies struct {
       Foundation FoundationDeps  // Logger, Metrics, Clock, EventBus, Auth
       HTTP       HTTPDeps        // CorsRegistry, SSELimiter, TrustedProxies
       Media      MediaDeps       // Libraries, Items, Images, Metadata, ...
       Streaming  StreamingDeps   // StreamManager, HWAccelDefault
       IPTV       IPTVDeps
       Federation FederationDeps
       Admin      AdminDeps
   }
   ```
   Cada `mountXxx(r, deps.X)` recibe solo su grupo. Tests minimalistas construyen `Dependencies{Foundation: ...}` y dejan el resto en cero.
2. Eliminar `Config *config.Config` del Dependencies. Los dos handlers que mutan el YAML (setup wizard, admin DB) reciben un `*config.Writer` específico con interfaz estrecha `Save(cfg Config) error`. El resto no recibe ni Config ni primitivos.
3. `fillFromConfig()` desaparece.

**Bloquea a:** OO (split del mega-paquete handlers) parcialmente, NN (interfaces gigantes) tangencialmente.

---

### 🔴 NN — `handlers/interfaces.go` con interfaces "de servicio" gigantes

**Dónde:** [internal/api/handlers/interfaces.go](../../internal/api/handlers/interfaces.go) (450 LoC).

Concretamente:
- `LibraryService` — **25 métodos** ([interfaces.go:71-114](../../internal/api/handlers/interfaces.go#L71)).
- `IPTVService` — **~50 métodos** ([interfaces.go:146-260](../../internal/api/handlers/interfaces.go#L146)).
- `AuthService` — **16 métodos**.
- `UserService` — **13 métodos**.

**Por qué es problema:**

Esto es exactamente el patrón que Go idiom recomienda evitar (*"accept interfaces, return structs"* — Rob Pike). La interface se define en el **consumidor**, contiene SOLO los métodos que ese consumidor llama, y es típicamente de 1–3 métodos.

Lo que pasa aquí:
1. **El consumidor no sabe lo que usa.** `library_test.go` mockea `LibraryService` enteros con stubs de 25 métodos. Si un test toca `GetItem`, los otros 24 métodos están ahí como ruido; cualquier cambio (renombrado, firma) en uno de los 24 fuerza al test a editar.
2. **God-mock anti-pattern**. El audit anterior ya lo identificó (F15-5 pendiente: *"God-handler mocks de 16 métodos en handlers/library_test.go e items_test.go. Reducir fakes + añadir tests de integración con DB real"*). **F15-5 es un síntoma; NN es la causa raíz.**
3. **Imposibilita la división del package handlers**, porque la interface vive en común y cualquier sub-package nuevo tendría que importarla.
4. **Estilo "service interface" típico de Spring/Java**: una sola interface por servicio, todos los métodos.

**Principio que viola:** Interface Segregation Principle (literalmente), Go idiom de "interfaces en el consumer", Dependency Inversion bien aplicado.

**Impacto futuro:**
- F15-5 no se cerrará bien sin atacar esto primero.
- Cada vez que se añade un método al servicio se añade aquí, incluso si solo un handler nuevo lo necesita.
- Tests frágiles: cambiar una firma rompe N mocks.

**Refactor (uno a uno; empezar por LibraryService porque es el más limpio):**

Por cada handler, definir su contrato local mínimo en el mismo fichero del handler:

```go
// handlers/items.go
type itemFetcher interface {
    Get(ctx context.Context, id string) (*librarymodel.Item, error)
    GetChildren(ctx context.Context, id string) ([]*librarymodel.Item, error)
}

type ItemHandler struct {
    items   itemFetcher
    streams itemStreamFetcher
    // ...
}
```

El `*library.Service` concreto satisface todas estas micro-interfaces porque tiene los métodos. **`handlers/interfaces.go` puede borrarse** cuando todos sus consumidores hayan migrado.

**Bloquea a:** F15-5 (que está pendiente), OO (split handlers).

---

### 🟡 OO — `internal/api/handlers/` es un mega-paquete con 60+ ficheros

**Dónde:** `internal/api/handlers/` — 60+ `.go` (excluyendo tests). Conviven 15 dominios distintos en un solo package: auth, items, library, iptv (10 ficheros), federation (5), me_*, admin_*, image, stream, etc.

**Síntomas verificados:**
- Un solo `package handlers` fuerza prefijos defensivos en los nombres de tipos (`IPTVPersonalisationHandler`, `MePeerImageHandler`, `AdminStreamsHandler`) — síntoma de no-empaquetado.
- El paquete es un god-package: cualquier handler puede tocar cualquier helper interno sin reconocimiento explícito. `interfaces.go` (450 LoC) y `deps_repos.go` (217 LoC) viven aquí porque no hay un lugar mejor.
- Tests de un handler pueden romper al añadir un helper en otro porque comparten globals (ej. `errorRecorder` registrado vía `SetErrorRecorder`).

**Principio que viola:** SRP a nivel paquete, Interface Segregation (al definir contratos masivos centralmente). KISS no — al revés, evitar romperlo es lo que mantiene este olor.

**Impacto futuro:** Coste constante por añadir features (cada handler nuevo sube el ruido del package), refactors atómicos imposibles. Cuando llegue una feature grande nueva (chat, plugins) ahuyentará al desarrollador a *crear otro mega-package*.

**Refactor:** Sub-packages por dominio:

```
internal/api/handlers/
  common/      # respondData, ClientIP, AppError mapping, CacheControl, requireParam
  auth/        # auth.go, auth_device.go, sessions.go + contracts.go
  library/     # library.go, items.go, items_search.go + contracts.go
  iptv/        # 10 ficheros + un solo contracts.go con la interface mínima por handler
  federation/  # federation_admin/public/url/image + contracts.go
  admin/       # admin_auth, admin_db, admin_streams, admin_logs, admin_backup, admin_storage
  me/          # me_home, me_events, me_peers*, me_peer_image/progress/stream, notifications, preferences
  system/      # health.go, system.go, updates.go, setup.go
```

Cada sub-package define SUS contratos pequeños. `interfaces.go` se elimina (resuelve NN). El refactor cierra la duda permanente *"¿dónde meto el nuevo handler?"* porque el nombre del dominio decide.

**Coste estimado:** alto (~1-2 sesiones grandes), pero **cada sub-package puede moverse independientemente** si se hace incrementalmente.

**Bloqueado por:** parcial NN (las interfaces locales tienen que existir antes de mover los ficheros).

---

### 🟡 PP — Tipos de `internal/db` fugan a la capa handlers (35 imports)

**Dónde:** `internal/api/handlers/**` importa `internal/db` 35 veces. Tipos concretos:
- `db.UserData`, `db.ContinueWatchingItem`, `db.FavoriteItem`, `db.NextUpItem`.
- `db.HomeTrendingItem`, `db.HomeRecommendation`, `db.HomeLiveNowChannel`, `db.HomeBecauseResult`.
- `db.UserPreference`, `db.ProviderConfig`, `db.UploadAuditRow`.
- `db.DailyWatchBucket`, `db.TopItemRow`, `db.LibrarySizeRow`.

**Por qué es problema:**
- Los handlers atan su contrato HTTP a tipos generados por (o adyacentes a) sqlc. Si renombras una columna y regeneras sqlc, el handler nota.
- La separación handler ↔ servicio ↔ repo se filtra: `UserDataRepo` en `deps_repos.go` declara métodos que devuelven `*db.UserData`. El contrato del handler tiene grano de la persistencia.
- En contraste, `library/model.Item`, `iptv/model.Channel` están bien aislados — son los tipos verdaderamente del dominio.

**Principio que viola:** Dependency Inversion (handler depende de detalle de DB), Separation of Concerns.

**Impacto futuro:** Cualquier rediseño del esquema HomeTrending implica un PR cross-layer (db + repo + handler + tests). Soportable hoy porque los handlers son delgados; deja de serlo cuando entren más features.

**Refactor:** Mover tipos a su sub-`model`:
- `db.UserData` → `library/model.UserData` (es per-(usuario, item)).
- `db.HomeTrendingItem`, `HomeRecommendation`, `HomeLiveNowChannel`, `HomeBecauseResult` → nuevo `internal/home/model/` o `library/model/home.go`.
- `db.UserPreference` → `internal/user/model` (sub-package nuevo).
- `db.AuditLogRow`, `db.TopItemRow`, `db.DailyWatchBucket` → `internal/audit/model`.

Lo que se queda en `internal/db`: queries, adapters sqlc, dialect, maintenance, migrator. Esto sí es infra pura.

**Coste:** medio. Trabajo mecánico (move + rename imports) pero toca muchos ficheros.

---

### 🟡 QQ — `main.run()` con ~630 LoC de wiring imperativo

**Dónde:** [cmd/hubplay/main.go:82-713](../../cmd/hubplay/main.go#L82).

**Síntomas verificados:**
- 7 fases marcadas con comentarios, pero todo en una sola función.
- Construcción del `Dependencies` (líneas 605-685) ocupa 80 líneas; mucha repetición tediosa (`Items: repos.Items, MediaStreams: repos.MediaStreams, ...`).
- Lógica de negocio mezclada con wiring: lectura de overrides desde `app_settings` (líneas 293-317), bloqueo del avatar dir si DB en memoria (líneas 207-209), parseo de uploads habilitado (líneas 503-561), federation init con fallback no-fatal (líneas 416-469).
- Test coverage de `run()` es prácticamente cero — es el punto ciego más obvio de los 191 `_test.go`.

**Principio que viola:** SRP. La fase "Phase 4b: Streaming" lee app_settings, parsea ints, valida, hace fallback al auto-tuner; eso no es wiring, es lógica de configuración runtime.

**Impacto futuro:** Cualquier feature nueva añade un bloque de ~20 LoC aquí. La función pasará de 630 → 800 → 1000 LoC. Y nadie querrá tests aquí porque su shape es "construir todo el mundo".

**Refactor:** Extraer builders por fase a funciones libres:

```go
// cmd/hubplay/main.go (~150 LoC)
func run(configPath string) error {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    foundation, err := buildFoundation(configPath)
    if err != nil { return err }

    database, repos, err := openDatabase(foundation.Config)
    if err != nil { return err }
    defer database.Close()

    infra := buildInfra(foundation, repos)

    lc := &lifecycle{}
    libMod := mustBuild(library.New(ctx, libraryDeps(foundation, infra, repos)))
    libMod.RegisterWith(lc)
    iptvMod := mustBuild(iptv.New(ctx, iptvDeps(foundation, infra, repos)))
    iptvMod.RegisterWith(lc)

    streamMgr := buildStreamManager(ctx, foundation, infra, repos)
    lc.AddService("stream manager", func(_ context.Context) error { streamMgr.Shutdown(); return nil })

    deps := buildHTTPDeps(foundation, infra, repos, libMod, iptvMod, streamMgr, ...)
    server := buildHTTPServer(foundation.Config, api.NewRouter(deps))

    go serve(server, foundation.Logger, cancel)
    return waitForShutdown(ctx, cancel, server, lc, database, foundation.Config.Database.Driver, foundation.Logger)
}
```

Cada builder vive en su fichero (`build_database.go`, `build_infra.go`, `build_http.go`), es testeable como unidad.

**Sub-olor QQ-a — Lectura de `app_settings` desde main.go:**
- [cmd/hubplay/main.go:293-317](../../cmd/hubplay/main.go#L293) lee 5 claves de `app_settings` para construir `streamingCfg`. main.go conoce las claves exactas (`"hardware_acceleration.enabled"`, etc.). Refactor: el paquete `stream` provee un `ApplyRuntimeOverrides(ctx, settingsReader, cfg)` y main.go solo pasa el `settingsReader`.

---

### 🟢 RR — Duplicación `interfaces.go` ↔ `deps_repos.go` (se resuelve con NN)

**Dónde:**
- [internal/api/handlers/interfaces.go:288-307](../../internal/api/handlers/interfaces.go#L288) declara `ItemRepository`, `ImageRepository`, `MetadataRepository`.
- [internal/api/handlers/deps_repos.go:35-101](../../internal/api/handlers/deps_repos.go#L35) declara `ItemsRepo`, `ImagesRepo`, `MetadataRepo` (con sufijo `Repo` para no chocar).

**Por qué es problema:** El comentario de `deps_repos.go` afirma:
> *"Cuando un handler ya tiene su interface estrecha (...), la interface 'broad' la re-exporta como subset por composición — el handler sigue consumiendo el contrato estrecho que ya conocía."*

Pero en la práctica **NO hay composición**. Son interfaces independientes que se parecen. Cuando se añade un método hay que mantener la sincronía en dos sitios.

**Principio que viola:** DRY, KISS.

**Impacto:** Erosión semántica. En 6 meses nadie sabrá por qué `ItemRepository` tiene 2 métodos y `ItemsRepo` tiene 8.

**Refactor:** Se resuelve automáticamente cerrando NN — solo queda una interface, micro, definida en el handler que la usa. `deps_repos.go` se elimina porque `Dependencies` deja de tipar repos como `handlers.X`.

---

## 3. Estructura de carpetas

### Tamaños por paquete (producción, sin tests)

| Paquete | LoC | Comentario |
|---|---:|---|
| `internal/db` | 13.065 | Dominado por sqlc-generated (`sqlc/`, `sqlc_pg/` ≈ 4-5k). |
| `internal/iptv` | 8.918 | El más grande de los dominios "vivos". transmux.go 1107 + proxy.go 804 + channel_order_ops.go 731. |
| `internal/federation` | 4.759 | Razonable para un servicio P2P con identidad propia. |
| `internal/library` | 3.204 | Watcher + service + segment detection + image refresher. |
| `internal/provider` | 2.280 | TMDb 788 + Fanart + OpenSubtitles + manager. |
| `internal/stream` | 2.270 | Manager 823 — alto pero focalizado. |
| `internal/api` | 2.239 | + handlers/ aparte (~25k). |
| `internal/auth` | 2.164 | Service + JWT + rate limit + device code + permissions. |
| `internal/scanner` | 2.027 | Walker + enrich + ingest. |
| `internal/upload` | 1.872 | tusd integration + GC + service. |

### Sub-packages bien aplicados

- `internal/clock` (21 LoC): solo el seam de tiempo.
- `library/model`, `iptv/model`, `auth/model`: tipos compartidos por sub-package. Rompe el ciclo handlers ↔ servicio.
- `federation/storage`: separa el repo de la lógica del manager.

### Ficheros más grandes (señales para revisión profunda)

```
1465 internal/db/sqlc/federation.sql.go        (generated)
1115 internal/api/handlers/auth.go
1107 internal/iptv/transmux.go
 981 internal/db/user_repository.go
 938 internal/db/item_repository.go
 906 internal/api/handlers/me_home.go
 823 internal/stream/manager.go
 817 internal/api/handlers/federation_admin.go
 812 internal/db/channel_repository.go
 804 internal/iptv/proxy.go
 794 internal/db/user_data_repository.go
 788 internal/provider/tmdb.go
 781 cmd/hubplay/main.go
 777 internal/api/handlers/library.go
 752 internal/api/handlers/stream.go
 731 internal/iptv/channel_order_ops.go
 713 internal/api/handlers/iptv_channels.go
```

Top sospechosos a auditar (sin contar sqlc generated):
- **auth.go 1115 LoC** (handler) — F14-3/4/5 ya splitearon funciones largas, pero ¿el handler entero merece split?
- **transmux.go 1107 LoC** (iptv) — sospechoso de god-component.
- **me_home.go 906 LoC** (handler) — agrupa Home/Trending/Recommended/BecauseYouWatched/LiveNow; ¿split por endpoint?
- **manager.go 823 LoC** (stream) — núcleo del transcoding; revisar concurrencia.
- **federation_admin.go 817 LoC** (handler) — admin de peers, pairing, sync.

---

## 4. Grafo de dependencias

### Lo que está verificado bien

| Hecho | Implicación |
|---|---|
| `internal/observability` no aparece en imports de stream/handlers/iptv/federation. | Sink pattern correctamente aplicado. |
| `internal/scanner` no importa `internal/library`. | Acíclico. `library` consume `scanner`, no al revés. |
| `internal/stream` solo importa `library/model` (no `library`). | Tipos compartidos por sub-package. No hay ciclo. |
| `internal/iptv` solo importa `library/model`. | Igual: cero acoplamiento con library/service o library/scanner. |
| `internal/federation` no importa `library`, `iptv`, `stream` ni `provider`. | Federation está bien aislada como feature ortogonal. |
| No ciclos declarados por `go vet`/`golangci-lint`. | Confirmado por CI verde. |

### Imports cruzados (paquetes principales)

```
internal/stream      → clock, config, db, domain, event, library/model
internal/library     → clock, db, domain, event, imaging, imaging/pathmap,
                       library/model, probe, provider, scanner
internal/iptv        → clock, db, event, imaging, iptv/model, library/model
internal/federation  → clock, domain, event, imaging
internal/scanner     → clock, db, event, imaging, imaging/pathmap,
                       library/model, probe, provider
internal/provider    → db
```

### Imports desde `handlers/**` (top)

```
46 hubplay/internal/auth          (middleware + service)
35 hubplay/internal/db            ← FUGA DE TIPOS (PP)
33 hubplay/internal/library/model
31 hubplay/internal/domain
23 hubplay/internal/testutil
13 hubplay/internal/iptv/model
11 hubplay/internal/stream
11 hubplay/internal/library
11 hubplay/internal/federation
11 hubplay/internal/event
11 hubplay/internal/auth/model
```

---

## 5. Flujo general del sistema

### Boot — 7 fases ([main.go:82-713](../../cmd/hubplay/main.go#L82))

1. **Foundation**: config, logger, clock, PATH prepend (bundled ffmpeg), preflight.
2. **Database**: open + restore-if-any + migrate + repos.
3. **Infrastructure**: event bus, metrics, observability.
4. **Core Services**:
   - 4a Library Module (`library.New() → *Module`, ~10 componentes long-lived).
   - 4b Streaming (`stream.NewManager` + runtime overrides desde app_settings).
   - 4c IPTV Module (`iptv.New() → *Module`, ~6 componentes long-lived).
   - 4d Providers (TMDb, Fanart, OpenSubtitles).
   - 4e Setup service.
5. **HTTP Server**: federation init + uploads + audit + updates + mdns + cors registry + router + http.Server.
6. **Start**: `go server.ListenAndServe()`.
7. **Wait for shutdown**: `waitForShutdown(ctx, cancel, server, lc, ...)`.

### Shutdown — 3 fases ([lifecycle.go:54-93](../../cmd/hubplay/lifecycle.go#L54))

1. **Workers** (add-order): background jobs paran primero para no generar actividad nueva.
2. **HTTP drain**: `server.Shutdown(ctx)`.
3. **Services** (LIFO): componentes HTTP-coupled paran en orden inverso al de registro.

Esto es **textbook**. Reemplaza el `runtime` god-struct con 16 campos posicionales. El razonamiento "workers add-order, services LIFO" está bien justificado en el comentario.

---

## 6. Cola de revisión por paquete

**Por dónde seguir.** Cada paquete tendrá su propia sección añadida abajo (§8+) cuando se audite. El orden está optimizado por valor/coste: atacar primero lo que desbloquea más cosas downstream.

| # | Paquete | Foco | Por qué primero | Bloquea a |
|---|---|---|---|---|
| **1** | `internal/api` + `internal/api/handlers` | Romper `Dependencies` (MM) + eliminar `interfaces.go` gigante (NN) + sub-packages por dominio (OO) | Es el cuello donde todo converge. Resolver aquí libera F15-5 y permite refactors downstream. | F15-5, NN→OO→PP en cadena |
| **2** | `internal/iptv` | service.go/proxy.go/transmux.go/channel_order_ops.go — 4 ficheros grandes. ¿Hay sub-domain real? | Más grande (8.9k LoC). Si el split de IPTVService (NN) se hace bien, este paquete revela su shape natural. | — |
| **3** | `internal/stream` | Manager 823 LoC + decisión direct/transcode + sesiones. Concurrencia y leaks. | Núcleo de performance. Estado mutable + workers + cancelación. | — |
| **4** | `internal/library` + `internal/scanner` | Cross-wiring scanner/service/watcher. Job lifecycle. | Concurrencia (file watcher, scheduler, fingerprinter). | — |
| **5** | `internal/db` | Sacar tipos de dominio fuera (PP). Audit del shape de los repos vs. queries sqlc. | Trabajo mecánico pero importante para la separación de capas. Sale natural si NN→OO cerrados. | PP |
| **6** | `internal/auth` | JWT keystore, rate limit, device codes, permissions. | Estable; revisar interfaces de servicio (16 métodos en `AuthService` ya cubierto por NN). | — |
| **7** | `internal/federation` | Manager, client, peer protocol. | Aislado del resto — revisable independientemente. | — |
| **8** | `internal/provider`, `internal/upload`, `internal/event`, `internal/observability` | Iteraciones cortas. | Más pequeños, ya parecen estar bien. | — |

### Preguntas concretas a llevar a cada revisión por paquete

Para cada paquete:

1. **Shape**: ¿el paquete tiene un sub-domain implícito que justifica split? (ej. iptv puede ser `iptv/proxy`, `iptv/epg`, `iptv/transmux`, `iptv/personalisation`).
2. **Interfaces**: ¿el servicio expone una interface gigante (NN) o tipos concretos?
3. **Concurrencia**: ¿hay shared mutable state? ¿workers con bound? ¿context propagation correcta?
4. **Lifecycle**: ¿el Shutdown está bien aislado? ¿hay leaks de goroutines (goleak coverage)?
5. **Tipos**: ¿salen tipos de `db` o `infra` cruzando capas hacia handlers? (PP).
6. **Errores**: ¿uso correcto de `domain.AppError` + `errors.Is`?
7. **Tests**: ¿cobertura de error paths? ¿god-mocks? ¿test fragility con time.Sleep?

---

## 7. Decisiones pendientes

Antes de bajar al paquete 1 (`internal/api` + `handlers`):

### Q1 — Orden de ataque dentro del paquete 1

- **Opción A**: NN primero (interfaces gigantes → micro en consumer), luego OO (sub-packages), luego MM (split Dependencies).
- **Opción B**: MM primero (Dependencies → FoundationDeps/MediaDeps/...), luego NN, luego OO.

NN es lo que más desbloquea (F15-5 + OO + tests más limpios). MM es más visible pero menos interconectado. **Recomendación: NN primero.**

### Q2 — Alcance de PP (mover tipos db → model)

- **Inline** durante NN/OO: cuando se mueva cada handler a su sub-package, mover sus tipos `db.X` a `library/model/X`/`audit/model/X` en la misma sesión.
- **Separado** como sesión propia: hacer NN+OO con `db.X` todavía importado, luego una sesión de "tipo cleanup".

**Recomendación: separado.** El move de tipos es mecánico y voluminoso; mezclarlo con cambios de interfaces oscurece el diff.

### Q3 — QQ (split main.run) timing

- **Antes** de tocar handlers: extraer builders ya, baja a 150 LoC, gana tests.
- **Después**: hacer NN+OO+PP primero porque QQ es síntoma, no causa.

**Recomendación: después.** QQ es local y bajo impacto; lo grande está abajo.

---

## 8. Revisiones por paquete

> Sección que crecerá. Cada paquete revisado añade su sub-sección
> (`### 8.1 internal/api + handlers`, `### 8.2 internal/iptv`, ...) con
> hallazgos al mismo nivel de detalle que §2 (ubicación + principio +
> refactor + ejemplo + gravedad).

**Status:** 0 / 8 paquetes revisados.

---

## Cierre y protocolo de sesiones

- Cada sesión de revisión por paquete actualiza §8 con su sub-sección y mueve
  el contador "Status: N/8".
- Los olores nuevos siguen la nomenclatura SS, TT, UU, VV... (continuando la
  serie del audit anterior que cerró en LL).
- Al cerrar un olor por código, se marca aquí + se añade nota en
  [`project-status.md`](project-status.md) + se mergea como PR independiente
  (no batches grandes).
- Cuando todos los olores macro estén cerrados, este audit pasa a
  `archive/audit-2026-05-27-architecture-macro.md` con el sello "cerrado".
