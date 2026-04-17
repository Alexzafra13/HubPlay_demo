# HubPlay — Contexto de proyecto

> Servidor de media self-hosted estilo **Plex / Jellyfin**. Go backend + React frontend.
> Versión: `0.1.0` · Estado: **MVP funcional, cerca de early-production**.

---

## Stack

**Backend (Go 1.24.7)**
- Router: `chi/v5` + `chi/cors`
- DB: SQLite (`modernc.org/sqlite`, pure-Go, sin CGO)
- Queries: `sqlc` (generated) · Migraciones: `goose`
- Auth: JWT (`golang-jwt/v5`) + bcrypt + refresh tokens
- Transcoding: FFmpeg / FFprobe (externo)

**Frontend (React 19 + Vite 7 + TS estricto)**
- Estilado: TailwindCSS v4
- Estado: Zustand · Data: TanStack Query v5
- Routing: react-router v7 (lazy routes)
- Player: `hls.js`
- i18n: i18next con `en` + `es` configurados (`web/src/i18n/index.ts`)
- Tests: Vitest + Testing Library + jsdom

**Infra**
- Dockerfile multi-stage + target `hwaccel` (amd64 only, VAAPI/NVENC)
- `docker-compose.yml` (prod) y `docker-compose.dev.yml`
- CI en GitHub Actions (`.github/workflows/`)
- Package manager frontend: `pnpm@10.32.1`

---

## Layout del repo

```
cmd/hubplay/              # entrypoint binario
internal/
  api/          # handlers HTTP + middleware (CORS, CSRF, rate-limit)
  auth/         # JWT, sesiones, login/refresh, QuickConnect
  blurhash/     # hashes para miniaturas
  clock/        # abstracción de tiempo (testable)
  config/       # YAML + env override, auto-gen JWT secret
  db/           # repos sqlc-generated
  domain/       # entidades + errores sentinel
  event/        # pub/sub interno (progreso de scans, etc.)
  imaging/      # generación de thumbnails / blurhash
  iptv/         # M3U + EPG + channel proxy
  library/      # scanner + scheduler + servicios de biblioteca
  logging/      # wrapper de logger
  probe/        # ffprobe wrapper
  provider/     # TMDb, Fanart, OpenSubtitles (interfaz pluggable)
  scanner/      # walker de filesystem + cambios
  setup/        # wizard de primera ejecución
  stream/       # decisión direct-play / direct-stream / transcode
  testutil/     # helpers de test
  user/         # servicio + permisos
migrations/sqlite/        # SQL up-only (sin down)
web/src/
  api/ components/ hooks/ i18n/ pages/ store/ styles/ test/ utils/
docs/architecture/        # 25+ docs (muy completos)
deploy/                   # compose de prod + nginx + TLS
```

---

## Comandos habituales

```bash
make build          # build web + go
make dev            # go con air (hot reload), requiere hubplay.example.yaml
make web-dev        # vite dev server (HMR)
make test           # go test -race ./...
make test-cover     # genera coverage.html
make lint           # golangci-lint
make sqlc           # regenera código desde queries
cd web && pnpm test # tests frontend (vitest)
```

Config de ejemplo: `hubplay.example.yaml` (puerto 8096, SQLite local, JWT auto-gen, rate-limit activo).

---

## Métricas rápidas (verificadas 2026-04-17)

- **97** ficheros `.go` de producción en `internal/`+`cmd/` · **53** `_test.go` (~55%)
- **12** test files en frontend (cobertura ~15% estimada — páginas y admin aún sin tests)
- **26** docs de arquitectura en `docs/architecture/`
- **74** rutas HTTP registradas en `internal/api/router.go`
- Handlers con tests: image, stream, progress, iptv, library (5 de 15). Sin tests: items, users, setup, providers, events, health.

---

## Convenciones del proyecto

- Errores: `domain.AppError` tipo rico con `.Kind` sentinel — compatible con `errors.Is()`. Ver ADR en `docs/memory/architecture-decisions.md`
- Handlers: Handler → Service → Repository → DB. Repos usan sqlc-generated code (**migración completa** — 83 de 88 métodos vía sqlc; 5 quedan raw SQL por razones documentadas)
- `main.go` arranca en fases: foundation → DB → infra → services → HTTP; shutdown graceful
- Patrón anti-ciclo: cuando un paquete necesita observability, inyectar vía interface local (sink pattern). Nunca importar `observability` desde `stream`, `handlers`, etc.
- sqlc adapter pattern: repos públicos son thin wrappers sobre `db/sqlc/*.sql.go`. Ver `docs/memory/conventions.md`
- Event bus (`internal/event`): `Subscribe` devuelve `func()` para unsub — callers con ciclo de vida (SSE, jobs) **deben** llamarlo al terminar o filtran handlers
- Imaging: pure helpers + pathmap + safety (SSRF-safe fetch, decompression-bomb guard) en `internal/imaging/`. `library.ImageRefresher` hace batch refresh de imágenes; el handler HTTP es un thin wrapper.
- Commits cortos, descriptivos; PRs con contexto

---

## Memoria de proyecto

Ver `docs/memory/` (versionado en git) para contexto entre sesiones:
- `project-status.md` — estado actual, qué se hizo, qué falta, próximos pasos
- `architecture-decisions.md` — ADRs: AppError, observability, keystore, sink pattern, preflight
- `conventions.md` — patrones del codebase, reglas de test, anti-ciclo
- `audit-2026-04-15.md` — snapshot del review senior inicial
- `README.md` — política de docs/memory/ y diferencia con docs/architecture/

**Leer `docs/memory/project-status.md` al inicio de cada sesión** para retomar contexto.

<!-- autoskills:start -->

Summary generated by `autoskills`. Check the full files inside `.claude/skills`.

## Accessibility (a11y)

Audit and improve web accessibility following WCAG 2.2 guidelines. Use when asked to "improve accessibility", "a11y audit", "WCAG compliance", "screen reader support", "keyboard navigation", or "make accessible".

- `.claude/skills/accessibility/SKILL.md`
- `.claude/skills/accessibility/references/A11Y-PATTERNS.md`: Practical, copy-paste-ready patterns for common accessibility requirements. Each pattern is self-contained and linked from the main [SKILL.md](../SKILL.md).
- `.claude/skills/accessibility/references/WCAG.md`

## Design Thinking

Create distinctive, production-grade frontend interfaces with high design quality. Use this skill when the user asks to build web components, pages, artifacts, posters, or applications (examples include websites, landing pages, dashboards, React components, HTML/CSS layouts, or when styling/beaut...

- `.claude/skills/frontend-design/SKILL.md`

## Go Development Patterns

Idiomatic Go patterns, best practices, and conventions for building robust, efficient, and maintainable Go applications.

- `.claude/skills/golang-patterns/SKILL.md`

## Go Testing Patterns

Go testing patterns including table-driven tests, subtests, benchmarks, fuzzing, and test coverage. Follows TDD methodology with idiomatic Go practices.

- `.claude/skills/golang-testing/SKILL.md`

## SEO optimization

Optimize for search engine visibility and ranking. Use when asked to "improve SEO", "optimize for search", "fix meta tags", "add structured data", "sitemap optimization", or "search engine optimization".

- `.claude/skills/seo/SKILL.md`

<!-- autoskills:end -->
