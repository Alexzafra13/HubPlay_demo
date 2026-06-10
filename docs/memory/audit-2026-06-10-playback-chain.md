# Auditoría de la cadena de playback — 2026-06-10

> Audit focalizado en **lo que el usuario nota al darle al play**: decisión
> direct-play/direct-stream/transcode, construcción de args FFmpeg, seeking,
> HW accel, ciclo de vida de procesos, ffprobe, serving HTTP, trickplay,
> proxy/transmux IPTV y el reproductor frontend (hls.js).
> Método: 4 sweeps paralelos especializados con lectura de punta a punta
> (request → decisión → ffmpeg → HTTP → player). **Solo análisis — 0 cambios
> de código.** Toda evidencia con `file:line`. Dos hallazgos fueron
> encontrados de forma independiente por dos sweeps (PB-2 y PB-8) — señal
> de robustez del diagnóstico.

Leyenda severidad: 🔴 Crítico (playback roto en casos comunes) · 🟠 Alto ·
🟡 Medio · 🟢 Bajo.

---

## 🔴 Críticos

### PB-1 · Todo MKV con códecs compatibles obtiene falso DirectPlay (alias `webm`)
`internal/stream/decision.go:98` + `internal/scanner/scan_walk.go:117`.
ffprobe reporta MKV como `format_name: "matroska,webm"`; se persiste tal
cual y `containerInSet` parte por comas → matchea el `"webm"` de
`DefaultWebCapabilities().Containers` (`capabilities.go:90`). Un MKV
h264+AAC (el fichero más común) pasa como DirectPlay y se sirve como
`video/x-matroska`: Chrome suele tragarlo, **Firefox y Safari no
reproducen Matroska** → pantalla negra donde el path correcto
(DirectStream/remux) ya existe y funciona. Los tests no lo cazan porque el
único caso `"matroska,webm"` usa audio AC3, que fuerza DirectStream por el
lado del audio (`decision_test.go:108-123`). **Fix:** normalizar el
container en el scanner (primer nombre del alias-list o mapeo por
extensión) o excluir `matroska,*` del match `webm`.

### PB-2 · Se sirven segmentos HLS a medio escribir — falta `temp_file`
`internal/stream/transcode.go:510` (`-hls_flags independent_segments`) +
`internal/api/handlers/media/stream.go:566-575` (`waitForFile` da por
bueno `Size() > 0`). ffmpeg escribe los `.ts` in-place con su nombre
final; el handler sirve un TS truncado con `Content-Length` parcial →
glitches de decodificación, stalls y reintentos de hls.js, sobre todo en
la ventana post-seek (15s, `stream.go:494`). En transcode real la ventana
es de segundos por segmento. El transmux IPTV **ya lo hace bien**
(`temp_file` en `transmux_args.go`). **Fix:** `-hls_flags
independent_segments+temp_file` — una palabra; `waitForFile` pasa a ser
correcto sin tocarlo. *(Encontrado por 2 sweeps independientes.)*

### PB-3 · El encoder no fuerza keyframes al grid de 6s en que se basa todo el seeking
`internal/stream/transcode.go:440-451,505-512`. Todo el diseño descansa en
"segmento N cubre [6N, 6N+6)" (`SynthesizeVODManifest` `hls.go:55`,
`startSegment = startTime/6` `manager.go:467`, `RestartSessionAt`
`stream.go:478`), pero el path de encode no emite `-g`, ni
`-sc_threshold 0`, ni `-force_key_frames` — con el keyint default de
libx264 (~10s @24fps) los segmentos reales miden 6–10s y el drift se
acumula: seeks que aterrizan mal, huecos de buffer, colisiones entre el
run continuo y el seek-restart (el síntoma que ya se debuggeó el
2026-05-08; `-copyts` mitigó, no eliminó la causa). El transmux IPTV del
mismo repo **sí lo hace** (`-g 48 -sc_threshold 0`,
`transmux_args.go:176-177`). **Fix:** `-force_key_frames
"expr:gte(t,n_forced*6)"` en el branch de encode (vale para libx264 y HW).
En `CopyVideo` es imposible por diseño — documentar que el grid es
aproximado en remux.

### PB-4 · VOD sin listener de `error` del `<video>`: fallo de decode = pantalla negra sin mensaje
`web/src/hooks/useHls.ts:292-319` (rutas Safari/iOS nativa y direct_play)
+ `useVideoPlaybackEvents.ts:214-219`. Si la reproducción va por
`video.src` directo y el elemento dispara `error` (decode mid-play,
fichero corrupto, red), nadie escucha → `firstFrameReady` no llega y el
overlay de carga se queda para siempre. Live TV **sí** lo maneja
(`useLiveHls.ts:140-144`) — la asimetría confirma el hueco. **Fix:**
`video.addEventListener("error", …)` en las tres ramas de `useHls`,
mapeando `video.error.code` a mensajes específicos.

---

## 🟠 Altos

### Backend — decisión y transcode

- **PB-5 · VAAPI/QSV no funcionan de verdad.** `transcode.go:361-362,440` +
  `hwaccel.go:139-173`. `h264_vaapi` necesita `-init_hw_device`/`
  -vaapi_device` + `format=nv12,hwupload` al final de la vf chain;
  `BuildFFmpegArgs` no emite ninguno. `verifyEncoder` falla al arrancar y
  cae a libx264 con un Warn → quien compró el target Docker `hwaccel` para
  VAAPI **transcodea por software sin saberlo**, con
  `MaxTranscodeSessions` autotuneado a 6 como si hubiera iGPU
  (`autotune.go:28`). NVENC sí funciona. **Fix:** path VAAPI dedicado
  (init_hw_device + hwupload, tonemap software antes del upload) y
  visibilizar el fallback en el panel admin.
- **PB-6 · La decisión ignora la pista de audio seleccionada.**
  `decision.go:84-97` + `manager.go:428-446`. `Decide` evalúa `audioOK`
  solo contra la pista default; `AudioStreamIndex` no le llega. MKV con
  default AAC + pista DTS: el usuario cambia a DTS → DirectStream con
  `CopyAudio` → DTS dentro del TS → **vídeo mudo**. Inverso: re-encode
  innecesario. **Fix:** pasar `AudioStreamIndex` a `Decide` y evaluar
  contra la pista efectiva.
- **PB-7 · `mp4`/`mov` no están en `remuxableContainers`.**
  `decision.go:36-41,127`. MP4 h264+AC3 (rip típico): falla `audioOK`, y
  el gate de DirectStream exige container remuxeable → **re-encode
  completo del vídeo** (CPU + pérdida de calidad + 720p) cuando bastaba
  `-c:v copy` + transcode de audio. **Fix:** añadir mp4/mov/m4a al set +
  test `TestDecide_DirectStream_MP4_H264_AC3`.
- **PB-8 · Perfil del códec ignorado: Hi10P/HEVC Main10 → DirectPlay imposible.**
  `decision.go:96` (el probe captura `Profile` en `probe.go:127` y se
  persiste — simplemente no se consulta). h264 High 10 (omnipresente en
  anime) no lo decodifica ningún navegador → pantalla rota. **Fix:** gate
  "profile contiene '10' → transcode salvo cap explícita". Capturar
  `pix_fmt`/`bits_per_raw_sample` daría señal más fiable. *(Encontrado
  por 2 sweeps.)*
- **PB-9 · ffmpeg huérfano por race StopSession/cleanupIdle ↔ RestartSessionAt.**
  `manager.go:547-628` vs `:660-682,812-836`. El restart solo sostiene
  `restartMu`; un Stop concurrente (usuario cierra el player durante un
  seek, o tick de idle-reap) borra la key y mata el ffmpeg viejo, y el
  restart spawnea uno nuevo **fuera del map** → nadie lo para hasta el
  timeout de 4h. **Fix:** tras `RestartAt`, re-verificar bajo `m.mu` que
  la key sigue viva; si no, `newSession.Stop()`.
- **PB-10 · ABR + sesión-por-calidad agota los caps → 503 a mitad de película.**
  `hls.go:31-44` + `manager.go:241-245,375-397` + `autotune.go:24-49`.
  El master anuncia 4 variantes; cada switch de calidad de hls.js spawnea
  otra sesión ffmpeg y la anterior vive 5 min más. Con caps de software
  (global 2 / user 1) dos switches en <5 min → `TranscodeBusy` → 503.
  Agravante: en DirectStream las 4 variantes producen bytes idénticos con
  `BANDWIDTH` inventados → el ABR no puede adaptar. **Fix:** parar
  sesiones hermanas del mismo item al cambiar de profile; no contar
  `CopyVideo` como full-transcode en el cap; filtrar variantes por
  resolución de la fuente.

### Backend — probe, trickplay, scan

- **PB-11 · ffprobe/fpcalc/ffmpeg sin timeout por fichero — un fichero malo cuelga el scan entero.**
  `probe.go:85-97` + `scan_walk.go:51,162` + `fingerprint.go:148,184`. El
  walk es secuencial: un NFS colgado o un fichero corrupto bloquea la
  biblioteca indefinidamente ("escaneando…" eterno). El mismo `Probe` sin
  timeout se usa en uploads de usuarios (`upload/service.go:366`).
  **Fix:** `context.WithTimeout` (30–60s) por invocación. Bonus: `-v
  error` en vez de `-v quiet` para que `ExitError.Stderr` sea accionable.
- **PB-12 · Trickplay sin ACL de biblioteca y sin límite global de ffmpegs.**
  `item_trickplay_handler.go:75-130`. Todo el streaming pasa por
  `authorizeItem`, trickplay no: cualquier usuario autenticado ve la
  timeline visual (200 frames de la película) de bibliotecas restringidas
  y puede disparar generación. El lock es por-item sin semáforo global:
  hoverear una fila de la home puede lanzar N ffmpegs de 180s. **Fix:**
  pasar `deps.Access` + gate 404 como `stream.go:92-101`; semáforo global
  de 2-3 slots.
- **PB-13 · Trickplay reintenta para siempre los fallos de generación.**
  `item_trickplay_handler.go:229-238` + `imaging/trickplay.go:247-254`.
  Fallo de ffmpeg (corrupto, o >180s en hardware lento) no deja marcador
  → cada hover relanza otro ffmpeg de 180s, en bucle con el
  `Retry-After: 10` del frontend. **Fix:** negative-cache en disco
  (`failed.json` + TTL).

### IPTV

- **PB-14 · Fallo de upstream al arrancar → 200 vacío tras ~15s.**
  `iptv_channels.go:250-253` + `proxy.go:314-343`. Si `fetchUpstream`
  falla antes de escribir, el handler solo loguea ("response may already
  be partially written") — pero no se escribió nada → 200 implícito con
  body vacío; hls.js quema sus 6 reintentos contra más 200s. **Fix:**
  wrapper de `w` que registre bytes escritos; sin bytes → 502/503 (el
  patrón ya existe en `ProxyURL`, `proxy.go:746`).
- **PB-15 · El retry-loop interno rompe la semántica del breaker y reintenta errores permanentes.**
  `proxy.go:315,547` + `circuit_breaker.go:44`. Cada uno de los 5 intentos
  registra `RecordFailure` → **una sola request fallida abre el breaker
  30s** (umbral nominal: 5). Un blip de 3s = ~45s de canal muerto para
  todos. Y los 403 (geo-block)/404 se reintentan 4 veces inútilmente.
  **Fix:** no reintentar 4xx (tipar el error con status); un único
  outcome por request (mover `reportOutcome` tras agotar retries).

### Frontend player

- **PB-16 · Recovery de hls.js sin contador ni backoff → bucle infinito.**
  `useHls.ts:253-282`. `NETWORK_ERROR` fatal → `startLoad()` siempre, sin
  límite; `MEDIA_ERROR` → `recoverMediaError()` ilimitado y el
  `swapAudioCodec` del "segundo pase" que menciona el comentario **no
  está implementado**. Con el server caído: overlay parpadeando para
  siempre. (En live, media error tiene el mismo bucle —
  `useLiveHls.ts:227-230` — network sí está acotado a 3.) **Fix:**
  contadores con ventana + backoff, `swapAudioCodec` en el 2º media
  error, tras N intentos `destroy()` + error terminal traducido.
- **PB-17 · Federación: `markPlayed` y cleanup de sesión apuntan al server local con id remoto.**
  `useVideoPlaybackEvents.ts:210` + `VideoPlayer.tsx:349`. Con `peerId`,
  `ended` hace `api.markPlayed(remoteId)` local → 404 tragado; y como
  `useProgressReporter` nunca envía `completed:true`, **un item federado
  jamás se marca visto**. El cleanup `DELETE /stream/{remoteId}/session`
  también va al server equivocado → la sesión de transcode del peer se
  filtra hasta el reaper. **Fix:** branch por `peerId` (ya existe
  `updatePeerItemProgress` con campo `completed`, `client.ts:2086-2095`).
- **PB-18 · El progreso no se persiste al cerrar la pestaña.**
  `useProgressReporter.ts`: guarda cada 10s (saltando paused/seeking) y en
  el cleanup de unmount de React — que no corre al cerrar pestaña. Se
  pierden hasta 10s siempre; "pauso → seekeo → cierro" pierde el seek
  entero. La infra `pagehide + keepalive` ya existe en el propio fichero
  vecino (`useStreamSessionCleanup.ts:14-24`). **Fix:** listener
  `pagehide` + guardar en `pause`.

---

## 🟡 Medios

- **PB-19 · Hardware lento → restart-thrash.** `stream.go:451-494` +
  `manager.go:529-537`. Si el encode va por detrás de tiempo real, el
  handler espera 2s y **reinicia ffmpeg** perdiendo el progreso; a 20
  restarts/min entra el rate-limit → 429 → stall. **Fix:** mirar el
  último segmento en disco; si lo pedido está ≤3-4 por delante, esperar.
- **PB-20 · Sesión zombie con ffmpeg muerto = spinner sin error; `?audio=N` fuera de rango lo provoca.**
  `manager.go:486-523` + `transcode.go:402-407` (`-map 0:a:N` sin sufijo
  `?`) + `stream.go:264-269` (índice sin validar). **Fix:** validar
  índices contra `mediaStreams` (400) + observar `session.done` y
  desregistrar con error tipado.
- **PB-21 · `-tune zerolatency` en VOD** degrada calidad por bit sin
  ganar latencia (`transcode.go:441-446`). Quitarlo del path VOD.
- **PB-22 · Downmix forzado `-ac 2`** en todo transcode de audio
  (`transcode.go:495-499`): las fuentes 5.1/7.1 pierden surround aunque el
  cliente soporte AAC 5.1. **Fix:** `channels` en capabilities + `-ac
  min(src, client, 6)`.
- **PB-23 · Dolby Vision casi nunca se detecta.** `probe.go:218-229` mira
  `profile`, pero ffprobe anuncia DV en `side_data_list` (DOVI). DV
  Profile 5 (WEB-DLs) se etiqueta HDR10/SDR → colores verde/morado.
  **Fix:** parsear `side_data_list`; DV sin base compatible → transcode.
- **PB-24 · Cover art embebido (`attached_pic`) persiste como pista de vídeo real.**
  `probe.go:137` (no lee `disposition.attached_pic`) → música con
  carátula = "transcode completo" del MP3; pista fantasma en el UI.
  **Fix:** filtrar/marcar en `probeResultToStreams`.
- **PB-25 · Fichero borrado entre scan y play → errores crípticos.**
  `stream.go:529-538` (404 text/plain con Content-Type de vídeo) +
  `transcode.go:140` (ffmpeg muere a Debug → bucle de
  SEGMENT_NOT_FOUND). **Fix:** `os.Stat` antes de servir/arrancar →
  `FILE_NOT_FOUND` JSON; exit prematuro de ffmpeg a Warn con stderr.
- **PB-26 · Duración "N/A" → 0 silencioso** sin fallback a la duración de
  los streams (`probe.go:155-157`). El downstream degrada con gracia
  (manifest legacy sin free-seek) — fix barato: `max(stream.duration)`.
- **PB-27 · Canales HEVC: `-bsf:v h264_mp4toannexb` incondicional.**
  `transmux_args.go:135`. Con HEVC ffmpeg muere → el classifier promueve a
  **re-encode permanente** (1h TTL) cuando `-c copy` funcionaría. +5-10s
  de zap y CPU quemada. **Fix:** quitar el bsf (mpegts no lo necesita) o
  por codec.
- **PB-28 · Zapping >10 canales TS en 30s → TRANSMUX_BUSY sin forma de liberar.**
  `transmux.go:463-469`; `Stop` (`transmux.go:814`) no se llama desde
  ningún handler. **Fix:** beacon/DELETE al cambiar de canal o viewers
  refcount con reap a 0.
- **PB-29 · `net.LookupIP` síncrono sin ctx en cada segmento proxiado**
  (`proxy.go:254`) + **PB-30 · TOCTOU DNS-rebinding en el guard SSRF**
  (`proxy.go:233-264`; ya anotado como B2 en el audit de prod). Ambos se
  resuelven juntos validando la IP en el `Control` del `net.Dialer`.
- **PB-31 · Stall de body sin watchdog** en raw-TS passthrough con
  transmux off (`proxy.go:632-653`); mitigado por hls.js/`-rw_timeout` en
  los paths default. Corregir el comentario engañoso de `proxy.go:85-95`.
- **PB-32 · Timeouts default de hls.js en seek a zona no transcodificada**
  (`useHls.ts:158-177`): un restart en frío con HW lento supera
  `fragLoadingTimeOut` → cae en el bucle PB-16 para algo normal. **Fix:**
  subir timeouts cuando `playbackMethod === "transcode"`.
- **PB-33 · Atajos de teclado a nivel window activos con modales abiertos**
  (`usePlayerKeyboard.ts:34-41`). **Fix:** prop `enabled` o check de
  `[role="dialog"]`.
- **PB-34 · VOD sin tuning de buffers** (`useHls.ts:158-177`):
  `backBufferLength` default = Infinity → memoria sin límite en sesiones
  largas (móviles/TV boxes). La config live está exquisita; la VOD no se
  revisó. **Fix:** `backBufferLength: 30-90`.
- **PB-35 · Mensajes de error del player hardcodeados en inglés/técnicos**
  ("Playback failed: bufferStallError") en una app i18n es/en
  (`useHls.ts:262-321`). **Fix:** claves i18n + tabla detalle→humano.

## 🟢 Bajos

- **PB-36** · Profile `"original"` fuerza `CopyAudio` pisando la decisión
  (`transcode.go:336-339`) — latente, la API lo expone.
- **PB-37** · Data race benigna `ms.Session` handler↔restart
  (`manager.go:616-621` vs `stream.go:442`); campos inmutables o accessor.
- **PB-38** · N viewers HLS passthrough = N conexiones upstream — trade-off
  deliberado (`proxy.go:71-78`), pero es la queja nº1 esperable con
  providers `max_connections=1`. Roadmap: relay compartido por canal.
- **PB-39** · `channelBreaker.Prune()` nunca se invoca; `SubtitleTrack`
  índice sin validar (500 en vez de 400, `stream.go:797`); teardown
  secuencial 5s×N en transmux; dead code en `fingerprint.go` y
  `useStartPositionSeek` con comentario falso.

---

## Lo que está bien (no re-auditar)

- **Seguridad de la cadena**: sin inyección de args (exec directo, `file:`
  prefix, índices clampados), serving de segmentos blindado (regex +
  containment), ACL por biblioteca en todos los endpoints de stream
  (salvo trickplay, PB-12), escape correcto del filtro subtitles.
- **Procesos**: process-groups + kill de árbol, `cmd.Cancel`+`WaitDelay`,
  timeouts de sesión, singleflight anti-doble-spawn, coalesce+rate-limit
  de restarts. (Los problemas son las races PB-9/PB-37, no el diseño.)
- **Transmux IPTV**: 1 ffmpeg/canal compartido, workdir versionado,
  `delete_segments+temp_file`, fallback a re-encode con TTL, stderr ring.
  **El breaker** (3 estados, half-open, cooldown exponencial) está bien
  razonado — el problema es cómo se alimenta (PB-15).
- **Reescritura de playlists HLS**: cubre variantes, EXT-X-KEY/MAP/MEDIA,
  URLs relativas/absolutas post-redirect, streaming sin bufferizar.
- **Player**: lifecycle de hls.js impecable (destroy compartido,
  auto-advance sin remount, cero leaks detectados), ruta Safari/iOS nativa
  en las 3 superficies, cambio de pista estilo Jellyfin con resume al
  playhead live, config live de hls.js excelente, bajo riesgo con React
  Compiler.
- **Manifest VOD sintético + `-copyts`**: la barra de progreso no se rompe
  al seekear; decisión bien documentada y pineada en tests.

## Gaps de test (consolidado)

1. **0 tests de integración con ffmpeg real** (ni con build tag): nada
   verifica que los args *arranquen* — PB-5 (VAAPI) y PB-20 (`-map` fuera
   de rango) serían triviales de cazar transcodeando 2s de `testsrc`.
2. **El test que destapa PB-1**: `Decide` con `"matroska,webm"` + h264 +
   **aac**. Toda la matriz actual usa audio incompatible o `"matroska"`.
3. `useHls.ts` **sin ningún test** (su gemelo live tiene ~13KB de tests):
   recovery acotado, fallback nativo, destroy en cambio de source.
4. `rewriteHLSPlaylist`/`resolveURL` (IPTV) **sin tests** — un edge de
   EXT-X-KEY = canal en negro.
5. Handler `Segment`: seek-restart, 429/Retry-After, fichero a medio
   escribir. Concurrencia destructiva Stop↔Restart con `-race`.
6. Trickplay handler: ACL, TryLock, bucle de regeneración.
7. **Smoke E2E (Playwright)** recomendado: (a) play transcode → primer
   frame → seek +5min → cerrar → resume correcto; (b) fin de episodio →
   UpNext → auto-advance; (c) cambio de dub mid-play mantiene posición;
   (d) LiveTV: zapear 3 canales sin spinner colgado; (e) matar backend
   mid-play → error accionable <30s (hoy: bucle infinito, PB-16).

---

## Plan de ataque propuesto

| Fase | Tema | Items | Coste |
|---|---|---|---|
| **P0** | Correctness que rompe playback común | PB-1, PB-2, PB-3, PB-4 + tests que los fijan | 1 sesión |
| **P1a** | Decisión/transcode | PB-6, PB-7, PB-8, PB-9, PB-20, PB-21 | 1 sesión |
| **P1b** | Trickplay + probe | PB-11, PB-12, PB-13, PB-24, PB-25 | 1 sesión |
| **P1c** | IPTV | PB-14, PB-15, PB-27, PB-28 | 1 sesión |
| **P1d** | Player frontend | PB-16, PB-17, PB-18, PB-32, PB-35 + tests useHls | 1 sesión |
| **P2** | VAAPI real + ABR/caps + surround | PB-5, PB-10, PB-22, PB-23 | 1-2 sesiones |
| **P3** | E2E smoke + resto 🟡/🟢 | gaps de test 1-7, PB-26, PB-29–34, PB-36–39 | 1-2 sesiones |
