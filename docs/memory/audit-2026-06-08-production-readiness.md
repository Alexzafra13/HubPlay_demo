# Auditoría de Production-Readiness — 2026-06-08

> Auditoría transversal de cara a sacar a producción. Cubre lo que los
> audits previos (2026-05-14 / 2026-05-27) NO cubrían: seguridad
> operativa, ops/infra, supply-chain, observabilidad y frontend prod.
> Método: 5 sweeps paralelos especializados + chequeos manuales de deps
> y gobernanza. **Solo análisis — 0 cambios de código.** Toda evidencia
> con `file:line`.

El proyecto parte de una base **notablemente madura** (SSRF guards con
re-validación de redirects, rotación de refresh tokens con detección de
reuse, keystore JWT con rotación por `kid`, SQL dinámico whitelisteado,
defensas de path-traversal, CSP real, pool SQLite tuneado, shutdown
graceful en 3 fases, health/ready reales, govulncheck que bloquea
merges). Lo de abajo es **lo que falta, está flojo o es arriesgado**.

Leyenda severidad: 🔴 Crítico · 🟠 Alto · 🟡 Medio · 🟢 Bajo.

---

## 🔴 Crítico — bloqueantes antes de exponer a internet

### C1 · Token JWT aceptado por query param `?token=` → fuga en logs
`internal/auth/middleware.go:75-78`. `extractToken` acepta el access
token por la query string (para SSE/WS). Las query strings acaban en
logs de nginx (`$request` por defecto), en el `RequestLogger` propio, en
el historial del navegador y en cabeceras `Referer`. Un token filtrado =
sesión completa. **Fix:** ticket efímero single-use para SSE/WS, o exigir
cookie/Bearer; si se mantiene, TTL muy bajo + nginx que strip-ee la query
de los logs.

### C2 · URLs de IPTV (M3U/EPG/stream) logueadas enteras → fuga de credenciales del proveedor
`internal/iptv/service_m3u.go:158,217,221`; `internal/iptv/proxy.go:285,667,671`.
Las URLs Xtream/M3U casi siempre llevan `?username=…&password=…`. El
redactor de slog (`internal/logging/logging.go:27-32`) solo matchea
*claves* `password`/`token`, no inspecciona *valores* tipo URL. Las
credenciales acaban en stdout y en el ring-buffer del panel admin
(visible a cualquier admin). **Fix:** helper `redactURL` (strip userinfo +
params `username`/`password`/`token`) aplicado en cada log site de IPTV,
o extender `ReplaceAttr` para detectar valores con forma de URL en claves
`url`/`epg_url`/`m3u_url`.

---

## 🟠 Alto

### Seguridad
- **A1 · Sin rate-limit HTTP en endpoints de auth.** `internal/api/mount_public.go:35-49`
  monta `/auth/login|refresh|device/poll|setup` sin `IPRateLimitMiddleware`
  (que existe pero solo se usa en federation pairing). La protección
  anti-fuerza-bruta depende solo del limiter in-service y, en prod, de
  que el operador configure nginx. **Fix:** envolver las rutas de auth con
  `IPRateLimitMiddleware` a nivel app.
- **A2 · Lockout por username = DoS de cuenta.** `internal/auth/login_service.go:62,70-93`.
  `recordFailure(username)` bloquea el *username* tras N fallos; un
  atacante que conozca un usuario lo bloquea a voluntad. Combinado con A1
  es barato de explotar. **Fix:** bloqueo duro por IP; el contador por
  username solo añade delay, o lockout por tupla (username, IP).
- **A3 · Proxy IPTV es un relay HTTP abierto.** `internal/api/handlers/iptv/iptv_channels.go:521,540`
  + `internal/iptv/proxy.go:647-664`. `ProxyURL` fetchea un `url`
  arbitrario del cliente; solo se valida acceso al *canal*, no que la URL
  pertenezca al host upstream del canal. SSRF interno está bloqueado por
  `isSafeUpstream` (sólido), el residual es relay abierto / bypass de
  geo-bloqueo con la IP del server / amplificación de banda. **Fix:**
  validar que el host de `url` coincide con el origin configurado del
  canal.
- **A4 · `Access-Control-Allow-Origin: *` en respuestas HLS proxiadas.**
  `internal/iptv/proxy.go:640,725,746,757`. Endpoints autenticados
  per-usuario con CORS wildcard → cualquier web puede leer los bytes del
  stream vía el navegador de la víctima. **Fix:** quitar el wildcard;
  apoyarse en el middleware CORS global.

### Despliegue / Infra
- **A5 · `/metrics` sin auth y activo por defecto.** (Detectado por 3
  sweeps.) `internal/api/router.go:235-248`; default `metrics_enabled:true`
  (`config.go:341-344`); `bind` default `0.0.0.0`; **ningún** proxy de
  `deploy/` lo bloquea. Fuga de versión, plantillas de rutas, volúmenes de
  request, sesiones activas, goroutines/FDs → reconocimiento. **Fix:**
  bind a localhost o gate tras auth admin, **y** `location /metrics { deny }`
  en nginx/Caddy/Traefik.
- **A6 · nginx bufferiza los SSE → UI realtime rota.** `deploy/nginx/hubplay.conf:142-163`.
  `/api/v1/me/events`, `/events`, `/uploads/events`, `/auth/device/events`
  caen en el `location /` que no pone `proxy_buffering off`. Progreso de
  scan/upload y device-pairing aparecen colgados. Caddy/Traefik sí lo
  hacen bien; nginx es el único roto. **Fix:** `location` dedicado para los
  paths SSE con `proxy_buffering off; proxy_read_timeout 86400s; proxy_http_version 1.1;`.
- **A7 · Sin init PID-1 / reaper de zombies ffmpeg.** `Dockerfile:116,146`
  (`ENTRYPOINT ["hubplay"]`, sin tini/dumb-init; ningún compose con
  `init:true`). Un server de transcoding spawnea muchos ffmpeg/ffprobe;
  los huérfanos reparentados a PID 1 (la app Go) se acumulan como zombies.
  **Fix:** `init: true` en cada servicio hubplay de los 3 compose (lo más
  barato) o `ENTRYPOINT ["tini","--","hubplay"]`.
- **A8 · `stop_grace_period` de Docker (10s) < shutdown de la app (30s).**
  `cmd/hubplay/main.go:556` da 30s; Docker mata a los 10s. Drenajes de
  playback/transcode/upload se cortan y un checkpoint WAL puede
  interrumpirse. **Fix:** `stop_grace_period: 40s` en los 3 compose.

### Supply-chain / Release
- ✅ **A9 · GitHub Actions pineadas a tags flotantes, no a SHA.** Casi todas
  (`checkout@v4`, `setup-go@v6`, `action-gh-release@v3`, `build-push-action@v6`,
  `golangci-lint-action@v9`, `signpath/...@v2`, `Inno-Setup-Action@v1.2.8`,
  y `millionco/react-doctor@main` ← rama móvil). Compromiso upstream =
  ejecución con contexto `contents:write`/`packages:write`/`SIGNPATH_API_TOKEN`.
  La disciplina ya existe (trivy-action SHA-pineada, docker.yml:124) pero
  no se aplicó al resto. **Fix:** pinear a SHA completo con comentario
  `# vX.Y.Z`; Dependabot (ya configurado para actions) los bumpea.
- ✅ **A10 · Releases sin provenance/firma + FFmpeg sin verificar + bypass en `install.sh`.**
  Cluster supply-chain:
  - `release.yml:384-396,486-499` publica solo con sidecars `.sha256` (no
    prueban nada: quien altera el binario altera el checksum). Sin cosign,
    sin SLSA provenance, sin attestation. SignPath wireado pero **off**.
  - `scripts/fetch-ffmpeg.sh:82,118-119` baja FFmpeg de tags `latest`
    mutables y de evermeet con única verificación `ffmpeg -version`
    (liveness, no autenticidad). Cada release embebe binarios de terceros
    sin pinear.
  - `scripts/install.sh:337-349`: si no puede bajar el `.sha256` solo
    *avisa* y continúa — bypass de verificación en un instalador que corre
    como root vía `curl | sudo bash`.
  **Fix:** `actions/attest-build-provenance` (+cosign de los archivos +
  firmar SHA256SUMS); pinear FFmpeg a versión + verificar SHA256 por
  plataforma; hacer fatal el `.sha256` ausente en install.sh; activar
  SignPath.

### Frontend
- **A11 · Un único ErrorBoundary global → un crash deja toda la app en blanco.**
  `web/src/App.tsx:104` envuelve todo `<Routes>` en un boundary. Un crash
  de render en cualquier página tira el shell entero (sidebar/topbar) y el
  usuario pierde la navegación. **Fix:** boundary a nivel de ruta envolviendo
  el `<Outlet/>` de `AppLayout`.
- **A12 · Grids de catálogo sin virtualizar → DOM crece sin límite.**
  `web/src/components/media/MediaGrid.tsx:114-119` + `MediaBrowse.tsx:93`
  (infinite query). Scrollear una biblioteca de miles de items acumula
  miles de `PosterCard` (blurhash canvas + img) sin reciclar.
  `@tanstack/react-virtual` ya es dependencia pero solo se usa en EPGGrid.
  **Fix:** virtualizar MediaGrid para bibliotecas grandes. Es el cliff de
  rendimiento más probable en un server poblado.

### Gobernanza
- **A13 · No hay `README.md`** (ni en root ni en docs). Básico para
  producción / open-source. Tampoco `SECURITY.md` (relevante en un server
  de media con auth) ni `CODEOWNERS` (rutas sensibles: `.github/`, `auth/`,
  scripts de release). **Fix:** README + SECURITY.md (política de
  divulgación) + CODEOWNERS mínimo.

---

## 🟡 Medio

### Seguridad
- **M1 · CSRF se salta si no hay cookie de acceso.** `internal/api/csrf.go:74-79`.
  Correcto para el modelo double-submit, pero toda la capa CSRF descansa
  en la presencia de cookie. Auditar que ningún flujo mutante mezcle
  cookie-auth saltándose el header. Considerar `SameSite=Strict`.
- ✅ **M2 · `password_change_required` solo es advisory.** `internal/api/handlers/auth/auth.go:38,224`.
  El server emite tokens válidos aunque el flag esté activo; el frontend
  es quien enruta. Un usuario/atacante con password temporal puede ignorar
  la rotación vía API. **Fix:** rechazar mutaciones (salvo allowlist
  pequeño tipo `/me/password`) mientras el flag esté activo.
- ✅ **M3 · Ventana pre-setup abierta en `0.0.0.0`.** `internal/api/handlers/system/setup.go:149-169`.
  Antes del primer admin, `/auth/setup` y `/setup/browse` están abiertos
  → race-to-setup (reclamar admin) + browse del filesystem del host.
  Post-setup sí queda bloqueado. **Fix:** documentar bind a localhost hasta
  completar setup, o token de setup impreso en consola (patrón Jellyfin).
- ✅ **M4 · `X-Forwarded-Proto` confiado incondicionalmente.** `internal/api/security_headers.go:96-99`,
  `csrf.go:54-56`, `auth.go:114`. Desacoplado de `trusted_proxies` (que sí
  gobierna XFF). Frontera de confianza inconsistente. **Fix:** honrar XFP
  solo si el peer está en `trusted_proxies`.

### Despliegue / DB
- **M5 · Password Postgres por defecto `hubplay` en plano.** (2 sweeps.)
  `.env.example` + los 3 compose con `${HUBPLAY_POSTGRES_PASSWORD:-hubplay}`.
  El prod compose ships ese default. **Fix:** quitar el default en prod
  (fail-closed) o password generado; documentar que activar el toggle PG
  con el default es inseguro.
- **M6 · Sin estrategia de backup en `deploy/`.** Ni `pg_dump` ni SQLite
  online-backup ni sidecar. La app ya tiene plumbing de restore
  (`internal/db/restore.go`) y `VACUUM INTO` (`maintenance.go`). **Fix:**
  automatizar/documentar backup al volumen `/config`.
- **M7 · Sin límites de recursos (mem/cpu) en ningún compose.** Un
  transcode/scan desbocado puede OOM-killear el host. **Fix:** `mem_limit`
  + `cpus` en hubplay/Postgres/nginx.
- ✅ **M8 · Falta CSP/Permissions-Policy en los proxies.** `deploy/nginx/hubplay.conf:84-87`
  pone XFO/nosniff/Referrer/HSTS pero no CSP ni Permissions-Policy. (Nota:
  la app **sí** setea CSP server-side — `security_headers.go` — así que el
  hueco del proxy es redundante para CSP, pero Permissions-Policy falta en
  ambos lados.) **Fix:** añadir Permissions-Policy; HSTS sin `preload`.
- **M9 · `client_max_body_size 0` (ilimitado) en todo `location /`.**
  `deploy/nginx/hubplay.conf:152`. Quita cualquier techo en todos los
  endpoints (exhaustión memoria/disco). **Fix:** límite acotado solo en la
  location de uploads.
- **M10 · Persistencia SQLite frágil si el wizard escribe una ruta relativa.**
  `internal/config/config.go:198-204,295`; sin `WORKDIR` en stages 3/4 (=`/`).
  Si el YAML persistido lleva `./hubplay.db`, el DB + `-wal`/`-shm` caen en
  `/` (efímero) y se pierden al recrear el contenedor. **Fix:** `WORKDIR /config`
  en runtime + que el wizard persista siempre ruta absoluta.
- **M11 · Migraciones up-only + fallo no transaccional deja DB a medias.**
  57 migraciones, 0 con `+goose Down`; DDL SQLite auto-commit por
  statement. Si la migración N falla en el statement 2/3, queda
  parcialmente aplicada y el reboot re-ejecuta el fichero → "duplicate
  column" → arranque atascado, sin rollback ni runbook. **Fix:** backup
  obligatorio pre-upgrade (wirearlo al flujo), migraciones idempotentes
  (guards via PRAGMA), nota de recuperación en docs.

### CI/CD
- ✅ **M12 · govulncheck instalado con `@latest` en un job gating.** `ci.yml:225`.
  Indeterminismo en un gate de seguridad. **Fix:** pinear `@v1.x.y`.
- ✅ **M13 · Sin SBOM en ningún sitio.** Ni syft ni `buildx --sbom`. **Fix:**
  CycloneDX/SPDX del binario + imagen; `build-push-action` soporta
  `sbom:true` + `provenance:mode=max`.
- ✅ **M14 · `make sqlc-verify` existe pero NO corre en CI.** El drift guard
  solo protege si el dev lo recuerda. **Fix:** job de CI con `make sqlc-verify`.
- ✅ **M15 · `pnpm build` no corre en CI.** Solo en release.yml. Un cambio que
  rompa el build de producción de Vite/Rollup no se detecta hasta release.
  **Fix:** añadir `pnpm build` al job frontend. (+ coverage sin gate;
  opcional añadir floor en auth/stream.)
- ✅ **M16 · Trivy es report-only (`exit-code:0`), nunca bloquea.** `docker.yml:131`.
  CVEs HIGH/CRITICAL en `:latest` se publican igual. **Fix:** `exit-code:1`
  en CRITICAL para builds de tag/release.
- ✅ **M17 · Dependabot sin ecosistema `docker` + bases sin pinear.**
  `dependabot.yml` cubre gomod/npm/actions pero no docker; `Dockerfile`
  usa `node:22-alpine`, `golang:1.25.11-alpine`, `ubuntu:24.04`,
  `alpine:3.21` sin digest. **Fix:** añadir `docker` a dependabot + pinear
  bases por `@sha256`.

### Observabilidad / Config
- **M18 · `RequestLogger` loguea `r.RemoteAddr` crudo.** `internal/api/middleware.go:29`.
  Ignora el client-IP resuelto por el middleware trusted-proxy; tras un
  reverse proxy cada línea registra la IP del proxy → inútil para
  auditoría/abuso. **Fix:** loguear `handlers.ClientIP(r)`.
- **M19 · `logging.LogIPs` es un knob muerto.** `logging.go:11`, `config.go:306`,
  nunca leído. `log_ips:false` no hace nada (privacidad/GDPR). **Fix:**
  honrarlo en `RequestLogger` o eliminarlo.
- **M20 · Panics invisibles en métricas.** `router.go:194-199`. El
  `Recoverer` de chi recupera (no crashea — bien) pero loguea a stderr
  (no slog) y no incrementa `hubplay_http_errors_total`. **Fix:** recoverer
  propio que loguee vía slog con request_id e incremente un counter
  etiquetado `panic`.
- **M21 · Sin error-tracking/alerting más allá de logs.** Solo stdout + ring
  buffer de 500 líneas en memoria (se pierde al reiniciar). **Fix:**
  documentar agregación (journald/Loki) + opcional Sentry/OTel-errors.
- **M22 · JWT auto-gen no se persiste → riesgo multi-réplica.** `config.go:215-217`;
  `main.go:117`. OK para single-instance (siembra keystore en DB), pero con
  varias réplicas contra un Postgres compartido hay race en quién siembra,
  y un reset/restore de DB sin el secret en config invalida todos los
  tokens silenciosamente. **Fix:** exigir `HUBPLAY_AUTH_JWT_SECRET` explícito
  en multi-réplica; documentar que auto-gen es single-instance.
- **M23 · Gaps de validación de config + overrides de env ignorados en silencio.**
  `config.go:247-284,378,407`. No valida `Bind`, `BaseURL`, CIDRs de
  `TrustedProxies`, valores de `RateLimit`; un `HUBPLAY_SERVER_PORT=80x`
  arranca en el puerto default sin avisar. **Fix:** validar CIDRs y
  overrides numéricos al cargar; warning/fail en valores no parseables.
- **M24 · `example.yaml` omite media schema.** Faltan secciones `streaming`
  (caps de transcode, HW accel, timeouts), `iptv.transmux`, parte de
  `rate_limit`. Inconsistencia pgloader vs migrador interno. **Fix:**
  generar/mantener un example anotado completo desde el struct.

---

## 🟢 Bajo

- **B1 · Modulo bias** en generación de user-codes y passwords
  (`auth/device.go:357`, `auth/account_service.go:163`). Preferir rejection
  sampling para los passwords de 12 chars.
- **B2 · DNS-rebinding TOCTOU** en los SSRF guards (`imaging/safety.go:158-167`,
  `iptv/proxy.go:231-240`): resuelven, validan, y dejan re-resolver al
  dialer. Redirects sí se re-validan. **Fix:** `DialContext` que valide la
  IP conectada.
- **B3 · `refresh_token_ttl` de 720h (30d)** es largo para instancia
  pública (`config.go:298-301`). Informativo.
- **B4 · `Browse` admin con denylist** (`library.go:221-278,752-766`): un
  admin puede enumerar `/home`, `/var/lib`. Admin-only, bajo riesgo, pero
  denylist es frágil. Preferir allowlist anclada a media roots.
- ✅ **B5 · Sin `ReadHeaderTimeout`** (`main.go:505-518`); `ReadTimeout` cubre
  Slowloris pero es idiomático ponerlo.
- **B6 · `/health` filtra el string de error de DB** crudo
  (`system/health.go:73,122`, sin auth): para Postgres podría filtrar
  fragmentos de DSN. **Fix:** status genérico externo, detalle a logs.
- ✅ **B7 · CI corre en cada push de cada rama** (duplica runs con
  `pull_request`). **Fix:** scope a `main` + `concurrency:` para cancelar.
- **B8 · Bloat de SDKs cloud (AWS/Azure/GCP)** arrastrados por `tus/tusd/v2`
  (indirectos). Hinchan binario/superficie. Bajo, transitivo.
- **B9 · `web/dist/` commiteado vacío** + `//go:embed` → un build sin
  `pnpm build` previo ships UI en blanco. Verificar orden en release.
- **B10 · ESLint sin reglas type-aware** (`recommendedTypeChecked`):
  `no-floating-promises` no se enforce. **B11 · `baseUrl` deprecado**
  (tsconfig) romperá en TS7. **B12 · dep RC en build path**
  (`eslint-plugin-react-compiler 19.1.0-rc.2`). **B13 · PWA autoUpdate**
  puede servir shell stale sin prompt de "nueva versión".
- **B14 · Tests:** 12 páginas frontend sin tests (Settings, peers/federation,
  Collections…); `Movies`/`Series.test` son stubs; **0 E2E** (Playwright).
  Backend ~57%. **Fix:** smoke E2E setup→login→play + cubrir Settings/peers.
- **B15 · Sin distributed tracing** (OTel). Aceptable single-binary; útil
  solo si federation crece.
- **B16 · Sin query-timeout en jobs background** (scans/retention) más allá
  del ctx del request. **Fix:** `context.WithTimeout` acotado o
  `statement_timeout` en DSN PG.

---

## Plan de ataque por fases (orden recomendado)

> **Estado (2026-06-08):** Fase 0 **completada** ✅ — C1, C2, A1, A2, A3,
> A4, A5 implementados con tests en la rama `claude/project-review-PO25J`.
> Ver §"Fase 0 — implementación" al final.

| Fase | Tema | Items | Coste |
|---|---|---|---|
| **0 ✅** | **Bloqueantes de exposición** | C1, C2, A1, A2, A3, A4, A5 | hecho |
| **1 ✅** | **Robustez de despliegue** | A6, A7, A8, M9, M10, M11, A5-perímetro | hecho (M5/M7 diferidos, ver nota) |
| **2 ✅** | **Supply-chain / release** | A9, A10, M12, M13, M14, M15, M16, M17 | hecho (2026-06-10; SignPath y pin de NSSM quedan como acciones de operador) |
| **3** | **Observabilidad & config** | M18, M19, M20, M21, M22, M23, M24, B6 | 1 sesión |
| **4** | **Frontend hardening** | A11, A12, M2(ui), B10-B14 | 1 sesión |
| **5** | **Gobernanza & docs** | A13, M3, M8, B-varios | 0.5 sesión |

**Top-7 a tocar primero (antes de cualquier exposición a internet):**
C1, C2, A1+A2, A3+A4, A5.

> Nota: muchos fixes son de bajo riesgo y alto valor (config de proxy,
> compose, pineado de actions) — no tocan lógica de negocio. Los de auth
> (C1, A1, A2) requieren cuidado y tests.

---

## Fase 0 — implementación (2026-06-08)

Rama `claude/project-review-PO25J`. 4 commits, todos con tests; build +
`go vet` + `-race` verdes en los paquetes tocados (auth, iptv, api,
logging, config).

| Item | Qué se hizo | Tests |
|---|---|---|
| **C1** | `extractToken` ya no acepta `?token=`; solo Bearer/cookie | `middleware_test.go` (Bearer/cookie 200, query/none 401) |
| **C2** | `RedactURL` + redacción por-valor en el `ReplaceAttr` central para claves url/m3u_url/epg_url/upstream/… | `logging_test.go` |
| **A1** | `IPRateLimitMiddleware` en login/refresh/setup/device (30/min burst 10 por IP); SSE fuera | reuso del limiter de federation |
| **A2** | Lockout de login por tupla `user:<u>@<ip>` (no username global) → sin DoS de cuenta | `service_test.go` (víctima desde otra IP entra) |
| **A3** | Proxy IPTV firma (HMAC) las URLs reescritas; handler exige+verifica firma → no relay abierto. No host-lock (compatible multi-CDN) | `proxy_sign_test.go`, `iptv_test.go` (403 invalid sig) |
| **A4** | Quitadas las 4 cabeceras `Access-Control-Allow-Origin: *` del proxy | suite iptv verde |
| **A5** | `observability.metrics_token` (+env) gate Bearer/?token= en /metrics; aviso si expuesto sin token | `metrics_auth_test.go` |

**Notas:**
- C1/A5 aceptan el token de métricas por `?token=` SOLO para `/metrics`
  (scrapers que no ponen cabecera); el token de sesión sigue prohibido en
  query.
- A3 usa clave de firma aleatoria de vida de proceso: al reiniciar, los
  players en vuelo re-piden el manifest (refresco de segundos en live).
- Pendiente de Fase 1 (deploy): bloquear `/metrics` en nginx/Caddy/Traefik
  como perímetro adicional a A5; binding a localhost durante setup (M3).

---

## Fase 1 — implementación (2026-06-08)

Rama `claude/project-review-PO25J`. Enfoque **plug-and-play** (estilo
Plex): defaults seguros que funcionan out-of-the-box, cero pérdida de
datos, sin pasos manuales. 3 commits, con tests; build/vet/`-race` verdes.

| Item | Qué se hizo | Test |
|---|---|---|
| **A7** | `tini` como PID 1 en ambos stages → reapea ffmpeg/ffprobe huérfanos + forward SIGTERM. `init:true` en compose (defensa extra) | — (Docker) |
| **A8** | `stop_grace_period: 40s` en los 2 compose (la app drena en 30s) | — (compose) |
| **M10** | `SaveDatabaseConfig` ancla el SQLite vacío/relativo bajo el dir del config (volumen `/config`) → no pérdida de datos al recrear contenedor, sin depender del input del frontend | `service_test.go` |
| **A6** | nginx: bloque SSE dedicado (`proxy_buffering off`) para events/me-events/uploads-events/device-events/admin-logs | — (nginx) |
| **M9** | nginx: `location /` body acotado a 1g; subidas tus por bloque `/api/v1/uploads/` dedicado (sin tope, sin rate-limit) | — (nginx) |
| **A5-perímetro** | `deny`/`403` de `/metrics` en nginx y Caddy (complementa el token del server, Fase 0) | — |
| **M11** | Backup SQLite automático antes de migrar (`VACUUM INTO` a `<dir>/backups/`, keep 3, best-effort, solo si hay pending + datos). Red de seguridad para migraciones up-only | `build_database_backup_test.go` + integración real |

**Diferidos deliberadamente (incompatibles con plug-and-play si se fuerzan):**
- **M5** (password Postgres por defecto): Postgres es **opt-in/avanzado**
  (SQLite es el default y el puerto no se expone). Auto-generar un
  password compartido en compose puro es frágil; se deja documentado. Si
  el operador activa el toggle PG, debería poner su propio password.
- **M7** (límites de recursos mem/cpu): **NO hardcodeados** — dependen del
  hardware (un Raspberry Pi vs un servidor potente). Hardcodear límites
  rompería transcoding en hardware capaz o causaría OOM en uno pequeño.
  Mejor sin límite (la app usa lo que necesita) o que el operador los
  ajuste. Documentado como decisión.

**Pendiente Fase 2+ (no plug-and-play-bloqueante):** supply-chain
(SHA-pin actions, provenance/firma, checksum FFmpeg), observabilidad
(client IP en logs, panics en métricas), frontend (error boundaries,
virtualización), gobernanza (README/SECURITY/CODEOWNERS).

---

## Fase 2 — implementación (2026-06-10)

Rama `claude/project-review-8tznz4`. Solo CI/scripts — 0 cambios de
código de la app. Validado con actionlint, `bash -n`, parse YAML y
`make sqlc-verify` en local.

| Item | Qué se hizo |
|---|---|
| **A9** | Las 18 actions de `ci.yml`/`docker.yml`/`release.yml` pineadas a SHA completo con comentario `# vX.Y.Z` (incl. `react-doctor@main` → SHA del día). Dependabot bumpea SHA+comentario juntos |
| **A10** | (1) `actions/attest-build-provenance` en `release-tag` y `release-nightly` → SLSA provenance firmada (Sigstore/OIDC) de todos los assets; verificable con `gh attestation verify`. (2) `fetch-ffmpeg.sh` verifica sha256 de cada asset BtbN contra el campo `digest` de la API de releases de GitHub (canal TLS independiente; GA desde jun-2025) y los zips de evermeet contra su API de info; `FFMPEG_SKIP_VERIFY=1` como escape local, mismatch siempre fatal. (3) `install.sh`: `.sha256` ausente ahora es **fatal** (`HUBPLAY_SKIP_VERIFY=1` opt-out explícito). (4) NSSM: verificación fail-closed opt-in vía repo var `NSSM_EXPECTED_SHA256` (patrón SignPath) |
| **M12** | govulncheck `@latest` → `@v1.3.0` (la DB de vulns sigue siendo online; solo se congela el tool) |
| **M13** | `build-push-action`: `sbom:true` + `provenance:mode=max` (attestations OCI en GHCR; off en PRs porque `load` no las soporta) |
| **M14** | Job `sqlc-verify` en ci.yml (`make sqlc-verify`; sqlc v1.31.1 auto-instala toolchain go1.26 vía GOTOOLCHAIN=auto — verificado en local) |
| **M15** | Step `pnpm build` en el job test-frontend |
| **M16** | Trivy gate bloqueante (`exit-code:1`, severity CRITICAL, ignore-unfixed) **solo en builds de tag**; main sigue report-only via SARIF |
| **M17** | Ecosistema `docker` en dependabot + 4 bases del Dockerfile pineadas `tag@sha256:digest` |

**Limitaciones documentadas (aceptadas):**
- La verificación de FFmpeg autentica el **canal de descarga** (CDN), no
  el origen BtbN/evermeet — protegerse de un upstream comprometido
  requeriría buildear FFmpeg propio. El tag `latest` de BtbN sigue siendo
  rolling: pinearlo a un autobuild concreto no es viable porque BtbN
  borra los autobuilds antiguos (rompería releases futuros).
- Sin cosign manual de archivos: la attestation de GitHub cubre el mismo
  caso (firma + provenance) con menos llaves que gestionar.

**Acciones de operador pendientes (no de código):**
1. `NSSM_EXPECTED_SHA256`: correr una release, copiar el sha256 logueado,
   contrastarlo con una fuente independiente y fijar la repo variable.
2. SignPath (ya wireado): aplicar en signpath.org y activar
   `HUBPLAY_SIGNING_ENABLED` (guía en
   `docs/architecture/windows-installer-signing.md`).
