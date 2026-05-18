-- +goose Up
-- Per-channel logo overrides. See SQLite sibling for the full design
-- rationale (separate table from channel_overrides, keyed by
-- (library_id, stream_url) to survive M3U refreshes, read-time overlay
-- in applyAdminOverlay).
CREATE TABLE channel_logo_overrides (
    library_id  TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    stream_url  TEXT NOT NULL,
    logo_url    TEXT NOT NULL DEFAULT '',
    logo_file   TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (library_id, stream_url),
    CHECK (logo_url <> '' OR logo_file <> '')
);
