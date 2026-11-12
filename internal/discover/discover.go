// Package discover builds each user's Discover Weekly: a mix, fresh every
// Monday, of songs the engine thinks they'll like, most of them songs the
// library doesn't have yet. For users allowed to add music, Reverb downloads
// those songs ahead of time so the mix is complete when they press play.
//
// When the week ends, the keep rule decides what happens to the songs Reverb
// downloaded for it:
//
//   - "playlist" (the default) keeps a song only if it was added to a playlist;
//   - "played" also keeps songs the user played through;
//   - "always" keeps everything.
//
// Whatever the rule, a song is never deleted when anyone has it in a playlist,
// someone else played it through, it is still in another user's current
// Discover, or Reverb didn't download it for Discover in the first place
// (it was already in the library, or its download was already there). Only
// regular files inside the music folders are ever removed.
package discover

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/engine"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// Keep rules.
const (
	KeepPlaylist = "playlist"
	KeepPlayed   = "played"
	KeepAlways   = "always"
)

// Item states.
const (
	StateSuggested   = "suggested"   // offered; nothing downloaded
	StateDownloading = "downloading" // Reverb started (or finished) a download for it
	StateKept        = "kept"        // week over, the song stays
	StateRemoved     = "removed"     // week over, Reverb deleted the file
	StateExpired     = "expired"     // week over, the download never landed
)

// Server settings keys (settings table).
const (
	SettingWeeklyCap = "discover_weekly_cap"
	SettingDownloads = "discover_downloads"
	DefaultWeeklyCap = 30
)

// ErrInvalid is a bad prefs update.
var ErrInvalid = errors.New("discover: invalid settings")

// Prefs are a user's Discover Weekly settings.
type Prefs struct {
	Enabled  bool   `json:"enabled"`
	Size     int    `json:"size"`
	NewShare int    `json:"newShare"` // % of songs not in the library
	KeepRule string `json:"keepRule"`
}

// DefaultPrefs apply until the user changes something.
var DefaultPrefs = Prefs{Enabled: true, Size: 30, NewShare: 70, KeepRule: KeepPlaylist}

// Item is one song of the week.
type Item struct {
	ID    string `json:"id"`
	State string `json:"state"`
	// Owned: playable now (it was in the library, or its download landed).
	Owned    bool                 `json:"owned"`
	Track    *core.Track          `json:"track,omitempty"`
	External *core.ExternalResult `json:"external,omitempty"`
	Reason   engine.Reason        `json:"reason"`
	// Download, for songs Reverb is getting: the job and its progress.
	JobID     string `json:"jobId,omitempty"`
	JobStatus string `json:"jobStatus,omitempty"`
	Progress  int    `json:"progress,omitempty"`
}

// Week is a user's current Discover Weekly.
type Week struct {
	Week     string `json:"week"`
	StartsAt int64  `json:"startsAt"`
	EndsAt   int64  `json:"endsAt"`
	Prefs    Prefs  `json:"prefs"`
	// Downloads: whether Reverb downloads this user's new songs ahead of time.
	Downloads bool   `json:"downloads"`
	Items     []Item `json:"items"`
}

// Querier is the slice of the store Discover uses.
type Querier interface {
	GetDiscoverPrefs(ctx context.Context, userID string) (db.DiscoverPref, error)
	UpsertDiscoverPrefs(ctx context.Context, arg db.UpsertDiscoverPrefsParams) error
	ListDiscoverUsers(ctx context.Context) ([]db.ListDiscoverUsersRow, error)
	ListDiscoverItems(ctx context.Context, arg db.ListDiscoverItemsParams) ([]db.DiscoverItem, error)
	InsertDiscoverItem(ctx context.Context, arg db.InsertDiscoverItemParams) error
	UpdateDiscoverItemJob(ctx context.Context, arg db.UpdateDiscoverItemJobParams) error
	UpdateDiscoverItemState(ctx context.Context, arg db.UpdateDiscoverItemStateParams) error
	ListUndecidedDiscoverItems(ctx context.Context, arg db.ListUndecidedDiscoverItemsParams) ([]db.DiscoverItem, error)
	ListDiscoveredExternalIDs(ctx context.Context, userID string) ([]interface{}, error)
	CountDiscoverDownloadsSince(ctx context.Context, arg db.CountDiscoverDownloadsSinceParams) (int64, error)
	CountPlaylistsWithTrack(ctx context.Context, arg db.CountPlaylistsWithTrackParams) (int64, error)
	CountCompletedListens(ctx context.Context, arg db.CountCompletedListensParams) (int64, error)
	CountOtherUsersCompletedListens(ctx context.Context, arg db.CountOtherUsersCompletedListensParams) (int64, error)
	CountPlaylistAddSignals(ctx context.Context, catalogID string) (int64, error)
	CountOtherDiscoverHolds(ctx context.Context, arg db.CountOtherDiscoverHoldsParams) (int64, error)
	GetLibraryTrack(ctx context.Context, id string) (db.LibraryTrack, error)
	GetSetting(ctx context.Context, key string) (string, error)
}

// Engine is the recommendation engine.
type Engine interface {
	Next(ctx context.Context, userID string, req engine.Request) (engine.Result, error)
	Discover(ctx context.Context, userID string, n int, exclude map[string]bool) ([]engine.Candidate, error)
}

// Downloader starts and reports downloads (the download manager).
type Downloader interface {
	Enqueue(ctx context.Context, req core.DownloadRequest) (core.DownloadJob, error)
	Status(ctx context.Context, jobID string) (core.DownloadJob, error)
}

// Options wires the pieces that live outside the store.
type Options struct {
	// Downloads returns the live download manager (nil when none is configured).
	Downloads func() Downloader
	// Remove deletes one downloaded file; it must refuse paths outside the
	// music folders (see SafeRemover).
	Remove func(path string) error
	// Rescan asks the library to pick up removals. May be nil.
	Rescan func(ctx context.Context)
	// Locate finds a library track's file on disk. Some downloaders only
	// report their output folder, so this is the reliable way to the file.
	// May be nil (then the job's output path is used, and Remove refuses
	// anything that isn't a single file).
	Locate func(libraryTrackID string) (string, bool)
}

// Service builds and closes Discover weeks.
type Service struct {
	q     Querier
	eng   Engine
	opt   Options
	now   func() time.Time
	idgen func() string

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// New builds the service.
func New(q Querier, eng Engine, opt Options, now func() time.Time, idgen func() string) *Service {
	return &Service{q: q, eng: eng, opt: opt, now: now, idgen: idgen, locks: map[string]*sync.Mutex{}}
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

// weekOf returns the ISO week key and its [start, end) in server time. Weeks
// start on Monday at midnight.
func weekOf(t time.Time) (string, time.Time, time.Time) {
	y, w := t.ISOWeek()
	day := (int(t.Weekday()) + 6) % 7 // Monday = 0
	start := time.Date(t.Year(), t.Month(), t.Day()-day, 0, 0, 0, 0, t.Location())
	return fmt.Sprintf("%04d-W%02d", y, w), start, start.AddDate(0, 0, 7)
}

// Prefs returns a user's settings (defaults until they change them).
func (s *Service) Prefs(ctx context.Context, userID string) (Prefs, error) {
	r, err := s.q.GetDiscoverPrefs(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultPrefs, nil
	}
	if err != nil {
		return Prefs{}, err
	}
	return Prefs{Enabled: r.Enabled == 1, Size: int(r.Size), NewShare: int(r.NewShare), KeepRule: r.KeepRule}, nil
}

// SetPrefs saves a user's settings. Size and share changes apply from the next week.
func (s *Service) SetPrefs(ctx context.Context, userID string, p Prefs) (Prefs, error) {
	switch p.KeepRule {
	case KeepPlaylist, KeepPlayed, KeepAlways:
	default:
		return Prefs{}, ErrInvalid
	}
	if p.Size < 10 || p.Size > 100 || p.NewShare < 0 || p.NewShare > 100 {
		return Prefs{}, ErrInvalid
	}
	en := int64(0)
	if p.Enabled {
		en = 1
	}
	err := s.q.UpsertDiscoverPrefs(ctx, db.UpsertDiscoverPrefsParams{
		UserID: userID, Enabled: en, Size: int64(p.Size), NewShare: int64(p.NewShare), KeepRule: p.KeepRule, UpdatedAt: s.now().Unix(),
	})
	return p, err
}

// WeeklyCap is the most songs Reverb downloads for one user's Discover per
// week (a manager setting).
func (s *Service) WeeklyCap(ctx context.Context) int {
	v, err := s.q.GetSetting(ctx, SettingWeeklyCap)
	if err != nil {
		return DefaultWeeklyCap
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 {
		return DefaultWeeklyCap
	}
	return n
}

// DownloadsEnabled is the manager switch for Discover downloads (default on).
func (s *Service) DownloadsEnabled(ctx context.Context) bool {
	v, err := s.q.GetSetting(ctx, SettingDownloads)
	return err != nil || v != "false"
}

// mayDownload: the user may add music without approval, downloads are on,
// and a downloader is configured.
func (s *Service) mayDownload(ctx context.Context, userID string) bool {
	if !s.DownloadsEnabled(ctx) || s.opt.Downloads == nil || s.opt.Downloads() == nil {
		return false
	}
	users, err := s.q.ListDiscoverUsers(ctx)
	if err != nil {
		return false
	}
	for _, u := range users {
		if u.ID != userID {
			continue
		}
		if u.IsOwner == 1 {
			return true
		}
		var caps []string
		_ = json.Unmarshal([]byte(u.Capabilities), &caps)
		for _, c := range caps {
			if c == "auto_approve" {
				return true
			}
		}
	}
	return false
}

// Current returns the user's Discover Weekly, closing last week (the keep
// rule) and building this one first when the week has turned.
func (s *Service) Current(ctx context.Context, userID string) (Week, error) {
	l := s.userLock(userID)
	l.Lock()
	defer l.Unlock()
	prefs, err := s.Prefs(ctx, userID)
	if err != nil {
		return Week{}, err
	}
	now := s.now()
	key, start, end := weekOf(now)
	wk := Week{Week: key, StartsAt: start.Unix(), EndsAt: end.Unix(), Prefs: prefs, Downloads: s.mayDownload(ctx, userID), Items: []Item{}}
	// Close earlier weeks whether or not Discover is still on: songs
	// downloaded before it was switched off still follow the keep rule.
	if err := s.closeWeeks(ctx, userID, key, prefs.KeepRule); err != nil {
		return Week{}, err
	}
	if !prefs.Enabled {
		return wk, nil
	}
	rows, err := s.q.ListDiscoverItems(ctx, db.ListDiscoverItemsParams{UserID: userID, Week: key})
	if err != nil {
		return Week{}, err
	}
	if len(rows) == 0 {
		if err := s.build(ctx, userID, prefs, key, start, wk.Downloads); err != nil {
			return Week{}, err
		}
		if rows, err = s.q.ListDiscoverItems(ctx, db.ListDiscoverItemsParams{UserID: userID, Week: key}); err != nil {
			return Week{}, err
		}
	}
	for _, r := range rows {
		wk.Items = append(wk.Items, s.view(ctx, r))
	}
	return wk, nil
}

// RunAll builds (and closes) every user's week. Run from a background loop so
// downloads are done before people open the app on Monday.
func (s *Service) RunAll(ctx context.Context) {
	users, err := s.q.ListDiscoverUsers(ctx)
	if err != nil {
		log.Printf("discover: list users: %v", err)
		return
	}
	for _, u := range users {
		if ctx.Err() != nil {
			return
		}
		if _, err := s.Current(ctx, u.ID); err != nil {
			log.Printf("discover: user %s: %v", u.ID, err)
		}
	}
}

// Start runs RunAll after initialDelay and then every interval.
func (s *Service) Start(ctx context.Context, initialDelay, interval time.Duration) {
	go func() {
		t := time.NewTimer(initialDelay)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.RunAll(ctx)
				t.Reset(interval)
			}
		}
	}()
}

func (s *Service) build(ctx context.Context, userID string, p Prefs, week string, start time.Time, downloads bool) error {
	nNew := int(math.Round(float64(p.Size) * float64(p.NewShare) / 100))
	exclude := map[string]bool{}
	refs, err := s.q.ListDiscoveredExternalIDs(ctx, userID)
	if err != nil {
		return err
	}
	for _, r := range refs {
		if v, ok := r.(string); ok {
			exclude[v] = true
		}
	}
	ext, err := s.eng.Discover(ctx, userID, nNew, exclude)
	if err != nil {
		return err
	}
	// Whatever the outside world couldn't fill comes from the library: songs
	// the user has never played, near what they like.
	nOwned := p.Size - len(ext)
	var own []engine.Candidate
	if nOwned > 0 {
		res, err := s.eng.Next(ctx, userID, engine.Request{Lane: engine.LaneNewToYou, N: nOwned})
		if err != nil && !errors.Is(err, engine.ErrInvalid) {
			return err
		}
		own = res.Items
	}
	items := spread(own, ext)

	budget := 0
	if downloads {
		used, err := s.q.CountDiscoverDownloadsSince(ctx, db.CountDiscoverDownloadsSinceParams{UserID: userID, CreatedAt: start.Unix()})
		if err != nil {
			return err
		}
		budget = s.WeeklyCap(ctx) - int(used)
	}
	built := s.now()
	for i, c := range items {
		raw, _ := json.Marshal(c)
		row := db.InsertDiscoverItemParams{
			ID: s.idgen(), UserID: userID, Week: week, Position: int64(i), ItemJson: string(raw), State: StateSuggested, CreatedAt: built.Unix(),
		}
		switch {
		case c.Track != nil:
			row.Owned, row.LibraryTrackID = 1, c.Track.ID
			row.Title, row.Artist, row.Album, row.Isrc, row.DurationMs = c.Track.Title, c.Track.Artist, c.Track.Album, c.Track.ISRC, int64(c.Track.DurationMs)
		case c.External != nil:
			e := c.External
			row.Source, row.ExternalID, row.Title, row.Artist, row.Album = e.Source, e.ExternalID, e.Title, e.Artist, e.Album
			row.CoverUrl, row.Isrc, row.DurationMs = e.CoverURL, e.ISRC, int64(e.DurationMs)
		default:
			continue
		}
		if err := s.q.InsertDiscoverItem(ctx, row); err != nil {
			return err
		}
		if c.External != nil && budget > 0 {
			if s.download(ctx, row, built) {
				budget--
			}
		}
	}
	return nil
}

// download starts the download for an external item. A job that already
// existed (someone downloaded the song before) is linked but marked
// preexisting, so the cleanup will never delete it.
func (s *Service) download(ctx context.Context, row db.InsertDiscoverItemParams, built time.Time) bool {
	dl := s.opt.Downloads()
	if dl == nil {
		return false
	}
	job, err := dl.Enqueue(ctx, core.DownloadRequest{
		Source: row.Source, ExternalID: row.ExternalID, Artist: row.Artist, Title: row.Title, Album: row.Album,
		ISRC: row.Isrc, DurationMs: int(row.DurationMs),
	})
	if err != nil {
		log.Printf("discover: download %q: %v", row.Title, err)
		return false
	}
	pre := int64(0)
	if job.CreatedAt < built.Unix() {
		pre = 1
	}
	if err := s.q.UpdateDiscoverItemJob(ctx, db.UpdateDiscoverItemJobParams{JobID: job.ID, Preexisting: pre, State: StateDownloading, ID: row.ID}); err != nil {
		return false
	}
	return pre == 0
}

// spread places the new songs evenly through the owned ones.
func spread(own, ext []engine.Candidate) []engine.Candidate {
	out := make([]engine.Candidate, 0, len(own)+len(ext))
	total := len(own) + len(ext)
	i, j := 0, 0
	for k := 0; k < total; k++ {
		// Take from ext when it is "behind" its share of the list so far.
		if j < len(ext) && (i >= len(own) || float64(j)*float64(total) <= float64(k)*float64(len(ext))) {
			out = append(out, ext[j])
			j++
		} else {
			out = append(out, own[i])
			i++
		}
	}
	return out
}

func (s *Service) job(ctx context.Context, id string) (core.DownloadJob, bool) {
	if id == "" || s.opt.Downloads == nil {
		return core.DownloadJob{}, false
	}
	dl := s.opt.Downloads()
	if dl == nil {
		return core.DownloadJob{}, false
	}
	j, err := dl.Status(ctx, id)
	if err != nil {
		return core.DownloadJob{}, false
	}
	return j, true
}

// view turns a stored row into what the UI shows, following its download.
func (s *Service) view(ctx context.Context, r db.DiscoverItem) Item {
	var c engine.Candidate
	_ = json.Unmarshal([]byte(r.ItemJson), &c)
	it := Item{ID: r.ID, State: r.State, Reason: c.Reason, JobID: r.JobID}
	if r.Owned == 1 && c.Track != nil {
		it.Owned, it.Track = true, c.Track
		return it
	}
	it.External = c.External
	if j, ok := s.job(ctx, r.JobID); ok {
		it.JobStatus, it.Progress = string(j.Status), j.Progress
		if j.LibraryTrackID != "" && r.State != StateRemoved {
			it.Owned = true
			it.Track = s.libraryTrack(ctx, j)
		}
	}
	return it
}

func (s *Service) libraryTrack(ctx context.Context, j core.DownloadJob) *core.Track {
	if t, err := s.q.GetLibraryTrack(ctx, j.LibraryTrackID); err == nil {
		return &core.Track{
			ID: t.LibraryTrackID, Title: t.Title, AlbumID: t.AlbumID, Album: t.Album, ArtistID: t.ArtistID, Artist: t.Artist,
			CoverArtID: t.CoverArtID, TrackNumber: int(t.TrackNumber), DiscNumber: int(t.DiscNumber), DurationMs: int(t.DurationMs),
			BitRate: int(t.BitRate), Suffix: t.Suffix, ContentType: t.ContentType, ISRC: t.Isrc,
		}
	}
	// Not indexed yet: the job knows enough to play it.
	return &core.Track{ID: j.LibraryTrackID, Title: j.Title, Artist: j.Artist, Album: j.Album, CoverArtID: j.CoverArtID, DurationMs: j.DurationMs, ISRC: j.ISRC}
}

// closeWeeks applies the keep rule to songs downloaded for earlier weeks.
func (s *Service) closeWeeks(ctx context.Context, userID, currentWeek, rule string) error {
	rows, err := s.q.ListUndecidedDiscoverItems(ctx, db.ListUndecidedDiscoverItemsParams{UserID: userID, Week: currentWeek})
	if err != nil {
		return err
	}
	removed := false
	now := s.now().Unix()
	for _, r := range rows {
		state := StateKept
		j, ok := s.job(ctx, r.JobID)
		switch {
		case !ok:
			// The downloader is gone or the job vanished: leave the file alone.
			state = StateKept
		case j.Status == core.DownloadFailed:
			state = StateExpired
		case j.Status != core.DownloadCompleted || j.LibraryTrackID == "":
			// Still in flight or not scanned yet: decide next time.
			continue
		default:
			keep, err := s.keep(ctx, userID, r, j, rule)
			if err != nil {
				return err
			}
			if !keep {
				path := j.OutputPath
				if s.opt.Locate != nil {
					if p, ok := s.opt.Locate(j.LibraryTrackID); ok {
						path = p
					}
				}
				if err := s.opt.Remove(path); err != nil {
					log.Printf("discover: not removing %q: %v", path, err)
				} else {
					state = StateRemoved
					removed = true
					log.Printf("discover: removed %q by %q (not kept under the %q rule)", j.Title, j.Artist, rule)
				}
			}
		}
		if err := s.q.UpdateDiscoverItemState(ctx, db.UpdateDiscoverItemStateParams{State: state, DecidedAt: now, ID: r.ID}); err != nil {
			return err
		}
	}
	if removed && s.opt.Rescan != nil {
		s.opt.Rescan(ctx)
	}
	return nil
}

// keep decides whether a downloaded Discover song stays.
func (s *Service) keep(ctx context.Context, userID string, r db.DiscoverItem, j core.DownloadJob, rule string) (bool, error) {
	lib, cat := j.LibraryTrackID, j.CanonicalID
	// Safety first, whatever the rule: anyone's playlist, anyone else's
	// listening, anyone else's current Discover.
	n, err := s.q.CountPlaylistsWithTrack(ctx, db.CountPlaylistsWithTrackParams{LibraryTrackID: lib, ExternalID: r.ExternalID})
	if err != nil || n > 0 {
		return true, err
	}
	if cat != "" {
		if n, err = s.q.CountPlaylistAddSignals(ctx, cat); err != nil || n > 0 {
			return true, err
		}
	}
	if n, err = s.q.CountOtherUsersCompletedListens(ctx, db.CountOtherUsersCompletedListensParams{UserID: userID, LibraryTrackID: lib, CatalogID: cat}); err != nil || n > 0 {
		return true, err
	}
	if n, err = s.q.CountOtherDiscoverHolds(ctx, db.CountOtherDiscoverHoldsParams{UserID: userID, JobID: j.ID}); err != nil || n > 0 {
		return true, err
	}
	switch rule {
	case KeepAlways:
		return true, nil
	case KeepPlayed:
		n, err := s.q.CountCompletedListens(ctx, db.CountCompletedListensParams{UserID: userID, LibraryTrackID: lib, CatalogID: cat})
		return n > 0, err
	default:
		return false, nil
	}
}
