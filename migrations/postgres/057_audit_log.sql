-- +goose Up
-- Audit log unificado. Ver SQLite sibling para el rationale completo.
-- Diferencias vs SQLite:
--   - DATETIME → TIMESTAMPTZ.
--   - CURRENT_TIMESTAMP → NOW().

CREATE TABLE audit_log (
    id              TEXT PRIMARY KEY,
    actor_user_id   TEXT,
    event_type      TEXT NOT NULL,
    target_type     TEXT NOT NULL DEFAULT '',
    target_id       TEXT NOT NULL DEFAULT '',
    payload         TEXT NOT NULL DEFAULT '',
    ip_address      TEXT NOT NULL DEFAULT '',
    user_agent      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_log_created_at ON audit_log(created_at DESC);
CREATE INDEX idx_audit_log_actor_created ON audit_log(actor_user_id, created_at DESC);
CREATE INDEX idx_audit_log_type_created ON audit_log(event_type, created_at DESC);
