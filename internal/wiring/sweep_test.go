package wiring

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxjb-xyz/reverb/internal/resolver"
	"github.com/maxjb-xyz/reverb/internal/store"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// sweepResolver is a test BindingResolver that records which catalog IDs were
// Resolved and can be configured to return an error for all calls.
type sweepResolver struct {
	mu         sync.Mutex
	resolved   []string
	resolveErr error
	wg         sync.WaitGroup // callers call wg.Add(n) before triggering sweep
}

func (r *sweepResolver) Resolve(ctx context.Context, catalogID string) (resolver.Addressing, error) {
	r.mu.Lock()
	r.resolved = append(r.resolved, catalogID)
	r.mu.Unlock()
	r.wg.Done()
	if r.resolveErr != nil {
		return resolver.Addressing{}, r.resolveErr
	}
	return resolver.Addressing{Found: false}, nil
}

func (r *sweepResolver) RefreshLinked(ctx context.Context, catalogIDs []string) error {
	return nil
}

func (r *sweepResolver) resolvedIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.resolved))
	copy(out, r.resolved)
	return out
}

// seedPlay inserts a catalog_entity and a plays row for the given catalogID.
func seedPlay(t *testing.T, st *store.Store, catalogID string) {
	t.Helper()
	ctx := context.Background()
	if err := st.Q().InsertCatalogEntity(ctx, db.InsertCatalogEntityParams{
		ID:         catalogID,
		Kind:       "track",
		Source:     "test",
		ExternalID: catalogID,
		Title:      "T",
		Artist:     "A",
		Album:      "",
		DurationMs: 0,
		CreatedAt:  1000,
	}); err != nil {
		t.Fatalf("insert catalog_entity %q: %v", catalogID, err)
	}
	if err := st.Q().InsertPlay(ctx, db.InsertPlayParams{
		ID:        uuid.NewString(),
		UserID:    "u1",
		CatalogID: catalogID,
		PlayedAt:  1000,
		MsPlayed:  30000,
		Completed: 1,
		CreatedAt: 1000,
	}); err != nil {
		t.Fatalf("insert play %q: %v", catalogID, err)
	}
}

// seedDownloadJob inserts a download_job row with the given canonical_id.
func seedDownloadJob(t *testing.T, st *store.Store, canonicalID string) {
	t.Helper()
	ctx := context.Background()
	jobID := uuid.NewString()
	if err := st.Q().InsertDownloadJob(ctx, db.InsertDownloadJobParams{
		ID:             jobID,
		DedupKey:       canonicalID,
		RequestJson:    "{}",
		DownloaderName: "spotdl",
		Status:         "completed",
	}); err != nil {
		t.Fatalf("insert download_job %q: %v", canonicalID, err)
	}
	// Set canonical_id via the dedicated update query (canonical_id has a default
	// of '' and is not part of the INSERT columns).
	if err := st.Q().UpdateDownloadJobCanonicalID(ctx, db.UpdateDownloadJobCanonicalIDParams{
		CanonicalID: canonicalID,
		ID:          jobID,
	}); err != nil {
		t.Fatalf("update canonical_id for %q: %v", canonicalID, err)
	}
}

// TestPostSwapSweep_PreresolvesOnIdentityChange asserts that when the library
// identity changes, reconcileLibraryIdentity launches a sweep that calls
// Resolve for each durable canonical id (plays + download_jobs).
func TestPostSwapSweep_PreresolvesOnIdentityChange(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	// Seed two durable catalog IDs — one from plays, one from download_jobs.
	playCatalogID := "cat-plays-1"
	djCanonicalID := "cat-dj-1"
	seedPlay(t, st, playCatalogID)
	seedDownloadJob(t, st, djCanonicalID)

	// Pre-seed an old identity so reconcile sees a change.
	if err := st.Q().UpsertSetting(ctx, db.UpsertSettingParams{
		Key: settingLibraryIdentity, Value: "external:old",
	}); err != nil {
		t.Fatal(err)
	}

	res := &sweepResolver{}
	// Expect exactly 2 Resolve calls (one per durable id).
	res.wg.Add(2)

	b := &Builder{
		queries:          st.Q(),
		version:          st,
		resolverProvider: func() BindingResolver { return res },
	}

	if err := b.reconcileLibraryIdentity(ctx, "builtin"); err != nil {
		t.Fatalf("reconcileLibraryIdentity: %v", err)
	}

	// Wait for the async sweep to complete with a timeout.
	done := make(chan struct{})
	go func() { res.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for sweep to complete")
	}

	got := res.resolvedIDs()
	want := map[string]bool{playCatalogID: true, djCanonicalID: true}
	if len(got) != len(want) {
		t.Fatalf("Resolve called for %v, want ids %v", got, want)
	}
	for _, id := range got {
		if !want[id] {
			t.Errorf("unexpected Resolve call for %q", id)
		}
	}
}

// TestPostSwapSweep_ResolveErrorDoesNotFailReconcile asserts that a resolver
// that always errors does not cause reconcileLibraryIdentity to return an error.
func TestPostSwapSweep_ResolveErrorDoesNotFailReconcile(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	playCatalogID := "cat-err-1"
	seedPlay(t, st, playCatalogID)

	if err := st.Q().UpsertSetting(ctx, db.UpsertSettingParams{
		Key: settingLibraryIdentity, Value: "external:old",
	}); err != nil {
		t.Fatal(err)
	}

	res := &sweepResolver{resolveErr: errors.New("matcher down")}
	res.wg.Add(1)

	b := &Builder{
		queries:          st.Q(),
		version:          st,
		resolverProvider: func() BindingResolver { return res },
	}

	// reconcile must return nil regardless of resolver errors.
	if err := b.reconcileLibraryIdentity(ctx, "builtin"); err != nil {
		t.Fatalf("reconcileLibraryIdentity returned error on resolver failure: %v", err)
	}

	// Let sweep finish so we don't pollute later tests.
	done := make(chan struct{})
	go func() { res.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for sweep (error path)")
	}
}

// TestPostSwapSweep_NilResolverProviderNoSweepNoPanic asserts that when
// resolverProvider is nil, reconcileLibraryIdentity succeeds without panicking.
func TestPostSwapSweep_NilResolverProviderNoSweepNoPanic(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	if err := st.Q().UpsertSetting(ctx, db.UpsertSettingParams{
		Key: settingLibraryIdentity, Value: "external:old",
	}); err != nil {
		t.Fatal(err)
	}

	b := &Builder{
		queries:          st.Q(),
		version:          st,
		resolverProvider: nil, // nil provider — no sweep should be attempted
	}

	if err := b.reconcileLibraryIdentity(ctx, "builtin"); err != nil {
		t.Fatalf("reconcileLibraryIdentity with nil resolverProvider: %v", err)
	}
}

// TestPostSwapSweep_NilResolverReturnNoSweepNoPanic asserts that when
// resolverProvider returns nil, reconcileLibraryIdentity succeeds without panicking.
func TestPostSwapSweep_NilResolverReturnNoSweepNoPanic(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	if err := st.Q().UpsertSetting(ctx, db.UpsertSettingParams{
		Key: settingLibraryIdentity, Value: "external:old",
	}); err != nil {
		t.Fatal(err)
	}

	b := &Builder{
		queries:          st.Q(),
		version:          st,
		resolverProvider: func() BindingResolver { return nil }, // returns nil resolver
	}

	if err := b.reconcileLibraryIdentity(ctx, "builtin"); err != nil {
		t.Fatalf("reconcileLibraryIdentity with nil BindingResolver return: %v", err)
	}
}

// TestPostSwapSweep_NoChangeDoesNotSweep asserts that when the identity is
// unchanged, no sweep is launched (epoch unchanged, no Resolve calls).
func TestPostSwapSweep_NoChangeDoesNotSweep(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	playCatalogID := "cat-nochg-1"
	seedPlay(t, st, playCatalogID)

	// Seed identity as already matching — reconcile is a pure no-op.
	if err := st.Q().UpsertSetting(ctx, db.UpsertSettingParams{
		Key: settingLibraryIdentity, Value: "builtin",
	}); err != nil {
		t.Fatal(err)
	}

	res := &sweepResolver{}
	// No wg.Add — if Resolve is called we detect it via resolvedIDs check.

	b := &Builder{
		queries:          st.Q(),
		version:          st,
		resolverProvider: func() BindingResolver { return res },
	}

	if err := b.reconcileLibraryIdentity(ctx, "builtin"); err != nil {
		t.Fatalf("reconcileLibraryIdentity: %v", err)
	}

	// Give any inadvertent goroutine time to fire.
	time.Sleep(100 * time.Millisecond)

	if ids := res.resolvedIDs(); len(ids) > 0 {
		t.Errorf("Resolve called on no-change path for ids %v, want no calls", ids)
	}
}

// TestPostSwapSweep_Bounded asserts that the sweep is bounded: the query LIMIT
// means at most sweepLimit IDs are resolved even with a large library.
// We seed n=10 ids (below sweepLimit=500) to verify the sweep processes all of
// them and no more than sweepLimit overall.
func TestPostSwapSweep_Bounded(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	const n = 10
	for i := 0; i < n; i++ {
		seedPlay(t, st, uuid.NewString())
	}

	if err := st.Q().UpsertSetting(ctx, db.UpsertSettingParams{
		Key: settingLibraryIdentity, Value: "external:old",
	}); err != nil {
		t.Fatal(err)
	}

	res := &sweepResolver{}
	// We seeded n ids, so expect exactly n Resolve calls.
	res.wg.Add(n)

	b := &Builder{
		queries:          st.Q(),
		version:          st,
		resolverProvider: func() BindingResolver { return res },
	}

	if err := b.reconcileLibraryIdentity(ctx, "builtin"); err != nil {
		t.Fatalf("reconcileLibraryIdentity: %v", err)
	}

	done := make(chan struct{})
	go func() { res.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for bounded sweep")
	}

	got := res.resolvedIDs()
	// Exactly n ids should be resolved (all seeded, well within sweepLimit).
	if len(got) != n {
		t.Errorf("Resolve called %d times, want %d", len(got), n)
	}
	if len(got) > sweepLimit {
		t.Errorf("Resolve called %d times, exceeds sweepLimit %d", len(got), sweepLimit)
	}
}
