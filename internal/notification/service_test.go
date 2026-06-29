package notification_test

import (
	"context"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/notification"
	"github.com/maxjb-xyz/reverb/internal/store"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// newTestService opens a migrated temp sqlite store and returns the notification Service.
func newTestService(t *testing.T) *notification.Service {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/n.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	// Seed minimum role + two users (notifications have no FK to users, but keep
	// consistent with the rest of the test helpers in the project).
	q := st.Q()
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
		{ID: "user-a", Username: "alice", PasswordHash: "x", RoleID: "role-user"},
		{ID: "user-b", Username: "bob", PasswordHash: "x", RoleID: "role-user"},
	} {
		if err := q.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}

	fixed := time.Unix(1_700_000_000, 0)
	return notification.NewService(q, func() time.Time { return fixed })
}

// ---- helpers ----

func mustCreate(t *testing.T, svc *notification.Service, n core.Notification) core.Notification {
	t.Helper()
	got, err := svc.Create(context.Background(), n)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return got
}

// ---- tests ----

// TestCreateAndListForUser verifies round-trip insertion and newest-first ordering.
func TestCreateAndListForUser(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	n1 := mustCreate(t, svc, core.Notification{
		UserID: "user-a", Type: core.NotifyRequestPending,
		Title: "Pending 1", Body: "body1",
	})
	// Ensure a distinct created_at by bumping the mock clock is not needed here
	// because UUIDs are different; ordering by created_at DESC relies on the
	// fixed-then-incremented timestamps the implementation assigns. We seed a
	// second notification with an explicit CreatedAt one second later.
	n2base := core.Notification{
		UserID: "user-a", Type: core.NotifyRequestApproved,
		Title: "Approved 2", Body: "body2",
		CreatedAt: n1.CreatedAt + 1,
	}
	n2 := mustCreate(t, svc, n2base)

	list, err := svc.ListForUser(ctx, "user-a", 10)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 notifications, got %d", len(list))
	}
	// Newest first
	if list[0].ID != n2.ID {
		t.Errorf("expected newest (n2=%s) first, got %s", n2.ID, list[0].ID)
	}
	if list[1].ID != n1.ID {
		t.Errorf("expected n1=%s second, got %s", n1.ID, list[1].ID)
	}
	// IDs were auto-assigned
	if n1.ID == "" {
		t.Error("n1.ID should not be empty")
	}
	if n2.ID == "" {
		t.Error("n2.ID should not be empty")
	}
}

// TestListForUser_limit verifies LIMIT is respected.
func TestListForUser_limit(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	base := int64(1_700_000_000)
	for i := range 5 {
		mustCreate(t, svc, core.Notification{
			UserID: "user-a", Type: core.NotifyRequestPending,
			Title: "T", Body: "B", CreatedAt: base + int64(i),
		})
	}

	list, err := svc.ListForUser(ctx, "user-a", 3)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("expected 3 (limit), got %d", len(list))
	}
}

// TestCountUnread counts only unread rows for the target user.
func TestCountUnread(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	// user-a: 2 unread
	mustCreate(t, svc, core.Notification{UserID: "user-a", Type: core.NotifyRequestPending, Title: "T", Body: "B"})
	mustCreate(t, svc, core.Notification{UserID: "user-a", Type: core.NotifyRequestApproved, Title: "T", Body: "B"})
	// user-b: 1 unread
	mustCreate(t, svc, core.Notification{UserID: "user-b", Type: core.NotifyRequestPending, Title: "T", Body: "B"})

	countA, err := svc.CountUnread(ctx, "user-a")
	if err != nil {
		t.Fatalf("CountUnread user-a: %v", err)
	}
	if countA != 2 {
		t.Errorf("expected user-a unread=2, got %d", countA)
	}

	countB, err := svc.CountUnread(ctx, "user-b")
	if err != nil {
		t.Fatalf("CountUnread user-b: %v", err)
	}
	if countB != 1 {
		t.Errorf("expected user-b unread=1, got %d", countB)
	}
}

// TestMarkRead flips only the targeted row; leaves another user's and other rows untouched.
func TestMarkRead(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	nA1 := mustCreate(t, svc, core.Notification{UserID: "user-a", Type: core.NotifyRequestPending, Title: "T", Body: "B"})
	nA2 := mustCreate(t, svc, core.Notification{UserID: "user-a", Type: core.NotifyRequestApproved, Title: "T", Body: "B"})
	nB1 := mustCreate(t, svc, core.Notification{UserID: "user-b", Type: core.NotifyRequestPending, Title: "T", Body: "B"})

	_ = nB1 // keep for clarity

	// Mark only nA1 read as user-a
	if err := svc.MarkRead(ctx, "user-a", []string{nA1.ID}); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}

	// user-a: nA1 read, nA2 still unread
	countA, _ := svc.CountUnread(ctx, "user-a")
	if countA != 1 {
		t.Errorf("expected user-a unread=1 after MarkRead(nA1), got %d", countA)
	}

	// user-b unaffected
	countB, _ := svc.CountUnread(ctx, "user-b")
	if countB != 1 {
		t.Errorf("expected user-b unread=1 (unaffected), got %d", countB)
	}

	// nA2 is still unread
	listA, _ := svc.ListForUser(ctx, "user-a", 10)
	for _, n := range listA {
		if n.ID == nA2.ID && n.Read {
			t.Error("nA2 should still be unread")
		}
	}
}

// TestMarkRead_crossUserScope verifies MarkRead won't mark a row owned by another user.
func TestMarkRead_crossUserScope(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	nB1 := mustCreate(t, svc, core.Notification{UserID: "user-b", Type: core.NotifyRequestPending, Title: "T", Body: "B"})

	// user-a tries to mark user-b's notification
	if err := svc.MarkRead(ctx, "user-a", []string{nB1.ID}); err != nil {
		t.Fatalf("MarkRead should not error (no-op): %v", err)
	}

	// user-b's row should still be unread
	countB, _ := svc.CountUnread(ctx, "user-b")
	if countB != 1 {
		t.Errorf("expected user-b unread=1 (scoped MarkRead should no-op), got %d", countB)
	}
}

// TestMarkAllRead zeros user-a's unread without touching user-b.
func TestMarkAllRead(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	mustCreate(t, svc, core.Notification{UserID: "user-a", Type: core.NotifyRequestPending, Title: "T", Body: "B"})
	mustCreate(t, svc, core.Notification{UserID: "user-a", Type: core.NotifyRequestApproved, Title: "T", Body: "B"})
	mustCreate(t, svc, core.Notification{UserID: "user-b", Type: core.NotifyRequestPending, Title: "T", Body: "B"})

	if err := svc.MarkAllRead(ctx, "user-a"); err != nil {
		t.Fatalf("MarkAllRead: %v", err)
	}

	countA, _ := svc.CountUnread(ctx, "user-a")
	if countA != 0 {
		t.Errorf("expected user-a unread=0, got %d", countA)
	}

	countB, _ := svc.CountUnread(ctx, "user-b")
	if countB != 1 {
		t.Errorf("expected user-b unread=1 (unaffected), got %d", countB)
	}
}

// TestResolvePendingForRequest marks only request_pending rows for the given request;
// a request_approved row for the same request stays unread; a request_pending for
// a different request stays unread.
func TestResolvePendingForRequest(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	reqX := "req-x"
	reqY := "req-y"

	// reqX: one pending (should flip) + one approved (should NOT flip)
	nXPending := mustCreate(t, svc, core.Notification{
		UserID: "user-a", Type: core.NotifyRequestPending, Title: "T", Body: "B", RequestID: reqX,
	})
	nXApproved := mustCreate(t, svc, core.Notification{
		UserID: "user-a", Type: core.NotifyRequestApproved, Title: "T", Body: "B", RequestID: reqX,
	})
	// reqY: pending (should NOT flip)
	nYPending := mustCreate(t, svc, core.Notification{
		UserID: "user-a", Type: core.NotifyRequestPending, Title: "T", Body: "B", RequestID: reqY,
	})

	if err := svc.ResolvePendingForRequest(ctx, reqX); err != nil {
		t.Fatalf("ResolvePendingForRequest: %v", err)
	}

	list, _ := svc.ListForUser(ctx, "user-a", 10)
	byID := make(map[string]core.Notification, len(list))
	for _, n := range list {
		byID[n.ID] = n
	}

	if !byID[nXPending.ID].Read {
		t.Error("nXPending should be read after ResolvePendingForRequest(reqX)")
	}
	if byID[nXApproved.ID].Read {
		t.Error("nXApproved should still be unread (type is request_approved, not request_pending)")
	}
	if byID[nYPending.ID].Read {
		t.Error("nYPending (reqY) should still be unread (different request)")
	}
}
