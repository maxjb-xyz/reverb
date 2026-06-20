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
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	DurationMs int            `json:"duration_ms"`
	Artists    []artistRefDTO `json:"artists"`
	Album      albumRefDTO    `json:"album"`
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

// searchResponse mirrors GET /v1/search. Only the requested type is populated.
type searchResponse struct {
	Tracks  *struct{ Items []trackDTO } `json:"tracks"`
	Albums  *struct{ Items []albumRefDTO } `json:"albums"`
	Artists *struct {
		Items []struct {
			ID     string     `json:"id"`
			Name   string     `json:"name"`
			Images []imageDTO `json:"images"`
		}
	} `json:"artists"`
}
