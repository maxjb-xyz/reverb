-- name: CreateNotification :exec
INSERT INTO notifications (id, user_id, type, title, body, request_id, read, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListNotificationsForUser :many
SELECT * FROM notifications
WHERE user_id = ?
ORDER BY created_at DESC
LIMIT ?;

-- name: CountUnreadForUser :one
SELECT COUNT(*) FROM notifications
WHERE user_id = ? AND read = 0;

-- name: MarkAllReadForUser :exec
UPDATE notifications SET read = 1 WHERE user_id = ?;

-- name: MarkNotificationsRead :exec
UPDATE notifications SET read = 1
WHERE user_id = ? AND id IN (sqlc.slice('ids'));

-- name: MarkPendingResolvedForRequest :exec
UPDATE notifications SET read = 1
WHERE request_id = ? AND type = 'request_pending';
