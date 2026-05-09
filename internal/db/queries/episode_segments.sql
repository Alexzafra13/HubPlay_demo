-- Skip-intro / skip-credits markers per item.
--
-- Schema: migrations/sqlite/037_episode_segments.sql.
-- PK: (item_id, kind, source). Repository.Replace() wraps DELETE+INSERT
-- in a transaction so a re-run of one detector replaces only its own
-- rows; segments from other sources stay untouched.

-- name: InsertEpisodeSegment :exec
INSERT INTO episode_segments (item_id, kind, source, start_ticks, end_ticks, confidence, detected_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: DeleteEpisodeSegmentsByItem :exec
DELETE FROM episode_segments WHERE item_id = ?;

-- name: DeleteEpisodeSegmentsByItemAndSource :exec
DELETE FROM episode_segments WHERE item_id = ? AND source = ?;

-- name: ListEpisodeSegmentsByItem :many
SELECT item_id, kind, source, start_ticks, end_ticks, confidence, detected_at
FROM episode_segments
WHERE item_id = ?
ORDER BY kind, source;
