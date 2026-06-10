# Sesión 2026-06-10 — Supply-chain (Fase 2), audit + fixes de playback, player UX

> Detalle archivado de la sesión del 2026-06-10 (rama
> `claude/project-review-8tznz4`, mergeada a main vía PRs #505/#513/
> #514/#515 + la PR final). El estado vivo está en `project-status.md`;
> el roadmap con ✅ por item en `audit-2026-06-10-playback-chain.md`.

## Fase 2 — supply-chain / release (audit prod 2026-06-08) ✅

Actions SHA-pineadas (18, con `# vX.Y.Z`), SLSA provenance
(attest-build-provenance) en releases + nightly, SBOM/provenance OCI en
la imagen Docker, verificación sha256 de FFmpeg (digest de la API de
releases de GitHub; evermeet en modo soft — 403 a IPs de runners) y
NSSM (opt-in vía repo var `NSSM_EXPECTED_SHA256`), `.sha256` fatal en
install.sh (`HUBPLAY_SKIP_VERIFY=1` opt-out), Trivy bloqueante en tags,
govulncheck pineado, jobs `sqlc-verify` (GOTOOLCHAIN=auto) + `pnpm
build` en CI, dependabot docker + bases del Dockerfile por digest.
Detalle completo: §"Fase 2 — implementación" del audit de producción.

## Audit de la cadena de playback + implementación P0–P1 ✅

4 sweeps paralelos → 39 hallazgos (4 críticos). Implementado el mismo
día: **P0** (PB-1 alias webm, PB-2 temp_file, PB-3 force_key_frames
prev_forced_t, PB-4 listener error del video), **P1a**
(decisión/transcode: PB-6/7/8/9/20/21), **P1b** (trickplay ACL +
semáforo + negative-cache, timeouts de probe/fpcalc, attached_pic,
stat de fichero borrado), **P1c** (IPTV: 502 real, breaker un-fallo-
por-request + 4xx fail-fast, fuera bsf h264 que mataba HEVC, refcount
de viewers para el zapping), **P1d** (recovery hls.js acotado +
swapAudioCodec, federación markPlayed/cleanup, progreso en
pagehide/pause, fragLoadPolicy transcode, backBuffer 90s, errores i18n).
Detalle con file:line y fixes: el propio audit (todos marcados ✅).

## Reportes de usuario (PB-40..44) ✅

- **PB-40** player alimentado del item de la PÁGINA (temporada/serie →
  sin pickers ni next-up) → usePlayback deriva todo del item EN
  REPRODUCCIÓN.
- **PB-41** subtítulos de texto embebidos sin carril en el picker →
  4º carril "texto local" vía extractor WebVTT (índice absoluto).
- **PB-42** BottomSheet anclado al botón (`absolute` en wrapper
  relative) → tira vertical rota en móvil → `fixed inset-0`.
- **PB-43** **causa raíz de pickers vacíos**: el tipo MediaStream del
  cliente declaraba `type`/`index` pero el wire emite
  `stream_type`/`stream_index` (+ omite campos vacíos) →
  `normalizeMediaStream` en la frontera del cliente; tests con
  fixtures wire-shaped.
- **PB-44** subtítulos nativos pintados en el borde del ELEMENTO de
  vídeo (pisando controles, recortados, solapados) → render propio
  (`useSubtitleOverlay`, pista hidden + overlay con safe-area que sube
  con los controles). `useExternalSubMode` eliminado.

## Decisión de producto

**Fuera "Buscar subtítulos online"** (petición del owner): eliminado
todo el surface frontend (modal, fila del picker, lane externo, métodos
del cliente, tipo, i18n). Los endpoints backend
(`/subtitles/external*`) quedan sin consumer — candidatos a retirarse.

## Player UX (gap analysis vs Jellyfin/Plex)

Quick wins con identidad propia: botones ±10s (arco que gira),
doble-tap por zonas encadenable, **SeekTide** (marea teal + chevrons +
total acumulado), botón PiP, toggle total↔restante persistido, corazón
con latido + anillo. Faltan (producto): Chromecast, SyncPlay, control
remoto, ajustes de apariencia/offset de subtítulos.

## Docker / CI

Imagen default ~166MB descomprimida (~60MB pull): alpine 8 + binario
28 (frontend embebido) + ffmpeg completo ~125. Es la más ligera de su
clase (Jellyfin 1.55GB). Decisión: no recortar ffmpeg (mantener build
propio no compensa ~50MB). Grafana/Prometheus son sidecars OPT-IN en
`deploy/observability/` (pineados por digest) — no van en la imagen.
CI Docker ~20min → cross-compile con `--platform=$BUILDPLATFORM` +
GOOS/GOARCH (solo el runtime se emula). B7 cerrado: push solo en main
+ concurrency cancel (release serializa sin cancelar).

## Incidencias de la sesión

- Los **squash-merges en cascada** de la rama viva (#505→#515)
  generaron conflictos repetidos y en #515 la resolución manual dejó
  `VideoPlayer.tsx` con el bloque ±10s DUPLICADO en main → CI/Docker/
  Release rojos. Reparado mergeando la rama (superset) de vuelta.
- El guard `TestOpenAPISpec_RouterCoverage` cazó la ruta
  `DELETE /channels/{channelId}/hls/viewer` sin documentar → añadida a
  openapi.yaml.
- Hotfixes CI: GOTOOLCHAIN=local de setup-go vs sqlc que exige go1.26;
  evermeet 403 a runners → verificación soft solo en macOS.
