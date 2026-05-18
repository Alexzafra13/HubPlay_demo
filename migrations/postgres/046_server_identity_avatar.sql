-- +goose Up
-- Server-side branding: avatar color + uploaded image path.
-- Ver el hermano SQLite para el rationale completo.

ALTER TABLE server_identity ADD COLUMN avatar_color TEXT NOT NULL DEFAULT '';
ALTER TABLE server_identity ADD COLUMN avatar_image_path TEXT NOT NULL DEFAULT '';
