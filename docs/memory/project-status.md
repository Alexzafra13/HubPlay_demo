# Estado del proyecto

> Snapshot: **2026-04-24** (live-TV arc + simplify sweep) · Rama: `claude/review-memory-tv-code-8bjei` · **tests: verde**

## Resumen ejecutivo

MVP funcional con backend maduro y frontend refactorizado. Sqlc completa, 15/15
handlers backend con tests, IPTV hardened (SSRF, timeouts, ACL por biblioteca,
dedup M3U), LiveTV partido en 10+ ficheros con **EPG grid real** estilo Plex.

Arco de trabajo de esta rama — la parte de LiveTV de "funciona" a "producto":
importador davidmuma robusto (fix 414 + UTC scan), EPG **multi-fuente con
catálogo curado** (prioridad + merge + per-source status), **health-check
oportunista** de canales (apagados auto-ocultos del user, surface para admin),
**edición manual de tvg-id** con overrides persistentes entre M3U refreshes,
**admin polish** (panel unificado con status dot + stats strip + tabs), y
**Discover rediseñado** (tarjetas limpias, rail Apagados, hero personalizable
con auto-preview HLS + persistencia multi-dispositivo). 14 commits, todos con
tests + lint + go race verde.

Ciclo de cierre (`claude/review-memory-tv-code-8bjei`, 1 commit + 1 follow-up):
revisión de código contra la memoria del arc, **−400 loc netas** con cero
cambios de comportamiento y **dos bugs arreglados**: el fallback del logo
roto de `ChannelCard` ya muestra las iniciales (antes dejaba el hueco), y la
barra de progreso del `HeroSpotlight` se actualiza también con un solo item
(antes quedaba congelada hasta que cambiaba `nowPlaying`). Extraídos tres
hooks / módulos reutilizables: `useNowTick`, `useHeroSpotlight`,
`livetv/categoryOrder`.

## Tamaño verificado

- **~100** ficheros `.go` de producción · **~60** `_test.go`
- **74** rutas HTTP (ver `internal/api/router.go`)
- **~20** test files en frontend (añadidos `livetv/` components)
- **Cero** `TODO`/`FIXME`/`HACK`
- `go test -race ./...` verde en 21 paquetes; `golangci-lint v1.64.8`: exit 0
- Frontend: `pnpm build` + `pnpm test` (72/72) verdes

## Simplify sweep (hoy, 2026-04-24, `claude/review-memory-tv-code-8bjei`)

Review completa del paquete `internal/iptv` + `web/src/components/livetv` +
`web/src/pages/LiveTV.tsx` contra la memoria de este arc. Un único commit
grande con 9 cambios ordenados por impacto:

1. **`HeroMosaic.tsx` borrado** (−182 loc): reemplazado por `HeroSpotlight`
   en `5baeae5`, quedaba exportado en el barrel sin que nada lo importara.
2. **`stopCh` eliminado de `iptv.Service`**: creado, cerrado por `Shutdown`
   y nunca seleccionado. `Shutdown()` queda como no-op documentado para no
   romper el patrón simétrico con los otros servicios.
3. **HLS en `proxy.go` deduplicado**: extraídos `peekForHLS`,
   `absorbAndRewriteHLS`, `isAmbiguousStreamCT`. `ProxyStream` y `ProxyURL`
   usan los mismos primitivos; un fix futuro ya no se puede aplicar a solo
   uno de los dos paths (`pipeStream` con flush vs `io.Copy`, ACAO, health
   reporting siguen siendo diferentes a propósito).
4. **`useNowTick(ms)` hook nuevo** (`web/src/hooks/useNowTick.ts`): colapsa
   tres `setInterval → setState(Date.now())` idénticos en `EPGGrid`,
   `PlayerOverlay` y `HeroSpotlight`. **Bug colateral arreglado**: la barra
   de progreso del hero se congelaba con `items.length < 2` (el timer de
   auto-rotate no disparaba).
5. **Fallback real del logo en `ChannelCard`**: `onError` ponía
   `display: none` al `<img>` y las iniciales vivían en la rama opuesta del
   ternario — el resultado era un hueco sobre el gradient. Ahora se trackea
   `failedLogoUrl` y se muestra `<ChannelLogo>`. Estado derivado de props
   para evitar `setState-in-effect`.
6. **`useHeroSpotlight` extraído**: la preferencia + el fallback silencioso
   `favorites → live-now → newest` + las opciones del menú vivían en
   `LiveTV.tsx` (~130 loc). Ahora en `web/src/components/livetv/
   useHeroSpotlight.ts`. `LiveTV.tsx` baja de **763 a ~600 loc**.
7. **`getUpNext` simplificado**: quitada la ordenación cliente-side
   (`[...programs].sort`). El backend ya devuelve `ORDER BY start_time` en
   `internal/db/epg_repository.go:118,198`.
8. **`livetv/categoryOrder.ts`**: orden canónico de categorías en un único
   sitio. Antes duplicado entre `LiveTV.tsx:railOrder` y
   `CategoryChips.tsx:defaultOrder`.
9. **Guide tab ya no traga búsquedas sin resultado**: `channels={filteredChannels
   .length > 0 ? filteredChannels : channels}` era un silent fallback que
   mostraba TODOS los canales cuando la búsqueda no matcheaba nada. Ahora
   pasa `filteredChannels` y `EPGGrid` muestra su empty state.

Follow-up del mismo día (mismo ciclo, commit aparte):
- **`PublicEPGSources()` → `var publicEPGSources`**: la función devolvía un
  slice nuevo en cada llamada. Ahora variable de paquete, read-only por
  convención, zero-alloc en el hot path del handler del catálogo.
- **`capitalize(s)` compartido en `epgHelpers.ts`**: antes duplicado en
  `LiveTV.tsx` y `PlayerOverlay.tsx`.

**Verificación**: `go test -race ./...` 21/21 verde · `pnpm test` 74/74 ·
`pnpm build` ok · `pnpm lint` sin regresiones vs HEAD (los 11 errors + 4
warnings que quedan son deuda pre-existente en `AppLayout`, `CountrySelector`,
`MediaGrid`, `FolderBrowser`, `ItemDetail` — ya apuntada abajo).

## Último ciclo de trabajo (2026-04-24, live-TV arc)

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
10. **Edición manual de tvg-id + panel "canales sin guía"**: los IDs
    de canal son UUIDs aleatorios que se regeneran en cada M3U
    refresh, por lo que cualquier edición del admin se perdería
    inevitablemente. Solución: tabla nueva `channel_overrides(library_id,
    stream_url, tvg_id)` (migración 009) keyed por el atributo más
    estable del canal — el stream URL. Ciclo: admin hace
    `PATCH /channels/{id}` → `service.SetChannelTvgID` hace UPDATE en
    `channels` (cambio inmediato) + UPSERT en `channel_overrides` (por
    stream_url) → siguiente M3U refresh corre `ReplaceForLibrary` (wipe)
    y luego un nuevo hook `overrides.ApplyToLibrary(libraryID)` reaplica
    los overrides en una transacción. Overrides huérfanos (stream_url
    ya no en el playlist) son no-op y se quedan en la tabla esperando
    a que la URL vuelva. Endpoints nuevos:
    `GET /libraries/{id}/channels/without-epg` (LEFT ANTI-JOIN contra
    epg_programs en ventana -2h..+24h) y `PATCH /channels/{channelId}`
    admin-only con `{tvg_id}` opcional (string vacía limpia override y
    columna). Componente `ChannelsWithoutEPGPanel` con edición inline;
    solo aparece si hay orphans. Tests: repo (upsert replaza, apply
    rewritea en 1 tx, orphan no-op, delete idempotente, listar
    without-EPG), service end-to-end (override sobrevive un refresh M3U
    real contra httptest, clearing elimina la fila persistente, orphans
    detectados correctamente), handler (happy path, empty=clear,
    missing=no-op, invalid JSON 400, ACL deny 404, trim whitespace).
11. **Catálogo EPG verificado + 409 duplicados + invalidación refresh**
    (`36f36c4`): el catálogo inicial shippeaba URLs a ojo — `guiaiptvmovistar.xml`
    y `tdtsat.xml` no existen en davidmuma; todas las `epg.pw/api/epg.xml.gz?lang=*`
    daban 404 (URL scheme inventado). Ahora el catálogo es 5 entradas
    davidmuma HEAD-verificadas contra upstream (`guiatv.xml.gz`,
    `guiaiptv.xml`, `guiatv_plex.xml.gz`, `guiafanart.xml.gz`,
    `tiviepg.xml`); internacional queda a URL personalizada. Test
    `NoKnownBrokenURLs` impide que vuelvan a colarse por accidente.
    Además: `ErrEPGSourceAlreadyAttached` sentinel en el repo + 409
    Conflict + mensaje limpio (antes salía el `UNIQUE constraint
    failed` crudo de SQLite). Y `useRefreshEPG` invalida ahora
    `libraryEPGSources` + `channelsWithoutEPG` para que tras "Refrescar
    EPG" las badges se actualicen y los canales recién emparejados
    salgan del panel de huérfanos, sin recargar.
12. **Panel admin livetv unificado** (`1548dd6` + `381d5d6`): los tres
    paneles apilados (fuentes EPG + sin guía + con problemas) se comen
    la pantalla en cualquier biblioteca real. Primero hice el panel
    "sin guía" colapsable con búsqueda + paginación (20 filas inicial,
    "Mostrar más" de 40 en 40, filtro por número/nombre/tvg-id/grupo).
    Luego el refactor final: los tres paneles pasan a ser "tab bodies"
    dentro de un solo contenedor `LivetvAdminPanel` con header de
    status (dot 🟢/🟡/🔴/⚪ agregado de todas las señales), stats strip
    (`268 canales · EPG 52 (19%) ▓░ · 3 con problemas · 211 sin guía`
    con mini barra de cobertura), y pestañas que solo aparecen cuando
    tienen contenido. Tab por defecto auto-selecciona la más
    problemática. Full WAI-ARIA tabs pattern (← → Home End,
    aria-selected/controls/labelledby).
13. **Visual: tarjetas más limpias + rail "Apagados"** (`5f713ad` +
    `5baeae5`). Dos problemas reportados por el usuario:
    - Cards pintaban `logo_bg` a 67-100% opacidad → cada rail era un
      arcoíris. Ahora backdrop neutral oscuro con un radial suave de
      la marca al 20% — hint de identidad sin que compita.
    - La card envolvía thumbnail + info en una sola caja con borde,
      estilo "boxed". Ahora el borde rodeado vive solo en el
      thumbnail (el poster es la card visualmente); nombre / "Ahora"
      / up-next caen debajo como texto sobre el fondo de la página.
      Estilo YouTube/Netflix/Plex. La barra de progreso EPG pasa a
      una línea fina al borde inferior del poster.
    - Rail "Apagados" al final de Discover: tarjetas grises + badge
      "Apagado" (en lugar de LIVE), sin hover preview (no quemar
      bandwidth en canales que creemos muertos), click sigue
      funcionando (admin puede probar si volvió). Solo aparece con
      filtro "Todos". `UnhealthyChannel` extiende `Channel` para que
      pase al mismo `ChannelCard` con un flag `dimmed`.
14. **Hero personalizable por cuenta** (`5baeae5` + `ebbc64d`): el
    mosaico de 5 tiles aleatorios se sustituye por un `HeroSpotlight`
    de 1 tile grande con **auto-preview muted HLS** en el mount
    (landing feel-alive). Label claro arriba ("Tu favorito" / "En
    directo ahora" / "Recién añadidos"). Carousel dots cuando hay
    más de 1. Señal elegible por el usuario desde una rueda que
    vive **en la TopBar** (no dentro del hero — los 3 bugs que el
    usuario encontró: hero vacío con favoritos 0, dropdown clipeado
    por la aspect-ratio del poster, "Ocultar" como trampa sin vuelta).
    Auto-fallback silencioso: si favoritos está vacío, resuelve a
    live-now; si también, a newest. Label reflejando lo que está
    realmente en pantalla. Persistencia multi-dispositivo vía nueva
    tabla `user_preferences(user_id, key, value)` — migración 010 +
    `/api/v1/me/preferences[/{key}]` scoped a la sesión (sin lectura
    cruzada) + hook frontend `useUserPreference<T>(key, default)` con
    optimistic cache update + JSON encode/decode. `HoverPreview` del
    card se promueve a `StreamPreview` compartido con el hero.
    `pointer-events-none` en el `<video>` fija un bug de dwell que
    cancelaba el preview al entrar el cursor sobre el video. Tests:
    repo user_preferences (upsert, isolation por usuario, delete
    idempotente).

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
- **"Continuar viendo"** — último canal visto requiere tracking backend
  (tabla `channel_watch_history(user_id, channel_id, last_watched_at,
  seconds_watched)`). Cuando exista, se añade rail en Discover por
  encima de los de categoría (Fase 2 del plan Plex-max)
- Modal de detalles de programa al clicar en el EPG grid
- Virtualización de filas del EPG (flat DOM aguanta ~200 canales bien)
- Búsqueda unificada de canales + programas
- Grid EPG con colores por categoría + filtros

### IPTV backend
- XMLTV streaming parser (bomba memoria con feeds de 2GB; `xmltv.go:70-76`)
- Matcher más agresivo para subir cobertura EPG: tabla de alias conocidos,
  matching por channel number cuando ambos lo tienen, Levenshtein fuzzy
  con threshold. Hoy es tvg-id + display-name variants + quality strip +
  accent fold — suficiente para ~52/268 con davidmuma, pero escalaría
  a 70-80% con el refuerzo

### Event bus (3 tipos reservados sin publisher)
- `ChannelAdded`/`ChannelRemoved` — requiere descomponer `ReplaceForLibrary`
- `MetadataUpdated` — necesita hook en scanner o flujo de refresh dedicado

### Frontend general
- Tests del setup wizard (0 cobertura, ruta crítica de primer arranque)
- Tests de páginas admin (mutan estado, 0 tests — los panels nuevos
  `LivetvAdminPanel` + sub-paneles entran en este hueco)
- Warnings pre-existentes de `react-hooks/set-state-in-effect` en
  `AppLayout`, `CountrySelector`, `MediaGrid`, `FolderBrowser`, `ItemDetail`
  — ninguno de mi código nuevo, pero conviene barrerlos

## Próximo paso sugerido

Sin prisa. Candidatos ordenados por impacto sobre la experiencia live-TV:

1. **Matcher EPG más agresivo** — mayor multiplicador de valor ahora mismo:
   subir la cobertura de 52/268 a 150-200+ hace que los 5 rails por
   categoría + el hero "live-now" se llenen de contenido real sin que el
   admin tenga que mapear a mano.
2. **"Continuar viendo" + tabla de historial** — desbloquea el rail
   personalizado de Discover y el modo hero "más vistos". Migración +
   service tracking via el proxy (ya existe el hook en `streamOnceWithChannel`
   para el health-check; reusar).
3. **Modal de detalle de programa** en EPG grid — click en celda → modal
   con título/descripción/franja/canal/"Ver ahora". Cierra la experiencia
   Plex-like.
4. **Streaming parser del XMLTV** — único problema IPTV pendiente con impacto
   real en producción (feeds de 2GB petan servidores pequeños). davidmuma
   está en ~30MB así que no es urgente hoy.
5. **Tests del setup wizard + páginas admin** — deuda de cobertura, no
   bloqueante pero pendiente.
