package embedded

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// Process is a running child (test seam).
type Process interface{ Wait() error }

// Runner starts a child process with env and returns it.
type Runner func(ctx context.Context, env []string) (Process, error)

// Probe reports nil when the child is serving.
type Probe func(ctx context.Context) error

type Options struct {
	Mode         Mode
	Env          []string
	Runner       Runner
	Probe        Probe
	ProbeEvery   time.Duration
	RestartDelay time.Duration
	MaxRestarts  int
}

type Supervisor struct {
	opts     Options
	mu       sync.Mutex
	health   Health
	sawReady bool
	cancel   context.CancelFunc
	done     chan struct{}
}

func New(o Options) *Supervisor {
	if o.ProbeEvery == 0 {
		o.ProbeEvery = 500 * time.Millisecond
	}
	if o.RestartDelay == 0 {
		o.RestartDelay = time.Second
	}
	if o.MaxRestarts == 0 {
		o.MaxRestarts = 5
	}
	h := HealthStarting
	if o.Mode != ModeBuiltIn {
		h = HealthExternal
	}
	return &Supervisor{opts: o, health: h, done: make(chan struct{})}
}

func (s *Supervisor) Health() Health { s.mu.Lock(); defer s.mu.Unlock(); return s.health }
func (s *Supervisor) Ready() bool    { return s.Health() == HealthReady }

func (s *Supervisor) setHealth(h Health) { s.mu.Lock(); s.health = h; s.mu.Unlock() }

// Start launches the supervise loop. No-op (beyond external health) when not built-in.
func (s *Supervisor) Start() {
	if s.opts.Mode != ModeBuiltIn {
		close(s.done)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	go s.supervise(ctx)
}

func (s *Supervisor) supervise(ctx context.Context) {
	defer close(s.done)
	restarts := 0
	for {
		proc, err := s.opts.Runner(ctx, s.opts.Env)
		if err != nil {
			log.Printf("navidrome: start failed: %v", err)
		} else {
			s.mu.Lock()
			s.sawReady = false
			s.mu.Unlock()
			readyCtx, stopReady := context.WithCancel(ctx)
			go s.waitReady(readyCtx)
			werr := proc.Wait()
			stopReady()
			if ctx.Err() != nil {
				return // shutting down
			}
			log.Printf("navidrome: exited: %v", werr)
		}
		if ctx.Err() != nil {
			return
		}
		s.mu.Lock()
		hadReady := s.sawReady
		s.mu.Unlock()
		if hadReady {
			restarts = 0 // a previously-healthy instance crashed: fresh budget
		} else {
			restarts++
		}
		if restarts >= s.opts.MaxRestarts {
			s.setHealth(HealthDegraded)
			log.Printf("navidrome: %d consecutive failures — degraded; stopping restarts", restarts)
			return
		}
		s.setHealth(HealthStarting)
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.opts.RestartDelay * time.Duration(restarts+1)):
		}
	}
}

func (s *Supervisor) waitReady(ctx context.Context) {
	t := time.NewTicker(s.opts.ProbeEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.opts.Probe(ctx); err == nil {
				s.mu.Lock()
				if s.health != HealthDegraded {
					s.health = HealthReady
				}
				s.sawReady = true
				s.mu.Unlock()
				return
			}
		}
	}
}

// Shutdown cancels the supervise loop (which SIGTERMs the child via ExecRunner's
// cmd.Cancel) and waits for it to exit, or until ctx is done.
func (s *Supervisor) Shutdown(ctx context.Context) error {
	if s.cancel != nil {
		s.cancel()
	}
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ExecRunner runs the real navidrome binary. Context cancel sends SIGTERM (via
// cmd.Cancel), then SIGKILL after WaitDelay — a graceful child shutdown.
func ExecRunner(binaryPath string) Runner {
	return func(ctx context.Context, env []string) (Process, error) {
		cmd := exec.CommandContext(ctx, binaryPath)
		cmd.Env = env
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
		cmd.WaitDelay = 10 * time.Second
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return execProcess{cmd}, nil
	}
}

type execProcess struct{ cmd *exec.Cmd }

func (p execProcess) Wait() error { return p.cmd.Wait() }

// PingProbe returns a Probe that hits the Subsonic ping endpoint (auth omitted —
// any HTTP response means the server is up and accepting connections).
func PingProbe(baseURL string, hc *http.Client) Probe {
	if hc == nil {
		hc = &http.Client{Timeout: 3 * time.Second}
	}
	return func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/rest/ping", nil)
		if err != nil {
			return err
		}
		resp, err := hc.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()
		return nil
	}
}
