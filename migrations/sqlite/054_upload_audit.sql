-- +goose Up
--
-- 054_upload_audit.sql — append-only audit log for media uploads.
--
-- Cada subida queda registrada al cierre del pipeline (sea éxito o
-- error) — quién subió qué, cuántos bytes, qué llegó a hacer la
-- pipeline (validate / probe / move / index), y dónde aterrizó si
-- aterrizó. La fila se inserta UNA SOLA VEZ por upload, en su estado
-- final; las fases intermedias se publican en el event bus (SSE) sin
-- tocar la DB.
--
-- Sin foreign key a `users` ni a `libraries`:
--   - Borrar al usuario NO debe borrar el rastro de auditoría — la
--     normativa razonable es "el log sobrevive al sujeto". Si el FK
--     existiera con ON DELETE CASCADE perderíamos rastro; sin CASCADE
--     bloquearíamos el delete. Mejor desacoplar.
--   - Idem para librerías borradas.
--
-- Estados (`outcome`):
--   accepted    todas las fases pasaron, fichero en su librería
--   rejected    falló validación binaria (magic bytes / ffprobe /
--               extensión / cuota)
--   aborted     el cliente canceló o se desconectó sin terminar
--   error       falló algo no atribuible al cliente (disco lleno,
--               move falló, panic en ffprobe...). El service llena
--               `error_message` con un texto humano (no traza).
--
-- Tamaño bytes: INTEGER en SQLite (8-byte signed) cubre hasta 8 EB —
-- de sobra. Gemelo Postgres usa BIGINT.

CREATE TABLE upload_audit (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL,
    library_id      TEXT,
    original_name   TEXT NOT NULL,
    final_path      TEXT,           -- relativa al data dir; NULL si no aterrizó
    bytes           INTEGER NOT NULL,
    sha256          TEXT,           -- hex, NULL si no se llegó a calcular
    mime_detected   TEXT,           -- "video/mp4", "video/x-matroska"...
    outcome         TEXT NOT NULL CHECK (outcome IN ('accepted','rejected','aborted','error')),
    error_message   TEXT,           -- NULL salvo outcome != accepted
    started_at      DATETIME NOT NULL,
    finished_at     DATETIME NOT NULL,
    duration_ms     INTEGER NOT NULL DEFAULT 0
);

-- Índice principal: el panel admin filtra por usuario + ventana
-- temporal ("uploads de Alex en los últimos 7 días"). created_at DESC
-- es lo que más se consulta.
CREATE INDEX idx_upload_audit_user_started ON upload_audit(user_id, started_at DESC);

-- Outcome para el agregado "cuántos fallos en las últimas N horas"
-- del dashboard de salud. Parcial — la mayoría de filas son 'accepted'
-- y nunca querríamos filtrarlas por outcome solo.
CREATE INDEX idx_upload_audit_outcome ON upload_audit(outcome, started_at DESC)
    WHERE outcome != 'accepted';
