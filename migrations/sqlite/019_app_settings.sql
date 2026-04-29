-- +goose Up
-- Persisted runtime settings the admin can edit from the UI without
-- touching hubplay.yaml. The split with the YAML config is deliberate
-- and matches what Plex / Jellyfin / Sonarr do:
--
--   YAML / env (boot-time, immutable at runtime):
--     server.bind, server.port, database.path, streaming.cache_dir,
--     auth bootstrap secret. Anything that requires a restart anyway
--     because changing it would invalidate already-bound listeners,
--     open file handles, signing keys, etc.
--
--   app_settings (runtime-mutable, admin-editable from the panel):
--     server.base_url, hardware_acceleration.enabled,
--     hardware_acceleration.preferred. Operator preferences that
--     should never have required SSHing into the host.
--
-- The reads layer the YAML value as a fallback so a fresh install
-- still picks up env / docker-compose defaults, and a row in this
-- table acts as an override. The endpoint that writes here is
-- whitelisted to a fixed key set — it is NOT a generic KV store. New
-- runtime-editable settings join the whitelist explicitly so a typo
-- can't poison something the admin shouldn't touch from the UI.
CREATE TABLE app_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
