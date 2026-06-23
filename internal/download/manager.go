package download

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/events"
)

// shortID trims a job UUID for compact log lines.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// JobStore is the persistence slice the Manager needs. *db.Queries does NOT
// satisfy this directly (it speaks db.DownloadJob); the composition root adapts
// it via a thin sqlStore wrapper (Task 7). The in-memory test store satisfies it.
type JobStore interface {
	// Insert persists a new job. The originating request is passed alongside so
	// the sqlStore can marshal the FULL core.DownloadRequest into request_json
	// (artist/title/album/source/externalId/isrc/playWhenReady/downloader),
	// giving a job loaded back from SQLite enough to run.
	Insert(ctx context.Context, j core.DownloadJob, req core.DownloadRequest) error
	Get(ctx context.Context, id string) (core.DownloadJob, bool, error)
	ActiveByDedup(ctx context.Context, dedupKey string) (core.DownloadJob, bool, error)
	List(ctx context.Context) ([]core.DownloadJob, error)
	Update(ctx context.Context, j core.DownloadJob) error
}

// ScanController is the library slice the Manager needs (StartScan + ScanStatus).
type ScanController interface {
	StartScan(ctx context.Context) error
	ScanStatus(ctx context.Context) (core.ScanStatus, error)
}

// Rematcher re-resolves an external result after a scan. *matching.Service fits.
type Rematcher interface {
	Match(ctx context.Context, ext core.ExternalResult) (core.MatchResult, error)
}

// Publisher is the EventBus slice the Manager needs. *events.Bus fits.
type Publisher interface {
	Publish(ev events.Event)
}

// VersionBumper reads and bumps library_version. *store.Store fits.
type VersionBumper interface {
	LibraryVersion(ctx context.Context) (int64, error)
	SetLibraryVersion(ctx context.Context, v int64) error
}

// Config tunes the Manager. Zero values are replaced with safe defaults.
type Config struct {
	Workers        int
	DebounceWindow time.Duration
	ScanPollEvery  time.Duration
	ScanPollMax    time.Duration
	// ScanSettleMax bounds how long waitForScan waits for Navidrome to actually
	// BEGIN scanning (flip getScanStatus.scanning → true) after StartScan. startScan
	// is async: getScanStatus reports scanning=false for a brief window before the
	// scan engages. Without this grace the poll loop saw scanning=false on the first
	// tick and returned immediately — re-matching against the PRE-download index, so
	// the freshly-downloaded file was never found and library_track_id stayed empty
	// forever. A short settle window lets the scan engage before we wait for it to end.
	ScanSettleMax time.Duration
	// JobTimeout caps how long a single download may run before it is killed and
	// marked failed — so a stuck/rate-limited downloader (e.g. spotDL backing off
	// for 24h) can't pin a worker forever.
	JobTimeout time.Duration
}

func (c Config) withDefaults() Config {
	if c.Workers <= 0 {
		c.Workers = 2
	}
	if c.DebounceWindow <= 0 {
		c.DebounceWindow = 5 * time.Second
	}
	if c.ScanPollEvery <= 0 {
		c.ScanPollEvery = 500 * time.Millisecond
	}
	if c.ScanPollMax <= 0 {
		c.ScanPollMax = 30 * time.Second
	}
	if c.ScanSettleMax <= 0 {
		c.ScanSettleMax = 5 * time.Second
	}
	if c.JobTimeout <= 0 {
		c.JobTimeout = 15 * time.Minute
	}
	return c
}

// Manager owns the download queue, a bounded worker pool, dedup-join, the
// fallback chain, scan-debounce, cancel/retry, and EventBus publication.
type Manager struct {
	cfg         Config
	downloaders []Downloader
	store       JobStore
	bus         Publisher
	scanner     ScanController
	rematcher   Rematcher
	version     VersionBumper
	clock       Clock

	queue chan string // job IDs to process

	mu       sync.Mutex
	cancels  map[string]context.CancelFunc // in-flight job cancel funcs
	reqs     map[string]core.DownloadRequest
	debounce func() bool // active debounce timer stop (or nil)
	pending  bool        // a completion is awaiting the scan window

	wg       sync.WaitGroup
	stopOnce sync.Once
	stopCh   chan struct{}
}

// NewManager constructs the Manager. Call Start() to launch workers.
func NewManager(cfg Config, downloaders []Downloader, store JobStore, bus Publisher,
	scanner ScanController, rematcher Rematcher, version VersionBumper, clock Clock) *Manager {
	if clock == nil {
		clock = RealClock{}
	}
	cfg = cfg.withDefaults()
	return &Manager{
		cfg:         cfg,
		downloaders: downloaders,
		store:       store,
		bus:         bus,
		scanner:     scanner,
		rematcher:   rematcher,
		version:     version,
		clock:       clock,
		queue:       make(chan string, 256),
		cancels:     map[string]context.CancelFunc{},
		reqs:        map[string]core.DownloadRequest{},
		stopCh:      make(chan struct{}),
	}
}

// Start launches the worker pool and kicks off a one-shot startup backfill (in a
// goroutine) that re-matches any completed job whose LibraryTrackID is still empty.
// This handles jobs that finished under an older/weaker matcher before the post-scan
// rematch path was in place, or whose scan-window closed before the file was indexed.
// The backfill runs once at startup and never retries still-unmatchable jobs.
func (m *Manager) Start() {
	for i := 0; i < m.cfg.Workers; i++ {
		m.wg.Add(1)
		go m.worker()
	}
	log.Printf("download manager: %d worker(s) started, %d downloader(s) available", m.cfg.Workers, len(m.downloaders))
	go m.backfillUnlinked()
}

// backfillUnlinked is a one-shot startup pass that re-matches every completed job
// whose LibraryTrackID is empty. A job that still can't be matched is left alone
// (no retry loop). Jobs that now match get LibraryTrackID + CoverArtID set and a
// download.complete event published so the FE updates live.
func (m *Manager) backfillUnlinked() {
	if m.rematcher == nil {
		return
	}
	ctx := context.Background()
	jobs, err := m.store.List(ctx)
	if err != nil {
		log.Printf("download backfill: list jobs failed: %v", err)
		return
	}
	matched := 0
	for _, j := range jobs {
		if j.Status != core.DownloadCompleted || j.LibraryTrackID != "" {
			continue
		}
		res, merr := m.rematcher.Match(ctx, core.ExternalResult{
			Source: j.Source, ExternalID: j.ExternalID, Type: core.EntityTrack,
			Title: j.Title, Artist: j.Artist, Album: j.Album, ISRC: j.ISRC,
			DurationMs: j.DurationMs,
		})
		if merr != nil || res.Status != core.MatchInLibrary {
			continue
		}
		j.LibraryTrackID = res.LibraryTrackID
		j.CoverArtID = res.CoverArtID
		if err := m.store.Update(ctx, j); err != nil {
			log.Printf("download backfill: update job %s failed: %v", shortID(j.ID), err)
			continue
		}
		m.publishComplete(j, res.LibraryTrackID)
		matched++
		log.Printf("download backfill: re-linked job %s -> library track %s", shortID(j.ID), res.LibraryTrackID)
	}
	if matched > 0 {
		log.Printf("download backfill: re-linked %d previously unmatched completed job(s)", matched)
	}
}

// Stop signals workers to drain and waits for them. It ALSO cancels any pending
// scan-debounce timer (and clears pending) so a real-clock test cannot have
// runScan fire against fakes after the test ends. Idempotent.
//
// Ordering rationale: close stopCh first so workers exit their select loops,
// then wg.Wait() until every worker (and any scheduleScan it calls) has fully
// finished, then cancel the debounce timer. This guarantees we cancel the
// LAST timer armed by any worker — if we cancelled before Wait, a worker still
// in process() could call scheduleScan() and re-arm a new timer after we
// cleared it. No deadlock risk: wg.Wait() holds no lock, and workers only
// acquire m.mu briefly inside callbacks (never blocking on Stop's lock).
func (m *Manager) Stop() {
	m.stopOnce.Do(func() { close(m.stopCh) })
	m.wg.Wait()
	m.mu.Lock()
	if m.debounce != nil {
		m.debounce() // stop the AfterFunc/clock timer
		m.debounce = nil
	}
	m.pending = false
	m.mu.Unlock()
}

// pick chooses the downloader: an explicit name if set & present, else the first
// (priority order is preserved by the input slice) whose CanDownload returns true.
func (m *Manager) pick(ctx context.Context, req core.DownloadRequest) (Downloader, error) {
	if req.Downloader != "" {
		for _, d := range m.downloaders {
			if d.Name() == req.Downloader {
				return d, nil
			}
		}
		return nil, fmt.Errorf("downloader %q not registered", req.Downloader)
	}
	for _, d := range m.downloaders {
		ok, err := d.CanDownload(ctx, req)
		if err != nil {
			continue
		}
		if ok {
			return d, nil
		}
	}
	return nil, fmt.Errorf("no downloader can fetch %q by %q", req.Title, req.Artist)
}

// Enqueue persists a new job (or JOINS an active one with the same dedup key) and
// pushes it to the worker pool. Concurrency-safe: simultaneous same-key enqueues
// return the single existing job.
func (m *Manager) Enqueue(ctx context.Context, req core.DownloadRequest) (core.DownloadJob, error) {
	dedup := DedupKey(req)

	// Serialize the dedup-check + insert so two same-key callers can't both create.
	m.mu.Lock()

	if existing, ok, err := m.store.ActiveByDedup(ctx, dedup); err != nil {
		m.mu.Unlock()
		return core.DownloadJob{}, err
	} else if ok {
		m.mu.Unlock()
		return existing, nil // dedup-join: no second dispatch
	}

	dl, err := m.pick(ctx, req)
	if err != nil {
		m.mu.Unlock()
		return core.DownloadJob{}, err
	}

	job := core.DownloadJob{
		ID:             uuid.NewString(),
		DedupKey:       dedup,
		Status:         core.DownloadQueued,
		Progress:       0,
		DownloaderName: dl.Name(),
		Source:         req.Source,
		ExternalID:     req.ExternalID,
		// Carry the request fields so any JobStore (incl. in-memory) and the worker
		// fallback have enough to run; the sqlStore ALSO persists request_json.
		Artist:        req.Artist,
		Title:         req.Title,
		Album:         req.Album,
		ISRC:          req.ISRC,
		PlayWhenReady: req.PlayWhenReady,
		CreatedAt:     m.clock.Now().Unix(),
	}
	if err := m.store.Insert(ctx, job, req); err != nil {
		m.mu.Unlock()
		return core.DownloadJob{}, err
	}
	m.reqs[job.ID] = req
	m.publishEvent(TopicQueued, job, "")
	log.Printf("download queued: %q by %q (job %s, downloader %s)", job.Title, job.Artist, shortID(job.ID), job.DownloaderName)
	id := job.ID

	// Unlock BEFORE dispatching to the queue. Workers re-acquire m.mu inside the
	// progress callback, so a blocking send under m.mu would deadlock. The job is
	// already persisted as queued, so nothing is lost even if we shut down between
	// unlock and send.
	m.mu.Unlock()

	// Blocking send: never silently drops the job, never deadlocks (no lock held).
	// Cancelled only when the Manager is stopping, which is safe — the job is in
	// the DB and can be recovered on restart.
	select {
	case m.queue <- id:
	case <-m.stopCh:
	}
	return job, nil
}

// Status returns the current persisted job.
func (m *Manager) Status(ctx context.Context, jobID string) (core.DownloadJob, error) {
	j, ok, err := m.store.Get(ctx, jobID)
	if err != nil {
		return core.DownloadJob{}, err
	}
	if !ok {
		return core.DownloadJob{}, fmt.Errorf("job %q not found", jobID)
	}
	return j, nil
}

// List returns all jobs (newest-first ordering is the store's responsibility).
func (m *Manager) List(ctx context.Context) ([]core.DownloadJob, error) {
	return m.store.List(ctx)
}

// publishEvent emits a DownloadEvent on the given topic from the job's state.
func (m *Manager) publishEvent(topic string, job core.DownloadJob, errMsg string) {
	if m.bus == nil {
		return
	}
	m.bus.Publish(events.Event{Topic: topic, Payload: core.DownloadEvent{
		JobID:          job.ID,
		DedupKey:       job.DedupKey,
		Status:         job.Status,
		Progress:       job.Progress,
		Error:          errMsg,
		Source:         job.Source,
		ExternalID:     job.ExternalID,
		LibraryTrackID: job.LibraryTrackID,
		CoverArtID:     job.CoverArtID,
	}})
}

// --- worker plumbing ---

func (m *Manager) worker() {
	defer m.wg.Done()
	for {
		select {
		case <-m.stopCh:
			return
		case id := <-m.queue:
			m.process(id)
		}
	}
}

func (m *Manager) process(id string) {
	ctx := context.Background()
	job, ok, err := m.store.Get(ctx, id)
	if err != nil || !ok {
		return
	}
	m.mu.Lock()
	req, haveReq := m.reqs[id]
	m.mu.Unlock()
	// Fall back to the request rehydrated onto the job from request_json (durable
	// across restart) when the in-memory map has nothing (e.g. a retried/loaded job).
	if !haveReq || req.ExternalID == "" {
		req = core.DownloadRequest{
			Source: job.Source, ExternalID: job.ExternalID, Artist: job.Artist,
			Title: job.Title, Album: job.Album, ISRC: job.ISRC,
			Downloader: job.DownloaderName, PlayWhenReady: job.PlayWhenReady,
		}
	}
	m.mu.Lock()
	jctx, cancel := context.WithTimeout(ctx, m.cfg.JobTimeout)
	m.cancels[id] = cancel
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.cancels, id)
		m.mu.Unlock()
		cancel()
	}()

	var dl Downloader
	for _, d := range m.downloaders {
		if d.Name() == job.DownloaderName {
			dl = d
			break
		}
	}
	if dl == nil {
		cur, _, _ := m.store.Get(ctx, id)
		cur.Status = core.DownloadFailed
		cur.Error = "downloader not registered"
		_ = m.store.Update(ctx, cur)
		m.publishEvent(TopicFailed, cur, cur.Error)
		log.Printf("download failed: %q — downloader %q not registered", job.Title, job.DownloaderName)
		m.mu.Lock()
		delete(m.reqs, id)
		m.mu.Unlock()
		return
	}

	// If the job was canceled before the worker picked it up, do not start.
	if job.Status == core.DownloadCanceled {
		return
	}

	job.Status = core.DownloadRunning
	job.StartedAt = m.clock.Now().Unix()
	_ = m.store.Update(ctx, job)
	m.publishEvent(TopicProgress, job, "")
	log.Printf("download running: %q (job %s via %s)", job.Title, shortID(id), dl.Name())

	// Heartbeat: while the download runs, log every 30s so a long-running or stuck
	// job is visibly alive (with elapsed time). Stops as soon as Start returns.
	hbStop := make(chan struct{})
	go func() {
		start := time.Now()
		tk := time.NewTicker(30 * time.Second)
		defer tk.Stop()
		for {
			select {
			case <-hbStop:
				return
			case <-tk.C:
				log.Printf("download still running: %q (job %s, %s elapsed)", job.Title, shortID(id), time.Since(start).Round(time.Second))
			}
		}
	}()

	outPath, serr := dl.Start(jctx, req, func(p int) {
		m.mu.Lock()
		cur, _, _ := m.store.Get(ctx, id)
		cur.Progress = p
		_ = m.store.Update(ctx, cur)
		m.mu.Unlock()
		m.publishEvent(TopicProgress, cur, "")
	})
	close(hbStop)

	cur, _, _ := m.store.Get(ctx, id)
	if serr != nil {
		switch {
		case errors.Is(jctx.Err(), context.DeadlineExceeded):
			// Hit the per-job timeout (e.g. a downloader backing off for hours).
			cur.Status = core.DownloadFailed
			cur.Error = fmt.Sprintf("timed out after %s", m.cfg.JobTimeout)
			cur.FinishedAt = m.clock.Now().Unix()
			_ = m.store.Update(ctx, cur)
			m.publishEvent(TopicFailed, cur, cur.Error)
			log.Printf("download timed out: %q (job %s) after %s", cur.Title, shortID(id), m.cfg.JobTimeout)
			m.mu.Lock()
			delete(m.reqs, id)
			m.mu.Unlock()
			return
		case jctx.Err() == context.Canceled:
			cur.Status = core.DownloadCanceled
			cur.FinishedAt = m.clock.Now().Unix()
			_ = m.store.Update(ctx, cur)
			m.publishEvent(TopicFailed, cur, "canceled")
			m.mu.Lock()
			delete(m.reqs, id)
			m.mu.Unlock()
			return
		default:
			cur.Status = core.DownloadFailed
			cur.Error = serr.Error()
			cur.FinishedAt = m.clock.Now().Unix()
			_ = m.store.Update(ctx, cur)
			m.publishEvent(TopicFailed, cur, serr.Error())
			log.Printf("download failed: %q (job %s) — %v", cur.Title, shortID(id), serr)
			m.mu.Lock()
			delete(m.reqs, id)
			m.mu.Unlock()
			return
		}
	}

	cur.Status = core.DownloadCompleted
	cur.Progress = 100
	cur.OutputPath = outPath
	cur.FinishedAt = m.clock.Now().Unix()
	_ = m.store.Update(ctx, cur)
	m.publishEvent(TopicComplete, cur, "")
	log.Printf("download completed: %q (job %s) -> %s", cur.Title, shortID(id), outPath)

	// Clear the rehydrated request now the download is done.
	m.mu.Lock()
	delete(m.reqs, id)
	m.mu.Unlock()

	// Coalesce this completion into the debounced scan window.
	m.scheduleScan(id)
}

// scheduleScan (re)arms the debounce timer. Multiple completions within the
// window collapse into ONE scan. Uses the injectable clock so tests advance time.
func (m *Manager) scheduleScan(jobID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending = true
	if m.debounce != nil {
		m.debounce() // cancel the previous timer; we re-arm to extend the window
	}
	m.debounce = m.clock.AfterFunc(m.cfg.DebounceWindow, func() {
		m.runScan()
	})
}

// runScan performs the coalesced library refresh: StartScan → poll → re-match all
// recently-completed jobs → set library_track_id → bump library_version →
// publish library.updated + per-job download.complete (with artist/album IDs).
func (m *Manager) runScan() {
	m.mu.Lock()
	if !m.pending {
		m.mu.Unlock()
		return
	}
	m.pending = false
	m.debounce = nil
	m.mu.Unlock()

	ctx := context.Background()
	if m.scanner == nil {
		return
	}
	if err := m.scanner.StartScan(ctx); err != nil {
		log.Printf("library scan after download failed: %v", err)
		return
	}
	m.waitForScan(ctx)

	// Bump library_version FIRST so re-matches recompute against fresh data
	// (invalidates match_cache rows whose library_version is now stale).
	if m.version != nil {
		if cur, err := m.version.LibraryVersion(ctx); err == nil {
			_ = m.version.SetLibraryVersion(ctx, cur+1)
		}
	}

	// Re-match every completed job that has no library_track_id yet.
	jobs, err := m.store.List(ctx)
	if err != nil {
		return
	}
	for _, j := range jobs {
		if j.Status != core.DownloadCompleted || j.LibraryTrackID != "" {
			continue
		}
		if m.rematcher == nil {
			continue
		}
		// Forward all job metadata so the matcher can search the library by title/artist/ISRC.
		// An empty Title would leave the matcher with no candidate query → no match ever found.
		res, merr := m.rematcher.Match(ctx, core.ExternalResult{
			Source: j.Source, ExternalID: j.ExternalID, Type: core.EntityTrack,
			Title: j.Title, Artist: j.Artist, Album: j.Album, ISRC: j.ISRC,
			DurationMs: j.DurationMs,
		})
		if merr != nil || res.Status != core.MatchInLibrary {
			continue
		}
		j.LibraryTrackID = res.LibraryTrackID
		j.CoverArtID = res.CoverArtID
		_ = m.store.Update(ctx, j)
		m.publishComplete(j, res.LibraryTrackID)
	}
	// Per-album/artist IDs on LibraryUpdatedEvent are deferred to a later milestone;
	// the frontend does broad library invalidation on this event.
	if m.bus != nil {
		m.bus.Publish(events.Event{Topic: TopicLibraryUpdate, Payload: core.LibraryUpdatedEvent{}})
	}
}

// waitForScan blocks until the Navidrome scan triggered by StartScan has run to
// completion (or a budget elapses). It is deliberately two-phase to defeat the
// scan-start RACE:
//
//  1. SETTLE: poll until getScanStatus.scanning flips true (the scan has actually
//     engaged) OR a short settle budget (ScanSettleMax) elapses. startScan is
//     asynchronous on Navidrome — for a brief window after it returns, getScanStatus
//     still reports scanning=false. The OLD code broke out of its poll loop on that
//     first false, re-matching against the PRE-download index, so a just-downloaded
//     file was never found (library_track_id stayed empty permanently).
//  2. DRAIN: once scanning has been observed (or the settle budget lapsed for an
//     instantaneous scan), poll until scanning=false again OR the poll budget
//     (ScanPollMax) elapses — the file is now indexed and re-match can find it.
//
// All budgets use wall-clock time (this runs inside the debounce timer fn, off the
// hot path) so a frozen test clock can't stall the loop; the cadence is ScanPollEvery.
func (m *Manager) waitForScan(ctx context.Context) {
	poll := m.cfg.ScanPollEvery
	if poll <= 0 {
		poll = 500 * time.Millisecond
	}

	// Phase 1 — SETTLE: wait for the scan to begin.
	settleDeadline := time.Now().Add(m.cfg.ScanSettleMax)
	started := false
	for time.Now().Before(settleDeadline) {
		st, err := m.scanner.ScanStatus(ctx)
		if err != nil {
			break
		}
		if st.Scanning {
			started = true
			break
		}
		time.Sleep(poll)
	}

	// Phase 2 — DRAIN: wait for the scan to finish. If we never observed it start
	// (an already-idle/instantaneous scan), there is nothing to drain.
	if !started {
		return
	}
	drainDeadline := time.Now().Add(m.cfg.ScanPollMax)
	for time.Now().Before(drainDeadline) {
		st, err := m.scanner.ScanStatus(ctx)
		if err != nil || !st.Scanning {
			break
		}
		time.Sleep(poll)
	}
}

// publishComplete emits a final download.complete carrying the library_track_id.
// artistId/albumId are left empty in MVP (the re-matcher returns only the track
// id); the client invalidates by libraryTrackId + does a scoped library refetch.
func (m *Manager) publishComplete(job core.DownloadJob, libraryTrackID string) {
	if m.bus == nil {
		return
	}
	m.bus.Publish(events.Event{Topic: TopicComplete, Payload: core.DownloadEvent{
		JobID: job.ID, DedupKey: job.DedupKey, Status: core.DownloadCompleted, Progress: 100,
		Source: job.Source, ExternalID: job.ExternalID, LibraryTrackID: libraryTrackID,
		CoverArtID: job.CoverArtID,
	}})
}

// Cancel aborts an in-flight or queued job. An in-flight exec is killed via its
// context; a queued job is marked canceled so the worker skips it.
func (m *Manager) Cancel(ctx context.Context, jobID string) error {
	m.mu.Lock()
	cancel, inFlight := m.cancels[jobID]
	m.mu.Unlock()
	if inFlight {
		cancel() // kills the in-flight Start; process() marks it canceled
		return nil
	}
	job, ok, err := m.store.Get(ctx, jobID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("job %q not found", jobID)
	}
	if job.Status == core.DownloadQueued {
		job.Status = core.DownloadCanceled
		job.FinishedAt = m.clock.Now().Unix()
		if err := m.store.Update(ctx, job); err != nil {
			return err
		}
		m.publishEvent(TopicFailed, job, "canceled")
	}
	return nil
}

// Retry resets a failed/canceled job to queued (attempts++) and re-enqueues it.
// When manualURL is non-empty it is stored on the job's DownloadRequest so the
// spotDL adapter can use the pipe syntax (or direct URL) on the next attempt.
// A plain retry (manualURL=="") behaves exactly as before.
func (m *Manager) Retry(ctx context.Context, jobID string, manualURL string) (core.DownloadJob, error) {
	job, ok, err := m.store.Get(ctx, jobID)
	if err != nil {
		return core.DownloadJob{}, err
	}
	if !ok {
		return core.DownloadJob{}, fmt.Errorf("job %q not found", jobID)
	}
	if job.Status != core.DownloadFailed && job.Status != core.DownloadCanceled {
		return job, nil // nothing to retry
	}
	job.Status = core.DownloadQueued
	job.Progress = 0
	job.Error = ""
	job.Attempts++
	job.FinishedAt = 0
	if err := m.store.Update(ctx, job); err != nil {
		return core.DownloadJob{}, err
	}
	// When a manual URL is provided, seed (or update) the in-memory request so the
	// worker picks up the ManualURL on its next run even if request_json pre-dates
	// this field.
	if manualURL != "" {
		m.mu.Lock()
		req := m.reqs[job.ID]
		// If there is no in-memory request yet, rehydrate from the job fields so we
		// have a complete base before setting ManualURL.
		if req.ExternalID == "" {
			req = core.DownloadRequest{
				Source: job.Source, ExternalID: job.ExternalID, Artist: job.Artist,
				Title: job.Title, Album: job.Album, ISRC: job.ISRC,
				Downloader: job.DownloaderName, PlayWhenReady: job.PlayWhenReady,
			}
		}
		req.ManualURL = manualURL
		m.reqs[job.ID] = req
		m.mu.Unlock()
	}
	m.publishEvent(TopicQueued, job, "")
	select {
	case m.queue <- job.ID:
	case <-m.stopCh:
	}
	return job, nil
}

// SeedRequest rehydrates the originating request for a job (used after restart or
// to retry a job whose in-memory request was cleared on completion). The
// composition root rehydrates queued jobs from request_json at startup.
func (m *Manager) SeedRequest(jobID string, req core.DownloadRequest) {
	m.mu.Lock()
	m.reqs[jobID] = req
	m.mu.Unlock()
}
