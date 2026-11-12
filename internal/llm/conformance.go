package llm

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// RunConformance exercises the LLM contract. Call it from each adapter's test
// package with a configured adapter pointed at a fake server that answers any
// completion with some text.
func RunConformance(t *testing.T, m LLM) {
	t.Helper()

	t.Run("Plugin/identity", func(t *testing.T) {
		if m.Type() != Type {
			t.Errorf("Type() = %q, want %q", m.Type(), Type)
		}
		if m.Name() == "" {
			t.Error("Name() must not be empty")
		}
		if len(m.ConfigSchema().Fields) == 0 {
			t.Error("ConfigSchema() must describe at least one field")
		}
	})

	t.Run("Complete/returns-text", func(t *testing.T) {
		out, err := m.Complete(context.Background(), Request{System: "You are a radio DJ.", Prompt: "Say hi."})
		if err != nil {
			t.Fatalf("Complete: %v", err)
		}
		if strings.TrimSpace(out) == "" {
			t.Fatal("expected text")
		}
	})

	t.Run("Complete/rejects-bad-prompts", func(t *testing.T) {
		if _, err := m.Complete(context.Background(), Request{Prompt: "  "}); !errors.Is(err, ErrEmptyPrompt) {
			t.Fatalf("blank prompt: err = %v, want ErrEmptyPrompt", err)
		}
		long := strings.Repeat("a", MaxPromptLen+1)
		if _, err := m.Complete(context.Background(), Request{Prompt: long}); !errors.Is(err, ErrPromptTooLong) {
			t.Fatalf("long prompt: err = %v, want ErrPromptTooLong", err)
		}
	})

	t.Run("Complete/honours-cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := m.Complete(ctx, Request{Prompt: "hello"}); err == nil {
			t.Fatal("expected an error for a cancelled context")
		}
	})
}
