package wiring

import (
	"context"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/llm"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

type stubLLM struct {
	name string
	got  map[string]any
}

func (s *stubLLM) Type() string                        { return llm.Type }
func (s *stubLLM) Name() string                        { return s.name }
func (s *stubLLM) ConfigSchema() registry.ConfigSchema { return registry.ConfigSchema{} }
func (s *stubLLM) Init(cfg map[string]any) error       { s.got = cfg; return nil }
func (s *stubLLM) TestConnection(context.Context) error {
	return nil
}
func (s *stubLLM) Complete(context.Context, llm.Request) (string, error) { return "", nil }

func TestBuildLLMPicksFirstEnabledAndAppliesEnvKey(t *testing.T) {
	made := map[string]*stubLLM{}
	reg := registry.NewRegistry(llm.Type)
	for _, n := range []string{"anthropic", "openai-chat"} {
		reg.Register(n, func() registry.Plugin { v := &stubLLM{name: n}; made[n] = v; return v })
	}
	insts := []db.AdapterInstance{
		{ID: "0", Type: llm.Type, Name: "openai-chat", Enabled: 0},
		{ID: "1", Type: llm.Type, Name: "anthropic", Enabled: 1, ConfigJson: `{"model":"claude-haiku-5-5"}`},
	}
	env := map[string]string{"REVERB_ANTHROPIC_API_KEY": "sk-env"}
	m := BuildLLM(reg, insts, func(k string) string { return env[k] })
	if m == nil || m.Name() != "anthropic" {
		t.Fatalf("got %v", m)
	}
	if made["anthropic"].got["api_key"] != "sk-env" || made["anthropic"].got["model"] != "claude-haiku-5-5" {
		t.Fatalf("config = %v", made["anthropic"].got)
	}
	if BuildLLM(nil, insts, nil) != nil {
		t.Fatal("nil registry must give no model")
	}
}
