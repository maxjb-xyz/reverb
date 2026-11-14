package openaichat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/llm"
)

type seen struct {
	path, auth string
	body       map[string]any
}

func fakeServer(t *testing.T, s *seen, reply string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.path = r.URL.Path
		s.auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&s.body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": reply}}},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func configured(t *testing.T, srv *httptest.Server, extra map[string]any) *Adapter {
	t.Helper()
	cfg := map[string]any{"base_url": srv.URL + "/v1/", "model": "llama3.2"}
	for k, v := range extra {
		cfg[k] = v
	}
	a := New()
	if err := a.Init(cfg); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestConformance(t *testing.T) {
	llm.RunConformance(t, configured(t, fakeServer(t, &seen{}, "Hey there."), nil))
}

func TestSendsChatCompletion(t *testing.T) {
	var s seen
	a := configured(t, fakeServer(t, &s, "  That was Bonobo.  "), nil)
	out, err := a.Complete(context.Background(), llm.Request{System: "sys", Prompt: "user", MaxTokens: 200})
	if err != nil {
		t.Fatal(err)
	}
	if out != "That was Bonobo." {
		t.Fatalf("out = %q", out)
	}
	if s.path != "/v1/chat/completions" || s.auth != "" {
		t.Fatalf("path=%q auth=%q", s.path, s.auth)
	}
	msgs, _ := s.body["messages"].([]any)
	if s.body["model"] != "llama3.2" || len(msgs) != 2 || s.body["max_tokens"] != float64(200) {
		t.Fatalf("body = %v", s.body)
	}
	first, _ := msgs[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "sys" {
		t.Fatalf("system message = %v", first)
	}
}

func TestSendsKeyWhenSet(t *testing.T) {
	var s seen
	a := configured(t, fakeServer(t, &s, "ok"), map[string]any{"api_key": "sk-x"})
	if _, err := a.Complete(context.Background(), llm.Request{Prompt: "hi"}); err != nil {
		t.Fatal(err)
	}
	if s.auth != "Bearer sk-x" {
		t.Fatalf("auth = %q", s.auth)
	}
}

func TestEmptyAnswerIsAnError(t *testing.T) {
	a := configured(t, fakeServer(t, &seen{}, "   "), nil)
	if _, err := a.Complete(context.Background(), llm.Request{Prompt: "hi"}); err == nil {
		t.Fatal("want error for an empty answer")
	}
}

func TestInitNeedsURLAndModel(t *testing.T) {
	if err := New().Init(map[string]any{"model": "m"}); err == nil {
		t.Fatal("want error without base_url")
	}
	if err := New().Init(map[string]any{"base_url": "http://x"}); err == nil {
		t.Fatal("want error without model")
	}
}

func TestServerErrorIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"model not found"}`, http.StatusNotFound)
	}))
	defer srv.Close()
	a := New()
	_ = a.Init(map[string]any{"base_url": srv.URL, "model": "m"})
	if _, err := a.Complete(context.Background(), llm.Request{Prompt: "hi"}); err == nil {
		t.Fatal("want error on HTTP 404")
	}
}
