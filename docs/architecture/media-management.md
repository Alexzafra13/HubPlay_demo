# Media Management Module — Design Document

## Overview

The media management module handles: discovering media files, extracting metadata, organizing the library, IPTV live TV, and keeping everything in sync. It's the core of HubPlay — without it, there's nothing to stream.

### Supported Content Types
| Type | Source | Metadata Provider |
|------|--------|-------------------|
| Movies | Local files | TMDb + Fanart.tv |
| Series | Local files | TMDb + Fanart.tv |
| TV en Directo (IPTV) | M3U/M3U8 lists | EPG (XMLTV) + logos from list |

---

## 1. High-Level Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    Library Manager                        │
│  (Orchestrates scanning, metadata, and organization)     │
├──────────┬──────────────┬──────────────┬────────────────┤
│ Scanner  │  Resolver    │  Metadata    │  File Watcher  │
│ Pipeline │  Chain       │  Providers   │  (fsnotify)    │
├──────────┴──────────────┴──────────────┴────────────────┤
│                    Repository Layer                       │
│  (Items, Metadata, Images, Libraries)                    │
├─────────────────────────────────────────────────────────┤
│                    SQLite / PostgreSQL                    │
└─────────────────────────────────────────────────────────┘
```

---

## 2. Core Domain Types

### Library
```go
type Library struct {
    ID          uuid.UUID
    Name        string
    ContentType ContentType  // Movies, TVShows, LiveTV
    Paths       []string     // Physical directories (or M3U URL for LiveTV)
    ScanMode    ScanMode     // Auto, Manual, Scheduled
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type ContentType string
const (
    ContentMovies  ContentType = "movies"
    ContentTV      ContentType = "tvshows"
    ContentLiveTV  ContentType = "livetv"
)
```

### MediaItem (Base Entity)
```go
type MediaItem struct {
    ID            uuid.UUID
    LibraryID     uuid.UUID
    ParentID      *uuid.UUID     // nil for root items, set for episodes/tracks
    Type          ItemType       // Movie, Series, Season, Episode, Channel
    Title         string
    SortTitle     string
    OriginalTitle string
    Year          int
    Path          string         // Filesystem path
    Size          int64          // File size in bytes
    Duration      time.Duration  // Runtime
    AddedAt       time.Time
    UpdatedAt     time.Time

    // Media analysis results
    MediaStreams   []MediaStream   // Video, audio, subtitle streams
    Container     string          // mkv, mp4, avi, etc.
    Fingerprint   string          // Quick hash for change detection
}

type ItemType string
const (
    ItemMovie   ItemType = "movie"
    ItemSeries  ItemType = "series"
    ItemSeason  ItemType = "season"
    ItemEpisode ItemType = "episode"
    ItemChannel ItemType = "channel"  // IPTV live channel
)
```

### MediaStream (FFprobe result)
```go
type MediaStream struct {
    Index     int
    Type      StreamType  // Video, Audio, Subtitle
    Codec     string      // h264, hevc, aac, srt, etc.
    Profile   string      // Main, High, etc.
    BitRate   int64
    // Video-specific
    Width     int
    Height    int
    FrameRate float64
    HDRType   string      // SDR, HDR10, HDR10+, DolbyVision
    // Audio-specific
    Channels  int
    SampleRate int
    Language  string
    Title     string
    IsDefault bool
    IsForced  bool
}
```

### Metadata
```go
type Metadata struct {
    ItemID      uuid.UUID
    Overview    string
    Genres      []string
    Tags        []string
    Rating      float64
    ContentRating string    // PG-13, R, etc.
    Studio      string
    Premiered   *time.Time

    // External IDs
    ExternalIDs map[string]string  // "tmdb": "123", "imdb": "tt456", "tvdb": "789"

    // People
    People      []PersonRole       // Cast, crew, directors

    // Images (paths in cache dir, never in media dir)
    Poster      string
    Backdrop    string
    Thumb       string
    Logo        string
}

type PersonRole struct {
    PersonID uuid.UUID
    Name     string
    Role     string  // "Actor", "Director", "Writer"
    Character string // For actors: character name
    Order    int     // Sort order in credits
}
```

---

## 3. Scanner Pipeline

### Flow

```
Trigger (manual/scheduled/file-watcher)
    │
    ▼
┌─ Walk filesystem ──────────────────────────────────┐
│  For each file/directory:                          │
│  1. Apply ignore rules (hidden files, .hubplayignore) │
│  2. Check fingerprint vs DB (skip unchanged)       │
│  3. Run through Resolver Chain                     │
│  4. Queue new/changed items for metadata fetch     │
│  5. Mark missing items as unavailable              │
└────────────────────────────────────────────────────┘
    │
    ▼
Media Analysis (FFprobe)
    │
    ▼
Metadata Fetch (provider chain)
    │
    ▼
Emit events (item.added, item.updated, item.removed)
```

### Scanner Interface
```go
type Scanner interface {
    // Full scan of a library
    ScanLibrary(ctx context.Context, libraryID uuid.UUID) (*ScanResult, error)

    // Scan specific paths (triggered by file watcher)
    ScanPaths(ctx context.Context, libraryID uuid.UUID, paths []string) (*ScanResult, error)

    // Cancel a running scan
    CancelScan(ctx context.Context, libraryID uuid.UUID) error

    // Get scan progress
    Progress(libraryID uuid.UUID) *ScanProgress
}

type ScanResult struct {
    Added    int
    Updated  int
    Removed  int
    Errors   []ScanError
    Duration time.Duration
}

type ScanProgress struct {
    LibraryID   uuid.UUID
    Phase       string  // "walking", "analyzing", "metadata", "done"
    Total       int
    Processed   int
    CurrentFile string
    StartedAt   time.Time
}
```

### Resolver Chain
Inspired by Jellyfin's resolver pipeline. Each resolver handles a content type.

```go
type Resolver interface {
    // Priority determines execution order (lower = first)
    Priority() int

    // CanResolve checks if this resolver handles the given path
    CanResolve(ctx context.Context, path string, contentType ContentType) bool

    // Resolve extracts item info from the filesystem path
    Resolve(ctx context.Context, path string) (*ResolvedItem, error)
}

type ResolvedItem struct {
    Type          ItemType
    Title         string
    Year          int
    // TV-specific
    SeasonNumber  *int
    EpisodeNumber *int
    // Multi-file support
    Parts         []string  // For split movies/episodes
}
```

### Built-in Resolvers

| Resolver | Priority | Content Types | Logic |
|----------|----------|---------------|-------|
| `MovieResolver` | 10 | Movies | Parses `Title (Year)/Title (Year).ext` pattern |
| `TVResolver` | 10 | TVShows | Parses `Show/Season XX/Show - SxxExx - Title.ext` |
| `MultiPartResolver` | 3 | Movies, TV | Groups `cd1/cd2`, `part1/part2` files |

### File Naming Patterns (examples)

**Movies:**
```
/movies/Inception (2010)/Inception (2010).mkv
/movies/Inception (2010)/Inception (2010) - Behindthescenes.mkv  → Extra
/movies/The Matrix (1999)/The Matrix (1999) - cd1.mkv            → Multi-part
```

**TV Shows:**
```
/tv/Breaking Bad/Season 01/Breaking Bad - S01E01 - Pilot.mkv
/tv/Breaking Bad/Season 01/Breaking Bad - S01E01E02 - Multi.mkv  → Multi-episode
/tv/Breaking Bad/Specials/Breaking Bad - S00E01 - Special.mkv
```


---

## 4. Metadata Provider System

### Provider Chain (inspired by Jellyfin/Plex separation)

```go
type MetadataProvider interface {
    Name() string
    Priority() int
    Supports(itemType ItemType) bool
}

// Reads metadata from local files (NFO, embedded tags)
type LocalMetadataProvider interface {
    MetadataProvider
    FetchLocal(ctx context.Context, item *MediaItem) (*Metadata, error)
}

// Fetches metadata from online APIs
type RemoteMetadataProvider interface {
    MetadataProvider
    Search(ctx context.Context, query MetadataQuery) ([]SearchResult, error)
    Fetch(ctx context.Context, externalID string) (*Metadata, error)
}

// Fetches images
type ImageProvider interface {
    MetadataProvider
    FetchImages(ctx context.Context, item *MediaItem) ([]ImageResult, error)
}

type MetadataQuery struct {
    Title    string
    Year     int
    ItemType ItemType
    // TV-specific
    SeriesName    string
    SeasonNumber  int
    EpisodeNumber int
}
```

### Built-in Providers

| Provider | Type | Priority | Source |
|----------|------|----------|--------|
| `EmbeddedTagProvider` | Local | 1 | Embedded video/audio tags |
| `TMDbProvider` | Remote | 10 | TMDb API (movies, TV) |
| `FanartProvider` | Image | 5 | fanart.tv (logos, clearart, banners) |
| `TMDbImageProvider` | Image | 10 | TMDb images (posters, backdrops) |

### Metadata Refresh Modes
```go
type RefreshMode int
const (
    RefreshNone      RefreshMode = iota  // Skip
    RefreshLocal                          // Only local providers
    RefreshDefault                        // Local + remote for missing fields
    RefreshFull                           // Re-fetch everything
)
```

### Provider Orchestration
```go
type MetadataManager interface {
    // Refresh metadata for an item
    RefreshItem(ctx context.Context, itemID uuid.UUID, mode RefreshMode) error

    // Bulk refresh (e.g., after library scan)
    RefreshItems(ctx context.Context, itemIDs []uuid.UUID, mode RefreshMode) error

    // Search remote providers manually
    SearchRemote(ctx context.Context, query MetadataQuery) ([]SearchResult, error)

    // Apply a specific search result to an item
    ApplyMatch(ctx context.Context, itemID uuid.UUID, result SearchResult) error

    // Register a provider (for plugins)
    RegisterProvider(provider MetadataProvider)
}
```

### Metadata Merge Strategy
When multiple providers return data:
1. Local providers always take precedence (user's manual edits)
2. Remote providers fill gaps — first provider with data wins
3. Images: collect from all providers, user picks preferred
4. External IDs: merge from all providers (TMDb + IMDb + TVDb)

---

## 5. File Watcher (Real-time Updates)

```go
type FileWatcher interface {
    // Start watching library paths
    Watch(ctx context.Context, library Library) error

    // Stop watching
    Unwatch(libraryID uuid.UUID) error

    // Pause during full scans (Jellyfin pattern)
    Pause(libraryID uuid.UUID)
    Resume(libraryID uuid.UUID)
}
```

### Debounce Strategy
File operations often come in bursts (copying a movie = many write events). We debounce:
- Accumulate change events per directory for **5 seconds**
- After quiet period, trigger `ScanPaths()` for affected directories
- During a full scan, file watcher is paused to prevent duplicate work

### Implementation
Using `fsnotify` Go library with:
- Per-library goroutine for event processing
- Debounce timer per directory
- Recursive directory watching (fsnotify doesn't do this natively — we walk and add subdirs)

---

## 6. Media Analysis (FFprobe Integration)

```go
type MediaAnalyzer interface {
    // Analyze a media file and return stream information
    Analyze(ctx context.Context, path string) (*AnalysisResult, error)

    // Extract a thumbnail at a specific timestamp
    ExtractThumbnail(ctx context.Context, path string, timestamp time.Duration) (string, error)
}

type AnalysisResult struct {
    Streams   []MediaStream
    Container string
    Duration  time.Duration
    BitRate   int64
    Size      int64
    Chapters  []Chapter
}

type Chapter struct {
    Title string
    Start time.Duration
    End   time.Duration
}
```

### Concurrency Control
Like Jellyfin, limit concurrent FFprobe calls:
- Default: `runtime.NumCPU()` concurrent analyses
- Configurable via `HUBPLAY_PROBE_WORKERS` env var
- Use `semaphore.Weighted` from `golang.org/x/sync`

### Fingerprinting for Change Detection
Instead of re-analyzing every file on each scan:
- Compute quick fingerprint: `SHA256(path + modtime + size)`
- Compare with stored fingerprint in DB
- Only re-analyze if fingerprint changed

---

## 7. Repository Layer

```go
type ItemRepository interface {
    // CRUD
    Create(ctx context.Context, item *MediaItem) error
    Update(ctx context.Context, item *MediaItem) error
    Delete(ctx context.Context, id uuid.UUID) error
    GetByID(ctx context.Context, id uuid.UUID) (*MediaItem, error)

    // Queries
    GetByLibrary(ctx context.Context, libraryID uuid.UUID, opts ListOptions) ([]MediaItem, int, error)
    GetByParent(ctx context.Context, parentID uuid.UUID) ([]MediaItem, error)
    GetByPath(ctx context.Context, path string) (*MediaItem, error)

    // Search
    Search(ctx context.Context, query string, opts ListOptions) ([]MediaItem, int, error)

    // Bulk operations
    MarkUnavailable(ctx context.Context, libraryID uuid.UUID, activePaths []string) (int, error)
}

type MetadataRepository interface {
    Upsert(ctx context.Context, meta *Metadata) error
    GetByItemID(ctx context.Context, itemID uuid.UUID) (*Metadata, error)
    GetPeople(ctx context.Context, itemID uuid.UUID) ([]PersonRole, error)
}

type LibraryRepository interface {
    Create(ctx context.Context, lib *Library) error
    Update(ctx context.Context, lib *Library) error
    Delete(ctx context.Context, id uuid.UUID) error
    GetByID(ctx context.Context, id uuid.UUID) (*Library, error)
    GetAll(ctx context.Context) ([]Library, error)
}

type ListOptions struct {
    Offset  int
    Limit   int
    SortBy  string
    SortDir string
    Filters map[string]string  // genre=Action, year=2024, etc.
}
```

---

## 8. Event System

All mutations emit events for decoupling:

```go
type EventType string
const (
    EventItemAdded     EventType = "item.added"
    EventItemUpdated   EventType = "item.updated"
    EventItemRemoved   EventType = "item.removed"
    EventMetadataUpdated EventType = "metadata.updated"
    EventScanStarted   EventType = "scan.started"
    EventScanCompleted EventType = "scan.completed"
    EventLibraryCreated EventType = "library.created"
    EventLibraryDeleted EventType = "library.deleted"
)

type Event struct {
    Type      EventType
    ItemID    uuid.UUID
    LibraryID uuid.UUID
    Timestamp time.Time
    Data      map[string]any
}

type EventBus interface {
    Publish(ctx context.Context, event Event)
    Subscribe(eventType EventType, handler func(Event)) (unsubscribe func())
}
```

### Who Subscribes to What
- **Streaming module** → `item.added`, `item.removed` — update playable items cache
- **WebSocket notifier** → all events — push real-time updates to clients
- **Trickplay generator** → `item.added` — queue thumbnail generation
- **Search indexer** → `item.added`, `item.updated`, `metadata.updated` — update search index

---

## 9. Directory Structure

```
internal/
├── library/
│   ├── library.go          # Library domain type + service
│   ├── scanner.go          # Scanner pipeline implementation
│   ├── watcher.go          # File watcher (fsnotify)
│   └── resolver/
│       ├── resolver.go     # Resolver interface + chain
│       ├── movie.go        # Movie resolver
│       ├── tv.go           # TV show resolver
│       └── multipart.go    # Multi-part file grouping
├── metadata/
│   ├── manager.go          # MetadataManager orchestration
│   ├── provider.go         # Provider interfaces
│   └── providers/
│       ├── embedded.go     # Embedded video/audio tags
│       ├── tmdb.go         # TMDb API client
│       └── fanart.go       # Fanart.tv images (logos, clearart, banners)
├── media/
│   ├── item.go             # MediaItem domain type
│   ├── stream.go           # MediaStream type
│   └── analyzer.go         # FFprobe wrapper
├── iptv/
│   ├── manager.go          # IPTV channel manager + playlist refresh
│   ├── m3u.go              # M3U/M3U8 parser
│   ├── epg.go              # XMLTV EPG parser
│   ├── proxy.go            # Stream proxy with reconnection
│   └── channel.go          # Channel domain type
├── event/
│   └── bus.go              # In-process event bus
└── db/
    ├── item_repo.go        # ItemRepository (SQLite/PG)
    ├── metadata_repo.go    # MetadataRepository
    ├── library_repo.go     # LibraryRepository
    └── channel_repo.go     # ChannelRepository (IPTV)
```

---

## 10. IPTV / Live TV Module

### Overview
Native IPTV support allows users to watch live TV channels within HubPlay. The user provides a M3U playlist URL (from legal IPTV providers or public free-to-air channels) and optionally an EPG (Electronic Program Guide) URL for the TV schedule.

### Domain Types
```go
type Channel struct {
    ID          uuid.UUID
    LibraryID   uuid.UUID
    Name        string
    Number      int            // Channel number for sorting
    Group       string         // Category: "Sports", "News", "Entertainment", etc.
    LogoURL     string         // Channel logo from M3U metadata
    StreamURL   string         // Source stream URL
    Language    string
    Country     string
    IsActive    bool           // Whether the stream is currently reachable
    AddedAt     time.Time
}

type EPGProgram struct {
    ChannelID   uuid.UUID
    Title       string
    Description string
    Start       time.Time
    End         time.Time
    Category    string
    Icon        string         // Program thumbnail if available
}
```

### M3U Parser
Parses standard M3U/M3U8 playlists with extended attributes:
```
#EXTM3U
#EXTINF:-1 tvg-id="la1" tvg-name="La 1" tvg-logo="https://..." group-title="Nacionales",La 1 HD
http://stream-url/la1.m3u8
```

Extracts: channel name, logo URL, group/category, tvg-id (for EPG matching), language, stream URL.

### EPG Parser (XMLTV)
Parses XMLTV format — the standard for TV guide data:
- Maps programs to channels via `tvg-id`
- Stores current + upcoming programs (configurable window, default 48h)
- Auto-refreshes on schedule (default every 12h)
- Caches parsed data to avoid re-downloading on restart

### Stream Proxy
HubPlay proxies IPTV streams instead of sending the raw URL to clients:
- **Why proxy?** Unified auth, CORS handling, stream health monitoring, and consistent player experience across all clients
- **Reconnection:** If source stream drops, auto-retry with backoff (1s, 2s, 4s) before marking channel as temporarily unavailable
- **No transcoding:** IPTV streams arrive pre-encoded, we just relay them. Client must support the codec (usually H.264/AAC — universally supported)
- **Timeshift (future):** Buffer last N minutes for pause/rewind on live TV

### Channel Manager
```go
type ChannelManager interface {
    // Playlist management
    LoadPlaylist(ctx context.Context, libraryID uuid.UUID, m3uURL string) error
    RefreshPlaylist(ctx context.Context, libraryID uuid.UUID) error

    // EPG
    LoadEPG(ctx context.Context, libraryID uuid.UUID, epgURL string) error
    GetCurrentProgram(ctx context.Context, channelID uuid.UUID) (*EPGProgram, error)
    GetSchedule(ctx context.Context, channelID uuid.UUID, from, to time.Time) ([]EPGProgram, error)

    // Channels
    GetChannels(ctx context.Context, libraryID uuid.UUID, opts ListOptions) ([]Channel, int, error)
    GetByGroup(ctx context.Context, libraryID uuid.UUID, group string) ([]Channel, error)
    SetFavorite(ctx context.Context, userID, channelID uuid.UUID, fav bool) error

    // Stream
    ProxyStream(ctx context.Context, channelID uuid.UUID, w io.Writer) error
}
```

### User Flow
1. User creates library → type "TV en Directo"
2. Enters M3U URL (and optionally EPG URL)
3. HubPlay downloads and parses the playlist → channels appear organized by group
4. User browses channels by category, sees current program from EPG
5. Clicks a channel → stream starts via HubPlay proxy
6. Playlist auto-refreshes periodically (default 24h) to pick up new channels

### Events
```go
const (
    EventChannelAdded   EventType = "channel.added"
    EventChannelRemoved EventType = "channel.removed"
    EventEPGUpdated     EventType = "epg.updated"
    EventPlaylistRefreshed EventType = "playlist.refreshed"
)
```

---

## 11. Configuration

```yaml
# hubplay.yaml
libraries:
  - name: "Movies"
    type: movies
    paths:
      - /media/movies
      - /nas/movies
    scan_mode: auto        # auto | manual | scheduled
    scan_interval: 6h      # for scheduled mode

  - name: "TV Shows"
    type: tvshows
    paths:
      - /media/tv
    scan_mode: auto

  - name: "TV en Directo"
    type: livetv
    m3u_url: "https://provider.com/playlist.m3u"
    epg_url: "https://provider.com/epg.xml"    # Optional
    refresh_interval: 24h

metadata:
  language: "es"           # Preferred metadata language
  country: "ES"
  providers:
    tmdb:
      api_key: "${TMDB_API_KEY}"
    fanart:
      api_key: "${FANART_API_KEY}"

scanner:
  probe_workers: 4         # Concurrent FFprobe processes
  ignore_patterns:
    - "*.sample.*"
    - "*.txt"
    - ".DS_Store"

cache:
  path: "~/.hubplay/cache" # All generated content goes here
  max_size: "10GB"
```
