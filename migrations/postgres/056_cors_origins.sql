-- +goose Up
-- Orígenes CORS dinámicos. Ver SQLite sibling para el rationale.
-- Diferencias vs SQLite:
--   - DATETIME → TIMESTAMPTZ (norma del proyecto).
--   - DEFAULT CURRENT_TIMESTAMP → DEFAULT NOW().

CREATE TABLE cors_origins (
    origin       TEXT PRIMARY KEY,
    created_by   TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    note         TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_cors_origins_created_at ON cors_origins(created_at DESC);
