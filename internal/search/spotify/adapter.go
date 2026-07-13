package spotify

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/search"
)

var (
	_ search.SearchSource           = (*Adapter)(nil)
	_ registry.Plugin               = (*Adapter)(nil)
	_ search.DiscographyProvider    = (*Adapter)(nil)
	_ search.PlaylistProvider       = (*Adapter)(nil)
	_ search.PlaylistSearchProvider = (*Adapter)(nil)
	_ search.TrackProvider          = (*Adapter)(nil)
	_ search.ArtistProvider         = (*Adapter)(nil)
)

var playlistIDRe = regexp.MustCompile(`(?:open\.spotify\.com/playlist/|spotify:playlist:)([A-Za-z0-9]+)`)

// ParsePlaylistID extracts a Spotify playlist id from a URL or URI; ok=false if absent.
func ParsePlaylistID(s string) (string, bool) {
	m := playlistIDRe.FindStringSubmatch(s)
	if len(m) < 2 {
		return "", false
	}
	return m[1], true
}

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

func artistID(arts []artistRefDTO) string {
	if len(arts) > 0 {
		return arts[0].ID
	}
	return ""
}

func (a *Adapter) mapTrack(t trackDTO) core.ExternalResult {
	return core.ExternalResult{
		Source:           "spotify",
		ExternalID:       t.ID,
		Title:            t.Name,
		Artist:           artistName(t.Artists),
		Album:            t.Album.Name,
		DurationMs:       t.DurationMs,
		ISRC:             t.ExternalIDs.ISRC,
		CoverURL:         firstImage(t.Album.Images),
		Type:             core.EntityTrack,
		ArtistExternalID: artistID(t.Artists),
		AlbumExternalID:  t.Album.ID,
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
	case core.EntityPlaylist:
		stype = "playlist"
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
	case core.EntityPlaylist:
		if resp.Playlists != nil {
			for _, pl := range resp.Playlists.Items {
				out = append(out, core.ExternalResult{
					Source: "spotify", ExternalID: pl.ID, Title: pl.Name,
					Artist: pl.Owner.DisplayName, CoverURL: firstImage(pl.Images),
					Type: core.EntityPlaylist,
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

// SearchPlaylists finds public Spotify playlists for a normal Everywhere search.
// It is kept optional so providers without a playlist catalog remain unaffected.
func (a *Adapter) SearchPlaylists(ctx context.Context, q string) ([]core.ExternalResult, error) {
	return a.Search(ctx, q, core.EntityPlaylist)
}

// GetTrack fetches one track so durable catalog rows can recover its parent
// artist and album identities for navigation after the original search is gone.
func (a *Adapter) GetTrack(ctx context.Context, externalID string) (core.ExternalResult, error) {
	var dto trackDTO
	if err := a.client.apiGet(ctx, "/tracks/"+url.PathEscape(externalID), url.Values{}, &dto); err != nil {
		return core.ExternalResult{}, err
	}
	return a.mapTrack(dto), nil
}

// GetArtist fetches the artist profile (name + images) from GET /artists/{id}.
func (a *Adapter) GetArtist(ctx context.Context, externalID string) (core.ExternalArtist, error) {
	var dto artistDTO
	if err := a.client.apiGet(ctx, "/artists/"+url.PathEscape(externalID), url.Values{}, &dto); err != nil {
		return core.ExternalArtist{}, err
	}
	return core.ExternalArtist{
		Source:     "spotify",
		ExternalID: dto.ID,
		Name:       dto.Name,
		CoverURL:   firstImage(dto.Images),
	}, nil
}

// maxDiscographyPages caps pagination at ~300 albums (6 pages × 50 per page) to
// avoid 13.8s hangs for prolific/composer artists (e.g. Chopin has 1289+ entries).
const maxDiscographyPages = 6

// GetArtistDiscography returns the artist's Albums + Singles/EPs (no tracklists).
// It follows Spotify pagination and asks only for album,single groups so
// compilations and "appears on" never reach the dedup stage.
func (a *Adapter) GetArtistDiscography(ctx context.Context, externalID string) ([]core.ExternalAlbum, error) {
	out := []core.ExternalAlbum{}
	params := url.Values{}
	params.Set("include_groups", "album,single")
	params.Set("limit", "50")
	offset := 0
	for page := 0; page < maxDiscographyPages; page++ {
		params.Set("offset", strconv.Itoa(offset))
		var resp artistAlbumsResponse
		if err := a.client.apiGet(ctx, "/artists/"+url.PathEscape(externalID)+"/albums", params, &resp); err != nil {
			return nil, err
		}
		for _, it := range resp.Items {
			kind := "album"
			if it.AlbumType == "single" {
				kind = "single"
			}
			out = append(out, core.ExternalAlbum{
				Source:      "spotify",
				ExternalID:  it.ID,
				Name:        it.Name,
				Artist:      artistName(it.Artists),
				CoverURL:    firstImage(it.Images),
				Year:        yearFromReleaseDate(it.ReleaseDate),
				Kind:        kind,
				TotalTracks: it.TotalTracks,
				Tracks:      []core.ExternalResult{},
			})
		}
		if resp.Next == "" || len(resp.Items) == 0 {
			break
		}
		offset += len(resp.Items)
	}
	return out, nil
}

// GetPlaylist fetches a public Spotify playlist's metadata + all tracks (paginated).
func (a *Adapter) GetPlaylist(ctx context.Context, externalID string) (core.ExternalPlaylist, error) {
	var obj playlistObjectDTO
	if err := a.client.apiGet(ctx, "/playlists/"+url.PathEscape(externalID), url.Values{}, &obj); err != nil {
		return core.ExternalPlaylist{}, err
	}
	pl := core.ExternalPlaylist{
		Source: "spotify", ExternalID: externalID, Name: obj.Name,
		CoverURL: firstImage(obj.Images), Tracks: []core.ExternalResult{},
	}
	page := obj.Tracks
	seen := 0 // total items processed (including skipped local tracks)
	for {
		for _, it := range page.Items {
			seen++                 // advance for every item, regardless of whether we keep it
			if it.Track.ID == "" { // local/unavailable tracks have no id
				continue
			}
			pl.Tracks = append(pl.Tracks, a.mapTrack(it.Track))
		}
		if page.Next == "" {
			break
		}
		// Follow the absolute next URL; apiGet takes a path+params, so re-issue the
		// tracks endpoint with an offset derived from items seen (NOT accepted tracks),
		// so the offset always advances by the full page size even when all items were
		// skipped (e.g. a page of 100 local tracks), preventing an infinite loop.
		params := url.Values{}
		params.Set("offset", strconv.Itoa(seen))
		params.Set("limit", "100")
		var next playlistPageDTO
		if err := a.client.apiGet(ctx, "/playlists/"+url.PathEscape(externalID)+"/tracks", params, &next); err != nil {
			return core.ExternalPlaylist{}, err
		}
		if len(next.Items) == 0 {
			break
		}
		page = next
	}
	return pl, nil
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
