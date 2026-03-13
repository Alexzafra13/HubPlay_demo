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

| Library | Purpose | Why |
|---------|---------|-----|
| Vite | Build tool | Fast, instant HMR, modern defaults |
| TypeScript | Type safety | Catches bugs at compile time |
| React 18+ | UI framework | Ecosystem, community, mature video player libs |
| React Router | Navigation | SPA routing, standard for React |
| hls.js | HLS video player | Plays HLS streams in browsers that don't support it natively |
| TanStack Query | API data fetching | Automatic caching, refetch, loading states |
| Tailwind CSS | Styling | Rapid prototyping, consistent design, small bundle |

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

```dockerfile
# Multi-stage: frontend → backend → runtime
FROM node:20-alpine AS frontend
# npm ci && npm run build

FROM golang:1.22-alpine AS backend
# Copy frontend build, go build with -tags embed

FROM debian:bookworm-slim AS runtime
# Copy binary + install ffmpeg
# Minimal image: ~150MB with FFmpeg
```

The final binary includes the React frontend via `go:embed`. The Docker image only needs the binary + FFmpeg.

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
