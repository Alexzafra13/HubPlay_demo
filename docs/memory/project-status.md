# Estado del proyecto

> Snapshot: **2026-04-17** · Rama: `main` · HEAD: `f07e031`

## Resumen ejecutivo

MVP funcional. Base backend estable: migración a sqlc completa, handlers principales
con cobertura de tests, event bus con unsubscribe, imaging endurecido (MIME sniffing,
SSRF, decompression-bomb guard, path traversal). Gap actual: frontend sin tests de
páginas ni wizard; varios handlers backend aún sin tests.

## Tamaño verificado

- **97** ficheros `.go` de producción · **53** `_test.go` (~55%)
- **74** rutas HTTP (ver `internal/api/router.go`)
- **12** test files en frontend (api client + algunos components + 3 hooks + 2 stores)
- **Cero** `TODO`/`FIXME`/`HACK` en todo `internal/` o `web/src/`
- `go test -race ./...` verde en 21 paquetes

## Lo que está hecho

### Backend
- sqlc como única capa de queries (83 de 88 métodos; 5 raw SQL documentados:
  dynamic IN(), FTS5+cursor, CTE con params duplicados)
- `domain.AppError` con `.Kind` sentinel, `handleServiceError` mapea sentinel → HTTP
- JWT keystore con rotación por `kid` + periodo de overlap
- Observabilidad con registry propio por test (no DefaultRegisterer)
- Preflight de arranque (ffmpeg, permisos, cache dir)
- Event bus con panic-recovery, timeout de handler, y `Subscribe` que devuelve unsub
- `internal/imaging/`:
  - `validators.go` — IsValidKind, IsValidContentType, ExtensionForContentType, MaxUploadBytes
  - `blurhash.go` — ComputeBlurhash(bytes, logger)
  - `safety.go` — SniffContentType (body-based), EnforceMaxPixels (40 MP), SafeGet (SSRF-safe)
  - `pathmap/pathmap.go` — imageID→path store con UUID validation y errores retornados
- `internal/library/imagerefresh.go` — refresh batch de imágenes por library, extraído del handler HTTP
- Migración 005 droppea tabla `api_keys` no usada

### Handlers con tests de caracterización (5 de 15)
- `admin_auth_test.go` (5 tests)
- `auth_test.go` (6 tests)
- `responses_test.go` (9 tests)
- `image_test.go` (25 tests, incl. regresión de security — MIME spoof, bomb, traversal, SSRF)
- `stream_test.go` (25 tests)
- `progress_test.go` (20 tests)
- `iptv_test.go` (23 tests)
- `library_test.go` (21 tests)

### Frontend
- 19 páginas, todas funcionales (Home, ItemDetail, LiveTV, Login, Movies, Series,
  Search, Settings, NotFound, 4 pasos del wizard, 4 páginas admin)
- i18n con `en` + `es` wired vía `i18next-browser-languagedetector`
- Deps todas estables (React 19.2, Vite 7.3, TanStack Query 5.90, Zustand 5.0, TS 5.9)

## Lo que falta

### Tests
- **Handlers backend sin tests**: `items.go` (247 L), `users.go` (103 L), `setup.go` (232 L),
  `providers.go` (285 L), `events.go` (105 L), `health.go` (63 L). Patrón de fakes + httptest
  ya probado en image/stream/progress/iptv/library_test.go.
- **Frontend sin tests**: páginas, admin, setup wizard, ImageManager. Páginas admin mutan
  estado — alta prioridad. Wizard es ruta crítica de primer arranque — alta prioridad.
- Servicios con tests flojos: `scanner` (621 L / 1 test), `library/service.go` (369 L / 1 test).

### Features (event types reservados pero sin publisher)
- `TranscodeStarted`, `TranscodeCompleted` en `internal/stream/`
- `ChannelAdded`, `ChannelRemoved`, `EPGUpdated`, `PlaylistRefreshed` en `internal/iptv/`
- `MetadataUpdated` en `internal/library/` (tras refresh de metadata)
- `UserLoggedIn`, `UserLoggedOut` en `internal/auth/`

El SSE handler ya los escucha — sólo falta publicar.

### API client frontend (endpoints backend sin wrapper)
- `/admin/auth/keys` (list, rotate, prune) — admin UI no consume signing-key management
- `/providers/search/metadata`, `/providers/metadata/{id}`, `/providers/images`, `/providers/search/subtitles`
- `/libraries/{id}/iptv/refresh-m3u`, `.../refresh-epg`
- `/events` (SSE) — no hay wrapper; se consume ad-hoc o no se consume

## Known issues

- chi v5 devuelve 404/405 sin pasar por middleware → métricas Prometheus pierden
  esas respuestas. Documentado, no resuelto.
- Tests de `auth/domain/iptv/config/stream` fallan en Windows local por App Control
  Policy bloqueando binarios de test. CI/Docker pasan. Solución: correr en Docker o WSL.

## Próximo paso sugerido

Tier siguiente tras tiers A+B (mergeados hoy):
1. **Tests frontend del setup wizard** — ruta crítica de primer arranque, cero cobertura
2. **Tests frontend de páginas admin** — mutan estado, riesgo alto
3. **Wire publishers de event types reservados** — SSE ya los escucha, sólo falta emitir

Orden: (2) da más cobertura con menos fakes que (1); (3) es feature-sized, en sesión aparte.
