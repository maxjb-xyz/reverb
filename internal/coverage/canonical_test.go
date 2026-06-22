// internal/coverage/canonical_test.go
package coverage

import (
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
)

func TestCanonicalizeCollapsesEditions(t *testing.T) {
	in := []core.ExternalAlbum{
		{ExternalID: "a1", Name: "Kid A", Year: 2000, Kind: "album", TotalTracks: 10},
		{ExternalID: "a2", Name: "Kid A (Deluxe Edition)", Year: 2009, Kind: "album", TotalTracks: 22},
		{ExternalID: "a3", Name: "Kid A (Remastered)", Year: 2016, Kind: "album", TotalTracks: 10},
		{ExternalID: "s1", Name: "Creep", Year: 1992, Kind: "single", TotalTracks: 1},
	}
	got := Canonicalize(in)
	if len(got) != 2 {
		t.Fatalf("want 2 canonical releases, got %d: %+v", len(got), got)
	}
	if got[0].Kind != "album" || got[0].ExternalID != "a1" {
		t.Fatalf("want standard Kid A (a1) first, got %+v", got[0])
	}
	if got[1].Kind != "single" {
		t.Fatalf("want single last, got %+v", got[1])
	}
}
