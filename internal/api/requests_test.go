package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/events"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/request"
	"github.com/maxjb-xyz/reverb/internal/store"
)

// requestTestServer builds a Server with a real store (backed by a temp SQLite
// file), the request service, and an optional download manager. The owner (admin
// / auto_approve) session cookie is returned alongside the server.
func requestTestServer(t *testing.T, mgr DownloadManager) (*store.Store, *Server, *http.Cookie) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/req.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	authSvc, ownerTok := seededAuthToken(t, st)

	bus := events.New()
	reqSvc := request.NewService(st.Q(), bus, time.Now)

	srv := NewServer(Deps{
		Auth:       authSvc,
		Downloads:  mgr,
		Requests:   reqSvc,
		Search:     registry.NewRegistry("search"),
		Downloader: registry.NewRegistry("downloader"),
	})
	return st, srv, &http.Cookie{Name: sessionCookie, Value: ownerTok}
}

const reqItem = `{"source":"spotify","externalId":"sp1","title":"Song","artist":"Artist","album":"Album","durationMs":200000}`

// doReq fires an HTTP request with the given cookie and returns the recorder.
func doReq(t *testing.T, srv *Server, cookie *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var buf *bytes.Buffer
	if body != "" {
		buf = bytes.NewBufferString(body)
	} else {
		buf = bytes.NewBufferString("")
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, buf)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// TestRequestPending: a role-requester user POSTing /requests gets status=pending;
// the download manager is NOT called.
func TestRequestPending(t *testing.T) {
	mgr := newFakeManager()
	_, srv, ownerCookie := requestTestServer(t, mgr)

	// Create a requester user via the admin endpoint.
	rec := doReq(t, srv, ownerCookie, http.MethodPost, "/api/v1/users",
		`{"username":"req1","password":"pw123456","roleId":"role-requester"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user = %d: %s", rec.Code, rec.Body.String())
	}

	requesterTok := mustLogin(t, srv, "req1", "pw123456")
	requesterCookie := &http.Cookie{Name: sessionCookie, Value: requesterTok}

	rec = doReq(t, srv, requesterCookie, http.MethodPost, "/api/v1/requests", reqItem)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /requests = %d: %s", rec.Code, rec.Body.String())
	}
	var req core.Request
	if err := json.NewDecoder(rec.Body).Decode(&req); err != nil {
		t.Fatal(err)
	}
	if req.Status != core.RequestPending {
		t.Fatalf("want status=pending, got %q", req.Status)
	}
	if req.ID == "" {
		t.Fatal("expected non-empty request ID")
	}

	// Manager must NOT have been called.
	if mgr.lastReq.ExternalID != "" {
		t.Fatal("Enqueue should NOT have been called for a pending request")
	}

	// Admin can see it in GET /requests.
	rec = doReq(t, srv, ownerCookie, http.MethodGet, "/api/v1/requests", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /requests = %d: %s", rec.Code, rec.Body.String())
	}
	var all []core.Request
	if err := json.NewDecoder(rec.Body).Decode(&all); err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].ID != req.ID {
		t.Fatalf("GET /requests returned %+v, want 1 item with id=%s", all, req.ID)
	}
}

// TestRequestAutoApprove: the owner (has auto_approve) POSTing /requests gets
// status=approved and Enqueue IS called with InitiatedBy=ownerID.
func TestRequestAutoApprove(t *testing.T) {
	mgr := newFakeManager()
	_, srv, ownerCookie := requestTestServer(t, mgr)

	// Fetch owner's ID.
	rec := doReq(t, srv, ownerCookie, http.MethodGet, "/api/v1/me", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /me = %d: %s", rec.Code, rec.Body.String())
	}
	var me struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&me); err != nil {
		t.Fatal(err)
	}
	ownerID := me.ID

	rec = doReq(t, srv, ownerCookie, http.MethodPost, "/api/v1/requests", reqItem)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /requests = %d: %s", rec.Code, rec.Body.String())
	}
	var req core.Request
	if err := json.NewDecoder(rec.Body).Decode(&req); err != nil {
		t.Fatal(err)
	}
	if req.Status != core.RequestApproved {
		t.Fatalf("want status=approved, got %q", req.Status)
	}
	// Enqueue must have been called with InitiatedBy = requester (=owner here since same person).
	if mgr.lastReq.ExternalID != "sp1" {
		t.Fatalf("Enqueue not called or wrong ExternalID: %+v", mgr.lastReq)
	}
	if mgr.lastReq.InitiatedBy != ownerID {
		t.Fatalf("InitiatedBy = %q, want owner %q", mgr.lastReq.InitiatedBy, ownerID)
	}
}

// TestRequestDedup: same user posting the same item twice returns the same request ID.
func TestRequestDedup(t *testing.T) {
	mgr := newFakeManager()
	_, srv, ownerCookie := requestTestServer(t, mgr)

	rec := doReq(t, srv, ownerCookie, http.MethodPost, "/api/v1/requests", reqItem)
	if rec.Code != http.StatusOK {
		t.Fatalf("first POST = %d: %s", rec.Code, rec.Body.String())
	}
	var r1 core.Request
	if err := json.NewDecoder(rec.Body).Decode(&r1); err != nil {
		t.Fatal(err)
	}

	rec = doReq(t, srv, ownerCookie, http.MethodPost, "/api/v1/requests", reqItem)
	if rec.Code != http.StatusOK {
		t.Fatalf("second POST = %d: %s", rec.Code, rec.Body.String())
	}
	var r2 core.Request
	if err := json.NewDecoder(rec.Body).Decode(&r2); err != nil {
		t.Fatal(err)
	}

	if r1.ID != r2.ID {
		t.Fatalf("dedup: got two different IDs: %s vs %s", r1.ID, r2.ID)
	}
}

// TestListMyRequests: GET /requests/mine returns only the caller's requests.
func TestListMyRequests(t *testing.T) {
	mgr := newFakeManager()
	_, srv, ownerCookie := requestTestServer(t, mgr)

	// Create requester.
	doReq(t, srv, ownerCookie, http.MethodPost, "/api/v1/users",
		`{"username":"req2","password":"pw123456","roleId":"role-requester"}`)
	reqTok := mustLogin(t, srv, "req2", "pw123456")
	reqCookie := &http.Cookie{Name: sessionCookie, Value: reqTok}

	// Requester posts a request.
	doReq(t, srv, reqCookie, http.MethodPost, "/api/v1/requests", reqItem)

	// GET /requests/mine as requester.
	rec := doReq(t, srv, reqCookie, http.MethodGet, "/api/v1/requests/mine", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /requests/mine = %d: %s", rec.Code, rec.Body.String())
	}
	var mine []core.Request
	if err := json.NewDecoder(rec.Body).Decode(&mine); err != nil {
		t.Fatal(err)
	}
	if len(mine) != 1 {
		t.Fatalf("GET /requests/mine returned %d items, want 1", len(mine))
	}
	if mine[0].Source != "spotify" {
		t.Fatalf("wrong item: %+v", mine[0])
	}
}

// TestApproveRequest: admin approves a pending request → approved + Enqueue called
// with InitiatedBy = original requester (not the admin).
func TestApproveRequest(t *testing.T) {
	mgr := newFakeManager()
	_, srv, ownerCookie := requestTestServer(t, mgr)

	// Create a requester.
	rec := doReq(t, srv, ownerCookie, http.MethodPost, "/api/v1/users",
		`{"username":"req3","password":"pw123456","roleId":"role-requester"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user = %d: %s", rec.Code, rec.Body.String())
	}
	var createdUser struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&createdUser); err != nil {
		t.Fatal(err)
	}
	requesterID := createdUser.ID

	reqTok := mustLogin(t, srv, "req3", "pw123456")
	reqCookie := &http.Cookie{Name: sessionCookie, Value: reqTok}

	// Requester posts a request → pending.
	rec = doReq(t, srv, reqCookie, http.MethodPost, "/api/v1/requests", reqItem)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /requests = %d: %s", rec.Code, rec.Body.String())
	}
	var req core.Request
	if err := json.NewDecoder(rec.Body).Decode(&req); err != nil {
		t.Fatal(err)
	}
	if req.Status != core.RequestPending {
		t.Fatalf("want pending, got %q", req.Status)
	}

	// Admin approves it.
	rec = doReq(t, srv, ownerCookie, http.MethodPost, "/api/v1/requests/"+req.ID+"/approve", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("approve = %d: %s", rec.Code, rec.Body.String())
	}
	var approved core.Request
	if err := json.NewDecoder(rec.Body).Decode(&approved); err != nil {
		t.Fatal(err)
	}
	if approved.Status != core.RequestApproved {
		t.Fatalf("want approved, got %q", approved.Status)
	}
	// InitiatedBy must be the requester, not the admin.
	if mgr.lastReq.InitiatedBy != requesterID {
		t.Fatalf("InitiatedBy = %q, want requester %q", mgr.lastReq.InitiatedBy, requesterID)
	}
}

// TestApproveAlreadyApproved: approving an already-approved request → 409 and NO
// extra Enqueue call (the stray-download guard fires before the download manager).
func TestApproveAlreadyApproved(t *testing.T) {
	mgr := newFakeManager()
	_, srv, ownerCookie := requestTestServer(t, mgr)

	// Owner (auto_approve) posts → immediately approved (Enqueue called once via auto_approve path).
	rec := doReq(t, srv, ownerCookie, http.MethodPost, "/api/v1/requests", reqItem)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /requests = %d: %s", rec.Code, rec.Body.String())
	}
	var req core.Request
	if err := json.NewDecoder(rec.Body).Decode(&req); err != nil {
		t.Fatal(err)
	}
	if req.Status != core.RequestApproved {
		t.Fatalf("want approved, got %q", req.Status)
	}

	callsAfterFirst := mgr.enqueueCalls // should be 1 from the auto-approve path

	// Attempt to approve again → 409 and Enqueue must NOT be called again.
	rec = doReq(t, srv, ownerCookie, http.MethodPost, "/api/v1/requests/"+req.ID+"/approve", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("double-approve = %d, want 409", rec.Code)
	}
	if mgr.enqueueCalls != callsAfterFirst {
		t.Fatalf("Enqueue called %d time(s) on double-approve, want 0 extra calls (stray download guard missing)",
			mgr.enqueueCalls-callsAfterFirst)
	}
}

// TestDenyRequest: admin denies a pending request.
func TestDenyRequest(t *testing.T) {
	mgr := newFakeManager()
	_, srv, ownerCookie := requestTestServer(t, mgr)

	// Create requester.
	doReq(t, srv, ownerCookie, http.MethodPost, "/api/v1/users",
		`{"username":"req4","password":"pw123456","roleId":"role-requester"}`)
	reqTok := mustLogin(t, srv, "req4", "pw123456")
	reqCookie := &http.Cookie{Name: sessionCookie, Value: reqTok}

	rec := doReq(t, srv, reqCookie, http.MethodPost, "/api/v1/requests", reqItem)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /requests = %d: %s", rec.Code, rec.Body.String())
	}
	var req core.Request
	if err := json.NewDecoder(rec.Body).Decode(&req); err != nil {
		t.Fatal(err)
	}

	rec = doReq(t, srv, ownerCookie, http.MethodPost,
		"/api/v1/requests/"+req.ID+"/deny", `{"reason":"no thanks"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("deny = %d: %s", rec.Code, rec.Body.String())
	}
	var denied core.Request
	if err := json.NewDecoder(rec.Body).Decode(&denied); err != nil {
		t.Fatal(err)
	}
	if denied.Status != core.RequestDenied {
		t.Fatalf("want denied, got %q", denied.Status)
	}
}

// TestCancelOwnRequest: requester can cancel their own pending request → 200.
func TestCancelOwnRequest(t *testing.T) {
	mgr := newFakeManager()
	_, srv, ownerCookie := requestTestServer(t, mgr)

	// Create requester.
	doReq(t, srv, ownerCookie, http.MethodPost, "/api/v1/users",
		`{"username":"req5","password":"pw123456","roleId":"role-requester"}`)
	reqTok := mustLogin(t, srv, "req5", "pw123456")
	reqCookie := &http.Cookie{Name: sessionCookie, Value: reqTok}

	rec := doReq(t, srv, reqCookie, http.MethodPost, "/api/v1/requests", reqItem)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /requests = %d: %s", rec.Code, rec.Body.String())
	}
	var req core.Request
	if err := json.NewDecoder(rec.Body).Decode(&req); err != nil {
		t.Fatal(err)
	}

	rec = doReq(t, srv, reqCookie, http.MethodPost, "/api/v1/requests/"+req.ID+"/cancel", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel own = %d: %s", rec.Code, rec.Body.String())
	}
}

// TestCancelOtherUserRequest: another user (different requester) cannot cancel
// someone else's request → 403.
func TestCancelOtherUserRequest(t *testing.T) {
	mgr := newFakeManager()
	_, srv, ownerCookie := requestTestServer(t, mgr)

	// Create two requesters.
	doReq(t, srv, ownerCookie, http.MethodPost, "/api/v1/users",
		`{"username":"req6","password":"pw123456","roleId":"role-requester"}`)
	doReq(t, srv, ownerCookie, http.MethodPost, "/api/v1/users",
		`{"username":"req7","password":"pw123456","roleId":"role-requester"}`)

	req6Tok := mustLogin(t, srv, "req6", "pw123456")
	req7Tok := mustLogin(t, srv, "req7", "pw123456")
	req6Cookie := &http.Cookie{Name: sessionCookie, Value: req6Tok}
	req7Cookie := &http.Cookie{Name: sessionCookie, Value: req7Tok}

	// req6 posts a request.
	rec := doReq(t, srv, req6Cookie, http.MethodPost, "/api/v1/requests", reqItem)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /requests = %d: %s", rec.Code, rec.Body.String())
	}
	var req core.Request
	if err := json.NewDecoder(rec.Body).Decode(&req); err != nil {
		t.Fatal(err)
	}

	// req7 tries to cancel req6's request → 403.
	rec = doReq(t, srv, req7Cookie, http.MethodPost, "/api/v1/requests/"+req.ID+"/cancel", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cancel other = %d, want 403", rec.Code)
	}
}

// TestRequestCapabilityGates: a plain role-requester user cannot hit the
// manage_requests-gated endpoints.
func TestRequestCapabilityGates(t *testing.T) {
	mgr := newFakeManager()
	_, srv, ownerCookie := requestTestServer(t, mgr)

	doReq(t, srv, ownerCookie, http.MethodPost, "/api/v1/users",
		`{"username":"reqgate","password":"pw123456","roleId":"role-requester"}`)
	reqTok := mustLogin(t, srv, "reqgate", "pw123456")
	reqCookie := &http.Cookie{Name: sessionCookie, Value: reqTok}

	// GET /requests → 403 (manage_requests required).
	rec := doReq(t, srv, reqCookie, http.MethodGet, "/api/v1/requests", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("GET /requests as requester = %d, want 403", rec.Code)
	}

	// POST /requests/{id}/approve → 403.
	rec = doReq(t, srv, reqCookie, http.MethodPost, "/api/v1/requests/fake-id/approve", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST /approve as requester = %d, want 403", rec.Code)
	}

	// POST /requests/{id}/deny → 403.
	rec = doReq(t, srv, reqCookie, http.MethodPost, "/api/v1/requests/fake-id/deny", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST /deny as requester = %d, want 403", rec.Code)
	}
}

// TestCreateRequestNilManager503: auto_approve user with nil download manager → 503.
func TestCreateRequestNilManager503(t *testing.T) {
	_, srv, ownerCookie := requestTestServer(t, nil)

	rec := doReq(t, srv, ownerCookie, http.MethodPost, "/api/v1/requests", reqItem)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestDownloadReqFromItemAlbumKind verifies that downloadReqFromItem of an album item
// produces Granularity==GranularityAlbum and a non-empty Album field.
func TestDownloadReqFromItemAlbumKind(t *testing.T) {
	item := core.RequestItem{
		Source:     "lidarr",
		ExternalID: "album-123",
		Title:      "Dark Side of the Moon",
		Artist:     "Pink Floyd",
		Album:      "Dark Side of the Moon",
		Kind:       "album",
	}
	got := downloadReqFromItem(item, "user-1")
	if got.Granularity != core.GranularityAlbum {
		t.Fatalf("want Granularity=%q, got %q", core.GranularityAlbum, got.Granularity)
	}
	if got.Album == "" {
		t.Fatal("want non-empty Album")
	}
}

// TestDownloadReqFromRequestAlbumKind verifies that downloadReqFromRequest of an album
// Request produces Granularity==GranularityAlbum.
func TestDownloadReqFromRequestAlbumKind(t *testing.T) {
	req := core.Request{
		ID:          "req-1",
		RequestedBy: "user-1",
		Source:      "lidarr",
		ExternalID:  "album-456",
		Title:       "Wish You Were Here",
		Artist:      "Pink Floyd",
		Album:       "Wish You Were Here",
		Kind:        "album",
		Status:      core.RequestApproved,
	}
	got := downloadReqFromRequest(req)
	if got.Granularity != core.GranularityAlbum {
		t.Fatalf("want Granularity=%q, got %q", core.GranularityAlbum, got.Granularity)
	}
}

// ─── Album end-to-end handler tests ────────────────────────────────────────────

const albumReqItem = `{"source":"lidarr","externalId":"album-123","title":"Dark Side of the Moon","artist":"Pink Floyd","album":"Dark Side of the Moon","kind":"album"}`

// TestAlbumRequestAutoApproveEnqueuesWithAlbumGranularity: an auto_approve user
// POSTing an album request gets a job enqueued whose Granularity==album.
func TestAlbumRequestAutoApproveEnqueuesWithAlbumGranularity(t *testing.T) {
	mgr := newFakeManager()
	_, srv, ownerCookie := requestTestServer(t, mgr)

	rec := doReq(t, srv, ownerCookie, http.MethodPost, "/api/v1/requests", albumReqItem)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /requests (album) = %d: %s", rec.Code, rec.Body.String())
	}
	var req core.Request
	if err := json.NewDecoder(rec.Body).Decode(&req); err != nil {
		t.Fatal(err)
	}
	if req.Status != core.RequestApproved {
		t.Fatalf("want status=approved, got %q", req.Status)
	}
	if mgr.enqueueCalls != 1 {
		t.Fatalf("Enqueue called %d times, want 1", mgr.enqueueCalls)
	}
	if mgr.lastReq.Granularity != core.GranularityAlbum {
		t.Fatalf("enqueued request Granularity = %q, want %q", mgr.lastReq.Granularity, core.GranularityAlbum)
	}
	if mgr.lastReq.ExternalID != "album-123" {
		t.Fatalf("enqueued request ExternalID = %q, want album-123", mgr.lastReq.ExternalID)
	}
}

// TestAlbumRequestPendingNoEnqueue: a request-only user POSTing an album request
// gets a pending request and Enqueue is NOT called.
func TestAlbumRequestPendingNoEnqueue(t *testing.T) {
	mgr := newFakeManager()
	_, srv, ownerCookie := requestTestServer(t, mgr)

	// Create a requester user.
	rec := doReq(t, srv, ownerCookie, http.MethodPost, "/api/v1/users",
		`{"username":"albreq1","password":"pw123456","roleId":"role-requester"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user = %d: %s", rec.Code, rec.Body.String())
	}
	requesterTok := mustLogin(t, srv, "albreq1", "pw123456")
	requesterCookie := &http.Cookie{Name: sessionCookie, Value: requesterTok}

	rec = doReq(t, srv, requesterCookie, http.MethodPost, "/api/v1/requests", albumReqItem)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /requests (album, requester) = %d: %s", rec.Code, rec.Body.String())
	}
	var req core.Request
	if err := json.NewDecoder(rec.Body).Decode(&req); err != nil {
		t.Fatal(err)
	}
	if req.Status != core.RequestPending {
		t.Fatalf("want status=pending, got %q", req.Status)
	}
	if mgr.enqueueCalls != 0 {
		t.Fatalf("Enqueue called %d times for pending album request, want 0", mgr.enqueueCalls)
	}
}

// TestAlbumRequestDedup: a second identical album request returns the existing one
// with no additional Enqueue call.
func TestAlbumRequestDedup(t *testing.T) {
	mgr := newFakeManager()
	_, srv, ownerCookie := requestTestServer(t, mgr)

	rec := doReq(t, srv, ownerCookie, http.MethodPost, "/api/v1/requests", albumReqItem)
	if rec.Code != http.StatusOK {
		t.Fatalf("first album POST = %d: %s", rec.Code, rec.Body.String())
	}
	var r1 core.Request
	if err := json.NewDecoder(rec.Body).Decode(&r1); err != nil {
		t.Fatal(err)
	}
	callsAfterFirst := mgr.enqueueCalls

	rec = doReq(t, srv, ownerCookie, http.MethodPost, "/api/v1/requests", albumReqItem)
	if rec.Code != http.StatusOK {
		t.Fatalf("second album POST = %d: %s", rec.Code, rec.Body.String())
	}
	var r2 core.Request
	if err := json.NewDecoder(rec.Body).Decode(&r2); err != nil {
		t.Fatal(err)
	}
	if r1.ID != r2.ID {
		t.Fatalf("album dedup: got two different IDs: %s vs %s", r1.ID, r2.ID)
	}
	if mgr.enqueueCalls != callsAfterFirst {
		t.Fatalf("Enqueue called %d extra times on dup album request, want 0",
			mgr.enqueueCalls-callsAfterFirst)
	}
}

// ─── End album handler tests ────────────────────────────────────────────────────

// ─── Batch request handler tests ────────────────────────────────────────────────

const batchAlbumItem1 = `{"source":"lidarr","externalId":"album-a1","title":"Album One","artist":"Artist A","album":"Album One","kind":"album"}`
const batchAlbumItem2 = `{"source":"lidarr","externalId":"album-a2","title":"Album Two","artist":"Artist B","album":"Album Two","kind":"album"}`

// batchBody builds the JSON body for POST /requests/batch.
func batchBody(items ...string) string {
	var buf bytes.Buffer
	buf.WriteString(`{"items":[`)
	for i, item := range items {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.WriteString(item)
	}
	buf.WriteString(`]}`)
	return buf.String()
}

// batchResp is the response shape for POST /requests/batch.
type batchResp struct {
	Created  int            `json:"created"`
	Skipped  int            `json:"skipped"`
	Requests []core.Request `json:"requests"`
}

// TestBatchRequestAutoApprove: auto_approve user sends 2 distinct album items →
// both enqueued (GranularityAlbum), created==2, skipped==0.
func TestBatchRequestAutoApprove(t *testing.T) {
	mgr := newFakeManager()
	_, srv, ownerCookie := requestTestServer(t, mgr)

	body := batchBody(batchAlbumItem1, batchAlbumItem2)
	rec := doReq(t, srv, ownerCookie, http.MethodPost, "/api/v1/requests/batch", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /requests/batch (auto_approve) = %d: %s", rec.Code, rec.Body.String())
	}
	var resp batchResp
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Created != 2 {
		t.Fatalf("want created=2, got %d", resp.Created)
	}
	if resp.Skipped != 0 {
		t.Fatalf("want skipped=0, got %d", resp.Skipped)
	}
	if mgr.enqueueCalls != 2 {
		t.Fatalf("want 2 Enqueue calls, got %d", mgr.enqueueCalls)
	}
	if mgr.lastReq.Granularity != core.GranularityAlbum {
		t.Fatalf("last enqueue Granularity = %q, want album", mgr.lastReq.Granularity)
	}
	if len(resp.Requests) != 2 {
		t.Fatalf("want 2 requests in response, got %d", len(resp.Requests))
	}
}

// TestBatchRequestPendingUser: request-only user sends 2 distinct album items →
// 2 pending requests, 0 enqueues, created==2, skipped==0.
func TestBatchRequestPendingUser(t *testing.T) {
	mgr := newFakeManager()
	_, srv, ownerCookie := requestTestServer(t, mgr)

	// Create a requester user.
	rec := doReq(t, srv, ownerCookie, http.MethodPost, "/api/v1/users",
		`{"username":"batchreq1","password":"pw123456","roleId":"role-requester"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user = %d: %s", rec.Code, rec.Body.String())
	}
	reqTok := mustLogin(t, srv, "batchreq1", "pw123456")
	reqCookie := &http.Cookie{Name: sessionCookie, Value: reqTok}

	body := batchBody(batchAlbumItem1, batchAlbumItem2)
	rec = doReq(t, srv, reqCookie, http.MethodPost, "/api/v1/requests/batch", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /requests/batch (requester) = %d: %s", rec.Code, rec.Body.String())
	}
	var resp batchResp
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Created != 2 {
		t.Fatalf("want created=2, got %d", resp.Created)
	}
	if resp.Skipped != 0 {
		t.Fatalf("want skipped=0, got %d", resp.Skipped)
	}
	if mgr.enqueueCalls != 0 {
		t.Fatalf("want 0 Enqueue calls for pending user, got %d", mgr.enqueueCalls)
	}
	for _, r := range resp.Requests {
		if r.Status != core.RequestPending {
			t.Fatalf("want status=pending, got %q for request %s", r.Status, r.ID)
		}
	}
}

// TestBatchRequestDedup: batch includes an item that duplicates an already-open
// album request → that item skipped (counted), not re-enqueued; the new one created.
func TestBatchRequestDedup(t *testing.T) {
	mgr := newFakeManager()
	_, srv, ownerCookie := requestTestServer(t, mgr)

	// Pre-create the first album request via single endpoint.
	rec := doReq(t, srv, ownerCookie, http.MethodPost, "/api/v1/requests", batchAlbumItem1)
	if rec.Code != http.StatusOK {
		t.Fatalf("pre-create = %d: %s", rec.Code, rec.Body.String())
	}
	callsAfterFirst := mgr.enqueueCalls // 1 from auto-approve

	// Now batch with the same item (dup) + a new item.
	body := batchBody(batchAlbumItem1, batchAlbumItem2)
	rec = doReq(t, srv, ownerCookie, http.MethodPost, "/api/v1/requests/batch", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /requests/batch (dedup) = %d: %s", rec.Code, rec.Body.String())
	}
	var resp batchResp
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	// album-a1 is a dup → skipped; album-a2 is new → created
	if resp.Skipped != 1 {
		t.Fatalf("want skipped=1, got %d", resp.Skipped)
	}
	if resp.Created != 1 {
		t.Fatalf("want created=1, got %d", resp.Created)
	}
	// Only the new item should trigger an additional Enqueue.
	if mgr.enqueueCalls != callsAfterFirst+1 {
		t.Fatalf("want %d Enqueue calls total, got %d", callsAfterFirst+1, mgr.enqueueCalls)
	}
}

// TestBatchRequestEmpty: empty items list → {created:0, skipped:0, requests:[]}.
func TestBatchRequestEmpty(t *testing.T) {
	mgr := newFakeManager()
	_, srv, ownerCookie := requestTestServer(t, mgr)

	rec := doReq(t, srv, ownerCookie, http.MethodPost, "/api/v1/requests/batch", `{"items":[]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /requests/batch (empty) = %d: %s", rec.Code, rec.Body.String())
	}
	var resp batchResp
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Created != 0 || resp.Skipped != 0 {
		t.Fatalf("want created=0 skipped=0, got created=%d skipped=%d", resp.Created, resp.Skipped)
	}
}

// ─── End batch request handler tests ─────────────────────────────────────────────

// ─── Quota tests ─────────────────────────────────────────────────────────────────

// batchRespWithQuota is the expected response shape for POST /requests/batch
// once quota enforcement is in place.
type batchRespWithQuota struct {
	Created     int            `json:"created"`
	Skipped     int            `json:"skipped"`
	QuotaCapped int            `json:"quotaCapped"`
	Requests    []core.Request `json:"requests"`
}

// quotaTestServer builds a Server with a real store (temp SQLite), the request
// service, Adapters (for settings), and a download manager. Returns the store
// (so tests can write settings), the server, and the owner cookie.
func quotaTestServer(t *testing.T, mgr DownloadManager) (*store.Store, *Server, *http.Cookie) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/quota.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	authSvc, ownerTok := seededAuthToken(t, st)

	bus := events.New()
	reqSvc := request.NewService(st.Q(), bus, time.Now)

	srv := NewServer(Deps{
		Auth:       authSvc,
		Downloads:  mgr,
		Requests:   reqSvc,
		Adapters:   st.Q(),
		Search:     registry.NewRegistry("search"),
		Downloader: registry.NewRegistry("downloader"),
	})
	return st, srv, &http.Cookie{Name: sessionCookie, Value: ownerTok}
}

// setQuotaCap is a helper to PUT the maxPendingRequestsPerUser setting.
func setQuotaCap(t *testing.T, srv *Server, cookie *http.Cookie, cap int) {
	t.Helper()
	body := fmt.Sprintf(`{"maxPendingRequestsPerUser":%d}`, cap)
	rec := doReq(t, srv, cookie, http.MethodPut, "/api/v1/settings", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("setQuotaCap PUT /settings = %d: %s", rec.Code, rec.Body.String())
	}
}

// TestQuotaSingleRequest_AtCapReturns429: cap=2, requester already has 2 pending
// → POST /requests returns 429 with limit in body.
func TestQuotaSingleRequest_AtCapReturns429(t *testing.T) {
	mgr := newFakeManager()
	_, srv, ownerCookie := quotaTestServer(t, mgr)

	// Set cap=2 (admin).
	setQuotaCap(t, srv, ownerCookie, 2)

	// Create a requester user.
	rec := doReq(t, srv, ownerCookie, http.MethodPost, "/api/v1/users",
		`{"username":"quotareq1","password":"pw123456","roleId":"role-requester"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user = %d: %s", rec.Code, rec.Body.String())
	}
	reqTok := mustLogin(t, srv, "quotareq1", "pw123456")
	reqCookie := &http.Cookie{Name: sessionCookie, Value: reqTok}

	// Post 2 requests (distinct) → both should succeed.
	item1 := `{"source":"spotify","externalId":"q-sp1","title":"Song1","artist":"A","album":"Al","durationMs":200000}`
	item2 := `{"source":"spotify","externalId":"q-sp2","title":"Song2","artist":"A","album":"Al","durationMs":200000}`
	rec = doReq(t, srv, reqCookie, http.MethodPost, "/api/v1/requests", item1)
	if rec.Code != http.StatusOK {
		t.Fatalf("request 1 = %d: %s", rec.Code, rec.Body.String())
	}
	rec = doReq(t, srv, reqCookie, http.MethodPost, "/api/v1/requests", item2)
	if rec.Code != http.StatusOK {
		t.Fatalf("request 2 = %d: %s", rec.Code, rec.Body.String())
	}

	// 3rd distinct request → 429 (quota reached).
	item3 := `{"source":"spotify","externalId":"q-sp3","title":"Song3","artist":"A","album":"Al","durationMs":200000}`
	rec = doReq(t, srv, reqCookie, http.MethodPost, "/api/v1/requests", item3)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("at-cap request = %d, want 429: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error string `json:"error"`
		Limit int    `json:"limit"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode 429 body: %v", err)
	}
	if body.Limit != 2 {
		t.Fatalf("429 body limit = %d, want 2", body.Limit)
	}
	if body.Error == "" {
		t.Fatal("429 body error must not be empty")
	}
}

// TestQuotaSingleRequest_BelowCapSucceeds: cap=2, requester has 1 pending → still creates.
func TestQuotaSingleRequest_BelowCapSucceeds(t *testing.T) {
	mgr := newFakeManager()
	_, srv, ownerCookie := quotaTestServer(t, mgr)

	setQuotaCap(t, srv, ownerCookie, 2)

	rec := doReq(t, srv, ownerCookie, http.MethodPost, "/api/v1/users",
		`{"username":"quotareq2","password":"pw123456","roleId":"role-requester"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user = %d: %s", rec.Code, rec.Body.String())
	}
	reqTok := mustLogin(t, srv, "quotareq2", "pw123456")
	reqCookie := &http.Cookie{Name: sessionCookie, Value: reqTok}

	// Post 1 request.
	item1 := `{"source":"spotify","externalId":"q2-sp1","title":"Song1","artist":"A","album":"Al","durationMs":200000}`
	rec = doReq(t, srv, reqCookie, http.MethodPost, "/api/v1/requests", item1)
	if rec.Code != http.StatusOK {
		t.Fatalf("request 1 = %d: %s", rec.Code, rec.Body.String())
	}

	// 2nd request → below cap (1 < 2) → still succeeds.
	item2 := `{"source":"spotify","externalId":"q2-sp2","title":"Song2","artist":"A","album":"Al","durationMs":200000}`
	rec = doReq(t, srv, reqCookie, http.MethodPost, "/api/v1/requests", item2)
	if rec.Code != http.StatusOK {
		t.Fatalf("request 2 (below cap) = %d: %s", rec.Code, rec.Body.String())
	}
}

// TestQuotaZeroMeansUnlimited: cap=0 → no quota check, creates any number.
func TestQuotaZeroMeansUnlimited(t *testing.T) {
	mgr := newFakeManager()
	_, srv, ownerCookie := quotaTestServer(t, mgr)

	// cap=0 (default unlimited).
	setQuotaCap(t, srv, ownerCookie, 0)

	rec := doReq(t, srv, ownerCookie, http.MethodPost, "/api/v1/users",
		`{"username":"quotareq3","password":"pw123456","roleId":"role-requester"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user = %d: %s", rec.Code, rec.Body.String())
	}
	reqTok := mustLogin(t, srv, "quotareq3", "pw123456")
	reqCookie := &http.Cookie{Name: sessionCookie, Value: reqTok}

	// Post 5 distinct requests → all should succeed (no cap).
	for i := 0; i < 5; i++ {
		body := fmt.Sprintf(`{"source":"spotify","externalId":"q3-sp%d","title":"Song%d","artist":"A","album":"Al","durationMs":200000}`, i, i)
		rec = doReq(t, srv, reqCookie, http.MethodPost, "/api/v1/requests", body)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d with cap=0 = %d: %s", i, rec.Code, rec.Body.String())
		}
	}
}

// TestQuotaAutoApproveNotChecked: auto_approve user (owner) ignores quota even with cap=2
// and 5 prior approved requests.
func TestQuotaAutoApproveNotChecked(t *testing.T) {
	mgr := newFakeManager()
	_, srv, ownerCookie := quotaTestServer(t, mgr)

	setQuotaCap(t, srv, ownerCookie, 2)

	// Owner has auto_approve — post 5 requests, all should succeed.
	for i := 0; i < 5; i++ {
		body := fmt.Sprintf(`{"source":"spotify","externalId":"aa-sp%d","title":"Song%d","artist":"A","album":"Al","durationMs":200000}`, i, i)
		rec := doReq(t, srv, ownerCookie, http.MethodPost, "/api/v1/requests", body)
		if rec.Code != http.StatusOK {
			t.Fatalf("auto_approve request %d = %d: %s", i, rec.Code, rec.Body.String())
		}
	}
}

// TestQuotaBatch_CapEnforced: cap=2, non-auto_approve user, 0 pending, batch of 4 distinct
// → created==2, quotaCapped==2.
func TestQuotaBatch_CapEnforced(t *testing.T) {
	mgr := newFakeManager()
	_, srv, ownerCookie := quotaTestServer(t, mgr)

	setQuotaCap(t, srv, ownerCookie, 2)

	rec := doReq(t, srv, ownerCookie, http.MethodPost, "/api/v1/users",
		`{"username":"batchquota1","password":"pw123456","roleId":"role-requester"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user = %d: %s", rec.Code, rec.Body.String())
	}
	reqTok := mustLogin(t, srv, "batchquota1", "pw123456")
	reqCookie := &http.Cookie{Name: sessionCookie, Value: reqTok}

	bItem1 := `{"source":"lidarr","externalId":"bq-a1","title":"Album A1","artist":"Ar","album":"Al A1","kind":"album"}`
	bItem2 := `{"source":"lidarr","externalId":"bq-a2","title":"Album A2","artist":"Ar","album":"Al A2","kind":"album"}`
	bItem3 := `{"source":"lidarr","externalId":"bq-a3","title":"Album A3","artist":"Ar","album":"Al A3","kind":"album"}`
	bItem4 := `{"source":"lidarr","externalId":"bq-a4","title":"Album A4","artist":"Ar","album":"Al A4","kind":"album"}`

	body := batchBody(bItem1, bItem2, bItem3, bItem4)
	rec = doReq(t, srv, reqCookie, http.MethodPost, "/api/v1/requests/batch", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /requests/batch = %d: %s", rec.Code, rec.Body.String())
	}

	var resp batchRespWithQuota
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	if resp.Created != 2 {
		t.Fatalf("want created=2, got %d", resp.Created)
	}
	if resp.QuotaCapped != 2 {
		t.Fatalf("want quotaCapped=2, got %d", resp.QuotaCapped)
	}
	if resp.Skipped != 0 {
		t.Fatalf("want skipped=0, got %d", resp.Skipped)
	}
}

// TestQuotaBatch_AutoApproveNotCapped: auto_approve user batch of 4 → all created, quotaCapped==0.
func TestQuotaBatch_AutoApproveNotCapped(t *testing.T) {
	mgr := newFakeManager()
	_, srv, ownerCookie := quotaTestServer(t, mgr)

	setQuotaCap(t, srv, ownerCookie, 2)

	bItem1 := `{"source":"lidarr","externalId":"bq-aa1","title":"Album AA1","artist":"Ar","album":"Al AA1","kind":"album"}`
	bItem2 := `{"source":"lidarr","externalId":"bq-aa2","title":"Album AA2","artist":"Ar","album":"Al AA2","kind":"album"}`
	bItem3 := `{"source":"lidarr","externalId":"bq-aa3","title":"Album AA3","artist":"Ar","album":"Al AA3","kind":"album"}`
	bItem4 := `{"source":"lidarr","externalId":"bq-aa4","title":"Album AA4","artist":"Ar","album":"Al AA4","kind":"album"}`

	body := batchBody(bItem1, bItem2, bItem3, bItem4)
	rec := doReq(t, srv, ownerCookie, http.MethodPost, "/api/v1/requests/batch", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /requests/batch (auto_approve) = %d: %s", rec.Code, rec.Body.String())
	}

	var resp batchRespWithQuota
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Created != 4 {
		t.Fatalf("want created=4, got %d", resp.Created)
	}
	if resp.QuotaCapped != 0 {
		t.Fatalf("want quotaCapped=0 for auto_approve, got %d", resp.QuotaCapped)
	}
}

// ─── End quota tests ──────────────────────────────────────────────────────────────

// TestDownloadReqFromItemTrackKind verifies that a track/empty Kind yields GranularityTrack.
func TestDownloadReqFromItemTrackKind(t *testing.T) {
	item := core.RequestItem{
		Source:     "spotify",
		ExternalID: "track-abc",
		Title:      "Song",
		Artist:     "Artist",
		Kind:       "track",
	}
	got := downloadReqFromItem(item, "user-1")
	if got.Granularity != core.GranularityTrack {
		t.Fatalf("want Granularity=%q, got %q", core.GranularityTrack, got.Granularity)
	}

	// empty kind also resolves to track
	itemEmpty := item
	itemEmpty.Kind = ""
	got2 := downloadReqFromItem(itemEmpty, "user-1")
	if got2.Granularity != core.GranularityTrack {
		t.Fatalf("empty Kind: want Granularity=%q, got %q", core.GranularityTrack, got2.Granularity)
	}
}
