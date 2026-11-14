// Package sidecar is the HTTP analyzer: it sends each song to an analysis
// service the owner runs (Reverb's reference one is in sidecar/analysis, a
// CLAP model that is much faster on a GPU) and stores what comes back.
//
// Protocol:
//
//	GET  {url}/v1/health   → 200 {"model": "...", "device": "cuda"}
//	POST {url}/v1/analyze  multipart field "audio" (the song file)
//	                       → 200 {"model", "embedding": [...], "tempo", "energy"}
//	                       → 503 while the model is still loading
package sidecar

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/maxjb-xyz/reverb/internal/analysis"
	"github.com/maxjb-xyz/reverb/internal/registry"
)

// Name is the registry name.
const Name = "audio-sidecar"

const (
	defaultTimeout = 2 * time.Minute
	maxResponse    = 1 << 20
)

// Adapter is the HTTP analyzer.
type Adapter struct {
	url     string
	apiKey  string
	timeout time.Duration
	hc      *http.Client
}

// New returns an unconfigured adapter (the registry factory).
func New() *Adapter { return &Adapter{hc: &http.Client{}, timeout: defaultTimeout} }

// WithHTTPClient injects an *http.Client (test seam).
func (a *Adapter) WithHTTPClient(h *http.Client) *Adapter {
	a.hc = h
	return a
}

func (a *Adapter) Type() string { return analysis.Type }
func (a *Adapter) Name() string { return Name }

func (a *Adapter) ConfigSchema() registry.ConfigSchema {
	return registry.ConfigSchema{Fields: []registry.ConfigField{
		{Key: "url", Label: "Service URL", Type: "string", Required: true,
			Help: "Where the analysis service listens, for example http://reverb-analysis:8765. Run Reverb's reference service (sidecar/analysis in the repository) as a separate container; it is much faster with a GPU."},
		{Key: "api_key", Label: "API key", Type: "string", Secret: true, Help: "Optional. Sent as a Bearer token if the service asks for one."},
		{Key: "timeout_seconds", Label: "Timeout per song (seconds)", Type: "number", Help: "Defaults to 120."},
	}}
}

func (a *Adapter) Init(cfg map[string]any) error {
	u, _ := cfg["url"].(string)
	a.url = strings.TrimRight(strings.TrimSpace(u), "/")
	if a.url == "" {
		return fmt.Errorf("audio-sidecar: url is required")
	}
	if !strings.HasPrefix(a.url, "http://") && !strings.HasPrefix(a.url, "https://") {
		return fmt.Errorf("audio-sidecar: url must start with http:// or https://")
	}
	k, _ := cfg["api_key"].(string)
	a.apiKey = strings.TrimSpace(k)
	switch v := cfg["timeout_seconds"].(type) {
	case float64:
		if v > 0 {
			a.timeout = time.Duration(v * float64(time.Second))
		}
	case string:
		if s := strings.TrimSpace(v); s != "" {
			f, err := strconv.ParseFloat(s, 64)
			if err != nil || f <= 0 {
				return fmt.Errorf("audio-sidecar: timeout_seconds must be a positive number")
			}
			a.timeout = time.Duration(f * float64(time.Second))
		}
	}
	return nil
}

func (a *Adapter) auth(r *http.Request) {
	if a.apiKey != "" {
		r.Header.Set("Authorization", "Bearer "+a.apiKey)
	}
}

// TestConnection checks the service answers its health check.
func (a *Adapter) TestConnection(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.url+"/v1/health", nil)
	if err != nil {
		return err
	}
	a.auth(req)
	res, err := a.hc.Do(req)
	if err != nil {
		return fmt.Errorf("audio-sidecar: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusServiceUnavailable {
		return fmt.Errorf("audio-sidecar: the service is still loading its model")
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("audio-sidecar: health check returned %s", res.Status)
	}
	var h struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxResponse)).Decode(&h); err != nil || h.Model == "" {
		return fmt.Errorf("audio-sidecar: health check didn't name a model")
	}
	return nil
}

// Analyze streams the song to the service.
func (a *Adapter) Analyze(ctx context.Context, name string, audio io.Reader) (analysis.Features, error) {
	if a.url == "" {
		return analysis.Features{}, fmt.Errorf("audio-sidecar: not initialized")
	}
	if err := ctx.Err(); err != nil {
		return analysis.Features{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		part, err := mw.CreateFormFile("audio", safeName(name))
		if err == nil {
			_, err = io.Copy(part, audio)
		}
		if err == nil {
			err = mw.Close()
		}
		pw.CloseWithError(err)
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.url+"/v1/analyze", pr)
	if err != nil {
		_ = pr.CloseWithError(err)
		return analysis.Features{}, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	a.auth(req)
	res, err := a.hc.Do(req)
	if err != nil {
		_ = pr.CloseWithError(err)
		return analysis.Features{}, fmt.Errorf("audio-sidecar: %w", err)
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusServiceUnavailable || res.StatusCode == http.StatusTooManyRequests:
		return analysis.Features{}, analysis.ErrUnavailable
	case res.StatusCode != http.StatusOK:
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 300))
		return analysis.Features{}, fmt.Errorf("audio-sidecar: %s: %s", res.Status, strings.TrimSpace(string(msg)))
	}
	var f analysis.Features
	if err := json.NewDecoder(io.LimitReader(res.Body, maxResponse)).Decode(&f); err != nil {
		return analysis.Features{}, fmt.Errorf("audio-sidecar: bad answer: %w", err)
	}
	if err := f.Validate(); err != nil {
		return analysis.Features{}, err
	}
	return f, nil
}

// safeName keeps only the base name and a known extension: the service gets
// a format hint, not the owner's folder layout.
func safeName(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	ext := ""
	if i := strings.LastIndex(name, "."); i >= 0 {
		ext = strings.ToLower(name[i:])
	}
	if len(ext) > 6 || strings.ContainsAny(ext, " \"") {
		ext = ""
	}
	return "song" + ext
}
