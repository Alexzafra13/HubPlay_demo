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
- i18n: i18next (solo `en` por ahora)
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

## Métricas rápidas

- **105** ficheros `.go` en `internal/` · **38** ficheros `_test.go` (~36%)
- **12** test files en frontend (cobertura ~15% estimada)
- **25+** docs de arquitectura en `docs/architecture/`

---

## Convenciones del proyecto

- Errores: sentinel + wrap con `%w`; definidos en `domain/errors.go`
- Handlers siguen patrón Handler → Service → Repository → DB
- `main.go` arranca en fases: foundation → DB → infra → services → HTTP; shutdown graceful
- Rama activa de desarrollo: `claude/project-review-setup-QYnZs` (solo pushear ahí)
- Commits cortos, descriptivos; PRs con contexto

---

## Memoria de sesiones

Ver `.claude/memory/` para:
- `project-status.md` — estado actual, qué falta, próximos pasos
- `review-2026-04-15.md` — snapshot del review senior inicial
