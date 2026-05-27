# Estado del proyecto

> **Entrypoint de cada sesión.** Lo viejo (todo lo previo a la sesión
> 2026-05-19/20) vive en `archive/`. No se pierde nada — sólo se mueve
> de sitio para que este fichero sea legible de un vistazo.

---

## 🪵 Sesión 2026-05-27 — F15-7 + F15-2 + auditoría completa de logs (7 PRs)

Sesión larga centrada en cerrar el F15-7 (t.Parallel restante) y el
F15-2 (clock injection en lo que quedaba), terminando con una
**auditoría sistemática de los 568 logs del backend** que produjo el
ADR-026 (convención de logs) + cleanup de naming.

### PRs

| PR | Tema | Estado |
|---|---|---|
| [#444](https://github.com/Alexzafra13/HubPlay_demo/pull/444) | F15-7: t.Parallel en 5 ficheros restantes + mutex en `testutil.Migrate` | ✅ merged |
| [#445](https://github.com/Alexzafra13/HubPlay_demo/pull/445) | F15-2 scanner: refactor `New(17 params)` → `New(Config)` + clock | ✅ merged |
| [#446](https://github.com/Alexzafra13/HubPlay_demo/pull/446) | F15-2 rest: clock injection en notification + upload | ✅ merged |
| [#447](https://github.com/Alexzafra13/HubPlay_demo/pull/447) | Fix races + flakes heredados (federation/url, event/bus, transmux) | ✅ merged |
| [#448](https://github.com/Alexzafra13/HubPlay_demo/pull/448) | docs(memory): F16 cerrado al 100% (verificación) | ✅ merged |
| [#449](https://github.com/Alexzafra13/HubPlay_demo/pull/449) | Auditoría completa de logs + ADR-026 convención | ✅ merged |
| [#450](https://github.com/Alexzafra13/HubPlay_demo/pull/450) | Sub-loggers en me_home + naming canónico `err`→`error` en handlers | 🟡 abierta (CI verde, lista para merge) |

### Hallazgos importantes descubiertos en el camino

- **Race en `testutil.NewTestDB`** (PR #444): `goose.SetBaseFS` muta globals
  sin proteger. Con tests `t.Parallel` se solapaban → DATA RACE. Fix:
  `var migrateMu sync.Mutex` en testutil.
- **Race en `federation/url_test`** (PR #447): la sesión anterior (#443) añadió
  `t.Parallel()` a `TestValidatePeerURL_TestSeamRespected` sin reconocer que
  muta `blockedPeerIP` global. Quitar `t.Parallel()` cierra.
- **`unblockLoopback(t)` cargo-cult** (PR #444): 9 tests de `preflight_test` lo
  llamaban "defensivamente"; `PreflightCheck` no consulta `blockedIP` →
  llamadas redundantes. Eliminadas para habilitar paralelismo.
- **`finishInputFromHook`** (PR #446): único `time.Now()` en producción de
  upload que queda — fallback en función libre sin acceso al Service.
  Documentado como cero impacto (caller siempre rellena `started_at`).

### ADR nuevo

- **ADR-026** — Convención de logs (nivel, sub-logger, hot paths, secretos).
  4 reglas en orden de importancia + naming canónico definido.
  Regla operacional: *"si un log dispara > 1/min en uso normal, está mal
  de nivel"*.

### Métricas globales acumuladas hoy

- **Clock-injected en codebase**: 16 → 23 ficheros de producción.
- **t.Parallel()**: 314 → 375 (**+61**, sin contar handlers ya paralelizados).
- **`time.Now()` en producción** de los paquetes audited: −20 sitios.
- **Logs con naming inconsistente** en handlers: 31 sitios `"err"` → 0.
- **Sub-loggers `.With()`** nuevos: 13 funciones (library, scanner, iptv, me_home).
- **Niveles corregidos**: 17 hot path noise (Info/Warn → Debug), 3 importancia
  (transmux Info → Warn, setup Warn → Debug, providers 404+Error → 502+Warn).
- **Errores tragados ahora logueados**: 8 sitios (scanner cleanup, imagerefresh,
  identify, prober panic con stack).
- **Sin leaks PII/secretos** detectados en todo `handlers/` (verificado a fondo).

### F15 y F16 status

| Item | Estado |
|---|---|
| F15-1 (time.Sleep en tests) | ✅ 100% (sesión 2026-05-26) |
| F15-2 (clock injection) | ✅ Núcleo cerrado. Quedan db repos (sesión propia). |
| F15-7 (t.Parallel) | ✅ Lo paralelizable. Lo que queda requiere refactor del seam global. |
| F15-8 (t.TempDir) | ✅ adoptado |
| F15-3..6, F15-9..12 | sin desglosar |
| F16 | ✅ 100% (verificado #448) |

### Follow-ups concretos de la auditoría de logs (sesión propia, ~1-2h)

Documentados en el ADR-026, pendientes de ejecutar:

1. **`iptv/circuit_breaker.go`** — Inyectar `*slog.Logger` y loguear
   transiciones closed→open / half-open→open. Cambio de API del constructor
   (`newChannelBreaker`). Hoy el operador no ve cuándo un canal entra en
   cooldown por fallos repetidos — sólo lo expone `BreakerState()` polleado
   por el panel admin.
2. **`iptv/transmux.go::startLocked`** — Log de spawn-error antes de los 3
   `return err` (L550, L596, L617). Hoy `IncStarts("spawn_error")` cuenta
   métrica pero no hay log de la causa.
3. **Verificar `handleServiceError` cadena de logs** — Handlers usan
   `handleServiceError(w, r, err)` sin loguear antes, asumiendo que el
   servicio loguea. Hay que confirmar paquete por paquete (`auth`, `library`,
   etc) que efectivamente loguean los 5xx. Si no, los 500 quedan ciegos para
   el operador.
4. **Sub-loggers handlers grandes (revisión)** — La auditoría inicial sugirió
   `federation_admin × 5`, `me_peers × 4`, `collections × 3`. Al revisar caso
   por caso, sólo `me_home::Trending` y `Recommended` cumplieron el umbral de
   ≥ 3 logs con campo repetido. El resto son 1-2 logs por handler. **Esto
   ya está cerrado a la práctica** — solo dejar nota explicativa si se reabre.

### Pendientes priorizadas (próximas sesiones)

- **Follow-ups logs** (~1-2h) — los 4 items de arriba.
- **F15-2 db repos** — `time.Now()` en queries INSERT/UPDATE de los repos
  (voluminoso pero homogéneo, ~2-3h).
- **VideoPlayer 3ª ola** — 787 LoC, frontend.
- **LL Transcoder stateless** — sesión grande propia.
- **Distribución** — installer Windows firmado (SignPath), auto-update, TLS LAN.

### Aprendizajes registrados

- **Lanzar 4 sub-agentes en paralelo para auditorías**: cada uno auditó un
  paquete (handlers / iptv / scanner / library) con prompt estructurado y
  formato de reporte fijo. Tardó ~2 min en total vs días de revisión manual.
  Modelo replicable para futuros audits cross-package.
- **Win desactivar Smart App Control para tests `-race` locales**: ya hecho
  esta sesión. SAC bloquea binarios temporales de `go test`. La desactivación
  es **irreversible** sin reinstalar Windows — anotado en
  `~/.claude/projects/.../memory/race-tests-windows.md`.

---

## 🧪 Sesión 2026-05-26 (parte 2) — F15-2 clock injection + F15-7 t.Parallel() ×4.8

Continuación de la sesión 2026-05-26. Tres commits en `claude/project-review-status-PHIHv`.

### F15-2 clock injection (3 componentes nuevos)

| Componente | `time.Now()` reemplazados | Notas |
|---|---:|---|
| `auth/ratelimit.go` | 3 | Lockout/window/cleanup ahora determinísticos. 4 tests nuevos con `clock.Mock`. |
| `retention/runner.go` | 1 | Cutoff del audit prune verificable con mock. 1 test nuevo. |
| `library/service.go` | 3 | Create/CreatePersonalIPTV/Update usan `s.clock.Now()`. |

Total clock-injected en codebase: 16 → 19 ficheros de producción.

### F15-7 t.Parallel() expansion

| Fichero(s) | Tests añadidos |
|---|---:|
| stream/decision_test | 18 |
| upload/validator_test | 17 |
| iptv/circuit_breaker_test (excl. ConcurrentAccess) | 9 |
| handlers/collections_test | 2 |
| handlers: progress, setup, library, items, users_library_access, upload_browse, auth | 152 |
| domain/errors, federation/url, iptv/m3u_language, stream/capabilities | 47 |
| auth/ratelimit_test (tests nuevos) | 4 (+1 retention) |

**Total t.Parallel()**: 65 → 314 (**×4.8**). Excluidos por races en fakes:
handlers/image_test, imaging/safety_test, iptv/{preflight,proxy_security,xmltv}_test.

### F16 bajas — cerradas al 100 %

La sesión anterior (#440) documentó F16-11/13/18/19 como decisiones
deliberadas. El commit message de #440 ya cerraba F16-10 (VacuumInto
helper), F16-14 (código cambiado, audit outdated), F16-17 (cosmético).
**F16 completamente cerrado**: alta 1/1, medium 8/8, bajas 10/10.

### ~~Pendientes priorizadas~~ (obsoleto — ver sesión 2026-05-27 arriba)

> **Superseded por sesión 2026-05-27**: F15-2 scanner + notification/sweeper
> + upload cerrados (#445, #446). F15-7 restante cerrado (#444). F14-7-a
> sub-loggers aplicados donde valían (#449, #450). Backlog vivo está en
> la sesión 2026-05-27.

---

## 🎯 Sesión 2026-05-26 (cierre completo) — F15-1 100% + F16 medium 100% + 3/10 F16 bajas

Sesión larga (7+1 PRs). Cierra el F15-1 ALTA por completo y los 3 issues
medium restantes del F16. Resumen:

| Bloque | PRs | Notas |
|---|---|---|
| **F15-1 cierre** | #429, #430, #431, #432 (docs), #433, #434, #435 | 29 sleeps eliminados + 11 documentados legítimos + memoria. F15-1 al 100 %. |
| **F16-3 requirePeer** | #436 | 11 sitios federation migrados al helper. |
| **F16-5 / F16-9 verificación** | (sin código) | Ambos ya cerrados en sesiones previas — F16-5 con doc en iptv_admin:61-64, F16-9 con SpawnBackground/bgWG. |
| **F16 bajas** | #437 | F16-12 (UTF-8 + null bytes en preferences), F16-15 (log err en GetPrimaryURLs ×2), F16-16 (log err en json.Unmarshal de providers). |

**Total acumulado**: 7 PRs nuevas en esta sesión + #436 + #437 = 9 PRs. ~80 LoC neto de cambios productivos (la mayoría son tests + helpers + comentarios).

### F16 status post-sesión

| Severidad | Cerrados | Pendientes |
|---|---|---|
| Alta | 1/1 (F16-1 path traversal) | 0 |
| Medium | 8/8 | 0 ✅ |
| Bajas | 10/10 ✅ | 0 |

**F16 cerrado al 100%** — verificado sesión 2026-05-27. Las "7 bajas pendientes" listadas en sesiones previas ya estaban cubiertas: #440 cerró F16-10 (VacuumInto helper), F16-14 (código cambió, audit outdated) y F16-17 (cosmético); el código tiene comentarios `// F16-11/13/18/19 (audit): …` que documentan las decisiones deliberadas.

### ~~Pendientes priorizadas~~ (obsoleto — ver sesión 2026-05-27 arriba)

> **Superseded por sesión 2026-05-27**: F14-7-a sub-loggers parcialmente
> cerrados (#449 library/scanner/iptv + #450 me_home). Backlog vivo arriba.

---

## 🧪 Sesión 2026-05-26 — F15-1 (parte 2): time.Sleep → seams determinísticos

Continuación del F15-1 ALTA arrancado en PR #426. Tres PRs encadenadas
eliminan **20 `time.Sleep`** de tests aplicando el patrón notify-channel
+ Wait helpers establecido en PR #426 (watcher/retention/event-bus).

### PRs

| PR | Tema | Sleeps |
|---|---|---:|
| [#429](https://github.com/Alexzafra13/HubPlay_demo/pull/429) | SSE events_test + me_events_test | 8 |
| [#430](https://github.com/Alexzafra13/HubPlay_demo/pull/430) | iptv scheduler + prober_worker | 8 |
| [#431](https://github.com/Alexzafra13/HubPlay_demo/pull/431) | iptv_test + auth/service + federation/client_retry | 4 |

### Seams nuevos añadidos

| Componente | Seam | Para |
|---|---|---|
| `event.Bus` | `changes chan struct{}` (32) + `WaitForHandlerCount(et, want, timeout)` | Tests de SSE subscribe/unsub sin polling. |
| `handlers.SSELimiter` | `changes chan struct{}` (32) + `WaitForGlobal(want, timeout)` | Espera release del slot tras disconnect. |
| `iptv.Scheduler` | `startedCh chan struct{}` + `Started() <-chan struct{}` cerrado al entrar el loop en el select | Evita el sleep "allow goroutine to enter select". |

Los demás seams viven en los fakes locales del test (notify channels en
`fakeRunner`, `fakeLibLister`, `fakeChanLister`, `panicLibLister`,
`blockingChanLister`, `iptvFakeService`, `recordingSubscriber`).

### Patrón canónico

```go
// En el componente / fake
type X struct {
    ...
    changes chan struct{} // buffer 32, non-blocking signal
}

func (x *X) doThing() {
    ...
    select { case x.changes <- struct{}{}: default: } // non-blocking
}

// Helper (production o test-only)
func (x *X) WaitForFoo(want int, timeout time.Duration) bool {
    deadline := time.After(timeout)
    for x.fooCount() != want {
        select {
        case <-x.changes:
        case <-deadline:
            return x.fooCount() == want
        }
    }
    return true
}

// Test
if !x.WaitForFoo(0, time.Second) {
    t.Fatal("foo did not reach 0")
}
```

### Sleeps deferidos (batch 2b + batch 4)

| Fichero | # | Razón |
|---|---:|---|
| `iptv/transmux_test.go` | 10 | Requiere notify en `TransmuxManager` producción; algunos sleeps son legítimos (TTL/reaper tests). PR aparte. |
| `iptv/prober_test.go::ConcurrencyCap` | 1 | Simulación de upstream lento (leader holds slot for piggybackers). |
| `api/handlers/auth_device_test.go` | 2 | Requiere clock injection en device-auth service + bus introspection para "subscription ready". |
| `api/handlers/stream_test.go::WaitForFile` | 1 | Sleep en goroutine simula "file appears late" — es el comportamiento que testa. |
| `federation/stream_test.go::CloseStopsSweeper` | 1 | Scheduler de Go drenando goroutines. Goleak cubre via CI. |
| `stream/manager_test.go::StartGroup` | 1 | Singleflight leader holds slot — sin seam externo. |
| Time-based legítimos | ~5 | `db/channel_watch_history` (timestamps), `streaming_deadline` (WriteTimeout), `updates/checker` (Windows 15ms clock), `provider/httpcache` (singleflight similar). |

### Aprendizajes

- **No todos los time.Sleep son malos**. Tests que verifican comportamiento
  time-based real (TTL, reaper, deadline propagation, simulación de
  upstream lento) usan sleep legítimamente. La fix no es eliminar; es
  reemplazar SOLO los polls "while !state { sleep }" por waits en señal.
- **El bus de eventos + el SSE limiter ahora tienen `WaitForXxx`**
  promovidos a API pública con comentario "Test-only". Convención
  consistente con `bus.HandlerCount` que ya existía con ese disclaimer.
- **El patrón notify-channel buffer 32 + send non-blocking** se
  consolida como el camino estándar para señalizar transiciones
  entre componente y test. Producción nunca lee → cero overhead;
  test selecciona sobre el chan con timeout para deadline garantizado.
- **Algunos polls eran simplemente redundantes**: en `events_test.go`
  línea 51 + `me_events_test.go` líneas 70/112/164 había sleeps "para
  asegurar que el handler suscribió" cuando líneas arriba ya esperaban
  via `waitForHandlerCount`. 4 sleeps eliminados sin tocar producción.

### Métricas

- **20 sleeps eliminados** de 14 ficheros (de los 19 originales del F15-1)
- **3 seams en producción** (event.Bus, SSELimiter, iptv.Scheduler) — APIs
  públicas con documentación "Test-only".
- **5 fakes** con notify channels añadidas.
- **0 cambios de behaviour**: tests pasan sin tocar la lógica testada.

---

## 📋 Sesión 2026-05-25 (parte V, extensión) — PRs #426-#428 (no documentadas previamente)

Trabajo cerrado el mismo 2026-05-25 después de #425 que no estaba en la
memoria. Tres PRs incrementales en `claude/project-review-IYCoR`:

### PR #426 — F14-7-a sub-loggers + F15-1 parcial

- **F14-7-a sub-loggers `.With()` para context fields repetidos**: 11
  ficheros (library/{watcher, segment_detector, segment_fingerprinter,
  service}, scanner/{scanner, scan_walk, enrich, enrich_season_episode,
  media_ingest}, iptv/{service_m3u, service_epg}). Cada método que opera
  sobre una entidad crea `log := w.logger.With("library_id", id)` al
  entry y reemplaza las repeticiones manuales.
- **F15-1 parcial**: añade seams determinísticos (`WaitForWalksDone`,
  `WaitForReconcileDone`, `WaitForSweep`, canales de notificación) para
  eliminar `time.Sleep` en watcher_test (6), retention/runner_test (2),
  event/bus_test (4). Establece el patrón notify-channel + `WaitForXxx`
  que la sesión F15-1 parte 2 (PRs #429-#431) replica.

### PR #427 — F16-2/4/8 + F15-1 service_lock

- **F16-2/4 parsePagination helper**: nuevo
  `parsePagination`/`parsePaginationFromValues` que valida `offset >= 0`,
  `limit >= 0` y cap a 500. Aplicado a 9 sites en 6 handlers (library,
  item_search, users, audit_log, me_peers, federation_public).
- **F16-8 SSE drop observability counter**: atomic counter de SSE drops
  por conexión; loguea total al desconectar si > 0 (events.go,
  me_events.go). Permite detectar clientes lentos sin bloquear el bus.
- **F15-1 service_lock_test**: el `time.Sleep(50ms)` para esperar a que
  la primera goroutine adquiriera el lock se reemplaza por un canal de
  señal desde el httptest server. Pasa en ~2s sin race.

### PR #428 — VideoPlayer 2ª ola

- **VideoPlayer 817 → 787 LoC** (−30) consolidando 4 useState de overlays
  (upNextActive, externalSubsModalOpen, activeExternalSub, showHelp) en
  un `useReducer` dentro de nuevo hook `usePlayerOverlays`.
- VideoPlayer baja de **5 useState a 1** (sólo playbackRate).
- Cierra los issues de React Doctor `prefer-useReducer` y
  `no-cascading-set-state` para VideoPlayer.

### F16 — estado tras #427

De los 8 issues medium originales, **3 cerrados** (F16-2 + F16-4 +
F16-8). Quedan 5: race async iptv_admin, auth check redundante,
paginación restante en algunos endpoints aún no migrados,
deduplicación SSE en clientes con misma userID, telemetría de
conexiones SSE.

---

## 🔒 Sesión 2026-05-25 (parte IV) — Security: migrar middleware.RealIP a ClientIPFromXFF

chi v5.3.0 deprecó `middleware.RealIP` por 3 CVE de IP spoofing (incl.
GHSA-3fxj-6jh8-hvhx Critical 9.3). Bump del go-deps group (#423) lo
descubre + golangci-lint lo flagga como `SA1019`. Migración a la nueva
API (`ClientIPFromXFF` + `GetClientIP` del ctx) hecha en una sola PR
junto al bump.

### Cambios

- `internal/api/router.go::applyGlobalMiddleware`: sustituye
  `middleware.RealIP` por `ClientIPFromXFF(deps.TrustedProxies...)` si
  hay CIDRs trusted declarados, sino `ClientIPFromRemoteAddr` (seguro
  por defecto: si el operador no declara proxy de confianza, no
  honramos XFF).
- `Dependencies.TrustedProxies []string` (campo nuevo) — el operador
  lo declara en `server.trusted_proxies` (campo YAML que ya existía
  pero nunca se cableaba).
- `internal/api/handlers/client_ip.go` (nuevo, ~28 LoC) — helper
  `ClientIP(r)` que lee `middleware.GetClientIP(r.Context())` con
  fallback a `r.RemoteAddr` para tests sin router completo.
- 10 sitios migrados de `r.RemoteAddr` a `ClientIP(r)`: 4 en `auth.go`
  (Login, RefreshToken, deviceLogin, setupLogin), 2 en `auth_device.go`
  (PollDevice, audit log), 2 en `events.go` (SSE connect/disconnect),
  2 en `me_events.go` (user SSE).
- `iprate_middleware.go`: el helper local `clientIP(r)` (lowercase) que
  parseaba XFF manualmente sin trust verification — **mismo bug que el
  deprecation arregla** — se elimina. El middleware ahora consume
  `ClientIP(r)` directamente.
- `hubplay.example.yaml`: doc del setting `server.trusted_proxies`
  explicando el trade-off seguro vs honrar XFF.
- `go.mod` / `go.sum`: bump chi 5.2.5→5.3.0 + crypto 0.51→0.52 +
  image 0.40→0.41 (cierra #423 + dependabot).

### Comportamiento

- **Operador con docker + nginx en localhost** (default config):
  trusted_proxies = `["127.0.0.1", "172.16.0.0/12"]` ⇒ XFF honrado
  saltando hops trusted. Comportamiento equivalente al pre-refactor
  con `RealIP`.
- **Operador directo a Internet** sin proxy: trusted_proxies vacío ⇒
  `ClientIPFromRemoteAddr` ⇒ usa la IP de la conexión TCP, **seguro
  contra spoofing**.
- La nueva API NO muta `r.RemoteAddr` — el IP va en el ctx. Los
  handlers leen con `ClientIP(r)` (helper) en vez de `r.RemoteAddr`.

### Cierra

- 3 CVE de spoofing (GHSA-3fxj-6jh8-hvhx Critical 9.3,
  GHSA-rjr7-jggh-pgcp, GHSA-9g5q-2w5x-hmxf).
- Dependabot #423 (go-deps group).
- Bug latente del helper `clientIP(r)` lowercase que confiaba en XFF
  sin trust verification (mismo vector que el de RealIP).

---

## 🔭 Estado actual (2026-05-25, extensión post-cierre — G + H al 100 %)

Sesión de extensión sobre la "todo mergeado" del cierre 2026-05-25.
**Tres PRs estructurales** consecutivas cierran los dos olores
arquitectónicos pendientes del audit:

1. **G fase iptv** (#417, ✅ merged) — `iptv.Module`.
2. **G fase library** (#418, ✅ merged) — `library.Module`. Olor G
   al 100 %.
3. **H deps-interfaces** (#419, ✅ merged) — 18 `*db.X` en
   Dependencies → interfaces broad. Olor H al 100 % (mountXxx ya
   estaba cerrado en sesión 2026-05-21).

Con `lifecycle.go` (#396, ya en main) + estas tres PRs, **iteración
6 del audit (composition root) cerrada al 100 %**.

**20 PRs en la sesión 2026-05-25 base** (#396-#415). Todas mergeadas. 0 PRs abiertas al inicio.

### Cerrado en esta sesión

- **6/6 olores altos** del audit 2026-05-14 — 0 pendientes
- **H** — router.go 1549→465 LoC (7 mount_*.go)
- **F14 completo**: F14-2-a/b (7 firmas→structs), F14-3/4/5 (3 splits de funciones largas: RunRefreshM3U, startSessionSlow, NewHomeRepository), F14-4-a (panic→error), F14-6-a/b (respondData 115 sites + requireParam 53 sites), F14-9 (where builder), F14-9-a (CacheControl constantes), F14-10-a (4-value returns→structs), F14-12-a (sqlPlaceholders), F14-5-a (naming convention)
- **Iter 9** — goleak enforcement (4 paquetes) + fix CI Postgres singleton
- **BB** — ~3800 líneas de comentarios traducidas + acortadas (stream/ library/ iptv/ federation/ handlers/)
- **Convención comentarios** documentada en conventions.md (español, cortos, "por qué")

### CI

Todos los jobs verdes: Test Backend, Test Backend (Postgres), Lint, Frontend, knip, govulncheck, goleak, React Doctor.

---

## 📋 Lo que queda para próxima(s) sesión(es)

### Polish baja (~2h)

- **F14-7-a** — sub-loggers `.With("library_id", id)` × 145 sites

### Tests (F15)

- ~~**F15-1 ALTA** — `time.Sleep` en 19 ficheros → seams determinísticos~~ — ✅ **cerrado al 100 %** en sesión 2026-05-26. 41 sleeps eliminados total (12 en #426 + 29 en #429-#434), 11 documentados como legítimos con tag uniforme (#435).
- **F15-2..12** — media/baja (time.Now no inyectado, t.Parallel infrautilizado, etc.)

### Handlers (F16) — medium ✅ cerrado al 100 %

PR #427 cerró F16-2/4 (parsePagination helper × 9 sites) + F16-8 (SSE drop observability). PR #436 cerró F16-3 (requirePeer helper × 11 sitios federation). F16-5 ya estaba documentado en `iptv_admin.go:61-64` y F16-9 ya usa `svc.SpawnBackground` (eco GGGG cerrado). PR #437 cerró 3/10 bajas (F16-12 UTF-8, F16-15 log err GetPrimaryURLs, F16-16 log err json.Unmarshal). Quedan 7 bajas non-critical.

### Arquitectónicos (sesión grande cada uno)

- ~~**G** — feature modules `library.New()` / `iptv.New()` con Shutdown integrado~~ — **cerrado al 100 %** (iptv.Module #417 + library.Module #418, post lifecycle.go #396).
- ~~**H** — 22 `*db.X` → interfaces en Dependencies~~ — **cerrado al 100 %** (mountXxx split sesión 2026-05-21 + deps-interfaces #419).
- **LL** — Transcoder stateless (cmd/cancel/done a ManagedSession)

### Frontend

- VideoPlayer segunda ola (663 LoC, useReducer)
- file-type vuln (bloqueada upstream)
- React Doctor gate a 80

### Distribución

- Firma installer Windows (SignPath)
- Auto-update + TLS LAN

---

## PRs de esta sesión (2026-05-25)

| PR | Tema | Estado |
|---|---|---|
| #396 | G parcial (lifecycle) | ✅ merged |
| #397 | H (router split 1549→465 LoC) | ✅ merged |
| #398 | F14-2-a (BuildFFmpegArgs→TranscodeRequest) | ✅ merged |
| #399 | F14-2-b (Transcoder.Start/RestartAt) | ✅ merged |
| #400 | F14-2-b (Manager.StartSession→StartSessionRequest) | ✅ merged |
| #401 | F14-2-b (NewTranscoder→TranscoderConfig) | ✅ merged |
| #402 | F14-2-b (RecordProgress→ProgressUpdate) | ✅ merged |
| #403 | Iter 9 goleak (4 paquetes) | ✅ merged |
| #404 | 5 quick wins F14 (panic, CacheControl, sqlPlaceholders, naming) | ✅ merged |
| #405 | Convención comentarios | ✅ merged |
| #406 | Fix CI Postgres (goleak singleton) | ✅ merged |
| #407 | respondData 115 sites | ✅ merged |
| #415 | Consolidación: requireParam + where builder + BB + splits + F14-10-a | ✅ merged |
| #414 | Memoria | ✅ merged |
- **Score React Doctor: ≥75/100 ("Great")** post-VideoPlayer-split (PR #381 mergeada). El offender principal de las reglas estructurales (`no-cascading-set-state`) eliminado; `no-giant-component` reducido de 1003 a 663 lines; `prefer-useReducer` de 12 useState a 5.
- **knip: 0 unused files / 0 unused deps / 0 unused exports / 0 unused types**. Hard gate en CI.
- **Audit 2026-05-14 — Iteración 6 al 80 % cerrada esta sesión** (V + JJ + LL + G parcial). Queda **H** (router split + interfaces en Dependencies) para sesión propia. De los **6 olores altos** del audit original (A+M, B+J, CC, P, W, F14-2-a), 5 están cerrados — sólo queda F14-2-a (function-level quality).
- **Dependabot alerts**: 8 → 1 (1 critical eliminada). PRs #385 (to-ico→png-to-ico, 5 vulns), #289 (picomatch alert #18) mergeadas. Queda 1 medium (file-type transitive de node-vibrant — bloqueada hasta upstream).
- HubPlay distribuible "descargar y usar" en los tres targets (desktop / Linux server / NAS-via-Docker) — flujo cerrado en la sesión 2026-05-19/20.

---

## 🧩 Sesión 2026-05-25 (extensión post-cierre, parte II) — G fase library: `library.Module`

Sigue inmediatamente a G fase iptv (mismo día). Nuevo
`internal/library/module.go` (~245 LoC) agrupa los 9 componentes
long-lived del feature library — el feature con mayor surface de
todos los del repo.

### Componentes agrupados

| Componente | Tipo | Lifecycle |
|---|---|---|
| `Scanner` | `*scanner.Scanner` | sin Start/Stop propio — driven por service |
| `Service` | `*library.Service` | `Shutdown()` drena bgWG (scans en vuelo) |
| `ScanScheduler` | `*Scheduler` | `Start(ctx)` + `Stop()` |
| `ImageRefresher` | `*ImageRefresher` | sin lifecycle — call-on-demand |
| `ImageRefreshScheduler` | `*ImageRefreshScheduler` | `Start(ctx)` + `Stop()` |
| `Fingerprinter` | `*Fingerprinter` | helper puro (fpcalc wrapper) |
| `SegmentDetector` | `*SegmentDetector` | suscriptor del bus; `Start()` devuelve unsub que drena bgWG |
| `SegmentFingerprinter` | `*SegmentFingerprinter` | idem; fail-soft sin fpcalc |
| `FSWatcher` | `*FSWatcher` | `Start(ctx)` puede fallar (sin inotify), fail-soft |

### Cross-wiring que el Module encapsula

- `scanner.New(...)` con 16 args → injected en `library.NewService(... scnr ...)`.
- Los dos detectores se atan al event.Bus en `library.scan.completed`
  dentro de su propio `Start`; el Module captura los dos handles de
  unsub para llamarlos en shutdown (sin ellos el bus filtra
  handlers + las goroutines de `DetectLibrary` no se drenan — audit
  olor Y).
- `fsWatcher.Start` puede fallar sin que el módulo aborte el boot
  (boolean `fsWatcherStarted` decide si registrar el hook de Stop
  o saltárselo).

### Orden de shutdown en RegisterWith

Workers (fase 1, add-order):

1. scan scheduler
2. image refresh scheduler
3. fs watcher *(sólo si arrancó OK)*

Services (fase 3, **LIFO** ⇒ último registrado = primero parado):

| Orden de registro | Orden de shutdown |
|---:|---|
| 1. segment detector | 3. último — drena bgWG de DetectLibrary |
| 2. segment fingerprinter | 2. drena bgWG de DetectLibrary audio |
| 3. library service | 1. **primero** — drena scans en vuelo |

La inversión es deliberada: library.Service.Shutdown drena scans
que pueden emitir `library.scan.completed` durante el drain. Si
desuscribimos los detectores primero, esos eventos finales se
pierden y no se generan markers de skip-intro para el último scan.
Manteniendo los detectores activos hasta después del library
Shutdown, el último scan SÍ produce markers — y luego los unsubs
drenan limpiamente sus propias goroutines.

### Aislamiento

`library/module.go` añade 6 imports nuevos al paquete library
(`context`, `slog`, `db`, `event`, `pathmap`, `probe`, `provider`,
`scanner`) — **todos** ya existentes en el paquete por otros
ficheros. **Cero imports nuevos cross-paquete**. El paquete sigue
libre de `config`/`observability`/`stream`.

### Impacto en main.go

- `cmd/hubplay/main.go`: bloque library 88 → 45 LoC (**−43 LoC**).
- `import "hubplay/internal/scanner"` desaparece — el Module lo
  encapsula.
- 8 variables locales (`scnr`, `libraryService`, `scanScheduler`,
  `imageRefresher`, `imageRefreshScheduler`, `segmentDetector`,
  `segmentFingerprinter`, `fingerprinter`, `fsWatcher`) → 1
  (`libMod`).
- 4 `defer X.Stop()` históricos desaparecen — RegisterWith los
  lleva al lifecycle ordenado.
- `api.Dependencies.Libraries` / `.Scanner` ahora vienen de
  `libMod.Service` / `libMod.Scanner` (preservando surface).

### Tests

`go test -count=1 ./internal/library/... ./internal/scanner/...
./internal/api/...` verde sin modificaciones — cero churn de
behaviour.

### G cerrada al 100 %

Las tres piezas del refactor propuesto en el audit 2026-05-14
están en su sitio:

1. **`cmd/hubplay/lifecycle.go`** (#396) — sustituye el `runtime`
   god-struct por phased `lc.AddWorker/AddService`.
2. **`internal/iptv/module.go`** (#417) — feature module IPTV.
3. **`internal/library/module.go`** (esta PR) — feature module
   library.

`main.run` pasa de los 645 LoC originales del audit a ~485 LoC
(−25 %), sin contar el lifecycle.go separado. Los **6 olores
estructurales altos** del audit están cerrados (A+M, B+J, CC, P,
W, F14-2-a) más los **5 olores composition-root** (V, JJ, LL, G,
H mountXxx). Sólo queda Dependencies-as-interfaces (H parte 2 —
abordada en la siguiente PR) y LL stateless como sesiones grandes
futuras.

---

## 🪞 Sesión 2026-05-25 (extensión, parte III) — H deps-interfaces: 18 `*db.X` → interfaces

Tercera PR de la extensión post-cierre. Cierra la segunda mitad del
olor H del audit 2026-05-14: tras el split del router en mount_*.go
helpers (sesión 2026-05-21, router.go 1549 → 465 LoC), faltaba que
`api.Dependencies` dejara de expresar el contrato dos veces — como
`*db.XRepository` arriba (composition root) y como interface estrecha
abajo (handler local).

### Cambio

Nuevo `internal/api/handlers/deps_repos.go` (~218 LoC) declara
**18 interfaces "broad"** — una por repo de `Dependencies` — con
sufijo `Repo` para distinguirlas de las interfaces estrechas
existentes a nivel handler:

| Campo en `Dependencies` | Antes | Ahora |
|---|---|---|
| `IPTVSchedules` | `*db.IPTVScheduleRepository` | `handlers.IPTVSchedulesRepo` |
| `Items` | `*db.ItemRepository` | `handlers.ItemsRepo` |
| `MediaStreams` | `*db.MediaStreamRepository` | `handlers.MediaStreamsRepo` |
| `Images` | `*db.ImageRepository` | `handlers.ImagesRepo` |
| `Metadata` | `*db.MetadataRepository` | `handlers.MetadataRepo` |
| `UserData` | `*db.UserDataRepository` | `handlers.UserDataRepo` |
| `Chapters` | `*db.ChapterRepository` | `handlers.ChaptersRepo` |
| `EpisodeSegments` | `*db.EpisodeSegmentRepository` | `handlers.EpisodeSegmentsRepo` |
| `People` | `*db.PeopleRepository` | `handlers.PeopleRepo` |
| `Studios` | `*db.StudioRepository` | `handlers.StudiosRepo` |
| `Collections` | `*db.CollectionRepository` | `handlers.CollectionsRepo` |
| `CollectionImageOverrides` | `*db.CollectionImageOverrideRepository` | `handlers.CollectionImageOverridesRepo` |
| `UserPreferences` | `*db.UserPreferenceRepository` | `handlers.UserPreferencesRepoForDeps` |
| `Home` | `*db.HomeRepository` | `handlers.HomeRepo` |
| `ExternalIDs` | `*db.ExternalIDRepository` | `handlers.ExternalIDsRepo` |
| `LibraryRepo` | `*db.LibraryRepository` | `handlers.LibrariesRepo` |
| `ProviderRepo` | `*db.ProviderRepository` | `handlers.ProvidersConfigRepo` |
| `Settings` | `*db.SettingsRepository` | `handlers.SettingsRepo` |
| `Activity` | `*db.ActivityRepository` | `handlers.ActivityRepo` |

### 4 handlers que aún aceptaban concretos

Actualizados a interfaces locales **estrechas** (no a las broad —
preservar el principio "interfaz estrecha en consumidor"):

- `SystemHandlerConfig.Activity` → `activityRepo` (DailyWatchActivity
  + TopItems).
- `SettingsHandlerConfig.Settings` → `settingsStore` (Get + Set +
  Delete).
- `NewAdminStreamsHandler(... items ...)` → `adminStreamsItemLookup`
  (GetByID).
- `NewHomeHandler(home ..., items ...)` → `homeRepo` (4 rails) +
  `ItemRepository` (interface existente).

### Por qué dos niveles

- **Broad (`handlers.XRepo`)**: lo que `Dependencies` declara. Cubre
  la UNIÓN de los métodos invocados a través de `deps.X`. Expresa
  el contrato **una vez** a nivel composition root.
- **Estrecha (interface por handler)**: lo que cada handler consume.
  Ya existían — los handlers siempre consumieron interfaces
  localmente. Las broad son superset, así que `deps.X` (broad) se
  pasa a constructores que esperan estrechas via duck-typing.

### Aislamiento

- `internal/api/router.go`: 18 campos cambian de tipo. **Cero cambios
  semánticos**.
- 2 imports `"hubplay/internal/db"` desaparecen de handlers
  (`settings.go`, `admin_streams.go`).
- `main.go`: **cero cambios** — `repos.Items` etc. satisfacen
  estructuralmente las nuevas interfaces.

### Tests

`go test ./internal/api/...` verde sin modificaciones — los repos
concretos satisfacen las nuevas interfaces, los handler tests
siguen pasando `*db.X` directo y siguen compilando.

| Categoría | Métrica |
|---|---|
| Ficheros tocados | 5 modificados + 1 nuevo |
| LoC delta total | +280 / −15 (el +218 es deps_repos.go nuevo) |
| Tests modificados | 0 |
| Imports `db` eliminados de handlers | 2 |

### Iteración 6 del audit — estado final

| Olor | Estado |
|---|---|
| V (config en router) | ✅ #395 |
| JJ (3 setters stream.Manager) | ✅ #395 |
| LL (Manager+Transcoder doble session) | ✅ #395 (parcial, documentación) |
| G iptv (feature module) | ✅ #417 |
| G library (feature module) | ✅ #418 |
| H mountXxx (router split) | ✅ sesión 2026-05-21 |
| H deps-interfaces | ✅ #419 |

**Iteración 6 cerrada al 100 %.** Quedan polish (F14-7-a sub-loggers,
F15-1 time.Sleep) y refactors grandes con sesión propia (LL stateless
full).

---

## 🧩 Sesión 2026-05-25 (extensión post-cierre) — G fase iptv: `iptv.Module`

Sesión corta sobre la cierre del 2026-05-25. Una sola PR (open al
cierre, esperando review): el módulo per-feature de iptv que faltaba
para llevar el olor G hacia el 100 %.

### Cambio

Nuevo `internal/iptv/module.go` (~225 LoC) con tipo `Module` que
agrupa los 6 componentes long-lived del feature IPTV:

| Componente | Antes (main.go) | Ahora (Module) |
|---|---|---|
| `Service` | `iptv.NewService(11 args)` + `SetEventBus` + `SetIPTVOrgLogos` + `SetProberWorker` | `iptv.New` interno |
| `StreamProxy` | `iptv.NewStreamProxy` + `SetHealthReporter(service)` | idem, en `iptv.New` |
| `TransmuxManager` | `if cfg.IPTV.Transmux.Enabled { … }` 35 LoC en main con `Gate=proxy.Breaker()` + `Reporter=service` + gauges Prometheus | `Deps.Transmux TransmuxOpts` (zero-value = disabled), `RegisterGauges` callback inyectado |
| `LogoCache` | `iptv.NewLogoCache(dir, logger)` con fallback log-warn | idem, en `iptv.New` |
| `Scheduler` | `iptv.NewScheduler(...) + Start(ctx) + lc.AddWorker` | `iptv.New` lo arranca, `RegisterWith(lc)` registra el hook |
| `ProberWorker` | `iptv.NewProber + iptv.NewProberWorker + Start(ctx) + SetProberWorker + lc.AddWorker` | idem |

`iptv.New(ctx, Deps)` aplica todo el cross-wiring interno y arranca
los workers contra el ctx. `iptv.Module.RegisterWith(lc)` recibe el
`*lifecycle` del binario vía `iptv.LifecycleRegistrar` interface
local al paquete (compatible estructuralmente con `*lifecycle` —
mismo alias `stopFn = func(context.Context) error`). RegisterWith
añade 5 hooks en el orden correcto:

- **Workers** (fase 1, add-order): `iptv scheduler` → `iptv prober`.
- **Services** (fase 3, LIFO ⇒ último registrado = primero parado):
  `iptv service` → `iptv proxy` → `iptv transmux` (sólo si enabled).
  Service queda fuera el último porque proxy y transmux le reportan
  health durante su drain.

### Aislamiento de dependencias

`iptv.Deps` recibe **valores pre-resueltos** para no abrir imports
desde `iptv` hacia `config` / `observability` / `stream`:

- HWAccel encoder + flags → `TransmuxOpts.ReencodeEncoder`,
  `.ReencodeHWAccelArgs` (main los saca de `streamManager.HWAccelInfo()`).
- Sink Prometheus → `TransmuxOpts.Metrics = observability.NewIPTVTransmuxSink(metrics)`.
- Register gauges → callback `func(*TransmuxManager) error` que
  cierra sobre `metrics`.
- Cache dirs → string paths absolutos (main los compone con
  `filepath.Join(cfg.Streaming.EffectiveCacheDir(), ...)` y
  `filepath.Dir(cfg.Database.Path)`).

`internal/iptv` sigue importando sólo `db`, `event`, `clock`,
`imaging`, `testutil` (más `iptv/model`).

### Impacto en main.go

- `cmd/hubplay/main.go`: 123 LoC → 53 LoC en el bloque iptv (**−70 LoC**).
- 6 variables locales (`iptvService`, `iptvProxy`, `iptvTransmux`,
  `iptvLogoCache`, `iptvScheduler`, `iptvProberWorker`) → 1 (`iptvMod`).
- `retentionRunner` ahora consume `iptvMod.Service` (mismo interface).
- `api.Dependencies` recibe los punteros vía `iptvMod.X` (handlers
  no se tocan — preservar surface fue minimum-blast-radius;
  Dependencies-as-interfaces queda diferido como en sesión H).

### Tests

`go test ./...` verde sin modificaciones — cero churn de behaviour
(`internal/iptv` 13s, `internal/api` 13s, `internal/api/handlers`
18s). El único fallo local es `internal/clock` por WDAC de Windows
bloqueando .exe en TEMP — sin relación con iptv.

### Aprendizajes

- **El `lifecycle` del binario es compatible con interfaces locales
  de paquete sin convertir**. La clave es que `stopFn = func(...)`
  es **type alias** (con `=`), no defined type, así que cualquier
  interface anónima con la misma firma lo acepta. Patrón
  reutilizable para library.Module.
- **Cross-wiring interno (proxy↔service, transmux↔proxy.Breaker) es
  la parte más limpia del Module**. Antes el ordering de cuándo
  llamar `SetHealthReporter` y `Gate=proxy.Breaker()` vivía
  scattered en main.go entre construct + setter; ahora vive
  encapsulado en una sola función. El comentario explicativo
  ("wire health reporting now that both pieces exist") deja de
  hacer falta porque la función fija el orden.
- **Mantener `Dependencies` con los punteros individuales fue lo
  correcto** vs cambiar `api.NewRouter` para que reciba un `IPTV
  *iptv.Module`. El refactor de Dependencies-as-interfaces (olor H
  pendiente) ya está en su propia sesión; mezclar aquí habría
  duplicado work y aumentado blast-radius sin valor añadido.

---

## 🪛 Sesión 2026-05-21 (noche tardía IV) — F14-2-b: `Manager.StartSession` a `StartSessionRequest`

Tercera y última pieza de la cadena F14-2-b. Tras cerrar `BuildFFmpegArgs` (PR #398) y `Transcoder.Start/RestartAt` (PR #399), llega el turno de la **API pública** del Manager — la firma que cruza el seam handler→stream.

### Cambio

```go
// Antes — 8 params posicionales (StartSession) + 9 (startSessionSlow)
StartSession(ctx, userID, itemID, profileName, caps, startTime,
             audioStreamIndex, burnSubIndex)

startSessionSlow(ctx, userID, itemID, profileName, caps, startTime,
                 key, audioStreamIndex, burnSubIndex)

// Después
StartSession(ctx, req StartSessionRequest)
startSessionSlow(ctx, key string, req StartSessionRequest)
```

`StartSessionRequest` con 7 campos: `UserID`, `ItemID`, `ProfileName`, `Caps`, `StartTime`, `AudioStreamIndex`, `BurnSubIndex`. Helper `(r StartSessionRequest) sessionKey()` centraliza la derivación de la clave canónica que tanto el fast-path lookup como el singleflight admission usan.

### Call sites actualizados

- **Interface `StreamManagerService.StartSession`** (`handlers/interfaces.go`): cambia firma.
- **3 callers producción** en handlers:
  - `stream.go:228` (player web)
  - `federation_stream.go:149` (`StartSession` para peer)
  - `federation_stream.go:245` (`QualityPlaylist` reusa la sesión)
- **fake `fakeStreamManager.StartSession` y su `startSessionFn`** en `stream_test.go` — cambian firma. 5 sites donde se asigna `startSessionFn = func(...)` updateados con `sed` (los firmaban con `func(_ context.Context, _, _, _ string, _ *Capabilities, _ float64)`).

### Aprendizajes

- **Rebase tras squash-merge** es la pieza operativa olvidada en stacks de PRs. Cualquier rama basada en otra rama de PR queda stale tras squash-merge porque el SHA en main es distinto al ancestor commit local. Fix consistente: `git rebase --onto origin/main <ancestor-sha>` o `git rebase origin/main` + resolver conflictos. Aplicado a #399 (basado en #398) y luego de nuevo a #400 (basado en main pre-#399).

- **`startSessionFn` es un patrón sano para tests**: el fake `fakeStreamManager` tiene un campo `startSessionFn func(ctx, req) (*Session, error)` que los tests asignan ad-hoc. Cuando cambia la firma del verdadero `StartSession`, sólo hay que actualizar el tipo del campo + los 5-N sites de asignación. Los call-sites no cambian (siguen llamando `env.manager.StartSession(...)` indirectamente vía el handler).

- **Tener helper `sessionKey()` en el request** evita que el fast-path lookup y el singleflight derivan keys distintas si se renombran campos. Antes la derivación estaba inline en dos sitios del Manager; ahora un solo metodo en el struct.

### Métricas

- **7 ficheros tocados** (manager.go + interfaces.go + 2 handlers + 2 test files + project-status.md).
- 1 commit (post-rebase con resolución del conflict en memoria).
- 0 callers no-test sin actualizar (verificado por `go build ./...` limpio).

---

## 🔁 Sesión 2026-05-21 (noche tardía III) — F14-2-b: Transcoder.Start/RestartAt a `TranscodeRequest`

Continuación natural de F14-2-a. Cierra el "patrón replicado tres veces" del audit aplicado a las dos firmas restantes del trío: `Transcoder.Start` (11 params) y `Transcoder.RestartAt` (11 params) → `(sessionID, itemID string, req TranscodeRequest)`. **Contrato split-de-llenado** documentado: caller llena 9 campos caller-side, Transcoder sobrescribe los 4 transcoder-side (OutputDir, HWAccel, Encoder, Libx264Preset) desde su estado interno.

Esta PR descubrió la operativa **rebase tras squash-merge**: PR #399 estaba basada en `claude/f14-2-a-transcode-request`. Cuando se mergeó #398 con squash, el SHA en main era distinto al ancestor de #399 — GitHub mostró `Can't automatically merge`. Fix: `git rebase --onto origin/main fc26b43` para dropear el commit ya-en-main, + force-push, + cambiar la base de #399 a main vía MCP. PR pasó de `mergeable_state: "dirty"` a `"clean"` con 1 commit. Mergeada después.

---

## 🎯 Sesión 2026-05-21 (noche tardía II) — F14-2-a: `BuildFFmpegArgs` → `TranscodeRequest`

Sesión corta y mecánica. Cierra el **último olor *alto* de severidad** de los 6 originales del audit 2026-05-14 (F14-2-a, function-level quality). Branch `claude/f14-2-a-transcode-request` desde `origin/main` post-PR-#396.

### Cambio

`stream.BuildFFmpegArgs(input, outputDir, profile, startTime, hwAccel, encoder, libx264Preset, copyVideo, copyAudio, toneMap, startSegmentNumber, audioStreamIndex, burnSub)` (13 params posicionales, 192 LoC body) → `stream.BuildFFmpegArgs(req TranscodeRequest) []string` (1 param).

**`TranscodeRequest`** struct con los 13 campos del audit: `Input`, `OutputDir`, `Profile`, `StartTime`, `HWAccel`, `Encoder`, `Libx264Preset`, `CopyVideo`, `CopyAudio`, `ToneMap`, `StartSegmentNumber`, `AudioStreamIndex`, `BurnSub`. Cada campo documentado inline; el doc-comment de la función ahora puede ser corto (los detalles per-campo viven en el struct).

### Call sites actualizados

| Tipo | Antes | Después |
|---|---:|---:|
| Producción (`Transcoder.Start`, `Transcoder.RestartAt`) | 2 calls posicionales | 2 calls con struct literal |
| Tests (`BuildFFmpegArgs_*` en `transcode_test.go`) | 18 calls posicionales | 18 calls vía `baseRequest(...)` helper + overrides |

`baseRequest(profile)` ya estaba previsto en el spec — devuelve un `TranscodeRequest` con los defaults usados por la mayoría de tests (input/output canónicos, HWAccelNone, libx264, sin seek, sin burn-in, AudioStreamIndex -1) y cada test override sólo lo que le importa. Reduce noise visual y deja claro qué difiere entre un test y otro.

### Qué NO entra en esta sesión

El audit menciona "Patrón replicado tres veces" — `Transcoder.Start` y `Transcoder.RestartAt` tienen las mismas 11 params posicionales que `BuildFFmpegArgs` (subset). Esos dos NO se refactorizan en esta PR — sólo tienen 1 caller cada uno (`manager.go`), el ROI es mínimo, y el olor F14-2-a estaba específicamente dirigido a `BuildFFmpegArgs`. Si se quiere cerrar al 100 % la "replicación tres veces" se puede hacer en una iteración futura reusando el mismo `TranscodeRequest` (sería un Caller-Supplied subset del 13-field struct).

### Aprendizajes

- **Helper de defaults vs. struct literal por site**: la opción ganadora para 18+ sites es helper `baseRequest(...)` + mutación de campos relevantes. Struct literal completo en cada test inflaría el fichero en ~200 LoC y haría difícil ver qué difiere entre un test y otro. El helper hace explícito "esto es default" vs "esto es lo que estamos midiendo".

- **AudioStreamIndex = -1 NO es zero value**. El zero value (`0`) significa "pin a audio stream 0" en BuildFFmpegArgs; el sentinel "auto-pick" es `-1`. Hay que set explicit -1 en el helper de defaults, o el comportamiento cambia silenciosamente. Caso clásico de "los zero values no siempre son el default deseado".

- **Audit's exact spec ≠ scope mínimo**. El audit lista `BuildFFmpegArgs` con su 13-field struct específico, pero también menciona Start/RestartAt como pattern repetido. Aplicar el audit "exactamente" significa cerrar F14-2-a sin tocar Start/RestartAt — limpia el blast radius de la PR. Cerrar "al 100 %" es otra sesión.

### Métricas

- **2 ficheros tocados** (`transcode.go`, `transcode_test.go`).
- **transcode.go 580 → 638 LoC** (+58 por el struct + sus doc-comments inline; `BuildFFmpegArgs` body sin cambio).
- **transcode_test.go 106 LoC añadidos / 115 borrados** (los 18 call sites se compactan al usar el helper).
- **6 → 0 olores altos restantes del audit 2026-05-14**. Cierra Iteración 8 al menos en su pieza visible.

---

## 🪓 Sesión 2026-05-21 (noche tardía) — Olor H: split de `router.go` en mountXxx helpers

Sesión propia y dedicada al fichero de mayor blast-radius del repo. Cierra el último olor estructural de Iteración 6 del audit 2026-05-14 (en su variante "mountXxx helpers", la simple; la variante "interfaces en Dependencies" queda para otra iteración).

### Diff principal

| Fichero | Antes | Después | Δ |
|---|---:|---:|---:|
| `internal/api/router.go` | 1549 | 465 | −1084 (−70 %) |
| `mount_public.go` | — | 84 | +84 |
| `mount_federation.go` | — | 238 | +238 |
| `mount_me.go` | — | 132 | +132 |
| `mount_users.go` | — | 98 | +98 |
| `mount_uploads.go` | — | 48 | +48 |
| `mount_admin_system.go` | — | 247 | +247 |
| `mount_media.go` | — | 434 | +434 |
| **Total** | **1549** | **1746** | **+197 (+13 % por docstrings de cada fichero)** |

### Pattern aplicado

`NewRouter` queda como composition root puro:

1. `applyGlobalMiddleware(r, deps)` — RealIP/RequestID/Logger/Recoverer/SecurityHeaders/Metrics/CORS/CSRF.
2. `mountMetricsEndpoint(r, deps)` — `/metrics` top-level fuera de `/api/v1`.
3. Construcción de handlers compartidos por varios mounts: `authHandler`, `userHandler`, `healthHandler`, `deviceHandler` (device auth tiene endpoints públicos + uno auth-gated), `fedImgSrv` (federation peer poster + local `/images/file/{id}` comparten cache).
4. `r.Route("/api/v1", func(r) { mount*(...) })` cablea 14 mountXxx (más `mountSPAFallback` al final).

Cada `mountXxx` recibe `r chi.Router` + las deps que necesita (la mayoría `Dependencies` completo + algún handler compartido por puntero). No se cambió `Dependencies` — sigue siendo god-struct con 60 campos. Los handlers consumidores ya estrechan vía interfaces locales, así que la doble-expresión del contrato (audit's "Dependencies-as-interfaces") queda como iteración futura.

### Helpers nuevos

| Función | Fichero | Responsabilidad |
|---|---|---|
| `mountHealthAndOpenAPI` | mount_public.go | `/health/*`, `/openapi.yaml` |
| `mountAuthPublic` | mount_public.go | `/auth/login`, `/auth/refresh`, `/auth/setup`, device public |
| `mountSetupWizard` | mount_public.go | `/setup/*` (wizard primera ejecución) |
| `mountFederationPublic` | mount_federation.go | `/federation/info`, `/peer/*` (handshake + JWT-gated) |
| `mountAdminAuthAndFederation` | mount_federation.go | `/admin/auth/keys`, `/me/peers`, `/admin/peers` (gated `ks != nil`) |
| `mountAuthProtected` | mount_me.go | `/auth/logout`, `/auth/device/approve` |
| `mountSSEEvents` | mount_me.go | `/events`, `/me/events` |
| `mountMeIdentity` | mount_me.go | `/me`, `/me/password`, `/me/avatar`, `/me/sessions`, profiles |
| `mountMeNotificationsAndPreferences` | mount_me.go | `/me/notifications`, `/me/preferences` |
| `mountWatchProgress` | mount_me.go | `/me/continue-watching`, `/me/favorites`, `/me/progress` |
| `mountHome` | mount_me.go | `/me/home/*` rails |
| `mountUsers` | mount_users.go | `/users/*` (admin + auth-only sub-rutas pin/display-name/avatar) |
| `mountUploads` | mount_uploads.go | `/uploads/*` (tus + audit + browse) |
| `mountAdminSystem` | mount_admin_system.go | `/admin/system/*` (stats, settings, backup, db, cors, logs, audit, updates, sessions, storage) |
| `mountStreaming` | mount_media.go | `/stream/{itemId}/*` |
| `mountLibrariesItemsAndIPTV` | mount_media.go | `/libraries`, `/items`, `/channels`, `/iptv` |
| `mountIPTVChannels` | mount_media.go | sub-helper interno (canales + EPG + favoritos + admin IPTV) |
| `mountImagesPeopleStudiosCollections` | mount_media.go | imágenes, people, studios, collections |
| `mountProviders` | mount_media.go | `/providers/*` |
| `mountSPAFallback` | router.go | SPA fallback al final del router |

### Test del drift OpenAPI ↔ router (`openapi_drift_test.go`)

El test parseaba `router.go` por AST y enumeraba las rutas. Como las moví a `mount_*.go`, dejó de verlas. **Extendido** para:

1. Parsear todos los `.go` no-test del paquete `internal/api/` y construir un map `mountFuncs[name]→*ast.FuncDecl` de funciones cuyo nombre empieza por `mount`.
2. Cuando el walker encuentra una llamada como `mountAdminSystem(r, deps)` desde dentro del cuerpo de `NewRouter` (o de otro mount, recursivamente), descender al body de la función llamada **con el prefix vigente**. Así `mountAdminSystem` llamada desde dentro de `r.Route("/api/v1", ...)` aporta sus rutas bajo `/api/v1/admin/system/*` correctamente.

Una iteración inicial añadió un `case "With"` en el switch del verbo para chained-middleware `r.With(mw).Put(...)` — bug: el walker existente ya manejaba ese patrón correctamente (el verbo final es el `Sel.Name` del outer SelectorExpr). Quitado antes del commit.

### Aprendizajes

- **`r.With(mw).Put(...)` parsea como SelectorExpr(X=CallExpr-de-With, Sel="Put")**, así que el walker original lo coge en el `case "Put"` sin tratamiento especial. Sólo hace falta extender el walker cuando aparece una construcción NUEVA del idioma chi, no por mover rutas a otro fichero.

- **El test de drift OpenAPI es el guardián real del refactor**: sin él, mover rutas a ficheros aparte habría sido invisible a los tests unitarios (cada handler tiene sus propios tests, pero ninguno valida "esta ruta está mapeada en el router"). El AST walk catch'ea drift en milisegundos vs. levantar el router real con 30 servicios + DB.

- **`Dependencies` se queda como god-struct esta sesión**. La variante "interfaces en Dependencies" (los 22 `*db.X` concretos del campo → interfaces) es ortogonal al split de router; el split ya cierra el síntoma principal del olor H (callback monolítico de ~1100 LoC en `r.Route("/api/v1", ...)`). Los handlers downstream ya consumen interfaces locales, así que el contrato sigue doblemente expresado pero ahora confinado al composition root.

- **Construcción de handlers SE QUEDA en NewRouter cuando son compartidos** (`authHandler`, `userHandler`, `fedImgSrv`, `deviceHandler`). Lo opuesto — construir cada handler dentro de su propio mount — duplicaría instancias del mismo handler con state separado (caches, contadores). El compromiso: shared handlers cableados arriba + un nilcheck en cada mount que los recibe.

### Métricas globales

- **8 ficheros nuevos** (7 `mount_*.go` + cambios en `openapi_drift_test.go`).
- **router.go 1549 → 465 LoC** (−70 %). Fichero ya no es el de mayor LoC del paquete (lo es `mount_media.go` con 434, justificado por agrupar streaming + libraries + items + IPTV + images + collections + providers).
- **271 rutas chi preservadas** verbatim — `go test -race ./...` verde, `go vet` limpio.
- **Test de drift extendido** para parsear el paquete entero y descender a las funciones `mount*` recursivamente.

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

**Iteración 6 — composition root** (5 de 5 cerrados, G + H parciales/diferidos):
- ~~V~~ ✅ primitivos de Config a `Dependencies` (PR #395, `61396a3`)
- ~~JJ~~ ✅ `stream.NewManager(Deps{...})` wiring atómico (PR #395, `61396a3`)
- ~~LL~~ ✅ docs `Manager`/`Transcoder` (cerrado por diseño, PR #395, `61396a3`)
- ~~G~~ ⚠️ parcial — `runtime` god-struct sustituido por `lifecycle` con 3 fases (commit `8b746fc` en `claude/review-project-9YJxG`, pendiente PR). Feature modules (`library.Module`, `iptv.Module`) **diferidos** — cierra el síntoma del audit pero no al 100 %.
- ~~H~~ ✅ split `router.go` 1549 → 465 LoC (−70 %) en 7 ficheros `mount_*.go` per-feature (sesión 2026-05-21 noche tardía, branch `claude/project-review-1Zrtv`). La variante "interfaces en Dependencies para los 22 `*db.X` concretos" queda **diferida** — el split cierra el síntoma principal (callback monolítico ~1100 LoC) y los handlers ya consumen interfaces locales.

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
- ~~F14-2-a~~ ✅ `BuildFFmpegArgs` 13 params → `TranscodeRequest` struct (sesión 2026-05-21 noche tardía II, branch `claude/f14-2-a-transcode-request`). **0 olores altos pendientes del audit original.**
- F14-X, F15-X, F16-X (varios) — calidad de código residual. F14-2-b (Start/RestartAt comparten el pattern de 11 params, pendiente) es el más cercano en scope.

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
