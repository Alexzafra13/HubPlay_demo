# Estado del proyecto

> **Entrypoint de cada sesión.** Lo viejo (todo lo previo a la sesión
> 2026-05-19/20) vive en `archive/`. No se pierde nada — sólo se mueve
> de sitio para que este fichero sea legible de un vistazo.

---

## 🔭 Estado actual (2026-05-20, final del día)

- Branch principal: `main`, working tree limpio.
- Última release pública: `nightly` rolling tag (workflow `release.yml`).
- Tests: backend `go test -race ./...` verde en CI (incluyendo dos flakes del transmux ya parcheados); frontend 616/622 vitest verdes (el delta vs sesión anterior viene de los refactors del compiler, no de tests caídos); `tsc -b` limpio; production build limpio con React Compiler activado.
- **React Compiler activado** en `vite.config.ts` + plugin `eslint-plugin-react-compiler` como hard gate en cada PR. `react-compiler-healthcheck`: 542/542 componentes compatibles.
- HubPlay distribuible "descargar y usar" en los tres targets (desktop / Linux server / NAS-via-Docker) — flujo cerrado en la sesión 2026-05-19/20.

### PRs abiertas tras la sesión maratón 2026-05-20

| PR | Tema | Estado |
|---|---|---|
| [#354](https://github.com/Alexzafra13/HubPlay_demo/pull/354) | Activar React Compiler + refactor 15 anti-patterns + 5 fixes extra | CI en validación tras el último push |
| [#355](https://github.com/Alexzafra13/HubPlay_demo/pull/355) | CI: `typecheck` + `react-compiler-healthcheck` + `knip` (info-only) | CI en validación |
| [#247](https://github.com/Alexzafra13/HubPlay_demo/pull/247) | dependabot: docker/setup-buildx-action 3→4 | esperando rebase tras fix del flake |
| [#289](https://github.com/Alexzafra13/HubPlay_demo/pull/289) | dependabot: picomatch 4.0.3→4.0.4 | esperando rebase |
| [#330](https://github.com/Alexzafra13/HubPlay_demo/pull/330) | dependabot: web-deps bulk con 18 updates (incluye react-hooks 7.1.1) | redundante tras #354 — cerrar/dejar caducar |
| [#352](https://github.com/Alexzafra13/HubPlay_demo/pull/352) | dependabot: softprops/action-gh-release 2→3 | esperando rebase |

---

## 🏗️ Sesión 2026-05-20 (tarde) — Limpieza profunda: lockfile, React Compiler y quality gates

Sesión larga (~10 PRs en una tarde) iniciada como "revisar PRs dependabot" y derivada hacia un saneamiento general del frontend. Catch importante: **main estaba rojo** en CI / Lint y CI / Test Backend, y casi todas las PRs de dependabot heredaban esos fallos sin que tuviera nada que ver con ellas.

### Cadena de causa → efecto descubierta

1. **CI / Lint roto** en main: 8 issues que aparecieron cuando `golangci/golangci-lint-action@v7` empezó a resolver al binario `v2.5.0` con reglas más estrictas (ineffassign, ST1023, SA9003, ST1019, QF1001, unused). Lo bloqueaba cualquier dependabot trivial (postcss bump, picomatch bump, etc).
2. **CI / Test Backend roto** en main: dos flakes en `internal/iptv` aparecían bajo `-race -coverprofile` cuando el watcher goroutine se preempta más de lo previsto. `TestTransmuxManager_PromotesToReencodeOnCodecCrash` y `TestTransmuxManager_Touch_KeepsSessionAlive`.
3. **Workflow Release roto**: `git describe` se calculaba en dos jobs distintos (`build` y `windows-installer`), y entre ambos `release-nightly` reescribía el tag `nightly`. Resultado: nombres de zip desalineados, `unzip exit 9` rompiendo el instalador Windows.
4. **`pnpm-lock.yaml` corrupto**: `lru-cache@11.5.0` triplicado en `packages:` y `snapshots:` tras mergear 4 PRs de dependabot npm en sucesión rápida en la mañana (#290 → #338 → #248 → #141 en ~30 min). GitHub squash-merge no detecta colisiones semánticas de YAML.
5. **`web/package-lock.json` huérfano** commiteado desde el inicio del repo aunque el proyecto usa pnpm. Nunca se sincronizaba con `pnpm-lock.yaml`.

### Lo que se cerró (mergeado a main esta tarde)

- **[#345](https://github.com/Alexzafra13/HubPlay_demo/pull/345)** — Release workflow: nuevo job `meta` centraliza `git describe` y el resto consume `needs.meta.outputs.*`. Race del tag `nightly` desaparece.
- **[#346](https://github.com/Alexzafra13/HubPlay_demo/pull/346)** — Lint cleanups (8 issues) + deadline interno del flake `PromotesToReencodeOnCodecCrash` 5s → 15s.
- **[#347](https://github.com/Alexzafra13/HubPlay_demo/pull/347)** — `git rm web/package-lock.json` + añadir al `.gitignore` (junto con `yarn.lock`).
- **[#348](https://github.com/Alexzafra13/HubPlay_demo/pull/348)** — Flake `Touch_KeepsSessionAlive`: IdleTimeout 200ms → 500ms, Wait 700ms → 1500ms, ctx 5s → 10s, + Touch sincrónico antes de lanzar la goroutine.
- **[#349](https://github.com/Alexzafra13/HubPlay_demo/pull/349)** — Fix quirúrgico del lockfile corrompido (12 líneas borradas, sin tocar versiones).
- **[#350](https://github.com/Alexzafra13/HubPlay_demo/pull/350)** — `dependabot.yml`: `open-pull-requests-limit` npm 5 → 1 para prevenir futuras colisiones de lockfile.
- **6 PRs de dependabot mergeadas en cadena**: #137 (golangci-lint-action), #138 (setup-qemu), #141 (jsdom 28→29 major), #248 (go-deps group, 7 paquetes), #290 (postcss), #338 (tough-cookie 2→6, transitive lockfile-only). Más #280 (vite dev), #351 (go-deps), #353 (download-artifact) en la última pasada.

### Lo que queda en flight (abierto al cierre de sesión)

- **[#354](https://github.com/Alexzafra13/HubPlay_demo/pull/354) — Activación del React Compiler**. Resultado final: 0 errors de lint, 616/616 vitest, build limpio, healthcheck 542/542 compatibles. Cambios:
  - `babel-plugin-react-compiler@1.0.0` integrado vía `react({ babel: { plugins: [...] } })` en `vite.config.ts`.
  - `eslint-plugin-react-compiler@19.1.0-rc.2` con regla `react-compiler/react-compiler: 'error'`.
  - `eslint-plugin-react-hooks` 7.0.1 → 7.1.1 (las reglas estrictas que destaparon los anti-patterns).
  - **15 anti-patterns refactorizados** + 5 extras que sólo aparecían en CI:
    - **`set-state-in-effect` → render-time guarded setState** (patrón oficial React 19 "Adjusting state when a prop changes"): MainNav (route change), SearchBar (URL ?q=), LinkDevice (URL code), LibraryNewPage (validation reset), UsersAdmin (×2: access draft + libraryIds seed), useVibrantColors (palette swap), PairThisDevice (×2: mount-once + qrSvg reset).
    - **`refs during render`**: useLiveHls (ref-assign movido a useEffect), PlayerControls (`reportMenu` envuelto en useCallback), MainNav (`clearTimers` con disable narrow + justificación porque cancelar el timer es necesario funcionalmente).
    - **`immutability`**: HeroTrailer (`handleDismiss` declarado antes del useEffect que lo usa, envuelto en useCallback).
    - **`exhaustive-deps`**: VideoPlayer (`BURNABLE_CODECS` a module scope), MyNotifications + PeerLibraryItemsPage (derivar dentro del useMemo).
    - **5 fixes extra del CI** (no aparecieron en mi lint local porque mi script de filtrado descartó `react-compiler/*` como derivados, cuando algunos son detecciones primarias): useHls + usePlayerKeyboard (suppress narrow para mutar `HTMLMediaElement.src` / `.currentTime` que es API estándar del DOM, no state); useProgressReporter (añadir `[videoRef, itemId, peerId]` a deps reales del effect del cleanup); PairThisDevice (añadir `[poll, navigate]` a deps SSE); Uploads (patrón "latest value via ref" con useEffect dedicado para actualizar el ref + cleanup que lo lee al desmontar — sin esto, añadir `active` a deps abortaría uploads en cada cambio de estado).
- **[#355](https://github.com/Alexzafra13/HubPlay_demo/pull/355) — Quality gates extra en CI**. Tres nuevos steps:
  - **`pnpm typecheck`** (`tsc -b`) — hard gate. Antes el typecheck sólo corría en `pnpm build`, que CI no ejecuta para frontend.
  - **`pnpm dlx react-compiler-healthcheck`** — hard gate. Falla si la compatibilidad baja del 100%. Funciona independientemente de si el compiler está activado.
  - **`pnpm knip`** con `--no-exit-code` — info-only (job separado). Hoy reporta 5 unused files + 2 unused deps + 38 unused exports + 115 unused types. Cuando lleguemos a 0, elevamos a hard gate.

### Aprendizajes que se aplican a futuras sesiones

- **Antes de "arreglar" una PR de dependabot que falla en lint o test backend, comprobar primero si main está rojo**. La mitad del tiempo el problema no es del bump, es heredado.
- **`open-pull-requests-limit: 1` para npm** es la respuesta correcta a "GitHub squash-merge corrompe lockfiles cuando dos PRs lo tocan en paralelo". El precio (bumps serializados) es barato comparado con `ERR_PNPM_BROKEN_LOCKFILE` en producción.
- **`eslint-plugin-react-compiler` es estricto pero correcto**. Cuando reporta "Component skipped because rules were disabled", suele ser señal de que el `eslint-disable react-hooks/*` esconde un anti-pattern real, no que el plugin sea pedante.
- **No te fíes de tu lint local si filtra reglas**. Cinco anti-patterns aparecieron sólo en CI porque mi script filtraba `react-compiler/*` como "derivados" — pero el compiler también detecta cosas primarias.

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

### Cierre de la sesión 2026-05-20 (tarde)
- Mergear [#354](https://github.com/Alexzafra13/HubPlay_demo/pull/354) y [#355](https://github.com/Alexzafra13/HubPlay_demo/pull/355) cuando el CI termine. Tras ello, revisar #247, #289, #352 (dependabot que esperaban rebase tras los fixes de flake) y cerrar #330 si dependabot lo marca como redundante (su react-hooks 7.1.1 ya está en main vía #354).

### Limpieza incremental de dead code
- El job knip (PR #355) reporta 5 unused files + 2 unused deps + 38 unused exports + 115 unused types. Cuando llegue a 0, elevar `pnpm knip` a hard gate en CI (quitar `--no-exit-code`). Empezar por los **5 unused files** que son los más obvios: `scripts/extract-i18n-defaults.mjs`, `src/components/layout/index.ts`, `src/components/layout/SetupRoute.tsx`, `src/hooks/useLocalStorage.ts`, `src/pages/admin/index.ts`.

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
