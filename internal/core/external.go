package core

// MatchStatus is the verdict of MatchingService for an external result.
type MatchStatus string

const (
	MatchInLibrary    MatchStatus = "in_library"
	MatchNotInLibrary MatchStatus = "not_in_library"
	MatchUnknown      MatchStatus = "unknown"
)

// MatchMethod records which rung of the priority chain decided the match.
type MatchMethod string

const (
	MatchISRC  MatchMethod = "isrc"
	MatchMBID  MatchMethod = "mbid"
	MatchFuzzy MatchMethod = "fuzzy"
	MatchNone  MatchMethod = "none"
)

// MatchResult is attached to an ExternalResult after MatchingService runs.
// LibraryTrackID is set only when Status == MatchInLibrary. Confidence is a
// documented heuristic in [0,1]: 1.0 for ISRC/MBID exact, ~0.6–0.9 for fuzzy.
type MatchResult struct {
	Status         MatchStatus `json:"status"`
	LibraryTrackID string      `json:"libraryTrackId"`
	Method         MatchMethod `json:"method"`
	Confidence     float64     `json:"confidence"`
	// Metadata of the matched library candidate, threaded through so the synthesized
	// owned LibraryTrack can carry clickable artist/album links and a real cover.
	// Set only when Status == MatchInLibrary; reconstructed from match_cache on a HIT.
	ArtistID   string `json:"artistId,omitempty"`
	AlbumID    string `json:"albumId,omitempty"`
	CoverArtID string `json:"coverArtId,omitempty"`
}

// ExternalResult is one search hit from an external SearchSource. ISRC and MBID
// are DATA (optional) — the matcher uses them when non-empty. Match is filled in
// by MatchingService before the result is emitted to the client.
type ExternalResult struct {
	Source     string       `json:"source"`
	ExternalID string       `json:"externalId"`
	Title      string       `json:"title"`
	Artist     string       `json:"artist"`
	Album      string       `json:"album"`
	DurationMs int          `json:"durationMs"`
	ISRC       string       `json:"isrc,omitempty"`
	MBID       string       `json:"mbid,omitempty"`
	CoverURL   string       `json:"coverUrl,omitempty"`
	CoverArtID string       `json:"coverArtId,omitempty"`
	Type       EntityType   `json:"type"`
	Match      *MatchResult `json:"match,omitempty"`
}

// ExternalAlbum is an album fetched from a SearchSource (GetAlbum).
type ExternalAlbum struct {
	Source      string           `json:"source"`
	ExternalID  string           `json:"externalId"`
	Name        string           `json:"name"`
	Artist      string           `json:"artist"`
	CoverURL    string           `json:"coverUrl,omitempty"`
	Year        int              `json:"year"`
	Kind        string           `json:"kind,omitempty"`        // "album" | "single"
	TotalTracks int              `json:"totalTracks,omitempty"`
	Tracks      []ExternalResult `json:"tracks"`
}
