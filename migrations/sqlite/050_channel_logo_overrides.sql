-- +goose Up
-- Per-channel logo overrides — let the admin replace the M3U `tvg-logo`
-- with a custom URL or an uploaded image file.
--
-- Why a separate table instead of extending `channel_overrides` (the
-- tvg_id one from migration 009):
--
--   - `channel_overrides.tvg_id` is NOT NULL; an empty string already
--     means something specific ("no tvg_id, match by display-name"),
--     so we can't sentinel-encode "logo-only override" on the same row.
--   - Logo state is two-valued: URL OR uploaded file (one of, not both).
--     A separate table keeps the invariant local — clearer than nullable
--     columns on a row that may also be tracking unrelated state.
--   - Same survives-M3U-refresh design pattern: keyed by
--     `(library_id, stream_url)` because M3U re-imports regenerate
--     channel UUIDs, so a channel_id key would orphan every override
--     on every refresh.
--
-- Read flow:
--   1. The channel listing service joins channel_logo_overrides on
--      (library_id, stream_url) when materialising the response.
--   2. `applyAdminOverlay` (the same one that already applies order +
--      hidden) extends to swap `LogoURL` when a row is present.
--   3. The GET /channels/{id}/logo proxy reads the override row before
--      falling back to channels.logo_url — so an upload-only override
--      (logo_url='') still routes correctly.
--
-- Apply order on listing:
--   override.logo_file → override.logo_url → channels.logo_url → 404
--   (the frontend renders the initials fallback on 404).
--
-- Storage of uploaded files:
--   logo_file holds the BASENAME only ("ch-abcd1234.png"), not an
--   absolute path. The handler concatenates `<imageDir>/channel-logos/`
--   on read. Keeps the override row portable when the operator moves
--   imageDir between hosts.
CREATE TABLE channel_logo_overrides (
    library_id  TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    stream_url  TEXT NOT NULL,
    -- External URL pasted by the admin. Empty when the override is a
    -- file upload, or when only logo_file is set.
    logo_url    TEXT NOT NULL DEFAULT '',
    -- Basename of an uploaded file under <imageDir>/channel-logos/.
    -- Empty when the override is a URL.
    logo_file   TEXT NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (library_id, stream_url),
    -- Invariant: at least one of the two overrides is non-empty. A
    -- row with both empty would be a stale "cleared" state we should
    -- DELETE instead of leaving around (handlers honour this — clear
    -- = DELETE row, not UPDATE to empties).
    CHECK (logo_url <> '' OR logo_file <> '')
);
