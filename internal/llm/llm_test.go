package llm

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/registry"
)

type fakeLLM struct{ out string }

func (f *fakeLLM) Type() string                        { return Type }
func (f *fakeLLM) Name() string                        { return "fake" }
func (f *fakeLLM) ConfigSchema() registry.ConfigSchema { return registry.ConfigSchema{} }
func (f *fakeLLM) Init(map[string]any) error           { return nil }
func (f *fakeLLM) TestConnection(context.Context) error {
	return nil
}
func (f *fakeLLM) Complete(_ context.Context, req Request) (string, error) {
	return f.out + req.Prompt, nil
}

func TestCheckRequest(t *testing.T) {
	if err := CheckRequest(Request{Prompt: "hi"}); err != nil {
		t.Fatal(err)
	}
	if err := CheckRequest(Request{Prompt: " \n"}); !errors.Is(err, ErrEmptyPrompt) {
		t.Fatalf("blank: %v", err)
	}
	if err := CheckRequest(Request{System: strings.Repeat("s", MaxPromptLen), Prompt: "x"}); !errors.Is(err, ErrPromptTooLong) {
		t.Fatalf("long: %v", err)
	}
}

func TestServiceSwapsModels(t *testing.T) {
	s := NewService()
	if _, err := s.Complete(context.Background(), Request{Prompt: "x"}); !errors.Is(err, ErrNoLLM) {
		t.Fatalf("err = %v, want ErrNoLLM", err)
	}
	s.Set(&fakeLLM{out: "a:"})
	if s.Active() != "fake" {
		t.Fatalf("Active() = %q", s.Active())
	}
	out, err := s.Complete(context.Background(), Request{Prompt: "x"})
	if err != nil || out != "a:x" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	s.Set(nil)
	if s.Active() != "" {
		t.Fatal("Set(nil) should turn the model off")
	}
}
