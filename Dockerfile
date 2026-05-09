# ═══════════════════════════════════════════
# Stage 1: Build frontend
# ═══════════════════════════════════════════
FROM node:22-alpine AS frontend

RUN corepack enable && corepack prepare pnpm@10 --activate

WORKDIR /web
COPY web/package.json web/pnpm-lock.yaml ./
RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile
COPY web/ .
RUN pnpm run build

# ═══════════════════════════════════════════
# Stage 2: Build backend
# ═══════════════════════════════════════════
FROM golang:1.25-alpine AS backend

ARG VERSION=dev

WORKDIR /src

# GOPROXY fallback chain: try the official proxy first, then a public
# mirror, finally fall back to direct VCS. Buys us resilience against
# transient `proxy.golang.org` outages (GOAWAY frames during their
# rolling restarts) on multi-arch builds where the QEMU emulation
# slowdown widens the window. Each entry is tried in order; `direct`
# at the end means "skip the proxy and clone the module's repo
# straight" so a hard outage doesn't gate the build.
ENV GOPROXY=https://proxy.golang.org,https://goproxy.cn,direct
ENV GOSUMDB=sum.golang.org

COPY go.mod go.sum ./
# Up to 3 attempts with linear backoff (5s, 10s) so a single GOAWAY
# on a heavy module (modernc.org/libc weighs ~50 MB) doesn't fail
# the whole stage. The `until/exit 1` shape — instead of a for-loop
# with `break` — propagates the final attempt's exit code so a
# legitimate auth/checksum failure still fails the build instead of
# being silently swallowed by a `done && ...` chain.
RUN --mount=type=cache,target=/go/pkg/mod \
    set -e; \
    n=0; \
    until go mod download; do \
        n=$((n+1)); \
        if [ "$n" -ge 3 ]; then \
            echo "go mod download failed after $n attempts"; exit 1; \
        fi; \
        echo "go mod download attempt $n failed; retrying in $((n * 5))s..."; \
        sleep $((n * 5)); \
    done; \
    go mod verify

# Copy only what the Go build needs
COPY cmd/ cmd/
COPY internal/ internal/
COPY migrations/ migrations/
COPY migrations.go ./

# Inject built frontend for go:embed
COPY --from=frontend /web/dist ./web/dist

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /hubplay ./cmd/hubplay

# ═══════════════════════════════════════════
# Stage 3: Runtime — VAAPI + QSV hardware transcoding
#
#   docker build --target hwaccel -t hubplay:hwaccel .
#
# Runtime:
#   docker run --device /dev/dri:/dev/dri hubplay:hwaccel
# ═══════════════════════════════════════════
FROM ubuntu:24.04 AS hwaccel

RUN apt-get update && apt-get install -y --no-install-recommends \
    ffmpeg \
    libchromaprint-tools \
    ca-certificates \
    tzdata \
    wget \
    # VAAPI (Intel/AMD)
    libva2 \
    libva-drm2 \
    va-driver-all \
    mesa-va-drivers \
    # Intel QSV
    intel-media-va-driver-non-free \
    libmfx1 \
    # OpenCL (HDR tone-mapping)
    ocl-icd-libopencl1 \
    intel-opencl-icd \
    && rm -rf /var/lib/apt/lists/*

RUN groupadd -f render && \
    useradd -r -s /sbin/nologin -G video,render hubplay

COPY --from=backend /hubplay /usr/local/bin/hubplay
RUN mkdir -p /config /cache && chown hubplay:hubplay /config /cache

ENV HUBPLAY_STREAMING_CACHE_DIR=/cache

USER hubplay
EXPOSE 8096

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -qO /dev/null http://localhost:8096/api/v1/health || exit 1

VOLUME ["/config", "/cache"]
ENTRYPOINT ["hubplay"]
CMD ["--config", "/config/hubplay.yaml"]

# ═══════════════════════════════════════════
# Stage 4 (default): Runtime — lightweight, software transcoding
#
#   docker build -t hubplay .
# ═══════════════════════════════════════════
FROM alpine:3.21

RUN apk add --no-cache \
    ffmpeg \
    chromaprint \
    ca-certificates \
    tzdata

RUN adduser -D -s /sbin/nologin hubplay

COPY --from=backend /hubplay /usr/local/bin/hubplay
RUN mkdir -p /config /cache && chown hubplay:hubplay /config /cache

ENV HUBPLAY_STREAMING_CACHE_DIR=/cache

USER hubplay
EXPOSE 8096

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -qO /dev/null http://localhost:8096/api/v1/health || exit 1

VOLUME ["/config", "/cache"]
ENTRYPOINT ["hubplay"]
CMD ["--config", "/config/hubplay.yaml"]
