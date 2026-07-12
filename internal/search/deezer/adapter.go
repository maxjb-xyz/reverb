// Package deezer implements the SearchSource contract against Deezer's public
// API. It is keyless — the only search source that works with zero config —
// so a fresh install gets "Search Everywhere" without a Spotify dev account.
package deezer

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/search"
)

var (
	_ search.SearchSource = (*Adapter)(nil)
	_ registry.Plugin     = (*Adapter)(nil)
)

const defaultAPIURL = "https://api.deezer.com"

// Adapter is the Deezer SearchSource. Configure it via Init (no fields needed).
type Adapter struct {
	apiURL     string
	httpClient *http.Client
	client     *Client
}

// New returns an unconfigured adapter (the registry factory) using the production URL.
func New() *Adapter {
	return &Adapter{apiURL: defaultAPIURL}
}

// WithHTTPClient injects an *http.Client (test seam). Call before Init.
func (a *Adapter) WithHTTPClient(h *http.Client) *Adapter {
	a.httpClient = h
	return a
}

// WithBaseURL overrides the API host (test seam). Call before Init.
func (a *Adapter) WithBaseURL(apiURL string) *Adapter {
	a.apiURL = apiURL
	return a
}

func (a *Adapter) Type() string { return "search" }
func (a *Adapter) Name() string { return "deezer" }

// ConfigSchema is empty: Deezer's public API needs no credentials.
func (a *Adapter) ConfigSchema() registry.ConfigSchema {
	return registry.ConfigSchema{Fields: []registry.ConfigField{}}
}

func (a *Adapter) Init(cfg map[string]any) error {
	a.client = NewClient(a.apiURL, a.httpClient)
	return nil
}

// TestConnection verifies reachability with a one-result search.
func (a *Adapter) TestConnection(ctx context.Context) error {
	if a.client == nil {
		return fmt.Errorf("deezer: not initialized")
	}
	params := url.Values{}
	params.Set("q", "test")
	params.Set("limit", "1")
	var resp searchTracksResponse
	return a.client.get(ctx, "/search/track", params, &resp)
}

// --- mapping helpers ---

func id64(v int64) string { return strconv.FormatInt(v, 10) }

func yearFromReleaseDate(s string) int {
	if len(s) >= 4 {
		if y, err := strconv.Atoi(s[:4]); err == nil {
			return y
		}
	}
	return 0
}

// mapTrack converts a Deezer track. ISRC is absent from Deezer search/album
// payloads, so it stays empty — library matching falls back to metadata.
func mapTrack(t trackDTO) core.ExternalResult {
	cover := t.Album.CoverBig
	if cover == "" {
		cover = t.Album.CoverMedium
	}
	return core.ExternalResult{
		Source:           "deezer",
		ExternalID:       id64(t.ID),
		Title:            t.Title,
		Artist:           t.Artist.Name,
		Album:            t.Album.Title,
		DurationMs:       t.Duration * 1000,
		CoverURL:         cover,
		Type:             core.EntityTrack,
		ArtistExternalID: id64(t.Artist.ID),
		AlbumExternalID:  id64(t.Album.ID),
	}
}

// --- SearchSource methods ---

func (a *Adapter) Search(ctx context.Context, q string, t core.EntityType) ([]core.ExternalResult, error) {
	params := url.Values{}
	params.Set("q", q)
	params.Set("limit", "20")

	out := []core.ExternalResult{}
	switch t {
	case core.EntityAlbum:
		var resp searchAlbumsResponse
		if err := a.client.get(ctx, "/search/album", params, &resp); err != nil {
			return nil, err
		}
		for _, al := range resp.Data {
			out = append(out, core.ExternalResult{
				Source: "deezer", ExternalID: id64(al.ID), Title: al.Title,
				Artist: al.Artist.Name, CoverURL: al.CoverMedium,
				Type: core.EntityAlbum,
			})
		}
	case core.EntityArtist:
		var resp searchArtistsResponse
		if err := a.client.get(ctx, "/search/artist", params, &resp); err != nil {
			return nil, err
		}
		for _, ar := range resp.Data {
			out = append(out, core.ExternalResult{
				Source: "deezer", ExternalID: id64(ar.ID), Title: ar.Name,
				Artist: ar.Name, CoverURL: ar.PictureMedium,
				Type: core.EntityArtist,
			})
		}
	default:
		var resp searchTracksResponse
		if err := a.client.get(ctx, "/search/track", params, &resp); err != nil {
			return nil, err
		}
		for _, tr := range resp.Data {
			out = append(out, mapTrack(tr))
		}
	}
	return out, nil
}

func (a *Adapter) GetAlbum(ctx context.Context, externalID string) (core.ExternalAlbum, error) {
	var full fullAlbumDTO
	if err := a.client.get(ctx, "/album/"+url.PathEscape(externalID), url.Values{}, &full); err != nil {
		return core.ExternalAlbum{}, err
	}
	al := core.ExternalAlbum{
		Source:     "deezer",
		ExternalID: id64(full.ID),
		Name:       full.Title,
		Artist:     full.Artist.Name,
		CoverURL:   full.CoverBig,
		Year:       yearFromReleaseDate(full.ReleaseDate),
		Tracks:     []core.ExternalResult{},
	}
	for _, tr := range full.Tracks.Data {
		mt := mapTrack(tr)
		// album-tracks payloads omit the album object on each track; backfill.
		mt.Album = full.Title
		mt.AlbumExternalID = al.ExternalID
		if mt.CoverURL == "" {
			mt.CoverURL = al.CoverURL
		}
		al.Tracks = append(al.Tracks, mt)
	}
	return al, nil
}
