package wiring

import (
	"encoding/json"
	"log"

	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store/db"
	"github.com/maxjb-xyz/reverb/internal/voice"
)

// VoiceEnvOverrides applies REVERB_* secrets onto a voice adapter's config
// (env wins; the key never has to be stored or sent to the browser).
func VoiceEnvOverrides(name string, cfg map[string]any, getenv func(string) string) {
	if name == "elevenlabs" {
		if k := getenv("REVERB_ELEVENLABS_API_KEY"); k != "" {
			cfg["api_key"] = k
		}
	}
}

// BuildVoice returns the DJ's voice: the first ENABLED "voice" instance (in
// priority order) that initialises. Nil when none is configured, in which case
// the DJ shows its lines as text instead of speaking them.
func BuildVoice(reg *registry.Registry, instances []db.AdapterInstance, getenv func(string) string) voice.Voice {
	if reg == nil {
		return nil
	}
	for i := range instances {
		inst := instances[i]
		if inst.Type != voice.Type || inst.Enabled != 1 {
			continue
		}
		plugin, err := reg.Create(inst.Name)
		if err != nil {
			log.Printf("WARNING: voice %q create failed: %v — skipping", inst.Name, err)
			continue
		}
		v, ok := plugin.(voice.Voice)
		if !ok {
			log.Printf("WARNING: adapter %q is not a Voice — skipping", inst.Name)
			continue
		}
		cfg := map[string]any{}
		if inst.ConfigJson != "" {
			if err := json.Unmarshal([]byte(inst.ConfigJson), &cfg); err != nil {
				log.Printf("WARNING: voice %q config parse failed: %v — skipping", inst.Name, err)
				continue
			}
		}
		VoiceEnvOverrides(inst.Name, cfg, getenv)
		if err := v.Init(cfg); err != nil {
			log.Printf("WARNING: voice %q init failed: %v — skipping", inst.Name, err)
			continue
		}
		return v
	}
	return nil
}
