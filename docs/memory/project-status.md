# Estado del proyecto

> Snapshot: **2026-04-24** · Rama: `claude/review-tv-programming-bWTim` · **tests: verde**

## Resumen ejecutivo

MVP funcional con backend maduro y frontend refactorizado. Sqlc completa, 15/15
handlers backend con tests, IPTV hardened (SSRF, timeouts, ACL por biblioteca,
dedup M3U), LiveTV partido en 8 ficheros con **EPG grid real** estilo Plex.
Último commit arregla el ciclo de vida de goroutines en `library.Service`
(flake de CI + hazard de producción al apagar mid-scan).

## Tamaño verificado

- **~100** ficheros `.go` de producción · **~60** `_test.go`
- **74** rutas HTTP (ver `internal/api/router.go`)
- **~20** test files en frontend (añadidos `livetv/` components)
- **Cero** `TODO`/`FIXME`/`HACK`
- `go test -race ./...` verde en 21 paquetes; `golangci-lint v1.64.8`: exit 0
- Frontend: `pnpm build` + `pnpm test` (72/72) verdes

## Último ciclo de trabajo (hoy, 2026-04-24)

1. **Fix 414 en EPG bulk schedule** — el usuario importó 7008 programas
   desde davidmuma/EPG_dobleM y todas las tarjetas mostraban "sin guía".
   Causa: `useBulkSchedule` serializa TODOS los channel IDs en el query
   string; una biblioteca con ~260 canales produce una URL de ~9 KB que
   nginx rechaza con 414 antes de llegar a Go. Los programas ya estaban
   en la DB — el fallo era puro transporte.
2. **POST /api/v1/channels/schedule** — el handler `BulkSchedule` acepta
   ahora GET (compat) y POST con body JSON `{channels, from, to}`. Body
   limitado a 1 MiB con `MaxBytesReader`, `DisallowUnknownFields` para
   rechazar payloads malformados, tope de 5000 canales por request.
3. **Chunker en `EPGProgramRepository.BulkSchedule`** — el IN() dinámico
   se parte en bloques de 500 ids antes de pegar a SQLite (default
   `SQLITE_LIMIT_VARIABLE_NUMBER=999`). Dedupe previo para que un id
   repetido no duplique filas al mergear chunks. Tests con 1200 canales
   verifican que ningún row se pierde en los bordes.
4. **Frontend `getBulkSchedule` a POST** — siempre. Más simple y robusto
   que una heurística por tamaño. Tests añadidos al `client.test.ts`
   validan la shape del request y el short-circuit con lista vacía.
5. **nginx `large_client_header_buffers 8 32k`** — defensa en
   profundidad. El POST ya evita el 414, pero subir el buffer previene
   regresiones si alguien añade un endpoint GET con listas grandes en
   el futuro.
6. **Fix bug en `nameVariants`** (pre-existente en 7d95d1e): nombres
   con whitespace doble ("  Canal  Sur  ") generaban una variante
   espuria "canal  sur" además de la colapsada "canal sur", que nunca
   emparejaba nada real. Ahora el folding normaliza whitespace en la
   variante base también.
7. **Fix runtime 500 en `BulkSchedule`** (descubierto por el usuario
   contra un container real): modernc.org/sqlite serializa `time.Time`
   con Location nombrada usando `time.Time.String()` — por ejemplo
   `"2026-04-24 12:00:00 +0200 +0200"`. El Scan por defecto del driver
   no es capaz de deserializar ese formato. Afecta a todos los feeds
   XMLTV que traen offset (davidmuma, iptv-org, epg.pw). Fix en dos
   capas: (a) `ReplaceForChannel` normaliza a UTC antes de insertar
   vía sqlc, y (b) `BulkSchedule` / `Schedule` / `NowPlaying` leen con
   SQL crudo + `coerceSQLiteTime`, helper que acepta tanto
   `time.Time` como strings (`RFC3339` y el legado Go-stringer), de
   modo que bases de datos pre-fix siguen leyéndose. Tests cubren el
   roundtrip XMLTV exacto + legacy row sembrada con INSERT crudo.
8. **Multi-fuente EPG + catálogo curado**: una biblioteca livetv puede
   tener N proveedores XMLTV en orden de prioridad; el refresher los
   recorre y mergea "primera fuente gana por canal" (davidmuma lleva
   las grandes, epg.pw rellena los 216 canales huérfanos). Si una
   fuente 404ea, el refresh sigue con las demás — cada fuente persiste
   `last_status`, `last_error`, `last_program_count`, `last_channel_count`
   para que la UI del admin muestre badges "✓ 3200 programas" / "✗ 404".
   Nueva migración `007_library_epg_sources.sql` con FK + UNIQUE(library,
   url); migra el antiguo `libraries.epg_url` a una fila priority-0.
   Nuevo `internal/iptv/epg_catalog.go` con 10 fuentes curadas
   (davidmuma x4, epg.pw x6 multi-idioma). Endpoints nuevos:
   `GET /iptv/epg-catalog`, `GET /libraries/{id}/epg-sources` (viewer
   con ACL), `POST` / `DELETE` / `PATCH .../reorder` admin-only. Panel
   nuevo `EPGSourcesPanel` en LibrariesAdmin con dropdown del catálogo,
   input de URL custom y reorder vía botones ↑/↓ accesibles por teclado.
   Cubierto por tests (repo + service + handler + catalog lookup).
9. **Health-check oportunista de canales** (estilo Plex): el proxy
   IPTV graba cada intento de conectar al master playlist contra la
   fila del canal. Éxito → resetea contador; fallo → incremento
   atómico. Cancelaciones del cliente (usuario cierra pestaña) se
   filtran explícitamente y NO cuentan — la DB solo refleja fallos
   reales de upstream. Nueva migración `008_channel_health.sql` añade
   columnas `last_probe_at`, `last_probe_status`, `last_probe_error`,
   `consecutive_failures` a `channels` más un índice parcial sobre
   `(library_id, consecutive_failures) WHERE consecutive_failures > 0`.
   El handler de canales del usuario llama a `ListHealthyByLibrary`
   que excluye canales con ≥ `UnhealthyThreshold` (=3) fallos
   consecutivos; el cliente normal no ve canales rotos. Endpoints
   admin nuevos: `GET /libraries/{id}/channels/unhealthy[?threshold=N]`,
   `POST /channels/{id}/reset-health`, `POST /channels/{id}/disable`,
   `POST /channels/{id}/enable`. Interfaz `iptv.ChannelHealthReporter`
   (nil-safe) desacopla proxy↔DB; implementada en `iptv.Service` con
   timeout 2s para que la escritura DB no bloquee el hot path.
   Componente `UnhealthyChannelsPanel` en LibrariesAdmin muestra el
   panel solo si hay problemas (cero ruido si la biblioteca está
   sana); poll 30s; acciones "Marcar OK" (reset counter) y
   "Desactivar" (flip is_active). Tests: repo (atomic +1 concurrente,
   trim error largo, filtro threshold, reset, ListHealthy esconde
   desactivados+unhealthy), proxy (nil reporter, éxito, fallo,
   cancelación del cliente ignorada, DeadlineExceeded cuenta),
   handler (threshold custom, ACL deny, cada acción).

**Impacto operativo**: importar davidmuma ya funciona end-to-end.
Poner la URL XMLTV (o .gz) en el campo *EPG URL* de la biblioteca
livetv y darle a "Refrescar EPG". El matcher fuzzy (tvg-id → display-
name con quality strip + accent fold) junta los programas con los
canales correctos. El frontend carga la guía sin 414 hasta ~5000
canales por request.

## Ciclo anterior (2026-04-17)

1. **Auditoría IPTV** — detectados 5 problemas en backend y desajuste UX en frontend
2. **Tier 1 seguridad backend** (`85263d8`): SSRF proxy, timeouts transport,
   candado EPG refresh, relay cleanup, M3U dedup + BOM — 14 tests de regresión
3. **ACL por biblioteca** (`f22ce73`): 8 endpoints de canal/EPG ahora consultan
   `library_access`. 404 (no 403) para no filtrar existencia. Admin bypasa —
   10 tests ACL que verifican que el proxy NO se invoca en deny
4. **LiveTV refactor + EPG grid real** (`a8372fa`):
   - 828 líneas → ~420 + 8 módulos bajo `web/src/components/livetv/`
   - EPGGrid nuevo: filas de canales × columnas de tiempo con línea "ahora"
     auto-scroll, role=grid + a11y
   - Toggle Carrusel / Guía
   - `useLiveHls` hook nuevo (reemplaza HLS inline duplicado)
   - Zapping por teclado (↑↓ + números + Enter) con indicador visual
   - Logos con fallback a número en error
   - Canales inactivos filtrados
   - `aria-live` hero, `role=progressbar`, `<label>` en inputs
   - Bug fix: `useBulkSchedule` query key truncaba a 10 IDs
5. **Fix CI lint** (`a7fb54c`): eliminado código muerto de sqlc migration
   (`nullStr`, `applyFull`, `fullScanDests`, campos huérfanos)
6. **Fix CI flake + production hazard** (`3e90af9`): `library.Service` ahora
   tiene `Shutdown()` con `bgCtx` + `WaitGroup`. Auto-scan goroutines ya no
   corren con `context.Background()` detached.

## Arquitectura actualizada

### Patrón de servicios con goroutines de fondo

Servicios con Shutdown(): `stream.Manager`, `iptv.Service`, `auth.Service`,
`library.Service`. Todos siguen el mismo patrón:

```go
type Service struct {
    bgCtx    context.Context
    bgCancel context.CancelFunc
    bgWG     sync.WaitGroup
}
// goroutines: inheritan bgCtx, registran en bgWG, Shutdown cancela+espera
```

`main.go` los llama en orden de dependencia antes de cerrar DB. Tests usan
`t.Cleanup(svc.Shutdown)` para LIFO: Shutdown → DB close → TempDir rm.

## Bulk EPG — contrato del endpoint

`GET /api/v1/channels/schedule?channels=a,b,c&from=…&to=…` y
`POST /api/v1/channels/schedule` con body
```json
{ "channels": ["a","b","c"], "from": "-2", "to": "24" }
```

- `from`/`to` aceptan RFC3339 o entero (horas). Default: -2h..+24h.
- ACL por canal: cada id se valida individualmente contra
  `library_access`; canales no accesibles se ignoran silenciosamente.
  Admin bypasa.
- Dedup + chunk internos: el repo corta en bloques de 500 ids antes del
  IN() de SQLite. Tope de 5000 canales por request (400 si se excede).
- Frontend siempre POST — el GET queda para curl / back-compat.

## Lo que falta (no bloqueante)

### Frontend Live TV
- Favoritos / último canal visto
- Modal de detalles de programa al clicar en el EPG grid (requiere feature
  de grabación/recordatorio para ser útil)
- Virtualización de filas del EPG (flat DOM aguanta ~200 canales bien)

### IPTV backend
- XMLTV streaming parser (bomba memoria con feeds de 2GB; `xmltv.go:70-76`)
- Warning al importar EPG sin timezone offset (asumido UTC silenciosamente)
- Counter de huérfanos EPG (programas sin canal matching se descartan sin log)
- Healthcheck de iptv-org URLs

### Event bus (3 tipos reservados sin publisher)
- `ChannelAdded`/`ChannelRemoved` — requiere descomponer `ReplaceForLibrary`
- `MetadataUpdated` — necesita hook en scanner o flujo de refresh dedicado

### Frontend general
- Tests del setup wizard (0 cobertura, ruta crítica de primer arranque)
- Tests de páginas admin (mutan estado, 0 tests)
- Wrappers en `ApiClient` para rutas sin cubrir: admin keys, provider search,
  IPTV refresh
- i18n español: keys añadidos, falta traducir strings pendientes

## Próximo paso sugerido

Sin prisa. Candidatos ordenados por impacto:

1. **Tests del setup wizard** — ruta crítica de primer arranque, 0 cobertura.
   El patrón ya está probado en los tests de handler backend.
2. **Streaming parser del XMLTV** — único problema IPTV pendiente con impacto
   real en producción (feeds de 2GB petan servidores pequeños).
3. **Favoritos / último canal en LiveTV** — reusar el patrón `continue_watching`
   de películas.
4. **Wrappers ApiClient faltantes** — necesario antes de añadir admin UI
   para gestión de claves JWT.
