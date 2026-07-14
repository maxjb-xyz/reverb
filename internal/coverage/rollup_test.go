// internal/coverage/rollup_test.go
package coverage

import (
	"context"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
)

type fakeMatcher struct {
	owned map[string]string     // externalID -> libraryTrackID ("" = miss)
	meta  map[string]core.Track // externalID -> matched candidate metadata (optional)
}

func (f fakeMatcher) Match(_ context.Context, e core.ExternalResult) (core.MatchResult, error) {
	if id, ok := f.owned[e.ExternalID]; ok && id != "" {
		md := f.meta[e.ExternalID]
		return core.MatchResult{
			Status: core.MatchInLibrary, LibraryTrackID: id,
			ArtistID: md.ArtistID, AlbumID: md.AlbumID, CoverArtID: md.CoverArtID,
		}, nil
	}
	return core.MatchResult{Status: core.MatchNotInLibrary}, nil
}

func album(ids ...string) core.ExternalAlbum {
	al := core.ExternalAlbum{Source: "spotify", ExternalID: "AL", Name: "Kid A"}
	for _, id := range ids {
		al.Tracks = append(al.Tracks, core.ExternalResult{Source: "spotify", ExternalID: id, Title: id, Type: core.EntityTrack})
	}
	return al
}

func TestRollUpPartial(t *testing.T) {
	m := fakeMatcher{owned: map[string]string{"t1": "L1", "t2": "L2", "t3": ""}}
	cov, err := RollUp(context.Background(), m, album("t1", "t2", "t3"))
	if err != nil {
		t.Fatal(err)
	}
	if cov.State != core.CoveragePartial || cov.OwnedCount != 2 || cov.TotalCount != 3 {
		t.Fatalf("bad rollup: %+v", cov)
	}
	if len(cov.MissingTracks) != 1 || cov.MissingTracks[0].ExternalID != "t3" {
		t.Fatalf("want t3 missing, got %+v", cov.MissingTracks)
	}
}

func TestRollUpFull(t *testing.T) {
	m := fakeMatcher{owned: map[string]string{"t1": "L1", "t2": "L2"}}
	cov, _ := RollUp(context.Background(), m, album("t1", "t2"))
	if cov.State != core.CoverageFull || len(cov.MissingTracks) != 0 {
		t.Fatalf("want full/no-missing, got %+v", cov)
	}
}
