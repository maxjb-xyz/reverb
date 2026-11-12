package openaispeech

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/voice"
)

type seen struct {
	path, auth string
	body       map[string]any
}

func fakeServer(t *testing.T, s *seen) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.path = r.URL.Path
		s.auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&s.body)
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("ID3audio"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func configured(t *testing.T, srv *httptest.Server, extra map[string]any) *Adapter {
	t.Helper()
	cfg := map[string]any{"base_url": srv.URL + "/v1"}
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
	voice.RunConformance(t, configured(t, fakeServer(t, &seen{}), nil))
}

func TestSendsOpenAISpeechRequestWithDefaults(t *testing.T) {
	var s seen
	a := configured(t, fakeServer(t, &s), nil)
	if _, err := a.Synthesize(context.Background(), "Hello there"); err != nil {
		t.Fatal(err)
	}
	if s.path != "/v1/audio/speech" {
		t.Fatalf("path = %q", s.path)
	}
	if s.body["input"] != "Hello there" || s.body["model"] != "kokoro" || s.body["voice"] != "af_heart" || s.body["response_format"] != "mp3" {
		t.Fatalf("body = %v", s.body)
	}
	if s.auth != "" {
		t.Fatalf("no key configured, but Authorization = %q", s.auth)
	}
}

func TestUsesConfiguredModelVoiceAndKey(t *testing.T) {
	var s seen
	a := configured(t, fakeServer(t, &s), map[string]any{"model": "tts-1", "voice": "alloy", "api_key": "sk-test"})
	if _, err := a.Synthesize(context.Background(), "Hi"); err != nil {
		t.Fatal(err)
	}
	if s.body["model"] != "tts-1" || s.body["voice"] != "alloy" {
		t.Fatalf("body = %v", s.body)
	}
	if s.auth != "Bearer sk-test" {
		t.Fatalf("Authorization = %q", s.auth)
	}
}

func TestTrimsTrailingSlashOnBaseURL(t *testing.T) {
	var s seen
	srv := fakeServer(t, &s)
	a := New()
	if err := a.Init(map[string]any{"base_url": srv.URL + "/v1/"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Synthesize(context.Background(), "Hi"); err != nil {
		t.Fatal(err)
	}
	if s.path != "/v1/audio/speech" {
		t.Fatalf("path = %q", s.path)
	}
}

func TestInitRequiresBaseURL(t *testing.T) {
	if err := New().Init(map[string]any{}); err == nil {
		t.Fatal("want error without base_url")
	}
}

func TestServerErrorIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"bad voice"}`, http.StatusBadRequest)
	}))
	defer srv.Close()
	a := New()
	_ = a.Init(map[string]any{"base_url": srv.URL})
	if _, err := a.Synthesize(context.Background(), "Hi"); err == nil {
		t.Fatal("want error on HTTP 400")
	}
}
