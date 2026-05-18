-- +goose Up
-- Branding del peer remoto: avatar color + URL de su foto, capturados
-- en el handshake desde el ServerInfo que el remoto nos envia. Ver el
-- hermano SQLite para el rationale completo.

ALTER TABLE federation_peers ADD COLUMN avatar_color TEXT NOT NULL DEFAULT '';
ALTER TABLE federation_peers ADD COLUMN avatar_image_url TEXT NOT NULL DEFAULT '';
