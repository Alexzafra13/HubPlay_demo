-- +goose Up

-- Users & Auth
CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    display_name  TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    avatar_path   TEXT,
    role          TEXT NOT NULL DEFAULT 'user',
    is_active     BOOLEAN NOT NULL DEFAULT 1,
    max_sessions  INTEGER NOT NULL DEFAULT 0,
    subtitle_language TEXT DEFAULT '',
    audio_language    TEXT DEFAULT '',
    max_streaming_quality TEXT DEFAULT 'auto',
    enable_auto_play  BOOLEAN NOT NULL DEFAULT 1,
    theme             TEXT DEFAULT 'dark',
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_login_at DATETIME
);

CREATE TABLE sessions (
    id                 TEXT PRIMARY KEY,
    user_id            TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_name        TEXT NOT NULL,
    device_id          TEXT NOT NULL,
    ip_address         TEXT,
    refresh_token_hash TEXT NOT NULL,
    created_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_active_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at         DATETIME NOT NULL
);
CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE INDEX idx_sessions_expires ON sessions(expires_at);

CREATE TABLE api_keys (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    key_hash   TEXT NOT NULL UNIQUE,
    created_by TEXT NOT NULL REFERENCES users(id),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used  DATETIME
);

-- Libraries
CREATE TABLE libraries (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    content_type TEXT NOT NULL,
    scan_mode    TEXT NOT NULL DEFAULT 'auto',
    scan_interval TEXT DEFAULT '6h',
    m3u_url      TEXT,
    epg_url      TEXT,
    refresh_interval TEXT DEFAULT '24h',
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE library_paths (
    library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    path       TEXT NOT NULL,
    PRIMARY KEY (library_id, path)
);

CREATE TABLE library_access (
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, library_id)
);

-- Media Items
CREATE TABLE items (
    id              TEXT PRIMARY KEY,
    library_id      TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    parent_id       TEXT REFERENCES items(id) ON DELETE CASCADE,
    type            TEXT NOT NULL,
    title           TEXT NOT NULL,
    sort_title      TEXT NOT NULL,
    original_title  TEXT,
    year            INTEGER,
    path            TEXT,
    size            INTEGER DEFAULT 0,
    duration_ticks  INTEGER DEFAULT 0,
    container       TEXT,
    fingerprint     TEXT,
    season_number   INTEGER,
    episode_number  INTEGER,
    community_rating  REAL,
    content_rating    TEXT,
    premiere_date   DATETIME,
    added_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    is_available    BOOLEAN NOT NULL DEFAULT 1
);
CREATE INDEX idx_items_library ON items(library_id);
CREATE INDEX idx_items_parent ON items(parent_id);
CREATE INDEX idx_items_type ON items(type);
CREATE INDEX idx_items_path ON items(path);
CREATE INDEX idx_items_title ON items(sort_title);

CREATE TABLE ancestor_ids (
    item_id     TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    ancestor_id TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    PRIMARY KEY (item_id, ancestor_id)
);
CREATE INDEX idx_ancestors_ancestor ON ancestor_ids(ancestor_id);

-- Media Streams & Analysis
CREATE TABLE media_streams (
    item_id      TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    stream_index INTEGER NOT NULL,
    stream_type  TEXT NOT NULL,
    codec        TEXT,
    profile      TEXT,
    bitrate      INTEGER,
    width        INTEGER,
    height       INTEGER,
    frame_rate   REAL,
    hdr_type     TEXT,
    color_space  TEXT,
    channels     INTEGER,
    sample_rate  INTEGER,
    language     TEXT,
    title        TEXT,
    is_default   BOOLEAN DEFAULT 0,
    is_forced    BOOLEAN DEFAULT 0,
    is_hearing_impaired BOOLEAN DEFAULT 0,
    PRIMARY KEY (item_id, stream_index)
);

CREATE TABLE chapters (
    item_id    TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    start_ticks INTEGER NOT NULL,
    end_ticks   INTEGER NOT NULL,
    title      TEXT,
    image_path TEXT,
    PRIMARY KEY (item_id, start_ticks)
);

CREATE TABLE media_segments (
    id        TEXT PRIMARY KEY,
    item_id   TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    type      TEXT NOT NULL,
    start_ticks INTEGER NOT NULL,
    end_ticks   INTEGER NOT NULL
);
CREATE INDEX idx_segments_item ON media_segments(item_id);

CREATE TABLE trickplay_info (
    item_id       TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    width         INTEGER NOT NULL,
    height        INTEGER NOT NULL,
    tile_width    INTEGER NOT NULL,
    tile_height   INTEGER NOT NULL,
    thumb_count   INTEGER NOT NULL,
    interval_ms   INTEGER NOT NULL,
    sprite_path   TEXT NOT NULL,
    PRIMARY KEY (item_id, width)
);

-- Metadata
CREATE TABLE metadata (
    item_id     TEXT PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    overview    TEXT,
    tagline     TEXT,
    studio      TEXT,
    genres_json TEXT,
    tags_json   TEXT
);

CREATE TABLE item_values (
    id          TEXT PRIMARY KEY,
    type        TEXT NOT NULL,
    value       TEXT NOT NULL,
    clean_value TEXT NOT NULL,
    UNIQUE(type, clean_value)
);

CREATE TABLE item_value_map (
    item_id  TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    value_id TEXT NOT NULL REFERENCES item_values(id) ON DELETE CASCADE,
    PRIMARY KEY (item_id, value_id)
);
CREATE INDEX idx_value_map_value ON item_value_map(value_id);

CREATE TABLE external_ids (
    item_id    TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    provider   TEXT NOT NULL,
    external_id TEXT NOT NULL,
    PRIMARY KEY (item_id, provider)
);

CREATE TABLE people (
    id   TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    type TEXT,
    thumb_path TEXT
);

CREATE TABLE item_people (
    item_id   TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    person_id TEXT NOT NULL REFERENCES people(id) ON DELETE CASCADE,
    role      TEXT NOT NULL,
    character_name TEXT,
    sort_order INTEGER DEFAULT 0,
    PRIMARY KEY (item_id, person_id, role)
);
CREATE INDEX idx_item_people_person ON item_people(person_id);

CREATE TABLE images (
    id        TEXT PRIMARY KEY,
    item_id   TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    type      TEXT NOT NULL,
    path      TEXT NOT NULL,
    width     INTEGER,
    height    INTEGER,
    blurhash  TEXT,
    provider  TEXT,
    is_primary BOOLEAN DEFAULT 0,
    added_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_images_item ON images(item_id);

-- IPTV / Live TV
CREATE TABLE channels (
    id          TEXT PRIMARY KEY,
    library_id  TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    number      INTEGER DEFAULT 0,
    group_name  TEXT,
    logo_url    TEXT,
    stream_url  TEXT NOT NULL,
    tvg_id      TEXT,
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

-- User Activity & Progress
CREATE TABLE user_data (
    user_id           TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_id           TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    position_ticks    INTEGER DEFAULT 0,
    play_count        INTEGER DEFAULT 0,
    completed         BOOLEAN DEFAULT 0,
    is_favorite       BOOLEAN DEFAULT 0,
    liked             BOOLEAN,
    audio_stream_index    INTEGER,
    subtitle_stream_index INTEGER,
    last_played_at    DATETIME,
    updated_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, item_id)
);
CREATE INDEX idx_user_data_user ON user_data(user_id);
CREATE INDEX idx_user_data_completed ON user_data(user_id, completed);

CREATE TABLE activity_log (
    id        TEXT PRIMARY KEY,
    user_id   TEXT REFERENCES users(id) ON DELETE SET NULL,
    type      TEXT NOT NULL,
    item_id   TEXT,
    severity  TEXT DEFAULT 'info',
    message   TEXT NOT NULL,
    data_json TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_activity_user ON activity_log(user_id);
CREATE INDEX idx_activity_type ON activity_log(type);
CREATE INDEX idx_activity_date ON activity_log(created_at);

-- Providers & Webhooks
CREATE TABLE providers (
    name        TEXT PRIMARY KEY,
    type        TEXT NOT NULL DEFAULT 'metadata',
    version     TEXT NOT NULL DEFAULT '1.0',
    status      TEXT NOT NULL DEFAULT 'active',
    priority    INTEGER NOT NULL DEFAULT 100,
    config_json TEXT,
    api_key     TEXT,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE webhook_configs (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    url           TEXT NOT NULL,
    method        TEXT NOT NULL DEFAULT 'POST',
    events_json   TEXT NOT NULL,
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
    status        TEXT NOT NULL,
    response_code INTEGER,
    response_body TEXT,
    error_message TEXT,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_webhook_log_webhook ON webhook_log(webhook_id);

-- +goose Down
DROP TABLE IF EXISTS webhook_log;
DROP TABLE IF EXISTS webhook_configs;
DROP TABLE IF EXISTS providers;
DROP TABLE IF EXISTS activity_log;
DROP TABLE IF EXISTS user_data;
DROP TABLE IF EXISTS epg_programs;
DROP TABLE IF EXISTS channels;
DROP TABLE IF EXISTS images;
DROP TABLE IF EXISTS item_people;
DROP TABLE IF EXISTS people;
DROP TABLE IF EXISTS external_ids;
DROP TABLE IF EXISTS item_value_map;
DROP TABLE IF EXISTS item_values;
DROP TABLE IF EXISTS metadata;
DROP TABLE IF EXISTS trickplay_info;
DROP TABLE IF EXISTS media_segments;
DROP TABLE IF EXISTS chapters;
DROP TABLE IF EXISTS media_streams;
DROP TABLE IF EXISTS ancestor_ids;
DROP TABLE IF EXISTS items;
DROP TABLE IF EXISTS library_access;
DROP TABLE IF EXISTS library_paths;
DROP TABLE IF EXISTS libraries;
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
