-- +goose Up
-- Append-only audit log for media uploads. See SQLite sibling for the
-- design rationale.
--
-- Differences vs SQLite:
--   - bytes / duration_ms → BIGINT (INTEGER en SQLite ya es 8-byte;
--     en Postgres INTEGER es 4 bytes y nos quedaríamos cortos a partir
--     de 2 GB).
--   - DATETIME → TIMESTAMPTZ (norma del proyecto, ver postgres-migration.md).

CREATE TABLE upload_audit (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL,
    library_id      TEXT,
    original_name   TEXT NOT NULL,
    final_path      TEXT,
    bytes           BIGINT NOT NULL,
    sha256          TEXT,
    mime_detected   TEXT,
    outcome         TEXT NOT NULL CHECK (outcome IN ('accepted','rejected','aborted','error')),
    error_message   TEXT,
    started_at      TIMESTAMPTZ NOT NULL,
    finished_at     TIMESTAMPTZ NOT NULL,
    duration_ms     BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX idx_upload_audit_user_started ON upload_audit(user_id, started_at DESC);

CREATE INDEX idx_upload_audit_outcome ON upload_audit(outcome, started_at DESC)
    WHERE outcome != 'accepted';
