-- +goose Up
-- Opportunistic channel health tracking.
--
-- Every upstream proxy attempt records its outcome against the channel:
-- a successful fetch resets the failure counter; a network/upstream
-- error bumps it. After N consecutive failures (configurable on the
-- admin read side; default 3) the channel is surfaced in the admin UI
-- as "with problems" so the operator can investigate without waiting
-- for a user complaint.
--
-- Client-initiated cancellations (user navigates away mid-stream) are
-- filtered at the proxy call site so they never reach these columns —
-- a cancelled fetch is not a channel fault.
--
-- No auto-disable: `is_active` stays under operator control. The
-- health columns are informational; the admin decides whether to
-- flip the flag via the existing "Desactivar" action. Keeps the
-- system predictable (auto-hide of channels would surprise users
-- reacting to transient upstream blips).
--
-- Columns
--   last_probe_at         wall-clock time of the last probe (success or error)
--   last_probe_status     'ok' | 'error' | '' (never probed since import)
--   last_probe_error      message from the last error (clipped to 500 chars
--                         at the repo layer to stop a paranoid upstream from
--                         stuffing megabytes into the column)
--   consecutive_failures  count of consecutive errors since the last success.
--                         Atomic increment on failure, zero on success. NOT
--                         NULL so the atomic +1 never trips a sqlite type
--                         coercion corner case.
ALTER TABLE channels ADD COLUMN last_probe_at DATETIME;
ALTER TABLE channels ADD COLUMN last_probe_status TEXT NOT NULL DEFAULT '';
ALTER TABLE channels ADD COLUMN last_probe_error TEXT NOT NULL DEFAULT '';
ALTER TABLE channels ADD COLUMN consecutive_failures INTEGER NOT NULL DEFAULT 0;

-- Supports the unhealthy-channels admin query: library_id + high
-- failure counts. Partial index keeps it tiny — only rows that have
-- actually failed matter for the admin view.
CREATE INDEX idx_channels_unhealthy
    ON channels(library_id, consecutive_failures)
    WHERE consecutive_failures > 0;
