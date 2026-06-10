# Estado del proyecto

> **Entrypoint de cada sesión.** Solo el estado vivo y lo que falta. El
> detalle de sesiones cerradas vive en `archive/` (no se borra nada, solo
> se reubica). Última limpieza: 2026-06-09.

---

## 🔭 Estado actual (2026-06-10)

**Salud:** MVP funcional, cerca de early-production. Todo el trabajo
hasta el 2026-06-09 está **mergeado en `main`**; la Fase 2 (supply-chain)
está en la rama `claude/project-review-8tznz4`.

| Área | Estado |
|---|---|
| Tests backend | `go test ./...` verde (con `-race` en los paquetes tocados) |
| Tests frontend | **718/718** vitest verdes; `tsc` y `eslint` (0 errores) limpios |
| PRs abiertas | ninguna nuestra (#489 dependabot pendiente de revisar) |
| Audit prod 2026-06-08 | Fases 0/1/2 + Bloques 1/2 ✅. **Fases 3–5 abiertas** |
| Audits arquitectónicos previos | 2026-05-14 ✅ y 2026-05-27 (macro + per-package) ✅ — cerrados, archivados |

**Endurecido de cara a internet** (2026-06-08, en `main`): token solo por
Bearer/cookie, redacción de credenciales en logs, rate-limit de auth,
lockout por (user,IP), firma HMAC del proxy IPTV, gate de `/metrics`,
`X-Forwarded-Proto` de confianza, forzar cambio de pass, setup solo-LAN,
Permissions-Policy, slowloris timeout; tini + stop_grace + persistencia
SQLite + backup pre-migración; error boundary por ruta + grid
virtualizado. Detalle: `archive/2026-05-27-to-06-08.md`.

---

## 📋 Trabajo abierto

**Roadmap principal (nuevo):** `audit-2026-06-10-playback-chain.md` —
audit focalizado de la cadena de playback (decisión de streaming, FFmpeg,
probe, IPTV, player hls.js). 39 hallazgos, 4 críticos. **Es el trabajo de
más valor de usuario pendiente** — todo lo demás son cimientos.

| Prioridad | Tema | Items |
|---|---|---|
| **Alta** | **Playback P0 — rompe reproducción común** | PB-1 (MKV falso DirectPlay por alias webm), PB-2 (segmentos servidos a medio escribir), PB-3 (keyframes sin alinear al grid de 6s del seeking), PB-4 (sin listener de error del `<video>` en VOD) |
| Alta | **Playback P1 (a-d)** | decisión/transcode (PB-6..9, 20, 21), trickplay+probe (PB-11..13, 24, 25 — PB-12 es ACL faltante), IPTV (PB-14, 15, 27, 28), player (PB-16..18, 32, 35) |
| Media | **Playback P2/P3** | VAAPI real (PB-5), ABR/caps (PB-10), surround (PB-22), Dolby Vision (PB-23), E2E smoke Playwright |

**Roadmap secundario:** `audit-2026-06-08-production-readiness.md` (Fases
3–5). Ninguna bloquea el uso plug-and-play básico.

**Fase 2 — supply-chain / release: ✅ hecha (2026-06-10)** en
`claude/project-review-8tznz4`: actions SHA-pineadas, SLSA provenance
(attest-build-provenance) en releases + nightly, SBOM/provenance OCI en
la imagen Docker, verificación sha256 de FFmpeg (API digest) y NSSM
(opt-in por repo var), `.sha256` fatal en install.sh, Trivy bloqueante
en tags, govulncheck pineado, jobs `sqlc-verify` + `pnpm build` en CI,
dependabot docker + bases por digest. Detalle y acciones de operador
pendientes (NSSM_EXPECTED_SHA256, SignPath): §"Fase 2 — implementación"
del audit.

| Prioridad | Tema | Items |
|---|---|---|
| Media | **Fase 3 — observabilidad / config** | M18–M21, M23, M24 (IP de cliente en logs, panics en métricas, validación de config, completar `example.yaml`). M22 ya descartado (no-issue). |
| Media | **Fase 4 — frontend** | B10 (ESLint type-aware), B14 (tests de páginas grandes). A11/A12 ya hechos. |
| Baja | **Fase 5 — gobernanza** | README de despliegue, `SECURITY.md`, `CODEOWNERS` |
| Baja | **Bajos sueltos** | B2 (DNS-rebind TOCTOU), B3 (refresh TTL 30d), M6 (backup periódico) |

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
- `audit-2026-06-10-playback-chain.md` — **roadmap activo principal**
  (cadena de playback: 39 hallazgos, plan P0–P3).
- `audit-2026-06-08-production-readiness.md` — roadmap secundario (Fases 3–5).
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
