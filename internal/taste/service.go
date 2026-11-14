package taste

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/maxjb-xyz/reverb/internal/catalog"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// Window is how far back Build looks. Older history has decayed to noise.
const Window = 2 * 365 * 24 * time.Hour

// cacheTTL bounds how stale a cached profile can be. Every write for a user
// invalidates that user's entry immediately; the TTL only matters for decay.
const cacheTTL = 5 * time.Minute

// ErrInvalid is returned for malformed input (unknown outcome, missing title...).
var ErrInvalid = errors.New("taste: invalid input")

// Querier is the slice of *db.Queries the service needs.
type Querier interface {
	InsertListenEvent(ctx context.Context, arg db.InsertListenEventParams) error
	ListListenEventsSince(ctx context.Context, arg db.ListListenEventsSinceParams) ([]db.ListListenEventsSinceRow, error)
	EarliestListenEvent(ctx context.Context, userID string) (int64, error)
	ListPlaysForTaste(ctx context.Context, arg db.ListPlaysForTasteParams) ([]db.ListPlaysForTasteRow, error)
	InsertTasteSignal(ctx context.Context, arg db.InsertTasteSignalParams) error
	ListTasteSignalsSince(ctx context.Context, arg db.ListTasteSignalsSinceParams) ([]db.ListTasteSignalsSinceRow, error)
	UpsertTasteFeedback(ctx context.Context, arg db.UpsertTasteFeedbackParams) error
	DeleteTasteFeedback(ctx context.Context, arg db.DeleteTasteFeedbackParams) error
	ListTasteFeedback(ctx context.Context, userID string) ([]db.ListTasteFeedbackRow, error)
}

// CanonicalMinter mints catalog IDs. *catalog.Service satisfies it.
type CanonicalMinter interface {
	CanonicalFor(ctx context.Context, id catalog.Identity) (string, error)
}

// TrackRef identifies a track the way the rest of Reverb does: library tracks
// carry metadata only (exactly like play.Record, so they land on the same
// catalog entity as plays); external tracks may add Source/ExternalID.
type TrackRef struct {
	LibraryTrackID string `json:"libraryTrackId,omitempty"`
	Title          string `json:"title"`
	Artist         string `json:"artist"`
	Album          string `json:"album,omitempty"`
	ISRC           string `json:"isrc,omitempty"`
	DurationMs     int    `json:"durationMs,omitempty"`
	Source         string `json:"source,omitempty"`
	ExternalID     string `json:"externalId,omitempty"`
}

// ListenInput is one finished listen as the web player reports it.
type ListenInput struct {
	TrackRef
	MsPlayed    int    `json:"msPlayed"`
	Outcome     string `json:"outcome"`
	ContextKind string `json:"contextKind,omitempty"`
	ContextID   string `json:"contextId,omitempty"`
	StartedAt   int64  `json:"startedAt,omitempty"` // unix seconds; 0 = now
}

// FeedbackInput is a thumbs-down request. Kind "track" needs the track's
// metadata; kind "artist" needs only Artist.
type FeedbackInput struct {
	TargetKind string `json:"targetKind"`
	TrackRef
	LibraryArtistID string `json:"libraryArtistId,omitempty"`
}

type cached struct {
	p  *Profile
	at time.Time
}

// Service records taste events and serves per-user profiles.
type Service struct {
	q     Querier
	cat   CanonicalMinter
	now   func() time.Time
	idgen func() string

	mu    sync.Mutex
	cache map[string]cached
}

// NewService constructs a Service.
func NewService(q Querier, cat CanonicalMinter, now func() time.Time, idgen func() string) *Service {
	return &Service{q: q, cat: cat, now: now, idgen: idgen, cache: map[string]cached{}}
}

func (s *Service) invalidate(userID string) {
	s.mu.Lock()
	delete(s.cache, userID)
	s.mu.Unlock()
}

func (s *Service) canonical(ctx context.Context, t TrackRef) (string, error) {
	return s.cat.CanonicalFor(ctx, catalog.Identity{
		Kind:       "track",
		Title:      t.Title,
		Artist:     t.Artist,
		Album:      t.Album,
		ISRC:       t.ISRC,
		DurationMs: t.DurationMs,
		Source:     t.Source,
		ExternalID: t.ExternalID,
	})
}

// RecordListen stores one listen outcome.
func (s *Service) RecordListen(ctx context.Context, userID string, in ListenInput) error {
	if strings.TrimSpace(in.Title) == "" || in.MsPlayed < 0 {
		return ErrInvalid
	}
	outcome, ok := NormalizeOutcome(in.Outcome, in.MsPlayed, in.DurationMs)
	if !ok {
		return ErrInvalid
	}
	cid, err := s.canonical(ctx, in.TrackRef)
	if err != nil {
		return err
	}
	now := s.now().Unix()
	started := in.StartedAt
	if started <= 0 || started > now {
		started = now
	}
	err = s.q.InsertListenEvent(ctx, db.InsertListenEventParams{
		ID:             s.idgen(),
		UserID:         userID,
		CatalogID:      cid,
		LibraryTrackID: in.LibraryTrackID,
		ArtistKey:      ArtistKey(in.Artist),
		StartedAt:      started,
		MsPlayed:       int64(in.MsPlayed),
		DurationMs:     int64(in.DurationMs),
		Outcome:        outcome,
		ContextKind:    clip(in.ContextKind, 32),
		ContextID:      clip(in.ContextID, 200),
		CreatedAt:      now,
	})
	if err == nil {
		s.invalidate(userID)
	}
	return err
}

// RecordSignal stores a playlist add / download / request. With an empty
// Title the signal is artist-level (e.g. an album or artist download).
func (s *Service) RecordSignal(ctx context.Context, userID, kind string, t TrackRef) error {
	switch kind {
	case SignalPlaylistAdd, SignalDownload, SignalRequest:
	default:
		return ErrInvalid
	}
	key := ArtistKey(t.Artist)
	cid := ""
	if strings.TrimSpace(t.Title) != "" {
		var err error
		if cid, err = s.canonical(ctx, t); err != nil {
			return err
		}
	} else if key == "" {
		return ErrInvalid
	}
	err := s.q.InsertTasteSignal(ctx, db.InsertTasteSignalParams{
		ID:         s.idgen(),
		UserID:     userID,
		Kind:       kind,
		CatalogID:  cid,
		ArtistKey:  key,
		ArtistName: clip(strings.TrimSpace(t.Artist), 300),
		CreatedAt:  s.now().Unix(),
	})
	if err == nil {
		s.invalidate(userID)
	}
	return err
}

// SetFeedback records a thumbs-down and returns the stored row.
func (s *Service) SetFeedback(ctx context.Context, userID string, in FeedbackInput) (Feedback, error) {
	f := Feedback{TargetKind: in.TargetKind, CreatedAt: s.now().Unix()}
	switch in.TargetKind {
	case TargetTrack:
		if strings.TrimSpace(in.Title) == "" {
			return Feedback{}, ErrInvalid
		}
		cid, err := s.canonical(ctx, in.TrackRef)
		if err != nil {
			return Feedback{}, err
		}
		f.TargetKey, f.Label, f.Sublabel, f.LibraryID = cid, in.Title, in.Artist, in.LibraryTrackID
	case TargetArtist:
		key := ArtistKey(in.Artist)
		if key == "" {
			return Feedback{}, ErrInvalid
		}
		f.TargetKey, f.Label, f.LibraryID = key, strings.TrimSpace(in.Artist), in.LibraryArtistID
	default:
		return Feedback{}, ErrInvalid
	}
	err := s.q.UpsertTasteFeedback(ctx, db.UpsertTasteFeedbackParams{
		UserID:     userID,
		TargetKind: f.TargetKind,
		TargetKey:  f.TargetKey,
		Label:      clip(f.Label, 300),
		Sublabel:   clip(f.Sublabel, 300),
		LibraryID:  f.LibraryID,
		CreatedAt:  f.CreatedAt,
	})
	if err != nil {
		return Feedback{}, err
	}
	s.invalidate(userID)
	return f, nil
}

// ClearFeedback removes a thumbs-down. Removing one that does not exist is a no-op.
func (s *Service) ClearFeedback(ctx context.Context, userID, kind, key string) error {
	if kind != TargetTrack && kind != TargetArtist {
		return ErrInvalid
	}
	err := s.q.DeleteTasteFeedback(ctx, db.DeleteTasteFeedbackParams{UserID: userID, TargetKind: kind, TargetKey: key})
	if err == nil {
		s.invalidate(userID)
	}
	return err
}

// ListFeedback lists a user's thumbs-downs, newest first.
func (s *Service) ListFeedback(ctx context.Context, userID string) ([]Feedback, error) {
	rows, err := s.q.ListTasteFeedback(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]Feedback, 0, len(rows))
	for _, r := range rows {
		out = append(out, Feedback{
			TargetKind: r.TargetKind, TargetKey: r.TargetKey, Label: r.Label,
			Sublabel: r.Sublabel, LibraryID: r.LibraryID, CreatedAt: r.CreatedAt,
		})
	}
	return out, nil
}

// Profile returns the user's taste profile, from cache when fresh.
func (s *Service) Profile(ctx context.Context, userID string) (*Profile, error) {
	at := s.now()
	s.mu.Lock()
	if c, ok := s.cache[userID]; ok && at.Sub(c.at) < cacheTTL {
		s.mu.Unlock()
		return c.p, nil
	}
	s.mu.Unlock()

	in, err := s.load(ctx, userID, at)
	if err != nil {
		return nil, err
	}
	p := Build(userID, at, in)
	s.mu.Lock()
	s.cache[userID] = cached{p: p, at: at}
	s.mu.Unlock()
	return p, nil
}

func (s *Service) load(ctx context.Context, userID string, at time.Time) (Inputs, error) {
	since := at.Add(-Window).Unix()
	var in Inputs

	evs, err := s.q.ListListenEventsSince(ctx, db.ListListenEventsSinceParams{UserID: userID, StartedAt: since})
	if err != nil {
		return in, err
	}
	for _, e := range evs {
		in.Listens = append(in.Listens, Listen{
			CatalogID: e.CatalogID, LibraryTrackID: e.LibraryTrackID, Title: e.Title, Artist: e.Artist,
			ArtistKey: e.ArtistKey, StartedAt: e.StartedAt, MsPlayed: e.MsPlayed, DurationMs: e.DurationMs,
			Outcome: e.Outcome, ContextKind: e.ContextKind, ContextID: e.ContextID,
		})
	}

	// Legacy plays count only before the listen log starts, so a listen and the
	// play the same playback also recorded are never double-counted.
	until := at.Unix() + 1
	if earliest, err := s.q.EarliestListenEvent(ctx, userID); err != nil {
		return in, err
	} else if earliest > 0 {
		until = earliest
	}
	plays, err := s.q.ListPlaysForTaste(ctx, db.ListPlaysForTasteParams{UserID: userID, PlayedAt: since, PlayedAt_2: until})
	if err != nil {
		return in, err
	}
	for _, p := range plays {
		in.Plays = append(in.Plays, Play{CatalogID: p.CatalogID, Title: p.Title, Artist: p.Artist, PlayedAt: p.PlayedAt, Completed: p.Completed != 0})
	}

	sigs, err := s.q.ListTasteSignalsSince(ctx, db.ListTasteSignalsSinceParams{UserID: userID, CreatedAt: since})
	if err != nil {
		return in, err
	}
	for _, g := range sigs {
		in.Signals = append(in.Signals, Signal{Kind: g.Kind, CatalogID: g.CatalogID, Title: g.Title, Artist: g.Artist, ArtistKey: g.ArtistKey, CreatedAt: g.CreatedAt})
	}

	in.Feedback, err = s.ListFeedback(ctx, userID)
	return in, err
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
