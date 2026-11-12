package voice

import (
	"context"
	"errors"
	"sync"
)

// ErrNoVoice is returned by Service.Say when no voice is configured.
var ErrNoVoice = errors.New("voice: no voice is configured")

// Identifier is implemented by voices whose clips depend on settings beyond
// their name (which voice, which model). Clips are cached per identity.
type Identifier interface {
	Identity() string
}

// IdentityOf returns v's cache identity, falling back to its name.
func IdentityOf(v Voice) string {
	if i, ok := v.(Identifier); ok {
		if id := i.Identity(); id != "" {
			return id
		}
	}
	return v.Name()
}

// Service holds the active voice. The voice is swapped when an admin changes
// the voice provider; the clip cache outlives the swap.
type Service struct {
	cache *Cache

	mu      sync.RWMutex
	speaker *Speaker
	name    string
}

// NewService returns a service with no voice set.
func NewService(c *Cache) *Service { return &Service{cache: c} }

// Set makes v the active voice. nil turns the voice off.
func (s *Service) Set(v Voice) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v == nil {
		s.speaker, s.name = nil, ""
		return
	}
	s.speaker = NewSpeaker(v, IdentityOf(v), s.cache)
	s.name = v.Name()
}

// Active names the active voice adapter, or "" when there is none.
func (s *Service) Active() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.name
}

// Say renders text with the active voice and returns the clip id.
func (s *Service) Say(ctx context.Context, text string) (string, error) {
	s.mu.RLock()
	sp := s.speaker
	s.mu.RUnlock()
	if sp == nil {
		return "", ErrNoVoice
	}
	return sp.Say(ctx, text)
}

// Open finds a rendered clip by id.
func (s *Service) Open(id string) (path, contentType string, ok bool) {
	return s.cache.Open(id)
}
