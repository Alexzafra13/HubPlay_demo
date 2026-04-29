# Estado del proyecto

> Snapshot: **2026-04-29 día → tarde → noche, dos sesiones encadenadas de detail-UX premium** — bloque A `015ThedLMwhsx5ittdmtxSN4` (8 commits del "premium detail UX" pass: cast/crew end-to-end con fotos, IMDb/TMDb deep links, weekly image refresh scheduler, watched-count agregado, hero treatments) y bloque B `claude/review-project-resume-3V6Mr` (4 commits unificando hero movies↔series + aurora ambiental PS3-XMB como page canvas + fixes iterativos sobre fotos del usuario). Foundation lista para `/people/{id}` (SQL `ListFilmographyByPerson` + repo, sin handler aún). **tests: backend verde · frontend 336/336 · tsc clean**.

`main` al día — último push 2026-04-29 17:04. Tres commits frescos
cierran dos items P1 del roadmap pre-Kotlin TV:
- `5a37ed2` stream(caps): server honra `X-Hubplay-Client-Capabilities`
- `f21194f` api(client): probe MediaSource y manda el header
- `94cb74b` sync(progress): cross-device watch state via `/me/events` SSE

---

## 🌐 Sesión 2026-04-29 tarde (PRs #122 + #123 a `main`) — 3 commits foundation pre-Kotlin TV

Sesión doble que cierra **dos items P1 grandes** del roadmap "preparar la API
para que la consuma una app nativa Android TV". Hasta hoy `Decide()` asumía
"el cliente es un navegador web con codecs típicos" y el progreso de
reproducción no se sincronizaba entre dispositivos. Ambos asuntos quedan
cerrados.

### Commit 1 — `5a37ed2` `stream(caps)`: capability negotiation server-side

`stream.Decide()` deja de hard-codear codecs y acepta un `*Capabilities`
parseado del header `X-Hubplay-Client-Capabilities` (formato semicolon
separated key=value-list, mirror de `Accept-CH`/`Vary`):

```
video=h264,h265,vp9,av1; audio=aac,opus,eac3; container=mp4,mkv
```

Reglas clave:
- Tokens lower-cased y trimmed; keys desconocidas se ignoran (forward-compat).
- Segmentos malformados se descartan silenciosamente — un typo no envenena el resto.
- Header ausente → `DefaultWebCapabilities` (= comportamiento legacy exacto).
- Declaración parcial → `effectiveCapabilities` rellena con defaults.

Resultado real: una Chromecast que decodifica EAC3 nativamente deja de
recibir AAC stereo downmixed cuando el wire ya soportaba 5.1.

Tests: 15 backend (parse, backfill, decisión nil/declarada/parcial,
DirectPlay/DirectStream/Transcode). Bonus mientras estaba: fix de
`TestSession_SegmentPath` que asumía forward-slashes y fallaba en Windows.

**Files of record**: [`internal/stream/capabilities.go`](internal/stream/capabilities.go),
[`internal/stream/decision.go`](internal/stream/decision.go),
[`internal/api/handlers/stream.go`](internal/api/handlers/stream.go).

### Commit 2 — `f21194f` `api(client)`: probe MSE + send header

Cliente web probea `MediaSource.isTypeSupported` contra una lista fija de
pares (codec, MIME). Resultado cacheado por sesión de página (codec
support no cambia sin reload). El `api.request()` adjunta el header
automáticamente.

Detalles defensivos:
- `isTypeSupported` lanza en algunos browsers con MIME malformado → try/catch
  per-MIME, no envenena el resto.
- SSR / pre-MSE → null → server fallback a defaults web (preserva legacy).
- Cero probes pasan → header suprimido (no mentir con header vacío).
- `hevc` y `h265` se emiten ambos cuando MSE decodifica la familia (ffprobe
  normaliza a "hevc" pero hay items legacy con "h265" — listar ambos casa
  con cualquier nombre que llegue del scanner).

Tests: 9 (SSR, throw, partial, contenedor, memoización, alias hevc/h265,
isTypeSupported missing, todo-false, fetch real con header).

**Files of record**: [`web/src/api/clientCapabilities.ts`](web/src/api/clientCapabilities.ts),
[`web/src/api/client.ts`](web/src/api/client.ts).

### Commit 3 — `94cb74b` `sync(progress)`: cross-device via SSE

El feature insignia "lo empecé en el portátil, sigo en el móvil". El bus de
eventos (`internal/event/bus.go`) ya existía para scanner/IPTV; este commit
lo abre per-user con filtrado correcto.

Tres tipos nuevos en el bus: `user.progress.updated`, `user.played.toggled`,
`user.favorite.toggled`. Splitting en tres (en vez de un genérico
`user_data.changed`) permite al frontend invalidar el set TanStack correcto
sin parsear payload kind.

`ProgressHandler` recibe `EventBusPublisher` opcional; cada uno de los 4
endpoints mutating publica tras DB write. nil bus = no-op (test rigs
simples).

Nuevo handler `GET /api/v1/me/events` (SSE):
- **Filtra por `Data["user_id"] == claims.UserID` ANTES del channel write**
  → un cliente lento del usuario A no presiona la publicación al usuario B.
- Defence in depth: rechaza eventos con Data nil, sin user_id, o con
  user_id wrong-typed. Un publisher mal configurado **no** debe fan-out a
  todo el mundo.
- Mismo framing SSE que `/events` (keepalive, JSON shape, unsubscribe-on-disconnect)
  → el `EventSource` del frontend consume ambos sin divergencia.

**Por qué SSE y no WebSocket**: canal one-way (server → client) y casa con
todos los clientes (web `EventSource`, Kotlin TV `okhttp-sse` maduro,
auth por cookie, exp-backoff reconnect gratis). WebSocket compraría
bidireccionalidad innecesaria + nginx upgrade config.

Frontend:
- `useUserEventStream` — sibling de `useEventStream` apuntando a `/me/events`,
  nombre explícito para que admin code no instancie sin auth por accidente.
- `useUserDataSync` — orchestrator: 3 subs → invalidaciones correctas:
  - `progress` → `items/{id}`, `progress/{id}`, continue-watching
  - `played`   → `items/{id}`, continue-watching, next-up
  - `favorite` → `items/{id}`, favorites
- Montado **una vez** en `AppLayout` (no per-page) para no fan-out duplicate
  connections ni perder eventos cuando otra ruta esté activa.

Tests: 5 backend (401 unauth, delivers own, drops other users', drops
malformed, unsubscribes 3 tipos on disconnect) + 5 frontend (subs por tipo,
invalidaciones correctas, malformed JSON no-throw, close on unmount,
disabled mounts nothing). Total 70+ stream/handlers backend, 364/364 frontend.

**Files of record**: [`internal/api/handlers/me_events.go`](internal/api/handlers/me_events.go),
[`internal/api/handlers/progress.go`](internal/api/handlers/progress.go),
[`internal/event/bus.go`](internal/event/bus.go),
[`web/src/hooks/useUserDataSync.ts`](web/src/hooks/useUserDataSync.ts),
[`web/src/components/layout/AppLayout.tsx`](web/src/components/layout/AppLayout.tsx).

### Estado al cierre

- `main` 2026-04-29 17:04, working tree clean, sincronizado con origin.
- Backend `go test -race ./...` clean, frontend 364/364 verde, `tsc -b` clean.
- Quedan en P1: device-code login (~1-2 días) y OpenAPI spec (~½ día).
  Después de esos dos, **todos los prerequisitos para empezar la app
  Kotlin TV están completos**.

---

## 🎨 Sesión 2026-04-29 tarde-noche (rama `claude/review-project-resume-3V6Mr`) — 4 commits

Iteración guiada por capturas de usuario. Tres rondas de feedback in-the-loop: (1) "el hero series está roto, container desplazado", (2) "el hero **bueno** era el de series, las películas deberían igualarse", (3) "ahora aurora demasiado soso, backdrop pixelado, botón se corta, sigue viendo duplica". Cada ronda cerró con commit + push. Cierra con la **paleta de colores explicada** y memoria actualizada.

### Commit 1 — `39698ce` unify hero + Plex-style cast

Antes: `HeroSection` (movies) tenía layout poster-izq + info-flex anclado al bottom; `SeriesHero` (series/season) tenía contenido apretado en columna izquierda max-w-md, dejando el 60% de la banda como backdrop puro. Visualmente eran dos surfaces distintos. Plus el `ItemDetail` wrapper pintaba `--detail-tint` como `backgroundColor`, creando seam con AppLayout en los lados de la página = "container desplazado raro" alrededor de Temporadas.

Cambios:
- **Pivote tras malentendido**: primer pase intenté unificar moviendo SeriesHero al layout poster-bajo de HeroSection; el usuario corrigió "el bueno era series, no movies". Reverted SeriesHero al layout original; reescribí HeroSection adoptándolo (full-bleed band + columna izquierda con poster + título + meta + buttons). Episode breadcrumb + S01E03 prefix preservados. Kebab menu pop a `left-0` (la columna está en izquierda).
- **`CastChip` estilo Plex**: avatar circular 96-112 px con ring border, nombre + personaje en texto limpio centrado debajo, sin tarjeta envolvente. El usuario explícitamente "me gusta más como lo hace plex con un círculo del avatar y el nombre en limpio".
- **Wrapper `ItemDetail` deja de pintar `--detail-tint` como `backgroundColor`**: la CSS-var sigue definida (la usa el bottom-fade del hero como target) pero el wrapper queda transparente. Mata el container desplazado.
- **Foundation `ListFilmographyByPerson`**: SQL en `internal/db/queries/people.sql` + sqlc stub a mano (ADR-004) + repo method dedupe-por-item con min(sort_order). Handler/ruta no enchufados — coste 0 hoy, sesión próxima `/people/{id}` es puramente aditiva. Episode-level credits drop through (TMDb ya tiene cast a nivel de show en el 95% de casos).

### Commit 2 — `99fd307` ambient aurora PS3-style

Usuario: "el tinte para cuando salia la lista de temporadas en series si me gustaba como sensacion premium que cogia el color y cada pagina era personal de peliculas, pero claro estaba mal hecho". Quiere el efecto, sin el bug.

Solución arquitectural: en vez de pintar el wrapper, render `<div fixed inset-0 -z-10>` como capa canvas viewport-completo detrás de todo. Tres `radial-gradient` apilados:
- vibrant blob 80% × 60% en (15%, 10%) — cubre el hero left side
- muted blob 70% × 70% en (85%, 90%) — cubre seasons grid + cast
- halo central 35% radio en (50%, 50%) — soft tonal balance

Cada página de detalle pinta su propia personalidad sin animación (respeta reduce-motion por defecto, sin coste de GPU paint constante).

### Commit 3 — `7000ec9` aurora actually visible + softer seam

Foto del usuario: aurora invisible, hero "se corta". Diagnóstico:

- **AppLayout pintaba `bg-bg-base` en su wrapper**. El body ya tiene `bg-bg-base` global en `styles/globals.css`. La duplicación tapaba la capa fixed `-z-10` de la aurora. Da igual subir intensidades — invisible mientras el AppLayout no fuera transparente. **Fix**: quitado `bg-bg-base` del wrapper de AppLayout (el body se encarga). Ningún otro page se rompe porque el body cubre el viewport igual.
- **Bottom-fade `h-32` cliff**: 128 px de fade sobre un hero de 600-720 px lee como seam horizontal. Subido a `h-48 lg:h-56` (192-224 px).
- **Intensidades subidas**: 28% / 26% / 12% → 45% / 40% / 18% (vibrant / muted / halo).

### Commit 4 — `10afab3` sharp backdrop + clipped button + duplicate panel + soso aurora

Cuarta foto, cuatro síntomas distintos:

- **Backdrop pixelado en movies**: `thumb(url, 1280)` pedía variant 1280-wide al backend que el browser luego upscaling-a-1920 mostraba blando. Backdrop ahora sirve URL original (sin `?w=`). El poster mantiene `thumb(url, 720)` — es pequeño, vale la pena el ahorro de ancho de banda.
- **Botón Reproducir cortado**: el inner div del hero tenía `max-h-[720px]`. Cuando el contenido (logo + tagline + meta + watched-count + overview + buttons) excedía ese techo, las acciones se clippeaban. Removed. `min-h` se queda para que el hero no colapse.
- **"Sigue viendo" duplicado**: el panel de resume-target episode renderizaba en BOTH series y season pages. En la página de season ya está la lista completa de episodios con `EpisodeRow` mostrando progreso por fila — surface el mismo affordance dos veces (panel + list row) era ruido visual. Ahora condicionado a `heroScope === "series"`.
- **Aurora soso**: el blob inferior-derecho (donde scrollea el usuario más) se sembraba con `muted` — desaturado por definición. Cambiado a usar `vibrant` en ambos blobs principales (60% upper-left, 50% lower-right), `muted` queda como counter-blob central a 28%. Foreground contrast preservado por el corte del mix.

### Cómo funciona la extracción de colores (preguntado por el usuario al cierre)

Pipeline en dos pasos, sin SaaS:

**Backend** — `internal/imaging/colors.go::ExtractDominantColors`, corre cuando `IngestRemoteImage` baja la imagen al disco:
1. Decodifica con std-lib decoders (mismos que blurhash).
2. Muestrea ~1024 px en grid `step = max_dim / 32` (coste O(1) por imagen).
3. Bucketea en cubo RGB 16×16×16 (4096 bins, cada uno acumula r/g/b sum + count).
4. Por bucket calcula L y S del HSL y puntúa dos ganadores:
   - `vibrant = saturation × count`, restricted L ∈ [0.20, 0.80] (excluye blown highlights y jet black)
   - `muted = (1 − saturation/2) × count`, restricted L ≤ 0.40 (oscuro pero legible)
5. Persiste como strings `rgb(R, G, B)` en columnas `images.dominant_color` + `images.dominant_color_muted`.

Returns `("", "")` cuando el decoder no entiende la imagen (mismo contrato que `ComputeBlurhash`).

**Wire**: `db.PrimaryImageRef` carga ambos campos. `/items/{id}` los expone como `backdrop_colors: { vibrant, muted }`. `GetPrimaryURLs` los devuelve por item para que `PosterCard` pinte placeholder mientras decodifica.

**Frontend**:
- Path principal: `item.backdrop_colors.vibrant/muted` directos del wire.
- Fallback (`useVibrantColors`): para items pre-extracción, corre `node-vibrant` sobre los bytes de la imagen via dynamic import. Lazy chunk separado.
- La aurora del wrapper **solo** usa el path principal (no fallback runtime) para que el viewport-canvas paint sea barato.

Se extrae siempre del **backdrop**, no del poster — el backdrop es lo que pinta colorida la mayoría de la página.

---

## 🎬 Sesión 2026-04-29 día (sesión `015ThedLMwhsx5ittdmtxSN4` "premium detail UX") — 8 commits

Sesión sobre rama de catch-up (PRs ya mergeados a main), no documentada en su momento. Bloque temático: pulir todo el detail surface al nivel "premium" antes del pivot a Kotlin TV.

| Commit | Tema |
|---|---|
| `699d63e` | fix: nil-safe SettingsRepository + drop now-dead DedupeSeasonsByChildCount (los UNIQUE indexes 018 ya garantizan invariante) |
| `49d853e` | poster placeholder colour, hero crop, page tint, kill 401 cold-start noise (auth bootstrap real con `bootstrap()` que hace refresh antes de protected queries) |
| `b5988d7` | drop ItemDetail tint gradient + restore SeriesHero height (primer intento de fix del seam — fallido, el fix definitivo llegó en sesión `claude/review-project-resume-3V6Mr`) |
| `2f2cfed` | thumbnails ?w= en cards, tagline+studio en SeriesHero, **watched-count agregado en series** (`SeriesEpisodeProgress` query con JOIN parent_id 2-niveles), **weekly image refresh scheduler** en `library.ImageRefreshScheduler` |
| `e07fe4e` | series: tint de página coordinado con hero (`--detail-tint`), button breathing |
| `6b2996f` | movies: port hero premium desde series (parity primer intento — el unificado real fue en sesión siguiente) |
| `bed03a2` | detail: external_ids → deep links IMDb/TMDb en kebab del hero |
| `bd64951` | **detail: cast/crew end-to-end con fotos** — tablas `people`/`item_people` ya estaban en el schema desde la migración 001 pero nadie leía/escribía. Wired pipeline completa: `db.PeopleRepository` (4 ops + dedupe by name), `scanner.syncPeople` baja photo via `IngestRemoteImage` con SSRF guard, handler `GET /api/v1/people/{id}/thumb` (path-traversal validado), wire `/items/{id}` incluye `people: [{id, name, role, character?, image_url?, sort_order}]`. Frontend: `CastChip` con avatar real + onError fallback a inicial. Foundation para `/people/{id}` (la id ya viaja en el wire). |

---

## 🧹 Sesión 2026-04-29 noche (post-review hardening + settings runtime) — 4 commits

Sesión orientada a "lo que hicimos ayer, ¿es realmente sólido?". Empieza con un peer-review sobre el IPTV hardening (no del usuario — propio, como siguiente capa de tamiz) y termina con la pieza arquitectural más importante de la rama: dejar de pedirle al usuario que edite YAML.

### Commit 1 — `a21204c` IPTV transmux post-review hardening (5 bugs + split)

Auto-review del trabajo del 28→29 detectó **5 bugs reales latentes** y un techo de mantenibilidad:

- **B1 stderr drain race en `processWatcher`**: `cmd.Wait()` no espera al goroutine consumer del `StderrPipe()`. El stdlib lo dice explícitamente. Resultado: la cola de `stderrTail` puede no incluir la línea fatal que ffmpeg emite justo antes de exit. Eso rompía silenciosamente la decisión de auto-promoción a reencode (`looksLikeCodecError` decidía sobre cola incompleta) y truncaba el log al peor momento. **Fix**: `stderrRing.wait()` que bloquea hasta que `consume` retorna; processWatcher sincroniza antes de leer `String()`.

- **B2 scanner buffer 4 KiB causaba deadlock potencial**: `bufio.Scanner.Buffer(_, 4096)` aborta con `ErrTooLong` en cualquier línea >4 KiB (debug builds, full TLS chains). El consumer salía, la pipe del kernel se llenaba, ffmpeg bloqueaba en write hasta que `-rw_timeout` (10s) lo mataba. **Fix**: bump a 64 KiB + `io.Copy(io.Discard, rd)` de drenaje fallback al salir el scanner. Comentario "binary garbage is silently truncated" era falso, corregido.

- **B3 race entre `evict` y respawn por mismo `WorkDir`**: evict soltaba el lock antes de `os.RemoveAll(WorkDir)`. Mientras, otro `GetOrStart` para el mismo canal podía entrar, hacer `MkdirAll` en el mismo path, y luego el RemoveAll del primer evict se cargaba la dir nueva. **Fix**: `<cacheDir>/<channelID>/<startNanos>/` versionado por spawn. evict sólo borra ese subdir; el padre se limpia best-effort vía `os.Remove` (ENOTEMPTY se ignora). `clearWorkDir` borrado (innecesario con dirs nuevas siempre).

- **B4 doc lie en `iptv.go:52-55`**: el comentario decía "nil logoCache surfaces upstream URL" pero `logoProxyURL` siempre reescribe al proxy. Test `iptv_dto_test.go:51-68` ancla esa conducta. **Fix**: alinear comentario con la realidad (404 + React onError fallback a iniciales). No tocar el código — funciona, el comentario mentía.

- **M3 zombie sessions**: el reaper saltaba sesiones no-ready, pero ffmpeg con upstream que envía 1 byte cada 8s evade `-rw_timeout` y la sesión queda en el mapa indefinidamente, bloqueando un slot de `MaxSessions`. **Fix**: `startupGraceMultiplier = 2`, después de `2× ReadyTimeout` el reaper force-terminate la sesión y registra failure en el breaker (chronic offenders entran en cooldown).

- **M2 spawn_error metric**: pre-spawn fails (mkdir, fork, pipe) no incrementaban ningún counter en el sink de Prometheus. **Fix**: `IncStarts("spawn_error")` distinto de `crash` (upstream).

- **M1 split de `transmux.go`**: 1451 → 1052 líneas. Sin abstracciones nuevas, sólo relocalización:
  - `transmux_args.go` (287) — argv builders + encoder tuning + `defaultTransmuxUserAgent`
  - `transmux_codec_classify.go` (35) — `codecErrorPattern` + `looksLikeCodecError`
  - `transmux_stderr.go` (116) — `stderrRing` con la nueva `wait()` barrier

**Tests nuevos** (6 regression):
- `TestStderrRing_WaitBlocksUntilConsumeReturns` (B1)
- `TestStderrRing_WaitNilSafe` (B1, defensivo)
- `TestStderrRing_DrainsAfterOverlongLine` (B2)
- `TestTransmuxManager_ReapsStartupZombies` (M3)
- `TestTransmuxManager_PerSpawnVersionedWorkDir` (B3)
- `TestTransmuxManager_PreSpawnFailureCountsAsSpawnError` (M2)

### Commit 2 — `8a723c0` tests frontend de useEventStream / useTrickplay / useLiveHls (+31 tests)

Deuda arrastrada desde múltiples sesiones. 31 tests nuevos:
- **useEventStream (7)**: stub global `EventSource`, verifica open/close lifecycle, dispatch del tipo correcto, no churn al re-render con closure nueva (el ref-stash optimization), swap al cambiar tipo.
- **useTrickplay (8)**: empty itemId no-op, tolera envelope `{data: ...}` y bare manifest, 503 / network error → `available=false` silencioso, abort en unmount, encode del itemId.
- **useLiveHls (16)**: mock de `hls.js` vía `vi.hoisted` (FakeHls), null url/ref no-ops, onFirstPlay una sola vez, timeout fallback con `onFatalError("timeout")`, retry 3 network errors antes de rendirse, classify manifest errors, onFatalError fires once per stream URL, streamUrl change destruye instancia previa, unmount limpia visibilitychange listener, document.hidden flip → stopLoad/startLoad(-1), reload() force-reattach, ref-stash con onFirstPlay.

296 → 327 tests, suite verde.

### Commit 3 — `0779e4e` migración 018 UNIQUE partial indexes para show hierarchy

**Diagnóstico**: el usuario reportó series duplicadas. Análisis del código:
- Schema `items` no tiene UNIQUE constraint en (library_id, type, title); el cache `showCache` en `internal/scanner/show_hierarchy.go` evita dups en condiciones normales pero la key es title-exact (case/whitespace/accent miss → cache miss → dup).
- Cuando ya hay dups en DB, el seeding pasa `rememberSeries(title, id)` por cada uno y la última gana en el map; las anteriores quedan huérfanas para siempre.
- Para seasons ya había `DedupeSeasonsByChildCount` (read-time dedupe en `library/service.go`), para series no había equivalente — por eso aparecían duplicadas en el rail.

**Lo que se hizo**: el usuario verificó empíricamente que un wipe (`DELETE FROM items WHERE type IN ('series','season','episode')` con `PRAGMA foreign_keys = ON`) + rescan **no recreaba duplicados**. Confirmó que el bug era residuo histórico, no regresión activa.

**El fix**: migración 018 con (a) pasada de cleanup defensiva — re-parenta hijos de no-canónicos al canónico (MIN(id)), borra los demás; no-op en DB ya limpia. (b) **UNIQUE INDEX parciales**:
- `uniq_series_per_library ON items(library_id, title) WHERE type='series'`
- `uniq_season_per_series ON items(parent_id, season_number) WHERE type='season' AND season_number IS NOT NULL`

**Lo que NO se hizo (y por qué)** — Torvalds-simple aplicado consistentemente: sin `ErrItemConflict` tipado, sin `FindSeriesByTitle/FindSeasonByNumber` helpers, sin recovery branches en el scanner. El usuario verificó que el scanner actual no genera dups; añadir silent-recovery code defendería escenarios hipotéticos. Si la migración alguna vez "salta" en el futuro será porque hay un bug real, y queremos que falle ruidosamente (`UNIQUE constraint failed: items.title`) en vez de papelera.

### Commit 4 — `b1a84da` runtime-editable settings (kill the "edit yaml" prompts)

**Disparador**: el usuario miró el panel /admin/system y vio dos cards diciendo "Sin configurar (define server.base_url en hubplay.yaml)" y "Activa hardware_acceleration.enabled en hubplay.yaml". Su feedback: *"no quiero que el usuario tenga esa responsabilidad en el yaml, debería poder hacerse en el panel"*. Razón Torvalds: una abstracción que pide al admin SSH-ear y editar un fichero está rota.

**Decisión arquitectural** (ver ADR-010):

| Capa | Qué vive ahí | Inmutable? |
|---|---|---|
| YAML / env | server.bind, server.port, database.path, streaming.cache_dir, auth bootstrap secret | sí (boot-time) |
| `app_settings` (DB) | server.base_url, hardware_acceleration.enabled, hardware_acceleration.preferred | no (runtime overlay) |

Authority chain: `app_settings` row → YAML default → effective. Sin Runtime overlay struct, sin caching layer, sin goroutine watching changes. Handlers que necesitan un valor reciben `SettingsReader` (interfaz pequeña, sólo `GetOr`) y consultan al servir la request. Una SQLite point query por hit en `/admin/system/stats`, invisible en perfil.

**Surface nuevo**:
- Migración 019: `app_settings(key TEXT PK, value TEXT, updated_at)`.
- `internal/db/settings_repository.go` con `Get/GetOr/Set/Delete/All`. Sin tipos de dominio — strings raw, validados arriba en el handler.
- `internal/api/handlers/settings.go` con `GET/PUT/DELETE /admin/system/settings`. **Whitelist hardcoded** (no es un KV genérico). Una key nueva entra con un const + un caso en el switch del validator + un par de strings i18n.
- HWAccel se aplica al boot: `cmd/hubplay/main.go` lee del settings repo justo antes de `stream.NewManager`. La UI dice "Reinicia para aplicar" cuando hay override pendiente. **Sin re-detección runtime** (el detector tiene estado capturado, replicarlo es ruido).
- BaseURL es runtime: `SystemHandler.effectiveBaseURL(ctx)` y `StreamHandler.effectiveBaseURL(ctx)` consultan el settings repo en cada request. Save en panel → próximo request lo ve.

**Frontend**:
- `useSystemSettings`, `useUpdateSystemSetting`, `useResetSystemSetting` hooks; mutations invalidan `systemStats` para que el panel refresque al instante.
- `web/src/pages/admin/system/SystemSettingsSection.tsx` — sección nueva al final del System page. Per-row Save + Reset, dirty-state pinning del Save, badge `Custom`/`Default`, restart-needed hint inline. `<input>` para texto libre, `<select>` para enum (driven por `allowed_values` del backend).
- Borrados los strings i18n `baseURLEmpty` y `hwAccelDisabledHint` que pedían editar yaml. Reemplazados por `baseURLUnset` y `hwAccelDisabledPointer` que apuntan a la sección editable.

**Tests** (13 nuevos):
- 6 settings_repository_test (GetOr fallback, Set upsert, Delete reset, etc.)
- 7 settings_test del handler (whitelist gate, validation per-key, normalisation, reset, defaults)

### Estado al cierre

- Backend Go: `go test -race ./...` verde.
- Frontend: 41 test files, 327 tests, todos verde. tsc clean.
- Migraciones: 18 + 19 = 19 en total (017 → 019).
- `transmux.go` 1451 → 1052 líneas (extracción a 3 ficheros sibling).
- 4 commits limpios en la rama, listos para PR a `main` cuando el usuario diga.

### Lo que el usuario tiene que hacer al desplegar

- Wipe ya aplicado de su DB (manual). La migración 019 no hace nada en su DB ya limpia; `app_settings` arranca vacío y todo cae al YAML default.
- Tras pull de la nueva imagen, el panel /admin/system tendrá la sección "Configuración" al final con tres tarjetas editables. El YAML sigue funcionando como fallback; el operador puede ignorar el panel si quiere.

### Próximos hitos candidatos (no en este sprint)

- **App nativa Kotlin para Android TV** sigue siendo el gran post-merge.
- Virtualización de `EPGGrid` con `@tanstack/react-virtual` (deuda agendada conscientemente).
- Single-flight EPG fetches (deferida; lock per-library cubre el caso común).
- Cuarto setting runtime cuando aparezca un caso real — añadir es trivial (whitelist + i18n).

---

> **Las sesiones del 2026-04-27 al 2026-04-29 (pre-detail-UX) viven ahora en**
> [`archive/2026-04-27-to-04-29-pre-detail-ux.md`](archive/2026-04-27-to-04-29-pre-detail-ux.md).
> Incluye: IPTV hardening completo (7 commits), transmux + import event-driven,
> M3U import async, huge-list resilience, peer-review followups, iptv split,
> SRP refactor, auditoría senior + remediación, series detail UX completo,
> hot-fix responsive admin. Cuando algo de aquí en HANDOFF cite un commit del
> bloque archivado, ahí están los detalles.

---

## 👉 HANDOFF PARA LA PRÓXIMA SESIÓN

> **Lee esto primero.** Resume qué cerramos, qué decidimos y qué toca.

### Lo que cerramos esta sesión (rama `claude/review-movies-series-feature-9npZH`)

**35 commits** sobre la rama. Empezó como un *senior code review* del
surface Movies / Series y derivó en un rework profundo del flujo
end-to-end más cuatro bugs críticos que el usuario reportó al probar.

#### Bloque A — Bugs catastróficos cerrados

1. **`06bde24` — scanner persistía URLs remotas**. Cada vista de
   poster era un `307` → `image.tmdb.org`. Privacy + fragilidad.
   Ahora `imaging.IngestRemoteImage` (atomic write, blurhash,
   SafeGet). Test de regresión:
   `TestFetchAndStoreImages_PersistsLocalPathNotURL`.

2. **`56d18af` + `93c643e` — librería de Series 400 + admin invisible**.
   Cross-stack mismatch `tvshows`/`shows`. Backend +
   `api/types.ts` + `LibrariesAdmin.tsx` + setup wizard alineados
   al canónico `shows`.

3. **`79c319e` + `45888a1` — `/series` vacío**. El scanner no
   construía jerarquía series → season → episode. Implementé el
   parser estilo Plex (`show_parser.go`, ~25 tests) + cache
   in-memory por scan (`show_hierarchy.go`).

4. **`d07e367` — limpieza pre-launch**. Quitada toda capa de
   compatibilidad legacy (alias `tvshows`, fallback a URL remota
   en ServeFile, runtime backfill de hierarchy). Una sola forma
   válida para cada cosa. -237 LOC, +0 funcionalidad perdida.

#### Bloque B — Foundation arquitectónica

| Commit | Tema |
|---|---|
| `697734c` | Dedupe Movies/Series → `MediaBrowse` genérico |
| `eb7795e` | `user_data` per-item en listings (4 tests) |
| `bb8dc17` | TMDb/Fanart cache + backoff + single-flight (12 tests) |
| `06bde24` | Scanner descarga imágenes a disco + atomic writes |
| `e27e60b` | Thumbnails se reapan al borrar imagen |
| `bcc8fb7` | `is_locked` flag — manual override sobrevive refresh (Plex parity). Migración 013 |
| `4eb7b70` | Continue Watching filtra near-complete (≥90%) + abandoned (>30d ∧ <50%) |
| `6bbbb64` | `provider.ImageResult.Source` se rellena en el Manager |

#### Bloque C — Features visibles

| Commit | Tema |
|---|---|
| `07fd29f` | Up Next overlay con countdown 5s |
| `6d904db` | Quality picker en player |
| `33c9f9c` | i18n del player completo |
| `75eee70` | Capítulos: ffprobe → DB → marcas en seek bar |
| `782d233` | Endpoints external subs (OpenSubtitles wired) |
| `2b823e9` | HW accel ya **se usa** (antes se descartaba) |
| `0f26fb0` | Trickplay backend lazy generation |
| `444e7b6` | UI subs externos (modal + `<track>` dinámico) |
| `024586e` | Trickplay UI: hover preview en seek bar |
| `6981a9c` | Filtros género/año/rating en MediaBrowse |
| `3dda6dc` | "Watch Tonight" tile en Home |
| `465298c` | Audio picker enriquecido ("English · TrueHD 7.1") |

#### Bloque D — Tests añadidos

- Backend: `+~30 tests` netos. Cobertura nueva en `imaging/`,
  `provider/`, `library/`, `scanner/`, `db/`, `api/handlers/`.
- Frontend: 245 → **289 tests** (37 ficheros).

### Estado de operación

- Working tree limpio, push hecho, rama lista para review/merge.
- **Usuario probó la rama**. Destapó 4 bugs en proceso (todos
  cerrados). Falta verificación end-to-end exhaustiva siguiendo el
  QA checklist.
- **QA checklist actualizado** con un bloque ⚠️ al inicio:
  **borra DB / lib antes de probar** (la rama no tiene runtime
  migration; el código nuevo solo construye jerarquía al INSERT).

### Decisiones senior tomadas (registradas en architecture-decisions)

- **ADR-002**: Imágenes descargadas siempre a disco. URL remota
  nunca se sirve al cliente.
- **ADR-003**: `is_locked` per-image, auto-set en cualquier acción
  manual. Refresher gate per-kind.
- **ADR-004**: Continue Watching filtra near-complete ≥90% +
  abandoned >30d∧<50%. Sin duración → bypass.
- **ADR-005**: Show hierarchy desde estructura de dirs (Plex
  convention). Cache in-memory por scan.
- **ADR-006**: HW accel input flag sin `-hwaccel_output_format`
  (frames bajan a RAM, escala SW, encoder HW). Tradeoff
  documentado.

---

## 🎯 PRÓXIMO HITO ESTRATÉGICO: Kotlin Android TV

> Decisión registrada en sesión 2026-04-27. La siguiente gran fase
> después de mergear esta rama es **app nativa Kotlin para Android
> TV** (Jetpack Compose for TV / Leanback).

### Qué cambia este pivote

Toda decisión técnica de aquí en adelante se evalúa contra:

> "¿Esto facilita o estorba consumir la API desde un cliente nativo
> que necesita rendimiento, que decodifica códecs que el navegador
> no, y que vive en un mando D-pad sin ratón?"

Eso re-prioriza el roadmap. Trabajo que sigue siendo valioso pero
**baja en la cola**:
- Subtitle styling per-user web (la app nativa lo hará a su manera).
- Smart collections con DSL (vale igual; web-only ahora).
- Watch-together (gran feature pero post-MVP de la app TV).
- Trickplay scan-time pregeneration (lazy ya cubre el 90%).

Trabajo que **sube en la cola** porque la app TV lo necesita:
1. **Auth para clientes nativos** (device code flow estilo Netflix:
   "introduce este código en hubplay.tu-dominio.com/link"). Sin
   esto, login en TV con teclado D-pad es un infierno.
2. **API stable + documentada**. Cliente nativo no puede iterar
   sobre cambios silenciosos del wire format. Se necesita un
   contrato versionado (`/api/v1/*`).
3. **Capability negotiation real**. La app TV puede pasar TrueHD,
   Atmos, HDR10 al receptor; el server tiene que dejar de
   re-codificar cuando puede direct-stream. Hoy enriquezco labels
   pero todo audio se transcodea igual.
4. **Cross-device progress sync**. "Empecé en el móvil, sigo en
   la TV" es la killer-feature de Plex. WebSocket / SSE con
   debounce.
5. **Endpoint plano para Now Playing / Up Next** (ya existe el
   server-side, falta confirmar que el shape sea el que la app
   quiere consumir).

---

## Cola priorizada para la siguiente sesión

### **P0 · Bloqueante operacional**

1. **Validación end-to-end final del usuario** sobre la rama
   actual. Si destapa más bugs, se priorizan sobre todo. El QA
   checklist cubre los 35 commits.

### **P1 · Pre-Kotlin TV (foundation que la app va a consumir)**

2. ~~**Capability negotiation server-side**~~ ✅ **shipped 2026-04-29**
   (`5a37ed2` + `f21194f`). Header `X-Hubplay-Client-Capabilities`
   parseado en `stream.Capabilities`; web client probea
   `MediaSource.isTypeSupported` y manda el header. Defaults web
   intactos para legacy. La app Kotlin TV declarará agresivamente
   cuando llegue.

3. **Device-code login flow** (~1-2 días). Endpoints:
   - `POST /auth/device/start` → devuelve `{user_code: "ABC123",
     device_code: "...", verification_url: "/link", expires_in: 600}`.
   - `POST /auth/device/poll` → cliente sondea cada N segundos.
     Devuelve el JWT cuando el usuario aprueba en `/link`.
   - Frontend: página `/link` con input grande tipo TV-friendly
     que permite al usuario aprobar el device code desde móvil.
   Es el patrón que Netflix / Spotify / YouTube TV usan.

4. **Versionado del API + documentación OpenAPI** (~½ día).
   Hoy todo cuelga de `/api/v1/*` pero no hay un OpenAPI spec
   versionado. Generar con go-swagger o swag (anotaciones en
   handlers). Sin esto, la app Kotlin va a redescubrir el wire
   format por trial-and-error.

5. ~~**SSE para progress sync**~~ ✅ **shipped 2026-04-29** (`94cb74b`).
   `GET /api/v1/me/events` con filtrado per-user `BEFORE` channel write.
   Tres event types: `user.progress.updated`, `user.played.toggled`,
   `user.favorite.toggled`. Frontend `useUserDataSync` montado en
   `AppLayout`. Mismo framing que `/events` para que la app Kotlin TV
   reuse `okhttp-sse` igual que el web reusa `EventSource`.

### **P2 · Quick wins paralelos a la app**

6. **Blurhash en `<PosterCard>`** (30 min). Backend lo emite hace
   meses; frontend nunca lo consume. Mejora drástica del LCP
   percibido. *También aplica a la app TV* (Compose puede renderizar
   blurhash).

7. **WebP blurhash backend** (1h). Logos de Fanart vienen en WebP
   → blurhash vacío. Importar `golang.org/x/image/webp`.

8. **Provider priority configurable** (½ día). Campo ya existe
   en DB; falta UI admin (drag-to-reorder).

9. **Person/cast clickable** (~1 día). Página de actor con
   filmografía. La app TV lo va a querer también.

### **P3 · Diferenciadores estratégicos** (semanas, no días)

10. **fsnotify watcher + priority enrichment queue** (~1 semana).
    Scan que NO bloquea visibilidad. Items aparecen instantáneo,
    metadata progresivamente. La app TV se siente snappy desde el
    primer minuto del primer scan.

11. **Multi-version del mismo título** (4K + 1080p agrupados).
    Schema lo soporta; UI/scanner no agrupan. Ventaja sobre
    Jellyfin para usuarios que ripean en múltiples calidades.

12. **Intro skip detection** (audio fingerprinting cross-episode).
    ~2 semanas. Feature signature. La app TV la hace VISIBLE
    (botón flotante "skip intro" tipo Netflix).

13. **Privacy stack** (modo offline NFO, egress allowlist, CSP
    estricto). Diferenciador de mercado real.

14. **Watch-together** (sync sessions WebSocket). Plex lo tiene
    roto en web; tu lo haces bien para web Y app TV.

### **Lo que NO está en la lista**

- **Family/kids profiles** — excluido por usuario.
- **Modo offline 100% para web** — excluido por usuario.
- **Apps iOS/Android nativas** — Android TV es el target; móvil
  puede esperar a PWA + Cast.
- **AI metadata** — anti-marca self-hosted.

---


---

## 📦 Sesiones archivadas

Para mantener este fichero ligero (entrypoint de cada sesión nueva),
las sesiones anteriores a 2026-04-27 viven en
[`archive/2026-pre-04-28.md`](archive/2026-pre-04-28.md). Incluye los
HANDOFFs antiguos, ciclos previos (live-TV coverage, simplify sweep,
lint debt cero, sqlc sweep) y el resumen ejecutivo histórico. Sólo
abrir cuando haga falta arqueología puntual sobre una decisión vieja.
