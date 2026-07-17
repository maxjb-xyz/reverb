package deezer

// Deezer JSON payloads. IDs are numbers; durations are seconds.

type artistRefDTO struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type albumRefDTO struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	CoverMedium string `json:"cover_medium"`
	CoverBig    string `json:"cover_big"`
}

type trackDTO struct {
	ID       int64        `json:"id"`
	Title    string       `json:"title"`
	Duration int          `json:"duration"` // seconds
	Artist   artistRefDTO `json:"artist"`
	Album    albumRefDTO  `json:"album"`
}

type albumSearchDTO struct {
	ID          int64        `json:"id"`
	Title       string       `json:"title"`
	CoverMedium string       `json:"cover_medium"`
	ReleaseDate string       `json:"release_date"` // "2006-01-02"; present on artist-albums rows
	Artist      artistRefDTO `json:"artist"`
}

type artistSearchDTO struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	PictureMedium string `json:"picture_medium"`
}

// artistDTO is returned by GET /artist/{id}. Deezer may omit picture_big for
// artists without an uploaded portrait, so callers fall back to picture_medium.
type artistDTO struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	PictureMedium string `json:"picture_medium"`
	PictureBig    string `json:"picture_big"`
}

type searchTracksResponse struct {
	Data []trackDTO `json:"data"`
}

type searchAlbumsResponse struct {
	Data []albumSearchDTO `json:"data"`
}

type searchArtistsResponse struct {
	Data []artistSearchDTO `json:"data"`
}

type artistAlbumsResponse struct {
	Data []albumSearchDTO `json:"data"`
}

type fullAlbumDTO struct {
	ID          int64        `json:"id"`
	Title       string       `json:"title"`
	CoverBig    string       `json:"cover_big"`
	ReleaseDate string       `json:"release_date"` // "2006-01-02"
	Artist      artistRefDTO `json:"artist"`
	Tracks      struct {
		Data []trackDTO `json:"data"`
	} `json:"tracks"`
}

// apiError is Deezer's in-band error: HTTP 200 with {"error":{...}}.
type apiError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Code    int    `json:"code"`
}

type errEnvelope struct {
	Error *apiError `json:"error"`
}
