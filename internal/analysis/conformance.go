package analysis

import (
	"bytes"
	"context"
	"testing"
)

// RunConformance exercises the Analyzer contract. Call it from each adapter's
// tests with an adapter pointed at a fake service that answers any song.
func RunConformance(t *testing.T, a Analyzer) {
	t.Helper()
	t.Run("Plugin/identity", func(t *testing.T) {
		if a.Type() != Type {
			t.Errorf("Type() = %q, want %q", a.Type(), Type)
		}
		if a.Name() == "" {
			t.Error("Name() must not be empty")
		}
		if len(a.ConfigSchema().Fields) == 0 {
			t.Error("ConfigSchema() must describe at least one field")
		}
	})
	t.Run("Analyze/returns-valid-features", func(t *testing.T) {
		f, err := a.Analyze(context.Background(), "song.mp3", bytes.NewReader([]byte("ID3 fake audio")))
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		if err := f.Validate(); err != nil {
			t.Fatalf("features: %v", err)
		}
	})
	t.Run("Analyze/honours-cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := a.Analyze(ctx, "song.mp3", bytes.NewReader([]byte("x"))); err == nil {
			t.Fatal("Analyze with a cancelled context should fail")
		}
	})
}
