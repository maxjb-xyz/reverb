// Package core holds Crate's shared, serializable domain types. These cross the
// adapter boundary (LibraryAdapter, future SearchSource) and are emitted by the
// REST API, so every exported field carries a stable camelCase JSON tag.
package core

import "io"

type EntityType string

const (
	EntityTrack    EntityType = "track"
	EntityAlbum    EntityType = "album"
	EntityArtist   EntityType = "artist"
	EntityPlaylist EntityType = "playlist"
)

// Track is a single playable item.
type Track struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	AlbumID     string `json:"albumId"`
	Album       string `json:"album"`
	ArtistID    string `json:"artistId"`
	Artist      string `json:"artist"`
	CoverArtID  string `json:"coverArtId"`
	TrackNumber int    `json:"trackNumber"`
	DiscNumber  int    `json:"discNumber"`
	DurationMs  int    `json:"durationMs"`
	BitRate     int    `json:"bitRate"`
	Suffix      string `json:"suffix"`
	ContentType string `json:"contentType"`
	ISRC        string `json:"isrc,omitempty"`
}

type Album struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	ArtistID   string `json:"artistId"`
	Artist     string  `json:"artist"`
	CoverArtID string  `json:"coverArtId"`
	Year       int     `json:"year"`
	SongCount  int     `json:"songCount"`
	DurationMs int     `json:"durationMs"`
	Tracks     []Track `json:"tracks,omitempty"`
}

type Artist struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	CoverArtID string  `json:"coverArtId"`
	AlbumCount int     `json:"albumCount"`
	Albums     []Album `json:"albums,omitempty"`
}

type Playlist struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	CoverArtID string  `json:"coverArtId"`
	SongCount  int     `json:"songCount"`
	DurationMs int     `json:"durationMs"`
	Tracks     []Track `json:"tracks,omitempty"`
}

type SearchResults struct {
	Tracks  []Track  `json:"tracks"`
	Albums  []Album  `json:"albums"`
	Artists []Artist `json:"artists"`
}

// StreamOpts reserves transcoding knobs; MVP passes through (Navidrome transcodes).
type StreamOpts struct {
	MaxBitRate int    `json:"maxBitRate"`
	Format     string `json:"format"`
}

// StreamHandle is the upstream stream response, carried through the proxy.
// Body must be closed by the consumer. Not JSON-serialized.
type StreamHandle struct {
	Body          io.ReadCloser `json:"-"`
	ContentType   string        `json:"-"`
	ContentLength int64         `json:"-"`
	AcceptRanges  string        `json:"-"`
	ContentRange  string        `json:"-"`
	StatusCode    int           `json:"-"`
}

type ScanStatus struct {
	Scanning bool `json:"scanning"`
	Count    int  `json:"count"`
}

// CoverArt is an image stream from the adapter. Body must be closed. Not JSON-serialized.
type CoverArt struct {
	Body        io.ReadCloser `json:"-"`
	ContentType string        `json:"-"`
}
