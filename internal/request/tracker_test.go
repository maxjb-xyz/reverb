package request_test

import (
	"context"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/download"
	"github.com/maxjb-xyz/reverb/internal/events"
	"github.com/maxjb-xyz/reverb/internal/request"
)

// pollUntil repeatedly calls check until it returns true or the timeout expires.
// Returns true if check passed within the deadline.
func pollUntil(t *testing.T, timeout time.Duration, check func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return check() // one final attempt
}

// newTrackerTestService creates a real Service backed by a temp SQLite DB with the
// minimum schema / users seeded (reuses the helper pattern from service_test.go).
func newTrackerTestService(t *testing.T) (*request.Service, string) {
	t.Helper()
	svc, _, userID := newTestService(t)
	return svc, userID
}

// TestTrackerCompleteFulfillsRequest verifies that when the download manager publishes
// a download.complete event whose JobID matches a request's download_job_id, the
// Tracker calls MarkFulfilled and the request transitions to "fulfilled".
func TestTrackerCompleteFulfillsRequest(t *testing.T) {
	svc, userID := newTrackerTestService(t)
	ctx := context.Background()

	// Create a request and approve it so it has a download_job_id.
	req, _, err := svc.Create(ctx, userID, testItem)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err = svc.MarkApproved(ctx, req.ID, "manager-1", "j1")
	if err != nil {
		t.Fatalf("MarkApproved: %v", err)
	}

	bus := events.New()
	tracker := request.NewTracker(svc, bus)
	tracker.Start(context.Background())
	defer tracker.Stop()

	bus.Publish(events.Event{
		Topic:   download.TopicComplete,
		Payload: core.DownloadEvent{JobID: "j1", Status: core.DownloadCompleted},
	})

	ok := pollUntil(t, 500*time.Millisecond, func() bool {
		r, err := svc.Get(ctx, req.ID)
		return err == nil && r.Status == core.RequestFulfilled
	})
	if !ok {
		r, _ := svc.Get(ctx, req.ID)
		t.Fatalf("want status=%q, got %q after download.complete", core.RequestFulfilled, r.Status)
	}
}

// TestTrackerFailedFailsRequest verifies that a download.failed event transitions the
// request to "failed".
func TestTrackerFailedFailsRequest(t *testing.T) {
	svc, userID := newTrackerTestService(t)
	ctx := context.Background()

	req, _, err := svc.Create(ctx, userID, testItem)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err = svc.MarkApproved(ctx, req.ID, "manager-1", "j1")
	if err != nil {
		t.Fatalf("MarkApproved: %v", err)
	}

	bus := events.New()
	tracker := request.NewTracker(svc, bus)
	tracker.Start(context.Background())
	defer tracker.Stop()

	bus.Publish(events.Event{
		Topic:   download.TopicFailed,
		Payload: core.DownloadEvent{JobID: "j1", Status: core.DownloadFailed, Error: "network error"},
	})

	ok := pollUntil(t, 500*time.Millisecond, func() bool {
		r, err := svc.Get(ctx, req.ID)
		return err == nil && r.Status == core.RequestFailed
	})
	if !ok {
		r, _ := svc.Get(ctx, req.ID)
		t.Fatalf("want status=%q, got %q after download.failed", core.RequestFailed, r.Status)
	}
}

// TestTrackerUnknownJobIgnored verifies that an event for a job not linked to any
// request is silently ignored — no panic, no state change on other requests.
func TestTrackerUnknownJobIgnored(t *testing.T) {
	svc, userID := newTrackerTestService(t)
	ctx := context.Background()

	req, _, err := svc.Create(ctx, userID, testItem)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err = svc.MarkApproved(ctx, req.ID, "manager-1", "j1")
	if err != nil {
		t.Fatalf("MarkApproved: %v", err)
	}

	bus := events.New()
	tracker := request.NewTracker(svc, bus)
	tracker.Start(context.Background())
	defer tracker.Stop()

	// Publish an event for an unrelated job ID.
	bus.Publish(events.Event{
		Topic:   download.TopicComplete,
		Payload: core.DownloadEvent{JobID: "unknown-job", Status: core.DownloadCompleted},
	})

	// Give the goroutine time to process.
	time.Sleep(100 * time.Millisecond)

	// The linked request must stay in "approved" (not fulfilled).
	r, err := svc.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if r.Status != core.RequestApproved {
		t.Fatalf("want status=%q unchanged, got %q", core.RequestApproved, r.Status)
	}
}
