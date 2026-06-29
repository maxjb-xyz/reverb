// Package notification manages in-app notification rows.
package notification

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// Querier is the persistence slice the Service needs. *db.Queries satisfies it.
type Querier interface {
	CreateNotification(ctx context.Context, arg db.CreateNotificationParams) error
	ListNotificationsForUser(ctx context.Context, arg db.ListNotificationsForUserParams) ([]db.Notification, error)
	CountUnreadForUser(ctx context.Context, userID string) (int64, error)
	MarkNotificationsRead(ctx context.Context, arg db.MarkNotificationsReadParams) error
	MarkAllReadForUser(ctx context.Context, userID string) error
	MarkPendingResolvedForRequest(ctx context.Context, requestID sql.NullString) error
}

// Service manages notification rows.
type Service struct {
	q   Querier
	now func() time.Time
}

// NewService constructs a Service.
func NewService(q Querier, now func() time.Time) *Service {
	return &Service{q: q, now: now}
}

// Create inserts a notification, generating an id and createdAt if absent.
func (s *Service) Create(ctx context.Context, n core.Notification) (core.Notification, error) {
	if n.ID == "" {
		n.ID = uuid.NewString()
	}
	if n.CreatedAt == 0 {
		n.CreatedAt = s.now().Unix()
	}
	var readInt int64
	if n.Read {
		readInt = 1
	}
	params := db.CreateNotificationParams{
		ID:        n.ID,
		UserID:    n.UserID,
		Type:      n.Type,
		Title:     n.Title,
		Body:      n.Body,
		RequestID: nullStr(n.RequestID),
		Read:      readInt,
		CreatedAt: n.CreatedAt,
	}
	if err := s.q.CreateNotification(ctx, params); err != nil {
		return core.Notification{}, err
	}
	return n, nil
}

// ListForUser returns notifications for a user, newest first, up to limit rows.
func (s *Service) ListForUser(ctx context.Context, userID string, limit int) ([]core.Notification, error) {
	rows, err := s.q.ListNotificationsForUser(ctx, db.ListNotificationsForUserParams{
		UserID: userID,
		Limit:  int64(limit),
	})
	if err != nil {
		return nil, err
	}
	return mapRows(rows), nil
}

// CountUnread returns the number of unread notifications for a user.
func (s *Service) CountUnread(ctx context.Context, userID string) (int, error) {
	n, err := s.q.CountUnreadForUser(ctx, userID)
	return int(n), err
}

// MarkRead marks the given notification ids as read, scoped to userID so
// cross-user tampering is a no-op.
func (s *Service) MarkRead(ctx context.Context, userID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return s.q.MarkNotificationsRead(ctx, db.MarkNotificationsReadParams{
		UserID: userID,
		Ids:    ids,
	})
}

// MarkAllRead marks all of a user's notifications as read.
func (s *Service) MarkAllRead(ctx context.Context, userID string) error {
	return s.q.MarkAllReadForUser(ctx, userID)
}

// ResolvePendingForRequest marks request_pending notifications for the given
// request as read (used when the request transitions away from pending).
func (s *Service) ResolvePendingForRequest(ctx context.Context, requestID string) error {
	return s.q.MarkPendingResolvedForRequest(ctx, nullStr(requestID))
}

// --- helpers ---

func fromRow(r db.Notification) core.Notification {
	return core.Notification{
		ID:        r.ID,
		UserID:    r.UserID,
		Type:      r.Type,
		Title:     r.Title,
		Body:      r.Body,
		RequestID: r.RequestID.String,
		Read:      r.Read != 0,
		CreatedAt: r.CreatedAt,
	}
}

func mapRows(rows []db.Notification) []core.Notification {
	out := make([]core.Notification, len(rows))
	for i, r := range rows {
		out[i] = fromRow(r)
	}
	return out
}

func nullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
