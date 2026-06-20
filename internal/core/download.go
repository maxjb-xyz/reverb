package core

// DownloadStatus is the lifecycle state of a DownloadJob.
type DownloadStatus string

const (
	DownloadQueued    DownloadStatus = "queued"
	DownloadRunning   DownloadStatus = "running"
	DownloadCompleted DownloadStatus = "completed"
	DownloadFailed    DownloadStatus = "failed"
	DownloadCanceled  DownloadStatus = "canceled"
)

// DownloadRequest is built from an ExternalResult when the user clicks download.
// Downloader is optional (empty = let the Manager pick via the fallback chain).
type DownloadRequest struct {
	Source        string `json:"source"`
	ExternalID    string `json:"externalId"`
	Artist        string `json:"artist"`
	Title         string `json:"title"`
	Album         string `json:"album"`
	ISRC          string `json:"isrc,omitempty"`
	Downloader    string `json:"downloader,omitempty"`
	PlayWhenReady bool   `json:"playWhenReady"`
}

// DownloadJob is the persisted state of one download. Progress is 0-100, or -1
// when the downloader cannot report it (indeterminate ring on the client).
type DownloadJob struct {
	ID             string         `json:"id"`
	DedupKey       string         `json:"dedupKey"`
	Status         DownloadStatus `json:"status"`
	Progress       int            `json:"progress"`
	Error          string         `json:"error,omitempty"`
	OutputPath     string         `json:"outputPath,omitempty"`
	LibraryTrackID string         `json:"libraryTrackId,omitempty"`
	DownloaderName string         `json:"downloaderName"`
	Priority       int            `json:"priority"`
	Attempts       int            `json:"attempts"`
	Source         string         `json:"source"`
	ExternalID     string         `json:"externalId"`
	// Request fields carried so a job rehydrated from request_json can run.
	Artist         string         `json:"artist,omitempty"`
	Title          string         `json:"title,omitempty"`
	Album          string         `json:"album,omitempty"`
	ISRC           string         `json:"isrc,omitempty"`
	PlayWhenReady  bool           `json:"playWhenReady"`
	CreatedAt      int64          `json:"createdAt"`
	StartedAt      int64          `json:"startedAt"`
	FinishedAt     int64          `json:"finishedAt"`
}

// DownloadEvent is published on the EventBus (topics download.queued|progress|
// complete|failed) and marshaled over the WebSocket. ArtistID/AlbumID are set on
// completion for surgical client cache invalidation (empty when unknown).
type DownloadEvent struct {
	JobID          string         `json:"jobId"`
	DedupKey       string         `json:"dedupKey"`
	Status         DownloadStatus `json:"status"`
	Progress       int            `json:"progress"`
	Error          string         `json:"error,omitempty"`
	Source         string         `json:"source"`
	ExternalID     string         `json:"externalId"`
	LibraryTrackID string         `json:"libraryTrackId,omitempty"`
	ArtistID       string         `json:"artistId,omitempty"`
	AlbumID        string         `json:"albumId,omitempty"`
}

// LibraryUpdatedEvent is published on topic library.updated after a scan-driven
// re-match so the client can invalidate exactly the affected queries.
type LibraryUpdatedEvent struct {
	ArtistIDs []string `json:"artistIds"`
	AlbumIDs  []string `json:"albumIds"`
}
