package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/play"
)

type statsMetadataProvider interface {
	GetTrack(context.Context, string, string) (core.ExternalResult, error)
	GetArtist(context.Context, string, string) (core.ExternalArtist, error)
	GetAlbum(context.Context, string, string) (core.ExternalAlbum, error)
}

// enrichStatsRows restores source-specific navigation and artwork for durable
// catalog rows. Stats aggregation only stores text plus a representative track;
// direct source lookup supplies the artist/album IDs needed by full pages.
func (s *Server) enrichStatsRows(ctx context.Context, rows []play.TopRow, kind string) {
	provider, ok := s.searchAggregator().(statsMetadataProvider)
	if !ok {
		return
	}
	for i := range rows {
		row := &rows[i]
		if row.Source == "" || row.ExternalID == "" {
			continue
		}
		lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		track, err := provider.GetTrack(lookupCtx, row.Source, row.ExternalID)
		cancel()
		if err != nil {
			continue
		}
		row.CoverURL = track.CoverURL
		row.ArtistExternalID = track.ArtistExternalID
		row.AlbumExternalID = track.AlbumExternalID
		if kind == "artist" && row.ArtistExternalID != "" {
			lookupCtx, cancel = context.WithTimeout(ctx, 3*time.Second)
			artist, err := provider.GetArtist(lookupCtx, row.Source, row.ArtistExternalID)
			cancel()
			if err == nil && artist.CoverURL != "" {
				row.CoverURL = artist.CoverURL
			}
		}
		if kind == "album" && row.AlbumExternalID != "" {
			lookupCtx, cancel = context.WithTimeout(ctx, 3*time.Second)
			album, err := provider.GetAlbum(lookupCtx, row.Source, row.AlbumExternalID)
			cancel()
			if err == nil && album.CoverURL != "" {
				row.CoverURL = album.CoverURL
			}
		}
	}
}

// playCountsRequest is the POST /stats/play-counts request body. The FE sends
// library-track identities (it has no catalog ids); the server resolves each
// WITHOUT minting. The user is taken from the session, NEVER from this body.
type playCountsRequest struct {
	Tracks []playCountTrack `json:"tracks"`
}

// playCountTrack is one track identity in a play-counts request. Key is an opaque
// caller handle echoed back as the key in the response counts map.
type playCountTrack struct {
	Key        string `json:"key"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Album      string `json:"album"`
	ISRC       string `json:"isrc"`
	DurationMs int    `json:"durationMs"`
}

// playCountsResponse is the POST /stats/play-counts response: counts maps each
// request track's key to its all-time play count for the session user.
type playCountsResponse struct {
	Counts map[string]int `json:"counts"`
}

const (
	// defaultTo is a far-future unix-second sentinel used when the caller omits
	// the "to" query parameter. 2^31-1 (year 2038) is well beyond practical use
	// and avoids overflow on 32-bit sqlite integer columns.
	statsDefaultTo int64 = 2_000_000_000
	// defaultLimit is the default limit for top/recent endpoints.
	statsDefaultLimit = 50
	// maxLimit caps the limit parameter to prevent runaway queries.
	statsMaxLimit = 200
)

// parseFrom parses the "from" query param as int64, defaulting to 0.
func parseFrom(r *http.Request) int64 {
	if s := r.URL.Query().Get("from"); s != "" {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			return v
		}
	}
	return 0
}

// parseTo parses the "to" query param as int64, defaulting to statsDefaultTo.
func parseTo(r *http.Request) int64 {
	if s := r.URL.Query().Get("to"); s != "" {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			return v
		}
	}
	return statsDefaultTo
}

// parseLimit parses the "limit" query param as int, clamping to [1, statsMaxLimit].
func parseLimit(r *http.Request) int {
	if s := r.URL.Query().Get("limit"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 {
			if v > statsMaxLimit {
				return statsMaxLimit
			}
			return v
		}
	}
	return statsDefaultLimit
}

// nilStats returns 503 when Deps.Stats is not wired in.
func (s *Server) nilStats(w http.ResponseWriter) bool {
	if s.deps.Stats == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "stats service unavailable"})
		return true
	}
	return false
}

// handleStatsSummary serves GET /api/v1/stats/summary?from&to
func (s *Server) handleStatsSummary(w http.ResponseWriter, r *http.Request) {
	if s.nilStats(w) {
		return
	}
	cu, ok := currentUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	from, to := parseFrom(r), parseTo(r)
	result, err := s.deps.Stats.Summary(r.Context(), cu.ID, from, to)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleStatsTopTracks serves GET /api/v1/stats/top/tracks?from&to&limit
func (s *Server) handleStatsTopTracks(w http.ResponseWriter, r *http.Request) {
	if s.nilStats(w) {
		return
	}
	cu, ok := currentUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	from, to := parseFrom(r), parseTo(r)
	limit := parseLimit(r)
	result, err := s.deps.Stats.TopTracks(r.Context(), cu.ID, from, to, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.enrichStatsRows(r.Context(), result, "track")
	writeJSON(w, http.StatusOK, result)
}

// handleStatsTopArtists serves GET /api/v1/stats/top/artists?from&to&limit
func (s *Server) handleStatsTopArtists(w http.ResponseWriter, r *http.Request) {
	if s.nilStats(w) {
		return
	}
	cu, ok := currentUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	from, to := parseFrom(r), parseTo(r)
	limit := parseLimit(r)
	result, err := s.deps.Stats.TopArtists(r.Context(), cu.ID, from, to, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.enrichStatsRows(r.Context(), result, "artist")
	writeJSON(w, http.StatusOK, result)
}

// handleStatsTopAlbums serves GET /api/v1/stats/top/albums?from&to&limit
func (s *Server) handleStatsTopAlbums(w http.ResponseWriter, r *http.Request) {
	if s.nilStats(w) {
		return
	}
	cu, ok := currentUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	from, to := parseFrom(r), parseTo(r)
	limit := parseLimit(r)
	result, err := s.deps.Stats.TopAlbums(r.Context(), cu.ID, from, to, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.enrichStatsRows(r.Context(), result, "album")
	writeJSON(w, http.StatusOK, result)
}

// handleStatsTimeline serves GET /api/v1/stats/timeline?from&to&bucket
func (s *Server) handleStatsTimeline(w http.ResponseWriter, r *http.Request) {
	if s.nilStats(w) {
		return
	}
	cu, ok := currentUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	from, to := parseFrom(r), parseTo(r)
	bucket := r.URL.Query().Get("bucket")
	if bucket == "" {
		bucket = "day"
	}
	result, err := s.deps.Stats.Timeline(r.Context(), cu.ID, from, to, bucket)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleStatsClock serves GET /api/v1/stats/clock?from&to&tzOffsetMinutes
func (s *Server) handleStatsClock(w http.ResponseWriter, r *http.Request) {
	if s.nilStats(w) {
		return
	}
	cu, ok := currentUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	from, to := parseFrom(r), parseTo(r)
	tzOffsetMin := 0
	if s := r.URL.Query().Get("tzOffsetMinutes"); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			tzOffsetMin = v
		}
	}
	result, err := s.deps.Stats.Clock(r.Context(), cu.ID, from, to, tzOffsetMin)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleStatsRecent serves GET /api/v1/stats/recent?before&limit
func (s *Server) handleStatsRecent(w http.ResponseWriter, r *http.Request) {
	if s.nilStats(w) {
		return
	}
	cu, ok := currentUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	before := statsDefaultTo
	if bs := r.URL.Query().Get("before"); bs != "" {
		if v, err := strconv.ParseInt(bs, 10, 64); err == nil {
			before = v
		}
	}
	limit := parseLimit(r)
	result, err := s.deps.Stats.Recent(r.Context(), cu.ID, before, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleStatsEntity serves GET /api/v1/stats/entity?kind&id&from&to
func (s *Server) handleStatsEntity(w http.ResponseWriter, r *http.Request) {
	if s.nilStats(w) {
		return
	}
	cu, ok := currentUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	kind := r.URL.Query().Get("kind")
	id := r.URL.Query().Get("id")
	if kind == "" || id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "kind and id are required"})
		return
	}
	// An album is identified by NAME (id) + ARTIST, because album titles collide
	// across artists, so kind=album requires the additional "artist" query param.
	artist := r.URL.Query().Get("artist")
	if kind == "album" && artist == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "artist is required for kind=album"})
		return
	}
	from, to := parseFrom(r), parseTo(r)
	result, err := s.deps.Stats.Entity(r.Context(), cu.ID, kind, id, artist, from, to)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handlePlayCounts serves POST /api/v1/stats/play-counts.
// It resolves each requested track identity to a play count scoped to the
// SESSION user (cu.ID, never the body) and responds {"counts":{<key>:<count>}}.
// Identities are resolved lookup-only — a never-played track counts 0 and no
// catalog entity is minted.
func (s *Server) handlePlayCounts(w http.ResponseWriter, r *http.Request) {
	if s.deps.Play == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "play service unavailable"})
		return
	}
	cu, ok := currentUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req playCountsRequest
	if err := decode(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	items := make([]play.PlayCountQuery, len(req.Tracks))
	for i, t := range req.Tracks {
		items[i] = play.PlayCountQuery{
			Key:        t.Key,
			Title:      t.Title,
			Artist:     t.Artist,
			Album:      t.Album,
			ISRC:       t.ISRC,
			DurationMs: t.DurationMs,
		}
	}

	counts, err := s.deps.Play.PlayCounts(r.Context(), cu.ID, items)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, playCountsResponse{Counts: counts})
}
