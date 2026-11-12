package wiring

import (
	"encoding/json"
	"log"

	"github.com/maxjb-xyz/reverb/internal/analysis"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// BuildAnalyzer returns the audio analyzer: the first ENABLED "analyzer"
// instance (in priority order) that initialises. Nil when none is configured,
// in which case recommendations don't use audio analysis.
func BuildAnalyzer(reg *registry.Registry, instances []db.AdapterInstance) analysis.Analyzer {
	if reg == nil {
		return nil
	}
	for i := range instances {
		inst := instances[i]
		if inst.Type != analysis.Type || inst.Enabled != 1 {
			continue
		}
		plugin, err := reg.Create(inst.Name)
		if err != nil {
			log.Printf("WARNING: analyzer %q create failed: %v — skipping", inst.Name, err)
			continue
		}
		a, ok := plugin.(analysis.Analyzer)
		if !ok {
			log.Printf("WARNING: adapter %q is not an Analyzer — skipping", inst.Name)
			continue
		}
		cfg := map[string]any{}
		if inst.ConfigJson != "" {
			if err := json.Unmarshal([]byte(inst.ConfigJson), &cfg); err != nil {
				log.Printf("WARNING: analyzer %q config parse failed: %v — skipping", inst.Name, err)
				continue
			}
		}
		if err := a.Init(cfg); err != nil {
			log.Printf("WARNING: analyzer %q init failed: %v — skipping", inst.Name, err)
			continue
		}
		return a
	}
	return nil
}
