package api

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/maxjb-xyz/reverb/internal/auth"
	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/discord"
	"github.com/maxjb-xyz/reverb/internal/matching"
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
	if errors.Is(err, auth.ErrNoUser) {
		return discord.RequestOutcome{Status: discord.RequestDenied}, nil
	}
	if err != nil {
		return discord.RequestOutcome{}, err
	}
	if !cu.Has(auth.CapRequest) {
		return discord.RequestOutcome{Status: discord.RequestDenied}, nil
	}
	agg := s.searchAggregator()
	if agg == nil {
		return discord.RequestOutcome{Status: discord.RequestUnavailable}, nil
	}
	best, found, owned, searched := bestOnlineTrack(ctx, agg, query)
	if !searched {
		return discord.RequestOutcome{Status: discord.RequestSearchFailed}, nil
	}
	if !found {
		return discord.RequestOutcome{Status: discord.RequestNotFound}, nil
	}
	out := discord.RequestOutcome{Title: best.Title, Artist: best.Artist}
	if owned {
		out.Status = discord.RequestOwned
		return out, nil
	}
	// Only request what the library is known not to have: when the check
	// failed (library down) the web lets the person judge, the bot can't.
	if best.Match == nil || best.Match.Status != core.MatchNotInLibrary {
		out.Status = discord.RequestUnchecked
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

// bestOnlineTrack waits for every source and takes the top track (not
// playlist or album) of the first source in name order, so the same query
// requests the same item every time. owned is true when that song (same
// ISRC, or same title and artist) is in the library per any source.
// searched is false when no source answered.
func bestOnlineTrack(ctx context.Context, agg Streamer, q string) (best core.ExternalResult, found, owned, searched bool) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var envs []search.Envelope
	for env := range agg.Stream(ctx, q, core.EntityTrack) {
		if env.Status != search.StatusOK {
			continue
		}
		searched = true
		envs = append(envs, env)
	}
	sort.SliceStable(envs, func(i, j int) bool { return envs[i].Source < envs[j].Source })
	var tracks []core.ExternalResult
	for _, env := range envs {
		for _, r := range env.Results {
			if r.Type == core.EntityTrack || r.Type == "" {
				tracks = append(tracks, r)
			}
		}
	}
	if len(tracks) == 0 {
		return best, false, false, searched
	}
	best, found = tracks[0], true
	for _, t := range tracks {
		if t.Match != nil && t.Match.Status == core.MatchInLibrary && sameSong(t, best) {
			return best, true, true, true
		}
	}
	return best, true, false, true
}

func sameSong(a, b core.ExternalResult) bool {
	if a.ISRC != "" && a.ISRC == b.ISRC {
		return true
	}
	return matching.Normalize(a.Title) == matching.Normalize(b.Title) &&
		matching.Normalize(a.Artist) == matching.Normalize(b.Artist)
}
