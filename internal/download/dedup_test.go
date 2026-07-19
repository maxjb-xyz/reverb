package download

import (
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
)

func TestDedupKeyStableAndNormalized(t *testing.T) {
	a := core.DownloadRequest{Artist: "The Beatles", Title: "Hey Jude", Album: "1"}
	// Cosmetic noise the matcher's Normalize strips: case, feat group, punctuation.
	b := core.DownloadRequest{Artist: "the beatles", Title: "Hey Jude (feat. Nobody)", Album: "1"}
	if DedupKey(a) == "" {
		t.Fatal("DedupKey must be non-empty")
	}
	if DedupKey(a) != DedupKey(b) {
		t.Fatalf("normalized-equal requests must share a key: %q vs %q", DedupKey(a), DedupKey(b))
	}
}

func TestDedupKeyDistinguishesDifferentTracks(t *testing.T) {
	a := core.DownloadRequest{Artist: "Radiohead", Title: "Creep", Album: "Pablo Honey"}
	b := core.DownloadRequest{Artist: "TLC", Title: "Creep", Album: "CrazySexyCool"}
	if DedupKey(a) == DedupKey(b) {
		t.Fatal("different artist/album must produce different keys")
	}
}

func TestDedupKeyUsesStableExternalIdentity(t *testing.T) {
	a := core.DownloadRequest{Source: "spotify", ExternalID: "5abc", Artist: "Artist", Title: "Original title", Album: "Album"}
	b := core.DownloadRequest{Source: "Spotify", ExternalID: "5abc", Artist: "Different artist", Title: "Display title changed", Album: "Other"}
	if DedupKey(a) != DedupKey(b) {
		t.Fatal("same source/external id must deduplicate despite metadata differences")
	}
	c := core.DownloadRequest{Source: "spotify", ExternalID: "5Abc", Artist: "Artist", Title: "Original title", Album: "Album"}
	if DedupKey(a) == DedupKey(c) {
		t.Fatal("external ids are case-sensitive and must remain distinct")
	}
}

func TestDedupKeyDeterministicLength(t *testing.T) {
	k := DedupKey(core.DownloadRequest{Artist: "x", Title: "y", Album: "z"})
	if len(k) != 64 {
		t.Fatalf("sha256 hex must be 64 chars, got %d (%q)", len(k), k)
	}
}
