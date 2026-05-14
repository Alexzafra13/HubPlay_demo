-- +goose Up
--
-- 043_library_channel_order.sql — admin overlay of the channel
-- list, Postgres twin of migrations/sqlite/043. See the sqlite file
-- for the design rationale.

CREATE TABLE library_channel_order (
    library_id TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    position   INTEGER NOT NULL,
    hidden     BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (library_id, channel_id)
);

CREATE INDEX idx_library_channel_order_library
    ON library_channel_order (library_id, position);
