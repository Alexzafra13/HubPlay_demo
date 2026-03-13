# Database Schema — Design Document

## Overview

SQLite by default (WAL mode), PostgreSQL optional for large deployments. Schema informed by Jellyfin's EF Core model (25+ entities, battle-tested) adapted for HubPlay's scope.

---

## Design Principles

1. **Normalized where it matters** — genres, people, external IDs are separate tables to avoid duplication
2. **Denormalized for speed** — sort_title, community_rating stored on items directly for fast queries
3. **Ancestor table** for efficient hierarchical queries (Series → Season → Episode)
4. **Blurhash on images** — instant placeholders while full images load
5. **Flexible values** — ItemValue table for genres, studios, tags — reusable across items

---

## Tables

### Users & Auth

```sql
-- Core user account
CREATE TABLE users (
    id            TEXT PRIMARY KEY,  -- UUID
    username      TEXT NOT NULL UNIQUE,
    display_name  TEXT NOT NULL,
    password_hash TEXT NOT NULL,      -- bcrypt
    avatar_path   TEXT,               -- path in cache dir
    role          TEXT NOT NULL DEFAULT 'user',  -- 'admin' | 'user'
    is_active     BOOLEAN NOT NULL DEFAULT 1,
    max_sessions  INTEGER NOT NULL DEFAULT 0,    -- 0 = unlimited
    -- Playback defaults
    subtitle_language TEXT DEFAULT '',
    audio_language    TEXT DEFAULT '',
    max_streaming_quality TEXT DEFAULT 'auto',
    enable_auto_play  BOOLEAN NOT NULL DEFAULT 1,
    theme             TEXT DEFAULT 'dark',
    --
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_login_at DATETIME
);

-- Active sessions / refresh tokens
CREATE TABLE sessions (
    id                 TEXT PRIMARY KEY,
    user_id            TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_name        TEXT NOT NULL,     -- "Chrome on Linux"
    device_id          TEXT NOT NULL,     -- unique device identifier
    ip_address         TEXT,
    refresh_token_hash TEXT NOT NULL,
    created_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_active_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at         DATETIME NOT NULL
);
CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE INDEX idx_sessions_expires ON sessions(expires_at);

-- API keys (admin-generated, full access)
CREATE TABLE api_keys (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,          -- description: "Home Assistant", "Sonarr"
    key_hash   TEXT NOT NULL UNIQUE,
    created_by TEXT NOT NULL REFERENCES users(id),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used  DATETIME
);
```

### Libraries

```sql
CREATE TABLE libraries (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    content_type TEXT NOT NULL,       -- 'movies' | 'tvshows' | 'livetv'
    scan_mode    TEXT NOT NULL DEFAULT 'auto',  -- 'auto' | 'manual' | 'scheduled'
    scan_interval TEXT DEFAULT '6h',
    -- IPTV specific
    m3u_url      TEXT,
    epg_url      TEXT,
    refresh_interval TEXT DEFAULT '24h',
    --
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Multiple paths per library
CREATE TABLE library_paths (
    library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    path       TEXT NOT NULL,
    PRIMARY KEY (library_id, path)
);

-- Per-user library access (null = all users can access)
CREATE TABLE library_access (
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, library_id)
);
```

### Media Items

```sql
-- Universal media item (movies, series, seasons, episodes)
CREATE TABLE items (
    id              TEXT PRIMARY KEY,
    library_id      TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    parent_id       TEXT REFERENCES items(id) ON DELETE CASCADE,
    type            TEXT NOT NULL,      -- 'movie' | 'series' | 'season' | 'episode'
    title           TEXT NOT NULL,
    sort_title      TEXT NOT NULL,      -- for alphabetical sorting (no "The", etc.)
    original_title  TEXT,
    year            INTEGER,
    path            TEXT,               -- filesystem path (null for virtual items like seasons)
    size            INTEGER DEFAULT 0,  -- bytes
    duration_ticks  INTEGER DEFAULT 0,  -- duration in ticks (100ns units, like Jellyfin)
    container       TEXT,               -- mkv, mp4, avi
    fingerprint     TEXT,               -- SHA256(path+mtime+size) for change detection
    -- Series/Episode specific
    season_number   INTEGER,
    episode_number  INTEGER,
    -- Ratings
    community_rating  REAL,            -- TMDb score (0-10)
    content_rating    TEXT,            -- PG-13, R, TV-MA, etc.
    -- Dates
    premiere_date   DATETIME,
    added_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    -- Status
    is_available    BOOLEAN NOT NULL DEFAULT 1  -- false if file disappeared from disk
);
CREATE INDEX idx_items_library ON items(library_id);
CREATE INDEX idx_items_parent ON items(parent_id);
CREATE INDEX idx_items_type ON items(type);
CREATE INDEX idx_items_path ON items(path);
CREATE INDEX idx_items_title ON items(sort_title);

-- Efficient hierarchical queries: find all ancestors of an item
-- e.g. Episode → Season → Series, or quickly find "all episodes in this series"
CREATE TABLE ancestor_ids (
    item_id     TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    ancestor_id TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    PRIMARY KEY (item_id, ancestor_id)
);
CREATE INDEX idx_ancestors_ancestor ON ancestor_ids(ancestor_id);
```

### Media Streams & Analysis

```sql
-- FFprobe results: video, audio, subtitle streams per item
CREATE TABLE media_streams (
    item_id      TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    stream_index INTEGER NOT NULL,
    stream_type  TEXT NOT NULL,       -- 'video' | 'audio' | 'subtitle' | 'attachment'
    codec        TEXT,                -- h264, hevc, aac, srt, etc.
    profile      TEXT,                -- Main, High, etc.
    bitrate      INTEGER,
    -- Video
    width        INTEGER,
    height       INTEGER,
    frame_rate   REAL,
    hdr_type     TEXT,                -- null, 'HDR10', 'HDR10+', 'DolbyVision'
    color_space  TEXT,
    -- Audio
    channels     INTEGER,
    sample_rate  INTEGER,
    -- Common
    language     TEXT,
    title        TEXT,
    is_default   BOOLEAN DEFAULT 0,
    is_forced    BOOLEAN DEFAULT 0,
    is_hearing_impaired BOOLEAN DEFAULT 0,
    PRIMARY KEY (item_id, stream_index)
);

-- Chapter markers (scenes within a video)
CREATE TABLE chapters (
    item_id    TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    start_ticks INTEGER NOT NULL,
    end_ticks   INTEGER NOT NULL,
    title      TEXT,
    image_path TEXT,                  -- thumbnail for chapter (in cache dir)
    PRIMARY KEY (item_id, start_ticks)
);

-- Media segments: intro, outro, credits, recap detection
-- Enables "Skip Intro" / "Skip Credits" buttons
CREATE TABLE media_segments (
    id        TEXT PRIMARY KEY,
    item_id   TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    type      TEXT NOT NULL,          -- 'intro' | 'outro' | 'credits' | 'recap' | 'commercial'
    start_ticks INTEGER NOT NULL,
    end_ticks   INTEGER NOT NULL
);
CREATE INDEX idx_segments_item ON media_segments(item_id);

-- Trickplay data: timeline thumbnail previews
CREATE TABLE trickplay_info (
    item_id       TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    width         INTEGER NOT NULL,   -- thumbnail width (e.g. 160)
    height        INTEGER NOT NULL,
    tile_width    INTEGER NOT NULL,   -- tiles per row in sprite sheet
    tile_height   INTEGER NOT NULL,   -- tiles per column
    thumb_count   INTEGER NOT NULL,   -- total thumbnails
    interval_ms   INTEGER NOT NULL,   -- ms between thumbnails
    sprite_path   TEXT NOT NULL,      -- path to sprite sheet in cache
    PRIMARY KEY (item_id, width)
);
```

### Metadata

```sql
-- Extended metadata per item
CREATE TABLE metadata (
    item_id     TEXT PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    overview    TEXT,                 -- synopsis/description
    tagline     TEXT,
    studio      TEXT,
    -- Cached for display (denormalized from item_values for speed)
    genres_json TEXT,                 -- '["Action","Sci-Fi"]' for fast reads
    tags_json   TEXT                  -- '["4K","HDR"]'
);

-- Reusable values: genres, studios, tags (normalized)
-- Prevents "Sci-Fi" vs "Science Fiction" duplication
CREATE TABLE item_values (
    id          TEXT PRIMARY KEY,
    type        TEXT NOT NULL,        -- 'genre' | 'studio' | 'tag'
    value       TEXT NOT NULL,
    clean_value TEXT NOT NULL,        -- lowercase, trimmed, for matching
    UNIQUE(type, clean_value)
);

CREATE TABLE item_value_map (
    item_id  TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    value_id TEXT NOT NULL REFERENCES item_values(id) ON DELETE CASCADE,
    PRIMARY KEY (item_id, value_id)
);
CREATE INDEX idx_value_map_value ON item_value_map(value_id);

-- External IDs (TMDb, IMDb, Fanart.tv, etc.)
CREATE TABLE external_ids (
    item_id    TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    provider   TEXT NOT NULL,         -- 'tmdb' | 'imdb' | 'fanart' | 'tvdb'
    external_id TEXT NOT NULL,
    PRIMARY KEY (item_id, provider)
);

-- People (actors, directors, writers)
CREATE TABLE people (
    id   TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    type TEXT,                        -- 'actor' | 'director' | 'writer' | 'producer'
    thumb_path TEXT                   -- photo in cache dir
);

CREATE TABLE item_people (
    item_id   TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    person_id TEXT NOT NULL REFERENCES people(id) ON DELETE CASCADE,
    role      TEXT NOT NULL,          -- 'Actor', 'Director', 'Writer'
    character_name TEXT,              -- for actors: character played
    sort_order INTEGER DEFAULT 0,
    PRIMARY KEY (item_id, person_id, role)
);
CREATE INDEX idx_item_people_person ON item_people(person_id);

-- Images with blurhash for instant placeholders
CREATE TABLE images (
    id        TEXT PRIMARY KEY,
    item_id   TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    type      TEXT NOT NULL,          -- 'poster' | 'backdrop' | 'logo' | 'thumb' | 'banner' | 'clearart'
    path      TEXT NOT NULL,          -- path in cache dir
    width     INTEGER,
    height    INTEGER,
    blurhash  TEXT,                   -- compact image placeholder (e.g. "LEHV6nWB2yk8pyo0adR*.7kCMdnj")
    provider  TEXT,                   -- 'tmdb' | 'fanart' | 'embedded'
    is_primary BOOLEAN DEFAULT 0,    -- preferred image for this type
    added_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_images_item ON images(item_id);
```

### IPTV / Live TV

```sql
CREATE TABLE channels (
    id          TEXT PRIMARY KEY,
    library_id  TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    number      INTEGER DEFAULT 0,
    group_name  TEXT,                 -- "Sports", "News", "Entertainment"
    logo_url    TEXT,
    stream_url  TEXT NOT NULL,
    tvg_id      TEXT,                 -- for EPG matching
    language    TEXT,
    country     TEXT,
    is_active   BOOLEAN NOT NULL DEFAULT 1,
    added_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_channels_library ON channels(library_id);
CREATE INDEX idx_channels_group ON channels(group_name);

CREATE TABLE epg_programs (
    id          TEXT PRIMARY KEY,
    channel_id  TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    title       TEXT NOT NULL,
    description TEXT,
    category    TEXT,
    icon_url    TEXT,
    start_time  DATETIME NOT NULL,
    end_time    DATETIME NOT NULL
);
CREATE INDEX idx_epg_channel_time ON epg_programs(channel_id, start_time);
```

### User Activity & Progress

```sql
-- Per-user watch progress and engagement
CREATE TABLE user_data (
    user_id           TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_id           TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    -- Playback
    position_ticks    INTEGER DEFAULT 0,    -- current playback position
    play_count        INTEGER DEFAULT 0,
    completed         BOOLEAN DEFAULT 0,    -- watched > 90%
    -- Engagement
    is_favorite       BOOLEAN DEFAULT 0,
    liked             BOOLEAN,              -- null = no opinion, true = liked, false = disliked
    -- Preferences per item
    audio_stream_index    INTEGER,          -- preferred audio track for this item
    subtitle_stream_index INTEGER,          -- preferred subtitle track for this item
    --
    last_played_at    DATETIME,
    updated_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, item_id)
);
CREATE INDEX idx_user_data_user ON user_data(user_id);
CREATE INDEX idx_user_data_completed ON user_data(user_id, completed);

-- Activity log (admin dashboard)
CREATE TABLE activity_log (
    id        TEXT PRIMARY KEY,
    user_id   TEXT REFERENCES users(id) ON DELETE SET NULL,
    type      TEXT NOT NULL,          -- 'login' | 'playback_start' | 'scan_complete' | 'user_created' | ...
    item_id   TEXT,
    severity  TEXT DEFAULT 'info',    -- 'info' | 'warning' | 'error'
    message   TEXT NOT NULL,
    data_json TEXT,                   -- extra context as JSON
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_activity_user ON activity_log(user_id);
CREATE INDEX idx_activity_type ON activity_log(type);
CREATE INDEX idx_activity_date ON activity_log(created_at);
```

### Plugins & Webhooks

```sql
CREATE TABLE plugin_state (
    name       TEXT PRIMARY KEY,
    version    TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'active',  -- 'active' | 'disabled' | 'malfunctioned'
    provides   TEXT,                 -- JSON array: '["metadata_provider","image_provider"]'
    config_json TEXT,                -- plugin-specific config
    installed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE webhook_configs (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    url           TEXT NOT NULL,
    method        TEXT NOT NULL DEFAULT 'POST',
    events_json   TEXT NOT NULL,      -- '["item.added","scan.completed"]'
    headers_json  TEXT,
    body_template TEXT,
    retry_count   INTEGER DEFAULT 3,
    timeout_sec   INTEGER DEFAULT 10,
    is_active     BOOLEAN NOT NULL DEFAULT 1,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE webhook_log (
    id            TEXT PRIMARY KEY,
    webhook_id    TEXT NOT NULL REFERENCES webhook_configs(id) ON DELETE CASCADE,
    event_type    TEXT NOT NULL,
    status        TEXT NOT NULL,      -- 'success' | 'failed'
    response_code INTEGER,
    response_body TEXT,
    error_message TEXT,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_webhook_log_webhook ON webhook_log(webhook_id);
```

### Full-Text Search

```sql
-- FTS5 virtual table for global search
CREATE VIRTUAL TABLE items_fts USING fts5(
    title,
    original_title,
    overview,
    content=items,
    content_rowid=rowid,
    tokenize='unicode61 remove_diacritics 2'
);

-- Triggers to keep FTS in sync
CREATE TRIGGER items_fts_insert AFTER INSERT ON items BEGIN
    INSERT INTO items_fts(rowid, title, original_title)
    VALUES (NEW.rowid, NEW.title, NEW.original_title);
END;

CREATE TRIGGER items_fts_delete AFTER DELETE ON items BEGIN
    INSERT INTO items_fts(items_fts, rowid, title, original_title)
    VALUES ('delete', OLD.rowid, OLD.title, OLD.original_title);
END;

CREATE TRIGGER items_fts_update AFTER UPDATE ON items BEGIN
    INSERT INTO items_fts(items_fts, rowid, title, original_title)
    VALUES ('delete', OLD.rowid, OLD.title, OLD.original_title);
    INSERT INTO items_fts(rowid, title, original_title)
    VALUES (NEW.rowid, NEW.title, NEW.original_title);
END;
```

---

## What We Added from Jellyfin's Schema

| Feature | Why | Jellyfin Reference |
|---------|-----|-------------------|
| `ancestor_ids` table | Fast hierarchical queries (find all episodes in a series) | `AncestorId` entity |
| `media_segments` table | "Skip Intro" / "Skip Credits" buttons | `MediaSegment` entity with type enum |
| `trickplay_info` table | Timeline preview thumbnail metadata | `TrickplayInfo` entity |
| `chapters` table | Scene/chapter navigation in player | `Chapter` entity |
| `blurhash` on images | Instant image placeholders while loading | `BaseItemImageInfo.Blurhash` |
| `activity_log` table | Admin dashboard, debugging, audit trail | `ActivityLog` entity |
| `liked` field on user_data | Thumbs up/down separate from favorites | `UserData.Likes` |
| `audio/subtitle_stream_index` | Remember user's preferred tracks per item | `UserData.AudioStreamIndex` |
| `play_count` on user_data | Track how many times something was watched | `UserData.PlayCount` |
| `api_keys` table | External integrations (Sonarr, Home Assistant) | `ApiKey` entity |
| `item_values` normalized table | Prevents genre/tag duplication | `ItemValue` + `ItemValueMap` |
| `is_hearing_impaired` on streams | Accessibility metadata | `MediaStreamInfo` property |

## What We Intentionally Left Out (From Jellyfin)

| Jellyfin Feature | Why Not |
|-----------------|---------|
| `AccessSchedule` (time-based user access) | Parental controls are v2. Simple admin/user roles for now |
| `DisplayPreferences` per client | Our React client handles its own display state |
| `Permission` (23 granular types) | Overkill for v1. admin/user roles cover 95% of cases |
| `HomeSection` customization | v2 feature. Default home layout is fine initially |
| `DeviceOptions` | Simple device tracking in sessions is enough |
| 190-column `BaseItemEntity` | We split this into items + metadata + separate tables. Cleaner |

---

## Migration Strategy

Using `goose` with raw SQL migration files:

```
migrations/
├── sqlite/
│   ├── 001_initial_schema.sql
│   ├── 002_fts_search.sql
│   └── ...
└── postgres/
    ├── 001_initial_schema.sql
    ├── 002_fts_search.sql    (uses tsvector instead of FTS5)
    └── ...
```

Each migration is idempotent and includes both `up` and `down` sections.

---

## Table Count Summary

| Category | Tables | Purpose |
|----------|--------|---------|
| Users & Auth | 3 | users, sessions, api_keys |
| Libraries | 3 | libraries, library_paths, library_access |
| Media Items | 2 | items, ancestor_ids |
| Media Analysis | 3 | media_streams, chapters, media_segments |
| Trickplay | 1 | trickplay_info |
| Metadata | 6 | metadata, item_values, item_value_map, external_ids, people, item_people |
| Images | 1 | images |
| IPTV | 2 | channels, epg_programs |
| User Activity | 2 | user_data, activity_log |
| Plugins & Webhooks | 3 | plugin_state, webhook_configs, webhook_log |
| Search | 1 | items_fts (virtual) |
| **Total** | **27** | |
