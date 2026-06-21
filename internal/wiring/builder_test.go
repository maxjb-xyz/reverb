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

func TestBuilderBuildEmpty(t *testing.T) {
	st := newTestStore(t)
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
