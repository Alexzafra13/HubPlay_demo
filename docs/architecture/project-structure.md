# Project Structure — Directory Map

Quick reference for navigating the HubPlay codebase. One sentence per directory.

---

## Root

```
hubplay/
├── cmd/                        # Application entry points
│   └── hubplay/
│       └── main.go             # THE entry point: wiring, DI, startup, shutdown
├── internal/                   # All application code (not importable externally)
├── proto/                      # gRPC .proto definitions for plugin system
├── migrations/                 # SQL migration files (goose)
│   ├── sqlite/                 # SQLite-specific migrations
│   └── postgres/               # PostgreSQL-specific migrations
├── web/                        # React frontend (embedded in binary via go:embed)
├── docs/                       # Architecture documentation (you are here)
│   └── architecture/
├── testdata/                   # Test fixtures: sample configs, media stubs
├── hubplay.example.yaml        # Example config — copy and customize
├── Dockerfile                  # Multi-stage build (frontend → backend → runtime)
├── docker-compose.yml          # Production-ready plug-and-play compose
├── Makefile                    # Dev commands: make dev, make build, make test
├── go.mod / go.sum             # Go module dependencies
└── .goreleaser.yml             # Cross-platform release config
```

---

## `internal/` — Backend Code

Every package inside `internal/` follows the same pattern:
- **Domain types** (structs, interfaces) at the top of the package
- **Service implementation** in the main file
- **Dependencies injected via constructor** (`NewXxx(deps...) *Xxx`)
- **Interfaces defined where consumed**, not where implemented

```
internal/
├── api/                        # HTTP layer (chi router)
│   ├── router.go               # Route registration + middleware stack
│   ├── deps.go                 # Dependencies struct (all services aggregated)
│   ├── middleware.go            # Auth, logging, CORS, rate limiting, timeout
│   ├── handlers/               # One file per resource
│   │   ├── auth.go             # POST /auth/login, /auth/refresh, /auth/quickconnect
│   │   ├── library.go          # CRUD /libraries, POST /libraries/{id}/scan
│   │   ├── items.go            # GET /items, /items/{id} (browse, detail)
│   │   ├── stream.go           # GET /stream/{id}/master.m3u8, /stream/{id}/direct
│   │   ├── iptv.go             # GET /channels, /channels/{id}/stream, /channels/epg
│   │   ├── user.go             # CRUD /users (admin), GET /me (self)
│   │   ├── progress.go         # PUT /me/progress/{itemId}, GET /me/continue-watching
│   │   ├── search.go           # GET /search?q=
│   │   ├── plugin.go           # CRUD /plugins
│   │   └── system.go           # GET /health, GET /system/info
│   └── ws/
│       └── hub.go              # WebSocket hub — broadcasts events to connected clients
│
├── auth/                       # Authentication & authorization
│   ├── service.go              # Login, register, token generation, refresh
│   ├── jwt.go                  # JWT signing, validation, claims
│   ├── quickconnect.go         # PIN-based TV/device pairing
│   └── middleware.go           # Extract + validate JWT from request header
│
├── user/                       # User management
│   ├── service.go              # CRUD users, admin operations
│   ├── preferences.go          # Per-user settings (language, quality, theme)
│   └── session.go              # Session tracking (devices, last active, revocation)
│
├── library/                    # Library & scanner
│   ├── library.go              # Library domain type + CRUD service
│   ├── scanner.go              # Filesystem scan pipeline (walk → resolve → analyze → metadata)
│   ├── watcher.go              # Real-time file watcher (fsnotify + debounce)
│   └── resolver/               # Filename → structured media info
│       ├── resolver.go         # Resolver interface + chain orchestration
│       ├── movie.go            # "Title (Year)/Title (Year).mkv" pattern
│       ├── tv.go               # "Show/Season XX/Show - SxxExx.mkv" pattern
│       └── multipart.go        # cd1/cd2, part1/part2 grouping
│
├── metadata/                   # Metadata orchestration
│   ├── manager.go              # MetadataManager — runs provider chain, merges results
│   ├── provider.go             # Provider interfaces (Local, Remote, Image)
│   └── providers/
│       ├── embedded.go         # Read tags from video file (FFprobe metadata)
│       ├── tmdb.go             # TMDb API client (movies, TV, search)
│       └── fanart.go           # Fanart.tv client (logos, clearart, banners)
│
├── media/                      # Media domain types + analysis
│   ├── item.go                 # MediaItem struct (core entity)
│   ├── stream.go               # MediaStream struct (video/audio/subtitle track info)
│   └── analyzer.go             # FFprobe wrapper — extract codec info from files
│
├── streaming/                  # Playback delivery
│   ├── manager.go              # TranscodingManager — session lifecycle, resource limits
│   ├── session.go              # TranscodeSession — FFmpeg process per stream
│   ├── decision.go             # Playback waterfall: Direct Play → Remux → Transcode
│   ├── profiles.go             # Built-in client profiles (Chrome, Safari, TV, etc.)
│   ├── hls.go                  # HLS master/variant playlist generation + segment serving
│   ├── direct.go               # Direct play (serve file as-is) + progressive download
│   └── subtitle.go             # Subtitle extraction, VTT conversion, burn-in detection
│
├── ffmpeg/                     # FFmpeg integration
│   ├── builder.go              # Construct FFmpeg command lines from PlaybackDecision
│   ├── hwaccel.go              # Hardware acceleration detection + encoder selection
│   ├── process.go              # FFmpeg process wrapper (start, monitor, kill)
│   └── probe.go                # FFprobe wrapper with concurrency limiter (semaphore)
│
├── trickplay/                  # Timeline preview thumbnails
│   ├── generator.go            # Background job — extract frames, build sprite sheets
│   └── sprites.go              # Sprite sheet assembly + WebVTT timestamp mapping
│
├── iptv/                       # Live TV / IPTV
│   ├── manager.go              # ChannelManager — playlists, EPG, health, proxying
│   ├── m3u.go                  # M3U/M3U8 playlist parser (streaming, handles 10K+ channels)
│   ├── epg.go                  # XMLTV parser + EPGManager (schedule, now/next, search)
│   ├── proxy.go                # Stream proxy with fan-out, reconnection, stats
│   ├── health.go               # Channel health checker (periodic ping)
│   ├── channel.go              # Channel domain type
│   └── mapping.go              # EPG ↔ channel matching (auto + manual)
│
├── progress/                   # Watch progress & engagement
│   ├── service.go              # Track position, play count, completed status
│   ├── nextup.go               # "Next Up" algorithm (next unwatched episode)
│   └── favorites.go            # User favorites (movies, series, channels)
│
├── plugin/                     # Plugin system
│   ├── manager.go              # Plugin lifecycle (discover, start, stop, health)
│   ├── loader.go               # Load plugin binary, validate manifest, start process
│   ├── registry.go             # Extension point registry (metadata, auth, notification, etc.)
│   └── manifest.go             # Plugin manifest parsing (hubplay-plugin.yaml)
│
├── federation/                 # Server-to-server federation
│   ├── manager.go              # FederationManager — peer lifecycle, permissions
│   ├── peer.go                 # Peer domain type + status tracking
│   ├── invite.go               # Invite code generation + exchange
│   ├── catalog.go              # Remote catalog caching + sync
│   ├── proxy.go                # Transparent streaming proxy to peer servers
│   ├── download.go             # Download content from federated peers
│   ├── crypto.go               # Ed25519 key generation, JWT signing/verification
│   └── client.go               # HTTP client for peer-to-peer API calls
│
├── webhook/                    # Webhook automation
│   ├── dispatcher.go           # Listen to events, render templates, fire HTTP requests
│   ├── config.go               # Webhook configuration types
│   └── template.go             # Go template rendering for webhook payloads
│
├── event/
│   └── bus.go                  # In-process pub/sub event bus (goroutine-safe)
│
├── db/                         # Data access layer
│   ├── sqlite.go               # SQLite connection setup (WAL, pragmas, pool)
│   ├── postgres.go             # PostgreSQL connection setup
│   ├── item_repo.go            # ItemRepository (CRUD, search, bulk ops)
│   ├── metadata_repo.go        # MetadataRepository (upsert, people, images)
│   ├── library_repo.go         # LibraryRepository
│   ├── channel_repo.go         # ChannelRepository + EPG queries
│   ├── user_repo.go            # UserRepository
│   ├── session_repo.go         # SessionRepository (auth sessions)
│   ├── progress_repo.go        # ProgressRepository (user_data table)
│   ├── favorite_repo.go        # FavoriteRepository
│   └── sqlc/                   # Auto-generated code from sqlc (DO NOT edit manually)
│       ├── queries.sql.go
│       ├── models.go
│       └── db.go
│
└── config/
    └── config.go               # YAML loading, env var expansion, validation
```

---

## `web/` — React Frontend

```
web/
├── public/                     # Static assets (favicon, manifest)
├── src/
│   ├── components/
│   │   ├── layout/             # Sidebar, TopBar, MiniPlayer (persistent UI)
│   │   ├── player/             # VideoPlayer, Controls, Trickplay, Subtitles
│   │   ├── epg/                # EPG grid (Planby), ChannelCard, NowNextBar
│   │   ├── media/              # PosterCard, EpisodeCard, HeroSection, MediaGrid
│   │   ├── common/             # BlurhashImage, ProgressBar, SkeletonLoader, SearchBar
│   │   └── admin/              # LibraryManager, UserManager, ActivityLog
│   ├── pages/                  # One file per route (lazy-loaded)
│   │   ├── Home.tsx
│   │   ├── Movies.tsx / MovieDetail.tsx
│   │   ├── Series.tsx / SeriesDetail.tsx
│   │   ├── LiveTV.tsx / EPGGuide.tsx
│   │   ├── Search.tsx / Favorites.tsx
│   │   ├── Settings.tsx / Admin.tsx
│   │   ├── Login.tsx / Setup.tsx / QuickConnect.tsx
│   │   └── ...
│   ├── hooks/                  # Custom React hooks
│   │   ├── usePlayer.ts        # Player state + controls
│   │   ├── useEPG.ts           # EPG data fetching + cache
│   │   ├── useProgress.ts      # Watch progress sync (save every 10s)
│   │   └── useKeyboard.ts      # Keyboard shortcuts + spatial navigation
│   ├── api/
│   │   └── client.ts           # TanStack Query client, fetch wrapper, auth interceptor
│   ├── store/
│   │   └── player.ts           # Zustand store (player state, auth state, UI prefs)
│   ├── i18n/                   # Translations (ES/EN)
│   │   ├── es.json
│   │   └── en.json
│   ├── styles/
│   │   └── globals.css         # Tailwind v4 + CSS custom properties (theme)
│   └── App.tsx                 # Root component, router, providers
├── package.json                # Dependencies + packageManager: pnpm
├── pnpm-lock.yaml              # Lockfile (deterministic)
├── tsconfig.json               # TypeScript config
├── vite.config.ts              # Vite build config
├── vitest.config.ts            # Unit test config
├── playwright.config.ts        # E2E test config
└── index.html                  # SPA entry point
```

---

## `migrations/` — Database Migrations

```
migrations/
├── sqlite/
│   ├── 001_initial_schema.sql       # Users, libraries, items, metadata, images
│   ├── 002_fts_search.sql           # FTS5 virtual table + sync triggers
│   ├── 003_iptv_channels.sql        # Channels + EPG tables
│   ├── 004_federation.sql           # Peers, invites, cached catalogs
│   ├── 005_plugins_webhooks.sql     # Plugin state, webhook configs/logs
│   └── ...
└── postgres/
    ├── 001_initial_schema.sql       # Same schema, PostgreSQL syntax
    ├── 002_fts_search.sql           # Uses tsvector instead of FTS5
    └── ...
```

Each migration has `-- +goose Up` and `-- +goose Down` sections. Migrations run automatically on startup.

---

## `proto/` — gRPC Plugin Interfaces

```
proto/
├── metadata.proto              # MetadataProvider service (search, fetch)
├── auth.proto                  # AuthProvider service (LDAP, SSO extensions)
├── notification.proto          # NotificationProvider service (push, email plugins)
└── health.proto                # Plugin health check service
```

Generated Go code goes to `internal/plugin/gen/` (via `protoc`).

---

## Key Conventions

| Convention | Rule |
|---|---|
| **Package naming** | Short, lowercase, no underscores: `iptv`, `ffmpeg`, `auth` |
| **File naming** | `snake_case.go` for Go, `PascalCase.tsx` for React components |
| **Interface location** | Defined where consumed, not where implemented |
| **Constructor pattern** | `NewXxx(deps...) *Xxx` — explicit dependency injection |
| **Error handling** | Wrap with `fmt.Errorf("doing X: %w", err)` — see [error-handling.md](error-handling.md) |
| **Testing** | `*_test.go` next to source, `testdata/` for fixtures |
| **Generated code** | `internal/db/sqlc/` (sqlc) and `internal/plugin/gen/` (protoc) — never edit manually |
| **Config** | YAML → struct in `internal/config/`, env vars override with `HUBPLAY_` prefix |
| **Embedded frontend** | `web/dist/` embedded via `go:embed` with build tag `-tags embed` |
