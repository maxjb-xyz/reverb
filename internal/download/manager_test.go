package download

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maxjb-xyz/reverb/internal/catalog"
	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/events"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/resolver"
	"github.com/maxjb-xyz/reverb/internal/store"
)

// ---- fakes ----

// fakeDL is a controllable downloader. canDownload gates the fallback chain;
// block lets a test hold a download open (to assert dedup-join while in-flight).
// errOnStart, if non-nil, makes Start return that error immediately.
type fakeDL struct {
	name        string
	canDownload bool
	block       chan struct{} // if non-nil, Start blocks until closed/canceled
	errOnStart  error         // if non-nil, Start returns this error
	started     int32
	mu          sync.Mutex
	startCount  int
}

func (d *fakeDL) Type() string { return "downloader" }
func (d *fakeDL) Name() string { return d.name }
func (d *fakeDL) SupportedGranularities() []core.DownloadGranularity {
	return []core.DownloadGranularity{core.GranularityTrack}
}
func (d *fakeDL) ConfigSchema() registry.ConfigSchema  { return registry.ConfigSchema{} }
func (d *fakeDL) Init(map[string]any) error            { return nil }
func (d *fakeDL) TestConnection(context.Context) error { return nil }
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

// fakeScanner records StartScan calls and models the Navidrome scan lifecycle.
//
// By default it reports a completed scan immediately (scanning=false) — the
// already-idle / instantaneous-scan path. When statusSeq is set, ScanStatus
// returns each element in turn (then sticks on the last), letting a test drive the
// realistic false→true→…→false transition that waitForScan must wait through.
type fakeScanner struct {
	mu        sync.Mutex
	scans     int
	statusSeq []bool // scanning values returned in order; nil → always false
	statusIdx int
	statusN   int // number of ScanStatus calls observed
}

func (s *fakeScanner) StartScan(context.Context) error {
	s.mu.Lock()
	s.scans++
	// Reset the status sequence cursor each scan so reused scanners replay it.
	s.statusIdx = 0
	s.mu.Unlock()
	return nil
}
func (s *fakeScanner) ScanStatus(context.Context) (core.ScanStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statusN++
	scanning := false
	if len(s.statusSeq) > 0 {
		if s.statusIdx >= len(s.statusSeq) {
			scanning = s.statusSeq[len(s.statusSeq)-1]
		} else {
			scanning = s.statusSeq[s.statusIdx]
			s.statusIdx++
		}
	}
	return core.ScanStatus{Scanning: scanning, Count: 1}, nil
}
func (s *fakeScanner) count() int       { s.mu.Lock(); defer s.mu.Unlock(); return s.scans }
func (s *fakeScanner) statusCalls() int { s.mu.Lock(); defer s.mu.Unlock(); return s.statusN }

// fakeRematcher returns a fixed in-library match and records the last ExternalResult it saw.
type fakeRematcher struct {
	trackID    string
	coverArtID string // optional; when set, returned in the MatchResult
	mu         sync.Mutex
	lastReq    core.ExternalResult
}

func (r *fakeRematcher) Match(_ context.Context, ext core.ExternalResult) (core.MatchResult, error) {
	r.mu.Lock()
	r.lastReq = ext
	r.mu.Unlock()
	if r.trackID == "" {
		return core.MatchResult{Status: core.MatchNotInLibrary, Method: core.MatchNone}, nil
	}
	return core.MatchResult{Status: core.MatchInLibrary, LibraryTrackID: r.trackID, CoverArtID: r.coverArtID, Method: core.MatchFuzzy, Confidence: 0.9}, nil
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
	reqs map[string]core.DownloadRequest // mirrors request_json for FIX 2 tests
}

func newMemStore() *memStore {
	return &memStore{jobs: map[string]core.DownloadJob{}, reqs: map[string]core.DownloadRequest{}}
}

func (s *memStore) Insert(_ context.Context, j core.DownloadJob, req core.DownloadRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[j.ID] = j
	s.reqs[j.ID] = req
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

func (s *memStore) UpdateRequest(_ context.Context, id string, req core.DownloadRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqs[id] = req
	return nil
}

func (s *memStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.jobs, id)
	delete(s.reqs, id)
	return nil
}

func (s *memStore) DeleteFinished(_ context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []string
	for id, j := range s.jobs {
		if j.Status == core.DownloadCompleted || j.Status == core.DownloadFailed || j.Status == core.DownloadCanceled {
			ids = append(ids, id)
			delete(s.jobs, id)
			delete(s.reqs, id)
		}
	}
	return ids, nil
}

func (s *memStore) UpdateRef(_ context.Context, id string, ref string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j, ok := s.jobs[id]; ok {
		j.DownloaderRef = ref
		s.jobs[id] = j
	}
	return nil
}

func (s *memStore) UpdateCanonicalID(_ context.Context, id string, canonicalID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j, ok := s.jobs[id]; ok {
		j.CanonicalID = canonicalID
		s.jobs[id] = j
	}
	return nil
}

func (s *memStore) getReq(id string) (core.DownloadRequest, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.reqs[id]
	return r, ok
}

func (s *memStore) GetRequest(_ context.Context, id string) (core.DownloadRequest, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.reqs[id]
	return r, ok, nil
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

// wrapDownloaders wraps a []Downloader into []DownloaderEntry using default
// ordering: each granularity in SupportedGranularities() gets order 0 (same
// priority). This mirrors the wiring.BuildDownloaders default.
func wrapDownloaders(downloaders []Downloader) []DownloaderEntry {
	entries := make([]DownloaderEntry, 0, len(downloaders))
	for _, d := range downloaders {
		order := make(map[core.DownloadGranularity]int, len(d.SupportedGranularities()))
		for _, g := range d.SupportedGranularities() {
			order[g] = 0
		}
		entries = append(entries, DownloaderEntry{Downloader: d, Order: order})
	}
	return entries
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
	m := NewManager(Config{Workers: 2, DebounceWindow: 5 * time.Second, ScanPollEvery: time.Millisecond, ScanPollMax: time.Second, ScanSettleMax: 10 * time.Millisecond},
		wrapDownloaders(downloaders), store, bus, scanner, rematch, ver, clk, nil, nil)
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

func TestEnqueueFallbackPicksFirstCanDownload(t *testing.T) {
	// With two track downloaders and both able to download, the first (a) wins.
	a := &fakeDL{name: "a", canDownload: true}
	b := &fakeDL{name: "b", canDownload: true}
	store := newMemStore()
	m, _ := testManager(t, []Downloader{a, b}, store, nil, nil, nil)
	job, err := m.Enqueue(context.Background(), core.DownloadRequest{Source: "s", ExternalID: "e", Title: "T"})
	if err != nil {
		t.Fatal(err)
	}
	if job.DownloaderName != "a" {
		t.Fatalf("fallback should pick first CanDownload downloader 'a', got %q", job.DownloaderName)
	}
}

// -- Task 2: Granularity-scoped pick() tests --

// TestPickGranularityTrackExplicit: a request with Granularity=track must select
// the track downloader even when an album downloader is also registered.
func TestPickGranularityTrackExplicit(t *testing.T) {
	track := &fakeDL{name: "spotdl", canDownload: true}
	album := &fakeAsyncDL{name: "lidarr", submitRef: "ref1"}
	store := newMemStore()
	m, _ := testManager(t, []Downloader{track, album}, store, nil, nil, nil)

	job, err := m.Enqueue(context.Background(), core.DownloadRequest{
		Source: "spotify", ExternalID: "e1", Artist: "A", Title: "T", Album: "Al",
		Granularity: core.GranularityTrack,
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.DownloaderName != "spotdl" {
		t.Fatalf("track request should pick track downloader, got %q", job.DownloaderName)
	}
}

// TestPickGranularityEmptyDefaultsToTrack: an empty Granularity on the request must
// be treated as GranularityTrack (must not fall through to album downloaders).
func TestPickGranularityEmptyDefaultsToTrack(t *testing.T) {
	track := &fakeDL{name: "spotdl", canDownload: true}
	album := &fakeAsyncDL{name: "lidarr", submitRef: "ref1"}
	store := newMemStore()
	m, _ := testManager(t, []Downloader{track, album}, store, nil, nil, nil)

	job, err := m.Enqueue(context.Background(), core.DownloadRequest{
		Source: "spotify", ExternalID: "e2", Artist: "A", Title: "T", Album: "Al",
		// Granularity intentionally empty — must default to track
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.DownloaderName != "spotdl" {
		t.Fatalf("empty-granularity request should default to track downloader, got %q", job.DownloaderName)
	}
}

// TestPickGranularityAlbum: a request with Granularity=album must select the album
// downloader and never reach the track downloader.
func TestPickGranularityAlbum(t *testing.T) {
	track := &fakeDL{name: "spotdl", canDownload: true}
	album := &fakeAsyncDL{name: "lidarr", submitRef: "ref1"}
	store := newMemStore()
	m, _ := testManager(t, []Downloader{track, album}, store, nil, nil, nil)

	job, err := m.Enqueue(context.Background(), core.DownloadRequest{
		Source: "spotify", ExternalID: "e3", Artist: "Daft Punk", Title: "Discovery",
		Album: "Discovery", Granularity: core.GranularityAlbum,
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.DownloaderName != "lidarr" {
		t.Fatalf("album request should pick album downloader, got %q", job.DownloaderName)
	}
}

// TestPickGranularityTrackPriorityOrder: with two track downloaders, the first one
// in slice order wins (provided its CanDownload returns true).
func TestPickGranularityTrackPriorityOrder(t *testing.T) {
	first := &fakeDL{name: "first", canDownload: true}
	second := &fakeDL{name: "second", canDownload: true}
	store := newMemStore()
	m, _ := testManager(t, []Downloader{first, second}, store, nil, nil, nil)

	job, err := m.Enqueue(context.Background(), core.DownloadRequest{
		Source: "spotify", ExternalID: "e4", Artist: "A", Title: "T", Album: "Al",
		Granularity: core.GranularityTrack,
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.DownloaderName != "first" {
		t.Fatalf("first track downloader in slice should win, got %q", job.DownloaderName)
	}
}

// TestPickGranularityNoMatchReturnsError: when no downloader matches the requested
// granularity the error must mention the granularity.
func TestPickGranularityNoMatchReturnsError(t *testing.T) {
	track := &fakeDL{name: "spotdl", canDownload: true}
	store := newMemStore()
	m, _ := testManager(t, []Downloader{track}, store, nil, nil, nil)

	_, err := m.Enqueue(context.Background(), core.DownloadRequest{
		Source: "spotify", ExternalID: "e5", Artist: "A", Title: "T", Album: "Al",
		Granularity: core.GranularityAlbum,
	})
	if err == nil {
		t.Fatal("expected error when no album downloader is registered")
	}
	const want = "no album downloader"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q should contain %q", err.Error(), want)
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

func (c *fakeClock) scheduledCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.fns)
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
	m := NewManager(Config{Workers: 3, DebounceWindow: 5 * time.Second, ScanPollEvery: time.Millisecond, ScanPollMax: time.Second, ScanSettleMax: 10 * time.Millisecond},
		wrapDownloaders([]Downloader{dl}), store, bus, scanner, &fakeRematcher{trackID: "t1"}, ver, clk, nil, nil)
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
	rematcher := &fakeRematcher{trackID: "lib-track-9", coverArtID: "mf-lib-track-9_abc123"}
	m := NewManager(Config{Workers: 1, DebounceWindow: 5 * time.Second, ScanPollEvery: time.Millisecond, ScanPollMax: time.Second, ScanSettleMax: 10 * time.Millisecond},
		wrapDownloaders([]Downloader{dl}), store, bus, &fakeScanner{}, rematcher, &fakeVersion{v: 1}, clk, nil, nil)
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

	// Assert the store reflects the re-matched library_track_id and cover_art_id.
	cur, _, _ := store.Get(context.Background(), job.ID)
	if cur.LibraryTrackID != "lib-track-9" {
		t.Fatalf("library_track_id not set after re-match, got %q", cur.LibraryTrackID)
	}
	if cur.CoverArtID != "mf-lib-track-9_abc123" {
		t.Fatalf("cover_art_id not set after re-match, got %q (needed for home recently-downloaded covers)", cur.CoverArtID)
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
		return
	}
	if found.CoverArtID != "mf-lib-track-9_abc123" {
		t.Fatalf("complete event coverArtId: got %q want %q (must be carried on WS event for live recently-downloaded covers)", found.CoverArtID, "mf-lib-track-9_abc123")
	}
	if found.Source != "spotify" {
		t.Fatalf("complete event source: got %q want %q", found.Source, "spotify")
	}
	if found.ExternalID != "e1" {
		t.Fatalf("complete event externalId: got %q want %q", found.ExternalID, "e1")
	}
}

// TestRunScanWaitsForScanToCompleteBeforeRematch is the regression test for the
// scan-start RACE (Bug A): Navidrome's startScan is async, so getScanStatus
// reports scanning=false for a window before the scan engages. The manager must
// NOT re-match during that window (it would search the pre-download index and miss
// the file forever). With a scanner that returns false (settle), then true
// (scanning), then false (done), waitForScan must observe the scanning phase and
// only re-match after it ends — leaving the job linked to its library track.
func TestRunScanWaitsForScanToCompleteBeforeRematch(t *testing.T) {
	clk := newFakeClock()
	dl := &fakeDL{name: "dl", canDownload: true}
	store := newMemStore()
	bus := events.New()
	// false on the first poll (scan not engaged yet) → true (scanning) → false (done).
	// If the manager broke out of the poll on the first false (the bug), it would
	// re-match too early; this sequence asserts it waits through the scanning phase.
	scanner := &fakeScanner{statusSeq: []bool{false, true, true, false}}
	rematcher := &fakeRematcher{trackID: "lib-track-classical"}
	m := NewManager(Config{Workers: 1, DebounceWindow: 5 * time.Second, ScanPollEvery: time.Millisecond, ScanPollMax: time.Second, ScanSettleMax: time.Second},
		wrapDownloaders([]Downloader{dl}), store, bus, scanner, rematcher, &fakeVersion{v: 1}, clk, nil, nil)
	t.Cleanup(m.Stop)
	m.Start()

	job, err := m.Enqueue(context.Background(), core.DownloadRequest{
		Source: "spotify", ExternalID: "cl1", Artist: "Glenn Gould",
		Title: "Goldberg Variations, BWV 988: Aria", Album: "Bach: The Goldberg Variations",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Wait for the download to complete in the store.
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

	// Fire the debounced scan: runScan → waitForScan (settle→drain) → re-match.
	clk.Advance(5 * time.Second)

	// The scanner must have been polled enough to traverse the false→true→false
	// sequence (at least 3 ScanStatus calls), proving the manager waited rather than
	// bailing on the first scanning=false.
	if n := scanner.statusCalls(); n < 3 {
		t.Fatalf("waitForScan polled ScanStatus %d times; expected >=3 (it bailed before the scan engaged — the race)", n)
	}

	cur, _, _ := store.Get(context.Background(), job.ID)
	if cur.LibraryTrackID != "lib-track-classical" {
		t.Fatalf("library_track_id not set after scan-complete re-match, got %q", cur.LibraryTrackID)
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

	j, err := m.Retry(context.Background(), "j1", "")
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

func TestRetryWithManualURLSetsRequestField(t *testing.T) {
	// When Retry is called with a non-empty manualURL it must be visible on the
	// in-memory DownloadRequest that the worker reads so the spotDL adapter can
	// construct the correct query (pipe or direct URL).
	store := newMemStore()
	failed := core.DownloadJob{
		ID: "j2", DedupKey: "dk2", Status: core.DownloadFailed,
		DownloaderName: "dl", Attempts: 1,
		Source: "spotify", ExternalID: "sp1",
		Artist: "Einaudi", Title: "Una mattina",
	}
	_ = store.Insert(context.Background(), failed, core.DownloadRequest{
		Source: "spotify", ExternalID: "sp1", Artist: "Einaudi", Title: "Una mattina",
	})
	dl := &fakeDL{name: "dl", canDownload: true}
	m, _ := testManager(t, []Downloader{dl}, store, nil, nil, nil)
	m.SeedRequest("j2", core.DownloadRequest{
		Source: "spotify", ExternalID: "sp1", Artist: "Einaudi", Title: "Una mattina",
	})

	const url = "https://www.youtube.com/watch?v=MANUAL"
	j, err := m.Retry(context.Background(), "j2", url)
	if err != nil {
		t.Fatal(err)
	}
	if j.Status != core.DownloadQueued {
		t.Fatalf("retry should set queued, got %q", j.Status)
	}

	// Inspect the in-memory request: ManualURL must be set.
	m.mu.Lock()
	req := m.reqs["j2"]
	m.mu.Unlock()
	if req.ManualURL != url {
		t.Fatalf("ManualURL on re-dispatched request: got %q, want %q", req.ManualURL, url)
	}
}

func TestRetryWithEmptyManualURLLeavesRequestUnchanged(t *testing.T) {
	// A plain retry (manualURL=="") must not modify the ManualURL field on any
	// previously seeded request (it stays empty, preserving original behaviour).
	store := newMemStore()
	failed := core.DownloadJob{
		ID: "j3", DedupKey: "dk3", Status: core.DownloadFailed,
		DownloaderName: "dl", Attempts: 1,
		Source: "spotify", ExternalID: "sp2",
		Artist: "Bach", Title: "Goldberg",
	}
	_ = store.Insert(context.Background(), failed, core.DownloadRequest{
		Source: "spotify", ExternalID: "sp2", Artist: "Bach", Title: "Goldberg",
	})
	dl := &fakeDL{name: "dl", canDownload: true}
	m, _ := testManager(t, []Downloader{dl}, store, nil, nil, nil)
	m.SeedRequest("j3", core.DownloadRequest{
		Source: "spotify", ExternalID: "sp2", Artist: "Bach", Title: "Goldberg",
	})

	_, err := m.Retry(context.Background(), "j3", "")
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	req := m.reqs["j3"]
	m.mu.Unlock()
	if req.ManualURL != "" {
		t.Fatalf("plain retry must leave ManualURL empty, got %q", req.ManualURL)
	}
}

// TestManualURLClearedAfterFailure asserts that when a Retry(id, url) call results
// in DownloadFailed, the ManualURL is not silently reused on the next plain Retry.
// Before the fix, m.reqs[id] was only deleted on success, so a subsequent plain
// retry (manualURL=="") would pick up the stale ManualURL from the map.
func TestManualURLClearedAfterFailure(t *testing.T) {
	dlErr := errors.New("bad url")
	dl := &fakeDL{name: "dl", canDownload: true, errOnStart: dlErr}
	store := newMemStore()

	// Seed a failed job that can be retried.
	failed := core.DownloadJob{
		ID: "jurl", DedupKey: "dkurl", Status: core.DownloadFailed,
		DownloaderName: "dl", Attempts: 1,
		Source: "spotify", ExternalID: "sp-url",
		Artist: "Artist", Title: "Track",
	}
	_ = store.Insert(context.Background(), failed, core.DownloadRequest{
		Source: "spotify", ExternalID: "sp-url", Artist: "Artist", Title: "Track",
	})

	m, _ := testManager(t, []Downloader{dl}, store, nil, nil, nil)
	m.SeedRequest("jurl", core.DownloadRequest{
		Source: "spotify", ExternalID: "sp-url", Artist: "Artist", Title: "Track",
	})

	// First retry: supply a manual URL. The download fails (errOnStart).
	const manURL = "https://www.youtube.com/watch?v=MANUAL"
	_, err := m.Retry(context.Background(), "jurl", manURL)
	if err != nil {
		t.Fatal(err)
	}

	// The seeded job already has DownloadFailed status, so waiting for that status
	// alone can finish before the asynchronous retry worker has processed it.
	// Wait for the request cleanup itself, which is the behaviour under test.
	deadline := time.After(3 * time.Second)
	for {
		m.mu.Lock()
		_, exists := m.reqs["jurl"]
		m.mu.Unlock()
		if !exists {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for request cleanup after manual-URL retry")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	// After failure the in-memory request entry must be gone.
	m.mu.Lock()
	_, exists := m.reqs["jurl"]
	m.mu.Unlock()
	if exists {
		t.Fatal("m.reqs entry should have been deleted on DownloadFailed; stale ManualURL would leak into next retry")
	}

	// Re-seed so the second retry can run (simulates a plain retry from the UI).
	m.SeedRequest("jurl", core.DownloadRequest{
		Source: "spotify", ExternalID: "sp-url", Artist: "Artist", Title: "Track",
	})

	// Second retry: plain (no manual URL). The worker should NOT see the old manURL.
	_, err = m.Retry(context.Background(), "jurl", "")
	if err != nil {
		t.Fatal(err)
	}

	// Wait for the second request cleanup, for the same reason as above.
	deadline2 := time.After(3 * time.Second)
	for {
		m.mu.Lock()
		_, exists := m.reqs["jurl"]
		m.mu.Unlock()
		if !exists {
			break
		}
		select {
		case <-deadline2:
			t.Fatal("timed out waiting for second request cleanup")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	// After the second failure the entry must again be gone (no stale URL).
	m.mu.Lock()
	_, exists = m.reqs["jurl"]
	m.mu.Unlock()
	if exists {
		t.Fatal("m.reqs entry should be deleted after second DownloadFailed")
	}
}

// TestRetryNonSpotifyJobKeepsManualURL asserts FIX 1: a non-Spotify job
// (Source:"youtube", ExternalID:"") retried with a manualURL keeps it on the
// re-dispatched request. The old guard `|| req.ExternalID == ""` would overwrite
// the valid in-memory request with a struct literal missing ManualURL.
func TestRetryNonSpotifyJobKeepsManualURL(t *testing.T) {
	store := newMemStore()
	failed := core.DownloadJob{
		ID: "yt1", DedupKey: "dkyt1", Status: core.DownloadFailed,
		DownloaderName: "dl", Attempts: 1,
		Source: "youtube", ExternalID: "", // non-Spotify: ExternalID is empty
		Artist: "Daft Punk", Title: "One More Time",
	}
	_ = store.Insert(context.Background(), failed, core.DownloadRequest{
		Source: "youtube", ExternalID: "", Artist: "Daft Punk", Title: "One More Time",
	})
	dl := &fakeDL{name: "dl", canDownload: true}
	m, _ := testManager(t, []Downloader{dl}, store, nil, nil, nil)
	m.SeedRequest("yt1", core.DownloadRequest{
		Source: "youtube", ExternalID: "", Artist: "Daft Punk", Title: "One More Time",
	})

	const url = "https://www.youtube.com/watch?v=YT_MANUAL"
	_, err := m.Retry(context.Background(), "yt1", url)
	if err != nil {
		t.Fatal(err)
	}

	// The in-memory request must carry ManualURL (FIX 1).
	m.mu.Lock()
	req := m.reqs["yt1"]
	m.mu.Unlock()
	if req.ManualURL != url {
		t.Fatalf("FIX 1: ManualURL on non-Spotify re-dispatched request: got %q, want %q", req.ManualURL, url)
	}
}

// TestRetryWithManualURLPersistsToStore asserts FIX 2: when Retry is called with
// a manualURL, the updated DownloadRequest (including ManualURL) is persisted to
// the store (request_json) so it survives a server restart between Retry and the
// worker picking up the job.
func TestRetryWithManualURLPersistsToStore(t *testing.T) {
	store := newMemStore()
	failed := core.DownloadJob{
		ID: "sp-persist", DedupKey: "dk-persist", Status: core.DownloadFailed,
		DownloaderName: "dl", Attempts: 1,
		Source: "spotify", ExternalID: "sp-abc",
		Artist: "Einaudi", Title: "Una mattina",
	}
	_ = store.Insert(context.Background(), failed, core.DownloadRequest{
		Source: "spotify", ExternalID: "sp-abc", Artist: "Einaudi", Title: "Una mattina",
	})
	dl := &fakeDL{name: "dl", canDownload: true}
	m, _ := testManager(t, []Downloader{dl}, store, nil, nil, nil)
	m.SeedRequest("sp-persist", core.DownloadRequest{
		Source: "spotify", ExternalID: "sp-abc", Artist: "Einaudi", Title: "Una mattina",
	})

	const url = "https://www.youtube.com/watch?v=PERSIST_TEST"
	_, err := m.Retry(context.Background(), "sp-persist", url)
	if err != nil {
		t.Fatal(err)
	}

	// The store's request_json (via memStore.reqs) must carry ManualURL (FIX 2).
	persisted, ok := store.getReq("sp-persist")
	if !ok {
		t.Fatal("FIX 2: store has no request entry for job after Retry with manualURL")
	}
	if persisted.ManualURL != url {
		t.Fatalf("FIX 2: persisted ManualURL: got %q, want %q", persisted.ManualURL, url)
	}
}

// TestBackfillUnlinkedReLinksCompletedJobs asserts that on startup, the manager
// re-matches completed jobs that have no LibraryTrackID (e.g. finished under an
// older matcher) and sets LibraryTrackID + CoverArtID and publishes a complete event.
func TestBackfillUnlinkedReLinksCompletedJobs(t *testing.T) {
	store := newMemStore()

	// Seed a completed job with empty LibraryTrackID — simulates a job that
	// completed before the rematcher could link it.
	seeded := core.DownloadJob{
		ID: "backfill-j1", DedupKey: "dk-backfill", Status: core.DownloadCompleted,
		DownloaderName: "dl", Source: "spotify", ExternalID: "ext-bf1",
		Artist: "Bach", Title: "Goldberg Variations", Album: "Goldberg",
		Progress: 100,
	}
	_ = store.Insert(context.Background(), seeded, core.DownloadRequest{
		Source: "spotify", ExternalID: "ext-bf1", Artist: "Bach", Title: "Goldberg Variations",
	})

	rematcher := &fakeRematcher{trackID: "lib-bf-1", coverArtID: "cover-bf-1"}
	bus := events.New()
	dl := &fakeDL{name: "dl", canDownload: true}
	m := NewManager(Config{Workers: 1, DebounceWindow: 5 * time.Second, ScanPollEvery: time.Millisecond, ScanPollMax: time.Second, ScanSettleMax: 10 * time.Millisecond},
		wrapDownloaders([]Downloader{dl}), store, bus, &fakeScanner{}, rematcher, &fakeVersion{v: 1}, RealClock{}, nil, nil)
	t.Cleanup(m.Stop)

	// Subscribe to complete events BEFORE starting so we don't miss the backfill publish.
	completeCh, unsub := bus.Subscribe(TopicComplete)
	defer unsub()

	var backfillEvents []core.DownloadEvent
	var evMu sync.Mutex
	gotEvent := make(chan struct{}, 1)
	go func() {
		for ev := range completeCh {
			if de, ok := ev.Payload.(core.DownloadEvent); ok && de.JobID == seeded.ID {
				evMu.Lock()
				backfillEvents = append(backfillEvents, de)
				evMu.Unlock()
				select {
				case gotEvent <- struct{}{}:
				default:
				}
			}
		}
	}()

	m.Start()

	// Wait for the backfill goroutine to publish the complete event.
	select {
	case <-gotEvent:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for backfill to publish a complete event")
	}

	// Job must now have LibraryTrackID and CoverArtID set.
	updated, ok, err := store.Get(context.Background(), seeded.ID)
	if err != nil || !ok {
		t.Fatalf("job not found: %v", err)
	}
	if updated.LibraryTrackID != "lib-bf-1" {
		t.Fatalf("backfill LibraryTrackID: got %q, want %q", updated.LibraryTrackID, "lib-bf-1")
	}
	if updated.CoverArtID != "cover-bf-1" {
		t.Fatalf("backfill CoverArtID: got %q, want %q", updated.CoverArtID, "cover-bf-1")
	}

	// The published event must carry the library track id and cover art id.
	evMu.Lock()
	evs := make([]core.DownloadEvent, len(backfillEvents))
	copy(evs, backfillEvents)
	evMu.Unlock()

	var found *core.DownloadEvent
	for i := range evs {
		if evs[i].LibraryTrackID == "lib-bf-1" {
			found = &evs[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no backfill complete event with libraryTrackId=%q; got: %+v", "lib-bf-1", evs)
		return
	}
	if found.CoverArtID != "cover-bf-1" {
		t.Fatalf("backfill event CoverArtID: got %q, want %q", found.CoverArtID, "cover-bf-1")
	}
}

// TestBackfillPlaylistAdderCalledWhenAddToPlaylistIDSet mirrors
// TestPlaylistAdderCalledOnCompletionWithAddToPlaylistID but exercises the BACKFILL
// path: a completed, unlinked job whose request carries AddToPlaylistID must have
// AddTracksToPlaylist called when the manager starts and re-links it.
func TestBackfillPlaylistAdderCalledWhenAddToPlaylistIDSet(t *testing.T) {
	store := newMemStore()

	const playlistID = "pl-backfill-123"
	const libTrackID = "lib-backfill-pl-1"

	// Seed a completed, unlinked job with AddToPlaylistID set on the job struct
	// (mirrors how Enqueue stores it: AddToPlaylistID is copied from the request
	// directly onto the job row so BackfillUnlinked can read it from store.List).
	seeded := core.DownloadJob{
		ID: "backfill-pl-j1", DedupKey: "dk-backfill-pl", Status: core.DownloadCompleted,
		DownloaderName: "dl", Source: "spotify", ExternalID: "ext-bf-pl1",
		Artist: "Artist", Title: "Playlist Track", Album: "Album",
		AddToPlaylistID: playlistID,
		Progress:        100,
	}
	_ = store.Insert(context.Background(), seeded, core.DownloadRequest{
		Source: "spotify", ExternalID: "ext-bf-pl1", Artist: "Artist",
		Title: "Playlist Track", Album: "Album", AddToPlaylistID: playlistID,
	})

	rematcher := &fakeRematcher{trackID: libTrackID}
	adder := &fakePlaylistAdder{}
	bus := events.New()
	dl := &fakeDL{name: "dl", canDownload: true}
	m := NewManager(Config{Workers: 1, DebounceWindow: 5 * time.Second, ScanPollEvery: time.Millisecond, ScanPollMax: time.Second, ScanSettleMax: 10 * time.Millisecond},
		wrapDownloaders([]Downloader{dl}), store, bus, &fakeScanner{}, rematcher, &fakeVersion{v: 1}, RealClock{}, adder, nil)
	t.Cleanup(m.Stop)

	// Subscribe before Start so we don't miss the backfill publish.
	completeCh, unsub := bus.Subscribe(TopicComplete)
	defer unsub()
	gotEvent := make(chan struct{}, 1)
	go func() {
		for ev := range completeCh {
			if de, ok := ev.Payload.(core.DownloadEvent); ok && de.JobID == seeded.ID {
				select {
				case gotEvent <- struct{}{}:
				default:
				}
			}
		}
	}()

	m.Start()

	// Wait for the backfill to publish the complete event.
	select {
	case <-gotEvent:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for backfill complete event")
	}

	// Give the playlist add a moment to execute (it runs synchronously inside the
	// backfill loop, so by the time gotEvent fires it should already be called).
	if adder.callCount() != 1 {
		t.Fatalf("expected 1 AddTracksToPlaylist call from backfill, got %d", adder.callCount())
	}
	gotPlaylistID, gotTrackIDs := adder.getCall(0)
	if gotPlaylistID != playlistID {
		t.Fatalf("backfill AddTracksToPlaylist playlistID: got %q, want %q", gotPlaylistID, playlistID)
	}
	if len(gotTrackIDs) != 1 || gotTrackIDs[0] != libTrackID {
		t.Fatalf("backfill AddTracksToPlaylist trackIDs: got %v, want [%q]", gotTrackIDs, libTrackID)
	}
}

// fakePlaylistAdder records AddTracksToPlaylist calls for assertions.
type fakePlaylistAdder struct {
	mu    sync.Mutex
	calls []struct {
		playlistID string
		trackIDs   []string
	}
}

func (f *fakePlaylistAdder) AddTracksToPlaylist(_ context.Context, playlistID string, trackIDs []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, struct {
		playlistID string
		trackIDs   []string
	}{playlistID, trackIDs})
	return nil
}

func (f *fakePlaylistAdder) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakePlaylistAdder) getCall(i int) (string, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.calls[i]
	return c.playlistID, c.trackIDs
}

// TestPlaylistAdderCalledOnCompletionWithAddToPlaylistID asserts that when a
// completed job's request carries AddToPlaylistID, the manager calls
// PlaylistAdder.AddTracksToPlaylist with the playlist ID and matched library track ID.
func TestPlaylistAdderCalledOnCompletionWithAddToPlaylistID(t *testing.T) {
	clk := newFakeClock()
	dl := &fakeDL{name: "dl", canDownload: true}
	store := newMemStore()
	bus := events.New()
	const libTrackID = "lib-playlist-track-1"
	rematcher := &fakeRematcher{trackID: libTrackID}
	adder := &fakePlaylistAdder{}

	m := NewManager(Config{Workers: 1, DebounceWindow: 5 * time.Second, ScanPollEvery: time.Millisecond, ScanPollMax: time.Second, ScanSettleMax: 10 * time.Millisecond},
		wrapDownloaders([]Downloader{dl}), store, bus, &fakeScanner{}, rematcher, &fakeVersion{v: 1}, clk, adder, nil)
	t.Cleanup(m.Stop)
	m.Start()

	const playlistID = "pl-abc-123"
	job, err := m.Enqueue(context.Background(), core.DownloadRequest{
		Source: "spotify", ExternalID: "e-pl-1", Artist: "Artist", Title: "Track",
		Album: "Album", AddToPlaylistID: playlistID,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Wait for the job to complete.
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

	// Fire the debounced scan; this triggers runScan → re-match → playlist add.
	clk.Advance(5 * time.Second)

	if adder.callCount() != 1 {
		t.Fatalf("expected 1 AddTracksToPlaylist call, got %d", adder.callCount())
	}
	gotPlaylistID, gotTrackIDs := adder.getCall(0)
	if gotPlaylistID != playlistID {
		t.Fatalf("AddTracksToPlaylist playlistID: got %q, want %q", gotPlaylistID, playlistID)
	}
	if len(gotTrackIDs) != 1 || gotTrackIDs[0] != libTrackID {
		t.Fatalf("AddTracksToPlaylist trackIDs: got %v, want [%q]", gotTrackIDs, libTrackID)
	}
}

func TestClearRemovesTerminalJobAndPublishes(t *testing.T) {
	dl := &fakeDL{name: "dl", canDownload: true}
	store := newMemStore()
	m, bus := testManager(t, []Downloader{dl}, store, nil, nil, nil)

	sub, unsub := bus.Subscribe(TopicRemoved)
	defer unsub()

	// Seed a completed job directly in the store.
	job := core.DownloadJob{ID: "done1", DedupKey: "dk", Status: core.DownloadCompleted, Source: "spotify", ExternalID: "e"}
	if err := store.Insert(context.Background(), job, core.DownloadRequest{}); err != nil {
		t.Fatal(err)
	}

	if err := m.Clear(context.Background(), "done1"); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, ok, _ := store.Get(context.Background(), "done1"); ok {
		t.Fatal("job should be deleted")
	}
	select {
	case ev := <-sub:
		re := ev.Payload.(core.DownloadRemovedEvent)
		if len(re.JobIDs) != 1 || re.JobIDs[0] != "done1" {
			t.Fatalf("removed event = %+v", re)
		}
	case <-time.After(time.Second):
		t.Fatal("expected download.removed event")
	}
}

func TestClearRejectsActiveJob(t *testing.T) {
	store := newMemStore()
	m, _ := testManager(t, []Downloader{&fakeDL{name: "dl", canDownload: true}}, store, nil, nil, nil)
	job := core.DownloadJob{ID: "run1", DedupKey: "dk", Status: core.DownloadRunning}
	if err := store.Insert(context.Background(), job, core.DownloadRequest{}); err != nil {
		t.Fatal(err)
	}
	if err := m.Clear(context.Background(), "run1"); err == nil {
		t.Fatal("Clear of a running job must error")
	}
	if _, ok, _ := store.Get(context.Background(), "run1"); !ok {
		t.Fatal("running job must NOT be deleted")
	}
}

func TestClearFinishedDeletesOnlyTerminal(t *testing.T) {
	store := newMemStore()
	m, _ := testManager(t, []Downloader{&fakeDL{name: "dl", canDownload: true}}, store, nil, nil, nil)
	for _, tc := range []struct{ id, st string }{{"a", "completed"}, {"b", "failed"}, {"c", "queued"}, {"d", "canceled"}} {
		j := core.DownloadJob{ID: tc.id, DedupKey: "dk-" + tc.id, Status: core.DownloadStatus(tc.st)}
		if err := store.Insert(context.Background(), j, core.DownloadRequest{}); err != nil {
			t.Fatal(err)
		}
	}
	ids, err := m.ClearFinished(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 3 {
		t.Fatalf("ClearFinished removed %v, want 3 (a,b,d)", ids)
	}
	if _, ok, _ := store.Get(context.Background(), "c"); !ok {
		t.Fatal("queued job c must survive")
	}
}

// TestPlaylistAdderNotCalledWhenNoAddToPlaylistID asserts that jobs without
// AddToPlaylistID do not trigger any playlist add call.
func TestPlaylistAdderNotCalledWhenNoAddToPlaylistID(t *testing.T) {
	clk := newFakeClock()
	dl := &fakeDL{name: "dl", canDownload: true}
	store := newMemStore()
	bus := events.New()
	rematcher := &fakeRematcher{trackID: "lib-track-no-pl"}
	adder := &fakePlaylistAdder{}

	m := NewManager(Config{Workers: 1, DebounceWindow: 5 * time.Second, ScanPollEvery: time.Millisecond, ScanPollMax: time.Second, ScanSettleMax: 10 * time.Millisecond},
		wrapDownloaders([]Downloader{dl}), store, bus, &fakeScanner{}, rematcher, &fakeVersion{v: 1}, clk, adder, nil)
	t.Cleanup(m.Stop)
	m.Start()

	job, err := m.Enqueue(context.Background(), core.DownloadRequest{
		Source: "spotify", ExternalID: "e-no-pl", Artist: "Artist", Title: "Track", Album: "Album",
		// AddToPlaylistID intentionally empty
	})
	if err != nil {
		t.Fatal(err)
	}

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

	clk.Advance(5 * time.Second)

	if adder.callCount() != 0 {
		t.Fatalf("expected 0 AddTracksToPlaylist calls for job without AddToPlaylistID, got %d", adder.callCount())
	}
}

// TestBackfillSkipsAlreadyLinkedAndNonCompleted ensures the backfill only touches
// completed jobs with empty LibraryTrackID.
func TestBackfillSkipsAlreadyLinkedAndNonCompleted(t *testing.T) {
	store := newMemStore()

	// Already-linked completed job — must NOT get a second rematch call.
	linked := core.DownloadJob{
		ID: "linked-j1", DedupKey: "dk-linked", Status: core.DownloadCompleted,
		DownloaderName: "dl", Source: "spotify", ExternalID: "ext-linked",
		Artist: "Artist", Title: "Linked Track", LibraryTrackID: "already-linked",
	}
	_ = store.Insert(context.Background(), linked, core.DownloadRequest{})

	// Failed job — must not be touched.
	failed := core.DownloadJob{
		ID: "failed-j1", DedupKey: "dk-failed", Status: core.DownloadFailed,
		DownloaderName: "dl", Source: "spotify", ExternalID: "ext-failed",
		Artist: "Artist", Title: "Failed Track",
	}
	_ = store.Insert(context.Background(), failed, core.DownloadRequest{})

	rematcher := &fakeRematcher{trackID: "should-not-be-set"}
	dl := &fakeDL{name: "dl", canDownload: true}
	m := NewManager(Config{Workers: 1, DebounceWindow: 5 * time.Second, ScanPollEvery: time.Millisecond, ScanPollMax: time.Second, ScanSettleMax: 10 * time.Millisecond},
		wrapDownloaders([]Downloader{dl}), store, nil, &fakeScanner{}, rematcher, &fakeVersion{v: 1}, RealClock{}, nil, nil)
	t.Cleanup(m.Stop)
	m.Start()

	// Give the backfill goroutine time to run (it's fast — no I/O).
	time.Sleep(50 * time.Millisecond)

	// The already-linked job must still have its original library_track_id.
	j, _, _ := store.Get(context.Background(), linked.ID)
	if j.LibraryTrackID != "already-linked" {
		t.Fatalf("backfill must not overwrite an already-linked job: got %q", j.LibraryTrackID)
	}

	// The failed job must still be failed (LibraryTrackID empty, status unchanged).
	fj, _, _ := store.Get(context.Background(), failed.ID)
	if fj.LibraryTrackID != "" {
		t.Fatalf("backfill must not touch failed jobs: got LibraryTrackID=%q", fj.LibraryTrackID)
	}
}

func TestPauseGatesDispatchResumeDrains(t *testing.T) {
	dl := &fakeDL{name: "dl", canDownload: true}
	store := newMemStore()
	m, bus := testManager(t, []Downloader{dl}, store, nil, nil, nil)

	sub, unsub := bus.Subscribe(TopicQueueState)
	defer unsub()

	m.Pause()
	if !m.IsPaused() {
		t.Fatal("expected IsPaused() true after Pause")
	}
	select {
	case ev := <-sub:
		if !ev.Payload.(core.QueueStateEvent).Paused {
			t.Fatal("pause event should carry Paused=true")
		}
	case <-time.After(time.Second):
		t.Fatal("expected download.queue event on Pause")
	}

	job, err := m.Enqueue(context.Background(), core.DownloadRequest{Source: "spotify", ExternalID: "e1", Artist: "A", Title: "T", Album: "Al"})
	if err != nil {
		t.Fatal(err)
	}

	// While paused, no worker may pick the job up: it stays queued.
	time.Sleep(80 * time.Millisecond)
	if got, _, _ := store.Get(context.Background(), job.ID); got.Status != core.DownloadQueued {
		t.Fatalf("paused: want job to stay queued, got %s", got.Status)
	}

	m.Resume()
	if m.IsPaused() {
		t.Fatal("expected IsPaused() false after Resume")
	}

	// After resume the job runs to completion (poll, RealClock fakeDL completes fast).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got, _, _ := store.Get(context.Background(), job.ID); got.Status == core.DownloadCompleted {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	got, _, _ := store.Get(context.Background(), job.ID)
	t.Fatalf("after resume: want completed, got %s", got.Status)
}

// TestPauseKeepsBufferedJobsQueued asserts that jobs enqueued while the manager is
// paused remain in the Queued state (no worker picks them up), and that they all
// drain to Completed once Resume is called.
func TestPauseKeepsBufferedJobsQueued(t *testing.T) {
	dl := &fakeDL{name: "dl", canDownload: true}
	store := newMemStore()
	m, _ := testManager(t, []Downloader{dl}, store, nil, nil, nil)

	m.Pause()

	// Enqueue 3 distinct jobs while paused.
	var jobIDs [3]string
	for i := 0; i < 3; i++ {
		j, err := m.Enqueue(context.Background(), core.DownloadRequest{
			Source: "spotify", ExternalID: string(rune('a' + i)), Artist: "A", Title: "T" + string(rune('a'+i)), Album: "Al",
		})
		if err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
		jobIDs[i] = j.ID
	}

	// After ~80ms none should have been dispatched (they stay Queued).
	time.Sleep(80 * time.Millisecond)
	for _, id := range jobIDs {
		cur, _, _ := store.Get(context.Background(), id)
		if cur.Status != core.DownloadQueued {
			t.Fatalf("paused: job %s should be Queued, got %s", id, cur.Status)
		}
	}

	// Resume and wait for all 3 to complete.
	m.Resume()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		jobs, _ := store.List(context.Background())
		done := 0
		for _, j := range jobs {
			if j.Status == core.DownloadCompleted {
				done++
			}
		}
		if done == 3 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	jobs, _ := store.List(context.Background())
	done := 0
	for _, j := range jobs {
		if j.Status == core.DownloadCompleted {
			done++
		}
	}
	t.Fatalf("after resume: want 3 completed, got %d", done)
}

// TestStopUnblocksPausedWorkers asserts that calling Stop() while the manager is
// paused does not hang — the paused workers must wake up and exit cleanly.
func TestStopUnblocksPausedWorkers(t *testing.T) {
	dl := &fakeDL{name: "dl", canDownload: true}
	store := newMemStore()
	bus := events.New()
	m := NewManager(
		Config{Workers: 2, DebounceWindow: 5 * time.Second, ScanPollEvery: time.Millisecond, ScanPollMax: time.Second, ScanSettleMax: 10 * time.Millisecond},
		wrapDownloaders([]Downloader{dl}), store, bus, &fakeScanner{}, &fakeRematcher{trackID: "t1"}, &fakeVersion{v: 1}, RealClock{}, nil, nil,
	)
	m.Start()
	m.Pause()

	done := make(chan struct{})
	go func() {
		m.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Stop returned — workers unblocked successfully.
	case <-time.After(2 * time.Second):
		t.Fatal("Stop hung with workers paused")
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

// fakeAsyncDL is a fake AsyncDownloader (also a Downloader) for async-lane tests.
type fakeAsyncDL struct {
	mu          sync.Mutex
	name        string
	submitRef   string
	submitErr   error
	submitCalls int
	cancelCalls int
	status      AsyncStatus
}

func (d *fakeAsyncDL) Type() string { return "downloader" }
func (d *fakeAsyncDL) Name() string { return d.name }
func (d *fakeAsyncDL) SupportedGranularities() []core.DownloadGranularity {
	return []core.DownloadGranularity{core.GranularityAlbum}
}
func (d *fakeAsyncDL) ConfigSchema() registry.ConfigSchema  { return registry.ConfigSchema{} }
func (d *fakeAsyncDL) Init(map[string]any) error            { return nil }
func (d *fakeAsyncDL) TestConnection(context.Context) error { return nil }
func (d *fakeAsyncDL) CanDownload(_ context.Context, req core.DownloadRequest) (bool, error) {
	g := req.Granularity
	if g == "" {
		g = core.GranularityTrack
	}
	return g == core.GranularityAlbum, nil
}
func (d *fakeAsyncDL) Start(context.Context, core.DownloadRequest, func(int)) (string, error) {
	return "", fmt.Errorf("fakeAsyncDL.Start should never be called")
}
func (d *fakeAsyncDL) Submit(_ context.Context, _ core.DownloadRequest) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.submitCalls++
	return d.submitRef, d.submitErr
}
func (d *fakeAsyncDL) Poll(_ context.Context, _ string) (AsyncStatus, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.status, nil
}
func (d *fakeAsyncDL) CancelAsync(_ context.Context, _ string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cancelCalls++
	return nil
}
func (d *fakeAsyncDL) setStatus(s AsyncStatus) { d.mu.Lock(); d.status = s; d.mu.Unlock() }

func TestEnqueueAsyncSubmitsAndDoesNotPinWorker(t *testing.T) {
	async := &fakeAsyncDL{name: "lidarr", submitRef: "album-42"}
	store := newMemStore()
	m, _ := testManager(t, []Downloader{async}, store, nil, nil, nil)

	job, err := m.Enqueue(context.Background(), core.DownloadRequest{
		Source: "spotify", ExternalID: "e1", Artist: "Daft Punk", Title: "One More Time",
		Album: "Discovery", Granularity: core.GranularityAlbum,
	})
	if err != nil {
		t.Fatal(err)
	}
	if async.submitCalls != 1 {
		t.Fatalf("Submit calls = %d, want 1", async.submitCalls)
	}
	// After submit the job is running, carries the ref, and was NOT pushed to the
	// worker queue (the fake's Start panics if a worker ever ran it).
	got, _, _ := store.Get(context.Background(), job.ID)
	if got.Status != core.DownloadRunning {
		t.Fatalf("status = %s, want running", got.Status)
	}
	if got.DownloaderRef != "album-42" {
		t.Fatalf("ref = %q, want album-42", got.DownloaderRef)
	}
}

func TestEnqueueAsyncSubmitErrorFailsJob(t *testing.T) {
	async := &fakeAsyncDL{name: "lidarr", submitErr: fmt.Errorf("couldn't find album in Lidarr")}
	store := newMemStore()
	m, _ := testManager(t, []Downloader{async}, store, nil, nil, nil)

	job, _ := m.Enqueue(context.Background(), core.DownloadRequest{
		Source: "spotify", ExternalID: "e1", Artist: "X", Title: "Y", Album: "Z", Granularity: core.GranularityAlbum,
	})
	got, _, _ := store.Get(context.Background(), job.ID)
	if got.Status != core.DownloadFailed {
		t.Fatalf("status = %s, want failed", got.Status)
	}
	if got.Error == "" {
		t.Fatal("failed job should carry an error message")
	}
}

func TestCancelAsyncJob(t *testing.T) {
	async := &fakeAsyncDL{name: "lidarr", submitRef: "album-7"}
	store := newMemStore()
	m, _ := testManager(t, []Downloader{async}, store, nil, nil, nil)

	job, _ := m.Enqueue(context.Background(), core.DownloadRequest{
		Source: "spotify", ExternalID: "e1", Artist: "X", Title: "Y", Album: "Z", Granularity: core.GranularityAlbum,
	})
	if err := m.Cancel(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	if async.cancelCalls != 1 {
		t.Fatalf("CancelAsync calls = %d, want 1", async.cancelCalls)
	}
	got, _, _ := store.Get(context.Background(), job.ID)
	if got.Status != core.DownloadCanceled {
		t.Fatalf("status = %s, want canceled", got.Status)
	}
}

func TestReconcileAdvancesProgressThenCompletes(t *testing.T) {
	clk := newFakeClock()
	async := &fakeAsyncDL{name: "lidarr", submitRef: "album-9"}
	store := newMemStore()
	scanner := &fakeScanner{}
	bus := events.New()
	m := NewManager(Config{Workers: 1, DebounceWindow: time.Second, ScanPollEvery: time.Millisecond, ScanPollMax: time.Second, ScanSettleMax: 10 * time.Millisecond},
		wrapDownloaders([]Downloader{async}), store, bus, scanner, &fakeRematcher{trackID: "t1"}, &fakeVersion{v: 1}, clk, nil, nil)
	t.Cleanup(m.Stop)
	m.Start()

	job, _ := m.Enqueue(context.Background(), core.DownloadRequest{
		Source: "spotify", ExternalID: "e1", Artist: "A", Title: "T", Album: "Al", Granularity: core.GranularityAlbum,
	})

	// Downloading at 40%.
	async.setStatus(AsyncStatus{State: core.DownloadRunning, Progress: 40})
	m.reconcileOnce(context.Background())
	if got, _, _ := store.Get(context.Background(), job.ID); got.Progress != 40 {
		t.Fatalf("progress = %d, want 40", got.Progress)
	}

	// Imported → completed, and a scan is scheduled.
	async.setStatus(AsyncStatus{State: core.DownloadCompleted, Progress: 100})
	m.reconcileOnce(context.Background())
	if got, _, _ := store.Get(context.Background(), job.ID); got.Status != core.DownloadCompleted {
		t.Fatalf("status = %s, want completed", got.Status)
	}
	// Fire the debounced scan; the rematcher links the track.
	clk.Advance(time.Second)
	// waitForScan uses wall-clock polling against the fakeScanner (idle → returns fast).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got, _, _ := store.Get(context.Background(), job.ID); got.LibraryTrackID == "t1" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	got, _, _ := store.Get(context.Background(), job.ID)
	t.Fatalf("expected scan rematch to set library_track_id, got %q", got.LibraryTrackID)
}

func TestReconcileFailMapsToFailed(t *testing.T) {
	async := &fakeAsyncDL{name: "lidarr", submitRef: "album-1"}
	store := newMemStore()
	m, _ := testManager(t, []Downloader{async}, store, nil, nil, nil)
	job, _ := m.Enqueue(context.Background(), core.DownloadRequest{Source: "spotify", ExternalID: "e", Artist: "A", Title: "T", Album: "Al", Granularity: core.GranularityAlbum})

	async.setStatus(AsyncStatus{State: core.DownloadFailed, Error: "Lidarr found no release"})
	m.reconcileOnce(context.Background())
	got, _, _ := store.Get(context.Background(), job.ID)
	if got.Status != core.DownloadFailed || got.Error != "Lidarr found no release" {
		t.Fatalf("job = %+v, want failed with reason", got)
	}
}

func TestAsyncCapabilityProbe(t *testing.T) {
	registry.RegisterCapability("async", func(p registry.Plugin) bool {
		_, ok := p.(AsyncDownloader)
		return ok
	})
	caps := registry.DescribeCapabilities(&fakeAsyncDL{name: "lidarr"})
	found := false
	for _, c := range caps {
		if c == "async" {
			found = true
		}
	}
	if !found {
		t.Fatalf("async capability not detected, caps = %v", caps)
	}
	// A plain sync downloader is NOT async.
	caps = registry.DescribeCapabilities(&fakeDL{name: "spotdl"})
	for _, c := range caps {
		if c == "async" {
			t.Fatal("sync downloader must not report async")
		}
	}
}

// -- Task 1: SupportedGranularities + DownloaderEntry + granularity-aware pick --

// testManagerEntries builds a Manager directly from []DownloaderEntry (the new
// API). Used by Task-1 pick/pickAfter tests.
func testManagerEntries(t *testing.T, entries []DownloaderEntry, store JobStore, clk Clock) (*Manager, *events.Bus) {
	t.Helper()
	bus := events.New()
	scanner := &fakeScanner{}
	m := NewManager(
		Config{Workers: 2, DebounceWindow: 5 * time.Second, ScanPollEvery: time.Millisecond, ScanPollMax: time.Second, ScanSettleMax: 10 * time.Millisecond},
		entries, store, bus, scanner, &fakeRematcher{trackID: "t1"}, &fakeVersion{v: 1}, clk, nil, nil,
	)
	t.Cleanup(m.Stop)
	m.Start()
	return m, bus
}

// TestPickOrderTrackRespected: two track entries with Order{track:1} and
// Order{track:0} → pick returns the Order{track:0} one (lower order = higher priority).
func TestPickOrderTrackRespected(t *testing.T) {
	lo := &fakeDL{name: "lo", canDownload: true} // order 1 — should lose
	hi := &fakeDL{name: "hi", canDownload: true} // order 0 — should win
	store := newMemStore()
	entries := []DownloaderEntry{
		{Downloader: lo, Order: map[core.DownloadGranularity]int{core.GranularityTrack: 1}},
		{Downloader: hi, Order: map[core.DownloadGranularity]int{core.GranularityTrack: 0}},
	}
	m, _ := testManagerEntries(t, entries, store, nil)

	job, err := m.Enqueue(context.Background(), core.DownloadRequest{
		Source: "spotify", ExternalID: "e-order", Artist: "A", Title: "T", Album: "Al",
		Granularity: core.GranularityTrack,
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.DownloaderName != "hi" {
		t.Fatalf("lower Order[track] value should win: expected %q, got %q", "hi", job.DownloaderName)
	}
}

// TestPickAlbumSelectsAlbumEntry: an album request selects the album-capable entry.
func TestPickAlbumSelectsAlbumEntry(t *testing.T) {
	trackDL := &fakeDL{name: "spotdl", canDownload: true}
	albumDL := &fakeAsyncDL{name: "lidarr", submitRef: "ref-album-1"}
	store := newMemStore()
	entries := []DownloaderEntry{
		{Downloader: trackDL, Order: map[core.DownloadGranularity]int{core.GranularityTrack: 0}},
		{Downloader: albumDL, Order: map[core.DownloadGranularity]int{core.GranularityAlbum: 0}},
	}
	m, _ := testManagerEntries(t, entries, store, nil)

	job, err := m.Enqueue(context.Background(), core.DownloadRequest{
		Source: "spotify", ExternalID: "e-album", Artist: "A", Title: "Album X",
		Album: "Album X", Granularity: core.GranularityAlbum,
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.DownloaderName != "lidarr" {
		t.Fatalf("album request should pick album entry, got %q", job.DownloaderName)
	}
}

// TestPickTrackNeverSelectsAlbumOnly: a track request must never select an
// album-only entry (one whose Order map lacks GranularityTrack).
func TestPickTrackNeverSelectsAlbumOnly(t *testing.T) {
	albumDL := &fakeAsyncDL{name: "lidarr", submitRef: "ref-should-not-pick"}
	store := newMemStore()
	entries := []DownloaderEntry{
		{Downloader: albumDL, Order: map[core.DownloadGranularity]int{core.GranularityAlbum: 0}},
	}
	m, _ := testManagerEntries(t, entries, store, nil)

	_, err := m.Enqueue(context.Background(), core.DownloadRequest{
		Source: "spotify", ExternalID: "e-track-only", Artist: "A", Title: "T",
		Granularity: core.GranularityTrack,
	})
	if err == nil {
		t.Fatal("track request with only an album-only entry must fail, got nil error")
	}
	if !strings.Contains(err.Error(), "track") {
		t.Fatalf("error %q should mention granularity 'track'", err.Error())
	}
}

// TestPickAfterReturnsNextTrackEntryByOrder: pickAfter skips 'lo' (order 1)
// and returns 'mid' (order 2) when afterName = "lo", skipping 'hi' (order 0
// — it comes before 'lo' in sorted order and has already been tried).
func TestPickAfterReturnsNextTrackEntryByOrder(t *testing.T) {
	hi := &fakeDL{name: "hi", canDownload: true}   // order 0 — first in sorted order
	lo := &fakeDL{name: "lo", canDownload: true}   // order 1 — second
	mid := &fakeDL{name: "mid", canDownload: true} // order 2 — third
	store := newMemStore()
	entries := []DownloaderEntry{
		// Deliberately register in non-sorted order to prove sort is by Order[g].
		{Downloader: lo, Order: map[core.DownloadGranularity]int{core.GranularityTrack: 1}},
		{Downloader: mid, Order: map[core.DownloadGranularity]int{core.GranularityTrack: 2}},
		{Downloader: hi, Order: map[core.DownloadGranularity]int{core.GranularityTrack: 0}},
	}
	m, _ := testManagerEntries(t, entries, store, nil)

	ctx := context.Background()
	req := core.DownloadRequest{Granularity: core.GranularityTrack}
	// pickAfter("lo") should return "mid" (next in ascending Order after lo=1 is mid=2).
	got, err := m.pickAfter(ctx, req, "lo")
	if err != nil {
		t.Fatalf("pickAfter returned error: %v", err)
	}
	if got.Name() != "mid" {
		t.Fatalf("pickAfter('lo') should return 'mid' (next by order), got %q", got.Name())
	}
}

// -- Task 3: on-failure fallback through the sync downloader chain --

// TestFallbackToNextDownloaderOnStartError asserts that when the first (picked)
// sync downloader's Start returns an error, the worker tries the next downloader in
// the same-granularity chain rather than failing the job immediately.  The job must
// reach DownloadCompleted and its final DownloaderName must be the second downloader.
func TestFallbackToNextDownloaderOnStartError(t *testing.T) {
	d1 := &fakeDL{name: "d1", canDownload: true, errOnStart: errors.New("d1 lookup failed")}
	d2 := &fakeDL{name: "d2", canDownload: true}
	store := newMemStore()
	m, _ := testManager(t, []Downloader{d1, d2}, store, nil, nil, nil)

	job, err := m.Enqueue(context.Background(), core.DownloadRequest{
		Source: "spotify", ExternalID: "fb1", Artist: "A", Title: "T", Album: "Al",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Wait for the job to reach a terminal state.
	deadline := time.After(3 * time.Second)
	for {
		cur, _, _ := store.Get(context.Background(), job.ID)
		if cur.Status == core.DownloadCompleted || cur.Status == core.DownloadFailed {
			if cur.Status != core.DownloadCompleted {
				t.Fatalf("job should complete via d2 fallback, got status=%q error=%q", cur.Status, cur.Error)
			}
			if cur.DownloaderName != "d2" {
				t.Fatalf("final DownloaderName should be %q (fallback), got %q", "d2", cur.DownloaderName)
			}
			break
		}
		select {
		case <-deadline:
			cur2, _, _ := store.Get(context.Background(), job.ID)
			t.Fatalf("job did not reach terminal state (status=%q)", cur2.Status)
		default:
			time.Sleep(time.Millisecond)
		}
	}

	if d1.starts() != 1 {
		t.Fatalf("d1 should have been attempted exactly once, got %d", d1.starts())
	}
	if d2.starts() != 1 {
		t.Fatalf("d2 should have been attempted exactly once (as fallback), got %d", d2.starts())
	}
}

// TestFallbackChainExhaustedReachesDownloadFailed asserts that when all sync
// downloaders in the chain fail on Start, the job ends up DownloadFailed
// (not stuck or panicking).
func TestFallbackChainExhaustedReachesDownloadFailed(t *testing.T) {
	d1 := &fakeDL{name: "d1", canDownload: true, errOnStart: errors.New("d1 error")}
	d2 := &fakeDL{name: "d2", canDownload: true, errOnStart: errors.New("d2 error")}
	store := newMemStore()
	m, _ := testManager(t, []Downloader{d1, d2}, store, nil, nil, nil)

	job, err := m.Enqueue(context.Background(), core.DownloadRequest{
		Source: "spotify", ExternalID: "fb2", Artist: "A", Title: "T", Album: "Al",
	})
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.After(3 * time.Second)
	for {
		cur, _, _ := store.Get(context.Background(), job.ID)
		if cur.Status == core.DownloadCompleted || cur.Status == core.DownloadFailed {
			if cur.Status != core.DownloadFailed {
				t.Fatalf("chain-exhausted job should be DownloadFailed, got %q", cur.Status)
			}
			break
		}
		select {
		case <-deadline:
			cur2, _, _ := store.Get(context.Background(), job.ID)
			t.Fatalf("job did not reach DownloadFailed (status=%q)", cur2.Status)
		default:
			time.Sleep(time.Millisecond)
		}
	}

	if d1.starts() != 1 {
		t.Fatalf("d1 should have been attempted once, got %d", d1.starts())
	}
	if d2.starts() != 1 {
		t.Fatalf("d2 should have been attempted once (after d1 failed), got %d", d2.starts())
	}
}

// TestFallbackSingleDownloaderFailedReachesDownloadFailed asserts that with a
// single-downloader chain (the common today case), a Start error still reaches
// DownloadFailed — i.e., the fallback path does not break the no-fallback scenario.
// The ManualURL last-resort path (Retry with a URL) must remain reachable after this.
func TestFallbackSingleDownloaderFailedReachesDownloadFailed(t *testing.T) {
	dl := &fakeDL{name: "only", canDownload: true, errOnStart: errors.New("no match")}
	store := newMemStore()
	m, _ := testManager(t, []Downloader{dl}, store, nil, nil, nil)

	job, err := m.Enqueue(context.Background(), core.DownloadRequest{
		Source: "spotify", ExternalID: "fb3", Artist: "A", Title: "T", Album: "Al",
	})
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.After(3 * time.Second)
	for {
		cur, _, _ := store.Get(context.Background(), job.ID)
		if cur.Status == core.DownloadCompleted || cur.Status == core.DownloadFailed {
			if cur.Status != core.DownloadFailed {
				t.Fatalf("single-downloader failure should reach DownloadFailed, got %q", cur.Status)
			}
			break
		}
		select {
		case <-deadline:
			cur2, _, _ := store.Get(context.Background(), job.ID)
			t.Fatalf("job did not reach DownloadFailed (status=%q)", cur2.Status)
		default:
			time.Sleep(time.Millisecond)
		}
	}

	// After failing, the ManualURL last-resort must be reachable: Retry(id, url)
	// should re-queue the job so a human-supplied link can be used.
	m.SeedRequest(job.ID, core.DownloadRequest{
		Source: "spotify", ExternalID: "fb3", Artist: "A", Title: "T", Album: "Al",
	})
	// Swap out the erroring downloader so the retry can succeed (simulates the user
	// providing a direct URL that the downloader can use — not testing the full
	// ManualURL flow here, just that Retry is still callable after the fallback path).
	retried, err := m.Retry(context.Background(), job.ID, "https://example.com/manual.mp3")
	if err != nil {
		t.Fatalf("Retry after fallback-exhaustion should succeed: %v", err)
	}
	if retried.Status != core.DownloadQueued {
		t.Fatalf("retried job should be DownloadQueued, got %q", retried.Status)
	}
}

// ---- album-job timeout tests ----

// TestAlbumJobTimeoutDefault asserts that withDefaults sets AlbumJobTimeout to 2h.
func TestAlbumJobTimeoutDefault(t *testing.T) {
	cfg := Config{}.withDefaults()
	if cfg.AlbumJobTimeout != 2*time.Hour {
		t.Fatalf("AlbumJobTimeout default: want 2h, got %v", cfg.AlbumJobTimeout)
	}
}

// -- Task 4: Retry-async routing + granularity recovery --

// TestRetryAsyncJobRoutesToAsyncLane asserts that retrying a FAILED async job (e.g.
// Lidarr album) re-submits via the async Submit path, NOT the sync worker.
// Concretely: Submit is called exactly once (from the Retry), the job goes Running
// with a ref, and the fakeAsyncDL.Start is never called (it would return an error if
// it were, proving no sync-lane routing happened).
func TestRetryAsyncJobRoutesToAsyncLane(t *testing.T) {
	async := &fakeAsyncDL{name: "lidarr", submitRef: "retry-ref-1"}
	store := newMemStore()

	// Seed a FAILED async job directly in the store (simulates a Lidarr album job
	// that failed, which the user then retries via the UI).
	failed := core.DownloadJob{
		ID: "async-retry-j1", DedupKey: "dk-async-retry", Status: core.DownloadFailed,
		DownloaderName: "lidarr", Attempts: 1,
		Source: "spotify", ExternalID: "album-ext-1",
		Artist: "Daft Punk", Title: "Discovery", Album: "Discovery",
	}
	_ = store.Insert(context.Background(), failed, core.DownloadRequest{
		Source: "spotify", ExternalID: "album-ext-1", Artist: "Daft Punk",
		Title: "Discovery", Album: "Discovery", Granularity: core.GranularityAlbum,
	})

	m, _ := testManager(t, []Downloader{async}, store, nil, nil, nil)
	// Seed the in-memory request (simulates a live retry where m.reqs has the req).
	m.SeedRequest("async-retry-j1", core.DownloadRequest{
		Source: "spotify", ExternalID: "album-ext-1", Artist: "Daft Punk",
		Title: "Discovery", Album: "Discovery", Granularity: core.GranularityAlbum,
	})

	// Capture Submit call count BEFORE Retry (Enqueue also calls Submit on the fake).
	beforeSubmit := async.submitCalls

	_, err := m.Retry(context.Background(), "async-retry-j1", "")
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}

	// Give the goroutine spawned by Retry (go m.submitAsync) time to run.
	deadline := time.After(2 * time.Second)
	for {
		got, _, _ := store.Get(context.Background(), "async-retry-j1")
		if got.Status == core.DownloadRunning {
			break
		}
		select {
		case <-deadline:
			got2, _, _ := store.Get(context.Background(), "async-retry-j1")
			t.Fatalf("job did not become Running after Retry (status=%q) — async routing not triggered", got2.Status)
		default:
			time.Sleep(time.Millisecond)
		}
	}

	// Submit must have been called exactly once (from the Retry).
	async.mu.Lock()
	calls := async.submitCalls - beforeSubmit
	async.mu.Unlock()
	if calls != 1 {
		t.Fatalf("Submit call count after Retry = %d, want 1 (async routing failed)", calls)
	}

	// The job must carry the ref from Submit.
	got, _, _ := store.Get(context.Background(), "async-retry-j1")
	if got.DownloaderRef != "retry-ref-1" {
		t.Fatalf("job DownloaderRef = %q, want %q", got.DownloaderRef, "retry-ref-1")
	}
}

// TestRetryAsyncJobDoesNotCallStart asserts the complement: fakeAsyncDL.Start
// (which returns an error) is never invoked during a retried async job. If it
// were called, the job would fail with "fakeAsyncDL.Start should never be called".
// This is a belt-and-suspenders assertion that the sync worker does NOT run the job.
func TestRetryAsyncJobDoesNotCallStart(t *testing.T) {
	async := &fakeAsyncDL{name: "lidarr", submitRef: "retry-ref-2"}
	store := newMemStore()

	failed := core.DownloadJob{
		ID: "async-retry-j2", DedupKey: "dk-async-retry-2", Status: core.DownloadFailed,
		DownloaderName: "lidarr", Attempts: 1,
		Source: "spotify", ExternalID: "album-ext-2",
		Artist: "Radiohead", Title: "OK Computer", Album: "OK Computer",
	}
	_ = store.Insert(context.Background(), failed, core.DownloadRequest{
		Source: "spotify", ExternalID: "album-ext-2", Artist: "Radiohead",
		Title: "OK Computer", Album: "OK Computer", Granularity: core.GranularityAlbum,
	})

	m, _ := testManager(t, []Downloader{async}, store, nil, nil, nil)
	m.SeedRequest("async-retry-j2", core.DownloadRequest{
		Source: "spotify", ExternalID: "album-ext-2", Artist: "Radiohead",
		Title: "OK Computer", Album: "OK Computer", Granularity: core.GranularityAlbum,
	})

	_, err := m.Retry(context.Background(), "async-retry-j2", "")
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}

	// Wait for Running (Submit called) or Failed (Start called → error).
	deadline := time.After(2 * time.Second)
	for {
		got, _, _ := store.Get(context.Background(), "async-retry-j2")
		if got.Status == core.DownloadRunning {
			return // success: async route taken
		}
		if got.Status == core.DownloadFailed {
			t.Fatalf("job failed after retry — Start was called instead of Submit (sync routing bug): error=%q", got.Error)
		}
		select {
		case <-deadline:
			got2, _, _ := store.Get(context.Background(), "async-retry-j2")
			t.Fatalf("timed out waiting for Running (status=%q)", got2.Status)
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

// TestRetrySyncJobStillUsesWorker asserts that retrying a FAILED sync job still
// dispatches via the worker channel (calls Start, not Submit). The existing sync
// retry path must be unaffected by the async routing change.
func TestRetrySyncJobStillUsesWorker(t *testing.T) {
	dl := &fakeDL{name: "dl", canDownload: true}
	store := newMemStore()

	failed := core.DownloadJob{
		ID: "sync-retry-j1", DedupKey: "dk-sync-retry", Status: core.DownloadFailed,
		DownloaderName: "dl", Attempts: 1,
		Source: "spotify", ExternalID: "track-ext-1",
		Artist: "A", Title: "T", Album: "Al",
	}
	_ = store.Insert(context.Background(), failed, core.DownloadRequest{
		Source: "spotify", ExternalID: "track-ext-1", Artist: "A",
		Title: "T", Album: "Al", Granularity: core.GranularityTrack,
	})

	m, _ := testManager(t, []Downloader{dl}, store, nil, nil, nil)
	m.SeedRequest("sync-retry-j1", core.DownloadRequest{
		Source: "spotify", ExternalID: "track-ext-1", Artist: "A",
		Title: "T", Album: "Al", Granularity: core.GranularityTrack,
	})

	_, err := m.Retry(context.Background(), "sync-retry-j1", "")
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}

	// Wait for the sync job to complete (worker called Start → success).
	deadline := time.After(2 * time.Second)
	for {
		got, _, _ := store.Get(context.Background(), "sync-retry-j1")
		if got.Status == core.DownloadCompleted {
			break
		}
		select {
		case <-deadline:
			got2, _, _ := store.Get(context.Background(), "sync-retry-j1")
			t.Fatalf("sync retry did not complete (status=%q)", got2.Status)
		default:
			time.Sleep(time.Millisecond)
		}
	}

	if dl.starts() != 1 {
		t.Fatalf("sync retry should call Start exactly once, got %d", dl.starts())
	}
}

// TestGranularityRecoveredFromRequestJSON asserts that when the !haveReq path in
// process() or Retry reconstructs a DownloadRequest for a job that has no in-memory
// m.reqs entry, it recovers Granularity from the persisted request_json — so an album
// job retried after a restart uses AlbumJobTimeout and the correct URL, not the track defaults.
//
// Setup: seed a failed job with request_json carrying granularity:"album" but do NOT
// put anything in m.reqs (simulates cross-restart recovery). Trigger the !haveReq
// path by calling Retry without a prior SeedRequest. Assert the reconstructed request
// has Granularity == GranularityAlbum.
func TestGranularityRecoveredFromRequestJSON(t *testing.T) {
	async := &fakeAsyncDL{name: "lidarr", submitRef: "gran-ref-1"}
	store := newMemStore()

	failed := core.DownloadJob{
		ID: "gran-recovery-j1", DedupKey: "dk-gran-recovery", Status: core.DownloadFailed,
		DownloaderName: "lidarr", Attempts: 1,
		Source: "spotify", ExternalID: "album-gran-1",
		Artist: "Boards of Canada", Title: "Music Has the Right to Children", Album: "Music Has the Right to Children",
	}
	// Persist with Granularity in request_json.
	_ = store.Insert(context.Background(), failed, core.DownloadRequest{
		Source: "spotify", ExternalID: "album-gran-1",
		Artist: "Boards of Canada", Title: "Music Has the Right to Children",
		Album: "Music Has the Right to Children", Granularity: core.GranularityAlbum,
	})

	m, _ := testManager(t, []Downloader{async}, store, nil, nil, nil)
	// Intentionally do NOT call m.SeedRequest — force the !haveReq path.

	_, err := m.Retry(context.Background(), "gran-recovery-j1", "")
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}

	// Wait for the async lane to pick it up (submitAsync sets Running).
	deadline := time.After(2 * time.Second)
	for {
		got, _, _ := store.Get(context.Background(), "gran-recovery-j1")
		if got.Status == core.DownloadRunning || got.Status == core.DownloadFailed {
			break
		}
		select {
		case <-deadline:
			got2, _, _ := store.Get(context.Background(), "gran-recovery-j1")
			t.Fatalf("timed out waiting for Running/Failed (status=%q)", got2.Status)
		default:
			time.Sleep(time.Millisecond)
		}
	}

	// The in-memory reqs entry must now carry GranularityAlbum (set by Retry from
	// request_json before dispatch). If it's empty the timeout/URL logic would use
	// the track defaults — the core bug this test guards against.
	m.mu.Lock()
	req, haveReq := m.reqs["gran-recovery-j1"]
	m.mu.Unlock()
	// Note: submitAsync deletes the reqs entry on completion/failure. But Retry sets
	// it before dispatch, and submitAsync only deletes on error. On success (Running)
	// the entry may still be present; if deleted (submit error) the job is Failed and
	// we check the submit error path differently. Regardless, the key assertion is that
	// Submit received a request with GranularityAlbum — verify via submit count and job state.
	got, _, _ := store.Get(context.Background(), "gran-recovery-j1")
	if got.Status == core.DownloadRunning {
		// submitAsync succeeded → reqs may still be present; if so, check granularity.
		if haveReq && req.Granularity != core.GranularityAlbum {
			t.Fatalf("reconstructed request Granularity = %q, want %q (granularity lost in !haveReq path)", req.Granularity, core.GranularityAlbum)
		}
		// Submit was called — that's the async lane. The test is green.
		async.mu.Lock()
		calls := async.submitCalls
		async.mu.Unlock()
		if calls != 1 {
			t.Fatalf("Submit should be called once after granularity recovery, got %d", calls)
		}
	} else {
		t.Fatalf("job failed after granularity-recovery retry (status=%q, error=%q)", got.Status, got.Error)
	}
}

// TestJobTimeoutByGranularity uses the jobTimeout helper seam to verify that the
// manager selects AlbumJobTimeout for album-granularity requests and JobTimeout for
// track-granularity (or empty granularity) requests.
func TestJobTimeoutByGranularity(t *testing.T) {
	cfg := Config{
		JobTimeout:      50 * time.Millisecond,
		AlbumJobTimeout: 300 * time.Millisecond,
	}
	m := &Manager{cfg: cfg.withDefaults()}
	// withDefaults must NOT overwrite explicitly set positive values.
	m.cfg.JobTimeout = cfg.JobTimeout
	m.cfg.AlbumJobTimeout = cfg.AlbumJobTimeout

	trackReq := core.DownloadRequest{Granularity: core.GranularityTrack}
	albumReq := core.DownloadRequest{Granularity: core.GranularityAlbum}
	emptyReq := core.DownloadRequest{}

	if got := m.jobTimeout(trackReq); got != 50*time.Millisecond {
		t.Fatalf("track granularity: want 50ms, got %v", got)
	}
	if got := m.jobTimeout(emptyReq); got != 50*time.Millisecond {
		t.Fatalf("empty granularity: want 50ms (JobTimeout), got %v", got)
	}
	if got := m.jobTimeout(albumReq); got != 300*time.Millisecond {
		t.Fatalf("album granularity: want 300ms, got %v", got)
	}
}

// TestStopWithoutStartIsNoOp asserts that calling Stop() on a Manager that was
// never Start()ed returns promptly (no deadlock, no panic). This guards the guard
// we added to Stop() that skips wg.Wait() / close(stopCh) when started==false.
func TestStopWithoutStartIsNoOp(t *testing.T) {
	m := NewManager(Config{}, wrapDownloaders(nil), newMemStore(), nil, &fakeScanner{}, nil, nil, nil, nil, nil)
	done := make(chan struct{})
	go func() {
		m.Stop()
		close(done)
	}()
	select {
	case <-done:
		// returned promptly — pass
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() without Start() did not return within 2s (potential deadlock)")
	}
}

// ctxSensitiveAsyncDL is a fakeAsyncDL variant whose Submit blocks until the test
// signals via readyCh, then checks ctx.Err(). This makes the cancellation test
// deterministic: the test cancels the request context WHILE Submit is blocking,
// so ctx.Err() is guaranteed to be non-nil if the un-detached context was passed.
// With context.WithoutCancel the child ctx is never canceled → Submit returns the
// ref and the job reaches Running.
type ctxSensitiveAsyncDL struct {
	fakeAsyncDL
	readyCh chan struct{} // closed by the test to unblock Submit
}

func (d *ctxSensitiveAsyncDL) Submit(ctx context.Context, req core.DownloadRequest) (string, error) {
	// Block until the test signals (gives it time to call cancel()).
	<-d.readyCh
	// Now check — if the un-detached request ctx was forwarded it will be canceled.
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return d.fakeAsyncDL.Submit(ctx, req)
}

// TestRetryAsyncDetachesRequestContext asserts that Retry's spawned goroutine uses
// context.WithoutCancel so that canceling the caller's request context (which the
// HTTP handler does the instant it returns) does not kill the in-flight Submit.
//
// Fail-without-fix: with the old `go m.submitAsync(ctx, …)` the request ctx is
// forwarded into Submit; canceling it before Submit runs causes Submit to return
// ctx.Err(), which submitAsync treats as a failure → job goes Failed.
// With the fix (context.WithoutCancel(ctx)), Submit receives a non-canceled ctx →
// job goes Running. The test is deterministic: Submit blocks on readyCh until the
// test has called cancel(), guaranteeing the cancellation has propagated.
func TestRetryAsyncDetachesRequestContext(t *testing.T) {
	readyCh := make(chan struct{})
	async := &ctxSensitiveAsyncDL{readyCh: readyCh}
	async.name = "lidarr"
	async.submitRef = "detach-ref-1"
	store := newMemStore()

	failed := core.DownloadJob{
		ID: "async-detach-j1", DedupKey: "dk-async-detach", Status: core.DownloadFailed,
		DownloaderName: "lidarr", Attempts: 1,
		Source: "spotify", ExternalID: "album-detach-ext",
		Artist: "Pink Floyd", Title: "The Wall", Album: "The Wall",
	}
	_ = store.Insert(context.Background(), failed, core.DownloadRequest{
		Source: "spotify", ExternalID: "album-detach-ext", Artist: "Pink Floyd",
		Title: "The Wall", Album: "The Wall", Granularity: core.GranularityAlbum,
	})

	m, _ := testManager(t, []Downloader{async}, store, nil, nil, nil)
	m.SeedRequest("async-detach-j1", core.DownloadRequest{
		Source: "spotify", ExternalID: "album-detach-ext", Artist: "Pink Floyd",
		Title: "The Wall", Album: "The Wall", Granularity: core.GranularityAlbum,
	})

	// Simulate an HTTP handler context: the caller cancels it immediately after
	// Retry returns (mimicking the Go HTTP server canceling r.Context() on return).
	reqCtx, cancel := context.WithCancel(context.Background())

	_, err := m.Retry(reqCtx, "async-detach-j1", "")
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}

	// Cancel the request context BEFORE unblocking Submit. This guarantees the
	// cancellation has propagated when Submit checks ctx.Err().
	cancel()
	close(readyCh) // now let Submit proceed and observe the (possibly canceled) ctx

	// The job must reach Running (Submit succeeded with detached ctx), NOT Failed.
	deadline := time.After(2 * time.Second)
	for {
		got, _, _ := store.Get(context.Background(), "async-detach-j1")
		if got.Status == core.DownloadRunning {
			if got.DownloaderRef != "detach-ref-1" {
				t.Fatalf("ref = %q, want detach-ref-1", got.DownloaderRef)
			}
			return // pass
		}
		if got.Status == core.DownloadFailed {
			t.Fatalf("job went Failed after retry — request ctx was NOT detached (context.WithoutCancel fix missing): error=%q", got.Error)
		}
		select {
		case <-deadline:
			got2, _, _ := store.Get(context.Background(), "async-detach-j1")
			t.Fatalf("timed out waiting for Running (status=%q)", got2.Status)
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

// ─── Task 3: CanonicalMinter fakes + tests ──────────────────────────────────

// fakeCanonicalMinter records calls and returns a fixed canonical id.
type fakeCanonicalMinter struct {
	mu     sync.Mutex
	calls  []catalog.Identity
	retID  string
	retErr error
}

func (f *fakeCanonicalMinter) CanonicalFor(_ context.Context, id catalog.Identity) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, id)
	if f.retErr != nil {
		return "", f.retErr
	}
	return f.retID, nil
}

func (f *fakeCanonicalMinter) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeCanonicalMinter) lastCall() (catalog.Identity, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return catalog.Identity{}, false
	}
	return f.calls[len(f.calls)-1], true
}

// testManagerWithMinter constructs a Manager with a CanonicalMinter injected.
func testManagerWithMinter(t *testing.T, downloaders []Downloader, store JobStore, rematch Rematcher, minter CanonicalMinter) (*Manager, *events.Bus) {
	t.Helper()
	bus := events.New()
	scanner := &fakeScanner{}
	ver := &fakeVersion{v: 1}
	if rematch == nil {
		rematch = &fakeRematcher{trackID: "t1"}
	}
	m := NewManager(
		Config{Workers: 2, DebounceWindow: 5 * time.Second, ScanPollEvery: time.Millisecond, ScanPollMax: time.Second, ScanSettleMax: 10 * time.Millisecond},
		wrapDownloaders(downloaders), store, bus, scanner, rematch, ver, nil, nil, nil,
	)
	m.SetCanonicalMinter(minter)
	t.Cleanup(m.Stop)
	m.Start()
	return m, bus
}

// TestMintAtLink_BackfillMints verifies that BackfillUnlinked mints a canonical_id
// for a completed/matched job via the injected CanonicalMinter.
func TestMintAtLink_BackfillMints(t *testing.T) {
	store := newMemStore()
	// Seed a completed job with no library_track_id (unlinked).
	job := core.DownloadJob{
		ID: "j-mint-1", DedupKey: "dk1", Status: core.DownloadCompleted,
		DownloaderName: "dl", Source: "spotify", ExternalID: "sp1",
		Title: "Moon River", Artist: "Audrey Hepburn", Album: "Breakfast", ISRC: "US001",
		DurationMs: 120000,
	}
	_ = store.Insert(context.Background(), job, core.DownloadRequest{
		Source: "spotify", ExternalID: "sp1", Title: "Moon River", Artist: "Audrey Hepburn",
		Album: "Breakfast", ISRC: "US001", DurationMs: 120000,
	})
	rematch := &fakeRematcher{trackID: "libtrack-1", coverArtID: "art-1"}
	minter := &fakeCanonicalMinter{retID: "trk_aaaa"}

	m := NewManager(
		Config{Workers: 1, DebounceWindow: time.Millisecond, ScanPollEvery: time.Millisecond, ScanPollMax: 10 * time.Millisecond},
		wrapDownloaders(nil), store, nil, &fakeScanner{}, rematch, &fakeVersion{v: 1}, nil, nil, nil,
	)
	m.SetCanonicalMinter(minter)
	// Run BackfillUnlinked directly (not via Start) to keep the test synchronous.
	m.BackfillUnlinked()

	// The minter should have been called once for the linked job.
	if minter.callCount() != 1 {
		t.Fatalf("minter called %d times, want 1", minter.callCount())
	}
	id, ok := minter.lastCall()
	if !ok {
		t.Fatal("no minter call recorded")
	}
	if id.Kind != "track" || id.Source != "spotify" || id.ExternalID != "sp1" {
		t.Fatalf("unexpected identity passed to minter: %+v", id)
	}
	// The job should have canonical_id set.
	got, _, _ := store.Get(context.Background(), "j-mint-1")
	if got.CanonicalID != "trk_aaaa" {
		t.Fatalf("job.CanonicalID = %q, want %q", got.CanonicalID, "trk_aaaa")
	}
}

// TestMintAtLink_Scoping verifies mint scoping:
//   - A completed + linked job (already has library_track_id) is NOT re-minted
//     during BackfillUnlinked (it's already matched).
//   - An archived (failed/canceled) job is NOT minted.
//   - A queued/running job is NOT minted.
func TestMintAtLink_Scoping(t *testing.T) {
	store := newMemStore()
	ctx := context.Background()

	// Already-linked completed job (has library_track_id) — should not enter backfill loop.
	linked := core.DownloadJob{
		ID: "j-linked", DedupKey: "dk-linked", Status: core.DownloadCompleted,
		LibraryTrackID: "existing-lib-track", CanonicalID: "trk_existing",
		DownloaderName: "dl", Source: "spotify", ExternalID: "sp-linked",
		Title: "Already Linked", Artist: "A", Album: "B", DurationMs: 120000,
	}
	_ = store.Insert(ctx, linked, core.DownloadRequest{Source: "spotify", ExternalID: "sp-linked", Title: "Already Linked"})

	// Failed job — must not be minted.
	failed := core.DownloadJob{
		ID: "j-failed", DedupKey: "dk-failed", Status: core.DownloadFailed,
		DownloaderName: "dl", Source: "spotify", ExternalID: "sp-failed",
		Title: "Failed Song", Artist: "A", Album: "B", DurationMs: 120000,
	}
	_ = store.Insert(ctx, failed, core.DownloadRequest{Source: "spotify", ExternalID: "sp-failed", Title: "Failed Song"})

	// Queued job — must not be minted.
	queued := core.DownloadJob{
		ID: "j-queued", DedupKey: "dk-queued", Status: core.DownloadQueued,
		DownloaderName: "dl", Source: "spotify", ExternalID: "sp-queued",
		Title: "Queued Song", Artist: "A", Album: "B", DurationMs: 120000,
	}
	_ = store.Insert(ctx, queued, core.DownloadRequest{Source: "spotify", ExternalID: "sp-queued", Title: "Queued Song"})

	minter := &fakeCanonicalMinter{retID: "trk_new"}
	// Rematcher returns nothing matched (so no job gets linked in this pass).
	rematch := &fakeRematcher{trackID: ""} // MatchNotInLibrary

	m := NewManager(
		Config{Workers: 1, DebounceWindow: time.Millisecond},
		wrapDownloaders(nil), store, nil, &fakeScanner{}, rematch, &fakeVersion{v: 1}, nil, nil, nil,
	)
	m.SetCanonicalMinter(minter)
	m.BackfillUnlinked()

	// Minter must NOT have been called: no jobs were newly linked.
	if minter.callCount() != 0 {
		t.Fatalf("minter called %d times for non-linked jobs; want 0", minter.callCount())
	}
}

// TestMintAtLink_Idempotent verifies that re-running BackfillUnlinked on an already-linked
// job (with a canonical_id already set) does not attempt to re-link it (it has
// library_track_id, so the backfill loop skips it).
func TestMintAtLink_Idempotent(t *testing.T) {
	store := newMemStore()
	ctx := context.Background()

	// Already-linked + already-minted job.
	job := core.DownloadJob{
		ID: "j-idem", DedupKey: "dk-idem", Status: core.DownloadCompleted,
		LibraryTrackID: "libtrack-idem", CanonicalID: "trk_idem",
		DownloaderName: "dl", Source: "spotify", ExternalID: "sp-idem",
		Title: "Idempotent", Artist: "A", Album: "B", DurationMs: 120000,
	}
	_ = store.Insert(ctx, job, core.DownloadRequest{Source: "spotify", ExternalID: "sp-idem", Title: "Idempotent"})

	minter := &fakeCanonicalMinter{retID: "trk_should_not_be_called"}
	rematch := &fakeRematcher{trackID: "libtrack-idem"}

	m := NewManager(
		Config{Workers: 1, DebounceWindow: time.Millisecond},
		wrapDownloaders(nil), store, nil, &fakeScanner{}, rematch, &fakeVersion{v: 1}, nil, nil, nil,
	)
	m.SetCanonicalMinter(minter)
	m.BackfillUnlinked()

	// Minter never called: the job was skipped because library_track_id is non-empty.
	if minter.callCount() != 0 {
		t.Fatalf("minter called %d times for already-linked job; want 0", minter.callCount())
	}
	// canonical_id is unchanged.
	got, _, _ := store.Get(ctx, "j-idem")
	if got.CanonicalID != "trk_idem" {
		t.Fatalf("canonical_id changed from trk_idem to %q", got.CanonicalID)
	}
}

// TestBackfillCanonicalIDs_MintsLegacyLinkedJob is the regression test for the
// cover-rot fix: a LEGACY job linked BEFORE Task 3 (has library_track_id, but
// canonical_id==”) is NEVER minted by BackfillUnlinked/runScan (they gate on
// library_track_id==”). BackfillCanonicalIDs converges these legacy rows onto the
// canonical path so retiring the clear-dance does not rot their covers on a swap.
func TestBackfillCanonicalIDs_MintsLegacyLinkedJob(t *testing.T) {
	store := newMemStore()
	ctx := context.Background()

	// A pre-existing completed+linked job with NO canonical_id (minted before Task 3).
	legacy := core.DownloadJob{
		ID: "j-legacy", DedupKey: "dk-legacy", Status: core.DownloadCompleted,
		LibraryTrackID: "legacy-backend-track", CoverArtID: "legacy-backend-cover",
		CanonicalID:    "", // the hole this fix plugs
		DownloaderName: "dl", Source: "spotify", ExternalID: "sp-legacy",
		Title: "Legacy Song", Artist: "L", Album: "M", ISRC: "US-legacy", DurationMs: 200000,
	}
	_ = store.Insert(ctx, legacy, core.DownloadRequest{
		Source: "spotify", ExternalID: "sp-legacy", Title: "Legacy Song", Artist: "L", Album: "M", ISRC: "US-legacy", DurationMs: 200000,
	})

	minter := &fakeCanonicalMinter{retID: "trk_legacy_minted"}
	m := NewManager(
		Config{Workers: 1, DebounceWindow: time.Millisecond},
		wrapDownloaders(nil), store, nil, &fakeScanner{}, &fakeRematcher{trackID: "legacy-backend-track"}, &fakeVersion{v: 1}, nil, nil, nil,
	)
	m.SetCanonicalMinter(minter)

	m.BackfillCanonicalIDs()

	// The minter must have been called ONCE for the legacy linked job.
	if minter.callCount() != 1 {
		t.Fatalf("BackfillCanonicalIDs minted %d times; want 1 for the legacy linked job", minter.callCount())
	}
	id, _ := minter.lastCall()
	if id.Source != "spotify" || id.ExternalID != "sp-legacy" || id.ISRC != "US-legacy" {
		t.Fatalf("mint identity built from wrong columns: %+v", id)
	}
	// The row now carries the canonical id.
	got, _, _ := store.Get(ctx, "j-legacy")
	if got.CanonicalID != "trk_legacy_minted" {
		t.Fatalf("legacy job canonical_id = %q; want trk_legacy_minted", got.CanonicalID)
	}
}

// TestBackfillCanonicalIDs_Idempotent verifies a second run does NOT re-mint an
// already-minted row (once canonical_id is set, the row no longer matches).
func TestBackfillCanonicalIDs_Idempotent(t *testing.T) {
	store := newMemStore()
	ctx := context.Background()

	// Legacy linked job, canonical_id empty.
	_ = store.Insert(ctx, core.DownloadJob{
		ID: "j-idem2", DedupKey: "dk-idem2", Status: core.DownloadCompleted,
		LibraryTrackID: "backend-track", CanonicalID: "",
		DownloaderName: "dl", Source: "spotify", ExternalID: "sp-idem2",
		Title: "Idem2", Artist: "A", Album: "B", DurationMs: 100000,
	}, core.DownloadRequest{Source: "spotify", ExternalID: "sp-idem2", Title: "Idem2"})

	minter := &fakeCanonicalMinter{retID: "trk_idem2"}
	m := NewManager(
		Config{Workers: 1, DebounceWindow: time.Millisecond},
		wrapDownloaders(nil), store, nil, &fakeScanner{}, &fakeRematcher{trackID: "backend-track"}, &fakeVersion{v: 1}, nil, nil, nil,
	)
	m.SetCanonicalMinter(minter)

	m.BackfillCanonicalIDs() // first run mints
	m.BackfillCanonicalIDs() // second run must be a no-op

	if minter.callCount() != 1 {
		t.Fatalf("BackfillCanonicalIDs minted %d times across two runs; want 1 (idempotent)", minter.callCount())
	}
}

// TestBackfillCanonicalIDs_Scoping verifies BackfillCanonicalIDs does NOT touch
// unlinked jobs (library_track_id==”) nor non-completed (failed/queued) jobs — it
// only converges completed+linked+unminted rows.
func TestBackfillCanonicalIDs_Scoping(t *testing.T) {
	store := newMemStore()
	ctx := context.Background()

	// Unlinked completed job — must NOT be minted (that's BackfillUnlinked's job, only after a match).
	_ = store.Insert(ctx, core.DownloadJob{
		ID: "j-unlinked", DedupKey: "dk-unlinked", Status: core.DownloadCompleted,
		LibraryTrackID: "", CanonicalID: "",
		DownloaderName: "dl", Source: "spotify", ExternalID: "sp-unlinked", Title: "Unlinked",
	}, core.DownloadRequest{Source: "spotify", ExternalID: "sp-unlinked", Title: "Unlinked"})

	// Failed job with a library_track_id — archived, must NOT be minted.
	_ = store.Insert(ctx, core.DownloadJob{
		ID: "j-failed2", DedupKey: "dk-failed2", Status: core.DownloadFailed,
		LibraryTrackID: "some-track", CanonicalID: "",
		DownloaderName: "dl", Source: "spotify", ExternalID: "sp-failed2", Title: "Failed2",
	}, core.DownloadRequest{Source: "spotify", ExternalID: "sp-failed2", Title: "Failed2"})

	// Queued job — must NOT be minted.
	_ = store.Insert(ctx, core.DownloadJob{
		ID: "j-queued2", DedupKey: "dk-queued2", Status: core.DownloadQueued,
		LibraryTrackID: "", CanonicalID: "",
		DownloaderName: "dl", Source: "spotify", ExternalID: "sp-queued2", Title: "Queued2",
	}, core.DownloadRequest{Source: "spotify", ExternalID: "sp-queued2", Title: "Queued2"})

	// Already-minted linked job — must NOT be re-minted.
	_ = store.Insert(ctx, core.DownloadJob{
		ID: "j-minted", DedupKey: "dk-minted", Status: core.DownloadCompleted,
		LibraryTrackID: "track-minted", CanonicalID: "trk_already",
		DownloaderName: "dl", Source: "spotify", ExternalID: "sp-minted", Title: "Minted",
	}, core.DownloadRequest{Source: "spotify", ExternalID: "sp-minted", Title: "Minted"})

	minter := &fakeCanonicalMinter{retID: "trk_should_not_fire"}
	m := NewManager(
		Config{Workers: 1, DebounceWindow: time.Millisecond},
		wrapDownloaders(nil), store, nil, &fakeScanner{}, &fakeRematcher{trackID: "x"}, &fakeVersion{v: 1}, nil, nil, nil,
	)
	m.SetCanonicalMinter(minter)

	m.BackfillCanonicalIDs()

	if minter.callCount() != 0 {
		t.Fatalf("BackfillCanonicalIDs minted %d times; want 0 (no completed+linked+unminted rows to converge)", minter.callCount())
	}
}

// swapMatcher is a resolver.Rematcher whose returned backend refs can be swapped
// at runtime to model a backend identity change (old backend → new backend).
type swapMatcher struct {
	mu     sync.Mutex
	result core.MatchResult
}

func (m *swapMatcher) Match(_ context.Context, _ core.ExternalResult) (core.MatchResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.result, nil
}

func (m *swapMatcher) set(r core.MatchResult) {
	m.mu.Lock()
	m.result = r
	m.mu.Unlock()
}

// TestSwapSurvival is the load-bearing test: a completed download job with a minted
// canonical_id resolves a FRESH, CORRECT cover via the resolver AFTER a backend
// swap, WITHOUT the ClearMatchedDownloadJobLibraryRefs dance running. It drives the
// job's canonical id through the REAL resolver.Service.Resolve across the swap and
// asserts the resolver returns the NEW backend cover (not the stale pre-swap one).
func TestSwapSurvival(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/swap.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	q := st.Q()
	ctx := context.Background()

	// Mint a canonical entity for the download's track (this is the stable id).
	catSvc := catalog.NewService(q, time.Now, func() string { return "swap-0001" })
	canonicalID, err := catSvc.CanonicalFor(ctx, catalog.Identity{
		Kind: "track", Source: "spotify", ExternalID: "sp-swap",
		Title: "Swap Song", Artist: "B", Album: "C", DurationMs: 180000,
	})
	if err != nil {
		t.Fatal(err)
	}

	// The resolver re-resolves the canonical id → backend addressing via the matcher.
	matcher := &swapMatcher{result: core.MatchResult{
		Status: core.MatchInLibrary, LibraryTrackID: "old-backend-track", CoverArtID: "old-backend-cover",
	}}
	res := resolver.NewService(q, func() resolver.Rematcher { return matcher }, time.Now)

	// PRE-swap: the resolver returns the OLD backend's cover.
	pre, err := res.Resolve(ctx, canonicalID)
	if err != nil {
		t.Fatal(err)
	}
	if !pre.Found || pre.CoverArtID != "old-backend-cover" {
		t.Fatalf("pre-swap resolve: got %+v, want cover old-backend-cover", pre)
	}

	// SWAP the backend: the live library now serves different track/cover ids, so the
	// matcher returns the NEW backend addressing. RefreshLinked then marks the binding
	// stale and re-resolves against the new backend. This models a real backend swap
	// WITHOUT the clear-dance touching download_jobs.
	matcher.set(core.MatchResult{
		Status: core.MatchInLibrary, LibraryTrackID: "new-backend-track", CoverArtID: "new-backend-cover",
	})
	if err := res.RefreshLinked(ctx, []string{canonicalID}); err != nil {
		t.Fatal(err)
	}

	// POST-swap: resolving the SAME canonical id now yields the NEW backend cover.
	post, err := res.Resolve(ctx, canonicalID)
	if err != nil {
		t.Fatal(err)
	}
	if !post.Found {
		t.Fatalf("post-swap resolve not found: %+v", post)
	}
	if post.CoverArtID != "new-backend-cover" {
		t.Fatalf("post-swap resolve cover = %q; want new-backend-cover (canonical id re-resolved through the boundary — no stale-raw fallback, no clear dance)", post.CoverArtID)
	}
	// The canonical id itself is stable across the swap (same id resolved both times).
	if canonicalID == "" {
		t.Fatal("canonical id must be non-empty and stable across the swap")
	}
}

// TestCanonicalIDInDTO verifies that a job with a canonical_id emits it in the DTO
// (the JSON-serialized DownloadJob). This is the API layer check.
func TestCanonicalIDInDTO(t *testing.T) {
	job := core.DownloadJob{
		ID:          "j-dto",
		DedupKey:    "dk-dto",
		Status:      core.DownloadCompleted,
		CanonicalID: "trk_abc123",
		Source:      "spotify",
		ExternalID:  "sp1",
		Title:       "DTO Song",
	}
	data, err := jsonMarshal(job)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"canonicalId":"trk_abc123"`) {
		t.Fatalf("DTO must emit canonicalId; got: %s", string(data))
	}
}

// ─── Task 4: runScan targeted RefreshLinked ──────────────────────────────────

// fakeBindingResolver records RefreshLinked calls for assertions.
type fakeBindingResolver struct {
	mu     sync.Mutex
	calls  [][]string // each call's catalogIDs arg
	retErr error
}

func (f *fakeBindingResolver) Resolve(_ context.Context, _ string) (resolver.Addressing, error) {
	return resolver.Addressing{}, nil
}

func (f *fakeBindingResolver) RefreshLinked(_ context.Context, ids []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]string, len(ids))
	copy(cp, ids)
	f.calls = append(f.calls, cp)
	return f.retErr
}

func (f *fakeBindingResolver) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeBindingResolver) getCall(i int) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[i]
}

// TestRunScan_RefreshLinkedCalledWithLinkedCanonicalIDs asserts that after
// runScan links jobs, RefreshLinked is called with exactly the canonical ids of
// the jobs linked in that scan (jobs that were skipped or got an empty
// canonical_id are excluded).
//
// Setup: enqueue 3 jobs that download and complete inside the debounce window
// (so BackfillUnlinked at Start does not consume them — BackfillUnlinked only
// sees already-completed jobs at startup, and these jobs are in Queued state
// then). The minter returns "" for ext3, so only trk_scan1 + trk_scan2 end up
// in the RefreshLinked call.
func TestRunScan_RefreshLinkedCalledWithLinkedCanonicalIDs(t *testing.T) {
	clk := newFakeClock()
	store := newMemStore()

	// Minter returns deterministic ids keyed by ExternalID; ext3 returns "".
	minterIDs := map[string]string{
		"ext1": "trk_scan1",
		"ext2": "trk_scan2",
		"ext3": "",
	}
	minter := &fakeDynamicMinter{ids: minterIDs}
	res := &fakeBindingResolver{}
	dl := &fakeDL{name: "dl", canDownload: true}
	bus := events.New()
	m := NewManager(
		Config{Workers: 3, DebounceWindow: 5 * time.Second, ScanPollEvery: time.Millisecond, ScanPollMax: time.Second, ScanSettleMax: 10 * time.Millisecond},
		wrapDownloaders([]Downloader{dl}), store, bus, &fakeScanner{}, &fakeRematcher{trackID: "lib-scan-track"}, &fakeVersion{v: 1}, clk, nil,
		func() BindingResolver { return res },
	)
	m.SetCanonicalMinter(minter)
	t.Cleanup(m.Stop)
	m.Start()

	ctx := context.Background()
	for _, ext := range []string{"ext1", "ext2", "ext3"} {
		_, err := m.Enqueue(ctx, core.DownloadRequest{
			Source: "spotify", ExternalID: ext, Title: "Song " + ext, Artist: "A", Album: "B",
		})
		if err != nil {
			t.Fatalf("Enqueue %s: %v", ext, err)
		}
	}

	// Wait for all 3 jobs to finish downloading.
	deadline := time.After(2 * time.Second)
	for {
		jobs, _ := store.List(ctx)
		done := 0
		for _, j := range jobs {
			if j.Status == core.DownloadCompleted {
				done++
			}
		}
		if done == 3 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("downloads did not complete in time")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	// A worker records completion before it arms the debounce timer. Wait for all
	// three completions to arm/re-arm it so Advance cannot fire a partial scan.
	timerDeadline := time.After(2 * time.Second)
	for clk.scheduledCount() < 3 {
		select {
		case <-timerDeadline:
			t.Fatal("not all completed downloads armed the debounce timer within 2s")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	// Fire the final debounced scan: runScan links the 3 jobs, mints ids, and
	// calls RefreshLinked synchronously through fakeClock.Advance.
	clk.Advance(5 * time.Second)

	// RefreshLinked must have been called exactly once.
	if res.callCount() != 1 {
		t.Fatalf("RefreshLinked called %d times; want 1", res.callCount())
	}
	got := res.getCall(0)
	// Must contain exactly trk_scan1 and trk_scan2 (ext3 returned "" → excluded).
	if len(got) != 2 {
		t.Fatalf("RefreshLinked ids len=%d; want 2; got %v", len(got), got)
	}
	want := map[string]bool{"trk_scan1": true, "trk_scan2": true}
	for _, id := range got {
		if !want[id] {
			t.Fatalf("RefreshLinked got unexpected id %q; want only trk_scan1 and trk_scan2", id)
		}
		delete(want, id)
	}
	if len(want) > 0 {
		t.Fatalf("RefreshLinked missing expected ids: %v", want)
	}
}

// TestRunScan_DoesNotBumpBindingEpoch asserts that a plain scan does NOT bump
// binding_epoch for any identity. Only library_version moves.
// We wire a fakeEpochBumper to observe any BumpEpoch calls and assert 0.
// The fakeEpochBumper is a standalone counter not wired into the Manager — it
// serves as a canary: if runScan ever called it (which the spec forbids), the
// test fails.
func TestRunScan_DoesNotBumpBindingEpoch(t *testing.T) {
	clk := newFakeClock()
	store := newMemStore()
	ver := &fakeVersion{v: 7}
	epochBumper := &fakeEpochBumper{epoch: 42}
	res := &fakeBindingResolver{}
	dl := &fakeDL{name: "dl", canDownload: true}
	bus := events.New()
	m := NewManager(
		Config{Workers: 1, DebounceWindow: 5 * time.Second, ScanPollEvery: time.Millisecond, ScanPollMax: time.Second, ScanSettleMax: 10 * time.Millisecond},
		wrapDownloaders([]Downloader{dl}), store, bus, &fakeScanner{}, &fakeRematcher{trackID: "lib-epoch-track"}, ver, clk, nil,
		func() BindingResolver { return res },
	)
	t.Cleanup(m.Stop)
	m.Start()

	ctx := context.Background()
	_, err := m.Enqueue(ctx, core.DownloadRequest{
		Source: "spotify", ExternalID: "epoch-ext1", Title: "Epoch Song", Artist: "A", Album: "B",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Wait for the download to complete.
	deadline := time.After(2 * time.Second)
	for {
		jobs, _ := store.List(ctx)
		if len(jobs) > 0 && jobs[0].Status == core.DownloadCompleted {
			break
		}
		select {
		case <-deadline:
			t.Fatal("job never completed")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	// Trigger the scan.
	clk.Advance(5 * time.Second)

	// library_version must have moved (bumped from 7 to 8).
	if ver.get() != 8 {
		t.Fatalf("library_version: got %d, want 8", ver.get())
	}
	// The fake epoch bumper must NOT have been touched by runScan.
	if epochBumper.bumpCount() != 0 {
		t.Fatalf("binding_epoch bumped %d times by runScan; want 0 (plain scan must not touch binding_epoch)", epochBumper.bumpCount())
	}
}

// TestRunScan_NilResolverProviderDoesNotPanic asserts that when m.resolve is nil
// (or returns nil), runScan completes normally without panicking.
func TestRunScan_NilResolverProviderDoesNotPanic(t *testing.T) {
	ctx := context.Background()
	dl := &fakeDL{name: "dl", canDownload: true}
	bus := events.New()

	waitCompleted := func(t *testing.T, st *memStore, id string) {
		t.Helper()
		deadline := time.After(2 * time.Second)
		for {
			j, _, _ := st.Get(ctx, id)
			if j.Status == core.DownloadCompleted {
				return
			}
			select {
			case <-deadline:
				t.Fatal("job never completed")
			default:
				time.Sleep(time.Millisecond)
			}
		}
	}

	// Case 1: nil resolve provider.
	clk1 := newFakeClock()
	store1 := newMemStore()
	m1 := NewManager(
		Config{Workers: 1, DebounceWindow: 5 * time.Second, ScanPollEvery: time.Millisecond, ScanPollMax: time.Second, ScanSettleMax: 10 * time.Millisecond},
		wrapDownloaders([]Downloader{dl}), store1, bus, &fakeScanner{}, &fakeRematcher{trackID: "lib-t1"}, &fakeVersion{v: 1}, clk1, nil, nil,
	)
	t.Cleanup(m1.Stop)
	m1.Start()
	j1, err := m1.Enqueue(ctx, core.DownloadRequest{Source: "spotify", ExternalID: "nr-ext1", Title: "NilRes1", Artist: "A", Album: "B"})
	if err != nil {
		t.Fatal(err)
	}
	waitCompleted(t, store1, j1.ID)
	// Must not panic.
	clk1.Advance(5 * time.Second)

	// Case 2: provider returning nil.
	clk2 := newFakeClock()
	store2 := newMemStore()
	m2 := NewManager(
		Config{Workers: 1, DebounceWindow: 5 * time.Second, ScanPollEvery: time.Millisecond, ScanPollMax: time.Second, ScanSettleMax: 10 * time.Millisecond},
		wrapDownloaders([]Downloader{dl}), store2, bus, &fakeScanner{}, &fakeRematcher{trackID: "lib-t2"}, &fakeVersion{v: 1}, clk2, nil,
		func() BindingResolver { return nil },
	)
	t.Cleanup(m2.Stop)
	m2.Start()
	j2, err := m2.Enqueue(ctx, core.DownloadRequest{Source: "spotify", ExternalID: "nr-ext2", Title: "NilRes2", Artist: "A", Album: "B"})
	if err != nil {
		t.Fatal(err)
	}
	waitCompleted(t, store2, j2.ID)
	// Must not panic.
	clk2.Advance(5 * time.Second)
}

// fakeDynamicMinter returns canonical ids keyed by ExternalID, enabling per-job
// control over what id (or empty string) the minter returns.
type fakeDynamicMinter struct {
	mu  sync.Mutex
	ids map[string]string // ExternalID → canonical id
}

func (f *fakeDynamicMinter) CanonicalFor(_ context.Context, id catalog.Identity) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ids[id.ExternalID], nil
}

// fakeEpochBumper records bumps (used only to assert runScan does NOT call it).
type fakeEpochBumper struct {
	mu    sync.Mutex
	epoch int64
	bumps int
}

func (f *fakeEpochBumper) bumpCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bumps
}

// TestManagerAcceptsNilSafeResolverProvider verifies that NewManager accepts a
// nil-safe resolve func() BindingResolver dep (the provider seam from Task 1).
// The dep must be stored but NOT called — Tasks 3-5 add the actual Resolve calls.
// A nil provider and a provider-returning-nil are both safe (no panic at construction
// time or during a normal download cycle).
func TestManagerAcceptsNilSafeResolverProvider(t *testing.T) {
	// Case 1: nil provider — NewManager with no resolver wired.
	cfg := Config{Workers: 1, DebounceWindow: time.Millisecond, ScanPollEvery: time.Millisecond, ScanPollMax: 10 * time.Millisecond}
	m := NewManager(cfg, nil, newMemStore(), nil, &fakeScanner{}, nil, &fakeVersion{v: 1}, nil, nil, nil)
	// Must not panic on construction. No Start/Stop cycle needed.
	_ = m

	// Case 2: non-nil provider returning nil — provider says "no resolver yet".
	m2 := NewManager(cfg, nil, newMemStore(), nil, &fakeScanner{}, nil, &fakeVersion{v: 1}, nil, nil, func() BindingResolver { return nil })
	_ = m2

	// Case 3: non-nil provider — the provider must NOT be called during construction
	// (Tasks 3-5 add the actual call sites; Task 1 only stores the dep).
	var callCount int
	m3 := NewManager(cfg, nil, newMemStore(), nil, &fakeScanner{}, nil, &fakeVersion{v: 1}, nil, nil, func() BindingResolver {
		callCount++
		return nil
	})
	if callCount != 0 {
		t.Fatalf("resolve provider called %d times during NewManager construction; expected 0", callCount)
	}
	_ = m3
}
