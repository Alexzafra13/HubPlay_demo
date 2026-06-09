# Estado del proyecto

> **Entrypoint de cada sesión.** Solo el estado vivo y lo que falta. El
> detalle de sesiones cerradas vive en `archive/` (no se borra nada, solo
> se reubica). Última limpieza: 2026-06-09.

---

## 🔭 Estado actual (2026-06-09)

**Salud:** MVP funcional, cerca de early-production. Hay una **PR grande
abierta sin mergear** con endurecimiento/perf de esta sesión (ver abajo).

| Área | Estado |
|---|---|
| Tests backend | `go test ./...` verde (con `-race` en los paquetes tocados) |
| Tests frontend | **718/718** vitest verdes; `tsc` y `eslint` (0 errores) limpios |
| PRs abiertas | **#504** (rama `claude/monorepo-audit-go-react-94dgzk`) — 13 commits, **pendiente de CI + merge**. #489 dependabot pendiente |
| Audit prod 2026-06-08 | Fases 0/1 + Bloques 1/2 ✅ shipped. **Fases 2–5 abiertas** |
| Audits arquitectónicos previos | 2026-05-14 ✅ y 2026-05-27 (macro + per-package) ✅ — cerrados, archivados |

### 🚧 PR #504 — endurecimiento + perf + observabilidad (2026-06-09, SIN mergear)

Resultado de una auditoría técnica completa (comité multi-rol) → 13 commits
con tests. **Solo backend; el frontend embebido NO cambia — no hay nada
nuevo visible en la web.** El único cambio de comportamiento: el ACL por
biblioteca ahora se aplica en VOD/streaming (un usuario sin grant deja de
ver/reproducir esa biblioteca). Contenido de la PR:

- **Seguridad:** H1 — ACL por biblioteca en TODO el surface VOD/streaming
  (stream endpoints autorizan antes de crear sesión; item detail/children/
  recommendations; `/libraries/{id}`+items; scoping cross-library en
  `/items`, `/items/search`, `/items/latest` vía `ItemFilter.LibraryIDs`).
  Home rails ya estaban scoped en SQL (verificado). Cap de body JSON 1 MiB
  (`handlers.DecodeJSON`, anti-DoS). CSRF constant-time. Logs de errores
  antes tragados (provider register, migrador PG).
- **Fiabilidad:** ffmpeg en su propio process-group + kill del árbol
  (`internal/procutil`, build-tagged) → sin huérfanos VAAPI/NVENC. Quitado
  el cap de 30 min del scan (mataba bibliotecas grandes).
- **Perf:** cgroup-awareness (`internal/runtimetune`: GOMAXPROCS desde
  cuota CFS + GOMEMLIMIT; autotune lee GOMAXPROCS) → corre bien en
  Pi/NAS/contenedores. Scan en 1 transacción/fichero (`IngestItem`). Drop
  de 2 índices PK-redundantes (migración **058**, ambos backends). `/health`
  ya no llama a `ReadMemStats` (sin pausa STW) — verificado re-perfilando.
- **Observabilidad/medición (opt-in, sin impacto):** pprof gated
  (`observability.pprof_enabled`, off por defecto, fail-closed sin token);
  stack Prometheus+Grafana turnkey (`deploy/observability/`) sobre las
  métricas RED ya existentes; script k6 (`scripts/perf/`); herramientas dev
  `cmd/hpseed` + `cmd/hploadgen`; runbook `docs/perf-measurement.md`.
  ⚠️ **OJO: el stack Grafana/Prometheus está EMPAQUETADO pero NO levantado
  ni hay datos en vivo.** Son ficheros de config en el repo; el operador lo
  arranca aparte (`docker compose -f deploy/observability/docker-compose.observability.yml up`)
  contra un HubPlay corriendo con `metrics_token`. Grafana es un programa
  SEPARADO (puerto 3000), solo para el operador; NO es parte de la web ni
  se despliega con el binario. `/metrics` (los datos crudos) sí existe ya
  en el binario, pero por sí solo no "se ve" — necesita el scraper+dashboard.

**Aprendizajes de la sesión:**
- `runtime.NumCPU()` NO respeta la cuota CFS (`--cpus`), solo afinidad
  (`--cpuset`). Para contenedores hay que leer cgroup y fijar GOMAXPROCS.
- pprof reveló en vivo que `/health` hacía `ReadMemStats` (STW) por request
  — los microbenchmarks de DB nunca lo verían. Profiling > intuición.
- El perfil de allocs reprodujo el hallazgo #1 del perf-doc
  (`ListChannelsByLibrary` ~51% de allocs por el prober IPTV con 5000 canales).
- Para medir un media server de verdad: Grafana (sobre métricas RED ya
  existentes) + pprof + medir transcodes bajo reproducción real. k6 es
  secundario (mide la API, no el transcoding). No sobre-ingenierar.

**Endurecido de cara a internet** (2026-06-08, en `main`): token solo por
Bearer/cookie, redacción de credenciales en logs, rate-limit de auth,
lockout por (user,IP), firma HMAC del proxy IPTV, gate de `/metrics`,
`X-Forwarded-Proto` de confianza, forzar cambio de pass, setup solo-LAN,
Permissions-Policy, slowloris timeout; tini + stop_grace + persistencia
SQLite + backup pre-migración; error boundary por ruta + grid
virtualizado. Detalle: `archive/2026-05-27-to-06-08.md`.

---

## 📋 Trabajo abierto

**Roadmap principal:** `audit-2026-06-08-production-readiness.md` (Fases
2–5). Ninguna bloquea el uso plug-and-play básico.

| Prioridad | Tema | Items |
|---|---|---|
| Media-alta | **Fase 2 — supply-chain / release** | A9 (SHA-pin de GitHub Actions), A10 (provenance/firma de binarios + checksum FFmpeg + install.sh), M12–M17 (Trivy/govulncheck bloqueantes, SBOM, `pnpm build` y sqlc-verify en CI) |
| Media | **Fase 3 — observabilidad / config** | M18–M21, M23, M24 (IP de cliente en logs, panics en métricas, validación de config, completar `example.yaml`). M22 ya descartado (no-issue). |
| Media | **Fase 4 — frontend** | B10 (ESLint type-aware), B14 (tests de páginas grandes). A11/A12 ya hechos. |
| Baja | **Fase 5 — gobernanza** | README de despliegue, `SECURITY.md`, `CODEOWNERS` |
| Baja | **Bajos sueltos** | B2 (DNS-rebind TOCTOU), B3 (refresh TTL 30d), M6 (backup periódico) |

**Pendiente del hilo perf/medición (sesión 2026-06-09, post-#504):**
- **C1 worker-pool del scan** — paralelizar `processFile` + desacoplar el
  enrich TMDb a un pipeline rate-limited. Refactor delicado (estado mutable
  compartido: `showCache`/`seenPaths`/`result`); hacer con harness sobre
  biblioteca real. *Lo único crítico del audit que queda sin tocar.*
- **#4 paginación de canales** — el read path (`GetChannelsForUser`) aplica
  3 capas de overlay (logo/admin/user) en Go, así que paginar exige meter
  la ordenación/visibilidad en SQL. Rewrite ordering-sensitive; verificar
  en LiveTV real. El 51% de allocs medido era del prober (background), no
  del read path.
- **Métrica time-to-first-segment del transcode** — el KPI que falta para
  un media server; instrumentar el stream manager + panel en el dashboard.
  Verificar con ffmpeg real en el target.
- **OFFSET profundo** (DB-High) sigue O(offset); migrar a cursor-only.
- Highs del audit sin tocar: SHA-pin de Actions (A9), SBOM/provenance,
  `AuditEmitter` 23 métodos (ISP). Mediums: rate-limit TMDb, SSRF guard del
  transmux IPTV, rol stale en JWT, god-components React (`UsersAdmin` 1828 LoC).

**Pendientes menores (de audits cerrados):**
- **TT-8 (resto)** — traducir comentarios en inglés en los sub-paquetes de
  `handlers/` (admin, auth, federation, iptv, me, media, system). El root
  ya está en español. Incremental, al tocar cada fichero. Cosmético.
- **F15-10/11/12** — polish opcional (fakes compartidos, naming,
  concurrency tests).
- **Distribución avanzada** — auto-update, TLS LAN, macOS notarized,
  AppImage. Producto, sesión grande.
- **SignPath** (operador): aplicar en signpath.org + activar
  `vars.HUBPLAY_SIGNING_ENABLED`. Guía:
  `docs/architecture/windows-installer-signing.md`.

---

## 🏛 Referencias vivas

- `architecture-decisions.md` — ADRs (AppError, observability/sink,
  keystore, preflight, sqlc adapter, ADR-026 logs).
- `conventions.md` — patrones del codebase, reglas de test, anti-ciclo,
  comentarios en español, regeneración sqlc.
- `audit-2026-06-08-production-readiness.md` — roadmap activo (Fases 2–5).
- `perf-benchmarks-2026-05-17.md` — baseline benchmarks dual-backend.
- `web/verify/` — arnés de verificación en navegador del grid virtualizado.

## 📦 Archivo (`archive/`, no se lee al inicio)

- `2026-05-27-to-06-08.md` — endurecimiento prod (Fases 0/1 + Bloques 1/2),
  F15-5, TT-8 root, audits 2026-05-27 cerrados.
- `2026-05-19-to-05-27.md` y anteriores — sesiones históricas.
- `audit-2026-05-14-go-backend-review.md` + `intervention-2026-05-14.md` —
  audit original (cerrado).
- `audit-2026-05-27-architecture-macro.md` +
  `audit-2026-05-27-per-package-review.md` — audits estructurales (cerrados).
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
  setup externo del operador.
- **Cerrar por análisis** cuando el runtime moderno resuelve el problema
  teórico (F15-9 / Go 1.23+). No refactorizar sin bug observable.
- **Leer el código existente antes de implementar del backlog** (el
  installer Windows ya existía; solo faltaba firmar).
- **Fix centralizado vs audit por paquete** (ej. `handleServiceError`,
  redactor de slog central, middleware XFP) — un punto en vez de N.
- **Verificación en navegador real para cambios visuales/de layout**: los
  tests jsdom no ven el layout. El arnés Playwright (`web/verify/`)
  detectó que el React Compiler rompía el reciclado del grid — algo que
  ningún test jsdom podía pillar.
- **React Compiler + stores externos mutables** (`@tanstack/react-virtual`):
  sobre-memoiza y rompe updates; aislar en un subcomponente con
  `"use no memo"`. La regla `incompatible-library` solo reconoce
  `useVirtualizer`, no `useWindowVirtualizer`.
