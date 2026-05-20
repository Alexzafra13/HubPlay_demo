# Estado del proyecto

> **Entrypoint de cada sesión.** Lo viejo (todo lo previo a la sesión
> 2026-05-19/20) vive en `archive/`. No se pierde nada — sólo se mueve
> de sitio para que este fichero sea legible de un vistazo.

---

## 🔭 Estado actual (2026-05-20)

- Branch principal: `main`, working tree limpio.
- Última release pública: `nightly` rolling tag (workflow `release.yml`).
- Tests: backend `go test ./...` verde tras los fixes cross-platform; frontend 622/622 vitest; `tsc -b` limpio; production build limpio.
- HubPlay distribuible "descargar y usar" en los tres targets (desktop / Linux server / NAS-via-Docker) — flujo cerrado en la sesión 2026-05-19/20.

### PRs abiertas (a falta de review humano)

| PR | Tema | Estado tests |
|---|---|---|
| [#339](https://github.com/Alexzafra13/HubPlay_demo/pull/339) | Opt-out runtime del update checker desde el panel admin | verde |
| [#341](https://github.com/Alexzafra13/HubPlay_demo/pull/341) | LICENSE GPL-3.0-or-later en el root + wizard de Inno + `web/package.json` | verde |
| [#342](https://github.com/Alexzafra13/HubPlay_demo/pull/342) | 5 fallos pre-existentes en `go test ./...` por incompatibilidades cross-platform | verde |
| [#343](https://github.com/Alexzafra13/HubPlay_demo/pull/343) | `Service.BaseURL` inyectable en `updates` + tests E2E HTTP (cierra 2 `t.Skip`) | verde |

PR mergeada hoy: [#340](https://github.com/Alexzafra13/HubPlay_demo/pull/340) — URL mDNS en `/admin/system` con botón copiar.

---

## 🧹 Sesión 2026-05-20 — Pendientes pequeños + limpieza

Después de cerrar la fase de distribución (sesión 2026-05-19/20), barrido de los pendientes "pequeños y autocontenidos" + housekeeping del repo. Cinco PRs creados, una mergeada en caliente.

### Lo que cambió

- **Opt-out del update checker en runtime** ([#339](https://github.com/Alexzafra13/HubPlay_demo/pull/339)). `Service.SetUserEnabled(bool)` togglable; persistido en `app_settings` con key `updates.check_enabled`. Bootstrap en `main.go` lee el setting antes de arrancar el ticker. Nuevo campo `user_disabled` en el wire de `/admin/system/updates`. Endpoints `GET`/`PUT /admin/system/updates/config`. Banner ahora pinta 4 estados (capability off / user-disabled con "Activar" / has_update / al día con "Comprobar" + "Deshabilitar"). 4 unit + 6 handler + 6 vitest.
- **URL mDNS en `/admin/system`** (#340, **mergeada**). Nuevo campo `mdns_url` (omitempty) en `serverStats`. El router lo computa cuando `cfg.MDNS.Enabled`. Fila condicional en la card "Conexión" del System status con valor mono + `CopyToClipboardButton`. Componente nuevo en `components/common/` (reutilizable, tolerante a HTTP plano sin clipboard API).
- **LICENSE GPL-3.0-or-later** ([#341](https://github.com/Alexzafra13/HubPlay_demo/pull/341)). Texto canónico de gnu.org en root. `LicenseFile=` añadido a `installer.iss` bajo `#if FileExists`. Campo `"license"` en `web/package.json`. Cumple sección 4 de la GPL-3. FFmpeg LGPL mantiene su `LICENSE-ffmpeg.txt` aparte.
- **Fixes cross-platform de `go test ./...`** ([#342](https://github.com/Alexzafra13/HubPlay_demo/pull/342)). Cinco fallos pre-existentes en main que NO eran bugs de producción — eran asumptions de test:
  - `upload/sanitize.go`: `filepath.Base("a:::b.mkv")` en Windows trata `a:` como drive prefix → cambiado a `path.Base` (POSIX).
  - `stream/manager_test.go`: `newTestManager` pasaba `ffmpegPath=""` que cae a PATH lookup; en hosts con ffmpeg `cmd.Start()` arrancaba el proceso y los tests del coalesce veían `nil` error → ffmpeg sentinel inexistente.
  - `config/preflight_test.go`: `stubPathWith` no añadía `.exe` para Windows (`exec.LookPath` requiere extensión en `%PATHEXT%`).
  - `db/sqlc/*models.go`: drift por migraciones `audit_log` + `cors_origins` que añadieron tablas sin regenerar — `make sqlc` regen.
- **`Service.BaseURL` inyectable en updates** ([#343](https://github.com/Alexzafra13/HubPlay_demo/pull/343)). Setter `SetBaseURL(url)` thread-safe. Reemplaza los 2 `t.Skip` con tests E2E reales contra `httptest`: `DetectsNewerVersion`, `SamePinsHasUpdateFalse`, `PrereleaseSkippedSilently`, `RecordsLastErrorOn500`, `HTTPIntegration_ETagRoundTrip` (verifica round-trip de `If-None-Match`, cabeceras `Accept` + `User-Agent`).
- **Limpieza de memoria** (esta sesión). 8 ficheros .md sueltos de sesiones cerradas movidos a `archive/`. `project-status.md` reescrito (de 441 KB a manejable) con las sesiones viejas en `archive/2026-04-29-to-05-04.md` y `archive/2026-05-05-to-05-19.md`.

### Métricas al cierre

- Backend: `go test ./...` verde en mi máquina (Windows + ffmpeg en PATH + sqlc 1.31.1).
- Frontend: 622/622 vitest (+18 sobre la sesión anterior: 4 service + 6 handler updates + 6 banner + 3 CopyToClipboardButton, menos 1 skip placeholder).
- TypeScript: `tsc -b` limpio.
- Producción: build limpio.

---

## 📦 Sesión 2026-05-19/20 (continuación) — Distribución y empaquetado

Cerramos el flujo "descargar y usar" para tres públicos: PC desktop, servidor Linux y NAS. 8 PRs mergeadas. Nada toca la lógica de negocio — todo es plumbing alrededor del binario.

**Audit panel — usernames en vez de UUIDs.** LEFT JOIN a users en el SELECT del audit log. Pinta `alice` y `bob → carol` en lugar de `bd91…/4def…`; UUID truncado en gris debajo, tooltip con el ID completo para copiar.

**Versión inyectable.** `version`, `commit`, `buildDate` como `var` en main package, inyectadas por `-ldflags` en Makefile y CI vía `git describe`. Se exponen en `/admin/system/stats` y en `hubplay --version`. Builds locales muestran `v0.1.0-N-gSHA-dirty`, build de tag muestra el tag limpio.

**PATH prepend del exe-dir al arranque.** Una línea en `main.go` que mete `filepath.Dir(os.Executable())` al inicio del `$PATH`. Con eso, cualquier `exec.LookPath("ffmpeg")` o `exec.Command("ffmpeg", …)` (probe, stream, imaging, iptv, library) encuentra el `ffmpeg` bundleado en la misma carpeta que `hubplay.exe` sin tocar ningún call-site.

**Release workflow cross-platform** (`.github/workflows/release.yml`):
- Matrix 5 targets: linux/darwin × amd64/arm64, windows × amd64. `CGO_ENABLED=0` (modernc.org/sqlite es pure-Go).
- `scripts/fetch-ffmpeg.sh` descarga ffmpeg LGPL por plataforma — BtbN/FFmpeg-Builds (Linux/Windows), evermeet.cx (macOS).
- Empaqueta `.tar.gz`/`.zip` + `.sha256` con `hubplay` + `ffmpeg` + `ffprobe` + `hubplay.example.yaml` + `LICENSE-ffmpeg.txt`.
- Job `release-nightly`: en cada push a `main` borra el tag `nightly` y lo recrea con los binarios del último commit. Pre-release público, visible en la pestaña Releases sin esperar a taguear.
- Job `release-tag`: en push de `v*` publica release ESTABLE (no draft, no prerelease).
- `fetch-depth: 0` en todos los checkouts que llaman a `git describe` — sin esto el job downstream calculaba una versión distinta y el unzip fallaba.

**Installer Windows** (`scripts/installer.iss` + `Minionguyjpro/Inno-Setup-Action@v1.2.8`):
- Genera `HubPlay-Setup-vXXX-windows-amd64.exe`.
- Bundlea NSSM (BSD) para registrar `HubPlay` como servicio Windows; arranca con el PC, sin consola.
- Icono multi-resolución (16/32/48/64/128/256), banner del wizard con marca integrada — todo generado desde `web/public/hubplay_icon_mark.svg` con `web/scripts/gen-installer-assets.mjs` (sharp + to-ico).
- Shortcut Escritorio + Menú Inicio apunta a `launch-hubplay.vbs` que intenta `msedge --app`, luego `chrome --app`, fallback al navegador por defecto → abre como ventana standalone sin chrome del browser.
- Textos del wizard en español sobrescritos en `[Messages]`, sin tono de marketing.
- Caveats: pin `@v1.2.8`, `fetch-depth: 0`, NSSM con retry+fallback a archive.org (nssm.cc da 503), env vars en lugar de `/D`. El `LICENSE` ya no es opcional (PR #341).

**Install script Linux** (`scripts/install.sh` + `scripts/hubplay.service`):
- One-liner estilo Tailscale/k3s: `curl -fsSL https://github.com/Alexzafra13/HubPlay_demo/releases/latest/download/install.sh | sudo bash`.
- Detecta arch (amd64/arm64), exige systemd, refusa con instrucciones a Docker en Synology/Unraid/QNAP/TrueNAS/Alpine.
- Resuelve "latest" vía GitHub API, descarga `.tar.gz`, verifica `sha256`, crea usuario sistema `hubplay`, instala binarios en `/usr/local/bin/`, config en `/etc/hubplay/`, datos en `/var/lib/hubplay/`, registra systemd unit endurecido, `enable --now`.
- Idempotente: correrlo dos veces hace upgrade in-place. Preserva el `hubplay.yaml` editado por el operador.

**PWA** (`vite-plugin-pwa` + `@vite-pwa/assets-generator`):
- Manifest + service worker injectados automáticamente. Workbox precachea JS/CSS/assets, no HTML (evita stale tras swap de server).
- Iconos generados desde `public/hubplay_icon_mark.svg` con `pnpm gen:pwa-assets`.
- Browser detecta "instalable" en `localhost` o HTTPS → botón "Instalar" → icono propio en escritorio/home + ventana standalone.
- **Caveat conocido**: en LAN sobre HTTP plano (192.168.x.y) los browsers NO ofrecen instalar la PWA — requieren secure context.

**Update notifier** (`internal/updates/`):
- Goroutine background con ticker de 24h y jitter inicial 0-30min.
- GET a `api.github.com/repos/.../releases/latest` con `If-None-Match` (ETag) → 304 cuando no hay versión nueva, ~200B por check.
- Auto-deshabilitado en `version=="dev"` o `repo==""`.
- Endpoints: `GET /admin/system/updates`, `POST /admin/system/updates/check` (rate-limit 1/min), `GET`/`PUT /admin/system/updates/config` (opt-out runtime — sesión 2026-05-20).
- Banner en `/admin/system` (`UpdateBanner.tsx`): 4 estados (ver sesión 2026-05-20 arriba).

**mDNS auto-anuncio** (`internal/mdns/` con `grandcat/zeroconf`):
- El server registra `_http._tcp` con hostname forzado `<cfg.MDNS.Hostname>.local` (default `hubplay.local`).
- Cualquier dispositivo de la LAN resuelve `http://hubplay.local:8096` sin tocar router ni DNS.
- Config `mdns.enabled` (default true) y `mdns.hostname` en `hubplay.yaml`.
- La URL se expone en `/admin/system/stats` (sesión 2026-05-20, PR #340) con botón copiar.

**PRs mergeadas en esta sesión:** #327 → #328 → #329 → #331 → #332 → #333 → #334 → #335-#337.

---

## 🎯 Cola priorizada para la próxima sesión

### Cortos / autocontenidos
Cerrados en la sesión 2026-05-20. Quedan los grandes.

### Medianos (vale la pena cuando arranque la siguiente sesión)

1. **Tests frontend de páginas grandes**. Coverage Vitest cubre admin panels + componentes comunes; faltan **Home**, **LiveTV**, **Search**, **Movies**, **Series**. Son las páginas con más LOC del repo y las más visibles para el usuario.
2. **Refactor estructural pendiente** del audit 2026-05-14 ([audit-2026-05-14-go-backend-review.md](audit-2026-05-14-go-backend-review.md) + [intervention-2026-05-14.md](intervention-2026-05-14.md)). Iteraciones 4-7 abiertas: split de god-handlers/services (P, Z, QQ), refactor estructural de `iptv/` (CC), composition root (G, H, V, Q, LL, JJ), schema + cosmética (D, X, W, BB, UUU-mig).
3. **Per-user channel order + hide en Live TV**. Spec en [per-user-channel-order-pending.md](per-user-channel-order-pending.md). Migración + servicio + API + UI. ~1 sesión.

### Grandes (requieren ventana dedicada)

4. **Firma del installer Windows con SignPath Foundation**. **Es gratis para OSS** (verificado en signpath.org el 2026-05-20). Apply via `apply.signpath.io` — la verificación tarda días/semanas pero la integración con el workflow es directa (action `signpath/github-action-submit-signing-request`). Mientras llega el approval, SmartScreen sigue avisando — no urgente.
5. **Auto-update one-click + cert TLS en LAN**. Estilo `*.plex.direct`: el server obtiene un cert real para `<hash>.hubplay.direct` o similar, lo sirve en LAN sin warnings, y el client comprueba el feed de updates y aplica binarios firmados in-place. Feature grande, sin presión de calendario.

---

## 📚 Documentos vivos en `docs/memory/`

- **[architecture-decisions.md](architecture-decisions.md)** — ADRs cerrados (AppError, observability, keystore, sink pattern, preflight, sqlc adapter, etc.). Sólo se añaden ADRs nuevos; nunca se edita un ADR cerrado.
- **[conventions.md](conventions.md)** — patrones del codebase (anti-ciclos, sqlc adapter, helpers de test, gotchas, reglas de dependencia entre paquetes).
- **[audit-2026-05-14-go-backend-review.md](audit-2026-05-14-go-backend-review.md)** — review vivo por fases. Iteraciones 4-7 pendientes (ver intervention).
- **[intervention-2026-05-14.md](intervention-2026-05-14.md)** — tracker de iteración del review 2026-05-14. Marca olores cerrados por commit.
- **[perf-benchmarks-2026-05-17.md](perf-benchmarks-2026-05-17.md)** — baseline benchmarks dual-backend (SQLite + Postgres) para repos del hot-path.
- **[per-user-channel-order-pending.md](per-user-channel-order-pending.md)** — spec de feature pendiente Live TV.

## 🗄️ Archivo (`docs/memory/archive/`)

Sesiones cerradas, conservadas para arqueología:

- `2026-pre-04-28.md` — orígenes del proyecto.
- `2026-04-27-to-04-29-pre-detail-ux.md` — sesiones pre-detail UX.
- `2026-04-29-to-05-04.md` — federación P2P, OpenAPI, hardening federation.
- `2026-05-05-to-05-19.md` — senior reviews intermedios, fixes player/seek, mDNS bringup, uploads + permisos granulares.

Audits cerrados + sesiones específicas:
- `audit-2026-04-15.md`, `audit-2026-04-28.md`, `audit-2026-05-05.md`, `audit-plan.md`
- `manual-qa-movies-series-2026-04-27.md`
- `player-seek-bugs-2026-05-07.md` (los bugs ya están arreglados en main)
- `session_2026-05-10_audit_p0_fixes.md`
- `topbar-redesign-2026-05-06.md`
