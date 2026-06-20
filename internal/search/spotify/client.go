package spotify

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Client is a low-level Spotify Web API client using client-credentials OAuth.
// Base URLs and *http.Client are injectable so tests run against httptest.
type Client struct {
	accountsURL  string
	apiURL       string
	clientID     string
	clientSecret string
	http         *http.Client
	now          func() time.Time

	mu       sync.Mutex
	token_   string
	tokenExp time.Time
}

func NewClient(accountsURL, apiURL, clientID, clientSecret string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		accountsURL:  strings.TrimRight(accountsURL, "/"),
		apiURL:       strings.TrimRight(apiURL, "/"),
		clientID:     clientID,
		clientSecret: clientSecret,
		http:         httpClient,
		now:          time.Now,
	}
}

// token returns a cached access token, refreshing it when empty or near expiry.
func (c *Client) token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token_ != "" && c.now().Before(c.tokenExp) {
		return c.token_, nil
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.accountsURL+"/api/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	basic := base64.StdEncoding.EncodeToString([]byte(c.clientID + ":" + c.clientSecret))
	req.Header.Set("Authorization", "Basic "+basic)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("spotify token: HTTP %d: %s", resp.StatusCode, string(body))
	}
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", fmt.Errorf("spotify token: decode: %w", err)
	}
	if tr.AccessToken == "" {
		return "", fmt.Errorf("spotify token: empty access_token")
	}
	c.token_ = tr.AccessToken
	// Refresh 60s early to avoid using a just-expired token. Clamp at 0 so a
	// short expires_in can't produce a negative window and an infinite refetch.
	secs := tr.ExpiresIn - 60
	if secs < 0 {
		secs = 0
	}
	c.tokenExp = c.now().Add(time.Duration(secs) * time.Second)
	return c.token_, nil
}

// apiGet performs an authed GET against apiURL+path with query q, decoding the
// JSON body into out.
func (c *Client) apiGet(ctx context.Context, path string, q url.Values, out any) error {
	tok, err := c.token(ctx)
	if err != nil {
		return err
	}
	u := c.apiURL + path
	if enc := q.Encode(); enc != "" {
		u += "?" + enc
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("spotify GET %s: HTTP %d: %s", path, resp.StatusCode, string(body))
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("spotify GET %s: decode: %w", path, err)
		}
	}
	return nil
}
