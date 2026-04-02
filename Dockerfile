# ═══════════════════════════════════════════
# Stage 1: Build frontend
# ═══════════════════════════════════════════
FROM node:22-alpine AS frontend

RUN corepack enable && corepack prepare pnpm@9 --activate

WORKDIR /web
COPY web/package.json web/pnpm-lock.yaml ./
RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile
COPY web/ .
RUN pnpm run build

# ═══════════════════════════════════════════
# Stage 2: Build backend
# ═══════════════════════════════════════════
FROM golang:1.24-alpine AS backend

WORKDIR /src

# Download deps first (cached if go.mod/go.sum unchanged)
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy only what the Go build needs
COPY cmd/ cmd/
COPY internal/ internal/
COPY migrations/ migrations/
COPY migrations.go ./

# Inject built frontend for go:embed
COPY --from=frontend /web/dist ./web/dist

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /hubplay ./cmd/hubplay

# ═══════════════════════════════════════════
# Stage 3: Runtime (Alpine — lightweight)
# ═══════════════════════════════════════════
FROM alpine:3.21

RUN apk add --no-cache \
    ffmpeg \
    ca-certificates \
    tzdata

RUN adduser -D -s /sbin/nologin hubplay

COPY --from=backend /hubplay /usr/local/bin/hubplay

RUN mkdir -p /config /cache && chown hubplay:hubplay /config /cache

USER hubplay

EXPOSE 8096

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -qO /dev/null http://localhost:8096/api/v1/health || exit 1

VOLUME ["/config", "/cache"]

ENTRYPOINT ["hubplay"]
CMD ["--config", "/config/hubplay.yaml"]
