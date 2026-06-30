package scrobble

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// maxAttempts is the number of attempts before a queue row is permanently failed.
const maxAttempts = 6

// provider is the canonical provider name for Last.fm in this service.
const provider = "lastfm"

// ----------------------------------------------------------------------------
// Querier — the subset of db.Queries used by Service.
// db.Queries satisfies this interface automatically.
// ----------------------------------------------------------------------------

// Querier is the minimal data-layer interface consumed by Service.
// It is implemented by *db.Queries (the real store) and by fakes in tests.
type Querier interface {
	// link operations
	UpsertScrobbleLink(ctx context.Context, arg db.UpsertScrobbleLinkParams) error
	GetScrobbleLink(ctx context.Context, arg db.GetScrobbleLinkParams) (db.ScrobbleLink, error)
	ListScrobbleLinks(ctx context.Context, userID string) ([]db.ScrobbleLink, error)
	DeleteScrobbleLink(ctx context.Context, arg db.DeleteScrobbleLinkParams) error
	SetScrobbleLinkStatus(ctx context.Context, arg db.SetScrobbleLinkStatusParams) error

	// queue operations
	InsertScrobbleQueue(ctx context.Context, arg db.InsertScrobbleQueueParams) error
	SelectDueScrobbles(ctx context.Context, arg db.SelectDueScrobblesParams) ([]db.ScrobbleQueue, error)
	MarkScrobbleDone(ctx context.Context, id string) error
	MarkScrobbleRetry(ctx context.Context, arg db.MarkScrobbleRetryParams) error
	MarkScrobbleFailed(ctx context.Context, id string) error
}

// ----------------------------------------------------------------------------
// Link — public view of a scrobble link (no SessionKey)
// ----------------------------------------------------------------------------

// Link is the public representation of a provider link. SessionKey is
// intentionally omitted to prevent accidental leakage.
type Link struct {
	Provider string
	Username string
	Status   string
}

// ----------------------------------------------------------------------------
// Service
// ----------------------------------------------------------------------------

// Service orchestrates Last.fm scrobbling per-user: auth, queueing, and the
// background worker that drains the queue.
type Service struct {
	q     Querier
	sc    Scrobbler
	cfg   func() Creds // returns app-level Creds{APIKey, APISecret}
	now   func() time.Time
	idgen func() string
}

// NewService constructs a Service.
//
//   - q: data layer (real db.Queries or test fake)
//   - sc: Scrobbler adapter (e.g. lastfm.Adapter)
//   - cfg: returns the app-level Creds (APIKey + APISecret only; SessionKey is per-user)
//   - now: clock function (use time.Now in production; override in tests)
//   - idgen: ID generator for queue rows (use uuid.NewString; override in tests)
func NewService(q Querier, sc Scrobbler, cfg func() Creds, now func() time.Time, idgen func() string) *Service {
	return &Service{q: q, sc: sc, cfg: cfg, now: now, idgen: idgen}
}

// ----------------------------------------------------------------------------
// Auth passthroughs
// ----------------------------------------------------------------------------

// IsConfigured reports whether the app-level API key and secret are both set.
// Used by the /scrobble/links handler to populate the "configured" field.
func (s *Service) IsConfigured() bool {
	c := s.cfg()
	return c.APIKey != "" && c.APISecret != ""
}

// AuthURL starts the OAuth-style token flow for the provider.
// Returns an error if the app API key or secret are not configured.
func (s *Service) AuthURL(ctx context.Context) (authURL, token string, err error) {
	c := s.cfg()
	if c.APIKey == "" || c.APISecret == "" {
		return "", "", fmt.Errorf("lastfm not configured: api_key and api_secret required")
	}
	return s.sc.AuthURL(ctx, c)
}

// CompleteAuth exchanges the approved token for a session key, stores the link
// as "active", and returns the provider username.
func (s *Service) CompleteAuth(ctx context.Context, userID, token string) (username string, err error) {
	c := s.cfg()
	sessionKey, user, err := s.sc.CompleteAuth(ctx, c, token)
	if err != nil {
		return "", fmt.Errorf("scrobble: complete auth: %w", err)
	}
	if err := s.q.UpsertScrobbleLink(ctx, db.UpsertScrobbleLinkParams{
		UserID:     userID,
		Provider:   provider,
		SessionKey: sessionKey,
		Username:   user,
		Status:     "active",
		CreatedAt:  s.now().Unix(),
	}); err != nil {
		return "", fmt.Errorf("scrobble: store link: %w", err)
	}
	return user, nil
}

// Links returns the provider links for userID, omitting SessionKey.
func (s *Service) Links(ctx context.Context, userID string) ([]Link, error) {
	rows, err := s.q.ListScrobbleLinks(ctx, userID)
	if err != nil {
		return nil, err
	}
	links := make([]Link, 0, len(rows))
	for _, r := range rows {
		links = append(links, Link{
			Provider: r.Provider,
			Username: r.Username,
			Status:   r.Status,
		})
	}
	return links, nil
}

// Unlink deletes the link for (userID, provider).
func (s *Service) Unlink(ctx context.Context, userID, prov string) error {
	return s.q.DeleteScrobbleLink(ctx, db.DeleteScrobbleLinkParams{
		UserID:   userID,
		Provider: prov,
	})
}

// ----------------------------------------------------------------------------
// NowPlaying (fire-and-forget)
// ----------------------------------------------------------------------------

// NowPlaying updates the "now playing" status for userID if they have an
// active provider link. Errors are logged and swallowed — callers must not
// depend on this succeeding.
func (s *Service) NowPlaying(ctx context.Context, userID string, t Track) {
	link, err := s.q.GetScrobbleLink(ctx, db.GetScrobbleLinkParams{
		UserID:   userID,
		Provider: provider,
	})
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			log.Printf("scrobble: NowPlaying: GetScrobbleLink user=%s: %v", userID, err)
		}
		return
	}
	if link.Status != "active" {
		return
	}
	c := s.cfg()
	c.SessionKey = link.SessionKey
	if err := s.sc.NowPlaying(ctx, c, t); err != nil {
		log.Printf("scrobble: NowPlaying: provider error user=%s: %v", userID, err)
	}
}

// ----------------------------------------------------------------------------
// Enqueue
// ----------------------------------------------------------------------------

// Enqueue inserts a pending queue row for the play IFF the user has an active
// provider link. If the user has no link or their link is not active, Enqueue
// is a no-op (returns nil).
func (s *Service) Enqueue(ctx context.Context, userID string, p ScrobblePlay) error {
	link, err := s.q.GetScrobbleLink(ctx, db.GetScrobbleLinkParams{
		UserID:   userID,
		Provider: provider,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil // no link — no-op
		}
		return fmt.Errorf("scrobble: enqueue: get link: %w", err)
	}
	if link.Status != "active" {
		return nil // broken/other — no-op
	}

	now := s.now().Unix()
	return s.q.InsertScrobbleQueue(ctx, db.InsertScrobbleQueueParams{
		ID:            s.idgen(),
		UserID:        userID,
		Provider:      provider,
		CatalogID:     "",
		Title:         p.Title,
		Artist:        p.Artist,
		Album:         p.Album,
		DurationMs:    int64(p.DurationMs),
		PlayedAt:      p.PlayedAt,
		Status:        "pending",
		Attempts:      0,
		NextAttemptAt: now,
		CreatedAt:     now,
	})
}

// ----------------------------------------------------------------------------
// Worker
// ----------------------------------------------------------------------------

// RunWorker runs a background ticker that calls drainOnce on every tick until
// ctx is done. tick is typically 30s–1m in production.
func (s *Service) RunWorker(ctx context.Context, tick time.Duration) {
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.drainOnce(ctx, 50); err != nil {
				log.Printf("scrobble: worker: drainOnce: %v", err)
			}
		}
	}
}

// drainOnce selects up to batch due queue rows, groups them by user_id, and
// submits each user's plays to the Scrobbler adapter.
//
// Outcomes per user:
//   - success           → MarkScrobbleDone for each row
//   - ErrAuth           → SetScrobbleLinkStatus("broken") + MarkScrobbleRetry
//     with a long backoff to avoid a hot loop
//   - transient error   → MarkScrobbleRetry(attempts+1, backoffAt); if
//     attempts+1 >= maxAttempts → MarkScrobbleFailed
func (s *Service) drainOnce(ctx context.Context, batch int) error {
	now := s.now()
	rows, err := s.q.SelectDueScrobbles(ctx, db.SelectDueScrobblesParams{
		NextAttemptAt: now.Unix(),
		Limit:         int64(batch),
	})
	if err != nil {
		return fmt.Errorf("scrobble: drainOnce: select: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}

	// Group rows by user_id.
	byUser := make(map[string][]db.ScrobbleQueue)
	for _, r := range rows {
		byUser[r.UserID] = append(byUser[r.UserID], r)
	}

	for userID, userRows := range byUser {
		if err := s.processUserRows(ctx, userID, userRows, now); err != nil {
			// Log and continue to other users.
			log.Printf("scrobble: drainOnce: user=%s: %v", userID, err)
		}
	}
	return nil
}

// processUserRows handles all due rows for a single user.
func (s *Service) processUserRows(ctx context.Context, userID string, rows []db.ScrobbleQueue, now time.Time) error {
	// Look up the user's active link.
	link, err := s.q.GetScrobbleLink(ctx, db.GetScrobbleLinkParams{
		UserID:   userID,
		Provider: provider,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// User has no link; leave rows as-is (they'll be re-examined next tick).
			return nil
		}
		return fmt.Errorf("get link: %w", err)
	}
	if link.Status != "active" {
		// Link is broken — don't submit; rows will stay pending with their
		// existing next_attempt_at values (which drainOnce set to far-future on
		// ErrAuth, so they won't be immediately re-selected).
		return nil
	}

	// Build the plays slice.
	plays := make([]ScrobblePlay, 0, len(rows))
	for _, r := range rows {
		plays = append(plays, ScrobblePlay{
			Track: Track{
				Title:      r.Title,
				Artist:     r.Artist,
				Album:      r.Album,
				DurationMs: int(r.DurationMs),
			},
			PlayedAt: r.PlayedAt,
		})
	}

	// Build per-call Creds (app key/secret + user session key).
	c := s.cfg()
	c.SessionKey = link.SessionKey

	_, scrobbleErr := s.sc.Scrobble(ctx, c, plays)

	if scrobbleErr == nil {
		// Mark all rows done.
		for _, r := range rows {
			if err := s.q.MarkScrobbleDone(ctx, r.ID); err != nil {
				log.Printf("scrobble: MarkScrobbleDone %s: %v", r.ID, err)
			}
		}
		return nil
	}

	// Handle ErrAuth: break the link and push all rows' next_attempt_at far
	// into the future to avoid a hot loop.
	if errors.Is(scrobbleErr, ErrAuth) {
		if err := s.q.SetScrobbleLinkStatus(ctx, db.SetScrobbleLinkStatusParams{
			Status:   "broken",
			UserID:   userID,
			Provider: provider,
		}); err != nil {
			log.Printf("scrobble: SetScrobbleLinkStatus broken user=%s: %v", userID, err)
		}
		// Push rows far out so they're not hot-looping.
		farFuture := now.Add(24 * time.Hour).Unix()
		for _, r := range rows {
			newAttempts := r.Attempts + 1
			if err := s.q.MarkScrobbleRetry(ctx, db.MarkScrobbleRetryParams{
				Attempts:      newAttempts,
				NextAttemptAt: farFuture,
				ID:            r.ID,
			}); err != nil {
				log.Printf("scrobble: MarkScrobbleRetry(auth) %s: %v", r.ID, err)
			}
		}
		return fmt.Errorf("user=%s: %w", userID, scrobbleErr)
	}

	// Transient error: retry each row individually with backoff.
	for _, r := range rows {
		newAttempts := r.Attempts + 1
		if newAttempts >= maxAttempts {
			if err := s.q.MarkScrobbleFailed(ctx, r.ID); err != nil {
				log.Printf("scrobble: MarkScrobbleFailed %s: %v", r.ID, err)
			}
			continue
		}
		nextAt := now.Add(backoff(int(newAttempts))).Unix()
		if err := s.q.MarkScrobbleRetry(ctx, db.MarkScrobbleRetryParams{
			Attempts:      newAttempts,
			NextAttemptAt: nextAt,
			ID:            r.ID,
		}); err != nil {
			log.Printf("scrobble: MarkScrobbleRetry %s: %v", r.ID, err)
		}
	}
	return fmt.Errorf("transient scrobble error user=%s: %w", userID, scrobbleErr)
}

// backoff returns min(1h, 60s * 2^(n-1)) for attempt number n (1-based).
func backoff(n int) time.Duration {
	const base = 60 * time.Second
	const cap = 1 * time.Hour
	d := base
	for i := 1; i < n; i++ {
		d *= 2
		if d > cap {
			return cap
		}
	}
	return d
}
