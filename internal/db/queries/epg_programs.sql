-- Electronic program guide entries per channel.
--
-- Table schema: migrations/sqlite/001_initial_schema.sql (CREATE TABLE epg_programs).
-- NOTE: BulkSchedule uses dynamic IN() and remains as raw SQL in the adapter.

-- name: InsertEPGProgram :exec
INSERT INTO epg_programs (id, channel_id, title, description, category, icon_url, start_time, end_time)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: DeleteEPGProgramsByChannel :exec
DELETE FROM epg_programs WHERE channel_id = ?;

-- name: GetNowPlaying :one
SELECT id, channel_id, title, COALESCE(description, '') AS description,
       COALESCE(category, '') AS category, COALESCE(icon_url, '') AS icon_url,
       start_time, end_time
FROM epg_programs
WHERE channel_id = ? AND start_time <= ? AND end_time > ?
LIMIT 1;

-- name: ListSchedule :many
SELECT id, channel_id, title, COALESCE(description, '') AS description,
       COALESCE(category, '') AS category, COALESCE(icon_url, '') AS icon_url,
       start_time, end_time
FROM epg_programs
WHERE channel_id = ? AND end_time > ? AND start_time < ?
ORDER BY start_time;

-- name: CleanupOldPrograms :execrows
DELETE FROM epg_programs WHERE end_time < ?;
