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
