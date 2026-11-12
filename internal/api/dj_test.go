package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store"
	"github.com/maxjb-xyz/reverb/internal/voice"
)

// fakeVoiceSvc is a controllable VoiceService.
type fakeVoiceSvc struct {
	name  string
	err   error
	said  []string
	clips map[string]string // id -> file path
}

func (f *fakeVoiceSvc) Active() string { return f.name }
func (f *fakeVoiceSvc) Say(_ context.Context, text string) (string, error) {
	f.said = append(f.said, text)
	if f.err != nil {
		return "", f.err
	}
	return "0123456789abcdef0123456789abcdef", nil
}
func (f *fakeVoiceSvc) Open(id string) (string, string, bool) {
	p, ok := f.clips[id]
	return p, "audio/mpeg", ok
}

func djTestServer(t *testing.T, vs VoiceService) (*Server, *http.Cookie) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/dj.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	authSvc, tok := seededAuthToken(t, st)
	srv := NewServer(Deps{
		Auth:       authSvc,
		Search:     registry.NewRegistry("search"),
		Downloader: registry.NewRegistry("downloader"),
		Voice:      vs,
	})
	return srv, &http.Cookie{Name: sessionCookie, Value: tok}
}

func TestDJVoiceStatus(t *testing.T) {
	srv, c := djTestServer(t, &fakeVoiceSvc{name: "elevenlabs"})
	rec := doGET(t, srv, "/api/v1/dj/voice", c.Value)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"available":true`) || !strings.Contains(rec.Body.String(), `"name":"elevenlabs"`) {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}

	srv, c = djTestServer(t, nil)
	rec = doGET(t, srv, "/api/v1/dj/voice", c.Value)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"available":false`) {
		t.Fatalf("no voice: got %d %s", rec.Code, rec.Body.String())
	}
}

func TestDJSayReturnsClipURL(t *testing.T) {
	fv := &fakeVoiceSvc{name: "fake"}
	srv, c := djTestServer(t, fv)
	rec := do(t, srv, c, http.MethodPost, "/api/v1/dj/say", `{"text":"Here's one you had on repeat."}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
	var out struct{ ID, URL string }
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.ID != "0123456789abcdef0123456789abcdef" || out.URL != "/api/v1/dj/clips/"+out.ID {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if len(fv.said) != 1 || fv.said[0] != "Here's one you had on repeat." {
		t.Fatalf("said = %v", fv.said)
	}
}

func TestDJSayErrors(t *testing.T) {
	cases := []struct {
		name string
		svc  VoiceService
		body string
		want int
	}{
		{"no voice service", nil, `{"text":"hi"}`, http.StatusServiceUnavailable},
		{"no voice configured", &fakeVoiceSvc{err: voice.ErrNoVoice}, `{"text":"hi"}`, http.StatusServiceUnavailable},
		{"still setting up", &fakeVoiceSvc{name: "kokoro", err: fmt.Errorf("%w: Downloading", voice.ErrWarmingUp)}, `{"text":"hi"}`, http.StatusServiceUnavailable},
		{"empty", &fakeVoiceSvc{name: "f", err: voice.ErrEmptyText}, `{"text":" "}`, http.StatusBadRequest},
		{"too long", &fakeVoiceSvc{name: "f", err: voice.ErrTextTooLong}, `{"text":"x"}`, http.StatusBadRequest},
		{"bad json", &fakeVoiceSvc{name: "f"}, `nope`, http.StatusBadRequest},
		{"provider down", &fakeVoiceSvc{name: "f", err: errors.New("HTTP 401: key sk-secret refused")}, `{"text":"hi"}`, http.StatusBadGateway},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, c := djTestServer(t, tc.svc)
			rec := do(t, srv, c, http.MethodPost, "/api/v1/dj/say", tc.body)
			if rec.Code != tc.want {
				t.Fatalf("got %d %s, want %d", rec.Code, rec.Body.String(), tc.want)
			}
			if strings.Contains(rec.Body.String(), "sk-secret") {
				t.Fatal("provider error text must not reach the browser")
			}
		})
	}
}

func TestDJSayIsRateLimited(t *testing.T) {
	srv, c := djTestServer(t, &fakeVoiceSvc{name: "f"})
	var last int
	for i := 0; i < djSayPerMinute+1; i++ {
		last = do(t, srv, c, http.MethodPost, "/api/v1/dj/say", `{"text":"hi"}`).Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("request %d got %d, want 429", djSayPerMinute+1, last)
	}
}

func TestDJClipServesAudio(t *testing.T) {
	p := filepath.Join(t.TempDir(), "clip.mp3")
	if err := os.WriteFile(p, []byte("ID3clip"), 0o644); err != nil {
		t.Fatal(err)
	}
	id := "0123456789abcdef0123456789abcdef"
	srv, c := djTestServer(t, &fakeVoiceSvc{clips: map[string]string{id: p}})
	rec := doGET(t, srv, "/api/v1/dj/clips/"+id, c.Value)
	if rec.Code != http.StatusOK || rec.Body.String() != "ID3clip" {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Fatalf("content-type = %q", ct)
	}
	if rec = doGET(t, srv, "/api/v1/dj/clips/ffffffffffffffffffffffffffffffff", c.Value); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown clip: %d", rec.Code)
	}
	if rec = doGET(t, srv, "/api/v1/dj/clips/"+id, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("clips need a session: %d", rec.Code)
	}
}

func TestAdaptersAvailableListsVoiceProviders(t *testing.T) {
	srv, c := adapterTestServer(t, adapterServerOpts{})
	reg := registry.NewRegistry(voice.Type)
	reg.Register("elevenlabs", func() registry.Plugin { return &fakeAdapter{typ: voice.Type, name: "elevenlabs"} })
	srv.deps.VoiceReg = reg
	rec := do(t, srv, c, http.MethodGet, "/api/v1/adapters/available", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"type":"voice","name":"elevenlabs"`) {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
}
