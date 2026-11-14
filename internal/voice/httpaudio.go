package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// MaxClipBytes caps a single rendered clip; a DJ line is a few seconds long.
const MaxClipBytes = 8 << 20

// DefaultHTTPClient is used by adapters that are not given one.
var DefaultHTTPClient = &http.Client{Timeout: 60 * time.Second}

// PostJSONForAudio posts body as JSON and returns the audio in the answer. Any
// status other than 2xx, or an answer that is not audio, is an error that
// carries a short excerpt of what the server said.
func PostJSONForAudio(ctx context.Context, hc *http.Client, url string, headers map[string]string, body any) (Audio, error) {
	if hc == nil {
		hc = DefaultHTTPClient
	}
	b, err := json.Marshal(body)
	if err != nil {
		return Audio{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return Audio{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "audio/*")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return Audio{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return Audio{}, fmt.Errorf("voice: HTTP %d: %s", resp.StatusCode, excerpt(resp.Body))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxClipBytes+1))
	if err != nil {
		return Audio{}, err
	}
	if len(data) > MaxClipBytes {
		return Audio{}, fmt.Errorf("voice: clip is larger than %d bytes", MaxClipBytes)
	}
	if len(data) == 0 {
		return Audio{}, fmt.Errorf("voice: empty answer")
	}
	ctype := resp.Header.Get("Content-Type")
	if strings.HasPrefix(ctype, "application/json") || strings.HasPrefix(ctype, "text/") {
		return Audio{}, fmt.Errorf("voice: expected audio, got %s: %s", ctype, excerpt(bytes.NewReader(data)))
	}
	if !strings.HasPrefix(ctype, "audio/") {
		// Some servers answer application/octet-stream; we asked for mp3.
		ctype = "audio/mpeg"
	}
	return Audio{Data: data, ContentType: ctype}, nil
}

func excerpt(r io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(r, 300))
	return strings.TrimSpace(string(b))
}
