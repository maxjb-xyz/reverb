package wiring

import (
	"context"
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
	return NewBuilder(libReg, searchReg, dlReg, st.Q(), st, events.New(), nil, func(string) string { return "" })
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
