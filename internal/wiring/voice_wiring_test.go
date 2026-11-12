package wiring

import (
	"context"
	"errors"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store/db"
	"github.com/maxjb-xyz/reverb/internal/voice"
)

type stubVoice struct {
	name    string
	got     map[string]any
	initErr error
}

func (s *stubVoice) Type() string                        { return voice.Type }
func (s *stubVoice) Name() string                        { return s.name }
func (s *stubVoice) ConfigSchema() registry.ConfigSchema { return registry.ConfigSchema{} }
func (s *stubVoice) Init(cfg map[string]any) error       { s.got = cfg; return s.initErr }
func (s *stubVoice) TestConnection(context.Context) error {
	return nil
}
func (s *stubVoice) Synthesize(context.Context, string) (voice.Audio, error) {
	return voice.Audio{}, nil
}

func voiceReg(made map[string]*stubVoice) *registry.Registry {
	reg := registry.NewRegistry(voice.Type)
	for _, n := range []string{"elevenlabs", "openai-speech", "broken"} {
		reg.Register(n, func() registry.Plugin {
			v := &stubVoice{name: n}
			if n == "broken" {
				v.initErr = errors.New("bad config")
			}
			made[n] = v
			return v
		})
	}
	return reg
}

func TestBuildVoiceNoneConfigured(t *testing.T) {
	made := map[string]*stubVoice{}
	insts := []db.AdapterInstance{{ID: "1", Type: "search", Name: "deezer", Enabled: 1}}
	if v := BuildVoice(voiceReg(made), insts, func(string) string { return "" }); v != nil {
		t.Fatalf("want nil, got %v", v)
	}
	if v := BuildVoice(nil, insts, func(string) string { return "" }); v != nil {
		t.Fatal("nil registry must give no voice")
	}
}

func TestBuildVoicePicksFirstEnabledThatInits(t *testing.T) {
	made := map[string]*stubVoice{}
	insts := []db.AdapterInstance{
		{ID: "0", Type: voice.Type, Name: "elevenlabs", Enabled: 0, ConfigJson: `{"api_key":"k"}`},
		{ID: "1", Type: voice.Type, Name: "broken", Enabled: 1},
		{ID: "2", Type: voice.Type, Name: "openai-speech", Enabled: 1, ConfigJson: `{"base_url":"http://kokoro:8880/v1"}`},
	}
	v := BuildVoice(voiceReg(made), insts, func(string) string { return "" })
	if v == nil || v.Name() != "openai-speech" {
		t.Fatalf("got %v, want openai-speech", v)
	}
	if made["openai-speech"].got["base_url"] != "http://kokoro:8880/v1" {
		t.Fatalf("config = %v", made["openai-speech"].got)
	}
	if _, ok := made["elevenlabs"]; ok {
		t.Fatal("a disabled instance must not be built")
	}
}

func TestBuildVoiceAppliesElevenLabsEnvKey(t *testing.T) {
	made := map[string]*stubVoice{}
	insts := []db.AdapterInstance{{ID: "1", Type: voice.Type, Name: "elevenlabs", Enabled: 1, ConfigJson: `{"voice_id":"abc"}`}}
	env := map[string]string{"REVERB_ELEVENLABS_API_KEY": "from-env"}
	v := BuildVoice(voiceReg(made), insts, func(k string) string { return env[k] })
	if v == nil {
		t.Fatal("want a voice")
	}
	if made["elevenlabs"].got["api_key"] != "from-env" || made["elevenlabs"].got["voice_id"] != "abc" {
		t.Fatalf("config = %v", made["elevenlabs"].got)
	}
}
