# Federation — Design Document

## Overview

HubPlay servers can connect with each other peer-to-peer. Users can browse, stream, and optionally download content from federated servers — all from their own interface. No central service, no intermediaries. Each server stays independent and the admin controls exactly what is shared.

This is a **v1 feature** — built into the core from day one.

---

## 1. How It Works (User Perspective)

### Linking two servers
1. Admin of Server A goes to Settings → Federation → "Generate Invite Code"
2. Gets a code like `hp-invite-a8f3k2m9x4` (valid 24h)
3. Sends code to their friend who runs Server B
4. Admin of Server B goes to Settings → Federation → "Add Server" → pastes code
5. Both servers exchange keys → appear as "Linked" with a green status

### Browsing remote content
1. User on Server A sees a "Federated Servers" section in the sidebar
2. Clicks "Server B (Pedro)" → sees Pedro's shared libraries
3. Browses movies with posters, metadata, ratings — same UI as local content
4. Remote items show a small badge/icon indicating they're federated

### Streaming from remote server
1. User clicks play on a remote movie
2. Server A tells Server B: "stream this item to my user"
3. Server B streams via HLS directly to the user's client
4. The user sees it like any other movie — progress is saved locally on Server A

### Downloading to local server
1. User (or admin) finds a movie on Server B they want locally
2. Clicks "Download to my server"
3. Server A downloads the file from Server B in the background
4. Once done, the item appears in a local library with full metadata
5. Original file on Server B is untouched

---

## 2. Architecture

```
┌──────────────────┐                    ┌──────────────────┐
│   Server A       │                    │   Server B       │
│                  │   Federation API   │                  │
│  ┌────────────┐  │◄──── REST+JWT ────►│  ┌────────────┐  │
│  │ Federation │  │                    │  │ Federation │  │
│  │ Manager    │  │                    │  │ Manager    │  │
│  └─────┬──────┘  │                    │  └─────┬──────┘  │
│        │         │                    │        │         │
│  ┌─────▼──────┐  │                    │  ┌─────▼──────┐  │
│  │ Peer       │  │                    │  │ Peer       │  │
│  │ Registry   │  │                    │  │ Registry   │  │
│  └─────┬──────┘  │                    │  └─────┬──────┘  │
│        │         │                    │        │         │
│  ┌─────▼──────┐  │                    │  ┌─────▼──────┐  │
│  │ Content    │  │                    │  │ Content    │  │
│  │ Cache      │  │                    │  │ Cache      │  │
│  └────────────┘  │                    │  └────────────┘  │
└──────────────────┘                    └──────────────────┘
```

---

## 3. Trust & Security

### Key Exchange
When two servers link, they exchange Ed25519 public keys. All subsequent communication is authenticated:

```go
type PeerIdentity struct {
    ServerID    uuid.UUID       // Unique server identity
    Name        string          // "Pedro's Server"
    URL         string          // "https://pedro.hubplay.local"
    PublicKey   ed25519.PublicKey
    LinkedAt    time.Time
}
```

### Request Authentication
Every server-to-server request includes a signed JWT:
- Signed with the sending server's Ed25519 private key
- Verified by the receiving server using the stored public key
- Short-lived tokens (5 minutes) to prevent replay attacks
- Includes: server_id, timestamp, requested resource

### Permission Model
Each peer has configurable permissions set by the local admin:

```go
type PeerPermissions struct {
    PeerID            uuid.UUID
    SharedLibraries   []uuid.UUID   // Which local libraries this peer can see (empty = all)
    AllowStreaming    bool           // Can their users stream our content
    AllowDownload     bool           // Can they download files to their server
    MaxBandwidthMbps  int            // 0 = unlimited
    MaxConcurrentStreams int         // 0 = unlimited
}
```

---

## 4. Federation API

### Discovery & Linking

```
# Server info (public, no auth needed)
GET  /api/v1/federation/info
→ { server_id, name, version, public_key }

# Generate invite code (admin only)
POST /api/v1/federation/invites
→ { code: "hp-invite-a8f3k2m9x4", expires_at: "..." }

# Accept an invite (admin only, sends our info to the remote server)
POST /api/v1/federation/accept
← { code: "hp-invite-a8f3k2m9x4", server_url: "https://my.server" }
→ { peer: { server_id, name, url, public_key } }

# List linked peers
GET  /api/v1/federation/peers
→ [{ server_id, name, url, status, linked_at, last_seen_at }]

# Update peer permissions
PUT  /api/v1/federation/peers/{id}/permissions
← { allow_streaming: true, allow_download: false, shared_libraries: [...] }

# Unlink a peer
DELETE /api/v1/federation/peers/{id}
```

### Content Browsing (server-to-server, authenticated)

```
# List shared libraries
GET  /api/v1/federation/catalog/libraries
→ [{ id, name, content_type, item_count }]

# Browse items in a library (paginated)
GET  /api/v1/federation/catalog/libraries/{id}/items?offset=0&limit=20&sort=title
→ { items: [{ id, type, title, year, poster_url, ... }], total: 350 }

# Get item detail with full metadata
GET  /api/v1/federation/catalog/items/{id}
→ { id, type, title, year, overview, genres, cast, images, streams, ... }

# Search across shared libraries
GET  /api/v1/federation/catalog/search?q=inception
→ { items: [...] }

# Get children (seasons of a series, episodes of a season)
GET  /api/v1/federation/catalog/items/{id}/children
→ { items: [...] }
```

### Streaming (server-to-server, authenticated)

```
# Request streaming session for a federated user
POST /api/v1/federation/stream/{itemId}/session
← { requesting_server: "...", user_display_name: "Alex", client_profile: {...} }
→ { session_id, master_playlist_url }

# HLS endpoints (same as local but scoped to federation session)
GET  /api/v1/federation/stream/session/{sessionId}/master.m3u8
GET  /api/v1/federation/stream/session/{sessionId}/{quality}/index.m3u8
GET  /api/v1/federation/stream/session/{sessionId}/{quality}/{segment}.ts
```

### Download (server-to-server, authenticated, if permitted)

```
# Request download of an item
POST /api/v1/federation/download/{itemId}/request
→ { download_id, file_size, estimated_time }

# Download the actual file (streamed)
GET  /api/v1/federation/download/{downloadId}/file
→ (binary stream of the media file)

# Download metadata + images for the item
GET  /api/v1/federation/download/{downloadId}/metadata
→ { title, year, overview, genres, images: [...], external_ids: {...} }
```

---

## 5. Domain Types

```go
type FederationPeer struct {
    ID              uuid.UUID
    ServerID        uuid.UUID         // Remote server's unique ID
    Name            string            // "Pedro's Server"
    URL             string            // "https://pedro.hubplay.local"
    PublicKey        []byte           // Ed25519 public key
    Status          PeerStatus        // Online, Offline, Pending
    Permissions     PeerPermissions
    LinkedAt        time.Time
    LastSeenAt      time.Time
    LastSyncAt      time.Time         // Last catalog cache refresh
}

type PeerStatus string
const (
    PeerOnline  PeerStatus = "online"
    PeerOffline PeerStatus = "offline"
    PeerPending PeerStatus = "pending"  // Invite sent, not yet accepted
)

type FederationInvite struct {
    ID        uuid.UUID
    Code      string                  // "hp-invite-a8f3k2m9x4"
    CreatedBy uuid.UUID               // Admin who created it
    ExpiresAt time.Time               // 24h from creation
    AcceptedBy *uuid.UUID             // Peer server ID, null if pending
}

// Cached remote content for fast browsing without hitting the remote server
type FederatedItemCache struct {
    PeerID      uuid.UUID
    RemoteID    uuid.UUID             // Item ID on the remote server
    Type        ItemType
    Title       string
    Year        int
    Overview    string
    PosterURL   string                // URL on remote server
    Genres      string                // JSON array
    Rating      float64
    CachedAt    time.Time
}
```

---

## 6. Services

```go
type FederationManager interface {
    // Peer management
    GenerateInvite(ctx context.Context) (*FederationInvite, error)
    AcceptInvite(ctx context.Context, code string, remoteURL string) (*FederationPeer, error)
    ListPeers(ctx context.Context) ([]FederationPeer, error)
    RemovePeer(ctx context.Context, peerID uuid.UUID) error
    UpdatePermissions(ctx context.Context, peerID uuid.UUID, perms PeerPermissions) error

    // Health
    PingPeer(ctx context.Context, peerID uuid.UUID) (PeerStatus, error)
    PingAllPeers(ctx context.Context) map[uuid.UUID]PeerStatus

    // Catalog browsing (fetches from remote or cache)
    GetRemoteLibraries(ctx context.Context, peerID uuid.UUID) ([]Library, error)
    GetRemoteItems(ctx context.Context, peerID uuid.UUID, libraryID uuid.UUID, opts ListOptions) ([]FederatedItemCache, int, error)
    GetRemoteItem(ctx context.Context, peerID uuid.UUID, itemID uuid.UUID) (*FederatedItemCache, error)
    SearchRemote(ctx context.Context, peerID uuid.UUID, query string) ([]FederatedItemCache, error)

    // Streaming
    RequestStream(ctx context.Context, peerID uuid.UUID, itemID uuid.UUID, profile *ClientProfile) (string, error) // returns HLS URL

    // Download
    RequestDownload(ctx context.Context, peerID uuid.UUID, itemID uuid.UUID, targetLibraryID uuid.UUID) (*DownloadTask, error)
}

type DownloadTask struct {
    ID          uuid.UUID
    PeerID      uuid.UUID
    RemoteItemID uuid.UUID
    TargetLibraryID uuid.UUID
    Status      string           // "pending" | "downloading" | "completed" | "failed"
    Progress    float64          // 0.0 - 1.0
    FileSize    int64
    StartedAt   time.Time
}
```

---

## 7. Catalog Caching

To avoid hitting remote servers on every browse:

- When a user opens a federated server's library, items are cached locally
- Cache refreshes every **6 hours** in the background
- Remote posters/images are proxied through the local server (user never contacts remote directly)
- If the remote server is offline, cached content is still browsable (with an "offline" badge)
- Cache stores: title, year, type, overview, genres, rating, poster URL — enough for browsing
- Full metadata is fetched on-demand when the user opens an item detail

---

## 8. Streaming Flow (Detailed)

```
User on Server A clicks play on content from Server B:

1. Server A → Server B: POST /federation/stream/{itemId}/session
   (includes client profile: codecs, resolution, bandwidth)

2. Server B evaluates: direct play? remux? transcode?
   (same decision logic as local streaming)

3. Server B → Server A: { session_id, master_playlist_url }

4. Server A rewrites the playlist URLs to proxy through itself:
   Original:  https://server-b/federation/stream/session/xyz/1080p/index.m3u8
   Rewritten: https://server-a/api/v1/federated-stream/peer-id/session/xyz/1080p/index.m3u8

5. User's client requests segments from Server A

6. Server A proxies segment requests to Server B
   (transparent to the user — they only talk to their own server)

7. Watch progress is saved on Server A (local user_data table)
```

**Why proxy through Server A instead of direct client→Server B?**
- User only needs auth on their own server
- Server B never sees the user's IP
- Server A can enforce bandwidth limits
- Works even if Server B is behind NAT (as long as A can reach B)

---

## 9. Download Flow

```
Admin on Server A requests download of a movie from Server B:

1. Server A → Server B: POST /federation/download/{itemId}/request
2. Server B checks permissions → approves
3. Server A starts background download:
   a. GET /federation/download/{id}/metadata → saves metadata + queues image downloads
   b. GET /federation/download/{id}/file → streams file to local storage
4. Progress tracked in download_tasks table
5. On completion: file is added to the target local library
6. Scanner picks it up → already has metadata from step 3a
7. Item appears in local library as if it was always there
```

---

## 10. Database Tables

```sql
-- Server identity (generated on first run)
CREATE TABLE server_identity (
    id          TEXT PRIMARY KEY,      -- this server's UUID
    name        TEXT NOT NULL,         -- display name
    private_key BLOB NOT NULL,         -- Ed25519 private key
    public_key  BLOB NOT NULL,         -- Ed25519 public key
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Linked peers
CREATE TABLE federation_peers (
    id              TEXT PRIMARY KEY,
    server_id       TEXT NOT NULL UNIQUE,   -- remote server's UUID
    name            TEXT NOT NULL,
    url             TEXT NOT NULL,
    public_key      BLOB NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending',
    -- Permissions
    shared_libraries TEXT,            -- JSON array of library IDs, null = all
    allow_streaming  BOOLEAN NOT NULL DEFAULT 1,
    allow_download   BOOLEAN NOT NULL DEFAULT 0,
    max_bandwidth_mbps INTEGER DEFAULT 0,
    max_concurrent_streams INTEGER DEFAULT 0,
    --
    linked_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at    DATETIME,
    last_sync_at    DATETIME
);

-- Invite codes
CREATE TABLE federation_invites (
    id          TEXT PRIMARY KEY,
    code        TEXT NOT NULL UNIQUE,
    created_by  TEXT NOT NULL REFERENCES users(id),
    expires_at  DATETIME NOT NULL,
    accepted_by TEXT REFERENCES federation_peers(id)
);

-- Cached remote catalog (for browsing without hitting remote server)
CREATE TABLE federation_item_cache (
    peer_id     TEXT NOT NULL REFERENCES federation_peers(id) ON DELETE CASCADE,
    remote_id   TEXT NOT NULL,         -- item ID on remote server
    type        TEXT NOT NULL,
    title       TEXT NOT NULL,
    year        INTEGER,
    overview    TEXT,
    poster_url  TEXT,
    genres_json TEXT,
    rating      REAL,
    parent_remote_id TEXT,             -- for episodes → season, seasons → series
    cached_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (peer_id, remote_id)
);
CREATE INDEX idx_fed_cache_peer ON federation_item_cache(peer_id);

-- Active download tasks
CREATE TABLE federation_downloads (
    id                TEXT PRIMARY KEY,
    peer_id           TEXT NOT NULL REFERENCES federation_peers(id),
    remote_item_id    TEXT NOT NULL,
    target_library_id TEXT NOT NULL REFERENCES libraries(id),
    status            TEXT NOT NULL DEFAULT 'pending',
    progress          REAL DEFAULT 0,
    file_size         INTEGER DEFAULT 0,
    error_message     TEXT,
    started_at        DATETIME,
    completed_at      DATETIME,
    created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

---

## 11. Events

```go
const (
    EventPeerLinked       EventType = "federation.peer_linked"
    EventPeerUnlinked     EventType = "federation.peer_unlinked"
    EventPeerOnline       EventType = "federation.peer_online"
    EventPeerOffline      EventType = "federation.peer_offline"
    EventCatalogSynced    EventType = "federation.catalog_synced"
    EventDownloadStarted  EventType = "federation.download_started"
    EventDownloadComplete EventType = "federation.download_complete"
    EventDownloadFailed   EventType = "federation.download_failed"
)
```

---

## 12. Configuration

```yaml
# hubplay.yaml
federation:
  enabled: true
  server_name: "Alex's Server"     # How this server appears to others
  invite_expiry: 24h               # How long invite codes are valid
  catalog_sync_interval: 6h        # How often to refresh cached catalogs
  max_download_concurrent: 2       # Concurrent downloads from remote servers
  # Defaults for new peers (can be overridden per peer)
  default_allow_streaming: true
  default_allow_download: false
  default_max_bandwidth_mbps: 0    # 0 = unlimited
```

---

## 13. Directory Structure

```
internal/
├── federation/
│   ├── manager.go          # FederationManager implementation
│   ├── peer.go             # Peer domain type + trust management
│   ├── invite.go           # Invite code generation + acceptance
│   ├── catalog.go          # Remote catalog browsing + caching
│   ├── proxy.go            # Stream proxying from remote servers
│   ├── download.go         # File download from remote servers
│   ├── crypto.go           # Ed25519 key generation + JWT signing
│   └── client.go           # HTTP client for talking to remote servers
└── db/
    ├── federation_repo.go  # Peers, invites, cache, downloads persistence
    └── identity_repo.go    # Server identity (keys)
```

---

## 14. What Makes This Unique

No other self-hosted media server has this:

| Feature | Plex | Jellyfin | HubPlay |
|---------|------|----------|---------|
| Share content with friends | Via centralized Plex account | Manual, no native support | Native P2P federation |
| Stream from friend's server | Yes (via Plex relay) | No | Yes (direct or proxied) |
| Download from friend's server | No | No | Yes (with permission) |
| Requires central service | Yes (auth.plex.tv) | N/A | No — fully P2P |
| Privacy | Plex sees all activity | N/A | Only between peers |
| Offline browsing of remote catalog | No | N/A | Yes (cached catalog) |
| Admin controls what's shared | Partial | N/A | Full control per peer |
