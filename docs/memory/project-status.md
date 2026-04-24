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
