package wiring

import (
	"encoding/json"
	"log"

	"github.com/maxjb-xyz/reverb/internal/llm"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// LLMEnvOverrides applies REVERB_* secrets onto a language-model adapter's
// config (env wins).
func LLMEnvOverrides(name string, cfg map[string]any, getenv func(string) string) {
	if name == "anthropic" {
		if k := getenv("REVERB_ANTHROPIC_API_KEY"); k != "" {
			cfg["api_key"] = k
		}
	}
}

// BuildLLM returns the DJ's writer model: the first ENABLED "llm" instance (in
// priority order) that initialises. Nil when none is configured, in which case
// the DJ writes its lines from templates.
func BuildLLM(reg *registry.Registry, instances []db.AdapterInstance, getenv func(string) string) llm.LLM {
	if reg == nil {
		return nil
	}
	for i := range instances {
		inst := instances[i]
		if inst.Type != llm.Type || inst.Enabled != 1 {
			continue
		}
		plugin, err := reg.Create(inst.Name)
		if err != nil {
			log.Printf("WARNING: llm %q create failed: %v — skipping", inst.Name, err)
			continue
		}
		m, ok := plugin.(llm.LLM)
		if !ok {
			log.Printf("WARNING: adapter %q is not an LLM — skipping", inst.Name)
			continue
		}
		cfg := map[string]any{}
		if inst.ConfigJson != "" {
			if err := json.Unmarshal([]byte(inst.ConfigJson), &cfg); err != nil {
				log.Printf("WARNING: llm %q config parse failed: %v — skipping", inst.Name, err)
				continue
			}
		}
		if getenv != nil {
			LLMEnvOverrides(inst.Name, cfg, getenv)
		}
		if err := m.Init(cfg); err != nil {
			log.Printf("WARNING: llm %q init failed: %v — skipping", inst.Name, err)
			continue
		}
		return m
	}
	return nil
}
