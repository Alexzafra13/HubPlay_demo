# Estado del proyecto

> **Entrypoint de cada sesión.** Lo viejo (todo lo previo a la sesión
> 2026-05-19/20) vive en `archive/`. No se pierde nada — sólo se mueve
> de sitio para que este fichero sea legible de un vistazo.

---

## 🔭 Estado actual (2026-05-21, Iter6 medio — composition root V+JJ+LL+G)

- Branch principal: `main`, V+JJ+LL ya mergeados via **PR #395** (commit `61396a3`). Branch de trabajo `claude/review-project-9YJxG` rebaseada sobre el nuevo main — aporta solo G parcial (`8b746fc`) + memoria (`e899169`) en hacia el PR siguiente.
- Working tree limpio. PR única abierta en GitHub: **#376** (web-deps group, 17 updates — CI pendiente del último estado).
- Última release pública: `nightly` rolling tag (workflow `release.yml`).
- Tests: `go test -race ./...` verde end-to-end con todo Iter 6 V+JJ+LL+G aplicado; frontend **646/646** vitest verdes; `tsc -b` limpio; production build limpio.
- **React Compiler activado** + `eslint-plugin-react-compiler` como hard gate. `react-compiler-healthcheck`: 542/542 componentes compatibles. Quality gates en CI: `typecheck` (hard), `react-compiler-healthcheck` (hard), **`knip` (hard)**, `react-doctor` (visibility-only con comentarios inline en PRs).
- **Score React Doctor: ≥75/100 ("Great")** post-VideoPlayer-split (PR #381 mergeada). El offender principal de las reglas estructurales (`no-cascading-set-state`) eliminado; `no-giant-component` reducido de 1003 a 663 lines; `prefer-useReducer` de 12 useState a 5.
- **knip: 0 unused files / 0 unused deps / 0 unused exports / 0 unused types**. Hard gate en CI.
- **Audit 2026-05-14 — Iteración 6 al 80 % cerrada esta sesión** (V + JJ + LL + G parcial). Queda **H** (router split + interfaces en Dependencies) para sesión propia. De los **6 olores altos** del audit original (A+M, B+J, CC, P, W, F14-2-a), 5 están cerrados — sólo queda F14-2-a (function-level quality).
- **Dependabot alerts**: 8 → 1 (1 critical eliminada). PRs #385 (to-ico→png-to-ico, 5 vulns), #289 (picomatch alert #18) mergeadas. Queda 1 medium (file-type transitive de node-vibrant — bloqueada hasta upstream).
- HubPlay distribuible "descargar y usar" en los tres targets (desktop / Linux server / NAS-via-Docker) — flujo cerrado en la sesión 2026-05-19/20.

---

## 🏗️ Sesión 2026-05-21 (noche) — Iteración 6 composition root: V + JJ + LL + G parcial

Sesión continuación que ataca el grueso de Iter 6 del audit 2026-05-14 (composition root). De los 5 olores listados (G, H, V, LL, JJ) **4 se cierran** total o parcialmente en 2 commits sobre `claude/review-project-9YJxG`. H queda para sesión propia (router.go es el fichero de mayor blast-radius — 1460 LoC en un solo `r.Route("/api/v1", ...)`).

### Commits en `claude/review-project-9YJxG` (pusheada, sin PR)

| Commit | Tema | Olores | Estado |
|---|---|---|---|
| [`61396a3`](https://github.com/Alexzafra13/HubPlay_demo/commit/61396a3) (PR #395) | Primitivos de Config en Dependencies + stream.NewManager(Deps) + docs LL | V + JJ + LL | ✅ mergeado a main |
| [`8b746fc`](https://github.com/Alexzafra13/HubPlay_demo/commit/8b746fc) | Drop `runtime` god-struct → `lifecycle` con 3 fases | G parcial | 🟡 en `claude/review-project-9YJxG` pendiente de PR |

### Olor V — `router.go` lee `deps.Config.*` directo (media)

Las 17 lecturas dispersas de `deps.Config.X.Y` desde el cuerpo del router (handler construction sites: AuthHandler, HealthHandler, SystemHandler, SettingsHandler, AdminBackupHandler, ImageHandler federation, trickplayDir, etc.) se reemplazan por **13 campos primitivos** en `Dependencies`, materializados una sola vez en `main.go` desde `cfg`:

```
MetricsEnabled, MetricsPath, AuthConfig, DataDir, DatabasePath,
DatabaseDriver, ServerAddr, ServerBaseURL, ServerPort, MDNSEnabled,
MDNSHostname, HWAccelDefault, AllowedOrigins.
```

El campo `Config *config.Config` se mantiene únicamente para los dos handlers que MUTAN el fichero on-the-fly (setup wizard + `AdminDBHandler`, ambos llaman a `config.Save`). Docstring del campo actualizado para narrow del uso permitido.

**Retro-compat**: helper privado `Dependencies.fillFromConfig()` al top de `NewRouter` rellena primitivos a-zero desde `Config`. Los dos integration tests (`integration_test.go`, `stream_integration_test.go`) que sólo pasan `Config: cfg` siguen funcionando sin tocarse; el "path idiomático" es pasar primitivos explícitos (main.go).

### Olor JJ — 3 setters post-construcción en `stream.Manager` (baja)

`SetMetrics + SetEventBus + SetForceDirectPlayLookup` eran un Builder Pattern accidental: 4 llamadas encadenadas en `main.go` para dejar el Manager en estado "listo". Sustituidas por un único call con `stream.Deps{Items, Streams, Config, Logger, Metrics, EventBus, ForceDirectPlayLookup}` pasado a `NewManager`.

Los setters **siguen existiendo** en la API pública porque tests del paquete los usan para swap de stub→real mid-test (`TestManager_SetMetrics_*`) y el comentario de `SetForceDirectPlayLookup` documenta el contrato runtime-swap. Producción wires todo atómico vía `NewManager(Deps{...})`.

`NewManager` ahora seedea el gauge `SetActiveSessions(0)` en el wiring inicial cuando `Deps.Metrics != nil`, igual que el contrato documentado de `SetMetrics`.

### Olor LL — Manager + Transcoder con doble session tracking (media)

**Cerrado por documentación**. El grounding del audit confirmó que las dos maps (`Manager.sessions` keyed por `sessionKey(user,item,profile,audio,sub)` y `Transcoder.sessions` keyed por sessionID bare) NO son duplicado — apuntan al mismo `*Session` por debajo (`ManagedSession` embed un `*Session`) pero con propósitos distintos:

- `Manager.sessions`: sesión LÓGICA del usuario (decisión de playback, user context, `restartMu` por-sesión, `LastAccessed`). API pública.
- `Transcoder.sessions`: proceso ffmpeg físico (`cmd`, `cancel`, `done`). Interno al paquete.

Docstrings struct-level en `Manager` y `Transcoder` hacen explícita la separación de responsabilidades. El refactor "Transcoder stateless" que el audit sugería (mover cmd/cancel/done a ManagedSession) implica reescribir Start + RestartSessionAt + StopSession y se difiere como sesión propia — la documentación inline marca el camino.

### Olor G — `Dependencies`+`runtime`+`main.run` god-trio (media-alta) · **parcial**

Antes:

```
runtime { server, streamManager, iptvService, iptvProxy, iptvTransmux,
          iptvScheduler, iptvProber, scanScheduler, imageRefreshScheduler,
          libraryService, authService, retention, database, dbDriver,
          logger }  // 16 campos
waitForShutdown(ctx, cancel, rt *runtime) → 98 LoC desempaquetando los
                14 punteros + 14 .Stop()/.Shutdown() encadenados con orden
                explícito en el cuerpo.
```

El comentario del repo lo admitía como el síntoma + workaround ("adding a new bg service is now a one-line struct-field append plus a Stop call inside waitForShutdown" = ES el smell, no la fix).

Después:

Nuevo fichero `cmd/hubplay/lifecycle.go` (93 LoC) con un `lifecycle` struct que agrupa componentes long-lived en **dos slices según fase**:

- **`workers`** — bg jobs independientes de HTTP (iptv scheduler, iptv prober, scan scheduler, image refresh scheduler, session cleaner, retention runner). Se paran PRIMERO en add-order — dejan de generar actividad antes de tirar el resto.
- **`services`** — componentes HTTP-coupled (stream manager, iptv service/proxy/transmux, library service). Se paran ÚLTIMO en **LIFO** (reverse-of-add) — el último wirings depende de los anteriores, así que tirarlo primero respeta el grafo.

Entre las dos fases va el `server.Shutdown(ctx)`. El root ctx se cancela tras services, antes de `db.Optimize` + `database.Close`.

main.run wirea cada componente y lo registra con una sola llamada:

```go
lc.AddWorker("iptv scheduler", func(ctx context.Context) error {
    iptvScheduler.Stop(ctx); return nil
})
```

Sin god-struct intermedio, sin desempaquetado posicional, sin "olvidé añadirlo a `runtime`". `waitForShutdown` pasó de 98 LoC a ~70. main.go neto: +13 LoC.

**Lo que NO se cierra en este commit**: el olor G del audit pide también extraer **feature modules** (`library.New(ctx, deps) *Module`, `iptv.New(ctx, deps) *Module`) que devuelvan service + workers + cleanup como una unidad. Cada módulo wraparía 3-9 sub-componentes (library: scnr + scheduler + refresher + segmentDetector + fingerprinter + fsWatcher; iptv: service + proxy + transmux + scheduler + prober + logo). Eso requiere un commit per-paquete porque toca seams entre paquetes (scanner shared library/iptv, libraryService passed a iptv proxy via interface, etc.). **Diferido como sesiones futuras**. Esta tanda cierra el síntoma del audit (god-struct + workaround comment) sin tocar la API pública de los paquetes feature.

### Olor H — `Dependencies` (57 campos, 22 `*db.X` concretos) — **pendiente, sesión propia**

Dos paths posibles según el audit:

1. **mountXxx helpers** (más simple) — split `r.Route("/api/v1", ...)` callback monolítico (~1100 LoC dentro del callback) en `mountAdmin(r, deps)`, `mountIPTV(r, deps)`, `mountFederation(r, deps)`, `mountItems(r, deps)`, etc. Cada helper recibe `Dependencies` + chi.Router. NewRouter pasa a ser una serie de calls a mountXxx.
2. **Interfaces en Dependencies** — los 22 `*db.X` concretos en Dependencies → interfaces. Los handlers ya consumen interfaces locales (consumer-side, bien); el contrato queda doblemente expresado. Más limpio arquitectónicamente pero más blast-radius.

router.go es **el fichero de mayor blast-radius del repo** (TODA el tráfico HTTP pasa por él). Hacer ambos (1+2) es la fix completa al 100% del olor. Mínimo viable: 3-4 mount helpers grandes (admin/system, iptv, federation) como proof-of-pattern, dejando el resto en NewRouter por ahora.

### Aprendizajes operativos

- **El audit's "LIFO slice" para teardown es too-simple en la práctica**: la ordenación de shutdown de HubPlay tiene 3 fases por **dominio** (workers independientes → HTTP drain → services HTTP-coupled), no LIFO de init order. Ejemplo: el `iptv scheduler` se wirea TARDÍSIMO pero hay que pararlo PRONTO (antes de HTTP) porque genera DB load durante shutdown. Strict LIFO lo haría al revés. La `lifecycle` con phased AddWorker/AddService captura el dominio.

- **Setters como API de tests es legítimo**: el audit JJ pide eliminar los 3 setters de `stream.Manager`, pero los tests usan `SetMetrics`/`SetEventBus` mid-test para swap de stubs. Decisión: setters se quedan (API pública para tests), producción usa Deps. El "Builder Pattern accidental" smell se refiere a producción, no a tests.

- **fillFromConfig() como retro-compat para tests minimalistas**: los dos integration tests sólo pasan `Config: cfg` y nunca lo tocarían. En vez de obligarles a llenar 13 primitivos nuevos, un helper privado al top de NewRouter rellena a-zero desde Config. Tests no se tocan, main.go usa el path idiomático, ambos caminos coexisten.

- **El comentario que admite el síntoma ES el smell**: el comentario de `runtime` en main.go ("adding a new bg service is now a one-line struct-field append…") presentaba el workaround como solución; el audit lo flaggea correctamente. Al cerrar G hay que sustituir tanto el código como el comentario — si dejara la justificación intacta, el lector futuro reintroducirí­a el smell pensando que es deliberado.

### Métricas globales de esta sesión

- **2 commits** sobre `claude/review-project-9YJxG`, pusheados.
- **4 olores cerrados** (V + JJ + LL completos + G parcial) de Iteración 6.
- **1 olor pendiente** (H) — para sesión propia.
- Tests: `go test -race ./...` verde en 2 corridas independientes (V+JJ luego con G), `golangci-lint`: 0 issues, `go vet`: limpio.
- LoC: `runtime` (16 campos) + `waitForShutdown` (98 LoC) eliminados; `lifecycle.go` (93 LoC) nuevo. main.go neto +13. router.go +115 (campos primitivos + docs + helper).

---

## 🔧 Sesión 2026-05-21 (post-cierre) — CC fase 2 + scanner W + dependabots

Sesión corta de extensión sobre la "tarde-noche" ya cerrada (#391). Cuatro PRs mergeadas: dos cierres estructurales del audit (CC fase 2 + W) y dos dependabot quick-wins. Quinta abierta esperando CI (#376 web-deps group).

### PRs mergeadas

| PR | Tema | Diff |
|---|---|---|
| [#392](https://github.com/Alexzafra13/HubPlay_demo/pull/392) | **Olor CC fase 2** — `ChannelOrderOps` extraído de `iptv.Service` | 6 ficheros (`service_channel_order.go` 646 LoC → `channel_order_ops.go` + 5 fields fuera del facade) |
| [#393](https://github.com/Alexzafra13/HubPlay_demo/pull/393) | **Olor W** — split `scanner.go` en 6 ficheros temáticos | scanner.go 1491 → 332 LoC (−78 %); +5 ficheros nuevos por carril funcional |
| [#289](https://github.com/Alexzafra13/HubPlay_demo/pull/289) | picomatch 4.0.3 → 4.0.4 (Dependabot alert #18) | lockfile-only |
| [#247](https://github.com/Alexzafra13/HubPlay_demo/pull/247) | docker/setup-buildx-action 3 → 4 | workflow only |

### CC fase 2 (PR #392) — cierra olor CC al 100 %

`iptv.Service` god-service (45 métodos, 11 sub-features — el más grande del repo) ahora descompuesto en facade + 4 sub-services:

| Sub-service | Métodos | Origen |
|---|---:|---|
| `FavoritesOps` | 5 | CC fase 1 (#390) |
| `WatchHistoryOps` | 2 | CC fase 1 (#390) |
| `HealthOps` | 5 | CC fase 1 (#390) |
| **`ChannelOrderOps`** | **16** | **CC fase 2 (esta sesión)** |

`ChannelOrderOps` agrupa **3 sub-features tightly-coupled por compartir overlay-at-read-time**:
1. Overlay por-usuario (6 métodos) — `user_channel_order` repo.
2. Overlay admin de library (5 métodos) — `library_channel_order` repo.
3. Overrides de logo + iptv-org refresh (5 métodos) — `channel_logo_overrides` + post-construction `iptvOrgLogos` lookup.

Helpers puros (`applyLogoOverlay`, `applyAdminOverlay`, `applyOrderOverlay`), constantes exportadas (`LocalLogoSentinel`) y tipo `IPTVOrgRefreshSummary` movidos junto a sus usuarios. `channels` repo compartido por puntero con Service core (todos los overlays parten de `channels.ListByLibrary`).

Mismo pattern embedding facade que CC fase 1 + QQ + P + Z. La method promotion preserva la API pública — `Service.SetIPTVOrgLogos(...)`, `Service.GetChannelsForUser(...)` etc. siguen accesibles externos sin que el facade los declare. **0 tests modificados** — los callers usan `svc.Method(...)` y la promotion preserva el surface.

**Detalle técnico nuevo** (no aplicado antes en CC fase 1 porque sus sub-services no necesitaban llamar a Service): `Service.GetChannels` no es accesible desde un método con receiver `*ChannelOrderOps` (embedding va hacia afuera, no hacia adentro). Solución: helper privado `listChannels(...)` dentro del sub-service que duplica las 3 líneas de la lógica de `GetChannels` (dispatch a `ListHealthyByLibrary` o `ListByLibrary`). Mismo patrón que `WatchHistoryOps` usa con `channels.GetByID`.

### Olor W (PR #393) — split scanner.go (1491 LoC) en 6 ficheros

`scanner.go` había crecido desde los 1270 LoC del audit original hasta 1491. Split textual puro por carril funcional — el código sigue montado contra el mismo `*Scanner`, sólo dispersado.

| Fichero | LoC | Responsabilidad |
|---|---:|---|
| `scanner.go` | 332 | Top-level: struct + `New` + `ScanLibrary` + `walkPath` + `iterateLibraryItems` + helpers de naming |
| `scan_walk.go` | 285 | Procesamiento por fichero: `processFile` + `createItem` + `updateItem` + `probeResultTo*` + `fingerprint` |
| `enrich.go` | 343 | Enrichment movies/series: `RefreshMetadata` + `enrichIfMissing` + `enrichMetadata` + `applyMetadata` |
| `enrich_season_episode.go` | 242 | Enrichment hojas: `enrichSeason` + `enrichEpisode` + `fetchAndStore*` |
| `identify.go` | 273 | Flujo humano: `SearchCandidates` + `IdentifyAndApply` + `UpdateItemMetadata` + `RefreshItemMetadata` + lock |
| `media_ingest.go` | 149 | Ingest externo: `fetchAndStoreImages` + `syncPeople` |

Total post-split: 1624 LoC (+133 por doc headers de cada fichero explicando responsabilidad). **0 cambios de comportamiento** — métodos públicos preservados verbatim, tests pasan sin tocar.

### Cómo se mergeó #376 (lección operativa)

Al rebasear #376 (web-deps group, 17 updates) contra el nuevo main tras mergear #289, hubo conflicto en `pnpm-lock.yaml`. Solución: `@dependabot recreate` como comentario en la PR — dependabot regenera el lockfile desde main fresh. Sigue siendo lo correcto cuando dos PRs npm tocan el lockfile en paralelo, aunque el `dependabot.yml` ya limite a 1 PR npm concurrente (cambio de sesión 2026-05-20 tarde).

### Audit 2026-05-14 — olores altos restantes

De los 6 olores altos originales del audit:

| Olor | Estado | Sesión cerradora |
|---|---|---|
| A+M | ✅ cerrado | Iter 3 (M.6/7/8) |
| B+J | ✅ cerrado | Iter 2 (M.2) |
| **CC** | ✅ **cerrado** (esta sesión, PR #392) | CC fase 1 + 2 |
| P | ✅ cerrado | Iter 4 (#386 + #388) |
| **W** | ✅ **cerrado** (esta sesión, PR #393) | Iter 7 |
| F14-2-a | ⏳ pendiente | Iter 8 |

**5 de 6 olores altos cerrados.** Sólo queda F14-2-a (function-level quality) de los altos.

### Aprendizajes

- **Embedding facade va hacia afuera, no hacia adentro**: cuando un método de sub-service necesita llamar a otro método del Service core (e.g. `GetChannels`), hay que duplicar la lógica como helper privado del sub-service. No es viable hacer back-reference porque rompe el grafo. En CC fase 2 esto fue 3 líneas — el coste es nominal y el aislamiento del sub-service permanece intacto.
- **El split textual de un god-FILE (W) es más mecánico que el de un god-SERVICE (CC, P, Z, QQ)**: no requiere extracción de fields ni embedding facade, sólo cortar bloques por responsabilidad y verificar imports. El audit los flagga distinto por algo: el "alto" estructural es por tamaño del fichero en bytes, no por complejidad arquitectónica.
- **`@dependabot recreate` es el camino para colisiones de lockfile**: más limpio que rebasear a mano o hacer recover via merge. Si la PR original tenía 17 deps, la recreación también las tendrá (dependabot re-resuelve contra main).

### Métricas globales

- **4 PRs mergeadas en esta sesión** (2 cierres del audit + 2 dependabots) + 1 abierta esperando CI.
- **2 olores del audit cerrados** (CC al 100 %, W).
- **6 → 1 olores altos pendientes** (queda F14-2-a).
- **Dependabot alerts 2 → 1** (cerrada alert #18 picomatch). Queda file-type (transitive de node-vibrant, sin solución upstream).

---

## 🔧 Sesión 2026-05-21 (tarde-noche) — Audit Iter 4 cerrada + seguridad Dependabot

Sesión larga (10 PRs en una tarde). Tres olores estructurales del audit 2026-05-14 cerrados al 100 % (QQ + P + Z) + uno en fase parcial (CC). Dos PRs de seguridad cerraron 6 de 8 alertas Dependabot (1 critical). Tests vitest de páginas grandes cubiertos. Memoria del repo limpiada (un spec obsoleto identificado y archivado).

### PRs cerradas y abiertas

| PR | Tema | Estado | Cambio principal |
|---|---|---|---|
| [#381](https://github.com/Alexzafra13/HubPlay_demo/pull/381) | VideoPlayer split en 4 hooks + 2 overlays | ✅ merged | VideoPlayer.tsx 1166 → 817 LoC (−30 %) |
| [#382](https://github.com/Alexzafra13/HubPlay_demo/pull/382) | Tests vitest Home / LiveTV / Search / Movies / Series | ✅ merged | +30 tests (616 → 646), 90 test files |
| [#383](https://github.com/Alexzafra13/HubPlay_demo/pull/383) | Archivar `per-user-channel-order-pending.md` (shipped) | ✅ merged | memoria limpia |
| [#384](https://github.com/Alexzafra13/HubPlay_demo/pull/384) | **Olor QQ** — auth.Service split en 4 sub-services | ✅ merged | service.go 764 → 180 LoC (−76 %) |
| [#385](https://github.com/Alexzafra13/HubPlay_demo/pull/385) | to-ico → png-to-ico (seguridad) | ✅ merged | **5 CVEs cerradas (1 critical)**, −87 paquetes pnpm |
| [#386](https://github.com/Alexzafra13/HubPlay_demo/pull/386) | **Olor P** — ItemHandler split fases 1-2 (3 sub-handlers) | ✅ merged | items.go 1211 → 444 LoC |
| [#388](https://github.com/Alexzafra13/HubPlay_demo/pull/388) | Olor P fases 3-4 (MetadataHandler + ItemDetailHandler) — cierre completo | 🟡 open | items.go 444 → 266 LoC (−78 % total) |
| [#389](https://github.com/Alexzafra13/HubPlay_demo/pull/389) | **Olor Z** — library.Service split (AccessControl + ItemQueries) | 🟡 open | service.go 593 → 498 LoC |
| [#390](https://github.com/Alexzafra13/HubPlay_demo/pull/390) | **Olor CC fase 1** — iptv.Service Favorites + WatchHistory + Health | 🟡 open | 3 sub-services extraídos |

### Patrón refactor: embedding facade + shared publisher

Mismo patrón aplicado para los 4 olores estructurales (QQ + P + Z + CC fase 1) — establecido una vez en QQ (auth.Service) y replicado mecánicamente en los otros tres:

```go
type Service struct {
    *SubServiceA   // 5 methods (promoted via embedding)
    *SubServiceB   // 3 methods
    *SubServiceC   // 7 methods
    // ... core state que se queda en el facade

    pub *publisher // compartido por puntero con sub-services que publican
}

func (s *Service) SetEventBus(bus *event.Bus) {
    s.pub.setBus(bus) // muta el publisher → ambos sub-services ven el cambio
}
```

**Características del patrón:**

1. Sub-services son structs propios con sus deps mínimas — la audit nota "cada constructor toma 3-4 deps en vez de 13".
2. Facade Service embed punteros — method promotion intra-paquete preserva el surface externo. Handlers + tests + router siguen llamando `svc.Method(...)` exactamente como antes.
3. Constructor distribuye args del `NewXxx` pre-split a sub-constructors. La firma pública del constructor se preserva para minimum-blast-radius.
4. Estado compartido (rate-limiter, event bus, issuer de sesiones) via struct dedicado compartido por puntero — un solo `SetEventBus` muta el campo en todos los sub-services a la vez.
5. **Tests pre-existentes pasan SIN CAMBIOS**. Cero churn en ningún caller externo.

**Aplicado en orden de tamaño creciente:**

| Olor | God-service / handler | Sub-units | LoC delta |
|---|---|---|---|
| **QQ** | auth.Service (18 métodos, 6 responsabilidades) | LoginService + AccountService + SessionService + ProfileService | 764 → 180 (−76 %) |
| **P** | ItemHandler (19 métodos, 14 fields, 4 responsabilidades) | ItemDetailHandler + TrickplayHandler + SearchHandler + RecommendationsHandler + MetadataHandler | 1211 → 266 (−78 %) |
| **Z** | library.Service (27 métodos, 6 responsabilidades) | AccessControl + ItemQueries + facade (CRUD/scan/lifecycle) | 593 → 498 (−16 %, menos drástico porque el core CRUD+scan se queda) |
| **CC fase 1** | iptv.Service (45 métodos, 11 sub-features) | FavoritesOps + WatchHistoryOps + HealthOps + facade (M3U/EPG/...) | 343 → 379 (+36, overhead doc — saca 12 métodos del facade) |

### Seguridad: Dependabot alerts (8 → 2)

De 8 alerts (2 critical, 6 medium) bajamos a 2 medium con una PR + un dependabot pendiente:

- **#385** reemplazó `to-ico@1.1.5` (devDep con cadena `jimp@0.2.28 → resize-img → request 2.88.2 → form-data 2.3.3 / qs / tough-cookie / minimist 0.0.8`) por `png-to-ico@3.0.1` (3 deps directas, todas current). Cierra **5 alertas**:
  - #34 critical: `form-data` unsafe random for boundary
  - #35 medium: `qs` arrayLimit bypass → DoS
  - #33 medium: `tough-cookie` Prototype Pollution
  - #32 medium: `request` SSRF (paquete EOL)
  - #27 medium: `minimist` Prototype Pollution
- PR de dependabot **#289** (picomatch 4.0.3 → 4.0.4) cuando se mergee cerrará la 6ª (medium).
- Quedan **2 medium**: `file-type` (transitive de `node-vibrant@4.0.4` que ya está en latest — bloqueada hasta que upstream actualice jimp interno) + otro. No bloqueable sin breaks.

### Aprendizajes para futuras sesiones

- **Squash merge puede dropear commits posteriores al título del PR**. PR #386 fue mergeada con título "fases 1-2" cuando el branch ya tenía fases 3-4 commiteadas encima. El squash sólo aplicó lo que el título describía; los commits posteriores se quedaron en el branch sin llegar a main. Recovery: nueva PR (#388) desde origin/main fresh con sólo el diff de las fases pendientes. **Lección para PRs incrementales con título "fase X"**: actualizar el título antes del merge si se añaden fases, O crear PRs nuevas por incremento.

- **El patrón embedding facade se establece una vez (auth) y se reaplica mecánicamente** a P/Z/CC. Las 4 PRs estructurales salieron en ~6 horas combinadas. El audit estructura los olores P/Z/CC como god-services todos del mismo flavor, así un patrón único cierra los tres.

- **Frontend tests masivos (+30) usaron `vi.hoisted` + stub agresivo de subcomponentes pesados** (rails de Home, EPGGrid de LiveTV, MediaGrid de MediaBrowse, etc.). Plantilla en `MediaBrowse.test.tsx` ya existía — el copy/paste/customize fue rápido.

- **Spec docs en `docs/memory/` pueden quedarse stale sin que nadie lo note**. El `per-user-channel-order-pending.md` decía "NOT IMPLEMENTED" pero la feature llevaba meses en main (migraciones 042 + 043, `LiveTvCustomize.tsx`, etc.). Al arrancar a trabajarla descubrí la duplicación accidental potencial. **Convención**: cuando una feature ship, mover el spec a `archive/` con header "SHIPPED" + diferencias respecto al spec original. Lo hace PR #383.

- **`png-to-ico` es el reemplazo moderno de `to-ico`** (~10 años sin actualizar). API compatible (acepta array de PNG buffers), 3 deps en vez de 50+, mantenido 2024.

- **El field promotion intra-paquete funciona para fields no exportados**. En P fase 4, `ItemDetailHandler.identifier` (lowercase) sigue accesible vía `itemHandler.identifier` desde otros métodos del mismo paquete `handlers` porque `*ItemHandler` embed `*ItemDetailHandler`. Cross-paquete sería distinto pero no aplica aquí.

### Métricas globales

- **10 PRs abiertas en la sesión** (7 mergeadas, 3 abiertas esperando review)
- **3 olores del audit cerrados** (QQ + P + Z) + 1 en fase parcial (CC fase 1)
- **8 → 2 vulnerabilidades Dependabot** (75 % reducción, 1 critical eliminada)
- **+30 tests vitest** (616 → 646, 5 nuevas páginas cubiertas)
- LoC shrunk: VideoPlayer −349, auth.Service −584, ItemHandler −945 (78 %), library.Service −95

---

## 🧹 Sesión 2026-05-21 (mañana) — Cleanup knip a 0 + React Doctor quick wins + hard gate

Sesión corta y limpia. Tres PRs encadenadas para cerrar la deuda de dead code que arrastraba desde la integración inicial de knip (PR #355) y atacar los 3 issues mecánicos de React Doctor que aparecieron tras el cleanup.

### PRs cerradas

| PR | Tema | Diff |
|---|---|---|
| [#375](https://github.com/Alexzafra13/HubPlay_demo/pull/375) | 5 unused files + 2 unused deps (`@radix-ui/react-dialog`, `@radix-ui/react-tooltip`) | −739 |
| [#377](https://github.com/Alexzafra13/HubPlay_demo/pull/377) | 7 hooks + 9 huérfanos + 30+ exports/types: `export` → interno o borrado entero | −327 |
| [#378](https://github.com/Alexzafra13/HubPlay_demo/pull/378) | 3 mecánicos React Doctor: skeleton keys, animation 1.8s→900ms, `new Date()` JSX → helper | +19/−12 |
| Este branch | `pnpm knip` elevado a hard gate en CI + memoria actualizada | — |

### Cleanup knip: lo que se aprendió

- **`import("./types").Foo` se cuela sin que knip lo detecte.** Patrón usado en `client.ts` y `media.ts` para algunos types — knip los marca como unused aunque sí se usen. Solución: sustituir por imports normales al top. Detectados 2 (PeerStreamSessionResponse, StudioDetail) y migrados.
- **Hooks "anti-pair" hibernando.** `useEnableChannel` existía como complemento de `useDisableChannel` que sí está conectado a UI; el enable nunca se conectó. Mismo patrón: `useSetChannelVisibility` admin sin UI. Si vuelven a hacer falta cuando se implemente la feature, son 5 líneas — borrarlos hoy fue safe.
- **El barrel `*/index.ts` no añade valor si todos los consumers importan archivos directos.** Casos: `components/layout/index.ts` y `pages/admin/index.ts` (5 líneas cada uno, 0 importadores). Borrados enteros.
- **`*Props` types nunca se importan**, aunque el component se importa miles de veces. Limpieza: quitar `export` keyword del type, mantener como tipo interno usado por el `function Component(props: Props)`. Cero impacto en consumers.

### Falsos positivos React Doctor — documentados, NO se tocan

Convención del repo: cuando una regla react-doctor entra en conflicto con un patrón oficial de React 19 o con `react-hooks/refs`, se prefiere el patrón oficial y se suprime el aviso con justificación inline.

| Regla | Archivo | Por qué se deja |
|---|---|---|
| `rerender-state-only-in-handlers` + `no-derived-useState` | MediaGrid.tsx:43, UserAvatar.tsx:64 | Patrón "[Adjusting state when a prop changes](https://react.dev/learn/you-might-not-need-an-effect#adjusting-some-state-when-a-prop-changes)" (React 19 oficial). El `useState` de tracking se mueve a render-time guarded `setState`. Usar `useRef` aquí dispara `react-hooks/refs` (no asignar `ref.current` en render). Es el único patrón que satisface las dos reglas estrictas de react-hooks. |
| `no-derived-useState` | ExternalSubsModal.tsx:39 | El state es **edición local del usuario** tras inicializar desde prop. Derivar en render reiniciaría su selección de idiomas cada vez que el padre se re-renderice. |

### Issues React Doctor estructurales pendientes — todos en `VideoPlayer.tsx`

VideoPlayer es 1003 líneas, 12 `useState`, un `useEffect` con 9 `setState` en cadena, y un `useEffect` que resetea state cuando un prop cambia (debería ser `key` prop). Las reglas implicadas:

- `no-giant-component` (1003 LoC)
- `prefer-useReducer` (12 useState)
- `no-cascading-set-state` (línea 509 — 9 setState en un effect)
- `no-derived-state-effect` (línea 628 — reset por prop change)

Para atacar de forma sensata hay que **split de VideoPlayer en subcomponentes** y consolidar el state en `useReducer`. Es refactor estructural, no auto-fix mecánico. Requiere backend corriendo en preview para verificar playback / seek / quality switching tras el split — sesión dedicada.

Mismas reglas también disparan en `HeroSection.tsx` (192 LoC) y `SeriesHero.tsx` (61 LoC) pero más manejables.

---

## 🩺 Sesión 2026-05-20 (noche) — React Doctor onboarding + 9 reglas eliminadas

**[React Doctor](https://github.com/millionco/react-doctor)** (de millionco, MIT, basado en Oxlint) audita 60+ reglas en performance, correctness, accessibility, architecture, security y bundle size, y resume todo en un score 0-100. Integrado en CI como visibility-only (PR #358) — la GitHub Action comenta inline en cada PR con las regresiones/mejoras del score. Fórmula: `score = 100 - (errores únicos × 1.5) - (warnings únicos × 0.75)`. Bandas: 75+ "Great" / 50-74 "Needs work" / <50 "Critical".

**Baseline al integrar**: 67/100, 645 issues, 47 reglas únicas.
**Final tras 5 PRs de quick wins + el fix del squash bug**: **75/100 ("Great"), 166 issues, 34 reglas únicas**.

### PRs cerradas

| PR | Reglas eliminadas | Casos resueltos |
|---|---|---|
| [#358](https://github.com/Alexzafra13/HubPlay_demo/pull/358) | — | Integración CI (job nuevo `react-doctor`, visibility-only con `continue-on-error`) |
| [#359](https://github.com/Alexzafra13/HubPlay_demo/pull/359) | `js-tosorted-immutable`, `js-combine-iterations`, `design-no-redundant-size-axes` | ~430 reemplazos en ~140 archivos |
| [#360](https://github.com/Alexzafra13/HubPlay_demo/pull/360) | `design-no-redundant-padding-axes`, `design-no-bold-heading`, `no-autofocus`, `design-no-em-dash-in-jsx-text`, `use-lazy-motion`, `rendering-hydration-mismatch-time` | 6 reglas mecánicas, −30 KB bundle, helper `dateFormat` nuevo. **Squash merge perdió 54 líneas del LazyMotion — fixed en #364** (ver abajo) |
| [#363](https://github.com/Alexzafra13/HubPlay_demo/pull/363) | `click-events-have-key-events`, `no-static-element-interactions` | 17 + 13 casos de a11y. Backdrops de modal con `onKeyDown` (Escape), bodies con `role="presentation"`, VideoPlayer container con `role="application"` + `aria-label` |
| [#364](https://github.com/Alexzafra13/HubPlay_demo/pull/364) | `use-lazy-motion` (otra vez) | Re-aplicar los 54 reemplazos `motion` → `m` que perdió el squash de #360 |
| [#365](https://github.com/Alexzafra13/HubPlay_demo/pull/365) | — | Memoria: bug del squash + regla "audit del merge" en conventions |
| [#367](https://github.com/Alexzafra13/HubPlay_demo/pull/367) | `no-array-index-as-key` (×2), `no-render-in-render`, `js-set-map-lookups`, `prefer-use-effect-event` (×2), `advanced-event-handler-refs`, `no-react19-deprecated-apis` (`forwardRef`), `no-scale-from-zero` | **Cruza el umbral 75 ("Great")**. Patrón "latest value via ref" (sustituto pragmático de `useEffectEvent`), `<ProfilePicker />` extraído del closure inline, `forwardRef` → `ref` prop |

### ⚠️ Bug del squash merge — 54 líneas de LazyMotion perdidas en #360

Descubierto el 2026-05-20 noche por el CI report de React Doctor en PR #363: la regla `use-lazy-motion` reapareció en `WhoIsWatching.tsx:27` aunque PR #360 supuestamente la había eliminado.

**Qué pasó**: el commit `LazyMotion + helper de fechas` migró 7 archivos de `motion` → `m` (54 reemplazos verificados localmente). Pero el squash merge de la PR a main aplicó SÓLO las modificaciones de otro commit anterior (autoFocus → useEffect+ref, ~8 líneas) y descartó silenciosamente las 54 líneas de LazyMotion sobre los mismos 7 archivos. `App.tsx` SÍ se mergeó con `LazyMotion strict` activado, dejando los archivos rotos: el primer usuario que llegase a Login, WhoIsWatching, MainNav, etc, vería `Error: You are rendering "motion" without LazyMotion features loaded`.

**Cómo se cazó**: React Doctor en CI dice "Score unavailable in offline mode" pero sí lista los issues. La regla `use-lazy-motion` apareció DE NUEVO en `WhoIsWatching.tsx:27` cuando ya debería estar resuelta. Auditados los 7 archivos del PR original: ninguno tenía la migración. Re-aplicado el mismo script en PR #363.

**Lección aprendida** (en `conventions.md`):
- Tras cualquier squash merge donde un script modifica decenas de archivos, **verificar en main** que los cambios reales coinciden con el diff del commit local. El visor de PR de GitHub puede ocultar conflict resolutions silenciosas.
- Si una regla del lint que se eliminó vuelve a aparecer en un PR posterior, NO asumir que es un regreso nuevo del autor — comprobar si el bug del lint ya estaba en main por un merge previo malo.

### Patrones nuevos en el proyecto

- **`[arr].toSorted()` en lugar de `[...arr].sort()`** (ES2023): ya el patrón canónico en el repo, evita el spread allocation.
- **`.flatMap()` para combinar filter+map**: en JSX o reductores rápidos. Evita el doble recorrido sobre listas grandes (cientos de canales).
- **Tailwind `size-N` y `p-N` consolidados**: nunca `w-4 h-4` o `px-2 py-2`. Convención del proyecto a partir de ahora.
- **`<m.*>` de framer-motion** en lugar de `<motion.*>`. `LazyMotion strict` en `App.tsx` carga sólo `domAnimation` por defecto (~30 KB menos en el bundle base).
- **`react-compiler/react-compiler: 'error'`** en ESLint. Las regresiones de compatibilidad con el compiler rompen el CI.
- **Helper centralizado [`src/utils/dateFormat.ts`](../../web/src/utils/dateFormat.ts)**: `formatDateTime`, `formatDate`, `formatTime`, `epochOf`. Nunca `new Date(...).toLocale*()` directo en JSX.
- **`localId: crypto.randomUUID()`** en entradas de listas dinámicas (LibrariesStep del setup wizard) para que React keys no usen índices (evita perder foco al reordenar).
- **`font-semibold` (no `font-bold`)** en headings `<hN>`: peso 700+ aplasta las contraformas a display sizes.
- **Em-dash (—) en JSX text NUNCA**: usar en-dash (–) para "sin valor" o bullet (·) para separadores inline. Em-dash lee como output AI.
- **`autoFocus` evitado**: interfere con lectores de pantalla. Cuando es UX-crítico (UpNextOverlay, WhoIsWatching PIN), patrón `useEffect + ref.current.focus()`.
- **Patrón "latest value via ref"** (sustituto pragmático de `useEffectEvent`, que aún es experimental en React 19): cuando un effect monta un listener cuyo handler depende de un prop/state pero el effect NO debería re-suscribirse al cambiar esa identidad:
  ```tsx
  const cbRef = useRef(onClose);
  useEffect(() => { cbRef.current = onClose; }, [onClose]);
  useEffect(() => {
    if (!isOpen) return;
    const handle = (e: Event) => { /* … */ cbRef.current(); };
    el.addEventListener("event", handle);
    return () => el.removeEventListener("event", handle);
  }, [isOpen]); // onClose ya NO es dep
  ```
  Aplicado en BottomSheet (escape), VideoPlayer (onEndedCallback) y ImageManager (escape). Sin esto, cada re-render del padre re-suscribiría el listener y perdería eventos durante el churn.
- **`forwardRef` eliminado de Button/Input** (React 19): ahora `ref` es prop normal en componentes función. Declarar `ref?: Ref<HTML*Element>` en la interfaz de props y desestructurar en el componente.
- **Closures de render → componente real**: ejemplo `WhoIsWatching`, donde `const renderPicker = (...)` inline se extrajo a `<ProfilePicker />` con props explícitas. Mejora reconciliación y satisface `react-doctor/no-render-in-render`.
- **Set para lookups en bucles**: `ACCEPTED_EXTENSIONS_SET = new Set(ACCEPTED_EXTENSIONS)` en `Uploads.tsx`. `.includes()` en loop = O(n²); Set = O(1).
- **Backdrops de modal accesibles**: `<button>` semántico cuando es posible, o `<div role="dialog">` con `onClick`+`onKeyDown` (Escape) + body interno `role="presentation"` con `stopPropagation` en ambos handlers.
- **`<video>` container con `role="application"`**: comunica al lector de pantalla que es un widget interactivo; añade `aria-label`.

### Reglas no eliminadas (decisiones documentadas como deuda técnica)

| Regla | Casos | Por qué se deja |
|---|---|---|
| `no-pure-black-background` | 7 | `bg-black` en contenedores de video. Cambiar a `bg-bg-base` dejaría borde gris alrededor. |
| `query-mutation-missing-invalidation` | 12 | Falsos positivos: mutations read-only (probe peer, test DB, preflight M3U, deviceAuth tres) o invalidación vía helper indirecto que el lint no detecta (images.ts). |
| `rerender-state-only-in-handlers` | 23 | **Conflicto irreconciliable** entre `react-hooks/refs` (no asignar `ref.current` en render) y `react-doctor` (que pide ese patrón). El `useState` de tracking del patrón "Adjusting state when a prop changes" satisface las dos reglas de react-hooks aunque viole esta de react-doctor. |
| `no-derived-useState` | ~6 falsos positivos | Casos donde `useState` se inicializa de un prop PERO el estado representa edición local del usuario (CollisionPicker decisiones, ExternalSubsModal langs). Derivar en render reiniciaría el trabajo del usuario en cada re-render del padre. Suprimidos narrow con justificación. |

### Reglas pendientes para próxima(s) sesión(es)

**Decisión 2026-05-20 noche**: aplazar la segunda ola **hasta que la web
esté en estado "terminada"** (menos churn de componentes). El criterio de
entrada para la segunda ola es:

1. **Subir el gate del CI** a `min-score: 80` o `85` en
   `.github/workflows/ci.yml` y quitar `continue-on-error`. Convierte
   visibility-only en hard gate y bloquea regresiones.
2. **Refactor mayor de los grandes**: aquí es donde hay rendimiento real,
   no en los micro-quick-wins.
   - `no-giant-component` (15): UsersAdmin, VideoPlayer, AuditLogPanel,
     WhoIsWatching, LogsPanel — split por sub-componentes con
     responsabilidad única.
   - `prefer-useReducer` (22) + `no-cascading-set-state` (7): consolidar.
     **useHls (23 setState en un effect)** es el más urgente — cada
     setState dispara un render durante la carga del stream, un reducer
     único lo colapsa a uno.
3. **`no-array-index-as-key` restantes** (13) cuando el backend exponga
   IDs estables o se generen con `crypto.randomUUID()` al ingest.
4. **`rendering-hydration-mismatch-time` complejos** (15): callbacks de
   Recharts y datos paginated. No hacemos SSR; riesgo real bajo, pero
   limpia.

### Reglas que NUNCA se van a eliminar (falsos positivos legítimos)

Documentar aquí para que ningún PR futuro pierda tiempo:

- `rerender-state-only-in-handlers` (23): **conflicto irreconciliable**
  con `react-hooks/refs`. El patrón "Adjusting state when a prop changes"
  viola esta regla pero satisface las dos de react-hooks (que son
  hard-gates). Ganan los hooks.
- `query-mutation-missing-invalidation` (12): falsos positivos —
  mutations read-only (probe peer, test DB, preflight M3U, deviceAuth) o
  invalidación vía helper indirecto que el lint no detecta (images.ts).
- `no-derived-useState` (6 de 8): casos donde el `useState` representa
  edición local del usuario (CollisionPicker decisiones, ExternalSubsModal
  langs). Derivar en render reiniciaría el trabajo del usuario en cada
  re-render del padre. Suprimidos narrow con justificación.
- `no-pure-black-background` (7): `bg-black` en contenedores de video.
  Cambiar a `bg-bg-base` dejaría borde gris alrededor.
- `label-has-associated-control` (6): asociación implícita (`<label>`
  envolviendo `<input>`) **ES a11y válida**. Falso positivo del lint.
- `async-await-in-loop` (2): stream reader (DatabasePanel SSE) + retry
  loop con backoff (api/client). Secuencial por diseño.
- `async-defer-await` (2): la awaited value SÍ se usa después del
  early-return (ItemDetail onClose, useVibrantColors swatches).
- `no-derived-state-effect` (1): VideoPlayer:628 con `key={itemId}` re-
  montaría hls.js. Documentado in-line con eslint-disable.
- `client-localstorage-no-version` (2) + `js-cache-storage` (1): test
  files. Cambiar la key rompe migración real (production usa
  `hubplay_user`).

### PRs dependabot abiertas (estado)

Las que sobrevivieron a la limpieza de tarde (#247, #289, #352 ya mergeadas, #330 cerrada por redundancia):

| PR | Tema | Estado |
|---|---|---|
| Nuevas dependabot semanales | — | A revisar la próxima vez que se abran (lunes) |

---

## 🏗️ Sesión 2026-05-20 (tarde) — Limpieza profunda: lockfile, React Compiler y quality gates

Sesión larga (~10 PRs en una tarde) iniciada como "revisar PRs dependabot" y derivada hacia un saneamiento general del frontend. Catch importante: **main estaba rojo** en CI / Lint y CI / Test Backend, y casi todas las PRs de dependabot heredaban esos fallos sin que tuviera nada que ver con ellas.

### Cadena de causa → efecto descubierta

1. **CI / Lint roto** en main: 8 issues que aparecieron cuando `golangci/golangci-lint-action@v7` empezó a resolver al binario `v2.5.0` con reglas más estrictas (ineffassign, ST1023, SA9003, ST1019, QF1001, unused). Lo bloqueaba cualquier dependabot trivial (postcss bump, picomatch bump, etc).
2. **CI / Test Backend roto** en main: dos flakes en `internal/iptv` aparecían bajo `-race -coverprofile` cuando el watcher goroutine se preempta más de lo previsto. `TestTransmuxManager_PromotesToReencodeOnCodecCrash` y `TestTransmuxManager_Touch_KeepsSessionAlive`.
3. **Workflow Release roto**: `git describe` se calculaba en dos jobs distintos (`build` y `windows-installer`), y entre ambos `release-nightly` reescribía el tag `nightly`. Resultado: nombres de zip desalineados, `unzip exit 9` rompiendo el instalador Windows.
4. **`pnpm-lock.yaml` corrupto**: `lru-cache@11.5.0` triplicado en `packages:` y `snapshots:` tras mergear 4 PRs de dependabot npm en sucesión rápida en la mañana (#290 → #338 → #248 → #141 en ~30 min). GitHub squash-merge no detecta colisiones semánticas de YAML.
5. **`web/package-lock.json` huérfano** commiteado desde el inicio del repo aunque el proyecto usa pnpm. Nunca se sincronizaba con `pnpm-lock.yaml`.

### Lo que se cerró (mergeado a main esta tarde)

- **[#345](https://github.com/Alexzafra13/HubPlay_demo/pull/345)** — Release workflow: nuevo job `meta` centraliza `git describe` y el resto consume `needs.meta.outputs.*`. Race del tag `nightly` desaparece.
- **[#346](https://github.com/Alexzafra13/HubPlay_demo/pull/346)** — Lint cleanups (8 issues) + deadline interno del flake `PromotesToReencodeOnCodecCrash` 5s → 15s.
- **[#347](https://github.com/Alexzafra13/HubPlay_demo/pull/347)** — `git rm web/package-lock.json` + añadir al `.gitignore` (junto con `yarn.lock`).
- **[#348](https://github.com/Alexzafra13/HubPlay_demo/pull/348)** — Flake `Touch_KeepsSessionAlive`: IdleTimeout 200ms → 500ms, Wait 700ms → 1500ms, ctx 5s → 10s, + Touch sincrónico antes de lanzar la goroutine.
- **[#349](https://github.com/Alexzafra13/HubPlay_demo/pull/349)** — Fix quirúrgico del lockfile corrompido (12 líneas borradas, sin tocar versiones).
- **[#350](https://github.com/Alexzafra13/HubPlay_demo/pull/350)** — `dependabot.yml`: `open-pull-requests-limit` npm 5 → 1 para prevenir futuras colisiones de lockfile.
- **6 PRs de dependabot mergeadas en cadena**: #137 (golangci-lint-action), #138 (setup-qemu), #141 (jsdom 28→29 major), #248 (go-deps group, 7 paquetes), #290 (postcss), #338 (tough-cookie 2→6, transitive lockfile-only). Más #280 (vite dev), #351 (go-deps), #353 (download-artifact) en la última pasada.

### Lo que queda en flight (abierto al cierre de sesión)

- **[#354](https://github.com/Alexzafra13/HubPlay_demo/pull/354) — Activación del React Compiler**. Resultado final: 0 errors de lint, 616/616 vitest, build limpio, healthcheck 542/542 compatibles. Cambios:
  - `babel-plugin-react-compiler@1.0.0` integrado vía `react({ babel: { plugins: [...] } })` en `vite.config.ts`.
  - `eslint-plugin-react-compiler@19.1.0-rc.2` con regla `react-compiler/react-compiler: 'error'`.
  - `eslint-plugin-react-hooks` 7.0.1 → 7.1.1 (las reglas estrictas que destaparon los anti-patterns).
  - **15 anti-patterns refactorizados** + 5 extras que sólo aparecían en CI:
    - **`set-state-in-effect` → render-time guarded setState** (patrón oficial React 19 "Adjusting state when a prop changes"): MainNav (route change), SearchBar (URL ?q=), LinkDevice (URL code), LibraryNewPage (validation reset), UsersAdmin (×2: access draft + libraryIds seed), useVibrantColors (palette swap), PairThisDevice (×2: mount-once + qrSvg reset).
    - **`refs during render`**: useLiveHls (ref-assign movido a useEffect), PlayerControls (`reportMenu` envuelto en useCallback), MainNav (`clearTimers` con disable narrow + justificación porque cancelar el timer es necesario funcionalmente).
    - **`immutability`**: HeroTrailer (`handleDismiss` declarado antes del useEffect que lo usa, envuelto en useCallback).
    - **`exhaustive-deps`**: VideoPlayer (`BURNABLE_CODECS` a module scope), MyNotifications + PeerLibraryItemsPage (derivar dentro del useMemo).
    - **5 fixes extra del CI** (no aparecieron en mi lint local porque mi script de filtrado descartó `react-compiler/*` como derivados, cuando algunos son detecciones primarias): useHls + usePlayerKeyboard (suppress narrow para mutar `HTMLMediaElement.src` / `.currentTime` que es API estándar del DOM, no state); useProgressReporter (añadir `[videoRef, itemId, peerId]` a deps reales del effect del cleanup); PairThisDevice (añadir `[poll, navigate]` a deps SSE); Uploads (patrón "latest value via ref" con useEffect dedicado para actualizar el ref + cleanup que lo lee al desmontar — sin esto, añadir `active` a deps abortaría uploads en cada cambio de estado).
- **[#355](https://github.com/Alexzafra13/HubPlay_demo/pull/355) — Quality gates extra en CI**. Tres nuevos steps:
  - **`pnpm typecheck`** (`tsc -b`) — hard gate. Antes el typecheck sólo corría en `pnpm build`, que CI no ejecuta para frontend.
  - **`pnpm dlx react-compiler-healthcheck`** — hard gate. Falla si la compatibilidad baja del 100%. Funciona independientemente de si el compiler está activado.
  - **`pnpm knip`** con `--no-exit-code` — info-only (job separado). Hoy reporta 5 unused files + 2 unused deps + 38 unused exports + 115 unused types. Cuando lleguemos a 0, elevamos a hard gate.

### Aprendizajes que se aplican a futuras sesiones

- **Antes de "arreglar" una PR de dependabot que falla en lint o test backend, comprobar primero si main está rojo**. La mitad del tiempo el problema no es del bump, es heredado.
- **`open-pull-requests-limit: 1` para npm** es la respuesta correcta a "GitHub squash-merge corrompe lockfiles cuando dos PRs lo tocan en paralelo". El precio (bumps serializados) es barato comparado con `ERR_PNPM_BROKEN_LOCKFILE` en producción.
- **`eslint-plugin-react-compiler` es estricto pero correcto**. Cuando reporta "Component skipped because rules were disabled", suele ser señal de que el `eslint-disable react-hooks/*` esconde un anti-pattern real, no que el plugin sea pedante.
- **No te fíes de tu lint local si filtra reglas**. Cinco anti-patterns aparecieron sólo en CI porque mi script filtraba `react-compiler/*` como "derivados" — pero el compiler también detecta cosas primarias.

---

## 🧹 Sesión 2026-05-20 — Pendientes pequeños + limpieza

Después de cerrar la fase de distribución (sesión 2026-05-19/20), barrido de los pendientes "pequeños y autocontenidos" + housekeeping del repo. Cinco PRs creados, una mergeada en caliente.

### Lo que cambió

- **Opt-out del update checker en runtime** ([#339](https://github.com/Alexzafra13/HubPlay_demo/pull/339)). `Service.SetUserEnabled(bool)` togglable; persistido en `app_settings` con key `updates.check_enabled`. Bootstrap en `main.go` lee el setting antes de arrancar el ticker. Nuevo campo `user_disabled` en el wire de `/admin/system/updates`. Endpoints `GET`/`PUT /admin/system/updates/config`. Banner ahora pinta 4 estados (capability off / user-disabled con "Activar" / has_update / al día con "Comprobar" + "Deshabilitar"). 4 unit + 6 handler + 6 vitest.
- **URL mDNS en `/admin/system`** (#340, **mergeada**). Nuevo campo `mdns_url` (omitempty) en `serverStats`. El router lo computa cuando `cfg.MDNS.Enabled`. Fila condicional en la card "Conexión" del System status con valor mono + `CopyToClipboardButton`. Componente nuevo en `components/common/` (reutilizable, tolerante a HTTP plano sin clipboard API).
- **LICENSE GPL-3.0-or-later** ([#341](https://github.com/Alexzafra13/HubPlay_demo/pull/341)). Texto canónico de gnu.org en root. `LicenseFile=` añadido a `installer.iss` bajo `#if FileExists`. Campo `"license"` en `web/package.json`. Cumple sección 4 de la GPL-3. FFmpeg LGPL mantiene su `LICENSE-ffmpeg.txt` aparte.
- **Fixes cross-platform de `go test ./...`** ([#342](https://github.com/Alexzafra13/HubPlay_demo/pull/342)). Cinco fallos pre-existentes en main que NO eran bugs de producción — eran asumptions de test:
  - `upload/sanitize.go`: `filepath.Base("a:::b.mkv")` en Windows trata `a:` como drive prefix → cambiado a `path.Base` (POSIX).
  - `stream/manager_test.go`: `newTestManager` pasaba `ffmpegPath=""` que cae a PATH lookup; en hosts con ffmpeg `cmd.Start()` arrancaba el proceso y los tests del coalesce veían `nil` error → ffmpeg sentinel inexistente.
  - `config/preflight_test.go`: `stubPathWith` no añadía `.exe` para Windows (`exec.LookPath` requiere extensión en `%PATHEXT%`).
  - `db/sqlc/*models.go`: drift por migraciones `audit_log` + `cors_origins` que añadieron tablas sin regenerar — `make sqlc` regen.
- **`Service.BaseURL` inyectable en updates** ([#343](https://github.com/Alexzafra13/HubPlay_demo/pull/343)). Setter `SetBaseURL(url)` thread-safe. Reemplaza los 2 `t.Skip` con tests E2E reales contra `httptest`: `DetectsNewerVersion`, `SamePinsHasUpdateFalse`, `PrereleaseSkippedSilently`, `RecordsLastErrorOn500`, `HTTPIntegration_ETagRoundTrip` (verifica round-trip de `If-None-Match`, cabeceras `Accept` + `User-Agent`).
- **Limpieza de memoria** (esta sesión). 8 ficheros .md sueltos de sesiones cerradas movidos a `archive/`. `project-status.md` reescrito (de 441 KB a manejable) con las sesiones viejas en `archive/2026-04-29-to-05-04.md` y `archive/2026-05-05-to-05-19.md`.

### Métricas al cierre

- Backend: `go test ./...` verde en mi máquina (Windows + ffmpeg en PATH + sqlc 1.31.1).
- Frontend: 622/622 vitest (+18 sobre la sesión anterior: 4 service + 6 handler updates + 6 banner + 3 CopyToClipboardButton, menos 1 skip placeholder).
- TypeScript: `tsc -b` limpio.
- Producción: build limpio.

---

## 📦 Sesión 2026-05-19/20 (continuación) — Distribución y empaquetado

Cerramos el flujo "descargar y usar" para tres públicos: PC desktop, servidor Linux y NAS. 8 PRs mergeadas. Nada toca la lógica de negocio — todo es plumbing alrededor del binario.

**Audit panel — usernames en vez de UUIDs.** LEFT JOIN a users en el SELECT del audit log. Pinta `alice` y `bob → carol` en lugar de `bd91…/4def…`; UUID truncado en gris debajo, tooltip con el ID completo para copiar.

**Versión inyectable.** `version`, `commit`, `buildDate` como `var` en main package, inyectadas por `-ldflags` en Makefile y CI vía `git describe`. Se exponen en `/admin/system/stats` y en `hubplay --version`. Builds locales muestran `v0.1.0-N-gSHA-dirty`, build de tag muestra el tag limpio.

**PATH prepend del exe-dir al arranque.** Una línea en `main.go` que mete `filepath.Dir(os.Executable())` al inicio del `$PATH`. Con eso, cualquier `exec.LookPath("ffmpeg")` o `exec.Command("ffmpeg", …)` (probe, stream, imaging, iptv, library) encuentra el `ffmpeg` bundleado en la misma carpeta que `hubplay.exe` sin tocar ningún call-site.

**Release workflow cross-platform** (`.github/workflows/release.yml`):
- Matrix 5 targets: linux/darwin × amd64/arm64, windows × amd64. `CGO_ENABLED=0` (modernc.org/sqlite es pure-Go).
- `scripts/fetch-ffmpeg.sh` descarga ffmpeg LGPL por plataforma — BtbN/FFmpeg-Builds (Linux/Windows), evermeet.cx (macOS).
- Empaqueta `.tar.gz`/`.zip` + `.sha256` con `hubplay` + `ffmpeg` + `ffprobe` + `hubplay.example.yaml` + `LICENSE-ffmpeg.txt`.
- Job `release-nightly`: en cada push a `main` borra el tag `nightly` y lo recrea con los binarios del último commit. Pre-release público, visible en la pestaña Releases sin esperar a taguear.
- Job `release-tag`: en push de `v*` publica release ESTABLE (no draft, no prerelease).
- `fetch-depth: 0` en todos los checkouts que llaman a `git describe` — sin esto el job downstream calculaba una versión distinta y el unzip fallaba.

**Installer Windows** (`scripts/installer.iss` + `Minionguyjpro/Inno-Setup-Action@v1.2.8`):
- Genera `HubPlay-Setup-vXXX-windows-amd64.exe`.
- Bundlea NSSM (BSD) para registrar `HubPlay` como servicio Windows; arranca con el PC, sin consola.
- Icono multi-resolución (16/32/48/64/128/256), banner del wizard con marca integrada — todo generado desde `web/public/hubplay_icon_mark.svg` con `web/scripts/gen-installer-assets.mjs` (sharp + to-ico).
- Shortcut Escritorio + Menú Inicio apunta a `launch-hubplay.vbs` que intenta `msedge --app`, luego `chrome --app`, fallback al navegador por defecto → abre como ventana standalone sin chrome del browser.
- Textos del wizard en español sobrescritos en `[Messages]`, sin tono de marketing.
- Caveats: pin `@v1.2.8`, `fetch-depth: 0`, NSSM con retry+fallback a archive.org (nssm.cc da 503), env vars en lugar de `/D`. El `LICENSE` ya no es opcional (PR #341).

**Install script Linux** (`scripts/install.sh` + `scripts/hubplay.service`):
- One-liner estilo Tailscale/k3s: `curl -fsSL https://github.com/Alexzafra13/HubPlay_demo/releases/latest/download/install.sh | sudo bash`.
- Detecta arch (amd64/arm64), exige systemd, refusa con instrucciones a Docker en Synology/Unraid/QNAP/TrueNAS/Alpine.
- Resuelve "latest" vía GitHub API, descarga `.tar.gz`, verifica `sha256`, crea usuario sistema `hubplay`, instala binarios en `/usr/local/bin/`, config en `/etc/hubplay/`, datos en `/var/lib/hubplay/`, registra systemd unit endurecido, `enable --now`.
- Idempotente: correrlo dos veces hace upgrade in-place. Preserva el `hubplay.yaml` editado por el operador.

**PWA** (`vite-plugin-pwa` + `@vite-pwa/assets-generator`):
- Manifest + service worker injectados automáticamente. Workbox precachea JS/CSS/assets, no HTML (evita stale tras swap de server).
- Iconos generados desde `public/hubplay_icon_mark.svg` con `pnpm gen:pwa-assets`.
- Browser detecta "instalable" en `localhost` o HTTPS → botón "Instalar" → icono propio en escritorio/home + ventana standalone.
- **Caveat conocido**: en LAN sobre HTTP plano (192.168.x.y) los browsers NO ofrecen instalar la PWA — requieren secure context.

**Update notifier** (`internal/updates/`):
- Goroutine background con ticker de 24h y jitter inicial 0-30min.
- GET a `api.github.com/repos/.../releases/latest` con `If-None-Match` (ETag) → 304 cuando no hay versión nueva, ~200B por check.
- Auto-deshabilitado en `version=="dev"` o `repo==""`.
- Endpoints: `GET /admin/system/updates`, `POST /admin/system/updates/check` (rate-limit 1/min), `GET`/`PUT /admin/system/updates/config` (opt-out runtime — sesión 2026-05-20).
- Banner en `/admin/system` (`UpdateBanner.tsx`): 4 estados (ver sesión 2026-05-20 arriba).

**mDNS auto-anuncio** (`internal/mdns/` con `grandcat/zeroconf`):
- El server registra `_http._tcp` con hostname forzado `<cfg.MDNS.Hostname>.local` (default `hubplay.local`).
- Cualquier dispositivo de la LAN resuelve `http://hubplay.local:8096` sin tocar router ni DNS.
- Config `mdns.enabled` (default true) y `mdns.hostname` en `hubplay.yaml`.
- La URL se expone en `/admin/system/stats` (sesión 2026-05-20, PR #340) con botón copiar.

**PRs mergeadas en esta sesión:** #327 → #328 → #329 → #331 → #332 → #333 → #334 → #335-#337.

---

## 🎯 Cola priorizada para la próxima sesión

### Audit 2026-05-14 — lo que queda

Ver [audit-2026-05-14-go-backend-review.md](audit-2026-05-14-go-backend-review.md) + [intervention-2026-05-14.md](intervention-2026-05-14.md).

**Iteración 4 — cerrada al 100 %** (sesión 2026-05-21 tarde):
- ~~QQ~~ ✅ auth.Service split (PR #384)
- ~~P~~ ✅ ItemHandler split (PRs #386 + #388)
- ~~Z~~ ✅ library.Service split (PR #389)

**Iteración 5 — cerrada al 100 %**:
- ~~CC fase 1~~ ✅ Favorites + WatchHistory + Health (PR #390, sesión tarde-noche)
- ~~CC fase 2~~ ✅ ChannelOrderOps (PR #392, esta sesión)

**Iteración 6 — composition root** (4 de 5 cerrados esta sesión 2026-05-21 noche):
- ~~V~~ ✅ primitivos de Config a `Dependencies` (PR #395, `61396a3`)
- ~~JJ~~ ✅ `stream.NewManager(Deps{...})` wiring atómico (PR #395, `61396a3`)
- ~~LL~~ ✅ docs `Manager`/`Transcoder` (cerrado por diseño, PR #395, `61396a3`)
- ~~G~~ ⚠️ parcial — `runtime` god-struct sustituido por `lifecycle` con 3 fases (commit `8b746fc` en `claude/review-project-9YJxG`, pendiente PR). Feature modules (`library.Module`, `iptv.Module`) **diferidos** — cierra el síntoma del audit pero no al 100 %.
- **H** ⏳ pendiente — split `router.go` (1460 LoC, `r.Route("/api/v1", ...)` monolítico de ~1100 LoC) en `mountXxx` helpers per-feature + opcionalmente interfaces en `Dependencies` para los 22 `*db.X` concretos. **Sesión propia** — router.go es el fichero de mayor blast-radius del repo. Branch `claude/review-project-9YJxG` pendiente de PR.

**Iteración 6 fase 2 (post-H)** — feature modules:
- `library.New(ctx, deps) (*Module, error)` que devuelva Service + scnr + scanScheduler + imageRefresher + imageRefreshScheduler + segmentDetector + segmentFingerprinter + fsWatcher + `Shutdown(ctx)`. Cerraría G al 100%.
- `iptv.New(ctx, deps) (*Module, error)` análogo: service + proxy + transmux + scheduler + prober + logoCache + Shutdown. Toca el seam `scanner` (compartido con library para IPTV-as-channel-source).
- Auth, federation, retention también admiten feature modules pero ROI menor.

**Iteración 7 — cosmética + schema** (parcialmente cerrada):
- ~~W~~ ✅ `scanner.go` split en 6 ficheros (PR #393, esta sesión).
- **X** — frontera `library/` vs `scanner/` artificial (promover scanner a sub-paquete).
- **D** — cosmética (no leído el detalle).
- **BB** — comentarios en inglés masivos en `internal/library/` y otros → traducir al español (convención del repo). Mecánico pero grande.

**Iteración 8 — polish**:
- **F14-2-a** — único olor alto restante de los 6 originales del audit. Function-level quality.
- F14-X, F15-X, F16-X (varios) — calidad de código residual.

**Iteración 9 — verificación empírica**:
- `go test -race` + `goleak` + `govulncheck` post-merge.

### Frontend

**Segunda ola VideoPlayer** (opcional):
- React Doctor residuales tras PR #381: `no-giant-component` (663 LoC residuales en VideoPlayer), `prefer-useReducer` (5 useState residuales), `no-derived-state-effect:496`. Requiere más extracciones de JSX + `useReducer` para UI flags + `key={itemId}` para reset.

**file-type vuln** (medium):
- Transitive de `node-vibrant@4.0.4` (que ya está en latest). Bloqueada hasta que node-vibrant actualice su jimp interno.

### Dependabot abierto

- **#376** — web-deps group (17 minor/patch updates). CI mayoritariamente verde al cierre de esta sesión; Test Backend/Postgres/Build pendientes. Recreado tras colisión de lockfile con #289 (vía `@dependabot recreate`). 1-click cuando termine CI.

### Grandes (requieren ventana dedicada)

- **Firma del installer Windows con SignPath Foundation**. **Es gratis para OSS** (verificado en signpath.org el 2026-05-20). Apply via `apply.signpath.io` — la verificación tarda días/semanas pero la integración con el workflow es directa (action `signpath/github-action-submit-signing-request`). Mientras llega el approval, SmartScreen sigue avisando — no urgente.
- **Auto-update one-click + cert TLS en LAN**. Estilo `*.plex.direct`: el server obtiene un cert real para `<hash>.hubplay.direct` o similar, lo sirve en LAN sin warnings, y el client comprueba el feed de updates y aplica binarios firmados in-place. Feature grande, sin presión de calendario.

---

## 📚 Documentos vivos en `docs/memory/`

- **[architecture-decisions.md](architecture-decisions.md)** — ADRs cerrados (AppError, observability, keystore, sink pattern, preflight, sqlc adapter, etc.). Sólo se añaden ADRs nuevos; nunca se edita un ADR cerrado.
- **[conventions.md](conventions.md)** — patrones del codebase (anti-ciclos, sqlc adapter, helpers de test, gotchas, reglas de dependencia entre paquetes).
- **[audit-2026-05-14-go-backend-review.md](audit-2026-05-14-go-backend-review.md)** — review vivo por fases. Iteraciones 4-7 pendientes (ver intervention).
- **[intervention-2026-05-14.md](intervention-2026-05-14.md)** — tracker de iteración del review 2026-05-14. Marca olores cerrados por commit.
- **[perf-benchmarks-2026-05-17.md](perf-benchmarks-2026-05-17.md)** — baseline benchmarks dual-backend (SQLite + Postgres) para repos del hot-path.

## 🗄️ Archivo (`docs/memory/archive/`)

Sesiones cerradas, conservadas para arqueología:

- `2026-pre-04-28.md` — orígenes del proyecto.
- `2026-04-27-to-04-29-pre-detail-ux.md` — sesiones pre-detail UX.
- `2026-04-29-to-05-04.md` — federación P2P, OpenAPI, hardening federation.
- `2026-05-05-to-05-19.md` — senior reviews intermedios, fixes player/seek, mDNS bringup, uploads + permisos granulares.

Audits cerrados + sesiones específicas:
- `audit-2026-04-15.md`, `audit-2026-04-28.md`, `audit-2026-05-05.md`, `audit-plan.md`
- `manual-qa-movies-series-2026-04-27.md`
- `player-seek-bugs-2026-05-07.md` (los bugs ya están arreglados en main)
- `session_2026-05-10_audit_p0_fixes.md`
- `topbar-redesign-2026-05-06.md`
