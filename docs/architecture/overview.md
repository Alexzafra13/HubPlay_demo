# HubPlay — Architecture Overview

## What Is HubPlay

A self-hosted media server for movies, TV shows, and live TV (IPTV). Written in Go with a React web client. Open source, no paywalls, no phone-home. One binary, one config file, done.

---

## Technology Stack

| Component | Technology | Why |
|-----------|-----------|-----|
| Backend | Go | Single binary, fast, great concurrency, cross-platform |
| Web Frontend | React + TypeScript | Large ecosystem, hls.js for video, community contributors |
| Database | SQLite (default) | Zero config, single file, FTS5 for search |
| Database (optional) | PostgreSQL | For large deployments with many concurrent users |
| Transcoding | FFmpeg | Industry standard, hardware acceleration support |
| Media Analysis | FFprobe | Part of FFmpeg, extracts codec/stream info |
| API | REST + JSON | Universal, any client can consume it |
| Real-time | WebSocket | Push events to clients (scan progress, new items) |
| Plugins | gRPC | Language-agnostic, fast, strongly typed |
| Deployment | Docker + native binary | Docker for self-hosters, binary for direct install |

---

## High-Level Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                        Clients                                │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐    │
│  │ Web      │  │ Mobile   │  │ TV       │  │ 3rd party│    │
│  │ (React)  │  │ (future) │  │ (future) │  │ API      │    │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └────┬─────┘    │
└───────┼──────────────┼──────────────┼──────────────┼─────────┘
        │              │              │              │
        ▼              ▼              ▼              ▼
┌──────────────────────────────────────────────────────────────┐
│                     REST API + WebSocket                       │
│                     (Go HTTP server)                           │
├──────────┬───────────┬───────────┬───────────┬───────────────┤
│  Auth    │ Library   │ Streaming │ IPTV      │ Plugin        │
│  & Users │ & Media   │ & Trans-  │ & Live TV │ Manager       │
│          │ Manager   │ coding    │           │               │
├──────────┴───────────┴───────────┴───────────┴───────────────┤
│                      Event Bus (in-process)                    │
├──────────────────────────────────────────────────────────────┤
│                      Repository Layer                          │
├──────────────────────────────────────────────────────────────┤
│               SQLite / PostgreSQL      FFmpeg / FFprobe        │
└──────────────────────────────────────────────────────────────┘
        │                                        │
        ▼                                        ▼
   ┌──────────┐                           ┌──────────────┐
   │ Database  │                           │ Media Files  │
   │ file      │                           │ on disk      │
   └──────────┘                           └──────────────┘
```

---

## Module Summary

### 1. Library & Media Management
- Scans filesystem for movies/series using resolver chain
- Fetches metadata from TMDb + images from Fanart.tv
- File watcher for real-time updates (fsnotify + debounce)
- [Full design →](media-management.md)

### 2. Streaming & Transcoding
- Direct Play → Direct Stream → Transcode decision waterfall
- HLS adaptive bitrate for remote playback
- FFmpeg with hardware acceleration (VAAPI, QSV, NVENC, VideoToolbox)
- Trickplay preview thumbnails
- [Full design →](streaming.md)

### 3. IPTV / Live TV
- M3U playlist parsing with channel metadata
- XMLTV EPG (electronic program guide)
- Stream proxying with auto-reconnection
- Channels organized by groups/categories
- [Full design →](media-management.md#10-iptv--live-tv-module)

### 4. Users & Authentication
- Local auth with bcrypt + JWT tokens
- Multiuser with admin/user roles
- QuickConnect PIN pairing for TVs
- Watch progress, continue watching, next up
- Favorites and per-user preferences
- Plugin auth (LDAP, SSO) via gRPC
- [Full design →](users-auth.md)

### 5. Plugin System
- Plugins as external processes (any language)
- Communication via gRPC over Unix sockets
- Extension points: metadata, auth, notifications, subtitles, resolvers
- Webhooks for lightweight automation
- [Full design →](plugins.md)

### 6. Federation (Server-to-Server)
- P2P linking between HubPlay instances via invite codes
- Browse, stream, and download content from federated servers
- Ed25519 key exchange for mutual authentication
- Admin controls per peer: shared libraries, streaming, download permissions
- Cached remote catalogs for offline browsing
- No central service — fully decentralized
- [Full design →](federation.md)

---

## Project Structure

```
hubplay/
├── cmd/
│   └── hubplay/
│       └── main.go              # Entry point: wire everything, start server
├── internal/
│   ├── api/
│   │   ├── router.go            # Route registration
│   │   ├── middleware.go         # Auth, logging, CORS, rate limiting
│   │   ├── handlers/
│   │   │   ├── library.go       # Library CRUD endpoints
│   │   │   ├── items.go         # Media item endpoints
│   │   │   ├── stream.go        # Streaming endpoints (HLS, direct play)
│   │   │   ├── iptv.go          # IPTV/channel endpoints
│   │   │   ├── auth.go          # Login, register, quickconnect
│   │   │   ├── user.go          # User management endpoints
│   │   │   ├── progress.go      # Watch progress endpoints
│   │   │   ├── search.go        # Search endpoint
│   │   │   ├── plugin.go        # Plugin management endpoints
│   │   │   └── system.go        # Server info, settings, health
│   │   └── ws/
│   │       └── hub.go           # WebSocket hub for real-time events
│   ├── auth/
│   │   ├── service.go
│   │   ├── jwt.go
│   │   ├── quickconnect.go
│   │   └── middleware.go
│   ├── user/
│   │   ├── service.go
│   │   ├── preferences.go
│   │   └── session.go
│   ├── library/
│   │   ├── library.go
│   │   ├── scanner.go
│   │   ├── watcher.go
│   │   └── resolver/
│   │       ├── resolver.go
│   │       ├── movie.go
│   │       ├── tv.go
│   │       └── multipart.go
│   ├── metadata/
│   │   ├── manager.go
│   │   ├── provider.go
│   │   └── providers/
│   │       ├── embedded.go
│   │       ├── tmdb.go
│   │       └── fanart.go
│   ├── media/
│   │   ├── item.go
│   │   ├── stream.go
│   │   └── analyzer.go
│   ├── streaming/
│   │   ├── manager.go
│   │   ├── session.go
│   │   ├── decision.go
│   │   ├── profiles.go
│   │   ├── hls.go
│   │   ├── direct.go
│   │   └── subtitle.go
│   ├── ffmpeg/
│   │   ├── builder.go
│   │   ├── hwaccel.go
│   │   ├── process.go
│   │   └── probe.go
│   ├── trickplay/
│   │   ├── generator.go
│   │   └── sprites.go
│   ├── iptv/
│   │   ├── manager.go
│   │   ├── m3u.go
│   │   ├── epg.go
│   │   ├── proxy.go
│   │   └── channel.go
│   ├── progress/
│   │   ├── service.go
│   │   ├── nextup.go
│   │   └── favorites.go
│   ├── plugin/
│   │   ├── manager.go
│   │   ├── loader.go
│   │   ├── registry.go
│   │   └── manifest.go
│   ├── federation/
│   │   ├── manager.go
│   │   ├── peer.go
│   │   ├── invite.go
│   │   ├── catalog.go
│   │   ├── proxy.go
│   │   ├── download.go
│   │   ├── crypto.go
│   │   └── client.go
│   ├── webhook/
│   │   ├── dispatcher.go
│   │   ├── config.go
│   │   └── template.go
│   ├── event/
│   │   └── bus.go
│   ├── db/
│   │   ├── sqlite.go           # SQLite connection + migrations
│   │   ├── postgres.go         # PostgreSQL connection + migrations
│   │   ├── item_repo.go
│   │   ├── metadata_repo.go
│   │   ├── library_repo.go
│   │   ├── channel_repo.go
│   │   ├── user_repo.go
│   │   ├── session_repo.go
│   │   ├── progress_repo.go
│   │   └── favorite_repo.go
│   └── config/
│       └── config.go           # YAML config loading + validation
├── proto/
│   ├── metadata.proto
│   ├── auth.proto
│   ├── notification.proto
│   └── health.proto
├── web/                         # React frontend (embedded in binary)
│   ├── src/
│   ├── package.json
│   └── ...
├── migrations/
│   ├── sqlite/
│   └── postgres/
├── hubplay.example.yaml         # Example configuration
├── Dockerfile
├── docker-compose.yml
├── Makefile
├── go.mod
└── go.sum
```

---

## Deployment

### Docker (Recommended)
```yaml
# docker-compose.yml
services:
  hubplay:
    image: hubplay/hubplay:latest
    ports:
      - "8096:8096"
    volumes:
      - ./config:/config          # hubplay.yaml + database
      - /media/movies:/media/movies:ro
      - /media/tv:/media/tv:ro
      - ./cache:/cache            # Transcoding cache, thumbnails
    environment:
      - TMDB_API_KEY=your-key
      - FANART_API_KEY=your-key
    restart: unless-stopped
```

### Native Binary
```bash
# Download
curl -L https://github.com/hubplay/hubplay/releases/latest/download/hubplay-linux-amd64 -o hubplay
chmod +x hubplay

# Run
./hubplay --config /etc/hubplay/hubplay.yaml
```

### System Requirements
- **Minimum**: 1 CPU, 1GB RAM, any storage (for direct play only)
- **Recommended**: 4 CPU, 4GB RAM, SSD for database (for transcoding)
- **Dependencies**: FFmpeg installed on the system (Docker image includes it)
- **Supported OS**: Linux (primary), macOS, Windows

---

## API Design Principles

1. **REST with JSON** — standard, predictable, well-tooled
2. **Versioned** — all endpoints under `/api/v1/`
3. **Paginated** — list endpoints return `{items: [], total: N}` with `?offset=0&limit=20`
4. **Filtered** — query params for filtering: `?genre=Action&year=2024&sort=title`
5. **Consistent errors** — `{error: "message", code: "ITEM_NOT_FOUND"}`
6. **Auth via header** — `Authorization: Bearer <jwt>`

### Key Endpoint Groups
```
/api/v1/auth/*          → Authentication
/api/v1/me/*            → Current user (profile, progress, favorites)
/api/v1/users/*         → User management (admin)
/api/v1/libraries/*     → Library CRUD + scan triggers
/api/v1/items/*         → Media items (browse, search, details)
/api/v1/stream/*        → Streaming (HLS playlists, segments, direct)
/api/v1/channels/*      → IPTV channels + EPG
/api/v1/plugins/*       → Plugin management
/api/v1/federation/*    → Peer management, remote catalog, federated streaming
/api/v1/system/*        → Server info, settings, health
/api/v1/search          → Global search
```

Full endpoint documentation: [API Reference →](api-reference.md) | [Error Codes →](error-codes.md)

---

## Design Decisions Summary

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Language | Go | Single binary, fast, great stdlib for HTTP/concurrency |
| Architecture | Monolith with clean modules | Simple to deploy, modules can be separated later if needed |
| Database | SQLite default, PostgreSQL optional | Zero-config for most users, PG for large deployments |
| Search | SQLite FTS5 | Built-in, no extra dependencies, covers 95% of cases |
| Frontend | React + TypeScript | Ecosystem, hls.js, community |
| Streaming | HLS with FFmpeg | Industry standard, works on all devices |
| Plugins | gRPC external processes | Language-agnostic, crash isolation, secure |
| Federation | P2P with Ed25519 + JWT | No central service, fully decentralized, admin-controlled |
| Webhooks | HTTP POST with templates | Simple automation without writing plugins |
| Auth | Local bcrypt + JWT | Stateless, scalable, plugin-extensible |
| Config | YAML + env vars | Human-readable, 12-factor app compatible |
| Deployment | Docker plug-and-play + native binary | Docker image includes everything (FFmpeg, drivers, fonts). `docker compose up` and done |
| Security | Self-hosted: bcrypt, JWT, rate limit, no phone-home | [Full design →](security.md) |
| Media metadata | TMDb + Fanart.tv | Free, comprehensive, good API |
| Live TV | M3U/IPTV + XMLTV EPG | Standard formats, works with legal providers |

### Additional Documentation
- [API Reference](api-reference.md) — Full endpoint catalog with request/response examples
- [Error Codes](error-codes.md) — Standardized error codes, client handling, retry strategy
- [Security](security.md) — Threat model, auth, API security, TLS, plugin isolation
- [Deployment & Production](deployment-production.md) — Docker, systemd, reverse proxy, backups, NAS, monitoring
