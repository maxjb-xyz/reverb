package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxjb-xyz/reverb/internal/auth"
	"github.com/maxjb-xyz/reverb/internal/playlistsync"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store"
	"github.com/maxjb-xyz/reverb/internal/wiring"
)

// TestDownloadRequiresCapability verifies that a user whose role lacks
// auto_approve (role-requester) is rejected with 403 on POST /downloads, while
// the capability gate runs before the handler (no Manager needed).
func TestDownloadRequiresCapability(t *testing.T) {
	srv := newTestServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")
	otok := mustLogin(t, srv, "owner", "pw12345")
	doPOST(t, srv, "/api/v1/users", otok, `{"username":"req","password":"reqpw123","roleId":"role-requester"}`)
	rtok := mustLogin(t, srv, "req", "reqpw123")
	// requester lacks auto_approve → must 403
	if rec := doPOST(t, srv, "/api/v1/downloads", rtok, `{"source":"spotify","externalId":"x","title":"t","artist":"a"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("requester download = %d, want 403: %s", rec.Code, rec.Body.String())
	}
}

// realSyncServer builds a Server backed by a real DB-backed playlistsync.Service
// + PlaylistOwnerStore over a freshly migrated+seeded store, so playlist creates
// actually persist with an owner and the owner-scoped list reads the real DB.
// Returns the server; callers complete setup + login via the public API.
func realSyncServer(t *testing.T) *Server {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/realsync.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	authSvc := auth.NewService(st.Q(), time.Now)
	if err := authSvc.EnsureSeed(context.Background()); err != nil {
		t.Fatal(err)
	}
	syncStore := wiring.NewSyncStore(st.Q())
	now := func() int64 { return time.Now().Unix() }
	// src/match/dl/lib are nil: CreateManaged + List only touch the store.
	svc := playlistsync.NewService(nil, nil, nil, syncStore, nil, now, uuid.NewString, nil)
	return NewServer(Deps{
		Auth:          authSvc,
		Sync:          svc,
		PlaylistOwner: st.Q(),
		Search:        registry.NewRegistry("search"),
		Downloader:    registry.NewRegistry("downloader"),
	})
}

// TestPlaylistsScopedToOwner verifies a non-admin user's playlist is not visible
// in another user's (the owner's) GET /playlists list.
func TestPlaylistsScopedToOwner(t *testing.T) {
	srv := realSyncServer(t)
	mustSetupOwner(t, srv, "owner", "pw12345")
	otok := mustLogin(t, srv, "owner", "pw12345")
	doPOST(t, srv, "/api/v1/users", otok, `{"username":"bob","password":"bobpw123","roleId":"role-user"}`)
	btok := mustLogin(t, srv, "bob", "bobpw123")
	// bob creates a playlist
	if rec := doPOST(t, srv, "/api/v1/playlists", btok, `{"name":"Bobs Mix"}`); rec.Code != http.StatusCreated {
		t.Fatalf("bob create = %d: %s", rec.Code, rec.Body.String())
	}
	// the owner's list must NOT include bob's playlist
	rr := doGET(t, srv, "/api/v1/playlists", otok)
	if bytesContain(rr.Body.Bytes(), "Bobs Mix") {
		t.Fatalf("owner list leaked bob's playlist: %s", rr.Body.String())
	}
}
