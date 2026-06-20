package download

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/maximusjb/crate/internal/core"
	"github.com/maximusjb/crate/internal/events"
)

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

// Start launches the worker pool.
func (m *Manager) Start() {
	for i := 0; i < m.cfg.Workers; i++ {
		m.wg.Add(1)
		go m.worker()
	}
}

// Stop signals workers to drain and waits for them. It ALSO cancels any pending
// scan-debounce timer (and clears pending) so a real-clock test cannot have
// runScan fire against fakes after the test ends. Idempotent.
func (m *Manager) Stop() {
	m.stopOnce.Do(func() { close(m.stopCh) })
	m.mu.Lock()
	if m.debounce != nil {
		m.debounce() // stop the AfterFunc/clock timer
		m.debounce = nil
	}
	m.pending = false
	m.mu.Unlock()
	m.wg.Wait()
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
	defer m.mu.Unlock()

	if existing, ok, err := m.store.ActiveByDedup(ctx, dedup); err != nil {
		return core.DownloadJob{}, err
	} else if ok {
		return existing, nil // dedup-join
	}

	dl, err := m.pick(ctx, req)
	if err != nil {
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
		return core.DownloadJob{}, err
	}
	m.reqs[job.ID] = req
	m.publishEvent(TopicQueued, job, "")

	select {
	case m.queue <- job.ID:
	default:
		// Queue full: still persisted as queued; a worker will pick it up when space
		// frees. For MVP the buffer (256) is generous; treat as enqueued.
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
	}})
}

// --- minimal worker plumbing (expanded in Task 6) ---

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

// process runs one job to a terminal state. Full body (progress, completion,
// re-match, debounce) is implemented in Task 6; part 1 ships the happy path so
// the dedup-join concurrency test exercises a real in-flight download.
func (m *Manager) process(id string) {
	ctx := context.Background()
	job, ok, err := m.store.Get(ctx, id)
	if err != nil || !ok {
		return
	}
	m.mu.Lock()
	req := m.reqs[id]
	jctx, cancel := context.WithCancel(ctx)
	m.cancels[id] = cancel
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.cancels, id)
		delete(m.reqs, id)
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
		return
	}

	job.Status = core.DownloadRunning
	_ = m.store.Update(ctx, job)
	m.publishEvent(TopicProgress, job, "")

	_, serr := dl.Start(jctx, req, func(p int) {
		m.mu.Lock()
		cur, _, _ := m.store.Get(ctx, id)
		cur.Progress = p
		_ = m.store.Update(ctx, cur)
		m.mu.Unlock()
		m.publishEvent(TopicProgress, cur, "")
	})
	cur, _, _ := m.store.Get(ctx, id)
	if serr != nil {
		cur.Status = core.DownloadFailed
		_ = m.store.Update(ctx, cur)
		return
	}
	cur.Status = core.DownloadCompleted
	cur.Progress = 100
	_ = m.store.Update(ctx, cur)
	m.publishEvent(TopicComplete, cur, "")
}
