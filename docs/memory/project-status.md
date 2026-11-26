# Estado del proyecto

> **Entrypoint de cada sesión.** Solo el estado vivo, lo que falta y el
> orden en que atacarlo. El detalle de sesiones cerradas vive en
> `archive/` (no se borra nada, solo se reubica). Última reorganización:
> 2026-11-26 (sesiones 06-12 → 09-16 movidas a
> `archive/2026-06-12-to-09-16.md`).

---

## 🧭 Cómo retomar (checklist de inicio de sesión)

1. Leer este fichero entero (es corto a propósito) y, si toca decidir
   qué construir, `roadmap-producto.md` (visión, pilares y qué es
   "terminado" en cada uno; objetivo final: la app de TV en Kotlin).
2. `git fetch origin main && git log --oneline -10 origin/main`: ¿hay
   commits que no estén reflejados aquí? Si los hay, reconstruirlos y
   apuntarlos antes de nada (ya pasó dos veces: junio y septiembre).
3. Mirar CI de `main` (GitHub Actions: CI, Docker, Release) y PRs abiertos.
4. Verificar que todo está verde ANTES de tocar nada:
   - `go build ./... && go vet ./... && go test ./...` (Go **1.26.11** desde
     2026-11-26; `GOTOOLCHAIN=auto` lo descarga solo. Con `anacrolix/torrent`
     v1.63 también compila con 1.27).
   - `cd web && CI=true pnpm install --frozen-lockfile && pnpm exec tsc -b
     && pnpm test --run && pnpm exec knip && pnpm exec eslint .`
   - `golangci-lint run ./...` (CI usa v2.15.0; tiene que estar compilado
     con un Go >= el de `go.mod`, si no se niega a cargar la config).
     Sin binario local: `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run ./...`.
5. Elegir la siguiente tarea de **"▶️ Próxima sesión"** (abajo, en orden).
6. Al cerrar: actualizar este fichero (estado + próxima sesión) y el audit
   que corresponda; mover a `archive/` lo que deje de ser vivo.

---

## 🔭 Estado actual (2026-11-26, retomando tras ~2 meses parado)

**Salud:** MVP funcional, cerca de early-production. `main` en
`abf881a` (#553): Release (nightly de nuevo publicada), Docker,
vulnerability scan y lint **verdes**; solo el job E2E en rojo, arreglado
en la rama de la sesión (ver "Revisión pre-app de TV"). Sin issues abiertos.

| Área | Estado |
|---|---|
| Tests backend | `go test ./...` verde: 45 paquetes (Linux, Go 1.26.11) |
| Tests frontend | **783/783** vitest (108 ficheros); `tsc -b`, `knip` limpios; eslint 0 errores / 1 warning conocido (React Compiler + `useVirtualizer`, no accionable) |
| CI de `main` | ✅ todo salvo ❌ E2E (`livetv-zap`, `server-down`): causas encontradas y arregladas en la rama (SQLite `SQLITE_BUSY` + tests que ya no probaban lo que decían). E2E local con Chrome: 20/20 |
| Rama de trabajo | `claude/stoic-feynman-pyojkw` (desde `main` `b840d0b`, sin mergear): federación con DirectPlay + pósters en miniatura. Pareja de la rama homónima de la app de TV ("Compartido contigo") |
| Audit playback 2026-06-10 | P0/P1/P2 ✅. **P3 abierta** (ver tabla de trabajo abierto) |
| Audit torrent 2026-09-10 | **Todos los hallazgos ✅** (T-8/T-9 el 2026-11-26). Queda: tests de handler `Play`/`HLSPlaylist`/`HLSSegment` y refcount explícito VOD (T-6) |
| Audit prod 2026-06-08 | Fases 0/1/2 ✅. Fase 3: M18/M19/M20 ✅, M24 parcial, M21/M23 abiertos. Fases 4–5 abiertas |
| Audit federación 2026-06-12 | F-1/3/4/10 ✅, F-2 parcial; F-5..F-9, F-11..F-13 abiertos. **Nuevo 2026-11-26**: DirectPlay federado daba 409 (arreglado en la rama) |
| Producción (servidor de la TV, `192.168.1.100:8097`) | Sigue con una imagen anterior a la caché de subtítulos, el id de instalación y el fix de búsqueda. **Pendiente desplegar.** |

### Revisión pre-app de TV (2026-11-26, segunda sesión del día)

Local, sobre `main` (`0d3cefd`): build, vet, `go test` (45 paquetes),
golangci-lint v2.5.0 (0 issues), `tsc -b`, knip, eslint y vitest 783/783
**verdes**. Pero el CI de `main` está en rojo desde #552:

- **Vulnerability scan** (15): 11 de la librería estándar (net/http,
  crypto/tls, crypto/x509, net/textproto) con arreglo **solo en Go
  1.26.9–1.26.11**: Go 1.25 ya no recibe parches (salió 1.27). Más
  `x/net` 0.54 → 0.60, `otel/trace` 1.43 → 1.47, `anacrolix/torrent`
  1.61 y `gorilla/websocket` 1.5.3 (PRNG débil en la máscara).
  **Hecho en la rama:** Go 1.26.11 (`go.mod` + imagen
  `golang:1.26.11-alpine` del Dockerfile), `anacrolix/torrent` 1.63,
  `x/net` 0.63, otel 1.47, `gorilla/websocket` con el fix. Build, vet y
  `go test ./...` verdes con 1.26.11; compila también con 1.27.4, así que
  la trampa de "compilar siempre con el Go 1.25 de `~/sdk/go`" desaparece.
  `govulncheck` no se pudo correr aquí: lo confirma el CI.
- **Lint** (consecuencia de lo anterior): golangci-lint v2.5.0 está
  compilado con Go 1.25 y se niega a cargar un módulo `go 1.26`. Subido a
  v2.15.0 en `ci.yml`. Esa versión trae una regla nueva de gosec, **G703**
  (path traversal por taint analysis), con **43 avisos** en handlers
  (media/image, trickplay, stream, iptv logo, uploads, torrent HLS…).
  **Revisados los 43 a mano:** 42 falsos positivos (pasan por
  `IsSafePathSegment`, regex de segmento + `filepath.Dir`,
  `SanitizeSubpath`/`ResolveSubpath`, cachés por sha256 o rutas de la BD,
  que el análisis no reconoce) y **1 real**: la subida de imagen de
  colección (`POST /collections/{id}/images/{type}/upload`) usaba el id
  ya decodificado en el nombre del fichero, y `..%2F..%2Fx` escribía la
  imagen fuera de `collection-images/` (solo admin, contenido limitado a
  imagen válida). Arreglado en `parseCollectionImageRoute` (id con
  `IsSafePathSegment` → 400) con
  `TestCollectionHandler_UploadImage_RejectsTraversalID` (falla sin el
  fix). Con eso, G703 excluido en `.golangci.yml` como G304, con el
  motivo apuntado. El errcheck nuevo (`procutil_unix_test.go`), arreglado.
- **E2E** (tras #553 seguía rojo: `livetv-zap` y ahora `server-down`).
  Reproducido en local con `/usr/bin/google-chrome` (el CI usa Chrome; el
  Chromium de Playwright no trae H.264 y falla por otras razones). Cuatro
  causas, ninguna de "flake":
  - **Bug real — SQLite `SQLITE_BUSY` con escrituras concurrentes.** Con
    `BEGIN DEFERRED`, dos transacciones de escritura a la vez (dos
    bibliotecas escaneando, o un escaneo + importar canales IPTV/EPG)
    fallaban al instante sin esperar `busy_timeout`: el scanner se saltaba
    el fichero hasta el siguiente escaneo ("create item: database is
    locked"). En el E2E, la importación de canales coincide con el escaneo
    de pelis/series → canal sin reproducir (`readyState 0`). Fix:
    `_txlock=immediate` en el DSN (`db/sqlite.go`; todos los `BeginTx`
    escriben). Test: `TestItemRepository_IngestItem_ConcurrentWritersDontFail`
    (4×150 ingestas; sin el fix fallan varias).
  - **`server-down` dejó de probar nada con #552**: la peli de prueba
    (5 min, bitrate ínfimo) cabe entera en el buffer de hls.js (techo
    `maxMaxBufferLength` 600 s) y el remux por segmentos la sirve en
    segundos, así que el seek fijo a 200 s nunca tocaba la red. Peli de
    15 min y seek calculado tras el final de lo bufferizado (falla con
    mensaje claro si toda la peli está en buffer).
  - **`upnext`, carrera del test**: `waitForPlayback(0.5)` se cumplía con
    el `<video>` del episodio terminado (t=30). Ahora espera reproducción
    dentro del rango del episodio nuevo.
  - **`playback`, carrera del test**: al cerrar, ItemDetail guarda progreso
    y DESPUÉS `navigate(-1)`; reabrir enseguida chocaba con esa navegación.
    Ahora espera a salir de la ficha.
  - Harness: el stdout del server (slog, nivel warn) va al log de CI y
    `livetv-zap` vuelca el estado del `<video>` al fallar (antes: solo
    "timeout", imposible de diagnosticar).
  - **`livetv-zap` en CI, la causa real** (sacada con ese diagnóstico): el
    runner es ya **Ubuntu 26.04 con ffmpeg 8.0.1**. El upstream del test
    servía el `.ts` finito de golpe y contaba con `-reconnect_at_eof`
    para repetirlo; ese ffmpeg no lo repite, se come los 180 s en
    ~200 ms y sale con código 0 sin un segmento → 502 TRANSMUX_FAILED. En
    local (ffmpeg 6 y master de BtbN) sí reconecta, por eso no se veía.
    Un canal real nunca llega a EOF: ahora el upstream emite a ~1,5x
    tiempo real tras 4 s de ráfaga, y el fixture tiene keyframe cada 2 s
    (antes 25 s, el default de x264 a 10 fps). Además, "ffmpeg salió
    limpio sin segmento" se logueaba a Info sin stderr: ahora Warn.
  - **Pendiente (producto, no bloquea)**: un upstream IPTV *finito* (p. ej.
    una entrada de M3U que en realidad es un fichero) que ffmpeg procese
    entero antes de que el vigilante vea el primer segmento acaba en 502
    aunque haya generado todo. Raro en directos reales; apuntado.
  - Ruido: "segmented remux failed: context canceled" (sesión parada con
    un segmento a medias al cerrar el player) pasa de ERROR a debug.
- **Release macOS**: `https://evermeet.cx/ffmpeg/getrelease/zip` devuelve
  **410 Gone**. Arreglado en `scripts/fetch-ffmpeg.sh`: la URL versionada
  (`ffmpeg-9.1.zip`) se saca de la API de info (`download.zip.url`). No
  verificable desde este entorno (la descarga redirige a un mirror en el
  puerto 4242 que el proxy corta): lo confirma el siguiente Release. La
  API de info ya no publica `sha256`, así que macOS sigue en verificación
  `soft` (ya lo estaba).

### Sesión 2026-11-26 — retomar + cerrar pendientes concretos

- **Búsqueda: 500 con puntuación** (`GET /items/search?q=Toc,%20toc`,
  apuntado el 2026-09-16). El texto del usuario iba crudo al `MATCH` de
  FTS5 y `,` `"` `(` `-` `:` `^`… son sintaxis FTS5 → `fts5: syntax error`.
  Fix: `toFTS5Prefix` (tokeniza como `unicode61`, entrecomilla cada token,
  prefijo `*` en el último; solo puntuación ⇒ sin resultados) en
  `db/item_repository.go` y copia privada en `federation/storage`
  (`SearchSharedItems` tenía el mismo bug). Postgres ya saneaba. Tests:
  `TestItemRepository_List_FTSSearch_PunctuationIsNotSyntax` (reproduce el
  error exacto sin el fix) + caso en `TestFederationRepository_SearchSharedItems`.
- **Torrent T-8** (de-dup por infohash): `claimDownload` registra holder +
  infohash bajo `m.mu`; un segundo job activo del mismo contenido (magnet
  vs `.torrent`) falla con `ErrDuplicateDownload` sin soltar el torrent
  del original.
- **Torrent T-9** (cancelación + timeout): ctx propio por job (hijo de
  `dlCtx`). `DELETE /torrent/downloads/{id}` cancela jobs en cola o
  descargando (suelta holder, limpia scratch, deja de publicarse por SSE);
  409 **solo** mientras copia a la biblioteca (cortar ahí dejaría ficheros
  a medias). `torrent.download_stall_timeout` (default 30m) falla un job sin
  progreso con `ErrDownloadStalled`. Panel admin: × "Cancelar descarga" en
  activos, sin botón al copiar. Claves i18n `downloads.*` añadidas en
  es/en (antes no existían; salía siempre el `defaultValue` en español).
- **Tests nuevos:** `download_lifecycle_test.go` con cliente torrent real
  pero hermético (seam `tuneClientConfig`: sin DHT/trackers, puerto
  efímero; el stall usa un `.torrent` servido por httptest sin seeds) —
  estables con `-race -count=20`. `DownloadsPanel.test.tsx`.
- **Docs:** este fichero reorganizado como guía; checklist de inicio.
- **QR de emparejado:** escanear el QR de la TV con la cámara nativa del
  móvil abre `/link?code=…`; sin sesión, `ProtectedRoute` mandaba a
  `/login` y **el código se perdía**. Ahora `state.from` sobrevive al
  login y a "¿Quién está viendo?" (`utils/returnTo.ts`, solo rutas
  internas: bloquea `//host` → open redirect). En HTTP (sin cámara en la
  página) `/link` explica que sirve la cámara nativa en vez de esconder
  el botón sin más. Tests: `returnTo.test.ts`, `Login.test.tsx`,
  `LinkDevice.test.tsx`.
- **Saltos adelante/atrás medidos con el binario real**
  (`scripts/seek-check.py`, nuevo): Transcode ✅ 11/11 alineados; **remux
  ❌ 6/11 segmentos +2 s** (manifest sintético de 6 s vs cortes en los
  keyframes del fichero). Probable causa de saltos raros en ExoPlayer.
  Diseño del arreglo en `roadmap-producto.md`, pilar 2.
- **`roadmap-producto.md`** (nuevo): visión, 5 pilares con estado medido
  y criterio de "terminado", y lo que le falta a la app de TV (no consume
  federación; servidor sin desplegar).

- **Remux por segmentos (pilar 2, la pieza principal de la sesión)**:
  el remux (vídeo copiado) ya no usa un ffmpeg largo con manifest
  sintético de 6 s. Índice de keyframes cacheado por fichero
  (`stream/keyframes.go`, caché en `<cache>/../keyframes/`) → plan con
  los cortes reales (`segplan.go`) → cada segmento es un ffmpeg corto e
  independiente (`remux.go`, 60–90 ms; prefetch de 3). Ir adelante o
  atrás es lo mismo: `RestartSessionAt` es no-op en estas sesiones.
  Receta validada fotograma a fotograma (MKV/MP4, GOP abierto/cerrado,
  B-frames, keyframes irregulares, audio AC3→AAC sobre rejilla común) y
  documentada flag a flag en `remux.go`. Si no hay índice en 3 s, el remux
  continuo de antes (la indexación sigue y la próxima vez va por
  segmentos); `/stream/{id}/info` precalienta el índice. La federación
  también sirve el manifest del plan y los segmentos bajo demanda (antes
  dependía del manifest de ffmpeg, que en este modo no existe).
  `seek-check.py` ampliado a 4 películas (regular, irregular+B-frames,
  GOP abierto+AC3 en MP4, transcode) con desfase constante ±0,1 s y
  continuidad entre segmentos: **verde**. Tests: unitarios de índice,
  plan, args y remuxer; integración con ffmpeg real
  (`remux_integration_test.go`, verificada con mutaciones); handler
  local y de federación. Diseño en `docs/architecture/streaming.md`
  § Segmented Remux.

### Sesión 2026-11-26 (tarde) — app de TV con todo lo que da el backend

Inventario de rutas de `openapi.yaml` contra lo que llama la app de TV
(detalle en `Hubplay_Demo_Android/docs/memory/archive/project-status-detalle-2026-11-26.md`).
La app no usaba federación, ni liberaba sesiones, ni el logout del
servidor, ni "quitar de Continuar viendo": hecho en la rama
`claude/stoic-feynman-pyojkw` de la app. Del lado del servidor:

- **Bug: federación + DirectPlay = imposible de ver** (web y TV). La
  federación solo proxya manifest + segmentos, pero el amigo podía decidir
  DirectPlay (MP4 H.264/AAC) y su playlist de calidad respondía 409
  `WRONG_METHOD`. `StartSessionRequest.RequireHLS` (las dos llamadas
  federadas lo piden) rebaja DirectPlay a remux con `stream.RequireHLS`
  (audio copiado si cabe en TS, AAC si no; contenedor no remuxable →
  transcode). Tests: `TestRequireHLS_*` y
  `TestFederationStream_SessionsRequireHLS` (falla sin el arreglo).
- **Pósters de amigos en miniatura**: `GET /me/peers/{p}/items/{i}/poster`
  reenvía `?w=N` al amigo (antes cada tarjeta bajaba el original por dos
  servidores). Id escapado en la ruta. Test
  `TestProxyPeerItemPoster_ForwardsWidth`.
- `go vet` + `go test ./...` (45 paquetes) verdes con Go 1.26.11.

**Hallazgos de esta sesión sin arreglar:**
- `gofmt -l internal/` lista ~145 ficheros: el gofmt de Go ≥1.19 reformatea
  listas dentro de comentarios de doc (y algún orden de import). El CI no
  lo exige. Si se quiere limpiar: un commit aislado `gofmt -w` sin más
  cambios, nunca mezclado con trabajo real.
- 13 PRs de Dependabot abiertos. **Ya cubiertos por #551** (cerrables):
  #547 gorilla/websocket 1.5.3, #543 pion/dtls 3.1.4, #542 pion/stun 3.1.5.
  **Majors que requieren trabajo**: #539 react-router 7→8, #511 golang
  1.26 (**superado** por la rama de la revisión: Go 1.26.11 + torrent
  1.63; #532 x/net también), #508 pnpm/action-setup 6, #507/#506 acciones
  de docker. Resto (#550 vitest, #549 sharp, #540 go-deps, #509 alpine
  3.24): probar y mergear uno a uno.

---

## ▶️ Próxima sesión (en orden)

0. **CI de `main` en verde**: la rama de la revisión entró como #554
   (E2E) y #555 (next-up con artwork) — `main` en `b840d0b`. Confirmar en
   GitHub Actions que CI, Docker y Release terminaron en verde. Después,
   PR de `claude/stoic-feynman-pyojkw` **esperando al CI** (#552 y #553 se
   mergearon con el CI a medias).
1. **Desplegar** `main` en el servidor de la TV (lleva #552: remux por
   segmentos, QR, búsqueda) y probar el QR con el móvil en real. Incluir
   la rama `claude/stoic-curie-9je0wx` (2026-11-26): `/me/next-up` ahora
   manda artwork (`poster_url`/`backdrop_url` del episodio y
   `series_poster_url`/`series_backdrop_url`/`series_logo_url`); sin ella
   el rail "A continuación" de la app de TV sale con tarjetas vacías.
2. **Probar saltos en la Mi TV** con el remux por segmentos (hecho
   2026-11-26, `seek-check.py` verde); después, indexar keyframes en
   segundo plano tras el escaneo y el reinicio inmediato del transcode
   en saltos lejanos (`roadmap-producto.md` pilar 2, "Pendiente").
3. ~~**App TV: "Compartido contigo"**~~ hecho en las ramas
   `claude/stoic-feynman-pyojkw` (app + servidor, 2026-11-26): mergear con
   CI verde y probar con un segundo servidor emparejado. Queda F-5/F-6 en
   servidor (cortar el stream federado al desconectar; segmentos fuera del
   rate-limit) y compartir series (hoy solo items de primer nivel).
4. **Dependabot**: cerrar los 3 ya cubiertos; mergear los menores tras CI;
   decidir react-router 8 y golang 1.26 (este último bloqueado hasta que
   `anacrolix/torrent` compile con x/net nuevo / Go ≥1.27).
5. **Torrent — tests de handler** `Play`/`HLSPlaylist`/`HLSSegment`
   (segmento inválido → 400, infohash desconocido → 404, mapeo de modos).
6. **Fase 3 prod**: M23 (validación de config + overrides de env), cerrar
   M24 (`example.yaml` completo), M21 (alerting/agregación documentado).
7. **Playback P3**: PB-29/30 (DNS-rebind vía `Control` del `net.Dialer`;
   cierra también B2), PB-26, PB-33, PB-39 (`SubtitleTrack` índice → 400).
8. **Pequeños**: snake_case en `POST /setup/libraries`; `/health` fuera de
   `/api/v1` devuelve la SPA.
9. **Fase 5 gobernanza**: `SECURITY.md`, `CODEOWNERS`.

---

## 📋 Trabajo abierto

**Roadmap principal:** `audit-2026-06-10-playback-chain.md`.

| Prioridad | Tema | Items |
|---|---|---|
| **Alta** | **Remux por segmentos: probar en TV + indexar tras escaneo** | Remux arreglado 2026-11-26 (`seek-check.py` verde). Falta verificación en ExoPlayer real e indexación previa de ficheros grandes. Pilar 2 |
| **Alta** | **Federación en la TV: probar** | App ("Compartido contigo") y servidor (DirectPlay federado, pósters `?w=`) hechos en `claude/stoic-feynman-pyojkw`, sin mergear ni probar en real. Pilar 3 |
| Media | **Playback P3** | PB-19, PB-26, PB-29/30/31, PB-33, PB-36..39. Gaps de test: **1** (integración ffmpeg real en `stream/transcode`; solo el VOD de torrent la tiene), **4** (`rewriteHLSPlaylist`/`resolveURL` de IPTV sin tests), **5** parcial (Segment: seek-restart, 429, Stop↔Restart con `-race`), **6** parcial (trickplay: ACL ✅, TryLock/regeneración no). Gaps 2, 3 y 7 ✅ |
| Media | **Torrent** | Tests de handler Play/HLS; T-6 refcount explícito (holder) desde el VOD |
| Media | **Fase 3 — observabilidad/config** | M21, M23, resto de M24 |
| Media | **Fase 4 — frontend** | B10 (ESLint type-aware), B14 (tests de páginas grandes) |
| Media | **Dependencias** | 13 PRs de Dependabot (ver hallazgos arriba) |
| Baja | **Fase 5 — gobernanza** | `SECURITY.md`, `CODEOWNERS` (README ✅) |
| Baja | **Bajos sueltos** | B2 (DNS-rebind; se cierra con PB-29/30), B3 (refresh TTL 30d), M6 (backup periódico), snake_case en `POST /setup/libraries`, `/health` sin prefijo → SPA |

**Features de producto (gap vs Jellyfin/Plex, no son bugs):**
Chromecast, SyncPlay, control remoto de sesiones, ajustes de
apariencia/offset de subtítulos (fácil: el render es propio —
`useSubtitleOverlay`), audio boost. Y retirar del backend los endpoints de
subtítulos online que ya no tienen consumer (`/subtitles/external*` +
provider OpenSubtitles).

**Acciones de OPERADOR (no de código):**
- Desplegar la versión actual en el servidor de la TV.
- `NSSM_EXPECTED_SHA256`: tras la próxima release, copiar el sha256
  logueado, contrastarlo y fijar la repo variable.
- SignPath: aplicar en signpath.org + `vars.HUBPLAY_SIGNING_ENABLED`.
  Guía: `docs/architecture/windows-installer-signing.md`.
- Probar el instalador Windows de la próxima release (W-1/W-2 corregidos
  el 2026-09-10; el servicio instalado en la máquina de desarrollo es de
  una release anterior y tiene ambos bugs).

**Pendientes menores (de audits cerrados):** TT-8 resto (comentarios en
inglés en sub-paquetes de handlers, incremental), F15-10/11/12 (polish),
distribución avanzada (auto-update, TLS LAN, macOS notarized, AppImage).

---

## 🏛 Referencias vivas

- `roadmap-producto.md` — **visión y pilares** (qué es "terminado"),
  estado medido, qué le falta a la app de TV.
- `scripts/seek-check.py` — prueba de saltos adelante/atrás contra el
  binario real (criterio de "terminado" del pilar de reproducción; 4
  películas: remux regular, irregular+B-frames, GOP abierto+AC3, transcode).
- `architecture-decisions.md` — ADRs (AppError, observability/sink,
  keystore, preflight, sqlc adapter, ADR-026 logs).
- `conventions.md` — patrones del codebase, reglas de test, anti-ciclo,
  comentarios en español, regeneración sqlc, desarrollo en Windows.
- `audit-2026-06-10-playback-chain.md` — **roadmap activo** (playback).
- `audit-2026-09-10-torrent-vod.md` — módulo torrent (todo ✅ salvo tests
  de handler HLS).
- `audit-2026-06-12-federation.md` — P2P/federación.
- `audit-2026-06-08-production-readiness.md` — roadmap secundario
  (Fases 3–5 abiertas).
- `perf-benchmarks-2026-05-17.md` — baseline benchmarks dual-backend.
- `web/verify/` — arnés de verificación en navegador (layout real).
- `web/e2e/` — smokes Playwright (ver su README: hace falta Chrome real).

## 📦 Archivo (`archive/`, no se lee al inicio)

- `2026-06-12-to-09-16.md` — P2 playback (VAAPI, ABR, surround, DV),
  smokes E2E, SSRF transmux, revisión de septiembre (audit torrent,
  W-1/W-2 Windows, README), descubrimiento LAN para la app de TV, caché de
  subtítulos, CI en rojo de #551.
- `2026-06-10-supply-chain-and-playback.md` — Fase 2 supply-chain, audit
  playback + P0–P1, PB-40..44, Docker/CI.
- `2026-05-27-to-06-08.md` y anteriores — sesiones históricas.
- Audits cerrados: `audit-2026-05-14-*`, `audit-2026-05-27-*`, 2026-04/05.

---

## 🧠 Aprendizajes transversales

- **Notify-channel + deadline** para tests determinísticos (canon F15-1):
  buffer 32, send non-blocking, `select { case <-notify; case <-deadline }`.
- **Sink pattern** para observability: interfaces locales por paquete con
  `noopSink{}` default. Evita ciclos de import.
- **Package-level seam** (`var timeNow = time.Now`,
  `var tuneClientConfig`) cuando la API es ancha o el colaborador es
  externo: idiomático, opt-in para tests.
- **Feature modules** (`library.Module`, `iptv.Module`) con shutdown LIFO.
- **Adapter en la frontera** para no importar `db` en paquetes de dominio.
- **Opt-in via repo variable** (`vars.X_ENABLED`) para features de CI con
  setup externo del operador.
- **Cerrar por análisis** cuando el runtime moderno resuelve el problema
  teórico. No refactorizar sin bug observable.
- **Fix centralizado vs audit por paquete** — un punto en vez de N.
- **Verificación en navegador real para cambios visuales/de layout**:
  jsdom no ve layout.
- **React Compiler + stores externos mutables**: aislar con
  `"use no memo"`; refs no se leen en render.
- **El tipo TS puede mentir sobre el wire** (PB-43): fixtures con la forma
  del WIRE; conversión en la frontera del cliente.
- **Squash-merge de rama viva = conflictos en cascada**. Rama nueva tras
  cada merge.
- **Multi-arch sin QEMU**: `--platform=$BUILDPLATFORM` + `GOOS/GOARCH`.
- **Guards de drift** (OpenAPI, sqlc-verify): correr `go test ./...`
  COMPLETO antes de cada push.
- **(Nuevo) Nunca texto de usuario crudo en un lenguaje de consulta**
  (FTS5 `MATCH`, `to_tsquery`): sanear en la frontera, en TODOS los
  dialectos y TODOS los call sites (el bug de FTS5 estaba duplicado en
  federación).
- **(Nuevo) `cmd.StderrPipe()` + `cmd.Wait()` pierde líneas**: `Wait`
  cierra el read-end al reapear. Pipe propio (`os.Pipe`) si el tail de
  stderr decide algo (fix de #551).
- **(Nuevo) Prototipar ffmpeg antes de codificar**: el remux por
  segmentos destapó 6 trampas de ffmpeg (cortes relativos al arranque,
  `-ss` de MKV que cae antes, `-ss` de salida que pone tiempos a 0, `-to`
  por DTS, DTS negativo solo en el segmento 0, SPS/PPS ausentes en
  keyframes no IDR) que una implementación "de memoria" habría metido.
  Validar fotograma a fotograma con ffprobe y fijarlo con tests de
  integración + mutaciones.
- **(Nuevo) No mergear sin el CI terminado**: #552 se mergeó 11 s después
  de abrir el PR y `main` quedó en rojo en 3 jobs (y sin nightly).
- **(Nuevo) Una versión de Go sin soporte acumula CVEs de stdlib sin
  arreglo posible**: cuando sale Go 1.N+2, el 1.N deja de recibir
  parches. No fijar el proyecto a una versión por una dependencia;
  subir la dependencia (aquí `anacrolix/torrent` 1.63 resolvió el
  bloqueo con 1.27).
- **(Nuevo) Apuntar el trabajo al cerrar sesión**: dos veces se ha
  reconstruido trabajo no documentado desde el git log.
