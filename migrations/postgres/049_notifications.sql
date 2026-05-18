-- +goose Up
-- Inbox de notificaciones generico por usuario. Ver hermano SQLite
-- para el rationale completo.

CREATE TABLE notifications (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL,
    title      TEXT NOT NULL,
    body       TEXT NOT NULL DEFAULT '',
    link       TEXT NOT NULL DEFAULT '',
    payload    TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    read_at    TIMESTAMPTZ
);

CREATE INDEX idx_notifications_user_created
    ON notifications(user_id, created_at DESC);

CREATE INDEX idx_notifications_unread
    ON notifications(user_id, created_at DESC)
    WHERE read_at IS NULL;
