package wiring

import (
	"testing"

	"github.com/maxjb-xyz/reverb/internal/analysis"
	"github.com/maxjb-xyz/reverb/internal/analysis/sidecar"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

func TestBuildAnalyzer_FirstEnabledThatInitialises(t *testing.T) {
	reg := registry.NewRegistry(analysis.Type)
	reg.Register(sidecar.Name, func() registry.Plugin { return sidecar.New() })
	if BuildAnalyzer(reg, nil) != nil {
		t.Fatal("no instances should mean no analyzer")
	}
	got := BuildAnalyzer(reg, []db.AdapterInstance{
		{Type: analysis.Type, Name: sidecar.Name, Enabled: 0, ConfigJson: `{"url":"http://off"}`},
		{Type: analysis.Type, Name: sidecar.Name, Enabled: 1, ConfigJson: `{}`},
		{Type: analysis.Type, Name: sidecar.Name, Enabled: 1, ConfigJson: `{"url":"http://gpu-box:8765"}`},
	})
	if got == nil || got.Name() != sidecar.Name {
		t.Fatalf("analyzer = %v", got)
	}
	if BuildAnalyzer(nil, nil) != nil {
		t.Fatal("nil registry")
	}
}
