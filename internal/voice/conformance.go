package voice

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// RunConformance exercises the Voice contract. Call it from each adapter's test
// package with a configured adapter pointed at a fake server that answers any
// speech request with audio.
func RunConformance(t *testing.T, v Voice) {
	t.Helper()

	t.Run("Plugin/identity", func(t *testing.T) {
		if v.Type() != Type {
			t.Errorf("Type() = %q, want %q", v.Type(), Type)
		}
		if v.Name() == "" {
			t.Error("Name() must not be empty")
		}
		if len(v.ConfigSchema().Fields) == 0 {
			t.Error("ConfigSchema() must describe at least one field")
		}
	})

	t.Run("Synthesize/returns-audio", func(t *testing.T) {
		a, err := v.Synthesize(context.Background(), "Here's one you had on repeat.")
		if err != nil {
			t.Fatalf("Synthesize: %v", err)
		}
		if len(a.Data) == 0 {
			t.Fatal("expected audio bytes")
		}
		if !strings.HasPrefix(a.ContentType, "audio/") {
			t.Fatalf("ContentType = %q, want audio/*", a.ContentType)
		}
	})

	t.Run("Synthesize/rejects-bad-text", func(t *testing.T) {
		if _, err := v.Synthesize(context.Background(), "  "); !errors.Is(err, ErrEmptyText) {
			t.Fatalf("blank text: err = %v, want ErrEmptyText", err)
		}
		long := strings.Repeat("a", MaxTextLen+1)
		if _, err := v.Synthesize(context.Background(), long); !errors.Is(err, ErrTextTooLong) {
			t.Fatalf("long text: err = %v, want ErrTextTooLong", err)
		}
	})

	t.Run("Synthesize/honours-cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := v.Synthesize(ctx, "hello"); err == nil {
			t.Fatal("expected an error for a cancelled context")
		}
	})
}
