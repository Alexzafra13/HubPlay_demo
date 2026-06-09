# Estado del proyecto

> **Entrypoint de cada sesión.** Lo cerrado antes del 2026-05-27 parte 2
> vive en `archive/2026-05-19-to-05-27.md`. No se pierde nada — sólo se
> mueve de sitio para que este fichero sea legible de un vistazo.

---

## 🌐 Sesión 2026-06-08 (parte 3) — Endurecimiento prod: Fase 0 (seguridad) + Fase 1 (despliegue)

Rama: `claude/project-review-PO25J`. A partir del audit
`audit-2026-06-08-production-readiness.md`. Enfoque **plug-and-play**
(estilo Plex): defaults seguros que funcionan sin configurar nada, cero
pérdida de datos. Todo con tests; build/vet/`-race` verdes.

### Fase 0 — bloqueantes de exposición a internet (7 items)
| Item | Fix |
|---|---|
| C1 | `extractToken` ya no acepta `?token=` (solo Bearer/cookie) — evita fuga del token por logs/Referer/historial. `middleware_test.go` |
| C2 | `logging.RedactURL` + redacción por-valor en el `ReplaceAttr` central → enmascara user/pass en URLs IPTV (m3u/epg/upstream) en todos los call-sites. `logging_test.go` |
| A1 | `IPRateLimitMiddleware` en login/refresh/setup/device (`NewAuthRateLimiter` 30/min·burst 10 por IP) |
| A2 | Lockout de login por tupla `user:<u>@<ip>` (no username global) → mata el DoS de cuenta. `service_test.go` |
| A3 | Proxy IPTV firma (HMAC-SHA256) las URLs reescritas; el handler exige+verifica firma → cierra el relay HTTP abierto. **No** host-lock (compatible multi-CDN). `proxy_sign_test.go`, `iptv_test.go` |
| A4 | Quitadas las 4 cabeceras `Access-Control-Allow-Origin: *` del proxy HLS |
| A5 | `observability.metrics_token` (+env) gate Bearer/`?token=` en `/metrics` + aviso si expuesto. `metrics_auth_test.go` |

### Fase 1 — robustez de despliegue (plug-and-play)
| Item | Fix |
|---|---|
| A7 | `tini` PID 1 en ambos stages del Dockerfile (reapea ffmpeg huérfanos + SIGTERM) + `init:true` en compose |
| A8 | `stop_grace_period: 40s` en los compose (la app drena en 30s) |
| M10 | `SaveDatabaseConfig` ancla el SQLite vacío/relativo bajo `/config` → no pérdida de datos al recrear contenedor. `service_test.go` |
| A6 | nginx: bloque SSE dedicado (`proxy_buffering off`) → progreso de scans/uploads/pairing en tiempo real |
| M9 | nginx: `location /` body acotado a 1g; subidas tus por bloque `/api/v1/uploads/` propio (sin tope) |
| A5-perímetro | `deny`/`403` de `/metrics` en nginx y Caddy |
| M11 | Backup SQLite automático (`VACUUM INTO`) antes de migrar, keep 3, best-effort. `build_database_backup_test.go` + integración real |

**Diferidos a propósito** (forzarlos rompería plug-and-play): **M5**
(password Postgres — es opt-in/avanzado, SQLite es default), **M7**
(límites mem/cpu — dependen del hardware; hardcodearlos rompería
transcoding o causaría OOM). Documentado en el audit.

**Hallazgo afinado:** el supuesto bug crítico de DB efímera (M10) era
parcial — la imagen por defecto ya pasa `--config /config/hubplay.yaml`,
así que la DB ya caía en el volumen. Aun así se añadió la defensa en
`SaveDatabaseConfig`.

### Bloque 1 — seguridad media (post-Fase 0/1)
| Item | Fix |
|---|---|
| M4 | `trustForwardedProto` borra `X-Forwarded-Proto` de peers no declarados en `trusted_proxies` → no se puede falsear https. `forwarded_proto_test.go` |
| M2 | `EnforcePasswordChange` bloquea mutaciones (403) de usuarios con `password_change_required` salvo `/me/password` + `/auth/logout`; mira el flag en DB (desbloqueo inmediato). `password_change_test.go` |
| M8 | `Permissions-Policy` en la app (deniega camera/mic/geo/payment/usb; NO toca fullscreen/autoplay/PiP). `security_headers_test.go` |
| M3 | `RequirePrivateClient`: `/auth/setup` y `/setup/*` (salvo `/setup/status`) solo desde IP privada/loopback → cierra race-to-setup + browse del FS desde internet, sin romper LAN. `setup_network_test.go` |
| B5 | `ReadHeaderTimeout: 10s` en el `http.Server` (slowloris) |
| M1 | Verificado: el double-submit CSRF ya exige header en mutaciones cookie-auth; Bearer no es CSRF-vulnerable. Sin cambio. |

### Bloque 2 — quick wins de mayor retorno
| Item | Resultado |
|---|---|
| A11 ✅ | Error boundary por ruta en `AppLayout` (`key={pathname}`): un crash de página ya no tira el shell/navegación. Test `ErrorBoundary.test.tsx`. |
| A12 ✅ | `MediaGrid` virtualizado (window scroll, `@tanstack/react-virtual`). DOM acotado: pico ~72 tarjetas para 5000 ítems (vs 5000), verificado en Chromium real (arnés `web/verify/` con Playwright + capturas). Filas de altura fija (título/meta 1 línea), columnas responsive idénticas. Lleva `"use no memo"` (el compiler v1.0 sobre-memoizaba el store del virtualizador y rompía el reciclado); override de eslint en ese fichero por desfase del plugin de lint (rc2, sin 1.x). Guardia jsdom + stub `ResizeObserver` en setup. |
| M22 | **No-issue**: la `JWTSecret` auto-gen solo siembra el keystore en el 1er arranque (`Bootstrap` solo si la tabla está vacía); luego las claves viven en DB (persistidas) y son la fuente de verdad. Con DB compartida las réplicas comparten clave → ya mitigado. No se toca. |

**Verificación en navegador real (nuevo en el repo):** `web/verify/` —
arnés standalone (`grid-harness.*`) + script Playwright (`measure-grid.mjs`,
usa `PW_CHROME`) que mide cuántas `PosterCard` hay en el DOM y comprueba
que la virtualización recicla. No entra en build ni CI (manual). `playwright-core` añadido como devDep.

**Pendiente (no bloquea plug-and-play):** Fase 2 supply-chain (SHA-pin
de actions, provenance/firma, checksum FFmpeg), Fase 3 observabilidad
(M18-M21/M23/M24), Fase 4 frontend (A12 virtualización de grids — alto
valor pero toca el render core, requiere prueba de scroll real), Fase 5
gobernanza (README/SECURITY/CODEOWNERS). Bajos: B2 (DNS-rebind TOCTOU),
B3 (refresh TTL 30d), M6 (backup periódico).

---

## 🌐 Sesión 2026-06-08 (parte 2) — F15-5 integration tests library

Rama: `claude/project-review-PO25J`.

### F15-5 — integration tests con DB real para LibraryHandler

Cerrado el último item de severidad media del audit 2026-05-14. Nuevo
fichero `internal/api/handlers/library/library_integration_test.go`
(~9 tests) que cablea el **`library.Service` real sobre repos sqlc
reales** (`testutil.NewTestRepos`) y dirige requests HTTP por chi.
A diferencia de `library_test.go` (fakes), ejercita el camino completo
**Handler → Service → Repository → DB** — cubre lo que los fakes no
pueden:

| Test | Qué pina (con DB real) |
|---|---|
| `CreateThenGet_Persists` | round-trip de persistencia + `item_count` |
| `Get_NotFound_404` | 404 desde el repo real (sentinel) |
| `List_AdminSeesAll_UserScopedByAccess` | ACL `ListForUser` (INNER JOIN `library_access` + `GrantAccess`) |
| `Update_Persists` / `Delete_RemovesFromDB` | mutaciones reflejadas en la DB |
| `Items_KeysetPagination` | paginación keyset (cursor, `next_cursor`, total) |
| `Items_ContentRatingCap` | cap por content-rating materializado en SQL (`IN (...)`, NULL excluido) |
| `LatestItems_NewestFirst` | orden `added_at DESC` |
| `Genres_CountsFromItemValues` | vocabulario desde `item_value_map` |

Detalles: items sembrados vía `repos.Items.Create` (top-level,
`IsAvailable: true`); libs creadas en `ScanMode: "manual"` para no
disparar auto-scan; `svc.Shutdown` en `t.Cleanup` (LIFO antes del
teardown de DB). El cap usa un usuario real con `max_content_rating`
fijado vía `testutil.Exec`. Dual-backend listo (sqlite + postgres en CI).

**Verificado:** paquete verde con `-race` (19.6s), `go vet` limpio,
gofmt limpio. 0 cambios de producción — sólo tests.

---

## 🌐 Sesión 2026-06-08 — TT-8 root + limpieza docs

Rama: `claude/project-review-EOwig`.

### TT-8 — comentarios en inglés del root de `handlers/`

Traducidos a español (convención del proyecto: comentarios técnicos,
concisos, en español) **los 9 ficheros compartidos del paquete raíz**
`internal/api/handlers/` — exactamente los targets nombrados en el audit:

| Fichero | Qué |
|---|---|
| `interfaces.go` | 30+ doc comments de interfaces de servicio/repo (AuthService, StreamManagerService, IPTVTransmuxer, todas las `*Repository`…). gofmt reordenó imports de paso. |
| `responses.go` | `RequireParam`, `SetErrorRecorder`, `ParsePagination`, `RespondAppError`, `RespondError`, `HandleServiceError` + caso default. |
| `contracts.go` | `PermissionsStore`, `CorsOriginStore`, `AuditLogStore`, `UpdatesProvider`. |
| `item_helpers.go` | `AttachPosterPlaceholder`, `UserDataResponse`, `ItemSummaryResponse` + nota `sort_title`. |
| `sse_limiter.go` | defaults, `ErrSSE*`, `SSELimiter`, `Acquire`, `Snapshot`. |
| `streaming_deadline.go` | bloque doc inicial de `DisableWriteDeadline`. |
| `cache_control.go`, `client_ip.go`, `iprate_middleware.go` | ya estaban en español (sin cambios). |

**Sólo comentarios** — 0 cambios de lógica. `go build ./internal/api/...`
verde, gofmt limpio.

**Pendiente (incremental):** los sub-paquetes (`admin`, `auth`,
`federation`, `iptv`, `me`, `media`, `system`) aún tienen ~muchos
comentarios en inglés. El audit clasifica esto como "hacer
incrementalmente al tocar cada fichero" — no un big-bang de miles de
líneas. Registrado como item 2 en pendientes.

### Limpieza de docs

La tabla "📋 Pendientes priorizadas" listaba OO/MM/RR + el merge de PR
#477 como abiertos, pero el propio header del doc ya los marcaba
cerrados (sesiones 2026-05-28). Tabla reescrita: sólo F15-5, TT-8 resto,
F15-10/11/12 y distribución avanzada siguen abiertos.

---

## 🔭 Estado actual (2026-05-30)

**Salud del proyecto**: MVP funcional, cerca de early-production.

| Área | Estado |
|---|---|
| **Audit olores altos 2026-05-14** | ✅ **6/6 cerrados** |
| **Audit olores macro 2026-05-27** | NN ✅, PP ✅, QQ ✅, SS-3/4/5 ✅, **OO ✅, RR ✅, MM ✅**. Todos cerrados. |
| **Audit per-package satélite SS/TT** | **SS-1 ✅, SS-6 ✅, SS-2 ✅, TT-5 ✅, TT-7 ✅** (sesión 2026-05-30). **TT-8 root compartido ✅** (2026-06-08); sub-paquetes pendientes incrementalmente. |
| **Tests backend** | todos los paquetes verdes (`go test ./...` exit 0, con -race en los tocados) |
| **Tests frontend** | **717/717** vitest verdes |
| **PRs abiertas** | ninguna |

---

## 🧹 Sesión 2026-05-30 — TT-5 + TT-7 + SS-2 + auditoría fresca

Rama: `claude/project-review-mnJ9a`. 2 commits, suite backend verde
(`go test ./...` exit 0). Cerrados los 3 olores satélite accionables que
quedaban del audit per-package; sólo queda **TT-8** (cosmético).

### TT-5 — `apperror.recorder` lock-free

`var recorder func(code string)` plano → `atomic.Pointer[func(...)]`.
Antes un `t.Parallel` que llamara `SetRecorder` podía pisar el global a
mitad de un `Write` (data race latente — sólo no se manifestaba porque
un único test lo seteaba). Ahora set/read es atómico. `init()` instala
el no-op. ~10 LoC en `internal/api/apperror/apperror.go`.

### TT-7 — `NewItemHandler` con struct de deps

15 parámetros posicionales → `media.ItemHandlerDeps` con campos
nombrados (precedente: `stream.Deps`, `iptv.Deps`). Dos interfaces del
mismo tipo subyacente reordenadas ya no compilan a un handler roto.
Callsite de producción (`mount_media.go`) + 4 de test migrados.

### SS-2 — `upload`/`audit` dejan de importar `db`

`AuditStore.Insert(db.UploadAuditRow)` y `Store.Insert(db.AuditLogRow)`
forzaban importar `internal/db` sólo para construir el parámetro (viola
DIP). Ahora cada paquete define su struct espejo (`upload.AuditRow`,
`audit.LogRow`) y **no importa db en absoluto**. La conversión a tipos
`db.*` vive en 2 adapters en el composition root
(`cmd/hubplay/audit_adapters.go`). `audit.LogRow` lleva sólo los 9
campos que el INSERT persiste (actor/target username se resuelven en
read vía join). Patrón: **adapter en la frontera** (más ligero que crear
un model package nuevo para 2 tipos diminutos; consistente con la
dirección Opción B).

### Auditoría fresca del backend (sin hallazgos accionables nuevos)

Sweep exhaustivo de `internal/`+`cmd/` buscando bugs/seguridad/recursos
fuera de lo ya catalogado. **Conclusión: el código está limpio.** El
único hallazgo "alto" reportado (supuesto lock-order inversion /
deadlock en `stream/manager.go:543-617` `RestartSessionAt`) se verificó
y es **falso positivo**: la línea 546 hace `m.mu.Unlock()` ANTES de
tomar `ms.restartMu` (551); el único anidamiento es `restartMu → m.mu`
(612-617) y ningún camino mantiene `m.mu` esperando `restartMu`
(`restartMu` sólo se usa en esa función). Sin ciclo de locks, sin
deadlock. Resto de hallazgos del barrido eran no-problemas (patrones Go
aceptados: `//nolint:errcheck` en `resp.Body.Close()`, validación de
itemID mitigada por lookup en DB).

---

## 🏛 Sesión 2026-05-28 (continuación) — OO + RR cerrados

Rama: `claude/project-review-97WBR`.

### OO — Split del mega-paquete `handlers/` en 10 sub-paquetes

`internal/api/handlers/` tenía 60+ ficheros en un solo paquete con 15 dominios mezclados. Split por dominio:

| Sub-paquete | Package name | Ficheros | Dominio |
|---|---|---|---|
| `handlers/` (root) | `handlers` | 7 | Shared: responses, cache, SSE, contracts |
| `handlers/system/` | `system` | 14 | health, system, setup, openapi, cors, updates |
| `handlers/auth/` | `authhandler` | 5 | auth, auth_device |
| `handlers/admin/` | `admin` | 13 | admin_auth, admin_db, settings, audit_log, etc. |
| `handlers/users/` | `users` | 5 | users, permissions |
| `handlers/media/` | `media` | 22 | image, stream, items facade, people, etc. |
| `handlers/me/` | `me` | 13 | home, events, peers, preferences, progress |
| `handlers/federation/` | `fedhandler` | 9 | federation admin/public/stream/image |
| `handlers/iptv/` | `iptvhandler` | 20 | IPTV facade + 13 sub-handlers |
| `handlers/library/` | `libhandler` | 3 | library handler |
| `handlers/uploads/` | `uploads` | 3 | uploads, upload_browse |

**Decisiones clave:**
- Helpers de respuesta exportados (`RespondData`, `HandleServiceError`, etc.)
- `AuditEmitter` + `NoopAudit` movidos a `handlers/contracts.go` (compartido)
- Cada sub-package define micro-interfaces locales (Go idiom)
- `FederationImageHandler` usa interface `imageServer` (no concrete type)
- Packages con conflicto de nombre con `internal/auth`, `internal/iptv`, etc. usan sufijo `handler` (`authhandler`, `iptvhandler`, etc.)

**Métricas:** 126 ficheros tocados, +3187 / -2293 LoC. 0 regresiones de API HTTP.

### MM — Split de `api.Dependencies` en 11 sub-structs por dominio

`api.Dependencies` tenía 71 campos planos que cualquier mount file veía
enteros. Split en 11 sub-structs por dominio en `internal/api/deps.go`:

```go
type Dependencies struct {
    Infra      InfraDeps      // Logger, Metrics, EventBus, Audit, SSELimiter, ...
    Server     ServerDeps     // Config, AuthConfig, CORS, TrustedProxies, ...
    Auth       AuthDeps       // Auth, DeviceCode, Users, UserRepo, Permissions
    Catalog    CatalogDeps    // Libraries, Items, Images, Metadata, ...
    Streaming  StreamingDeps  // StreamManager
    IPTV       IPTVDeps       // Service, Proxy, Transmux, LogoCache, ...
    Federation FederationDeps // Manager
    Providers  ProvidersDeps  // Manager, Repo
    Admin      AdminDeps      // DB, Activity, Settings, Updates
    Setup      SetupDeps      // Service
    Uploads    UploadsDeps    // Handler, Audit
}
```

Cambios:
- `internal/api/deps.go` (176 LoC, nuevo) — define los 11 sub-structs
- `internal/api/router.go` 581→362 LoC (-38%) — accesos via `deps.Group.Field`
- `cmd/hubplay/main.go` — composition root construye sub-structs explícitos
- 7 mount files actualizados a accesos anidados
- Tests de integración actualizados

Cada `mountXxx` ya solo "ve" los campos de los grupos que necesita
(via `deps.Group.X`). No hay forma accidental de tocar otros dominios.

### RR — Eliminado `deps_repos.go`

Las 19 interfaces "broad" de repos que vivían en `handlers/deps_repos.go` (216 LoC) eran **sólo** usadas por `api.Dependencies` (verificado con grep). Movidas a `internal/api/repos.go` en el paquete `api`. Ahora:

- Sub-packages handler usan sus propias micro-interfaces (cerrado por OO+NN)
- `Dependencies` referencia tipos locales del paquete `api` (sin prefijo `handlers.`)
- `handlers/deps_repos.go` eliminado

**Beneficio:** los tipos de wiring viven donde el wiring vive. No hay re-exportación de tipos cross-paquete.

---

## 🏛 Sesión 2026-05-28 — Audit arquitectónico profundo + implementación

Rama: `claude/go-backend-arch-review-UDzqB`. PR: [#477](https://github.com/Alexzafra13/HubPlay_demo/pull/477).
21 commits, 28 paquetes verdes, 0 regresiones.

### Olores cerrados en esta sesión

| Olor | Antes | Después |
|------|-------|---------|
| **NN** (interfaces gigantes) | `LibraryService` 25m, `IPTVService` ~50m en interfaces.go | 20+ micro-interfaces locales (1-12m c/u). `interfaces.go` 449→279 LoC (-38%) |
| **TT-6** (IPTVHandler god-handler) | 1 struct, 10 deps, 12 ficheros | Facade puro, 9 sub-handlers embebidos |
| **SS-3** (defer fuera de lifecycle) | 4 defer tras database.Close() | Migrados a lc.AddWorker |
| **SS-4** (os.Exit en run) | os.Exit(1) sin defers | return fmt.Errorf |
| **SS-5** (browse sin timeout) | BrowseAllPeerLibraries se cuelga si un peer no responde | 10s per-peer timeout |
| **QQ** (main.run monolítico) | 780 LoC | 581 LoC (-25%). 5 builders: foundation, database, streaming, uploads, federation |
| **PP** (tipos db fugan) | 35 db imports en handlers, tipos sql.Null* cruzando capas | 15 imports (-57%). 14 tipos → library/model + provider/model con tipos Go puros |

### Ficheros nuevos creados

- `cmd/hubplay/build_foundation.go` — config + logger + clock + PATH
- `cmd/hubplay/build_database.go` — open + migrate + repos
- `cmd/hubplay/build_streaming.go` — applyStreamingOverrides
- `cmd/hubplay/build_uploads.go` — tusd + GC
- `cmd/hubplay/build_federation.go` — identity + manager + lifecycle hooks
- `internal/library/model/home.go` — HomeTrendingItem, HomeRecommendation, HomeBecauseSeed, HomeBecauseResult, HomeLiveNowChannel
- `internal/library/model/userdata.go` — UserData, ContinueWatchingItem, FavoriteItem, NextUpItem, UserPreference
- `internal/library/model/admin.go` — DailyWatchBucket, TopItemRow, LibrarySizeRow
- `internal/provider/model/types.go` — ProviderConfig
- `docs/memory/audit-2026-05-27-per-package-review.md` — análisis completo

### Documentación del análisis

[`audit-2026-05-27-per-package-review.md`](audit-2026-05-27-per-package-review.md):
- Estructura de 29 paquetes, grafo de dependencias verificado
- Flujo de bootstrap (7 fases), shutdown (3 fases con diagrama)
- Concurrencia: stream.Manager, iptv.TransmuxManager, federation.Manager
- Revisión profunda paquete 1 (internal/api + handlers): 8 hallazgos TT-1..TT-8
- 6 hallazgos transversales SS-1..SS-6
- Plan de ataque en 6 pasos con tabla de prioridades

---

## 🏛 Sesión 2026-05-27 (parte 5) — Audit arquitectónico macro

Documento nuevo: [`audit-2026-05-27-architecture-macro.md`](audit-2026-05-27-architecture-macro.md). 0 PRs (sólo análisis). Sin código.

### Por qué un audit nuevo

El audit [`2026-05-14`](audit-2026-05-14-go-backend-review.md) está casi al
100% cerrado (6/6 olores altos + medium + F16 + bajas). Lo que queda son
**olores estructurales que emergieron por agregación** después de cerrar lo
táctico: god-structs de wiring, interfaces de servicio gigantes, fuga de
tipos de persistencia, y división del mega-paquete handlers. Son problemas
de **shape**, no de bugs — el código funciona, pero su API es ancha y
sufre presión a la baja con cada feature.

### Olores nuevos catalogados (SS/TT no usados aún — siguen serie MM..RR)

| Olor | Severidad | Tema | Bloquea |
|---|---|---|---|
| **MM** | 🔴 Alta | `api.Dependencies` con 77 campos (god-struct de wiring) | — |
| **NN** | 🔴 Alta | `handlers/interfaces.go` con interfaces de servicio gigantes (LibraryService 25 métodos, IPTVService 50+) | F15-5, OO |
| **OO** | 🟡 Media | `internal/api/handlers/` mega-paquete con 60+ ficheros y 15 dominios | — |
| **PP** | 🟡 Media | Tipos `db.X` fugan a handlers (35 imports) — db.UserData, db.HomeTrendingItem, etc. | — |
| **QQ** | 🟡 Media | `main.run()` con ~630 LoC de wiring imperativo | — |
| **RR** | 🟢 Baja | Duplicación `interfaces.go` ↔ `deps_repos.go` (se resuelve cerrando NN) | — |

### Cola de revisión por paquete (8 ítems)

Orden optimizado por valor/coste. Cada paquete tendrá su propia sub-sección
en §8 del audit cuando se revise.

| # | Paquete | Foco principal |
|---|---|---|
| 1 | `internal/api` + `internal/api/handlers` | Romper Dependencies (MM) + eliminar interfaces.go gigante (NN) + sub-packages por dominio (OO) |
| 2 | `internal/iptv` | service.go/proxy.go/transmux.go — ¿sub-domain real? IPTVService 50+ métodos |
| 3 | `internal/stream` | Manager 823 LoC, concurrencia, lifecycle de sesiones |
| 4 | `internal/library` + `internal/scanner` | Cross-wiring scanner/service/watcher, jobs |
| 5 | `internal/db` | Sacar tipos de dominio (PP), shape repos vs sqlc |
| 6 | `internal/auth` | JWT, keystore, ratelimit, AuthService 16 métodos |
| 7 | `internal/federation` | Manager, peer protocol — aislado |
| 8 | `internal/{provider,upload,event,observability}` | Iteraciones cortas |

### Decisiones pendientes (antes de tocar paquete 1)

- **Q1**: orden NN→OO→MM o MM→NN→OO. Recomendación: **NN primero** (más bloqueante).
- **Q2**: PP inline durante NN/OO o sesión propia. Recomendación: **sesión propia** (move mecánico, oscurece el diff).
- **Q3**: QQ timing. Recomendación: **después** de NN+OO+PP (QQ es síntoma).

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

> **Nota (2026-06-08):** los items OO/MM/RR de esta tabla estaban
> **obsoletos** — se cerraron en las sesiones 2026-05-28 (ver §"OO + RR
> cerrados" y §"split Dependencies"). El merge de PR #477 también está
> resuelto (la rama está mergeada). Tabla reescrita para reflejar sólo lo
> realmente abierto.

| # | Tarea | Coste | Severidad |
|---|---|---|---|
| ~~1~~ | ~~**F15-5** — Integration tests con DB real para handlers de library.~~ ✅ **cerrado** (2026-06-08 parte 2) | — | — |
| **2** | **TT-8 (resto)** — traducir comentarios en inglés en los **sub-paquetes** de `handlers/` (admin, auth, federation, iptv, me, media, system). El root compartido ya está 100% en español (sesión 2026-06-08). Hacer incrementalmente al tocar cada fichero. | Bajo, incremental | Baja (cosmético) |
| **3** | **F15-10/11/12** — Polish: fakes compartidos, naming, concurrency tests. | Baja | Baja |
| **4** | **Distribución avanzada** — auto-update, TLS LAN, macOS notarized, AppImage. | Sesión grande | Producto |

**Olores del audit 2026-05-27**: NN ✅ PP ✅ QQ ✅ OO ✅ MM ✅ RR ✅ cerrados. Del satélite SS/TT: SS-1/2/3/4/5/6 ✅, TT-5 ✅ TT-6 ✅ TT-7 ✅. Queda **TT-8** (comentarios en inglés en `handlers/`): **root compartido cerrado** (2026-06-08), sub-paquetes pendientes incrementalmente.

---

## 🏛 Referencias (vivos, mantenidos)

- [`architecture-decisions.md`](architecture-decisions.md) — ADRs (AppError, observability, keystore, sink pattern, preflight, sqlc adapter, ADR-026 logs).
- [`conventions.md`](conventions.md) — patrones del codebase, reglas de test, anti-ciclo, comentarios en español, regeneración sqlc.
- [`audit-2026-05-27-architecture-macro.md`](audit-2026-05-27-architecture-macro.md) — Olores estructurales. NN ✅ PP ✅ QQ ✅ cerrados. Quedan OO, MM, RR (organización física).
- [`audit-2026-05-27-per-package-review.md`](audit-2026-05-27-per-package-review.md) — **Análisis profundo sesión 2026-05-28**. Estructura, dependencias, flujo, revisión paquete 1 (handlers). Hallazgos TT-1..TT-8 + SS-1..SS-6. Estado de cierre con tablas de micro-interfaces.
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

### Olores medios (todos cerrados ✅)

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
| F15-5 | ✅ Integration tests con DB real para LibraryHandler (2026-06-08 parte 2) |
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
