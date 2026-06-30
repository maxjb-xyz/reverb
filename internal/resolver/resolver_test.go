package resolver

import (
	"context"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/store"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// fakeMatcher records how many times Match is called and returns a fixed result.
type fakeMatcher struct {
	calls  int
	result core.MatchResult
}

func (f *fakeMatcher) Match(_ context.Context, _ core.ExternalResult) (core.MatchResult, error) {
	f.calls++
	return f.result, nil
}

// openStore opens a migrated in-memory sqlite store for testing.
func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/resolver.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	return st
}

// seedEntity inserts a catalog_entity row directly and returns its id.
func seedEntity(t *testing.T, q *db.Queries, externalID, title, artist, album string, durationMs int64) string {
	t.Helper()
	id := "trk_" + externalID
	err := q.InsertCatalogEntity(context.Background(), db.InsertCatalogEntityParams{
		ID:         id,
		Kind:       "track",
		Title:      title,
		Artist:     artist,
		Album:      album,
		DurationMs: durationMs,
		Isrc:       "",
		Mbid:       "",
		Source:     "spotify",
		ExternalID: externalID,
		CreatedAt:  time.Now().Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// newTestResolver returns (service, queries, *fakeMatcher) with matcher set to a HIT result.
func newTestResolver(t *testing.T) (*Service, *db.Queries, *fakeMatcher) {
	t.Helper()
	st := openStore(t)
	q := st.Q()
	fm := &fakeMatcher{
		result: core.MatchResult{
			Status:         core.MatchInLibrary,
			LibraryTrackID: "nav-1",
			CoverArtID:     "al-1",
		},
	}
	svc := NewService(q, func() Rematcher { return fm }, time.Now)
	return svc, q, fm
}

// newTestResolverMiss returns (service, queries, *fakeMatcher) with matcher set to a MISS result.
func newTestResolverMiss(t *testing.T) (*Service, *db.Queries, *fakeMatcher) {
	t.Helper()
	st := openStore(t)
	q := st.Q()
	fm := &fakeMatcher{
		result: core.MatchResult{
			Status: core.MatchNotInLibrary,
		},
	}
	svc := NewService(q, func() Rematcher { return fm }, time.Now)
	return svc, q, fm
}

func TestResolve_CachesAndDoesNotRematchOnHit(t *testing.T) {
	s, q, fm := newTestResolver(t)
	ctx := context.Background()
	cid := seedEntity(t, q, "trk_x", "Song", "Artist", "Album", 200000)

	a1, _ := s.Resolve(ctx, cid)
	if !a1.Found || a1.BackendID != "nav-1" {
		t.Fatalf("resolve miss: %+v", a1)
	}
	a2, _ := s.Resolve(ctx, cid)
	if a2.BackendID != "nav-1" {
		t.Fatal("second resolve wrong")
	}
	if fm.calls != 1 {
		t.Fatalf("expected 1 re-match (cached after), got %d", fm.calls)
	}
}

func TestResolve_NegativeCacheBoundsRematch(t *testing.T) {
	s, q, fm := newTestResolverMiss(t)
	ctx := context.Background()
	cid := seedEntity(t, q, "trk_y", "Gone", "Artist", "Album", 200000)
	for i := 0; i < 3; i++ {
		a, _ := s.Resolve(ctx, cid)
		if a.Found {
			t.Fatal("should be not-found")
		}
	}
	if fm.calls != 1 {
		t.Fatalf("known_absent must bound re-match to 1 per epoch, got %d", fm.calls)
	}
}

func TestResolve_UnknownCatalogIDIsNotFound(t *testing.T) {
	s, _, fm := newTestResolver(t)
	ctx := context.Background()
	// Never seeded → no catalog_entity row. An unknown canonical id must be
	// treated as not-found (→ boundary 404), not a hard error (→ 502), and the
	// matcher must not be consulted (there is nothing to match).
	addr, err := s.Resolve(ctx, "trk_does_not_exist")
	if err != nil {
		t.Fatalf("unknown catalog id must not error (404 not 502): %v", err)
	}
	if addr.Found {
		t.Fatalf("unknown catalog id must be Found:false, got %+v", addr)
	}
	if fm.calls != 0 {
		t.Fatalf("matcher must not be called for a nonexistent entity, got %d", fm.calls)
	}
}

// cancelObservingMatcher cancels the supplied func the moment Match is entered,
// then records whether the context handed to it was already cancelled. This
// proves the singleflight closure runs on a DETACHED context: the resolver must
// not abort the matcher call + cache write-back just because the request that
// triggered the flight was cancelled.
type cancelObservingMatcher struct {
	cancel        context.CancelFunc
	result        core.MatchResult
	sawCancelled  bool
	gotCtxErrText string
}

func (m *cancelObservingMatcher) Match(ctx context.Context, _ core.ExternalResult) (core.MatchResult, error) {
	m.cancel() // simulate the caller abandoning the request mid-flight
	if err := ctx.Err(); err != nil {
		m.sawCancelled = true
		m.gotCtxErrText = err.Error()
	}
	return m.result, nil
}

func TestResolve_DetachesContextForFlight(t *testing.T) {
	st := openStore(t)
	q := st.Q()
	cid := seedEntity(t, q, "trk_z", "Detach", "Artist", "Album", 200000)

	ctx, cancel := context.WithCancel(context.Background())
	m := &cancelObservingMatcher{
		cancel: cancel,
		result: core.MatchResult{Status: core.MatchInLibrary, LibraryTrackID: "nav-9", CoverArtID: "al-9"},
	}
	s := NewService(q, func() Rematcher { return m }, time.Now)

	// The matcher cancels ctx as soon as it runs. With the fix, the flight is on
	// a detached context, so the matcher sees a live context, the binding is
	// written, and Resolve returns the resolved Addressing.
	a, err := s.Resolve(ctx, cid)
	if err != nil {
		t.Fatalf("Resolve returned error despite cancellation during flight: %v", err)
	}
	if !a.Found || a.BackendID != "nav-9" {
		t.Fatalf("expected resolved addressing, got %+v", a)
	}
	if m.sawCancelled {
		t.Fatalf("matcher ran on a cancelled context (%s); flight context not detached", m.gotCtxErrText)
	}

	// And the write-back must have persisted at the current epoch so a follow-up
	// resolve is a pure cache hit (no second matcher call). Use a fresh context.
	a2, err := s.Resolve(context.Background(), cid)
	if err != nil || !a2.Found || a2.BackendID != "nav-9" {
		t.Fatalf("write-back did not persist; follow-up resolve = %+v err=%v", a2, err)
	}
}
