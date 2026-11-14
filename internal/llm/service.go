package llm

import (
	"context"
	"errors"
	"sync"
)

// ErrNoLLM is returned by Service.Complete when no model is configured.
var ErrNoLLM = errors.New("llm: no language model is configured")

// Service holds the active model, swapped live when an admin changes the
// provider.
type Service struct {
	mu   sync.RWMutex
	m    LLM
	name string
}

// NewService returns a service with no model set.
func NewService() *Service { return &Service{} }

// Set makes m the active model. nil turns it off.
func (s *Service) Set(m LLM) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m = m
	s.name = ""
	if m != nil {
		s.name = m.Name()
	}
}

// Active names the active adapter, or "" when there is none.
func (s *Service) Active() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.name
}

// Complete runs req on the active model.
func (s *Service) Complete(ctx context.Context, req Request) (string, error) {
	s.mu.RLock()
	m := s.m
	s.mu.RUnlock()
	if m == nil {
		return "", ErrNoLLM
	}
	return m.Complete(ctx, req)
}
