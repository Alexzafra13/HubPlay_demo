-- +goose Up
-- Stores the YouTube key of the best-matched trailer (or teaser, when no
-- official trailer is available) so the SeriesHero can start an embedded
-- preview ~2s after page load — same affordance Netflix uses on its
-- media tiles. Keys come from TMDb's `/tv/{id}/videos` and
-- `/movie/{id}/videos` endpoints, picked at scan time so the UI doesn't
-- pay a second TMDb round-trip per page view.
--
-- `trailer_key` is the opaque platform-specific id (`dQw4w9WgXcQ`-shaped
-- for YouTube). `trailer_site` is the platform name from TMDb
-- ("YouTube", "Vimeo") so the frontend picks the right embed URL —
-- defaults to "" because the column is also populated when no trailer
-- exists, and "" is easier to detect on the wire than NULL.
ALTER TABLE metadata ADD COLUMN trailer_key TEXT NOT NULL DEFAULT '';
ALTER TABLE metadata ADD COLUMN trailer_site TEXT NOT NULL DEFAULT '';
