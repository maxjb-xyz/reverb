// Package elevenlabs is a Voice adapter for the ElevenLabs text-to-speech API.
package elevenlabs

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/voice"
)

const (
	defaultBaseURL = "https://api.elevenlabs.io"
	// DefaultVoiceID is "Rachel", one of the voices every account has.
	DefaultVoiceID = "21m00Tcm4TlvDq8ikWAM"
	// DefaultModel is the fast, low-cost model; DJ lines are short.
	DefaultModel = "eleven_flash_v2_5"
	outputFormat = "mp3_44100_128"
)

// Adapter is the ElevenLabs Voice.
type Adapter struct {
	baseURL string
	apiKey  string
	voiceID string
	model   string
	hc      *http.Client
}

// New returns an unconfigured adapter (the registry factory).
func New() *Adapter { return &Adapter{baseURL: defaultBaseURL} }

// WithHTTPClient injects an *http.Client (test seam). Call before Init.
func (a *Adapter) WithHTTPClient(h *http.Client) *Adapter {
	a.hc = h
	return a
}

func (a *Adapter) Type() string { return voice.Type }
func (a *Adapter) Name() string { return "elevenlabs" }

func (a *Adapter) ConfigSchema() registry.ConfigSchema {
	return registry.ConfigSchema{Fields: []registry.ConfigField{
		{Key: "api_key", Label: "API key", Type: "string", Required: true, Secret: true,
			Help: "From elevenlabs.io under Developers > API keys. It can also be set with REVERB_ELEVENLABS_API_KEY."},
		{Key: "voice_id", Label: "Voice ID", Type: "string",
			Help: "Copy it from the ElevenLabs voice library. Defaults to Rachel."},
		{Key: "model_id", Label: "Model", Type: "string", Help: "Defaults to " + DefaultModel + "."},
	}}
}

func cfgString(cfg map[string]any, key string) string {
	if s, ok := cfg[key].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func (a *Adapter) Init(cfg map[string]any) error {
	a.apiKey = cfgString(cfg, "api_key")
	if a.apiKey == "" {
		return fmt.Errorf("elevenlabs: api_key is required")
	}
	a.voiceID = cfgString(cfg, "voice_id")
	if a.voiceID == "" {
		a.voiceID = DefaultVoiceID
	}
	a.model = cfgString(cfg, "model_id")
	if a.model == "" {
		a.model = DefaultModel
	}
	return nil
}

// Identity names this voice and its settings, for clip caching.
func (a *Adapter) Identity() string {
	return a.Name() + ":" + a.voiceID + ":" + a.model
}

// TestConnection looks the configured voice up, which checks the key without
// spending characters.
func (a *Adapter) TestConnection(ctx context.Context) error {
	if a.apiKey == "" {
		return fmt.Errorf("elevenlabs: not initialized")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL+"/v1/voices/"+url.PathEscape(a.voiceID), nil)
	if err != nil {
		return err
	}
	req.Header.Set("xi-api-key", a.apiKey)
	hc := a.hc
	if hc == nil {
		hc = voice.DefaultHTTPClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("elevenlabs: the API key was refused")
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusBadRequest:
		return fmt.Errorf("elevenlabs: voice %q was not found", a.voiceID)
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return fmt.Errorf("elevenlabs: HTTP %d", resp.StatusCode)
	}
	return nil
}

func (a *Adapter) Synthesize(ctx context.Context, text string) (voice.Audio, error) {
	if err := voice.CheckText(text); err != nil {
		return voice.Audio{}, err
	}
	if a.apiKey == "" {
		return voice.Audio{}, fmt.Errorf("elevenlabs: not initialized")
	}
	u := a.baseURL + "/v1/text-to-speech/" + url.PathEscape(a.voiceID) + "?output_format=" + outputFormat
	return voice.PostJSONForAudio(ctx, a.hc, u, map[string]string{"xi-api-key": a.apiKey}, map[string]any{
		"text":     text,
		"model_id": a.model,
	})
}
