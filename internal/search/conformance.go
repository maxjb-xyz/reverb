package search

import (
	"context"
	"testing"

	"github.com/maxjb-xyz/crate/internal/core"
)

// RunConformance exercises the SearchSource contract. Call it from each adapter's
// test package with a configured, ready-to-use source (pointed at httptest).
//
// The source passed in MUST be configured (e.g. pointed at an httptest server) so that:
//   - Search(ctx, "test", core.EntityTrack) returns at least one result
//   - GetAlbum(ctx, "al1") returns an album with a non-empty ExternalID
//
// Adapter tests are responsible for seeding fixtures that satisfy these calls.
func RunConformance(t *testing.T, s SearchSource) {
	t.Helper()
	ctx := context.Background()

	t.Run("Plugin/identity", func(t *testing.T) {
		if s.Type() != "search" {
			t.Errorf("Type() = %q, want \"search\"", s.Type())
		}
		if s.Name() == "" {
			t.Error("Name() must not be empty")
		}
	})

	t.Run("Search/track-returns-results", func(t *testing.T) {
		res, err := s.Search(ctx, "test", core.EntityTrack)
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(res) == 0 {
			t.Fatalf("expected at least one result")
		}
		for _, r := range res {
			if r.Source == "" || r.ExternalID == "" {
				t.Fatalf("result missing Source/ExternalID: %+v", r)
			}
		}
	})

	t.Run("Search/album-and-artist-do-not-error", func(t *testing.T) {
		if _, err := s.Search(ctx, "test", core.EntityAlbum); err != nil {
			t.Fatalf("Search(album): %v", err)
		}
		if _, err := s.Search(ctx, "test", core.EntityArtist); err != nil {
			t.Fatalf("Search(artist): %v", err)
		}
	})

	t.Run("GetAlbum/returns-album", func(t *testing.T) {
		al, err := s.GetAlbum(ctx, "al1")
		if err != nil {
			t.Fatalf("GetAlbum: %v", err)
		}
		if al.ExternalID == "" {
			t.Error("GetAlbum returned empty ExternalID")
		}
		// Tracks slice must be addressable (may be empty).
		_ = al.Tracks
	})
}
