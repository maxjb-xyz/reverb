package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/auth"
	"github.com/maxjb-xyz/reverb/internal/catalog"
	"github.com/maxjb-xyz/reverb/internal/play"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// playsTestServer builds a Server with a real play.Service wired in.
// Returns: the Server, a session cookie for the owner, the owner's user ID, and
// the store handle so tests can query recorded plays directly.
func playsTestServer(t *testing.T) (*Server, *http.Cookie, string, *store.Store) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/plays.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}

	authSvc, tok := seededAuthToken(t, st)

	// Fetch the owner's user ID from the seeded store.
	users, err := authSvc.ListUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(users) == 0 {
		t.Fatal("expected at least one user (owner)")
	}
	ownerID := users[0].ID

	// Build a real play.Service backed by the same DB.
	var counter int
	idgen := func() string {
		counter++
		return fmt.Sprintf("%08d-0000-0000-0000-000000000000", counter)
	}
	q := st.Q()
	catalogSvc := catalog.NewService(q, time.Now, idgen)
	playSvc := play.NewService(q, catalogSvc, time.Now, idgen)

	srv := NewServer(Deps{
		Auth:       authSvc,
		Search:     registry.NewRegistry("search"),
		Downloader: registry.NewRegistry("downloader"),
		Play:       playSvc,
	})
	return srv, &http.Cookie{Name: sessionCookie, Value: tok}, ownerID, st
}

// TestHandlePlay_RecordsForSessionUser verifies that an authenticated POST /plays
// with a valid body returns 204 and records the play scoped to the session user.
func TestHandlePlay_RecordsForSessionUser(t *testing.T) {
	srv, cookie, ownerID, st := playsTestServer(t)
	ctx := context.Background()

	body := `{
		"LibraryTrackID": "lib-track-1",
		"Title": "Hurt",
		"Artist": "Johnny Cash",
		"Album": "American IV",
		"ISRC": "USRC10601234",
		"DurationMs": 218000,
		"MsPlayed": 218000,
		"Completed": true,
		"PlayedAt": 1719000000
	}`

	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/plays", body)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST /plays = %d, want 204; body: %s", rec.Code, rec.Body.String())
	}

	// Assert DB-level user attribution: exactly one play was recorded, and it is
	// scoped to the SESSION user (ownerID). A regression that misroutes the user
	// would leave ownerID with zero plays and fail here.
	rows, err := st.Q().ListRecentPlays(ctx, db.ListRecentPlaysParams{
		UserID:   ownerID,
		PlayedAt: 9999999999,
		Limit:    10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected exactly 1 play for session user %q, got %d: %+v", ownerID, len(rows), rows)
	}
	if rows[0].Title != "Hurt" {
		t.Fatalf("recorded play title = %q, want %q", rows[0].Title, "Hurt")
	}
}

// TestHandlePlay_IgnoresBodyUserID verifies that the recorded userID is always
// the session user's — a body that attempts to set another user's ID is ignored.
// PlayInput has no UserID field, so this is structurally guaranteed: whatever
// the body supplies, the handler extracts userID from currentUser(r).ID.
func TestHandlePlay_IgnoresBodyUserID(t *testing.T) {
	srv, cookie, ownerID, st := playsTestServer(t)
	ctx := context.Background()

	const attackerID = "attacker-user-999"

	// Body includes a stray "UserID" field — it should be silently ignored since
	// play.PlayInput has no such field; the handler reads userID from the session.
	body := `{
		"UserID": "` + attackerID + `",
		"Title": "Ring of Fire",
		"Artist": "Johnny Cash",
		"Album": "Ring of Fire",
		"DurationMs": 157000,
		"MsPlayed": 157000,
		"Completed": true,
		"PlayedAt": 1719001000
	}`

	// Should succeed with 204 — stray fields are silently ignored by json.Decode.
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/plays", body)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST /plays with stray UserID = %d, want 204; body: %s", rec.Code, rec.Body.String())
	}

	// The play must be recorded under the SESSION user, not the body-supplied id.
	ownerRows, err := st.Q().ListRecentPlays(ctx, db.ListRecentPlaysParams{
		UserID:   ownerID,
		PlayedAt: 9999999999,
		Limit:    10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ownerRows) != 1 {
		t.Fatalf("expected exactly 1 play for session user %q, got %d: %+v", ownerID, len(ownerRows), ownerRows)
	}
	if ownerRows[0].Title != "Ring of Fire" {
		t.Fatalf("recorded play title = %q, want %q", ownerRows[0].Title, "Ring of Fire")
	}

	// The body-supplied user id must own ZERO plays. If a UserID field were ever
	// added to PlayInput and wired through, this assertion would catch it.
	attackerRows, err := st.Q().ListRecentPlays(ctx, db.ListRecentPlaysParams{
		UserID:   attackerID,
		PlayedAt: 9999999999,
		Limit:    10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(attackerRows) != 0 {
		t.Fatalf("expected 0 plays for body-supplied user %q, got %d: %+v", attackerID, len(attackerRows), attackerRows)
	}
}

// newAuthedUser creates a second, distinct user in the shared store and returns
// a session cookie for them. Used to exercise cross-user privacy boundaries.
func newAuthedUser(t *testing.T, srv *Server, st *store.Store, username, password string) *http.Cookie {
	t.Helper()
	ctx := context.Background()
	authSvc := auth.NewService(st.Q(), time.Now)
	uid, err := authSvc.CreateUser(ctx, username, password, "role-requester")
	if err != nil {
		t.Fatal(err)
	}
	tok, err := authSvc.CreateSession(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: sessionCookie, Value: tok}
}

// recordAndGetPlayID records a play for the owner via POST /plays, then reads
// its id back via ListRecentPlays. Returns the play id.
func recordAndGetPlayID(t *testing.T, srv *Server, cookie *http.Cookie, st *store.Store, ownerID string) string {
	t.Helper()
	body := `{
		"Title": "Hurt",
		"Artist": "Johnny Cash",
		"Album": "American IV",
		"DurationMs": 218000,
		"MsPlayed": 218000,
		"Completed": true,
		"PlayedAt": 1719000000
	}`
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/plays", body)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST /plays = %d, want 204; body: %s", rec.Code, rec.Body.String())
	}
	rows, err := st.Q().ListRecentPlays(context.Background(), db.ListRecentPlaysParams{
		UserID:   ownerID,
		PlayedAt: 9999999999,
		Limit:    10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected exactly 1 play for owner, got %d", len(rows))
	}
	return rows[0].ID
}

// countPlays returns how many plays userID owns.
func countPlays(t *testing.T, st *store.Store, userID string) int {
	t.Helper()
	rows, err := st.Q().ListRecentPlays(context.Background(), db.ListRecentPlaysParams{
		UserID:   userID,
		PlayedAt: 9999999999,
		Limit:    10,
	})
	if err != nil {
		t.Fatal(err)
	}
	return len(rows)
}

// TestHandleDeletePlay_OwnerDeletes verifies that the owner can DELETE their own
// play: the response is 204 and the play is gone.
func TestHandleDeletePlay_OwnerDeletes(t *testing.T) {
	srv, cookie, ownerID, st := playsTestServer(t)
	playID := recordAndGetPlayID(t, srv, cookie, st, ownerID)

	rec := do(t, srv, cookie, http.MethodDelete, "/api/v1/plays/"+playID, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /plays/%s = %d, want 204; body: %s", playID, rec.Code, rec.Body.String())
	}

	if n := countPlays(t, st, ownerID); n != 0 {
		t.Fatalf("expected 0 plays after owner delete, got %d", n)
	}
}

// TestHandleDeletePlay_CrossUserNoOp is the load-bearing privacy assertion at the
// HTTP layer: a DIFFERENT authed user calling DELETE /plays/{ownerPlayId} gets
// 204 (idempotent, no existence leak) BUT the owner's play STILL EXISTS — the
// cross-user delete was a no-op.
func TestHandleDeletePlay_CrossUserNoOp(t *testing.T) {
	srv, ownerCookie, ownerID, st := playsTestServer(t)
	playID := recordAndGetPlayID(t, srv, ownerCookie, st, ownerID)

	// Create a second, different authenticated user and log them in.
	attackerCookie := newAuthedUser(t, srv, st, "attacker", "attacker-pass-12345")

	rec := do(t, srv, attackerCookie, http.MethodDelete, "/api/v1/plays/"+playID, "")
	// Idempotent / no existence leak: the attacker gets 204, not 403/404.
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /plays/%s as attacker = %d, want 204; body: %s", playID, rec.Code, rec.Body.String())
	}

	// The owner's play MUST still exist — the cross-user delete was a no-op.
	if n := countPlays(t, st, ownerID); n != 1 {
		t.Fatalf("cross-user delete leaked: expected owner to still have 1 play, got %d", n)
	}
}

// TestHandleDeletePlay_UnauthenticatedReturns401 verifies that the route is
// guarded by requireAuth and returns 401 for unauthenticated requests.
func TestHandleDeletePlay_UnauthenticatedReturns401(t *testing.T) {
	srv, _, _, _ := playsTestServer(t)
	rec := do(t, srv, &http.Cookie{Name: sessionCookie, Value: ""}, http.MethodDelete, "/api/v1/plays/some-id", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("DELETE /plays/{id} unauthenticated = %d, want 401", rec.Code)
	}
}

// TestHandleDeletePlay_NilServiceReturns503 verifies that when s.deps.Play is nil
// the handler returns 503 Service Unavailable.
func TestHandleDeletePlay_NilServiceReturns503(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/plays-del-nil.db")
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
		// Play intentionally nil.
	})
	cookie := &http.Cookie{Name: sessionCookie, Value: tok}
	rec := do(t, srv, cookie, http.MethodDelete, "/api/v1/plays/some-id", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("DELETE /plays/{id} with nil Play = %d, want 503", rec.Code)
	}
}

// TestHandlePlay_UnauthenticatedReturns401 verifies that the route is guarded by
// requireAuth and returns 401 for unauthenticated requests.
func TestHandlePlay_UnauthenticatedReturns401(t *testing.T) {
	srv, _, _, _ := playsTestServer(t)

	body := `{"Title":"Hurt","Artist":"Johnny Cash","DurationMs":218000,"MsPlayed":218000}`
	rec := do(t, srv, &http.Cookie{Name: sessionCookie, Value: ""}, http.MethodPost, "/api/v1/plays", body)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST /plays unauthenticated = %d, want 401", rec.Code)
	}
}

// TestHandlePlay_NilServiceReturns503 verifies that when s.deps.Play is nil the
// handler returns 503 Service Unavailable.
func TestHandlePlay_NilServiceReturns503(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/plays-nil.db")
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
		// Play intentionally nil.
	})
	cookie := &http.Cookie{Name: sessionCookie, Value: tok}

	body := `{"Title":"Hurt","Artist":"Johnny Cash","DurationMs":218000,"MsPlayed":218000}`
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/plays", body)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST /plays with nil Play = %d, want 503", rec.Code)
	}
}
