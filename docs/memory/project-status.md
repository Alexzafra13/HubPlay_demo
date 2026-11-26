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
   - `go build ./... && go vet ./... && go test ./...` (Go **1.25.x**; con
     1.27 `anacrolix/torrent` no compila, ver `conventions.md`).
   - `cd web && CI=true pnpm install --frozen-lockfile && pnpm exec tsc -b
     && pnpm test --run && pnpm exec knip && pnpm exec eslint .`
   - `golangci-lint run ./...` (CI usa v2.5.0).
5. Elegir la siguiente tarea de **"▶️ Próxima sesión"** (abajo, en orden).
6. Al cerrar: actualizar este fichero (estado + próxima sesión) y el audit
   que corresponda; mover a `archive/` lo que deje de ser vivo.

---

## 🔭 Estado actual (2026-11-26, retomando tras ~2 meses parado)

**Salud:** MVP funcional, cerca de early-production. `main` en
`8e6559b` (#551) con **CI, Docker y Release verdes**. Sin issues abiertos.

| Área | Estado |
|---|---|
| Tests backend | `go test ./...` verde (Linux, Go 1.25.13) |
| Tests frontend | **783/783** vitest (108 ficheros); `tsc -b`, `knip` limpios; eslint 0 errores / 1 warning conocido (React Compiler + `useVirtualizer`, no accionable) |
| Rama de trabajo | `claude/retomar-proyecto-actualizar-znt18w` (sesión 2026-11-26, sin mergear) |
| Audit playback 2026-06-10 | P0/P1/P2 ✅. **P3 abierta** (ver tabla de trabajo abierto) |
| Audit torrent 2026-09-10 | **Todos los hallazgos ✅** (T-8/T-9 el 2026-11-26). Queda: tests de handler `Play`/`HLSPlaylist`/`HLSSegment` y refcount explícito VOD (T-6) |
| Audit prod 2026-06-08 | Fases 0/1/2 ✅. Fase 3: M18/M19/M20 ✅, M24 parcial, M21/M23 abiertos. Fases 4–5 abiertas |
| Audit federación 2026-06-12 | F-1/3/4/10 ✅, F-2 parcial; F-5..F-9, F-11..F-13 abiertos |
| Producción (servidor de la TV, `192.168.1.100:8097`) | Sigue con una imagen anterior a la caché de subtítulos, el id de instalación y el fix de búsqueda. **Pendiente desplegar.** |

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

**Hallazgos de esta sesión sin arreglar:**
- `gofmt -l internal/` lista ~145 ficheros: el gofmt de Go ≥1.19 reformatea
  listas dentro de comentarios de doc (y algún orden de import). El CI no
  lo exige. Si se quiere limpiar: un commit aislado `gofmt -w` sin más
  cambios, nunca mezclado con trabajo real.
- 13 PRs de Dependabot abiertos. **Ya cubiertos por #551** (cerrables):
  #547 gorilla/websocket 1.5.3, #543 pion/dtls 3.1.4, #542 pion/stun 3.1.5.
  **Majors que requieren trabajo**: #539 react-router 7→8, #511 golang
  1.26 (choca con la restricción de Go 1.25 por `anacrolix/torrent`),
  #508 pnpm/action-setup 6, #507/#506 acciones de docker. Resto (#550
  vitest, #549 sharp, #540 go-deps, #532 x/net, #509 alpine 3.24): probar
  y mergear uno a uno.

---

## ▶️ Próxima sesión (en orden)

1. **Mergear esta rama** (PR → main) y desplegar en el servidor de la TV;
   probar el QR con el móvil en real.
2. **Remux con keyframes reales** + reinicio inmediato en saltos lejanos
   (`roadmap-producto.md` pilar 2) hasta `scripts/seek-check.py` en verde;
   luego probar saltos en la Mi TV.
3. **App TV: "Compartido contigo"** (federación; el servidor ya lo da) +
   F-5/F-6 en servidor.
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
| **Alta** | **Remux con keyframes reales** | Medido: 6/11 segmentos +2 s (`scripts/seek-check.py`). Pilar 2 de `roadmap-producto.md` |
| **Alta** | **App TV sin federación** | La app no consume `/me/peers/*`. Pilar 3 |
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
  binario real (criterio de "terminado" del pilar de reproducción).
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
- **(Nuevo) Apuntar el trabajo al cerrar sesión**: dos veces se ha
  reconstruido trabajo no documentado desde el git log.
