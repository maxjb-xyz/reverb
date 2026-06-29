-- +goose Up
CREATE TABLE notifications (
    id         TEXT    PRIMARY KEY,
    user_id    TEXT    NOT NULL,
    type       TEXT    NOT NULL,
    title      TEXT    NOT NULL,
    body       TEXT    NOT NULL,
    request_id TEXT,
    read       INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL
);
CREATE INDEX idx_notifications_user_read ON notifications(user_id, read);
CREATE INDEX idx_notifications_request_id ON notifications(request_id);

-- +goose Down
DROP TABLE notifications;
