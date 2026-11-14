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
	UpdatePlayProgress(ctx context.Context, arg db.UpdatePlayProgressParams) error
	DeletePlay(ctx context.Context, arg db.DeletePlayParams) error
	CountPlaysByCatalog(ctx context.Context, arg db.CountPlaysByCatalogParams) (int64, error)
}

// CanonicalMinter resolves or mints a catalog ID for an identity, and resolves
// (lookup-only, no minting) an identity to an existing catalog ID.
// *catalog.Service satisfies this interface.
type CanonicalMinter interface {
	CanonicalFor(ctx context.Context, id catalog.Identity) (string, error)
	Lookup(ctx context.Context, id catalog.Identity) (catalogID string, found bool, err error)
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
	_, err := s.RecordPlay(ctx, userID, in)
	return err
}

// RecordPlay is Record that also returns the new play's id, so the caller can
// later report the final listened time with UpdateProgress.
func (s *Service) RecordPlay(ctx context.Context, userID string, in PlayInput) (string, error) {
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
		return "", err
	}

	played := in.PlayedAt
	if played == 0 {
		played = s.now().Unix()
	}

	completed := int64(0)
	if in.Completed {
		completed = 1
	}

	id := s.idgen()
	if err := s.q.InsertPlay(ctx, db.InsertPlayParams{
		ID:        id,
		UserID:    userID,
		CatalogID: cid,
		PlayedAt:  played,
		MsPlayed:  int64(in.MsPlayed),
		Completed: completed,
		CreatedAt: s.now().Unix(),
	}); err != nil {
		return "", err
	}
	return id, nil
}

// UpdateProgress records the final listened time of a play owned by userID.
// A play is recorded as soon as it qualifies (half the song or four minutes),
// so without this every play would count only the time heard up to that point.
// Values only ever go up, so a late or repeated update cannot shrink a play,
// and the query is owner-scoped like Delete: another user's id is a no-op.
func (s *Service) UpdateProgress(ctx context.Context, userID, playID string, msPlayed int, completed bool) error {
	if msPlayed < 0 {
		msPlayed = 0
	}
	c := int64(0)
	if completed {
		c = 1
	}
	return s.q.UpdatePlayProgress(ctx, db.UpdatePlayProgressParams{
		MsPlayed:  int64(msPlayed),
		Completed: c,
		ID:        playID,
		UserID:    userID,
	})
}

// PlayCountQuery is a single track identity for which the caller wants a play
// count. Key is an opaque caller-supplied handle echoed back in the result map.
type PlayCountQuery struct {
	Key        string
	Title      string
	Artist     string
	Album      string
	ISRC       string
	DurationMs int
}

// PlayCounts returns the all-time play count for each requested track identity,
// scoped to userID. For each item it resolves the canonical catalog id via a
// LOOKUP-ONLY path (no minting): an identity that has never been recorded
// resolves to no entity and counts 0. The result maps each item's Key to its
// count. Identities build the SAME catalog.Identity that Record builds (Kind
// "track", Source/ExternalID empty), so a played track resolves to the same
// entity it was recorded under.
func (s *Service) PlayCounts(ctx context.Context, userID string, items []PlayCountQuery) (map[string]int, error) {
	out := make(map[string]int, len(items))
	for _, it := range items {
		cid, found, err := s.cat.Lookup(ctx, catalog.Identity{
			Kind:       "track",
			Title:      it.Title,
			Artist:     it.Artist,
			Album:      it.Album,
			ISRC:       it.ISRC,
			DurationMs: it.DurationMs,
			// Source and ExternalID intentionally empty, exactly like Record.
		})
		if err != nil {
			return nil, err
		}
		if !found {
			out[it.Key] = 0
			continue
		}
		n, err := s.q.CountPlaysByCatalog(ctx, db.CountPlaysByCatalogParams{
			UserID:    userID,
			CatalogID: cid,
		})
		if err != nil {
			return nil, err
		}
		out[it.Key] = int(n)
	}
	return out, nil
}

// Delete removes a single play owned by userID. Owner-scoping is enforced in the
// query (WHERE id = ? AND user_id = ?): a request for another user's play id
// matches zero rows and is a no-op — a user can NEVER delete another user's play.
func (s *Service) Delete(ctx context.Context, userID, playID string) error {
	return s.q.DeletePlay(ctx, db.DeletePlayParams{ID: playID, UserID: userID})
}
