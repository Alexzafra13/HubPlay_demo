# API Reference — Design Document

REST API completa de HubPlay. Todas las rutas bajo `/api/v1/`. Autenticación via `Authorization: Bearer <jwt>` salvo donde se indique.

---

## Convenciones

### Request/Response Format
- **Content-Type**: `application/json` (excepto streaming/subtitles)
- **Paginación**: `?offset=0&limit=20` → `{ items: [...], total: N }`
- **Filtros**: query params — `?genre=Action&year=2024&sort=title&order=asc`
- **Sort válidos**: whitelist por endpoint (nunca raw SQL)
- **IDs**: UUID v4 en formato string
- **Timestamps**: ISO 8601 con timezone (`2026-03-13T10:15:00Z`)

### Response Envelope

```json
// Success (single item)
{
    "data": { ... }
}

// Success (list)
{
    "items": [ ... ],
    "total": 350,
    "offset": 0,
    "limit": 20
}

// Error
{
    "error": {
        "code": "ITEM_NOT_FOUND",
        "message": "Media item not found",
        "details": {}              // Optional: extra context
    }
}
```

### HTTP Status Codes

| Code | When |
|------|------|
| 200 | Success |
| 201 | Created (POST that creates a resource) |
| 204 | Success, no body (DELETE, some PUTs) |
| 400 | Bad request (validation error, malformed JSON) |
| 401 | Not authenticated (missing/invalid/expired JWT) |
| 403 | Forbidden (valid JWT but insufficient permissions) |
| 404 | Resource not found |
| 409 | Conflict (duplicate username, etc.) |
| 422 | Unprocessable entity (valid JSON but business logic error) |
| 429 | Rate limited |
| 500 | Internal server error |

---

## 1. Authentication

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| POST | `/auth/login` | No | Login, get access + refresh tokens |
| POST | `/auth/refresh` | No* | Refresh access token (*requires refresh token) |
| POST | `/auth/logout` | Yes | Logout, revoke refresh token |
| POST | `/auth/quickconnect/code` | No | Generate QuickConnect PIN (6 chars, 5 min TTL) |
| POST | `/auth/quickconnect/auth` | Yes | Authorize a QuickConnect PIN |
| GET | `/auth/quickconnect/poll` | No | Poll for PIN authorization |

### POST `/auth/login`

```json
// Request
{
    "username": "alex",
    "password": "my-password"
}

// Response 200
{
    "data": {
        "access_token": "eyJhbG...",     // JWT, 15 min
        "refresh_token": "a8f3k2m9...",  // Opaque, 30 days
        "expires_in": 900,               // Seconds
        "user": {
            "id": "uuid",
            "username": "alex",
            "display_name": "Alex",
            "role": "admin",
            "avatar_url": "/api/v1/users/uuid/avatar"
        }
    }
}

// Error 401
{ "error": { "code": "INVALID_CREDENTIALS", "message": "Invalid username or password" } }

// Error 429
{ "error": { "code": "RATE_LIMITED", "message": "Too many login attempts. Try again in 12 minutes" } }
```

### POST `/auth/refresh`

```json
// Request
{
    "refresh_token": "a8f3k2m9..."
}

// Response 200
{
    "data": {
        "access_token": "eyJhbG...",
        "refresh_token": "new-token...",   // Token rotation
        "expires_in": 900
    }
}
```

### QuickConnect Flow (TV/Device Pairing)

```
POST /auth/quickconnect/code → { "code": "A8F3K2", "expires_at": "..." }

// User authorizes from phone/browser:
POST /auth/quickconnect/auth  { "code": "A8F3K2" }  → 204

// TV polls:
GET /auth/quickconnect/poll?code=A8F3K2
  → 202 { "status": "pending" }         // Not yet authorized
  → 200 { "data": { "access_token": "...", "refresh_token": "..." } }
```

---

## 2. Current User (`/me`)

All endpoints require authentication.

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/me` | Get current user profile |
| PUT | `/me` | Update profile (display name, avatar) |
| PUT | `/me/password` | Change password |
| GET | `/me/preferences` | Get user preferences |
| PUT | `/me/preferences` | Update preferences |
| GET | `/me/sessions` | List active sessions |
| DELETE | `/me/sessions/{id}` | Revoke a session |
| DELETE | `/me/sessions` | Revoke all sessions except current |

### GET `/me/preferences`

```json
{
    "data": {
        "language": "es",
        "subtitle_language": "es",
        "audio_language": "es",
        "theme": "dark",
        "default_quality": "auto",
        "enable_trickplay": true,
        "home_sections": ["continue_watching", "recently_added", "next_up", "live_now"]
    }
}
```

---

## 3. Watch Progress

| Method | Endpoint | Description |
|--------|----------|-------------|
| PUT | `/me/progress/{itemId}` | Update watch progress (ticks, percentage) |
| GET | `/me/progress/{itemId}` | Get progress for a specific item |
| GET | `/me/continue-watching` | Continue watching list (paginated) |
| GET | `/me/recently-watched` | Recently watched (paginated) |
| GET | `/me/next-up` | Next episodes to watch |
| POST | `/me/watched/{itemId}` | Mark as watched (100%) |
| DELETE | `/me/watched/{itemId}` | Mark as unwatched (reset progress) |

### PUT `/me/progress/{itemId}`

```json
// Request
{
    "position_ticks": 36000000000,    // Nanoseconds (1 hour)
    "audio_stream_index": 1,          // Selected audio track
    "subtitle_stream_index": 2        // Selected subtitle track
}

// Response 204
```

---

## 4. Favorites

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/me/favorites` | List favorites (paginated, filterable by type) |
| POST | `/me/favorites/{itemId}` | Add to favorites |
| DELETE | `/me/favorites/{itemId}` | Remove from favorites |

---

## 5. Home (Aggregated)

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/me/home` | Aggregated home data |

### GET `/me/home`

Returns all home sections in a single request (avoids waterfall):

```json
{
    "data": {
        "continue_watching": [
            { "item": { ... }, "progress": { "position_ticks": 3600, "percentage": 45 } }
        ],
        "recently_added": {
            "movies": [ ... ],
            "episodes": [ ... ]
        },
        "next_up": [ ... ],
        "live_now": [
            { "channel": { ... }, "program": { "title": "...", "start": "...", "end": "..." } }
        ],
        "federation_highlights": [ ... ]
    }
}
```

---

## 6. Libraries

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| GET | `/libraries` | User | List libraries accessible to current user |
| POST | `/libraries` | Admin | Create library |
| GET | `/libraries/{id}` | User | Get library details |
| PUT | `/libraries/{id}` | Admin | Update library |
| DELETE | `/libraries/{id}` | Admin | Delete library (keeps files) |
| POST | `/libraries/{id}/scan` | Admin | Trigger library scan |
| GET | `/libraries/{id}/scan/status` | Admin | Get scan progress |

### POST `/libraries`

```json
// Request
{
    "name": "Movies",
    "content_type": "movie",           // movie | series | music | live_tv
    "paths": ["/media/movies"],
    "language": "es",
    "metadata_providers": ["tmdb", "fanart"]
}

// Response 201
{
    "data": {
        "id": "uuid",
        "name": "Movies",
        "content_type": "movie",
        "paths": ["/media/movies"],
        "item_count": 0,
        "scan_status": "idle",
        "created_at": "2026-03-13T10:15:00Z"
    }
}
```

---

## 7. Media Items

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/items` | List items (paginated, filterable) |
| GET | `/items/{id}` | Get full item details (metadata, streams, people) |
| GET | `/items/{id}/children` | Get children (seasons of series, episodes of season) |
| GET | `/items/{id}/similar` | Similar items (genre + cast based) |
| GET | `/items/{id}/images` | List images (poster, backdrop, logo) |
| DELETE | `/items/{id}` | Delete item from DB (admin, keeps file) |
| POST | `/items/{id}/refresh` | Re-scan metadata for this item |

### GET `/items?genre=Action&year=2024&sort=title&order=asc&offset=0&limit=20`

```json
{
    "items": [
        {
            "id": "uuid",
            "type": "movie",
            "title": "Inception",
            "original_title": "Inception",
            "year": 2010,
            "sort_title": "Inception",
            "poster_url": "/api/v1/items/uuid/images/poster",
            "backdrop_url": "/api/v1/items/uuid/images/backdrop",
            "community_rating": 8.4,
            "runtime_ticks": 88560000000000,
            "genres": ["Action", "Sci-Fi", "Thriller"],
            "has_subtitles": true,
            "video_codec": "hevc",
            "audio_codec": "eac3",
            "resolution": "4K",
            "container": "mkv"
        }
    ],
    "total": 350,
    "offset": 0,
    "limit": 20
}
```

### GET `/items/{id}`

Full detail including metadata, streams, and people:

```json
{
    "data": {
        "id": "uuid",
        "type": "movie",
        "title": "Inception",
        "year": 2010,
        "overview": "A thief who steals corporate secrets...",
        "tagline": "Your mind is the scene of the crime",
        "genres": ["Action", "Sci-Fi", "Thriller"],
        "community_rating": 8.4,
        "content_rating": "PG-13",
        "runtime_ticks": 88560000000000,
        "premiere_date": "2010-07-16",
        "studios": ["Warner Bros."],
        "external_ids": {
            "tmdb": "27205",
            "imdb": "tt1375666"
        },
        "images": {
            "poster": "/api/v1/items/uuid/images/poster",
            "backdrop": "/api/v1/items/uuid/images/backdrop",
            "logo": "/api/v1/items/uuid/images/logo"
        },
        "media_streams": [
            { "index": 0, "type": "video", "codec": "hevc", "width": 3840, "height": 2160, "bitrate": 45000000, "hdr_type": "HDR10" },
            { "index": 1, "type": "audio", "codec": "eac3", "channels": 6, "language": "eng", "title": "English 5.1" },
            { "index": 2, "type": "audio", "codec": "aac", "channels": 2, "language": "spa", "title": "Spanish" },
            { "index": 3, "type": "subtitle", "codec": "srt", "language": "spa", "is_forced": false }
        ],
        "people": [
            { "name": "Christopher Nolan", "role": "Director", "type": "director" },
            { "name": "Leonardo DiCaprio", "role": "Cobb", "type": "actor", "image_url": "..." }
        ],
        "chapters": [
            { "start_ticks": 0, "end_ticks": 5400000000, "title": "Opening" }
        ],
        "user_data": {
            "progress": { "position_ticks": 3600, "percentage": 45 },
            "is_favorite": true,
            "played": false
        }
    }
}
```

---

## 8. Search

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/search?q=inception&type=movie&limit=10` | Global full-text search |

```json
{
    "items": [
        {
            "id": "uuid",
            "type": "movie",
            "title": "Inception",
            "year": 2010,
            "poster_url": "...",
            "match_score": 0.95
        }
    ],
    "total": 3
}
```

Query params:
- `q` (required): search term (uses FTS5, supports partial matches)
- `type`: filter by `movie`, `series`, `episode`, `person`
- `library_id`: filter by library
- `limit`: max results (default 20, max 100)

---

## 9. Streaming

No auth on segment/playlist URLs — they use session-scoped tokens instead.

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| GET | `/stream/{itemId}/master.m3u8` | Session | Master playlist (adaptive bitrate) |
| GET | `/stream/{itemId}/{quality}/index.m3u8` | Session | Quality-specific playlist |
| GET | `/stream/{itemId}/{quality}/{segment}.ts` | Session | HLS segment |
| GET | `/stream/{itemId}/direct` | Session | Direct play (progressive download) |
| GET | `/stream/{itemId}/subtitles/{streamIndex}.vtt` | Session | Subtitle track (WebVTT) |
| POST | `/stream/{itemId}/session` | JWT | Create stream session, get session token |

### POST `/stream/{itemId}/session`

```json
// Request
{
    "client_profile": "web-chrome",     // Or custom profile object
    "audio_stream_index": 1,
    "subtitle_stream_index": 3,
    "start_position_ticks": 36000000000,
    "max_bitrate": 8000000
}

// Response 201
{
    "data": {
        "session_id": "uuid",
        "session_token": "short-lived-token",   // 1 hour, tied to this item + user
        "playback_method": "transcode",          // direct_play | direct_stream | transcode
        "master_playlist": "/api/v1/stream/uuid/master.m3u8?token=short-lived-token",
        "direct_url": null,                       // Set if direct_play
        "media_streams": [ ... ],
        "trickplay_url": "/api/v1/stream/uuid/trickplay/sprites.vtt"
    }
}
```

All subsequent segment/playlist requests include `?token=session-token`.

---

## 10. IPTV / Live TV

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| GET | `/channels` | User | List channels (paginated, filterable by group) |
| GET | `/channels/{id}` | User | Get channel detail |
| GET | `/channels/{id}/stream` | User | Proxy stream URL |
| GET | `/channels/{id}/epg?from=&to=` | User | EPG for one channel |
| GET | `/channels/epg?from=&to=&group=` | User | EPG grid (batch) |
| GET | `/channels/now` | User | Currently playing on all channels |
| GET | `/channels/groups` | User | List channel groups |

---

## 11. User Management (Admin)

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| GET | `/users` | Admin | List users |
| POST | `/users` | Admin | Create user |
| GET | `/users/{id}` | Admin | Get user detail |
| PUT | `/users/{id}` | Admin | Update user (role, libraries, display name) |
| DELETE | `/users/{id}` | Admin | Delete user and their data |

### POST `/users`

```json
// Request
{
    "username": "maria",
    "password": "strong-password",
    "display_name": "María",
    "role": "user",                    // admin | user
    "allowed_libraries": ["uuid1", "uuid2"]
}

// Response 201
{
    "data": {
        "id": "uuid",
        "username": "maria",
        "display_name": "María",
        "role": "user",
        "allowed_libraries": ["uuid1", "uuid2"],
        "created_at": "2026-03-13T10:15:00Z"
    }
}
```

---

## 12. Plugins (Admin)

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| GET | `/plugins` | Admin | List installed plugins |
| POST | `/plugins/install` | Admin | Install plugin from URL or repo |
| DELETE | `/plugins/{name}` | Admin | Uninstall plugin |
| PUT | `/plugins/{name}/enabled` | Admin | Enable/disable plugin |
| POST | `/plugins/{name}/restart` | Admin | Restart plugin |
| GET | `/plugins/{name}/config` | Admin | Get plugin config |
| PUT | `/plugins/{name}/config` | Admin | Update plugin config |

---

## 13. Federation

### Admin Endpoints (manage peers)

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| GET | `/federation/info` | No | Server info (public, for peer discovery) |
| POST | `/federation/invites` | Admin | Generate invite code |
| POST | `/federation/accept` | Admin | Accept invite, link servers |
| GET | `/federation/peers` | Admin | List linked peers |
| PUT | `/federation/peers/{id}/permissions` | Admin | Update peer permissions |
| DELETE | `/federation/peers/{id}` | Admin | Unlink peer |

### Catalog Browsing (server-to-server, signed JWT)

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| GET | `/federation/catalog/libraries` | Peer JWT | List shared libraries |
| GET | `/federation/catalog/libraries/{id}/items` | Peer JWT | Browse library items |
| GET | `/federation/catalog/items/{id}` | Peer JWT | Get item detail |
| GET | `/federation/catalog/items/{id}/children` | Peer JWT | Get children |
| GET | `/federation/catalog/search?q=` | Peer JWT | Search peer catalog |

### Federated Streaming

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| POST | `/federation/stream/{itemId}/session` | Peer JWT | Request stream session |
| GET | `/federation/stream/session/{id}/*.m3u8` | Session | HLS playlists |
| GET | `/federation/stream/session/{id}/*/*.ts` | Session | HLS segments |

### Federated Download (if permitted)

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| POST | `/federation/download/{itemId}/request` | Peer JWT | Request download |
| GET | `/federation/download/{id}/file` | Peer JWT | Binary stream |
| GET | `/federation/download/{id}/metadata` | Peer JWT | Metadata bundle |

---

## 14. System

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| GET | `/health` | No | Health check (DB, FFmpeg, disk, streams) |
| GET | `/system/info` | Admin | Server info (version, OS, FFmpeg version, HW accel) |
| GET | `/system/logs?level=warn&limit=100` | Admin | Recent log entries |
| GET | `/system/activity` | Admin | Activity log (scans, logins, streams) |
| POST | `/system/restart` | Admin | Restart server |
| POST | `/system/cache/clear` | Admin | Clear transcode/thumbnail cache |
| GET | `/system/tasks` | Admin | Background tasks (scan, metadata, trickplay) |

### GET `/health`

```json
{
    "status": "healthy",
    "version": "1.2.0",
    "uptime": "72h15m",
    "database": "ok",
    "ffmpeg": "ok",
    "active_streams": 3,
    "active_transcodes": 1,
    "disk": {
        "config_free_gb": 45.2,
        "cache_free_gb": 120.8,
        "cache_used_gb": 12.3
    }
}
```

---

## 15. WebSocket

### `GET /ws` (requires JWT as query param or cookie)

Real-time events pushed to connected clients:

```json
// Server → Client
{ "type": "library.scan.progress", "data": { "library_id": "uuid", "progress": 0.45 } }
{ "type": "item.added", "data": { "id": "uuid", "type": "movie", "title": "..." } }
{ "type": "transcode.progress", "data": { "session_id": "uuid", "fps": 45, "progress": 0.3 } }
{ "type": "federation.peer.online", "data": { "peer_id": "uuid", "name": "Pedro's Server" } }
```

Event types:
- `library.scan.started`, `library.scan.progress`, `library.scan.completed`
- `item.added`, `item.updated`, `item.removed`
- `metadata.updated`
- `transcode.started`, `transcode.progress`, `transcode.completed`
- `federation.peer.online`, `federation.peer.offline`
- `plugin.status_changed`

---

## TypeScript Type Generation

The API spec is used to auto-generate TypeScript types for the frontend:

```bash
make api-types    # Generates web/src/api/types.ts from OpenAPI spec
```

The OpenAPI spec (`api/openapi.yaml`) is the single source of truth. Handlers are validated against it at test time to prevent drift.
