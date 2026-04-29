-- +goose Up
-- Federation Phase 2: per-request audit log + per-peer rate limit state.
--
-- These two tables are the trust escape hatch for federation:
--
--   federation_audit_log — every peer request hitting our server is
--                          recorded for at least 30 days. If a peer is
--                          compromised the admin can review what they
--                          accessed before revoking. Append-only by
--                          design; never updated, only pruned.
--
--   federation_rate_limit_state — token-bucket state per peer. Held
--                          primarily in memory (hot path: every peer
--                          request); persisted so restarts don't grant
--                          a noisy peer a free burst window. Schema
--                          ready in Phase 2; persistence wiring is
--                          opportunistic (shutdown flush, periodic
--                          tick-based snapshot).
--
-- Why two tables instead of one: the access pattern is wildly
-- different. Audit log is write-heavy / read-rare (admin reviews
-- weekly), tiny rows, append-only. Rate-limit state is read-heavy /
-- write-on-state-change, single row per peer, frequently overwritten.
-- Mixing them on the same WAL pages would slow audit writes during
-- request bursts.

CREATE TABLE federation_audit_log (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    peer_id         TEXT NOT NULL REFERENCES federation_peers(id) ON DELETE CASCADE,
    remote_user_id  TEXT,                                   -- null for non-user actions (catalog sync, ping)
    method          TEXT NOT NULL,
    endpoint        TEXT NOT NULL,                          -- normalized: "/peer/stream/:itemId/session"
    status_code     INTEGER NOT NULL,
    bytes_out       INTEGER NOT NULL DEFAULT 0,
    item_id         TEXT,                                   -- when applicable (stream, download, channel)
    session_id      TEXT,                                   -- when applicable (stream sessions)
    error_kind      TEXT,                                   -- AppError.Kind on failure
    duration_ms     INTEGER,
    occurred_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Hot path: "what did peer X do in the last N days" — admin review.
CREATE INDEX idx_fed_audit_peer_time ON federation_audit_log(peer_id, occurred_at DESC);

-- Hot path: "which endpoint is this peer hammering" — abuse triage.
CREATE INDEX idx_fed_audit_endpoint ON federation_audit_log(endpoint, occurred_at DESC);

-- Cold path but useful: prune by occurred_at < cutoff.
CREATE INDEX idx_fed_audit_occurred ON federation_audit_log(occurred_at);


CREATE TABLE federation_rate_limit_state (
    peer_id                  TEXT PRIMARY KEY REFERENCES federation_peers(id) ON DELETE CASCADE,
    tokens                   REAL NOT NULL,
    last_refill_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    daily_bytes              INTEGER NOT NULL DEFAULT 0,
    daily_window_started_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);


-- +goose Down
DROP INDEX IF EXISTS idx_fed_audit_occurred;
DROP INDEX IF EXISTS idx_fed_audit_endpoint;
DROP INDEX IF EXISTS idx_fed_audit_peer_time;
DROP TABLE IF EXISTS federation_audit_log;
DROP TABLE IF EXISTS federation_rate_limit_state;
