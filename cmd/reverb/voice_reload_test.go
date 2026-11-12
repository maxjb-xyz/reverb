package main

import (
	"context"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/voice"
	"github.com/maxjb-xyz/reverb/internal/voice/openaispeech"
	"github.com/maxjb-xyz/reverb/internal/wiring"
)

// A reload swaps the DJ voice to whatever the rebuilt bundle carries, so
// turning a voice provider on or off in the admin UI needs no restart.
func TestReloadSwapsDJVoice(t *testing.T) {
	v := openaispeech.New()
	if err := v.Init(map[string]any{"base_url": "http://kokoro:8880/v1"}); err != nil {
		t.Fatal(err)
	}
	bundles := []wiring.ServiceBundle{{Voice: v}, {}}
	var idx int
	rl := newServiceReloaderFunc(func(context.Context) (wiring.ServiceBundle, error) {
		b := bundles[idx]
		idx++
		return b, nil
	})
	rl.voice = voice.NewService(voice.NewCache(t.TempDir(), 10))

	if _, _, _, _, _, err := rl.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := rl.voice.Active(); got != "openai-speech" {
		t.Fatalf("after enabling: Active() = %q", got)
	}
	if _, _, _, _, _, err := rl.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := rl.voice.Active(); got != "" {
		t.Fatalf("after disabling: Active() = %q", got)
	}
}
