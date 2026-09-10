# Audit 2026-09-10 · módulo torrent (streaming, VOD remux, descargas, fuentes)

> Revisión por lectura del código añadido el 15–16 de junio (commits
> `eafd22e7..f30a4709`: descargar-a-biblioteca, Prowlarr/Torznab, búsqueda
> por TMDb, remux VOD a HLS, PRs #526–528). Ese trabajo se hizo sin audit
> y sin actualizar `project-status.md`. Estado de cada hallazgo abajo.
> Verificado contra código el 2026-09-10; los ✅ tienen test de regresión.

## Veredicto

Base sólida: `{segment}` validado por regex estricta, `{infohash}` solo se
usa como clave de mapa (nunca en rutas), ningún string de usuario llega al
argv de ffmpeg, todo `/torrent/*` va detrás del middleware de auth y las
operaciones sensibles son admin-only, mapas siempre bajo lock, status HTTP
razonables. Los fallos estaban en el guard SSRF relajado, en el ciclo de
vida del transcoder y en la frontera de escritura a la biblioteca.

## 🟠 Altos

### ✅ T-1 · `allow_private_upstreams` desactivaba el guard SSRF por completo
`internal/torrentstream/manager.go` (`fetchTorrentFile`). Con el flag activo
(y `docker-compose.torrent.yml` lo pone a `true` por defecto) la descarga
del `.torrent` usaba un `http.Client` sin ninguna validación de host ni de
redirects: `169.254.169.254`, loopback, cualquier host LAN y pivotes 302
eran alcanzables desde `src` (admin-only, pero controlado por el usuario).
**Fix:** `imaging.SafeGetWith(..., SafeGetOpts{AllowPrivate})` — el flag
relaja loopback/RFC1918/ULA pero link-local (metadata cloud), unspecified y
multicast siguen bloqueados y cada redirect se re-valida. Tests:
`safety_private_test.go`.

### ✅ T-2 · Dos `Prepare` concurrentes del mismo infohash lanzaban dos ffmpeg
`vodtransmux.go`. Ambos fallaban el lookup de `sessions[ih]`, probeaban, y
arrancaban dos transcodes en el mismo `workDir`; el segundo pisaba la
sesión del primero, cuyo `stop` quedaba inalcanzable (ffmpeg huérfano).
**Fix:** placeholder `vodSession` registrado bajo el lock ANTES del probe
con canal `ready`; el resto de callers espera (`awaitSession`, respeta su
ctx) y recibe el mismo resultado. Test:
`TestVODPrepare_ConcurrentSameInfohash_SingleTranscode` (8 callers → 1
probe, 1 start).

## 🟡 Medios

- ✅ **T-3 · Goroutine filtrada por transcode** (`newVODStderr`): el
  `io.Pipe` nunca se cerraba tras `cmd.Wait()`. **Fix:** el writer se
  cierra en la goroutine de wait.
- ✅ **T-4 · `Stop` borraba el workDir antes de que ffmpeg saliera** (Kill
  es asíncrono → "file in use" en Windows / `.tmp` tardío en Linux) y un
  ffmpeg muerto seguía anunciándose como sesión HLS. **Fix:** `stop` espera
  (≤5 s) a la salida; `watchExit` tira la sesión si ffmpeg muere con error
  (un EOF limpio la mantiene: los segmentos siguen servibles). Tests:
  `TestVODWatchExit_*`.
- ✅ **T-5 · Eventos SSE de descargas a todos los usuarios**
  (`handlers/me/events.go`): `GET /torrent/downloads` es admin-only porque
  la lista revela qué se baja, pero el SSE emitía `TorrentDownload` a
  cualquier usuario autenticado. **Fix:** solo admins se suscriben. Test:
  `events_role_test.go`.
- ✅ **T-6 (parcial) · Torrent y sesión VOD se reapeaban por separado**: el
  reaper del `Manager` solo ve lecturas; un viewer reproduciendo segmentos
  ya escritos no genera ninguna y el torrent podía caer bajo un transcode
  vivo. **Fix:** `PlaylistPath`/`SegmentPath` hacen `Touch()` del reader
  del torrent. Pendiente: refcount explícito (holder) desde el VOD.
- ✅ **T-7 · `copyFiles` confiaba en las rutas del metainfo** al escribir en
  la biblioteca. **Fix:** `containedPath` (Clean + prefijo estricto) en
  origen y destino. Test: `TestCopyFiles_RejectsEscapingPaths`.

## 🟢 Bajos

- **T-8 · De-dup de descargas por `src` exacto**: magnet y URL `.torrent`
  del mismo contenido lanzan dos jobs que copian sobre el mismo destino.
  Fix propuesto: de-dup por `InfoHash()` tras `addTorrent`.
- **T-9 · Descargas sin cancelación ni timeout**: swarm muerto ⇒ goroutine,
  holder y scratch vivos para siempre; `DELETE` responde 409 eternamente.
  Fix propuesto: ctx por job + `DELETE` cancela activos, o timeout sin
  progreso.
- ✅ **T-10 · Alias `bySrc` no registrado** al re-unirse a una sesión ya
  activa por otro `src`. **Fix:** se registra el alias.
- ✅ **T-11 · `Close`/`Shutdown` no idempotentes** (segundo close del canal
  del reaper = panic). **Fix:** `sync.Once`. Test:
  `TestVODShutdown_Idempotent`.

## Gaps de test que quedan

- Handler: `Play`, `HLSPlaylist`, `HLSSegment` sin tests (segmento
  inválido → 400, infohash desconocido → 404, mapeo de modos).
- `fetchTorrentFile` ya no existe; `addTorrentFromURL` con `allowPrivate`
  no tiene test de integración propio (el guard sí, en `imaging`).
- T-8/T-9 cuando se aborden.
