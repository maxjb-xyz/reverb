// Package resolver maps catalog IDs to current backend addressing via a
// cache-first strategy backed by the backend_binding table. It uses a
// per-identity epoch (settings key "binding_epoch:<identity>") to decide
// freshness, so a library swap (identity change) automatically invalidates
// all bindings without a table scan. Concurrent resolves for the same catalog
// ID are collapsed by singleflight so the matcher is called at most once per
// (catalogID, epoch) pair.
package resolver

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// Addressing is the resolved backend location for a catalog entity.
type Addressing struct {
	BackendID  string
	CoverArtID string
	Found      bool
}

// Rematcher is satisfied by *matching.Service. Using an interface keeps the
// resolver import-free from the matching package and supports the provider
// pattern below.
type Rematcher interface {
	Match(ctx context.Context, ext core.ExternalResult) (core.MatchResult, error)
}

// Querier is the subset of *db.Queries consumed by the resolver.
type Querier interface {
	GetCatalogEntity(ctx context.Context, id string) (db.CatalogEntity, error)
	GetBackendBinding(ctx context.Context, arg db.GetBackendBindingParams) (db.BackendBinding, error)
	UpsertBackendBinding(ctx context.Context, arg db.UpsertBackendBindingParams) error
	GetSetting(ctx context.Context, key string) (string, error)
	UpsertSetting(ctx context.Context, arg db.UpsertSettingParams) error
}

// Service resolves catalog IDs to backend addressing.
type Service struct {
	q       Querier
	matcher func() Rematcher // provider, not a fixed ref — avoids dead-adapter holding
	now     func() time.Time
	sf      singleflight.Group
}

// NewService creates a resolver Service. matcher is a PROVIDER func — the
// resolver calls matcher() per-resolve so it always reaches the current adapter.
func NewService(q Querier, matcher func() Rematcher, now func() time.Time) *Service {
	return &Service{q: q, matcher: matcher, now: now}
}

// identity returns the current library identity string. Returns "" with no
// error if the key is absent (ErrNoRows).
func (s *Service) identity(ctx context.Context) (string, error) {
	v, err := s.q.GetSetting(ctx, "library_identity")
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return v, nil
}

// epoch returns the current binding epoch for the given identity. Defaults to
// 1 when the key is absent or unparseable. Uses epochKey so the read path is
// always in sync with the write path in BumpEpoch.
func (s *Service) epoch(ctx context.Context, identity string) int64 {
	v, err := s.q.GetSetting(ctx, epochKey(identity))
	if err != nil {
		return 1
	}
	n, _ := strconv.ParseInt(v, 10, 64)
	if n == 0 {
		return 1
	}
	return n
}

// Resolve returns the current backend addressing for catalogID.
//
//   - Cache HIT  (binding_epoch >= curEpoch AND backend_id != ""):  return stored addressing.
//   - Negative cache (binding_epoch >= curEpoch AND known_absent=1): return Found:false without re-matching.
//   - Miss/stale: re-match under singleflight, write result back.
func (s *Service) Resolve(ctx context.Context, catalogID string) (Addressing, error) {
	identity, err := s.identity(ctx)
	if err != nil {
		return Addressing{}, err
	}
	curEpoch := s.epoch(ctx, identity)

	b, err := s.q.GetBackendBinding(ctx, db.GetBackendBindingParams{
		CatalogID:       catalogID,
		LibraryIdentity: identity,
	})
	if err == nil && b.BindingEpoch >= curEpoch {
		// Negative cache short-circuit.
		if b.KnownAbsent == 1 {
			return Addressing{Found: false}, nil
		}
		// Positive cache hit.
		if b.BackendID != "" {
			return Addressing{BackendID: b.BackendID, CoverArtID: b.CoverArtID, Found: true}, nil
		}
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Addressing{}, err
	}

	// Miss or stale — re-match under singleflight keyed by catalogID.
	// Detach the context for the flight: only the FIRST caller's goroutine runs
	// the closure, so if that caller's request context is cancelled mid-flight
	// (e.g. an abandoned HTTP request) the matcher call + cache write-back would
	// abort, and every collapsed waiter — with still-valid contexts — would get
	// the cancellation error. WithoutCancel preserves values (trace IDs) while
	// severing cancellation so the shared work + write-back always complete.
	detached := context.WithoutCancel(ctx)
	v, sfErr, _ := s.sf.Do(catalogID, func() (any, error) {
		return s.rematchAndStore(detached, catalogID, identity, curEpoch)
	})
	if sfErr != nil {
		return Addressing{}, sfErr
	}
	return v.(Addressing), nil
}

// rematchAndStore calls the current matcher and writes the result back to
// backend_binding. On a miss it writes known_absent=1 stamped at the current
// epoch so subsequent Resolve calls short-circuit.
func (s *Service) rematchAndStore(ctx context.Context, catalogID, identity string, epoch int64) (Addressing, error) {
	e, err := s.q.GetCatalogEntity(ctx, catalogID)
	if err != nil {
		// Unknown canonical id (entity never minted, or deleted): there is no
		// entity to resolve, so report not-found rather than a hard error. The
		// addressing boundary maps Found:false → 404, not 502. Nothing to bind
		// (backend_binding FKs catalog_entity), so we write no binding.
		if errors.Is(err, sql.ErrNoRows) {
			return Addressing{Found: false}, nil
		}
		return Addressing{}, err
	}

	m := s.matcher()
	if m == nil {
		// No library configured — return a benign not-found rather than panicking.
		// The matcher-provider yields nil when no library adapter is live.
		return Addressing{Found: false}, nil
	}
	res, err := m.Match(ctx, core.ExternalResult{
		Source:     e.Source,
		ExternalID: e.ExternalID,
		Title:      e.Title,
		Artist:     e.Artist,
		Album:      e.Album,
		DurationMs: int(e.DurationMs),
		ISRC:       e.Isrc,
		MBID:       e.Mbid,
		Type:       core.EntityTrack,
	})
	if err != nil {
		return Addressing{}, err
	}

	addr := Addressing{}
	bind := db.UpsertBackendBindingParams{
		CatalogID:       catalogID,
		LibraryIdentity: identity,
		BindingEpoch:    epoch,
		ResolvedAt:      s.now().Unix(),
	}

	if res.Status == core.MatchInLibrary && res.LibraryTrackID != "" {
		addr = Addressing{BackendID: res.LibraryTrackID, CoverArtID: res.CoverArtID, Found: true}
		bind.BackendID = res.LibraryTrackID
		bind.CoverArtID = res.CoverArtID
		bind.KnownAbsent = 0
	} else {
		// Negative cache: stamp at current epoch to bound future matcher calls.
		bind.KnownAbsent = 1
	}

	if writeErr := s.q.UpsertBackendBinding(ctx, bind); writeErr != nil {
		return Addressing{}, writeErr
	}
	return addr, nil
}

// BumpEpoch increments the per-identity epoch, causing all bindings for that
// identity to be treated as stale on the next Resolve. Called on library swap.
// Delegates to the package-level BumpEpoch so Service and wiring share one
// implementation of the key format and read-increment-write logic.
func (s *Service) BumpEpoch(ctx context.Context, identity string) error {
	return BumpEpoch(ctx, s.q, identity)
}

// RefreshLinked forces a re-resolve for each given catalog ID at the current
// epoch by marking any existing binding as stale (epoch-1) and then resolving.
// This is used by the scan completion path to refresh a batch of linked IDs.
func (s *Service) RefreshLinked(ctx context.Context, catalogIDs []string) error {
	identity, err := s.identity(ctx)
	if err != nil {
		return err
	}
	curEpoch := s.epoch(ctx, identity)

	for _, catalogID := range catalogIDs {
		// Mark the binding as stale (epoch-1) so Resolve will re-match. A failed
		// write must abort: otherwise a fresh-epoch row survives and the follow-up
		// Resolve returns the STALE cached value, silently defeating RefreshLinked.
		if werr := s.q.UpsertBackendBinding(ctx, db.UpsertBackendBindingParams{
			CatalogID:       catalogID,
			LibraryIdentity: identity,
			BindingEpoch:    curEpoch - 1,
			KnownAbsent:     0,
			ResolvedAt:      s.now().Unix(),
		}); werr != nil {
			return werr
		}
		if _, rerr := s.Resolve(ctx, catalogID); rerr != nil {
			return rerr
		}
	}
	return nil
}
