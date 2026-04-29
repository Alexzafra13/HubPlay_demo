# Estado del proyecto

> Snapshot: **2026-04-29 día → tarde → noche, dos sesiones encadenadas de detail-UX premium** — bloque A `015ThedLMwhsx5ittdmtxSN4` (8 commits del "premium detail UX" pass: cast/crew end-to-end con fotos, IMDb/TMDb deep links, weekly image refresh scheduler, watched-count agregado, hero treatments) y bloque B `claude/review-project-resume-3V6Mr` (4 commits unificando hero movies↔series + aurora ambiental PS3-XMB como page canvas + fixes iterativos sobre fotos del usuario). Foundation lista para `/people/{id}` (SQL `ListFilmographyByPerson` + repo, sin handler aún). **tests: backend verde · frontend 336/336 · tsc clean**.

Trabajo activo en `claude/review-project-resume-3V6Mr`:
- `39698ce` ui(detail): unify movie hero with series layout + Plex-style cast strip
- `99fd307` ui(detail): PS3-XMB ambient aurora as page canvas
- `7000ec9` ui(detail): make ambient aurora actually visible + smoother hero seam
- `10afab3` ui(detail): sharp hero backdrop, no clipped Play button, vibrant aurora, fix duplicate "Sigue viendo"

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

## 🛡️ Sesión 2026-04-28 noche → 2026-04-29 (IPTV hardening completo) — 7 commits

Empieza con un usuario reportando "se corta" en su provider Xtream y termina con todo el flujo IPTV listo para producción real. Ningún commit es un feature nuevo grande — son los pequeños refinamientos que separan "demo técnica" de "producto self-hosted".

### Commits en orden

1. **`2be58eb` — Buffer tuning para Xtream**. Real-traffic trace mostró bursty downloads + skip ocasional sobre la ventana del manifest. ffmpeg: `hls_time 4→2`, `hls_list_size 6→10`, `+rtbufsize 50M`, `+max_delay 5000000`. hls.js: `maxBufferLength 30→60`, `maxMaxBufferLength 60→120`, `liveSyncDurationCount 3→4`, `liveMaxLatencyDurationCount 6→12`. Sólo más generoso, no cambio de régimen.

2. **`357a334` — Recovery production-grade**. Logs post-tuning mostraban gaps de ~10s sin requests del browser → text-book Chrome/Firefox background-tab throttling. Cambios:
   - **Visibility API**: `document.addEventListener("visibilitychange")` → en hidden, `hls.stopLoad()`; en visible, `hls.startLoad(-1)` (resume desde live edge). Elimina el síntoma raíz, no lo recupera.
   - **`maxLiveSyncPlaybackRate: 1.5`**: cuando hls.js detecta retraso, acelera 1.5× en vez de saltar. Imperceptible al oído.
   - **`hls_list_size 10→20` + `hls_delete_threshold 5`**: 40s de ventana absorbe el stall.
   - **`+temp_file` en hls_flags**: rename atómico — Go's `http.ServeFile` no lockea, sin esto serviría segmentos parciales.
   - **`nudgeMaxRetry 3→10`**: más reintentos antes de rendirse en buffer atascado.

3. **`1bdac6e` — Circuit breaker + stderr + VLC UA** *(commit del usuario)*. Tres mejoras de fiabilidad después de probar contra un provider muy flaky:
   - Circuit breaker compartido entre proxy y transmux planes vía `ChannelGate`. Antes el transmux no tenía breaker y un canal muerto producía fork-bomb de ffmpegs.
   - Captura ring-buffered de stderr (64 líneas) por sesión.
   - `User-Agent: VLC/3.0.20` consistente con el prober (muchos paneles Xtream filtran por UA).

4. **`ce9f51a` — Re-encode fallback automático + métricas Prometheus** *(commit del usuario)*. Codec rescue para upstreams que `-c copy` no traga (HEVC main10, AC3 raros):
   - `decodeMode` enum (`direct` / `reencode`) con TTL de 1 hora. Self-healing: tras el TTL retorna a `direct` automáticamente.
   - `looksLikeCodecError()` clasifica stderr — diferenciación real, no por exit code.
   - `EnableReencodeFallback` opt-in (default true) por config.
   - `TransmuxMetrics` interface (sink pattern): `IncStarts(outcome)`, `IncDecodeMode(mode)`, `IncReencodePromotions()`.

5. **`1a5f2a2` — Proxy + caché de logos de canales**. Usuario reportó violaciones CSP en consola: M3U logos desde hosts random (`lo1.in`, `i.imgur.com`, `i.ibb.co`...) y `img-src` sólo permitía `tmdb` + `fanart`.
   - `internal/iptv/logo_cache.go`: cache content-addressed (`sha256[:16]`) bajo `<cache_dir>/iptv-logos/`. Reusa `imaging.SafeGet` (SSRF + size + timeout). Sniff por `http.DetectContentType` rechaza HTML mis-tagged.
   - `GET /api/v1/channels/{id}/logo` ACL-checked + `Cache-Control: public, max-age=86400`. 404 → frontend cae al avatar de iniciales por `onError` existente.
   - `iptv_dto.logoProxyURL(ch)`: la URL en el DTO pasa a same-origin. CSP queda restrictivo, hosts externos dejan de trackear.

6. **`12867fb` — Hardware-accel para el reencode fallback**. `ce9f51a` shipeó con `libx264` software. Tienes `internal/stream/hwaccel.go` ya con detección VAAPI/NVENC/QSV/VideoToolbox para VOD. Wiring:
   - `stream.HWAccelInputArgs` exportado.
   - `TransmuxManagerConfig` gana `ReencodeEncoder` + `ReencodeHWAccelInputArgs` (rellenados desde `streamManager.HWAccelInfo()`).
   - `encoderTuningArgs(encoder)` con flags específicos: libx264 (veryfast/zerolatency), h264_nvenc (p4/ll/cbr), h264_vaapi (quality 4), h264_qsv (veryfast/look_ahead 0), h264_videotoolbox (allow_sw 0/realtime 1).
   - **`MaxReencodeSessions` cap separado** del global (default `MaxSessions/2`, floor 1).
   - 5-10× CPU saving en HEVC→H.264 cuando hwaccel disponible.

7. **`d470a34` — Cross-library EPG matching**. Real data del usuario: 1.073 canales movistar sin EPG (0 sources configuradas), pero matchearían contra la fuente XMLTV configurada en Cat TV. Cobertura visible ~26%; con cross-library debería subir a ~70%.
   - `ChannelRepository.ListLivetvChannels()` (raw SQL, JOIN libraries WHERE content_type='livetv').
   - `service_epg.go RefreshEPG`: el `channelIndex` se construye desde TODOS los canales livetv del instance, no sólo la lib del refresh.
   - Persistencia inalterada: `ReplaceForChannel` sólo dispara para canales matched → libs no matched conservan su EPG previa.
   - Logs: `channels_matched_by_lib` (map), `channels_matched_cross_library` (count fuera de la lib source).
   - 0 colisiones tvg-id cross-library en datos del usuario verificadas → cambio safe.

### Estado funcional al cerrar la sesión

- **Reproducción IPTV TS**: funcionando en provider real (49+ segmentos servidos sin errores en pruebas).
- **Recovery frente a stalls**: cubre background-tab + jitter del provider.
- **Codec rescue**: HEVC/AC3 entran en reencode automáticamente, con hwaccel cuando esté.
- **CSP**: queda restrictivo; logos servidos same-origin.
- **EPG coverage**: cross-library matching activo; un click en "Refrescar EPG" en Cat TV cubre movistar también.
- **Observabilidad**: stderr capturado, métricas por outcome / decode_mode / reencode promotions.

---

## 📡 Sesión 2026-04-28 tarde (IPTV transmux + import event-driven) — 2 commits

Sesión de tarde, dos problemas reales detectados al probar la lib
`tv - movistar` (Xtream M3U_PLUS de 2.6M líneas → 1.073 canales TV en
vivo) en `pdmkibyg.xiagdns.com`:

1. **Tras importar, los canales no aparecían en LiveTV.** El usuario tenía
   que recargar manualmente.
2. **Al pinchar "Reproducir" en cualquier canal de esa lib, el player
   se quedaba en spinner / fallaba.**

### Diagnóstico

#### Problema 1 — Import event drop

`useRefreshM3U` (web/src/api/hooks/iptv-admin.ts) abre un EventSource
local antes del POST y resuelve con el evento SSE `playlist.refreshed`.
Si el modal o componente que lo dispara se desmonta antes de que termine
el import (88s en una lista grande), el cleanup cierra el EventSource
y el `invalidateQueries` jamás se ejecuta. Con la lista de movistar:

```
17:49:04  POST /iptv/refresh-m3u → 202 (import async arranca)
17:49:35  SSE client desconecta (usuario navega a LiveTV)
17:49:31, 37, 44  3 reintentos manuales → 500 (refresh in progress)
17:50:32  M3U parse complete channels=1073 + emite playlist.refreshed
17:50:50  Pero ya nadie escucha el evento → cache stale
```

Tres bugs concatenados:

- **`ErrRefreshInProgress` mapeado a 500** — caía en el default de
  `handleServiceError` porque no es `*domain.AppError`. Cliente lo
  trataba como error real, mostraba toast.
- **Mutación rechazaba en 500** — el reintento manual del usuario
  rompía la mutación inicial que estaba esperando el SSE.
- **Cero listener global** — `playlist.refreshed` solo era escuchado
  por la mutación local. Sin mutación viva, evento perdido. Tampoco
  invalidaba caches el flujo "scheduler corre refresh sin UI".

#### Problema 2 — MPEG-TS sobre HTTP no es reproducible en navegador

Las dos libs guardan formatos distintos en `channels.stream_url`:

```
Cat TV (funciona):  https://.../master.m3u8        ← HLS, hls.js OK
movistar (falla):   http://.../N83HL55L/.../30935  ← MPEG-TS crudo ❌
```

`internal/iptv/proxy.go` ya detecta HLS (Content-Type, sufijo URL,
sniff) y reescribe la playlist correctamente. Para todo lo demás cae
en `pipeStream` — passthrough binario. El navegador recibe MPEG-TS
crudo, que ni `hls.js` ni `<video>` nativo pueden decodificar:

```
proxy 200 OK bytes=17786364 → context canceled (player se rinde)
POST /playback-failure (beacon de error del cliente)
```

Es exactamente lo que Jellyfin/Plex/Tvheadend resuelven con un
transmux ffmpeg en tiempo real (`-c copy -f hls`).

### Solución 1 — Import event-driven robusto

**Backend** (`internal/api/handlers/iptv_admin.go:78`):

`ErrRefreshInProgress` deja de pasar por `handleServiceError`. El
handler responde directamente:

```go
if errors.Is(err, iptv.ErrRefreshInProgress) {
    respondJSON(w, http.StatusConflict, map[string]any{
        "data": map[string]any{
            "library_id": libraryID,
            "status":     "in_progress",
        },
    })
    return
}
```

**Frontend mutación** (`web/src/api/hooks/iptv-admin.ts:166`):

El 409 deja de propagarse como error. La mutación sigue escuchando
SSE como si nada — la lógica es "alguien (yo, otra pestaña, el
scheduler) ya está importando esto, espera el mismo evento":

```ts
api.refreshM3U(libraryId).catch((err) => {
  if (settled) return;
  if (err instanceof ApiError && err.status === 409) return; // join
  cleanup();
  reject(err);
});
```

**Frontend listener global** (nuevo
`web/src/hooks/usePlaylistRefreshEvents.ts` montado en
`web/src/components/layout/AppLayout.tsx`):

```ts
useEventStream("playlist.refreshed", (raw) => {
  const libId = JSON.parse(raw).data?.library_id;
  qc.invalidateQueries({ queryKey: queryKeys.channels(libId) });
  qc.invalidateQueries({ queryKey: queryKeys.libraries });
  qc.invalidateQueries({ queryKey: queryKeys.unhealthyChannels(libId) });
  qc.invalidateQueries({ queryKey: queryKeys.channelsWithoutEPG(libId) });
  qc.invalidateQueries({ queryKey: queryKeys.libraryEPGSources(libId) });
  qc.invalidateQueries({ queryKey: ["bulk-schedule"] });
});
```

Mismo tratamiento para `playlist.refresh_failed` (log devtools, no
invalidar). El listener vive mientras el usuario está autenticado;
unauthenticated `/login` y `/setup` no lo necesitan.

### Solución 2 — Live MPEG-TS → HLS transmux

Pieza nueva `internal/iptv/transmux.go` (`TransmuxManager` +
`TransmuxSession`). Decisiones de diseño:

- **1 sesión por canal compartida**. Cinco usuarios mismo canal = 1
  ffmpeg, 1 conexión upstream. Indexado por `channel_id`. Crítico
  porque providers Xtream rate-limitan por cuenta.
- **Lazy spawn, idle reap**. Sesión arranca al primer GET de manifest;
  reaper la mata tras `IdleTimeout` sin segment requests.
- **Bounded**. `MaxSessions` cap por config. 503 con `Retry-After` si
  se llega.
- **Ready signal**. `GetOrStart` bloquea hasta que ffmpeg escribe el
  primer segmento (timeout `ReadyTimeout`). Sin esto, el primer GET
  del player encontraría manifest vacío.
- **Reaper salta sesiones aún arrancando** (sin segment todavía) — un
  bug detectado por test, sin esto el reaper killaba spawns lentos.
- **Failure isolation**. ffmpeg crash → session evicta del map; el
  siguiente GET re-spawnea. No se hace retry-loop interno; se delega
  al circuit breaker existente del proxy.

ffmpeg argv (`buildTransmuxFFmpegArgs`):

```
-fflags +genpts+discardcorrupt
-reconnect 1 -reconnect_at_eof 1 -reconnect_streamed 1 -reconnect_delay_max 5
-rw_timeout 10000000           # 10s I/O timeout
-i {upstream}
-map 0:v:0 -map 0:a:0?
-c copy                        # transmux puro, no re-encode
-bsf:v h264_mp4toannexb        # AnnexB para HLS
-f hls -hls_time 4 -hls_list_size 6
-hls_flags delete_segments+independent_segments+omit_endlist+program_date_time
-hls_segment_filename {workDir}/seg-%05d.ts
{workDir}/index.m3u8
```

**Dispatch** en `iptv_channels.go:Stream`:

```go
if h.transmux != nil && !iptv.IsHLSURL(ch.StreamURL) {
    http.Redirect(w, r, "/api/v1/channels/"+channelID+"/hls/index.m3u8", 302)
    return
}
// ... else passthrough proxy ...
```

`isHLSURL` se exporta como `IsHLSURL` para reuso desde el handler sin
mover lógica.

**Endpoints nuevos** (en `/api/v1/channels/{id}/`):

- `GET /hls/index.m3u8` → arranca/obtiene sesión, espera primer segmento,
  sirve manifest con `Cache-Control: no-cache`.
- `GET /hls/{segment}` → valida nombre contra regex `seg-\d{5,6}\.ts`
  (path traversal guard), `Touch()` la sesión, sirve fichero con
  `Content-Type: video/mp2t`.

Códigos de estado mapeados:

- `200` — manifest/segmento OK.
- `404 NO_TRANSMUX_SESSION` — sesión expirada (player recarga manifest →
  respawn → reproducción se resume).
- `502 TRANSMUX_FAILED` — ffmpeg muere antes de producir segmento
  (canal muerto / codec no copy-compatible).
- `503 TRANSMUX_BUSY + Retry-After: 5` — `MaxSessions` alcanzado.
- `400 INVALID_SEGMENT` — nombre de segmento no matchea regex.
- `501 TRANSMUX_DISABLED` — opt-out por config.

**Config nueva** (`internal/config/config.go`):

```yaml
iptv:
  transmux:
    enabled: true                # default
    max_sessions: 10
    idle_timeout: 30s
    ready_timeout: 15s
```

Cache dir = `<streaming.cache_dir>/iptv-hls/<channel-id>/`. Reusa el
volumen de transcode VOD para no inventar otro mount point.

**Wiring** (`cmd/hubplay/main.go`):

```go
if cfg.IPTV.Transmux.Enabled {
    iptvTransmux = iptv.NewTransmuxManager(...)
}
// ...
deps.IPTVTransmux = iptvTransmux  // nil-safe en handler
// shutdown:
if iptvTransmux != nil { iptvTransmux.Shutdown() }
```

### Tests

11 nuevos en `internal/iptv/transmux_test.go` con un fake-ffmpeg
shell shim (modos `ok` / `noseg` / `crash`). Cubre:

- Spawn + ready signal en upstream sano.
- **Coalescing**: 5 callers concurrentes mismo canal = 1 PID ffmpeg.
- `MaxSessions` cap → `ErrTooManySessions`.
- Idle reaper mata sesión sin Touch.
- Touch periódico mantiene viva.
- ffmpeg crash → `ErrTransmuxFailed` + evict.
- No-segment timeout → `ErrTransmuxFailed`.
- `Shutdown` idempotente, mata todo.
- Validación nombre de segmento (path traversal).
- ffmpeg argv contiene flags críticos.

Test de regresión backend tightened: `RefreshM3U_AlreadyInProgress`
ahora afirma 409 + body estructurado, no solo "no es 202".

### Verificación en producción

Container `hubplay-dev` reconstruido y probado contra la lib movistar
real. Logs después de pulsar Reproducir en un canal:

```
seg-00021.ts ... seg-00049.ts → todos 200 OK, 2-3 MB/seg
manifest cada 2s → 200 OK, 634 bytes
0 errores, 0 "context canceled", 0 "stream proxy error"
```

49 segmentos consecutivos servidos = ~3 minutos de TV en directo
sostenida. Reproductor sin spinner.

### Limitaciones conocidas

- **`-c copy` requiere codec compatible**. Providers Xtream casi
  siempre sirven H.264/AAC, pero un canal con codec exótico (HEVC sin
  HLS, AC-3, etc.) fallaría con `TRANSMUX_FAILED`. Solución futura:
  detectar el error de ffmpeg en stderr y fallback a re-encode con
  el `internal/stream/hwaccel` ya existente. No urgente; añadir cuando
  surja un canal real que falle.
- **`MaxSessions = 10` puede ser bajo** para Plex/Jellyfin
  multi-usuario. Operador puede subirlo en config. Por canal, no
  multiusuario, así que 10 ≈ 10 canales distintos simultáneos.
- **Frontend no muestra estado "spawning"**: el primer GET del
  manifest puede tardar 3-5s en responder. Hoy se ve como spinner
  normal de carga; aceptable para v1.
- **Sin métricas**. `iptv_transmux_active`, `iptv_transmux_spawn_total`
  serían útiles en grafana. Añadir si surge necesidad operativa.

### Archivos tocados

```
M  cmd/hubplay/main.go                          (wire + shutdown)
M  internal/api/handlers/interfaces.go          (IPTVTransmuxer)
M  internal/api/handlers/iptv.go                (handler field)
M  internal/api/handlers/iptv_admin.go          (409 estructurado)
M  internal/api/handlers/iptv_channels.go       (Stream redirect + HLSManifest + HLSSegment)
M  internal/api/handlers/iptv_test.go           (assertion 409 + nil transmux)
M  internal/api/router.go                       (Dependencies + routes)
M  internal/config/config.go                    (IPTVConfig)
M  internal/iptv/proxy.go                       (export IsHLSURL)
A  internal/iptv/transmux.go                    (manager + session, ~530 LOC)
A  internal/iptv/transmux_test.go               (11 tests, fake-ffmpeg shim)
M  web/src/api/hooks/iptv-admin.ts              (tolera 409)
M  web/src/components/layout/AppLayout.tsx      (mount listener)
A  web/src/hooks/usePlaylistRefreshEvents.ts    (listener SSE global)
M  .gitignore                                   (cache/, web/.pnpm-store/)
```

---

## 🛰️ Sesión 2026-04-28 (M3U import async) — 1 commit

Caso real reportado por el usuario al probar en producción la rama
`huge-list resilience` sobre su provider `pdmkibyg.xiagdns.com`. Logs:

```
17:11:36  refreshing M3U playlist library=43115037-…
17:12:36  M3U parse truncated; importing what we got lines=97988
          channels=1073 vod_skipped=23331 language_skipped=21821
          error="reading M3U at line 97988: context canceled"
17:12:36  ERROR replace channels: begin tx: context canceled
17:12:36  POST /…/iptv/refresh-m3u → 500   duration_ms=60003
```

### Diagnóstico

Cadena causal exacta:

1. La lista filtra a 1.073 canales después de descartar 23.331 VOD +
   21.821 idiomas no-`es` (filtros que vienen de `5bf1ba7`). Aun así,
   bajar el body de 331 MB + parsear streaming pasa de los 60s.
2. **`deploy/nginx/hubplay.conf:142`** (`location /`) no override-aba
   `proxy_read_timeout` → default nginx 60s. A los 60s exactos
   (`duration_ms=60003`) nginx cierra la conexión upstream.
3. El `http.Server` cancela `r.Context()`. El `bufio.Scanner` dentro
   de `ParseM3UStream` aborta a media línea con `context canceled`,
   pero **los 1.073 canales ya están acumulados en `dbChannels`**.
4. El código tolerante de `service_m3u.go:111` (≥50 canales =
   "importamos lo que tengamos") pasa al siguiente paso.
5. `s.channels.ReplaceForLibrary(ctx, …)` llama a
   `r.db.BeginTx(ctx, nil)` con el **mismo ctx ya muerto**. El begin
   falla en seco → handler devuelve 500 → **0 canales en DB**.

Segundo disparador idéntico aunque amplíes nginx: si el cliente
cierra la pestaña a media importación, `r.Context()` también cancela
y el `BeginTx` se cae igual.

### Fix (`ad6b61f`) — 9 ficheros, +396 / −29

**Backend — split del refresh en lock + work + signal**

- `internal/iptv/service_m3u.go`: nuevos
  `Service.TryAcquireRefresh(libraryID) (release func(), error)` y
  `Service.RunRefreshM3U(ctx, libraryID) (int, error)`. El antiguo
  `RefreshM3U(ctx, libraryID)` ahora es wrapper síncrono (mantiene
  compat con scheduler + import público); el handler usa los dos
  primitivos por separado.
- `Service.PublishRefreshFailed(libraryID, err)` emite el nuevo
  evento `playlist.refresh_failed` para que la SSE entregue el
  desenlace cuando la goroutine no es el request.
- `internal/event/bus.go`: constante
  `PlaylistRefreshFailed Type = "playlist.refresh_failed"`.

**Backend — handler 202**

- `internal/api/handlers/iptv_admin.go RefreshM3U`:
  1. Adquiere el slot síncronamente con `TryAcquireRefresh` (lock
     ocupado → 409 inmediato, sin TOCTOU).
  2. Lanza goroutine: `ctx, cancel := context.WithTimeout(
     context.Background(), 10 * time.Minute)`. Llama a
     `RunRefreshM3U`. Si falla, `PublishRefreshFailed`.
  3. Responde **202 Accepted** con `{ library_id, status: "started" }`.
- El timeout de 10 min se eligió contra el ceiling real de Xtream
  M3U_PLUS (~2 min de fetch + parse + tx); margen para upstreams
  degradados sin que un fetch colgado bloquee el slot per-library
  para siempre.

**Backend — SSE keepalive**

- `internal/api/handlers/events.go`: ticker de **25s** envía `: ping\n\n`
  (comment frame, invisible al EventSource API, resetea el idle
  timer del proxy). nginx default = 60s; 25s deja margen frente a
  cutoffs de 30s también. Sin esto el SSE moría cada 60s y el browser
  reconectaba perdiendo eventos en la ventana de gap.

**Frontend — la mutation espera al evento**

- `web/src/api/hooks/iptv-admin.ts useRefreshM3U`: la `mutationFn`
  abre `EventSource("/api/v1/events")` ANTES del POST (evita perder
  el evento si el import es muy rápido), lanza
  `api.refreshM3U(libraryId)` (202), y devuelve la promise que
  resuelve con `{ channels_imported }` cuando llega
  `playlist.refreshed` con `library_id` matching, o rechaza con el
  mensaje del backend cuando llega `playlist.refresh_failed`.
  Timeout local: **11 min** (1 min más que el backend, así un fallo
  limpio gana la carrera al timeout local).
- Cleanup paranoico: `removeEventListener` + `source.close()` en
  resolve / reject / timeout / `finally` defensivo.
- **El spinner del `LibraryCard` ahora refleja progreso real** —
  `isPending` queda `true` durante todo el import, no se apaga al
  202.

**Nginx — defensa en profundidad**

- `deploy/nginx/hubplay.conf`: `proxy_read_timeout 5m` y
  `proxy_send_timeout 5m` añadidos al `location /`. No bloqueante
  (el SSE keepalive cubre el caso); útil si en el futuro alguien
  monta un endpoint largo sin async.

### Tests

- 3 tests nuevos en `iptv_test.go`:
  - `Returns202AndRunsAsync` — happy path: 202 inmediato, goroutine
    invoca `RunRefreshM3U` con el library_id correcto, sin
    publicación de fallo.
  - `AlreadyInProgress_Returns409` — `TryAcquireRefresh` devuelve
    `ErrRefreshInProgress`; ninguna goroutine arranca.
  - `AsyncFailure_PublishesEvent` — `RunRefreshM3U` falla; espera
    a que `PublishRefreshFailed` se llame con el error
    forwardeado.
- Suite Go completa verde con `-race`. Frontend 296/296.

### Notas de diseño

- **Por qué no aumentar solo nginx**: aunque ampliar
  `proxy_read_timeout` a 10m hubiera enmascarado el síntoma del
  timeout, el fix no resuelve el segundo disparador (cliente cierra
  pestaña). Detachar el contexto era inevitable. Una vez detachado
  el ctx, devolver 202 es gratis y mejor UX.
- **Por qué reusar el event bus + SSE existente** en vez de un
  endpoint de polling: ya hay infraestructura SSE (`useEventStream`
  en frontend, `event.Bus` en backend) para `channel.health.changed`
  y `library.scan.*`. Añadir un evento más cuesta una constante.
- **`RefreshM3U` síncrono se mantiene** porque scheduler
  (`internal/iptv/scheduler.go`) y `ImportPublicIPTV` ya viven en
  goroutines suyas con su propio ctx; no pagan el coste del split.
- TOCTOU evitado: el lock se adquiere síncronamente en el handler,
  la goroutine sólo lo libera. Dos clicks rápidos no producen dos
  goroutines.

---

## 🛰️ Sesión 2026-04-28 (huge-list resilience) — 4 commits

Sesión de bloque "que las listas IPTV de 20k+ canales no rompan
HubPlay". Se persiguió un caso real (lista xiagdns 331 MB con
~20k canales que no cargaba), y por el camino se detectaron y
arreglaron varios huecos de robustez/UX que afectan a TODO
operador con un provider IPTV serio.

### Investigación previa al código

Se comparó HubPlay contra Plex / Jellyfin / Threadfin / xTeVe /
Tuliprox / Dispatcharr (vía web research) para no reinventar peor
de lo que ya existe. Hallazgos relevantes:

- **Plex** tiene límite hardcoded de **~480 canales**. Por encima
  exige filtrar.
- **Jellyfin** no filtra nada nativo (issue #835 abierto desde 2017);
  solución oficial = poner Threadfin delante.
- **Threadfin/xTeVe** filtra por group-title regex y poco más.
- **Tuliprox** tiene una DSL boolean expressiva — más potente, más
  curva de aprendizaje. **No copiada** porque 95% de usuarios solo
  quieren "español".

Conclusión: HubPlay puede plantarse cómodamente al nivel de
Threadfin (mejor en filtro de idioma porque combinamos 4 señales)
sin la complejidad de Tuliprox.

### Lo que entró

**`eba4f82`** — **Circuit breaker per-canal en StreamProxy**.
Sin breaker, una CDN caída + 100 viewers = 100 retry loops en
paralelo machacando upstream muerto. Ahora: 5 fallos consecutivos
por canal → open 30s; siguientes peticiones 503 + Retry-After SIN
tocar la red. Half-open admite UN trial; éxito cierra, fallo
re-abre con cooldown × 2 (cap 5 min). Trial-timeout (30s) protege
contra probes que no resuelven. Key: `channelID` (descartadas
per-URL = explosión en segments, per-host = injusto en CDN
compartida). Memoria acotada vía `Prune()` (closed + 0 fallos +
10 min idle). Tests: `circuit_breaker_test.go` (9) +
`proxy_breaker_test.go` (4) con httptest end-to-end.

**`5bf1ba7`** — **Filtro de idioma al import M3U**.
- Migración 016: columna `libraries.language_filter TEXT` (CSV de
  ISO 639-1, vacío = no filtro = comportamiento histórico).
- `iptv.MatchesLanguageFilter` cascada de 4 heurísticas en orden de
  fiabilidad:
  1. `tvg-language` (ISO o nombre humano "Spanish")
  2. `tvg-country` → idioma dominante (mx/ar/co/cl/...→ es;
     us/uk/au/ie → en) — multi-language countries (ch/ca/be) NO
     en la tabla (deny only when sure).
  3. `group-title` keyword anclado a límites de palabra (sin falsos
     positivos en "best" de "best of HD")
  4. Prefijo en nombre: `[ES]` / `(en)` / `ES |` / `DE -` / `pt:`
- Regla "no signal → allow" (no se descarta lo que no se puede
  clasificar — feeds sin metadata pasan).
- UI: chip multi-select de 12 idiomas comunes + free-text (cualquier
  2-3 letras ISO).
- 16 unit tests cubriendo cada heurística + cascada + sin-señal +
  multi-allow.

**`d16efc4`** — **Toggle `tls_insecure` per-library**.
- Migración 017: columna `libraries.tls_insecure INTEGER` (0/1, off
  por defecto).
- IPTV providers en el wild (sobre todo Spain con LaLiga/Movistar
  cambiando hosts) cuelgan certs caducados / self-signed. Strict
  Go client refusa → "no carga" sin recurso obvio.
- HTTP client lazy + cached (`Service.insecureFetchClient`) con
  `TLSClientConfig.InsecureSkipVerify=true`. Per-fetch: el flag
  llega como param a `fetchURL`. **El stream proxy mantiene
  verificación estricta** — clients confían en HubPlay para
  servir bytes verificados; weakening ahí sería peor que no tener
  toggle.
- Backend logea `Warn` en cada fetch con flag activo (auditable).
- Frontend: card amarillo cuando active + warning text que cambia
  según estado (off "actívalo solo si confías…", on "⚠ las
  descargas aceptarán cualquier cert…").
- 4 tests con `httptest.NewTLSServer` (cert auto-firmado) que
  verifican: strict default falla, flag ON pasa, toggle en runtime
  threading correcto, cached client estable + strict NO tiene
  InsecureSkipVerify.

**`<preflight>`** — **Preflight check al guardar library**.
- Caso real: el provider del usuario tarda 60-90s en empezar a
  enviar bytes (genera M3U de 331 MB on-demand). HubPlay aguanta
  5min → import funciona, pero la UI da spinner silencioso 3-5
  min. Indistinguible de "está roto".
- `Service.PreflightCheck(ctx, url, tlsInsecure)` con budget de
  12s. Devuelve verdict tipado:
  - `ok` — HTTP 200 + body shape M3U-like
  - `slow` — TCP conecta pero no responde en budget (típico del
    caso del usuario; mensaje: "guarda y espera, el import tiene
    timeout 5 min")
  - `html` — el provider devolvió HTML (cuenta suspendida, IP
    bloqueada por LaLiga, captive portal)
  - `auth` — 401/403
  - `not_found` — 404
  - `tls` — cert error (sugiere activar tls_insecure)
  - `dns` — host no resuelve (URL mala o ISP bloqueando)
  - `connect` — TCP refused
  - `empty` — 200 OK pero body vacío
  - `invalid_url` / `unknown` — catch-all
- Body sniff: lee primeros 4 KB tolerante a `io.ErrUnexpectedEOF`
  (algunos providers mienten en Content-Length).
- Endpoint: `POST /api/v1/iptv/preflight` admin-gated (request
  body es URL arbitraria → SSRF-adjacent si fuera público).
- UI: botón "Probar conexión" en LibraryFormModal + LibraryEditModal.
  Verdict inline con tono colour-coded (verde/amarillo/rojo) +
  HTTP status + bytes anunciados + body hint truncado.
- 11 tests (`preflight_test.go`) cubriendo cada verdict.

### Métricas

- **+~1500 líneas Go** (backend) + **+~600 líneas TSX** (frontend)
- Tests nuevos: **40 backend** (9+4 breaker, 16 lang, 4 TLS, 11
  preflight) + frontend 296/296 estable.
- 4 commits. Branch: `claude/iptv-circuit-breaker` → main.

### Caso del usuario (cierre del bloque)

El provider `pdmkibyg.xiagdns.com`:
- DNS OK, TCP OK
- `/get.php?type=m3u_plus` **SÍ funciona** pero tarda ~90s en
  primer byte y devuelve **331 MB**
- `/get.php?type=m3u` (basic) cuelga indefinidamente
- `player_api.php` responde rápido con JSON (categorías
  `VIP / AR / EN / ES...`)

Workflow recomendado al usuario: pegar URL `m3u_plus`, activar
filtro de idioma `["es"]` (su provider tiene group-titles
`AR / EN / VIP / etc.` muy estructuradas → matcher acierta perfecto),
darle a "Probar conexión" → verdict será `slow` con mensaje
"guarda y espera 1-2 min", clickar Crear, esperar el import. Sin
sorpresas.

### Lo que NO entró (deuda explicada)

- **Single-flight EPG fetches**: la pieza B de la tarea original
  del review. Diferida porque el streaming-XMLTV parser ya bajó
  el pico de memoria de 250 MB a ~10-20 MB; compartir body entre
  dos refreshes simultáneos exige bufferear o tempfile spool, lo
  que devuelve la deuda. El lock per-library ya cubre el caso "la
  misma library refrescada dos veces"; el caso restante "dos libs
  con la misma URL EPG" es bajo y se difiere.
- **Xtream API nativo** (`player_api.php` paginado): considerado
  pero descartado para esta sesión. El provider del usuario SÍ
  sirve M3U_plus, así que el path actual le sirve. Sería slice 2
  cuando aparezca un provider que SOLO tenga `player_api.php`.
- **Frontend virtualization** (la lista de canales con .map de
  20k entradas que choca al render): conocido, en TODO en
  `EPGGrid.tsx:26`. Mitigado por el filtro de idioma (de 20k →
  3-5k típico). Slice independiente — irá con `@tanstack/react-virtual`.

---

## 🔎 Sesión 2026-04-28 (peer-review followups) — `7e32c41`

Pase de peer-review senior sobre el refactor SRP de la rama. De
ocho findings, **cinco** se aceptaron y se ejecutaron; los otros
tres (useFavoriteMutation "over-generalised", usePlayback
interface "too wide", LibraryCard mutation hooks) los marcó el
propio reviewer como "defensible" — quedan as-is.

### Lo que entró (1 commit, 10 ficheros, +385 / −206)

1. **`librariesAdmin/constants.ts` → granularity**: mezclaba
   tablas de datos (catálogos iptv-org) con helpers React-coupled
   (`originLabel`/`originTitle`/`scanStatusVariant`). Helpers
   movidos a `helpers.ts` sibling. `constants.ts` ahora **pure
   data**, `helpers.ts` **pure functions** — un futuro port que
   sólo quiera los slugs no tiene que leer past tres funciones de
   UI.

2. **`hooks/iptv-admin.ts` (238 líneas, el mayor) → split en 3
   sub-dominios** bajo el mismo barrel:
   - `iptv-admin.ts` — refresh + public-IPTV import
   - `iptv-sources.ts` — EPG source CRUD + catalogue
   - `iptv-jobs.ts` — scheduled M3U/EPG jobs

   `hooks.ts` re-exporta los 3 → **0 call-sites tocados**.

3. **`hooks/channels.ts` — comentario inline de intencionalidad**:
   el resto de los per-domain files separan queries de mutations,
   pero éste las junta a propósito (las mutations invalidan
   `queryKeys.channelFavorite{IDs,s}` definidas en el mismo
   fichero, y `useFavoriteMutation` toca la cache de IDs
   directamente). Documentado para que el próximo lector no
   "arregle" lo que no está roto.

4. **`persistManualImage` — test unitario directo**: el helper
   que respalda Select + Upload sólo se ejercitaba vía HTTP-level
   integration tests. Nuevo test en `image_test.go` (+65 líneas)
   ancla los **9 pasos** (file on disk, IsLocked, SetPrimary
   promotion, blurhash, pathmap entry, response Path) → una
   regresión que pierda un paso falla local en vez de en prod.

5. **`hooks.ts` barrel — contract test**: 90 líneas nuevas en
   `hooks.test.ts` que importan una muestra representativa desde
   `@/api/hooks` y asertan que cada uno es función. Protege ~50
   call-sites del SPA contra "olvidé un re-export" PRs.

### Métricas

- Frontend tests: **290 → 296** (+6 del barrel test).
- Backend tests: nuevo `TestPersistManualImage` verde.
- `tsc -b` 0 errores · preview reload limpio.
- **Ficheros >800 líneas en src/ frontend**: 4 → 0 (sigue).
- **Ficheros >1000 líneas en handlers backend**: 1 → 0 (sigue).
- Mayor hook file ahora: `media.ts` ≈ 200 líneas (sano).

### Lo que NO entró del review (judgement calls)

- `useFavoriteMutation` "over-generalised" para 2 callers — el
  reviewer mismo flag "defensible". Si aparece un 3er caso se
  reabre.
- `usePlayback` interface "too wide" para su single caller — la
  superficie refleja el state-machine real del overlay; estrechar
  por estrechar oculta la verdad.
- `LibraryCard` mutation hooks → `useLibraryMutations` —
  abstracción prematura para 2 callers que ya están legibles.

---

## 🧹 Sesión 2026-04-28 (final) — iptv split + audit duplicación

Cierre del refactor SRP iniciado en la sesión "late PM" (sección
inferior).

### Lo que entró

**`b31614b`** — `internal/api/handlers/iptv.go` partido de **1159 → 67
líneas** (shell con struct + constructor + 2 helpers compartidos). El
resto distribuido en 5 ficheros nuevos:

| Fichero | Líneas | Contenido |
|---|---|---|
| `iptv_channels.go` | 347 | List/Get/Groups/Stream/ProxyURL/Schedule/BulkSchedule + parse helpers |
| `iptv_favorites.go` | 253 | Favorites + RecordChannelWatch + ListContinueWatching |
| `iptv_health.go` | 245 | unhealthy / without-EPG / disable / enable + DTOs |
| `iptv_epg.go` | 173 | EPG-source CRUD + catalog |
| `iptv_admin.go` | 151 | RefreshM3U/EPG + PublicCountries + ImportPublicIPTV |

Esto completa la convención que ya existía en el repo (`iptv_access.go`,
`iptv_dto.go`, `iptv_schedule.go`, `iptv_playback_failure.go` ya estaban
extraídos).

**Audit de duplicación real (post-split)**: agente cazó copy-paste real
en backend + frontend. Hallazgo: **1 caso** real de 78% overlap entre
`useAddChannelFavorite` y `useRemoveChannelFavorite` en
`web/src/api/hooks/channels.ts`. Resuelto con factory
`useFavoriteMutation(apiCall, apply)` que las dos hooks invocan con
una línea cada una.

Resto del codebase confirmado limpio:
- Patrón `canAccessLibrary + denyForbidden` repetido ~25 veces en
  iptv_*.go pero son 4 líneas idiomáticas — un wrapper `requireAccess`
  ahorraría 3 líneas/llamada a costa de oscurecer la lógica. Skip.
- `LibraryFormModal` vs `LibraryEditModal` (~120 líneas comunes en
  layout) divergen en lógica (Add tiene 9 vars de estado + reset on
  open; Edit tiene 4 + hidratación desde target). Co-incidencia visual
  legítima, no copy-paste real. Skip.
- DTOs (`toChannelDTO`, `programToJSON`, `channelHealthDTO`,
  `channelWithoutEPGDTO`) ya bien separados, cada uno con shape
  específico. Limpio.

### Métricas finales del refactor SRP completo (sesiones de hoy)

| Fichero | Antes | Después | Δ |
|---|---|---|---|
| `web/src/api/hooks.ts` | 1174 | 23 (barrel) | −98% |
| `internal/api/handlers/iptv.go` | 1159 | 67 (shell) | −94% |
| `web/src/pages/admin/LibrariesAdmin.tsx` | 1188 | 219 | −82% |
| `web/src/pages/ItemDetail.tsx` | 636 | 352 | −45% |
| `web/src/components/player/PlayerControls.tsx` | 793 | 585 | −26% |
| `internal/api/handlers/items.go` | 771 | 698 | −10% |
| `internal/api/handlers/image.go` | 595 | similar (dedupe interna) | net 0 |

**Ficheros >800 líneas en src/ del frontend**: 4 → 0.
**Ficheros >1000 líneas en handlers backend**: 1 → 0.

### Lo que NO se tocó (decisión deliberada, regla "Process vs Group")

- `internal/scanner/scanner.go` (1126) — Process cohesivo: cada
  función llama a la siguiente. Romperlo empeora la lectura linear.
- `internal/iptv/proxy.go` (682) — HLS rewriting + relay tracking,
  dominio coherente.
- `internal/provider/tmdb.go` (586) — adapter puro, una responsabilidad.

La regla está documentada en
[`conventions.md`](conventions.md#cuándo-trocear-un-fichero-gordo-y-cuándo-no):
**Process** (cadena de calls) vs **Group** (handlers independientes).
Sólo se parten Groups.

---

## 🪓 Sesión 2026-04-28 (late PM) — refactor SRP de los ficheros gordos

Audit posterior a la sesión de seguridad/correctness (audit-2026-04-28.md):
4 auditores buscaron código spaghetti, responsabilidades mezcladas y
dependencias ocultas. Los 6 ficheros más largos del repo se trocearon
sin añadir abstracciones nuevas — sólo relocalización a ficheros con
responsabilidad única.

### Resumen ratio antes/después

| Fichero | Antes | Después | Δ |
|---|---|---|---|
| `web/src/api/hooks.ts` | 1174 | 23 (barrel) | −98% |
| `web/src/pages/admin/LibrariesAdmin.tsx` | 1188 | 219 | −82% |
| `web/src/pages/ItemDetail.tsx` | 636 | 352 | −45% |
| `web/src/components/player/PlayerControls.tsx` | 793 | 585 | −26% |
| `internal/api/handlers/items.go` | 771 | 698 | −10% |
| `internal/api/handlers/image.go` | 595 | ~similar (dedupe interna) | net 0 |

### Cambios (6 commits)

1. **`ca02687`** PlayerControls: extracción de 13 icons SVG → `icons.tsx`
   y 3 helpers de audio (codecLabel, channelLabel, enrichAudioTracks)
   → `audioTracks.ts`. Lo segundo gana testabilidad sin React render.

2. **`e6369dd`** items handler: `dedupeSeasons` (lógica de items
   domain) movida del handler al `LibraryService` con test E2E real
   en `library/service_test.go`. La regla "qué fila gana cuando hay
   duplicados de seasons" ahora vive donde corresponde.

3. **`99cfe9d`** hooks.ts: junk-drawer de 1174 líneas split en 12
   ficheros por dominio bajo `web/src/api/hooks/` (auth, setup,
   users, media, progress, channels, iptv-admin, channel-health,
   providers, system, images, preferences) + `queryKeys.ts`
   centralizado. `hooks.ts` queda como barrel re-export de 23 líneas
   para back-compat de imports existentes.

4. **`d0fe2e2`** ItemDetail: `usePlayback` hook (overlay state machine
   completo: showPlayer/playerInfo/playingItemId/playError + handlers
   handlePlay/handlePlayerEnded/handleClosePlayer + next-up prefetch +
   session DELETE en close/retarget) → `pages/itemDetail/usePlayback.ts`.
   Componentes de season (`SeasonEpisodes`/`SeasonGrid`/`SeasonCard`/
   `SeasonEpisodeList`) → `pages/itemDetail/season.tsx`.

5. **`2773130`** LibrariesAdmin: 1188 → 219 líneas. Split en 6 ficheros
   bajo `pages/admin/librariesAdmin/`:
   - `constants.ts` (catálogos iptv-org, LIBRARY_SECTIONS, helpers)
   - `SectionChevron.tsx` (icono)
   - `FilteredSelect.tsx` (select con filtro)
   - `LibraryCard.tsx` (row + sus mutation hooks propios)
   - `LibraryFormModal.tsx` (modal Add con todos los livetv branches)
   - `LibraryEditModal.tsx` (modal Edit con hidratación desde target)

6. **`0264cd8`** image handler: `Select` + `Upload` eran copy-paste de
   los mismos 9 pasos. Extraídos a `persistManualImage` helper. El
   flujo "guardar imagen manual" vive en un solo sitio; añadir un
   paso (pre-resize, thumbnail bake, …) es one-diff en vez de two-
   diffs siempre desincronizadas.

### Skipped intencionalmente (no es over-engineering)

- **`errorRecorder` global → DI**: el auditor lo flageó como hidden
  dep (test blindness). Verificado: ningún test del codebase asierta
  sobre métricas, así que la "ceguera" es teórica, no funcional. Para
  self-hosted single-tenant el global pattern es claro y simple. Skip.
- **`internal/api/handlers/iptv.go`** (1159 líneas): 4 sub-responsabilidades
  detectadas pero el auditor lo marcó 🟡 ("rompible cuando moleste").
  Hoy nadie iterando en él. Si entra feature nuevo se trocea.
- **`internal/scanner/scanner.go`** (1126 líneas): 🟢 según auditor —
  largo pero cohesivo, cada función justifica tamaño por dominio.
- **Tests frontend de hooks críticos** (useLiveHls/useTrickplay/
  useEventStream): seguía pendiente de la sesión anterior, sigue
  pendiente.

### Anti-patrones evitados durante el refactor

1. **No se introdujo ningún wrapper / abstraction layer nuevo**.
   Cada split fue extraer función/componente/hook que YA existía
   pegado a una bola de barro a su propio fichero con responsabilidad
   única. Cero `IService` interfaces de moda.
2. **Cada split mantuvo back-compat de imports existentes** (barrel
   re-export en hooks.ts). Cero touchpoints de call sites en otros
   ficheros — el blast radius por commit fue 0 fuera del directorio
   tocado.
3. **Tests verde tras cada commit**, no al final. Permite revert
   granular si algo se tuerce en producción.

---

## 🛡️ Sesión 2026-04-28 (PM) — auditoría senior + remediación

Auditoría completa del codebase con 4 auditores LLM en paralelo
(security backend / arquitectura Go / frontend / infra) seguida de
remediación priorizada. Threat model asumido: **self-hosted single-
tenant tipo Plex/Jellyfin** — los límites y TTLs reflejan ese contexto,
no SaaS multi-cliente.

Detalle completo en [`audit-2026-04-28.md`](audit-2026-04-28.md). Highlights:

### Lo que entró (15 commits, todos en `main`)

**Seguridad HTTP** — `internal/api/security_headers.go` middleware con
CSP estricta (whitelist explícita TMDb/Fanart/YouTube/Vimeo/Google
Fonts), X-Frame-Options DENY, X-Content-Type-Options nosniff,
Referrer-Policy strict-origin-when-cross-origin, CORP same-origin, y
HSTS condicional sobre TLS real. Tests propios. Verificado e2e con
binary local en `:8098` antes del merge.

**Frontend correctness** — `ApiClient.refresh()` deduplica concurrentes
(5 queries paralelas con cookie expirada → 1 fetch + 1 onAuthFailure
en vez de N); `useProgressReporter` cierra con `keepalive: true` para
no perder los últimos 10 s al navegar; staleTime default 5 min → 60 s;
HeroTrailer respeta `prefers-reduced-motion`, `connection.saveData`,
viewport visibility y `sessionStorage` de dismiss antes de cargar el
iframe de YouTube (~700 KB ahorrados en cada vista de serie de un
usuario que no quiere trailer).

**Hardening** — ffmpeg input wrapped en `file:` protocol; nginx mount
narrow de letsencrypt (sólo `live/<domain>` y `archive/<domain>`);
nginx gzip + rate-limits suaves (login 5 r/m, API 10 r/s burst=20,
stream sin límite); `.golangci.yml` con `gosec` HIGH/HIGH (codebase
ya pasaba 0 issues a ese nivel).

**Calidad / CI** — Dependabot semanal agrupado para go/npm/actions;
Trivy scan en `docker.yml` con SARIF a la pestaña Security; permisos
GH Actions `contents: read` por defecto; `Modal` con focus trap; test
Unix-only de `/etc` skipeado en Windows.

### Lo que NO entró y por qué (deuda intencional)

- CSRF binding a sesión — para self-hosted single-tenant, la ventana
  de exfil del token actual no justifica la complejidad de session
  state + rotación coordinada.
- Refresh TTL 30d → 7d — Plex/Jellyfin dejan tokens indefinidos; lo
  crítico es que estén hasheados (verificado, ya estaba bien).
- gosec medium-severity sweep — 27 findings, mayoritariamente falsos
  positivos por sanitización vía `pathmap`/allowlist. Merece PR
  dedicado con `#nosec` razonados, no un drive-by.
- Tests frontend de `useLiveHls` / `useTrickplay` / `useEventStream` —
  pendiente para próxima sesión.

### Hallazgos del audit que NO eran reales

Cuatro claims de los auditores LLM que se desmontaron al abrir el
código (refresh ya hasheado, SSE no leak, useLogout ya hace `clear()`,
scanner ya respeta ctx). Documentados en `audit-2026-04-28.md §3` para
que la próxima sesión no los persiga.

---

## 🎬 Sesión 2026-04-27 → 2026-04-28 — series detail UX completo

Trabajo continuo sobre la rama `claude/episode-metadata`. Punto de
partida: la página de detalle de serie estaba feísima (hero negro,
year=0, episodios sin still, sin sinopsis) por una decisión heredada
del scanner que **saltaba enrichment de episodios y temporadas a
propósito** para no quemar cuota TMDb. Esta sesión arregla esa
herencia y construye encima un detail page nivel Plex/Netflix.

### Backend (cambios densos)

**Provider layer — capabilities opcionales nuevas**
- `EpisodeMetadataProvider` interface + `EpisodeMetadataResult` con
  `Title/Overview/PremiereDate/Rating/RuntimeMinutes/StillURL/GuestStars`.
- `SeasonMetadataProvider` interface + `SeasonMetadataResult` con
  `Title/Overview/PremiereDate/Rating/EpisodeCount/PosterURL`.
- TMDb implementa ambas: `GetEpisodeMetadata` golpea
  `/tv/{id}/season/{n}/episode/{m}`, `GetSeasonMetadata` golpea
  `/tv/{id}/season/{n}`. Ambas cacheadas 7 días vía el `httpcache` que
  ya existía.
- `Manager.FetchEpisodeMetadata` / `FetchSeasonMetadata` itera
  proveedores que satisfagan la capability. Sin proveedor → `(nil, nil)`,
  callers tratan absence como "sin datos".

**Provider layer — trailers**
- `MetadataResult` extendido con `TrailerKey/TrailerSite`.
- `tmdb.GetMetadata` ahora pide `append_to_response=credits,external_ids,videos`
  (mismo round-trip, sin coste extra).
- `pickTrailer()` rankea: `+100 official, +50 Trailer, +20 Teaser, +10 Clip`.
  Filtra a YouTube/Vimeo (frontend solo embed los dos).

**Scanner — self-healing total**
- `enrichEpisode(item, seasonItemID, seasonNum, episodeNum)`: climb
  episode→season→series, lookup tmdb_id de la serie en `external_ids`,
  llamar al provider, persistir overview en metadata + actualizar
  item (title/year/rating/premiere_date), descargar still como
  `backdrop` primary del episodio.
- `enrichSeason(item, seriesID, seasonNum)`: lookup tmdb_id, persistir
  overview + clean title (TMDb friendly name vs "Season N"),
  descargar poster como `primary` del season.
- `ensureSeasonRow` ahora llama `enrichSeason` síncrono al crear (vía
  cache miss) y `checkAndEnrichSeason` al cache hit (mismo patrón
  one-per-scan que series ya tenía).
- `enrichIfMissing` extendido: switch por type → `episode`/`season`/
  default. Auto-healing en re-scans cuando faltan imágenes.
- **Bug crítico arreglado**: `processFile` solo visita ARCHIVOS;
  seasons (filas agregadas, sin path) nunca se enriquecían en re-scans.
  Fix: `ScanLibrary` colecciona seasons existentes en pre-load, hace un
  sweep `enrichIfMissing(season)` tras el walk.
- `RefreshMetadata` antes borraba imágenes+overview de TODOS y solo
  re-enriquecía movies/series → episodios/seasons quedaban vacíos.
  Ahora dispatcha por type. Iteration order (`series→season→episode`)
  garantiza que el tmdb id del padre está fresco cuando llegan los hijos.
- Test: `TestEnrichEpisode_PersistsOverviewAndStill`,
  `TestEnrichEpisode_NoTMDbIDOnSeries`,
  `TestEnrichSeason_PersistsMetadataAndPoster`.

**Scanner — colores dominantes pre-computados**
- Migración `014_image_dominant_colors`: añade `dominant_color` y
  `dominant_color_muted` a `images` (TEXT NOT NULL DEFAULT '').
- `imaging.ExtractDominantColors(data, logger) (vibrant, muted string)`
  — algoritmo propio (~100 líneas, sin nuevas deps): muestreo grid 32×32
  → buckets 16³ → scoring por saturación×count y oscuridad×count.
  Devuelve "rgb(r, g, b)" para inyectar en CSS vars directamente.
- `IngestRemoteImage` ahora popula los campos automáticamente; el
  upload admin (image.go) y refresh (imagerefresh.go) también.
- Persistido en row de `images`; expuesto via `imageResponse` en API
  + atajo `backdrop_colors: { vibrant, muted }` en item-detail
  (preferencia backdrop, fallback poster).

**Scanner — trailer de TMDb**
- Migración `015_metadata_trailer`: añade `trailer_key/trailer_site`
  a `metadata`.
- `enrichMetadata` persiste `meta.TrailerKey/TrailerSite` en el upsert
  de metadata.
- API expone `trailer: { key, site }` en item-detail cuando ambos
  campos están presentes (par tuple).

**API — handlers y dedupes**
- `Children` handler:
  - **Dedupe seasons duplicadas** por `(parent_id, season_number)`:
    cuando hay > 1, conserva la que tiene más hijos (canonical) vía
    `db.ItemRepository.ChildCountsByParents` (batch SQL nuevo, una
    sola query). El huérfano queda en DB pero oculto del SeasonGrid.
  - Inyecta `backdrop_url`/`poster_url` por niño (batch via
    `GetPrimaryURLs`) → SeasonGrid + EpisodeRow tienen sus stills
    desde el primer paint.
  - Inyecta `episode_count` para seasons (batch via `ChildCountsByParents`).
  - Inyecta `overview` por niño (batch via `GetMetadataBatch`).
- `attachSeriesContext` refactorizado: extraído `attachSeriesContextFromSeries`
  reutilizable; episodios suben dos niveles, seasons un nivel solo.
  Ambos heredan `series_id/title/poster_url/backdrop_url/logo_url` y
  `backdrop_colors` cuando el item no tiene los suyos. Genres también
  se heredan.
- `itemSummaryResponse` + `itemDetailResponse` omiten `year` cuando es 0
  (Go zero-value se filtraba como `"year": 0` y la UI renderizaba "0"
  literal).
- **Continue-watching enriquecido**: SQL extendido con
  `season_number`, `episode_number` y `series_id` (vía
  `LEFT JOIN items season ON season.id = i.parent_id`).
  `ContinueWatchingItem` y handler response también — el frontend
  ya puede hacer match por scope.

**LibraryService extension**
- `GetItemChildCounts(parentIDs) (map[string]int, error)` añadido a
  la interface + implementado en `library.Service` (thin pass-through
  a `db.ItemRepository.ChildCountsByParents`).

### Frontend — overhaul completo del detail page

**Hooks nuevos**
- `useVibrantColors(imageUrl)`: extrae paleta vibrante + dark-muted
  vía `node-vibrant` (lazy dynamic import, ~12kb en chunk separado).
  Cacheada por URL en módulo. Solo se activa como **fallback** cuando
  la imagen no tiene colores pre-computados del backend (rows ingresadas
  antes de la migración 014).
- `useResumeTarget(scope, id)`: scope `"series"` o `"season"`. Filtra
  `useContinueWatching` y `useNextUp` por `series_id` o `parent_id`
  según scope. Cold-start fallback: para series escoge primera
  temporada, para season escoge primer episodio. Returns
  `{ mode: "resume"|"next-up"|"start"|"none", episode, seasonNumber,
  episodeNumber, progressPercent }`.
- `useSeriesResumeTarget` mantenido como alias deprecated (back-compat).

**Componentes nuevos**
- `SeriesHero`: hero full-bleed (`-mx-4 md:-mx-6` + `marginTop:
  calc(var(--topbar-height) * -1)`) → backdrop llega al borde superior
  detrás del topbar. TopBar global ya hacía glass-on-scroll
  (transparent en scrollY=0 desktop, `bg-bg-base/70 backdrop-blur-xl`
  al scrollear).
  - Backdrop falls-through: `item.backdrop_url ?? item.series_backdrop_url ?? item.poster_url`.
  - Color gradient izquierdo via CSS vars `--hero-c1/--hero-c2`
    alimentadas por `backdrop_colors` (backend) o `useVibrantColors`
    (fallback).
  - Layout vertical en columna izquierda: poster centrado
    (`h-[240px] sm:[280px] lg:[340px]`) + título + meta row (year o
    fecha completa para episodios) + overview clamped + botones.
  - **Botón "Reproducir" siempre limpio** (no muta a "Seguir viendo
    SXXEYY"). El "Seguir viendo" pasa a panel separado debajo.
  - Reusado para series Y season pages — mismo layout, diferente scope.
- `HeroTrailer` (subcomponente de SeriesHero):
  - Two-stage reveal: `loaded` flips a 2.5s (iframe carga oculto),
    `revealed` flips a 3.7s (fade-in). Total ~3.7s — evita que el
    play-overlay inicial de YouTube se cuele.
  - URL: `youtube-nocookie.com/embed/{key}?autoplay=1&mute=1&controls=0&loop=1&playlist={key}&modestbranding=1&playsinline=1&rel=0&iv_load_policy=3&disablekb=1`.
  - Iframe sized con `aspect-ratio: 16/9` + `width: 100%` +
    `minWidth: calc(100% * 16/9)` → cubre la franja sin letterbox de
    YouTube en heroes 16:7.
  - Mask `linear-gradient(to right, transparent 0%/25%, black 55%/100%)`
    → fade hacia el gradient izquierdo, sin corte vertical.
  - Botón "Saltar avance" abajo-derecha cuando está revealed.
- `EpisodeRow` (Jellyfin-style): horizontal layout, still 16:9
  izquierda + meta column derecha. Muestra: badge SXXEYY, título,
  fecha emisión, duración, rating, **"Termina a las HH:MM"** computado
  client-side, sinopsis clamp 2 líneas, hover-play overlay, barra de
  progreso si hay user_data.progress < 95%. Click → `onPlay(item.id)`
  (no navegación).

**Componentes refactorizados**
- `SeasonGrid`: cards de poster (2:3) reemplazan los tabs de texto.
  Cada card: poster, título, year, episode count, badge rating.
  Click → navega a `/items/<season-id>` (página propia de temporada).
  Fix colateral del "Season 1 / Season 1" duplicado vía dedupe en
  backend.
- `HeroSection`: para episodios (y seasons) cae a `series_backdrop_url`,
  `series_poster_url`, `series_logo_url` cuando faltan los propios.
  `isSubItem = episode || season`.
- `EpisodeCard`: link `/items/<id>` (antes `/episodes/<id>` que no
  existía como ruta — bug del frontend 404). Lee `duration_ticks`
  (antes leía `runtime_ticks` que el backend nunca emitía — bug
  silencioso meses).
- `MediaBrowse`: usa `useTopBarSlot` para portalear search/sort/filters
  al topbar global → un solo buscador en pantalla (antes había dos:
  global del topbar y página). Caratulas más grandes:
  `minmax(180px → 200px sm → 220px lg)` (antes 150px fijo).

**Página `ItemDetail` reorganizada**
- Series page: SeriesHero + opcional panel "Seguir viendo" + SeasonGrid
  (sin episodios inline — viven en la página de su temporada).
- Season page: SeriesHero (mismo layout, hereda backdrop de la serie)
  + opcional panel "Seguir viendo" + lista de EpisodeRow inline.
  Click episode → `handlePlay(episodeID)` lanza el VideoPlayer overlay
  en la misma página (cero navegación).
- Movies / episodes: HeroSection clásico sin cambios.
- `handlePlay(targetId?)` refactorizado para aceptar id opcional →
  un solo handler sirve series page (default = page id) y season page
  (override = episode id).

**Tipos y i18n**
- `MediaItem` extendido: `series_title`, `series_*_url`,
  `backdrop_colors`, `episode_count`, `trailer`, `duration_ticks`
  (renombrado desde `runtime_ticks` para alinear con backend).
- i18n keys nuevas: `continueWatching`, `endsAt`, `episodeCount_one/_other`,
  `trailer`, `dismissTrailer`. Es: "Seguir viendo", "Termina a las HH:MM",
  "Saltar avance".

### sqlc — manual-edit policy

`sqlc/sqlc:latest` (incluyendo `:1.27.0` y `:1.25.0` probadas) tiene un
**bug que se come el `?` final de `LIMIT ?`/`OFFSET ?`** cuando regenera
desde el SQL → la query rompe en runtime con "incomplete input".
Restaurado el sqlc gen anterior desde HEAD; los nuevos campos
(`dominant_color`, `dominant_color_muted`, `trailer_key`, `trailer_site`,
`season_number`, `episode_number`, `series_id` para continue-watching)
se aplican a mano sobre `internal/db/sqlc/*.sql.go` con un comentario
señalando la migration de origen. Las queries `.sql` también se actualizan
para que un futuro `sqlc generate` (cuando arreglen) refleje el shape
real.

### Verificado al cierre

- **Backend**: `go build ./... && go test ./...` 100% verde (sin -race
  por ahora — tiempos en este Windows lo hacen lento).
- **Frontend**: `pnpm tsc --noEmit` clean · 289/289 tests pasan ·
  bundle nuevo añade `node-vibrant` en chunk separado.
- **Live**: container `hubplay-dev` healthy; auto-rescan de la librería
  Daredevil dejó el flujo end-to-end funcionando: episodios con stills
  + sinopsis, season con poster + rating, hero con trailer Netflix-style,
  panel "Seguir viendo" reactivo a progreso real.

### Limitaciones conocidas

- **Trailer per-season** no existe (TMDb no expone videos a nivel de
  season de forma fiable). El SeasonHero hereda el trailer de la serie.
  Solución futura: job ffmpeg en scan que extrae 30s del propio archivo
  (rama planificada `claude/local-trailers`). Privacy-pure y per-episodio.
- **YouTube embed** (incluso con `youtube-nocookie.com`) carga ~600KB
  de player JS. Self-host del clip eliminaría toda telemetría +
  reduciría bundle a 0KB extra (HTML5 `<video>` nativo).
- Hueco visual a la derecha del trailer en pantallas extra-anchas
  (>2200px): el aspect-ratio 16:9 del iframe puede dejar bandas si
  la franja-hero supera ese ratio. Aceptable para resoluciones típicas
  (≤ 1920px); aspect-ratio sizing es la solución estándar Netflix-style.

---
>
> **🎯 Próximo gran hito (post-merge de esta rama)**: app nativa **Kotlin para Android TV**. Toda decisión técnica de aquí en adelante se mide contra "¿facilita o estorba la app TV?".

---

## 🩹 Hot-fix sesión tarde 2026-04-27 — responsive admin

Mientras yo trabajaba en esta rama, otra rama paralela
(`claude/adoring-taussig-b7e4ed`) hizo merge a `main` con un **rework
mayor del panel de admin**:

- Reorg IA: nueva pestaña Dashboard (landing), `/admin/system` partido
  en sub-pestañas `status` / `activity` / `advanced`.
- `SystemAdmin.tsx` **eliminado**, sustituido por
  `pages/admin/system/SystemStatus.tsx` con un endpoint nuevo
  `/admin/system/stats` (richer: bind_address, base_url,
  ffmpeg.hw_accel_enabled, library inventory, signing-keys UI).
- `internal/api/handlers/system.go` nuevo (con tests), antiguo
  `/health` se queda como sonda básica.

**Consecuencia para mi sesión**: dos commits que había hecho hoy
sobre la rama feature quedaron obsoletos antes de pushear:

- ❌ `9fd47c1 fix(admin): align SystemAdmin with real /health contract` →
  editaba `SystemAdmin.tsx`, archivo borrado en main. **Descartado**.
- 🟡 `f870c0a fix(admin): responsive layout for mobile viewports` →
  parte sobre `LibrariesAdmin.tsx` seguía siendo válida; parte sobre
  `AdminLayout.tsx` chocaba con la pestaña Dashboard nueva.
  **Re-aplicado** sobre la versión actual de main.

**Lo que terminó en main como hot-fix móvil** (1 commit):

- Header de Bibliotecas envuelve en pantallas estrechas (el botón
  "Agregar biblioteca" no se sale del viewport).
- Filas de biblioteca apilan info → acciones en móvil; los 3-5
  botones (Escanear / Actualizar metadatos / Actualizar imágenes /
  Editar / Eliminar) hacen wrap en lugar de cortarse.
- Tabs del admin con scroll horizontal contenido vía `-mx-4` +
  `overflow-x-auto`, así los 5 tabs (Dashboard, Bibliotecas,
  Proveedores, Usuarios, Sistema) no fuerzan ancho de página.

### Lección registrada (process)

Antes de aplicar fixes en una rama vieja, **siempre `git fetch
origin main` + `git log origin/main..HEAD`** primero. Hoy escribí
~150 LOC contra un `SystemAdmin.tsx` que ya no existía. No catastrófico
porque pausé antes de pushear, pero coste de tiempo evitable.

### Estado del scanner self-healing (`2f963f8`)

Ya está en `origin/main` (es el merge-base). Series sin metadata se
re-enriquecen en cada scan automáticamente. `enrichMetadata` ahora
salta `episode`/`season` para no quemar cuota TMDb cuando admin
toca "Actualizar metadatos" sobre una librería de shows.

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

2. **Capability negotiation server-side** (~1 día). Nuevo header
   `X-Hubplay-Client-Capabilities` con CSV de codecs decodificables
   (audio + video). El stream manager evalúa
   "¿puedes decodificar `eac3.7.1` directamente?" → direct-stream.
   La app TV lo declarará agresivamente; el web seguirá siendo
   conservador. Cuando la app TV exista será el primer cliente que
   le saque jugo a esto.

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

5. **WebSocket / SSE para progress sync** (~1 día). El cliente
   reporta posición cada 5s; otros clientes del mismo usuario
   reciben push si el item es el mismo. Crítico para "empezar en
   un sitio, seguir en otro". El server ya tiene `event.Bus`
   interno; falta exponerlo como SSE para clientes externos.

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
