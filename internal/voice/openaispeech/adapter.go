// Package openaispeech is a Voice adapter for any server that speaks the
// OpenAI speech API (POST {base}/audio/speech): Kokoro-FastAPI, LocalAI,
// openedai-speech, OpenAI itself and others.
package openaispeech

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/voice"
)

const (
	DefaultModel = "kokoro"
	DefaultVoice = "af_heart"
)

// Adapter is the OpenAI-compatible speech Voice.
type Adapter struct {
	baseURL string
	apiKey  string
	model   string
	voice   string
	hc      *http.Client
}

// New returns an unconfigured adapter (the registry factory).
func New() *Adapter { return &Adapter{} }

// WithHTTPClient injects an *http.Client (test seam). Call before Init.
func (a *Adapter) WithHTTPClient(h *http.Client) *Adapter {
	a.hc = h
	return a
}

func (a *Adapter) Type() string { return voice.Type }
func (a *Adapter) Name() string { return "openai-speech" }

func (a *Adapter) ConfigSchema() registry.ConfigSchema {
	return registry.ConfigSchema{Fields: []registry.ConfigField{
		{Key: "base_url", Label: "Server URL", Type: "string", Required: true,
			Help: "The speech server's API root, ending before /audio/speech. For Kokoro-FastAPI that is http://<host>:8880/v1; for OpenAI, https://api.openai.com/v1."},
		{Key: "api_key", Label: "API key", Type: "string", Secret: true,
			Help: "Only needed for hosted services. Local servers usually take none."},
		{Key: "model", Label: "Model", Type: "string", Help: "Defaults to " + DefaultModel + "."},
		{Key: "voice", Label: "Voice", Type: "string", Help: "Defaults to " + DefaultVoice + "."},
	}}
}

func cfgString(cfg map[string]any, key string) string {
	if s, ok := cfg[key].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func (a *Adapter) Init(cfg map[string]any) error {
	a.baseURL = strings.TrimRight(cfgString(cfg, "base_url"), "/")
	if a.baseURL == "" {
		return fmt.Errorf("openai-speech: base_url is required")
	}
	a.apiKey = cfgString(cfg, "api_key")
	a.model = cfgString(cfg, "model")
	if a.model == "" {
		a.model = DefaultModel
	}
	a.voice = cfgString(cfg, "voice")
	if a.voice == "" {
		a.voice = DefaultVoice
	}
	return nil
}

// Identity names this voice and its settings, for clip caching.
func (a *Adapter) Identity() string {
	return a.Name() + ":" + a.baseURL + ":" + a.model + ":" + a.voice
}

// TestConnection renders one short word.
func (a *Adapter) TestConnection(ctx context.Context) error {
	_, err := a.Synthesize(ctx, "Testing.")
	return err
}

func (a *Adapter) Synthesize(ctx context.Context, text string) (voice.Audio, error) {
	if err := voice.CheckText(text); err != nil {
		return voice.Audio{}, err
	}
	if a.baseURL == "" {
		return voice.Audio{}, fmt.Errorf("openai-speech: not initialized")
	}
	headers := map[string]string{}
	if a.apiKey != "" {
		headers["Authorization"] = "Bearer " + a.apiKey
	}
	return voice.PostJSONForAudio(ctx, a.hc, a.baseURL+"/audio/speech", headers, map[string]any{
		"input":           text,
		"model":           a.model,
		"voice":           a.voice,
		"response_format": "mp3",
	})
}
