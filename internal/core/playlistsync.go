package core

// ExternalPlaylist is a playlist fetched from a SearchSource (e.g. Spotify).
type ExternalPlaylist struct {
	Source     string           `json:"source"`
	ExternalID string           `json:"externalId"`
	Name       string           `json:"name"`
	CoverURL   string           `json:"coverUrl,omitempty"`
	Tracks     []ExternalResult `json:"tracks"`
}

// SyncedPlaylist is the stored synced-playlist summary (no ownership — computed live).
type SyncedPlaylist struct {
	ID              string `json:"id"`
	Source          string `json:"source"`
	ExternalID      string `json:"externalId"`
	Name            string `json:"name"`
	CoverURL        string `json:"coverUrl,omitempty"`
	Mode            string `json:"mode"`
	SyncEnabled     bool   `json:"syncEnabled"`
	SyncIntervalSec int    `json:"syncIntervalSec"`
	AutoDownload    bool   `json:"autoDownload"`
	LastSyncedAt    int64  `json:"lastSyncedAt"`
	TrackCount      int    `json:"trackCount"`
}

// SyncedPlaylistDetail adds live per-track ownership (mirrors AlbumDetail).
type SyncedPlaylistDetail struct {
	SyncedPlaylist
	OwnedCount int                `json:"ownedCount"`
	TotalCount int                `json:"totalCount"`
	Tracks     []AlbumDetailTrack `json:"tracks"`
}
