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
FROM golang:1.22-bookworm AS backend

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Inject built frontend into web/dist for go:embed
COPY --from=frontend /web/dist ./web/dist
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /hubplay ./cmd/hubplay

# ═══════════════════════════════════════════
# Stage 3: Runtime (Ubuntu for FFmpeg + HW accel)
# ═══════════════════════════════════════════
FROM ubuntu:24.04

RUN apt-get update && apt-get install -y --no-install-recommends \
    ffmpeg \
    ca-certificates \
    tzdata \
    # Intel VAAPI (QSV)
    intel-media-va-driver-non-free \
    vainfo \
    && rm -rf /var/lib/apt/lists/*

RUN useradd -r -s /bin/false hubplay

COPY --from=backend /hubplay /usr/local/bin/hubplay

RUN mkdir -p /config /cache && chown hubplay:hubplay /config /cache

USER hubplay

EXPOSE 8096

VOLUME ["/config", "/cache"]

ENTRYPOINT ["hubplay"]
CMD ["--config", "/config/hubplay.yaml"]
