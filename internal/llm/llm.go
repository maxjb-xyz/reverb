// Package llm is the language-model seam. The DJ uses it to write its lines
// from facts Reverb already has; it never picks songs. Adapters register with
// a registry of Type "llm" like the other seams.
package llm

import (
	"context"
	"errors"
	"strings"

	"github.com/maxjb-xyz/reverb/internal/registry"
)

// Type is the registry type for language-model adapters.
const Type = "llm"

// MaxPromptLen caps the system + user prompt, in runes.
const MaxPromptLen = 8000

// ErrEmptyPrompt is returned when the user prompt is blank.
var ErrEmptyPrompt = errors.New("llm: prompt is empty")

// ErrPromptTooLong is returned when the prompt is over MaxPromptLen.
var ErrPromptTooLong = errors.New("llm: prompt is too long")

// ErrNoText is returned when the model answered with no text (for example a
// refusal or an empty completion).
var ErrNoText = errors.New("llm: the model returned no text")

// Request is one short completion.
type Request struct {
	System string
	Prompt string
	// MaxTokens caps the answer. 0 lets the adapter choose.
	MaxTokens int
}

// LLM writes text.
type LLM interface {
	registry.Plugin
	// Complete returns the model's text answer to req.
	Complete(ctx context.Context, req Request) (string, error)
}

// CheckRequest validates a request before it costs a call.
func CheckRequest(req Request) error {
	if strings.TrimSpace(req.Prompt) == "" {
		return ErrEmptyPrompt
	}
	if len([]rune(req.System))+len([]rune(req.Prompt)) > MaxPromptLen {
		return ErrPromptTooLong
	}
	return nil
}
