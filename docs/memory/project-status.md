# Estado del proyecto

> **Entrypoint de cada sesión.** Solo el estado vivo y lo que falta. El
> detalle de sesiones cerradas vive en `archive/` (no se borra nada, solo
> se reubica). Última limpieza: 2026-06-10.

---

## 🔭 Estado actual (2026-09-10, retomando tras ~3 meses parado)

**Salud:** MVP funcional, cerca de early-production. `main` == `origin/main`
(`f30a4709`), sin ramas locales con trabajo sin fusionar.

| Área | Estado |
|---|---|
| Tests backend | `go test ./...` verde en Windows (43 paquetes) tras arreglar 1 test no portable (`setup/service_test.go`, ruta absoluta estilo Linux) |
| Tests frontend | **763/763** vitest (106 ficheros); `tsc -b`, `knip` limpios; eslint 0 errores / 1 warning informativo (React Compiler + `useVirtualizer`, no accionable) |
| Rama de trabajo | `main` (cambios de esta sesión sin commitear al cierre; ver abajo) |
| Audit playback 2026-06-10 | P0/P1/P2 ✅. **P3 abierta**: PB-19/26/29-31/33/36-39 + gaps de test 1-6 |
| Audit prod 2026-06-08 | Fases 0/1/2 ✅. Fase 3: **M18, M19, M20 ✅ (2026-09-10)**, M24 parcial; M21/M23 abiertos. Fases 4–5 abiertas |
| Audit federación 2026-06-12 | F-1/3/4 ✅, F-2 parcial, **F-10 ✅ (2026-09-10)**; F-5..F-9, F-11..F-13 abiertos |
| **Audit torrent 2026-09-10** (NUEVO) | `audit-2026-09-10-torrent-vod.md`: T-1..T-7, T-10, T-11 ✅; T-8, T-9 abiertos |

**Trabajo del 15–16 de junio que NO estaba documentado aquí** (reconstruido
del git log; commits `eafd22e7..f30a4709`, PRs #526–528 "dual indexers
search"): descargar torrent a biblioteca (jobs + refcount + scratch + SSE +
rescan), pestaña admin "Indexadores" (Prowlarr/Torznab en DB), búsqueda
general por texto y descubrimiento por carátulas TMDb → fuentes por imdbid,
Prowlarr turnkey en compose (`docker-compose.torrent.yml`), 429 claro +
aviso de códec, y **remux/reencode VOD de torrents a HLS**
(`internal/torrentstream/vodtransmux*.go`, rutas `/torrent/play` y
`/torrent/hls/{infohash}/…`). Todo mergeado en `main`.

**Sesión 2026-09-10 — revisión general + arreglos:**
- **Entorno Windows**: Go no estaba instalado (winget se quedó esperando
  UAC) → instalado por zip en `~/sdk/go` (1.25.11, sin admin). `web/
  node_modules` estaba desfasado del lockfile → `CI=true pnpm install
  --frozen-lockfile` (sin TTY pnpm aborta el borrado de `node_modules`).
  Ver `conventions.md` § "Desarrollo en Windows".
- **Torrent (audit nuevo)**: T-1 SSRF con `allow_private_upstreams`
  (`imaging.SafeGetWith` + `SafeGetOpts{AllowPrivate}` — relaja el rango,
  no desactiva el guard), T-2 doble ffmpeg por `Prepare` concurrente
  (placeholder + `ready`), T-3 goroutine de stderr filtrada, T-4 `Stop`
  espera la salida + `watchExit` tira sesiones con ffmpeg muerto, T-5 SSE
  de descargas solo a admins, T-6 touch del torrent al servir HLS, T-7
  containment de rutas al copiar a la biblioteca, T-10 alias `bySrc`,
  T-11 `Close`/`Shutdown` idempotentes. 12 tests nuevos.
- **Prod Fase 3**: M18+M19 (`RequestLogger` con `handlers.ClientIP` y
  `log_ips` honrado), M20 (`api.Recoverer` propio con slog + métrica
  `panic`), M24 parcial (`streaming:` en `hubplay.example.yaml`).
- **Federación**: F-10 (userinfo en URL de peer rechazado).
- **Frontend**: directiva `eslint-disable` sin uso en `PageHeader.tsx`.
- **Arranque nativo en Windows verificado** (binario + SQLite, sin Docker,
  puerto 8097 porque el servicio instalado ocupa el 8096): wizard por API,
  scan de `peliculas/` + `series/` (2 películas, 1 serie/9 episodios),
  login y páginas en Chrome headless. Destapó **dos bugs de playback
  exclusivos de instalación nativa** (en Docker no se ven porque `/cache`
  es absoluto y el FS es Linux), ambos corregidos con test:
  - **W-1** · La clave de sesión `user:item:profile:audio:sub` se usaba
    como nombre de directorio → `:` es inválido en Windows → TODO
    transcode/direct-stream fallaba con `mkdir … sintaxis … no correcta`.
    Fix: `sessionDirName` (`:` → `_`) en `stream/transcode.go`.
  - **W-2** · `cache_dir` relativo + `cmd.Dir = outputDir` ⇒ ffmpeg
    resolvía la ruta de manifest/segmentos dentro del propio outputDir →
    "Could not write header: No such file or directory". Fix: base dir
    absoluto en `NewTranscoder` y en `iptv.NewTransmuxManager`.
  - Tras los fixes: playlist 720p + 3 segmentos `video/mp2t` servidos.
- **README.md creado** (no existía): instalación con Docker y sin Docker
  (instalador Windows, `install.sh` Linux, tar.gz manual), desarrollo.
- `handlers.ClientIP` quita el puerto en el fallback a `RemoteAddr`.

**Hallazgos menores sin arreglar (esta sesión):**
- `POST /setup/libraries` responde con nombres de campo Go (`ID`, `Name`,
  `ContentType`…) en vez de snake_case como el resto de la API.
- `/health` fuera de `/api/v1` cae al fallback de la SPA (devuelve HTML);
  el probe real es `/api/v1/health` y `/health/live`.
- Un `hubplay.exe` de una release antigua sigue instalado como servicio
  (`C:\Program Files\HubPlay`, puerto 8096) en la máquina del usuario.

**Próximos pasos sugeridos (en orden):**
1. Commit + PR de esta sesión (`go test ./...` y vitest verdes).
2. Torrent T-8/T-9 (de-dup por infohash, cancelación/timeout de descargas)
   + tests del handler `Play`/`HLSPlaylist`/`HLSSegment`.
3. Fase 3 restante: M21 (agregación de logs/alerting documentada), M23
   (validación de config + overrides de env), cerrar M24.
4. Playback P3 (PB-29/30 DNS-rebind vía `Control` del dialer cierra también
   B2; PB-26; PB-33) y gaps de test 1-6.
5. Fase 5 gobernanza: `SECURITY.md`, `CODEOWNERS` (README ✅ 2026-09-10).
6. Snake_case en la respuesta de `POST /setup/libraries`; probar el
   instalador Windows de la próxima release con W-1/W-2 (el servicio
   instalado hoy es de una release anterior y tiene ambos bugs).

---

## 🔭 Estado anterior (2026-06-14, fin de sesión)

**Salud:** MVP funcional, cerca de early-production.

| Área | Estado |
|---|---|
| Tests backend | `go test ./...` verde (`-race` en stream/api/iptv) |
| Tests frontend | **748/748** vitest; `tsc`, `eslint` y `knip` limpios |
| Rama de trabajo | `claude/review-recent-work-9j5lg7` — SSRF transmux cerrado |
| Audit playback 2026-06-10 | P0 + P1a-d + PB-40..44 + **P2 ✅ (2026-06-12)**. **P3 en curso**: smoke E2E (a)(b)(e) ✅ |
| Audit prod 2026-06-08 | Fases 0/1/2 + B7 ✅. **Fases 3–5 abiertas** |

✔️ Checklist de retorno 2026-06-12 hecho: PR #518 mergeada, CI/Docker/
Release verdes en main (`cfafee0`), rama nueva desde main.

**Sesión 2026-06-12 — Playback P2 (PB-5/10/22/23):**
- **PB-5**: pipeline VAAPI real (`-init_hw_device` + `format=nv12,
  hwupload` al final de la vf chain, tonemap/overlay software antes del
  upload), `verifyEncoder` ejercita el pipeline real con razón
  diagnóstica, `FallbackReason`/`Device` expuestos en
  `/admin/system/stats` + tile GPU en warning, device configurable
  (`hardware_acceleration.device`), mismo hwupload en el reencode del
  transmux IPTV.
- **PB-10**: `stopSiblingSessions` al cambiar de variante, caps que solo
  cuentan/bloquean full-transcode (remux exento), master playlist
  filtrado por resolución de la fuente.
- **PB-22**: `channels=` en capabilities (web emite 6), `AudioChannels =
  min(src, client, 6)` de `Decide` a `-ac`.
- **PB-23**: DV vía `side_data_list` (DOVI record) con mapeo de
  `dv_bl_signal_compatibility_id` → base compatible o DolbyVision puro.
  ⚠️ items ya escaneados necesitan re-probe para re-etiquetar.

**Sesión 2026-06-12 (cont.) — Smoke E2E Playwright (P3, gap de test 7):**
- Harness en `web/e2e/`: cada spec arranca su servidor real (binario
  con SPA embebida) y lo aprovisiona por API (wizard → admin →
  bibliotecas → scan); fixtures de media generados con ffmpeg
  (película MKV 2-audios → DirectStream/HLS; episodios MP4).
- 3 smokes verdes: play→seek-restart→close→resume · backend SIGKILL
  mid-play→ErrorOverlay acotado (PB-16) · ended→UpNext→siguiente.
- Job `e2e-smoke` en ci.yml (paralelo; promover a `build.needs` cuando
  demuestre estabilidad). data-testid nuevos: `player-error-overlay`,
  `upnext-overlay`.
- ⚠️ Los Chromium de Playwright NO decodifican H.264/AAC (open codecs):
  local → `PW_CHROME` con Chrome/Chrome-for-Testing; CI → Chrome del
  runner (`channel: "chrome"`). Documentado en `web/e2e/README.md`.
- **(cont. 2)** Smokes (c) dub-switch (menú Audio → `?audio=1` →
  resume al playhead) y (d) LiveTV zap (upstream M3U+MPEG-TS sintético
  del propio test → import → transmux → zap por "Canales similares")
  ✅. **Los 5 escenarios E2E del audit cubiertos.**
- ✅ **Hallazgo (B2-adyacente) CERRADO (2026-06-14)**: `isSafeUpstream`
  solo cubría el proxy passthrough — el **transmux lanzaba ffmpeg
  contra la URL upstream sin validarla**. Cerrado: `GetOrStart` valida
  `isSafeUpstream` antes de spawnear (métrica `unsafe_upstream`, error
  → 502 `UPSTREAM_BLOCKED` en el handler). Knob nuevo
  `iptv.allow_private_upstreams` (default false) que relaja el guard en
  AMBOS planos (proxy + transmux) para tuners de LAN (HDHomeRun,
  tvheadend) y loopback. El E2E (`helpers/server.ts`) lo activa para su
  upstream sintético. Tests: `TestIsSafeUpstream_AllowPrivate`,
  `TestTransmuxManager_GetOrStart_RejectsUnsafeUpstream`. Documentado en
  `hubplay.example.yaml`.

**Sesión 2026-06-14 — SSRF transmux (cierre del hallazgo arriba):**
Ver bloque ✅. Nota: PB-29 (LookupIP sin ctx) y PB-30 (TOCTOU
DNS-rebind vía dialer Control) siguen abiertos — son un endurecimiento
distinto (validar en el `Control` del `net.Dialer`), no el hueco de
cobertura que se acaba de cerrar.

- **Pendiente P3**: PB-19/26/29-31/33/36-39 + resto de gaps de test
  del audit (1-6).

---

## 📋 Trabajo abierto

**Roadmap principal:** `audit-2026-06-10-playback-chain.md` (cada item
con ✅/pendiente y fix propuesto).

| Prioridad | Tema | Items |
|---|---|---|
| Media | **Playback P3** | Smoke E2E Playwright (play→seek→resume, UpNext, dub-switch, LiveTV zap, server caído), PB-19/26/29-31/33/36-39 + gaps de test del audit |
| Media | **Torrent** (audit 2026-09-10) | T-8 (de-dup descargas por infohash), T-9 (cancelación/timeout de descargas), tests de handler Play/HLS |
| Media | **Fase 3 — observabilidad/config** (audit prod) | M21, M23, resto de M24 (alerting documentado, validación de config, completar `example.yaml`). ✅ M18/M19/M20 (2026-09-10) |
| Media | **Fase 4 — frontend** | B10 (ESLint type-aware), B14 (tests de páginas grandes) |
| Baja | **Fase 5 — gobernanza** | `SECURITY.md`, `CODEOWNERS` (README ✅ 2026-09-10) |
| Baja | **Bajos sueltos** | B2 (DNS-rebind TOCTOU — parte se arregla con PB-29/30), B3 (refresh TTL 30d), M6 (backup periódico) |

**Features de producto (gap vs Jellyfin/Plex, no son bugs):**
Chromecast, SyncPlay, control remoto de sesiones, ajustes de
apariencia/offset de subtítulos (fácil ahora: el render es propio —
`useSubtitleOverlay`), audio boost. Y retirar del backend los endpoints
de subtítulos online que ya no tienen consumer
(`/subtitles/external*` + provider OpenSubtitles).

**Acciones de OPERADOR (no de código):**
- `NSSM_EXPECTED_SHA256`: tras la próxima release, copiar el sha256
  logueado, contrastarlo y fijar la repo variable.
- SignPath: aplicar en signpath.org + `vars.HUBPLAY_SIGNING_ENABLED`.
  Guía: `docs/architecture/windows-installer-signing.md`.

**Pendientes menores (de audits cerrados):** TT-8 resto (comentarios en
inglés en sub-paquetes de handlers, incremental), F15-10/11/12 (polish),
distribución avanzada (auto-update, TLS LAN, macOS notarized, AppImage).

---

## 🏛 Referencias vivas

- `architecture-decisions.md` — ADRs (AppError, observability/sink,
  keystore, preflight, sqlc adapter, ADR-026 logs).
- `conventions.md` — patrones del codebase, reglas de test, anti-ciclo,
  comentarios en español, regeneración sqlc.
- `audit-2026-06-10-playback-chain.md` — **roadmap activo** (playback;
  P0/P1/P2 ✅, P3 abierta; PB-40..44 de reportes de usuario ✅).
- `audit-2026-09-10-torrent-vod.md` — **NUEVO** audit del módulo torrent
  (streaming, remux VOD, descargas, fuentes). T-1..T-7/T-10/T-11 ✅;
  quedan T-8, T-9 y tests del handler HLS.
- `audit-2026-06-12-federation.md` — audit del módulo P2P/
  federación. Base cripto/auth sólida; abiertos F-1 (SSRF en redirects
  del cliente saliente) y F-2 (cuotas por peer prometidas y no
  implementadas → DoS de recursos locales) como 🟠, + 6 🟡 (exp sin
  techo, revoke no fail-closed, HLS bajo rate-limit, etc.). Key
  rotation y download siguen sin implementar (Phase 2/7).
- `audit-2026-06-08-production-readiness.md` — roadmap secundario
  (Fases 3–5 abiertas).
- `perf-benchmarks-2026-05-17.md` — baseline benchmarks dual-backend.
- `web/verify/` — arnés de verificación en navegador (layout real).

## 📦 Archivo (`archive/`, no se lee al inicio)

- `2026-06-10-supply-chain-and-playback.md` — **esta sesión**: Fase 2
  supply-chain, audit playback + P0–P1, PB-40..44, quick wins del
  player, Docker/CI, incidencias de squash-merges.
- `2026-05-27-to-06-08.md` — endurecimiento prod (Fases 0/1 + Bloques
  1/2), F15-5, TT-8 root, audits 2026-05-27 cerrados.
- `2026-05-19-to-05-27.md` y anteriores — sesiones históricas.
- `audit-2026-05-14-go-backend-review.md` + `intervention-2026-05-14.md`,
  `audit-2026-05-27-architecture-macro.md` +
  `audit-2026-05-27-per-package-review.md` — audits cerrados.
- `per-user-channel-order-spec-shipped.md` y audits 2026-04/05 antiguos.

---

## 🧠 Aprendizajes transversales

Patrones consolidados que vale la pena replicar:

- **Notify-channel + deadline** para tests determinísticos (canon F15-1):
  buffer 32, send non-blocking, `select { case <-notify; case <-deadline }`.
- **Sink pattern** para observability: interfaces locales por paquete con
  `noopSink{}` default. Evita ciclos de import.
- **Package-level seam** (`var timeNow = time.Now`) cuando la API es ancha
  (33+ callsites): idiomático stdlib, opt-in para tests. Mejor que DI
  cuando cambiar el constructor desborda el beneficio.
- **Feature modules** (`library.Module`, `iptv.Module`) con shutdown LIFO.
- **Adapter en la frontera** para no importar `db` en paquetes de dominio
  (structs espejo + conversión en el composition root).
- **Opt-in via repo variable** (`vars.X_ENABLED`) para features de CI con
  setup externo del operador (SignPath, NSSM_EXPECTED_SHA256).
- **Cerrar por análisis** cuando el runtime moderno resuelve el problema
  teórico. No refactorizar sin bug observable.
- **Fix centralizado vs audit por paquete** — un punto en vez de N.
- **Verificación en navegador real para cambios visuales/de layout**:
  jsdom no ve layout (PB-42/PB-44 eran invisibles para los tests).
- **React Compiler + stores externos mutables**: aislar con
  `"use no memo"`; refs no se leen en render (usar useState
  initializer para valores congelados por montaje).
- **(Nuevo) El tipo TS puede mentir sobre el wire** (PB-43): los
  fixtures de test deben tener la forma del WIRE, no del tipo; la
  conversión vive en la frontera del cliente (`normalizeMediaStream`).
  Si un helper tolera dos formas (`stream_type ?? type`), es señal de
  un desajuste sin resolver.
- **(Nuevo) Squash-merge de rama viva = conflictos en cascada** y
  resoluciones manuales peligrosas (main acabó con código duplicado).
  Rama nueva tras cada merge, o merge-commit para ramas largas.
- **(Nuevo) Multi-arch sin QEMU**: stages de build con
  `--platform=$BUILDPLATFORM` + `GOOS/GOARCH=$TARGETARCH`
  (CGO_ENABLED=0). 20min → ~6-8min.
- **(Nuevo) Guards de drift** (OpenAPI router coverage, sqlc-verify)
  cazan lo que el dev olvida — correr `go test ./...` COMPLETO antes de
  cada push, no solo los paquetes tocados.
