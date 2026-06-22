package core

// CoverageState is an album's exact-match verdict against the local library.
type CoverageState string

const (
	CoveragePending CoverageState = "pending" // not computed yet (skeleton)
	CoverageNone    CoverageState = "none"    // zero tracks owned
	CoveragePartial CoverageState = "partial" // some, not all, tracks owned
	CoverageFull    CoverageState = "full"    // every canonical track owned
)

// ExternalTrackRef is the minimum needed to enqueue a download and render a row.
type ExternalTrackRef struct {
	Source     string `json:"source"`
	ExternalID string `json:"externalId"`
	Title      string `json:"title"`
	Artist     string `json:"artist,omitempty"`
	Album      string `json:"album,omitempty"`
	ISRC       string `json:"isrc,omitempty"`
	DurationMs int    `json:"durationMs"`
}

// AlbumCoverage is the per-album rollup streamed to the client.
type AlbumCoverage struct {
	Source          string             `json:"source"`
	ExternalAlbumID string             `json:"externalAlbumId"`
	State           CoverageState      `json:"state"`
	OwnedCount      int                `json:"ownedCount"`
	TotalCount      int                `json:"totalCount"`
	LibraryAlbumID  string             `json:"libraryAlbumId,omitempty"`
	MissingTracks   []ExternalTrackRef `json:"missingTracks"`
}

// DiscographyAlbum is one deduped release in the artist-page skeleton.
type DiscographyAlbum struct {
	Source         string `json:"source"`
	ExternalID     string `json:"externalId"`
	Name           string `json:"name"`
	CoverURL       string `json:"coverUrl,omitempty"`
	Year           int    `json:"year"`
	Kind           string `json:"kind"` // "album" | "single"
	TotalTracks    int    `json:"totalTracks"`
	LibraryAlbumID string `json:"libraryAlbumId,omitempty"`
}

// ArtistDetail is the artist-page response: header + deduped discography skeleton.
// When Resolved is false, Albums holds the library-owned albums (graceful degrade)
// and no coverage stream is opened.
type ArtistDetail struct {
	Source           string             `json:"source"`
	ID               string             `json:"id"`
	Name             string             `json:"name"`
	CoverArtID       string             `json:"coverArtId,omitempty"`
	CoverURL         string             `json:"coverUrl,omitempty"`
	LibraryArtistID  string             `json:"libraryArtistId,omitempty"`
	ExternalArtistID string             `json:"externalArtistId,omitempty"`
	Resolved         bool               `json:"resolved"`
	Albums           []DiscographyAlbum `json:"albums"`
}

// AlbumDetailTrack is one track on the album page, owned or missing.
type AlbumDetailTrack struct {
	State       CoverageState     `json:"state"` // full = owned, none = missing
	LibraryTrack *Track            `json:"libraryTrack,omitempty"`
	ExternalRef  *ExternalTrackRef `json:"externalRef,omitempty"`
	Title       string            `json:"title"`
	Artist      string            `json:"artist"`
	Album       string            `json:"album,omitempty"`
	TrackNumber int               `json:"trackNumber"`
	DurationMs  int               `json:"durationMs"`
	CoverURL    string            `json:"coverUrl,omitempty"`
	// ArtistExternalID and AlbumExternalID carry the Spotify IDs for the track's
	// primary artist and album. Set on both owned and missing rows so synced-playlist
	// and album-detail rows can render clickable artist/album links to the Spotify
	// source — used by the FE to navigate to artist/album pages from synced rows.
	ArtistExternalID string `json:"artistExternalId,omitempty"`
	AlbumExternalID  string `json:"albumExternalId,omitempty"`
}

// AlbumDetail is the album-page response with per-track ownership.
type AlbumDetail struct {
	Source         string             `json:"source"`
	ID             string             `json:"id"`
	Name           string             `json:"name"`
	Artist         string             `json:"artist"`
	ArtistID       string             `json:"artistId,omitempty"`
	CoverArtID     string             `json:"coverArtId,omitempty"`
	CoverURL       string             `json:"coverUrl,omitempty"`
	Year           int                `json:"year"`
	LibraryAlbumID string             `json:"libraryAlbumId,omitempty"`
	OwnedCount     int                `json:"ownedCount"`
	TotalCount     int                `json:"totalCount"`
	Tracks         []AlbumDetailTrack `json:"tracks"`
}
