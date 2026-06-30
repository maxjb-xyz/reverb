// Package lastfm implements the scrobble.Scrobbler interface against the
// Last.fm API (https://www.last.fm/api).
package lastfm

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/scrobble"
)

const (
	defaultBase   = "https://ws.audioscrobbler.com/2.0/"
	lastfmAuthURL = "https://www.last.fm/api/auth/"
)

// Adapter implements scrobble.Scrobbler for Last.fm.
// Inject baseURL and client for tests; production callers use New() or Factory().
type Adapter struct {
	baseURL string
	client  *http.Client

	// config fields (set via Init)
	apiKey    string
	apiSecret string
}

// New returns an Adapter wired to the real Last.fm endpoint.
// Task 4 (registry wiring) calls this from the Factory.
func New() *Adapter {
	return &Adapter{
		baseURL: defaultBase,
		client:  &http.Client{},
	}
}

// ----------------------------------------------------------------------------
// registry.Plugin implementation
// ----------------------------------------------------------------------------

func (a *Adapter) Type() string { return "scrobbler" }
func (a *Adapter) Name() string { return "lastfm" }

func (a *Adapter) ConfigSchema() registry.ConfigSchema {
	return registry.ConfigSchema{
		Fields: []registry.ConfigField{
			{Key: "api_key", Label: "API Key", Type: "string", Required: true, Secret: false},
			{Key: "api_secret", Label: "API Secret", Type: "string", Required: true, Secret: true},
		},
	}
}

func (a *Adapter) Init(cfg map[string]any) error {
	key, _ := cfg["api_key"].(string)
	sec, _ := cfg["api_secret"].(string)
	if key == "" || sec == "" {
		return fmt.Errorf("lastfm: api_key and api_secret are required")
	}
	a.apiKey = key
	a.apiSecret = sec
	return nil
}

func (a *Adapter) TestConnection(ctx context.Context) error {
	_, _, err := a.AuthURL(ctx, scrobble.Creds{APIKey: a.apiKey, APISecret: a.apiSecret})
	return err
}

// Factory is the registry.Factory for the "lastfm" scrobbler adapter.
// Task 4 passes this to the scrobbler registry.
func Factory() registry.Plugin { return New() }

// ----------------------------------------------------------------------------
// Scrobbler interface
// ----------------------------------------------------------------------------

// AuthURL fetches a request token from Last.fm and returns the URL the user
// must visit to grant access, plus the token (needed for CompleteAuth).
func (a *Adapter) AuthURL(ctx context.Context, c scrobble.Creds) (authURL, token string, err error) {
	tok, err := a.getToken(ctx, c)
	if err != nil {
		return "", "", err
	}
	u := fmt.Sprintf("%s?api_key=%s&token=%s", lastfmAuthURL, c.APIKey, tok)
	return u, tok, nil
}

// CompleteAuth exchanges the approved token for a permanent session key and username.
func (a *Adapter) CompleteAuth(ctx context.Context, c scrobble.Creds, token string) (sessionKey, username string, err error) {
	params := map[string]string{
		"api_key": c.APIKey,
		"method":  "auth.getSession",
		"token":   token,
	}
	var resp struct {
		Session struct {
			Key  string `json:"key"`
			Name string `json:"name"`
		} `json:"session"`
		Error   int    `json:"error"`
		Message string `json:"message"`
	}
	if err := a.post(ctx, params, c.APISecret, &resp); err != nil {
		return "", "", err
	}
	if resp.Error != 0 {
		return "", "", lastfmError(resp.Error, resp.Message)
	}
	return resp.Session.Key, resp.Session.Name, nil
}

// NowPlaying updates the "now playing" indicator on Last.fm.
func (a *Adapter) NowPlaying(ctx context.Context, c scrobble.Creds, t scrobble.Track) error {
	params := map[string]string{
		"method":  "track.updateNowPlaying",
		"api_key": c.APIKey,
		"sk":      c.SessionKey,
		"artist":  t.Artist,
		"track":   t.Title,
	}
	if t.Album != "" {
		params["album"] = t.Album
	}
	if t.DurationMs > 0 {
		params["duration"] = strconv.Itoa(t.DurationMs / 1000)
	}

	var resp struct {
		Error   int    `json:"error"`
		Message string `json:"message"`
	}
	if err := a.post(ctx, params, c.APISecret, &resp); err != nil {
		return err
	}
	if resp.Error != 0 {
		return lastfmError(resp.Error, resp.Message)
	}
	return nil
}

// Scrobble submits one or more plays to Last.fm in batches of 50.
func (a *Adapter) Scrobble(ctx context.Context, c scrobble.Creds, plays []scrobble.ScrobblePlay) (accepted int, err error) {
	const batchSize = 50
	for start := 0; start < len(plays); start += batchSize {
		end := start + batchSize
		if end > len(plays) {
			end = len(plays)
		}
		n, err := a.scrobbleBatch(ctx, c, plays[start:end])
		if err != nil {
			return accepted, err
		}
		accepted += n
	}
	return accepted, nil
}

// scrobbleBatch handles a single batch of up to 50 plays.
func (a *Adapter) scrobbleBatch(ctx context.Context, c scrobble.Creds, plays []scrobble.ScrobblePlay) (int, error) {
	params := map[string]string{
		"method":  "track.scrobble",
		"api_key": c.APIKey,
		"sk":      c.SessionKey,
	}
	for i, p := range plays {
		idx := strconv.Itoa(i)
		params["artist["+idx+"]"] = p.Artist
		params["track["+idx+"]"] = p.Title
		params["timestamp["+idx+"]"] = strconv.FormatInt(p.PlayedAt, 10)
		if p.Album != "" {
			params["album["+idx+"]"] = p.Album
		}
		if p.DurationMs > 0 {
			params["duration["+idx+"]"] = strconv.Itoa(p.DurationMs / 1000)
		}
	}

	// Last.fm's scrobble response shape varies; be tolerant.
	var resp struct {
		Scrobbles struct {
			Attr struct {
				Accepted json.Number `json:"accepted"`
			} `json:"@attr"`
		} `json:"scrobbles"`
		Error   int    `json:"error"`
		Message string `json:"message"`
	}
	if err := a.post(ctx, params, c.APISecret, &resp); err != nil {
		return 0, err
	}
	if resp.Error != 0 {
		return 0, lastfmError(resp.Error, resp.Message)
	}
	n, _ := resp.Scrobbles.Attr.Accepted.Int64()
	return int(n), nil
}

// ----------------------------------------------------------------------------
// Internal helpers
// ----------------------------------------------------------------------------

// getToken fetches a short-lived request token from auth.getToken.
func (a *Adapter) getToken(ctx context.Context, c scrobble.Creds) (string, error) {
	params := map[string]string{
		"api_key": c.APIKey,
		"method":  "auth.getToken",
	}
	var resp struct {
		Token   string `json:"token"`
		Error   int    `json:"error"`
		Message string `json:"message"`
	}
	if err := a.post(ctx, params, c.APISecret, &resp); err != nil {
		return "", err
	}
	if resp.Error != 0 {
		return "", lastfmError(resp.Error, resp.Message)
	}
	return resp.Token, nil
}

// post signs params, encodes them as form data, POSTs to the Last.fm endpoint,
// and JSON-decodes the response into dst.
func (a *Adapter) post(ctx context.Context, params map[string]string, secret string, dst any) error {
	sig := apiSig(params, secret)

	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	form.Set("api_sig", sig)
	form.Set("format", "json")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("lastfm: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("lastfm: http: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("lastfm: read body: %w", err)
	}

	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("lastfm: decode response: %w", err)
	}
	return nil
}

// apiSig computes the Last.fm API signature:
//
//	md5( concat(sorted(key+value) for each param EXCLUDING format/callback) + secret )
//
// This is the exact algorithm Last.fm documents. The sort order is
// lexicographic on the key name. format and callback must NOT be included
// (even if present in params) — they are excluded before hashing.
func apiSig(params map[string]string, secret string) string {
	excluded := map[string]bool{"format": true, "callback": true}

	keys := make([]string, 0, len(params))
	for k := range params {
		if !excluded[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteString(params[k])
	}
	sb.WriteString(secret)

	sum := md5.Sum([]byte(sb.String()))
	return fmt.Sprintf("%x", sum)
}

// lastfmError maps a Last.fm error code to the appropriate Go error.
// Code 9 = Invalid session key → scrobble.ErrAuth.
func lastfmError(code int, msg string) error {
	if code == 9 {
		return fmt.Errorf("%w: %s", scrobble.ErrAuth, msg)
	}
	return fmt.Errorf("lastfm: error %d: %s", code, msg)
}
