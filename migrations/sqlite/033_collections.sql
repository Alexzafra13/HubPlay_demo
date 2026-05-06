-- +goose Up
--
-- Movie collections (sagas) as first-class entities, the way Jellyfin
-- surfaces them. TMDb attaches every movie to an optional
-- `belongs_to_collection: { id, name, poster_path, backdrop_path }`
-- record on /movie/{id} — that's the upstream id we key on, so the
-- same logical saga collapses across every member movie:
--   X-Men Collection, MCU Phase 1, Toy Story Collection, etc.
--
-- The table is movies-only (TV has its own native series → seasons →
-- episodes hierarchy). Joining is one-to-one from each movie's
-- metadata row to a single collection — TMDb only ever attaches one
-- "belongs_to" record per movie.
--
-- backdrop_url + overview let the /collections/{id} hero render
-- without a second TMDb round-trip; both default to '' so the
-- frontend can degrade gracefully when a collection lacks artwork.
CREATE TABLE collections (
    id              TEXT PRIMARY KEY,           -- "collection:<tmdb_id>"
    tmdb_id         INTEGER NOT NULL UNIQUE,    -- belongs_to_collection.id
    name            TEXT NOT NULL,
    overview        TEXT NOT NULL DEFAULT '',
    poster_url      TEXT NOT NULL DEFAULT '',
    backdrop_url    TEXT NOT NULL DEFAULT '',
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_collections_name ON collections(name);

ALTER TABLE metadata ADD COLUMN collection_id TEXT REFERENCES collections(id) ON DELETE SET NULL;

CREATE INDEX idx_metadata_collection_id ON metadata(collection_id) WHERE collection_id IS NOT NULL;
