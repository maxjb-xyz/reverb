package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

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
