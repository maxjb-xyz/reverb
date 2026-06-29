package notification_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/events"
	"github.com/maxjb-xyz/reverb/internal/notification"
	"github.com/maxjb-xyz/reverb/internal/request"
	"github.com/maxjb-xyz/reverb/internal/store"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// pollUntilN waits up to timeout for check() to return true.
func pollUntilN(t *testing.T, timeout time.Duration, check func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return check()
}

// capturedEvents is a thread-safe collector of events.Event.
type capturedEvents struct {
	mu   sync.Mutex
	evts []events.Event
}

func (c *capturedEvents) add(ev events.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evts = append(c.evts, ev)
}

func (c *capturedEvents) all() []events.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]events.Event, len(c.evts))
	copy(out, c.evts)
	return out
}

func (c *capturedEvents) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.evts)
}

// fakeAuthLister returns a fixed manager list: m1 + m2 are managers, n1 is not.
// Satisfies notification.AuthLister.
type fakeAuthLister struct {
	managerIDs []string
}

func (f *fakeAuthLister) ListManagerIDs(ctx context.Context) ([]string, error) {
	return f.managerIDs, nil
}

// newNotifierTestStore opens a migrated temp sqlite store.
func newNotifierTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/notifier.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	return st
}

// seedMinimalRoleAndUsers seeds the DB with a role + enough users.
// Returns the db.Queries handle.
func seedMinimalRoleAndUsers(t *testing.T, q *db.Queries) {
	t.Helper()
	ctx := context.Background()
	if err := q.CreateRole(ctx, db.CreateRoleParams{
		ID:           "role-user",
		Name:         "User",
		IsSystem:     1,
		Capabilities: `["request"]`,
	}); err != nil {
		t.Fatal(err)
	}
	for _, u := range []db.CreateUserParams{
		{ID: "u1", Username: "user1", PasswordHash: "x", RoleID: "role-user"},
		{ID: "m1", Username: "manager1", PasswordHash: "x", RoleID: "role-user"},
		{ID: "m2", Username: "manager2", PasswordHash: "x", RoleID: "role-user"},
		{ID: "n1", Username: "nonmgr1", PasswordHash: "x", RoleID: "role-user"},
	} {
		if err := q.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
}

// setupNotifier creates the bus, notification service, fake auth lister,
// and the Notifier. It also subscribes a "notification" capture channel.
func setupNotifier(t *testing.T, managerIDs []string) (
	bus *events.Bus,
	svc *notification.Service,
	notif *notification.Notifier,
	captured *capturedEvents,
) {
	t.Helper()
	st := newNotifierTestStore(t)
	seedMinimalRoleAndUsers(t, st.Q())

	fixed := time.Unix(1_700_000_000, 0)
	svc = notification.NewService(st.Q(), func() time.Time { return fixed })

	bus = events.New()
	auth := &fakeAuthLister{managerIDs: managerIDs}
	notif = notification.NewNotifier(bus, svc, auth)
	notif.Start(context.Background())
	t.Cleanup(func() { notif.Stop() })

	// Subscribe to "notification" events BEFORE publishing.
	captured = &capturedEvents{}
	ch, unsub := bus.Subscribe("notification")
	t.Cleanup(unsub)
	go func() {
		for ev := range ch {
			captured.add(ev)
		}
	}()

	return bus, svc, notif, captured
}

// publishRequestCreated publishes a request.created event.
func publishRequestCreated(bus *events.Bus, req core.Request) {
	bus.Publish(events.Event{
		Topic: request.TopicCreated,
		Payload: core.RequestEvent{
			Request:     req,
			ForManagers: true,
		},
	})
}

// publishRequestUpdated publishes a request.updated event.
func publishRequestUpdated(bus *events.Bus, req core.Request) {
	bus.Publish(events.Event{
		Topic: request.TopicUpdated,
		Payload: core.RequestEvent{
			Request:      req,
			TargetUserID: req.RequestedBy,
		},
	})
}

// ---- tests ----

// TestNotifier_RequestCreated_FansOutToManagers verifies that a request.created
// event causes one request_pending notification per manager (m1 + m2) but NOT
// for the non-manager (n1), and publishes one "notification" bus event per manager.
func TestNotifier_RequestCreated_FansOutToManagers(t *testing.T) {
	managers := []string{"m1", "m2"}
	bus, svc, _, captured := setupNotifier(t, managers)

	req := core.Request{
		ID:          "req-1",
		RequestedBy: "u1",
		Title:       "Bohemian Rhapsody",
		Artist:      "Queen",
		Status:      core.RequestPending,
	}
	publishRequestCreated(bus, req)

	// Wait for 2 notification events.
	ok := pollUntilN(t, 500*time.Millisecond, func() bool {
		return captured.count() >= 2
	})
	if !ok {
		t.Fatalf("expected 2 notification events published, got %d", captured.count())
	}

	// Both events must target managers.
	ctx := context.Background()
	evts := captured.all()
	targets := make(map[string]bool)
	for _, ev := range evts {
		ne, ok := ev.Payload.(core.NotificationEvent)
		if !ok {
			t.Fatalf("expected NotificationEvent payload, got %T", ev.Payload)
		}
		targets[ne.TargetUserID] = true
		if ne.Notification.Type != core.NotifyRequestPending {
			t.Errorf("expected Type=%q, got %q", core.NotifyRequestPending, ne.Notification.Type)
		}
		if ne.Notification.RequestID != req.ID {
			t.Errorf("expected RequestID=%q, got %q", req.ID, ne.Notification.RequestID)
		}
	}
	if !targets["m1"] || !targets["m2"] {
		t.Errorf("expected events for m1+m2, got targets: %v", targets)
	}
	if targets["n1"] {
		t.Error("n1 is not a manager and should not receive a notification event")
	}

	// Verify DB rows exist for m1 and m2.
	for _, uid := range managers {
		ns, err := svc.ListForUser(ctx, uid, 10)
		if err != nil {
			t.Fatalf("ListForUser(%s): %v", uid, err)
		}
		if len(ns) != 1 {
			t.Fatalf("expected 1 notification for %s, got %d", uid, len(ns))
		}
		if ns[0].Type != core.NotifyRequestPending {
			t.Errorf("%s: expected Type=%q, got %q", uid, core.NotifyRequestPending, ns[0].Type)
		}
	}

	// Non-manager n1 gets nothing.
	nsN1, err := svc.ListForUser(ctx, "n1", 10)
	if err != nil {
		t.Fatalf("ListForUser(n1): %v", err)
	}
	if len(nsN1) != 0 {
		t.Errorf("n1 should have 0 notifications, got %d", len(nsN1))
	}
}

// TestNotifier_RequestUpdated_Approved notifies the requester, publishes a
// notification event to them, and calls ResolvePendingForRequest.
func TestNotifier_RequestUpdated_Approved(t *testing.T) {
	managers := []string{"m1", "m2"}
	bus, svc, _, captured := setupNotifier(t, managers)
	ctx := context.Background()

	// Seed a pending notification for m1 so we can verify it gets resolved.
	pendingForM1, err := svc.Create(ctx, core.Notification{
		UserID:    "m1",
		Type:      core.NotifyRequestPending,
		Title:     "pending",
		Body:      "body",
		RequestID: "req-2",
	})
	if err != nil {
		t.Fatalf("seed pending notification: %v", err)
	}

	req := core.Request{
		ID:          "req-2",
		RequestedBy: "u1",
		Title:       "Stairway to Heaven",
		Artist:      "Led Zeppelin",
		Status:      core.RequestApproved,
	}
	publishRequestUpdated(bus, req)

	// Wait for 1 notification event (to the requester).
	ok := pollUntilN(t, 500*time.Millisecond, func() bool {
		return captured.count() >= 1
	})
	if !ok {
		t.Fatalf("expected 1 notification event published for approved, got %d", captured.count())
	}

	evts := captured.all()
	if len(evts) != 1 {
		t.Fatalf("expected exactly 1 notification event, got %d", len(evts))
	}
	ne, ok := evts[0].Payload.(core.NotificationEvent)
	if !ok {
		t.Fatalf("expected NotificationEvent, got %T", evts[0].Payload)
	}
	if ne.TargetUserID != "u1" {
		t.Errorf("expected TargetUserID=u1, got %q", ne.TargetUserID)
	}
	if ne.Notification.Type != core.NotifyRequestApproved {
		t.Errorf("expected Type=%q, got %q", core.NotifyRequestApproved, ne.Notification.Type)
	}

	// DB row for u1.
	ns, err := svc.ListForUser(ctx, "u1", 10)
	if err != nil {
		t.Fatalf("ListForUser(u1): %v", err)
	}
	if len(ns) != 1 {
		t.Fatalf("expected 1 notification for u1, got %d", len(ns))
	}
	if ns[0].Type != core.NotifyRequestApproved {
		t.Errorf("expected Type=%q, got %q", core.NotifyRequestApproved, ns[0].Type)
	}

	// ResolvePendingForRequest must have marked m1's pending notification as read.
	ok = pollUntilN(t, 500*time.Millisecond, func() bool {
		list, _ := svc.ListForUser(ctx, "m1", 10)
		for _, n := range list {
			if n.ID == pendingForM1.ID {
				return n.Read
			}
		}
		return false
	})
	if !ok {
		list, _ := svc.ListForUser(ctx, "m1", 10)
		for _, n := range list {
			if n.ID == pendingForM1.ID {
				t.Errorf("expected m1's pending notification to be resolved (read=true), got read=%v", n.Read)
			}
		}
	}
}

// TestNotifier_RequestUpdated_Fulfilled notifies the requester with request_fulfilled.
func TestNotifier_RequestUpdated_Fulfilled(t *testing.T) {
	managers := []string{"m1", "m2"}
	bus, svc, _, captured := setupNotifier(t, managers)
	ctx := context.Background()

	req := core.Request{
		ID:          "req-3",
		RequestedBy: "u1",
		Title:       "Hotel California",
		Artist:      "Eagles",
		Status:      core.RequestFulfilled,
	}
	publishRequestUpdated(bus, req)

	ok := pollUntilN(t, 500*time.Millisecond, func() bool {
		return captured.count() >= 1
	})
	if !ok {
		t.Fatalf("expected 1 notification event for fulfilled, got %d", captured.count())
	}

	ne, ok := captured.all()[0].Payload.(core.NotificationEvent)
	if !ok {
		t.Fatalf("expected NotificationEvent, got %T", captured.all()[0].Payload)
	}
	if ne.TargetUserID != "u1" {
		t.Errorf("expected TargetUserID=u1, got %q", ne.TargetUserID)
	}
	if ne.Notification.Type != core.NotifyRequestFulfilled {
		t.Errorf("expected Type=%q, got %q", core.NotifyRequestFulfilled, ne.Notification.Type)
	}

	ns, err := svc.ListForUser(ctx, "u1", 10)
	if err != nil {
		t.Fatalf("ListForUser(u1): %v", err)
	}
	if len(ns) != 1 || ns[0].Type != core.NotifyRequestFulfilled {
		t.Errorf("expected 1 request_fulfilled notification for u1")
	}
}

// TestNotifier_RequestUpdated_Denied notifies the requester with request_denied.
func TestNotifier_RequestUpdated_Denied(t *testing.T) {
	managers := []string{"m1", "m2"}
	bus, svc, _, captured := setupNotifier(t, managers)
	ctx := context.Background()

	// Seed a pending notification for m1.
	pendingForM1, err := svc.Create(ctx, core.Notification{
		UserID:    "m1",
		Type:      core.NotifyRequestPending,
		Title:     "pending",
		Body:      "body",
		RequestID: "req-4",
	})
	if err != nil {
		t.Fatalf("seed pending: %v", err)
	}

	req := core.Request{
		ID:          "req-4",
		RequestedBy: "u1",
		Title:       "Imagine",
		Artist:      "John Lennon",
		Status:      core.RequestDenied,
	}
	publishRequestUpdated(bus, req)

	ok := pollUntilN(t, 500*time.Millisecond, func() bool {
		return captured.count() >= 1
	})
	if !ok {
		t.Fatalf("expected 1 notification event for denied, got %d", captured.count())
	}

	ne, ok := captured.all()[0].Payload.(core.NotificationEvent)
	if !ok {
		t.Fatalf("expected NotificationEvent, got %T", captured.all()[0].Payload)
	}
	if ne.TargetUserID != "u1" {
		t.Errorf("expected TargetUserID=u1, got %q", ne.TargetUserID)
	}
	if ne.Notification.Type != core.NotifyRequestDenied {
		t.Errorf("expected Type=%q, got %q", core.NotifyRequestDenied, ne.Notification.Type)
	}

	// Resolve must have fired.
	ok = pollUntilN(t, 500*time.Millisecond, func() bool {
		list, _ := svc.ListForUser(ctx, "m1", 10)
		for _, n := range list {
			if n.ID == pendingForM1.ID {
				return n.Read
			}
		}
		return false
	})
	if !ok {
		t.Error("expected m1's pending notification to be resolved after denied")
	}
}

// TestNotifier_RequestUpdated_NonTerminalStatus_NoNotification verifies that an
// update with a non-terminal status (e.g. still pending) produces NO notification
// and NO published event.
func TestNotifier_RequestUpdated_NonTerminalStatus_NoNotification(t *testing.T) {
	managers := []string{"m1", "m2"}
	bus, svc, _, captured := setupNotifier(t, managers)
	ctx := context.Background()

	req := core.Request{
		ID:          "req-5",
		RequestedBy: "u1",
		Title:       "Sound of Silence",
		Artist:      "Simon & Garfunkel",
		Status:      core.RequestPending, // non-terminal
	}
	publishRequestUpdated(bus, req)

	// Give the goroutine time to process.
	time.Sleep(100 * time.Millisecond)

	if captured.count() != 0 {
		t.Errorf("expected 0 notification events for non-terminal status update, got %d", captured.count())
	}

	// No notifications in DB for either user.
	for _, uid := range []string{"u1", "m1", "m2", "n1"} {
		ns, err := svc.ListForUser(ctx, uid, 10)
		if err != nil {
			t.Fatalf("ListForUser(%s): %v", uid, err)
		}
		if len(ns) != 0 {
			t.Errorf("expected 0 notifications for %s on non-terminal update, got %d", uid, len(ns))
		}
	}
}

// TestNotifier_RequestUpdated_Failed_NoNotification verifies that a "failed" status
// (internal download failure, not user-visible) also produces no notification.
// (Only approved/denied/fulfilled are terminal statuses the notifier acts on.)
func TestNotifier_RequestUpdated_Failed_NoNotification(t *testing.T) {
	managers := []string{"m1", "m2"}
	bus, svc, _, captured := setupNotifier(t, managers)
	ctx := context.Background()

	req := core.Request{
		ID:          "req-6",
		RequestedBy: "u1",
		Title:       "Hey Jude",
		Artist:      "The Beatles",
		Status:      core.RequestFailed, // internal failure — not notified
	}
	publishRequestUpdated(bus, req)

	time.Sleep(100 * time.Millisecond)

	if captured.count() != 0 {
		t.Errorf("expected 0 notification events for 'failed' status, got %d", captured.count())
	}
	ns, err := svc.ListForUser(ctx, "u1", 10)
	if err != nil {
		t.Fatalf("ListForUser(u1): %v", err)
	}
	if len(ns) != 0 {
		t.Errorf("expected 0 notifications for u1 on failed status, got %d", len(ns))
	}
}
