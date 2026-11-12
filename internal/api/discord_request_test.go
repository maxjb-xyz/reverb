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
		{Source: "deezer", Status: search.StatusOK, Results: []core.ExternalResult{{Source: "deezer", ExternalID: "dz1", Title: "New Song", Artist: "Band", Type: core.EntityTrack, Match: &core.MatchResult{Status: core.MatchNotInLibrary}}}},
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
	// Only tracks are requested (Spotify adds playlists to track searches),
	// the same query picks the same source each time, and an owned copy of
	// the chosen song from another source counts.
	notIn := &core.MatchResult{Status: core.MatchNotInLibrary}
	srv4 := NewServer(Deps{
		Auth: authSvc, Downloads: mgr, Requests: request.NewService(st.Q(), events.New(), time.Now),
		SearchAggregator: fakeAgg{envs: []search.Envelope{
			{Source: "spotify", Status: search.StatusOK, Results: []core.ExternalResult{
				{Source: "spotify", ExternalID: "pl1", Title: "Chill Mix", Type: core.EntityPlaylist},
				{Source: "spotify", ExternalID: "sp9", Title: "Rare", Artist: "Band", Type: core.EntityTrack, Match: notIn},
			}},
			{Source: "deezer", Status: search.StatusTimeout},
		}},
	})
	if out, _ := srv4.DiscordRequester().Request(ctx, listener, "rare"); out.Status != discord.RequestPending || out.Title != "Rare" {
		t.Fatalf("playlist first: %+v", out)
	}
	srv5 := NewServer(Deps{
		Auth: authSvc, Downloads: mgr, Requests: request.NewService(st.Q(), events.New(), time.Now),
		SearchAggregator: fakeAgg{envs: []search.Envelope{
			{Source: "spotify", Status: search.StatusOK, Results: []core.ExternalResult{{Source: "spotify", ExternalID: "sp5", Title: "Maybe", Artist: "Band", Match: &core.MatchResult{Status: core.MatchUnknown}}}},
		}},
	})
	before := mgr.enqueueCalls
	if out, _ := srv5.DiscordRequester().Request(ctx, owner.ID, "maybe"); out.Status != discord.RequestUnchecked || mgr.enqueueCalls != before {
		t.Fatalf("unknown match: %+v", out)
	}
	srv6 := NewServer(Deps{
		Auth: authSvc, Downloads: mgr, Requests: request.NewService(st.Q(), events.New(), time.Now),
		SearchAggregator: fakeAgg{envs: []search.Envelope{{Source: "deezer", Status: search.StatusError}, {Source: "spotify", Status: search.StatusTimeout}}},
	})
	if out, _ := srv6.DiscordRequester().Request(ctx, owner.ID, "x"); out.Status != discord.RequestSearchFailed {
		t.Fatalf("all failed: %+v", out)
	}
	srv3 := NewServer(Deps{Auth: authSvc, Downloads: mgr, Requests: request.NewService(st.Q(), events.New(), time.Now), SearchAggregator: fakeAgg{envs: []search.Envelope{{Source: "deezer", Status: search.StatusOK}}}})
	if out, _ := srv3.DiscordRequester().Request(ctx, owner.ID, "zzz"); out.Status != discord.RequestNotFound {
		t.Fatalf("not found: %+v", out)
	}
}
