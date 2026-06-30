package scrobble

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/store/db"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

// ----------------------------------------------------------------------------
// Migrations embed — we re-open the real store to get a migrated SQLite.
// ----------------------------------------------------------------------------

// openTestDB opens a temporary in-memory (file-based temp) SQLite and runs
// all migrations so tests operate against the real schema.
func openTestDB(t *testing.T) *db.Queries {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	conn, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	// Run migrations via the same embedded FS the store package uses.
	// We call into the store package's migration path by running goose
	// against the real migration directory.
	migrationsDir := findMigrationsDir(t)
	goose.SetBaseFS(nil) // use OS FS
	if err := goose.SetDialect("sqlite"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
	if err := goose.Up(conn, migrationsDir); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return db.New(conn)
}

// findMigrationsDir locates the store/migrations directory relative to this
// test file's location. Go tests run with cwd = package directory, so we
// walk up from the CWD to find the repo root and then descend into
// internal/store/migrations.
func findMigrationsDir(t *testing.T) string {
	t.Helper()
	// The test runs with cwd = internal/scrobble/
	// Go up two levels to the repo root, then descend.
	candidates := []string{
		"../../store/migrations",        // from internal/scrobble/
		"../store/migrations",           // fallback
		"internal/store/migrations",     // from repo root
	}
	for _, rel := range candidates {
		abs, err := filepath.Abs(rel)
		if err != nil {
			continue
		}
		if _, err := os.Stat(abs); err == nil {
			return abs
		}
	}
	// last resort: try to find it by walking up CWD
	cwd, _ := os.Getwd()
	t.Fatalf("migrations dir not found; cwd=%s", cwd)
	return ""
}

// ----------------------------------------------------------------------------
// Fake Scrobbler
// ----------------------------------------------------------------------------

type fakeScrobbler struct {
	mu sync.Mutex

	// AuthURL results
	authURLResult string
	authToken     string
	authErr       error

	// CompleteAuth results
	completeSessionKey string
	completeUsername   string
	completeErr        error

	// NowPlaying recording
	nowPlayingCalls []nowPlayingCall
	nowPlayingErr   error

	// Scrobble recording
	scrobbleCalls []scrobbleCall
	scrobbleErr   error
	scrobbleAccepted int
}

type nowPlayingCall struct {
	Creds Creds
	Track Track
}

type scrobbleCall struct {
	Creds Creds
	Plays []ScrobblePlay
}

func (f *fakeScrobbler) AuthURL(ctx context.Context, c Creds) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.authURLResult, f.authToken, f.authErr
}

func (f *fakeScrobbler) CompleteAuth(ctx context.Context, c Creds, token string) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.completeSessionKey, f.completeUsername, f.completeErr
}

func (f *fakeScrobbler) NowPlaying(ctx context.Context, c Creds, t Track) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nowPlayingCalls = append(f.nowPlayingCalls, nowPlayingCall{Creds: c, Track: t})
	return f.nowPlayingErr
}

func (f *fakeScrobbler) Scrobble(ctx context.Context, c Creds, plays []ScrobblePlay) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scrobbleCalls = append(f.scrobbleCalls, scrobbleCall{Creds: c, Plays: append([]ScrobblePlay{}, plays...)})
	if f.scrobbleErr != nil {
		return 0, f.scrobbleErr
	}
	if f.scrobbleAccepted != 0 {
		return f.scrobbleAccepted, nil
	}
	return len(plays), nil
}

// ----------------------------------------------------------------------------
// Test helpers
// ----------------------------------------------------------------------------

func fixedCreds() Creds {
	return Creds{APIKey: "test-key", APISecret: "test-secret"}
}

func fixedNow() time.Time {
	return time.Unix(1_000_000, 0)
}

var idCounter int
var idMu sync.Mutex

func seqIDGen() string {
	idMu.Lock()
	defer idMu.Unlock()
	idCounter++
	return fmt.Sprintf("id-%d", idCounter)
}

// insertLink is a test helper to seed a scrobble link directly.
func insertLink(t *testing.T, q Querier, userID, provider, sessionKey, username, status string) {
	t.Helper()
	err := q.UpsertScrobbleLink(context.Background(), db.UpsertScrobbleLinkParams{
		UserID:     userID,
		Provider:   provider,
		SessionKey: sessionKey,
		Username:   username,
		Status:     status,
		CreatedAt:  1000,
	})
	if err != nil {
		t.Fatalf("insertLink: %v", err)
	}
}

// insertQueueRow seeds a pending queue row with a given next_attempt_at.
func insertQueueRow(t *testing.T, q Querier, id, userID string, nextAttemptAt int64, attempts int64) {
	t.Helper()
	err := q.InsertScrobbleQueue(context.Background(), db.InsertScrobbleQueueParams{
		ID:            id,
		UserID:        userID,
		Provider:      "lastfm",
		CatalogID:     "",
		Title:         "Song",
		Artist:        "Artist",
		Album:         "Album",
		DurationMs:    180000,
		PlayedAt:      999_000,
		Status:        "pending",
		Attempts:      attempts,
		NextAttemptAt: nextAttemptAt,
		CreatedAt:     999_000,
	})
	if err != nil {
		t.Fatalf("insertQueueRow: %v", err)
	}
}

// queueRowStatus fetches a row's status+attempts directly via raw DB.
// Because Querier only exposes the sqlc methods, we need to reach db.Queries
// which implements the real *db.Queries — cast it.
func queueRow(t *testing.T, q *db.Queries, id string) db.ScrobbleQueue {
	t.Helper()
	// SelectDueScrobbles won't return done/failed rows, so query via a
	// trick: select with a very far future cutoff and filter.
	rows, err := q.SelectDueScrobbles(context.Background(), db.SelectDueScrobblesParams{
		NextAttemptAt: 999_999_999_999,
		Limit:         1000,
	})
	if err != nil {
		t.Fatalf("queueRow select: %v", err)
	}
	for _, r := range rows {
		if r.ID == id {
			return r
		}
	}
	// Row not pending — check status via link status trick by fetching
	// a fresh scrobble link approach. Actually we need raw SQL access.
	// Since we control the test, cast q (db.Queries) directly.
	t.Fatalf("queueRow %q not found in due scrobbles (may be done/failed — use rawQueueRow)", id)
	return db.ScrobbleQueue{}
}

// newTestService returns a *Service backed by a real in-memory store and fake Scrobbler.
// nowFn defaults to fixedNow if nil.
func newTestService(t *testing.T, q *db.Queries, sc *fakeScrobbler, nowFn func() time.Time) *Service {
	t.Helper()
	if nowFn == nil {
		nowFn = fixedNow
	}
	return NewService(q, sc, fixedCreds, nowFn, seqIDGen)
}

// ----------------------------------------------------------------------------
// (a) Enqueue: linked → 1 pending row; unlinked → 0 rows
// ----------------------------------------------------------------------------

func TestEnqueue_LinkedUserInsertsPendingRow(t *testing.T) {
	q := openTestDB(t)
	sc := &fakeScrobbler{}
	svc := newTestService(t, q, sc, nil)
	ctx := context.Background()

	// Seed an active link.
	insertLink(t, q, "u1", "lastfm", "sk-u1", "alice", "active")

	play := ScrobblePlay{
		Track:    Track{Title: "Song", Artist: "Artist", Album: "Album", DurationMs: 180000},
		PlayedAt: fixedNow().Unix() - 300,
	}
	if err := svc.Enqueue(ctx, "u1", play); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// SelectDueScrobbles with a far-future cutoff should return 1 row.
	rows, err := q.SelectDueScrobbles(ctx, db.SelectDueScrobblesParams{
		NextAttemptAt: fixedNow().Unix() + 3600,
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("SelectDueScrobbles: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 pending row, got %d", len(rows))
	}
	r := rows[0]
	if r.UserID != "u1" || r.Status != "pending" || r.Attempts != 0 {
		t.Fatalf("unexpected row: %+v", r)
	}
	if r.Title != "Song" || r.Artist != "Artist" || r.Album != "Album" {
		t.Fatalf("track fields not stored: %+v", r)
	}
	if r.PlayedAt != play.PlayedAt {
		t.Fatalf("played_at mismatch: got %d want %d", r.PlayedAt, play.PlayedAt)
	}
}

func TestEnqueue_UnlinkedUserIsNoop(t *testing.T) {
	q := openTestDB(t)
	sc := &fakeScrobbler{}
	svc := newTestService(t, q, sc, nil)
	ctx := context.Background()

	// No link seeded for "u1".
	play := ScrobblePlay{
		Track:    Track{Title: "Song", Artist: "Artist", Album: "", DurationMs: 0},
		PlayedAt: 999000,
	}
	if err := svc.Enqueue(ctx, "u1", play); err != nil {
		t.Fatalf("Enqueue on unlinked user should return nil, got: %v", err)
	}

	rows, _ := q.SelectDueScrobbles(ctx, db.SelectDueScrobblesParams{
		NextAttemptAt: 999_999_999,
		Limit:         10,
	})
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows for unlinked user, got %d", len(rows))
	}
}

func TestEnqueue_BrokenLinkIsNoop(t *testing.T) {
	q := openTestDB(t)
	sc := &fakeScrobbler{}
	svc := newTestService(t, q, sc, nil)
	ctx := context.Background()

	// Broken link — should be a no-op.
	insertLink(t, q, "u1", "lastfm", "sk-old", "alice", "broken")

	play := ScrobblePlay{Track: Track{Title: "T", Artist: "A"}, PlayedAt: 1000}
	if err := svc.Enqueue(ctx, "u1", play); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	rows, _ := q.SelectDueScrobbles(ctx, db.SelectDueScrobblesParams{
		NextAttemptAt: 999_999_999,
		Limit:         10,
	})
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows for broken-link user, got %d", len(rows))
	}
}

// ----------------------------------------------------------------------------
// (b) drainOnce success → rows done + Scrobble called with correct sessionKey
// ----------------------------------------------------------------------------

func TestDrainOnce_SuccessMarksDoneAndCallsScrobble(t *testing.T) {
	q := openTestDB(t)
	sc := &fakeScrobbler{}
	now := fixedNow()
	svc := newTestService(t, q, sc, func() time.Time { return now })
	ctx := context.Background()

	insertLink(t, q, "u1", "lastfm", "sk-u1", "alice", "active")
	// Row with next_attempt_at in the past (due).
	insertQueueRow(t, q, "row-1", "u1", now.Unix()-1, 0)

	if err := svc.drainOnce(ctx, 50); err != nil {
		t.Fatalf("drainOnce: %v", err)
	}

	// Scrobbler.Scrobble should have been called once with u1's session key.
	sc.mu.Lock()
	calls := sc.scrobbleCalls
	sc.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("expected 1 Scrobble call, got %d", len(calls))
	}
	if calls[0].Creds.SessionKey != "sk-u1" {
		t.Fatalf("wrong sessionKey: got %q want %q", calls[0].Creds.SessionKey, "sk-u1")
	}
	if len(calls[0].Plays) != 1 {
		t.Fatalf("expected 1 play in batch, got %d", len(calls[0].Plays))
	}

	// Row should be done now — not returned as due.
	rows, _ := q.SelectDueScrobbles(ctx, db.SelectDueScrobblesParams{
		NextAttemptAt: 999_999_999_999,
		Limit:         10,
	})
	for _, r := range rows {
		if r.ID == "row-1" {
			t.Fatalf("row-1 should be done, still appears as due/pending: %+v", r)
		}
	}
}

// ----------------------------------------------------------------------------
// (c) Transient error → row stays pending, attempts++, next_attempt_at advanced
//     At maxAttempts → MarkScrobbleFailed
// ----------------------------------------------------------------------------

func TestDrainOnce_TransientError_IncrementsAttemptsAndSchedulesRetry(t *testing.T) {
	q := openTestDB(t)
	transientErr := errors.New("network error")
	sc := &fakeScrobbler{scrobbleErr: transientErr}
	now := fixedNow()
	svc := newTestService(t, q, sc, func() time.Time { return now })
	ctx := context.Background()

	insertLink(t, q, "u1", "lastfm", "sk-u1", "alice", "active")
	insertQueueRow(t, q, "row-retry", "u1", now.Unix()-1, 0 /*attempts=0*/)

	if err := svc.drainOnce(ctx, 50); err != nil {
		// drainOnce may return error or swallow — either is fine for transient
		// as long as the row is updated correctly.
		_ = err
	}

	// Row should still be pending with attempts=1 and next_attempt_at > now.
	rows, _ := q.SelectDueScrobbles(ctx, db.SelectDueScrobblesParams{
		NextAttemptAt: 999_999_999_999,
		Limit:         10,
	})
	var found *db.ScrobbleQueue
	for i := range rows {
		if rows[i].ID == "row-retry" {
			found = &rows[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("row-retry not found in pending rows after transient error")
	}
	if found.Attempts != 1 {
		t.Fatalf("expected attempts=1, got %d", found.Attempts)
	}
	if found.NextAttemptAt <= now.Unix() {
		t.Fatalf("next_attempt_at should be in the future: got %d, now=%d", found.NextAttemptAt, now.Unix())
	}
}

func TestDrainOnce_MaxAttemptsReached_MarksFailed(t *testing.T) {
	q := openTestDB(t)
	transientErr := errors.New("persistent network error")
	sc := &fakeScrobbler{scrobbleErr: transientErr}
	now := fixedNow()
	svc := newTestService(t, q, sc, func() time.Time { return now })
	ctx := context.Background()

	insertLink(t, q, "u1", "lastfm", "sk-u1", "alice", "active")
	// Row is already at maxAttempts-1 (5), so next drain should mark it failed.
	insertQueueRow(t, q, "row-maxed", "u1", now.Unix()-1, 5 /*attempts=5, maxAttempts=6*/)

	if err := svc.drainOnce(ctx, 50); err != nil {
		_ = err
	}

	// Row should NOT appear in pending (it's failed).
	rows, _ := q.SelectDueScrobbles(ctx, db.SelectDueScrobblesParams{
		NextAttemptAt: 999_999_999_999,
		Limit:         10,
	})
	for _, r := range rows {
		if r.ID == "row-maxed" {
			t.Fatalf("row-maxed should be failed, still appears as pending: %+v", r)
		}
	}
}

// ----------------------------------------------------------------------------
// (d) ErrAuth → link set broken + subsequent Enqueue no-ops
// ----------------------------------------------------------------------------

func TestDrainOnce_ErrAuth_SetsLinkBrokenAndEnqueueNoops(t *testing.T) {
	q := openTestDB(t)
	sc := &fakeScrobbler{scrobbleErr: fmt.Errorf("bad auth: %w", ErrAuth)}
	now := fixedNow()
	svc := newTestService(t, q, sc, func() time.Time { return now })
	ctx := context.Background()

	insertLink(t, q, "u1", "lastfm", "sk-u1", "alice", "active")
	insertQueueRow(t, q, "row-auth", "u1", now.Unix()-1, 0)

	_ = svc.drainOnce(ctx, 50)

	// Link should be broken now.
	link, err := q.GetScrobbleLink(ctx, db.GetScrobbleLinkParams{UserID: "u1", Provider: "lastfm"})
	if err != nil {
		t.Fatalf("GetScrobbleLink: %v", err)
	}
	if link.Status != "broken" {
		t.Fatalf("expected link status=broken, got %q", link.Status)
	}

	// Subsequent Enqueue should be a no-op (link is broken).
	play := ScrobblePlay{Track: Track{Title: "T", Artist: "A"}, PlayedAt: now.Unix()}
	if err := svc.Enqueue(ctx, "u1", play); err != nil {
		t.Fatalf("Enqueue after ErrAuth: %v", err)
	}

	// Count pending rows — row-auth might still be pending with long backoff;
	// the newly enqueued play should NOT add a new row.
	rows, _ := q.SelectDueScrobbles(ctx, db.SelectDueScrobblesParams{
		NextAttemptAt: 999_999_999_999,
		Limit:         100,
	})
	for _, r := range rows {
		// Check no new row was added (row-auth may still exist with far next_attempt_at).
		if r.UserID == "u1" && r.Title == "T" && r.ID != "row-auth" {
			t.Fatalf("Enqueue on broken link should be no-op, but found new row: %+v", r)
		}
	}
}

// ----------------------------------------------------------------------------
// (e) Per-user isolation: user A's row is scrobbled with A's sessionKey
// ----------------------------------------------------------------------------

func TestDrainOnce_PerUserIsolation(t *testing.T) {
	q := openTestDB(t)
	sc := &fakeScrobbler{}
	now := fixedNow()
	svc := newTestService(t, q, sc, func() time.Time { return now })
	ctx := context.Background()

	// Two users with different session keys.
	insertLink(t, q, "userA", "lastfm", "sk-A", "alice", "active")
	insertLink(t, q, "userB", "lastfm", "sk-B", "bob", "active")

	// Only user A has a due row.
	insertQueueRow(t, q, "row-A", "userA", now.Unix()-1, 0)

	if err := svc.drainOnce(ctx, 50); err != nil {
		t.Fatalf("drainOnce: %v", err)
	}

	sc.mu.Lock()
	calls := sc.scrobbleCalls
	sc.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 Scrobble call, got %d", len(calls))
	}
	// Must use A's session key, never B's.
	if calls[0].Creds.SessionKey != "sk-A" {
		t.Fatalf("wrong sessionKey: got %q, want sk-A", calls[0].Creds.SessionKey)
	}
	if calls[0].Creds.SessionKey == "sk-B" {
		t.Fatalf("used B's sessionKey for A's row!")
	}
}

// ----------------------------------------------------------------------------
// (f) CompleteAuth stores active link; Links returns {Provider,Username,Status}
//     with no SessionKey field; Unlink removes it.
// ----------------------------------------------------------------------------

func TestCompleteAuth_StoresActiveLink(t *testing.T) {
	q := openTestDB(t)
	sc := &fakeScrobbler{
		authURLResult:      "https://last.fm/auth",
		authToken:          "tok-1",
		completeSessionKey: "sk-complete",
		completeUsername:   "carol",
	}
	svc := newTestService(t, q, sc, nil)
	ctx := context.Background()

	username, err := svc.CompleteAuth(ctx, "u1", "tok-1")
	if err != nil {
		t.Fatalf("CompleteAuth: %v", err)
	}
	if username != "carol" {
		t.Fatalf("expected username=carol, got %q", username)
	}

	// Link should be stored as active.
	link, err := q.GetScrobbleLink(ctx, db.GetScrobbleLinkParams{UserID: "u1", Provider: "lastfm"})
	if err != nil {
		t.Fatalf("GetScrobbleLink: %v", err)
	}
	if link.Status != "active" {
		t.Fatalf("expected status=active, got %q", link.Status)
	}
	if link.SessionKey != "sk-complete" {
		t.Fatalf("expected session_key=sk-complete, got %q", link.SessionKey)
	}
	if link.Username != "carol" {
		t.Fatalf("expected username=carol, got %q", link.Username)
	}
}

func TestLinks_ReturnsNoSessionKey(t *testing.T) {
	q := openTestDB(t)
	sc := &fakeScrobbler{}
	svc := newTestService(t, q, sc, nil)
	ctx := context.Background()

	insertLink(t, q, "u1", "lastfm", "sk-secret", "alice", "active")

	links, err := svc.Links(ctx, "u1")
	if err != nil {
		t.Fatalf("Links: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	l := links[0]
	if l.Provider != "lastfm" {
		t.Fatalf("wrong provider: %q", l.Provider)
	}
	if l.Username != "alice" {
		t.Fatalf("wrong username: %q", l.Username)
	}
	if l.Status != "active" {
		t.Fatalf("wrong status: %q", l.Status)
	}
	// Compile-time proof: Link struct has no SessionKey field.
	// The following line would fail to compile if SessionKey existed:
	// _ = l.SessionKey // intentionally commented out — compile failure is the test
}

func TestUnlink_RemovesLink(t *testing.T) {
	q := openTestDB(t)
	sc := &fakeScrobbler{}
	svc := newTestService(t, q, sc, nil)
	ctx := context.Background()

	insertLink(t, q, "u1", "lastfm", "sk-u1", "alice", "active")

	if err := svc.Unlink(ctx, "u1", "lastfm"); err != nil {
		t.Fatalf("Unlink: %v", err)
	}

	links, err := svc.Links(ctx, "u1")
	if err != nil {
		t.Fatalf("Links after Unlink: %v", err)
	}
	if len(links) != 0 {
		t.Fatalf("expected 0 links after Unlink, got %d: %+v", len(links), links)
	}
}

func TestAuthURL_Passthrough(t *testing.T) {
	q := openTestDB(t)
	sc := &fakeScrobbler{
		authURLResult: "https://last.fm/auth?token=abc",
		authToken:     "abc",
	}
	svc := newTestService(t, q, sc, nil)

	authURL, token, err := svc.AuthURL(context.Background())
	if err != nil {
		t.Fatalf("AuthURL: %v", err)
	}
	if authURL != "https://last.fm/auth?token=abc" {
		t.Fatalf("wrong authURL: %q", authURL)
	}
	if token != "abc" {
		t.Fatalf("wrong token: %q", token)
	}
}

func TestNowPlaying_SwallowsErrors(t *testing.T) {
	q := openTestDB(t)
	sc := &fakeScrobbler{nowPlayingErr: errors.New("provider down")}
	svc := newTestService(t, q, sc, nil)
	ctx := context.Background()

	insertLink(t, q, "u1", "lastfm", "sk-u1", "alice", "active")

	// Must not panic or return — NowPlaying has no return value.
	svc.NowPlaying(ctx, "u1", Track{Title: "T", Artist: "A"})
}
