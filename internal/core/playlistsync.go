package core

// ExternalPlaylist is a playlist fetched from a SearchSource (e.g. Spotify).
type ExternalPlaylist struct {
	Source     string           `json:"source"`
	ExternalID string           `json:"externalId"`
	Name       string           `json:"name"`
	CoverURL   string           `json:"coverUrl,omitempty"`
	Tracks     []ExternalResult `json:"tracks"`
}
