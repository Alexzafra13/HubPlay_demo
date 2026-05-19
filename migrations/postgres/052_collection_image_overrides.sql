-- +goose Up
-- Per-collection image overrides. See SQLite sibling for the full
-- design rationale.
CREATE TABLE collection_image_overrides (
    collection_id TEXT NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    image_type    TEXT NOT NULL CHECK (image_type IN ('poster', 'backdrop')),
    url           TEXT NOT NULL DEFAULT '',
    file          TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (collection_id, image_type),
    CHECK (url <> '' OR file <> '')
);
