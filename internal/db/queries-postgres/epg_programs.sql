-- Electronic program guide entries per channel.
--
-- Table schema: migrations/sqlite/001_initial_schema.sql (CREATE TABLE epg_programs).
-- NOTE: BulkSchedule uses dynamic IN() and remains as raw SQL in the adapter.

-- name: InsertEPGProgram :exec
INSERT INTO epg_programs (id, channel_id, title, description, category, icon_url, start_time, end_time)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: DeleteEPGProgramsByChannel :exec
DELETE FROM epg_programs WHERE channel_id = $1;

-- name: GetNowPlaying :one
SELECT id, channel_id, title, COALESCE(description, '') AS description,
       COALESCE(category, '') AS category, COALESCE(icon_url, '') AS icon_url,
       start_time, end_time
FROM epg_programs
WHERE channel_id = $1 AND start_time <= $2 AND end_time > $3
LIMIT 1;

-- name: ListSchedule :many
SELECT id, channel_id, title, COALESCE(description, '') AS description,
       COALESCE(category, '') AS category, COALESCE(icon_url, '') AS icon_url,
       start_time, end_time
FROM epg_programs
WHERE channel_id = $1 AND end_time > $2 AND start_time < $3
ORDER BY start_time;

-- name: CleanupOldPrograms :execrows
DELETE FROM epg_programs WHERE end_time < $1;
