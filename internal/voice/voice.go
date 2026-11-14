// Package voice defines the text-to-speech seam the AI DJ speaks through, a
// conformance suite, and a clip cache. Adapters live in subpackages
// (voice/openaispeech for any OpenAI-compatible speech endpoint such as a
// Kokoro server or OpenAI itself, voice/elevenlabs for ElevenLabs).
package voice

import (
	"context"
	"errors"

	"github.com/maxjb-xyz/reverb/internal/registry"
)

// Type is the adapter_instance type for voice adapters.
const Type = "voice"

// MaxTextLen caps one line of speech. DJ lines are a sentence or two; anything
// longer is a bug upstream and would burn paid characters.
const MaxTextLen = 600

// ErrEmptyText is returned for a blank line.
var ErrEmptyText = errors.New("voice: text is empty")

// ErrTextTooLong is returned for a line over MaxTextLen.
var ErrTextTooLong = errors.New("voice: text is too long")

// ErrWarmingUp means the voice exists but is not ready yet, for example while
// a built-in voice downloads its model. Try again later.
var ErrWarmingUp = errors.New("voice: still being set up")

// Audio is one rendered line.
type Audio struct {
	Data        []byte
	ContentType string // e.g. audio/mpeg
}

// Voice renders text to speech.
type Voice interface {
	registry.Plugin

	// Synthesize renders one line. It must honour ctx cancellation and return
	// ErrEmptyText / ErrTextTooLong for bad input without calling out.
	Synthesize(ctx context.Context, text string) (Audio, error)
}

// CheckText validates a line before an adapter spends a request on it.
func CheckText(text string) error {
	trimmed := 0
	for _, r := range text {
		if r != ' ' && r != '\n' && r != '\t' && r != '\r' {
			trimmed++
		}
	}
	if trimmed == 0 {
		return ErrEmptyText
	}
	if len([]rune(text)) > MaxTextLen {
		return ErrTextTooLong
	}
	return nil
}
