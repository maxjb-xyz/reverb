package spotify

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/maximusjb/crate/internal/core"
	"github.com/maximusjb/crate/internal/registry"
	"github.com/maximusjb/crate/internal/search"
)

var (
	_ search.SearchSource = (*Adapter)(nil)
	_ registry.Plugin     = (*Adapter)(nil)
)

const (
	defaultAccountsURL = "https://accounts.spotify.com"
	defaultAPIURL      = "https://api.spotify.com/v1"
)

// Adapter is the Spotify SearchSource. Configure it via Init.
type Adapter struct {
	clientID     string
	clientSecret string
	accountsURL  string
	apiURL       string
	httpClient   *http.Client
	client       *Client
}

// New returns an unconfigured adapter (the registry factory) using production URLs.
func New() *Adapter {
	return &Adapter{accountsURL: defaultAccountsURL, apiURL: defaultAPIURL}
}

// WithHTTPClient injects an *http.Client (test seam). Call before Init.
func (a *Adapter) WithHTTPClient(h *http.Client) *Adapter {
	a.httpClient = h
	return a
}

// WithBaseURLs overrides the OAuth + API hosts (test seam). Call before Init.
func (a *Adapter) WithBaseURLs(accountsURL, apiURL string) *Adapter {
	a.accountsURL = accountsURL
	a.apiURL = apiURL
	return a
}

func (a *Adapter) Type() string { return "search" }
func (a *Adapter) Name() string { return "spotify" }

func (a *Adapter) ConfigSchema() registry.ConfigSchema {
	return registry.ConfigSchema{Fields: []registry.ConfigField{
		{Key: "client_id", Label: "Client ID", Type: "string", Required: true},
		{Key: "client_secret", Label: "Client Secret", Type: "string", Required: true, Secret: true},
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
	a.clientID = cfgString(cfg, "client_id")
	a.clientSecret = cfgString(cfg, "client_secret")
	if a.clientID == "" || a.clientSecret == "" {
		return fmt.Errorf("spotify: client_id and client_secret are required")
	}
	a.client = NewClient(a.accountsURL, a.apiURL, a.clientID, a.clientSecret, a.httpClient)
	return nil
}

// TestConnection verifies credentials by fetching a token.
func (a *Adapter) TestConnection(ctx context.Context) error {
	if a.client == nil {
		return fmt.Errorf("spotify: not initialized")
	}
	_, err := a.client.token(ctx)
	return err
}

// --- mapping helpers ---

func firstImage(imgs []imageDTO) string {
	if len(imgs) > 0 {
		return imgs[0].URL
	}
	return ""
}

func yearFromReleaseDate(s string) int {
	if len(s) >= 4 {
		if y, err := strconv.Atoi(s[:4]); err == nil {
			return y
		}
	}
	return 0
}

func artistName(arts []artistRefDTO) string {
	if len(arts) > 0 {
		return arts[0].Name
	}
	return ""
}

func (a *Adapter) mapTrack(t trackDTO) core.ExternalResult {
	return core.ExternalResult{
		Source:     "spotify",
		ExternalID: t.ID,
		Title:      t.Name,
		Artist:     artistName(t.Artists),
		Album:      t.Album.Name,
		DurationMs: t.DurationMs,
		ISRC:       t.ExternalIDs.ISRC,
		CoverURL:   firstImage(t.Album.Images),
		Type:       core.EntityTrack,
	}
}

// --- SearchSource methods ---

func (a *Adapter) Search(ctx context.Context, q string, t core.EntityType) ([]core.ExternalResult, error) {
	stype := "track"
	switch t {
	case core.EntityAlbum:
		stype = "album"
	case core.EntityArtist:
		stype = "artist"
	}
	params := url.Values{}
	params.Set("q", q)
	params.Set("type", stype)
	params.Set("limit", "20")

	var resp searchResponse
	if err := a.client.apiGet(ctx, "/search", params, &resp); err != nil {
		return nil, err
	}

	out := []core.ExternalResult{}
	switch t {
	case core.EntityAlbum:
		if resp.Albums != nil {
			for _, al := range resp.Albums.Items {
				out = append(out, core.ExternalResult{
					Source: "spotify", ExternalID: al.ID, Title: al.Name,
					Artist: artistName(al.Artists), CoverURL: firstImage(al.Images),
					Type: core.EntityAlbum,
				})
			}
		}
	case core.EntityArtist:
		if resp.Artists != nil {
			for _, ar := range resp.Artists.Items {
				out = append(out, core.ExternalResult{
					Source: "spotify", ExternalID: ar.ID, Title: ar.Name,
					Artist: ar.Name, CoverURL: firstImage(ar.Images),
					Type: core.EntityArtist,
				})
			}
		}
	default:
		if resp.Tracks != nil {
			for _, tr := range resp.Tracks.Items {
				out = append(out, a.mapTrack(tr))
			}
		}
	}
	return out, nil
}

func (a *Adapter) GetAlbum(ctx context.Context, externalID string) (core.ExternalAlbum, error) {
	var full fullAlbumDTO
	if err := a.client.apiGet(ctx, "/albums/"+url.PathEscape(externalID), url.Values{}, &full); err != nil {
		return core.ExternalAlbum{}, err
	}
	al := core.ExternalAlbum{
		Source:     "spotify",
		ExternalID: full.ID,
		Name:       full.Name,
		Artist:     artistName(full.Artists),
		CoverURL:   firstImage(full.Images),
		Year:       yearFromReleaseDate(full.ReleaseDate),
		Tracks:     []core.ExternalResult{},
	}
	for _, tr := range full.Tracks.Items {
		mt := a.mapTrack(tr)
		// album-tracks endpoint omits album images on each track; backfill name/cover.
		mt.Album = full.Name
		if mt.CoverURL == "" {
			mt.CoverURL = al.CoverURL
		}
		al.Tracks = append(al.Tracks, mt)
	}
	return al, nil
}
