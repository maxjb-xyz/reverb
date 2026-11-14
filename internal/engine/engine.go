// Package engine is Reverb's recommendation engine. It answers one question,
// "what should play next?", for radio stations, the DJ's lanes and (later)
// mixes and Discover Weekly.
//
// The shape follows SUB/WAVE's picker, minus the LLM: a pool of candidates is
// drawn from several capped sources (the seed's artist, artists fans of it also
// play, the same genre, the same era, the user's favourites...), hard rules
// drop what must not play (thumbed down, excluded, just heard), the taste
// profile scores what is left, and a weighted draw with per-artist and
// per-album caps picks the batch. The draw keeps stations from replaying the
// same top picks every time: better-scored songs are likelier, not certain.
//
// Every candidate carries a structured Reason so the UI and the DJ can say why
// it was picked ("because you play Bonobo a lot").
package engine

import (
	"context"
	"errors"
	"github.com/maxjb-xyz/reverb/internal/analysis"
	"maps"
	"math"
	"math/rand"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/libindex"
	"github.com/maxjb-xyz/reverb/internal/matching"
	"github.com/maxjb-xyz/reverb/internal/search"
	"github.com/maxjb-xyz/reverb/internal/store/db"
	"github.com/maxjb-xyz/reverb/internal/taste"
)

// Seed kinds.
const (
	SeedTrack  = "track"  // ID: library track id
	SeedArtist = "artist" // ID: artist key (or Artist name)
	SeedGenre  = "genre"  // ID: genre key
	SeedTaste  = "taste"  // the user's own taste, no seed
)

// Lanes are the DJ's (and later mixes') recipes. "" is plain radio.
const (
	LaneRadio      = ""
	LaneOnRepeat   = "on-repeat"   // what the user plays most right now
	LaneThrowback  = "throwback"   // a period: FromYear..ToYear
	LaneDeepCuts   = "deep-cuts"   // favourite artists' songs the user rarely plays
	LaneSoundsLike = "sounds-like" // artists similar to the seed, not the seed itself
	LaneNewToYou   = "new-to-you"  // never-played songs from nearby artists
	LaneFreshFinds = "fresh-finds" // recently added to the library
)

// Lanes lists the valid lane names.
var Lanes = map[string]bool{
	LaneRadio: true, LaneOnRepeat: true, LaneThrowback: true, LaneDeepCuts: true,
	LaneSoundsLike: true, LaneNewToYou: true, LaneFreshFinds: true,
}

// Reason kinds.
const (
	ReasonSeed          = "seed"           // the seed itself (track radio starts with it)
	ReasonSeedArtist    = "seed_artist"    // by the seed artist
	ReasonSimilarArtist = "similar_artist" // fans of the seed artist also play this artist
	ReasonGenre         = "genre"          // same genre as the seed
	ReasonEra           = "era"            // same era as the seed
	ReasonFavorite      = "favorite"       // an artist the user plays a lot
	ReasonOnRepeat      = "on_repeat"      // one of the user's most-played songs
	ReasonDeepCut       = "deep_cut"       // favourite artist, rarely played song
	ReasonNewToYou      = "new_to_you"     // never played
	ReasonFresh         = "fresh"          // recently added
	ReasonThrowback     = "throwback"      // from the requested period
	ReasonDiscovery     = "discovery"      // not in the library: a song to get
	ReasonLibrary       = "library"        // filler from the wider library
	ReasonSound         = "sounds_like"    // sounds like the seed (optional audio analysis)
)

// ErrInvalid is returned for a request the engine can't serve (bad lane, no seed).
var ErrInvalid = errors.New("engine: invalid request")

// ErrNotFound is returned when the seed isn't in the library index.
var ErrNotFound = errors.New("engine: seed not found")

// Seed says what a station is built around.
type Seed struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
	// Title/Artist identify a track seed that has no library id (or describe
	// an artist seed by name).
	Title  string `json:"title,omitempty"`
	Artist string `json:"artist,omitempty"`
}

// Request asks for the next n songs of a station or lane.
type Request struct {
	Seed Seed
	Lane string
	// FromYear..ToYear is the period for throwback; on other lanes, when
	// set, it keeps every pick inside that period.
	FromYear int
	ToYear   int
	// Exclude holds library track ids (or catalog ids) that must not be picked:
	// the queue so far, what the DJ already played.
	Exclude []string
	// IncludeExternal lets songs that aren't in the library into the batch
	// (Discover, the DJ's fresh-finds). Radio leaves it off: it plays now.
	IncludeExternal bool
	N               int
}

// Reason says why a song was picked. Fields beyond Kind are filled when they
// apply.
type Reason struct {
	Kind       string `json:"kind"`
	Artist     string `json:"artist,omitempty"`
	ArtistKey  string `json:"artistKey,omitempty"`
	SeedArtist string `json:"seedArtist,omitempty"` // for similar_artist: whose fans; for sounds_like: the seed artist
	SeedTitle  string `json:"seedTitle,omitempty"`  // for sounds_like from a song: that song
	Genre      string `json:"genre,omitempty"`
	Year       int    `json:"year,omitempty"`
	PlayCount  int    `json:"playCount,omitempty"`
}

// Candidate is one recommended song.
type Candidate struct {
	Owned bool `json:"owned"`
	// Track is set for songs in the library (playable now).
	Track *core.Track `json:"track,omitempty"`
	// External is set for songs not in the library.
	External *core.ExternalResult `json:"external,omitempty"`
	Year     int                  `json:"year,omitempty"`
	Genres   string               `json:"genres,omitempty"`
	Score    float64              `json:"score"`
	Reason   Reason               `json:"reason"`

	artistKey string
	albumKey  string
	base      float64
}

// Station describes what a request resolved to, for display.
type Station struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Lane  string `json:"lane,omitempty"`
	Label string `json:"label"`
}

// Result is a batch of picks.
type Result struct {
	Station Station     `json:"station"`
	Items   []Candidate `json:"items"`
}

// Querier is the slice of the store the engine reads.
type Querier interface {
	GetLibraryTrack(ctx context.Context, id string) (db.LibraryTrack, error)
	ListLibraryTracksByIDs(ctx context.Context, ids []string) ([]db.LibraryTrack, error)
	ListLibraryTracksByArtistKeys(ctx context.Context, keys []string) ([]db.LibraryTrack, error)
	ListLibraryTracksByGenre(ctx context.Context, arg db.ListLibraryTracksByGenreParams) ([]db.LibraryTrack, error)
	ListLibraryTracksByYearRange(ctx context.Context, arg db.ListLibraryTracksByYearRangeParams) ([]db.LibraryTrack, error)
	ListRecentlyAddedLibraryTracks(ctx context.Context, limit int64) ([]db.LibraryTrack, error)
	ListRandomLibraryTracks(ctx context.Context, limit int64) ([]db.LibraryTrack, error)
	ListLibraryTrackGenreKeys(ctx context.Context, id string) ([]string, error)
	ListGenresForArtistKeys(ctx context.Context, arg db.ListGenresForArtistKeysParams) ([]db.ListGenresForArtistKeysRow, error)
	ListLibraryGenres(ctx context.Context, limit int64) ([]db.ListLibraryGenresRow, error)
}

// TasteSource gives the engine a user's taste profile.
type TasteSource interface {
	Profile(ctx context.Context, userID string) (*taste.Profile, error)
}

// SimilarSource gives the engine the artist-similarity map.
type SimilarSource interface {
	Similar(ctx context.Context, artistKey string) ([]libindex.SimilarArtist, error)
}

// TopTracksSource lists the external sources that can give an artist's top
// songs (live, so adapter reloads are picked up). Nil disables discovery.
type TopTracksSource func() []search.ArtistTopTracksProvider

// Options tune the engine.
type Options struct {
	// RecentWindow: a song the user heard this recently isn't picked again.
	RecentWindow time.Duration
	// MaxPerArtist / MaxPerAlbum cap one batch (SUB/WAVE uses 3 per artist).
	MaxPerArtist int
	MaxPerAlbum  int
	// Temperature of the weighted draw: lower is greedier.
	Temperature float64
}

// DefaultOptions is the shipped tuning.
var DefaultOptions = Options{
	RecentWindow: 3 * time.Hour,
	MaxPerArtist: 3,
	MaxPerAlbum:  2,
	Temperature:  0.35,
}

// Engine picks songs.
type Engine struct {
	q       Querier
	taste   TasteSource
	similar SimilarSource
	top     TopTracksSource
	now     func() time.Time
	opt     Options

	mu    sync.Mutex
	rng   *rand.Rand
	sonic SonicSource
}

// SonicSource answers "which library songs sound like these" from the
// optional audio analysis. Ready is false until enough is analyzed.
type SonicSource interface {
	Ready(ctx context.Context) bool
	SimilarTo(ctx context.Context, ids []string, n int) ([]analysis.Neighbor, error)
}

// SetSonic plugs in audio analysis (call before serving; nil turns it off).
func (e *Engine) SetSonic(s SonicSource) {
	e.mu.Lock()
	e.sonic = s
	e.mu.Unlock()
}

func (e *Engine) sonicSource(ctx context.Context) SonicSource {
	e.mu.Lock()
	s := e.sonic
	e.mu.Unlock()
	if s == nil || !s.Ready(ctx) {
		return nil
	}
	return s
}

// New builds an engine. rng may be nil (seeded from the clock).
func New(q Querier, ts TasteSource, sim SimilarSource, top TopTracksSource, now func() time.Time, rng *rand.Rand, opt Options) *Engine {
	if rng == nil {
		rng = rand.New(rand.NewSource(now().UnixNano()))
	}
	if opt.MaxPerArtist <= 0 {
		opt.MaxPerArtist = DefaultOptions.MaxPerArtist
	}
	if opt.MaxPerAlbum <= 0 {
		opt.MaxPerAlbum = DefaultOptions.MaxPerAlbum
	}
	if opt.Temperature <= 0 {
		opt.Temperature = DefaultOptions.Temperature
	}
	return &Engine{q: q, taste: ts, similar: sim, top: top, now: now, rng: rng, opt: opt}
}

// pool collects candidates from sources, deduplicated by id (the best base wins).
type pool struct {
	byID  map[string]*Candidate
	order []string
}

func newPool() *pool { return &pool{byID: map[string]*Candidate{}} }

func (p *pool) add(c Candidate) {
	id := candID(&c)
	if id == "" {
		return
	}
	if cur := p.byID[id]; cur != nil {
		if c.base > cur.base {
			*cur = c
		}
		return
	}
	cc := c
	p.byID[id] = &cc
	p.order = append(p.order, id)
}

func (p *pool) list() []*Candidate {
	out := make([]*Candidate, 0, len(p.order))
	for _, id := range p.order {
		out = append(out, p.byID[id])
	}
	return out
}

func candID(c *Candidate) string {
	if c.Track != nil {
		return "lib:" + c.Track.ID
	}
	if c.External != nil {
		return "ext:" + c.External.Source + ":" + c.External.ExternalID
	}
	return ""
}

func owned(t db.LibraryTrack, base float64, r Reason) Candidate {
	tr := &core.Track{
		ID: t.LibraryTrackID, Title: t.Title, AlbumID: t.AlbumID, Album: t.Album, ArtistID: t.ArtistID,
		Artist: t.Artist, CoverArtID: t.CoverArtID, TrackNumber: int(t.TrackNumber), DiscNumber: int(t.DiscNumber),
		DurationMs: int(t.DurationMs), BitRate: int(t.BitRate), Suffix: t.Suffix, ContentType: t.ContentType, ISRC: t.Isrc,
	}
	album := t.AlbumID
	if album == "" {
		album = matching.Normalize(t.Album)
	}
	return Candidate{
		Owned: true, Track: tr, Year: int(t.Year), Genres: t.Genres, Reason: r,
		artistKey: t.ArtistKey, albumKey: t.ArtistKey + "\x1f" + album, base: base,
	}
}

// seedInfo is what a seed resolved to.
type seedInfo struct {
	track      *db.LibraryTrack
	artistKey  string
	artistName string
	genres     []string
	genreLabel string
	year       int
	// artistGenres: no seed track to take genres from, use the artist's own.
	artistGenres bool
}

// Next returns the next batch for a station or lane.
func (e *Engine) Next(ctx context.Context, userID string, req Request) (Result, error) {
	if !Lanes[req.Lane] {
		return Result{}, ErrInvalid
	}
	if req.N <= 0 {
		req.N = 20
	}
	if req.N > 100 {
		req.N = 100
	}
	prof, err := e.taste.Profile(ctx, userID)
	if err != nil {
		return Result{}, err
	}
	seed, err := e.resolveSeed(ctx, req.Seed)
	if err != nil {
		return Result{}, err
	}
	if req.Lane == LaneSoundsLike && seed.artistKey == "" {
		// "Sounds like" with no seed means "like what I play most".
		if top := prof.TopArtists(1); len(top) > 0 {
			seed.artistKey, seed.artistName = top[0].Key, top[0].Name
		} else {
			return Result{}, ErrInvalid
		}
	}
	if req.Lane == LaneThrowback {
		if req.FromYear <= 0 || req.ToYear < req.FromYear {
			return Result{}, ErrInvalid
		}
	}

	p := newPool()
	if err := e.gather(ctx, p, prof, req, seed); err != nil {
		return Result{}, err
	}
	cands := e.filter(p.list(), prof, req, seed)
	e.score(cands, prof, req)
	picked := e.draw(cands, req.N, seed)

	if req.IncludeExternal && e.top != nil {
		ext := e.discover(ctx, prof, req, seed, req.N/4+1)
		picked = interleave(picked, ext, req.N)
	}
	if req.Seed.Kind == SeedTrack && seed.track != nil && req.Lane == LaneRadio && !excluded(req.Exclude, seed.track.LibraryTrackID) {
		// Track radio starts with the song it was started from.
		first := owned(*seed.track, 0, Reason{Kind: ReasonSeed, Artist: seed.track.Artist, ArtistKey: seed.artistKey})
		picked = append([]Candidate{first}, picked...)
		if len(picked) > req.N {
			picked = picked[:req.N]
		}
	}
	for i := range picked {
		picked[i].Score = math.Round(picked[i].Score*1000) / 1000
	}
	return Result{Station: station(req, seed), Items: picked}, nil
}

func (e *Engine) resolveSeed(ctx context.Context, s Seed) (seedInfo, error) {
	var out seedInfo
	switch s.Kind {
	case SeedTrack:
		if s.ID == "" {
			return out, ErrInvalid
		}
		t, err := e.q.GetLibraryTrack(ctx, s.ID)
		if err != nil {
			// Not indexed (yet), or not a library id: build the station
			// around the song's artist instead when we know it.
			if key := matching.ArtistKey(s.Artist); key != "" {
				out.artistKey, out.artistName = key, matching.PrimaryArtist(s.Artist)
				out.artistGenres = true
				return out, nil
			}
			return out, ErrNotFound
		}
		out.track = &t
		out.artistKey, out.artistName, out.year = t.ArtistKey, matching.PrimaryArtist(t.Artist), int(t.Year)
		out.genres, _ = e.q.ListLibraryTrackGenreKeys(ctx, t.LibraryTrackID)
	case SeedArtist:
		key := s.ID
		if key == "" {
			key = matching.ArtistKey(s.Artist)
		}
		if key == "" {
			return out, ErrInvalid
		}
		out.artistKey, out.artistName = key, strings.TrimSpace(s.Artist)
		out.artistGenres = true
		if out.artistName == "" {
			if rows, _ := e.q.ListLibraryTracksByArtistKeys(ctx, []string{key}); len(rows) > 0 {
				out.artistName = matching.PrimaryArtist(rows[0].Artist)
			} else {
				out.artistName = key
			}
		}
	case SeedGenre:
		key := matching.GenreKey(s.ID)
		if key == "" {
			return out, ErrInvalid
		}
		out.genres = []string{key}
		out.genreLabel = e.genreLabel(ctx, key)
	case SeedTaste, "":
	default:
		return out, ErrInvalid
	}
	return out, nil
}

// genreLabel recovers a genre's display form from a track that carries it.
func (e *Engine) genreLabel(ctx context.Context, key string) string {
	rows, err := e.q.ListLibraryTracksByGenre(ctx, db.ListLibraryTracksByGenreParams{GenreKey: key, Limit: 1})
	if err == nil && len(rows) > 0 {
		for _, g := range splitGenres(rows[0].Genres) {
			if matching.GenreKey(g) == key {
				return g
			}
		}
	}
	return titleCase(key)
}

func splitGenres(s string) []string {
	var out []string
	for _, g := range strings.Split(s, ";") {
		if g = strings.TrimSpace(g); g != "" {
			out = append(out, g)
		}
	}
	return out
}

func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// Source caps: how many candidates each source may contribute.
const (
	capSound      = 40
	capSeedArtist = 25
	capSimilar    = 80
	capGenre      = 60
	capEra        = 30
	capFavorite   = 40
	capFiller     = 30
	similarFanout = 12 // similar artists to draw from
)

func (e *Engine) gather(ctx context.Context, p *pool, prof *taste.Profile, req Request, seed seedInfo) error {
	switch req.Lane {
	case LaneRadio:
		switch req.Seed.Kind {
		case SeedTrack, SeedArtist:
			if err := e.fromSound(ctx, p, seed, 0.8); err != nil {
				return err
			}
			if err := e.fromArtists(ctx, p, []string{seed.artistKey}, capSeedArtist, 0.55, func(t db.LibraryTrack) Reason {
				return Reason{Kind: ReasonSeedArtist, Artist: matching.PrimaryArtist(t.Artist), ArtistKey: t.ArtistKey}
			}); err != nil {
				return err
			}
			if err := e.fromSimilar(ctx, p, seed.artistKey, seed.artistName, 0.9); err != nil {
				return err
			}
			if err := e.fromGenres(ctx, p, seed.genres, 0.45); err != nil {
				return err
			}
			if seed.year > 0 {
				if err := e.fromEra(ctx, p, seed.year-3, seed.year+3, capEra, 0.2, ReasonEra); err != nil {
					return err
				}
			}
			if seed.artistGenres {
				if err := e.fromArtistGenres(ctx, p, seed.artistKey, 0.4); err != nil {
					return err
				}
			}
		case SeedGenre:
			if err := e.fromGenres(ctx, p, seed.genres, 0.8); err != nil {
				return err
			}
		default: // taste radio
			if err := e.fromFavorites(ctx, p, prof, 0.7); err != nil {
				return err
			}
			for _, a := range prof.TopArtists(3) {
				if err := e.fromSimilar(ctx, p, a.Key, a.Name, 0.6); err != nil {
					return err
				}
			}
		}
		if req.Seed.Kind != SeedGenre {
			if err := e.fromFavorites(ctx, p, prof, 0.25); err != nil {
				return err
			}
		}
		// Top up a thin pool from the wider library, except for genre radio,
		// which would rather run short than leave its genre.
		if req.Seed.Kind != SeedGenre && len(p.order) < req.N*2 {
			return e.fromRandom(ctx, p, capFiller, 0.05)
		}
	case LaneOnRepeat:
		return e.fromTopTracks(ctx, p, prof, 60)
	case LaneDeepCuts, LaneNewToYou:
		keys := topKeys(prof, 15)
		if seed.artistKey != "" {
			keys = append([]string{seed.artistKey}, keys...)
		}
		if req.Lane == LaneDeepCuts {
			return e.fromArtists(ctx, p, keys, 200, 0.5, func(t db.LibraryTrack) Reason {
				return Reason{Kind: ReasonDeepCut, Artist: matching.PrimaryArtist(t.Artist), ArtistKey: t.ArtistKey}
			})
		}
		// New to you: nearby artists' songs the user hasn't played.
		for _, k := range keys[:min(len(keys), 6)] {
			if err := e.fromSimilar(ctx, p, k, k, 0.6); err != nil {
				return err
			}
		}
		return e.fromRandom(ctx, p, 60, 0.1)
	case LaneSoundsLike:
		if err := e.fromSound(ctx, p, seed, 1.0); err != nil {
			return err
		}
		if err := e.fromSimilar(ctx, p, seed.artistKey, seed.artistName, 0.9); err != nil {
			return err
		}
		return e.fromGenres(ctx, p, seed.genres, 0.3)
	case LaneThrowback:
		return e.fromEra(ctx, p, req.FromYear, req.ToYear, 200, 0.4, ReasonThrowback)
	case LaneFreshFinds:
		rows, err := e.q.ListRecentlyAddedLibraryTracks(ctx, 150)
		if err != nil {
			return err
		}
		cutoff := e.now().Add(-60 * 24 * time.Hour).Unix()
		for i, t := range rows {
			if t.AddedAt < cutoff && i >= 40 {
				break
			}
			p.add(owned(t, 0.6-0.3*float64(i)/float64(len(rows)), Reason{Kind: ReasonFresh, Artist: matching.PrimaryArtist(t.Artist), ArtistKey: t.ArtistKey}))
		}
	}
	return nil
}

func topKeys(prof *taste.Profile, n int) []string {
	var keys []string
	for _, a := range prof.TopArtists(n) {
		keys = append(keys, a.Key)
	}
	return keys
}

func (e *Engine) fromArtists(ctx context.Context, p *pool, keys []string, limit int, base float64, why func(db.LibraryTrack) Reason) error {
	keys = nonEmpty(keys)
	if len(keys) == 0 {
		return nil
	}
	rows, err := e.q.ListLibraryTracksByArtistKeys(ctx, keys)
	if err != nil {
		return err
	}
	e.shuffle(len(rows), func(i, j int) { rows[i], rows[j] = rows[j], rows[i] })
	for i, t := range rows {
		if i >= limit {
			break
		}
		p.add(owned(t, base, why(t)))
	}
	return nil
}

func (e *Engine) fromSimilar(ctx context.Context, p *pool, artistKey, artistName string, base float64) error {
	if artistKey == "" || e.similar == nil {
		return nil
	}
	sims, err := e.similar.Similar(ctx, artistKey)
	if err != nil {
		return err
	}
	var keys []string
	rank := map[string]int{}
	for _, s := range sims {
		if s.OwnedTracks > 0 && len(keys) < similarFanout {
			rank[s.Key] = len(keys)
			keys = append(keys, s.Key)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	rows, err := e.q.ListLibraryTracksByArtistKeys(ctx, keys)
	if err != nil {
		return err
	}
	e.shuffle(len(rows), func(i, j int) { rows[i], rows[j] = rows[j], rows[i] })
	per := map[string]int{}
	n := 0
	for _, t := range rows {
		if n >= capSimilar || per[t.ArtistKey] >= 10 {
			continue
		}
		per[t.ArtistKey]++
		n++
		// Closer artists (lower rank) start higher.
		b := base * (1 - 0.4*float64(rank[t.ArtistKey])/float64(similarFanout))
		p.add(owned(t, b, Reason{
			Kind: ReasonSimilarArtist, Artist: matching.PrimaryArtist(t.Artist), ArtistKey: t.ArtistKey, SeedArtist: artistName,
		}))
	}
	return nil
}

// fromSound adds songs that sound like the seed song (or, for an artist,
// like a handful of their songs), when audio analysis is set up.
func (e *Engine) fromSound(ctx context.Context, p *pool, seed seedInfo, base float64) error {
	sonic := e.sonicSource(ctx)
	if sonic == nil {
		return nil
	}
	var ids []string
	title := ""
	if seed.track != nil {
		ids, title = []string{seed.track.LibraryTrackID}, seed.track.Title
	} else if seed.artistKey != "" {
		rows, err := e.q.ListLibraryTracksByArtistKeys(ctx, []string{seed.artistKey})
		if err != nil {
			return err
		}
		e.shuffle(len(rows), func(i, j int) { rows[i], rows[j] = rows[j], rows[i] })
		for _, t := range rows[:min(len(rows), 12)] {
			ids = append(ids, t.LibraryTrackID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	near, err := sonic.SimilarTo(ctx, ids, capSound)
	if err != nil || len(near) == 0 {
		return err
	}
	nids := make([]string, len(near))
	rank := map[string]int{}
	for i, nb := range near {
		nids[i] = nb.ID
		rank[nb.ID] = i
	}
	rows, err := e.q.ListLibraryTracksByIDs(ctx, nids)
	if err != nil {
		return err
	}
	for _, t := range rows {
		b := base * (1 - 0.5*float64(rank[t.LibraryTrackID])/float64(len(near)))
		p.add(owned(t, b, Reason{
			Kind: ReasonSound, Artist: matching.PrimaryArtist(t.Artist), ArtistKey: t.ArtistKey, SeedArtist: seed.artistName, SeedTitle: title,
		}))
	}
	return nil
}

func (e *Engine) fromGenres(ctx context.Context, p *pool, genres []string, base float64) error {
	for i, g := range genres {
		if i >= 3 {
			break
		}
		rows, err := e.q.ListLibraryTracksByGenre(ctx, db.ListLibraryTracksByGenreParams{GenreKey: g, Limit: capGenre})
		if err != nil {
			return err
		}
		label := g
		for _, t := range rows {
			for _, dg := range splitGenres(t.Genres) {
				if matching.GenreKey(dg) == g {
					label = dg
				}
			}
			break
		}
		for _, t := range rows {
			p.add(owned(t, base*(1-0.15*float64(i)), Reason{Kind: ReasonGenre, Genre: label, Artist: matching.PrimaryArtist(t.Artist), ArtistKey: t.ArtistKey}))
		}
	}
	return nil
}

func (e *Engine) fromArtistGenres(ctx context.Context, p *pool, artistKey string, base float64) error {
	rows, err := e.q.ListGenresForArtistKeys(ctx, db.ListGenresForArtistKeysParams{Keys: []string{artistKey}, Limit: 2})
	if err != nil {
		return err
	}
	var gs []string
	for _, r := range rows {
		gs = append(gs, r.GenreKey)
	}
	return e.fromGenres(ctx, p, gs, base)
}

func (e *Engine) fromEra(ctx context.Context, p *pool, from, to, limit int, base float64, kind string) error {
	rows, err := e.q.ListLibraryTracksByYearRange(ctx, db.ListLibraryTracksByYearRangeParams{Year: int64(from), Year_2: int64(to), Limit: int64(limit)})
	if err != nil {
		return err
	}
	for _, t := range rows {
		p.add(owned(t, base, Reason{Kind: kind, Year: int(t.Year), Artist: matching.PrimaryArtist(t.Artist), ArtistKey: t.ArtistKey}))
	}
	return nil
}

func (e *Engine) fromFavorites(ctx context.Context, p *pool, prof *taste.Profile, base float64) error {
	keys := topKeys(prof, 10)
	if len(keys) == 0 {
		return nil
	}
	return e.fromArtists(ctx, p, keys, capFavorite, base, func(t db.LibraryTrack) Reason {
		r := Reason{Kind: ReasonFavorite, Artist: matching.PrimaryArtist(t.Artist), ArtistKey: t.ArtistKey}
		if a := prof.Artists[t.ArtistKey]; a != nil {
			r.PlayCount = a.Listens
		}
		return r
	})
}

func (e *Engine) fromTopTracks(ctx context.Context, p *pool, prof *taste.Profile, n int) error {
	top := prof.TopTracks(n)
	if len(top) == 0 {
		return nil
	}
	var ids []string
	keySet := map[string]bool{}
	for _, t := range top {
		if t.LibraryTrackID != "" {
			ids = append(ids, t.LibraryTrackID)
		}
		keySet[t.ArtistKey] = true
	}
	var rows []db.LibraryTrack
	if len(ids) > 0 {
		r, err := e.q.ListLibraryTracksByIDs(ctx, ids)
		if err != nil {
			return err
		}
		rows = append(rows, r...)
	}
	// Songs only known by title/artist: look through their artists' tracks.
	r, err := e.q.ListLibraryTracksByArtistKeys(ctx, nonEmpty(keys(keySet)))
	if err != nil {
		return err
	}
	rows = append(rows, r...)
	for _, t := range rows {
		tt := prof.TrackFor(t.LibraryTrackID, t.Title, t.Artist)
		if tt == nil || tt.Score <= 0 {
			continue
		}
		p.add(owned(t, 0.5, Reason{Kind: ReasonOnRepeat, Artist: matching.PrimaryArtist(t.Artist), ArtistKey: t.ArtistKey, PlayCount: tt.Listens}))
	}
	return nil
}

func (e *Engine) fromRandom(ctx context.Context, p *pool, n int, base float64) error {
	rows, err := e.q.ListRandomLibraryTracks(ctx, int64(n))
	if err != nil {
		return err
	}
	for _, t := range rows {
		p.add(owned(t, base, Reason{Kind: ReasonLibrary, Artist: matching.PrimaryArtist(t.Artist), ArtistKey: t.ArtistKey}))
	}
	return nil
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func nonEmpty(in []string) []string {
	out := in[:0:0]
	seen := map[string]bool{}
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func excluded(list []string, id string) bool {
	for _, x := range list {
		if x == id {
			return true
		}
	}
	return false
}

// filter applies the hard rules.
func (e *Engine) filter(in []*Candidate, prof *taste.Profile, req Request, seed seedInfo) []*Candidate {
	ex := make(map[string]bool, len(req.Exclude))
	for _, id := range req.Exclude {
		ex[id] = true
	}
	recent := e.now().Add(-e.opt.RecentWindow).Unix()
	out := in[:0]
	for _, c := range in {
		t := c.Track
		if t == nil {
			continue
		}
		if ex[t.ID] || (seed.track != nil && t.ID == seed.track.LibraryTrackID) {
			continue
		}
		if t.DurationMs > 0 && t.DurationMs < 45_000 {
			continue // intros, skits, interludes
		}
		if req.FromYear > 0 && req.ToYear >= req.FromYear && (c.Year < req.FromYear || c.Year > req.ToYear) {
			continue // a period was asked for ("90s hip hop"): stay in it on every lane
		}
		if prof.HiddenArtists[c.artistKey] {
			continue
		}
		tt := prof.TrackFor(t.ID, t.Title, t.Artist)
		if tt != nil {
			if prof.HiddenTracks[tt.CatalogID] || ex[tt.CatalogID] {
				continue
			}
			if req.Lane != LaneOnRepeat && tt.LastListened > recent {
				continue
			}
			// Played and mostly skipped: leave it out of auto-picked lists.
			if tt.Score < -0.5 {
				continue
			}
		}
		switch req.Lane {
		case LaneNewToYou:
			if tt != nil && tt.Listens > 0 {
				continue
			}
		case LaneDeepCuts:
			if tt != nil && tt.Completions > 2 {
				continue
			}
		case LaneSoundsLike:
			if c.artistKey == seed.artistKey {
				continue
			}
		}
		out = append(out, c)
	}
	return out
}

// squash maps an unbounded score into (-1, 1).
func squash(x float64) float64 { return x / (1 + math.Abs(x)) }

// score adds the taste profile to each candidate's source base.
func (e *Engine) score(cands []*Candidate, prof *taste.Profile, req Request) {
	for _, c := range cands {
		t := c.Track
		tt := prof.TrackFor(t.ID, t.Title, t.Artist)
		var ts, as float64
		if tt != nil {
			ts = tt.Score
		}
		if a := prof.Artists[c.artistKey]; a != nil {
			as = a.Score
			// Early skips of this artist in auto contexts weigh in too.
			as -= 0.3 * float64(a.EarlySkips) / float64(1+a.Listens)
		}
		s := c.base + 0.5*squash(ts/3) + 0.35*squash(as/5)
		switch req.Lane {
		case LaneOnRepeat:
			s = c.base + squash(ts/2)
		case LaneDeepCuts:
			// Favourite artists, songs barely played.
			s = c.base + 0.6*squash(as/5)
			if tt != nil {
				s -= 0.1 * float64(tt.Listens)
			}
		case LaneNewToYou:
			s = c.base + 0.3*squash(as/5)
		case LaneThrowback:
			// Things the user liked from then come first, unknown ones still in.
			s = c.base + 0.6*squash(ts/3) + 0.3*squash(as/5)
		}
		c.Score = s
	}
}

// draw picks n candidates by weighted sampling without replacement, respecting
// the per-artist and per-album caps and never placing the same artist twice in
// a row when there is another choice.
func (e *Engine) draw(cands []*Candidate, n int, seed seedInfo) []Candidate {
	if len(cands) == 0 {
		return []Candidate{}
	}
	maxArtist := e.opt.MaxPerArtist
	if n >= 20 {
		maxArtist = max(maxArtist, n/6)
	}
	// Artist variety relaxes when the pool is one artist (artist radio on a
	// small library, deep cuts of one band).
	distinct := map[string]bool{}
	for _, c := range cands {
		distinct[c.artistKey] = true
	}
	if len(distinct)*maxArtist < n {
		maxArtist = (n + len(distinct) - 1) / len(distinct)
	}
	maxAlbum := e.opt.MaxPerAlbum
	if n >= 20 {
		maxAlbum = max(maxAlbum, n/8)
	}
	// Likewise for albums: a genre one band covers over two records would
	// otherwise stop at four songs.
	albums := map[string]bool{}
	for _, c := range cands {
		albums[c.albumKey] = true
	}
	if len(albums)*maxAlbum < n {
		maxAlbum = (n + len(albums) - 1) / len(albums)
	}

	weights := make([]float64, len(cands))
	for i, c := range cands {
		weights[i] = math.Exp(c.Score / e.opt.Temperature)
	}
	used := make([]bool, len(cands))
	perArtist := map[string]int{}
	perAlbum := map[string]int{}
	out := make([]Candidate, 0, n)
	last := ""
	if seed.track != nil {
		last = seed.artistKey
	}
	for len(out) < n {
		pick := e.pickOne(cands, weights, used, func(c *Candidate, strict bool) bool {
			if perArtist[c.artistKey] >= maxArtist || perAlbum[c.albumKey] >= maxAlbum {
				return false
			}
			return !strict || c.artistKey != last
		})
		if pick < 0 {
			break
		}
		used[pick] = true
		c := cands[pick]
		perArtist[c.artistKey]++
		perAlbum[c.albumKey]++
		last = c.artistKey
		out = append(out, *c)
	}
	return out
}

func (e *Engine) pickOne(cands []*Candidate, weights []float64, used []bool, ok func(c *Candidate, strict bool) bool) int {
	for _, strict := range []bool{true, false} {
		total := 0.0
		for i, c := range cands {
			if !used[i] && ok(c, strict) {
				total += weights[i]
			}
		}
		if total <= 0 {
			continue
		}
		e.mu.Lock()
		r := e.rng.Float64() * total
		e.mu.Unlock()
		for i, c := range cands {
			if used[i] || !ok(c, strict) {
				continue
			}
			r -= weights[i]
			if r <= 0 {
				return i
			}
		}
		// Float rounding: take the last eligible one.
		for i := len(cands) - 1; i >= 0; i-- {
			if !used[i] && ok(cands[i], strict) {
				return i
			}
		}
	}
	return -1
}

func (e *Engine) shuffle(n int, swap func(i, j int)) {
	e.mu.Lock()
	e.rng.Shuffle(n, swap)
	e.mu.Unlock()
}

// discover finds songs not in the library: top songs of artists similar to
// the seed (or the user's favourites) that the library doesn't have.
func (e *Engine) discover(ctx context.Context, prof *taste.Profile, req Request, seed seedInfo, n int) []Candidate {
	anchors := []string{}
	names := map[string]string{}
	if seed.artistKey != "" {
		anchors = append(anchors, seed.artistKey)
		names[seed.artistKey] = seed.artistName
	}
	for _, a := range prof.TopArtists(5) {
		anchors = append(anchors, a.Key)
		names[a.Key] = a.Name
	}
	ex := map[string]bool{}
	for _, id := range req.Exclude {
		ex[id] = true
	}
	return e.discoverFrom(ctx, prof, nonEmpty(anchors), names, n, ex)
}

// Discover returns up to n songs that aren't in the library, for a user's
// Discover Weekly: one top song each from artists that fans of the user's
// favourites also play, skipping artists the library has, thumbed-down
// artists, and any song in exclude (keyed "source:externalId").
func (e *Engine) Discover(ctx context.Context, userID string, n int, exclude map[string]bool) ([]Candidate, error) {
	if e.top == nil || n <= 0 {
		return []Candidate{}, nil
	}
	prof, err := e.taste.Profile(ctx, userID)
	if err != nil {
		return nil, err
	}
	var anchors []string
	names := map[string]string{}
	for _, a := range prof.TopArtists(12) {
		anchors = append(anchors, a.Key)
		names[a.Key] = a.Name
	}
	out := e.discoverFrom(ctx, prof, anchors, names, n, exclude)
	if out == nil {
		out = []Candidate{}
	}
	return out, nil
}

func (e *Engine) discoverFrom(ctx context.Context, prof *taste.Profile, anchors []string, names map[string]string, n int, exclude map[string]bool) []Candidate {
	if e.top == nil || e.similar == nil || n <= 0 {
		return nil
	}
	srcs := e.top()
	if len(srcs) == 0 {
		return nil
	}
	type pick struct {
		sim    libindex.SimilarArtist
		anchor string
	}
	// First a spread over the favourites, so no one of them takes the whole
	// list; then whatever else is left, so a listener with one clear
	// favourite still gets a full list.
	var picks, rest []pick
	seen := map[string]bool{}
	for _, a := range anchors {
		seen[a] = true
	}
	for _, a := range anchors {
		sims, err := e.similar.Similar(ctx, a)
		if err != nil {
			continue
		}
		taken := 0
		for _, s := range sims {
			if s.OwnedTracks > 0 || s.ExternalID == "" || seen[s.Key] || prof.HiddenArtists[s.Key] {
				continue
			}
			seen[s.Key] = true
			if taken < max(2, n/2) {
				picks = append(picks, pick{s, a})
				taken++
			} else {
				rest = append(rest, pick{s, a})
			}
		}
	}
	// Then one step further out: new artists like the library artists that
	// are like the favourites. Every song Discover offers is never offered
	// again, and an artist that gets downloaded counts as owned, so without
	// this a listener with few favourites runs out after a few refreshes and
	// the list falls back to songs they already have.
	var far []pick
	names = maps.Clone(names)
	for _, a := range anchors {
		sims, err := e.similar.Similar(ctx, a)
		if err != nil {
			continue
		}
		bridges := 0
		for _, b := range sims {
			if bridges >= maxDiscoverBridges {
				break
			}
			if b.OwnedTracks == 0 || prof.HiddenArtists[b.Key] || slices.Contains(anchors, b.Key) {
				continue
			}
			bridges++
			next, err := e.similar.Similar(ctx, b.Key)
			if err != nil {
				continue
			}
			for _, s := range next {
				if s.OwnedTracks > 0 || s.ExternalID == "" || seen[s.Key] || prof.HiddenArtists[s.Key] {
					continue
				}
				seen[s.Key] = true
				if _, ok := names[b.Key]; !ok {
					names[b.Key] = b.Name
				}
				far = append(far, pick{s, b.Key})
			}
		}
	}
	e.shuffle(len(picks), func(i, j int) { picks[i], picks[j] = picks[j], picks[i] })
	e.shuffle(len(rest), func(i, j int) { rest[i], rest[j] = rest[j], rest[i] })
	e.shuffle(len(far), func(i, j int) { far[i], far[j] = far[j], far[i] })
	picks = append(append(picks, rest...), far...)

	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	type songs struct {
		fresh []core.ExternalResult
		done  bool
	}
	cache := make([]songs, len(picks))
	used := map[string]bool{}
	var out []Candidate
	// One song per artist first; with too few artists, a second and a third.
	for round := 0; round < maxDiscoverPerArtist && len(out) < n; round++ {
		for i, pk := range picks {
			if len(out) >= n || ctx.Err() != nil {
				break
			}
			if !cache[i].done {
				cache[i].done = true
				for _, src := range srcs {
					if ss, ok := src.(search.SearchSource); ok && ss.Name() != pk.sim.Source {
						continue
					}
					tracks, err := src.ArtistTopTracks(ctx, pk.sim.ExternalID, discoverTopTracks)
					if err != nil {
						continue
					}
					for _, t := range tracks {
						if !exclude[t.Source+":"+t.ExternalID] {
							cache[i].fresh = append(cache[i].fresh, t)
						}
					}
					if len(cache[i].fresh) > 0 {
						break
					}
				}
			}
			var left []core.ExternalResult
			for _, t := range cache[i].fresh {
				if !used[t.Source+":"+t.ExternalID] {
					left = append(left, t)
				}
			}
			if len(left) == 0 {
				continue
			}
			e.mu.Lock()
			t := left[e.rng.Intn(len(left))]
			e.mu.Unlock()
			used[t.Source+":"+t.ExternalID] = true
			out = append(out, Candidate{
				External: &t, Score: 0.5,
				Reason:    Reason{Kind: ReasonDiscovery, Artist: pk.sim.Name, ArtistKey: pk.sim.Key, SeedArtist: names[pk.anchor]},
				artistKey: pk.sim.Key,
			})
		}
	}
	return out
}

// maxDiscoverPerArtist caps how many new songs one new artist brings.
const maxDiscoverPerArtist = 3

// discoverTopTracks is how deep into a new artist's best-known songs
// Discover looks. Songs offered before are skipped, so this is what lets an
// artist keep bringing something new over several weeks.
const discoverTopTracks = 10

// maxDiscoverBridges caps how many owned artists per favourite Discover
// steps through to reach artists one step further out.
const maxDiscoverBridges = 6

// interleave spreads external picks through the owned list.
func interleave(own, ext []Candidate, n int) []Candidate {
	if len(ext) == 0 {
		return own
	}
	out := make([]Candidate, 0, n)
	step := max(1, (len(own)+len(ext))/(len(ext)+1))
	i, j := 0, 0
	for len(out) < n && (i < len(own) || j < len(ext)) {
		if j < len(ext) && (i >= len(own) || (len(out)+1)%(step+1) == 0) {
			out = append(out, ext[j])
			j++
			continue
		}
		out = append(out, own[i])
		i++
	}
	return out
}

func station(req Request, seed seedInfo) Station {
	st := Station{Kind: req.Seed.Kind, Lane: req.Lane}
	if st.Kind == "" {
		st.Kind = SeedTaste
	}
	switch st.Kind {
	case SeedTrack:
		st.ID = "track:" + req.Seed.ID
		if seed.track != nil {
			st.Label = seed.track.Title + " radio"
		} else {
			st.Label = strings.TrimSpace(req.Seed.Title + " radio")
		}
	case SeedArtist:
		st.ID = "artist:" + seed.artistKey
		st.Label = seed.artistName + " radio"
	case SeedGenre:
		st.ID = "genre:" + seed.genres[0]
		st.Label = seed.genreLabel + " radio"
	default:
		st.ID = "taste"
		st.Label = "Your radio"
	}
	if req.Lane != LaneRadio {
		st.ID = "lane:" + req.Lane + ":" + st.ID
		st.Label = laneLabel(req, seed)
	}
	return st
}

func laneLabel(req Request, seed seedInfo) string {
	switch req.Lane {
	case LaneOnRepeat:
		return "On repeat"
	case LaneThrowback:
		if req.FromYear == req.ToYear {
			return "Throwback: " + strconv.Itoa(req.FromYear)
		}
		return "Throwback: " + strconv.Itoa(req.FromYear) + "–" + strconv.Itoa(req.ToYear)
	case LaneDeepCuts:
		return "Deep cuts"
	case LaneSoundsLike:
		return "Sounds like " + seed.artistName
	case LaneNewToYou:
		return "New to you"
	case LaneFreshFinds:
		return "Fresh finds"
	}
	return "Radio"
}
