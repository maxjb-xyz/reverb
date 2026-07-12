package deezer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Client is a low-level Deezer public-API client (no auth required). The base
// URL and *http.Client are injectable so tests run against httptest.
type Client struct {
	apiURL string
	http   *http.Client
}

func NewClient(apiURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{apiURL: strings.TrimRight(apiURL, "/"), http: httpClient}
}

// get performs a GET against apiURL+path with query q, decoding JSON into out.
// Deezer signals errors in-band: HTTP 200 with an {"error": {...}} body — both
// transport-level and in-band errors are surfaced as Go errors.
func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	u := c.apiURL + path
	if enc := q.Encode(); enc != "" {
		u += "?" + enc
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("deezer GET %s: HTTP %d: %s", path, resp.StatusCode, string(body))
	}
	var env errEnvelope
	if err := json.Unmarshal(body, &env); err == nil && env.Error != nil {
		return fmt.Errorf("deezer GET %s: %s (code %d)", path, env.Error.Message, env.Error.Code)
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("deezer GET %s: decode: %w", path, err)
		}
	}
	return nil
}
