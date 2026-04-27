-- Chapter markers per item (movie or episode), keyed by start time.
--
-- Table schema: migrations/sqlite/001_initial_schema.sql (CREATE TABLE chapters).
-- PK: (item_id, start_ticks). Chapters are immutable per scan — the
-- scanner clears the row set before re-inserting so a re-encode that
-- shifted markers can replace them cleanly without leaving phantom
-- entries from the previous timing.

-- name: InsertChapter :exec
INSERT INTO chapters (item_id, start_ticks, end_ticks, title, image_path)
VALUES (?, ?, ?, ?, ?);

-- name: DeleteChaptersByItem :exec
DELETE FROM chapters WHERE item_id = ?;

-- name: ListChaptersByItem :many
SELECT item_id, start_ticks, end_ticks, COALESCE(title, '') AS title,
       COALESCE(image_path, '') AS image_path
FROM chapters
WHERE item_id = ?
ORDER BY start_ticks ASC;
