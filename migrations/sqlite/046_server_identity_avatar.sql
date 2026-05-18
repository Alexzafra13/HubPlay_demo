-- +goose Up
--
-- 046_server_identity_avatar.sql — branding del propio servidor.
--
-- Hasta ahora server_identity guardaba solo el nombre del servidor,
-- el UUID y las claves Ed25519. Cuando un peer hacia probe a /identity
-- veia el nombre como string pelado — sin color de marca, sin icono,
-- sin nada que distinguiera visualmente a "Casa de Alex" de "Pedro's
-- HubPlay" mas alla del texto.
--
-- Aniade dos columnas opcionales:
--
--   avatar_color       — hex tipo "#1d4ed8" elegido por el admin desde
--                        el panel de Federation. Si vacio, el frontend
--                        cae a la paleta deterministica (mismo helper
--                        que para users).
--
--   avatar_image_path  — ruta relativa bajo el data dir donde queda
--                        guardada la imagen subida (jpg/png/webp). Si
--                        vacio, el frontend pinta solo el color +
--                        iniciales. Mismo patron que users.avatar_image
--                        para reusar el uploader.
--
-- Defaults vacios para no romper instalaciones existentes; las dos
-- columnas se ignoran si el admin nunca abre el editor.

ALTER TABLE server_identity ADD COLUMN avatar_color TEXT NOT NULL DEFAULT '';
ALTER TABLE server_identity ADD COLUMN avatar_image_path TEXT NOT NULL DEFAULT '';
