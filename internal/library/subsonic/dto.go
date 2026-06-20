package subsonic

// envelope wraps every Subsonic JSON response: {"subsonic-response": {...}}.
type envelope struct {
	Response subsonicResponse `json:"subsonic-response"`
}

type subsonicResponse struct {
	Status  string         `json:"status"`
	Version string         `json:"version"`
	Error   *subsonicError `json:"error,omitempty"`

	// Endpoint-specific payloads (only the one in use is populated).
	SearchResult3 *searchResult3  `json:"searchResult3,omitempty"`
	Artists       *artistsIndex   `json:"artists,omitempty"`
	Artist        *artistDetail   `json:"artist,omitempty"`
	Album         *albumDetail    `json:"album,omitempty"`
	AlbumList2    *albumList2     `json:"albumList2,omitempty"`
	Playlists     *playlistsList  `json:"playlists,omitempty"`
	Playlist      *playlistDetail `json:"playlist,omitempty"`
	ScanStatus    *scanStatusDTO  `json:"scanStatus,omitempty"`
}

type subsonicError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type childDTO struct {
	ID          string `json:"id"`
	Parent      string `json:"parent"`
	Title       string `json:"title"`
	Album       string `json:"album"`
	AlbumID     string `json:"albumId"`
	Artist      string `json:"artist"`
	ArtistID    string `json:"artistId"`
	CoverArt    string `json:"coverArt"`
	Track       int    `json:"track"`
	DiscNumber  int    `json:"discNumber"`
	Duration    int    `json:"duration"` // seconds
	BitRate     int    `json:"bitRate"`
	Suffix      string `json:"suffix"`
	ContentType string `json:"contentType"`
	IsDir       bool   `json:"isDir"`
}

type albumDTO struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Artist    string     `json:"artist"`
	ArtistID  string     `json:"artistId"`
	CoverArt  string     `json:"coverArt"`
	Year      int        `json:"year"`
	SongCount int        `json:"songCount"`
	Duration  int        `json:"duration"` // seconds
	Song      []childDTO `json:"song"`
}

type artistDTO struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CoverArt   string     `json:"coverArt"`
	AlbumCount int        `json:"albumCount"`
	Album      []albumDTO `json:"album"`
}

type searchResult3 struct {
	Artist []artistDTO `json:"artist"`
	Album  []albumDTO  `json:"album"`
	Song   []childDTO  `json:"song"`
}

type artistsIndex struct {
	Index []struct {
		Name   string      `json:"name"`
		Artist []artistDTO `json:"artist"`
	} `json:"index"`
}

type artistDetail struct {
	artistDTO
}

type albumDetail struct {
	albumDTO
}

type albumList2 struct {
	Album []albumDTO `json:"album"`
}

type playlistDTO struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	CoverArt  string     `json:"coverArt"`
	SongCount int        `json:"songCount"`
	Duration  int        `json:"duration"`
	Entry     []childDTO `json:"entry"`
}

type playlistsList struct {
	Playlist []playlistDTO `json:"playlist"`
}

type playlistDetail struct {
	playlistDTO
}

type scanStatusDTO struct {
	Scanning bool `json:"scanning"`
	Count    int  `json:"count"`
}
