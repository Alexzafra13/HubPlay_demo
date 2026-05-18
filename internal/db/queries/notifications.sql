-- Notifications: inbox por usuario. Feature-agnostico - el discriminador
-- es la columna `kind` (string libre) y el `payload` JSON. Schema en
-- migrations/sqlite/049.

-- name: InsertNotification :exec
INSERT INTO notifications
    (id, user_id, kind, title, body, link, payload, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListNotificationsForUser :many
-- Listado del dropdown: ultimas N notificaciones del usuario, mas
-- recientes arriba. El frontend pagina con limit alto (50) - el
-- dropdown solo muestra ~10, pero el "ver todas" usa la misma query.
SELECT id, user_id, kind, title, body, link, payload, created_at, read_at
FROM notifications
WHERE user_id = ?
ORDER BY created_at DESC
LIMIT ?;

-- name: CountUnreadNotificationsForUser :one
-- Lo consulta el badge del header cada vez que llega un evento de
-- notification.created por SSE. El indice parcial sobre read_at IS NULL
-- (migration 049) hace que esto sea O(1) incluso con miles de leidas
-- acumuladas.
SELECT COUNT(*) FROM notifications
WHERE user_id = ? AND read_at IS NULL;

-- name: MarkNotificationRead :execrows
-- execrows: el handler distingue "ya estaba leida o no existia" de
-- "marcada ahora" para no devolver 404 espurio.
UPDATE notifications
SET read_at = ?
WHERE id = ? AND user_id = ? AND read_at IS NULL;

-- name: MarkAllNotificationsReadForUser :execrows
UPDATE notifications
SET read_at = ?
WHERE user_id = ? AND read_at IS NULL;

-- name: DeleteOldReadNotifications :execrows
-- Limpieza periodica: borrar las leidas con mas de 30 dias para que
-- la tabla no crezca sin limite. Las no-leidas se conservan siempre.
DELETE FROM notifications
WHERE read_at IS NOT NULL AND read_at < ?;
