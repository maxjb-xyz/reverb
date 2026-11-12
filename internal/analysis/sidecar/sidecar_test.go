package sidecar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/analysis"
)

func fakeService(t *testing.T, status int, seen *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v1/health":
			_ = json.NewEncoder(w).Encode(map[string]string{"model": "clap-music-v1", "device": "cpu"})
		case "/v1/analyze":
			if status != http.StatusOK {
				w.WriteHeader(status)
				return
			}
			f, hdr, err := r.FormFile("audio")
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			b, _ := io.ReadAll(f)
			if seen != nil {
				*seen = hdr.Filename + "|" + string(b)
			}
			emb := make([]float32, 32)
			emb[3] = 0.5
			_ = json.NewEncoder(w).Encode(analysis.Features{Model: "clap-music-v1", Embedding: emb, Tempo: 122, Energy: 0.7})
		default:
			http.NotFound(w, r)
		}
	}))
}

func newAdapter(t *testing.T, url string) *Adapter {
	t.Helper()
	a := New()
	if err := a.Init(map[string]any{"url": url + "/", "api_key": "k"}); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestConformance(t *testing.T) {
	srv := fakeService(t, http.StatusOK, nil)
	defer srv.Close()
	analysis.RunConformance(t, newAdapter(t, srv.URL))
}

func TestAnalyze_SendsTheSongButNotItsPath(t *testing.T) {
	var seen string
	srv := fakeService(t, http.StatusOK, &seen)
	defer srv.Close()
	a := newAdapter(t, srv.URL)
	f, err := a.Analyze(context.Background(), "/music/Private Folder/01 Song.FLAC", bytes.NewReader([]byte("audio-bytes")))
	if err != nil {
		t.Fatal(err)
	}
	if seen != "song.flac|audio-bytes" {
		t.Fatalf("service saw %q", seen)
	}
	if f.Model != "clap-music-v1" || f.Tempo != 122 || len(f.Embedding) != 32 {
		t.Fatalf("features = %+v", f)
	}
	if err := a.TestConnection(context.Background()); err != nil {
		t.Fatalf("health: %v", err)
	}
}

func TestAnalyze_LoadingIsUnavailable(t *testing.T) {
	srv := fakeService(t, http.StatusServiceUnavailable, nil)
	defer srv.Close()
	_, err := newAdapter(t, srv.URL).Analyze(context.Background(), "a.mp3", strings.NewReader("x"))
	if !errors.Is(err, analysis.ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestAnalyze_RejectsBadAnswers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"m","embedding":[0,0,0]}`))
	}))
	defer srv.Close()
	a := New()
	if err := a.Init(map[string]any{"url": srv.URL}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Analyze(context.Background(), "a.mp3", strings.NewReader("x")); err == nil {
		t.Fatal("a 3-value embedding should be rejected")
	}
}

func TestInit_Validation(t *testing.T) {
	for _, cfg := range []map[string]any{{}, {"url": "ftp://x"}, {"url": "http://x", "timeout_seconds": "soon"}} {
		if err := New().Init(cfg); err == nil {
			t.Errorf("Init(%v) should fail", cfg)
		}
	}
}
