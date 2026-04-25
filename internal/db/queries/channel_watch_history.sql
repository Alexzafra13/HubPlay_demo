-- Per-user "continue watching" history for LiveTV channels.
--
-- Table schema: migrations/sqlite/012_channel_watch_history.sql.
-- Composite PK: (user_id, stream_url).
--
-- Keyed by stream_url so the rail survives M3U refreshes (channel
-- UUIDs regenerate). The list query joins back to `channels` by
-- stream_url at read time.

-- name: RecordChannelWatch :exec
INSERT INTO channel_watch_history (user_id, stream_url, last_watched_at)
VALUES (?, ?, ?)
ON CONFLICT(user_id, stream_url) DO UPDATE SET
   last_watched_at = excluded.last_watched_at;

-- name: DeleteChannelWatch :exec
DELETE FROM channel_watch_history WHERE user_id = ? AND stream_url = ?;

-- ListChannelWatchHistoryByUser joins onto active channels in
-- recency order. Filters applied in SQL so the handler cannot
-- accidentally bypass them: c.is_active = 1 (deactivated channels
-- drop off the rail), and orphan history rows (stream_url no longer
-- in any playlist) are joined out naturally.
--
-- limit is doubled at the call site to absorb stream_url duplicates
-- across libraries; the repo dedupes in Go.

-- name: ListChannelWatchHistoryByUser :many
SELECT c.id, c.library_id, c.name, c.number,
       COALESCE(c.group_name,'') AS group_name,
       COALESCE(c.logo_url,'') AS logo_url, c.stream_url,
       COALESCE(c.tvg_id,'') AS tvg_id,
       COALESCE(c.language,'') AS language,
       COALESCE(c.country,'') AS country,
       c.is_active, c.added_at,
       h.last_watched_at
FROM   channel_watch_history h
JOIN   channels c ON c.stream_url = h.stream_url
WHERE  h.user_id = ?
  AND  c.is_active = 1
ORDER BY h.last_watched_at DESC, c.library_id ASC
LIMIT  ?;
