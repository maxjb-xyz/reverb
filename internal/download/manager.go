package download

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/events"
	"github.com/maxjb-xyz/reverb/internal/resolver"
)

// shortID trims a job UUID for compact log lines.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// closedChan returns an already-closed channel — receiving from it returns
// immediately. Used as the "running" (open-gate) state of the pause gate.
func closedChan() chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
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
	// UpdateRequest re-persists the originating DownloadRequest for job id into
	// request_json so that ManualURL (and any other late-added field) survives a
	// server restart between a Retry call and the worker picking up the job.
	UpdateRequest(ctx context.Context, id string, req core.DownloadRequest) error
	// Delete hard-removes a single job row (used by Clear; the Manager guarantees
	// the job is in a terminal state before calling this).
	Delete(ctx context.Context, id string) error
	// DeleteFinished hard-removes every terminal (completed/failed/canceled) job
	// and returns the deleted ids so the Manager can publish a removal event.
	DeleteFinished(ctx context.Context) ([]string, error)
	// UpdateRef persists the downloader-internal ref (e.g. Lidarr album id) for a
	// job, used by async downloaders after Submit.
	UpdateRef(ctx context.Context, id string, ref string) error
	// GetRequest retrieves the originating DownloadRequest (from request_json) for
	// the given job id. Returns (req, true, nil) on hit, (zero, false, nil) if the
	// job exists but has no persisted request, and (zero, false, err) on error.
	// Used by the !haveReq reconstruction to recover Granularity after a restart.
	GetRequest(ctx context.Context, id string) (core.DownloadRequest, bool, error)
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

// BindingResolver is the narrow catalog-resolution seam the Manager will use in
// Tasks 3-5 to resolve catalog IDs to backend addressing. *resolver.Service
// satisfies this interface (Go structural typing). Declared here (consumer-side)
// so the resolver package never needs to import download, keeping the dependency
// direction one-way (download→resolver, not the reverse).
type BindingResolver interface {
	Resolve(ctx context.Context, catalogID string) (resolver.Addressing, error)
	RefreshLinked(ctx context.Context, catalogIDs []string) error
}

// VersionBumper reads and bumps library_version. *store.Store fits.
type VersionBumper interface {
	LibraryVersion(ctx context.Context) (int64, error)
	SetLibraryVersion(ctx context.Context, v int64) error
}

// PlaylistAdder adds tracks to a library playlist. *subsonic.LibraryAdapter satisfies it.
// Used by the one-time import path: after a download is matched, the track is appended
// to the target playlist. May be nil when no library is configured.
type PlaylistAdder interface {
	AddTracksToPlaylist(ctx context.Context, playlistID string, trackIDs []string) error
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
	// JobTimeout caps how long a single track-granularity download may run before
	// it is killed and marked failed — so a stuck/rate-limited downloader (e.g.
	// spotDL backing off for 24h) can't pin a worker forever.
	JobTimeout time.Duration
	// AlbumJobTimeout caps how long an album-granularity sync download may run.
	// Album downloads (e.g. spotDL fetching a full album) are expected to take
	// significantly longer than a single track, so they get a separate, larger
	// timeout. Async/Lidarr jobs run on the reconciler lane (bounded by AsyncMaxAge)
	// and are NOT affected by this setting.
	AlbumJobTimeout time.Duration
	// ReconcileEvery is the poll cadence for async (e.g. Lidarr) jobs.
	ReconcileEvery time.Duration
	// AsyncMaxAge bounds how long an async job may stay in-flight before it's
	// failed (Lidarr never found/imported a release).
	AsyncMaxAge time.Duration
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
	if c.AlbumJobTimeout <= 0 {
		c.AlbumJobTimeout = 2 * time.Hour
	}
	if c.ReconcileEvery <= 0 {
		c.ReconcileEvery = 10 * time.Second
	}
	if c.AsyncMaxAge <= 0 {
		c.AsyncMaxAge = 7 * 24 * time.Hour
	}
	return c
}

// Manager owns the download queue, a bounded worker pool, dedup-join, the
// fallback chain, scan-debounce, cancel/retry, and EventBus publication.
type Manager struct {
	cfg         Config
	downloaders []DownloaderEntry
	store       JobStore
	bus         Publisher
	scanner     ScanController
	rematcher   Rematcher
	version     VersionBumper
	clock       Clock
	playlists   PlaylistAdder  // optional; non-nil only when a library is configured
	resolve     func() BindingResolver // optional provider; Tasks 3-5 add call sites

	queue chan string // job IDs to process

	mu       sync.Mutex
	cancels  map[string]context.CancelFunc // in-flight job cancel funcs
	reqs     map[string]core.DownloadRequest
	debounce func() bool // active debounce timer stop (or nil)
	pending  bool        // a completion is awaiting the scan window
	paused   bool          // dispatch gate: workers stop pulling NEW jobs while true
	resumeCh chan struct{} // closed when running; a fresh OPEN channel while paused

	wg       sync.WaitGroup
	stopOnce sync.Once
	stopCh   chan struct{}
	started  bool // set to true by Start(); guards Stop() against double-close on an unstarted Manager
}

// jobTimeout returns the per-job context timeout for the given request.
// Album-granularity sync jobs use AlbumJobTimeout (default 2h) so that a full
// album download is not prematurely killed by the much-shorter track JobTimeout.
// All other (track / empty) granularities use JobTimeout.
func (m *Manager) jobTimeout(req core.DownloadRequest) time.Duration {
	if req.Granularity == core.GranularityAlbum {
		return m.cfg.AlbumJobTimeout
	}
	return m.cfg.JobTimeout
}

// NewManager constructs the Manager. Call Start() to launch workers.
// playlists may be nil; when non-nil, completed downloads whose request carries
// AddToPlaylistID will have the matched library track appended to that playlist.
// resolve is an optional provider func() BindingResolver — nil or returning nil
// means "no resolver available yet" (no panic). Tasks 3-5 add the actual Resolve
// and RefreshLinked call sites; this Task (1) only stores the dep.
func NewManager(cfg Config, downloaders []DownloaderEntry, store JobStore, bus Publisher,
	scanner ScanController, rematcher Rematcher, version VersionBumper, clock Clock,
	playlists PlaylistAdder, resolve func() BindingResolver) *Manager {
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
		playlists:   playlists,
		resolve:     resolve,
		queue:       make(chan string, 256),
		cancels:     map[string]context.CancelFunc{},
		reqs:        map[string]core.DownloadRequest{},
		stopCh:      make(chan struct{}),
		resumeCh:    closedChan(),
	}
}

// Start launches the worker pool and kicks off a one-shot startup backfill (in a
// goroutine) that re-matches any completed job whose LibraryTrackID is still empty.
// This handles jobs that finished under an older/weaker matcher before the post-scan
// rematch path was in place, or whose scan-window closed before the file was indexed.
// The backfill runs once at startup and never retries still-unmatchable jobs.
func (m *Manager) Start() {
	m.mu.Lock()
	m.started = true
	m.mu.Unlock()
	for i := 0; i < m.cfg.Workers; i++ {
		m.wg.Add(1)
		go m.worker()
	}
	log.Printf("download manager: %d worker(s) started, %d downloader(s) available", m.cfg.Workers, len(m.downloaders))
	if m.hasAsync() {
		m.wg.Add(1)
		go m.reconcileLoop()
		log.Printf("download manager: async reconciler started (every %s)", m.cfg.ReconcileEvery)
	}
	go m.BackfillUnlinked()
}

// BackfillUnlinked is a one-shot pass that re-matches every completed job whose
// LibraryTrackID is empty. A job that still can't be matched is left alone (no
// retry loop). Jobs that now match get LibraryTrackID + CoverArtID set and a
// download.complete event published so the FE updates live.
//
// Called automatically at Start() and also by waitReadyThenBackfill (cmd/reverb)
// after the bundled Navidrome reports ready, so the boot-race case (backfill ran
// before Navidrome was serving) is re-resolved.
func (m *Manager) BackfillUnlinked() {
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
		if m.playlists != nil && j.AddToPlaylistID != "" {
			if perr := m.playlists.AddTracksToPlaylist(ctx, j.AddToPlaylistID, []string{res.LibraryTrackID}); perr != nil {
				log.Printf("download backfill: add to playlist %s failed for job %s: %v", j.AddToPlaylistID, shortID(j.ID), perr)
			}
		}
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
	m.mu.Lock()
	started := m.started
	m.mu.Unlock()
	if !started {
		return // Start() was never called — no workers, no channel to drain
	}
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

// asyncFor returns the AsyncDownloader registered under name, or nil if that
// downloader isn't registered or isn't async.
func (m *Manager) asyncFor(name string) AsyncDownloader {
	for _, e := range m.downloaders {
		if e.Downloader.Name() == name {
			if a, ok := e.Downloader.(AsyncDownloader); ok {
				return a
			}
		}
	}
	return nil
}

// hasAsync reports whether any configured downloader is an AsyncDownloader.
func (m *Manager) hasAsync() bool {
	for _, e := range m.downloaders {
		if _, ok := e.Downloader.(AsyncDownloader); ok {
			return true
		}
	}
	return false
}

// reconcileOnce polls every in-flight async job (running, with a ref) once: it
// updates progress, completes (and schedules the scan) on import, fails on error,
// and gives up on jobs older than AsyncMaxAge. Safe to call from a test.
func (m *Manager) reconcileOnce(ctx context.Context) {
	jobs, err := m.store.List(ctx)
	if err != nil {
		return
	}
	now := m.clock.Now().Unix()
	for _, j := range jobs {
		if j.Status != core.DownloadRunning || j.DownloaderRef == "" {
			continue
		}
		async := m.asyncFor(j.DownloaderName)
		if async == nil {
			continue
		}
		if j.StartedAt > 0 && m.cfg.AsyncMaxAge > 0 && now-j.StartedAt > int64(m.cfg.AsyncMaxAge.Seconds()) {
			j.Status = core.DownloadFailed
			j.Error = "timed out waiting for the downloader to finish"
			j.FinishedAt = now
			_ = m.store.Update(ctx, j)
			m.publishEvent(TopicFailed, j, j.Error)
			m.mu.Lock()
			delete(m.reqs, j.ID)
			m.mu.Unlock()
			continue
		}
		st, perr := async.Poll(ctx, j.DownloaderRef)
		if perr != nil {
			continue // transient — retry next tick
		}
		switch st.State {
		case core.DownloadCompleted:
			j.Status = core.DownloadCompleted
			j.Progress = 100
			j.FinishedAt = now
			_ = m.store.Update(ctx, j)
			m.publishEvent(TopicComplete, j, "")
			m.mu.Lock()
			delete(m.reqs, j.ID)
			m.mu.Unlock()
			m.scheduleScan(j.ID) // reuse the normal scan + rematch path
		case core.DownloadFailed:
			j.Status = core.DownloadFailed
			j.Error = st.Error
			j.FinishedAt = now
			_ = m.store.Update(ctx, j)
			m.publishEvent(TopicFailed, j, st.Error)
			m.mu.Lock()
			delete(m.reqs, j.ID)
			m.mu.Unlock()
		default: // still running — publish progress changes
			if st.Progress != j.Progress {
				j.Progress = st.Progress
				_ = m.store.Update(ctx, j)
				m.publishEvent(TopicProgress, j, "")
			}
		}
	}
}

// reconcileLoop ticks reconcileOnce until Stop. Launched by Start only when an
// async downloader is configured.
func (m *Manager) reconcileLoop() {
	defer m.wg.Done()
	t := time.NewTicker(m.cfg.ReconcileEvery)
	defer t.Stop()
	for {
		select {
		case <-m.stopCh:
			return
		case <-t.C:
			m.reconcileOnce(context.Background())
		}
	}
}

// submitAsync hands a freshly-enqueued job to an async downloader. On success it
// persists the ref and flips the job to running (progress -1 = searching); the
// reconciler then advances it. On error it fails the job. Runs outside m.mu.
func (m *Manager) submitAsync(ctx context.Context, job core.DownloadJob, req core.DownloadRequest, async AsyncDownloader) {
	ref, err := async.Submit(ctx, req)
	cur, ok, _ := m.store.Get(ctx, job.ID)
	if !ok {
		return
	}
	if err != nil {
		cur.Status = core.DownloadFailed
		cur.Error = err.Error()
		cur.FinishedAt = m.clock.Now().Unix()
		_ = m.store.Update(ctx, cur)
		m.publishEvent(TopicFailed, cur, err.Error())
		m.mu.Lock()
		delete(m.reqs, job.ID)
		m.mu.Unlock()
		log.Printf("download submit failed: %q via %s — %v", cur.Title, job.DownloaderName, err)
		return
	}
	_ = m.store.UpdateRef(ctx, cur.ID, ref)
	cur.DownloaderRef = ref
	cur.Status = core.DownloadRunning
	cur.StartedAt = m.clock.Now().Unix()
	cur.Progress = -1 // searching — indeterminate until the download starts
	_ = m.store.Update(ctx, cur)
	m.publishEvent(TopicProgress, cur, "")
	log.Printf("download submitted to %s: %q (job %s, ref %s)", job.DownloaderName, cur.Title, shortID(cur.ID), ref)
}

// sortedEntries returns a copy of m.downloaders filtered to entries whose Order
// map contains granularity g, sorted ascending by Order[g] (stable — input order
// is the tiebreaker). An empty req.Granularity is treated as GranularityTrack.
func (m *Manager) sortedEntries(g core.DownloadGranularity) []DownloaderEntry {
	var filtered []DownloaderEntry
	for _, e := range m.downloaders {
		if _, ok := e.Order[g]; ok {
			filtered = append(filtered, e)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].Order[g] < filtered[j].Order[g]
	})
	return filtered
}

// pick chooses the first entry (by ascending Order[g]) whose CanDownload returns
// true for the request's granularity. An empty req.Granularity defaults to track.
func (m *Manager) pick(ctx context.Context, req core.DownloadRequest) (Downloader, error) {
	g := req.Granularity
	if g == "" {
		g = core.GranularityTrack
	}
	for _, e := range m.sortedEntries(g) {
		ok, err := e.Downloader.CanDownload(ctx, req)
		if err != nil {
			continue
		}
		if ok {
			return e.Downloader, nil
		}
	}
	return nil, fmt.Errorf("no %s downloader can fetch %q by %q", g, req.Title, req.Artist)
}

// pickAfter returns the first entry in the same granularity chain that comes
// AFTER the one named afterName (in ascending Order[g] sort) and whose CanDownload
// accepts req. It is used by the sync worker to fall back through the chain when a
// downloader's Start fails.
//
// Note: fallback is intentionally limited to the sync (track) lane. Async
// downloaders (e.g. Lidarr) run on their own reconciler lane and are not subject
// to this retry logic.
func (m *Manager) pickAfter(ctx context.Context, req core.DownloadRequest, afterName string) (Downloader, error) {
	g := req.Granularity
	if g == "" {
		g = core.GranularityTrack
	}
	skip := true
	for _, e := range m.sortedEntries(g) {
		if skip {
			if e.Downloader.Name() == afterName {
				skip = false
			}
			continue
		}
		// Intentionally uses the background ctx (not jctx) so a fallback candidate
		// isn't pre-poisoned by the prior attempt's timeout or cancellation.
		ok, err := e.Downloader.CanDownload(ctx, req)
		if err != nil {
			continue
		}
		if ok {
			return e.Downloader, nil
		}
	}
	return nil, fmt.Errorf("no further %s downloader after %q for %q", g, afterName, req.Title)
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
		Artist:          req.Artist,
		Title:           req.Title,
		Album:           req.Album,
		ISRC:            req.ISRC,
		PlayWhenReady:   req.PlayWhenReady,
		AddToPlaylistID: req.AddToPlaylistID,
		CreatedAt:       m.clock.Now().Unix(),
	}
	if err := m.store.Insert(ctx, job, req); err != nil {
		m.mu.Unlock()
		return core.DownloadJob{}, err
	}
	m.reqs[job.ID] = req
	m.publishEvent(TopicQueued, job, "")
	log.Printf("download queued: %q by %q (job %s, downloader %s)", job.Title, job.Artist, shortID(job.ID), job.DownloaderName)
	id := job.ID

	// Unlock BEFORE dispatching. Workers re-acquire m.mu inside the progress
	// callback, so a blocking send under m.mu would deadlock. The job is already
	// persisted as queued, so nothing is lost even if we shut down between here and
	// the dispatch.
	m.mu.Unlock()

	// Async downloader (e.g. Lidarr): hand off via Submit and let the reconciler
	// advance the job — never pin a worker. Submit runs OUTSIDE m.mu (it makes
	// network calls).
	if async, ok := dl.(AsyncDownloader); ok {
		m.submitAsync(ctx, job, req, async)
		return job, nil
	}

	// Sync downloader: dispatch to the worker pool.
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
		// Wait at the gate BEFORE pulling a job so a paused queue dispatches
		// nothing new. In-flight jobs are unaffected (already pulled). Stop
		// unblocks a gated worker.
		select {
		case <-m.stopCh:
			return
		case <-m.gate():
		}
		select {
		case <-m.stopCh:
			return
		case id := <-m.queue:
			// Guard: if Pause() arrived while we were waiting at the queue,
			// return the job to the channel and re-loop so the gate blocks us.
			select {
			case <-m.gate():
				m.process(id)
			default:
				// Paused — re-queue and let the next gate iteration block.
				select {
				case m.queue <- id:
				case <-m.stopCh:
					return
				}
			}
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
	// Use !haveReq as the sole sentinel: a map hit is always valid even when
	// ExternalID=="" (non-Spotify jobs have a real request with an empty ExternalID).
	if !haveReq {
		// Recover Granularity from the persisted request_json so that a cross-restart
		// album sync job (e.g. spotDL album mode) uses AlbumJobTimeout and the correct
		// /album/ URL rather than silently defaulting to track. Other fields (artist,
		// title, album, ISRC, etc.) are already denormalized onto the job row; only
		// Granularity is absent from the flat columns and must be read from request_json.
		stored, _, _ := m.store.GetRequest(ctx, id)
		req = core.DownloadRequest{
			Source: job.Source, ExternalID: job.ExternalID, Artist: job.Artist,
			Title: job.Title, Album: job.Album, ISRC: job.ISRC,
			PlayWhenReady:   job.PlayWhenReady,
			AddToPlaylistID: job.AddToPlaylistID,
			Granularity:     stored.Granularity,
		}
	}
	m.mu.Lock()
	jobTmo := m.jobTimeout(req)
	jctx, cancel := context.WithTimeout(ctx, jobTmo)
	m.cancels[id] = cancel
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.cancels, id)
		m.mu.Unlock()
		cancel()
	}()

	var dl Downloader
	for _, e := range m.downloaders {
		if e.Downloader.Name() == job.DownloaderName {
			dl = e.Downloader
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

	// Fallback loop: try the current downloader; on a genuine "couldn't produce"
	// error (not timeout, not cancel) fall through to the next downloader in the same
	// granularity chain via pickAfter.  Only when the chain is exhausted does the job
	// reach DownloadFailed.  Timeout and cancel remain terminal — no fallback.
	//
	// Note: this fallback is the sync (track) lane only.  Async downloaders (e.g.
	// Lidarr) run on their own reconciler lane and are not subject to this loop.
	var outPath string
	var lastErr error
	for {
		log.Printf("download attempting: %q (job %s via %s)", job.Title, shortID(id), dl.Name())

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

		var serr error
		outPath, serr = dl.Start(jctx, req, func(p int) {
			m.mu.Lock()
			cur, _, _ := m.store.Get(ctx, id)
			cur.Progress = p
			_ = m.store.Update(ctx, cur)
			m.mu.Unlock()
			m.publishEvent(TopicProgress, cur, "")
		})
		close(hbStop)

		if serr == nil {
			// Success — break out of the loop to the completion path below.
			lastErr = nil
			break
		}

		switch {
		case errors.Is(jctx.Err(), context.DeadlineExceeded):
			// Hit the per-job timeout — terminal, no fallback.
			cur, _, _ := m.store.Get(ctx, id)
			cur.Status = core.DownloadFailed
			cur.Error = fmt.Sprintf("timed out after %s", jobTmo)
			cur.FinishedAt = m.clock.Now().Unix()
			_ = m.store.Update(ctx, cur)
			m.publishEvent(TopicFailed, cur, cur.Error)
			log.Printf("download timed out: %q (job %s) after %s", cur.Title, shortID(id), jobTmo)
			m.mu.Lock()
			delete(m.reqs, id)
			m.mu.Unlock()
			return
		case jctx.Err() == context.Canceled:
			// Explicitly canceled — terminal, no fallback.
			cur, _, _ := m.store.Get(ctx, id)
			cur.Status = core.DownloadCanceled
			cur.FinishedAt = m.clock.Now().Unix()
			_ = m.store.Update(ctx, cur)
			m.publishEvent(TopicFailed, cur, "canceled")
			m.mu.Lock()
			delete(m.reqs, id)
			m.mu.Unlock()
			return
		default:
			// The chosen downloader couldn't produce the file (e.g. spotDL
			// LookupError).  Try the next downloader in the same granularity
			// chain before giving up.  The manual "download from a link" fallback
			// (DownloadRequest.ManualURL, surfaced on the failed state) remains the
			// last resort and is only reachable once every auto-downloader is exhausted.
			log.Printf("download: %q (job %s) downloader %q failed (%v), trying next in chain", job.Title, shortID(id), dl.Name(), serr)
			next, nerr := m.pickAfter(ctx, req, dl.Name())
			if nerr != nil {
				// Chain exhausted — fall through to DownloadFailed.
				lastErr = serr
				break
			}
			// Persist the new downloader name so the job is recoverable after restart.
			cur, _, _ := m.store.Get(ctx, id)
			cur.DownloaderName = next.Name()
			_ = m.store.Update(ctx, cur)
			dl = next
			continue
		}
		// Reached only when default: breaks (chain exhausted).
		break
	}

	if lastErr != nil {
		// All downloaders in the chain failed — mark the job failed.
		// The ManualURL last-resort (Retry with a URL) is still available to the user.
		cur, _, _ := m.store.Get(ctx, id)
		cur.Status = core.DownloadFailed
		cur.Error = lastErr.Error()
		cur.FinishedAt = m.clock.Now().Unix()
		_ = m.store.Update(ctx, cur)
		m.publishEvent(TopicFailed, cur, lastErr.Error())
		log.Printf("download failed (chain exhausted): %q (job %s) — %v", cur.Title, shortID(id), lastErr)
		m.mu.Lock()
		delete(m.reqs, id)
		m.mu.Unlock()
		return
	}

	cur, _, _ := m.store.Get(ctx, id)
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

		// One-time import hook: if the originating request named a target playlist,
		// append the newly-matched library track to it. Non-fatal on error.
		// AddToPlaylistID is carried on the job (mirrored from request_json by
		// toCoreFlatRow / Enqueue) so no extra store read is needed.
		if m.playlists != nil && j.AddToPlaylistID != "" {
			if paErr := m.playlists.AddTracksToPlaylist(ctx, j.AddToPlaylistID, []string{res.LibraryTrackID}); paErr != nil {
				log.Printf("download: add track %s to playlist %s failed: %v", res.LibraryTrackID, j.AddToPlaylistID, paErr)
			}
		}
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

	// Async job (running via an external manager like Lidarr): cancel externally.
	if job.DownloaderRef != "" {
		if async := m.asyncFor(job.DownloaderName); async != nil {
			_ = async.CancelAsync(ctx, job.DownloaderRef)
		}
		job.Status = core.DownloadCanceled
		job.FinishedAt = m.clock.Now().Unix()
		if err := m.store.Update(ctx, job); err != nil {
			return err
		}
		m.publishEvent(TopicFailed, job, "canceled")
		m.mu.Lock()
		delete(m.reqs, jobID)
		m.mu.Unlock()
		return nil
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
//
// Dispatch mirrors Enqueue: async downloaders (e.g. Lidarr) are re-submitted via
// submitAsync (not the sync worker channel) so a failed album job is re-queued on
// the async lane rather than routing to Start (which returns an error for async
// downloaders). Sync downloaders continue to use the worker channel as before.
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
	// When a manual URL is provided, seed (or update) the in-memory request AND
	// persist it to request_json so the ManualURL survives a server restart between
	// Retry and the worker picking up the job.
	if manualURL != "" {
		m.mu.Lock()
		req, haveReq := m.reqs[job.ID]
		// Use !haveReq as the sole sentinel — a map hit is always valid even when
		// ExternalID=="" (non-Spotify jobs have an empty ExternalID but a real request).
		if !haveReq {
			// Recover Granularity from request_json; fall back to job fields for the
			// remaining metadata. This ensures a ManualURL retry on a cross-restart job
			// also gets the correct granularity (album vs track).
			stored, _, _ := m.store.GetRequest(ctx, job.ID)
			req = core.DownloadRequest{
				Source: job.Source, ExternalID: job.ExternalID, Artist: job.Artist,
				Title: job.Title, Album: job.Album, ISRC: job.ISRC,
				PlayWhenReady: job.PlayWhenReady,
				Granularity:   stored.Granularity,
			}
		}
		req.ManualURL = manualURL
		m.reqs[job.ID] = req
		m.mu.Unlock()
		// Persist to the store so request_json carries ManualURL after a restart.
		if err := m.store.UpdateRequest(ctx, job.ID, req); err != nil {
			log.Printf("download: Retry %s: failed to persist ManualURL to store: %v", shortID(job.ID), err)
			// Non-fatal: the in-memory path still works for the no-restart fast path.
		}
	}

	// Ensure the in-memory request is seeded for dispatch. If it's not already in
	// m.reqs (cross-restart case or plain retry with no prior SeedRequest), reconstruct
	// it from the job fields and recover Granularity from the persisted request_json.
	// This guarantees the async-routing branch below and submitAsync both receive a
	// request with the correct Granularity (album vs track).
	m.mu.Lock()
	req, haveReq := m.reqs[job.ID]
	if !haveReq {
		stored, _, _ := m.store.GetRequest(ctx, job.ID)
		req = core.DownloadRequest{
			Source: job.Source, ExternalID: job.ExternalID, Artist: job.Artist,
			Title: job.Title, Album: job.Album, ISRC: job.ISRC,
			PlayWhenReady:   job.PlayWhenReady,
			AddToPlaylistID: job.AddToPlaylistID,
			Granularity:     stored.Granularity,
		}
		m.reqs[job.ID] = req
	}
	m.mu.Unlock()

	m.publishEvent(TopicQueued, job, "")

	// Mirror Enqueue's dispatch routing: async downloaders go via the async lane
	// (Submit → reconciler advances); sync downloaders go to the worker channel.
	// Before this fix, ALL retries pushed to the worker channel — causing async
	// (album/Lidarr) jobs to call Start, which returns "Start is not used (async
	// downloader)" and immediately fails the retried job.
	if async := m.asyncFor(job.DownloaderName); async != nil {
		go m.submitAsync(context.WithoutCancel(ctx), job, req, async)
		return job, nil
	}
	select {
	case m.queue <- job.ID:
	case <-m.stopCh:
	}
	return job, nil
}

// Clear hard-deletes a single terminal job (completed/failed/canceled) and
// publishes download.removed. It refuses to delete a queued/running job — those
// are canceled, not cleared.
func (m *Manager) Clear(ctx context.Context, jobID string) error {
	job, ok, err := m.store.Get(ctx, jobID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("job %q not found", jobID)
	}
	if job.Status == core.DownloadQueued || job.Status == core.DownloadRunning {
		return fmt.Errorf("cannot clear active job %q (status %s)", jobID, job.Status)
	}
	if err := m.store.Delete(ctx, jobID); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.reqs, jobID)
	m.mu.Unlock()
	m.publishRemoved([]string{jobID})
	return nil
}

// ClearFinished hard-deletes ALL terminal jobs and publishes a single
// download.removed carrying every deleted id.
func (m *Manager) ClearFinished(ctx context.Context) ([]string, error) {
	ids, err := m.store.DeleteFinished(ctx)
	if err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		m.mu.Lock()
		for _, id := range ids {
			delete(m.reqs, id)
		}
		m.mu.Unlock()
		m.publishRemoved(ids)
	}
	return ids, nil
}

// publishRemoved emits download.removed for the given job ids.
func (m *Manager) publishRemoved(ids []string) {
	if m.bus == nil {
		return
	}
	m.bus.Publish(events.Event{Topic: TopicRemoved, Payload: core.DownloadRemovedEvent{JobIDs: ids}})
}

// gate returns the current resume channel. When running it is closed (receiving
// returns instantly); when paused it is open (receiving blocks until Resume).
func (m *Manager) gate() <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.resumeCh
}

// Pause stops dispatching NEW jobs. Jobs already running finish; queued jobs stay
// queued. In-memory only (a restart comes up running). Idempotent.
func (m *Manager) Pause() {
	m.mu.Lock()
	if m.paused {
		m.mu.Unlock()
		return
	}
	m.paused = true
	m.resumeCh = make(chan struct{}) // open: workers block on it at the gate
	m.mu.Unlock()
	m.publishQueueState(true)
}

// Resume re-enables dispatch, unblocking any gated workers. Idempotent.
func (m *Manager) Resume() {
	m.mu.Lock()
	if !m.paused {
		m.mu.Unlock()
		return
	}
	m.paused = false
	close(m.resumeCh) // unblock workers; the now-closed channel reads as "running"
	m.mu.Unlock()
	m.publishQueueState(false)
}

// IsPaused reports the current gate state.
func (m *Manager) IsPaused() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.paused
}

// publishQueueState emits download.queue with the current paused flag.
func (m *Manager) publishQueueState(paused bool) {
	if m.bus == nil {
		return
	}
	m.bus.Publish(events.Event{Topic: TopicQueueState, Payload: core.QueueStateEvent{Paused: paused}})
}

// SeedRequest is the seam for cross-restart request recovery: it rehydrates the
// originating request (including Granularity) for a job whose in-memory entry was
// lost across a process restart. Not yet wired to a production caller — deferred
// until the composition root drives restart rehydration from request_json.
func (m *Manager) SeedRequest(jobID string, req core.DownloadRequest) {
	m.mu.Lock()
	m.reqs[jobID] = req
	m.mu.Unlock()
}
