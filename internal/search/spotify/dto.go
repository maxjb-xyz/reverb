package spotify

// tokenResponse is the client-credentials OAuth response.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

type imageDTO struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type artistRefDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type albumRefDTO struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	ReleaseDate string         `json:"release_date"`
	Images      []imageDTO     `json:"images"`
	Artists     []artistRefDTO `json:"artists"`
}

type trackDTO struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	DurationMs  int            `json:"duration_ms"`
	Artists     []artistRefDTO `json:"artists"`
	Album       albumRefDTO    `json:"album"`
	ExternalIDs struct {
		ISRC string `json:"isrc"`
	} `json:"external_ids"`
}

type fullAlbumDTO struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	ReleaseDate string         `json:"release_date"`
	Images      []imageDTO     `json:"images"`
	Artists     []artistRefDTO `json:"artists"`
	Tracks      struct {
		Items []trackDTO `json:"items"`
	} `json:"tracks"`
}

// artistAlbumsResponse is /artists/{id}/albums (paged).
type artistAlbumsResponse struct {
	Items []artistAlbumDTO `json:"items"`
	Next  string           `json:"next"`
}

type artistAlbumDTO struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	AlbumType   string         `json:"album_type"` // "album" | "single" | "compilation"
	TotalTracks int            `json:"total_tracks"`
	ReleaseDate string         `json:"release_date"`
	Images      []imageDTO     `json:"images"`
	Artists     []artistRefDTO `json:"artists"`
}

type playlistObjectDTO struct {
	Name   string          `json:"name"`
	Images []imageDTO      `json:"images"`
	Tracks playlistPageDTO `json:"tracks"`
}

type playlistPageDTO struct {
	Items []playlistItemDTO `json:"items"`
	Next  string            `json:"next"`
}

type playlistItemDTO struct {
	Track trackDTO `json:"track"`
}

// artistDTO is the response from GET /artists/{id}.
type artistDTO struct {
	ID     string     `json:"id"`
	Name   string     `json:"name"`
	Images []imageDTO `json:"images"`
}

// searchResponse mirrors GET /v1/search. Only the requested type is populated.
type searchResponse struct {
	Tracks  *struct{ Items []trackDTO }    `json:"tracks"`
	Albums  *struct{ Items []albumRefDTO } `json:"albums"`
	Artists *struct {
		Items []struct {
			ID     string     `json:"id"`
			Name   string     `json:"name"`
			Images []imageDTO `json:"images"`
		}
	} `json:"artists"`
}
