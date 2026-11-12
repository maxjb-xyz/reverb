package main

import (
	"context"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/llm"
	"github.com/maxjb-xyz/reverb/internal/llm/openaichat"
	"github.com/maxjb-xyz/reverb/internal/voice"
	"github.com/maxjb-xyz/reverb/internal/voice/openaispeech"
	"github.com/maxjb-xyz/reverb/internal/wiring"
)

// A reload swaps the DJ voice and writer model to whatever the rebuilt bundle carries, so
// turning a voice provider on or off in the admin UI needs no restart.
func TestReloadSwapsDJVoice(t *testing.T) {
	v := openaispeech.New()
	if err := v.Init(map[string]any{"base_url": "http://kokoro:8880/v1"}); err != nil {
		t.Fatal(err)
	}
	m := openaichat.New()
	if err := m.Init(map[string]any{"base_url": "http://ollama:11434/v1", "model": "llama3.2"}); err != nil {
		t.Fatal(err)
	}
	bundles := []wiring.ServiceBundle{{Voice: v, LLM: m}, {}}
	var idx int
	rl := newServiceReloaderFunc(func(context.Context) (wiring.ServiceBundle, error) {
		b := bundles[idx]
		idx++
		return b, nil
	})
	rl.voice = voice.NewService(voice.NewCache(t.TempDir(), 10))
	rl.llm = llm.NewService()

	if _, _, _, _, _, err := rl.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := rl.voice.Active(); got != "openai-speech" {
		t.Fatalf("after enabling: Active() = %q", got)
	}
	if got := rl.llm.Active(); got != "openai-chat" {
		t.Fatalf("after enabling: llm Active() = %q", got)
	}
	if _, _, _, _, _, err := rl.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := rl.voice.Active(); got != "" {
		t.Fatalf("after disabling: Active() = %q", got)
	}
	if got := rl.llm.Active(); got != "" {
		t.Fatalf("after disabling: llm Active() = %q", got)
	}
}
