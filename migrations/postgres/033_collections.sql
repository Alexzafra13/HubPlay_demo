-- +goose Up
-- Movie collections (sagas) as first-class entities. See SQLite
-- sibling for design notes. Direct translation.
CREATE TABLE collections (
    id              TEXT PRIMARY KEY,
    tmdb_id         INTEGER NOT NULL UNIQUE,
    name            TEXT NOT NULL,
    overview        TEXT NOT NULL DEFAULT '',
    poster_url      TEXT NOT NULL DEFAULT '',
    backdrop_url    TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_collections_name ON collections(name);

ALTER TABLE metadata
    ADD COLUMN collection_id TEXT REFERENCES collections(id) ON DELETE SET NULL;

CREATE INDEX idx_metadata_collection_id ON metadata(collection_id)
    WHERE collection_id IS NOT NULL;
