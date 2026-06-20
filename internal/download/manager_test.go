package download

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/maximusjb/crate/internal/core"
	"github.com/maximusjb/crate/internal/events"
	"github.com/maximusjb/crate/internal/registry"
)

// ---- fakes ----

// fakeDL is a controllable downloader. canDownload gates the fallback chain;
// block lets a test hold a download open (to assert dedup-join while in-flight).
// errOnStart, if non-nil, makes Start return that error immediately.
type fakeDL struct {
	name        string
	canDownload bool
	block       chan struct{} // if non-nil, Start blocks until closed/canceled
	errOnStart  error        // if non-nil, Start returns this error
	started     int32
	mu          sync.Mutex
	startCount  int
}

func (d *fakeDL) Type() string                             { return "downloader" }
func (d *fakeDL) Name() string                             { return d.name }
func (d *fakeDL) ConfigSchema() registry.ConfigSchema      { return registry.ConfigSchema{} }
func (d *fakeDL) Init(map[string]any) error                { return nil }
func (d *fakeDL) TestConnection(context.Context) error     { return nil }
func (d *fakeDL) CanDownload(context.Context, core.DownloadRequest) (bool, error) {
	return d.canDownload, nil
}
func (d *fakeDL) Start(ctx context.Context, req core.DownloadRequest, onProgress func(int)) (string, error) {
	d.mu.Lock()
	d.startCount++
	d.mu.Unlock()
	if d.errOnStart != nil {
		return "", d.errOnStart
	}
	onProgress(50)
	if d.block != nil {
		select {
		case <-d.block:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	onProgress(100)
	return "/out/" + req.ExternalID + ".mp3", nil
}
func (d *fakeDL) starts() int { d.mu.Lock(); defer d.mu.Unlock(); return d.startCount }

// fakeScanner records StartScan calls and reports a completed scan immediately.
type fakeScanner struct {
	mu    sync.Mutex
	scans int
}

func (s *fakeScanner) StartScan(context.Context) error {
	s.mu.Lock()
	s.scans++
	s.mu.Unlock()
	return nil
}
func (s *fakeScanner) ScanStatus(context.Context) (core.ScanStatus, error) {
	return core.ScanStatus{Scanning: false, Count: 1}, nil
}
func (s *fakeScanner) count() int { s.mu.Lock(); defer s.mu.Unlock(); return s.scans }

// fakeRematcher returns a fixed in-library match and records the last ExternalResult it saw.
type fakeRematcher struct {
	trackID string
	mu      sync.Mutex
	lastReq core.ExternalResult
}

func (r *fakeRematcher) Match(_ context.Context, ext core.ExternalResult) (core.MatchResult, error) {
	r.mu.Lock()
	r.lastReq = ext
	r.mu.Unlock()
	if r.trackID == "" {
		return core.MatchResult{Status: core.MatchNotInLibrary, Method: core.MatchNone}, nil
	}
	return core.MatchResult{Status: core.MatchInLibrary, LibraryTrackID: r.trackID, Method: core.MatchFuzzy, Confidence: 0.9}, nil
}

func (r *fakeRematcher) getLastReq() core.ExternalResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastReq
}

// fakeVersion is an in-memory VersionBumper.
type fakeVersion struct {
	mu sync.Mutex
	v  int64
}

func (f *fakeVersion) LibraryVersion(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.v == 0 {
		f.v = 1
	}
	return f.v, nil
}
func (f *fakeVersion) SetLibraryVersion(_ context.Context, v int64) error {
	f.mu.Lock()
	f.v = v
	f.mu.Unlock()
	return nil
}
func (f *fakeVersion) get() int64 { f.mu.Lock(); defer f.mu.Unlock(); return f.v }

// memStore is an in-memory JobStore (no SQLite) for fast concurrency tests.
type memStore struct {
	mu   sync.Mutex
	jobs map[string]core.DownloadJob
}

func newMemStore() *memStore { return &memStore{jobs: map[string]core.DownloadJob{}} }

func (s *memStore) Insert(_ context.Context, j core.DownloadJob, _ core.DownloadRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[j.ID] = j
	return nil
}
func (s *memStore) Get(_ context.Context, id string) (core.DownloadJob, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	return j, ok, nil
}
func (s *memStore) ActiveByDedup(_ context.Context, dedup string) (core.DownloadJob, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.jobs {
		if j.DedupKey == dedup && (j.Status == core.DownloadQueued || j.Status == core.DownloadRunning) {
			return j, true, nil
		}
	}
	return core.DownloadJob{}, false, nil
}
func (s *memStore) List(_ context.Context) ([]core.DownloadJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]core.DownloadJob, 0, len(s.jobs))
	for _, j := range s.jobs {
		out = append(out, j)
	}
	return out, nil
}
func (s *memStore) Update(_ context.Context, j core.DownloadJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[j.ID] = j
	return nil
}

// helper: drain bus events of a topic into a slice (test-only).
func drain(bus *events.Bus, topic string, into *[]core.DownloadEvent, wg *sync.WaitGroup, want int) (stop func()) {
	ch, unsub := bus.Subscribe(topic)
	go func() {
		for ev := range ch {
			if de, ok := ev.Payload.(core.DownloadEvent); ok {
				*into = append(*into, de)
				if len(*into) >= want {
					wg.Done()
					return
				}
			}
		}
	}()
	return unsub
}

func testManager(t *testing.T, downloaders []Downloader, store JobStore, rematch Rematcher, ver VersionBumper, clk Clock) (*Manager, *events.Bus) {
	t.Helper()
	bus := events.New()
	scanner := &fakeScanner{}
	if rematch == nil {
		rematch = &fakeRematcher{trackID: "t1"}
	}
	if ver == nil {
		ver = &fakeVersion{v: 1}
	}
	if clk == nil {
		clk = RealClock{}
	}
	m := NewManager(Config{Workers: 2, DebounceWindow: 5 * time.Second, ScanPollEvery: time.Millisecond, ScanPollMax: time.Second},
		downloaders, store, bus, scanner, rematch, ver, clk)
	t.Cleanup(m.Stop)
	m.Start()
	return m, bus
}

func TestEnqueuePicksDownloaderViaFallback(t *testing.T) {
	cant := &fakeDL{name: "cant", canDownload: false}
	can := &fakeDL{name: "can", canDownload: true}
	store := newMemStore()
	m, _ := testManager(t, []Downloader{cant, can}, store, nil, nil, nil)

	job, err := m.Enqueue(context.Background(), core.DownloadRequest{Source: "spotify", ExternalID: "e1", Artist: "A", Title: "T", Album: "Al"})
	if err != nil {
		t.Fatal(err)
	}
	if job.DownloaderName != "can" {
		t.Fatalf("fallback should pick 'can', got %q", job.DownloaderName)
	}
}

func TestEnqueueNoDownloaderAccepts(t *testing.T) {
	cant := &fakeDL{name: "cant", canDownload: false}
	store := newMemStore()
	m, _ := testManager(t, []Downloader{cant}, store, nil, nil, nil)
	_, err := m.Enqueue(context.Background(), core.DownloadRequest{Source: "spotify", ExternalID: "e1", Title: "T"})
	if err == nil {
		t.Fatal("expected error when no downloader accepts")
	}
}

func TestEnqueueExplicitDownloader(t *testing.T) {
	a := &fakeDL{name: "a", canDownload: true}
	b := &fakeDL{name: "b", canDownload: true}
	store := newMemStore()
	m, _ := testManager(t, []Downloader{a, b}, store, nil, nil, nil)
	job, err := m.Enqueue(context.Background(), core.DownloadRequest{Source: "s", ExternalID: "e", Title: "T", Downloader: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if job.DownloaderName != "b" {
		t.Fatalf("explicit downloader ignored, got %q", job.DownloaderName)
	}
}

func TestDedupJoinWhileInFlight(t *testing.T) {
	block := make(chan struct{})
	dl := &fakeDL{name: "dl", canDownload: true, block: block}
	store := newMemStore()
	m, _ := testManager(t, []Downloader{dl}, store, nil, nil, nil)

	req := core.DownloadRequest{Source: "spotify", ExternalID: "e1", Artist: "A", Title: "T", Album: "Al"}
	j1, err := m.Enqueue(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	// Wait until the worker has actually started the in-flight download.
	deadline := time.After(2 * time.Second)
	for dl.starts() == 0 {
		select {
		case <-deadline:
			t.Fatal("download never started")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	// A second identical request must JOIN the in-flight job, not start a new one.
	j2, err := m.Enqueue(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if j2.ID != j1.ID {
		t.Fatalf("dedup-join failed: j2=%q j1=%q", j2.ID, j1.ID)
	}
	close(block)
	if dl.starts() != 1 {
		t.Fatalf("download should have started exactly once, got %d", dl.starts())
	}
}

func TestConcurrentEnqueueSameKeyOneJob(t *testing.T) {
	block := make(chan struct{})
	dl := &fakeDL{name: "dl", canDownload: true, block: block}
	store := newMemStore()
	m, _ := testManager(t, []Downloader{dl}, store, nil, nil, nil)
	req := core.DownloadRequest{Source: "spotify", ExternalID: "e1", Artist: "A", Title: "T", Album: "Al"}

	const n = 8
	ids := make([]string, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			j, err := m.Enqueue(context.Background(), req)
			if err != nil {
				t.Errorf("enqueue: %v", err)
				return
			}
			ids[i] = j.ID
		}(i)
	}
	wg.Wait()
	close(block)
	for i := 1; i < n; i++ {
		if ids[i] != ids[0] {
			t.Fatalf("concurrent same-key enqueues produced different jobs: %v", ids)
		}
	}
}

// fakeTimer is a scheduled AfterFunc the fakeClock controls.
type fakeTimer struct {
	at      time.Time
	fn      func()
	stopped bool
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
	fns []*fakeTimer
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Unix(1_700_000_000, 0)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) func() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{at: c.now.Add(d), fn: f}
	c.fns = append(c.fns, t)
	return func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		if t.stopped {
			return false
		}
		t.stopped = true
		return true
	}
}

// Advance moves time forward and fires all timers now due (in order).
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []*fakeTimer
	for _, t := range c.fns {
		if !t.stopped && !t.at.After(c.now) {
			t.stopped = true
			due = append(due, t)
		}
	}
	c.mu.Unlock()
	for _, t := range due {
		t.fn()
	}
}

func TestCompletionDebouncesIntoOneScan(t *testing.T) {
	clk := newFakeClock()
	dl := &fakeDL{name: "dl", canDownload: true}
	store := newMemStore()
	scanner := &fakeScanner{}
	ver := &fakeVersion{v: 1}
	bus := events.New()
	m := NewManager(Config{Workers: 3, DebounceWindow: 5 * time.Second, ScanPollEvery: time.Millisecond, ScanPollMax: time.Second},
		[]Downloader{dl}, store, bus, scanner, &fakeRematcher{trackID: "t1"}, ver, clk)
	t.Cleanup(m.Stop)
	m.Start()

	// Enqueue several distinct jobs; each completes quickly (no block).
	for i := 0; i < 4; i++ {
		_, err := m.Enqueue(context.Background(), core.DownloadRequest{
			Source: "spotify", ExternalID: string(rune('a' + i)), Artist: "A", Title: "T" + string(rune('a'+i)), Album: "Al",
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	// Wait for all 4 downloads to finish (they schedule debounced scans).
	deadline := time.After(2 * time.Second)
	for {
		jobs, _ := store.List(context.Background())
		done := 0
		for _, j := range jobs {
			if j.Status == core.DownloadCompleted || j.Status == core.DownloadFailed {
				done++
			}
		}
		if done == 4 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("downloads did not complete (done=%d)", done)
		default:
			time.Sleep(time.Millisecond)
		}
	}

	if scanner.count() != 0 {
		t.Fatalf("scan must NOT fire before the debounce window elapses, got %d", scanner.count())
	}
	// Advance past the window: the coalesced completions trigger exactly ONE scan.
	clk.Advance(5 * time.Second)
	// The scan + poll + rematch + version bump runs synchronously in the timer fn.
	if scanner.count() != 1 {
		t.Fatalf("expected exactly 1 coalesced StartScan, got %d", scanner.count())
	}
	if ver.get() != 2 {
		t.Fatalf("library_version must bump from 1 to 2 on scan completion, got %d", ver.get())
	}
}

func TestCompletionSetsLibraryTrackIDAndPublishesComplete(t *testing.T) {
	clk := newFakeClock()
	dl := &fakeDL{name: "dl", canDownload: true}
	store := newMemStore()
	bus := events.New()
	rematcher := &fakeRematcher{trackID: "lib-track-9"}
	m := NewManager(Config{Workers: 1, DebounceWindow: 5 * time.Second, ScanPollEvery: time.Millisecond, ScanPollMax: time.Second},
		[]Downloader{dl}, store, bus, &fakeScanner{}, rematcher, &fakeVersion{v: 1}, clk)
	t.Cleanup(m.Stop)
	m.Start()

	// Subscribe to the complete topic BEFORE enqueuing so we don't miss the post-scan event.
	completeCh, unsub := bus.Subscribe(TopicComplete)
	defer unsub()

	// Collect all TopicComplete events in the background.
	var completeEvents []core.DownloadEvent
	var ceMu sync.Mutex
	go func() {
		for ev := range completeCh {
			if de, ok := ev.Payload.(core.DownloadEvent); ok {
				ceMu.Lock()
				completeEvents = append(completeEvents, de)
				ceMu.Unlock()
			}
		}
	}()

	job, err := m.Enqueue(context.Background(), core.DownloadRequest{Source: "spotify", ExternalID: "e1", Artist: "A", Title: "T", Album: "Al"})
	if err != nil {
		t.Fatal(err)
	}

	// Wait until the job is completed in the store (worker finished downloading).
	deadline := time.After(2 * time.Second)
	for {
		cur, _, _ := store.Get(context.Background(), job.ID)
		if cur.Status == core.DownloadCompleted {
			break
		}
		select {
		case <-deadline:
			t.Fatal("job never completed")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	// Advance the fake clock to fire the debounced scan → re-match → set library_track_id.
	// runScan executes synchronously inside Advance, so by the time Advance returns
	// the re-match has run, the store is updated, and publishComplete has been called.
	clk.Advance(5 * time.Second)

	// Regression test (Fix 1): assert the rematcher received real metadata, not an empty
	// ExternalResult. An empty Title means the candidate query has nothing to search
	// → MatchNotInLibrary → library_track_id never set → the loop never closes.
	lastReq := rematcher.getLastReq()
	if lastReq.Title == "" {
		t.Fatal("re-match ExternalResult.Title is empty: manager passed no metadata to the rematcher (regression)")
	}
	if lastReq.Artist == "" {
		t.Fatal("re-match ExternalResult.Artist is empty: manager passed no metadata to the rematcher (regression)")
	}
	if lastReq.Title != "T" {
		t.Fatalf("re-match ExternalResult.Title: got %q want %q", lastReq.Title, "T")
	}
	if lastReq.Artist != "A" {
		t.Fatalf("re-match ExternalResult.Artist: got %q want %q", lastReq.Artist, "A")
	}

	// Assert the store reflects the re-matched library_track_id.
	cur, _, _ := store.Get(context.Background(), job.ID)
	if cur.LibraryTrackID != "lib-track-9" {
		t.Fatalf("library_track_id not set after re-match, got %q", cur.LibraryTrackID)
	}

	// Allow the goroutine a moment to deliver the event (channel send is non-blocking;
	// goroutine scheduling may not have run yet).
	deadline2 := time.After(time.Second)
	for {
		ceMu.Lock()
		n := len(completeEvents)
		ceMu.Unlock()
		if n >= 2 { // first emit: job completes; second emit: post-scan publishComplete
			break
		}
		select {
		case <-deadline2:
			// Give it one final check before failing.
			goto checkEvents
		default:
			time.Sleep(time.Millisecond)
		}
	}
checkEvents:
	// Find the post-scan complete event that carries libraryTrackId.
	ceMu.Lock()
	evs := make([]core.DownloadEvent, len(completeEvents))
	copy(evs, completeEvents)
	ceMu.Unlock()

	var found *core.DownloadEvent
	for i := range evs {
		if evs[i].JobID == job.ID && evs[i].LibraryTrackID == "lib-track-9" {
			found = &evs[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no download.complete event with libraryTrackId=%q for job %q; got events: %+v",
			"lib-track-9", job.ID, evs)
	}
	if found.Source != "spotify" {
		t.Fatalf("complete event source: got %q want %q", found.Source, "spotify")
	}
	if found.ExternalID != "e1" {
		t.Fatalf("complete event externalId: got %q want %q", found.ExternalID, "e1")
	}
}

func TestCancelInFlight(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	dl := &fakeDL{name: "dl", canDownload: true, block: block}
	store := newMemStore()
	m, _ := testManager(t, []Downloader{dl}, store, nil, nil, nil)

	job, err := m.Enqueue(context.Background(), core.DownloadRequest{Source: "s", ExternalID: "e1", Artist: "A", Title: "T", Album: "Al"})
	if err != nil {
		t.Fatal(err)
	}
	// Wait for in-flight.
	deadline := time.After(2 * time.Second)
	for dl.starts() == 0 {
		select {
		case <-deadline:
			t.Fatal("never started")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if err := m.Cancel(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	// The job must reach canceled status.
	for {
		cur, _, _ := store.Get(context.Background(), job.ID)
		if cur.Status == core.DownloadCanceled {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("job not canceled")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func TestRetryResetsFailedJob(t *testing.T) {
	store := newMemStore()
	// Seed a failed job directly.
	failed := core.DownloadJob{ID: "j1", DedupKey: "dk", Status: core.DownloadFailed, DownloaderName: "dl", Attempts: 1, Source: "s", ExternalID: "e1"}
	_ = store.Insert(context.Background(), failed, core.DownloadRequest{Source: "s", ExternalID: "e1", Artist: "A", Title: "T", Album: "Al"})

	dl := &fakeDL{name: "dl", canDownload: true}
	m, _ := testManager(t, []Downloader{dl}, store, nil, nil, nil)
	// Rehydrate the request map so the worker can run the retried job.
	m.SeedRequest("j1", core.DownloadRequest{Source: "s", ExternalID: "e1", Artist: "A", Title: "T", Album: "Al"})

	j, err := m.Retry(context.Background(), "j1")
	if err != nil {
		t.Fatal(err)
	}
	if j.Status != core.DownloadQueued {
		t.Fatalf("retry should set queued, got %q", j.Status)
	}
	if j.Attempts != 2 {
		t.Fatalf("retry should bump attempts to 2, got %d", j.Attempts)
	}
}

func TestFailedDownloadPublishesFailedEvent(t *testing.T) {
	dlErr := errors.New("network timeout")
	dl := &fakeDL{name: "dl", canDownload: true, errOnStart: dlErr}
	store := newMemStore()
	m, bus := testManager(t, []Downloader{dl}, store, nil, nil, nil)

	// Subscribe to download.failed before enqueuing.
	var failedEvents []core.DownloadEvent
	var evMu sync.Mutex
	gotFailed := make(chan struct{})
	ch, unsub := bus.Subscribe(TopicFailed)
	defer unsub()
	go func() {
		for ev := range ch {
			if de, ok := ev.Payload.(core.DownloadEvent); ok {
				evMu.Lock()
				failedEvents = append(failedEvents, de)
				evMu.Unlock()
				close(gotFailed)
				return
			}
		}
	}()

	job, err := m.Enqueue(context.Background(), core.DownloadRequest{
		Source: "spotify", ExternalID: "fail1", Artist: "A", Title: "T", Album: "Al",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Wait for the failed event (or timeout).
	select {
	case <-gotFailed:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for download.failed event")
	}

	evMu.Lock()
	defer evMu.Unlock()
	if len(failedEvents) == 0 {
		t.Fatal("no download.failed events received")
	}
	fe := failedEvents[0]
	if fe.JobID != job.ID {
		t.Fatalf("failed event job ID mismatch: got %q want %q", fe.JobID, job.ID)
	}
	if fe.Status != core.DownloadFailed {
		t.Fatalf("failed event status: got %v want DownloadFailed", fe.Status)
	}
	if fe.Error != dlErr.Error() {
		t.Fatalf("failed event error: got %q want %q", fe.Error, dlErr.Error())
	}

	// Verify the job is persisted as failed.
	persisted, ok, err := store.Get(context.Background(), job.ID)
	if err != nil || !ok {
		t.Fatalf("job not found in store: %v", err)
	}
	if persisted.Status != core.DownloadFailed {
		t.Fatalf("persisted job status: got %v want DownloadFailed", persisted.Status)
	}
}
