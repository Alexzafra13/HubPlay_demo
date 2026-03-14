# ═══════════════════════════════════════════
# Stage 1: Build frontend
# ═══════════════════════════════════════════
FROM node:22-bookworm-slim AS frontend

WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ .
RUN npm run build

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
# Stage 3: Runtime
# ═══════════════════════════════════════════
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ffmpeg \
    ca-certificates \
    tzdata \
    && rm -rf /var/lib/apt/lists/*

RUN useradd -r -s /bin/false hubplay

COPY --from=backend /hubplay /usr/local/bin/hubplay

RUN mkdir -p /config /cache && chown hubplay:hubplay /config /cache

USER hubplay

EXPOSE 8096

VOLUME ["/config", "/cache"]

ENTRYPOINT ["hubplay"]
CMD ["--config", "/config/hubplay.yaml"]
