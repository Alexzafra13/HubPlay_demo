-- +goose Up
-- Opportunistic channel health tracking. See SQLite sibling for the
-- full rationale; Postgres translation only swaps DATETIME →
-- TIMESTAMPTZ and rewrites the partial index predicate (Postgres is
-- strict about types in WHERE clauses — `> 0` on an INTEGER is fine
-- as-is, no change needed).
ALTER TABLE channels ADD COLUMN last_probe_at TIMESTAMPTZ;
ALTER TABLE channels ADD COLUMN last_probe_status TEXT NOT NULL DEFAULT '';
ALTER TABLE channels ADD COLUMN last_probe_error TEXT NOT NULL DEFAULT '';
ALTER TABLE channels ADD COLUMN consecutive_failures INTEGER NOT NULL DEFAULT 0;

-- Partial index for the unhealthy-channels admin query. Tiny because
-- only rows with failures > 0 participate.
CREATE INDEX idx_channels_unhealthy
    ON channels(library_id, consecutive_failures)
    WHERE consecutive_failures > 0;

-- +goose Down
DROP INDEX IF EXISTS idx_channels_unhealthy;
ALTER TABLE channels DROP COLUMN IF EXISTS consecutive_failures;
ALTER TABLE channels DROP COLUMN IF EXISTS last_probe_error;
ALTER TABLE channels DROP COLUMN IF EXISTS last_probe_status;
ALTER TABLE channels DROP COLUMN IF EXISTS last_probe_at;
