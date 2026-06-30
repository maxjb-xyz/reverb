// Package play records user play events and mints catalog IDs for each track.
package play

import (
	"context"
	"time"

	"github.com/maxjb-xyz/reverb/internal/catalog"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// PlayInput carries the metadata and timing for a single play event.
type PlayInput struct {
	LibraryTrackID string
	Title          string
	Artist         string
	Album          string
	ISRC           string
	DurationMs     int
	MsPlayed       int
	Completed      bool
	PlayedAt       int64 // unix seconds; 0 means "use now"
}

// Querier is the narrow persistence slice play needs. *db.Queries satisfies it.
type Querier interface {
	InsertPlay(ctx context.Context, arg db.InsertPlayParams) error
	DeletePlay(ctx context.Context, arg db.DeletePlayParams) error
}

// CanonicalMinter resolves or mints a catalog ID for an identity.
// *catalog.Service satisfies this interface.
type CanonicalMinter interface {
	CanonicalFor(ctx context.Context, id catalog.Identity) (string, error)
}

// Service records user play events.
type Service struct {
	q     Querier
	cat   CanonicalMinter
	now   func() time.Time
	idgen func() string
}

// NewService constructs a Service.
func NewService(q Querier, cat CanonicalMinter, now func() time.Time, idgen func() string) *Service {
	return &Service{q: q, cat: cat, now: now, idgen: idgen}
}

// Record mints a catalog ID for the given track and inserts a play row scoped
// to userID. Source and ExternalID are intentionally left empty: the FE Track
// has no track-level external id, so these are pure-library entities.
func (s *Service) Record(ctx context.Context, userID string, in PlayInput) error {
	cid, err := s.cat.CanonicalFor(ctx, catalog.Identity{
		Kind:       "track",
		Title:      in.Title,
		Artist:     in.Artist,
		Album:      in.Album,
		ISRC:       in.ISRC,
		DurationMs: in.DurationMs,
		// Source and ExternalID intentionally empty.
	})
	if err != nil {
		return err
	}

	played := in.PlayedAt
	if played == 0 {
		played = s.now().Unix()
	}

	completed := int64(0)
	if in.Completed {
		completed = 1
	}

	return s.q.InsertPlay(ctx, db.InsertPlayParams{
		ID:        s.idgen(),
		UserID:    userID,
		CatalogID: cid,
		PlayedAt:  played,
		MsPlayed:  int64(in.MsPlayed),
		Completed: completed,
		CreatedAt: s.now().Unix(),
	})
}

// Delete removes a single play owned by userID. Owner-scoping is enforced in the
// query (WHERE id = ? AND user_id = ?): a request for another user's play id
// matches zero rows and is a no-op — a user can NEVER delete another user's play.
func (s *Service) Delete(ctx context.Context, userID, playID string) error {
	return s.q.DeletePlay(ctx, db.DeletePlayParams{ID: playID, UserID: userID})
}
