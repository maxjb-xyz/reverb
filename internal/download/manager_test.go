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

// fakeRematcher returns a fixed in-library match.
type fakeRematcher struct{ trackID string }

func (r fakeRematcher) Match(context.Context, core.ExternalResult) (core.MatchResult, error) {
	if r.trackID == "" {
		return core.MatchResult{Status: core.MatchNotInLibrary, Method: core.MatchNone}, nil
	}
	return core.MatchResult{Status: core.MatchInLibrary, LibraryTrackID: r.trackID, Method: core.MatchFuzzy, Confidence: 0.9}, nil
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
		rematch = fakeRematcher{trackID: "t1"}
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
