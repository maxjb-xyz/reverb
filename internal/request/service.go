// Package request manages download-request rows and publishes targeted events.
package request

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/events"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

const (
	TopicCreated  = "request.created"
	TopicUpdated  = "request.updated"
	TopicCanceled = "request.canceled"
)

var (
	ErrNotPending = errors.New("request is not pending")
	ErrForbidden  = errors.New("forbidden")
	ErrNotFound   = errors.New("request not found")
)

// Publisher is the minimal event-bus interface the Service needs. *events.Bus satisfies it.
type Publisher interface {
	Publish(events.Event)
}

// Querier is the persistence slice the service needs. *db.Queries satisfies it.
type Querier interface {
	CreateRequest(ctx context.Context, arg db.CreateRequestParams) error
	GetRequest(ctx context.Context, id string) (db.Request, error)
	GetOpenRequestByItem(ctx context.Context, arg db.GetOpenRequestByItemParams) (db.Request, error)
	GetRequestByDownloadJob(ctx context.Context, downloadJobID sql.NullString) (db.Request, error)
	ListRequestsForOwner(ctx context.Context, requestedBy string) ([]db.Request, error)
	ListRequests(ctx context.Context) ([]db.Request, error)
	ListRequestsByStatus(ctx context.Context, status string) ([]db.Request, error)
	UpdateRequestStatus(ctx context.Context, arg db.UpdateRequestStatusParams) error
	SetRequestStatus(ctx context.Context, arg db.SetRequestStatusParams) error
	DeleteRequest(ctx context.Context, id string) error
	CountPendingRequestsByUser(ctx context.Context, requestedBy string) (int64, error)
}

// Service manages request rows and publishes targeted WebSocket events.
type Service struct {
	q   Querier
	pub Publisher
	now func() time.Time
}

func NewService(q Querier, pub Publisher, now func() time.Time) *Service {
	return &Service{q: q, pub: pub, now: now}
}

// Create inserts a new pending request, or returns the existing open one (dedup).
func (s *Service) Create(ctx context.Context, requestedBy string, item core.RequestItem) (core.Request, bool, error) {
	existing, err := s.q.GetOpenRequestByItem(ctx, db.GetOpenRequestByItemParams{
		RequestedBy: requestedBy,
		Source:      item.Source,
		ExternalID:  item.ExternalID,
	})
	if err == nil {
		return fromRow(existing), true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return core.Request{}, false, err
	}

	id := uuid.NewString()
	kind := item.Kind
	if kind == "" {
		kind = "track"
	}
	params := db.CreateRequestParams{
		ID:          id,
		RequestedBy: requestedBy,
		Source:      item.Source,
		ExternalID:  item.ExternalID,
		Title:       item.Title,
		Artist:      item.Artist,
		Album:       nullStr(item.Album),
		Isrc:        nullStr(item.ISRC),
		DurationMs:  nullInt(int64(item.DurationMs)),
		CoverArtID:  nullStr(item.CoverArtID),
		CoverUrl:   nullStr(item.CoverUrl),
		Kind:       kind,
		TrackCount: int64(item.TrackCount),
		Status:     core.RequestPending,
	}
	if err := s.q.CreateRequest(ctx, params); err != nil {
		return core.Request{}, false, err
	}
	row, err := s.q.GetRequest(ctx, id)
	if err != nil {
		return core.Request{}, false, err
	}
	return fromRow(row), false, nil
}

// NotifyPending publishes a request.created event broadcast to managers.
func (s *Service) NotifyPending(ctx context.Context, req core.Request) {
	s.pub.Publish(events.Event{
		Topic: TopicCreated,
		Payload: core.RequestEvent{
			Request:     req,
			ForManagers: true,
		},
	})
}

// MarkApproved transitions a pending request to approved. Returns ErrNotPending if not pending.
func (s *Service) MarkApproved(ctx context.Context, id, approverID, downloadJobID string) (core.Request, error) {
	row, err := s.q.GetRequest(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.Request{}, ErrNotFound
		}
		return core.Request{}, err
	}
	if row.Status != core.RequestPending {
		return core.Request{}, ErrNotPending
	}
	now := s.now().Unix()
	if err := s.q.UpdateRequestStatus(ctx, db.UpdateRequestStatusParams{
		ID:            id,
		Status:        core.RequestApproved,
		DecidedBy:     nullStr(approverID),
		DecidedAt:     nullInt(now),
		DownloadJobID: nullStr(downloadJobID),
		DenyReason:    sql.NullString{},
	}); err != nil {
		return core.Request{}, err
	}
	return s.getAndPublish(ctx, id)
}

// Deny transitions a pending request to denied. Returns ErrNotPending if not pending.
func (s *Service) Deny(ctx context.Context, id, approverID, reason string) (core.Request, error) {
	row, err := s.q.GetRequest(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.Request{}, ErrNotFound
		}
		return core.Request{}, err
	}
	if row.Status != core.RequestPending {
		return core.Request{}, ErrNotPending
	}
	now := s.now().Unix()
	if err := s.q.UpdateRequestStatus(ctx, db.UpdateRequestStatusParams{
		ID:            id,
		Status:        core.RequestDenied,
		DecidedBy:     nullStr(approverID),
		DecidedAt:     nullInt(now),
		DownloadJobID: sql.NullString{},
		DenyReason:    nullStr(reason),
	}); err != nil {
		return core.Request{}, err
	}
	return s.getAndPublish(ctx, id)
}

// Cancel deletes a pending request owned by requesterID. Returns ErrForbidden if
// requesterID is not the owner or the request is not pending.
// On success, publishes a request.canceled event so the Notifier can resolve
// manager badges for the deleted request.
func (s *Service) Cancel(ctx context.Context, id, requesterID string) error {
	row, err := s.q.GetRequest(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if row.RequestedBy != requesterID {
		return ErrForbidden
	}
	if row.Status != core.RequestPending {
		return ErrForbidden
	}
	if err := s.q.DeleteRequest(ctx, id); err != nil {
		return err
	}
	s.pub.Publish(events.Event{
		Topic:   TopicCanceled,
		Payload: core.RequestEvent{Request: fromRow(row)},
	})
	return nil
}

// MarkFulfilled sets status to fulfilled and publishes request.updated.
// Uses SetRequestStatus (status-only) to preserve decided_by/decided_at/download_job_id.
// No-ops (without publishing) if the request is already in a terminal state (fulfilled).
func (s *Service) MarkFulfilled(ctx context.Context, id string) error {
	row, err := s.q.GetRequest(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if row.Status == core.RequestFulfilled {
		return nil
	}
	if err := s.q.SetRequestStatus(ctx, db.SetRequestStatusParams{
		ID:     id,
		Status: core.RequestFulfilled,
	}); err != nil {
		return err
	}
	row, err = s.q.GetRequest(ctx, id)
	if err != nil {
		return err
	}
	s.publishUpdated(fromRow(row))
	return nil
}

// MarkFailed sets status to failed and publishes request.updated.
// Uses SetRequestStatus (status-only) to preserve decided_by/decided_at/download_job_id.
// No-ops (without publishing) if the request is already in a terminal state (failed or fulfilled).
func (s *Service) MarkFailed(ctx context.Context, id, errMsg string) error {
	row, err := s.q.GetRequest(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if row.Status == core.RequestFailed || row.Status == core.RequestFulfilled {
		return nil
	}
	if err := s.q.SetRequestStatus(ctx, db.SetRequestStatusParams{
		ID:     id,
		Status: core.RequestFailed,
	}); err != nil {
		return err
	}
	row, err = s.q.GetRequest(ctx, id)
	if err != nil {
		return err
	}
	s.publishUpdated(fromRow(row))
	return nil
}

// Get returns the request by ID, or ErrNotFound.
func (s *Service) Get(ctx context.Context, id string) (core.Request, error) {
	row, err := s.q.GetRequest(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.Request{}, ErrNotFound
		}
		return core.Request{}, err
	}
	return fromRow(row), nil
}

// ListForOwner returns all requests for a given user, newest first.
func (s *Service) ListForOwner(ctx context.Context, userID string) ([]core.Request, error) {
	rows, err := s.q.ListRequestsForOwner(ctx, userID)
	if err != nil {
		return nil, err
	}
	return mapRows(rows), nil
}

// ListAll returns all requests, optionally filtered by status (empty = all).
func (s *Service) ListAll(ctx context.Context, statusFilter string) ([]core.Request, error) {
	if statusFilter == "" {
		rows, err := s.q.ListRequests(ctx)
		if err != nil {
			return nil, err
		}
		return mapRows(rows), nil
	}
	rows, err := s.q.ListRequestsByStatus(ctx, statusFilter)
	if err != nil {
		return nil, err
	}
	return mapRows(rows), nil
}

// CountPending returns the number of pending requests for the given user.
func (s *Service) CountPending(ctx context.Context, userID string) (int64, error) {
	return s.q.CountPendingRequestsByUser(ctx, userID)
}

// GetByDownloadJob returns the request associated with a download job ID, or ErrNotFound.
func (s *Service) GetByDownloadJob(ctx context.Context, jobID string) (core.Request, error) {
	row, err := s.q.GetRequestByDownloadJob(ctx, nullStr(jobID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.Request{}, ErrNotFound
		}
		return core.Request{}, err
	}
	return fromRow(row), nil
}

// --- helpers ---

// getAndPublish reads the freshly-updated row and publishes a request.updated event
// targeted at the original requester.
func (s *Service) getAndPublish(ctx context.Context, id string) (core.Request, error) {
	row, err := s.q.GetRequest(ctx, id)
	if err != nil {
		return core.Request{}, err
	}
	req := fromRow(row)
	s.publishUpdated(req)
	return req, nil
}

func (s *Service) publishUpdated(req core.Request) {
	s.pub.Publish(events.Event{
		Topic: TopicUpdated,
		Payload: core.RequestEvent{
			Request:      req,
			TargetUserID: req.RequestedBy,
		},
	})
}

// fromRow maps a db.Request to core.Request (nullable cols → zero values).
func fromRow(r db.Request) core.Request {
	return core.Request{
		ID:            r.ID,
		RequestedBy:   r.RequestedBy,
		Source:        r.Source,
		ExternalID:    r.ExternalID,
		Title:         r.Title,
		Artist:        r.Artist,
		Album:         r.Album.String,
		ISRC:          r.Isrc.String,
		DurationMs:    int(r.DurationMs.Int64),
		CoverArtID:    r.CoverArtID.String,
		CoverUrl:      r.CoverUrl.String,
		Kind:          r.Kind,
		TrackCount:    int(r.TrackCount),
		Status:        r.Status,
		CreatedAt:     r.CreatedAt,
		DecidedBy:     r.DecidedBy.String,
		DecidedAt:     r.DecidedAt.Int64,
		DownloadJobID: r.DownloadJobID.String,
		DenyReason:    r.DenyReason.String,
	}
}

func mapRows(rows []db.Request) []core.Request {
	out := make([]core.Request, len(rows))
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

func nullInt(n int64) sql.NullInt64 {
	if n == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: n, Valid: true}
}
