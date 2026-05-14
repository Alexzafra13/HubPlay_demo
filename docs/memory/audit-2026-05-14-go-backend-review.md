# Auditoría arquitectónica — Go backend (2026-05-14, vivo)

> Rama: `claude/review-go-media-backend-37MDe` · Base: `991dc19` (merge #283)
> Método: review por fases. Cada fase se apendiza aquí y se empuja a la rama
> antes de pasar a la siguiente. Todo lo que sigue está verificado contra
> código; cuando una afirmación necesita confirmación en una fase futura, se
> marca explícitamente `[PENDIENTE-FX]`.

> Diferencia con `audit-2026-04-15.md` / `audit-2026-04-28.md` /
> `audit-2026-05-05.md`: aquellos son snapshots cerrados (sweep + plan +
> cierre). Éste es un **documento vivo** que crece fase a fase y termina con
> un **plan de intervención final consolidado** una vez recorridos todos los
> paquetes.

---

## Índice

- [Resumen ejecutivo](#resumen-ejecutivo) — se actualiza al final de cada fase.
- [Fase 1 · Panorama: estructura, dependencias, flujo](#fase-1--panorama)
- [Fase 2 · `internal/db/`](#fase-2--internaldb) — pendiente
- [Fase 3 · `internal/api/` + `internal/api/handlers/`](#fase-3--internalapi--handlers) — pendiente
- [Fase 4 · `internal/library/` + `internal/scanner/`](#fase-4--library--scanner) — pendiente
- [Fase 5 · `internal/iptv/`](#fase-5--internaliptv) — pendiente
- [Fase 6 · `internal/stream/`](#fase-6--internalstream) — pendiente
- [Fase 7 · `internal/auth/` + `internal/federation/`](#fase-7--auth--federation) — pendiente
- [Fase 8 · `internal/event/` y primitivos sin deps](#fase-8--event--primitivos) — pendiente
- [Plan de intervención final](#plan-de-intervención-final) — se redacta al cerrar.

---

## Resumen ejecutivo

> Se actualiza al cierre de cada fase. Fase 1 cerrada — ver hallazgos abajo.

Estado tras Fase 1 (sólo panorama, sin descender a paquetes):

- Layout *package-por-dominio*, **sin anti-patrones tipo `service/`,
  `repository/`, `dto/` separados**. Es Go idiomático en lo macro.
- **Cero ciclos** directos entre paquetes. 8 paquetes raíz sin imports
  internos (`domain`, `event`, `clock`, `logging`, `observability`,
  `probe`, `blurhash`, `sysmetrics`) — activo arquitectónico valioso.
- **Tres olores estructurales con severidad alta-media** detectados a
  vista de pájaro:
  1. `internal/db/` es un god-package (13 KLOC, 31 repos, 80 tipos).
  2. `internal/db/federation_repository.go` invierte la capa
     (`db → federation`). Única violación de capa real.
  3. `Dependencies` (35+ campos) + `runtime` (14 campos) + `main.run`
     (645 LOC) son síntomas de wiring manual sin módulos compuestos.

Riesgos altos pendientes de confirmar en fases siguientes: lifecycle de
subscribers del event bus, nil-checks en `Federation == nil`, 5 setters
opcionales en `stream.Manager`, `*sql.DB` raw en `Dependencies` como
backdoor.

---

## Fase 1 · Panorama

Cerrada · 2026-05-14.

### 1.1 Estructura de carpetas

**Lo que es**: 24 paquetes en `internal/`, layout *package-por-dominio*,
con `internal/db/` como capa horizontal compartida y `internal/api/`
como adaptador HTTP. No es Clean Architecture canónica ni hexagonal
estricta — y eso es positivo en Go.

#### Sano

- Sin pseudo-Java (`service/`, `repository/`, `dto/`, `interfaces/`
  como paquetes-por-tipo). Cada feature owns sus tipos, sus servicios
  y sus tests.
- `internal/domain` minimal (332 LOC, un fichero): sólo `AppError` con
  `.Kind` sentinel. No es anemic-domain-model; los modelos vivos están
  en cada feature.
- 8 paquetes raíz con cero deps internas (`domain`, `event`, `clock`,
  `logging`, `observability`, `probe`, `blurhash`, `sysmetrics`).
- `internal/api/apperror` aislado como sub-paquete para romper ciclo
  `auth ↔ handlers`. Documentado en su package doc.

#### Olores

##### A — `internal/db/` god-package · **alta**

- 39 ficheros, **13 268 LOC**, ~18% del backend.
- Define 31+ repos, 80+ tipos exportados, helpers de migración, factory
  `Repositories`, dual-dialect templates raw, adaptadores sobre
  `sqlc`/`sqlc_pg`, periodic optimize, restore pendiente, modelos
  públicos (`db.Item`, `db.Channel`, `db.User`, `db.Image`, …).
- Principios violados: **SRP del paquete** (no de los ficheros — los
  ficheros sí son single-responsibility, el paquete hace ~8 cosas).
  **Cohesión** baja entre orquestación, adapter sqlc y modelos.
- Impacto futuro: cualquier cambio recompila el paquete entero; "¿qué
  es `db.X`?" tiene 8 respuestas distintas; bloquea futuros backends
  (Redis, etc.) sin refactor masivo.
- Refactor sugerido (incremental, no big-bang):
  - **Opción A** — `internal/db/` mantiene factory + helpers
    `Open/Migrate/Optimize`; sub-paquetes `internal/db/items/`,
    `internal/db/iptv/`, `internal/db/federation/` agrupan repo + tipos
    por dominio. Promociona la organización física actual a paquetes.
  - **Opción B (más idiomática Go, dirección recomendada)** — cada
    feature (`iptv`, `library`, …) **define sus propios tipos** y
    expone su repo; `internal/db/` queda reducido al adapter sqlc + a
    `Open/Migrate`. Esto ya pasa de facto en `federation` (ver olor B).

##### B — `db → federation`: inversión de capa · **alta**

- `internal/db/federation_repository.go:14` importa
  `"hubplay/internal/federation"` para devolver
  `*federation.Identity`, `*federation.Invite`, `*federation.Peer`.
- Contradice el patrón del resto del repo (modelos en `db.X`). Es la
  **única violación de capa real** del proyecto.
- Admitido en ADR-012: *"el repo no es sqlc todavía... ship-first
  decision"*.
- Principio violado: **Dependency Inversion** entre capas.
- Decisión recomendada antes del sweep de `db/`: tomar la **dirección
  de federation** (cada paquete owns sus tipos, el repo se queda como
  adapter sqlc puro) y aplicarla al resto. Esto es **Opción B** del
  olor A.

##### C — `internal/api/handlers/` plano con 79 ficheros · **media**

- 16 132 LOC, 0 sub-paquetes.
- `interfaces.go` declara **26 interfaces en 397 LOC** — puerto único
  para handlers de admin, iptv, federation, auth, items, image…
- Impacto futuro: cualquier cambio en un servicio toca un fichero que
  declara 25 interfaces no relacionadas. IDE lento en navegación.
- Refactor sugerido: `handlers/admin/`, `handlers/iptv/`,
  `handlers/federation/`, `handlers/me/`, `handlers/items/`. Las
  interfaces se mueven al sub-paquete que las consume (consumer-side
  ortodoxo en Go).

##### D — `library` vs `scanner`: frontera artificial · **baja**

- `scanner` (3 ficheros, 1 689 LOC) sólo lo importa `library` y
  `cmd/hubplay` (que lo pasa al library service). Misma cosa lógica.
- Refactor: promover `scanner` a sub-paquete de `library` o fusionar.
- A confirmar en Fase 4: si el split aporta separación de tests, se
  mantiene; si no, se fusiona.

##### E — `internal/iptv/` con 32 ficheros · **media, defendible**

- 7 776 LOC. Cubre 6-7 sub-features (M3U, EPG, scheduler, transmux,
  proxy, prober, logo cache, channel order overlay, favorites, watch
  history).
- Está al límite. **A confirmar en Fase 5** si las sub-features están
  ya acopladas o pueden separarse (`iptv/transmux/`, `iptv/proxy/`,
  `iptv/scheduling/`).

### 1.2 Dependencias entre paquetes

Grafo construido con grep sobre todo `internal/*/*.go` (excluyendo
`_test.go`):

```
domain         → ∅           event        → ∅
clock          → ∅           logging      → ∅
observability  → ∅           probe        → ∅
blurhash       → ∅           sysmetrics   → ∅

config         → logging
user           → db, domain
setup          → config
retention      → config
provider       → db
stream         → config, db, domain, event
scanner        → db, event, imaging, probe, provider
imaging        → blurhash
iptv           → clock, db, event, imaging
library        → db, domain, event, imaging, probe, provider, scanner
federation     → clock, domain, event
auth           → api/apperror, clock, config, db, domain, event
db             → domain, federation   (⚠ inversión, olor B)
api            → 19 paquetes internos (composition root HTTP)
cmd/hubplay    → 19 paquetes internos (composition root del proceso)
```

#### Sano

- **Cero ciclos directos** (verificado con grep cruzado).
- Fan-out vertical, no horizontal. Pocos cruces entre features
  (`library ↔ scanner` es el único, y es legítimo).
- `api/apperror` como cut-set documentado.

#### Olores

##### F — Repetición del olor B en términos de grafo · **alta**

`db → federation` es también un olor de dependencias, no sólo de
organización física.

##### G — `Dependencies` (35+ campos) + `runtime` (14 campos) + `main.run` (645 LOC) · **media-alta**

- `internal/api/router.go:33-90` declara struct gigante que `main` pasa
  al router.
- `cmd/hubplay/main.go:530` define `runtime` para shutdown, comentario
  que admite tácitamente el síntoma como solución:
  > Adding a new background service is now a one-line struct-field
  > append plus a Stop call inside waitForShutdown — instead of editing
  > the positional argument list…
- Olor real: no existen módulos compuestos por feature. `library` no
  expone `library.New(deps) *Module` que devuelva
  `(service, scheduler, refresher, fingerprinter, watcher, shutdown())`.
  `main.run` cablea 6 constructores per-feature.
- Principios violados: **SRP** de `main.run` (orquesta 7 fases con
  responsabilidades muy distintas); **OCP** parcial (cada nueva feature
  toca `main.run`, `Dependencies`, `runtime`).
- Refactor sugerido (incremental):
  1. Cada paquete de feature expone `New(ctx, deps) (*Module, error)`
     donde `*Module` agrupa servicio + workers + cleanup.
  2. `main.run` se reduce a `iptv.New(...)`, `library.New(...)`, etc.,
     y mete `Module.Stop` en un `[]func() error` único.
  3. `runtime` desaparece; shutdown es un loop LIFO sobre el slice.
- Ejemplo concreto antes/después en la conversación que originó esta
  fase. Sketch:
  ```go
  // Antes (main.go:184-218):
  imageRefresher := library.NewImageRefresher(repos.Items, ...)
  imageRefreshScheduler := library.NewImageRefreshScheduler(repos.Libraries, imageRefresher, logger)
  imageRefreshScheduler.Start(ctx)
  // + 4 sub-features más

  // Después:
  libMod, err := library.New(ctx, library.Deps{Repos: repos, ...})
  if err != nil { return fmt.Errorf("library: %w", err) }
  shutdown = append(shutdown, libMod.Stop)
  ```

##### H — `Dependencies` lleva tipos `*db.X` concretos · **media**

- 25+ campos son `*db.ItemRepository`, `*db.MediaStreamRepository`, …
- Cada handler los re-estrecha con interfaces locales (consumer-side,
  bien). El contrato queda **doblemente expresado** (concreto en
  composition root, interface en handler).
- Refactor: que `Dependencies` lleve las mismas interfaces que los
  handlers, o — más simple — eliminar `Dependencies` como God-Struct y
  pasar dependencias agrupadas por sub-router (`mountIPTV(...)`,
  `mountAdmin(...)`).

##### I — 26 interfaces en `handlers/interfaces.go` · **a confirmar Fase 3**

- Si todas se usan en tests y sólo en tests → patrón "fakes para
  handlers", sano.
- Si alguna se usa sólo para "tener un puerto" sin tests que la
  implementen distinto del runtime → **abstracción innecesaria** estilo
  Java.

### 1.3 Flujo general del sistema

#### Bootstrap

`cmd/hubplay/main.go::run` ejecuta 7 fases en 645 LOC:

1. **Foundation**: `config.Load` + logger + `clock.New` + `Preflight`.
2. **Database**: restore pendiente (SQLite-only) → `db.Open` →
   migraciones goose → `db.NewRepositories(driver)`.
3. **Infrastructure**: `event.NewBus` + `observability.NewMetrics`.
4. **Core services**: bootstrap signing keys → keystore → auth →
   user → provider manager → scanner → library service →
   schedulers (scan, image refresh, segment detection, fingerprint,
   fs watcher) → stream manager (con runtime overrides de
   `app_settings`) → IPTV stack (service, proxy, transmux, logo
   cache, scheduler, prober) → setup service.
5. **HTTP**: federation init (fail-soft → nil si falla) → retention
   runner → host metrics sampler → router.
6. **Start**: `server.ListenAndServe` en goroutine.
7. **Shutdown**: `waitForShutdown` con orden explícito (schedulers →
   server → stream/iptv → library scan drain → DB optimize → DB
   close), bajo `shutdownCtx 30s`.

#### Comunicación

- **Event bus in-proc** (`internal/event/bus.go`): `Subscribe` devuelve
  `unsubscribe()` que **deben llamar** (contrato ADR-008). Publishers:
  scanner, auth, iptv, federation. Subscribers: SegmentDetector,
  SegmentFingerprinter, SSE handlers, audit listeners. **[PENDIENTE-F8]**
  verificar que todos los unsub están atados a lifecycle real.
- **Background workers** (9 schedulers/workers): `Start(ctx)` + `Stop`,
  ctx raíz. **[PENDIENTE-F4,F5]** confirmar qué workers respetan el ctx
  para cancelación in-flight y cuáles sólo para drain.
- **HTTP**: chi router, 219 rutas, middleware stack (security headers →
  CORS → JWT → CSRF → rate limit). `WriteTimeout: 0` por streaming;
  parcialmente mitigado por `ReadTimeout 15s` e `IdleTimeout 60s`.
  **[PENDIENTE-F3]** confirmar si endpoints no-streaming tienen
  protección extra.
- **Federation**: peer JWT (EdDSA) sobre mismo middleware shape que
  auth (HS256). Comparte `stream.Manager` para streaming entre peers
  (ADR-012).
- **`Dependencies.Federation == nil`** cuando init falla. 21 ficheros de
  `handlers/federation_*.go` deben nil-check. **[PENDIENTE-F3,F7]**
  confirmar.

#### Storage

- SQLite (modernc.org pure-Go) por defecto; Postgres opcional, Pattern
  A dual-dialect repo a repo (no terminado — `repos.go` admite
  "until Sesión E finishes refactoring every repo, only the ones
  already migrated honour the driver").
- `sqlc` + `goose` per ADR-001 y ADR-013.
- `app_settings` overlay sobre YAML para configs runtime (ADR-010).

#### Riesgos del flujo

- **`main.run` con 645 LOC** = síntoma G.
- **`stream.Manager` con 5 setters post-construcción**
  (`SetMetrics`, `SetEventBus`, `SetForceDirectPlayLookup`, …).
  Olor a Builder Pattern accidental con mutabilidad runtime.
  **[PENDIENTE-F6]** confirmar nil-guards y semánticas de mutación.
- **`Federation = nil` fail-soft** → 21 handlers federation deben
  nil-check. **[PENDIENTE-F3,F7]**.
- **Subscribe del segment detector con `defer unsub`** en `main.run`:
  acoplamiento del lifecycle de subscribers al lifecycle de `run`, no
  al `ctx`. En la práctica funciona porque `run` retorna sólo en
  shutdown, pero documenta un patrón frágil. **[PENDIENTE-F8]**.
- **`Dependencies.Database *sql.DB` raw**: la API tiene acceso al
  `*sql.DB` además de a los repos. *Backdoor* — alguien puede saltarse
  los repos. **[PENDIENTE-F3]** revisar quién lo usa y por qué.

### 1.4 Observación transversal: comentarios

- **Casi todos los comentarios del código están en inglés**; docs
  (`CLAUDE.md`, `docs/memory/*`) en español. Incoherente respecto a la
  pauta operativa ("comentarios técnicos en español, concisos,
  explican el porqué no el qué").
- **Pauta para fases siguientes**: cada fase marca los comentarios que
  deben reescribirse. Comentarios nuevos se escriben directamente en
  español. No se hace big-bang translate del repo entero.
- Ejemplo (`cmd/hubplay/main.go:70-73`):
  - Mal (actual, inglés, verbose):
    > Preflight: validate external binaries and filesystem permissions
    > before any service is built. Catching these here means "ffmpeg
    > not installed" shows up as a clear boot error instead of an
    > opaque 500 during the first user's stream attempt.
  - Bien (español, conciso, porqué):
    > Preflight antes de cualquier servicio: convierte "ffmpeg no
    > instalado" en error de arranque, no en 500 opaco al primer stream.

### 1.5 Resumen de severidades (Fase 1)

| # | Problema | Severidad | Fase de fix |
|---|----------|-----------|-------------|
| A | `internal/db/` god-package | Alta | F2 |
| B | `db → federation` inversión | Alta | F2 + decisión global |
| C | `api/handlers/` plano, 26 interfaces en un fichero | Media | F3 |
| D | `library` y `scanner` con frontera artificial | Baja | F4 |
| E | `iptv` con 32 ficheros — al límite | Media | F5 |
| G | `Dependencies` + `runtime` + `main.run` 645 LOC | Media-alta | Tras F4-F7 |
| H | `Dependencies` con tipos `*db.X` concretos | Media | F3 + G |
| I | 26 interfaces en `handlers/interfaces.go` | A confirmar | F3 |
| — | Comentarios en inglés en código vs docs en español | Media | Marcar en cada fase |

---

## Fase 2 · `internal/db/`

> Pendiente. Foco propuesto:
> - Inventario de repos: cuáles son adapter sqlc puros, cuáles tienen
>   lógica de mapping no trivial, cuáles tienen raw SQL holdouts y por
>   qué.
> - Validación del olor A (god-package) con detalle: cohesión
>   intra-paquete, qué se queda y qué se separa.
> - Validación del olor B (`db → federation`) con propuesta concreta.
> - Patrón dual-dialect (Pattern A): cuántos repos lo honran, cuántos
>   ignoran `driver`, deuda de la "Sesión E" incompleta.
> - `*sql.DB` raw expuesto en `Dependencies` — quién lo usa.

---

## Fase 3 · `internal/api/` + handlers

> Pendiente. Foco propuesto:
> - Las 26 interfaces de `handlers/interfaces.go`: ¿cuáles tienen al
>   menos un fake en tests distinto del runtime? ¿Cuáles son
>   abstracción innecesaria?
> - Sub-paquetes propuestos (admin, iptv, federation, me, items).
> - Middleware stack: orden, idempotencia, timeouts por sub-router.
> - 21 handlers `federation_*.go` con `Federation == nil` posible.
> - `Dependencies.Database *sql.DB` — usos.
> - `Dependencies` God-Struct: refactor a sub-routers con sus propias
>   deps.

---

## Fase 4 · `library` + `scanner`

> Pendiente. Foco propuesto:
> - Frontera `library` vs `scanner` (olor D).
> - Lifecycle de workers (`scanScheduler`, `imageRefreshScheduler`,
>   `segmentDetector`, `segmentFingerprinter`, `fsWatcher`): respeto
>   del ctx, drain en shutdown, idempotencia de re-scans.
> - Patrón anti-ciclo (sink pattern) documentado en
>   `conventions.md` — verificar aplicación.

---

## Fase 5 · `internal/iptv/`

> Pendiente. Foco propuesto:
> - Sub-features (M3U, EPG, scheduler, transmux, proxy, prober, logo
>   cache, channel order) — acoplamiento interno (olor E).
> - Concurrencia: `TransmuxManager` compartido entre VOD y federation;
>   sesiones compartidas; cooldown breaker compartido entre proxy y
>   transmux.
> - `ProberWorker` lifecycle.
> - `LogoCache` fail-soft (nil si falla).

---

## Fase 6 · `internal/stream/`

> Pendiente. Foco propuesto:
> - `stream.Manager` con 5 setters post-construcción (olor en F1.3).
> - `MaxReencodeSessions` cap compartido entre VOD y federation
>   (ADR-012).
> - HW accel: detector único al boot (ADR-006).
> - Decisión direct-play / direct-stream / transcode (`decision.go`).

---

## Fase 7 · `auth` + `federation`

> Pendiente. Foco propuesto:
> - Reuse documentado en ADR-012: ¿se mantiene la promesa?
> - `auth/ratelimit.go` vs `federation/ratelimit.go` — 50 LOC
>   duplicados; revisar si la divergencia semántica sigue justificando
>   la copia.
> - JWT EdDSA + HS256 en el mismo `auth/jwt.go` — claims plumbing.
> - Audit writer async de federation: backpressure, drop policy.

---

## Fase 8 · `event` + primitivos sin deps

> Pendiente. Foco propuesto:
> - `event.Bus` política "no recover de handlers que cuelgan"
>   (ADR-008): correctness empírica.
> - Lifecycle de subscribers: cuántos llaman al `unsubscribe()`, cuántos
>   leakean potencialmente.
> - `clock`, `logging`, `observability`, `probe`, `blurhash`,
>   `sysmetrics`: revisar que su API sigue siendo mínima.

---

## Plan de intervención final

> Se redacta al cerrar Fase 8. Sintetiza:
> - Refactors a aplicar (orden, dependencias entre ellos, riesgo).
> - Comentarios a reescribir.
> - ADRs nuevos a abrir.
> - Tests a añadir antes de tocar.
> - Posibles ADRs a superseder.
