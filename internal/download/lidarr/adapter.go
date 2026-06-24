package lidarr

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/download"
	"github.com/maxjb-xyz/reverb/internal/registry"
)

// Adapter implements download.Downloader (opt-in: CanDownload=false) and
// download.AsyncDownloader for Lidarr.
type Adapter struct {
	url               string
	apiKey            string
	rootFolder        string
	qualityProfileID  int
	metadataProfileID int
	client            *Client
}

func New() *Adapter { return &Adapter{} }

// NewClientFor builds a Client bound to a's config with the given Doer (test seam).
func NewClientFor(a *Adapter, doer Doer) *Client { return NewClient(a.url, a.apiKey, doer) }

func (a *Adapter) Type() string { return "downloader" }
func (a *Adapter) Name() string { return "lidarr" }

func (a *Adapter) ConfigSchema() registry.ConfigSchema {
	return registry.ConfigSchema{Fields: []registry.ConfigField{
		{Key: "url", Label: "Lidarr URL", Type: "string", Required: true},
		{Key: "api_key", Label: "API key", Type: "string", Required: true, Secret: true},
		{Key: "root_folder", Label: "Root folder path", Type: "string", Required: true},
		{Key: "quality_profile_id", Label: "Quality profile ID", Type: "number", Required: true},
		{Key: "metadata_profile_id", Label: "Metadata profile ID", Type: "number", Required: true},
	}}
}

// asInt coerces a JSON config value (float64 from encoding/json, or string) to int.
func asInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case string:
		i, _ := strconv.Atoi(n)
		return i
	}
	return 0
}

func (a *Adapter) Init(cfg map[string]any) error {
	if v, ok := cfg["url"].(string); ok {
		a.url = v
	}
	if v, ok := cfg["api_key"].(string); ok {
		a.apiKey = v
	}
	if v, ok := cfg["root_folder"].(string); ok {
		a.rootFolder = v
	}
	a.qualityProfileID = asInt(cfg["quality_profile_id"])
	a.metadataProfileID = asInt(cfg["metadata_profile_id"])
	if a.url == "" || a.apiKey == "" || a.rootFolder == "" {
		return fmt.Errorf("lidarr: url, api_key and root_folder are required")
	}
	if a.qualityProfileID == 0 {
		return fmt.Errorf("lidarr: quality_profile_id is required")
	}
	if a.client == nil {
		a.client = NewClient(a.url, a.apiKey, &http.Client{Timeout: 30 * time.Second})
	}
	return nil
}

// TestConnection validates URL + API key by pinging system/status.
func (a *Adapter) TestConnection(ctx context.Context) error {
	return a.client.SystemStatus(ctx)
}

// CanDownload returns FALSE: Lidarr is opt-in only, never chosen by the auto
// fallback chain or batch/playlist imports.
func (a *Adapter) CanDownload(ctx context.Context, req core.DownloadRequest) (bool, error) {
	return false, nil
}

// Start is never called for an async downloader (the Manager uses Submit/Poll).
func (a *Adapter) Start(ctx context.Context, req core.DownloadRequest, onProgress func(int)) (string, error) {
	return "", fmt.Errorf("lidarr: Start is not used (async downloader)")
}

// Submit resolves the album, adds the artist UNMONITORED, monitors+searches only
// the target album, and returns the Lidarr album id as the ref.
func (a *Adapter) Submit(ctx context.Context, req core.DownloadRequest) (string, error) {
	if req.Album == "" || req.Artist == "" {
		return "", fmt.Errorf("lidarr needs an artist and album (couldn't map %q)", req.Title)
	}
	results, err := a.client.LookupAlbum(ctx, req.Artist+" "+req.Album)
	if err != nil {
		return "", fmt.Errorf("lidarr lookup: %w", err)
	}
	if len(results) == 0 {
		return "", fmt.Errorf("couldn't find %q by %q in Lidarr", req.Album, req.Artist)
	}
	top := results[0]

	// Ensure the artist exists (added UNMONITORED).
	artist, exists, err := a.client.GetArtistByForeignID(ctx, top.Artist.ForeignArtistID)
	if err != nil {
		return "", fmt.Errorf("lidarr artist lookup: %w", err)
	}
	if !exists {
		artist, err = a.client.AddArtist(ctx, top.Artist, a.rootFolder, a.qualityProfileID, a.metadataProfileID)
		if err != nil {
			return "", fmt.Errorf("lidarr add artist: %w", err)
		}
	}

	// Find the target album among the artist's albums (Lidarr assigns it an id).
	albums, err := a.client.GetAlbumsByArtist(ctx, artist.ID)
	if err != nil {
		return "", fmt.Errorf("lidarr list albums: %w", err)
	}
	albumID := 0
	for _, al := range albums {
		if al.ForeignAlbumID == top.ForeignAlbumID {
			albumID = al.ID
			break
		}
	}
	if albumID == 0 {
		return "", fmt.Errorf("lidarr: album %q not found under artist after add", req.Album)
	}

	if err := a.client.MonitorAlbum(ctx, albumID); err != nil {
		return "", fmt.Errorf("lidarr monitor album: %w", err)
	}
	if err := a.client.SearchAlbum(ctx, albumID); err != nil {
		return "", fmt.Errorf("lidarr album search: %w", err)
	}
	return strconv.Itoa(albumID), nil
}

// Poll maps Lidarr's album/queue state onto a download.AsyncStatus.
func (a *Adapter) Poll(ctx context.Context, ref string) (download.AsyncStatus, error) {
	albumID, err := strconv.Atoi(ref)
	if err != nil {
		return download.AsyncStatus{}, fmt.Errorf("lidarr: bad ref %q", ref)
	}
	album, err := a.client.GetAlbum(ctx, albumID)
	if err != nil {
		return download.AsyncStatus{}, err
	}
	// Fully imported → completed.
	if album.Statistics.TrackCount > 0 && album.Statistics.TrackFileCount >= album.Statistics.TrackCount {
		return download.AsyncStatus{State: core.DownloadCompleted, Progress: 100}, nil
	}
	// Otherwise inspect the queue for download progress / errors.
	records, err := a.client.GetQueueForAlbum(ctx, albumID)
	if err != nil {
		return download.AsyncStatus{}, err
	}
	for _, r := range records {
		if r.TrackedDownloadStatus == "error" {
			msg := r.ErrorMessage
			if msg == "" {
				msg = "Lidarr download error"
			}
			return download.AsyncStatus{State: core.DownloadFailed, Error: msg}, nil
		}
		if r.Size > 0 {
			pct := int(100 * (r.Size - r.Sizeleft) / r.Size)
			if pct < 0 {
				pct = 0
			}
			if pct > 100 {
				pct = 100
			}
			return download.AsyncStatus{State: core.DownloadRunning, Progress: pct}, nil
		}
	}
	// No queue item yet — still searching.
	return download.AsyncStatus{State: core.DownloadRunning, Progress: -1}, nil
}

// CancelAsync best-effort unmonitors the album so Lidarr stops chasing it.
func (a *Adapter) CancelAsync(ctx context.Context, ref string) error {
	albumID, err := strconv.Atoi(ref)
	if err != nil {
		return nil
	}
	return a.client.RemoveAlbumFromQueue(ctx, albumID)
}

// compile-time assertions live in adapter_test.go (download import there).
