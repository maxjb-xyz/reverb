package subsonic

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	apiVersion = "1.16.1"
	// Navidrome keys per-player settings on this client name, and settings like
	// Subsonic.DefaultReportRealPath only seed NEWLY-registered players — an
	// existing player row keeps the value it was created with forever. Bump the
	// suffix whenever a new player-level default must actually take effect
	// (v2: real paths for waveform peaks).
	clientName = "reverb-v2"
)

// Client is a low-level Subsonic API client using token auth. The *http.Client
// is injectable so tests can drive it against an httptest.Server.
type Client struct {
	baseURL  string
	username string
	password string
	http     *http.Client
}

func NewClient(baseURL, username, password string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		username: username,
		password: password,
		http:     httpClient,
	}
}

func newSalt() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func token(password, salt string) string {
	sum := md5.Sum([]byte(password + salt))
	return hex.EncodeToString(sum[:])
}

// buildURL appends auth + fixed params to a copy of params and returns the URL.
func (c *Client) buildURL(endpoint string, params url.Values) string {
	q := url.Values{}
	for k, vs := range params {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	salt := newSalt()
	q.Set("u", c.username)
	q.Set("t", token(c.password, salt))
	q.Set("s", salt)
	q.Set("v", apiVersion)
	q.Set("c", clientName)
	q.Set("f", "json")
	return fmt.Sprintf("%s/rest/%s?%s", c.baseURL, endpoint, q.Encode())
}

// RawGet performs an authed GET and returns the raw response (caller closes Body).
// rangeHeader, when non-empty, is forwarded as the inbound Range request header.
func (c *Client) RawGet(ctx context.Context, endpoint string, params url.Values, rangeHeader string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.buildURL(endpoint, params), nil)
	if err != nil {
		return nil, err
	}
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	return c.http.Do(req)
}

// GetJSON performs RawGet, decodes the subsonic-response envelope, validates the
// status, and unmarshals the response payload into out (out must be a *subsonicResponse
// or nil to skip payload decoding). It returns an error for status == "failed".
func (c *Client) GetJSON(ctx context.Context, endpoint string, params url.Values, out *subsonicResponse) error {
	resp, err := c.RawGet(ctx, endpoint, params, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("subsonic %s: HTTP %d", endpoint, resp.StatusCode)
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("subsonic %s: decode: %w", endpoint, err)
	}
	if env.Response.Status != "ok" {
		if env.Response.Error != nil {
			return fmt.Errorf("subsonic %s: error %d: %s", endpoint, env.Response.Error.Code, env.Response.Error.Message)
		}
		return fmt.Errorf("subsonic %s: status %q", endpoint, env.Response.Status)
	}
	if out != nil {
		*out = env.Response
	}
	return nil
}

func (c *Client) Ping(ctx context.Context) error {
	return c.GetJSON(ctx, "ping", nil, nil)
}
