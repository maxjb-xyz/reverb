package request_test

import (
	"context"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/events"
	"github.com/maxjb-xyz/reverb/internal/request"
	"github.com/maxjb-xyz/reverb/internal/store"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// fakePublisher captures every published event for assertions.
type fakePublisher struct {
	events []events.Event
}

func (f *fakePublisher) Publish(ev events.Event) { f.events = append(f.events, ev) }

// newTestService opens a migrated store, seeds a role + user so the FK constraint
// on requests.requested_by is satisfied, and returns the service + seeded userID.
func newTestService(t *testing.T) (*request.Service, *fakePublisher, string) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/r.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	q := st.Q()
	ctx := context.Background()

	// seed minimum role + user so FK on requested_by is satisfied
	if err := q.CreateRole(ctx, db.CreateRoleParams{
		ID:           "role-user",
		Name:         "User",
		IsSystem:     1,
		Capabilities: `["request"]`,
	}); err != nil {
		t.Fatal(err)
	}
	if err := q.CreateUser(ctx, db.CreateUserParams{
		ID:           "user-1",
		Username:     "alice",
		PasswordHash: "x",
		RoleID:       "role-user",
		IsOwner:      0,
	}); err != nil {
		t.Fatal(err)
	}
	// seed a second user for ownership tests and as an approver
	if err := q.CreateUser(ctx, db.CreateUserParams{
		ID:           "user-2",
		Username:     "bob",
		PasswordHash: "x",
		RoleID:       "role-user",
		IsOwner:      0,
	}); err != nil {
		t.Fatal(err)
	}
	// seed a manager user (decided_by is a FK to users.id)
	if err := q.CreateUser(ctx, db.CreateUserParams{
		ID:           "manager-1",
		Username:     "manager",
		PasswordHash: "x",
		RoleID:       "role-user",
		IsOwner:      0,
	}); err != nil {
		t.Fatal(err)
	}

	pub := &fakePublisher{}
	svc := request.NewService(q, pub, time.Now)
	return svc, pub, "user-1"
}

var testItem = core.RequestItem{
	Source:     "spotify",
	ExternalID: "track-abc",
	Title:      "Song Title",
	Artist:     "Some Artist",
}

// TestCreateInsertsPending verifies Create inserts a pending request and returns existed=false.
func TestCreateInsertsPending(t *testing.T) {
	svc, _, userID := newTestService(t)
	ctx := context.Background()

	req, existed, err := svc.Create(ctx, userID, testItem)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if existed {
		t.Fatal("want existed=false for new request")
	}
	if req.ID == "" {
		t.Fatal("want non-empty ID")
	}
	if req.Status != core.RequestPending {
		t.Fatalf("want status=%q, got %q", core.RequestPending, req.Status)
	}
	if req.RequestedBy != userID {
		t.Fatalf("want RequestedBy=%q, got %q", userID, req.RequestedBy)
	}
	if req.Source != testItem.Source || req.ExternalID != testItem.ExternalID {
		t.Fatal("item fields not persisted")
	}
}

// TestCreateDedupReturnsSameID verifies that creating the same item again returns existed=true.
func TestCreateDedupReturnsSameID(t *testing.T) {
	svc, _, userID := newTestService(t)
	ctx := context.Background()

	first, _, _ := svc.Create(ctx, userID, testItem)
	second, existed, err := svc.Create(ctx, userID, testItem)
	if err != nil {
		t.Fatalf("second Create: %v", err)
	}
	if !existed {
		t.Fatal("want existed=true for duplicate")
	}
	if second.ID != first.ID {
		t.Fatalf("want same ID %q, got %q", first.ID, second.ID)
	}
}

// TestMarkApprovedTransitionAndEvent verifies pending→approved transition, sets job ID,
// and publishes a request.updated event targeted at the requester.
func TestMarkApprovedTransitionAndEvent(t *testing.T) {
	svc, pub, userID := newTestService(t)
	ctx := context.Background()

	req, _, _ := svc.Create(ctx, userID, testItem)

	approved, err := svc.MarkApproved(ctx, req.ID, "manager-1", "job-xyz")
	if err != nil {
		t.Fatalf("MarkApproved: %v", err)
	}
	if approved.Status != core.RequestApproved {
		t.Fatalf("want status=%q, got %q", core.RequestApproved, approved.Status)
	}
	if approved.DownloadJobID != "job-xyz" {
		t.Fatalf("want DownloadJobID=job-xyz, got %q", approved.DownloadJobID)
	}
	if approved.DecidedBy != "manager-1" {
		t.Fatalf("want DecidedBy=manager-1, got %q", approved.DecidedBy)
	}
	if approved.DecidedAt == 0 {
		t.Fatal("want non-zero DecidedAt")
	}

	// should have published exactly one request.updated event
	if len(pub.events) != 1 {
		t.Fatalf("want 1 event, got %d", len(pub.events))
	}
	ev := pub.events[0]
	if ev.Topic != request.TopicUpdated {
		t.Fatalf("want topic %q, got %q", request.TopicUpdated, ev.Topic)
	}
	re, ok := ev.Payload.(core.RequestEvent)
	if !ok {
		t.Fatalf("want core.RequestEvent payload, got %T", ev.Payload)
	}
	if re.TargetUserID != userID {
		t.Fatalf("want TargetUserID=%q, got %q", userID, re.TargetUserID)
	}
	if re.Request.ID != req.ID {
		t.Fatal("event payload carries wrong request ID")
	}
}

// TestMarkApprovedNonPendingReturnsErrNotPending verifies approving a non-pending request fails.
func TestMarkApprovedNonPendingReturnsErrNotPending(t *testing.T) {
	svc, _, userID := newTestService(t)
	ctx := context.Background()

	req, _, _ := svc.Create(ctx, userID, testItem)
	// approve once
	if _, err := svc.MarkApproved(ctx, req.ID, "manager-1", "job-1"); err != nil {
		t.Fatal(err)
	}
	// approve again — must fail
	if _, err := svc.MarkApproved(ctx, req.ID, "manager-1", "job-2"); err != request.ErrNotPending {
		t.Fatalf("want ErrNotPending, got %v", err)
	}
}

// TestDenyTransitionAndEvent verifies pending→denied + event targeted at requester.
func TestDenyTransitionAndEvent(t *testing.T) {
	svc, pub, userID := newTestService(t)
	ctx := context.Background()

	req, _, _ := svc.Create(ctx, userID, testItem)
	denied, err := svc.Deny(ctx, req.ID, "manager-1", "not available")
	if err != nil {
		t.Fatalf("Deny: %v", err)
	}
	if denied.Status != core.RequestDenied {
		t.Fatalf("want status=%q, got %q", core.RequestDenied, denied.Status)
	}
	if denied.DenyReason != "not available" {
		t.Fatalf("want DenyReason=%q, got %q", "not available", denied.DenyReason)
	}
	if len(pub.events) != 1 || pub.events[0].Topic != request.TopicUpdated {
		t.Fatal("want 1 request.updated event")
	}
	re := pub.events[0].Payload.(core.RequestEvent)
	if re.TargetUserID != userID {
		t.Fatalf("want TargetUserID=%q, got %q", userID, re.TargetUserID)
	}
}

// TestCancelOwnPendingDeletes verifies Cancel deletes the row.
func TestCancelOwnPendingDeletes(t *testing.T) {
	svc, _, userID := newTestService(t)
	ctx := context.Background()

	req, _, _ := svc.Create(ctx, userID, testItem)
	if err := svc.Cancel(ctx, req.ID, userID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	// subsequent Get should return ErrNotFound
	if _, err := svc.Get(ctx, req.ID); err != request.ErrNotFound {
		t.Fatalf("want ErrNotFound after cancel, got %v", err)
	}
}

// TestCancelOthersForbidden verifies Cancel returns ErrForbidden for a different requester.
func TestCancelOthersForbidden(t *testing.T) {
	svc, _, userID := newTestService(t)
	ctx := context.Background()

	req, _, _ := svc.Create(ctx, userID, testItem)
	err := svc.Cancel(ctx, req.ID, "user-2")
	if err != request.ErrForbidden {
		t.Fatalf("want ErrForbidden, got %v", err)
	}
}

// TestCancelPublishesCanceledEvent verifies that a successful Cancel publishes a
// request.canceled event carrying the canceled request's ID.
func TestCancelPublishesCanceledEvent(t *testing.T) {
	svc, pub, userID := newTestService(t)
	ctx := context.Background()

	req, _, _ := svc.Create(ctx, userID, testItem)
	if err := svc.Cancel(ctx, req.ID, userID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	if len(pub.events) != 1 {
		t.Fatalf("want 1 event, got %d", len(pub.events))
	}
	ev := pub.events[0]
	if ev.Topic != request.TopicCanceled {
		t.Fatalf("want topic %q, got %q", request.TopicCanceled, ev.Topic)
	}
	re, ok := ev.Payload.(core.RequestEvent)
	if !ok {
		t.Fatalf("want core.RequestEvent payload, got %T", ev.Payload)
	}
	if re.Request.ID != req.ID {
		t.Fatalf("want Request.ID=%q, got %q", req.ID, re.Request.ID)
	}
}

// TestMarkFulfilledFlipsAndPublishes verifies approved→fulfilled transition + event,
// and that approval metadata (decided_by, decided_at, download_job_id) is preserved.
func TestMarkFulfilledFlipsAndPublishes(t *testing.T) {
	svc, pub, userID := newTestService(t)
	ctx := context.Background()

	req, _, _ := svc.Create(ctx, userID, testItem)
	if _, err := svc.MarkApproved(ctx, req.ID, "manager-1", "job-1"); err != nil {
		t.Fatal(err)
	}
	pub.events = nil // clear approval event

	if err := svc.MarkFulfilled(ctx, req.ID); err != nil {
		t.Fatalf("MarkFulfilled: %v", err)
	}

	got, err := svc.Get(ctx, req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != core.RequestFulfilled {
		t.Fatalf("want status=%q, got %q", core.RequestFulfilled, got.Status)
	}
	// approval metadata must survive the fulfill transition
	if got.DecidedBy != "manager-1" {
		t.Fatalf("MarkFulfilled wiped DecidedBy: want %q, got %q", "manager-1", got.DecidedBy)
	}
	if got.DownloadJobID != "job-1" {
		t.Fatalf("MarkFulfilled wiped DownloadJobID: want %q, got %q", "job-1", got.DownloadJobID)
	}
	if got.DecidedAt == 0 {
		t.Fatal("MarkFulfilled wiped DecidedAt")
	}
	if len(pub.events) != 1 || pub.events[0].Topic != request.TopicUpdated {
		t.Fatalf("want 1 request.updated event after MarkFulfilled, got %d", len(pub.events))
	}
	re := pub.events[0].Payload.(core.RequestEvent)
	if re.TargetUserID != userID {
		t.Fatalf("want TargetUserID=%q, got %q", userID, re.TargetUserID)
	}
	// event payload must also carry the preserved metadata
	if re.Request.DecidedBy != "manager-1" {
		t.Fatalf("event payload DecidedBy: want %q, got %q", "manager-1", re.Request.DecidedBy)
	}
	if re.Request.DownloadJobID != "job-1" {
		t.Fatalf("event payload DownloadJobID: want %q, got %q", "job-1", re.Request.DownloadJobID)
	}
}

// TestMarkFulfilledIdempotentPublishesOnce verifies that calling MarkFulfilled twice
// on an already-fulfilled request publishes request.updated exactly once (no duplicate toast).
func TestMarkFulfilledIdempotentPublishesOnce(t *testing.T) {
	svc, pub, userID := newTestService(t)
	ctx := context.Background()

	req, _, _ := svc.Create(ctx, userID, testItem)
	if _, err := svc.MarkApproved(ctx, req.ID, "manager-1", "job-1"); err != nil {
		t.Fatal(err)
	}
	pub.events = nil // clear approval event

	// first call: should flip to fulfilled and publish
	if err := svc.MarkFulfilled(ctx, req.ID); err != nil {
		t.Fatalf("first MarkFulfilled: %v", err)
	}
	if len(pub.events) != 1 {
		t.Fatalf("after first call: want 1 event, got %d", len(pub.events))
	}

	// second call (simulating duplicate download.complete): must no-op, no new event
	if err := svc.MarkFulfilled(ctx, req.ID); err != nil {
		t.Fatalf("second MarkFulfilled: %v", err)
	}
	if len(pub.events) != 1 {
		t.Fatalf("after second call: want still 1 event (idempotent), got %d", len(pub.events))
	}
}

// TestMarkFailedIdempotentPublishesOnce verifies that calling MarkFailed twice
// on an already-failed request publishes request.updated exactly once.
func TestMarkFailedIdempotentPublishesOnce(t *testing.T) {
	svc, pub, userID := newTestService(t)
	ctx := context.Background()

	req, _, _ := svc.Create(ctx, userID, testItem)
	if _, err := svc.MarkApproved(ctx, req.ID, "manager-1", "job-1"); err != nil {
		t.Fatal(err)
	}
	pub.events = nil // clear approval event

	// first call: should flip to failed and publish
	if err := svc.MarkFailed(ctx, req.ID, "download error"); err != nil {
		t.Fatalf("first MarkFailed: %v", err)
	}
	if len(pub.events) != 1 {
		t.Fatalf("after first call: want 1 event, got %d", len(pub.events))
	}

	// second call: must no-op, no new event
	if err := svc.MarkFailed(ctx, req.ID, "download error again"); err != nil {
		t.Fatalf("second MarkFailed: %v", err)
	}
	if len(pub.events) != 1 {
		t.Fatalf("after second call: want still 1 event (idempotent), got %d", len(pub.events))
	}
}

// TestCreateCoverUrlRoundTrip verifies that a coverUrl set on the request item
// persists to the DB and is returned on the core.Request.
func TestCreateCoverUrlRoundTrip(t *testing.T) {
	svc, _, userID := newTestService(t)
	ctx := context.Background()

	itemWithCover := core.RequestItem{
		Source:     "spotify",
		ExternalID: "track-cover",
		Title:      "Cover Song",
		Artist:     "Cover Artist",
		CoverUrl:   "https://i.scdn.co/image/abc123",
	}

	req, existed, err := svc.Create(ctx, userID, itemWithCover)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if existed {
		t.Fatal("want existed=false for new request")
	}
	if req.CoverUrl != "https://i.scdn.co/image/abc123" {
		t.Fatalf("want CoverUrl=%q, got %q", "https://i.scdn.co/image/abc123", req.CoverUrl)
	}

	// Also verify via a fresh Get
	fetched, err := svc.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if fetched.CoverUrl != "https://i.scdn.co/image/abc123" {
		t.Fatalf("Get: want CoverUrl=%q, got %q", "https://i.scdn.co/image/abc123", fetched.CoverUrl)
	}
}

// TestMarkFailedPreservesMetadata verifies approved→failed preserves approval metadata + publishes event.
func TestMarkFailedPreservesMetadata(t *testing.T) {
	svc, pub, userID := newTestService(t)
	ctx := context.Background()

	req, _, _ := svc.Create(ctx, userID, testItem)
	if _, err := svc.MarkApproved(ctx, req.ID, "manager-1", "job-1"); err != nil {
		t.Fatal(err)
	}
	pub.events = nil // clear approval event

	if err := svc.MarkFailed(ctx, req.ID, "download error"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	got, err := svc.Get(ctx, req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != core.RequestFailed {
		t.Fatalf("want status=%q, got %q", core.RequestFailed, got.Status)
	}
	// approval metadata must survive the fail transition
	if got.DecidedBy != "manager-1" {
		t.Fatalf("MarkFailed wiped DecidedBy: want %q, got %q", "manager-1", got.DecidedBy)
	}
	if got.DownloadJobID != "job-1" {
		t.Fatalf("MarkFailed wiped DownloadJobID: want %q, got %q", "job-1", got.DownloadJobID)
	}
	if got.DecidedAt == 0 {
		t.Fatal("MarkFailed wiped DecidedAt")
	}
	if len(pub.events) != 1 || pub.events[0].Topic != request.TopicUpdated {
		t.Fatalf("want 1 request.updated event after MarkFailed, got %d", len(pub.events))
	}
	re := pub.events[0].Payload.(core.RequestEvent)
	if re.TargetUserID != userID {
		t.Fatalf("want TargetUserID=%q, got %q", userID, re.TargetUserID)
	}
	// event payload must also carry the preserved metadata
	if re.Request.DecidedBy != "manager-1" {
		t.Fatalf("event payload DecidedBy: want %q, got %q", "manager-1", re.Request.DecidedBy)
	}
	if re.Request.DownloadJobID != "job-1" {
		t.Fatalf("event payload DownloadJobID: want %q, got %q", "job-1", re.Request.DownloadJobID)
	}
}

// TestCreateKindAlbumRoundTrip verifies that Kind:"album" is persisted and read back.
func TestCreateKindAlbumRoundTrip(t *testing.T) {
	svc, _, userID := newTestService(t)
	ctx := context.Background()

	albumItem := core.RequestItem{
		Source:     "lidarr",
		ExternalID: "album-xyz",
		Title:      "Dark Side of the Moon",
		Artist:     "Pink Floyd",
		Album:      "Dark Side of the Moon",
		Kind:       "album",
	}

	req, existed, err := svc.Create(ctx, userID, albumItem)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if existed {
		t.Fatal("want existed=false for new request")
	}
	if req.Kind != "album" {
		t.Fatalf("want Kind=%q, got %q", "album", req.Kind)
	}

	// Also verify via a fresh Get
	fetched, err := svc.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if fetched.Kind != "album" {
		t.Fatalf("Get: want Kind=%q, got %q", "album", fetched.Kind)
	}
}

// TestCreateTrackCountRoundTrip verifies that TrackCount:12 is persisted and read back.
func TestCreateTrackCountRoundTrip(t *testing.T) {
	svc, _, userID := newTestService(t)
	ctx := context.Background()

	albumItem := core.RequestItem{
		Source:     "lidarr",
		ExternalID: "album-tc",
		Title:      "Dark Side of the Moon",
		Artist:     "Pink Floyd",
		Album:      "Dark Side of the Moon",
		Kind:       "album",
		TrackCount: 12,
	}

	req, existed, err := svc.Create(ctx, userID, albumItem)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if existed {
		t.Fatal("want existed=false for new request")
	}
	if req.TrackCount != 12 {
		t.Fatalf("want TrackCount=12, got %d", req.TrackCount)
	}

	// Also verify via a fresh Get
	fetched, err := svc.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if fetched.TrackCount != 12 {
		t.Fatalf("Get: want TrackCount=12, got %d", fetched.TrackCount)
	}
}

// TestCreateEmptyKindDefaultsToTrack verifies that an empty Kind is stored as "track".
func TestCreateEmptyKindDefaultsToTrack(t *testing.T) {
	svc, _, userID := newTestService(t)
	ctx := context.Background()

	itemNoKind := core.RequestItem{
		Source:     "spotify",
		ExternalID: "track-nokind",
		Title:      "No Kind Song",
		Artist:     "Some Artist",
	}

	req, _, err := svc.Create(ctx, userID, itemNoKind)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if req.Kind != "track" {
		t.Fatalf("want Kind=%q (default), got %q", "track", req.Kind)
	}

	fetched, err := svc.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if fetched.Kind != "track" {
		t.Fatalf("Get: want Kind=%q (default), got %q", "track", fetched.Kind)
	}
}
