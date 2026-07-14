// Package lidarr is the Lidarr downloader adapter + a thin Lidarr v1 REST client.
// Lidarr acquires music asynchronously at the album level; this client drives the
// add-artist-unmonitored → monitor-album → search → poll flow used by the adapter.
package lidarr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Doer is the http.Client seam (injectable for tests).
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client is a thin Lidarr v1 API client.
type Client struct {
	base string
	key  string
	http Doer
}

// NewClient constructs a Client. base is the Lidarr URL (e.g. http://lidarr:8686).
func NewClient(base, key string, http Doer) *Client {
	return &Client{base: strings.TrimRight(base, "/"), key: key, http: http}
}

// --- API types (subset used) ---

type ArtistRef struct {
	ID                int    `json:"id"`
	ArtistName        string `json:"artistName"`
	ForeignArtistID   string `json:"foreignArtistId"`
	QualityProfileID  int    `json:"qualityProfileId,omitempty"`
	MetadataProfileID int    `json:"metadataProfileId,omitempty"`
	RootFolderPath    string `json:"rootFolderPath,omitempty"`
	Monitored         bool   `json:"monitored"`
}

type AlbumResult struct {
	ID             int             `json:"id"`
	Title          string          `json:"title"`
	ForeignAlbumID string          `json:"foreignAlbumId"`
	Monitored      bool            `json:"monitored"`
	Artist         ArtistRef       `json:"artist"`
	Statistics     AlbumStatistics `json:"statistics"`
}

type AlbumStatistics struct {
	TrackCount     int `json:"trackCount"`
	TrackFileCount int `json:"trackFileCount"`
}

type QueueRecord struct {
	AlbumID               int     `json:"albumId"`
	Size                  float64 `json:"size"`
	Sizeleft              float64 `json:"sizeleft"`
	Status                string  `json:"status"`
	TrackedDownloadStatus string  `json:"trackedDownloadStatus"`
	ErrorMessage          string  `json:"errorMessage"`
}

type queuePage struct {
	Records []QueueRecord `json:"records"`
}

// --- request helpers ---

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", c.key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("lidarr %s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("lidarr %s %s: decode: %w", method, path, err)
		}
	}
	return nil
}

// SystemStatus pings Lidarr to validate URL + API key.
func (c *Client) SystemStatus(ctx context.Context) error {
	var out map[string]any
	return c.do(ctx, http.MethodGet, "/api/v1/system/status", nil, &out)
}

// LookupAlbum searches Lidarr's metadata (MusicBrainz) for an album by free text.
func (c *Client) LookupAlbum(ctx context.Context, term string) ([]AlbumResult, error) {
	var out []AlbumResult
	err := c.do(ctx, http.MethodGet, "/api/v1/album/lookup?term="+url.QueryEscape(term), nil, &out)
	return out, err
}

// GetArtistByForeignID returns the existing Lidarr artist with the given
// MusicBrainz id, or (ArtistRef{}, false) if not yet added.
func (c *Client) GetArtistByForeignID(ctx context.Context, foreignID string) (ArtistRef, bool, error) {
	var out []ArtistRef
	if err := c.do(ctx, http.MethodGet, "/api/v1/artist", nil, &out); err != nil {
		return ArtistRef{}, false, err
	}
	for _, a := range out {
		if a.ForeignArtistID == foreignID {
			return a, true, nil
		}
	}
	return ArtistRef{}, false, nil
}

// addArtistBody is the POST /api/v1/artist payload. The artist is added
// UNMONITORED (monitored:false + addOptions.monitor:"none") so Lidarr does not
// chase the whole discography.
type addArtistBody struct {
	ArtistName        string           `json:"artistName"`
	ForeignArtistID   string           `json:"foreignArtistId"`
	QualityProfileID  int              `json:"qualityProfileId"`
	MetadataProfileID int              `json:"metadataProfileId"`
	RootFolderPath    string           `json:"rootFolderPath"`
	Monitored         bool             `json:"monitored"`
	AddOptions        addArtistAddOpts `json:"addOptions"`
}
type addArtistAddOpts struct {
	Monitor                string `json:"monitor"`
	SearchForMissingAlbums bool   `json:"searchForMissingAlbums"`
}

// AddArtist adds an artist UNMONITORED and returns the created artist (with its
// Lidarr id). Caller supplies the resolved foreign id from a prior LookupAlbum.
func (c *Client) AddArtist(ctx context.Context, a ArtistRef, rootFolder string, qualityProfileID, metadataProfileID int) (ArtistRef, error) {
	body := addArtistBody{
		ArtistName:        a.ArtistName,
		ForeignArtistID:   a.ForeignArtistID,
		QualityProfileID:  qualityProfileID,
		MetadataProfileID: metadataProfileID,
		RootFolderPath:    rootFolder,
		Monitored:         false,
		AddOptions:        addArtistAddOpts{Monitor: "none", SearchForMissingAlbums: false},
	}
	var out ArtistRef
	err := c.do(ctx, http.MethodPost, "/api/v1/artist", body, &out)
	return out, err
}

// GetAlbumsByArtist lists a Lidarr artist's albums (after the artist is added,
// Lidarr fetches them unmonitored).
func (c *Client) GetAlbumsByArtist(ctx context.Context, artistID int) ([]AlbumResult, error) {
	var out []AlbumResult
	err := c.do(ctx, http.MethodGet, "/api/v1/album?artistId="+strconv.Itoa(artistID), nil, &out)
	return out, err
}

// MonitorAlbum sets a single album monitored=true.
func (c *Client) MonitorAlbum(ctx context.Context, albumID int) error {
	body := map[string]any{"albumIds": []int{albumID}, "monitored": true}
	return c.do(ctx, http.MethodPut, "/api/v1/album/monitor", body, nil)
}

// SearchAlbum triggers an AlbumSearch command for one album.
func (c *Client) SearchAlbum(ctx context.Context, albumID int) error {
	body := map[string]any{"name": "AlbumSearch", "albumIds": []int{albumID}}
	return c.do(ctx, http.MethodPost, "/api/v1/command", body, nil)
}

// GetAlbum returns one album (with statistics) by Lidarr id.
func (c *Client) GetAlbum(ctx context.Context, albumID int) (AlbumResult, error) {
	var out AlbumResult
	err := c.do(ctx, http.MethodGet, "/api/v1/album/"+strconv.Itoa(albumID), nil, &out)
	return out, err
}

// GetQueueForAlbum returns active queue records for an album (download progress).
func (c *Client) GetQueueForAlbum(ctx context.Context, albumID int) ([]QueueRecord, error) {
	var page queuePage
	if err := c.do(ctx, http.MethodGet, "/api/v1/queue?pageSize=100", nil, &page); err != nil {
		return nil, err
	}
	var out []QueueRecord
	for _, r := range page.Records {
		if r.AlbumID == albumID {
			out = append(out, r)
		}
	}
	return out, nil
}

// RemoveAlbumFromQueue best-effort cancels by unmonitoring the album so Lidarr
// stops chasing it (drops its wanted status).
func (c *Client) RemoveAlbumFromQueue(ctx context.Context, albumID int) error {
	return c.do(ctx, http.MethodPut, "/api/v1/album/monitor", map[string]any{"albumIds": []int{albumID}, "monitored": false}, nil)
}
