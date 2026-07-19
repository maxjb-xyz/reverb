package lyrics

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type Query struct {
	Artist, Title, Album string
	DurationMs           int
}

// maxSearchDurationDiffSeconds bounds how far a /api/search candidate's
// duration may differ from the query duration before it's rejected. Without
// this bound the "closest duration wins" fallback can confidently return
// lyrics for a completely different song on a genuine miss.
const maxSearchDurationDiffSeconds = 15.0

// LRCLibClient talks to lrclib.net (no API key; identify via User-Agent).
type LRCLibClient struct {
	BaseURL   string // default "https://lrclib.net"
	UserAgent string
	HTTP      *http.Client
}

type lrclibRecord struct {
	Duration     float64 `json:"duration"`
	SyncedLyrics *string `json:"syncedLyrics"`
	PlainLyrics  *string `json:"plainLyrics"`
}

func (r lrclibRecord) body() (string, bool) {
	if r.SyncedLyrics != nil && *r.SyncedLyrics != "" {
		return *r.SyncedLyrics, true
	}
	if r.PlainLyrics != nil && *r.PlainLyrics != "" {
		return *r.PlainLyrics, true
	}
	return "", false
}

// Fetch returns raw lyrics text (synced LRC preferred over plain).
// found=false with nil err means a genuine miss (cacheable as none).
// A non-nil err means a transient failure (do NOT negative-cache).
func (c *LRCLibClient) Fetch(ctx context.Context, q Query) (string, bool, error) {
	// Exact match first.
	get := url.Values{
		"artist_name": {q.Artist},
		"track_name":  {q.Title},
		"album_name":  {q.Album},
		"duration":    {strconv.Itoa(int(math.Round(float64(q.DurationMs) / 1000)))},
	}
	var rec lrclibRecord
	status, err := c.getJSON(ctx, "/api/get?"+get.Encode(), &rec)
	if err != nil {
		return "", false, err
	}
	if status == http.StatusOK {
		if body, ok := rec.body(); ok {
			return body, true, nil
		}
	}
	// Without a known duration we have nothing to bound the search fallback
	// against, so guessing (e.g. picking the shortest result) risks
	// confidently-wrong lyrics. Treat it as a clean miss instead.
	if q.DurationMs == 0 {
		return "", false, nil
	}
	// One search fallback: closest duration wins, but only within
	// maxSearchDurationDiffSeconds — otherwise it's a miss, not a guess.
	search := url.Values{"artist_name": {q.Artist}, "track_name": {q.Title}}
	var recs []lrclibRecord
	status, err = c.getJSON(ctx, "/api/search?"+search.Encode(), &recs)
	if err != nil {
		return "", false, err
	}
	if status != http.StatusOK || len(recs) == 0 {
		return "", false, nil
	}
	best, bestDiff := -1, math.MaxFloat64
	for i, r := range recs {
		if _, ok := r.body(); !ok {
			continue
		}
		diff := math.Abs(r.Duration - float64(q.DurationMs)/1000)
		if diff > maxSearchDurationDiffSeconds {
			continue
		}
		if diff < bestDiff {
			best, bestDiff = i, diff
		}
	}
	if best < 0 {
		return "", false, nil
	}
	body, _ := recs[best].body()
	return body, true, nil
}

// getJSON performs a GET and decodes 200 bodies into out. 404 returns
// (404, nil) so callers treat it as a miss; other non-2xx are errors.
func (c *LRCLibClient) getJSON(ctx context.Context, path string, out any) (int, error) {
	base := c.BaseURL
	if base == "" {
		base = "https://lrclib.net"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return 0, err
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return resp.StatusCode, json.NewDecoder(resp.Body).Decode(out)
	case http.StatusNotFound:
		return resp.StatusCode, nil
	default:
		return resp.StatusCode, fmt.Errorf("lrclib %s: status %d", path, resp.StatusCode)
	}
}
