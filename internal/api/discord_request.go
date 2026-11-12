package api

import (
	"context"
	"errors"
	"time"

	"github.com/maxjb-xyz/reverb/internal/auth"
	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/discord"
	"github.com/maxjb-xyz/reverb/internal/search"
)

// DiscordRequester lets the Discord bot's /request create a request as a
// linked Reverb user, through the same path (role, quota, auto-approve) as
// the web.
type DiscordRequester struct{ s *Server }

// DiscordRequester returns the bot's view of requests.
func (s *Server) DiscordRequester() *DiscordRequester { return &DiscordRequester{s: s} }

// Request implements discord.Requester.
func (d *DiscordRequester) Request(ctx context.Context, userID, query string) (discord.RequestOutcome, error) {
	s := d.s
	if s.deps.Auth == nil || s.deps.Requests == nil {
		return discord.RequestOutcome{Status: discord.RequestUnavailable}, nil
	}
	cu, err := s.deps.Auth.UserByID(ctx, userID)
	if err != nil {
		return discord.RequestOutcome{Status: discord.RequestDenied}, nil
	}
	if !cu.Has(auth.CapRequest) {
		return discord.RequestOutcome{Status: discord.RequestDenied}, nil
	}
	agg := s.searchAggregator()
	if agg == nil {
		return discord.RequestOutcome{Status: discord.RequestUnavailable}, nil
	}
	best, ok := bestOnlineTrack(ctx, agg, query)
	if !ok {
		return discord.RequestOutcome{Status: discord.RequestNotFound}, nil
	}
	out := discord.RequestOutcome{Title: best.Title, Artist: best.Artist}
	if best.Match != nil && best.Match.Status == core.MatchInLibrary {
		out.Status = discord.RequestOwned
		return out, nil
	}
	_, created, err := s.createOneRequest(ctx, cu, core.RequestItem{
		Source: best.Source, ExternalID: best.ExternalID, Title: best.Title, Artist: best.Artist,
		Album: best.Album, ISRC: best.ISRC, DurationMs: best.DurationMs,
		CoverArtID: best.CoverArtID, CoverUrl: best.CoverURL, Kind: "track",
	})
	switch {
	case errors.Is(err, errQuotaReached):
		out.Status = discord.RequestQuota
	case errors.Is(err, errNoDownloader):
		out.Status = discord.RequestUnavailable
	case err != nil:
		return out, err
	case !created:
		out.Status = discord.RequestExists
	case cu.Has(auth.CapAutoApprove):
		out.Status = discord.RequestDownloading
	default:
		out.Status = discord.RequestPending
	}
	return out, nil
}

// bestOnlineTrack searches every source and takes the top track, preferring
// one already in the library (so nobody downloads a song twice).
func bestOnlineTrack(ctx context.Context, agg Streamer, q string) (core.ExternalResult, bool) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var first *core.ExternalResult
	for env := range agg.Stream(ctx, q, core.EntityTrack) {
		if env.Status != search.StatusOK || len(env.Results) == 0 {
			continue
		}
		top := env.Results[0]
		if top.Match != nil && top.Match.Status == core.MatchInLibrary {
			return top, true
		}
		if first == nil {
			first = &top
		}
	}
	if first == nil {
		return core.ExternalResult{}, false
	}
	return *first, true
}
