package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/auth"
	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/events"
	"github.com/maxjb-xyz/reverb/internal/notification"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store"
)

// notifTestServer builds a Server backed by a real store with the notification
// service wired in. Returns the store, server, notifSvc, owner session cookie,
// owner user ID, and the auth service (so callers can create additional users).
func notifTestServer(t *testing.T) (*store.Store, *Server, *notification.Service, *http.Cookie, string, *auth.Service) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/notif.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	authSvc, ownerTok := seededAuthToken(t, st)

	// Fetch the owner user ID so tests can seed notifications for them.
	users, err := authSvc.ListUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(users) == 0 {
		t.Fatal("expected at least one user (owner)")
	}
	ownerID := users[0].ID

	bus := events.New()
	notifSvc := notification.NewService(st.Q(), time.Now)

	srv := NewServer(Deps{
		Auth:          authSvc,
		Events:        bus,
		Notifications: notifSvc,
		Search:        registry.NewRegistry("search"),
		Downloader:    registry.NewRegistry("downloader"),
	})
	return st, srv, notifSvc, &http.Cookie{Name: sessionCookie, Value: ownerTok}, ownerID, authSvc
}

// TestListNotificationsReturnsCallerOwn verifies that GET /notifications returns
// the authenticated caller's notifications with the correct unread count.
func TestListNotificationsReturnsCallerOwn(t *testing.T) {
	_, srv, notifSvc, cookie, ownerID, _ := notifTestServer(t)
	ctx := context.Background()

	// Seed 2 notifications for the owner: 1 unread, 1 read.
	_, err := notifSvc.Create(ctx, core.Notification{
		UserID: ownerID,
		Type:   core.NotifyRequestApproved,
		Title:  "Approved",
		Body:   "Your request was approved",
		Read:   false,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = notifSvc.Create(ctx, core.Notification{
		UserID: ownerID,
		Type:   core.NotifyRequestDenied,
		Title:  "Denied",
		Body:   "Your request was denied",
		Read:   true,
	})
	if err != nil {
		t.Fatal(err)
	}

	rec := doGET(t, srv, "/api/v1/notifications", cookie.Value)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /notifications = %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Notifications []core.Notification `json:"notifications"`
		Unread        int                 `json:"unread"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Notifications) != 2 {
		t.Fatalf("got %d notifications, want 2", len(resp.Notifications))
	}
	if resp.Unread != 1 {
		t.Fatalf("unread = %d, want 1", resp.Unread)
	}
}

// TestListNotificationsDoesNotReturnOtherUsersData verifies that another user's
// notifications are NOT returned to the caller.
func TestListNotificationsDoesNotReturnOtherUsersData(t *testing.T) {
	_, srv, notifSvc, cookie, ownerID, authSvc := notifTestServer(t)
	ctx := context.Background()

	// Create a second user.
	otherID, err := authSvc.CreateUser(ctx, "other", "pw", "role-requester")
	if err != nil {
		t.Fatal(err)
	}

	// 1 notification for owner, 2 for other.
	_, err = notifSvc.Create(ctx, core.Notification{
		UserID: ownerID,
		Type:   core.NotifyRequestApproved,
		Title:  "For owner",
		Body:   "owner notification",
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		_, err = notifSvc.Create(ctx, core.Notification{
			UserID: otherID,
			Type:   core.NotifyRequestDenied,
			Title:  "For other",
			Body:   "other notification",
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	rec := doGET(t, srv, "/api/v1/notifications", cookie.Value)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /notifications = %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Notifications []core.Notification `json:"notifications"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Owner should only see their own 1 notification.
	if len(resp.Notifications) != 1 {
		t.Fatalf("got %d notifications, want 1 (only owner's)", len(resp.Notifications))
	}
	if resp.Notifications[0].UserID != ownerID {
		t.Fatalf("notification belongs to %q, want %q", resp.Notifications[0].UserID, ownerID)
	}
}

// TestMarkNotificationsReadById verifies that POST /notifications/read with
// specific IDs marks those notifications read and returns the new unread count.
func TestMarkNotificationsReadById(t *testing.T) {
	_, srv, notifSvc, cookie, ownerID, _ := notifTestServer(t)
	ctx := context.Background()

	// Seed 2 unread notifications.
	n1, err := notifSvc.Create(ctx, core.Notification{
		UserID: ownerID,
		Type:   core.NotifyRequestApproved,
		Title:  "N1",
		Body:   "body",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = notifSvc.Create(ctx, core.Notification{
		UserID: ownerID,
		Type:   core.NotifyRequestApproved,
		Title:  "N2",
		Body:   "body",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Mark only n1 as read.
	body := `{"ids":["` + n1.ID + `"]}`
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/notifications/read", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /notifications/read = %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Unread int `json:"unread"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// n2 still unread.
	if resp.Unread != 1 {
		t.Fatalf("unread after partial mark-read = %d, want 1", resp.Unread)
	}
}

// TestMarkAllNotificationsRead verifies that POST /notifications/read with an
// empty ids list marks ALL of the caller's notifications read.
func TestMarkAllNotificationsRead(t *testing.T) {
	_, srv, notifSvc, cookie, ownerID, _ := notifTestServer(t)
	ctx := context.Background()

	// Seed 3 unread notifications.
	for i := 0; i < 3; i++ {
		_, err := notifSvc.Create(ctx, core.Notification{
			UserID: ownerID,
			Type:   core.NotifyRequestApproved,
			Title:  "N",
			Body:   "body",
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	// POST with empty ids → MarkAllRead.
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/notifications/read", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /notifications/read = %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Unread int `json:"unread"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Unread != 0 {
		t.Fatalf("unread after mark-all-read = %d, want 0", resp.Unread)
	}
}

// TestMarkReadDoesNotAffectOtherUser verifies that user A's POST /notifications/read
// cannot mark user B's notifications as read (service is user-scoped).
func TestMarkReadDoesNotAffectOtherUser(t *testing.T) {
	_, srv, notifSvc, ownerCookie, _, authSvc := notifTestServer(t)
	ctx := context.Background()

	// Create a second user.
	otherID, err := authSvc.CreateUser(ctx, "other3", "pw", "role-requester")
	if err != nil {
		t.Fatal(err)
	}

	// Seed 1 unread notification for the other user.
	otherNotif, err := notifSvc.Create(ctx, core.Notification{
		UserID: otherID,
		Type:   core.NotifyRequestApproved,
		Title:  "For other",
		Body:   "body",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Owner tries to mark the other user's notification by ID. The service scopes
	// MarkRead to the caller's userID so this is a no-op for the other user's row.
	body := `{"ids":["` + otherNotif.ID + `"]}`
	rec := do(t, srv, ownerCookie, http.MethodPost, "/api/v1/notifications/read", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /notifications/read = %d: %s", rec.Code, rec.Body.String())
	}

	// The other user's notification should still be unread.
	unread, err := notifSvc.CountUnread(ctx, otherID)
	if err != nil {
		t.Fatal(err)
	}
	if unread != 1 {
		t.Fatalf("other user unread = %d, want 1 (owner's call must not affect other user)", unread)
	}
}

// TestListNotificationsRequiresAuth verifies that unauthenticated requests are rejected.
func TestListNotificationsRequiresAuth(t *testing.T) {
	_, srv, _, _, _, _ := notifTestServer(t)
	rec := doGET(t, srv, "/api/v1/notifications", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /notifications unauthenticated = %d, want 401", rec.Code)
	}
}

// TestNotificationsNilService verifies that 503 is returned when the notification
// service is not wired in.
func TestNotificationsNilService(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/notif-nil.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	authSvc, tok := seededAuthToken(t, st)
	srv := NewServer(Deps{
		Auth:       authSvc,
		Search:     registry.NewRegistry("search"),
		Downloader: registry.NewRegistry("downloader"),
		// Notifications intentionally nil.
	})
	cookie := &http.Cookie{Name: sessionCookie, Value: tok}

	rec := doGET(t, srv, "/api/v1/notifications", cookie.Value)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /notifications with nil service = %d, want 503", rec.Code)
	}

	rec2 := do(t, srv, cookie, http.MethodPost, "/api/v1/notifications/read", `{}`)
	if rec2.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST /notifications/read with nil service = %d, want 503", rec2.Code)
	}
}
