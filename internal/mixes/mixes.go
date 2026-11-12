// Package mixes turns the recommendation engine into playlists that keep
// themselves fresh. A mix is a saved recipe (seeds, an engine lane, a period,
// a size, a refresh schedule) plus the songs its last refresh produced.
//
// Daily mixes are Reverb's own: the user's favourite artists grouped into a
// few clusters of artists that sound alike (by the similarity map, or a shared
// genre), one mix per cluster, rebuilt once a day. Custom mixes are the user's
// own recipes. Refreshing happens lazily when mixes are read, so there is no
// scheduler to run.
package mixes

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/maxjb-xyz/reverb/internal/engine"
	"github.com/maxjb-xyz/reverb/internal/libindex"
	"github.com/maxjb-xyz/reverb/internal/store/db"
	"github.com/maxjb-xyz/reverb/internal/taste"
)

// Kinds.
const (
	KindDaily  = "daily"
	KindCustom = "custom"
)

// Refresh schedules.
const (
	RefreshDaily  = "daily"
	RefreshWeekly = "weekly"
	RefreshManual = "manual"
)

// Limits.
const (
	MaxDailyMixes  = 6
	DefaultSize    = 30
	MaxSize        = 100
	MaxSeeds       = 10
	MaxCustomMixes = 50
	clusterMax     = 5 // artists per daily mix
)

var (
	// ErrInvalid is a recipe that can't be built.
	ErrInvalid = errors.New("mixes: invalid recipe")
	// ErrNotFound is a mix that doesn't exist (or isn't the user's).
	ErrNotFound = errors.New("mixes: not found")
	// ErrReadOnly is an edit to a mix Reverb owns (daily mixes).
	ErrReadOnly = errors.New("mixes: daily mixes can't be edited")
	// ErrTooMany is one custom mix too many.
	ErrTooMany = errors.New("mixes: too many mixes")
)

// Recipe is what a mix is built from.
type Recipe struct {
	Seeds    []engine.Seed `json:"seeds"`
	Lane     string        `json:"lane,omitempty"`
	FromYear int           `json:"fromYear,omitempty"`
	ToYear   int           `json:"toYear,omitempty"`
	Size     int           `json:"size"`
	Refresh  string        `json:"refresh"`
}

// Mix is a mix with its current songs.
type Mix struct {
	ID          string             `json:"id"`
	Kind        string             `json:"kind"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Recipe      Recipe             `json:"recipe"`
	Items       []engine.Candidate `json:"items"`
	CoverArtID  string             `json:"coverArtId,omitempty"`
	GeneratedAt int64              `json:"generatedAt"`
	CreatedAt   int64              `json:"createdAt"`
}

// Querier is the slice of the store mixes use.
type Querier interface {
	ListMixes(ctx context.Context, userID string) ([]db.Mix, error)
	GetMix(ctx context.Context, arg db.GetMixParams) (db.Mix, error)
	InsertMix(ctx context.Context, arg db.InsertMixParams) error
	UpdateMixRecipe(ctx context.Context, arg db.UpdateMixRecipeParams) error
	UpdateMixItems(ctx context.Context, arg db.UpdateMixItemsParams) error
	DeleteMix(ctx context.Context, arg db.DeleteMixParams) (int64, error)
	DeleteMixesByKind(ctx context.Context, arg db.DeleteMixesByKindParams) error
	ListGenresForArtistKeys(ctx context.Context, arg db.ListGenresForArtistKeysParams) ([]db.ListGenresForArtistKeysRow, error)
	ListLibraryTracksByArtistKeys(ctx context.Context, keys []string) ([]db.LibraryTrack, error)
}

// Engine is the recommendation engine.
type Engine interface {
	Next(ctx context.Context, userID string, req engine.Request) (engine.Result, error)
}

// TasteSource gives the user's taste profile.
type TasteSource interface {
	Profile(ctx context.Context, userID string) (*taste.Profile, error)
}

// SimilarSource gives the artist-similarity map.
type SimilarSource interface {
	Similar(ctx context.Context, artistKey string) ([]libindex.SimilarArtist, error)
}

// Service manages mixes.
type Service struct {
	q       Querier
	eng     Engine
	taste   TasteSource
	similar SimilarSource
	now     func() time.Time
	idgen   func() string

	// One refresh per user at a time: two tabs opening Home at midnight
	// shouldn't build the daily mixes twice.
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// New builds the service.
func New(q Querier, eng Engine, ts TasteSource, sim SimilarSource, now func() time.Time, idgen func() string) *Service {
	return &Service{q: q, eng: eng, taste: ts, similar: sim, now: now, idgen: idgen, locks: map[string]*sync.Mutex{}}
}

func (s *Service) userLock(userID string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	l := s.locks[userID]
	if l == nil {
		l = &sync.Mutex{}
		s.locks[userID] = l
	}
	return l
}

// List returns the user's mixes, refreshing any that are due (daily mixes are
// rebuilt from the current taste each day).
func (s *Service) List(ctx context.Context, userID string) ([]Mix, error) {
	l := s.userLock(userID)
	l.Lock()
	defer l.Unlock()
	rows, err := s.q.ListMixes(ctx, userID)
	if err != nil {
		return nil, err
	}
	if s.dailyDue(rows) {
		if err := s.rebuildDaily(ctx, userID); err != nil {
			return nil, err
		}
		if rows, err = s.q.ListMixes(ctx, userID); err != nil {
			return nil, err
		}
	}
	out := make([]Mix, 0, len(rows))
	for _, r := range rows {
		m, err := decode(r)
		if err != nil {
			continue
		}
		if r.Kind == KindCustom && s.due(m) {
			if m, err = s.refresh(ctx, userID, m); err != nil {
				return nil, err
			}
		}
		out = append(out, m)
	}
	return out, nil
}

// Get returns one mix, refreshed if it is due.
func (s *Service) Get(ctx context.Context, userID, id string) (Mix, error) {
	l := s.userLock(userID)
	l.Lock()
	defer l.Unlock()
	r, err := s.q.GetMix(ctx, db.GetMixParams{ID: id, UserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return Mix{}, ErrNotFound
	}
	if err != nil {
		return Mix{}, err
	}
	m, err := decode(r)
	if err != nil {
		return Mix{}, err
	}
	if m.Kind == KindCustom && s.due(m) {
		return s.refresh(ctx, userID, m)
	}
	return m, nil
}

// Create saves a custom mix and builds it.
func (s *Service) Create(ctx context.Context, userID, name string, rec Recipe) (Mix, error) {
	name = strings.TrimSpace(name)
	rec, err := normalize(rec)
	if err != nil || name == "" || len(name) > 100 {
		return Mix{}, ErrInvalid
	}
	l := s.userLock(userID)
	l.Lock()
	defer l.Unlock()
	rows, err := s.q.ListMixes(ctx, userID)
	if err != nil {
		return Mix{}, err
	}
	custom := 0
	for _, r := range rows {
		if r.Kind == KindCustom {
			custom++
		}
	}
	if custom >= MaxCustomMixes {
		return Mix{}, ErrTooMany
	}
	// Build first, so a recipe the engine can't serve is never saved.
	items, err := s.build(ctx, userID, rec)
	if err != nil {
		return Mix{}, err
	}
	now := s.now().Unix()
	m := Mix{ID: s.idgen(), Kind: KindCustom, Name: name, Recipe: rec, Items: items, GeneratedAt: now, CreatedAt: now}
	m.Description, m.CoverArtID = describe(rec, items)
	recJSON, _ := json.Marshal(rec)
	itemsJSON, _ := json.Marshal(items)
	err = s.q.InsertMix(ctx, db.InsertMixParams{
		ID: m.ID, UserID: userID, Kind: KindCustom, Position: int64(custom), Name: name, Description: m.Description,
		Recipe: string(recJSON), Items: string(itemsJSON), CoverArtID: m.CoverArtID, GeneratedAt: now, CreatedAt: now, UpdatedAt: now,
	})
	return m, err
}

// Update changes a custom mix's name and recipe and rebuilds it.
func (s *Service) Update(ctx context.Context, userID, id, name string, rec Recipe) (Mix, error) {
	name = strings.TrimSpace(name)
	rec, err := normalize(rec)
	if err != nil || name == "" || len(name) > 100 {
		return Mix{}, ErrInvalid
	}
	l := s.userLock(userID)
	l.Lock()
	defer l.Unlock()
	r, err := s.q.GetMix(ctx, db.GetMixParams{ID: id, UserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return Mix{}, ErrNotFound
	}
	if err != nil {
		return Mix{}, err
	}
	if r.Kind != KindCustom {
		return Mix{}, ErrReadOnly
	}
	m, err := decode(r)
	if err != nil {
		return Mix{}, err
	}
	items, err := s.build(ctx, userID, rec)
	if err != nil {
		return Mix{}, err
	}
	recJSON, _ := json.Marshal(rec)
	if err := s.q.UpdateMixRecipe(ctx, db.UpdateMixRecipeParams{Name: name, Recipe: string(recJSON), UpdatedAt: s.now().Unix(), ID: id, UserID: userID}); err != nil {
		return Mix{}, err
	}
	m.Name, m.Recipe = name, rec
	return s.store(ctx, userID, m, items)
}

// Refresh rebuilds a mix now ("new songs, please").
func (s *Service) Refresh(ctx context.Context, userID, id string) (Mix, error) {
	l := s.userLock(userID)
	l.Lock()
	defer l.Unlock()
	r, err := s.q.GetMix(ctx, db.GetMixParams{ID: id, UserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return Mix{}, ErrNotFound
	}
	if err != nil {
		return Mix{}, err
	}
	m, err := decode(r)
	if err != nil {
		return Mix{}, err
	}
	return s.refresh(ctx, userID, m)
}

// Delete removes a custom mix.
func (s *Service) Delete(ctx context.Context, userID, id string) error {
	r, err := s.q.GetMix(ctx, db.GetMixParams{ID: id, UserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if r.Kind != KindCustom {
		return ErrReadOnly
	}
	_, err = s.q.DeleteMix(ctx, db.DeleteMixParams{ID: id, UserID: userID})
	return err
}

func decode(r db.Mix) (Mix, error) {
	m := Mix{
		ID: r.ID, Kind: r.Kind, Name: r.Name, Description: r.Description, CoverArtID: r.CoverArtID,
		GeneratedAt: r.GeneratedAt, CreatedAt: r.CreatedAt,
	}
	if err := json.Unmarshal([]byte(r.Recipe), &m.Recipe); err != nil {
		return m, err
	}
	if err := json.Unmarshal([]byte(r.Items), &m.Items); err != nil {
		m.Items = nil
	}
	if m.Items == nil {
		m.Items = []engine.Candidate{}
	}
	return m, nil
}

func normalize(rec Recipe) (Recipe, error) {
	if !engine.Lanes[rec.Lane] {
		return rec, ErrInvalid
	}
	if len(rec.Seeds) > MaxSeeds {
		return rec, ErrInvalid
	}
	var seeds []engine.Seed
	for _, sd := range rec.Seeds {
		sd.ID, sd.Artist, sd.Title = strings.TrimSpace(sd.ID), strings.TrimSpace(sd.Artist), strings.TrimSpace(sd.Title)
		switch sd.Kind {
		case engine.SeedArtist:
			if sd.ID == "" && sd.Artist == "" {
				return rec, ErrInvalid
			}
		case engine.SeedGenre, engine.SeedTrack:
			if sd.ID == "" {
				return rec, ErrInvalid
			}
		case engine.SeedTaste:
		default:
			return rec, ErrInvalid
		}
		seeds = append(seeds, sd)
	}
	rec.Seeds = seeds
	if rec.Lane == engine.LaneRadio && len(rec.Seeds) == 0 {
		rec.Seeds = []engine.Seed{{Kind: engine.SeedTaste}}
	}
	if rec.Lane == engine.LaneThrowback && (rec.FromYear <= 0 || rec.ToYear < rec.FromYear) {
		return rec, ErrInvalid
	}
	if rec.Size <= 0 {
		rec.Size = DefaultSize
	}
	if rec.Size > MaxSize {
		rec.Size = MaxSize
	}
	switch rec.Refresh {
	case "":
		rec.Refresh = RefreshDaily
	case RefreshDaily, RefreshWeekly, RefreshManual:
	default:
		return rec, ErrInvalid
	}
	return rec, nil
}

// due reports whether a custom mix's schedule says it should be rebuilt.
func (s *Service) due(m Mix) bool {
	var every time.Duration
	switch m.Recipe.Refresh {
	case RefreshDaily:
		every = 24 * time.Hour
	case RefreshWeekly:
		every = 7 * 24 * time.Hour
	default:
		return false
	}
	return s.now().Sub(time.Unix(m.GeneratedAt, 0)) >= every
}

// dailyDue: the daily mixes were built before today's start (server time),
// or never.
func (s *Service) dailyDue(rows []db.Mix) bool {
	now := s.now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
	seen := false
	for _, r := range rows {
		if r.Kind != KindDaily {
			continue
		}
		seen = true
		if r.GeneratedAt < today {
			return true
		}
	}
	return !seen
}

func (s *Service) refresh(ctx context.Context, userID string, m Mix) (Mix, error) {
	items, err := s.build(ctx, userID, m.Recipe)
	if err != nil {
		return Mix{}, err
	}
	return s.store(ctx, userID, m, items)
}

func (s *Service) store(ctx context.Context, userID string, m Mix, items []engine.Candidate) (Mix, error) {
	now := s.now().Unix()
	m.Items, m.GeneratedAt = items, now
	m.Description, m.CoverArtID = describe(m.Recipe, items)
	if m.Kind == KindDaily {
		m.Description = dailyDescription(m.Recipe)
	}
	itemsJSON, _ := json.Marshal(items)
	err := s.q.UpdateMixItems(ctx, db.UpdateMixItemsParams{
		Description: m.Description, Items: string(itemsJSON), CoverArtID: m.CoverArtID, GeneratedAt: now, UpdatedAt: now, ID: m.ID, UserID: userID,
	})
	return m, err
}

// build asks the engine for a recipe's songs: one request per seed, merged
// round-robin so every seed is heard, without repeats.
func (s *Service) build(ctx context.Context, userID string, rec Recipe) ([]engine.Candidate, error) {
	seeds := rec.Seeds
	if len(seeds) == 0 {
		seeds = []engine.Seed{{}}
	}
	per := rec.Size/len(seeds) + 3
	batches := make([][]engine.Candidate, 0, len(seeds))
	for _, sd := range seeds {
		res, err := s.eng.Next(ctx, userID, engine.Request{Seed: sd, Lane: rec.Lane, FromYear: rec.FromYear, ToYear: rec.ToYear, N: per})
		if errors.Is(err, engine.ErrInvalid) {
			return nil, ErrInvalid
		}
		if errors.Is(err, engine.ErrNotFound) {
			continue // a seed song left the library; the others still make a mix
		}
		if err != nil {
			return nil, err
		}
		batches = append(batches, res.Items)
	}
	seen := map[string]bool{}
	out := make([]engine.Candidate, 0, rec.Size)
	for i := 0; len(out) < rec.Size; i++ {
		progressed := false
		for _, b := range batches {
			if i >= len(b) {
				continue
			}
			progressed = true
			c := b[i]
			if c.Track == nil || seen[c.Track.ID] {
				continue
			}
			// A mix plays what its seeds drew, not the seed song itself
			// as a "where this started" opener.
			if c.Reason.Kind == engine.ReasonSeed && len(batches) > 1 {
				continue
			}
			seen[c.Track.ID] = true
			out = append(out, c)
			if len(out) >= rec.Size {
				break
			}
		}
		if !progressed {
			break
		}
	}
	// Seeds that overlap (similar artists share songs) can leave the mix
	// short: one more round, excluding what's in.
	if len(out) < rec.Size && len(batches) > 0 {
		exclude := make([]string, 0, len(seen))
		for id := range seen {
			exclude = append(exclude, id)
		}
		sort.Strings(exclude)
		for _, sd := range seeds {
			if len(out) >= rec.Size {
				break
			}
			res, err := s.eng.Next(ctx, userID, engine.Request{Seed: sd, Lane: rec.Lane, FromYear: rec.FromYear, ToYear: rec.ToYear, Exclude: exclude, N: rec.Size - len(out) + 2})
			if err != nil {
				continue
			}
			for _, c := range res.Items {
				if len(out) >= rec.Size {
					break
				}
				if c.Track == nil || seen[c.Track.ID] || c.Reason.Kind == engine.ReasonSeed {
					continue
				}
				seen[c.Track.ID] = true
				out = append(out, c)
			}
		}
	}
	return out, nil
}

// describe makes the subtitle ("Bonobo, Tycho, Four Tet and more") and picks a cover.
func describe(rec Recipe, items []engine.Candidate) (string, string) {
	var names []string
	seen := map[string]bool{}
	cover := ""
	for _, c := range items {
		if c.Track == nil {
			continue
		}
		if cover == "" && c.Track.CoverArtID != "" {
			cover = c.Track.CoverArtID
		}
		key := c.Reason.ArtistKey
		if key == "" {
			key = strings.ToLower(c.Track.Artist)
		}
		if !seen[key] && len(names) < 3 {
			seen[key] = true
			name := c.Reason.Artist
			if name == "" {
				name = c.Track.Artist
			}
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "", cover
	}
	return strings.Join(names, ", ") + " and more", cover
}

func dailyDescription(rec Recipe) string {
	var names []string
	for _, sd := range rec.Seeds {
		if sd.Artist != "" && len(names) < 3 {
			names = append(names, sd.Artist)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return strings.Join(names, ", ") + " and more"
}

// rebuildDaily replaces the user's daily mixes with fresh ones built from
// clusters of their favourite artists.
func (s *Service) rebuildDaily(ctx context.Context, userID string) error {
	prof, err := s.taste.Profile(ctx, userID)
	if err != nil {
		return err
	}
	clusters, err := s.clusters(ctx, prof)
	if err != nil {
		return err
	}
	if err := s.q.DeleteMixesByKind(ctx, db.DeleteMixesByKindParams{UserID: userID, Kind: KindDaily}); err != nil {
		return err
	}
	now := s.now().Unix()
	pos := 0
	for _, cl := range clusters {
		var seeds []engine.Seed
		for _, a := range cl {
			seeds = append(seeds, engine.Seed{Kind: engine.SeedArtist, ID: a.Key, Artist: a.Name})
		}
		rec := Recipe{Seeds: seeds, Size: DefaultSize, Refresh: RefreshDaily}
		items, err := s.build(ctx, userID, rec)
		if err != nil {
			return err
		}
		if len(items) < 8 {
			continue // too thin to be worth a tile
		}
		_, cover := describe(rec, items)
		recJSON, _ := json.Marshal(rec)
		itemsJSON, _ := json.Marshal(items)
		pos++
		if err := s.q.InsertMix(ctx, db.InsertMixParams{
			ID: s.idgen(), UserID: userID, Kind: KindDaily, Position: int64(pos), Name: "Daily Mix " + strconv.Itoa(pos),
			Description: dailyDescription(rec), Recipe: string(recJSON), Items: string(itemsJSON), CoverArtID: cover,
			GeneratedAt: now, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			return err
		}
	}
	// With no history (or an index still filling) nothing is built and the
	// next read tries again.
	return nil
}

// clusters groups the user's favourite library artists into up to
// MaxDailyMixes sets of artists that belong together: an artist joins a
// cluster when the similarity map links them (either way) or they share their
// main genre. Strongest artists anchor clusters first.
func (s *Service) clusters(ctx context.Context, prof *taste.Profile) ([][]taste.ArtistTaste, error) {
	top := prof.TopArtists(40)
	if len(top) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(top))
	for _, a := range top {
		keys = append(keys, a.Key)
	}
	// Only artists still in the library can seed a library mix.
	rows, err := s.q.ListLibraryTracksByArtistKeys(ctx, keys)
	if err != nil {
		return nil, err
	}
	inLib := map[string]bool{}
	for _, r := range rows {
		inLib[r.ArtistKey] = true
	}
	var artists []taste.ArtistTaste
	for _, a := range top {
		if inLib[a.Key] {
			artists = append(artists, a)
		}
	}
	links := map[string]map[string]bool{}
	link := func(a, b string) {
		if links[a] == nil {
			links[a] = map[string]bool{}
		}
		links[a][b] = true
	}
	genre := map[string]string{}
	for _, a := range artists {
		if s.similar != nil {
			if sims, err := s.similar.Similar(ctx, a.Key); err == nil {
				for _, sm := range sims {
					link(a.Key, sm.Key)
					link(sm.Key, a.Key)
				}
			}
		}
		if g, err := s.q.ListGenresForArtistKeys(ctx, db.ListGenresForArtistKeysParams{Keys: []string{a.Key}, Limit: 1}); err == nil && len(g) > 0 {
			genre[a.Key] = g[0].GenreKey
		}
	}
	assigned := map[string]bool{}
	var out [][]taste.ArtistTaste
	for _, anchor := range artists {
		if assigned[anchor.Key] {
			continue
		}
		if len(out) >= MaxDailyMixes {
			break
		}
		cl := []taste.ArtistTaste{anchor}
		assigned[anchor.Key] = true
		// Similar artists first, then same-genre ones.
		for pass := 0; pass < 2 && len(cl) < clusterMax; pass++ {
			for _, a := range artists {
				if assigned[a.Key] || len(cl) >= clusterMax {
					continue
				}
				belongs := false
				for _, m := range cl {
					if pass == 0 && links[m.Key][a.Key] {
						belongs = true
					}
					if pass == 1 && genre[a.Key] != "" && genre[a.Key] == genre[m.Key] {
						belongs = true
					}
				}
				if belongs {
					cl = append(cl, a)
					assigned[a.Key] = true
				}
			}
		}
		out = append(out, cl)
	}
	// Bigger, stronger clusters first.
	sort.SliceStable(out, func(i, j int) bool { return weight(out[i]) > weight(out[j]) })
	return out, nil
}

func weight(cl []taste.ArtistTaste) float64 {
	w := 0.0
	for _, a := range cl {
		w += a.Score
	}
	return w
}
