package store

import (
	"context"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/store/db"
)

func TestScrobbleLink_UpsertAndGet(t *testing.T) {
	st := openMigrated(t)
	ctx := context.Background()
	q := st.Q()

	err := q.UpsertScrobbleLink(ctx, db.UpsertScrobbleLinkParams{
		UserID:     "u1",
		Provider:   "lastfm",
		SessionKey: "sk-abc",
		Username:   "alice",
		Status:     "active",
		CreatedAt:  1000,
	})
	if err != nil {
		t.Fatalf("UpsertScrobbleLink: %v", err)
	}

	got, err := q.GetScrobbleLink(ctx, db.GetScrobbleLinkParams{
		UserID:   "u1",
		Provider: "lastfm",
	})
	if err != nil {
		t.Fatalf("GetScrobbleLink: %v", err)
	}
	if got.SessionKey != "sk-abc" || got.Username != "alice" || got.Status != "active" {
		t.Fatalf("GetScrobbleLink mismatch: %+v", got)
	}
}

func TestScrobbleLink_ListIsPerUser(t *testing.T) {
	st := openMigrated(t)
	ctx := context.Background()
	q := st.Q()

	// Insert a link for user A
	if err := q.UpsertScrobbleLink(ctx, db.UpsertScrobbleLinkParams{
		UserID:     "userA",
		Provider:   "lastfm",
		SessionKey: "sk-A",
		Username:   "alice",
		Status:     "active",
		CreatedAt:  1000,
	}); err != nil {
		t.Fatalf("upsert A: %v", err)
	}

	// ListScrobbleLinks for user B must return empty (non-vacuous isolation check)
	links, err := q.ListScrobbleLinks(ctx, "userB")
	if err != nil {
		t.Fatalf("ListScrobbleLinks userB: %v", err)
	}
	if len(links) != 0 {
		t.Fatalf("user B should have no links, got %d: %+v", len(links), links)
	}

	// ListScrobbleLinks for user A returns the link
	linksA, err := q.ListScrobbleLinks(ctx, "userA")
	if err != nil {
		t.Fatalf("ListScrobbleLinks userA: %v", err)
	}
	if len(linksA) != 1 || linksA[0].SessionKey != "sk-A" {
		t.Fatalf("user A should have 1 link, got %+v", linksA)
	}
}

func TestScrobbleLink_UpsertUpdates(t *testing.T) {
	st := openMigrated(t)
	ctx := context.Background()
	q := st.Q()

	if err := q.UpsertScrobbleLink(ctx, db.UpsertScrobbleLinkParams{
		UserID:     "u1",
		Provider:   "lastfm",
		SessionKey: "sk-old",
		Username:   "alice",
		Status:     "active",
		CreatedAt:  1000,
	}); err != nil {
		t.Fatalf("initial upsert: %v", err)
	}

	// Upsert again with changed session_key and status
	if err := q.UpsertScrobbleLink(ctx, db.UpsertScrobbleLinkParams{
		UserID:     "u1",
		Provider:   "lastfm",
		SessionKey: "sk-new",
		Username:   "alice",
		Status:     "broken",
		CreatedAt:  1000,
	}); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	got, err := q.GetScrobbleLink(ctx, db.GetScrobbleLinkParams{
		UserID:   "u1",
		Provider: "lastfm",
	})
	if err != nil {
		t.Fatalf("GetScrobbleLink after update: %v", err)
	}
	if got.SessionKey != "sk-new" || got.Status != "broken" {
		t.Fatalf("upsert should have updated row: %+v", got)
	}
}

func TestScrobbleLink_SetStatus(t *testing.T) {
	st := openMigrated(t)
	ctx := context.Background()
	q := st.Q()

	if err := q.UpsertScrobbleLink(ctx, db.UpsertScrobbleLinkParams{
		UserID:     "u1",
		Provider:   "lastfm",
		SessionKey: "sk",
		Username:   "alice",
		Status:     "active",
		CreatedAt:  1000,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	if err := q.SetScrobbleLinkStatus(ctx, db.SetScrobbleLinkStatusParams{
		Status:   "broken",
		UserID:   "u1",
		Provider: "lastfm",
	}); err != nil {
		t.Fatalf("SetScrobbleLinkStatus: %v", err)
	}

	got, _ := q.GetScrobbleLink(ctx, db.GetScrobbleLinkParams{UserID: "u1", Provider: "lastfm"})
	if got.Status != "broken" {
		t.Fatalf("status not updated: %+v", got)
	}
}

func TestScrobbleLink_Delete(t *testing.T) {
	st := openMigrated(t)
	ctx := context.Background()
	q := st.Q()

	if err := q.UpsertScrobbleLink(ctx, db.UpsertScrobbleLinkParams{
		UserID:     "u1",
		Provider:   "lastfm",
		SessionKey: "sk",
		Username:   "alice",
		Status:     "active",
		CreatedAt:  1000,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	if err := q.DeleteScrobbleLink(ctx, db.DeleteScrobbleLinkParams{
		UserID:   "u1",
		Provider: "lastfm",
	}); err != nil {
		t.Fatalf("DeleteScrobbleLink: %v", err)
	}

	links, _ := q.ListScrobbleLinks(ctx, "u1")
	if len(links) != 0 {
		t.Fatalf("link should be deleted, got %+v", links)
	}
}

func TestScrobbleQueue_SelectDueScrobbles(t *testing.T) {
	st := openMigrated(t)
	ctx := context.Background()
	q := st.Q()

	now := time.Now().Unix()

	// Insert a due pending row (next_attempt_at <= now)
	if err := q.InsertScrobbleQueue(ctx, db.InsertScrobbleQueueParams{
		ID:            "sq-due",
		UserID:        "u1",
		Provider:      "lastfm",
		CatalogID:     "cat-1",
		Title:         "Song A",
		Artist:        "Artist A",
		Album:         "Album A",
		DurationMs:    210000,
		PlayedAt:      now - 600,
		Status:        "pending",
		Attempts:      0,
		NextAttemptAt: now - 1, // in the past — due
		CreatedAt:     now - 700,
	}); err != nil {
		t.Fatalf("insert due row: %v", err)
	}

	// Insert a FUTURE next_attempt_at — must NOT be returned
	if err := q.InsertScrobbleQueue(ctx, db.InsertScrobbleQueueParams{
		ID:            "sq-future",
		UserID:        "u1",
		Provider:      "lastfm",
		CatalogID:     "cat-2",
		Title:         "Song B",
		Artist:        "Artist B",
		Album:         "Album B",
		DurationMs:    180000,
		PlayedAt:      now - 300,
		Status:        "pending",
		Attempts:      1,
		NextAttemptAt: now + 3600, // in the future — NOT due
		CreatedAt:     now - 400,
	}); err != nil {
		t.Fatalf("insert future row: %v", err)
	}

	// Insert a done row — must NOT be returned
	if err := q.InsertScrobbleQueue(ctx, db.InsertScrobbleQueueParams{
		ID:            "sq-done",
		UserID:        "u1",
		Provider:      "lastfm",
		CatalogID:     "cat-3",
		Title:         "Song C",
		Artist:        "Artist C",
		Album:         "Album C",
		DurationMs:    200000,
		PlayedAt:      now - 1200,
		Status:        "done",
		Attempts:      1,
		NextAttemptAt: 0,
		CreatedAt:     now - 1300,
	}); err != nil {
		t.Fatalf("insert done row: %v", err)
	}

	due, err := q.SelectDueScrobbles(ctx, db.SelectDueScrobblesParams{
		NextAttemptAt: now,
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("SelectDueScrobbles: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("expected 1 due scrobble, got %d: %+v", len(due), due)
	}
	if due[0].ID != "sq-due" {
		t.Fatalf("wrong due row: %+v", due[0])
	}
}

func TestScrobbleQueue_MarkDone(t *testing.T) {
	st := openMigrated(t)
	ctx := context.Background()
	q := st.Q()

	now := time.Now().Unix()

	if err := q.InsertScrobbleQueue(ctx, db.InsertScrobbleQueueParams{
		ID:            "sq-1",
		UserID:        "u1",
		Provider:      "lastfm",
		CatalogID:     "cat-1",
		Title:         "T",
		Artist:        "A",
		Album:         "",
		DurationMs:    0,
		PlayedAt:      now,
		Status:        "pending",
		Attempts:      0,
		NextAttemptAt: now - 1,
		CreatedAt:     now,
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if err := q.MarkScrobbleDone(ctx, "sq-1"); err != nil {
		t.Fatalf("MarkScrobbleDone: %v", err)
	}

	// Should no longer appear in due scrobbles
	due, _ := q.SelectDueScrobbles(ctx, db.SelectDueScrobblesParams{NextAttemptAt: now + 1000, Limit: 10})
	for _, row := range due {
		if row.ID == "sq-1" {
			t.Fatalf("done row should not appear in due scrobbles")
		}
	}
}

func TestScrobbleQueue_MarkRetry(t *testing.T) {
	st := openMigrated(t)
	ctx := context.Background()
	q := st.Q()

	now := time.Now().Unix()
	nextTry := now + 300

	if err := q.InsertScrobbleQueue(ctx, db.InsertScrobbleQueueParams{
		ID:            "sq-retry",
		UserID:        "u1",
		Provider:      "lastfm",
		CatalogID:     "cat-1",
		Title:         "T",
		Artist:        "A",
		Album:         "",
		DurationMs:    0,
		PlayedAt:      now,
		Status:        "pending",
		Attempts:      0,
		NextAttemptAt: now - 1,
		CreatedAt:     now,
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if err := q.MarkScrobbleRetry(ctx, db.MarkScrobbleRetryParams{
		Attempts:      1,
		NextAttemptAt: nextTry,
		ID:            "sq-retry",
	}); err != nil {
		t.Fatalf("MarkScrobbleRetry: %v", err)
	}

	// Not due yet (next_attempt_at = now+300, we query with now)
	due, _ := q.SelectDueScrobbles(ctx, db.SelectDueScrobblesParams{NextAttemptAt: now, Limit: 10})
	for _, row := range due {
		if row.ID == "sq-retry" {
			t.Fatalf("retried row should not be due yet")
		}
	}

	// Check attempts incremented via raw query (SelectDueScrobbles returns it after time passes)
	var attempts int64
	var status string
	err := st.DB().QueryRowContext(ctx, "SELECT attempts, status FROM scrobble_queue WHERE id='sq-retry'").Scan(&attempts, &status)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if attempts != 1 || status != "pending" {
		t.Fatalf("expected attempts=1 status=pending, got attempts=%d status=%s", attempts, status)
	}
}

func TestScrobbleQueue_MarkFailed(t *testing.T) {
	st := openMigrated(t)
	ctx := context.Background()
	q := st.Q()

	now := time.Now().Unix()

	if err := q.InsertScrobbleQueue(ctx, db.InsertScrobbleQueueParams{
		ID:            "sq-fail",
		UserID:        "u1",
		Provider:      "lastfm",
		CatalogID:     "cat-1",
		Title:         "T",
		Artist:        "A",
		Album:         "",
		DurationMs:    0,
		PlayedAt:      now,
		Status:        "pending",
		Attempts:      2,
		NextAttemptAt: now - 1,
		CreatedAt:     now,
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if err := q.MarkScrobbleFailed(ctx, "sq-fail"); err != nil {
		t.Fatalf("MarkScrobbleFailed: %v", err)
	}

	var status string
	err := st.DB().QueryRowContext(ctx, "SELECT status FROM scrobble_queue WHERE id='sq-fail'").Scan(&status)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if status != "failed" {
		t.Fatalf("expected status=failed, got %s", status)
	}

	// Also excluded from due scrobbles
	due, _ := q.SelectDueScrobbles(ctx, db.SelectDueScrobblesParams{NextAttemptAt: now + 1000, Limit: 10})
	for _, row := range due {
		if row.ID == "sq-fail" {
			t.Fatalf("failed row should not appear in due scrobbles")
		}
	}
}
