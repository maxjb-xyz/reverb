package core

import (
	"encoding/json"
	"testing"
)

func TestExternalResultJSONRoundTrip(t *testing.T) {
	in := ExternalResult{
		Source: "spotify", ExternalID: "sp1", Title: "Song", Artist: "Artist",
		Album: "Album", DurationMs: 210000, ISRC: "USX1234", MBID: "mb-1",
		CoverURL: "https://img/x.jpg", CoverArtID: "", Type: EntityTrack,
		Match: &MatchResult{Status: MatchInLibrary, LibraryTrackID: "t1", Method: MatchISRC, Confidence: 1.0},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out ExternalResult
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.ExternalID != "sp1" || out.Match == nil || out.Match.LibraryTrackID != "t1" {
		t.Fatalf("round trip mismatch: %+v", out)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for _, k := range []string{"externalId", "durationMs", "coverUrl", "match"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("expected camelCase key %q, got %v", k, m)
		}
	}
	mm := m["match"].(map[string]any)
	if _, ok := mm["libraryTrackId"]; !ok {
		t.Fatalf("expected match.libraryTrackId, got %v", mm)
	}
}

func TestMatchConstants(t *testing.T) {
	if MatchInLibrary != "in_library" || MatchNotInLibrary != "not_in_library" || MatchUnknown != "unknown" {
		t.Fatal("match status constant drift")
	}
	if MatchISRC != "isrc" || MatchMBID != "mbid" || MatchFuzzy != "fuzzy" || MatchNone != "none" {
		t.Fatal("match method constant drift")
	}
}

func TestExternalResultOmitsNilMatch(t *testing.T) {
	b, _ := json.Marshal(ExternalResult{Source: "s", ExternalID: "e"})
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if _, ok := m["match"]; ok {
		t.Fatalf("nil Match must be omitted, got %v", m)
	}
}
