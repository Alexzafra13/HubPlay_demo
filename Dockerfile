# ═══════════════════════════════════════════
# Stage 1: Build frontend
# ═══════════════════════════════════════════
# Bases pineadas por digest (tag + @sha256): el tag documenta la
# intención, el digest hace el build reproducible y a prueba de
# re-publicaciones del tag. Dependabot (ecosistema docker) los bumpea.
# --platform=$BUILDPLATFORM: los stages de BUILD corren nativos en la
# arquitectura del builder y cross-compilan al destino — sin esto, el
# build arm64 del CI corría ENTERO bajo emulación QEMU (Go + Node
# emulados ≈ 15-20 min por run). Solo los stages de runtime (apk/apt
# install, segundos) se ejecutan en la arquitectura destino.
FROM --platform=$BUILDPLATFORM node:22-alpine@sha256:968df39aedcea65eeb078fb336ed7191baf48f972b4479711397108be0966920 AS frontend

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
FROM --platform=$BUILDPLATFORM golang:1.26.4-alpine@sha256:f23e8b227fb4493eabe03bede4d5a32d04092da71962f1fb79b5f7d1e6c2a17f AS backend
# GOTOOLCHAIN=auto lets Go fetch the exact toolchain go.mod requires
# if a future bump outpaces this base image. Plug-and-play for prod.
ENV GOTOOLCHAIN=auto

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

# TARGETOS/TARGETARCH los inyecta buildx según --platform del build.
# CGO_ENABLED=0 hace la cross-compilación trivial (binario estático).
ARG TARGETOS TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
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
FROM ubuntu:24.04@sha256:786a8b558f7be160c6c8c4a54f9a57274f3b4fb1491cf65146521ae77ff1dc54 AS hwaccel

RUN apt-get update && apt-get install -y --no-install-recommends \
    ffmpeg \
    libchromaprint-tools \
    ca-certificates \
    tzdata \
    tini \
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
# tini como PID 1: reapea los ffmpeg/ffprobe huérfanos (un server de
# transcoding spawnea muchos) y reenvía SIGTERM para el shutdown graceful
# de 30s. Sin él los zombies se acumulan en un box de larga vida.
ENTRYPOINT ["tini", "--", "hubplay"]
CMD ["--config", "/config/hubplay.yaml"]

# ═══════════════════════════════════════════
# Stage 4 (default): Runtime — lightweight, software transcoding
#
#   docker build -t hubplay .
# ═══════════════════════════════════════════
FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d

RUN apk add --no-cache \
    ffmpeg \
    chromaprint \
    ca-certificates \
    tzdata \
    tini

RUN adduser -D -s /sbin/nologin hubplay

COPY --from=backend /hubplay /usr/local/bin/hubplay
RUN mkdir -p /config /cache && chown hubplay:hubplay /config /cache

ENV HUBPLAY_STREAMING_CACHE_DIR=/cache

USER hubplay
EXPOSE 8096

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -qO /dev/null http://localhost:8096/api/v1/health || exit 1

VOLUME ["/config", "/cache"]
# tini como PID 1: reapea los ffmpeg/ffprobe huérfanos (un server de
# transcoding spawnea muchos) y reenvía SIGTERM para el shutdown graceful
# de 30s. Sin él los zombies se acumulan en un box de larga vida.
ENTRYPOINT ["tini", "--", "hubplay"]
CMD ["--config", "/config/hubplay.yaml"]
