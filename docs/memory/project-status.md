# Estado del proyecto

> **Entrypoint de cada sesión.** Lo cerrado antes del 2026-05-27 parte 2
> vive en `archive/2026-05-19-to-05-27.md`. No se pierde nada — sólo se
> mueve de sitio para que este fichero sea legible de un vistazo.

---

## 🔭 Estado actual (2026-05-27)

**Salud del proyecto**: MVP funcional, cerca de early-production.

| Área | Estado |
|---|---|
| **Audit olores altos** | ✅ **6/6 cerrados** (A+M, B+J, CC, P, W, F14-2-a, G, H, LL) |
| **Audit olores medios** | ✅ Núcleo cerrado. Queda **F15-5** (integration tests library, sesión propia) |
| **F16 (handlers)** | ✅ 100% cerrado (8/8 medium + 10/10 bajas) |
| **F15 (tests)** | ✅ F15-1/2/3/4/6/7/8/9 cerrados. **F15-5** pendiente. F15-10/11/12 polish opcional |
| **Tests backend** | 191 `_test.go` files, race-clean (verificado con LLVM-MinGW UCRT en Windows) |
| **Tests frontend** | **717/717** vitest verdes |
| **CI** | Todos los jobs verdes: Lint, Test Backend, Postgres, Frontend, knip, govulncheck, goleak, React Doctor |
| **PRs abiertas** | [#471](https://github.com/Alexzafra13/HubPlay_demo/pull/471) F15-6 error coverage (recién abierta, esperando CI) |

**Distribución**: HubPlay ya distribuible "descargar y usar" en los 3 targets (desktop / Linux server / NAS Docker). Installer Windows existente sin firma; SignPath integration **lista en CI pero opt-in** — pendiente que Alejandro aplique a SignPath Foundation y configure secrets.

---

## 🧪 Sesión 2026-05-27 (parte 4) — F15-6 error coverage en LibraryHandler

PR única: [#471](https://github.com/Alexzafra13/HubPlay_demo/pull/471). 6 tests nuevos en `library_test.go` (+82 LoC, 0 producción) cubren error opaco (5xx no-AppError) en endpoints que solo tenían cobertura para AppError tipados (404, 409).

### Hallazgo honesto

El audit decía *"sólo 3% de tests con naming `*_Error`/`*_Fail`"*. En la práctica el ratio ya era **~25% en library_test.go** — la cobertura mejoró mucho desde 2026-05-14. Los gaps reales eran error genérico del repo (driver SQL caído, contexto cancelado, write timeout): si el repo devuelve `errors.New("db: timeout")`, el handler debe rendir **500** vía `handleServiceError` default case, no 200 con lista vacía.

### Tests añadidos

| Test | Pin |
|---|---|
| `Get_ServiceError_500` | err opaco → 500 (no 404) |
| `Update_NotFound_404` | NotFound mapeado correctamente |
| `Update_ServiceError_500` | err opaco → 500 |
| `Delete_ServiceError_500` | err opaco → 500 |
| `Items_ServiceError_500` | err opaco → 500 (no 200 con lista vacía) |
| `LatestItems_ServiceError_500` | err opaco → 500 |

### Decisiones

- **`items_test.go` no se toca**: análisis mostró cobertura adecuada (3 tests/endpoint con error paths). Forzar más tests sería scope creep.
- **F15-5 deferido**: integration tests con DB real para library require setup masivo (library.Service tiene 7+ deps, scanner aún más). Sesión propia, 4-6 h.

---

## 🧹 Sesión 2026-05-27 (parte 3) — LL Transcoder stateless + SignPath signing + Dependabot + F15-3/4/9 cerrado

3 PRs mergeadas + 3 items del audit cerrados sin código nuevo.

### PRs

| PR | Tema | Estado |
|---|---|---|
| [#468](https://github.com/Alexzafra13/HubPlay_demo/pull/468) | refactor(stream): Transcoder stateless (cierra olor **LL**) | ✅ merged |
| [#469](https://github.com/Alexzafra13/HubPlay_demo/pull/469) | ci(release): SignPath Foundation signing del installer Windows (opt-in) | ✅ merged |
| [#424](https://github.com/Alexzafra13/HubPlay_demo/pull/424) | chore(deps): bump web-deps group (18 npm packages) | ✅ merged |
| [#470](https://github.com/Alexzafra13/HubPlay_demo/pull/470) | docs(memory): registro de la sesión | ✅ merged |

### LL — Transcoder stateless (#468)

Eliminado `Transcoder.sessions map` + `Transcoder.mu` + 4 métodos públicos de tracking (`GetSession`, `Stop`, `StopAll`, `ActiveSessions`). El tracking vive **solamente en `Manager.sessions`**. El `Manager` ya garantizaba unicidad via `singleflight.Group` + fast-path, así que la lógica `if existing, ok := t.sessions[sessionID]` en `Transcoder.Start` era código defensivo muerto. Net: **-130 LoC**, último olor alto del audit cerrado (**6/6**).

### SignPath signing (#469)

Firma Authenticode opt-in al installer Windows existente (el installer ya existía — sólo faltaba la firma). 3 steps nuevos en `release.yml` gated tras `vars.HUBPLAY_SIGNING_ENABLED == 'true'`. Mientras la variable no esté activa, el flujo corre exactamente igual que antes. Release notes condicionados al estado de la firma. Documentación: [`docs/architecture/windows-installer-signing.md`](../architecture/windows-installer-signing.md) (~250 LoC) con aplicación SignPath Foundation paso a paso, configuración del dashboard, secrets/vars de GitHub, verificación local.

**Pendiente de Alejandro**: aplicar en [signpath.org/apply](https://signpath.org/apply) (10 min formulario, espera 1-2 semanas), configurar el dashboard, añadir 1 secret + 4 vars en GitHub, cambiar `HUBPLAY_SIGNING_ENABLED` a `true`. Próximo build sale firmado automático.

### F15-3 / F15-4 / F15-9 — análisis y cierre sin código

| Item | Estado | Justificación |
|---|---|---|
| **F15-3** (polling `waitForCount`) | ✅ ya cerrado por F15-1 | `auth/service_test.go:671-682` ya usa `select { case <-r.notify; case <-deadline }` con notify-channel. |
| **F15-4** (`TestManager_CloseStopsSweeperGoroutine`) | ✅ ya cerrado por F15-1 batch 4 | `federation/stream_test.go:112` mantiene sleep 50ms con comentario "Sleep LEGÍTIMO" — ruido de scheduler para 25 ciclos Close. Goleak cubre regresión real. |
| **F15-9** (`time.After` en 23 tests) | ✅ cerrado por análisis | 37 sitios revisados (no 23 — creció desde el audit). **TODOS son patrones legítimos** (`select` con timeout). El audit hablaba de un anti-pattern teórico que Go 1.23+ resuelve a nivel runtime. HubPlay usa 1.24.7. |

### Dependabot #424 — verificación

18 bumps web-deps: 12 patches + 6 minors (incluyendo react-query 5.90→5.100, tailwindcss 4.2→4.3, vitest 4.1.0→4.1.7). Verificado local:

- ✅ `pnpm install --frozen-lockfile` (12.1s)
- ✅ `pnpm run build` (30s, 107 entries PWA)
- ✅ `pnpm test` (**646/646**)
- ✅ `pnpm run lint` (0 errors, 2 warnings preexistentes react-compiler con `useVirtualizer`)
- ✅ `pnpm run typecheck` silent
- ✅ `pnpm run knip` 0 unused

Cero breaking changes a pesar de los 10 minors acumulados de react-query y el bump 4.2→4.3 de Tailwind v4.

---

## 🎬 Sesión 2026-05-27 (parte 2) — VideoPlayer 3ª ola + ADR-026 follow-ups + F15-2 cerrado (11 PRs)

Sesión larga centrada en cerrar la **3ª ola del refactor del VideoPlayer** (787 → 652 LoC, -17.2%), los **follow-ups del ADR-026** (logs centralizados + transmux + circuit breaker) y finalmente **F15-2 db repos** (clock seam para los 21 repos).

### VideoPlayer 3ª ola — hooks extraídos

| Hook | LoC quitadas | Tests | Notas |
|---|---|---|---|
| `useVideoElementSync` | -6 | 7 | 2 effects sync volume/mute/playbackRate al `<video>`. Re-aplica rate en remount. |
| `useStreamSessionCleanup` | -20 | 5 | `pagehide` → `api.stopStreamSession` (evita leak de transcode ~90s). |
| `useStartPositionSeek` | -19 | 8 | `canplay` listener + ref guard + reset on source change. |
| `useFullscreenSync` | -9 | 5 | Listener `fullscreenchange` → sync al store. |
| `useExternalSubMode` | -14 | 7 | rAF + force `track.mode = "showing"`. |
| `usePlayerActions` | -67 | 26 | 8 useCallback (togglePlay/surfaceTap/seek/volume/mute/fullscreen/close/PiP). |

**VideoPlayer.tsx**: 787 → **652 LoC** (-135 acumulados, -17.2%). 6 useEffect inline → 0. 9 useCallback inline → 1.

### F15-2 db repos (PR #466) — pattern decisivo

`NewRepositories(...)` tiene **33 callsites**. Cambiar la API del constructor para aceptar `clock.Clock` sería ruido masivo. Solución: **package-level seam** (`var timeNow = time.Now` en `internal/db/now.go`) con helper `SetTimeNowForTest(t, fn)` en `now_helpers_test.go`. Idiomático en stdlib (`crypto/rand`, `os/user`) cuando el coste de DI desborda el beneficio.

PRs: [#452](https://github.com/Alexzafra13/HubPlay_demo/pull/452), [#454](https://github.com/Alexzafra13/HubPlay_demo/pull/454), [#459](https://github.com/Alexzafra13/HubPlay_demo/pull/459), [#460](https://github.com/Alexzafra13/HubPlay_demo/pull/460), [#461](https://github.com/Alexzafra13/HubPlay_demo/pull/461), [#462](https://github.com/Alexzafra13/HubPlay_demo/pull/462), [#463](https://github.com/Alexzafra13/HubPlay_demo/pull/463), [#464](https://github.com/Alexzafra13/HubPlay_demo/pull/464), [#465](https://github.com/Alexzafra13/HubPlay_demo/pull/465), [#466](https://github.com/Alexzafra13/HubPlay_demo/pull/466), [#467](https://github.com/Alexzafra13/HubPlay_demo/pull/467).

---

## 📋 Pendientes priorizadas

| # | Tarea | Coste | Severidad |
|---|---|---|---|
| **1** | **F15-5** — Integration tests con DB real para handlers de library. Requiere extender `testApp` en `internal/api/integration_test.go` con `library.Service` real. library.Service tiene 7+ deps (libraries, items, streams, images, channels, itemValues, scanner) y scanner aún más. Setup ~4-6h. | ~4-6 h | Media |
| **2** | **F15-10 / F15-11 / F15-12** — Polish: fakes compartidos en `testutil/fakes/`, naming canónico `TestX_When_Then`, concurrency tests para `provider.Manager.Register` y `stream.Manager.StartSession`. | Baja | Baja |
| **3** | **Distribución avanzada** — auto-update del binario en producción, TLS LAN automático (mDNS + Let's Encrypt local), macOS notarized (DMG firmado), AppImage Linux. | Sesión grande | Producto |
| **4** | **(Manual) Alejandro: SignPath Foundation** — aplicar, esperar aprobación, configurar dashboard + secrets/vars de GitHub. Doc: [`windows-installer-signing.md`](../architecture/windows-installer-signing.md). | 10 min + 1-2 sem espera | Distribución |

**Sin olores altos pendientes** (6/6 cerrados desde el audit 2026-05-14). **Sin medium críticos** (salvo F15-5 que es item discreto y aislado).

---

## 🏛 Referencias (vivos, mantenidos)

- [`architecture-decisions.md`](architecture-decisions.md) — ADRs (AppError, observability, keystore, sink pattern, preflight, sqlc adapter, ADR-026 logs).
- [`conventions.md`](conventions.md) — patrones del codebase, reglas de test, anti-ciclo, comentarios en español, regeneración sqlc.
- [`audit-2026-05-14-go-backend-review.md`](audit-2026-05-14-go-backend-review.md) — referencia del audit original. La mayoría cerrada; ver tabla "items audit" abajo.
- [`intervention-2026-05-14.md`](intervention-2026-05-14.md) — review arquitectónico vivo.
- [`perf-benchmarks-2026-05-17.md`](perf-benchmarks-2026-05-17.md) — baseline benchmarks dual-backend.
- [`windows-installer-signing.md`](../architecture/windows-installer-signing.md) — guía de aplicación SignPath + activación.

## 📦 Archive

- [`archive/2026-05-19-to-05-27.md`](archive/2026-05-19-to-05-27.md) — sesiones 2026-05-19 al 2026-05-27 parte 1 (refactor masivo audit, F15-1, F16, security XFF, distribución, lifecycle, G+H feature modules, BB comentarios traducidos, F14 splits, t.Parallel, auditoría logs).
- [`archive/per-user-channel-order-spec-shipped.md`](archive/per-user-channel-order-spec-shipped.md) — spec cerrada Live TV.
- Audits antiguos archivados: `audit-2026-04-15.md`, `audit-2026-04-28.md`, `audit-2026-05-05.md`.

---

## 🗂 Quick reference: items audit 2026-05-14

### Olores altos (6/6 cerrados ✅)

| Olor | Tema | Cerrado por |
|---|---|---|
| A+M | `*db.X` en services, no via interfaces | Sesión 2026-05-21 (H deps-interfaces #419) |
| B+J | Dependencias ciclos (observability ↔ stream/handlers) | Sink pattern (interfaces locales por paquete) |
| CC | iptv.Service god-struct | Split CC fase 1 + 2 (sesión 2026-05-21) |
| P | ItemHandler god-handler 1186 LoC, 13 deps, 4 responsabilidades | Split en 5 sub-handlers via facade embedding |
| W | router.go 1549 LoC | Split en 7 mount_*.go (sesión 2026-05-25) |
| F14-2-a | BuildFFmpegArgs(13 params) | Struct `TranscodeRequest` (#398-#402) |
| G | Composition root (lifecycle, runtime, main.run) | `lifecycle.go` (#396) + `library.Module` (#418) + `iptv.Module` (#417) |
| H | `*db.X` directos en `Dependencies` | Interfaces broad (#419) |
| LL | Transcoder + Manager con doble session tracking | Transcoder stateless ([#468](https://github.com/Alexzafra13/HubPlay_demo/pull/468)) |

### Olores medios (núcleo cerrado, F15-5 pendiente)

| Olor | Estado |
|---|---|
| F14-3/4/5 | ✅ 3 splits de funciones largas + naming convention |
| F14-6 | ✅ `respondData` helper (115 sites) + `requireParam` (53 sites) |
| F14-7-a | ✅ Sub-loggers `.With()` aplicados donde valían |
| F14-9 / 9-a / 10-a / 12-a | ✅ Where builder, CacheControl constantes, structs returns, sqlPlaceholders |
| F15-1 | ✅ 41 sleeps eliminados, 11 documentados legítimos |
| F15-2 | ✅ Clock-injected en scanner/notification/upload + db repos via package seam |
| F15-3 | ✅ waitForCount ya migrado a notify-channel en F15-1 |
| F15-4 | ✅ Sleep legítimo documentado, goleak cubre regresión real |
| F15-5 | ⚠️ **Pendiente** (integration tests library, sesión propia) |
| F15-6 | ✅ 6 tests nuevos en library_test.go ([#471](https://github.com/Alexzafra13/HubPlay_demo/pull/471)) |
| F15-7 | ✅ 314 → 375 t.Parallel (+61) |
| F15-8 | ✅ `t.TempDir()` adoptado |
| F15-9 | ✅ Cerrado por análisis (todos legítimos, Go 1.23+ runtime cleanup) |
| F16 | ✅ 100% (8/8 medium + 10/10 bajas) |

### Olores bajos (polish)

- **F15-10/11/12** — Polish opcional (fakes compartidos, naming, concurrency tests).

---

## 🧠 Aprendizajes del proyecto (transversales)

Patrones consolidados durante el refactor que vale la pena replicar:

- **Patrón notify-channel + deadline** para tests determinísticos (canon de F15-1). Buffer 32, send non-blocking. `WaitForXxx` con `select { case <-notify; case <-deadline }`.
- **Sink pattern** para observability: interfaces locales por paquete con `noopSink{}` default. Evita ciclos de import (cierra olor B+J).
- **Package-level seam** (`var timeNow = time.Now`) cuando la API es ancha (33+ callsites): idiomático stdlib, opt-in para tests via helper `_test.go`. Mejor que DI cuando el coste de cambio de constructor desborda el beneficio.
- **Feature modules** (`library.Module`, `iptv.Module`) con orden de shutdown LIFO en `RegisterWith` (servicios paran ANTES que sus suscriptores del event bus).
- **Opt-in via repo variable** (no secret) para CI features con setup externo del operador: `vars.X_ENABLED == 'true'`. Patrón usado en SignPath signing (#469).
- **Cerrar por análisis cuando el runtime moderno resuelve el problema teórico**: F15-9 (`time.After` leak teórico) ya no aplica con Go 1.23+. No refactorizar a `context.WithTimeout` si no hay bug observable.
- **Leer el código existente antes de implementar lo del backlog**: el installer Windows ya existía cuando fui a "implementarlo"; lo único que faltaba era firmar.
- **Fix centralizado vs audit por paquete**: cuando un follow-up sugiere "auditar paquete por paquete", buscar si hay un punto centralizado (ej. `handleServiceError` en lugar de cada service.go).
- **Cherry-pick chain con conflictos triviales**: si 4 PRs ramificadas tienen el mismo conflict trivial, consolidar en una sola PR. Más limpio para revisor.
- **Cuidado al mergear PRs sin esperar CI**: si la rama tiene > 1 commit y el último es un fix de CI, esperar a que el CI lo refleje verde antes de mergear.
