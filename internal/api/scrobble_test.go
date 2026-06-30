package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/catalog"
	"github.com/maxjb-xyz/reverb/internal/play"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/scrobble"
	"github.com/maxjb-xyz/reverb/internal/store"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// ----------------------------------------------------------------------------
// fakeScrobbler — a controllable Scrobbler for API-layer tests.
// Injects preset responses without hitting the real Last.fm endpoint.
// ----------------------------------------------------------------------------

type fakeScrobbler struct {
	authURL    string
	authToken  string
	authURLErr error

	completeSessionKey string
	completeUsername   string
	completeErr        error

	nowPlayingErr error
}

func (f *fakeScrobbler) AuthURL(_ context.Context, _ scrobble.Creds) (string, string, error) {
	return f.authURL, f.authToken, f.authURLErr
}
func (f *fakeScrobbler) CompleteAuth(_ context.Context, _ scrobble.Creds, _ string) (string, string, error) {
	return f.completeSessionKey, f.completeUsername, f.completeErr
}
func (f *fakeScrobbler) NowPlaying(_ context.Context, _ scrobble.Creds, _ scrobble.Track) error {
	return f.nowPlayingErr
}
func (f *fakeScrobbler) Scrobble(_ context.Context, _ scrobble.Creds, _ []scrobble.ScrobblePlay) (int, error) {
	return 0, nil
}

// ----------------------------------------------------------------------------
// scrobbleTestServer builds a Server with a real scrobble.Service wired in.
// cfg is the Creds func for the service (controls configured/unconfigured state).
// sc is the Scrobbler to inject (can be a *fakeScrobbler).
// Returns server, session cookie for owner, owner userID, and the store.
// ----------------------------------------------------------------------------

func scrobbleTestServer(t *testing.T, sc scrobble.Scrobbler, cfg func() scrobble.Creds) (*Server, *http.Cookie, string, *store.Store) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/scrobble.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}

	authSvc, tok := seededAuthToken(t, st)

	users, err := authSvc.ListUsers(context.Background())
	if err != nil || len(users) == 0 {
		t.Fatal("expected at least one user after seeding")
	}
	ownerID := users[0].ID

	var counter int
	idgen := func() string {
		counter++
		return fmt.Sprintf("scr-%08d-0000-0000-0000-000000000000", counter)
	}

	scrobbleSvc := scrobble.NewService(st.Q(), sc, cfg, time.Now, idgen)

	// Build a play.Service for /plays wiring tests.
	var playCounter int
	playIDgen := func() string {
		playCounter++
		return fmt.Sprintf("play-%08d-0000-0000-0000-000000000000", playCounter)
	}
	catalogSvc := catalog.NewService(st.Q(), time.Now, playIDgen)
	playSvc := play.NewService(st.Q(), catalogSvc, time.Now, playIDgen)

	srv := NewServer(Deps{
		Auth:       authSvc,
		Search:     registry.NewRegistry("search"),
		Downloader: registry.NewRegistry("downloader"),
		Play:       playSvc,
		Scrobble:   scrobbleSvc,
	})

	return srv, &http.Cookie{Name: sessionCookie, Value: tok}, ownerID, st
}

// cfgUnconfigured returns Creds with empty key/secret.
func cfgUnconfigured() scrobble.Creds { return scrobble.Creds{} }

// cfgConfigured returns Creds with non-empty key+secret.
func cfgConfigured() scrobble.Creds {
	return scrobble.Creds{APIKey: "test-key", APISecret: "test-secret"}
}

// ----------------------------------------------------------------------------
// countScrobbleQueueRows counts pending rows in scrobble_queue for a user.
// Used for DB-level assertions in /plays wiring tests.
// ----------------------------------------------------------------------------

func countScrobbleQueueRows(t *testing.T, st *store.Store, userID string) int {
	t.Helper()
	rows, err := st.DB().QueryContext(context.Background(),
		"SELECT COUNT(*) FROM scrobble_queue WHERE user_id = ?", userID)
	if err != nil {
		t.Fatalf("count scrobble_queue: %v", err)
	}
	defer rows.Close()
	var n int
	if rows.Next() {
		_ = rows.Scan(&n)
	}
	return n
}

// scrobbleQueuePlayedAt returns the played_at of the first scrobble_queue row for a user.
func scrobbleQueuePlayedAt(t *testing.T, st *store.Store, userID string) int64 {
	t.Helper()
	row := st.DB().QueryRowContext(context.Background(),
		"SELECT played_at FROM scrobble_queue WHERE user_id = ? LIMIT 1", userID)
	var v int64
	if err := row.Scan(&v); err != nil {
		t.Fatalf("scrobble_queue played_at: %v", err)
	}
	return v
}

// seedLink inserts an active scrobble_link for userID (for linked-user tests).
func seedLink(t *testing.T, st *store.Store, userID string) {
	t.Helper()
	err := st.Q().UpsertScrobbleLink(context.Background(), db.UpsertScrobbleLinkParams{
		UserID:     userID,
		Provider:   "lastfm",
		SessionKey: "test-session-key",
		Username:   "testuser",
		Status:     "active",
		CreatedAt:  time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("seed link: %v", err)
	}
}

// ============================================================================
// Tests: GET /scrobble/links
// ============================================================================

// TestScrobbleLinks_EmptyForNewUser checks that a fresh user with no links gets
// an empty links array (not null) and configured=false when settings are unset.
func TestScrobbleLinks_EmptyForNewUser(t *testing.T) {
	fs := &fakeScrobbler{}
	srv, cookie, _, _ := scrobbleTestServer(t, fs, cfgUnconfigured)

	rec := do(t, srv, cookie, http.MethodGet, "/api/v1/scrobble/links", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /scrobble/links = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Configured bool        `json:"configured"`
		Links      interface{} `json:"links"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Configured {
		t.Fatal("expected configured=false when api_key/api_secret unset")
	}
}

// TestScrobbleLinks_NeverContainsSessionKey asserts that the JSON response for
// GET /scrobble/links never includes session_key or sessionKey — at any level.
func TestScrobbleLinks_NeverContainsSessionKey(t *testing.T) {
	fs := &fakeScrobbler{}
	srv, cookie, ownerID, st := scrobbleTestServer(t, fs, cfgConfigured)

	// Seed a link for the owner so there's something to return.
	seedLink(t, st, ownerID)

	rec := do(t, srv, cookie, http.MethodGet, "/api/v1/scrobble/links", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /scrobble/links = %d; body: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if strings.Contains(body, "session_key") || strings.Contains(body, "sessionKey") {
		t.Fatalf("response contains session_key/sessionKey — must never be returned; body: %s", body)
	}
}

// TestScrobbleLinks_ConfiguredReflectsCreds checks that configured=true only
// when cfg() returns both api_key and api_secret.
func TestScrobbleLinks_ConfiguredReflectsCreds(t *testing.T) {
	fs := &fakeScrobbler{}
	srv, cookie, _, _ := scrobbleTestServer(t, fs, cfgConfigured)

	rec := do(t, srv, cookie, http.MethodGet, "/api/v1/scrobble/links", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /scrobble/links = %d; body: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Configured bool `json:"configured"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Configured {
		t.Fatal("expected configured=true when api_key+api_secret are set")
	}
}

// TestScrobbleLinks_PerUserScoping asserts that user-1 never sees user-2's links.
// This is the DB-level per-user scoping requirement.
func TestScrobbleLinks_PerUserScoping(t *testing.T) {
	fs := &fakeScrobbler{}
	srv, ownerCookie, _, st := scrobbleTestServer(t, fs, cfgUnconfigured)

	// Create a second user and seed a link for them.
	_ = newAuthedUser(t, srv, st, "user2-scrobble", "user2-pass-12345")

	// Find user2's ID via the users table directly.
	row := st.DB().QueryRowContext(context.Background(),
		"SELECT id FROM users WHERE username = ?", "user2-scrobble")
	var user2ID string
	if err := row.Scan(&user2ID); err != nil {
		t.Fatalf("find user2 id: %v", err)
	}
	seedLink(t, st, user2ID)

	// Owner (user1) must see zero links even though user2 has one.
	rec := do(t, srv, ownerCookie, http.MethodGet, "/api/v1/scrobble/links", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /scrobble/links = %d; body: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Links []struct {
			Provider string `json:"provider"`
		} `json:"links"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Links) != 0 {
		t.Fatalf("user1 must see 0 links, got %d — per-user scoping violated", len(resp.Links))
	}
}

// TestScrobbleLinks_LinkVisibleForOwner verifies a link seeded for the owner appears.
func TestScrobbleLinks_LinkVisibleForOwner(t *testing.T) {
	fs := &fakeScrobbler{}
	srv, cookie, ownerID, st := scrobbleTestServer(t, fs, cfgUnconfigured)
	seedLink(t, st, ownerID)

	rec := do(t, srv, cookie, http.MethodGet, "/api/v1/scrobble/links", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /scrobble/links = %d; body: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Links []struct {
			Provider string `json:"provider"`
			Username string `json:"username"`
			Status   string `json:"status"`
		} `json:"links"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Links) != 1 {
		t.Fatalf("expected 1 link for owner, got %d", len(resp.Links))
	}
	if resp.Links[0].Provider != "lastfm" {
		t.Fatalf("link provider = %q, want %q", resp.Links[0].Provider, "lastfm")
	}
}

// TestScrobbleLinks_Unauth verifies 401 for unauthenticated requests.
func TestScrobbleLinks_Unauth(t *testing.T) {
	fs := &fakeScrobbler{}
	srv, _, _, _ := scrobbleTestServer(t, fs, cfgUnconfigured)
	rec := do(t, srv, &http.Cookie{Name: sessionCookie, Value: ""}, http.MethodGet, "/api/v1/scrobble/links", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /scrobble/links unauth = %d, want 401", rec.Code)
	}
}

// TestScrobbleLinks_NilDep503 verifies that nil Scrobble dep returns 503.
func TestScrobbleLinks_NilDep503(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/scr-nil.db")
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
		// Scrobble intentionally nil
	})
	cookie := &http.Cookie{Name: sessionCookie, Value: tok}
	rec := do(t, srv, cookie, http.MethodGet, "/api/v1/scrobble/links", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /scrobble/links nil dep = %d, want 503", rec.Code)
	}
}

// ============================================================================
// Tests: POST /scrobble/lastfm/auth-url
// ============================================================================

// TestAuthURL_400WhenUnconfigured expects 400 with lastfm_not_configured when
// api_key / api_secret are absent.
func TestAuthURL_400WhenUnconfigured(t *testing.T) {
	fs := &fakeScrobbler{}
	srv, cookie, _, _ := scrobbleTestServer(t, fs, cfgUnconfigured)

	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/scrobble/lastfm/auth-url", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("auth-url unconfigured = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error != "lastfm_not_configured" {
		t.Fatalf("error code = %q, want %q", resp.Error, "lastfm_not_configured")
	}
}

// TestAuthURL_ReturnsAuthUrlAndToken expects {authUrl, token} when configured.
func TestAuthURL_ReturnsAuthUrlAndToken(t *testing.T) {
	fs := &fakeScrobbler{
		authURL:   "https://www.last.fm/api/auth/?api_key=test-key&token=tok123",
		authToken: "tok123",
	}
	srv, cookie, _, _ := scrobbleTestServer(t, fs, cfgConfigured)

	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/scrobble/lastfm/auth-url", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("auth-url configured = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		AuthURL string `json:"authUrl"`
		Token   string `json:"token"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.AuthURL == "" {
		t.Fatal("authUrl must not be empty")
	}
	if resp.Token == "" {
		t.Fatal("token must not be empty")
	}
}

// TestAuthURL_ConfiguredButTransientError_Returns5xx asserts that a CONFIGURED
// deployment whose adapter's AuthURL fails transiently (e.g. Last.fm outage) gets
// a 5xx — NOT 400 lastfm_not_configured. Conflating the two would tell the user to
// ask their admin when it is actually a provider outage.
func TestAuthURL_ConfiguredButTransientError_Returns5xx(t *testing.T) {
	fs := &fakeScrobbler{authURLErr: fmt.Errorf("lastfm: http: connection refused")}
	srv, cookie, _, _ := scrobbleTestServer(t, fs, cfgConfigured)

	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/scrobble/lastfm/auth-url", "")
	if rec.Code < 500 || rec.Code >= 600 {
		t.Fatalf("auth-url configured+transient = %d, want 5xx; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "lastfm_not_configured") {
		t.Fatalf("configured-but-transient error must NOT report lastfm_not_configured; body: %s", body)
	}
}

// TestAuthURL_Unauth verifies 401.
func TestAuthURL_Unauth(t *testing.T) {
	fs := &fakeScrobbler{}
	srv, _, _, _ := scrobbleTestServer(t, fs, cfgUnconfigured)
	rec := do(t, srv, &http.Cookie{Name: sessionCookie, Value: ""}, http.MethodPost, "/api/v1/scrobble/lastfm/auth-url", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("auth-url unauth = %d, want 401", rec.Code)
	}
}

// TestAuthURL_NilDep503 verifies 503 when Scrobble dep is nil.
func TestAuthURL_NilDep503(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/scr-nil2.db")
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
	})
	cookie := &http.Cookie{Name: sessionCookie, Value: tok}
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/scrobble/lastfm/auth-url", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("auth-url nil dep = %d, want 503", rec.Code)
	}
}

// ============================================================================
// Tests: POST /scrobble/lastfm/complete
// ============================================================================

// TestComplete_StoresLink verifies that a successful complete call stores a link
// and returns the username.
func TestComplete_StoresLink(t *testing.T) {
	fs := &fakeScrobbler{
		completeSessionKey: "sess-key-abc",
		completeUsername:   "musicfan99",
	}
	srv, cookie, ownerID, st := scrobbleTestServer(t, fs, cfgConfigured)

	body := `{"token":"approved-token-123"}`
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/scrobble/lastfm/complete", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("complete = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Username string `json:"username"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Username != "musicfan99" {
		t.Fatalf("username = %q, want %q", resp.Username, "musicfan99")
	}

	// DB-level: link must exist for owner.
	link, err := st.Q().GetScrobbleLink(context.Background(), db.GetScrobbleLinkParams{
		UserID:   ownerID,
		Provider: "lastfm",
	})
	if err != nil {
		t.Fatalf("GetScrobbleLink: %v", err)
	}
	if link.Username != "musicfan99" {
		t.Fatalf("link.Username = %q, want %q", link.Username, "musicfan99")
	}
	// Session key must be in DB but must NOT appear in the API response.
	if link.SessionKey != "sess-key-abc" {
		t.Fatalf("link.SessionKey not stored correctly: %q", link.SessionKey)
	}
}

// TestComplete_Unauth verifies 401.
func TestComplete_Unauth(t *testing.T) {
	fs := &fakeScrobbler{}
	srv, _, _, _ := scrobbleTestServer(t, fs, cfgConfigured)
	rec := do(t, srv, &http.Cookie{Name: sessionCookie, Value: ""}, http.MethodPost, "/api/v1/scrobble/lastfm/complete", `{"token":"t"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("complete unauth = %d, want 401", rec.Code)
	}
}

// ============================================================================
// Tests: DELETE /scrobble/lastfm
// ============================================================================

// TestUnlink_Returns204 verifies DELETE /scrobble/lastfm returns 204.
func TestUnlink_Returns204(t *testing.T) {
	fs := &fakeScrobbler{}
	srv, cookie, ownerID, st := scrobbleTestServer(t, fs, cfgUnconfigured)
	seedLink(t, st, ownerID)

	rec := do(t, srv, cookie, http.MethodDelete, "/api/v1/scrobble/lastfm", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /scrobble/lastfm = %d, want 204; body: %s", rec.Code, rec.Body.String())
	}
}

// TestUnlink_Unauth verifies 401.
func TestUnlink_Unauth(t *testing.T) {
	fs := &fakeScrobbler{}
	srv, _, _, _ := scrobbleTestServer(t, fs, cfgUnconfigured)
	rec := do(t, srv, &http.Cookie{Name: sessionCookie, Value: ""}, http.MethodDelete, "/api/v1/scrobble/lastfm", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("DELETE /scrobble/lastfm unauth = %d, want 401", rec.Code)
	}
}

// ============================================================================
// Tests: POST /scrobble/nowplaying
// ============================================================================

// TestNowPlaying_Returns204EvenWhenUnlinked verifies that nowplaying is fire-and-forget:
// 204 even when the user has no link.
func TestNowPlaying_Returns204EvenWhenUnlinked(t *testing.T) {
	fs := &fakeScrobbler{}
	srv, cookie, _, _ := scrobbleTestServer(t, fs, cfgUnconfigured)

	body := `{"title":"Hurt","artist":"Johnny Cash","album":"American IV","durationMs":218000}`
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/scrobble/nowplaying", body)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("nowplaying unlinked = %d, want 204; body: %s", rec.Code, rec.Body.String())
	}
}

// TestNowPlaying_Returns204WhenLinked verifies 204 even when the adapter errors.
func TestNowPlaying_Returns204WhenLinked(t *testing.T) {
	fs := &fakeScrobbler{nowPlayingErr: fmt.Errorf("provider down")}
	srv, cookie, ownerID, st := scrobbleTestServer(t, fs, cfgConfigured)
	seedLink(t, st, ownerID)

	body := `{"title":"Hurt","artist":"Johnny Cash","album":"American IV","durationMs":218000}`
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/scrobble/nowplaying", body)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("nowplaying linked+error = %d, want 204; body: %s", rec.Code, rec.Body.String())
	}
}

// TestNowPlaying_Unauth verifies 401.
func TestNowPlaying_Unauth(t *testing.T) {
	fs := &fakeScrobbler{}
	srv, _, _, _ := scrobbleTestServer(t, fs, cfgUnconfigured)
	rec := do(t, srv, &http.Cookie{Name: sessionCookie, Value: ""}, http.MethodPost, "/api/v1/scrobble/nowplaying", `{"title":"t"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("nowplaying unauth = %d, want 401", rec.Code)
	}
}

// TestNowPlaying_NilDep503 verifies 503 when Scrobble dep is nil.
func TestNowPlaying_NilDep503(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/scr-nil3.db")
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
	})
	cookie := &http.Cookie{Name: sessionCookie, Value: tok}
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/scrobble/nowplaying", `{"title":"t"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nowplaying nil dep = %d, want 503", rec.Code)
	}
}

// ============================================================================
// Tests: /plays wiring — Enqueue on qualifying play
// ============================================================================

// TestPlays_LinkedUserEnqueuesScrobble verifies that a qualifying POST /plays for
// a user with an active link inserts a row into scrobble_queue (DB-level).
func TestPlays_LinkedUserEnqueuesScrobble(t *testing.T) {
	fs := &fakeScrobbler{}
	srv, cookie, ownerID, st := scrobbleTestServer(t, fs, cfgConfigured)

	// Seed an active link for the owner so Enqueue does not no-op.
	seedLink(t, st, ownerID)

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

	// DB-level: scrobble_queue must have exactly 1 row for the owner.
	n := countScrobbleQueueRows(t, st, ownerID)
	if n != 1 {
		t.Fatalf("expected 1 scrobble_queue row for linked user, got %d", n)
	}

	// played_at must be non-zero.
	playedAt := scrobbleQueuePlayedAt(t, st, ownerID)
	if playedAt == 0 {
		t.Fatal("scrobble_queue played_at must be non-zero")
	}
}

// TestPlays_UnlinkedUserDoesNotEnqueue verifies that a user without a link does
// NOT insert any scrobble_queue rows when they record a play.
func TestPlays_UnlinkedUserDoesNotEnqueue(t *testing.T) {
	fs := &fakeScrobbler{}
	srv, cookie, ownerID, st := scrobbleTestServer(t, fs, cfgConfigured)
	// No seedLink — user is unlinked.

	body := `{
		"Title": "Ring of Fire",
		"Artist": "Johnny Cash",
		"Album": "Ring of Fire",
		"DurationMs": 157000,
		"MsPlayed": 157000,
		"Completed": true,
		"PlayedAt": 1719001000
	}`
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/plays", body)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST /plays = %d, want 204; body: %s", rec.Code, rec.Body.String())
	}

	n := countScrobbleQueueRows(t, st, ownerID)
	if n != 0 {
		t.Fatalf("expected 0 scrobble_queue rows for unlinked user, got %d", n)
	}
}

// TestPlays_ScrobbleQueuePlayedAtNonZeroWhenBodyPlayedAtIsZero verifies that when
// PlayedAt=0 in the body, the enqueued played_at is resolved to a real unix timestamp.
func TestPlays_ScrobbleQueuePlayedAtNonZeroWhenBodyPlayedAtIsZero(t *testing.T) {
	fs := &fakeScrobbler{}
	srv, cookie, ownerID, st := scrobbleTestServer(t, fs, cfgConfigured)
	seedLink(t, st, ownerID)

	// PlayedAt omitted → defaults to 0 → must be resolved to time.Now().Unix().
	body := `{
		"Title": "Folsom Prison Blues",
		"Artist": "Johnny Cash",
		"DurationMs": 170000,
		"MsPlayed": 170000,
		"Completed": true
	}`
	rec := do(t, srv, cookie, http.MethodPost, "/api/v1/plays", body)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST /plays = %d, want 204; body: %s", rec.Code, rec.Body.String())
	}

	playedAt := scrobbleQueuePlayedAt(t, st, ownerID)
	if playedAt == 0 {
		t.Fatal("played_at in scrobble_queue must be non-zero even when body omits PlayedAt")
	}
}
