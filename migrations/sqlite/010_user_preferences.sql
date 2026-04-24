-- +goose Up
-- Per-user key/value preference store.
--
-- Used for any UI preference that must follow a user across devices
-- but doesn't warrant its own dedicated column on `users`. First
-- consumer is the LiveTV "hero" mode (favorites / live-now / newest /
-- off) — the viewer picks what gets spotlighted at the top of
-- Discover and the choice persists whether they open the app from
-- their phone, laptop, or TV browser.
--
-- Shape: (user_id, key) composite PK with an opaque TEXT value. The
-- value is free-form (JSON, primitive, whatever the frontend hook
-- encodes) — the backend doesn't need to parse it. Generic on purpose;
-- future preferences (preferred language, default tab, theme override)
-- land here without schema churn.
--
-- CASCADE on user delete keeps the table clean; no index on `key`
-- because the common query is `SELECT * WHERE user_id = ?` and the
-- PK already covers that prefix.
CREATE TABLE user_preferences (
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key        TEXT NOT NULL,
    value      TEXT NOT NULL,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, key)
);
