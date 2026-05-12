-- +goose Up
-- Manual channel edits that must survive an M3U refresh. See SQLite
-- sibling for the full design rationale.
CREATE TABLE channel_overrides (
    library_id  TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    stream_url  TEXT NOT NULL,
    tvg_id      TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (library_id, stream_url)
);

-- +goose Down
DROP TABLE IF EXISTS channel_overrides;
