package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/llm"
)

type seen struct {
	path, key, beta string
	body            map[string]any
}

func message(stop string, blocks ...map[string]any) map[string]any {
	content := make([]any, 0, len(blocks))
	for _, b := range blocks {
		content = append(content, b)
	}
	return map[string]any{
		"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-opus-5-5",
		"content": content, "stop_reason": stop,
		"usage": map[string]any{"input_tokens": 10, "output_tokens": 5},
	}
}

func fakeServer(t *testing.T, s *seen, reply map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.path = r.URL.Path
		// The SDK may send the key either way.
		s.key = r.Header.Get("X-Api-Key")
		if s.key == "" {
			s.key = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		}
		s.beta = r.Header.Get("Anthropic-Beta")
		_ = json.NewDecoder(r.Body).Decode(&s.body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(reply)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func textReply(text string) map[string]any {
	return message("end_turn",
		map[string]any{"type": "thinking", "thinking": "", "signature": "sig"},
		map[string]any{"type": "text", "text": text})
}

func configured(t *testing.T, srv *httptest.Server, cfg map[string]any) *Adapter {
	t.Helper()
	a := New()
	a.baseURL = srv.URL
	if cfg == nil {
		cfg = map[string]any{"api_key": "sk-ant-test"}
	}
	if err := a.Init(cfg); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestConformance(t *testing.T) {
	llm.RunConformance(t, configured(t, fakeServer(t, &seen{}, textReply("Hey there.")), nil))
}

func TestSendsMessagesRequest(t *testing.T) {
	var s seen
	a := configured(t, fakeServer(t, &s, textReply(" That was Bonobo. ")), nil)
	out, err := a.Complete(context.Background(), llm.Request{System: "You are a DJ.", Prompt: "Intro the next song."})
	if err != nil {
		t.Fatal(err)
	}
	if out != "That was Bonobo." {
		t.Fatalf("out = %q", out)
	}
	if s.path != "/v1/messages" || s.key != "sk-ant-test" {
		t.Fatalf("path=%q key=%q", s.path, s.key)
	}
	if s.body["model"] != DefaultModel {
		t.Fatalf("model = %v", s.body["model"])
	}
	oc, _ := s.body["output_config"].(map[string]any)
	if oc["effort"] != "low" {
		t.Fatalf("output_config = %v, want effort low", s.body["output_config"])
	}
	// Haiku has no server-side fallback, so none is asked for.
	if _, ok := s.body["fallbacks"]; ok || strings.Contains(s.beta, "server-side-fallback") {
		t.Fatalf("fallbacks=%v beta=%q, want none for %s", s.body["fallbacks"], s.beta, DefaultModel)
	}
	if !strings.Contains(string(mustJSON(s.body["system"])), "You are a DJ.") {
		t.Fatalf("system = %v", s.body["system"])
	}
}

func TestDefaultsToTheLowCostModel(t *testing.T) {
	if DefaultModel != "claude-haiku-5-5" {
		t.Fatalf("DefaultModel = %q", DefaultModel)
	}
}

func TestOptInModelGetsRefusalFallbacks(t *testing.T) {
	var s seen
	a := configured(t, fakeServer(t, &s, textReply("ok")), map[string]any{"api_key": "k", "model": "claude-opus-5-5"})
	if _, err := a.Complete(context.Background(), llm.Request{Prompt: "hi"}); err != nil {
		t.Fatal(err)
	}
	if s.body["model"] != "claude-opus-5-5" {
		t.Fatalf("model = %v", s.body["model"])
	}
	// Refusals fall back server-side to a model chosen by the API.
	if s.body["fallbacks"] != "default" || !strings.Contains(s.beta, "server-side-fallback-2026-07-01") {
		t.Fatalf("fallbacks=%v beta=%q", s.body["fallbacks"], s.beta)
	}
}

func TestRefusalIsNoText(t *testing.T) {
	a := configured(t, fakeServer(t, &seen{}, message("refusal")), nil)
	if _, err := a.Complete(context.Background(), llm.Request{Prompt: "hi"}); !errors.Is(err, llm.ErrNoText) {
		t.Fatalf("err = %v, want ErrNoText", err)
	}
}

func TestInitRequiresKey(t *testing.T) {
	if err := New().Init(map[string]any{}); err == nil {
		t.Fatal("want error without api_key")
	}
}

func TestKeyIsSecretInSchema(t *testing.T) {
	for _, f := range New().ConfigSchema().Fields {
		if f.Key == "api_key" && !f.Secret {
			t.Fatal("api_key must be marked secret")
		}
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
