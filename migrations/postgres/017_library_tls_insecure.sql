-- +goose Up
-- TLS-insecure flag on libraries (per-library opt-out of cert
-- validation for M3U / EPG fetches). See SQLite sibling for the full
-- rationale.
--
-- Translation note: the SQLite version stores this as INTEGER NOT
-- NULL DEFAULT 0 — it's semantically boolean but typed as INT for
-- historical reasons. We KEEP it as INTEGER in Postgres (not
-- BOOLEAN) so the sqlc-generated Go field has the same type across
-- dialects (`int64` in both, NOT `int64` vs `bool`). Switching to
-- BOOLEAN here would break the dual-dialect repo interface that
-- relies on identical method signatures. Idiomatically Postgres
-- prefers BOOLEAN, but parity > idiomatic here.
ALTER TABLE libraries ADD COLUMN tls_insecure INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE libraries DROP COLUMN IF EXISTS tls_insecure;
