package elevenlabs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/voice"
)

type seen struct {
	path, query, key string
	body             map[string]any
}

func fakeServer(t *testing.T, s *seen) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.path = r.URL.Path
		s.query = r.URL.RawQuery
		s.key = r.Header.Get("xi-api-key")
		_ = json.NewDecoder(r.Body).Decode(&s.body)
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("ID3audio"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func configured(t *testing.T, srv *httptest.Server, cfg map[string]any) *Adapter {
	t.Helper()
	a := New()
	a.baseURL = srv.URL
	if cfg == nil {
		cfg = map[string]any{"api_key": "xi-test"}
	}
	if err := a.Init(cfg); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestConformance(t *testing.T) {
	voice.RunConformance(t, configured(t, fakeServer(t, &seen{}), nil))
}

func TestSendsTextToSpeechRequestWithDefaults(t *testing.T) {
	var s seen
	a := configured(t, fakeServer(t, &s), nil)
	if _, err := a.Synthesize(context.Background(), "Hello there"); err != nil {
		t.Fatal(err)
	}
	if s.path != "/v1/text-to-speech/"+DefaultVoiceID {
		t.Fatalf("path = %q", s.path)
	}
	if s.query != "output_format=mp3_44100_128" {
		t.Fatalf("query = %q", s.query)
	}
	if s.key != "xi-test" {
		t.Fatalf("xi-api-key = %q", s.key)
	}
	if s.body["text"] != "Hello there" || s.body["model_id"] != DefaultModel {
		t.Fatalf("body = %v", s.body)
	}
}

func TestUsesConfiguredVoiceAndModel(t *testing.T) {
	var s seen
	a := configured(t, fakeServer(t, &s), map[string]any{"api_key": "k", "voice_id": "abc123", "model_id": "eleven_multilingual_v2"})
	if _, err := a.Synthesize(context.Background(), "Hi"); err != nil {
		t.Fatal(err)
	}
	if s.path != "/v1/text-to-speech/abc123" || s.body["model_id"] != "eleven_multilingual_v2" {
		t.Fatalf("path=%q body=%v", s.path, s.body)
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
			t.Fatal("api_key must be marked secret so the API redacts it")
		}
	}
}
