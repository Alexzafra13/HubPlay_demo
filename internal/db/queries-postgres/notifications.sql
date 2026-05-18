-- Notifications: inbox por usuario. Ver hermano SQLite para rationale.

-- name: InsertNotification :exec
INSERT INTO notifications
    (id, user_id, kind, title, body, link, payload, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: ListNotificationsForUser :many
SELECT id, user_id, kind, title, body, link, payload, created_at, read_at
FROM notifications
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: CountUnreadNotificationsForUser :one
SELECT COUNT(*) FROM notifications
WHERE user_id = $1 AND read_at IS NULL;

-- name: MarkNotificationRead :execrows
UPDATE notifications
SET read_at = $1
WHERE id = $2 AND user_id = $3 AND read_at IS NULL;

-- name: MarkAllNotificationsReadForUser :execrows
UPDATE notifications
SET read_at = $1
WHERE user_id = $2 AND read_at IS NULL;

-- name: DeleteOldReadNotifications :execrows
DELETE FROM notifications
WHERE read_at IS NOT NULL AND read_at < $1;
