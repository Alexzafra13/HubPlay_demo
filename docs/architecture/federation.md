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

## 2.5 Network Reachability & NAT

Federation requires that **at least one of the two peers be reachable from the public internet on TCP/443**. The protocol itself is transport-agnostic — what follows is a survey of the practical scenarios you'll see in the wild.

### Reachability scenarios

| Scenario | Outcome | Recommended setup |
|---|---|---|
| Both peers have public IPs / port-forwarded routers | Symmetric peering — best | Plain HTTPS via reverse proxy (Caddy / nginx / Traefik / Apache) |
| One peer is behind CGNAT or strict NAT, the other is public | Asymmetric peering: only the NAT'd peer initiates pulls | Plain HTTPS on the public side; the NAT'd side just needs outbound 443 |
| Both peers are behind CGNAT / no port forwarding | Direct peering impossible — needs an overlay | Cloudflare Tunnel (free for personal) **or** Tailscale mesh **or** WireGuard between them |
| Friends-only mesh, never public | Same as above, but Tailscale becomes the obvious pick | Tailscale: each peer's URL is `https://hostname.tailnet.ts.net` |

### Protocol invariants regardless of transport

The federation protocol does **not** make assumptions about how peer URLs resolve. It only requires:

1. The peer URL responds with valid TLS (so the peer's identity is verifiable beyond the Ed25519 signature — defence in depth).
2. The peer can keep an HTTPS connection open for at least 60 seconds (SSE / long-lived peer event streams).
3. The peer can serve large `Content-Length` responses without timing out (download endpoint).

A Tailscale mesh meets all three. A Cloudflare Tunnel meets all three. A Caddy / nginx fronted public IP meets all three. The federation code does not care which.

### What the admin must configure

Per peer, the local admin records:

- **Peer base URL** — the URL the peer itself advertises (set in their `federation.public_url`). This may differ from the URL their reverse proxy answers on if they're behind an overlay.
- **TLS verification policy** — default: full system CA trust. For Tailscale or self-signed deployments, optional per-peer CA pinning (the peer's exact cert fingerprint).

This split — the peer declares its own URL, the local side verifies — is what lets a NAT'd peer participate: their advertised URL is the overlay endpoint; the cryptographic identity (Ed25519 pubkey) is independent of network reachability.

### What HubPlay ships

The reverse-proxy turnkey package (`deploy/reverse-proxy/`) covers the three common cases:
- **Public IP / port-forward**: Caddy (auto-LE), nginx, Traefik, Apache configs.
- **CGNAT escape**: Cloudflare Tunnel and Tailscale walkthroughs.

See [Section 15: Deployment & Reverse Proxy](#15-deployment--reverse-proxy).

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
    PeerID                  uuid.UUID
    SharedLibraries         []uuid.UUID   // Which local libraries this peer can see (empty = all)
    AllowStreaming          bool          // Can their users stream our content
    AllowDownload           bool          // Can they download files to their server
    AllowLiveTV             bool          // Can they pull our IPTV channels + EPG
    MaxBandwidthMbps        int           // 0 = unlimited
    MaxConcurrentStreams    int           // 0 = unlimited (across all of this peer's users)
    MaxConcurrentTranscodes int           // 0 = unlimited; one peer's hostile re-encode storm shouldn't crowd local users
    DailyBytesQuota         int64         // 0 = unlimited; resets at server local midnight
    RateLimitPerMinute      int           // requests/min for this peer (defence in depth, not load shaping)
}
```

### Trust on First Use & Key Pinning

The first handshake between two servers establishes trust by **exchanging Ed25519 public keys over an out-of-band channel**: the invite code (carried via chat / email / paper) authenticates the introduction. After the handshake, each side **pins** the peer's public key.

#### Fingerprint confirmation (out-of-band channel)

The handshake UI shows both admins an **SSH-style fingerprint** of the peer's pubkey before completion: 16 hex chars in groups of 4 (`a8f3:k2m9:x4p1:c7e2`), plus the optional [BIP-39 / NATO-phonetic mnemonic](https://en.wikipedia.org/wiki/PGP_word_list) representation for voice confirmation. Both admins paste / read the fingerprint to each other in their out-of-band chat ("¿te casa este fingerprint?"); only when both confirm does the handshake commit.

This is the SSH model: TOFU is *only* trustworthy if the introduction itself is verified. A MITM that intercepts the invite code can complete the handshake with both servers, but the fingerprints they each show their respective admins will not match — the divergence is visible immediately.

Subsequent connections verify:
1. The peer's TLS certificate is valid (system CA chain, OR pinned per-peer cert if the admin chose to pin one — for Tailscale / self-signed deployments).
2. The peer's Ed25519 signature on the request matches the pinned public key.

A pubkey mismatch is an alarm condition: the request is rejected and an `EventPeerKeyMismatch` event fires. This catches both network-level MITM (impossible if TLS verifies, but defence in depth) and a peer that has rotated keys without re-handshake.

### Key Rotation

Server identity keys are sticky — they identify the server across years. But operators occasionally need to rotate (key compromise, hardware migration, accidental disk leak). Rotation is **explicit and pairwise**:

1. Server A admin: Settings → Federation → "Rotate identity key". Old key kept for a grace window.
2. Server A signs a `KeyRotationAnnouncement{old_pubkey, new_pubkey, signed_by_old_key}` and pushes it to every peer.
3. Each peer verifies the announcement is signed by the *currently pinned* old key, then atomically updates their pinned record.
4. After the grace window (default 24h), the old key is purged.

Rotation announcements expire after their grace window — a peer that was offline for the full window must re-handshake manually. This avoids a stale rotation token sitting valid for arbitrary time.

### Per-Peer Rate Limiting

Federation traffic is server-to-server, so reverse-proxy WAN rate limits would punish legitimate behaviour (a bulk catalog sync looks like a burst). Rate limiting is enforced at the **application layer per peer**:

- Token bucket per peer (default: 60 req/min, burst 30). Configurable.
- Streaming HLS segment endpoints are exempt from request rate (segments come in clusters).
- Concurrent stream cap, transcode cap, and daily-bytes quota are orthogonal ceilings.
- Exceeding the rate limit returns `429 Too Many Requests` with `Retry-After`. Peer HTTP clients implement exponential backoff.

### Audit Log

Every federation request hitting your server is recorded in `federation_audit_log` for at least 30 days (configurable):

- Timestamp, peer_id, endpoint, method, response status, bytes transferred.
- For streaming sessions: session_id and item_id.
- For downloads: target_library_id and final file size.
- For Live TV: channel_id and duration.

Per-peer audit retention is queryable via the admin UI ("what did Pedro's server do in the last 7 days?"). The log is the trust escape hatch: if you suspect a peer is compromised, the audit log shows exactly what they accessed before you revoke them.

### Threat Model

| Threat | Defence |
|---|---|
| Network MITM | TLS on transport + Ed25519 signature on every request (independent of TLS). MITM must break both. |
| Peer key compromise (peer's privkey leaked) | Rotation flow + audit log review; revocation is unilateral and immediate (`DELETE /peers/{id}` invalidates all subsequent JWTs). |
| Hostile peer (peer is malicious) | Scoped permissions, per-peer rate limits, daily byte quota, audit log. A hostile peer can only do what their grants allow. |
| Confused deputy (peer presents stolen JWT from another peer) | Each peer's JWT is signed by *their own* private key; impossible to forge as another peer without that peer's private key. |
| Replay attack | Short-TTL JWTs (5 min) + nonce in claim + clock-skew tolerance ±1 min. Server-side nonce cache for the token TTL window. |
| DoS by hostile peer | Token bucket per peer + per-peer concurrency cap + daily byte quota; revocation is one click. |
| Resource exhaustion (transcode storms) | `MaxConcurrentTranscodes` per peer, separate from the local user transcode budget. |
| Privacy leak (a local user's watch history visible to peer) | All peer requests run as `remote_user(peer_id, remote_user_id)`. Local user_id never appears in peer-facing responses. |

**What we do not defend against**:

- A peer admin spying on their own users' activity (out of scope; trust is between admins).
- Long-term traffic analysis of federation patterns (constant-rate cover traffic too costly for a self-hosted use case).
- Compromise of the local server itself (federation isn't a security boundary inside your own machine).

---

## 4. Federation API

### Discovery & Linking

```
# Server info (public, no auth needed). The receiving admin sees this
# at the moment of "Add Server" so they can confirm the version is
# compatible AND the fingerprint matches what the inviting admin sees
# on their side. Forward-compat: supported_scopes lets future versions
# decline scopes the peer doesn't implement (e.g. an older peer without
# livetv support).
GET  /api/v1/federation/info
→ {
    server_id:           "uuid",
    name:                "Pedro's Server",
    version:             "0.1.5",
    public_key:          "base64-ed25519-pubkey",
    pubkey_fingerprint:  "a8f3:k2m9:x4p1:c7e2",   // hex groups for paste/read
    pubkey_words:        ["aardvark", "barbados", ...], // PGP word list, voice-confirm friendly
    supported_scopes:    ["browse", "play", "download", "livetv"],
    admin_contact:       "alex@example.com",       // optional, displayed pre-confirmation
    advertised_url:      "https://hubplay.example.com" // peer's canonical URL (may differ from request host on Tailscale / Cloudflare)
  }

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
# Request streaming session for a federated user.
# The body MUST include the peer-user's declared client capabilities
# verbatim — same shape as the local `X-Hubplay-Client-Capabilities`
# header. The receiving server feeds these directly into stream.Decide()
# so a Chromecast on peer A keeps DirectPlay when going through peer B.
# Origin's transcode budget (MaxReencodeSessions, hwaccel) applies
# automatically because federation streaming reuses stream.Manager.
POST /api/v1/federation/stream/{itemId}/session
← {
    requesting_server: "...",
    user_display_name: "Alex",
    remote_user_id:    "uuid-on-peer-A",
    client_capabilities: {
      video:     ["h264", "hevc"],
      audio:     ["aac", "eac3"],
      container: ["mp4", "mkv"]
    }
  }
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
   Body includes the originating user's `stream.Capabilities` verbatim
   (the same struct the server reads from the X-Hubplay-Client-Capabilities
   header on direct-client requests). A's user's caps travel end-to-end,
   not B's defaults.

2. Server B calls stream.Manager.StartSession(item, caps, scope=peer).
   The existing Decide() runs unchanged: DirectPlay if codecs declared
   support the file; DirectStream if container needs remux only;
   Transcode otherwise. B's MaxReencodeSessions cap and HWAccel apply
   automatically — federation streams compete for the same transcode
   budget as local users.

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

## 9.5 Live TV Peering

The IPTV transmux stack already exists locally — your channels become HLS streams via ffmpeg, with EPG matched from XMLTV. Federation extends this so a peer's IPTV subscription becomes browsable and playable from your UI, gated by the `AllowLiveTV` permission.

### How a remote channel reaches a local viewer

```
User on Server A clicks a federated channel from Server B:

1. Server A → Server B: GET /api/v1/peer/livetv/channels
   (Server B filters to channels in libraries shared with A.)

2. Server A → Server B: GET /api/v1/peer/livetv/channels/{id}/stream
   (Includes client capabilities header — same as the local stream waterfall.)

3. Server B opens its existing IPTV transmux session for {id}
   (Reuses the shared-session-per-channel cache — multiple A users count as one
    session on B, same as local users.)

4. Server B returns master playlist URL signed with peer JWT.

5. Server A rewrites playlist URLs to proxy through itself:
     Original:  https://server-b/peer/livetv/{id}/index.m3u8
     Rewritten: https://server-a/api/v1/federated-livetv/{peer-id}/{id}/index.m3u8

6. User's browser streams segments from Server A; Server A proxies to B.

7. EPG data: Server A pulls EPG slices on demand from
   GET /api/v1/peer/livetv/channels/{id}/epg?from=now&hours=6
   and renders nowPlaying / upNext on the federated channel card.
```

### Why this design (and not "share the M3U")

A naive design might just hand the peer the M3U URL and let their server connect to your IPTV provider directly. That fails three ways:

1. **Provider terms of service**: most IPTV providers cap concurrent connections per credential. A peer connecting independently would burn one of your slots and could trip the provider's anti-sharing detection.
2. **No transmux reuse**: if both servers connect to the same MPEG-TS, both run their own ffmpeg pipelines for the same stream. CPU doubled for no reason.
3. **EPG drift**: your server has the matched, scheduled EPG; the peer would have to redo all of that work.

By proxying through the origin, the peer's users *share* the origin's existing transmux session and EPG, with the origin's IPTV provider seeing exactly one connection regardless of how many peer users are watching the same channel.

### Bandwidth accounting

Live TV federation consumes the **origin's** upstream bandwidth (peer's users → origin → peer → users). The origin's `MaxBandwidthMbps` and `DailyBytesQuota` per-peer caps apply identically to Live TV as to VOD.

For asymmetric home connections (typical: 100 Mbps down / 10 Mbps up), one shared 8 Mbps HD channel saturates 80% of upstream. The recommended `MaxConcurrentStreams` per peer should account for this — default `2` for the `AllowLiveTV` scope.

### EPG isolation

EPG endpoints filter strictly to channels in libraries the peer is allowed to see. A channel that exists in a library not shared with peer X **does not** appear in:
- Their channel list (404 on direct fetch).
- Their EPG bulk schedule response.
- Their search results (server-side filter applies before the index lookup).

This is enforced as a JOIN against `library_shares` in the channel SQL, not as a post-filter — it's not possible to bypass via parameter manipulation.

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

-- Library shares (which local libraries a peer can see).
-- Empty rows = peer cannot see anything; a row = peer can see that
-- library with the given scopes. Per-library opt-in, not blanket.
CREATE TABLE federation_library_shares (
    id            TEXT PRIMARY KEY,
    peer_id       TEXT NOT NULL REFERENCES federation_peers(id) ON DELETE CASCADE,
    library_id    TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    -- Scopes flat-encoded for simple JOIN filtering. JSON kept as escape
    -- hatch for future scopes without schema churn.
    can_browse    BOOLEAN NOT NULL DEFAULT 1,
    can_play      BOOLEAN NOT NULL DEFAULT 1,
    can_download  BOOLEAN NOT NULL DEFAULT 0,
    can_livetv    BOOLEAN NOT NULL DEFAULT 0,
    extra_scopes  TEXT,                              -- JSON object for future scopes
    created_by    TEXT NOT NULL REFERENCES users(id),
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(peer_id, library_id)
);
CREATE INDEX idx_fed_share_peer ON federation_library_shares(peer_id);
CREATE INDEX idx_fed_share_library ON federation_library_shares(library_id);

-- Remote users (one row per (peer, their_user) pair seen by this server).
-- Watch progress, favourites, and history for federated viewing all key
-- on this row's id, NOT on a local users.id. This guarantees a peer's
-- user_x cannot collide with our local user_x even if numeric or
-- alphabetic IDs happen to match.
CREATE TABLE federation_remote_users (
    id              TEXT PRIMARY KEY,
    peer_id         TEXT NOT NULL REFERENCES federation_peers(id) ON DELETE CASCADE,
    remote_user_id  TEXT NOT NULL,                 -- as declared by peer in stream session request
    display_name    TEXT,                          -- "Alex" — for the badge in our UI; never trusted as auth
    first_seen_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(peer_id, remote_user_id)
);

-- Audit log: every federation request that hit this server.
-- Retention is 30d default (configurable). Keep this table on its own
-- index-only WAL pages so audit writes never block streaming.
CREATE TABLE federation_audit_log (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    peer_id         TEXT NOT NULL REFERENCES federation_peers(id) ON DELETE CASCADE,
    remote_user_id  TEXT,                          -- null for non-user actions (catalog sync, ping)
    method          TEXT NOT NULL,
    endpoint        TEXT NOT NULL,                 -- normalized: "/peer/stream/:itemId/session"
    status_code     INTEGER NOT NULL,
    bytes_out       INTEGER NOT NULL DEFAULT 0,
    item_id         TEXT,                          -- when applicable (stream, download, channel)
    session_id      TEXT,                          -- when applicable (stream sessions)
    error_kind      TEXT,                          -- AppError.Kind on failure
    duration_ms     INTEGER,
    occurred_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_audit_peer_time ON federation_audit_log(peer_id, occurred_at DESC);
CREATE INDEX idx_audit_endpoint ON federation_audit_log(endpoint, occurred_at DESC);

-- Per-peer rate limit state (in-memory primary, but persisted so
-- restarts don't grant a free burst window to a noisy peer).
CREATE TABLE federation_rate_limit_state (
    peer_id        TEXT PRIMARY KEY REFERENCES federation_peers(id) ON DELETE CASCADE,
    tokens         REAL NOT NULL,
    last_refill_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    daily_bytes    INTEGER NOT NULL DEFAULT 0,
    daily_window_started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Watch state for federated content. Keyed on (remote_user_pk, item_id)
-- — a peer's user X has progress for THEIR copy of "Inception", scoped
-- per-peer. Local progress table is untouched.
CREATE TABLE federation_progress (
    remote_user_pk TEXT NOT NULL REFERENCES federation_remote_users(id) ON DELETE CASCADE,
    peer_id        TEXT NOT NULL REFERENCES federation_peers(id) ON DELETE CASCADE,
    remote_item_id TEXT NOT NULL,
    position_seconds REAL NOT NULL,
    duration_seconds REAL,
    played          BOOLEAN NOT NULL DEFAULT 0,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (remote_user_pk, remote_item_id)
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
│   ├── proxy.go            # Stream proxying — wraps stream.Manager sessions with peer scope
│   ├── download.go         # File download from remote servers
│   ├── livetv.go           # Live TV channel + EPG peering — wraps existing IPTV transmux
│   ├── audit.go            # Audit log writer (non-blocking; goroutine + bounded channel)
│   ├── ratelimit.go        # Per-peer token bucket — extends auth/ratelimit primitives
│   └── client.go           # HTTP client for talking to remote servers
├── auth/
│   └── keystore.go         # EXISTING — extended to host the federation Ed25519 server identity key
└── db/
    └── federation_repo.go  # Peers, invites, shares, audit, remote_users, progress
```

### Implementation reuse map (don't build parallel infra)

The federation feature does **not** introduce new primitives where existing ones fit. New code is the orchestration layer; the heavy lifting reuses what's already battle-tested in HubPlay:

| Federation concern | Reuses (don't reinvent) |
|---|---|
| Server Ed25519 identity key | [`internal/auth/keystore.go`](../../internal/auth/keystore.go) — extend with `PeerIdentityKey()` accessor; same encryption-at-rest as JWT signing keys. |
| Per-request peer JWT signing + verify | [`internal/auth/jwt.go`](../../internal/auth/jwt.go) — add `EdDSA` algorithm variant; existing claims plumbing reused. |
| Streaming session for peer requests | [`internal/stream/manager.go`](../../internal/stream/manager.go) `StartSession()` — federation is just a session with `scope=peer` and `remote_user_pk` populated. **Free**: capability honouring, hwaccel, `MaxReencodeSessions`, idle reaper. |
| Capabilities end-to-end | [`internal/stream/capabilities.go`](../../internal/stream/capabilities.go) `Capabilities` struct — peer A forwards its user's caps in the session-create body; B feeds straight into `stream.Decide()`. |
| Per-peer rate limit token bucket | [`internal/auth/ratelimit.go`](../../internal/auth/ratelimit.go) — same primitive, peer_id as key instead of IP. |
| Federation events (peer.linked, peer.audit, etc.) | [`internal/event/bus.go`](../../internal/event/bus.go) — new event types, same `Subscribe` / publish contract, same SSE framing as `/me/events`. |
| SSE endpoint for peer event stream | Mirror [`internal/api/handlers/me_events.go`](../../internal/api/handlers/me_events.go) — same keepalive ticker, JSON shape, unsubscribe-on-disconnect. |
| Remote poster / image proxying | [`internal/imaging/safe_get.go`](../../internal/imaging/safe_get.go) `SafeGet()` — same SSRF guards, decompression-bomb cap as the IPTV channel logo cache. |
| Live TV stream proxying | [`internal/iptv/transmux.go`](../../internal/iptv/transmux.go) `TransmuxManager.StartLocked()` — federation Live TV viewers count as additional readers on the existing shared session per channel. **Free**: shared ffmpeg pipeline, breaker integration. |
| Cross-library channel index for EPG peering | [`internal/db/channel_repository.go`](../../internal/db/channel_repository.go) `ListLivetvChannels()` — JOIN against `federation_library_shares` to filter to peer-visible channels. |
| Error kinds for peer failures | [`internal/domain/errors.go`](../../internal/domain/errors.go) — add `ErrPeerNotFound`, `ErrPeerScopeInsufficient`, `ErrPeerKeyMismatch`, `ErrPeerRateLimited`, `ErrPeerOffline`, etc. as sentinel `AppError` types. |

**The lift is integration, not implementation.** Phase 1 schema + handshake is the only stretch that has no direct reuse hook — every other phase plugs into existing infra.

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
| Live TV channel sharing | No | No | Yes (origin transmux session shared, EPG isolated per scope) |
| Per-peer audit log | N/A | N/A | Yes (every action 30d retention) |
| Run behind CGNAT | No | No | Yes (via Tailscale or Cloudflare Tunnel) |

---

## 15. Implementation Phases

The federation feature ships as **eight self-contained phases**, each mergeable and useful without the next. Phase boundaries match real review checkpoints; nothing is gated on a downstream phase.

| Phase | Scope | Effort | Gate |
|---|---|---|---|
| **0 — Design + Deploy turnkey** | This doc + `deploy/reverse-proxy/` plug-and-play (Caddy / nginx / Traefik / Apache / Cloudflare Tunnel / Tailscale) | 1-2 days | Doc reviewed; new users can deploy with a one-command-style proxy of their choice. |
| **1 — Identity + Pairing** | Ed25519 server keypair (in keystore), `server_identity` + `federation_peers` + `federation_invites` migrations, `POST /api/v1/admin/peers/invite`, `POST /api/v1/peer/handshake`, `GET /api/v1/admin/peers`, `DELETE /api/v1/admin/peers/{id}`. Admin UI to generate + accept invites. | 2-3 days | Two servers see each other in `/admin/peers` with status `paired`. |
| **2 — JWT auth + audit log** | Per-request Ed25519-signed JWT (5 min TTL, nonce, peer_id claim), `requirePeerJWT` middleware, `federation_audit_log` migration, per-peer token bucket (`federation_rate_limit_state`). | 1-2 days | Peer A can hit `GET /api/v1/peer/ping` on B with a signed token; B records every call in audit log; rate limit fires at configured threshold. |
| **3 — Library sharing** | `federation_library_shares` migration, `federation_remote_users` migration, share/unshare admin endpoints + UI, scoped catalog endpoints (`GET /api/v1/peer/libraries`, `GET /api/v1/peer/libraries/{id}/items`). | 2-3 days | Sharing a library with peer Y makes its items visible to Y; unshared libraries are 404 (not 403 — no existence leak). |
| **4 — Remote browsing UI** | Frontend "Connected servers" sidebar tab, item cards with peer badge, item-detail proxy through origin, catalog cache (`federation_item_cache`) with 6h refresh + offline mode (cached + banner). | 1-2 days | You browse peer's catalog; offline peer still browsable from cache. |
| **5 — Remote streaming + watch state** | `POST /api/v1/peer/stream/{itemId}/session` honouring client capabilities (re-uses Phase P1.2 caps), HLS proxy through origin, `federation_progress` keyed on remote_user_pk, per-peer cap on `MaxConcurrentStreams` and `MaxConcurrentTranscodes`. | 2-3 days | Play a peer's movie, transcode-only-if-needed, watch state on peer's side never mixes with your local users. |
| **6 — Live TV peering** | `AllowLiveTV` permission, `GET /api/v1/peer/livetv/channels` filtered by shares, channel stream proxy through origin's existing IPTV transmux session cache, EPG slice endpoint. UI badge for federated channels. | 3-5 days | Peer's IPTV channel appears in your Live TV with EPG; one origin transmux session serves N peer users + M local users. |
| **7 — Download to local + polish** | `POST /api/v1/peer/download/{itemId}/request`, file streaming + metadata + image fetch, target library reconciliation, scanner pickup. Audit log UI, Prometheus per-peer metrics, peer health badges. User docs (`docs/peering-guide.md`). | 2-3 days | "Download to my server" works; admin can review 30d audit log per peer; production-ready. |

**Total**: ~14-20 dev-days for full feature, mergeable progressively. Phase 0 is the only one that does not touch Go code; it's design + deployment ergonomics.

### Out of v1 scope (deferred to future phases)

- Federated playlists / collections (item-level sharing across libraries).
- Federated search across multiple peers (Phase 3 only searches a single peer at a time).
- Subtitle hash sharing for cross-peer subtitle de-dup.
- Cross-peer recommendation engine ("your friend started watching X").
- Multi-hop trust (A trusts B, B trusts C → A sees C). Explicitly **not** supported; trust is pairwise.

---

## 16. Deployment & Reverse Proxy

Federation requires a **publicly-reachable HTTPS endpoint** (or a Tailscale / Cloudflare Tunnel equivalent — see [Section 2.5](#25-network-reachability--nat)). HubPlay ships a plug-and-play multi-proxy package under `deploy/reverse-proxy/` so you can pick the stack you already operate:

| Proxy | When to choose | Path |
|---|---|---|
| **Caddy** | Greenfield setup, no proxy yet, want zero-config Let's Encrypt | [`deploy/reverse-proxy/caddy/`](../../deploy/reverse-proxy/caddy/) |
| **nginx** | Already operating nginx, want manual control + certbot | [`deploy/reverse-proxy/nginx/`](../../deploy/reverse-proxy/nginx/) |
| **Traefik** | Homelab with Docker stack, prefer label-based routing | [`deploy/reverse-proxy/traefik/`](../../deploy/reverse-proxy/traefik/) |
| **Apache** | Existing LAMP host, prefer not to add another server | [`deploy/reverse-proxy/apache/`](../../deploy/reverse-proxy/apache/) |
| **Cloudflare Tunnel** | Behind CGNAT or want to avoid exposing your home IP | [`deploy/reverse-proxy/cloudflare-tunnel/`](../../deploy/reverse-proxy/cloudflare-tunnel/) |
| **Tailscale** | Friends-only mesh; no public internet exposure | [`deploy/reverse-proxy/tailscale/`](../../deploy/reverse-proxy/tailscale/) |

All configs implement the same five protocol invariants regardless of choice:

1. TLS termination with valid certificates.
2. WebSocket upgrade pass-through (`/api/v1/ws`).
3. SSE-friendly buffering and timeouts (`/api/v1/me/events`, `/api/v1/events`, `/api/v1/peer/events`).
4. No request body cap on streaming and download endpoints (`/api/v1/stream/`, `/api/v1/peer/`).
5. Peer endpoints (`/api/v1/peer/`) **not** subject to WAN-style rate limiting; the application enforces per-peer token buckets.

See `deploy/reverse-proxy/README.md` for the unified picker and the per-proxy READMEs for setup walkthroughs.
