package core

import (
	"encoding/json"
	"testing"
)

func TestTrackJSONRoundTrip(t *testing.T) {
	in := Track{
		ID: "t1", Title: "Song", AlbumID: "al1", Album: "Album",
		ArtistID: "ar1", Artist: "Artist", CoverArtID: "co1",
		TrackNumber: 3, DiscNumber: 1, DurationMs: 210000, BitRate: 320,
		Suffix: "mp3", ContentType: "audio/mpeg", ISRC: "US-X-12",
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Track
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", out, in)
	}
	// JSON keys are camelCase
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if _, ok := m["durationMs"]; !ok {
		t.Fatalf("expected durationMs key, got %v", m)
	}
}

func TestSearchResultsZeroValueMarshals(t *testing.T) {
	b, err := json.Marshal(SearchResults{})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) == "" {
		t.Fatal("empty marshal")
	}
}

func TestEntityTypeConstants(t *testing.T) {
	if EntityTrack != "track" || EntityAlbum != "album" || EntityArtist != "artist" || EntityPlaylist != "playlist" {
		t.Fatal("entity type constant drift")
	}
}
