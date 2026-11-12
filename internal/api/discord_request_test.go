package api

import (
	"context"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/discord"
	"github.com/maxjb-xyz/reverb/internal/events"
	"github.com/maxjb-xyz/reverb/internal/request"
	"github.com/maxjb-xyz/reverb/internal/search"
	"github.com/maxjb-xyz/reverb/internal/store"
)

func TestDiscordRequestUsesTheUsersRole(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/dr.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	authSvc, ownerTok := seededAuthToken(t, st)
	owner, _ := authSvc.ResolveSession(ctx, ownerTok)
	listenRole, err := authSvc.CreateRole(ctx, "Listener", []string{"request"})
	if err != nil {
		t.Fatal(err)
	}
	noneRole, _ := authSvc.CreateRole(ctx, "Guest", []string{})
	listener, err := authSvc.CreateUser(ctx, "lis", "pw123456", listenRole)
	if err != nil {
		t.Fatal(err)
	}
	guest, _ := authSvc.CreateUser(ctx, "gue", "pw123456", noneRole)

	mgr := newFakeManager()
	agg := fakeAgg{envs: []search.Envelope{
		{Source: "deezer", Status: search.StatusOK, Results: []core.ExternalResult{{Source: "deezer", ExternalID: "dz1", Title: "New Song", Artist: "Band", Type: core.EntityTrack}}},
	}}
	srv := NewServer(Deps{
		Auth: authSvc, Downloads: mgr, SearchAggregator: agg,
		Requests: request.NewService(st.Q(), events.New(), time.Now),
	})
	r := srv.DiscordRequester()

	got := func(user string) string {
		t.Helper()
		out, err := r.Request(ctx, user, "new song band")
		if err != nil {
			t.Fatal(err)
		}
		return out.Status
	}
	if s := got(guest); s != discord.RequestDenied {
		t.Fatalf("guest: %s", s)
	}
	if s := got("missing"); s != discord.RequestDenied {
		t.Fatalf("unknown user: %s", s)
	}
	if s := got(listener); s != discord.RequestPending {
		t.Fatalf("listener: %s", s)
	}
	if mgr.enqueueCalls != 0 {
		t.Fatal("a pending request was downloaded")
	}
	if s := got(listener); s != discord.RequestExists {
		t.Fatalf("listener again: %s", s)
	}
	if s := got(owner.ID); s != discord.RequestDownloading || mgr.enqueueCalls != 1 {
		t.Fatalf("owner: %s, enqueued %d", s, mgr.enqueueCalls)
	}

	// Songs already in the library aren't requested.
	srv2 := NewServer(Deps{
		Auth: authSvc, Downloads: mgr, Requests: request.NewService(st.Q(), events.New(), time.Now),
		SearchAggregator: fakeAgg{envs: []search.Envelope{
			{Source: "deezer", Status: search.StatusOK, Results: []core.ExternalResult{{Source: "deezer", ExternalID: "dz2", Title: "Old", Artist: "Band"}}},
			{Source: "spotify", Status: search.StatusOK, Results: []core.ExternalResult{{Source: "spotify", ExternalID: "sp2", Title: "Old", Artist: "Band", Match: &core.MatchResult{Status: core.MatchInLibrary}}}},
		}},
	})
	if out, _ := srv2.DiscordRequester().Request(ctx, owner.ID, "old"); out.Status != discord.RequestOwned || out.Title != "Old" {
		t.Fatalf("owned: %+v", out)
	}
	srv3 := NewServer(Deps{Auth: authSvc, Downloads: mgr, Requests: request.NewService(st.Q(), events.New(), time.Now), SearchAggregator: fakeAgg{}})
	if out, _ := srv3.DiscordRequester().Request(ctx, owner.ID, "zzz"); out.Status != discord.RequestNotFound {
		t.Fatalf("not found: %+v", out)
	}
}
