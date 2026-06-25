package embedded

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeProcess returns from Wait when its ctx is canceled or crash is signaled.
type fakeProcess struct {
	ctx   context.Context
	crash chan struct{}
}

func (p *fakeProcess) Wait() error {
	select {
	case <-p.ctx.Done():
		return p.ctx.Err()
	case <-p.crash:
		return errors.New("crashed")
	}
}

func TestSupervisor_ExternalMode_RunsNothing(t *testing.T) {
	var started bool
	s := New(Options{
		Mode:   ModeExternal,
		Runner: func(ctx context.Context, _ []string) (Process, error) { started = true; return nil, nil },
		Probe:  func(context.Context) error { return nil },
	})
	s.Start()
	if started {
		t.Fatal("external mode must not start a child")
	}
	if s.Health() != HealthExternal {
		t.Errorf("health = %q, want external", s.Health())
	}
	if err := s.Shutdown(context.Background()); err != nil {
		t.Errorf("shutdown: %v", err)
	}
}

func TestSupervisor_BuiltIn_BecomesReadyThenShutsDown(t *testing.T) {
	var mu sync.Mutex
	var proc *fakeProcess
	s := New(Options{
		Mode: ModeBuiltIn,
		Runner: func(ctx context.Context, _ []string) (Process, error) {
			mu.Lock()
			proc = &fakeProcess{ctx: ctx, crash: make(chan struct{})}
			mu.Unlock()
			return proc, nil
		},
		Probe:      func(context.Context) error { return nil }, // immediately ready
		ProbeEvery: time.Millisecond,
	})
	s.Start()

	deadline := time.Now().Add(2 * time.Second)
	for !s.Ready() && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if !s.Ready() {
		t.Fatalf("never became ready; health=%q", s.Health())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}
