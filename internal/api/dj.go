package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/maxjb-xyz/reverb/internal/dj"
	"github.com/maxjb-xyz/reverb/internal/llm"
	"github.com/maxjb-xyz/reverb/internal/voice"
)

// LLMService is the DJ's writer model. *llm.Service satisfies it.
type LLMService interface {
	Active() string
	Complete(ctx context.Context, req llm.Request) (string, error)
}

// djLinePerMinute caps how many DJ lines one user can have written per minute.
const djLinePerMinute = 30

type djProvider struct {
	Available bool   `json:"available"`
	Name      string `json:"name"`
}

func (s *Server) handleDJStatus(w http.ResponseWriter, r *http.Request) {
	var v, m string
	if s.deps.Voice != nil {
		v = s.deps.Voice.Active()
	}
	if s.deps.LLM != nil {
		m = s.deps.LLM.Active()
	}
	writeJSON(w, http.StatusOK, map[string]djProvider{
		"voice":  {Available: v != "", Name: v},
		"writer": {Available: m != "", Name: m},
	})
}

func (s *Server) handleDJLine(w http.ResponseWriter, r *http.Request) {
	var seg dj.Segment
	if err := decode(r, &seg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	if len(seg.Normalize().Next) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "next must name at least one song"})
		return
	}
	key := "anon"
	if cu, ok := currentUser(r); ok {
		key = cu.ID
	}
	if !s.djLineLimiter.allow(key) {
		w.Header().Set("Retry-After", "60")
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many DJ lines, try again in a minute"})
		return
	}
	var m dj.Completer
	if s.deps.LLM != nil && s.deps.LLM.Active() != "" {
		m = s.deps.LLM
	}
	writeJSON(w, http.StatusOK, dj.NewWriter(m).Line(r.Context(), seg))
}

type djRequestBody struct {
	Text string `json:"text"`
}

// handleDJRequest serves POST /api/v1/dj/request: read what the listener asked
// the DJ for ("90s hip hop", "more like Bonobo") as an engine seed, lane and
// period. Shares the line writer's rate limit, since both can call the model.
func (s *Server) handleDJRequest(w http.ResponseWriter, r *http.Request) {
	var body djRequestBody
	if err := decode(r, &body); err != nil || strings.TrimSpace(body.Text) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "text is required"})
		return
	}
	key := "anon"
	if cu, ok := currentUser(r); ok {
		key = cu.ID
	}
	if !s.djLineLimiter.allow(key) {
		w.Header().Set("Retry-After", "60")
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many DJ requests, try again in a minute"})
		return
	}
	var m dj.Completer
	if s.deps.LLM != nil && s.deps.LLM.Active() != "" {
		m = s.deps.LLM
	}
	it, err := dj.NewInterpreter(s.deps.DJCatalog, m, time.Now).Interpret(r.Context(), body.Text)
	if err != nil {
		log.Printf("dj: reading a request failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "couldn't read the library"})
		return
	}
	writeJSON(w, http.StatusOK, it)
}

// VoiceService is the DJ's voice: it renders a line to a cached clip.
// *voice.Service satisfies it.
type VoiceService interface {
	// Active names the active voice adapter, or "" when none is configured.
	Active() string
	Say(ctx context.Context, text string) (id string, err error)
	Open(id string) (path, contentType string, ok bool)
}

// djSayPerMinute caps how many lines one user can have rendered per minute.
// Hosted voices bill per character, so a runaway client must not run up a bill.
const djSayPerMinute = 30

func (s *Server) handleDJVoice(w http.ResponseWriter, r *http.Request) {
	name := ""
	if s.deps.Voice != nil {
		name = s.deps.Voice.Active()
	}
	writeJSON(w, http.StatusOK, map[string]any{"available": name != "", "name": name})
}

type djSayBody struct {
	Text string `json:"text"`
}

func (s *Server) handleDJSay(w http.ResponseWriter, r *http.Request) {
	if s.deps.Voice == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no voice is configured"})
		return
	}
	var body djSayBody
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	key := "anon"
	if cu, ok := currentUser(r); ok {
		key = cu.ID
	}
	if !s.djSayLimiter.allow(key) {
		w.Header().Set("Retry-After", "60")
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many voice lines, try again in a minute"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	id, err := s.deps.Voice.Say(ctx, body.Text)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]string{"id": id, "url": "/api/v1/dj/clips/" + id})
	case errors.Is(err, voice.ErrNoVoice):
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no voice is configured"})
	case errors.Is(err, voice.ErrWarmingUp):
		w.Header().Set("Retry-After", "30")
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the voice is still being set up"})
	case errors.Is(err, voice.ErrEmptyText), errors.Is(err, voice.ErrTextTooLong):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	default:
		// The provider's own message can echo request details; keep it in the log.
		log.Printf("dj: voice %q failed: %v", s.deps.Voice.Active(), err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "the voice provider failed"})
	}
}

func (s *Server) handleDJClip(w http.ResponseWriter, r *http.Request) {
	if s.deps.Voice == nil {
		http.NotFound(w, r)
		return
	}
	path, ctype, ok := s.deps.Voice.Open(chi.URLParam(r, "id"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "clip not found"})
		return
	}
	w.Header().Set("Content-Type", ctype)
	// A clip id is a hash of the voice and the words, so its bytes never change.
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeFile(w, r, path)
}
