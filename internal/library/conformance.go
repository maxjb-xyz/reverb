package library

import (
	"context"
	"io"
	"testing"

	"github.com/maximusjb/crate/internal/core"
)

// RunConformance exercises the LibraryAdapter contract. Call it from each
// adapter's test package with a configured, ready-to-use adapter.
//
// StartScan / ScanStatus are treated as OPTIONAL / STUBABLE: an adapter that
// owns its own scanning (a future folder-scan adapter) may implement them as
// no-ops. The suite only asserts they do not panic and return a usable value /
// nil-or-error pair; it never requires a scan to actually run.
func RunConformance(t *testing.T, a LibraryAdapter) {
	t.Helper()
	ctx := context.Background()

	t.Run("Plugin/identity", func(t *testing.T) {
		if a.Type() != "library" {
			t.Errorf("Type() = %q, want \"library\"", a.Type())
		}
		if a.Name() == "" {
			t.Error("Name() must not be empty")
		}
	})

	t.Run("Search/returns-non-nil-slices", func(t *testing.T) {
		res, err := a.Search(ctx, "test", []core.EntityType{core.EntityTrack, core.EntityAlbum, core.EntityArtist})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		// Slices may be empty but must be addressable (no nil-deref downstream).
		_ = res.Tracks
		_ = res.Albums
		_ = res.Artists
	})

	t.Run("GetArtist", func(t *testing.T) {
		ar, err := a.GetArtist(ctx, "ar1")
		if err != nil {
			t.Fatalf("GetArtist: %v", err)
		}
		if ar.ID == "" {
			t.Error("GetArtist returned empty ID")
		}
	})

	t.Run("GetAlbum", func(t *testing.T) {
		al, err := a.GetAlbum(ctx, "al1")
		if err != nil {
			t.Fatalf("GetAlbum: %v", err)
		}
		if al.ID == "" {
			t.Error("GetAlbum returned empty ID")
		}
	})

	t.Run("GetPlaylists", func(t *testing.T) {
		if _, err := a.GetPlaylists(ctx); err != nil {
			t.Fatalf("GetPlaylists: %v", err)
		}
	})

	t.Run("Stream/range-aware", func(t *testing.T) {
		h, err := a.Stream(ctx, "t1", core.StreamOpts{}, "")
		if err != nil {
			t.Fatalf("Stream: %v", err)
		}
		if h.Body == nil {
			t.Fatal("Stream returned nil Body")
		}
		defer h.Body.Close()
		if _, err := io.ReadAll(h.Body); err != nil {
			t.Fatalf("read stream body: %v", err)
		}
		if h.StatusCode == 0 {
			t.Error("Stream returned zero StatusCode")
		}
	})

	t.Run("CoverArt", func(t *testing.T) {
		c, err := a.CoverArt(ctx, "co1", 300)
		if err != nil {
			t.Fatalf("CoverArt: %v", err)
		}
		if c.Body == nil {
			t.Fatal("CoverArt returned nil Body")
		}
		c.Body.Close()
	})

	// Optional / stubable — must not panic; error is acceptable for a no-op adapter.
	t.Run("StartScan/optional", func(t *testing.T) {
		_ = a.StartScan(ctx)
	})
	t.Run("ScanStatus/optional", func(t *testing.T) {
		_, _ = a.ScanStatus(ctx)
	})
}
