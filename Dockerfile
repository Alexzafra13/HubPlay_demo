# ═══════════════════════════════════════════
# Stage 1: Build frontend
# ═══════════════════════════════════════════
FROM node:22-slim AS frontend

RUN corepack enable && corepack prepare pnpm@latest --activate

WORKDIR /web
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ .
RUN pnpm run build

# ═══════════════════════════════════════════
# Stage 2: Build backend
# ═══════════════════════════════════════════
FROM golang:1.24-bookworm AS backend

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Inject built frontend into web/dist for go:embed
COPY --from=frontend /web/dist ./web/dist
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /hubplay ./cmd/hubplay

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
