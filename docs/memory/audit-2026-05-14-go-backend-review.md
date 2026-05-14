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

**Cómo leer este documento**:

- Si vienes a **arrancar la intervención** → salta al [Plan de
  intervención final](#plan-de-intervención-final).
- Si vienes a **entender el estado del backend** → lee el
  [Resumen ejecutivo](#resumen-ejecutivo).
- Si vienes a **resolver un olor concreto** → ve a la fase
  correspondiente (cada olor tiene letra única referenciada
  desde el plan).

### Secciones

| § | Sección | Propósito |
|---|---|---|
| 1 | [Resumen ejecutivo](#resumen-ejecutivo) | Veredicto, top hallazgos, modelo a replicar |
| 2 | [Plan de intervención final](#plan-de-intervención-final) | 9 iteraciones ordenadas; **entra aquí para empezar** |
| 3.1-3.16 | Fases detalladas (referencia) | Hallazgos crudos por paquete / dimensión |
| 4 | [Cierre y protocolo de sesiones](#cierre) | Cómo trabajar el plan entre sesiones |

### Fases auditadas (16 / 16 ✅)

| # | Fase | Foco |
|---|---|---|
| F1 | Panorama | Estructura, grafo de deps, flujo |
| F2 | `internal/db/` | God-package, 31 repos, dual-dialect |
| F3 | `internal/api/` + handlers | God-handlers, middleware, interfaces |
| F4 | `library` + `scanner` | Workers, lifecycle, event bus |
| F5 | `internal/iptv/` | 11 sub-features, transmux, proxy |
| F6 | `internal/stream/` | Manager, transcoder, decision tree |
| F7 | `auth` + `federation` | JWT, keystore, ratelimit, audit |
| F8 | `event` + primitivos | Bus, clock, logging, observability |
| F9 | `internal/imaging/` | SSRF, atomic writes, blurhash |
| F10 | middleware + csrf + sec + apperror | Stack HTTP base |
| F11 | `config` + `setup` + `retention` | Boot + wizard + sweep |
| F12 | Migraciones | 43 SQLite + 43 Postgres |
| F13 | Transversales | Error wrapping, context, deadlocks, naming |
| F14 | Calidad a nivel función | God-functions, struct params, boilerplate |
| F15 | Test suite | 137 ficheros, fragility, mocks |
| F16 | Handlers medianos | Path traversal, paginación, SSE drops |

### Olores catalogados

**110+ olores** identificados, cada uno con letra única (A..LLLL,
F14-X, F15-X, F16-X). Resumen por severidad:

- **2 CVE-class** (FFF SSRF + F16-1 path traversal).
- **6 altos** estructurales (A+M, B+J, CC, P, W, F14-2-a).
- **3 media-alta** (G, Q, RRR-mig, F14-2-b).
- **~25 medios**.
- **~75 bajos**.

### Documentos hermanos

- **`intervention-2026-05-XX.md`** — log de trabajo iteración por
  iteración; se crea al arrancar la intervención. Este audit
  permanece **inmutable**.
- `architecture-decisions.md` — recibirá 9 ADRs nuevos (015-023).
- `conventions.md` — recibirá 5 actualizaciones (Pattern A/B,
  consumer-side interfaces, naming `is*Safe*/Blocked*`, comentarios
  en español, sub-loggers con `.With()`).

---

## Resumen ejecutivo

> Auditoría cerrada al cierre de **Fase 16** (cobertura completa del
> brief original ~98 %). Plan de intervención consolidado en
> [Plan de intervención final](#plan-de-intervención-final).

### Veredicto global

HubPlay es un backend Go **sano en lo macro** (layout
package-por-dominio, cero ciclos directos, 8 paquetes raíz sin
deps, lifecycle drain bien aplicado en 3 paquetes modelo) con
**deuda focalizada en 3-4 god-objects** que han crecido
verticalmente sin que el patrón cambie: `internal/db/`,
`iptv.Service`, `ItemHandler`, `scanner.go`.

Si el proyecto fuese Java disfrazado de Go (DI containers,
`service/`, `repository/`, `interfaces/`), esto sería una espiral.
**No lo es**: las primitivas (`event.Bus`, `clock`, `decision.go`,
`singleflight`, `federation.Auditor`) están bien diseñadas y son
modelos a citar. Los olores son **estructurales pero ortogonales** —
se atacan por iteraciones independientes, sin requerir
re-arquitectura.

### Hallazgos críticos (CVE-class)

🚨 **FFF — SSRF redirect bypass en `imaging.SafeGet`** (F9). Cliente
HTTP sin `CheckRedirect` sigue redirects sin re-validar IP. Vector
real: atacante con `evilhost.com` redirige a `169.254.169.254` (AWS
metadata) o IP RFC1918 interna. **Fix urgente, ~20 LOC, modelo en
`iptv/proxy.go`.**

🚨 **F16-1 — Path traversal en `people.isUnderImageDir`** (F16).
`filepath.Clean + Abs` sin `EvalSymlinks` permite leer archivos
arbitrarios si un symlink apunta fuera de `imageDir`. Mitigado por
trust model (admin-only writes a DB) pero rompe el contrato
explícito que la función promete. **Fix ~10 LOC.**

### Hallazgos altos (resumen 1-línea)

| # | Olor | Fase |
|---|------|------|
| **FFF** | **SSRF redirect bypass en `imaging.SafeGet` (CVE-class)** | **F9** |
| **F16-1** | **Path traversal en `people.isUnderImageDir` (CVE-class)** | **F16** |
| A+M | `internal/db/` god-package: 13 KLOC, 31 repos, 80 tipos consumidos por 55 ficheros externos | F1, F2 |
| B+J | `db → federation` invierte capa; `federation_repository.go` son 6 repos disfrazados de uno | F1, F2 |
| CC | `iptv.Service` 45 métodos en 11 sub-features con split sólo cosmético por fichero | F5 |
| P | `ItemHandler` 1 186 LOC, 13 deps, 4 responsabilidades | F3 |
| W | `scanner.go` 1 270 LOC en un fichero (4 responsabilidades) | F4 |
| G | `Dependencies` (35+) + `runtime` (14) + `main.run` (645 LOC) sin módulos compuestos | F1, F3 |

### Bugs latentes y violaciones de contrato confirmados

| # | Bug | Fase | Coste fix |
|---|-----|------|-----------|
| **RRR-mig** | **15 migraciones tienen `-- +goose Down` violando política up-only** | **F12** | **mecánico** |
| RR | `loginRateLimiter` goroutine sin Stop (goroutine leak en tests integrados) | F7 | ~10 LOC |
| Y | `SegmentDetector`/`Fingerprinter` no drenan goroutines spawneadas | F4 | ~40 LOC |
| DD | `iptv.Service.RefreshM3U` detached goroutines sin drain | F5 | ~50 LOC |
| **GGGG** | **Handlers `iptv_admin.go` detached goroutines sin tracking** | **F13** | **incluido con DD** |
| Q | `WriteTimeout: 0` global afecta a las 219 rutas (sólo ~10 son streaming) | F3 | middleware |
| HHH | `pathmap.Read` no valida que el path resulte bajo la raíz | F9 | ~5 LOC |
| **F15-1** | **`time.Sleep` arbitrarios en 19 ficheros de test (CI flakes)** | **F15** | **test seams** |
| **F16-2..F16-8** | **Paginación sin validar + filtración de errores + `KillSession` sin audit + SSE drops silenciosos** | **F16** | **handlers medianos** |

### Patrones modelo del proyecto (a replicar)

- **Lifecycle drain con `bgWG`**: `library.Service`, `stream.Manager`,
  `iptv.TransmuxManager`, `federation.Manager`.
- **Lógica pura aislada de I/O**: `stream/decision.go`.
- **Sink pattern anti-cycle**: `auth.keyResolver` función, `api/apperror`
  cut-set, `iptv.proberRunner`.
- **Async writer con drop policy**: `federation.Auditor`.
- **Locking granular justificado**: `federation.Manager` con dos
  mutexes.
- **`singleflight.Group`** para colapsar races: `stream.Manager.StartSession`.
- **Split por sub-fichero con receiver compartido**:
  `federation/manager_*.go` — modelo para CC.

### Camino propuesto

10 iteraciones (0..9), **~15.5-16.5 días de trabajo focalizado**,
cada iteración deja el repo verde. La 9 es verificación empírica
post-merge, no implementación.

0. Pre-trabajo: 6 ADRs (incluye **ADR-015 dominio en feature**,
   **ADR-019 SSRF CheckRedirect**, **ADR-020 up-only**) + 2 updates
   de `conventions.md`.
1. **🚨 Fixes URGENTES de seguridad + correctness**: **FFF (SSRF) +
   F16-1 (path traversal)**, RRR-mig (up-only), RR (ratelimit leak),
   Y/DD/GGGG (drain), AAA/EE (comentarios + rename), HHH (pathmap),
   F16-6 (filtración de errores), F16-7 (audit `KillSession`).
2. **Sub-paquetes de db**: B+J+K+T+L (federation a su feature,
   ActivityLogRepo, split home).
3. **Migración Opción B incremental** por feature (iptv → auth →
   library → cleanup `db/`).
4. **Split de god-handlers/services** (P, Z, QQ).
5. **Refactor estructural iptv** (CC).
6. **Composition root**: módulos compuestos (G+H+V), `WithWriteDeadline`
   (Q), `Transcoder` stateless (LL).
7. **Cosmética y schema**: D, X, W, BB, UUU-mig, TTT-mig, VVV-mig,
   SSS-mig, WWW-trans, XXX-trans, PPP, NNN, OOO, JJJ/III/KKK/LLL.
8. **Polish de calidad** (F14): struct params (~30 firmas),
   helpers `respondData`/`requireParam` (~125 sites), tests sin
   `Sleep`, magic numbers a constantes, dead-code wipe. ~600 LOC
   eliminadas sin perder funcionalidad.

### Riesgos y lo que NO se hace

- Migración Opción B se hace incremental por feature, no big-bang.
- `auth.ratelimit` y `federation.ratelimit` NO se fusionan (ADR-012
  vigente).
- Comentarios NO se traducen en un commit gigante — pauta incremental.
- Wiring manual NO se cambia por DI container — los módulos
  compuestos cubren el caso sin perder explicitness.

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

Cerrada · 2026-05-14.

### 2.1 Inventario

- **31 repos**, 30 test files (~97 % cobertura de ficheros).
- **8 ficheros "infra"**: `database.go`, `dialect.go`, `migrator.go`,
  `postgres.go`, `repos.go`, `restore.go`, `scan_helpers.go`,
  `sqlite.go`. Cohesión correcta: infra de conexión y migración.
- **80+ tipos exportados** (modelos públicos: `db.Item`, `db.Channel`,
  `db.User`, `db.Image`, …) consumidos por **55 ficheros fuera del
  paquete** (handlers + servicios). Cuantifica el olor A de Fase 1.
- **Dialectos**:
  - 26 repos hacen *Pattern A* (importan `sqlc` + `sqlc_pg`, branch
    `IsPostgres(driver)` por método).
  - 19 repos hacen *Pattern B* (raw SQL con `RewritePlaceholders` al
    construir).
  - 5 repos son **raw puros sin sqlc**: `home`, `settings`,
    `user_preferences`, `library_channel_order`, `user_channel_order`.
    Cada uno con comentario que documenta el porqué (cross-cutting
    joins, bug sqlc 1.31.1 `ORDER BY ASC` per ADR-013, o queries
    triviales).
- **Cabezas grandes** (LOC y métodos exportados):
  - `federation_repository.go` — 1 474 LOC, **31 métodos**.
  - `item_repository.go` — 965 LOC, 12 métodos.
  - `user_data_repository.go` — 794 LOC, 12 métodos.
  - `channel_repository.go` — 760 LOC, 15 métodos.
  - `home_repository.go` — 671 LOC, 7 raw queries.
  - `library_repository.go` — 660 LOC, 12 métodos.
  - `user_repository.go` — 643 LOC, 18 métodos.

### 2.2 Hallazgos

#### J — `federation_repository.go` son 5-6 repos disfrazados de uno · **alta** (sub-caso de A+B)

- **1 474 LOC**, 2× el siguiente repo (`user_repository.go` 643 LOC).
- Mezcla seis responsabilidades en un fichero:
  1. Identity (keypair Ed25519 del servidor).
  2. Invites (códigos `hp-invite-…`).
  3. Peers (servers linkeados).
  4. Audit log (cola async).
  5. Item cache (FTS dual-dialect, raw).
  6. Rate limit state (declarado en ADR-012 pero no usado).
- Principio violado: **SRP del fichero** (los demás repos sí son
  single-responsibility).
- Impacto: cualquier cambio en una de las seis responsabilidades
  recompila + arrastra al test de las otras cinco. Es el primer
  candidato natural para validar la **Opción B del olor A** (mover
  tipos + repo a la feature). Concretamente: trasladar este fichero
  a `internal/federation/storage/` resuelve **olor B simultáneamente**
  porque deja de existir `db → federation`.
- Refactor sugerido:
  ```
  internal/federation/storage/
    identity.go    (5 métodos)
    invite.go      (~5 métodos)
    peer.go        (~6 métodos)
    audit.go       (~4 métodos + writer async)
    item_cache.go  (~6 métodos, FTS dual-dialect)
    ratelimit.go   (~5 métodos, hoy unused)
  ```
  Cada fichero hace lo que dice. La factory (`Repositories` en
  `db/repos.go`) deja de construir `FederationRepository`; en su lugar,
  `federation.New(...)` construye los suyos. Esto reduce
  `db.Repositories` en una entrada y elimina el import inverso.

#### K — `*sql.DB` raw expuesto al HTTP layer · **media** (confirma `[PENDIENTE-F3]`)

Cinco handlers reciben `*sql.DB` directamente vía `Dependencies.Database`:

| Handler | Uso | Veredicto |
|---|---|---|
| `health.go` | `db.Ping()` para `/health` y `/health/db` | **Legítimo**, pero idealmente `db.HealthChecker` |
| `system.go:299,459,600` | `PingContext` + `QueryContext` para audit log raw | **No legítimo** — queries directas a `activity_log` deberían ir por un repo |
| `admin_backup.go:99` | `VACUUM INTO` (SQLite-only) | **Legítimo** — operación meta, no de dominio |
| `admin_db.go:49,278` | `db.Stats()` + `probeVersion` | **Legítimo** — meta |
| `iptv_schedule.go:18` | Sólo comentario, no usa | Limpieza |

- Principio violado en `system.go`: **inversión de dependencias** del
  patrón "handlers consumen interfaces estrechas, no `*sql.DB`".
  Idéntico al patrón consumer-side correcto en los otros 23 handlers.
- Impacto: si mañana un audit log se mueve a otra tabla / a otro
  storage / a un buffer en memoria, `system.go` rompe en runtime y se
  detecta tarde.
- Refactor:
  - Crear `db.ActivityLogRepository` con `List(filter)` típed.
  - Para los otros usos (`Ping`, `Stats`, `VACUUM`, version), exponer
    `db.AdminTools` interface estrecha que **no leakea** `*sql.DB`. El
    `Database *sql.DB` en `Dependencies` desaparece.

#### L — `home_repository.go` es 3 repos disfrazados de uno · **media** (sub-caso de A)

- 671 LOC, **7 raw queries** distintas con joins + aggregations. Por
  comentarios cubre tres rails distintos:
  - `latest` per-library.
  - `trending` server-wide.
  - `live-now` (channel × EPG join).
- Justificación raw (no sqlc): correcta — son one-shots con joins
  dinámicos.
- Refactor sugerido: tres ficheros en el mismo paquete, no tres repos.
  Mantener la técnica raw. Al menos separar el código por sección. Es
  el patrón que ya aplican `image_repository.go` (que mezcla `Image` y
  `PrimaryImageRef`) y `collection_repository.go`.

#### M — 80 tipos `db.X` consumidos por 55 ficheros: dominio leak · **alta** (cuantifica A)

- Los modelos viven en `db/` por convención histórica; cualquier
  cambio en `db.Item`, `db.Channel`, `db.User` requiere actualizar 55
  callers.
- En la dirección **Opción B** (federation ya lo hace), los tipos del
  dominio viven en el paquete de feature (`iptv.Channel`,
  `library.Item`, `auth.User`) y el repo retorna esos tipos. El
  paquete `db/` queda como adapter sqlc + infra.
- Refactor incremental:
  1. Migrar **federation** primero (ya está casi: J resuelve el resto).
  2. Después `iptv` (todos los tipos `db.Channel*`, `db.EPGProgram`,
     `db.IPTVScheduledJob`, `db.LibraryEPGSource`,
     `db.ChannelOverride`, `db.ChannelFavorite`,
     `db.UserChannelOrderEntry`, `db.LibraryChannelOrderEntry`,
     `db.ChannelHealthSummary`, `db.ChannelWatchHistory…` → al menos
     11 tipos).
  3. Después `library` (Item, MediaStream, Image, Chapter,
     EpisodeSegment, ItemValue, Studio, Collection, ExternalID,
     Metadata, Person, ItemPersonCredit → 12 tipos).
  4. `auth` (User, Session, SigningKey, DeviceCode).
- Coste estimado: ~1 día por feature si se hace con `goimports -r` y
  un test suite verde. Beneficio: cada feature owns su contrato; `db`
  ya no es god-package.

#### N — `Pattern A` / `Pattern B` viven en comments, no en código · **baja**

- 26+19 repos repiten dos patrones de copia/pega. Los términos
  "Pattern A", "Pattern B" están en comentarios (ej.
  `federation_repository.go:18`), no en helpers ni docs.
- Refactor: documentar en `docs/memory/conventions.md` con cuándo
  elegir cada uno y un *template* mínimo. Ya hay reglas duras en
  ADR-013, pero falta el patrón nominado.

#### O — `Repositories` struct con 31 campos · **baja**, eco de G

- `db.Repositories` agrupa los 31 repos como factory. Es la
  composition root de la capa de persistencia y por eso es defendible
  (equivalente a `pool` en otros proyectos).
- Pero combinado con G (`Dependencies` con tipos `*db.X` concretos)
  significa que `Dependencies` reexpone `repos.Items`, `repos.Channels`,
  etc. — el handler recibe el mismo handle que vive en `Repositories`.
- Si en Fase 3 decidimos sub-routers con sus propias deps, el
  refactor podría reorganizar `Repositories` por feature
  (`repos.IPTV.*`, `repos.Library.*`, `repos.Auth.*`). Pero eso es
  cosmética — el verdadero olor está en G+H, no aquí.

### 2.3 Confirmaciones de `[PENDIENTE]` de Fase 1

- **`[PENDIENTE-F3]` `*sql.DB` raw en `Dependencies`**: confirmado,
  ver olor K. Cinco handlers, sólo uno (`system.go` audit log queries)
  realmente abusa.
- `[PENDIENTE-F3,F7]` `Federation == nil` nil-checks: no aplica a
  esta fase, se atacará en F3/F7.

### 2.4 Severidades Fase 2

| # | Problema | Severidad | Prerequisito |
|---|----------|-----------|--------------|
| J | `federation_repository.go` (1 474 LOC, 6 responsabilidades) | Alta | Decisión Opción B |
| K | `*sql.DB` raw vía `Dependencies` (queries en `system.go`) | Media | Crear `ActivityLogRepository` |
| L | `home_repository.go` con 3 rails mezclados | Media | Split por fichero |
| M | 80 tipos `db.X` consumidos por 55 ficheros | Alta | Decisión Opción B + migración incremental |
| N | `Pattern A/B` no documentado como helper | Baja | Documentar en `conventions.md` |
| O | `db.Repositories` con 31 campos | Baja | Eco de G |

### 2.5 Decisión arquitectónica pendiente para el plan final

**Opción A (split horizontal)** o **Opción B (mover tipos a la
feature)**. Recomiendo **Opción B**, ya validada de facto por
`federation`. Es el refactor estructural más alto-impacto del
proyecto. Si se confirma, abre nuevo ADR que supersede el modelo
implícito actual ("tipos viven en db/").

Coste-beneficio:
- Coste: ~4 días de refactor mecánico, ~55 ficheros tocados, alto
  riesgo de conflictos con PRs en vuelo si se hace big-bang.
- Beneficio: rompe acoplamiento estructural más alto del proyecto,
  elimina inversión de capa (olor B), elimina god-package (olor A),
  cierra la inconsistencia con federation.

---

## Fase 3 · `internal/api/` + handlers

Cerrada · 2026-05-14.

### 3.1 Inventario

- **79 ficheros** en `handlers/`, 16 132 LOC, 0 sub-paquetes.
- `internal/api/`: 4 ficheros (`router.go`, `middleware.go`, `csrf.go`,
  `security_headers.go`) + sub-paquete `apperror/`.
- **219 rutas** registradas, **25 `r.Route`/`r.Group`** nested.
- **26 interfaces** en `handlers/interfaces.go` (397 LOC).
- Handlers top por tamaño:
  - `items.go` — 1 186 LOC, **`ItemHandler` único** con 13 deps + 12
    helpers `attach*` privados.
  - `auth.go` — 928 LOC, `AuthHandler` con 13 endpoints.
  - `me_home.go` — 886 LOC.
  - `system.go` — 755 LOC, sólo 3 métodos pero queries SQL inline.
  - `stream.go` — 687 LOC.
  - `image.go` — 647 LOC.
  - `iptv_channels.go` — 625 LOC.
  - `library.go` — 623 LOC.
  - `admin_db.go` — 533 LOC.

### 3.2 Hallazgos

#### P — `ItemHandler` es un god-handler · **alta**

- `items.go:type ItemHandler struct` agrupa **13 dependencies**
  inyectadas (`lib`, `images`, `metadata`, `userData`, `users`,
  `chapters`, `segments`, `externalIDs`, `people`, `collections`,
  `providers`, …) + **3 atributos privados** (`trickplayDir`,
  `trickplayLocks sync.Map`, `trickplayBG sync.WaitGroup`).
- Mezcla 4 responsabilidades en un único struct:
  1. Item detail (`Get` + 8 `attach*` helpers que componen la
     respuesta consultando 6 repos distintos).
  2. Recommendations (TMDb passthrough).
  3. Trickplay (manifest + sprite + ensure + wait, con `sync.Map`
     para deduplicar generación).
  4. Children + Search (browsing).
- Principio violado: **SRP**. El handler "responde sobre un item" se
  convirtió en "responde cualquier cosa que tenga `itemId`".
- Impacto futuro: cada cambio cruza un fichero compartido por 4
  equipos lógicos; los tests del trickplay tienen que setear todas
  las deps de item-detail aunque no las use; las interfaces
  consumer-side se inflan en `handlers/interfaces.go`.
- Refactor sugerido:
  - `ItemDetailHandler` (`Get`, `Children` + 8 helpers `attach*`,
    deps: lib, images, metadata, userData, users, chapters,
    segments, externalIDs, people, collections).
  - `RecommendationsHandler` (deps: providers, externalIDs).
  - `TrickplayHandler` (deps: items para path lookup, trickplayDir,
    locks, BG). Aquí cae también `ItemHandler.WaitTrickplayInflight`.
  - `SearchHandler` (deps: lib).
- Beneficio directo: reduce 5+ interfaces consumer-side a 2-3 por
  handler, y cada constructor toma 3-4 deps en vez de 13.

#### Q — `WriteTimeout: 0` global sin sub-router protegido · **media-alta** (confirma `[PENDIENTE-F3]`)

- `cmd/hubplay/main.go:489` setea `WriteTimeout: 0` "*Streaming
  endpoints need unlimited write time*". Cierto, pero **se aplica al
  servidor entero**.
- **Las 219 rutas heredan el timeout = 0**. Sólo ~10 son streaming
  (`/stream/{itemId}/...`, `/transmux/...`, `/iptv/channel/{id}/...`,
  `/peer/stream/...`, `/events`, `/me/events`). Las otras ~210 no lo
  necesitan.
- Principio violado: **principle of least privilege** sobre los
  timeouts del HTTP server.
- Impacto: un cliente que abre `/api/v1/items?limit=50` y consume el
  body a 1 byte/segundo puede mantener una goroutine ocupada
  indefinidamente. No es un DoS práctico contra un servidor self-
  hosted en LAN, pero sí contra un deployment Tailscale / Cloudflare
  Tunnel donde el lado upstream es público.
- Refactor sugerido:
  - Opción 1: middleware `WithWriteDeadline(30 * time.Second)` aplicado
    al sub-router `/api/v1/*` **excepto** sub-trees streaming. chi
    permite encadenar middleware por `r.With(...).Group(...)`.
  - Opción 2: `http.TimeoutHandler` envolviendo el sub-router
    no-streaming. Más simple pero retorna un mensaje fijo en caso de
    timeout (no se ajusta al envelope `apperror`).
  - Opción 1 es más limpia.

#### R — Interfaces sin fake en tests: 6 de 26 · **baja, requiere convención**

- `IPTVTransmuxer`, `CollectionRepoForItems`,
  `EpisodeSegmentRepository`, `ImageRefreshService`,
  `EventBusSubscriber`, `EventBusPublisher` no se mockean en ningún
  `_test.go`.
- **No son abstracciones innecesarias**: cada una se usa en al menos
  un handler real, y abstraer el bus / el transmuxer evita acoplar
  el handler al paquete concreto.
- Lo que **sí** es olor es la inconsistencia: la convención del
  proyecto es "interface consumer-side **por handler**", y aquí
  conviven dos prácticas (interface por puerto del dominio +
  interface por handler) en el mismo fichero.
- Refactor: documentar en `conventions.md` que la regla es "una
  interface por consumer; si dos handlers necesitan el mismo método,
  cada uno declara su propia interface, no se comparte." Cierra
  ambigüedad sin tocar código existente.

#### S — Federation fail-soft vía gating en router · **resuelve `[PENDIENTE-F3,F7]`**

- 5 ficheros `federation_*.go` (48 usos del manager) **no nil-check**
  internamente.
- Pero `router.go:270, 441, 493` envuelve cada montaje con
  `if deps.Federation != nil`. **El gating está en registration
  time** — si federation init falla, las rutas no existen y los
  handlers no se construyen.
- Patrón **seguro**, idiomático y documentado por simetría
  (`if deps.Database != nil` en línea 170, igual).
- Resuelto: no es olor.

#### T — `system.go` con queries SQL crudas inline · **media** (consecuencia de K en F2)

- 755 LOC, 3 métodos exportados (`Stats`, `StreamActivity`,
  `TopItems`), pero **dos de ellos ejecutan `db.QueryContext`** con
  SELECTs raw sobre `activity_log`.
- Es la encarnación del olor K (Fase 2): el HTTP layer hace SQL.
- Refactor recomendado (dependiente de F2):
  1. Crear `db.ActivityLogRepository` con `ListRecent(filter)` y
     `Top(filter)` raw (justificación raw correcta: cross-cutting con
     joins + cutoff).
  2. `system.go` consume el repo; el campo `*sql.DB` desaparece de
     `SystemHandler`.
  3. `Dependencies.Database` deja de ser leído por `system.go`. Queda
     sólo en `health.go` (Ping legítimo), `admin_backup.go` (VACUUM
     INTO), `admin_db.go` (Stats + version probe).
- Tras esto se puede sustituir `Dependencies.Database *sql.DB` por
  3 interfaces estrechas (`HealthChecker`, `BackupOperator`,
  `PoolStatsReporter`). Cierra olor K.

#### U — Middleware stack bien ordenado · **sano**

Verificado `internal/api/router.go:110-138`. Orden:

```
RealIP → RequestID → RequestLogger → Recoverer →
SecurityHeaders → Metrics → CORS → CSRFProtect
```

Razonamientos correctos y documentados en código:
- `SecurityHeaders` después de `Recoverer` para que un 500 todavía
  los lleve.
- Antes de `CORS` para que preflight también los lleve.
- `Metrics` después de `Recoverer` para contar panics como 500.

Auth y rate-limit se aplican **por sub-router** con `r.Use` dentro de
`r.Group`. Es lo correcto (endpoints públicos como `/health`,
`/auth/login`, `/setup/*` quedan sin auth pero con todos los demás
middlewares). No es olor.

#### V — `Config` leído directamente por `router.go` · **media, eco de G/H**

- `router.go` accede a `deps.Config.Database.Path` (l.157),
  `deps.Config.Database.Driver` (l.168, 392, 618), `deps.Config.Auth`
  (l.155), `deps.Config.Observability.MetricsEnabled` (l.142),
  `deps.Config.Observability.MetricsPath` (l.143).
- Significa que el wiring depende del **shape exacto** de
  `config.Config`, además del `Dependencies` shape.
- Cualquier refactor de `Dependencies` tendría que decidir si los
  campos de config relevantes se promueven (`Dependencies.DBDriver
  string`) o se siguen leyendo desde `deps.Config.*`. Ambas son
  defendibles; recomiendo **promover sólo los campos que el HTTP
  layer realmente usa** para que `Dependencies` no necesite el
  `*config.Config` entero.
- No introduce ciclo ni acoplamiento mayor — es coste de claridad,
  no de correctness.

### 3.3 Confirmación de `[PENDIENTE]`

- `[PENDIENTE-F3]` `WriteTimeout: 0` sin sub-router protegido →
  confirmado, olor Q.
- `[PENDIENTE-F3,F7]` `Federation == nil` nil-checks → no es olor:
  resuelto por gating del router (hallazgo S).
- `[PENDIENTE-F3]` 26 interfaces → ninguna es "abstracción innecesaria"
  estricta; la inconsistencia de convención sí lo es (olor R). El
  problema verdadero es **upstream**: `ItemHandler` inflado (P)
  arrastra 6 interfaces granulares que sólo se justifican porque un
  único handler las necesita todas.

### 3.4 Severidades Fase 3

| # | Problema | Severidad | Prerequisito |
|---|----------|-----------|--------------|
| P | `ItemHandler` god-handler (1 186 LOC, 13 deps, 4 responsabilidades) | Alta | Split en 4 handlers |
| Q | `WriteTimeout: 0` global sin sub-router protegido | Media-alta | Middleware `WithWriteDeadline` en sub-router no-streaming |
| R | Inconsistencia "interface por puerto" vs "por consumer" | Baja | Documentar regla en `conventions.md` |
| T | `system.go` queries raw inline (consecuencia de K) | Media | `db.ActivityLogRepository` |
| V | `router.go` lee `deps.Config.*` directo | Media | Eco de G/H, atacar junto |
| — | Sub-paquetes `handlers/admin/`, `/iptv/`, etc. (C de Fase 1) | Media | Refactor cosmético, hacer junto al de P |

### 3.5 Notas operativas

- **Convención de tests**: 20 de 26 interfaces tienen al menos un
  fake. El patrón es sano. No tocar.
- **Cabeza del refactor de C (Fase 1)**: el split de `ItemHandler` (P)
  arrastra naturalmente la decisión de sub-paquetes —
  `handlers/items/detail.go`, `handlers/items/trickplay.go`, etc. Es
  el mejor punto de entrada para validar el patrón sin big-bang.

---

## Fase 4 · `library` + `scanner`

Cerrada · 2026-05-14.

### 4.1 Inventario

**`internal/library/` (9 ficheros, 2 714 LOC):**

| Fichero | LOC | Métodos | Notas |
|---|---:|---:|---|
| `service.go` | 592 | 27 | `Service`: CRUD + ACL + scan + item queries (6 responsabilidades) |
| `watcher.go` | 429 | 5 | `FSWatcher`: fsnotify + debounce + reconcile |
| `imagerefresh.go` | 345 | 2 | `ImageRefresher` (puro) + `ImageRefreshScheduler` |
| `segment_fingerprinter.go` | 293 | 2 | Subscriber bus + chromaprint runner |
| `fingerprint.go` | 292 | 2 | Wrapper `fpcalc` + disk cache |
| `segment_detector.go` | 288 | 2 | Subscriber bus + chapter-based markers |
| `segment_matcher.go` | 281 | 0 | Helpers puros |
| `contentrating.go` | 103 | 0 | Tabla + ordinal de ratings |
| `scheduler.go` | 91 | 2 | Periódico simple |

**`internal/scanner/` (3 ficheros, 1 689 LOC):**

| Fichero | LOC | Notas |
|---|---:|---|
| `scanner.go` | **1 270** | `Scanner` con 14 deps + 10 helpers privados |
| `show_hierarchy.go` | 216 | `showCache` puro |
| `show_parser.go` | 203 | parser de paths puro |

### 4.2 Hallazgos

#### W — `scanner.go` con 1 270 LOC en un único fichero · **media-alta**

- Es la cabeza más grande del repo junto a `items.go` (Fase 3).
- `Scanner` recibe **14 repos** (items, streams, metadata, externalIDs,
  images, chapters, people, itemValues, studios, collections,
  providers, prober, bus, pathmap). Cuantifica el olor M (Fase 2) en
  un sólo struct.
- `ScanLibrary` es **secuencial** por diseño (I/O + writes SQLite
  serializadas). Bien — paralelizarlo con un worker pool añadiría
  contención de escritura SQLite por nada.
- Pero el fichero mezcla **4 responsabilidades**:
  1. Walk + change detection (`ScanLibrary`, `iterateLibraryItems`,
     `walkPath`).
  2. Item create/update (`processFile`, `createItem`, `updateItem`).
  3. Enrichment (`enrichIfMissing`, `enrichMetadata`).
  4. Image ingestion (`fetchAndStoreImages`).
- Principio violado: **SRP** del fichero (no del struct: el struct
  legítimamente orquesta todo el pipeline).
- Refactor sugerido (no cambia API):
  ```
  internal/scanner/
    scanner.go        (struct + ScanLibrary + walk, ~400 LOC)
    enrich.go         (enrichIfMissing, enrichMetadata, ~350 LOC)
    persist.go        (createItem, updateItem, ~350 LOC)
    images.go         (fetchAndStoreImages, ~150 LOC)
    show_hierarchy.go (sin cambios)
    show_parser.go    (sin cambios)
  ```
- Beneficio: cada fichero tiene un responsable claro; los tests
  pueden mockear sólo las deps que el sub-fichero necesita (hoy el
  test del scanner construye un Scanner con 14 deps incluso para
  testar enrich).

#### X — Frontera `library` vs `scanner` artificial · **baja** (confirma D de Fase 1)

- `library.Service` lleva `*scanner.Scanner` como dep (línea 100 del
  struct).
- `scanner.Scanner` usa **los mismos 10 repos** que `library.Service`
  (items, streams, metadata, externalIDs, images, chapters, people,
  itemValues, studios, collections).
- El split no aporta test isolation — el scanner se construye con 14
  deps reales en cualquier test integrado.
- Refactor sugerido: promover `scanner` a sub-paquete
  `internal/library/scan/`. La frontera deja de ser package boundary y
  pasa a ser fichero boundary (más barato, mismo aislamiento lógico).
  Coste: un `goimports` + reorganizar `cmd/hubplay/main.go:161` y los
  tests. Cero impacto en el resto del código.

#### Y — Goroutines de `SegmentDetector` y `SegmentFingerprinter` sin drain · **media** (bug latente)

`segment_detector.go:71-83`:

```go
func (d *SegmentDetector) Start(ctx context.Context) (unsub func()) {
    return d.bus.Subscribe(event.LibraryScanCompleted, func(e event.Event) {
        libID, _ := e.Data["library_id"].(string)
        if libID == "" { return }
        go func() {                              // ← no tracking
            if err := d.DetectLibrary(ctx, libID); err != nil {
                d.logger.Warn(...)
            }
        }()
    })
}
```

- El `unsub func()` retornado **sólo desuscribe del bus**. No espera
  a las goroutines de `DetectLibrary` ya en vuelo.
- `main.run` defer `unsub()` después del `Wait()` del HTTP server,
  pero **antes del** `database.Close()`. Si una detection está en
  medio de un write cuando llega SIGTERM, escribe con ctx cancelado
  durante un periodo antes de que el `database.Close()` corte.
- Compárese con `library.Service` (líneas 100-103, 134-138) que
  tiene `bgWG sync.WaitGroup` y `Shutdown()` que `Wait`. El patrón
  correcto **ya existe** en el paquete; los segment workers no lo
  aplican.
- Impacto real: ruido en logs durante shutdown ("sql: database is
  closed"); en patológicos, escrituras parciales si la detection
  está a medio commit.
- Principio violado: **lifecycle management** consistente. El
  contrato implícito "`Stop`/`Close`/`unsub` drena trabajo
  in-flight" se rompe aquí.
- Refactor sugerido (cualquiera de los dos):
  - Añadir `wg sync.WaitGroup` al detector y al fingerprinter; el
    `unsub` cierra el subscribe pero también `wg.Wait()`.
  - O — más limpio — los detectors se registran en `library.Service`
    como sub-workers y reusan su `bgWG`.

#### Z — `library.Service` god-service con 27 métodos · **media**

- Cubre **6 responsabilidades**:
  1. CRUD libraries (`Create`, `CreatePersonalIPTV`, `GetByID`, `List`,
     `Update`, `Delete`).
  2. ACL (`ListForUser`, `UserHasAccess`, `GrantAccess`, `RevokeAccess`,
     `ListAccessByUser`, `ReplaceAccess`).
  3. Scan orchestration (`Scan`, `ScanSync`, `ScanAll`, `IsScanning`).
  4. Item queries con rating-cap (`ListItems`, `ListGenres`, `GetItem`,
     `GetItemChildren`, `GetItemChildCounts`, `GetItemStreams`,
     `GetItemImages`).
  5. Latest/rails (`LatestItems`, `LatestSeriesByActivity`).
  6. Telemetría (`ItemCount`).
- Principio violado: **SRP**.
- La mayoría de los métodos del bloque 4 (Item queries) son
  passthroughs a `db.ItemRepository` con un filtro opcional de
  rating. **Posible service-anémico** — el handler podría llamar al
  repo directo y un middleware/helper aplicar el rating cap.
- Refactor (impacto medio):
  - `library.LibraryService` ← CRUD + scan orchestration (bloques 1+3).
  - `library.AccessControl` ← ACL (bloque 2).
  - Item queries: que el handler llame al repo directamente; el
    rating-cap se aplica con un helper (`library.FilterByRating`) o,
    mejor, con un *decorator* del repo (`db.ItemRepository.WithCap(cap
    string)` → wrapper que añade el WHERE) — esto evita reintroducir
    una capa de service inútil.

#### AA — Frontend del olor M se manifiesta aquí · **eco de M**

`library.Service` y `scanner.Scanner` reciben **24 punteros `*db.X`
distintos entre ambos**. Cuando F2.5 (decisión Opción B) se aplique a
`library`, este paquete será el segundo más impactado tras `iptv` —
arrastra ~12 tipos (Item, MediaStream, Image, Chapter, EpisodeSegment,
ItemValue, Studio, Collection, ExternalID, Metadata, Person,
ItemPersonCredit). El refactor incremental tiene aquí el bloque más
grande y conviene **trabajarlo en último lugar**, no primero.

#### BB — Comentarios en inglés masivos · **media**, transversal

9 ficheros, casi todos los comentarios largos están en inglés. Ej.:
- `service.go:91-105` — 10 líneas del Shutdown lifecycle.
- `scanner.go` — todos los headers de métodos.
- `watcher.go:1-20` — header del paquete.
- `contentrating.go:1-25` — doc del paquete.

Apuntado para el plan final.

### 4.3 Confirmaciones de `[PENDIENTE]`

- `[PENDIENTE-F4]` "qué workers respetan ctx para cancelación
  in-flight" → confirmado:
  - `library.Service.Scan` usa `scanCtx, cancel := WithTimeout(s.bgCtx,
    30m)` + `bgWG` → drena en `Shutdown()`. **Correcto.**
  - `Scheduler` (scan periódico) usa el ctx + stopCh → **correcto.**
  - `ImageRefreshScheduler` igual → **correcto.**
  - `FSWatcher` (429 LOC) usa ctx + stopCh + mu+stopped → **correcto y
    rico**, con `atomic.Int64 walksDone` para tests.
  - `SegmentDetector` y `SegmentFingerprinter` **NO drenan** → olor Y.

### 4.4 Severidades Fase 4

| # | Problema | Severidad | Prerequisito |
|---|----------|-----------|--------------|
| W | `scanner.go` 1 270 LOC en un fichero | Media-alta | Split por responsabilidad |
| X | Frontera `library` vs `scanner` artificial | Baja | Promover a sub-paquete |
| Y | Segment detector/fingerprinter sin drain | Media | Añadir `bgWG` (bug latente) |
| Z | `library.Service` 27 métodos, 6 responsabilidades | Media | Split + repo decorator para rating |
| AA | Eco de M (cuantifica) | — | Atacar en último lugar al hacer M |
| BB | Comentarios en inglés | Media | Marcar para plan final |

### 4.5 Notas operativas

- **Sano**: lifecycle de `library.Service` (bgWG + bgCancel + bgCtx),
  el patrón debería replicarse en los detectors.
- **Sano**: `Scanner.ScanLibrary` secuencial. No optimizar
  prematuramente con worker pool — la I/O sequential + SQLite single-
  writer son la decisión correcta.
- **Sano**: `FSWatcher` 429 LOC, con su propia complejidad pero bien
  encapsulada (un único struct, lifecycle explícito).

---

## Fase 5 · `internal/iptv/`

Cerrada · 2026-05-14.

### 5.1 Inventario

32 ficheros, 7 776 LOC. Cabezas:

| Fichero | LOC | Concepto |
|---|---:|---|
| `transmux.go` | 1 052 | `TransmuxManager` + `TransmuxSession` (ffmpeg orchestrator) |
| `proxy.go` | 795 | `StreamProxy` + relay accounting + breaker |
| `matcher.go` | 427 | EPG/channel matching helpers |
| `service_epg.go` | 385 | EPG service methods |
| `xmltv.go` | 360 | XMLTV parser |
| `m3u.go` | 342 | M3U parser |
| `service_channel_order.go` | 326 | Channel-order overlay |
| `prober.go` | 319 | Active probe one-shot |
| `m3u_language.go` | 316 | Language filter |
| `service.go` | 290 | Constructor + struct |

`Service` reparte sus 45 métodos exportados en **11 ficheros
`service_*.go`** — split cosmético, mismo receiver `s *Service`.

`Service` struct: 9 repos + 2 mutexes + 2 maps + `sync.Once` + 2
`http.Client` (uno con lazy TLS-insecure) + bus + `proberWorker`
(post-construction interface).

Setters post-construcción "infraestructurales":
- `SetEventBus(bus)`.
- `SetProberWorker(w proberRunner)` — sink pattern documentado.

### 5.2 Hallazgos

#### CC — `iptv.Service` god-service con 45 métodos y 11 sub-features · **alta**

- 45 métodos exportados sobre el mismo `*Service`. Es la encarnación
  máxima de un *Java service object*.
- 11 sub-features mezcladas en un único receiver:
  1. M3U import / refresh (`service_m3u.go`).
  2. EPG refresh + lookup + sources (`service_epg.go`, `service_epg_sources.go`).
  3. Favorites (`service_favorites.go`).
  4. Channel order per-user (`service_channel_order.go`).
  5. Library channel order admin overlay (mismo fichero).
  6. Channel CRUD active/inactive (`service_channels.go`).
  7. Channel overrides (`service_overrides.go`, tvg_id).
  8. Channel health bucket transitions (`service_health.go`).
  9. Watch history (`service_watch_history.go`).
  10. Schedule queries (parte de `service_epg.go`).
  11. HTTP client pool (TLS-insecure lazy en `service.go`).
- Principio violado: **SRP**. El split por ficheros oculta el síntoma
  pero no lo cura — un cambio en una sub-feature recompila el paquete
  entero, los tests inflados, las interfaces `IPTVService` en handlers
  con 90+ LOC (`handlers/interfaces.go:134-218`).
- Refactor sugerido (no big-bang, por sub-feature):
  ```
  internal/iptv/
    m3u/        ← service_m3u, m3u, m3u_language, group_title,
                  categories, preflight, scheduler, public
    epg/        ← service_epg, service_epg_sources, xmltv,
                  epg_catalog, epg_aliases, epg_diagnostic, matcher
    channels/   ← service_channels, service_overrides, service_favorites,
                  service_channel_order
    transmux/   ← transmux, transmux_args, transmux_stderr,
                  transmux_codec_classify
    proxy/      ← proxy, circuit_breaker
    prober/     ← prober, prober_worker, service_health
    logo/       ← logo, logo_cache
  ```
- Beneficio: cada sub-paquete con cohesión real; el namespace `iptv.X`
  deja de tener 25+ tipos heterogéneos; el constructor de cada sub-
  servicio toma sólo los repos que necesita (hoy `Service` recibe 9
  repos aunque la mayoría de métodos usen sólo 1-2).
- Coste: alto (~1-2 días). Pero es el segundo refactor de mayor
  impacto del proyecto tras la decisión Opción B sobre `db/`.
- Severidad alta por tamaño + tasa de crecimiento. Cada nueva
  sub-feature (channel order per-user, watch history, prober…) ha
  añadido un fichero `service_X.go`. La inercia es divergente.

#### DD — Detached goroutines en `service_m3u.go` sin drain · **media** (mismo patrón Y de F4)

`service_m3u.go:230, 246` tras un `RefreshM3U`:

```go
go func(id string) {
    bg, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
    defer cancel()
    if _, err := s.RefreshEPG(bg, id); err != nil { ... }
}(libraryID)

if s.proberWorker != nil {
    go func(id string) {
        bg, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
        defer cancel()
        if _, err := s.proberWorker.ProbeNow(bg, id); err != nil { ... }
    }(libraryID)
}
```

- `context.Background()` desconecta del lifecycle del proceso —
  intencional ("import response should not block on a slow XMLTV
  download"), **pero no hay drain**.
- `Service.Shutdown()` está vacío (`func (s *Service) Shutdown() {}`).
- Impacto: shutdown llega justo después de un import → goroutines
  intentan escribir a DB cerrada. Logs ruidosos; en patológico,
  parciales.
- Mismo diagnóstico que Y (segment detector/fingerprinter, F4): el
  paquete no tiene patrón de drain.
- Refactor: añadir `bgCtx, bgCancel, bgWG` al `Service`. Las
  goroutines detached pasan a usar `bgCtx` (que se cancela en
  shutdown) y se registran en `bgWG`. `Service.Shutdown()` llama a
  `bgCancel(); bgWG.Wait()`. Mismo patrón que `library.Service`.

#### EE — `StreamProxy.Shutdown` engañoso · **baja**

```go
func (p *StreamProxy) Shutdown() {
    p.mu.Lock()
    for id := range p.relays {
        delete(p.relays, id)
    }
    p.mu.Unlock()
}
```

- Sólo borra el mapa de contabilidad. **No drena las goroutines de
  `ProxyStream` / `streamWithReconnect`** en vuelo.
- En la práctica el drain efectivo lo hace el `http.Server.Shutdown`:
  cuando los HTTP requests se cancelan, las goroutines reciben ctx
  cancelado y retornan.
- Severidad baja porque el drain funcional ya existe; el problema es
  **claridad**. El método se llama `Shutdown` y no shutdownea — es
  un `ResetCounters` o `ClearRelays`.
- Refactor cosmético: renombrar o documentar.

#### FF — TransmuxManager lifecycle bien drenado · **sano**

`transmux.go:847-867` `Shutdown`:
- `stopOnce.Do(close(m.stop))` para el reaper loop.
- `<-m.stopped` espera a que el reaper termine.
- Itera sessions y llama `m.terminate(s)` para cada una.
- Logging final.

**Patrón a replicar en olores DD y Y** (segment workers + iptv
goroutines detached). Es el ejemplo de cómo se debe hacer drain en
este proyecto.

Adicional: el Manager comparte el `breaker` con `StreamProxy` vía
`iptvProxy.Breaker()` (línea 323 de `main.go`) — **per-channel
breaker compartido entre HLS proxy y MPEG-TS transmux**. Razonado
correctamente: un upstream muerto bloquea ambos paths con el mismo
cooldown.

#### GG — Sink pattern para romper ciclo `Service ↔ ProberWorker` · **sano**

- `proberRunner` interface en `service.go:84` evita que `Service` (que
  ya implementa `ChannelHealthReporter`) tenga que importar
  `ProberWorker`. `SetProberWorker` lo wirea post-construction.
- Patrón documentado en `conventions.md`. Idiomático Go.
- Tras el refactor CC (split en sub-paquetes), un sub-paquete
  `iptv/prober/` puede depender unidireccionalmente del de health/
  channels — el sink se vuelve innecesario. Cosmético.

#### HH — TransmuxSession concurrency primitives · **sano (complejo pero justificado)**

- `cmd *exec.Cmd`, `cancel context.CancelFunc`, `done chan struct{}`,
  `ready chan struct{}`, `readyOnce sync.Once`, `outcomeOnce
  sync.Once`, `stderrTail *stderrRing`, `lastTouchUnixNano
  atomic.Int64`, `stopped atomic.Bool`.
- **Cada primitiva justificada con comentario inline**: por qué
  atomic, por qué Once, qué garantiza cada uno.
- 3 goroutines por sesión (`processWatcher`, `readyWatcher`, `cmd`
  itself). Todas con paths de salida claros.
- Es la pieza más concurrente del proyecto y está bien documentada.
  No es olor — es complejidad inherente al dominio.

#### II — DTOs `iptv.M3UChannel`/`iptv.Playlist` vs modelos `db.Channel*` · **baja, eco de M**

- 7 tipos `db.Channel*` (Channel, ChannelFavorite, ChannelHealthSummary,
  ChannelOverride, UserChannelOrderEntry, LibraryChannelOrderEntry,
  ChannelWatchHistory…) viven en `db/`.
- DTOs del parser (`M3UChannel`, `Playlist`) viven en `iptv/`.
- Cuando se aplique M (Opción B de F2), los 7 tipos migran a `iptv/`.
  Es **el bloque más grande del refactor M**.

### 5.3 Confirmaciones de `[PENDIENTE]`

- `[PENDIENTE-F5]` `TransmuxManager` lifecycle → confirmado **sano**,
  patrón modelo (hallazgo FF).
- `[PENDIENTE-F5]` `LogoCache` fail-soft → `internal/iptv/logo_cache.go`
  retorna `nil` en construcción si falla, los handlers tratan nil como
  "cache disabled". Confirmado **sano**.
- `[PENDIENTE-F5]` Acoplamiento interno de sub-features → confirmado
  alto (hallazgo CC); refactor pendiente.

### 5.4 Severidades Fase 5

| # | Problema | Severidad | Prerequisito |
|---|----------|-----------|--------------|
| CC | `iptv.Service` god-service, 45 métodos, 11 sub-features | Alta | Split en sub-paquetes |
| DD | Detached goroutines en `RefreshM3U` sin drain | Media | Replicar patrón `bgWG` |
| EE | `StreamProxy.Shutdown` engañoso (no drena) | Baja | Renombrar o documentar |
| II | DTOs `iptv.*` vs modelos `db.Channel*` | Baja | Eco de M; cubre 7 tipos |

### 5.5 Notas operativas

- **Sano**: `TransmuxManager.Shutdown` (FF), `TransmuxSession` (HH),
  breaker compartido proxy↔transmux (parte de FF), `LogoCache` fail-
  soft, sink pattern para evitar ciclo `Service↔ProberWorker` (GG).
- **Patrón modelo** para olores DD/Y: `library.Service` (F4) y
  `TransmuxManager` (F5) ambos hacen drain correcto. El proyecto
  tiene la solución idiomática **dos veces**; los detectors y los
  detached goroutines de iptv son los outliers.

---

## Fase 6 · `internal/stream/`

Cerrada · 2026-05-14.

### 6.1 Inventario

11 ficheros, 2 722 LOC:

| Fichero | LOC | Concepto |
|---|---:|---|
| `manager.go` | 953 | `Manager` + `ManagedSession` + cleanupLoop + StartSession con `singleflight.Group` |
| `transcode.go` | 572 | `Transcoder` (low-level ffmpeg) + `Session` |
| `decision.go` | 270 | `PlaybackDecision` + `Decide` / `DecideForceDirectPlay` puro |
| `hwaccel.go` | 188 | Detector VAAPI/NVENC/QSV/VideoToolbox |
| `capabilities.go` | 177 | Parse `X-HubPlay-Caps` HTTP header |
| `hls.go` | 147 | Playlist HLS generation |
| `autotune.go` | 136 | Auto-tune sessions cap + preset por core count |
| `subburn.go` | 115 | Burn-in PGS/DVDSUB/ASS |
| `testseam.go` | 73 | Test hooks |
| `subtitle.go` | 61 | Sub track extraction |
| `profiles.go` | 30 | Tabla de perfiles |

`Manager` struct: 14 campos (mu, sessions map, transcoder, items repo,
streams repo, cfg, logger, stopClean, metrics, bus, startGroup
singleflight, hwAccel result, forceDirectPlayLookup).

**3 setters post-construcción**: `SetMetrics`, `SetEventBus`,
`SetForceDirectPlayLookup`. Confirma el riesgo levantado en F1.3.

### 6.2 Hallazgos

#### JJ — 3 setters post-construcción · **baja-media**

- **Implementación correcta**: `SetMetrics` y `SetEventBus` toman
  `mu.Lock` al escribir; `publish()` re-lee bus bajo lock para
  evitar race. `SetMetrics` además rehilatra `metrics.SetActiveSessions`
  para sincronizar el counter inicial. Patrón sano y documentado.
- **Pero** dos de los tres son dependencias estables (se setean una
  vez al boot, nunca después): metrics y bus. Sólo
  `SetForceDirectPlayLookup` justifica el patrón post-construction —
  es un closure que lee `app_settings` en cada request, naturalmente
  no encaja en el constructor.
- Olor real: **Builder Pattern accidental**. La construcción de
  `Manager` queda artificialmente partida en 4 pasos
  (`NewManager` + `SetMetrics` + `SetEventBus` + `SetForceDirectPlayLookup`)
  cuando podrían ser 2 (constructor + un único setter "runtime").
- Refactor sugerido (cosmético, ~30 min):
  ```go
  // Antes:
  sm := stream.NewManager(items, streams, cfg, logger)
  sm.SetMetrics(observability.NewStreamSink(metrics))
  sm.SetEventBus(eventBus)
  sm.SetForceDirectPlayLookup(...)

  // Después:
  sm := stream.NewManager(stream.Deps{
      Items:    repos.Items,
      Streams:  repos.MediaStreams,
      Config:   streamingCfg,
      Logger:   logger,
      Metrics:  observability.NewStreamSink(metrics),
      Bus:      eventBus,
  })
  sm.SetForceDirectPlayLookup(...)  // único setter runtime
  ```
- Severidad baja (no afecta correctness). Lo apunto porque elimina
  argumento estructural de F1.3 ("5 setters") cuando en realidad
  sólo uno es genuinamente runtime.

#### KK — `Manager` recibe `*db.X` directos · **eco de M**

- `items *db.ItemRepository`, `streams *db.MediaStreamRepository`.
- Cuando F2.5 (Opción B) migre tipos, `Manager` también migra. No
  acción nueva.

#### LL — `Manager` y `Transcoder`: dos capas de session tracking · **media** (over-engineering)

- `Transcoder.sessions map[string]*Session` + `Transcoder.mu`.
- `Manager.sessions map[string]*ManagedSession` + `Manager.mu`, donde
  `ManagedSession` embebbe `*Session`.
- **Dos mutexes, dos maps, dos lifecycles** para el mismo concepto
  "sesión activa".
- Concepto del split (defendible):
  - `Transcoder` = wrapper low-level ffmpeg.
  - `Manager` = business (decisión, caps per-user, singleflight,
    reaper).
- Implementación (problemática):
  - `Transcoder` expone `GetSession`, `Stop`, `StopAll`,
    `ActiveSessions` que duplican los métodos del `Manager`. La
    *única* responsabilidad única del Transcoder son `Start` y
    `RestartAt`.
- Principio violado: **DRY** (en el sentido de "una sola fuente de
  verdad sobre qué sesiones existen"). El bug latente: si `Manager`
  saca una sesión de su map por idle reap pero `Transcoder` todavía
  la tiene, queda zombie. Hoy el código evita esto, pero la doble
  contabilidad es frágil.
- Refactor sugerido:
  - `Transcoder` se vuelve **stateless**: funciones puras
    `StartProcess(...) (*Session, error)` y `RestartAtProcess(...)`.
  - El tracking vive **sólo en `Manager.sessions`**.
  - Reduce `transcode.go` de 572 LOC a ~350 LOC y elimina la duplicación.
- Severidad media. Es la cabeza del paquete `stream/` que más se
  beneficia de un refactor focalizado.

#### MM — `decision.go` puro y testeable · **sano, modelo**

- Funciones puras: `Decide`, `DecideForceDirectPlay`,
  `containerInSet`, `splitContainer`, `audioCodecName`,
  `hdrFormatInSet`. Cero side effects.
- Es **el ejemplo del proyecto** de "lógica de negocio aislada de
  I/O". El resto del codebase (auth.Service, library.Service,
  iptv.Service) mezcla lógica con I/O. Este patrón debería
  replicarse.
- Tests probables: `decision_test.go` (existe, 270 LOC = ~mismo
  tamaño que decision.go).

#### NN — `singleflight.Group` para colapsar StartSession concurrentes · **sano, idiomático**

- `manager.go:51-65` declara `startGroup singleflight.Group`. Comentario
  excelente:
  > Two parallel callers for the same userID:itemID:profile (player
  > init + an immediate auth-retry burst, a double-clicked Play,
  > hls.js requesting the manifest while the page is still mounting,
  > etc.) used to BOTH miss the m.sessions fast-path lookup and BOTH
  > reach transcoder.Start, leaving two ffmpegs alive simultaneously
  > and writing segments to the same cache dir. singleflight collapses
  > the racers onto a single execution; late joiners receive the same
  > ManagedSession the winner built.
- Patrón **idiomático Go**, dolor real documentado. No es olor.

#### OO — HWAccel: detector único al boot · **sano** (ADR-006 cumplido)

- `Manager.hwAccel HWAccelResult` capturado en construcción, leído
  por todas las sesiones. Sin re-detect runtime.
- Admin UI muestra "Reinicia para aplicar" — explícito (ADR-010).
- `main.go:316-327` reusa el resultado para `TransmuxManager`
  (ReencodeEncoder + HWAccelInputArgs) → consistency entre VOD y
  IPTV transmux.
- No es olor.

#### PP — `cleanupLoop` y `Shutdown` correctos · **sano**

- Una goroutine única (`cleanupLoop`), tick 1 min, idle timeout
  configurable. Drena en `Shutdown` vía `close(m.stopClean)`.
- `Shutdown` itera sessions, llama `ms.Stop()` para cada una,
  resetea active sessions metric. Comparable a `TransmuxManager` (F5).
- No es olor.

### 6.3 Confirmaciones de `[PENDIENTE]`

- `[PENDIENTE-F6]` 5 setters post-construcción → confirmado, **son
  3 no 5** (F1 contaba mal). Análisis JJ.
- `[PENDIENTE-F6]` Decision tree → confirmado, **modelo** del proyecto
  (MM).
- `[PENDIENTE-F6]` HWAccel detector único → confirmado (OO).

### 6.4 Severidades Fase 6

| # | Problema | Severidad | Prerequisito |
|---|----------|-----------|--------------|
| JJ | 3 setters post-construcción (Builder Pattern accidental) | Baja | `NewManager(Deps)` con un único setter runtime |
| LL | `Manager` y `Transcoder` con doble session tracking | Media | Transcoder stateless |
| KK | `Manager` con `*db.X` directos | — | Eco de M |

### 6.5 Notas operativas

- **Patrones modelo del proyecto**: `decision.go` (lógica pura),
  `singleflight.Group` en StartSession, `cleanupLoop` con drain,
  HWAccel cacheado al boot. Cuando otros paquetes hagan refactor,
  citar éste.
- Confirma que `stream/` es el paquete **mejor diseñado del backend**
  pese a su tamaño. Los olores aquí son matices, no estructurales.

---

## Fase 7 · `auth` + `federation`

Cerrada · 2026-05-14.

### 7.1 Inventario

**`internal/auth/` (6 ficheros, 1 683 LOC):**

| Fichero | LOC | Concepto |
|---|---:|---|
| `service.go` | 757 | `Service` con 18 métodos (6 responsabilidades) |
| `device.go` | 368 | `DeviceCodeService` — flujo "device code" |
| `keystore.go` | 279 | `KeyStore` con rotación + encriptación at-rest |
| `ratelimit.go` | 107 | `loginRateLimiter` per-key lockout |
| `jwt.go` | 92 | Claims + sign/validate con `keyResolver` inyectada |
| `middleware.go` | 80 | HTTP middleware (importa `apperror`) |

**`internal/federation/` (21 ficheros, 3 613 LOC):**

| Fichero | LOC | Concepto |
|---|---:|---|
| `client.go` | 603 | HTTP client a peers |
| `manager.go` | 506 | `Manager` core + lifecycle |
| `manager_handshake.go` | 262 | pair/handshake (3 métodos del Manager) |
| `middleware.go` | 251 | `RequirePeerJWT` |
| `identity.go` | 200 | Ed25519 keypair + `IdentityStore` |
| `audit.go` | 192 | `Auditor` async writer + drop policy |
| `jwt.go` | 156 | `PeerClaims` EdDSA + Nonce |
| `manager_search.go` | 149 | search peers (4 métodos) |
| `ratelimit.go` | 141 | `RateLimiter` token-bucket per-peer |
| `nonce.go` | 133 | anti-replay cache |
| `manager_browse.go` | 130 | browse peer libraries (4 métodos) |
| `manager_shares.go` | 109 | shares config (6 métodos) |
| `manager_progress.go` | 74 | continue-watching cross-peer (3 métodos) |
| … | … | (8 ficheros más, <100 LOC cada uno) |

### 7.2 Hallazgos

#### QQ — `auth.Service` god-service con 18 métodos, 6 responsabilidades · **media** (mismo patrón Z/CC)

- Mezcla:
  1. Account lifecycle (Register, ResetPassword, ChangePassword).
  2. Login flow (Login con rate-limit, ValidateToken).
  3. Token lifecycle (RefreshToken, Logout, KeyStoreOrNil).
  4. Session management (ListSessions, RevokeSession, CurrentSessionID,
     InvalidateUserSessions).
  5. Session cleaner background goroutine
     (StartSessionCleaner / StopSessionCleaner).
  6. Profiles per-user (ListProfiles, SwitchProfile, SetPIN).
- Mismo olor que `library.Service` (Z, F4) y `iptv.Service` (CC, F5).
- Severidad **media** (es la cabeza más pequeña de los tres god-
  services: 18 métodos vs 27 vs 45).
- Refactor:
  - `auth.LoginService` (Login + RefreshToken + rate-limit).
  - `auth.AccountService` (Register + ResetPassword + ChangePassword).
  - `auth.SessionService` (ListSessions + RevokeSession +
    InvalidateUserSessions + Session cleaner).
  - `auth.ProfileService` (ListProfiles + SwitchProfile + SetPIN).
- Coste menor que CC porque `auth/` es 5× más pequeño que `iptv/`.

#### RR — `auth.loginRateLimiter` goroutine sin Stop · **media** (bug latente)

`auth/ratelimit.go:30-36`:

```go
go func() {
    ticker := time.NewTicker(10 * time.Minute)
    defer ticker.Stop()
    for range ticker.C {
        rl.cleanup()
    }
}()
```

- **No tiene `stopCh`, no se referencia, no se cancela.** Vive hasta
  que el proceso muere.
- En producción no causa daño visible (el proceso completo termina).
- Pero en **tests integrados** que crean múltiples `auth.Service` →
  goroutine leak detector lo flagea, y los tests acumulan goroutines
  zombie entre subtests.
- Principio violado: **lifecycle management** consistente. El
  proyecto ya tiene patrón (`Manager.Shutdown`, `TransmuxManager.Shutdown`,
  `library.Service.Shutdown`). Este es el outlier.
- Refactor: añadir `stopCh chan struct{}` al `loginRateLimiter` y
  un `Stop()` que `Service.StopSessionCleaner` (que ya existe y se
  llama en shutdown) lo invoque.
- Severidad **media** porque es bug latente, no en producción.

#### SS — `auth.ratelimit.go` (107) vs `federation.ratelimit.go` (141) divergentes · **baja**

- Confirmado, dos implementaciones de "token bucket / lockout".
- **auth**: fixed-window lockout tras N fails (login attempts).
  Semántica: "después de 5 fallos en 5 min, bloquea 15 min".
- **federation**: token-bucket con refill (catalog sync). Semántica:
  "X requests/sec, burst de Y".
- ADR-012 admite la duplicación con justificación explícita:
  > Las semánticas divergen sutilmente — auth ratelimit es per-IP
  > con burst muy bajo (login attempts), federation es per-peer con
  > bursts permisivos (catalog sync). Una abstracción unificada
  > habría llevado opciones que oscurecen call sites.
- **No es olor** — decisión consciente documentada. Sólo cito para
  confirmar que la promesa del ADR sigue cumpliéndose.

#### TT — `federation/` con split `manager_*.go` aplicado bien · **sano, modelo**

- `manager.go` + 5 `manager_*.go` (handshake, search, browse, shares,
  progress). Cada sub-fichero contiene 3-6 métodos del Manager
  agrupados por feature.
- **Es el patrón que CC (Fase 5) propone para `iptv.Service`** —
  aplicado aquí más limpio porque:
  - Federation tiene 30 métodos, no 45.
  - Las features no comparten estado mutable (cada uno usa el `repo`
    + `clock` + `identity`, no maps in-memory de sub-features).
- Modelo a citar cuando se haga el refactor CC.

#### UU — `federation.Auditor` async writer · **sano, modelo**

- `audit.go:75-184`: cola in-memory + flush periódico + drop policy
  explícita (`logDropOnce` cuando el canal se llena).
- `Auditor.Close()` se llama desde `Manager.Close()` → integrado.
- ADR-012 justifica:
  > El SQLite write añade ~5-10ms al hot path peer-to-peer; el audit
  > es por definición no-critical; mejor async + tolerate-drop
  > documentado.
- **Modelo del proyecto** de "async writer correcto". Citar cuando se
  propongan otros writers async (audit log de usuarios, telemetría,
  etc.).

#### VV — `federation.Manager` con dos mutexes granulares · **sano**

- `mu sync.RWMutex` protege `peerCache` (hot path JWT validation).
- `streamMu sync.Mutex` protege `streamSessions`.
- Comentario explícito:
  > Separate mutex from peerCache because the streaming hot path
  > doesn't need the peer-cache reader, and holding peerCache's
  > RWMutex during a stream sweep would block JWT validation.
- Locking granular **bien razonado y documentado**. Modelo de "no
  hace falta un único mutex grande".

#### WW — `auth.jwt.go` con `keyResolver` función inyectada · **sano**

- `auth/jwt.go:24`:
  ```go
  type keyResolver func(kid string) (*db.SigningKey, error)
  ```
- Desacopla el JWT layer del `KeyStore` concreto. Comentario:
  > Taking a function (rather than a concrete KeyStore) keeps the
  > JWT layer free of auth-package cycles and trivial to fake in
  > tests.
- **Patrón idiomático Go**: función como interfaz mínima de 1
  método. Más limpio que declarar un `type KeyResolver interface {
  Resolve(kid string) (*db.SigningKey, error) }` cuando sólo se usa
  un método.
- Federation reusa el mismo *shape* (`auth/jwt.go` y `federation/jwt.go`
  tienen forma paralela pese a no compartir código). ADR-012 lo
  documenta como "reuse del shape, no del código por divergencia
  HS256/EdDSA". Trade-off correcto.

#### XX — `auth/middleware.go → internal/api/apperror` · **conocido cut-set**

- Confirma el sano #4 de F1.1. Es la solución al ciclo
  `auth ↔ handlers`. **No es olor**, pero la "extrañeza" del import
  (un paquete `auth` que importa algo de `api/`) es intencional y
  documentada en el package doc de `apperror`.

#### YY — Tipos de auth y federation viven en su feature · **sano** (anticipa Opción B)

- `auth/`: tipos `Claims`, `AuthToken`, `RegisterRequest`,
  `DeviceCodePair`, `DeviceCodeStatus`, `KeySnapshot` viven en
  `internal/auth/`.
- `federation/`: tipos `Peer`, `Invite`, `ServerInfo`,
  `LibraryShare`, `SharedItem`, `CachedItem`, `Identity`,
  `AuditEntry`, etc. viven en `internal/federation/`.
- Pero ambos paquetes **siguen usando** `*db.User`, `*db.Session`,
  `*db.SigningKey` del paquete `db`. Mezcla pura: los tipos *propios*
  viven en la feature, los modelos de persistencia siguen en `db/`.
- Cuando F2.5 (Opción B) se aplique a `auth`, los tipos `db.User`,
  `db.Session`, `db.SigningKey`, `db.DeviceCode` deberían migrar a
  `auth.User`, `auth.Session`, `auth.SigningKey`, `auth.DeviceCode`.
  Federation ya lo hizo con su mitad.

### 7.3 Confirmaciones de `[PENDIENTE]`

- `[PENDIENTE-F7]` ADR-012 reuse documentado → confirmado **sano**.
  El "reuse del shape" se cumple en JWT (WW), keystore (Bootstrap +
  NewKeyStore), event bus (ambos publican), audit (modelo nuevo en
  federation, no en auth — pero auth tampoco lo necesita).
- `[PENDIENTE-F7]` Ratelimit duplication → confirmado no-olor (SS).

### 7.4 Severidades Fase 7

| # | Problema | Severidad | Prerequisito |
|---|----------|-----------|--------------|
| QQ | `auth.Service` 18 métodos, 6 responsabilidades | Media | Split (más fácil que Z/CC por tamaño) |
| RR | `loginRateLimiter` goroutine sin Stop | Media | Añadir `stopCh` + invocar desde `StopSessionCleaner` |
| SS | Ratelimit duplicado (ADR-012 lo justifica) | — | No-acción |
| YY | Tipos `db.User`/`db.Session` en repo (sub-caso de M) | — | Atacar como parte de M para auth |

### 7.5 Notas operativas

- **Modelos a replicar** que viven aquí:
  - `federation.Auditor` (UU) — async writer con drop policy.
  - `federation.Manager` lock granular (VV).
  - `federation/` split `manager_*.go` por feature (TT) — modelo para
    el refactor CC de F5.
  - `auth.jwt.keyResolver` función inyectada (WW) — modelo de "una
    interfaz de un método se reemplaza por `func(...)`".
- **El bug RR** es el único de Fase 7 que merece fix urgente. Es
  ~10 LOC de fix, sin impacto en API pública.

---

## Fase 8 · `event` + primitivos sin deps

Cerrada · 2026-05-14.

### 8.1 Inventario

| Paquete | LOC prod | Ficheros | Observación |
|---|---:|---:|---|
| `event` | 220 | 1 | Bus pub/sub in-proc, ADR-008 aplicado |
| `clock` | 24 | 1 | `Clock` interface + Real + Mock |
| `logging` | 255 | 2 | Wrapper slog + buffer in-memory |
| `observability` | 516 | 6 | Registry Prometheus dedicado |
| `probe` | 260 | 1 | Wrapper ffprobe |
| `blurhash` | 189 | 1 | Hash miniaturas |
| `sysmetrics` | 343 | 1 | Sampler CPU/RAM/GPU |

Todos sin imports internos (raíces del grafo, F1.2). Todos con
package-doc inicial que explica el porqué.

### 8.2 Hallazgos

#### ZZ — Bus contract OK; el bug está en los detectors · **eco de Y/DD**

- `event.Bus.Publish` lanza una goroutine por handler. El watchdog
  emite warning a los 30 s y siempre sale (ADR-008). Si un handler
  cuelga, leakea **una** goroutine.
- **Subscribers reales auditados**:
  - `library/segment_detector.go:78` — handler **no bloquea** (lanza
    `go d.DetectLibrary(ctx, libID)` y retorna). Bus contract OK.
  - `library/segment_fingerprinter.go:81` — mismo patrón.
  - `handlers/auth_device.go:367` — handler hace `select { case
    eventCh <- e: default: drop }`. Non-blocking, idiomático SSE.
  - `handlers/me_events.go:116` — mismo patrón, per-user filter.
  - `handlers/events.go:106` — mismo patrón.
- **Conclusión**: ningún subscriber bloquea en el handler. El bus
  contract se cumple. **El problema de olor Y (F4) y DD (F5) NO es
  del bus — es que las goroutines spawneadas por los handlers no se
  drenan**.
- No-acción sobre el bus. Acción ya catalogada en olores Y/DD.

#### AAA — Lista de tipos del bus con comentario desactualizado · **baja**

- `event/bus.go:24-31` declara:
  > NOTE: as of 2026-04-17 only the five scan/item types are
  > actually published by the scanner. The others (Metadata*,
  > Transcode*, Channel*, EPG*, Playlist*, User*) are reserved for
  > upcoming features.
- En 2026-05-14 esto **ya no es cierto**. Grep confirma publishers
  en 8 paquetes:
  - `scanner` — Library/Item events.
  - `auth` — User events.
  - `federation` — peer events.
  - `iptv` — ChannelHealthChanged, PlaylistRefreshed/Failed,
    EPGUpdated, ChannelAdded/Removed.
  - `stream` — TranscodeStarted/Completed.
  - `library` — LibraryScanProgress + segment detection events.
  - `handlers/progress` — progress events.
- Comentario desactualizado por 1 mes. Severidad baja.
- Refactor trivial: borrar la NOTE o reescribirla acorde.

#### BBB — `observability/` con package-doc modelo · **sano**

- Package-doc explica 4 decisiones de diseño (registry privado,
  labels low-cardinality, histogram buckets hand-picked, collectors
  typed) en 15 líneas.
- Es el ejemplo del proyecto de "documentar la decisión en el
  código, no sólo en ADR".
- Modelo a replicar cuando se escriban package-docs nuevos.

#### CCC — `clock` mínimo y útil · **sano, modelo de primitivo**

- 24 LOC: 1 interface, 2 implementaciones (Real + Mock con
  `Advance(d)`).
- Inyectado en `auth.Service`, `federation.Manager`, `iptv.Service`,
  `iptv.RateLimiter`, etc.
- Modelo de "primitivo correcto en Go": API mínima, sin estado
  global, tests trivializados.

#### DDD — `logging`/`probe`/`blurhash`/`sysmetrics` como librerías focalizadas · **sano**

- Cada uno cubre una sola responsabilidad. Cada uno con test
  adyacente. Cero deps internas.
- `logging.Buffer` es ring in-memory para el panel admin "Logs" — el
  comentario en `Dependencies.LogBuffer` (`router.go`) lo declara
  optional/nil-safe.
- `sysmetrics.Sampler` corre `Start(ctx)` con goroutine + `atomic.Value`
  para snapshot lock-free (per cmd/hubplay/main.go:431). Pattern
  correcto.
- No son olor.

#### EEE — `event.Bus` no expone `Close` · **baja, cosmético**

- El Bus no tiene cleanup. Los handlers viven hasta GC del proceso.
- En producción funciona — el proceso termina completo.
- En tests integrados los Bus se construyen de nuevo con `NewBus`,
  los antiguos son GC'd.
- **No es bug real.** Mencionado sólo para consistencia con el resto
  del proyecto (todo lo demás tiene `Shutdown`/`Close`). Añadir un
  `Bus.Close()` que vacíe el map sería cosmético.

### 8.3 Confirmaciones de `[PENDIENTE]`

- `[PENDIENTE-F8]` Política "no recover de handlers que cuelgan"
  (ADR-008) → confirmado **sano**, sin subscribers bloqueantes en el
  repo actual.
- `[PENDIENTE-F8]` Subscribers que llaman al `unsubscribe()` →
  confirmado **sano** en los 3 SSE handlers (defer unsub correcto).
  Los detectors (segment_detector, segment_fingerprinter) lo retornan
  desde `Start()` y `main.go` lo difiere — el bug es las goroutines
  spawneadas dentro del handler, no el unsub (ya catalogado en olor Y).

### 8.4 Severidades Fase 8

| # | Problema | Severidad | Prerequisito |
|---|----------|-----------|--------------|
| AAA | Comentario `event/bus.go:24-31` desactualizado | Baja | Reescribir |
| EEE | `event.Bus` sin `Close()` | Baja, cosmético | Añadir o documentar como intencional |

### 8.5 Notas operativas

- `event.Bus` es **el primitivo más crítico del proyecto** después de
  `*sql.DB`. ADR-008 sigue cumpliéndose; ningún subscriber bloquea.
- Los 8 paquetes raíz (sin deps internas) representan **el activo
  arquitectónico más valioso**: la base sobre la que el resto se
  construye sin acoplamientos circulares. Cualquier import nuevo en
  uno de ellos debe pasar revisión específica.

---

## Fase 9 · `internal/imaging/`

Cerrada · 2026-05-14 (con apoyo de Agent Explore).

### 9.1 Inventario

8 ficheros, 955 LOC:

| Fichero | LOC | Concepto |
|---|---:|---|
| `trickplay.go` | 254 | Generación de sprites trickplay con ffmpeg |
| `safety.go` | 160 | `SafeGet` + `BlockedIP` (SSRF) + `EnforceMaxPixels` |
| `colors.go` | 148 | HSL bucketing para paleta dominante |
| `ingest.go` | 111 | `IngestRemoteImage` + `AtomicWriteFile` |
| `pathmap/pathmap.go` | 101 | UUID-validated `Store` |
| `thumbnail.go` | 96 | Resize nearest-neighbor + copyFile |
| `validators.go` | 49 | `IsValidKind`, `IsValidContentType` |
| `blurhash.go` | 36 | Wrapper sobre `internal/blurhash` |

### 9.2 Hallazgos

#### FFF — SSRF bypass por redirect no validado · **ALTA**

`internal/imaging/safety.go:124-127`:

```go
client := &http.Client{Timeout: timeout}
resp, err := client.Get(rawURL) //nolint:gosec // target URL vetted above
```

- `SafeGet` valida la IP del host **inicial** con `net.LookupIP` +
  `BlockedIP`.
- El cliente HTTP **no setea `CheckRedirect`**. `client.Get` sigue
  por defecto hasta 10 redirects, sin re-validar IP del destino.
- Vector de ataque real:
  - Atacante controla `evilhost.com` (público).
  - Sirve `302 Location: http://10.0.0.1:9200` (Elasticsearch interno) o
    `http://169.254.169.254/latest/meta-data/...` (AWS metadata).
  - HubPlay sigue el redirect → GET a recurso interno → devuelve body.
- **El patrón correcto YA existe en el repo**:
  `internal/iptv/proxy.go:fetchUpstream` (F5) revalida `isSafeUpstream`
  en cada hop. **Duplicación olvidada en `imaging`.**
- Principio violado: **defensa en profundidad + DRY**.
- Impacto: SSRF a servicios privados; en deployments cloud, exfiltración
  de credenciales IAM via metadata endpoint.
- Refactor:
  ```go
  client := &http.Client{
      Timeout: timeout,
      CheckRedirect: func(req *http.Request, via []*http.Request) error {
          if len(via) >= 10 { return errors.New("too many redirects") }
          host := req.URL.Hostname()
          addrs, err := net.LookupIP(host)
          if err != nil { return err }
          for _, ip := range addrs {
              if BlockedIP(ip) {
                  return fmt.Errorf("%w: redirect to %s", ErrUnsafeURL, ip)
              }
          }
          return nil
      },
  }
  ```
- Severidad: **ALTA**. Es el hallazgo de seguridad más serio de la
  auditoría.

#### GGG — DNS rebinding teóricamente posible · **media**

- `SafeGet` resuelve el host UNA vez con `net.LookupIP` (línea 119) y
  conecta inmediatamente con `client.Get(rawURL)` que vuelve a
  resolver internamente. **Ventana de carrera** entre la resolución
  inicial (validada) y la del transporte HTTP (no validada).
- Requiere: control DNS del atacante + timing preciso (TTL=0 +
  cambio entre milisegundos).
- En la práctica baja probabilidad — `net.Resolver` cachea, Go usa
  el resolver del sistema.
- Mitigación: `Transport.DialContext` custom que se conecte a una IP
  resuelta una sola vez y validada, en vez de re-resolver dentro de
  `http.Client.Get`. Para sólo "media", no merece la pena por ahora.

#### HHH — `pathmap.Read` con `strings.TrimSpace` sin validar raíz · **media**

- `pathmap/pathmap.go:77` lee el contenido del fichero de mapping,
  hace `strings.TrimSpace` y retorna el path como-está.
- No valida que el path resultante esté bajo la raíz `s.dir`.
- En la práctica el caller construye `LocalPath` con
  `filepath.Join(dir, filename)` (ingest.go:78), así que la salida
  es absoluta y bien formada. **Pero el contrato del Store no lo
  enforza** — un caller futuro que use el path como-está está
  expuesto.
- Refactor: `Read` debería retornar **path relativo** (sólo el
  filename) o validar que está bajo la raíz antes de retornar.

#### III — `BlockedIP` variable global mutable · **baja**

- `safety.go:149`: `var BlockedIP = DefaultBlockedIP`.
- Comentario:
  > This is a variable so tests that need to hit an httptest.Server
  > on 127.0.0.1 can temporarily swap it out. Production callers
  > MUST NOT reassign it.
- Justificable (escape hatch para tests), pero un test runtime que
  olvide restaurar deja la guard rota en tests siguientes.
- Refactor opcional: pasar `BlockedIP` como parámetro de `SafeGet`
  (`SafeGet(url, maxBytes, timeout, blockedIPFn)`). Cosmético.

#### JJJ — `fmt.Errorf("status %d", ...)` sin `%w` · **baja**

- `safety.go:132`. Error construido sin wrap. Justificable (no hay
  error subyacente, solo un status code), pero el caller no puede
  `errors.Is` para distinguir status codes específicos.
- Refactor cosmético: declarar `ErrUnexpectedStatus` sentinel y
  `fmt.Errorf("status %d: %w", code, ErrUnexpectedStatus)`.

#### KKK — `colors.ExtractDominantColors` y `blurhash.ComputeBlurhash` sin `context.Context` · **baja**

- Funciones síncronas; no aceptan ctx; no se pueden cancelar.
- En la práctica la entrada está acotada por `EnforceMaxPixels`
  (40 MP) — el trabajo es O(píxeles), bounded.
- Refactor opcional: añadir `ctx` para permitir cancelar si el HTTP
  request padre se cancela.

#### LLL — Animated GIF/APNG bombs no detectados · **baja**

- `EnforceMaxPixels` chequea dimensiones de header, no frame count.
- Una APNG 100×100×1000frames pasa el guard.
- Mitigado por `MaxUploadBytes = 10 MiB`.
- Refactor: detectar APNG chunks si content-type es `image/png` y
  rechazar si frame count > N.

### 9.3 Severidades Fase 9

| # | Olor | Severidad |
|---|------|-----------|
| FFF | SSRF redirect bypass en `SafeGet` | **Alta** |
| GGG | DNS rebinding teórico | Media |
| HHH | `pathmap.Read` sin validar raíz | Media |
| III | `BlockedIP` var global mutable | Baja |
| JJJ | Status code sin `%w` | Baja |
| KKK | `blurhash`/`colors` sin ctx | Baja |
| LLL | Animated bombs no detectados | Baja |

### 9.4 Sano

- Atomic writes (`AtomicWriteFile`) correctos para POSIX.
- `pathmap.Store` con `validID()` que obliga UUID — path traversal
  bloqueado.
- Sin symlink dereference.
- `BlockedIP` cubre loopback + RFC1918 + RFC4193 + link-local
  (incluye `169.254.169.254`) + multicast + unspecified.
- `EnforceMaxPixels` (40 MP cap) y `MaxUploadBytes` (10 MiB) bien
  aplicados.
- 61/66 `fmt.Errorf` usan `%w` correctamente.

---

## Fase 10 · `internal/api/{middleware,csrf,security_headers}` + `apperror`

Cerrada · 2026-05-14. Total: 526 LOC.

### 10.1 Inventario

| Fichero | LOC | Concepto |
|---|---:|---|
| `internal/api/csrf.go` | 112 | Double-submit cookie pattern |
| `internal/api/security_headers.go` | 100 | CSP + HSTS + frame-ancestors + CORP |
| `internal/api/apperror/apperror.go` | 84 | Writer canónico de errors HTTP |
| `internal/api/middleware.go` | 34 | `RequestLogger` |

### 10.2 Hallazgos

#### MMM — `apperror.recorder` global mutable · **baja**

- `apperror.go:39`: `var recorder = func(code string) {}`.
- `SetRecorder(fn)` lo modifica sin lock.
- En producción se setea una vez en `router.go` (boot) y no se vuelve
  a tocar. Race teórica sólo en tests integrados que tocan recorder
  en paralelo.
- Justificable como singleton de observability. Documentado.
- Refactor opcional: pasar el recorder como parámetro a `Write` para
  eliminar el global.

#### NNN — CSRF token nunca rota · **baja**

- `csrf.go:65-71`: cookie con `MaxAge: 86400`. **El token no se rota
  tras login.** Vive 24 h.
- No es agujero (double-submit funciona; el atacante necesita XSS
  para leer el token de la cookie), pero **post-login rotation** es
  defensa en profundidad estándar (mitiga XSS-then-CSRF).
- Refactor: rotar el token en `auth.Login` success. ~5 LOC.

#### OOO — `RequestLogger` siempre `Info` · **baja**

- `middleware.go:23-31`: log único con `logger.Info`, incluso para
  responses 500.
- 5xx idealmente serían `Error`, 4xx `Warn`. Trivial pero impacta
  ergonomía operacional (grep / alerting por nivel).
- Refactor: branch por `ww.Status()` → nivel.

### 10.3 Sano

- **CSRF double-submit**: implementación correcta (`csrf.go`).
  Gateado por presencia de cookie `hubplay_access` — endpoints
  públicos (login, refresh, setup) no son sujetos. Comentario lo
  documenta.
- **Security headers**: CSP estrecha, comentada, mantenida en código
  (no en nginx, ADR-007). HSTS condicional sobre HTTPS. Doble
  protección `frame-ancestors 'none'` + `X-Frame-Options DENY`.
- **`script-src 'self'`** sin `unsafe-inline` en JS — bien.
- **`apperror.Write`** centraliza envelope + Retry-After + request_id
  + recorder. Cero `http.Error` directos en handlers (verificable con
  grep).
- Cero `init()` funcs en `api/`.

### 10.4 Severidades

| # | Olor | Severidad |
|---|------|-----------|
| MMM | `apperror.recorder` global | Baja |
| NNN | CSRF token sin rotación post-login | Baja |
| OOO | `RequestLogger` siempre Info | Baja |

---

## Fase 11 · `config` + `setup` + `retention`

Cerrada · 2026-05-14. Total: 923 LOC.

### 11.1 Inventario

| Paquete | Ficheros | LOC |
|---|---:|---:|
| `internal/config/` | 4 | 606 |
| `internal/setup/` | 1 | 195 |
| `internal/retention/` | 1 | 122 |

### 11.2 Hallazgos

#### PPP — `generateSecret()` produce data muerta tras primer boot · **baja**

`config.go:Load` genera `cfg.Auth.JWTSecret` si está vacío:

```go
if cfg.Auth.JWTSecret == "" {
    cfg.Auth.JWTSecret = generateSecret()
}
```

- En el primer boot, `Bootstrap(ctx, repo, clk, seed)` (auth/keystore)
  inserta una `db.SigningKey` con `secret = seed` (el generado).
- En boots siguientes, `Bootstrap` ve `len(existing) > 0` → retorna
  existente; el secret recién generado **se descarta**.
- Resultado: `cfg.Auth.JWTSecret` post-bootstrap es **dead data**.
  El JWT real vive en la DB vía `KeyStore`.
- **No es bug**, pero invita a confusión: alguien que vea
  `cfg.Auth.JWTSecret` en un panel admin podría asumir que
  determina la firma. No lo hace.
- Refactor: renombrar a `cfg.Auth.JWTSecretBootstrapOnly` o eliminar
  el campo del config y obligar a tener al menos una key seedable
  via env (`HUBPLAY_AUTH_JWT_SECRET`).

#### QQQ — `setup.BrowseDirectories` filter `isSensitivePath` no verificado · **a confirmar**

- `setup/service.go:64` bloquea paths "sensitive" antes de listar.
- **No leí la lista completa de `isSensitivePath`**.
- Riesgo: si la lista es incompleta (no incluye `/etc`, `/var/log`,
  `/proc`, `/sys`), el wizard podría leer info del sistema.
- Acción rápida en intervención: `grep -A 30 isSensitivePath`.

### 11.3 Sano (modelos del proyecto)

- **`config.Save` atomic write con `Chmod 0600` + rename**:
  `persist.go:25-60`. Modelo perfecto para escrituras atómicas en el
  proyecto. Documentado con razonamiento explícito (Plex/Jellyfin
  convention).
- **`config.Preflight`**: write probe real (no permission bits) —
  comentario lo justifica explícitamente (Docker bind mounts, ACL,
  immutable flags). Bien diseñado.
- **`RestartRequester`**: idempotente con `atomic.Bool.CompareAndSwap`,
  delay 100ms para flush JSON response antes de cancelar.
  `restart.go:49-56`.
- **`retention.Runner`**:
  - 2 interfaces estrechas (`EPGCleaner`, `AuditPruner`) — minimum
    capability surface.
  - nil-safe per dependency.
  - `Start/Stop` con stopCh + ctx.
  - `sweep` ejecuta epg y audit independientes — un fallo no para el otro.
  - **Modelo de "lifecycle correcto + nil-safe + sub-features
    independientes"**.
- **`config.Validate`**: usa `errors.Join` (Go 1.20+ idiomático).
- Cero `init()` funcs.
- Env overrides con prefijo `HUBPLAY_*` documentados.

### 11.4 Severidades

| # | Olor | Severidad |
|---|------|-----------|
| PPP | `JWTSecret` config dead-data post-bootstrap | Baja |
| QQQ | `isSensitivePath` no auditado | A confirmar |

---

## Fase 12 · Migraciones (`migrations/sqlite/` + `migrations/postgres/`)

Cerrada · 2026-05-14 (con apoyo de Agent Explore + verificación manual).

### 12.1 Inventario

- **43 migraciones SQLite + 43 Postgres en paridad** — verificado por
  el agente.
- Goose-managed.
- `migrations.go` en raíz expone `Migrations(driver)` que devuelve el
  FS embedido correcto.

### 12.2 Hallazgos

#### RRR-mig — Política "up-only" VIOLADA · **media-alta**

- **15 migraciones contienen secciones `-- +goose Down`**:
  - 004, 013-014, 015-017, 025, 027, 029, 032-033, 034-036, 038-039,
    042-043.
- Esto contradice la política declarada (ADR/conventions).
- Impacto: alguien que invoque `goose down` puede ejecutar SQL
  destructivo que no debería existir. Operador hostil o accidental
  ("hice down por error en producción") tiene una arma cargada.
- Refactor: eliminar los bloques `-- +goose Down` de las 15
  migraciones (cambio mecánico).

#### SSS-mig — `api_keys.created_by` FK sin `ON DELETE CASCADE` · **media**

- `migrations/sqlite/001_initial_schema.sql:40`: la FK existe sin
  cláusula de cascada.
- Si un usuario se borra, `api_keys` quedan huérfanas.
- Mitigado parcialmente por DDDD-mig (la tabla `api_keys` parece **no
  usarse** — sin queries en `internal/db/queries/`).
- Refactor: si la tabla se mantiene, añadir `ON DELETE CASCADE` en
  una migración nueva (no editar mig 001).

#### TTT-mig — INTEGER en SQLite vs BIGINT en Postgres para columnas grandes · **media**

- `items.size`, `items.duration_ticks`, `user_data.position_ticks`,
  `chapters.start_ticks/end_ticks`.
- SQLite acepta `INTEGER` para 64-bit, pero el contrato visual
  diverge del schema Postgres y produce confusión cross-team.
- Impacto práctico: bajo (películas <20 GB en bytes ~ 2.1·10¹⁰ <
  2^31·2). Pero la sintonía implícita es contractual.
- Refactor opcional: declarar `INTEGER` SQLite con comment de
  `BIGINT semantic`, o usar `BIGINT` explícito en SQLite (SQLite
  acepta el alias).

#### UUU-mig — Índice composite `channels(library_id, number)` ausente · **media**

- Query: `WHERE library_id = ? ORDER BY number, name` (ver
  `internal/db/queries/channels.sql`).
- Índice actual: `idx_channels_library` (solo `library_id`).
- Planner usa el índice pero ordena en memoria. Con 5 000+ canales
  por library la `ORDER BY` puede ser costosa.
- Refactor: añadir
  `CREATE INDEX idx_channels_library_number ON channels(library_id, number)`
  en migración nueva.

#### VVV-mig — Tablas declaradas sin queries en uso · **baja**

- `api_keys` (mig 001): sin `queries/api_keys.sql`. Dead schema o
  features futuras.
- `activity_log` (mig 001): SÍ se usa **pero con SQL raw inline en
  `internal/api/handlers/system.go`** (olor T de F3). Reconciliar
  con el plan: crear `ActivityLogRepository` (parte de It. 2 del plan)
  resuelve el odor visible aquí.
- Refactor: `api_keys` decidir si se borra (en migración nueva, no
  retroactiva) o si se materializa con su repo.

### 12.3 Hallazgos retirados (verificación manual)

- ❌ **`PRAGMA foreign_keys = ON` enforced**: confirmado en
  `internal/db/sqlite.go:66` — DSN incluye
  `&_pragma=foreign_keys(ON)`. El agente lo flagueó como falso
  positivo. **No es olor.**

### 12.4 Severidades

| # | Olor | Severidad |
|---|------|-----------|
| RRR-mig | Política up-only violada (15 migraciones con `Down`) | Media-alta |
| SSS-mig | `api_keys.created_by` FK sin CASCADE | Media |
| TTT-mig | INTEGER vs BIGINT divergencia SQLite/PG | Media |
| UUU-mig | Falta índice composite en `channels` | Media |
| VVV-mig | `api_keys` schema sin queries (dead) | Baja |

### 12.5 Sano

- 43↔43 paridad sqlite/postgres.
- `PRAGMA foreign_keys = ON` enforced vía DSN.
- ADR-011 partial UNIQUE indexes aplicado (mig 019).
- ADR-001 reaffirmation cumplida (sweep de raw → sqlc).
- Cero migraciones con datos hardcoded sensibles.

---

## Fase 13 · Transversales

Cerrada · 2026-05-14. Cubre lo del brief original no mapeado a
paquete: error wrapping, `context.Context` propagation, deadlocks,
estado global, naming, tests frágiles.

### 13.1 Error wrapping (105 `fmt.Errorf` sin `%w`)

- **Muestreo manual**: los 105 son **errores construidos desde
  scratch**, no wrappers perdidos.
- Ubicaciones típicas:
  - `internal/provider/{tmdb,fanart,opensubtitles}.go`: errores
    construidos de HTTP status codes ("status 429", "rate limited").
  - `internal/config/config.go`: validation errors construidos
    desde violaciones del schema YAML.
  - `internal/auth/keystore.go`: "no active signing key", "no primary
    key" — sentinels semánticos.
- **Veredicto**: el proyecto **SÍ usa `%w` correctamente** donde hay
  error subyacente. No es olor.
- **Excepción real**: `imaging/safety.go:132`
  `fmt.Errorf("status %d", code)` → JJJ (catalogado en F9).
- **Severidad: no-olor general**. Buen patrón.

### 13.2 `context.Context` propagation

- **29 hits de `context.Background()` en producción**. Clasificación:

| Tipo | Hits | Veredicto |
|---|---:|---|
| Boot / shutdown root context | 5 | Legítimo (main.go, db.Open, hwaccel) |
| Background workers con `bgCtx` propio | 4 | Legítimo (library.Service, federation.Manager, federation.Auditor, sysmetrics) |
| Detached intencional para fire-and-forget | 5 | **Olores DD, GGGG** (catalogados) |
| Test helpers | 1 | OK |
| Transcoder.Start con timeout propio | 2 | Cuestionable (no toma ctx del caller) |
| iptv.proxy.reportOutcome | 1 | OK — recibe `ctx` y `fetchCtx` deliberadamente (verificado) |
| Migrator post-migration tweak | 1 | OK |
| pg-smoke standalone | 1 | OK |

#### GGGG — Detached goroutines en handlers `iptv_admin.go` · **media** (nuevo)

- `iptv_admin.go:101, 200` lanzan `go func()` con
  `context.WithTimeout(context.Background(), 2*time.Minute)`.
- No hay WaitGroup, no hay tracking.
- Mismo patrón que olor DD (F5) pero **en HTTP handler**, no en
  service. Si shutdown llega durante un refresh M3U async, escribe
  a DB cerrada.
- Refactor: extender `iptv.Service.bgWG` (propuesto en olor DD) y
  registrar las goroutines de admin ahí.

### 13.3 Deadlocks — heurística de locks anidados

Inspección manual de funciones con múltiples `Lock()`:

- **`stream.Manager.RestartSessionAt`** (`manager.go:600+`): **OK**.
  El pattern es:
  ```
  m.mu.Lock(); ms := m.sessions[key]; m.mu.Unlock();
  ms.restartMu.Lock(); defer ms.restartMu.Unlock();
  ```
  Mutex manager liberado antes de adquirir per-session. Sin
  anidamiento real.
- **`library.Service.Scan`**: `mu` adquirido dos veces (set + clear
  flag), una al inicio y otra en defer al final. Sin anidamiento
  con otro mutex. **OK**.
- **`federation.Manager`**: dos mutexes (`peerCache` RWMutex +
  `streamSessions` Mutex). Por contrato, no se anidan (uno por
  hot path distinto). Comentado explícitamente (VV de F7). **OK**.

**Veredicto**: no detecto deadlocks en inspección estática.
Confirmación final requiere `go test -race -count=10 ./...`.

### 13.4 Estado global mutable

Inventario de `var` mutables exportadas:

| Variable | Ubicación | Veredicto |
|---|---|---|
| `iptv.AllCategories` | `categories.go:29` | Sintaxis (Go no permite `const []`); inmutable de facto |
| `imaging.ValidKinds` | `validators.go:11` | Array (no slice) → inmutable de facto |
| `imaging.BlockedIP` | `safety.go:149` | **III** — test override doc |
| `db.AbandonedAfter` | `user_data_repository.go:336` | ADR-004 lo declara mutable explícito |
| `stream.Profiles` | `profiles.go:14` | **WWW-trans** — map exportado mutable |
| `apperror.recorder` | `apperror.go:39` | **MMM** — global del observability |

#### WWW-trans — `stream.Profiles` map exportado mutable · **baja**

- `var Profiles = map[string]Profile{...}` accedido por 6 sitios
  externos (handlers, hls.go, decision.go) **como lectura**.
- Pero cualquier código del proyecto podría hacer
  `stream.Profiles["foo"] = Profile{...}` y romper otras lecturas
  concurrentes.
- Refactor: convertir a `func Profile(name string) (Profile, bool)`
  con map privado. ~10 LOC. Elimina state global mutable de la API
  pública de `stream`.

### 13.5 Naming idiomático

- **`auth.AuthToken`** (`auth/service.go`): stutter. Idiomático sería
  `auth.Token`. Olor menor.
- `provider.MetadataProvider` / `provider.EpisodeMetadataProvider`:
  stutter aceptable — son interfaces centrales del paquete; el
  Go style guide tolera stutter cuando el tipo es el "core" del
  paquete.
- `event.Event`, `clock.Clock`, `config.Config`, `probe.Prober`:
  stutter pero idiomático (cada paquete tiene UN tipo principal con
  ese nombre).
- **Receivers**: consistentes (`s` para Service, `m` para Manager,
  `h` para Handler, `r` para Repository, `q` para Queries del sqlc).
  Sano.
- **Interfaces sin sufijo `-er`**: 15 detectadas. Justificable cuando
  la interface tiene múltiples métodos (`Repo`, `EventBus`,
  `AuthService`, …). Go idiomático prefiere `-er` para interfaces
  de un método; aquí casi todas tienen 3+, así que el sufijo no
  aplica.

#### XXX-trans — `auth.AuthToken` stutter · **baja**

- Renombrar a `auth.Token`. Mecánico con `gopls rename`.

### 13.6 Tests sin `t.Parallel()` (8/122 ≈ 6.6%)

- Razón mayor: SQLite con shared file no paraleliza bien. Pero
  tests con DB en memoria o con mocks SÍ podrían.
- **YYY-trans (baja)**: oportunidad de speed-up del CI con
  `t.Parallel()` selectivo en tests que no compartan filesystem ni
  DB compartida.

### 13.7 init() funcs

- **Cero** init() funcs en `internal/` ni `cmd/`. **Sano** — sin
  side effects de import.

### 13.8 Resumen Fase 13

| # | Olor | Severidad |
|---|------|-----------|
| GGGG | Detached goroutines en `iptv_admin.go` | Media |
| WWW-trans | `stream.Profiles` map mutable exportado | Baja |
| XXX-trans | `auth.AuthToken` stutter | Baja |
| YYY-trans | Tests sin `t.Parallel()` (8/122) | Baja |

Confirmaciones positivas del brief que faltaban:
- ✅ Error wrapping correcto en general (105 sin `%w` son legítimos).
- ✅ No detecto deadlocks por inspección estática.
- ✅ Cero `init()` funcs.
- ✅ Naming consistente con buenas prácticas.

Hallazgos del brief que siguen **pendientes de tooling**, no de
inspección:
- ❌ `go test -race -count=10 ./...` para confirmar empíricamente
  race conditions.
- ❌ `goleak.VerifyNone(t)` integrado en tests para confirmar
  ausencia de leaks.
- ❌ `govulncheck` para CVEs en deps.

---

## Fase 14 · Calidad a nivel código / función

Cerrada · 2026-05-14. Cubre lo que NO se ve a nivel struct/paquete
pero impacta "esto parece código de profesional o de prototipo":
funciones gigantes individuales, firmas con muchos parámetros,
boolean params, boilerplate repetido, magic numbers, tests
frágiles, naming local.

### 14.1 Funciones gigantes individuales (>100 LOC)

25 funciones superan 100 LOC. Top 10:

| # | Función | LOC | Diagnóstico |
|---|---|---:|---|
| **F14-1** | `api.NewRouter` | **626** | Wiring de 219 rutas en un único `Route()` callback. Resuelto por **G/H** (sub-routers). |
| **F14-2** | `stream.BuildFFmpegArgs` | 192 | Switch gigante por hwAccel/encoder/copy/tonemap. Refactor en F14-2-a. |
| **F14-3** | `iptv.RunRefreshM3U` | 177 | Pipeline lineal: fetch → parse → diff → persist → publish. Split por etapa. |
| **F14-4** | `stream.startSessionSlow` | 175 | Path completo de start de sesión. Split por sub-decisión. |
| **F14-5** | `db.NewHomeRepository` | 175 | **Olor real**: constructor con queries SQL inline. Mover queries a fichero `.sql` o constantes. |
| **F14-6** | `iptv.RefreshEPG` | 172 | Mismo patrón pipeline que F14-3. |
| **F14-7** | `scanner.enrichMetadata` | 157 | Sub-caso de W. Split por content type. |
| **F14-8** | `handlers.ContinueWatching` | 153 | Sub-caso de Z; consume rating-cap + filter + format. |
| **F14-9** | `db.ItemRepository.List` | 150 | Construye WHERE dinámico con `where += " AND ..."` repetido (ver F14-9-a). |
| **F14-10** | `provider.TMDbProvider.GetMetadata` | 145 | Fan-out de calls TMDb. Acceptable para "external client" pero split por endpoint mejoraría tests. |

#### F14-2-a · `BuildFFmpegArgs` 192 LOC + 13 params · **alta**

- 13 parámetros + body lineal con 6+ ramas de hwAccel/encoder.
- Llamada en 30+ sites de tests + 2 en producción.
- Refactor:
  ```go
  type TranscodeRequest struct {
      Input, OutputDir       string
      Profile                Profile
      StartTime              float64
      HWAccel                HWAccelType
      Encoder, Libx264Preset string
      CopyVideo, CopyAudio   bool
      ToneMap                bool
      StartSegmentNumber     int
      AudioStreamIndex       int
      BurnSub                *BurnSubtitleSpec
  }
  func BuildFFmpegArgs(req TranscodeRequest) []string
  ```
- Reduce 13 → 1 param. Permite renombrar campos sin tocar 30+ callers
  de test. Permite añadir nuevos params sin breaking change.
- Severidad **alta** porque `Transcoder.Start` y `RestartAt` tienen
  la **misma firma de 11 params** (subset de la de BuildFFmpegArgs).
  Patrón replicado tres veces.

#### F14-9-a · `where += " AND ..."` para queries dinámicas · **media**

- `db/item_repository.go:233-240`: construye WHERE concatenando
  strings con `+=`.
- Olor de "código de prototipo": no usa `strings.Builder` ni
  `[]string{...}; strings.Join`. Performance OK para tamaños
  actuales, pero olor visible.
- Refactor:
  ```go
  conds := []string{"1=1"}
  args := []any{}
  if filter.LibraryID != "" {
      conds = append(conds, "library_id = ?")
      args = append(args, filter.LibraryID)
  }
  where := strings.Join(conds, " AND ")
  ```

### 14.2 Funciones con 7+ parámetros (anti-pattern)

| Función | Params | Sugerencia |
|---|---:|---|
| `stream.BuildFFmpegArgs` | 13 | `TranscodeRequest` struct (ver F14-2-a) |
| `handlers.NewItemHandler` | 13 | Cubierto por **P** (split god-handler) |
| `stream.Transcoder.Start` | 11 | `TranscodeRequest` |
| `stream.Transcoder.RestartAt` | 11 | `TranscodeRequest` |
| `stream.Manager.startSessionSlow` | 9 | `StartSessionRequest` struct |
| `stream.Manager.StartSession` | 8 | `StartSessionRequest` struct |
| `handlers.NewIPTVHandler` | 7 | Cubierto por **CC** (split paquete iptv) |
| `stream.NewTranscoder` | 7 | `TranscoderConfig` struct |
| `federation.Manager.RecordProgress` | 7 | `ProgressUpdate` struct |

**F14-2-b · severidad: media-alta**. Aplicable a 9 firmas. Cada
una es ~5 min con `gopls`.

### 14.3 Booleans seguidos en firmas (anti-pattern)

- `Transcoder.Start(..., copyVideo, copyAudio, toneMap bool, ...)` —
  3 booleanos seguidos. Caller call: `transcoder.Start(..., true,
  false, true, ...)`. **Ilegible** en review.
- 1 hit con tres booleans seguidos (`stream/transcode.go`). Otros
  con 1-2 boolean param mezclados con otros tipos — tolerables.
- **F14-3-a · severidad: media**. Resuelto naturalmente por
  `TranscodeRequest` (F14-2-a).

### 14.4 `panic()` en código de producción

- **Único hit**: `internal/iptv/prober_worker.go:87`:
  ```go
  panic("iptv.NewProberWorker: nil dependency")
  ```
- Justificable como "programmer error caught at construction time".
- **Pero**: inconsistente con el resto del proyecto. `federation.NewManager`
  retorna `(nil, err)` cuando init falla; el caller en `main.go`
  hace fail-soft. Aquí `panic` rompe el patrón.
- **F14-4-a · severidad: baja**. Refactor: retornar `(*ProberWorker,
  error)` con `errors.New("nil dependency")`. ~5 LOC.

### 14.5 Helpers con nombres similares en distintos paquetes

| Helper | Paquete | Significado |
|---|---|---|
| `isSafeMethod(method)` | `api/csrf.go` | HTTP method en `{GET,HEAD,OPTIONS}` |
| `isSafeUpstream(url)` | `iptv/proxy.go` | URL outbound segura (SSRF guard) |
| `IsSafePathSegment(s)` | `imaging/safety.go` | Path segment sin `..`/`/` |
| `BlockedIP(ip)` | `imaging/safety.go` | IP en rango privado/loopback |
| (futuro) | `imaging/safety.go::SafeGet CheckRedirect` | El olor **FFF** |

- **No es duplicación funcional** (cada uno valida algo distinto).
- **Es inconsistencia de naming**:
  - `is*Safe*` (camelCase) cuando es package-private.
  - `IsSafe*` cuando es exported.
  - `BlockedIP` rompe el patrón (no usa "Safe").
- **F14-5-a · severidad: baja**. Convención: documentar en
  `conventions.md` el patrón "Is{X}Safe / Is{X}Blocked" según
  dirección (whitelist vs blacklist).

### 14.6 Boilerplate repetitivo en handlers

#### F14-6-a · `map[string]any{"data": ...}` wrapping · **media**

- **100+ sites** que hacen
  `respondJSON(w, status, map[string]any{"data": payload})`.
- Inconsistencia: a veces `{"data": x}`, a veces el payload
  directo, a veces `{"items": x, "count": n}`. Sin convención de
  envelope.
- **Refactor**: helper:
  ```go
  // respondData escribe el envelope canonico {data: payload}.
  func respondData(w http.ResponseWriter, status int, data any) {
      respondJSON(w, status, struct {
          Data any `json:"data"`
      }{data})
  }
  ```
- Elimina ~100 sites de `map[string]any{"data": ...}`. **Mismo wire
  shape**.
- Adicional: helper `respondPage(w, status, items, total)` para el
  patrón paginado.

#### F14-6-b · `chi.URLParam(r, "id")` boilerplate · **baja**

- 15 ficheros repiten:
  ```go
  id := chi.URLParam(r, "id")
  if id == "" {
      apperror.Write(w, ctx, domain.NewBadRequest("missing id"))
      return
  }
  ```
- **Refactor**: helper:
  ```go
  // requireParam extrae un URL param y responde 400 si falta.
  // Retorna (value, ok); el handler hace `return` si !ok.
  func requireParam(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
      v := chi.URLParam(r, name)
      if v == "" {
          apperror.Write(w, r.Context(),
              domain.NewBadRequest("missing "+name))
          return "", false
      }
      return v, true
  }
  ```
- Elimina ~25 sites de boilerplate.

### 14.7 Logging repetitivo: `library_id` 145 veces como campo

- 145 hits de `"library_id"` como campo del logger.
- **Patrón correcto YA existe**: `library/scheduler.go:33`
  `logger.With("module", "scheduler")` enriquece el logger una vez.
- **No se aplica consistentemente** en hot paths. Cada handler /
  método pone el campo manualmente.
- **F14-7-a · severidad: baja**. Convención: cada feature crea su
  logger sub-named con `.With()`:
  ```go
  // En el service:
  func (s *Service) Scan(ctx, id) {
      log := s.logger.With("library_id", id)
      log.Info("scan started")
      ...
  }
  ```
- Reduce ruido + permite filtrado por feature en grep.

### 14.8 Tests frágiles con `time.Sleep` arbitrarios

10 ficheros de test hacen `time.Sleep(10/50/100ms)` para esperar a
goroutines. Patrón:

```go
go someWorkflow()
time.Sleep(50 * time.Millisecond) // hopefully enough
assert.Equal(...)
```

Anti-pattern: **CI flakes** en máquinas lentas; **slow tests** en
máquinas rápidas.

**Patrón correcto YA existe en el proyecto**:
- `library.FSWatcher.walksDone atomic.Int64` se expone para tests
  sin sleep.
- Test seam pattern documentado en `library/watcher_test.go`.

Sites con `Sleep`:
- `retention/runner_test.go:102,123`
- `federation/client_retry_test.go:150, stream_test.go:107`
- `provider/httpcache_test.go:236`
- `auth/service_test.go:669`
- `api/handlers/iptv_test.go:1159,1245`
- `library/watcher_test.go:148,177` (con comentario "// initial walk")
- `iptv/scheduler_test.go`

**F14-8-a · severidad: media**. Refactor: cada uno con channel /
WaitGroup expuesto vía test seam. ~30 min por test.

### 14.9 Magic numbers (HTTP cache headers)

`86400` (1 día), `3600` (1 hora), `604800` (1 semana) hardcoded en
12+ sites de Cache-Control:

- `handlers/iptv_channels.go:363`
- `handlers/federation_stream.go:309, 396`
- `handlers/image.go:441`
- `handlers/people.go:71`
- `handlers/openapi.go:65`
- `handlers/stream.go:420, 643`
- `api/csrf.go:62` (cookie MaxAge)

**F14-9-a · severidad: baja**. Refactor: constantes nombradas en
un fichero compartido `handlers/cache.go`:
```go
const (
    cacheOneHour = "max-age=3600"
    cacheOneDay  = "public, max-age=86400, stale-while-revalidate=604800"
    cacheOneWeek = "public, max-age=604800"
)
```

### 14.10 Funciones que retornan 4 valores

| Función | Retornos | Refactor |
|---|---|---|
| `handlers.parseBulkScheduleRequest` | `(ids, from, to, ok)` | `BulkScheduleQuery{}, error` |
| `federation.BrowsePeerItems` | `([]*Item, count, partial, error)` | `BrowseResult{}, error` |
| `scanner.extractEpisodeFromFilename` | `(season, episode int, title, ok)` | `*EpisodeInfo, ok` |
| `db.FederationRepository.ListCachedItems` | `(items, count, lastSync, error)` | `CachedItemPage{}, error` |

**F14-10-a · severidad: baja**. Múltiples retornos son OK en Go,
pero 4+ valores invitan a errores de orden en callers. Struct
nombrada por retorno mejora legibilidad.

### 14.11 Código dead-ish

- **`internal/db/scan_helpers.go`**: 52 LOC con `itemNullables`
  struct. Comentario dice "dead helpers were removed when the
  linter flagged them" — pero el struct todavía existe. Verificar
  uso.
- **`db.api_keys` schema**: VVV-mig (F12) — tabla + índices sin
  repo ni queries. Confirmado dead.
- **`internal/db/queries-postgres/`**: directorio listado en F1
  pero no analizado en profundidad. ¿Vivo o stub?

**F14-11-a · severidad: baja**. Pase de `staticcheck -unused` antes
del refactor para detectar más dead code.

### 14.12 Concatenación con `+=` vs `strings.Builder`

- 6 sites en producción usan `+=` para concatenar strings (algunos
  ya catalogados en F14-9-a):
  - `provider/fanart.go:269` — query string build.
  - `handlers/iptv_playback_failure.go:153` — error message.
  - `db/item_repository.go:233-240` — WHERE clause (F14-9-a).
- Performance OK para tamaño actual (1-5 concatenaciones), pero
  olor visible al lector.
- **F14-12-a · severidad: baja**. Convención: usar
  `strings.Builder` cuando >3 concatenaciones; `fmt.Sprintf` o
  string + cuando ≤2.

### 14.13 Severidades Fase 14

| # | Olor | Severidad |
|---|------|-----------|
| F14-1 | `api.NewRouter` 626 LOC | Alta (resuelto por G/H) |
| F14-2-a | `BuildFFmpegArgs` 13 params + Start/RestartAt mismo patrón | Alta |
| F14-5 | `db.NewHomeRepository` 175 LOC con SQL inline | Media (resuelto por L) |
| F14-6-a | 100+ sites de `map[string]any{"data": ...}` | Media |
| F14-7-a | Logging `library_id` repetido 145 veces | Baja |
| F14-8-a | Tests con `time.Sleep` arbitrarios (10 sitios) | Media |
| F14-2-b | 9 funciones con 7+ params (struct params) | Media-alta |
| F14-3-a | 3 booleans seguidos en `Transcoder.Start` | Media (subset F14-2-a) |
| F14-4-a | `panic()` en `prober_worker.go:87` | Baja |
| F14-5-a | Inconsistencia `is*Safe*` / `IsSafe*` / `Blocked*` | Baja |
| F14-6-b | `chi.URLParam` boilerplate (25 sites) | Baja |
| F14-9-a | Magic numbers cache (12 sites) | Baja |
| F14-10-a | 4-valor returns (4 funciones) | Baja |
| F14-11-a | `scan_helpers.go` posiblemente dead | Baja |
| F14-12-a | `+=` para concatenación (6 sites) | Baja |

### 14.14 Notas operativas: qué se puede borrar

**Líneas que se eliminan sin pérdida de funcionalidad** (estimado):

| Acción | LOC reducidos |
|---|---:|
| `respondData` helper + reemplazar 100 sites | ~200 LOC |
| `requireParam` helper + reemplazar 25 sites | ~100 LOC |
| `cacheOneDay`/`cacheOneHour` constantes (12 sites) | ~24 LOC |
| `TranscodeRequest` struct (3 funciones con 11-13 params) | ~50 LOC |
| `db.api_keys` schema + índices removidos | ~30 LOC |
| `scan_helpers.go itemNullables` si dead | ~52 LOC |
| Borrado de `-- +goose Down` blocks (RRR-mig, 15 ficheros) | ~150 LOC |
| **Total estimado** | **~600 LOC eliminados sin perder funcionalidad** |

Es el **0.8% del backend Go** (72 319 LOC). No es revolucionario,
pero combinado con los splits estructurales (P, Z, CC, J) el
código resultante baja un ~5-8% en LOC con MEJOR claridad. Eso es
lo que diferencia "código de prototipo" de "código de
profesional".

---

## Fase 15 · Calidad del test suite

Cerrada · 2026-05-14 (con apoyo de Agent Explore + verificación de
veredictos críticos).

### 15.1 Inventario

- 137 ficheros `_test.go` en `internal/` (≈ 1 288 tests totales).
- Veredicto global del agente: **MEDIA. Suite funcional pero frágil
  bajo estrés (CI con carga alta, `-parallel`).**
- 105 usos de `t.TempDir()` ✓ — consistente.
- 154 usos de `testutil.NewTestDB` ✓ — DB real preferida sobre mock
  cuando aplica.
- 79 ficheros con `t.Helper()` ✓.
- `go test -race` activo en CI ✓.

### 15.2 Hallazgos

#### F15-1 — `time.Sleep` no determinista en 19 ficheros · **alta**

- Ejemplos críticos:
  - `library/watcher_test.go:136` — `Sleep(100ms)` antes de FS event.
  - `library/watcher_test.go:186` — `Sleep(500ms)` esperando debounce.
  - `retention/runner_test.go` — múltiples `Sleep(50ms/100ms)` en
    loops de espera.
- El `Sleep(500ms)` es el más peligroso: en CI con CPU contendida o
  runner lento, el debounce no llega a disparar y el test falla.
- Modelo correcto YA existe en `library.FSWatcher.walksDone
  atomic.Int64` que se expone para tests sin sleep.
- **Refactor**: cada test seam usa channel/WaitGroup o
  `clock.Mock.Advance()`. ~30 min por test.
- **Severidad alta** porque genera CI flakes que cuestan dev-time
  semanal.

#### F15-2 — `time.Now()` no inyectado (231 instancias) · **media**

- Sólo 2 sitios usan `clock.Mock`: `federation/stream_test.go:14` y
  `auth/service_test.go`.
- Los 231 hits restantes hacen `time.Now()` directo en tests.
- Impacto: tests de TTL/expiración/cleanup no son reproducibles.
  Cualquier test que pase un timestamp como "now" y luego espera
  que algo expire en `30 * time.Minute` está atado a wall-clock.
- **Refactor**: `clock.Clock` ya existe en `internal/clock` —
  inyectar en cada service/repo que toque tiempo, y mock-ear en
  test. ~1 día de migración mecánica.

#### F15-3 — Race en setup vía `waitForCount` con polling sleep · **media**

- `auth/service_test.go:270-275`:
  ```go
  for time.Now().Before(deadline) {
      if len(got) >= want { return got }
      time.Sleep(10 * time.Millisecond)
  }
  ```
- Sub-caso de F15-1, pero patrón replicable. Polling en lugar de
  event sync.
- Impacto: passa en serial, puede flakear en `-parallel`.
- **Refactor**: `chan struct{}` cerrado cuando se cumple condición.

#### F15-4 — Goroutine tests dependientes de timing · **media**

- `federation/stream_test.go:92-120` `TestManager_CloseStopsSweeperGoroutine`:
  spawnea 25 managers en loop, chequea `runtime.NumGoroutine()`
  después de `Close()`. Si el sweeper tarda más de lo esperado en
  unwind, falsifies el conteo.
- `event/bus_test.go` — publica eventos, espera con `Sleep(50ms)`.
- **Refactor**: `Close()` retorna sólo cuando todas las goroutines
  unwind (ya lo hace correctamente — `<-m.sweepDone`). El test
  puede llamar directamente a la primitiva sin `Sleep`.

#### F15-5 — God-handlers con mocks que mockean 16 métodos · **media**

- `handlers/library_test.go:28-180` declara `libFakeService` que
  implementa **~16 métodos** de la interface `LibraryService`.
- `handlers/items_test.go` similar (644 LOC).
- Impacto: si el handler comienza a llamar un método nuevo, los
  fakes en cascada se rompen — pero más importante, **el mock NO
  captura cambios de comportamiento del service real**. Schema
  change en DB que rompa el service en runtime pasa el mock con
  éxito.
- **Refactor**:
  - Reducir fake a métodos realmente usados por cada test (en lugar
    de implementar la interface entera).
  - Añadir tests de **integración** en `api/integration_test.go` que
    usen DB real (`testutil.NewTestDB`) + servicios reales.
  - Esta acción se sinergiza con olor P (split de `ItemHandler`):
    handlers más pequeños tienen menos deps que mockear.

#### F15-6 — Happy path dominante: 3 % de naming de error · **media**

- Sólo **39 tests de 1 288** tienen nombres `*_Error`, `*_Fail`,
  `*_Invalid` (3 %).
- Pero hay buenos ejemplos: `auth/service_test.go` tiene
  `TestService_Login_WrongPassword`,
  `TestService_Login_DisabledAccount`,
  `TestService_Login_NonexistentUser` — modelo a replicar.
- En contraste, `handlers/library_test.go:750+` tiene
  `TestLibraryHandler_Create_HappyPath` sin equivalentes para empty
  path, duplicate name, permission denied, DB error.
- **Refactor**: pasar a table-driven tests con subtest por edge
  case. Plantilla en `auth/service_test.go`.

#### F15-7 — `t.Parallel()` infrautilizado (8 de 137 ficheros) · **baja**

- Sólo 5,8 % de los ficheros lo usan.
- Suite tarda ~30 % más de lo que podría.
- No crítico porque `-race` está activo, pero deja velocidad sobre
  la mesa.
- **Refactor**: añadir `t.Parallel()` en tests que usen `t.TempDir()`
  o que no toquen estado global. Mecánico.

#### F15-8 — `t.TempDir()` excepto un sitio · **baja**

- 105 usos correctos.
- **`stream/transcode_test.go:81-82`** hardcodea `/tmp/sessions/abc`
  — único outlier confirmado:
  ```go
  s := &stream.Session{OutputDir: "/tmp/sessions/abc"}
  expected := filepath.Join("/tmp/sessions/abc", "stream.m3u8")
  ```
- Es un test de path-joining (no I/O real), funciona; pero la
  consistencia importa.
- **Refactor**: `dir := t.TempDir(); …`.

#### F15-9 — `time.After` en 23 tests (leak menor) · **baja**

- Ejemplos: `config/persist_test.go`, `library/watcher_test.go`.
- Si el timeout salta, el canal queda abierto hasta GC. Anti-pattern
  pero no causa fugas reales con Go moderno (1.23+).
- **Refactor**: `context.WithTimeout` + `select { case <-done; case
  <-ctx.Done() }`. Cosmético.

#### F15-10 — Fixture duplication: 19 fakes inline · **baja**

- `library_test.go`, `items_test.go`, `iptv_test.go`, etc. cada uno
  define sus propios `fakeImageRepo`, `fakeMetadataProvider`,
  `fakeUserService` inline.
- Mismo shape en 3-4 ficheros sin compartir.
- **Refactor**: mover fakes a `testutil/fakes/` como tipos públicos.

#### F15-11 — Test naming inconsistente · **baja**

- Mezcla `TestX_HappyPath` + `TestX_Success` + `TestX_WrongPassword`
  + `TestIssueAndValidatePeerToken_HappyPath`.
- **Refactor**: documentar pauta `TestX_WhenY_ThenZ` en
  `conventions.md`. Aplicar progresivamente.

#### F15-12 — Concurrency tests escasos · **baja**

- `federation/stream_test.go:92-120` test goroutine leaks
  explícitamente — buen modelo.
- `provider/httpcache_test.go` usa `atomic.AddInt64` para conteo.
- Pero la mayoría no exercita concurrencia real (e.g.,
  `TestManager_ConcurrentRegister` no existe).
- **Refactor**: añadir tests `*_Concurrent_*` para métodos
  thread-unsafe identificados en F8 y F13.

### 15.3 Severidades Fase 15

| # | Olor | Severidad |
|---|------|-----------|
| F15-1 | `time.Sleep` arbitrarios en 19 ficheros | Alta |
| F15-2 | `time.Now()` no inyectado (231 hits) | Media |
| F15-3 | `waitForCount` con polling sleep | Media |
| F15-4 | Goroutine tests dependientes de timing | Media |
| F15-5 | God-handler tests con mock de 16 métodos | Media |
| F15-6 | Happy path domina (3 % error naming) | Media |
| F15-7 | `t.Parallel()` infrautilizado (8 / 137) | Baja |
| F15-8 | `/tmp/sessions/abc` hardcoded | Baja |
| F15-9 | `time.After` (23 usos) leak menor | Baja |
| F15-10 | Fixture duplication: 19 fakes inline | Baja |
| F15-11 | Test naming inconsistente | Baja |
| F15-12 | Concurrency tests escasos | Baja |

### 15.4 Notas operativas

- **80/20 del agente**: fix `time.Sleep` + inyectar `clock.Mock`
  resuelve el 50 % del riesgo del suite.
- `testutil.NewTestDB` (154 usos) es la decisión correcta —
  evitar mockear `*db.X` cuando se puede usar SQLite real.
- Iteración 8 ya incluye F15-1 (catálogo F14-8-a coincide). F15-2
  pide su propia tarea: inyectar `clock` en services que toquen
  tiempo (sólo 2 lo hacen).

---

## Fase 16 · Handlers medianos no top-10

Cerrada · 2026-05-14 (con apoyo de Agent Explore + verificación
de hallazgos críticos).

### 16.1 Inventario

~70 ficheros en `internal/api/handlers/` no cubiertos por Fase 3
(que se centró en god-handlers top-10). El agente Explore audita
los relevantes; aquí los hallazgos.

### 16.2 Hallazgos

#### F16-1 — Path traversal en `people.go:59-63` · **ALTA**

- `isUnderImageDir()`:
  ```go
  cleaned := filepath.Clean(p)
  abs, err := filepath.Abs(cleaned)
  // ...
  rel, err := filepath.Rel(root, abs)
  ```
- **No resuelve symlinks** antes de `filepath.Rel`. `filepath.Clean`
  + `filepath.Abs` solo normalizan, no siguen symlinks.
- Vector: si la fila `db.Image.path` contiene un path con symlink a
  `/etc/passwd` (insertado por un admin malicioso o por
  compromise de DB), el handler **lo sirve**.
- Mitigado parcialmente: en producción la DB sólo la edita un admin,
  y los paths normales vienen del scanner. Pero el contrato
  "imagen ⊆ imageDir" es la única barrera.
- **Refactor**:
  ```go
  resolved, err := filepath.EvalSymlinks(abs)
  if err != nil { return false }
  rel, err := filepath.Rel(root, resolved)
  if err != nil || strings.HasPrefix(rel, "..") { return false }
  ```
- **Severidad alta** por simetría con FFF: ambos son CVE-class si
  el threat model cambia (multi-admin, DB compartida, supply chain).

#### F16-2 — Paginación sin validar negativos · **media**

- `federation_public.go:100-101`:
  ```go
  offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
  ```
- `Atoi` retorna 0 si falla; negativos pasan tal cual al repo.
- Si el repo en sqlc usa `OFFSET ?`, SQLite/PG rechazan negativos
  con error opaco. Si en algún path se construye SQL con offset
  raw, comportamiento indefinido.
- Sub-caso de F16-4 (inconsistencia entre handlers).
- **Refactor**: helper compartido:
  ```go
  func parsePagination(q url.Values) (offset, limit int, err error) {
      offset = atoiOrZero(q.Get("offset"))
      limit = atoiOrZero(q.Get("limit"))
      if offset < 0 || limit < 0 || limit > maxLimit {
          return 0, 0, domain.NewBadRequest("invalid pagination")
      }
      return
  }
  ```

#### F16-3 — `PeerFromContext` puede ser nil sin re-check · **media**

- `federation_public.go:67-82`: el middleware setea `peer` en ctx;
  el handler hace `peer := federation.PeerFromContext(...)` una vez
  al inicio y lo usa después sin re-check.
- En la práctica, si el handler está montado dentro del
  `r.Use(federation.RequirePeerJWT(...))` group, `peer` nunca es
  nil al entrar al handler.
- Pero `InspectDevice` (línea 309) podría llamarse fuera del group
  protegido — el agente sugiere validar el escenario.
- **Refactor**: helper `requirePeer(w, ctx) (*Peer, bool)` que
  responde 401 si falta. Análogo a `requireParam` (F14-6-b).

#### F16-4 — Paginación inconsistente entre endpoints similares · **media**

- `me_peers.go:202-203` hace `offset, _ := strconv.Atoi(...)` sin
  validar.
- `SearchPeers` (línea 274) hace
  `if perPeerLimit <= 0 || perPeerLimit > 50` — sí valida.
- Dos endpoints del mismo dominio (federation) con sintonía
  distinta.
- **Refactor**: resuelto por F16-2 (helper compartido). Eliminar
  validaciones ad-hoc.

#### F16-5 — Auth check redundante con comentario obsoleto · **media**

- `iptv_admin.go:67-71` ejecuta `canAccessLibrary` con comentario
  "admins can see every library regardless of the ACL".
- El router ya gateaba el endpoint con `requireAdmin`. Doble check
  con comentario que **invalida** la utilidad del check
  ("regardless of the ACL").
- Olor: defense-in-depth o código muerto. El comentario sugiere
  que ya no aplica.
- **Refactor**: borrar el check si está cubierto por middleware del
  router; o documentar por qué se mantiene.

#### F16-6 — Filtración de internals en errores expuestos · **media**

- `federation_admin.go:115`: `"peer probe failed: "+err.Error()`.
- `err.Error()` puede contener IPs internas, paths del filesystem,
  resoluciones DNS internas, status codes específicos.
- `federation_admin.go:163`: mismo patrón.
- **Refactor**: clasificar el error con `errors.Is` / `errors.As`,
  y devolver mensaje genérico al cliente + detalle al log:
  ```go
  if errors.Is(err, ErrPeerUnreachable) {
      apperror.Write(...,"PEER_UNREACHABLE", "peer no responde")
      logger.Warn("probe failed", "err", err) // detalle en log
      return
  }
  apperror.Write(..., "PEER_PROBE_FAILED", "fallo el probe del peer")
  ```
- Olor de seguridad menor (information disclosure) pero acumulativo.

#### F16-7 — `KillSession` sin audit trail · **media**

- `admin_streams.go:140-149`: el endpoint admin `KillSession`
  acepta `session_id` y termina la sesión sin **loggear quién la
  mata**.
- Compliance: admin puede matar sesiones de cualquier usuario
  anónimamente desde el punto de vista del audit log.
- **Refactor**: log obligatorio con `"killed_by", claims.UserID` +
  emitir evento bus `admin.session.killed`.

#### F16-8 — SSE drops silenciosos · **media**

- `auth_device.go:367+` (también en `me_events.go`, `events.go`)
  hace `select { case eventCh <- e: default: drop }`.
- Drop es la decisión correcta (no bloquear el bus).
- **Pero**: no hay métrica ni log de drops. Si un cliente queda
  rezagado, el operador no lo ve.
- **Refactor**: incrementar contador Prometheus
  `hubplay_sse_dropped_total{handler="..."}` en el branch default.
  ~5 LOC, instrumentación pura.

#### F16-9 — `iptv_admin.go:198-207` race en M3U refresh async · **eco**

- Ya catalogado como **GGGG (F13)**. El agente lo encuentra
  independientemente, confirma la severidad media. Misma solución
  (extender `iptv.Service.bgWG`).

#### F16-10..F16-20 — bajas (lista corta)

| # | Ubicación | Olor | Severidad |
|---|-----------|------|-----------|
| F16-10 | `admin_backup.go:99` | `fmt.Sprintf("VACUUM INTO %q", tmpPath)` — defensa-en-profundidad débil | Baja |
| F16-11 | `admin_logs.go:38` | `buffer == nil` retorna vacío sin 503 | Baja |
| F16-12 | `preferences.go:81-87` | `Value` no validado UTF-8 / null bytes | Baja |
| F16-13 | `progress.go:359-373` | `idSet` sin cap de talla (DOS muy bajo) | Baja |
| F16-14 | `collections.go:79-94` | URL decode fallback confuso | Baja |
| F16-15 | `progress.go:297` | `images.GetPrimaryURLs` con error ignorado | Baja |
| F16-16 | `providers.go:43-44` | `json.Unmarshal` con error ignorado | Baja |
| F16-17 | `federation_public.go:79-82` | Patrón `nil → []` repetido | Baja |
| F16-18 | `settings.go:419-425` | `hwAccelChoices` validación case-sensitive con `ToLower` previo | Baja |
| F16-19 | `admin_auth.go:36-37` | Observer opcional silencioso | Baja |

### 16.3 Sanos (sin hallazgos críticos)

- `responses.go`, `me_events.go`, `auth_device.go` (estructura — el
  drop F16-8 es operacional), `federation_admin.go` (excepto F16-6,
  F16-9), `admin_backup.go` (excepto F16-10 baja), `setup.go` (la
  lista `isSensitivePath` quedó pendiente desde Fase 11 olor QQQ —
  no auditada en este pase).

### 16.4 Severidades Fase 16

| # | Olor | Severidad |
|---|------|-----------|
| **F16-1** | **Path traversal en `people.go:isUnderImageDir`** | **Alta** |
| F16-2 | Paginación sin validar en `federation_public.go` | Media |
| F16-3 | `PeerFromContext` sin re-check | Media |
| F16-4 | Paginación inconsistente | Media |
| F16-5 | Auth check redundante con comentario obsoleto | Media |
| F16-6 | Filtración de internals en errores | Media |
| F16-7 | `KillSession` sin audit trail | Media |
| F16-8 | SSE drops sin observabilidad | Media |
| F16-9 | Race async iptv_admin (eco GGGG) | Media |
| F16-10..F16-19 | Bajas (10 items) | Baja |

### 16.5 Nota operativa

Tras Fase 16, **dos** hallazgos de seguridad alta-críticos siguen
abiertos:

1. **FFF — SSRF redirect bypass en `imaging.SafeGet`** (F9).
2. **F16-1 — Path traversal en `people.isUnderImageDir`** (F16).

Ambos están **mitigados parcialmente** por trust model actual
(admin-only para inputs sensibles), pero ambos rompen el contrato
explícito que el código intenta enforcer. Tratarlos como
**CVE-class** en Iteración 1.

---

## Plan de intervención final

Cerrado · 2026-05-14, **revisado tras F9-F16**. Sintetiza las 16
fases. Las **letras entre paréntesis** referencian olores
específicos.

### A. Mapa consolidado de olores por severidad

#### Severidad alta (incluye nuevos de F9-F12)

| # | Olor | Fase | Sub-tareas |
|---|------|------|-----------|
| **FFF** | **SSRF redirect bypass en `imaging.SafeGet`** | **F9** | **`CheckRedirect` callback (~15 LOC) — CVE-class** |
| A+M | `internal/db/` god-package + 80 tipos `db.X` consumidos por 55 ficheros externos | F1, F2 | Decisión Opción B + migración por feature |
| B | `db → federation` inversión de capa | F1, F2 | Mover repo a `internal/federation/storage/` |
| CC | `iptv.Service` 45 métodos en 11 sub-features | F5 | Split en sub-paquetes (m3u/, epg/, channels/, transmux/, proxy/, prober/, logo/) |
| J | `federation_repository.go` 1 474 LOC con 6 responsabilidades | F2 | Resuelto por B (mover a feature) |
| P | `ItemHandler` god-handler 1 186 LOC, 13 deps | F3 | Split en 4 handlers |
| W | `scanner.go` 1 270 LOC en un fichero | F4 | Split por responsabilidad (no de paquete) |

#### Severidad media-alta

| # | Olor | Fase | Sub-tareas |
|---|------|------|-----------|
| G | `Dependencies` + `runtime` + `main.run` 645 LOC | F1, F3 | Módulos compuestos por feature (`<feature>.New(ctx, deps) *Module`) |
| Q | `WriteTimeout: 0` global aplica a las 219 rutas | F3 | Middleware `WithWriteDeadline(30s)` en sub-router no-streaming |
| **RRR-mig** | **Política up-only violada (15 migraciones con `Down`)** | **F12** | **Eliminar `-- +goose Down` de 15 ficheros (mecánico)** |
| **F14-2-a** | **`BuildFFmpegArgs` 192 LOC + 13 params; `Transcoder.Start`/`RestartAt` con misma firma de 11** | **F14** | **Struct `TranscodeRequest`** |
| **F14-2-b** | **9 funciones con 7+ parámetros** | **F14** | **Struct params para cada constructor / método público** |

#### Severidad media

| # | Olor | Fase | Sub-tareas |
|---|------|------|-----------|
| C | `api/handlers/` plano, 79 ficheros, 26 interfaces en un fichero | F1, F3 | Sub-paquetes (`handlers/admin/`, `/iptv/`, …) |
| H | `Dependencies` con tipos `*db.X` concretos | F1, F3 | Eco de G; se va junto |
| K+T | `*sql.DB` raw → `system.go` con queries SQL raw inline | F2, F3 | `db.ActivityLogRepository` + interfaces estrechas (`HealthChecker`/`BackupOperator`/`PoolStatsReporter`) |
| L | `home_repository.go` 671 LOC con 3 rails | F2 | Split por fichero (mantener raw) |
| V | `router.go` lee `deps.Config.*` directo | F3 | Promover campos relevantes a `Dependencies` |
| Y | SegmentDetector/Fingerprinter sin drain de goroutines | F4 | Añadir `bgWG` (modelo: `library.Service`, `TransmuxManager`) |
| Z | `library.Service` 27 métodos, 6 responsabilidades | F4 | Split + decorator repo para rating-cap |
| DD | Detached goroutines en `iptv.Service.RefreshM3U` sin drain | F5 | Mismo patrón que Y |
| LL | `stream.Manager` y `Transcoder` con doble session tracking | F6 | Transcoder stateless |
| QQ | `auth.Service` 18 métodos, 6 responsabilidades | F7 | Split (más fácil que Z/CC por tamaño) |
| RR | `loginRateLimiter` goroutine sin Stop (bug latente) | F7 | Añadir `stopCh` + invocar desde `StopSessionCleaner` |
| **GGG** | **DNS rebinding teórico en `SafeGet`** | **F9** | **Custom `DialContext` con IP pinning** |
| **HHH** | **`pathmap.Read` sin validar raíz** | **F9** | **Retornar path relativo o validar prefix** |
| **SSS-mig** | **`api_keys.created_by` FK sin `ON DELETE CASCADE`** | **F12** | **Migración nueva si la tabla se mantiene** |
| **TTT-mig** | **INTEGER vs BIGINT divergencia SQLite/PG** | **F12** | **Alinear con `BIGINT` explícito** |
| **UUU-mig** | **Falta `idx_channels_library_number`** | **F12** | **Migración nueva** |
| **GGGG** | **Detached goroutines en `iptv_admin.go`** | **F13** | **Mismo `bgWG` que olor DD** |
| BB | Comentarios en inglés en todos los `internal/` (transversal) | F1, F4, F5 | Pauta por fase, no big-bang |

#### Severidad baja

| # | Olor | Fase | Sub-tareas |
|---|------|------|-----------|
| D | `library` vs `scanner` frontera artificial | F1, F4 | Promover `scanner` a sub-paquete de `library` |
| E | `iptv` con 32 ficheros al límite | F1 | Resuelto por CC |
| I+R | 26 interfaces en `handlers/interfaces.go` con convención inconsistente | F1, F3 | Documentar regla en `conventions.md` + bajan con P |
| N | `Pattern A/B` viven en comments, no como helper | F2 | Documentar formalmente en `conventions.md` |
| O | `db.Repositories` 31 campos | F2 | Eco menor de G |
| EE | `StreamProxy.Shutdown` engañoso (no drena) | F5 | Renombrar o documentar |
| JJ | 3 setters post-construcción en `stream.Manager` | F6 | `NewManager(Deps)` |
| AAA | Comentario `event/bus.go:24-31` desactualizado | F8 | Reescribir |
| EEE | `event.Bus` sin `Close()` | F8 | Cosmético |
| **III** | **`imaging.BlockedIP` global mutable (test override)** | **F9** | Pasar como param |
| **JJJ** | **Status code sin `%w` en `imaging`** | **F9** | Sentinel + `%w` |
| **KKK** | **`blurhash`/`colors` sin `context`** | **F9** | Añadir ctx |
| **LLL** | **Animated GIF/APNG bombs no detectados** | **F9** | Cap frame count |
| **MMM** | **`apperror.recorder` global mutable** | **F10** | Pasar a `Write` |
| **NNN** | **CSRF token sin rotación post-login** | **F10** | Rotar en `auth.Login` |
| **OOO** | **`RequestLogger` siempre Info (5xx no es Error)** | **F10** | Branch por status |
| **PPP** | **`cfg.Auth.JWTSecret` dead data post-bootstrap** | **F11** | Renombrar o eliminar |
| **VVV-mig** | **`api_keys` schema sin queries (dead)** | **F12** | Decidir mantener o borrar |
| **WWW-trans** | **`stream.Profiles` map exportado mutable** | **F13** | Getter en lugar de var |
| **XXX-trans** | **`auth.AuthToken` stutter** | **F13** | Rename a `auth.Token` |
| **YYY-trans** | **Tests sin `t.Parallel()` (8/122)** | **F13** | Activar donde no haya shared state |
| **QQQ** | **`setup.isSensitivePath` sin auditar** | **F11** | Confirmar lista en intervención |
| **F14-3-a** | **3 booleans seguidos en `Transcoder.Start`** | **F14** | Cubierto por F14-2-a |
| **F14-4-a** | **`panic()` en `prober_worker.go:87`** | **F14** | Retornar `(*ProberWorker, error)` |
| **F14-5-a** | **Inconsistencia naming `is*Safe*` / `Blocked*`** | **F14** | Convención en `conventions.md` |
| **F14-6-a** | **100+ sites `map[string]any{"data": ...}`** | **F14** | Helper `respondData` |
| **F14-6-b** | **`chi.URLParam` boilerplate (25 sites)** | **F14** | Helper `requireParam` |
| **F14-7-a** | **Logging `library_id` 145 veces sin `.With()`** | **F14** | Sub-loggers per-context |
| **F14-8-a** | **Tests con `time.Sleep` arbitrarios (10 ficheros)** | **F14** | Test seams determinísticos |
| **F14-9-a** | **Magic numbers Cache-Control (12 sites)** | **F14** | Constantes nombradas |
| **F14-10-a** | **4-valor returns (4 funciones)** | **F14** | Structs nombradas |
| **F14-11-a** | **`scan_helpers.go itemNullables` posiblemente dead** | **F14** | Verificar con `staticcheck` |
| **F14-12-a** | **`+=` para concatenación (6 sites)** | **F14** | `strings.Builder` |
| **F14-5** | **`db.NewHomeRepository` constructor 175 LOC con SQL inline** | **F14** | Mover SQL a constantes; split por rail |
| **F14-9** | **`db.ItemRepository.List` 150 LOC con `where +=`** | **F14** | `[]string` + `strings.Join` |

#### "Sanos / modelos" a citar al refactorizar

- **Lifecycle drain con `bgWG`**: `library.Service.Shutdown`,
  `stream.Manager.Shutdown`, `iptv.TransmuxManager.Shutdown`,
  `federation.Manager.Close`. Replicar para Y/DD/RR.
- **Lógica pura aislada de I/O**: `stream/decision.go`. Replicar para
  cualquier business logic nueva.
- **Sink pattern anti-cycle**: `internal/auth/jwt.go` con
  `keyResolver` (función inyectada), `iptv.proberRunner` interface,
  `api/apperror` como cut-set. Documentado en `conventions.md`.
- **Async writer**: `federation.Auditor` (UU) — replicar para
  futuros writers no-críticos.
- **Locking granular**: `federation.Manager` con dos mutexes (VV).
- **`singleflight.Group`** para colapsar races: `stream.Manager.StartSession` (NN).
- **HW detection cacheada al boot**: `stream.Manager.hwAccel` (OO).
- **Split por sub-fichero con un receiver compartido**:
  `federation/manager_*.go` (TT) — modelo para CC.

### B. Orden de ejecución sugerido

El orden minimiza riesgo y maximiza valor por iteración. Cada
bloque es independiente del siguiente (se puede merge antes de
empezar el siguiente) y deja el repo en verde.

#### Iteración 0 · Pre-trabajo (~0.5 día, sin refactor de código)

1. **ADR nuevo** "ADR-015: tipos de dominio viven en su feature
   (Opción B)". Supersede el modelo implícito "tipos en db/".
2. **Documentar** Pattern A/B en `docs/memory/conventions.md` (olor N).
3. **Documentar** convención de interfaces (consumer-side por
   handler, olor I+R) en `conventions.md`.
4. **Borrar / reescribir** comentario obsoleto en `event/bus.go`
   (AAA).

#### Iteración 1 · Fixes URGENTES de seguridad + correctness (~1.5 días)

5. **🚨 FFF (PRIORIDAD MÁXIMA)**: añadir `CheckRedirect` a
   `imaging.SafeGet`. CVE-class — SSRF a servicios internos. ~20
   LOC, modelo en `iptv/proxy.go`. Test que monte httptest server
   con `302 Location: http://127.0.0.1:9200` y verifique rechazo.
6. **🚨 F16-1 (PRIORIDAD MÁXIMA)**: `people.isUnderImageDir` usar
   `filepath.EvalSymlinks` antes de `filepath.Rel`. ~10 LOC. Test
   que cree symlink dentro de `imageDir` apuntando fuera y
   verifique rechazo. CVE-class — path traversal vía symlink.
7. **RRR-mig**: eliminar bloques `-- +goose Down` de las 15
   migraciones que los tienen. ~15 ficheros, cambio mecánico.
8. **RR**: `loginRateLimiter.Stop()` + cablear desde
   `auth.Service.StopSessionCleaner`. ~10 LOC.
9. **Y**: añadir `bgWG sync.WaitGroup` a `SegmentDetector` y
   `SegmentFingerprinter`. Modelo `library.Service`. ~40 LOC.
10. **DD + GGGG**: añadir `bgCtx/bgCancel/bgWG` a `iptv.Service` +
    reemplazar `context.Background()` en `service_m3u.go:230,246` Y
    en `handlers/iptv_admin.go:101,200`. Modelo `library.Service`.
    ~80 LOC totales (resuelve dos olores con un patrón).
11. **AAA, EE**: comentarios + renombrar `StreamProxy.Shutdown` →
    `ClearRelays` (o documentar como intencional).
12. **HHH**: hacer `pathmap.Read` retornar path relativo + validar
    prefix. ~5 LOC.
13. **F16-6**: clasificar errores en `federation_admin.go:115,163`
    con `errors.Is/As` + mensaje genérico al cliente + detalle al
    log. ~20 LOC.
14. **F16-7**: `KillSession` (admin_streams.go:140) obligatorio
    `logger.Info("session killed", "session_id", id, "killed_by",
    claims.UserID)` + emitir evento bus. ~10 LOC.

**Cierre**: 2 CVE-class + 3 bugs latentes de drain + violación
up-only + leak de ratelimit + 2 olores menores de federation + 1
de audit trail. **Cero cambios de API pública.**

#### Iteración 2 · Sub-paquetes de db (~1 día)

9. **B + J**: mover `db.federation_repository.go` → `internal/federation/storage/`.
   - Split en `identity.go`, `invite.go`, `peer.go`, `audit.go`,
     `item_cache.go`, `ratelimit.go`.
   - Cada uno como adapter sqlc + raw donde justifique.
   - `db/repos.go` deja de construir `FederationRepository`.
   - `federation.NewManager` lo construye internamente.
   - Tests adyacentes acompañan.
10. **K+T**: crear `db.ActivityLogRepository`. Sustituir
    `system.go` queries raw por llamadas al repo. `Dependencies.Database`
    pasa de `*sql.DB` a interfaces estrechas
    (`HealthChecker`/`BackupOperator`/`PoolStatsReporter`).
11. **L**: split `home_repository.go` en tres ficheros (`home_latest.go`,
    `home_trending.go`, `home_live.go`) — mantener raw, sólo
    reorganizar.

#### Iteración 3 · Migración Opción B incremental (~3-4 días)

Por feature, una por commit:

12. **iptv** (12 tipos `db.Channel*`, `db.EPGProgram`,
    `db.IPTVScheduledJob`, etc.). Migración mecánica con `goimports
    -r`. Bloque más grande del refactor.
13. **auth** (4 tipos: `db.User`, `db.Session`, `db.SigningKey`,
    `db.DeviceCode`).
14. **library** (12 tipos: Item, MediaStream, Image, Chapter,
    EpisodeSegment, ItemValue, Studio, Collection, ExternalID,
    Metadata, Person, ItemPersonCredit). El bloque más grande de
    `library`.
15. Limpiar `internal/db/` post-migración. Debería quedar reducido a
    factory + adapter sqlc + dialect helpers + 4-5 repos restantes.

#### Iteración 4 · Split de god-handlers y god-services (~2 días)

16. **P + C**: split `ItemHandler` en
    `ItemDetailHandler`/`RecommendationsHandler`/`TrickplayHandler`/
    `SearchHandler`. Sub-paquete `handlers/items/`. **Disminuye
    automáticamente las interfaces en `handlers/interfaces.go`**
    (PeopleRepoForItems, CollectionRepoForItems, ChapterRepository,
    EpisodeSegmentRepository pasan a vivir en sub-paquetes).
17. **Z**: split `library.Service` en `LibraryManager` +
    `AccessControl`. Item queries pasan a llamar al repo directo
    con un decorator `WithRatingCap(cap)`.
18. **QQ**: split `auth.Service` en `LoginService` + `SessionService`
    + `AccountService` + `ProfileService`. Más fácil que Z/CC.

#### Iteración 5 · Refactor estructural grande de iptv (~1-2 días)

19. **CC**: split `internal/iptv/` en sub-paquetes
    (`m3u/`, `epg/`, `channels/`, `transmux/`, `proxy/`, `prober/`,
    `logo/`). Modelo: `federation/manager_*.go` (TT).

#### Iteración 6 · Composition root (~1 día)

20. **G + H + V**: introducir módulos compuestos
    (`<feature>.New(ctx, deps) *Module`) que devuelven service +
    workers + cleanup. `main.run` se reduce sustancialmente.
    `Dependencies` cambia a interfaces.
21. **Q**: middleware `WithWriteDeadline(30s)` aplicado al sub-router
    `/api/v1/*` **excepto** sub-trees streaming.
22. **JJ**: `stream.NewManager(Deps)` con un único setter runtime
    (`SetForceDirectPlayLookup`).
23. **LL**: hacer `Transcoder` stateless — tracking sólo en `Manager`.

#### Iteración 7 · Cosmética + comentarios + schema (~1.5 días, paralelizable)

24. **D + X**: promover `scanner` a `internal/library/scan/`.
25. **W**: split `scanner.go` en `scanner.go` + `enrich.go` +
    `persist.go` + `images.go`.
26. **BB**: traducir / reescribir comentarios largos en español por
    paquete. Pauta: técnico, conciso, explica el porqué.
27. **EEE**: añadir `Bus.Close()` cosmético (opcional).
28. **UUU-mig**: migración nueva con
    `idx_channels_library_number`.
29. **TTT-mig**: alinear `BIGINT` explícito en SQLite (migración
    nueva opcional).
30. **VVV-mig + SSS-mig**: decidir destino de `api_keys` (eliminar
    en migración nueva o materializar con `queries/api_keys.sql` +
    `ON DELETE CASCADE`).
31. **WWW-trans**: `stream.Profiles` map → getter privado.
32. **XXX-trans**: rename `auth.AuthToken` → `auth.Token`.
33. **PPP**: renombrar / documentar `cfg.Auth.JWTSecret` como
    bootstrap-only.
34. **NNN**: rotar CSRF token tras `auth.Login` success.
35. **OOO**: `RequestLogger` con nivel según status.
36. **JJJ + III + KKK + LLL**: cosmética de `imaging`.

#### Iteración 8 · Polish de calidad de código y "estilo profesional" (~2 días, paralelizable)

Las tareas de F14 no encajan en otras iteraciones y son
**transversalmente paralelizables** — cada subgrupo es independiente
y deja el repo verde tras commit.

37. **F14-2-a + F14-3-a**: `TranscodeRequest` struct para
    `BuildFFmpegArgs` + `Transcoder.Start` + `RestartAt`. Una
    sola PR resuelve los 3.
38. **F14-2-b**: structs de params para las 9 funciones con 7+
    args. Mecánico: `NewItemHandler`, `NewIPTVHandler`,
    `NewTranscoder`, `Manager.StartSession`, `startSessionSlow`,
    `RecordProgress`. Una commit por struct.
39. **F14-6-a**: helper `respondData` + sed-replace de los 100+
    sites de `map[string]any{"data": ...}`. Mantiene wire shape.
40. **F14-6-b**: helper `requireParam` + reemplazo de los 25 sites
    de `chi.URLParam(r, "id")` con guard.
41. **F14-9-a**: constantes `cacheOneHour`/`cacheOneDay`/
    `cacheOneWeek` en `handlers/cache.go` + reemplazo en 12
    sites.
42. **F14-7-a**: convención de sub-loggers con `.With(...)` en cada
    feature. Documentar en `conventions.md`. Aplicar en hot paths
    de iptv/library/auth.
43. **F14-8-a**: refactor de los 10 tests con `time.Sleep` a test
    seams determinísticos. Modelo: `FSWatcher.walksDone`.
44. **F14-9 + F14-5**: split de `db.ItemRepository.List` y
    `db.NewHomeRepository` con `strings.Builder` o `[]string` +
    `Join` para WHERE dinámico.
45. **F14-4-a**: `panic()` en `prober_worker.go:87` → error.
46. **F14-10-a**: structs nombradas para los 4 retornos múltiples.
47. **F14-11-a**: ejecutar `staticcheck -unused ./...` y eliminar
    `scan_helpers.go::itemNullables` si confirmado dead.
48. **F14-12-a**: `strings.Builder` donde `+=` se repite ≥3 veces
    (3 sites concretos).
49. **F14-5-a**: documentar convención `is*Safe*` / `Blocked*` en
    `conventions.md`.
50. **F15-2**: inyectar `clock.Clock` en services que tocan tiempo
    pero no lo usan (231 hits de `time.Now()` en tests). Migración
    mecánica.
51. **F15-3 + F15-4**: refactor de `waitForCount` (auth) y
    `TestManager_CloseStopsSweeperGoroutine` (federation) para
    eliminar polling sleep.
52. **F15-5 + F15-10**: consolidar fakes en `testutil/fakes/` y
    reducir mocks de 16 métodos en `libFakeService`.
53. **F15-6 + F15-11**: ampliar error path coverage. Empezar por
    handlers con < 3 tests de error. Documentar pauta
    `TestX_WhenY_ThenZ`.
54. **F15-8**: `/tmp/sessions/abc` → `t.TempDir()` en
    `transcode_test.go:81-82`.
55. **F15-12**: 1-2 tests `*_Concurrent_*` para
    `provider.Manager.Register`, `stream.Manager.StartSession`.
56. **F16-2 + F16-4**: helper `parsePagination` y reemplazar 5+
    sites con validación negativa ad-hoc.
57. **F16-3**: helper `requirePeer` análogo a `requireParam`.
58. **F16-5**: borrar `canAccessLibrary` redundante o documentar.
59. **F16-8**: contador Prometheus `hubplay_sse_dropped_total{...}`
    en los 3 SSE handlers (auth_device, me_events, events). ~15
    LOC, instrumentación pura.
60. **F16-10..F16-19**: tanda de fixes baja: validar `Value` UTF-8
    en preferences, cap `idSet` en progress, logear errores
    ignorados en progress + providers.

**Resultado esperado de Iteración 8**:
- ~600 LOC eliminadas sin pérdida de funcionalidad (ver F14.14).
- ~30 firmas de función con 7+ params reducidas a 1 param (struct).
- 100+ sites de boilerplate eliminados.
- 19 ficheros de test sin `time.Sleep` arbitrario.
- `clock.Clock` propagado a services que tocan tiempo.
- 3 SSE handlers con métrica de drop.
- Helpers `parsePagination` + `requirePeer` añadidos.
- Convenciones documentadas en `conventions.md`.

#### Iteración 9 · Verificación empírica post-intervención (~0.5 día)

Se ejecuta **después** de que las 8 iteraciones anteriores hayan
mergeado. Es una iteración de QA, no de implementación. Confirma
empíricamente lo que la auditoría afirmó estáticamente.

61. **`go test -race -count=10 ./...`** — confirmar que no hay
    races latentes tras los fixes de Iteración 1 (RR, Y, DD,
    GGGG).
62. **`goleak.VerifyNone(t)` integrado** en suites donde la
    auditoría identificó leaks potenciales (auth.Service,
    iptv.Service, library.SegmentDetector, library.SegmentFingerprinter).
    Confirmar 0 leaks.
63. **`govulncheck ./...`** — escanear CVEs en deps (`go.mod`,
    transitive). Reportar al usuario; no resolver salvo críticos.
64. **`staticcheck -unused ./...`** — confirmar / refutar el dead
    code identificado en F14-11-a (`scan_helpers.go::itemNullables`,
    `db.api_keys` schema). Limpiar lo confirmado.
65. **Shutdown test integrado**: simular `SIGTERM` durante un
    `RefreshM3U` en vuelo. Verificar drain limpio sin "sql:
    database is closed" en logs.

**Resultado esperado de Iteración 9**:
- CI con `-race` + `goleak` activos por defecto.
- 0 vulnerabilidades altas en deps reportadas.
- Dead code confirmado eliminado.
- Documento de evidencia de que la auditoría se cumple en runtime.

### C. ADRs a abrir

| ADR | Título | Supersede |
|---|---|---|
| 015 | Dominio en feature, no en `db/` (Opción B) | Modelo implícito previo |
| 016 | Composition root con módulos por feature | Parte del wiring de `main.run` |
| 017 | Timeouts diferenciados streaming vs API | — |
| 018 | Comentarios en español como convención | — |
| **019** | **SSRF guard con `CheckRedirect` obligatorio** | Refuerza ADR-002 |
| **020** | **Política `up-only` con migraciones de rollback positivas** | Refuerza convención existente |
| **021** | **Path traversal: `EvalSymlinks` obligatorio antes de `Rel`** | — |
| **022** | **`clock.Clock` inyectado en todo service que toque tiempo** | — |
| **023** | **Filtración de errores externos: clasificar antes de exponer** | — |

ADR-012 (federación reuse de primitivos) **NO se supersede**: la
promesa se confirma vigente en F7 (JWT shape, keystore, audit). El
único punto pendiente que abrió ADR-012 (federation repo no es sqlc
todavía) se cierra como efecto colateral de B+J en Iteración 2.

### D. Tests a añadir antes de tocar

- **Goroutine-leak tests** para `auth.Service`, `iptv.Service`,
  `library.SegmentDetector`, `library.SegmentFingerprinter`. Usar
  `goleak.VerifyNone(t)` al final del test. Sin esto, RR/Y/DD se
  arreglan a ciegas.
- **Shutdown test integrado** que simule `SIGTERM` durante un
  `RefreshM3U` en vuelo — debe terminar sin "sql: database is
  closed" en logs.
- **Concurrent StartSession** que verifique `singleflight` colapsa
  N callers → 1 ffmpeg (probablemente ya existe; auditar).

### E. Métricas de éxito post-refactor

- `internal/db/` LOC < 6 000 (hoy 13 268).
- Mayor fichero `internal/iptv/` < 600 LOC (hoy `transmux.go` 1 052
  está justificado, pero `service*.go` se distribuye en
  sub-paquetes).
- `Dependencies` < 15 campos (hoy 35+).
- `main.run` < 250 LOC (hoy 645).
- `api.NewRouter` < 100 LOC (hoy 626) — resuelto por sub-routers.
- Ninguna función pública con > 6 parámetros (hoy 9 con 7+).
- Ninguna función > 200 LOC (hoy 6 sobre 100 LOC).
- `handlers/interfaces.go` desaparece — cada sub-paquete lleva sus
  interfaces.
- `goleak.VerifyNone(t)` pasa en todos los servicios con goroutines.
- Backend total < 68 000 LOC prod (hoy 72 319) — reducción del
  ~5-6% por F14 + dedup post-Opción B.

### F. Riesgos y mitigaciones

- **Big-bang en F2.5 (Opción B)**: mitigado por migración incremental
  por feature (iteración 3, 4 commits).
- **Tests rotos en cascada**: cada iteración deja el repo verde
  antes de seguir. CI gate.
- **Conflictos con features en vuelo**: trabajar en rama
  `claude/review-go-media-backend-37MDe` ya aislada; rebase
  semanal contra `main` para detectar deriva temprana.
- **Reescritura de comentarios**: marcar por paquete; **no** hacer
  un commit "translate all comments" porque pierde contexto y mezcla
  con refactor real.

### G. Lo que NO se va a hacer

- **No** se fusiona `auth.ratelimit` y `federation.ratelimit`
  (ADR-012 lo justifica, decisión sigue vigente).
- **No** se introduce un microservicio para federation (ADR-012).
- **No** se cambia el wiring manual por un DI container — Go premia
  explicitness; los módulos compuestos cubren el caso sin perder
  claridad.
- **No** se reescriben tests de `stream/decision.go` ni los del
  scanner — ya son el modelo del proyecto.
- **No** se traducen big-bang los comentarios — pauta incremental.
- **No** se intenta hot-reload de HWAccel (ADR-010).

---

## Cierre

Esta auditoría está cerrada. Documento listo para servir de
checkpoint inicial de la intervención. Cada iteración del plan B
debería referenciar este documento por sección y, al cerrarse,
añadir un párrafo de cierre justo debajo de su entrada en el plan.

Cuando se inicie la intervención, recomiendo abrir un documento
hermano `docs/memory/intervention-2026-05-XX.md` que tracée el
trabajo iteración por iteración, dejando este como spec inmutable
del estado inicial.

### Cómo trabajar este plan en sesiones

El plan está diseñado **para sesiones independientes**:

- **Cada iteración (0..8)** deja el repo verde y mergeable. Una
  sesión = una iteración completa, o un subconjunto. No mezclar
  iteraciones en un mismo PR.
- **Cada olor tiene letra única** (A..LLLL, F14-X) que se referencia
  en el commit y la PR. El commit message dice "fix(security):
  resuelve FFF — SSRF redirect bypass" y en el doc se escribe el
  párrafo de cierre debajo de la entrada de FFF.
- **Iteración 1 es URGENTE** (FFF + correctness). Se hace antes que
  todo lo demás. Aislada.
- **Iteraciones 2-5** son secuenciales: cada una desbloquea la
  siguiente. (db sub-paquetes → Opción B → split god-handlers →
  refactor iptv.)
- **Iteración 6** (composition root) requiere que 2-5 estén hechas.
- **Iteración 7 y 8** son **paralelizables entre sí y con 6**.
  Cada commit es independiente.

**Estado en el doc**: marcar cada olor cerrado con `· ✅ cerrado en
PR #N (commit hash)` debajo de su entrada. La auditoría queda como
spec **inmutable**; las marcas de cierre van **debajo**, no editan
los hallazgos originales.

**Para retomar entre sesiones**:
1. Leer `docs/memory/project-status.md` (siempre, primero).
2. Leer este audit + buscar olores no cerrados.
3. Elegir el siguiente bloque del plan que no esté cerrado.
4. Trabajar 1 iteración, commit + PR, marcar cierre en el doc.
