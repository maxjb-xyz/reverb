package playlistsync

import (
	"context"
	"time"
)

type Scheduler struct {
	svc      *Service
	interval time.Duration
}

func NewScheduler(svc *Service, interval time.Duration) *Scheduler {
	return &Scheduler{svc: svc, interval: interval}
}

// Run ticks until ctx is cancelled, syncing due playlists each tick.
func (s *Scheduler) Run(ctx context.Context) {
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.tick(ctx)
		}
	}
}

// tick syncs every due playlist sequentially; failures are logged and skipped.
func (s *Scheduler) tick(ctx context.Context) {
	rows, err := s.svc.store.ListDue(ctx, s.svc.now())
	if err != nil {
		return
	}
	for _, r := range rows {
		if _, err := s.svc.Sync(ctx, r.ID); err != nil {
			// log-and-continue (use the project logger if one is wired; else skip)
			continue
		}
	}
}
