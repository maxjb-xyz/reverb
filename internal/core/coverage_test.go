// internal/core/coverage_test.go
package core

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAlbumCoverageJSONTags(t *testing.T) {
	c := AlbumCoverage{
		Source: "spotify", ExternalAlbumID: "abc", State: CoveragePartial,
		OwnedCount: 7, TotalCount: 10, LibraryAlbumID: "lib1",
		MissingTracks: []ExternalTrackRef{{Source: "spotify", ExternalID: "t1", Title: "x", DurationMs: 1000}},
	}
	b, _ := json.Marshal(c)
	got := string(b)
	for _, want := range []string{`"state":"partial"`, `"ownedCount":7`, `"totalCount":10`, `"libraryAlbumId":"lib1"`, `"missingTracks"`, `"externalAlbumId":"abc"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %s", want, got)
		}
	}
}
