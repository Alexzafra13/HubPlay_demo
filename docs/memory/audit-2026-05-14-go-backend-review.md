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
