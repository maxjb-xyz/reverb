// Package libindex keeps the recommendation engine's local index of the
// library — every track with its artist, genres, year and added date — and
// the artist-similarity map ("fans of X also play Y") fetched from external
// sources such as Deezer. The engine draws candidates from both. The index is
// derived: it is rebuilt from the library adapter, never the source of record.
package libindex

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/library"
	"github.com/maxjb-xyz/reverb/internal/matching"
	"github.com/maxjb-xyz/reverb/internal/search"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// SimilarSource is a search source that can resolve an artist and list
// similar artists. Deezer satisfies it.
type SimilarSource interface {
	Name() string
	Search(ctx context.Context, q string, t core.EntityType) ([]core.ExternalResult, error)
	search.SimilarArtistsProvider
}

// Live returns the currently wired library and search sources. Called on
// every run so the index follows adapter hot-reloads.
type Live func() (library.LibraryAdapter, []search.SearchSource)

// ErrNoWalker is returned when the library adapter can't list its tracks.
var ErrNoWalker = errors.New("libindex: library adapter cannot list tracks")

// ErrBusy is returned when a run is already in progress.
var ErrBusy = errors.New("libindex: already running")

// Status describes the index for the settings screen and the engine.
type Status struct {
	Running            bool   `json:"running"`
	Tracks             int64  `json:"tracks"`
	Artists            int64  `json:"artists"`
	ArtistsWithSimilar int64  `json:"artistsWithSimilar"`
	LastIndexedAt      int64  `json:"lastIndexedAt"`
	LastError          string `json:"lastError,omitempty"`
}

// Options tunes the service. Zero values take the defaults.
type Options struct {
	SimilarTTL   time.Duration // refetch an artist's similar list after this long (30 days)
	SimilarLimit int           // similar artists stored per artist (25)
	Pace         time.Duration // pause between external calls (250 ms; Deezer allows 50 per 5 s)
	BatchPerRun  int           // artists refreshed per run (500)
}

// Service builds and serves the index.
type Service struct {
	sqldb *sql.DB
	q     *db.Queries
	live  Live
	now   func() time.Time
	opt   Options
	sleep func(context.Context, time.Duration) error

	kick chan struct{}

	mu      sync.Mutex
	running bool
	lastAt  int64
	lastErr string
}

// New constructs a Service over the app database.
func New(sqldb *sql.DB, live Live, now func() time.Time, opt Options) *Service {
	if opt.SimilarTTL <= 0 {
		opt.SimilarTTL = 30 * 24 * time.Hour
	}
	if opt.SimilarLimit <= 0 {
		opt.SimilarLimit = 25
	}
	if opt.Pace < 0 {
		opt.Pace = 0
	} else if opt.Pace == 0 {
		opt.Pace = 250 * time.Millisecond
	}
	if opt.BatchPerRun <= 0 {
		opt.BatchPerRun = 500
	}
	return &Service{sqldb: sqldb, q: db.New(sqldb), live: live, now: now, opt: opt, sleep: sleepCtx, kick: make(chan struct{}, 1)}
}

// Kick asks the background loop (see Start) to run soon, e.g. after the
// library or search adapters were reconfigured. Never blocks.
func (s *Service) Kick() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// NoPace disables the pause between external calls (tests).
func (s *Service) NoPace() *Service {
	s.sleep = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	return s
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Run rebuilds the track index, then refreshes the similarity map. It returns
// ErrBusy if a run is already in progress.
func (s *Service) Run(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return ErrBusy
	}
	s.running = true
	s.mu.Unlock()

	err := s.rebuild(ctx)
	if err == nil {
		_, err = s.refreshSimilar(ctx)
	}

	s.mu.Lock()
	s.running = false
	if err != nil {
		s.lastErr = err.Error()
	} else {
		s.lastErr = ""
	}
	s.mu.Unlock()
	return err
}

// Status reports the index state.
func (s *Service) Status(ctx context.Context) (Status, error) {
	s.mu.Lock()
	st := Status{Running: s.running, LastIndexedAt: s.lastAt, LastError: s.lastErr}
	s.mu.Unlock()
	c, err := s.q.CountLibraryIndex(ctx)
	if err != nil {
		return st, err
	}
	st.Tracks, st.Artists = c.Tracks, c.Artists
	st.ArtistsWithSimilar, err = s.q.CountArtistsWithSimilar(ctx)
	return st, err
}

// rebuild walks the library and upserts every track under a fresh epoch, then
// drops tracks the walk didn't see. A failed walk drops nothing.
func (s *Service) rebuild(ctx context.Context) error {
	lib, _ := s.live()
	if lib == nil {
		return nil // no library configured: nothing to index
	}
	walker, ok := lib.(library.TrackWalker)
	if !ok {
		return ErrNoWalker
	}
	epoch := s.now().UnixNano()
	seen := 0
	err := walker.WalkTracks(ctx, func(batch []library.IndexedTrack) error {
		seen += len(batch)
		return s.upsertBatch(ctx, batch, epoch)
	})
	if err != nil {
		return fmt.Errorf("libindex: walk library: %w", err)
	}
	if seen == 0 {
		// An empty walk from a library that had tracks is far more likely a
		// transient server problem than everything being deleted. Keep the index.
		if c, err := s.q.CountLibraryIndex(ctx); err == nil && c.Tracks > 0 {
			log.Printf("libindex: library walk returned no tracks; keeping the existing %d-track index", c.Tracks)
			return nil
		}
	}
	if _, err := s.q.DeleteStaleLibraryTracks(ctx, epoch); err != nil {
		return err
	}
	if err := s.q.DeleteOrphanLibraryTrackGenres(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	s.lastAt = s.now().Unix()
	s.mu.Unlock()
	log.Printf("libindex: indexed %d library tracks", seen)
	return nil
}

func (s *Service) upsertBatch(ctx context.Context, batch []library.IndexedTrack, epoch int64) error {
	tx, err := s.sqldb.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.q.WithTx(tx)
	for _, t := range batch {
		if t.ID == "" {
			continue
		}
		if err := q.UpsertLibraryTrack(ctx, db.UpsertLibraryTrackParams{
			LibraryTrackID: t.ID,
			Title:          t.Title,
			Artist:         t.Artist,
			ArtistKey:      matching.ArtistKey(t.Artist),
			ArtistID:       t.ArtistID,
			Album:          t.Album,
			AlbumID:        t.AlbumID,
			CoverArtID:     t.CoverArtID,
			TrackNumber:    int64(t.TrackNumber),
			DiscNumber:     int64(t.DiscNumber),
			DurationMs:     int64(t.DurationMs),
			BitRate:        int64(t.BitRate),
			Suffix:         t.Suffix,
			ContentType:    t.ContentType,
			Isrc:           t.ISRC,
			Year:           int64(t.Year),
			Genres:         strings.Join(t.Genres, "; "),
			AddedAt:        t.AddedAt,
			Epoch:          epoch,
		}); err != nil {
			return err
		}
		if err := q.DeleteLibraryTrackGenres(ctx, t.ID); err != nil {
			return err
		}
		for _, g := range t.Genres {
			if k := matching.GenreKey(g); k != "" {
				if err := q.InsertLibraryTrackGenre(ctx, db.InsertLibraryTrackGenreParams{LibraryTrackID: t.ID, GenreKey: k}); err != nil {
					return err
				}
			}
		}
	}
	return tx.Commit()
}

// refreshSimilar fetches similar artists for library artists whose entry is
// missing or stale, for every source that supports it. Returns how many
// artists were refreshed.
func (s *Service) refreshSimilar(ctx context.Context) (int, error) {
	_, sources := s.live()
	done := 0
	for _, src := range sources {
		ss, ok := src.(SimilarSource)
		if !ok {
			continue
		}
		n, err := s.refreshSource(ctx, ss)
		done += n
		if err != nil {
			return done, err
		}
	}
	return done, nil
}

func (s *Service) refreshSource(ctx context.Context, src SimilarSource) (int, error) {
	cutoff := s.now().Add(-s.opt.SimilarTTL).Unix()
	rows, err := s.q.ListArtistsNeedingSimilar(ctx, db.ListArtistsNeedingSimilarParams{
		Source: src.Name(), FetchedAt: cutoff, Limit: int64(s.opt.BatchPerRun),
	})
	if err != nil {
		return 0, err
	}
	done := 0
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return done, err
		}
		if err := s.refreshArtist(ctx, src, row.ArtistKey, row.Name); err != nil {
			// One artist failing (rate limit, odd name) must not stop the map.
			log.Printf("libindex: %s similar artists for %q: %v", src.Name(), row.Name, err)
		}
		done++
	}
	return done, nil
}

// refreshArtist resolves one library artist on src and stores its similar
// artists. "No confident match" is recorded too, so it isn't retried until
// the TTL passes.
func (s *Service) refreshArtist(ctx context.Context, src SimilarSource, key, name string) error {
	now := s.now().Unix()
	if err := s.sleep(ctx, s.opt.Pace); err != nil {
		return err
	}
	cands, err := src.Search(ctx, name, core.EntityArtist)
	if err != nil {
		return err
	}
	extID, extName := "", ""
	for _, c := range cands {
		if matching.ArtistKey(c.Title) == key {
			extID, extName = c.ExternalID, c.Title
			break
		}
	}
	if err := s.q.UpsertArtistExternal(ctx, db.UpsertArtistExternalParams{
		ArtistKey: key, Source: src.Name(), ExternalID: extID, Name: extName, FetchedAt: now,
	}); err != nil {
		return err
	}
	if extID == "" {
		return nil
	}
	if err := s.sleep(ctx, s.opt.Pace); err != nil {
		return err
	}
	similar, err := src.SimilarArtists(ctx, extID, s.opt.SimilarLimit)
	if err != nil {
		return err
	}
	tx, err := s.sqldb.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.q.WithTx(tx)
	if err := q.DeleteArtistSimilar(ctx, db.DeleteArtistSimilarParams{ArtistKey: key, Source: src.Name()}); err != nil {
		return err
	}
	for i, a := range similar {
		sk := matching.ArtistKey(a.Name)
		if sk == "" || sk == key {
			continue
		}
		if err := q.InsertArtistSimilar(ctx, db.InsertArtistSimilarParams{
			ArtistKey: key, Source: src.Name(), SimilarKey: sk, SimilarName: a.Name,
			SimilarExternalID: a.ExternalID, SimilarImage: a.CoverURL, Rank: int64(i), FetchedAt: now,
		}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SimilarArtist is one entry of an artist's similarity list.
type SimilarArtist struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Source      string `json:"source"`
	ExternalID  string `json:"externalId,omitempty"`
	ImageURL    string `json:"imageUrl,omitempty"`
	Rank        int    `json:"rank"`
	OwnedTracks int    `json:"ownedTracks"`
	// LibraryArtistID is set when the artist is in the library.
	LibraryArtistID string `json:"libraryArtistId,omitempty"`
}

// Similar returns the stored similar artists for an artist key, best first,
// merged across sources (an artist listed by two sources keeps its best rank).
func (s *Service) Similar(ctx context.Context, artistKey string) ([]SimilarArtist, error) {
	rows, err := s.q.ListSimilarArtists(ctx, artistKey)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := make([]SimilarArtist, 0, len(rows))
	for _, r := range rows {
		if seen[r.SimilarKey] {
			continue
		}
		seen[r.SimilarKey] = true
		out = append(out, SimilarArtist{
			Key: r.SimilarKey, Name: r.SimilarName, Source: r.Source, ExternalID: r.SimilarExternalID,
			ImageURL: r.SimilarImage, Rank: int(r.Rank), OwnedTracks: int(r.OwnedTracks), LibraryArtistID: r.LibraryArtistID,
		})
	}
	return out, nil
}

// Start runs the index in the background: once shortly after boot, again
// (debounced) whenever the library changes, and on a fixed interval so the
// similarity map keeps filling in. libraryChanged may be nil.
func (s *Service) Start(ctx context.Context, libraryChanged <-chan struct{}, initialDelay, interval time.Duration) {
	go func() {
		run := func() {
			if err := s.Run(ctx); err != nil && !errors.Is(err, ErrBusy) && ctx.Err() == nil {
				log.Printf("libindex: %v", err)
			}
		}
		timer := time.NewTimer(initialDelay)
		defer timer.Stop()
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				run()
			case <-tick.C:
				run()
			case <-s.kick:
				run()
			case _, ok := <-libraryChanged:
				if !ok {
					libraryChanged = nil
					continue
				}
				// Debounce bursts of scans (a batch download finishes many jobs).
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(30 * time.Second)
			}
		}
	}()
}
