package subsonic

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/maxjb-xyz/crate/internal/core"
	"github.com/maxjb-xyz/crate/internal/library"
	"github.com/maxjb-xyz/crate/internal/registry"
)

// compile-time assertions
var (
	_ library.LibraryAdapter = (*Adapter)(nil)
	_ registry.Plugin        = (*Adapter)(nil)
)

// Adapter is the Subsonic/Navidrome LibraryAdapter. Configure it via Init.
type Adapter struct {
	baseURL    string
	username   string
	password   string
	httpClient *http.Client
	client     *Client
}

// New returns an unconfigured adapter (the registry factory).
func New() *Adapter { return &Adapter{} }

// WithHTTPClient injects an *http.Client (test seam). Call before Init.
func (a *Adapter) WithHTTPClient(h *http.Client) *Adapter {
	a.httpClient = h
	return a
}

func (a *Adapter) Type() string { return "library" }
func (a *Adapter) Name() string { return "subsonic" }

func (a *Adapter) ConfigSchema() registry.ConfigSchema {
	return registry.ConfigSchema{Fields: []registry.ConfigField{
		{Key: "url", Label: "Server URL", Type: "string", Required: true},
		{Key: "username", Label: "Username", Type: "string", Required: true},
		{Key: "password", Label: "Password", Type: "string", Required: true, Secret: true},
	}}
}

func cfgString(cfg map[string]any, key string) string {
	if v, ok := cfg[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func (a *Adapter) Init(cfg map[string]any) error {
	a.baseURL = cfgString(cfg, "url")
	a.username = cfgString(cfg, "username")
	a.password = cfgString(cfg, "password")
	if a.baseURL == "" || a.username == "" || a.password == "" {
		return fmt.Errorf("subsonic: url, username, and password are required")
	}
	a.client = NewClient(a.baseURL, a.username, a.password, a.httpClient)
	return nil
}

func (a *Adapter) TestConnection(ctx context.Context) error {
	if a.client == nil {
		return fmt.Errorf("subsonic: not initialized")
	}
	return a.client.Ping(ctx)
}

// --- mapping helpers (Subsonic seconds → core ms; field renames) ---

func mapTrack(c childDTO) core.Track {
	return core.Track{
		ID:          c.ID,
		Title:       c.Title,
		AlbumID:     c.AlbumID,
		Album:       c.Album,
		ArtistID:    c.ArtistID,
		Artist:      c.Artist,
		CoverArtID:  c.CoverArt,
		TrackNumber: c.Track,
		DiscNumber:  c.DiscNumber,
		DurationMs:  c.Duration * 1000,
		BitRate:     c.BitRate,
		Suffix:      c.Suffix,
		ContentType: c.ContentType,
		ISRC:        c.Isrc, // OpenSubsonic extension; empty on classic Subsonic
	}
}

func mapAlbum(a albumDTO) core.Album {
	al := core.Album{
		ID:         a.ID,
		Name:       a.Name,
		ArtistID:   a.ArtistID,
		Artist:     a.Artist,
		CoverArtID: a.CoverArt,
		Year:       a.Year,
		SongCount:  a.SongCount,
		DurationMs: a.Duration * 1000,
	}
	for _, s := range a.Song {
		al.Tracks = append(al.Tracks, mapTrack(s))
	}
	return al
}

func mapArtist(a artistDTO) core.Artist {
	ar := core.Artist{
		ID:         a.ID,
		Name:       a.Name,
		CoverArtID: a.CoverArt,
		AlbumCount: a.AlbumCount,
	}
	for _, al := range a.Album {
		ar.Albums = append(ar.Albums, mapAlbum(al))
	}
	return ar
}

func mapPlaylist(p playlistDTO) core.Playlist {
	pl := core.Playlist{
		ID:         p.ID,
		Name:       p.Name,
		CoverArtID: p.CoverArt,
		SongCount:  p.SongCount,
		DurationMs: p.Duration * 1000,
	}
	for _, e := range p.Entry {
		pl.Tracks = append(pl.Tracks, mapTrack(e))
	}
	return pl
}

// --- LibraryAdapter methods ---

func (a *Adapter) Search(ctx context.Context, q string, types []core.EntityType) (core.SearchResults, error) {
	params := url.Values{}
	params.Set("query", q)
	var resp subsonicResponse
	if err := a.client.GetJSON(ctx, "search3", params, &resp); err != nil {
		return core.SearchResults{}, err
	}
	res := core.SearchResults{Tracks: []core.Track{}, Albums: []core.Album{}, Artists: []core.Artist{}}
	if resp.SearchResult3 != nil {
		for _, s := range resp.SearchResult3.Song {
			res.Tracks = append(res.Tracks, mapTrack(s))
		}
		for _, al := range resp.SearchResult3.Album {
			res.Albums = append(res.Albums, mapAlbum(al))
		}
		for _, ar := range resp.SearchResult3.Artist {
			res.Artists = append(res.Artists, mapArtist(ar))
		}
	}
	return res, nil
}

func (a *Adapter) GetArtist(ctx context.Context, id string) (core.Artist, error) {
	params := url.Values{}
	params.Set("id", id)
	var resp subsonicResponse
	if err := a.client.GetJSON(ctx, "getArtist", params, &resp); err != nil {
		return core.Artist{}, err
	}
	if resp.Artist == nil {
		return core.Artist{}, fmt.Errorf("subsonic getArtist %q: empty response", id)
	}
	return mapArtist(resp.Artist.artistDTO), nil
}

func (a *Adapter) GetAlbum(ctx context.Context, id string) (core.Album, error) {
	params := url.Values{}
	params.Set("id", id)
	var resp subsonicResponse
	if err := a.client.GetJSON(ctx, "getAlbum", params, &resp); err != nil {
		return core.Album{}, err
	}
	if resp.Album == nil {
		return core.Album{}, fmt.Errorf("subsonic getAlbum %q: empty response", id)
	}
	return mapAlbum(resp.Album.albumDTO), nil
}

func (a *Adapter) GetPlaylists(ctx context.Context) ([]core.Playlist, error) {
	var resp subsonicResponse
	if err := a.client.GetJSON(ctx, "getPlaylists", nil, &resp); err != nil {
		return nil, err
	}
	out := []core.Playlist{}
	if resp.Playlists != nil {
		for _, p := range resp.Playlists.Playlist {
			out = append(out, mapPlaylist(p))
		}
	}
	return out, nil
}

func (a *Adapter) Stream(ctx context.Context, trackID string, opts core.StreamOpts, rangeHeader string) (core.StreamHandle, error) {
	params := url.Values{}
	params.Set("id", trackID)
	if opts.MaxBitRate > 0 {
		params.Set("maxBitRate", strconv.Itoa(opts.MaxBitRate))
	}
	if opts.Format != "" {
		params.Set("format", opts.Format)
	}
	resp, err := a.client.RawGet(ctx, "stream", params, rangeHeader)
	if err != nil {
		return core.StreamHandle{}, err
	}
	return core.StreamHandle{
		Body:          resp.Body,
		ContentType:   resp.Header.Get("Content-Type"),
		ContentLength: resp.ContentLength,
		AcceptRanges:  resp.Header.Get("Accept-Ranges"),
		ContentRange:  resp.Header.Get("Content-Range"),
		StatusCode:    resp.StatusCode,
	}, nil
}

func (a *Adapter) CoverArt(ctx context.Context, id string, size int) (core.CoverArt, error) {
	params := url.Values{}
	params.Set("id", id)
	if size > 0 {
		params.Set("size", strconv.Itoa(size))
	}
	resp, err := a.client.RawGet(ctx, "getCoverArt", params, "")
	if err != nil {
		return core.CoverArt{}, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return core.CoverArt{}, fmt.Errorf("subsonic getCoverArt %q: HTTP %d", id, resp.StatusCode)
	}
	return core.CoverArt{Body: resp.Body, ContentType: resp.Header.Get("Content-Type")}, nil
}

func (a *Adapter) StartScan(ctx context.Context) error {
	return a.client.GetJSON(ctx, "startScan", nil, nil)
}

func (a *Adapter) ScanStatus(ctx context.Context) (core.ScanStatus, error) {
	var resp subsonicResponse
	if err := a.client.GetJSON(ctx, "getScanStatus", nil, &resp); err != nil {
		return core.ScanStatus{}, err
	}
	if resp.ScanStatus == nil {
		return core.ScanStatus{}, nil
	}
	return core.ScanStatus{Scanning: resp.ScanStatus.Scanning, Count: resp.ScanStatus.Count}, nil
}

// GetArtistsBrowse returns the full artist list (Subsonic getArtists), flattened
// across index buckets. Used by the /library/artists browse endpoint.
func (a *Adapter) GetArtistsBrowse(ctx context.Context) ([]core.Artist, error) {
	var resp subsonicResponse
	if err := a.client.GetJSON(ctx, "getArtists", nil, &resp); err != nil {
		return nil, err
	}
	out := []core.Artist{}
	if resp.Artists != nil {
		for _, idx := range resp.Artists.Index {
			for _, ar := range idx.Artist {
				out = append(out, mapArtist(ar))
			}
		}
	}
	return out, nil
}

// GetAlbumsBrowse returns albums via Subsonic getAlbumList2 (listType e.g.
// "newest", "frequent", "recent", "alphabeticalByName"). size defaults to 50.
func (a *Adapter) GetAlbumsBrowse(ctx context.Context, listType string, size int) ([]core.Album, error) {
	if listType == "" {
		listType = "newest"
	}
	if size <= 0 {
		size = 50
	}
	params := url.Values{}
	params.Set("type", listType)
	params.Set("size", strconv.Itoa(size))
	var resp subsonicResponse
	if err := a.client.GetJSON(ctx, "getAlbumList2", params, &resp); err != nil {
		return nil, err
	}
	out := []core.Album{}
	if resp.AlbumList2 != nil {
		for _, al := range resp.AlbumList2.Album {
			out = append(out, mapAlbum(al))
		}
	}
	return out, nil
}
