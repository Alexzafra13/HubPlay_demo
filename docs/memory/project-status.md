# Estado del proyecto

> **Entrypoint de cada sesión.** Solo el estado vivo y lo que falta. El
> detalle de sesiones cerradas vive en `archive/` (no se borra nada, solo
> se reubica). Última limpieza: 2026-06-10.

---

## 🔭 Estado actual (2026-06-12, fin de sesión)

**Salud:** MVP funcional, cerca de early-production.

| Área | Estado |
|---|---|
| Tests backend | `go test ./...` verde (`-race` en stream/api/iptv) |
| Tests frontend | **748/748** vitest; `tsc`, `eslint` y `knip` limpios |
| Rama de trabajo | `claude/revisa-trabajar-9wevyd` — Playback P2 completo + smoke E2E |
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
- **Pendiente P3**: smoke (c) dub-switch (fixture ya trae eng+spa) y
  (d) LiveTV zap (necesita upstream IPTV sintético); PB-19/26/29-31/
  33/36-39 + resto de gaps de test del audit.

---

## 📋 Trabajo abierto

**Roadmap principal:** `audit-2026-06-10-playback-chain.md` (cada item
con ✅/pendiente y fix propuesto).

| Prioridad | Tema | Items |
|---|---|---|
| Media | **Playback P3** | Smoke E2E Playwright (play→seek→resume, UpNext, dub-switch, LiveTV zap, server caído), PB-19/26/29-31/33/36-39 + gaps de test del audit |
| Media | **Fase 3 — observabilidad/config** (audit prod) | M18–M21, M23, M24 (IP de cliente en logs, panics en métricas, validación de config, completar `example.yaml`) |
| Media | **Fase 4 — frontend** | B10 (ESLint type-aware), B14 (tests de páginas grandes) |
| Baja | **Fase 5 — gobernanza** | README (no hay), `SECURITY.md`, `CODEOWNERS` |
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
