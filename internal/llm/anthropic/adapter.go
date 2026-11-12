// Package anthropic is an LLM adapter for Claude through the official
// Anthropic Go SDK.
package anthropic

import (
	"context"
	"fmt"
	"strings"

	sdk "github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"

	"github.com/maxjb-xyz/reverb/internal/llm"
	"github.com/maxjb-xyz/reverb/internal/registry"
)

const (
	// DefaultModel is used when the admin names none.
	DefaultModel = "claude-opus-5-5"
	// minMaxTokens leaves room for the model's thinking before the line itself.
	minMaxTokens = 4096
)

// Adapter is the Claude LLM.
type Adapter struct {
	baseURL string // empty = the SDK default (tests point it at a fake)
	apiKey  string
	model   string
	client  sdk.Client
	ready   bool
}

// New returns an unconfigured adapter (the registry factory).
func New() *Adapter { return &Adapter{} }

func (a *Adapter) Type() string { return llm.Type }
func (a *Adapter) Name() string { return "anthropic" }

func (a *Adapter) ConfigSchema() registry.ConfigSchema {
	return registry.ConfigSchema{Fields: []registry.ConfigField{
		{Key: "api_key", Label: "API key", Type: "string", Required: true, Secret: true,
			Help: "From platform.claude.com under API keys. It can also be set with REVERB_ANTHROPIC_API_KEY."},
		{Key: "model", Label: "Model", Type: "string",
			Help: "Defaults to " + DefaultModel + ". claude-haiku-5-5 costs far less per line."},
	}}
}

func (a *Adapter) Init(cfg map[string]any) error {
	a.apiKey, _ = cfg["api_key"].(string)
	a.apiKey = strings.TrimSpace(a.apiKey)
	if a.apiKey == "" {
		return fmt.Errorf("anthropic: api_key is required")
	}
	a.model, _ = cfg["model"].(string)
	a.model = strings.TrimSpace(a.model)
	if a.model == "" {
		a.model = DefaultModel
	}
	opts := []option.RequestOption{option.WithAPIKey(a.apiKey), option.WithMaxRetries(1)}
	if a.baseURL != "" {
		opts = append(opts, option.WithBaseURL(a.baseURL))
	}
	a.client = sdk.NewClient(opts...)
	a.ready = true
	return nil
}

// TestConnection asks for a one-word answer.
func (a *Adapter) TestConnection(ctx context.Context) error {
	_, err := a.Complete(ctx, llm.Request{Prompt: "Reply with the word OK."})
	return err
}

func (a *Adapter) Complete(ctx context.Context, req llm.Request) (string, error) {
	if err := llm.CheckRequest(req); err != nil {
		return "", err
	}
	if !a.ready {
		return "", fmt.Errorf("anthropic: not initialized")
	}
	maxTokens := req.MaxTokens
	if maxTokens < minMaxTokens {
		maxTokens = minMaxTokens
	}
	params := sdk.BetaMessageNewParams{
		Model:     a.model,
		MaxTokens: int64(maxTokens),
		Messages:  []sdk.BetaMessageParam{sdk.NewBetaUserMessage(sdk.NewBetaTextBlock(req.Prompt))},
		// A DJ line is short and simple; low effort keeps it quick and cheap.
		OutputConfig: sdk.BetaOutputConfigParam{Effort: sdk.BetaOutputConfigEffortLow},
		// A refused request is re-served by a fallback model the API picks.
		Fallbacks: sdk.BetaFallbacksParamOfDefault(),
		Betas:     []sdk.AnthropicBeta{sdk.AnthropicBetaServerSideFallback2026_07_01},
	}
	if strings.TrimSpace(req.System) != "" {
		params.System = []sdk.BetaTextBlockParam{{Text: req.System}}
	}
	msg, err := a.client.Beta.Messages.New(ctx, params)
	if err != nil {
		return "", fmt.Errorf("anthropic: %w", err)
	}
	if msg.StopReason == sdk.BetaStopReasonRefusal {
		return "", llm.ErrNoText
	}
	var b strings.Builder
	for _, block := range msg.Content {
		if t, ok := block.AsAny().(sdk.BetaTextBlock); ok {
			b.WriteString(t.Text)
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "", llm.ErrNoText
	}
	return out, nil
}
