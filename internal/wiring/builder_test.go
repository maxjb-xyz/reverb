package wiring

import (
	"context"
	"database/sql"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/maxjb-xyz/reverb/internal/download/spotdl"
	"github.com/maxjb-xyz/reverb/internal/events"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/wiring.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	return st
}

func newTestBuilder(t *testing.T, st *store.Store) *Builder {
	t.Helper()
	libReg := registry.NewRegistry("library")
	libReg.Register("subsonic", func() registry.Plugin { return &stubLib{} })
	searchReg := registry.NewRegistry("search")
	searchReg.Register("spotify", func() registry.Plugin { return &stubSource{} })
	dlReg := registry.NewRegistry("downloader")
	dlReg.Register("spotdl", func() registry.Plugin { return spotdl.New() })
	return NewBuilder(libReg, searchReg, dlReg, st.Q(), st, events.New(), nil, func(string) string { return "" }, t.TempDir())
}

func addInstance(t *testing.T, st *store.Store, typ, name, cfg string) {
	t.Helper()
	if err := st.Q().CreateAdapterInstance(context.Background(), db.CreateAdapterInstanceParams{
		ID: uuid.NewString(), Type: typ, Name: name, Enabled: 1, Priority: 0, ConfigJson: cfg,
	}); err != nil {
		t.Fatal(err)
	}
}

// setExternalMode pins the library_backend_mode setting to "external" so
// builder tests that expect no library adapter are not affected by the
// built-in mode default (which synthesizes a localhost adapter when no
// library instance is present and no mode is set).
func setExternalMode(t *testing.T, st *store.Store) {
	t.Helper()
	if err := st.Q().UpsertSetting(context.Background(), db.UpsertSettingParams{
		Key: "library_backend_mode", Value: "external",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestBuilderBuildEmpty(t *testing.T) {
	st := newTestStore(t)
	setExternalMode(t, st)
	b := newTestBuilder(t, st)
	bundle, err := b.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Library != nil {
		t.Fatal("expected nil library with no instances")
	}
	if bundle.Aggregator != nil {
		t.Fatal("expected nil aggregator with no instances")
	}
	if bundle.Manager != nil {
		t.Fatal("expected nil manager with no instances")
	}
}

func TestBuilderBuildLibraryAndSearch(t *testing.T) {
	st := newTestStore(t)
	addInstance(t, st, "library", "subsonic", `{"url":"http://x"}`)
	addInstance(t, st, "search", "spotify", `{"client_id":"c"}`)
	b := newTestBuilder(t, st)

	bundle, err := b.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Library == nil {
		t.Fatal("expected a library adapter")
	}
	if bundle.Aggregator == nil {
		t.Fatal("expected an aggregator from the search source")
	}
	// No downloader configured + no REVERB_DOWNLOAD_DIR → no manager.
	if bundle.Manager != nil {
		t.Fatal("expected nil manager with no downloader configured")
	}
}

func TestBuilderManagerRequiresLibrary(t *testing.T) {
	st := newTestStore(t)
	setExternalMode(t, st)
	// Downloader present but no library → manager must be nil (warning-only case).
	addInstance(t, st, "downloader", "spotdl", `{"output_dir":"/music"}`)
	b := newTestBuilder(t, st)

	bundle, err := b.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Manager != nil {
		t.Fatal("expected nil manager when no library is configured")
	}
}

func TestBuilderManagerWithLibraryAndDownloader(t *testing.T) {
	st := newTestStore(t)
	addInstance(t, st, "library", "subsonic", `{"url":"http://x"}`)
	addInstance(t, st, "downloader", "spotdl", `{"output_dir":"/music"}`)
	b := newTestBuilder(t, st)

	bundle, err := b.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Manager == nil {
		t.Fatal("expected a manager with both a library and a downloader")
	}
	// Build must NOT start the manager — caller controls lifecycle.
	bundle.Manager.Stop()
}

// TestBuilderSyncServiceRequiresLibraryAndManager asserts that the sync service
// is nil when the library or manager is absent, regardless of search sources.
func TestBuilderSyncServiceRequiresLibraryAndManager(t *testing.T) {
	st := newTestStore(t)
	// Library + downloader but no Spotify search source → sync service must still
	// be constructed (managed playlists work without Spotify).
	addInstance(t, st, "library", "subsonic", `{"url":"http://x"}`)
	addInstance(t, st, "downloader", "spotdl", `{"output_dir":"/music"}`)
	b := newTestBuilder(t, st)

	bundle, err := b.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Sync == nil {
		t.Fatal("expected sync service with library + manager, even without Spotify")
	}
	bundle.Manager.Stop()
}

// TestBuilderSyncServiceNilWithoutLibrary asserts that the sync service is nil
// when no library adapter is configured (even with Spotify + downloader present).
func TestBuilderSyncServiceNilWithoutLibrary(t *testing.T) {
	st := newTestStore(t)
	setExternalMode(t, st)
	addInstance(t, st, "search", "spotify", `{"client_id":"c"}`)
	addInstance(t, st, "downloader", "spotdl", `{"output_dir":"/music"}`)
	b := newTestBuilder(t, st)

	bundle, err := b.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Sync != nil {
		t.Fatal("expected nil sync service without a library adapter")
	}
}

func TestReconcileDownloadJobIdentity_ClearsRefsOnIdentityChange(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	b := &Builder{queries: st.Q(), version: st}

	// Seed a completed download job with stale library refs (old backend IDs).
	if err := st.Q().InsertDownloadJob(ctx, db.InsertDownloadJobParams{
		ID:             "job-1",
		DedupKey:       "dedup-1",
		RequestJson:    "{}",
		DownloaderName: "spotdl",
		Status:         "completed",
		LibraryTrackID: sql.NullString{String: "old-track-id", Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Q().UpdateDownloadJobCoverArtID(ctx, db.UpdateDownloadJobCoverArtIDParams{
		CoverArtID: sql.NullString{String: "old-cover-id", Valid: true},
		ID:         "job-1",
	}); err != nil {
		t.Fatal(err)
	}

	// First call with new identity (setting unset) → refs must be cleared.
	if err := b.reconcileDownloadJobIdentity(ctx, "builtin"); err != nil {
		t.Fatal(err)
	}
	job, err := st.Q().GetDownloadJob(ctx, "job-1")
	if err != nil {
		t.Fatal(err)
	}
	if job.LibraryTrackID.String != "" {
		t.Fatalf("LibraryTrackID = %q, want empty (cleared on identity change)", job.LibraryTrackID.String)
	}
	if job.CoverArtID.String != "" {
		t.Fatalf("CoverArtID = %q, want empty (cleared on identity change)", job.CoverArtID.String)
	}
	// Setting must be stored.
	if id, _ := st.Q().GetSetting(ctx, settingDownloadJobIdentity); id != "builtin" {
		t.Fatalf("download_jobs_library_identity = %q, want builtin", id)
	}

	// Insert a second job with refs (simulates a freshly linked job after the identity was stored).
	if err := st.Q().InsertDownloadJob(ctx, db.InsertDownloadJobParams{
		ID:             "job-2",
		DedupKey:       "dedup-2",
		RequestJson:    "{}",
		DownloaderName: "spotdl",
		Status:         "completed",
		LibraryTrackID: sql.NullString{String: "new-track-id", Valid: true},
	}); err != nil {
		t.Fatal(err)
	}

	// Second call with SAME identity → no-op; job-2's refs must be preserved.
	if err := b.reconcileDownloadJobIdentity(ctx, "builtin"); err != nil {
		t.Fatal(err)
	}
	job2, err := st.Q().GetDownloadJob(ctx, "job-2")
	if err != nil {
		t.Fatal(err)
	}
	if job2.LibraryTrackID.String != "new-track-id" {
		t.Fatalf("LibraryTrackID = %q, want new-track-id (no-op on same identity)", job2.LibraryTrackID.String)
	}
}

func TestReconcileLibraryIdentity_BumpsOnBackendChange(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	b := &Builder{queries: st.Q(), version: st}

	// Seed a prior identity (external) + a known version, as if matches were cached
	// against an external Navidrome.
	if err := st.Q().UpsertSetting(ctx, db.UpsertSettingParams{Key: settingLibraryIdentity, Value: "external:http://old:4533"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetLibraryVersion(ctx, 5); err != nil {
		t.Fatal(err)
	}

	// Switching to the bundled backend changes identity → version must bump so the
	// match cache (keyed by library_version) is invalidated.
	if err := b.reconcileLibraryIdentity(ctx, "builtin"); err != nil {
		t.Fatal(err)
	}
	if v, _ := st.LibraryVersion(ctx); v != 6 {
		t.Fatalf("library_version = %d, want 6 (bumped on identity change)", v)
	}
	if id, _ := st.Q().GetSetting(ctx, settingLibraryIdentity); id != "builtin" {
		t.Fatalf("library_identity = %q, want builtin", id)
	}

	// Unchanged identity → no further bump.
	if err := b.reconcileLibraryIdentity(ctx, "builtin"); err != nil {
		t.Fatal(err)
	}
	if v, _ := st.LibraryVersion(ctx); v != 6 {
		t.Fatalf("library_version = %d, want 6 (no bump when identity unchanged)", v)
	}
}

// readEpoch is a test helper that reads binding_epoch:<identity> from settings,
// returning 1 if the key is absent (matching the resolver's default).
func readEpoch(t *testing.T, q *db.Queries, identity string) int64 {
	t.Helper()
	v, err := q.GetSetting(context.Background(), "binding_epoch:"+identity)
	if err != nil {
		return 1 // absent → default is 1 (same convention as the resolver)
	}
	n, _ := strconv.ParseInt(v, 10, 64)
	if n == 0 {
		return 1
	}
	return n
}

// TestReconcileLibraryIdentity_IdentityChangeBumpsBindingEpoch asserts that
// switching to a new identity bumps binding_epoch:<newIdentity> by exactly 1.
// On a repeated call with the SAME identity the epoch must NOT move (idempotent).
func TestReconcileLibraryIdentity_IdentityChangeBumpsBindingEpoch(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	b := &Builder{queries: st.Q(), version: st}

	// Pre-seed an old identity so the first reconcile sees a change.
	if err := st.Q().UpsertSetting(ctx, db.UpsertSettingParams{Key: settingLibraryIdentity, Value: "external:http://old:4533"}); err != nil {
		t.Fatal(err)
	}

	epochBefore := readEpoch(t, st.Q(), "builtin")

	// Change identity → binding_epoch for "builtin" must bump by exactly 1.
	if err := b.reconcileLibraryIdentity(ctx, "builtin"); err != nil {
		t.Fatal(err)
	}
	epochAfter := readEpoch(t, st.Q(), "builtin")
	if epochAfter != epochBefore+1 {
		t.Fatalf("binding_epoch:builtin = %d, want %d (bumped once on identity change)", epochAfter, epochBefore+1)
	}

	// Same identity again → epoch must NOT move (idempotent).
	if err := b.reconcileLibraryIdentity(ctx, "builtin"); err != nil {
		t.Fatal(err)
	}
	epochAfterNoop := readEpoch(t, st.Q(), "builtin")
	if epochAfterNoop != epochAfter {
		t.Fatalf("binding_epoch:builtin = %d, want %d (no bump when identity unchanged)", epochAfterNoop, epochAfter)
	}
}

// TestReconcileLibraryIdentity_SecondChangeToNewIdentityBumpsThat asserts that
// a subsequent swap to a different identity bumps that identity's epoch, not
// the first one.
func TestReconcileLibraryIdentity_SecondChangeToNewIdentityBumpsThat(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	b := &Builder{queries: st.Q(), version: st}

	// First swap: old → builtin.
	if err := st.Q().UpsertSetting(ctx, db.UpsertSettingParams{Key: settingLibraryIdentity, Value: "external:http://old:4533"}); err != nil {
		t.Fatal(err)
	}
	if err := b.reconcileLibraryIdentity(ctx, "builtin"); err != nil {
		t.Fatal(err)
	}
	epochBuiltin1 := readEpoch(t, st.Q(), "builtin")

	// Second swap: builtin → external:http://new:4533.
	const newIdentity = "external:http://new:4533"
	epochExtBefore := readEpoch(t, st.Q(), newIdentity)
	if err := b.reconcileLibraryIdentity(ctx, newIdentity); err != nil {
		t.Fatal(err)
	}
	epochExtAfter := readEpoch(t, st.Q(), newIdentity)
	if epochExtAfter != epochExtBefore+1 {
		t.Fatalf("binding_epoch:%s = %d, want %d (bumped on identity change)", newIdentity, epochExtAfter, epochExtBefore+1)
	}
	// "builtin" epoch must be untouched by the second swap.
	epochBuiltin2 := readEpoch(t, st.Q(), "builtin")
	if epochBuiltin2 != epochBuiltin1 {
		t.Fatalf("binding_epoch:builtin changed from %d to %d; should be unaffected by unrelated swap", epochBuiltin1, epochBuiltin2)
	}
}

// TestLibraryVersionBumpDoesNotTouchBindingEpoch asserts that bumping
// library_version (via SetLibraryVersion, as a plain runScan would do) has no
// effect on binding_epoch. This proves the per-identity epoch is decoupled from
// the global scan version — the key correctness property of piece (1).
func TestLibraryVersionBumpDoesNotTouchBindingEpoch(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// First establish a known identity (simulating a prior boot).
	b := &Builder{queries: st.Q(), version: st}
	if err := st.Q().UpsertSetting(ctx, db.UpsertSettingParams{Key: settingLibraryIdentity, Value: "external:old"}); err != nil {
		t.Fatal(err)
	}
	if err := b.reconcileLibraryIdentity(ctx, "builtin"); err != nil {
		t.Fatal(err)
	}
	epochAfterSwap := readEpoch(t, st.Q(), "builtin")

	// Now simulate what a runScan does: bump library_version only (no identity change).
	cur, err := st.LibraryVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetLibraryVersion(ctx, cur+1); err != nil {
		t.Fatal(err)
	}

	// binding_epoch for "builtin" must be unchanged.
	epochAfterScan := readEpoch(t, st.Q(), "builtin")
	if epochAfterScan != epochAfterSwap {
		t.Fatalf("binding_epoch:builtin changed from %d to %d after SetLibraryVersion; scan must not touch binding_epoch", epochAfterSwap, epochAfterScan)
	}
}

// TestReconcileLibraryIdentity_UnchangedIdentityNoopBindingEpoch mirrors the
// existing idempotence test but also asserts the binding_epoch is untouched.
func TestReconcileLibraryIdentity_UnchangedIdentityNoopBindingEpoch(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	b := &Builder{queries: st.Q(), version: st}

	// Seed the identity as already matching — reconcile should be a pure no-op.
	if err := st.Q().UpsertSetting(ctx, db.UpsertSettingParams{Key: settingLibraryIdentity, Value: "builtin"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetLibraryVersion(ctx, 3); err != nil {
		t.Fatal(err)
	}

	epochBefore := readEpoch(t, st.Q(), "builtin")

	if err := b.reconcileLibraryIdentity(ctx, "builtin"); err != nil {
		t.Fatal(err)
	}

	// Neither library_version nor binding_epoch should have moved.
	if v, _ := st.LibraryVersion(ctx); v != 3 {
		t.Fatalf("library_version = %d, want 3 (no bump when identity unchanged)", v)
	}
	epochAfter := readEpoch(t, st.Q(), "builtin")
	if epochAfter != epochBefore {
		t.Fatalf("binding_epoch:builtin changed from %d to %d; must be no-op when identity unchanged", epochBefore, epochAfter)
	}
}

