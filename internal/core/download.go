package core

// DownloadGranularity describes what unit a downloader operates on.
// GranularityTrack downloaders (e.g. spotDL) fetch individual songs.
// GranularityAlbum downloaders (e.g. Lidarr) fetch whole albums and are
// excluded from the per-track fallback chain.
type DownloadGranularity string

const (
	GranularityTrack DownloadGranularity = "track"
	GranularityAlbum DownloadGranularity = "album"
)

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
	// DurationMs from the originating search result; forwarded into the
	// post-download re-match so the fuzzy rung can disambiguate by length.
	DurationMs    int    `json:"durationMs,omitempty"`
	Downloader    string `json:"downloader,omitempty"`
	PlayWhenReady bool   `json:"playWhenReady"`
	// ManualURL is an optional user-supplied source URL (e.g. a YouTube link) that
	// overrides the default query construction. When set alongside a Spotify source +
	// ExternalID, the spotDL adapter uses the pipe syntax
	// "https://open.spotify.com/track/<id>|<manualURL>" so Spotify metadata is
	// preserved while the audio is fetched from the manual URL.
	ManualURL     string `json:"manualUrl,omitempty"`
	// AddToPlaylistID, when non-empty, causes the download manager to add the
	// resulting library track to this playlist ID once the download completes and
	// the track is matched in the library. Used by the one-time import path so
	// missing tracks are appended to the target playlist as each finishes.
	AddToPlaylistID string `json:"addToPlaylistId,omitempty"`
	// Granularity hints whether this is a track-level or album-level download
	// request. Empty defaults to GranularityTrack. Set by callers that want an
	// album-granularity downloader (e.g. Lidarr) for a full-album import.
	Granularity DownloadGranularity `json:"granularity,omitempty"`
	// InitiatedBy is the id of the user who initiated this download. It is set
	// server-side from the request context (never from the client body, hence
	// json:"-") and persisted on the job as download_jobs.initiated_by.
	InitiatedBy string `json:"-"`
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
	CoverArtID     string         `json:"coverArtId,omitempty"`
	DownloaderName string         `json:"downloaderName"`
	Priority       int            `json:"priority"`
	Attempts       int            `json:"attempts"`
	Source         string         `json:"source"`
	ExternalID     string         `json:"externalId"`
	// Request fields carried so a job rehydrated from request_json can run.
	Artist          string         `json:"artist,omitempty"`
	Title           string         `json:"title,omitempty"`
	Album           string         `json:"album,omitempty"`
	ISRC            string         `json:"isrc,omitempty"`
	DurationMs      int            `json:"durationMs,omitempty"`
	PlayWhenReady   bool           `json:"playWhenReady"`
	// AddToPlaylistID mirrors DownloadRequest.AddToPlaylistID so the post-download
	// playlist-add hook in runScan can read it from the rehydrated job.
	AddToPlaylistID string         `json:"addToPlaylistId,omitempty"`
	// DownloaderRef is downloader-internal handle for async downloaders (e.g. the
	// Lidarr album id). Empty for synchronous downloaders like spotDL.
	DownloaderRef string `json:"downloaderRef,omitempty"`
	CreatedAt       int64          `json:"createdAt"`
	StartedAt       int64          `json:"startedAt"`
	FinishedAt      int64          `json:"finishedAt"`
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
	CoverArtID     string         `json:"coverArtId,omitempty"`
	ArtistID       string         `json:"artistId,omitempty"`
	AlbumID        string         `json:"albumId,omitempty"`
}

// LibraryUpdatedEvent is published on topic library.updated after a scan-driven
// re-match so the client can invalidate exactly the affected queries.
type LibraryUpdatedEvent struct {
	ArtistIDs []string `json:"artistIds"`
	AlbumIDs  []string `json:"albumIds"`
}

// DownloadRemovedEvent is published on topic download.removed when one or more
// finished jobs are cleared (hard-deleted). The client drops them from its store.
type DownloadRemovedEvent struct {
	JobIDs []string `json:"jobIds"`
}

// QueueStateEvent is published on topic download.queue when the queue is paused
// or resumed, so every client reflects the gate state live.
type QueueStateEvent struct {
	Paused bool `json:"paused"`
}
