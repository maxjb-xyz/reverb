package request

import (
	"context"
	"errors"
	"log"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/download"
	"github.com/maxjb-xyz/reverb/internal/events"
)

// trackerService is the Service slice the Tracker needs.
type trackerService interface {
	GetByDownloadJob(ctx context.Context, jobID string) (core.Request, error)
	MarkFulfilled(ctx context.Context, id string) error
	MarkFailed(ctx context.Context, id, errMsg string) error
}

// Subscriber is the minimal event-bus interface the Tracker needs.
// *events.Bus satisfies it.
type Subscriber interface {
	Subscribe(topic string) (<-chan events.Event, func())
}

// Tracker listens for download complete/failed events and updates the matching
// request row accordingly. It is constructed once at startup and subscribes to
// the bus once; the bus remains stable across download-manager reloads.
type Tracker struct {
	svc    trackerService
	bus    Subscriber
	cancel context.CancelFunc
	done   chan struct{}
	// unsubscribe funcs, stored so Stop can clean them up.
	unsubs []func()
}

// NewTracker creates a Tracker. Call Start to begin listening.
func NewTracker(svc trackerService, bus Subscriber) *Tracker {
	return &Tracker{
		svc:  svc,
		bus:  bus,
		done: make(chan struct{}),
	}
}

// Start subscribes to download.complete and download.failed and begins processing
// events in a background goroutine. The provided ctx controls lifecycle alongside
// Stop(). It is safe to call Start only once.
func (t *Tracker) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	t.cancel = cancel

	completeCh, unsubComplete := t.bus.Subscribe(download.TopicComplete)
	failedCh, unsubFailed := t.bus.Subscribe(download.TopicFailed)
	t.unsubs = []func(){unsubComplete, unsubFailed}

	go func() {
		defer close(t.done)
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-completeCh:
				if !ok {
					return
				}
				t.handleEvent(ctx, ev, false)
			case ev, ok := <-failedCh:
				if !ok {
					return
				}
				t.handleEvent(ctx, ev, true)
			}
		}
	}()
}

// Stop cancels the background goroutine and unsubscribes from the bus.
func (t *Tracker) Stop() {
	if t.cancel != nil {
		t.cancel()
	}
	for _, unsub := range t.unsubs {
		unsub()
	}
	<-t.done
}

// handleEvent resolves the job to a request and calls MarkFulfilled or MarkFailed.
func (t *Tracker) handleEvent(ctx context.Context, ev events.Event, failed bool) {
	de, ok := ev.Payload.(core.DownloadEvent)
	if !ok {
		return
	}
	req, err := t.svc.GetByDownloadJob(ctx, de.JobID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return // not a request-linked job — ignore
		}
		log.Printf("tracker: GetByDownloadJob(%q): %v", de.JobID, err)
		return
	}
	if failed {
		if err := t.svc.MarkFailed(ctx, req.ID, de.Error); err != nil {
			log.Printf("tracker: MarkFailed(%q): %v", req.ID, err)
		}
	} else {
		if err := t.svc.MarkFulfilled(ctx, req.ID); err != nil {
			log.Printf("tracker: MarkFulfilled(%q): %v", req.ID, err)
		}
	}
}
