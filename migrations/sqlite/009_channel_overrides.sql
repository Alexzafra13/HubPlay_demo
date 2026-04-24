-- +goose Up
-- Manual channel edits that must survive an M3U refresh.
--
-- Every M3U refresh regenerates channel IDs from scratch (random UUIDs
-- in `generateID`) and DELETE+INSERTs the whole library. Any manual
-- edit the admin made — a hand-corrected `tvg_id`, for instance — would
-- be wiped out on the very next refresh.
--
-- Rather than refactor `ReplaceForLibrary` into an UPSERT (which
-- requires a stable channel identity we don't reliably have — stream
-- URLs rotate on some CDNs), we persist overrides in a separate table
-- keyed by `(library_id, stream_url)`. The stream URL is the most
-- stable per-channel attribute from a typical M3U source; when it
-- DOES rotate the override orphans itself silently, which is the
-- least surprising failure mode.
--
-- Import flow:
--   1. ReplaceForLibrary imports the playlist as today (DELETE+INSERT
--      with new IDs) — unchanged.
--   2. A post-import hook iterates `channel_overrides` for the same
--      library and UPDATEs each matching channel's `tvg_id` in place.
--
-- Admin edit flow:
--   1. PATCH /channels/{id} with {tvg_id} updates the channels row
--      immediately and upserts a matching row in channel_overrides
--      keyed by the channel's current stream_url.
--   2. Next EPG refresh picks up the new tvg_id.
--   3. Next M3U refresh runs the hook and re-applies the override
--      (or no-ops if the URL no longer appears in the playlist).
--
-- An empty tvg_id override is meaningful ("use no tvg_id, rely on
-- display-name matching") so we store the value as NOT NULL and
-- delete the whole row when the admin wants to fully clear.
CREATE TABLE channel_overrides (
    library_id  TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    stream_url  TEXT NOT NULL,
    tvg_id      TEXT NOT NULL,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (library_id, stream_url)
);
