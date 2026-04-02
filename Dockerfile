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
FROM golang:1.24-alpine AS backend

ARG VERSION=dev

WORKDIR /src

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
