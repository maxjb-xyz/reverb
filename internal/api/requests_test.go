package api

import (
	"bytes"
	"encoding/json"
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
		`{"username":"req1","password":"pw","roleId":"role-requester"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user = %d: %s", rec.Code, rec.Body.String())
	}

	requesterTok := mustLogin(t, srv, "req1", "pw")
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
		`{"username":"req2","password":"pw","roleId":"role-requester"}`)
	reqTok := mustLogin(t, srv, "req2", "pw")
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
		`{"username":"req3","password":"pw","roleId":"role-requester"}`)
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

	reqTok := mustLogin(t, srv, "req3", "pw")
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
		`{"username":"req4","password":"pw","roleId":"role-requester"}`)
	reqTok := mustLogin(t, srv, "req4", "pw")
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
		`{"username":"req5","password":"pw","roleId":"role-requester"}`)
	reqTok := mustLogin(t, srv, "req5", "pw")
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
		`{"username":"req6","password":"pw","roleId":"role-requester"}`)
	doReq(t, srv, ownerCookie, http.MethodPost, "/api/v1/users",
		`{"username":"req7","password":"pw","roleId":"role-requester"}`)

	req6Tok := mustLogin(t, srv, "req6", "pw")
	req7Tok := mustLogin(t, srv, "req7", "pw")
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
		`{"username":"reqgate","password":"pw","roleId":"role-requester"}`)
	reqTok := mustLogin(t, srv, "reqgate", "pw")
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
