# Tooling & Technical Decisions

## Go Dependencies

### Core

| Library | Purpose | Why This One |
|---------|---------|-------------|
| `github.com/go-chi/chi/v5` | HTTP router + middleware | Lightweight, idiomatic Go, uses stdlib `net/http` directly, easy to debug |
| `modernc.org/sqlite` | SQLite driver | Pure Go (no CGO), cross-compiles easily for all platforms |
| `github.com/golang-jwt/jwt/v5` | JWT tokens | Official library, well-maintained, no unnecessary deps |
| `github.com/google/uuid` | UUIDs | Simple, reliable, from Google |
| `github.com/fsnotify/fsnotify` | File system watcher | De facto standard in Go for file watching |
| `golang.org/x/crypto/bcrypt` | Password hashing | Part of Go extended stdlib, battle-tested |
| `golang.org/x/sync/semaphore` | Concurrency limiting | Stdlib extended, used for FFprobe worker limits |
| `gopkg.in/yaml.v3` | YAML config parsing | Standard YAML library for Go |
| `github.com/gorilla/websocket` | WebSocket connections | Most used, stable, well-documented |
| `google.golang.org/grpc` | Plugin communication | Industry standard RPC, language-agnostic |
| `github.com/pressly/goose/v3` | Database migrations | Simple, supports raw SQL, embeddable in binary |

### Optional (for PostgreSQL support)

| Library | Purpose |
|---------|---------|
| `github.com/jackc/pgx/v5` | PostgreSQL driver (pure Go, fast) |

### What We're NOT Using

| Rejected | Why |
|----------|-----|
| GORM / Ent (ORM) | SQL directo is clearer and faster for SQLite. ORMs add abstraction we don't need |
| Gin | More "magic" than chi, harder to debug. chi is more Go-idiomatic |
| Redis | No distributed cache needed. In-memory Go maps are enough for a monolith |
| RabbitMQ / NATS | In-process event bus is sufficient. No need for external message broker |
| GraphQL | REST is simpler and covers all our use cases |
| Next.js / SSR | Static SPA embedded in Go binary is simpler than server-side rendering |
| `go-sqlite3` (CGO) | Requires C compiler, complicates cross-compilation. modernc.org/sqlite is pure Go |
| Email service | No email in the system. Admin manages users directly |

---

## Frontend Dependencies

### Core

| Library | NPM Package | Purpose | Why |
|---------|-------------|---------|-----|
| Vite | `vite` | Build tool + HMR | Instant HMR, fast builds, TypeScript nativo |
| TypeScript | `typescript` | Type safety | Catches bugs at compile time |
| React 18+ | `react` | UI framework | Ecosystem, community, mature video player libs |
| React Router v7 | `react-router` | SPA routing | Type-safe routes, lazy loading de páginas |
| TanStack Query | `@tanstack/react-query` | API data fetching + cache | ~5M descargas/semana, deduplicación, refetch en foco. Estándar de facto para server state |
| Zustand | `zustand` | Client state management | ~1KB, sin boilerplate, sin Provider. Para estado del player, auth, UI prefs |
| Tailwind CSS v4 | `tailwindcss` | Styling (dark theme) | Utility-first, `dark:` mode nativo, purge automático, el más rápido para iterar |

### Video & Streaming

| Library | NPM Package | Purpose | Why |
|---------|-------------|---------|-----|
| Vidstack | `vidstack` | Video player completo | Modular, controles accesibles, HLS/DASH/DRM. Nota: se fusiona con Video.js v10 en 2026 (Vidstack + Plyr + Video.js + Media Chrome → proyecto unificado) |
| hls.js | `hls.js` | HLS streaming (bajo nivel) | Fallback directo si Vidstack no cubre un caso edge |
| Shaka Player | `shaka-player` | DRM (Widevine/PlayReady/FairPlay) | Mantenido por Google, soporte DRM completo para contenido protegido |
| SubtitlesOctopus | `@jellyfin/libass-wasm` | Subtítulos ASS/SSA | libass compilado a WASM, renderiza estilos complejos (anime, karaoke). Fork mantenido por Jellyfin |
| media-captions | `media-captions` | Subtítulos VTT/SRT | Parser ligero (~5KB) de WebVTT y SRT, integrado con Vidstack |

### EPG & Live TV

| Library | NPM Package | Purpose | Why |
|---------|-------------|---------|-----|
| Planby | `planby` | EPG timeline grid | Único componente React maduro para guía EPG. Virtual scrolling, 10k+ eventos, usado por JW Player |
| @noriginmedia/norigin-spatial-navigation | `@noriginmedia/norigin-spatial-navigation` | Navegación TV/mando | Navegación espacial con flechas tipo Smart TV. Usado en TVs reales (Tizen, webOS, Fire TV) |

### UI & UX

| Library | NPM Package | Purpose | Why |
|---------|-------------|---------|-----|
| TanStack Virtual | `@tanstack/react-virtual` | Virtual scrolling | Headless, 60fps, más flexible que react-window para layouts custom (channel grid, library) |
| unlazy | `unlazy` | Lazy loading imágenes | Soporta BlurHash + ThumbHash, framework-agnostic, IntersectionObserver |
| react-blurhash | `react-blurhash` | BlurHash placeholders | Componente React para decodificar y mostrar blurhash mientras carga la imagen real |
| React Hook Form | `react-hook-form` | Formularios (admin/settings) | Rendimiento, validación con Zod, mínimos re-renders |

### Infra & Utilidades

| Library | NPM Package | Purpose | Why |
|---------|-------------|---------|-----|
| react-use-websocket | `react-use-websocket` | WebSocket (React hook) | Hook idiomático `useWebSocket` con auto-reconnect, heartbeat, message history. Mejor integración con React que reconnecting-websocket |
| react-i18next | `react-i18next` | Internacionalización | Estándar, lazy loading de traducciones, pluralización. Permite español/inglés desde v1 |
| vite-plugin-pwa | `vite-plugin-pwa` | PWA support | Service worker, offline fallback, install prompt, precaching. Zero-config con Vite + Workbox |
| Vitest | `vitest` | Unit/component testing | Integrado con Vite, API compatible Jest, rápido. Browser Mode para tests en navegador real |
| Testing Library | `@testing-library/react` | Testing de componentes | Test como el usuario interactúa, no como se implementa |
| Playwright | `@playwright/test` | E2E testing | Cross-browser (Chromium, Firefox, WebKit), auto-waiting, network interception |
| MSW | `msw` | API mocking para tests | Mock Service Worker — intercepta requests a nivel de SW, realista para unit y E2E |

### What We're NOT Using (Frontend)

| Rejected | Why |
|----------|-----|
| Redux / Redux Toolkit | Demasiado boilerplate para nuestro caso. Zustand cubre todo con 1KB |
| react-window / react-virtualized | TanStack Virtual es más moderno, headless, y flexible |
| styled-components / CSS Modules | Tailwind es más rápido de iterar y genera bundles más pequeños |
| Socket.io | Overhead innecesario. reconnecting-websocket + WebSocket nativo es suficiente |
| Video.js v8 | Legacy, pesado (~300KB). Vidstack es la alternativa moderna (y se fusionan en Video.js v10) |
| Next.js / Remix | No necesitamos SSR. SPA embebida en Go es más simple |
| Axios | fetch() nativo + TanStack Query es suficiente. Sin dependencia extra |
| Material UI / Ant Design | Demasiado opinionated, difícil de customizar para un media player. Tailwind + componentes propios |

---

## Development Tools

| Tool | Purpose |
|------|---------|
| `golangci-lint` | Go linter — detects bugs, dead code, common mistakes |
| `air` | Hot reload in dev — recompiles Go on file save |
| `protoc` + `protoc-gen-go-grpc` | Generate Go code from `.proto` files (plugin interfaces) |
| `go test -race` | Race condition detector |
| `Makefile` | Centralized commands for build, test, lint, docker, dev |

---

## Build System

```makefile
# Key Makefile targets
make dev              # Start backend (air hot reload) + frontend (vite dev)
make build            # Build frontend + embed in Go binary
make build-frontend   # npm run build in web/
make build-backend    # go build with embedded frontend (go:embed)
make test             # go test ./... -race
make lint             # golangci-lint run
make docker           # docker build multi-stage
make release          # Cross-compile: linux/mac/windows × amd64/arm64
make proto            # Generate gRPC code from .proto files
make migrate-up       # Run database migrations
make migrate-create   # Create new migration file
```

---

## Docker Build

Plug-and-play: the image includes **everything** needed to run HubPlay. No external dependencies, no manual FFmpeg install, no additional setup. `docker compose up` and done.

```dockerfile
# Multi-stage: frontend → backend → runtime
# ============================================

# Stage 1: Build React frontend
FROM node:20-alpine AS frontend
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# Stage 2: Build Go backend (embeds frontend)
FROM golang:1.22-alpine AS backend
RUN apk add --no-cache gcc musl-dev   # CGo needed for SQLite
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /web/dist ./web/dist
RUN CGO_ENABLED=1 go build -tags embed -ldflags "-s -w" -o hubplay ./cmd/hubplay

# Stage 3: Runtime — everything included
FROM debian:bookworm-slim AS runtime

# FFmpeg (full build: all codecs, HW accel support, subtitle filters)
RUN apt-get update && apt-get install -y --no-install-recommends \
    ffmpeg \
    # HW acceleration support
    intel-media-va-driver-non-free \
    mesa-va-drivers \
    vainfo \
    # Subtitle rendering (libass for ASS/SSA burn-in)
    libass9 \
    # Font rendering for subtitle burn-in
    fonts-liberation \
    fonts-noto-core \
    fonts-noto-cjk \
    # TLS for outbound HTTPS (TMDb, federation, plugins)
    ca-certificates \
    # Timezone data
    tzdata \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user
RUN groupadd -r hubplay && useradd -r -g hubplay -d /config -s /sbin/nologin hubplay

# Copy binary
COPY --from=backend /app/hubplay /usr/local/bin/hubplay

# Default directories
RUN mkdir -p /config /cache /tmp/hubplay && \
    chown -R hubplay:hubplay /config /cache /tmp/hubplay

USER hubplay
WORKDIR /config

EXPOSE 8096

# Health check built into Docker
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD hubplay health || exit 1

ENTRYPOINT ["hubplay"]
CMD ["--config", "/config/hubplay.yaml"]
```

### What's Included in the Image

| Component | Why | Size Impact |
|-----------|-----|-------------|
| FFmpeg (full) | Transcoding, remuxing, media probing, thumbnail extraction | ~80MB |
| VA-API drivers (Intel/AMD) | Hardware transcoding out of the box | ~15MB |
| libass + fonts | Subtitle burn-in (ASS/SSA/PGS) with correct rendering | ~20MB |
| CA certificates | HTTPS to TMDb, federation peers, plugin repos | ~1MB |
| Timezone data | Correct scheduling (scanner cron, EPG times) | ~2MB |
| HubPlay binary (with embedded frontend) | The app | ~25MB |
| **Total image** | | **~180MB** |

NVIDIA users: use `hubplay/hubplay:latest-nvidia` variant (adds CUDA + NVENC runtime libs, ~350MB).

The final binary includes the React frontend via `go:embed`. Zero runtime dependencies beyond what the Docker image provides.

---

## Database Decisions

### SQLite Configuration
- **WAL mode** (Write-Ahead Logging) — allows concurrent reads during writes
- **Single writer** — SQLite limitation, managed via connection pool with max 1 write connection
- **FTS5** for full-text search — built into SQLite, no extra dependencies
- **Migrations** via goose — raw SQL files in `migrations/sqlite/`

### PostgreSQL (Optional)
- For deployments with many concurrent users (10+)
- Same migration tool (goose) with separate migration files in `migrations/postgres/`
- Switched via config: `database.driver: postgres`
- Interface abstraction: all repositories use `database/sql` interfaces, driver-agnostic

---

## Risks & Mitigations

| Risk | Impact | Mitigation |
|------|--------|-----------|
| FFmpeg not installed | Can't transcode or analyze media | Detect at startup, clear error message. Docker image includes it |
| SQLite concurrent writes | Write contention under heavy load | WAL mode + single writer goroutine. Recommend PostgreSQL for 10+ users |
| Plugin process crashes | Feature provided by plugin unavailable | Process isolation, health checks, auto-restart with backoff (1s, 2s, 4s, max 30s) |
| TMDb API key invalid/expired | No metadata for new items | Graceful degradation: items added without metadata, retry queue |
| Corrupt/unreadable media files | Scanner fails on single file | Log error per file, continue scanning remaining files |
| Very large libraries (100K+ items) | Slow scans, memory pressure | Incremental scanning via fingerprint, server-side pagination, streaming DB queries |
| modernc.org/sqlite performance | Slightly slower than CGO version | Acceptable trade-off for build simplicity. Benchmarks show <10% difference for typical queries |
