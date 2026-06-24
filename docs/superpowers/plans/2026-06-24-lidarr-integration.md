# Lidarr Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Lidarr as a second, opt-in downloader that acquires a track's whole album asynchronously, via a reusable async-downloader capability + a background reconciler, plus a default-downloader admin setting.

**Architecture:** A new optional `AsyncDownloader` interface lets an adapter hand a request to an external manager (Lidarr) and report progress by polling instead of blocking. The `Manager` grows an async lane in `Enqueue` (Submit → persist a ref → never pin a worker) and a reconciler goroutine (poll in-flight async jobs → map state/progress → fire the existing scan+rematch on import). The Lidarr adapter resolves track→album via Lidarr's lookup API, adds the artist UNMONITORED + monitors/searches only the target album, and polls Lidarr's queue/album for state. spotDL is untouched.

**Tech Stack:** Go (chi, modernc sqlite, sqlc, goose, EventBus), React 19 + TS, Vite, Tailwind (design tokens), TanStack Query, Zustand, Vitest, Playwright.

## Global Constraints

- **Branch:** all work on `feat/lidarr-integration` (already created off local `main`). Never commit on `main`. Never `git push` — the user pushes + rebuilds.
- **Generated code:** `internal/store/db/*` is sqlc-generated. Edit `internal/store/queries/*.sql`, then run `make gen`. Never hand-edit `internal/store/db/`. Goose migrations live in `internal/store/migrations/` (latest is `0011`; add `0012`, never edit applied ones).
- **Lidarr is opt-in:** the Lidarr adapter's `CanDownload` returns **false** so it is NEVER chosen by the auto fallback chain or batch/playlist imports — only when explicitly picked (`req.Downloader == "lidarr"`).
- **No discography grab:** when submitting, the Lidarr artist is added **UNMONITORED** (`monitored:false`, `addOptions.monitor:"none"`); ONLY the requested album is monitored + searched.
- **Async never pins a worker:** an async `Submit` returns a ref; the reconciler advances the job. The sync worker pool + pause gate are for spotDL only.
- **Design tokens only** (frontend): no raw hex, no `text-black`/`text-white`. Use token classes (`bg-surface`/`bg-raised`/`bg-raised-hover`/`bg-input`, `border-border-subtle`, `text-text-primary`/`-secondary`/`-muted`, `bg-accent` + `text-on-accent`, `text-success`/`-warning`/`-error`).
- **Lidarr API field names:** the Lidarr client below uses concrete Lidarr v1 API endpoints + struct fields. Where a field name could differ by Lidarr version, the step says so — verify against your Lidarr's actual `/api/v1/...` JSON during implementation; tests run against a fake `Doer` with the canned JSON given here, so the unit tests are deterministic regardless.
- **Commit footer:** end every commit message with these two trailer lines exactly:
  ```
  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_012VxPKRqALKUo3uoatQSwWu
  ```
- **The gate (green before merge):** repo root `go test ./... && go build ./... && go vet ./...`; `web/` `npx vitest run && npx tsc --noEmit && npm run build && npm run e2e` (e2e stays **3/3**).
- **`NewManager` signature (unchanged):** `NewManager(cfg Config, downloaders []Downloader, store JobStore, bus Publisher, scanner ScanController, rematcher Rematcher, version VersionBumper, clock Clock, playlists PlaylistAdder) *Manager`.

---

## Task 1: `downloader_ref` column + store plumbing

**Files:**
- Create: `internal/store/migrations/0012_download_jobs_downloader_ref.sql`
- Modify: `internal/store/queries/download_jobs.sql` (add `downloader_ref` to selects + Insert; new `UpdateDownloadJobRef`)
- Generated: `internal/store/db/download_jobs.sql.go` (via `make gen`)
- Modify: `internal/core/download.go` (add `DownloaderRef` field)
- Modify: `internal/download/manager.go` (add `UpdateRef` to the `JobStore` interface)
- Modify: `internal/download/sqlstore.go` (rowFields + toCoreFlatRow + from*Row + Insert + `UpdateRef`)
- Modify: `internal/download/manager_test.go` (memStore: `UpdateRef`; preserve ref)
- Test: `internal/download/sqlstore_test.go`

**Interfaces:**
- Produces: `core.DownloadJob.DownloaderRef string`; `JobStore.UpdateRef(ctx, id, ref string) error`; generated `db.Queries.UpdateDownloadJobRef`.

- [ ] **Step 1: Add the migration**

Create `internal/store/migrations/0012_download_jobs_downloader_ref.sql`:

```sql
-- +goose Up
ALTER TABLE download_jobs ADD COLUMN downloader_ref TEXT NOT NULL DEFAULT '';

-- +goose Down
-- SQLite does not support DROP COLUMN on older versions; leave the column in place.
-- The column has a NOT NULL default and no index, so reverting the application is safe.
```

- [ ] **Step 2: Add `downloader_ref` to the queries**

In `internal/store/queries/download_jobs.sql`: add `downloader_ref` to the column list of **InsertDownloadJob** (and a `?` value), and to the SELECT column lists of **GetDownloadJob**, **GetActiveDownloadJobByDedup**, **ListDownloadJobs**, **ListDownloadJobsByStatus**. Then append a new query.

InsertDownloadJob becomes:
```sql
-- name: InsertDownloadJob :exec
INSERT INTO download_jobs (
    id, dedup_key, request_json, downloader_name, status, progress, error,
    output_path, library_track_id, priority, requested_by, attempts, downloader_ref,
    created_at, started_at, finished_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, unixepoch(), NULL, NULL);
```
Each SELECT adds `downloader_ref` to its column list, e.g. GetDownloadJob:
```sql
-- name: GetDownloadJob :one
SELECT id, dedup_key, request_json, downloader_name, status, progress, error,
       output_path, library_track_id, cover_art_id, priority, requested_by, attempts,
       downloader_ref, created_at, started_at, finished_at
FROM download_jobs WHERE id = ?;
```
(Do the same `downloader_ref` addition to the SELECT lists of `GetActiveDownloadJobByDedup`, `ListDownloadJobs`, `ListDownloadJobsByStatus`.) Append:
```sql
-- name: UpdateDownloadJobRef :exec
UPDATE download_jobs SET downloader_ref = ? WHERE id = ?;
```

- [ ] **Step 3: Regenerate sqlc**

Run: `make gen`
Expected: succeeds; the generated row structs (`GetDownloadJobRow`, etc.) gain a `DownloaderRef string` field; `InsertDownloadJobParams` gains `DownloaderRef string`; `UpdateDownloadJobRef(ctx, UpdateDownloadJobRefParams{DownloaderRef, ID})` exists. Confirm: `grep -n "DownloaderRef" internal/store/db/download_jobs.sql.go`

- [ ] **Step 4: Add the core field**

In `internal/core/download.go`, add to `DownloadJob` (after `ExternalID`, near the carried request fields):
```go
	// DownloaderRef is downloader-internal handle for async downloaders (e.g. the
	// Lidarr album id). Empty for synchronous downloaders like spotDL.
	DownloaderRef string `json:"downloaderRef,omitempty"`
```

- [ ] **Step 5: Extend the JobStore interface + sqlStore**

In `internal/download/manager.go`, add to the `JobStore` interface (after `DeleteFinished`):
```go
	// UpdateRef persists the downloader-internal ref (e.g. Lidarr album id) for a
	// job, used by async downloaders after Submit.
	UpdateRef(ctx context.Context, id string, ref string) error
```

In `internal/download/sqlstore.go`: add `downloaderRef string` to `rowFields`; in `toCoreFlatRow` add `j.DownloaderRef = r.downloaderRef` (after `j.Attempts`); in each of `fromGetRow`/`fromGetDedupRow`/`fromListRow` add `downloaderRef: r.DownloaderRef`; in `Insert` add `DownloaderRef: j.DownloaderRef,` to `InsertDownloadJobParams`; and add the method:
```go
func (s *sqlStore) UpdateRef(ctx context.Context, id string, ref string) error {
	return s.q.UpdateDownloadJobRef(ctx, db.UpdateDownloadJobRefParams{DownloaderRef: ref, ID: id})
}
```

- [ ] **Step 6: Add memStore.UpdateRef (test fake) + write the failing round-trip test**

In `internal/download/manager_test.go`, add (after `memStore.DeleteFinished`):
```go
func (s *memStore) UpdateRef(_ context.Context, id string, ref string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j, ok := s.jobs[id]; ok {
		j.DownloaderRef = ref
		s.jobs[id] = j
	}
	return nil
}
```

Append to `internal/download/sqlstore_test.go`:
```go
func TestSQLStoreDownloaderRefRoundTrip(t *testing.T) {
	s := newSQLStore(t)
	ctx := context.Background()
	job := core.DownloadJob{ID: "r1", DedupKey: "dk", Status: core.DownloadRunning, DownloaderName: "lidarr", Source: "spotify", ExternalID: "e1"}
	if err := s.Insert(ctx, job, core.DownloadRequest{Source: "spotify", ExternalID: "e1", Album: "Discovery", Artist: "Daft Punk"}); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := s.Get(ctx, "r1"); got.DownloaderRef != "" {
		t.Fatalf("new job should have empty ref, got %q", got.DownloaderRef)
	}
	if err := s.UpdateRef(ctx, "r1", "lidarr-album-42"); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Get(ctx, "r1")
	if err != nil || !ok {
		t.Fatalf("get: %v ok=%v", err, ok)
	}
	if got.DownloaderRef != "lidarr-album-42" {
		t.Fatalf("ref not persisted: %q", got.DownloaderRef)
	}
}
```

- [ ] **Step 7: Run the test, expect PASS; build the package**

Run: `go test ./internal/download/ -run 'TestSQLStoreDownloaderRefRoundTrip|TestSQLStoreInsertGetUpdate' -v && go build ./...`
Expected: PASS, build clean.

- [ ] **Step 8: Commit**

```bash
git add internal/store/migrations/0012_download_jobs_downloader_ref.sql internal/store/queries/download_jobs.sql internal/store/db/ internal/core/download.go internal/download/manager.go internal/download/sqlstore.go internal/download/manager_test.go internal/download/sqlstore_test.go
git commit -m "feat(downloads): downloader_ref column for async downloader handles"
```

---

## Task 2: `AsyncDownloader` capability + Manager async lane

**Files:**
- Modify: `internal/download/download.go` (add `AsyncDownloader` + `AsyncStatus`)
- Modify: `internal/download/manager.go` (`Enqueue` async branch + `submitAsync` + `asyncFor` + `Cancel` async branch)
- Test: `internal/download/manager_test.go`

**Interfaces:**
- Consumes: `JobStore.UpdateRef` (Task 1).
- Produces: `download.AsyncDownloader` interface (`Submit`/`Poll`/`CancelAsync`), `download.AsyncStatus`, `Manager.asyncFor(name) AsyncDownloader`.

- [ ] **Step 1: Define the interface**

In `internal/download/download.go`, after the `Downloader` interface, add:
```go
// AsyncDownloader is an OPTIONAL capability. An adapter implementing it hands the
// request to an external manager (e.g. Lidarr) and reports progress by polling,
// instead of blocking in Start. The Manager detects it via a type assertion and
// runs such jobs on the reconciler lane (never pinning a worker). Detected for the
// admin UI via the registry capability probe "async".
type AsyncDownloader interface {
	// Submit hands req to the external system and returns a ref to track it. Must
	// NOT block on completion. An error means the request couldn't be placed (e.g.
	// album not found) → the job fails.
	Submit(ctx context.Context, req core.DownloadRequest) (ref string, err error)

	// Poll reports the current state of a submitted job. State == DownloadCompleted
	// means the files were imported into the library folder (the Manager then runs
	// the normal scan + rematch). State == DownloadFailed carries Error. Otherwise
	// the job is still running; Progress is 0-100 or -1 (unknown).
	Poll(ctx context.Context, ref string) (AsyncStatus, error)

	// CancelAsync best-effort abandons the external job.
	CancelAsync(ctx context.Context, ref string) error
}

// AsyncStatus is the polled state of an async download.
type AsyncStatus struct {
	State    core.DownloadStatus
	Progress int
	Error    string
}
```

- [ ] **Step 2: Write the failing test**

Append to `internal/download/manager_test.go`:
```go
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

func (d *fakeAsyncDL) Type() string                         { return "downloader" }
func (d *fakeAsyncDL) Name() string                         { return d.name }
func (d *fakeAsyncDL) ConfigSchema() registry.ConfigSchema  { return registry.ConfigSchema{} }
func (d *fakeAsyncDL) Init(map[string]any) error            { return nil }
func (d *fakeAsyncDL) TestConnection(context.Context) error { return nil }
func (d *fakeAsyncDL) CanDownload(context.Context, core.DownloadRequest) (bool, error) {
	return false, nil
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
		Album: "Discovery", Downloader: "lidarr",
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
		Source: "spotify", ExternalID: "e1", Artist: "X", Title: "Y", Album: "Z", Downloader: "lidarr",
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
		Source: "spotify", ExternalID: "e1", Artist: "X", Title: "Y", Album: "Z", Downloader: "lidarr",
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
```

- [ ] **Step 3: Run, expect FAIL**

Run: `go test ./internal/download/ -run 'TestEnqueueAsync|TestCancelAsyncJob' -v`
Expected: FAIL (compile error: `AsyncDownloader`/async lane not implemented).

- [ ] **Step 4: Add the async lane to the Manager**

In `internal/download/manager.go`:

(a) In `Enqueue`, replace the final dispatch block (the `m.mu.Unlock()` at line ~345 through `return job, nil`) with:
```go
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
```

(b) Add these methods (near `pick`):
```go
// asyncFor returns the AsyncDownloader registered under name, or nil if that
// downloader isn't registered or isn't async.
func (m *Manager) asyncFor(name string) AsyncDownloader {
	for _, d := range m.downloaders {
		if d.Name() == name {
			if a, ok := d.(AsyncDownloader); ok {
				return a
			}
		}
	}
	return nil
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
```

(c) In `Cancel`, after the `if !ok { return ... }` for the store Get and BEFORE the `if job.Status == core.DownloadQueued` block, add the async branch:
```go
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
```

- [ ] **Step 5: Run, expect PASS**

Run: `go test ./internal/download/ -run 'TestEnqueueAsync|TestCancelAsyncJob' -v && go test ./internal/download/`
Expected: PASS (and existing manager tests still green — spotDL is sync, so its path is unchanged).

- [ ] **Step 6: Commit**

```bash
git add internal/download/download.go internal/download/manager.go internal/download/manager_test.go
git commit -m "feat(downloads): AsyncDownloader capability + Manager async submit/cancel lane"
```

---

## Task 3: Manager reconciler

**Files:**
- Modify: `internal/download/manager.go` (`Config` gets `ReconcileEvery`/`AsyncMaxAge`; `reconcileOnce`, `reconcileLoop`, `hasAsync`; `Start` launches the loop)
- Test: `internal/download/manager_test.go`

**Interfaces:**
- Consumes: `AsyncDownloader.Poll` (Task 2), `scheduleScan` (existing).
- Produces: `Manager.reconcileOnce(ctx)` (test-drivable), the reconciler loop (started in `Start`).

- [ ] **Step 1: Write the failing test**

Append to `internal/download/manager_test.go`:
```go
func TestReconcileAdvancesProgressThenCompletes(t *testing.T) {
	clk := newFakeClock()
	async := &fakeAsyncDL{name: "lidarr", submitRef: "album-9"}
	store := newMemStore()
	scanner := &fakeScanner{}
	bus := events.New()
	m := NewManager(Config{Workers: 1, DebounceWindow: time.Second, ScanPollEvery: time.Millisecond, ScanPollMax: time.Second, ScanSettleMax: 10 * time.Millisecond},
		[]Downloader{async}, store, bus, scanner, &fakeRematcher{trackID: "t1"}, &fakeVersion{v: 1}, clk, nil)
	t.Cleanup(m.Stop)
	m.Start()

	job, _ := m.Enqueue(context.Background(), core.DownloadRequest{
		Source: "spotify", ExternalID: "e1", Artist: "A", Title: "T", Album: "Al", Downloader: "lidarr",
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
	job, _ := m.Enqueue(context.Background(), core.DownloadRequest{Source: "spotify", ExternalID: "e", Artist: "A", Title: "T", Album: "Al", Downloader: "lidarr"})

	async.setStatus(AsyncStatus{State: core.DownloadFailed, Error: "Lidarr found no release"})
	m.reconcileOnce(context.Background())
	got, _, _ := store.Get(context.Background(), job.ID)
	if got.Status != core.DownloadFailed || got.Error != "Lidarr found no release" {
		t.Fatalf("job = %+v, want failed with reason", got)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

Run: `go test ./internal/download/ -run 'TestReconcile' -v`
Expected: FAIL — `m.reconcileOnce` undefined.

- [ ] **Step 3: Add reconciler config defaults**

In `internal/download/manager.go`, add to `Config` (after `JobTimeout`):
```go
	// ReconcileEvery is the poll cadence for async (e.g. Lidarr) jobs.
	ReconcileEvery time.Duration
	// AsyncMaxAge bounds how long an async job may stay in-flight before it's
	// failed (Lidarr never found/imported a release).
	AsyncMaxAge time.Duration
```
And in `withDefaults`, before `return c`:
```go
	if c.ReconcileEvery <= 0 {
		c.ReconcileEvery = 10 * time.Second
	}
	if c.AsyncMaxAge <= 0 {
		c.AsyncMaxAge = 7 * 24 * time.Hour
	}
```

- [ ] **Step 4: Add the reconciler + launch it in Start**

In `internal/download/manager.go`, add (after `submitAsync`):
```go
// hasAsync reports whether any configured downloader is an AsyncDownloader.
func (m *Manager) hasAsync() bool {
	for _, d := range m.downloaders {
		if _, ok := d.(AsyncDownloader); ok {
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
```

In `Start`, after the worker launch loop and before `go m.backfillUnlinked()`, add:
```go
	if m.hasAsync() {
		m.wg.Add(1)
		go m.reconcileLoop()
		log.Printf("download manager: async reconciler started (every %s)", m.cfg.ReconcileEvery)
	}
```

- [ ] **Step 5: Run, expect PASS; full package + race**

Run: `go test ./internal/download/ -run 'TestReconcile' -v && go test -race ./internal/download/`
Expected: PASS; no data races (the reconciler reads shared state through the store + m.mu like the rest of the Manager).

- [ ] **Step 6: Commit**

```bash
git add internal/download/manager.go internal/download/manager_test.go
git commit -m "feat(downloads): async reconciler — poll in-flight jobs, scan on import"
```

---

## Task 4: Lidarr HTTP client

**Files:**
- Create: `internal/download/lidarr/client.go`
- Test: `internal/download/lidarr/client_test.go`

**Interfaces:**
- Produces: `lidarr.Client` with `New(base, key string, http Doer) *Client`, and methods `SystemStatus`, `LookupAlbum`, `GetArtistByForeignID`, `AddArtist`, `GetAlbumsByArtist`, `MonitorAlbum`, `SearchAlbum`, `GetAlbum`, `GetQueueForAlbum`. Types `AlbumResult`, `ArtistRef`, `AlbumStatistics`, `QueueRecord`.

> **Lidarr API note:** endpoints are Lidarr v1 (`/api/v1/...`, auth via `X-Api-Key` header). The struct fields below cover the subset used. If a field name differs in your Lidarr version, adjust — the tests below pin behavior against canned JSON via a fake `Doer`, so they pass regardless.

- [ ] **Step 1: Write the failing test**

Create `internal/download/lidarr/client_test.go`:
```go
package lidarr

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// fakeDoer routes requests to canned responses by method+path substring.
type fakeDoer struct {
	routes map[string]string // "METHOD /path" substring → JSON body
	lastBodies map[string]string
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	if f.lastBodies == nil {
		f.lastBodies = map[string]string{}
	}
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		f.lastBodies[req.Method+" "+req.URL.Path] = string(b)
	}
	key := req.Method + " " + req.URL.Path
	// Longest-prefix match so "GET /api/v1/album/lookup" beats "GET /api/v1/album".
	best, bestLen := "", -1
	for k, v := range f.routes {
		if strings.HasPrefix(key, k) && len(k) > bestLen {
			best, bestLen = v, len(k)
		}
	}
	if bestLen >= 0 {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(best)), Header: http.Header{}}, nil
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("[]")), Header: http.Header{}}, nil
}

func TestLookupAlbumParsesResults(t *testing.T) {
	doer := &fakeDoer{routes: map[string]string{
		"GET /api/v1/album/lookup": `[{"title":"Discovery","foreignAlbumId":"mb-album-1","artist":{"artistName":"Daft Punk","foreignArtistId":"mb-artist-1"}}]`,
	}}
	c := NewClient("http://lidarr:8686", "key", doer)
	res, err := c.LookupAlbum(context.Background(), "Daft Punk Discovery")
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].ForeignAlbumID != "mb-album-1" || res[0].Artist.ForeignArtistID != "mb-artist-1" {
		t.Fatalf("lookup = %+v", res)
	}
}

func TestSystemStatusOK(t *testing.T) {
	doer := &fakeDoer{routes: map[string]string{"GET /api/v1/system/status": `{"version":"2.0.0"}`}}
	c := NewClient("http://lidarr:8686", "key", doer)
	if err := c.SystemStatus(context.Background()); err != nil {
		t.Fatalf("SystemStatus: %v", err)
	}
}

func TestSearchAlbumSendsCommand(t *testing.T) {
	doer := &fakeDoer{routes: map[string]string{"POST /api/v1/command": `{"id":1}`}}
	c := NewClient("http://lidarr:8686", "key", doer)
	if err := c.SearchAlbum(context.Background(), 42); err != nil {
		t.Fatal(err)
	}
	body := doer.lastBodies["POST /api/v1/command"]
	if !strings.Contains(body, "AlbumSearch") || !strings.Contains(body, "42") {
		t.Fatalf("command body = %s", body)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

Run: `go test ./internal/download/lidarr/ -v`
Expected: FAIL — package/types not defined.

- [ ] **Step 3: Implement the client**

Create `internal/download/lidarr/client.go`:
```go
// Package lidarr is the Lidarr downloader adapter + a thin Lidarr v1 REST client.
// Lidarr acquires music asynchronously at the album level; this client drives the
// add-artist-unmonitored → monitor-album → search → poll flow used by the adapter.
package lidarr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Doer is the http.Client seam (injectable for tests).
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client is a thin Lidarr v1 API client.
type Client struct {
	base string
	key  string
	http Doer
}

// NewClient constructs a Client. base is the Lidarr URL (e.g. http://lidarr:8686).
func NewClient(base, key string, http Doer) *Client {
	return &Client{base: strings.TrimRight(base, "/"), key: key, http: http}
}

// --- API types (subset used) ---

type ArtistRef struct {
	ID              int    `json:"id"`
	ArtistName      string `json:"artistName"`
	ForeignArtistID string `json:"foreignArtistId"`
	QualityProfileID int   `json:"qualityProfileId,omitempty"`
	MetadataProfileID int  `json:"metadataProfileId,omitempty"`
	RootFolderPath  string `json:"rootFolderPath,omitempty"`
	Monitored       bool   `json:"monitored"`
}

type AlbumResult struct {
	ID             int             `json:"id"`
	Title          string          `json:"title"`
	ForeignAlbumID string          `json:"foreignAlbumId"`
	Monitored      bool            `json:"monitored"`
	Artist         ArtistRef       `json:"artist"`
	Statistics     AlbumStatistics `json:"statistics"`
}

type AlbumStatistics struct {
	TrackCount     int `json:"trackCount"`
	TrackFileCount int `json:"trackFileCount"`
}

type QueueRecord struct {
	AlbumID               int    `json:"albumId"`
	Size                  float64 `json:"size"`
	Sizeleft              float64 `json:"sizeleft"`
	Status                string `json:"status"`
	TrackedDownloadStatus string `json:"trackedDownloadStatus"`
	ErrorMessage          string `json:"errorMessage"`
}

type queuePage struct {
	Records []QueueRecord `json:"records"`
}

// --- request helpers ---

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", c.key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("lidarr %s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("lidarr %s %s: decode: %w", method, path, err)
		}
	}
	return nil
}

// SystemStatus pings Lidarr to validate URL + API key.
func (c *Client) SystemStatus(ctx context.Context) error {
	var out map[string]any
	return c.do(ctx, http.MethodGet, "/api/v1/system/status", nil, &out)
}

// LookupAlbum searches Lidarr's metadata (MusicBrainz) for an album by free text.
func (c *Client) LookupAlbum(ctx context.Context, term string) ([]AlbumResult, error) {
	var out []AlbumResult
	err := c.do(ctx, http.MethodGet, "/api/v1/album/lookup?term="+url.QueryEscape(term), nil, &out)
	return out, err
}

// GetArtistByForeignID returns the existing Lidarr artist with the given
// MusicBrainz id, or (ArtistRef{}, false) if not yet added.
func (c *Client) GetArtistByForeignID(ctx context.Context, foreignID string) (ArtistRef, bool, error) {
	var out []ArtistRef
	if err := c.do(ctx, http.MethodGet, "/api/v1/artist", nil, &out); err != nil {
		return ArtistRef{}, false, err
	}
	for _, a := range out {
		if a.ForeignArtistID == foreignID {
			return a, true, nil
		}
	}
	return ArtistRef{}, false, nil
}

// addArtistBody is the POST /api/v1/artist payload. The artist is added
// UNMONITORED (monitored:false + addOptions.monitor:"none") so Lidarr does not
// chase the whole discography.
type addArtistBody struct {
	ArtistName        string         `json:"artistName"`
	ForeignArtistID   string         `json:"foreignArtistId"`
	QualityProfileID  int            `json:"qualityProfileId"`
	MetadataProfileID int            `json:"metadataProfileId"`
	RootFolderPath    string         `json:"rootFolderPath"`
	Monitored         bool           `json:"monitored"`
	AddOptions        addArtistAddOpts `json:"addOptions"`
}
type addArtistAddOpts struct {
	Monitor                string `json:"monitor"`
	SearchForMissingAlbums bool   `json:"searchForMissingAlbums"`
}

// AddArtist adds an artist UNMONITORED and returns the created artist (with its
// Lidarr id). Caller supplies the resolved foreign id from a prior LookupAlbum.
func (c *Client) AddArtist(ctx context.Context, a ArtistRef, rootFolder string, qualityProfileID, metadataProfileID int) (ArtistRef, error) {
	body := addArtistBody{
		ArtistName:        a.ArtistName,
		ForeignArtistID:   a.ForeignArtistID,
		QualityProfileID:  qualityProfileID,
		MetadataProfileID: metadataProfileID,
		RootFolderPath:    rootFolder,
		Monitored:         false,
		AddOptions:        addArtistAddOpts{Monitor: "none", SearchForMissingAlbums: false},
	}
	var out ArtistRef
	err := c.do(ctx, http.MethodPost, "/api/v1/artist", body, &out)
	return out, err
}

// GetAlbumsByArtist lists a Lidarr artist's albums (after the artist is added,
// Lidarr fetches them unmonitored).
func (c *Client) GetAlbumsByArtist(ctx context.Context, artistID int) ([]AlbumResult, error) {
	var out []AlbumResult
	err := c.do(ctx, http.MethodGet, "/api/v1/album?artistId="+strconv.Itoa(artistID), nil, &out)
	return out, err
}

// MonitorAlbum sets a single album monitored=true.
func (c *Client) MonitorAlbum(ctx context.Context, albumID int) error {
	body := map[string]any{"albumIds": []int{albumID}, "monitored": true}
	return c.do(ctx, http.MethodPut, "/api/v1/album/monitor", body, nil)
}

// SearchAlbum triggers an AlbumSearch command for one album.
func (c *Client) SearchAlbum(ctx context.Context, albumID int) error {
	body := map[string]any{"name": "AlbumSearch", "albumIds": []int{albumID}}
	return c.do(ctx, http.MethodPost, "/api/v1/command", body, nil)
}

// GetAlbum returns one album (with statistics) by Lidarr id.
func (c *Client) GetAlbum(ctx context.Context, albumID int) (AlbumResult, error) {
	var out AlbumResult
	err := c.do(ctx, http.MethodGet, "/api/v1/album/"+strconv.Itoa(albumID), nil, &out)
	return out, err
}

// GetQueueForAlbum returns active queue records for an album (download progress).
func (c *Client) GetQueueForAlbum(ctx context.Context, albumID int) ([]QueueRecord, error) {
	var page queuePage
	if err := c.do(ctx, http.MethodGet, "/api/v1/queue?pageSize=100", nil, &page); err != nil {
		return nil, err
	}
	var out []QueueRecord
	for _, r := range page.Records {
		if r.AlbumID == albumID {
			out = append(out, r)
		}
	}
	return out, nil
}

// RemoveAlbumFromQueue best-effort cancels by unmonitoring the album so Lidarr
// stops chasing it (drops its wanted status).
func (c *Client) RemoveAlbumFromQueue(ctx context.Context, albumID int) error {
	return c.do(ctx, http.MethodPut, "/api/v1/album/monitor", map[string]any{"albumIds": []int{albumID}, "monitored": false}, nil)
}
```

- [ ] **Step 4: Run, expect PASS**

Run: `go test ./internal/download/lidarr/ -v && go vet ./internal/download/lidarr/`
Expected: PASS, vet clean.

- [ ] **Step 5: Commit**

```bash
git add internal/download/lidarr/client.go internal/download/lidarr/client_test.go
git commit -m "feat(lidarr): Lidarr v1 REST client (lookup/add/monitor/search/queue)"
```

---

## Task 5: Lidarr adapter

**Files:**
- Create: `internal/download/lidarr/adapter.go`
- Test: `internal/download/lidarr/adapter_test.go`

**Interfaces:**
- Consumes: the `lidarr.Client` (Task 4); `download.AsyncDownloader`/`AsyncStatus` (Task 2).
- Produces: `lidarr.New() *Adapter` implementing `registry.Plugin` + `download.Downloader` (`CanDownload` false) + `download.AsyncDownloader`.

- [ ] **Step 1: Write the failing test**

Create `internal/download/lidarr/adapter_test.go`:
```go
package lidarr

import (
	"context"
	"strings"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/download"
)

// compile-time: the adapter is a Downloader AND an AsyncDownloader.
var _ download.Downloader = (*Adapter)(nil)
var _ download.AsyncDownloader = (*Adapter)(nil)

func newTestAdapter(t *testing.T, doer *fakeDoer) *Adapter {
	t.Helper()
	a := New()
	if err := a.Init(map[string]any{
		"url": "http://lidarr:8686", "api_key": "k", "root_folder": "/music",
		"quality_profile_id": float64(1), "metadata_profile_id": float64(1),
	}); err != nil {
		t.Fatal(err)
	}
	a.client = NewClientFor(a, doer) // inject the fake Doer (test seam)
	return a
}

func TestCanDownloadIsFalseOptInOnly(t *testing.T) {
	a := New()
	ok, _ := a.CanDownload(context.Background(), core.DownloadRequest{Artist: "A", Title: "T", Album: "Al"})
	if ok {
		t.Fatal("Lidarr CanDownload must be false (opt-in only)")
	}
}

func TestSubmitResolvesAddsMonitorsSearches(t *testing.T) {
	doer := &fakeDoer{routes: map[string]string{
		"GET /api/v1/album/lookup": `[{"title":"Discovery","foreignAlbumId":"mb-al","artist":{"artistName":"Daft Punk","foreignArtistId":"mb-ar"}}]`,
		"GET /api/v1/artist":       `[]`,
		"POST /api/v1/artist":      `{"id":7,"artistName":"Daft Punk","foreignArtistId":"mb-ar"}`,
		"GET /api/v1/album":        `[{"id":42,"title":"Discovery","foreignAlbumId":"mb-al"}]`,
		"PUT /api/v1/album/monitor": `{}`,
		"POST /api/v1/command":     `{"id":1}`,
	}}
	a := newTestAdapter(t, doer)
	ref, err := a.Submit(context.Background(), core.DownloadRequest{Artist: "Daft Punk", Album: "Discovery", Title: "One More Time"})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if ref != "42" {
		t.Fatalf("ref = %q, want 42 (the Lidarr album id)", ref)
	}
	// Artist added UNMONITORED (no discography grab).
	body := doer.lastBodies["POST /api/v1/artist"]
	if !strings.Contains(body, `"monitored":false`) || !strings.Contains(body, `"monitor":"none"`) {
		t.Fatalf("artist must be added unmonitored, body = %s", body)
	}
	// Only the target album searched.
	if !strings.Contains(doer.lastBodies["POST /api/v1/command"], "AlbumSearch") {
		t.Fatal("expected AlbumSearch command")
	}
}

func TestSubmitNoMatchFails(t *testing.T) {
	doer := &fakeDoer{routes: map[string]string{"GET /api/v1/album/lookup": `[]`}}
	a := newTestAdapter(t, doer)
	_, err := a.Submit(context.Background(), core.DownloadRequest{Artist: "Nobody", Album: "Nothing", Title: "X"})
	if err == nil {
		t.Fatal("Submit with no lookup match must error")
	}
}

func TestPollMapsImportedToCompleted(t *testing.T) {
	doer := &fakeDoer{routes: map[string]string{
		"GET /api/v1/album/42": `{"id":42,"statistics":{"trackCount":12,"trackFileCount":12}}`,
		"GET /api/v1/queue":    `{"records":[]}`,
	}}
	a := newTestAdapter(t, doer)
	st, err := a.Poll(context.Background(), "42")
	if err != nil {
		t.Fatal(err)
	}
	if st.State != core.DownloadCompleted {
		t.Fatalf("state = %s, want completed", st.State)
	}
}

func TestPollMapsDownloadingToRunningProgress(t *testing.T) {
	doer := &fakeDoer{routes: map[string]string{
		"GET /api/v1/album/42": `{"id":42,"statistics":{"trackCount":12,"trackFileCount":0}}`,
		"GET /api/v1/queue":    `{"records":[{"albumId":42,"size":100,"sizeleft":40,"status":"downloading","trackedDownloadStatus":"ok"}]}`,
	}}
	a := newTestAdapter(t, doer)
	st, _ := a.Poll(context.Background(), "42")
	if st.State != core.DownloadRunning || st.Progress != 60 {
		t.Fatalf("state/progress = %s/%d, want running/60", st.State, st.Progress)
	}
}
```

- [ ] **Step 2: Run, expect FAIL**

Run: `go test ./internal/download/lidarr/ -run 'TestCanDownload|TestSubmit|TestPoll' -v`
Expected: FAIL — adapter not implemented.

- [ ] **Step 3: Implement the adapter**

Create `internal/download/lidarr/adapter.go`:
```go
package lidarr

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/registry"
)

// Adapter implements download.Downloader (opt-in: CanDownload=false) and
// download.AsyncDownloader for Lidarr.
type Adapter struct {
	url              string
	apiKey           string
	rootFolder       string
	qualityProfileID int
	metadataProfileID int
	client           *Client
}

func New() *Adapter { return &Adapter{} }

// NewClientFor builds a Client bound to a's config with the given Doer (test seam).
func NewClientFor(a *Adapter, doer Doer) *Client { return NewClient(a.url, a.apiKey, doer) }

func (a *Adapter) Type() string { return "downloader" }
func (a *Adapter) Name() string { return "lidarr" }

func (a *Adapter) ConfigSchema() registry.ConfigSchema {
	return registry.ConfigSchema{Fields: []registry.ConfigField{
		{Key: "url", Label: "Lidarr URL", Type: "string", Required: true},
		{Key: "api_key", Label: "API key", Type: "string", Required: true, Secret: true},
		{Key: "root_folder", Label: "Root folder path", Type: "string", Required: true},
		{Key: "quality_profile_id", Label: "Quality profile ID", Type: "number", Required: true},
		{Key: "metadata_profile_id", Label: "Metadata profile ID", Type: "number", Required: true},
	}}
}

// asInt coerces a JSON config value (float64 from encoding/json, or string) to int.
func asInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case string:
		i, _ := strconv.Atoi(n)
		return i
	}
	return 0
}

func (a *Adapter) Init(cfg map[string]any) error {
	if v, ok := cfg["url"].(string); ok {
		a.url = v
	}
	if v, ok := cfg["api_key"].(string); ok {
		a.apiKey = v
	}
	if v, ok := cfg["root_folder"].(string); ok {
		a.rootFolder = v
	}
	a.qualityProfileID = asInt(cfg["quality_profile_id"])
	a.metadataProfileID = asInt(cfg["metadata_profile_id"])
	if a.url == "" || a.apiKey == "" || a.rootFolder == "" {
		return fmt.Errorf("lidarr: url, api_key and root_folder are required")
	}
	if a.qualityProfileID == 0 {
		return fmt.Errorf("lidarr: quality_profile_id is required")
	}
	if a.client == nil {
		a.client = NewClient(a.url, a.apiKey, &http.Client{Timeout: 30 * time.Second})
	}
	return nil
}

// TestConnection validates URL + API key by pinging system/status.
func (a *Adapter) TestConnection(ctx context.Context) error {
	return a.client.SystemStatus(ctx)
}

// CanDownload returns FALSE: Lidarr is opt-in only, never chosen by the auto
// fallback chain or batch/playlist imports.
func (a *Adapter) CanDownload(ctx context.Context, req core.DownloadRequest) (bool, error) {
	return false, nil
}

// Start is never called for an async downloader (the Manager uses Submit/Poll).
func (a *Adapter) Start(ctx context.Context, req core.DownloadRequest, onProgress func(int)) (string, error) {
	return "", fmt.Errorf("lidarr: Start is not used (async downloader)")
}

// Submit resolves the album, adds the artist UNMONITORED, monitors+searches only
// the target album, and returns the Lidarr album id as the ref.
func (a *Adapter) Submit(ctx context.Context, req core.DownloadRequest) (string, error) {
	if req.Album == "" || req.Artist == "" {
		return "", fmt.Errorf("lidarr needs an artist and album (couldn't map %q)", req.Title)
	}
	results, err := a.client.LookupAlbum(ctx, req.Artist+" "+req.Album)
	if err != nil {
		return "", fmt.Errorf("lidarr lookup: %w", err)
	}
	if len(results) == 0 {
		return "", fmt.Errorf("couldn't find %q by %q in Lidarr", req.Album, req.Artist)
	}
	top := results[0]

	// Ensure the artist exists (added UNMONITORED).
	artist, exists, err := a.client.GetArtistByForeignID(ctx, top.Artist.ForeignArtistID)
	if err != nil {
		return "", fmt.Errorf("lidarr artist lookup: %w", err)
	}
	if !exists {
		artist, err = a.client.AddArtist(ctx, top.Artist, a.rootFolder, a.qualityProfileID, a.metadataProfileID)
		if err != nil {
			return "", fmt.Errorf("lidarr add artist: %w", err)
		}
	}

	// Find the target album among the artist's albums (Lidarr assigns it an id).
	albums, err := a.client.GetAlbumsByArtist(ctx, artist.ID)
	if err != nil {
		return "", fmt.Errorf("lidarr list albums: %w", err)
	}
	albumID := 0
	for _, al := range albums {
		if al.ForeignAlbumID == top.ForeignAlbumID {
			albumID = al.ID
			break
		}
	}
	if albumID == 0 {
		return "", fmt.Errorf("lidarr: album %q not found under artist after add", req.Album)
	}

	if err := a.client.MonitorAlbum(ctx, albumID); err != nil {
		return "", fmt.Errorf("lidarr monitor album: %w", err)
	}
	if err := a.client.SearchAlbum(ctx, albumID); err != nil {
		return "", fmt.Errorf("lidarr album search: %w", err)
	}
	return strconv.Itoa(albumID), nil
}

// Poll maps Lidarr's album/queue state onto an AsyncStatus.
func (a *Adapter) Poll(ctx context.Context, ref string) (AsyncStatus, error) {
	albumID, err := strconv.Atoi(ref)
	if err != nil {
		return AsyncStatus{}, fmt.Errorf("lidarr: bad ref %q", ref)
	}
	album, err := a.client.GetAlbum(ctx, albumID)
	if err != nil {
		return AsyncStatus{}, err
	}
	// Fully imported → completed.
	if album.Statistics.TrackCount > 0 && album.Statistics.TrackFileCount >= album.Statistics.TrackCount {
		return AsyncStatus{State: core.DownloadCompleted, Progress: 100}, nil
	}
	// Otherwise inspect the queue for download progress / errors.
	records, err := a.client.GetQueueForAlbum(ctx, albumID)
	if err != nil {
		return AsyncStatus{}, err
	}
	for _, r := range records {
		if r.TrackedDownloadStatus == "error" {
			msg := r.ErrorMessage
			if msg == "" {
				msg = "Lidarr download error"
			}
			return AsyncStatus{State: core.DownloadFailed, Error: msg}, nil
		}
		if r.Size > 0 {
			pct := int(100 * (r.Size - r.Sizeleft) / r.Size)
			if pct < 0 {
				pct = 0
			}
			if pct > 100 {
				pct = 100
			}
			return AsyncStatus{State: core.DownloadRunning, Progress: pct}, nil
		}
	}
	// No queue item yet — still searching.
	return AsyncStatus{State: core.DownloadRunning, Progress: -1}, nil
}

// CancelAsync best-effort unmonitors the album so Lidarr stops chasing it.
func (a *Adapter) CancelAsync(ctx context.Context, ref string) error {
	albumID, err := strconv.Atoi(ref)
	if err != nil {
		return nil
	}
	return a.client.RemoveAlbumFromQueue(ctx, albumID)
}

// compile-time assertions live in adapter_test.go (download import there).
```

Note on the test seam: `Init` builds a real `*http.Client`-backed `Client`; the test calls `a.client = NewClientFor(a, doer)` after `Init` to swap in the fake `Doer`. (`a.client` is set before the real one in `Init` only if already non-nil — confirm `Init` keeps an injected client. In the code above, `Init` sets `a.client` only when `a.client == nil`; the test sets it AFTER `Init`, so adjust: in the test helper, call `a.Init(...)` then overwrite `a.client`. The code above already supports this — `Init` won't overwrite a nil→real, and the test overwrites afterward.)

- [ ] **Step 4: Run, expect PASS**

Run: `go test ./internal/download/lidarr/ -v && go vet ./internal/download/lidarr/`
Expected: PASS, vet clean.

- [ ] **Step 5: Commit**

```bash
git add internal/download/lidarr/adapter.go internal/download/lidarr/adapter_test.go
git commit -m "feat(lidarr): Lidarr adapter — opt-in async downloader (submit/poll/cancel)"
```

---

## Task 6: Register Lidarr + the async capability probe

**Files:**
- Modify: `cmd/reverb/main.go` (register `lidarr` + `RegisterCapability("async", ...)`)
- Test: `cmd/reverb/` has no unit tests for registration; verify via build + a registry probe test in the download package.

**Interfaces:**
- Consumes: the lidarr adapter (Task 5), `registry.RegisterCapability` (existing).

- [ ] **Step 1: Write a probe test (download package)**

Append to `internal/download/manager_test.go`:
```go
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
```

- [ ] **Step 2: Run, expect FAIL or PASS**

Run: `go test ./internal/download/ -run 'TestAsyncCapabilityProbe' -v`
Expected: PASS (the probe is registered inside the test). This test documents + locks the probe contract used by main.go. (If `registry` isn't imported in the test file, it already is — `fakeDL.ConfigSchema` returns `registry.ConfigSchema{}`.)

- [ ] **Step 3: Register Lidarr + the probe in main.go**

In `cmd/reverb/main.go`, add the import `"github.com/maxjb-xyz/reverb/internal/download/lidarr"` and, right after the `downloaderReg.Register("spotdl", ...)` line, add:
```go
	downloaderReg.Register("lidarr", func() registry.Plugin { return lidarr.New() })
	// Surface the async capability to the admin UI (/adapters/available).
	registry.RegisterCapability("async", func(p registry.Plugin) bool {
		_, ok := p.(download.AsyncDownloader)
		return ok
	})
```

- [ ] **Step 4: Build + verify the whole thing wires**

Run: `go build ./... && go vet ./... && go test ./internal/download/ ./cmd/...`
Expected: all green. (`internal/wiring.BuildDownloaders` already instantiates any enabled downloader instance by name via the registry — `lidarr` works with no wiring change, since its config comes from `config_json`; no env overrides are needed for Lidarr.)

- [ ] **Step 5: Commit**

```bash
git add cmd/reverb/main.go internal/download/manager_test.go
git commit -m "feat(lidarr): register adapter + async registry capability probe"
```

---

## Task 7: `default_downloader` setting (backend)

**Files:**
- Modify: `internal/api/settings.go` (DTO + key + read + validate + persist)
- Test: `internal/api/settings_test.go` (create if absent, else append)

**Interfaces:**
- Consumes: `s.deps.Downloader` (the downloader registry — has `Names()`), `AdapterStore.GetSetting/UpsertSetting` (existing).
- Produces: `settingsDTO.DefaultDownloader`, accepted via `putSettingsBody.DefaultDownloader`.

- [ ] **Step 1: Write the failing test**

Append to `internal/api/settings_test.go` (mirror the existing settings/test-server helpers in that file or `downloads_test.go`; the snippet below uses `NewServer(Deps{...})` with an `Adapters` store + a `Downloader` registry):
```go
func TestDefaultDownloaderSetting(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	authSvc := auth.NewService(st.Q(), time.Now)
	_ = authSvc.SetAdminPassword(context.Background(), "pw")
	tok, _ := authSvc.CreateSession(context.Background())
	reg := registry.NewRegistry("downloader")
	reg.Register("spotdl", func() registry.Plugin { return nil })
	reg.Register("lidarr", func() registry.Plugin { return nil })
	srv := NewServer(Deps{Auth: authSvc, Adapters: st.Q(), Downloader: reg})
	cookie := &http.Cookie{Name: sessionCookie, Value: tok}

	put := func(body string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", bytes.NewBufferString(body))
		req.AddCookie(cookie)
		srv.Handler().ServeHTTP(rec, req)
		return rec.Code
	}
	// Valid: a registered downloader name.
	if code := put(`{"defaultDownloader":"lidarr"}`); code != http.StatusOK {
		t.Fatalf("set valid default = %d", code)
	}
	// Valid: empty = "Always ask".
	if code := put(`{"defaultDownloader":""}`); code != http.StatusOK {
		t.Fatalf("clear default = %d", code)
	}
	// Invalid: unknown downloader → 400.
	if code := put(`{"defaultDownloader":"bogus"}`); code != http.StatusBadRequest {
		t.Fatalf("unknown default = %d, want 400", code)
	}
	// GET reflects the last valid set ("").
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	var dto struct {
		DefaultDownloader string `json:"defaultDownloader"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &dto)
	if dto.DefaultDownloader != "" {
		t.Fatalf("default = %q, want empty", dto.DefaultDownloader)
	}
}
```
(Ensure the test file imports: `bytes`, `context`, `encoding/json`, `net/http`, `net/http/httptest`, `testing`, `time`, and the `auth`, `registry`, `store` packages — match the imports already used by `downloads_test.go`.)

- [ ] **Step 2: Run, expect FAIL**

Run: `go test ./internal/api/ -run 'TestDefaultDownloaderSetting' -v`
Expected: FAIL — `defaultDownloader` not handled.

- [ ] **Step 3: Implement in settings.go**

In `internal/api/settings.go`:

(a) Add the key const:
```go
	keyDefaultDownloader = "default_downloader"
```
(b) Add to `settingsDTO`:
```go
	DefaultDownloader string `json:"defaultDownloader"`
```
(c) In `currentSettings`, before `return out`, add:
```go
	if v, err := s.deps.Adapters.GetSetting(r.Context(), keyDefaultDownloader); err == nil {
		out.DefaultDownloader = v
	}
```
(d) Add to `putSettingsBody`:
```go
	DefaultDownloader *string `json:"defaultDownloader"`
```
(e) In `handlePutSettings`, before the final `writeJSON(...)`, add:
```go
	if body.DefaultDownloader != nil {
		name := *body.DefaultDownloader
		if name != "" && !s.downloaderRegistered(name) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "defaultDownloader must be empty or a registered downloader"})
			return
		}
		if err := s.deps.Adapters.UpsertSetting(r.Context(), db.UpsertSettingParams{Key: keyDefaultDownloader, Value: name}); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not save settings"})
			return
		}
	}
```
(f) Add the helper (anywhere in settings.go):
```go
// downloaderRegistered reports whether name is a registered downloader adapter.
func (s *Server) downloaderRegistered(name string) bool {
	if s.deps.Downloader == nil {
		return false
	}
	for _, n := range s.deps.Downloader.Names() {
		if n == name {
			return true
		}
	}
	return false
}
```
(If `s.deps.Downloader` isn't already a field on `Deps`, it is — `main.go` sets `Downloader: downloaderReg` and `downloads_test.go` passes `Downloader: registry.NewRegistry("downloader")`. Confirm the `Deps.Downloader` type exposes `Names()`; `*registry.Registry` does.)

- [ ] **Step 4: Run, expect PASS**

Run: `go test ./internal/api/ -run 'TestDefaultDownloaderSetting' -v && go test ./internal/api/`
Expected: PASS, no regressions.

- [ ] **Step 5: Commit**

```bash
git add internal/api/settings.go internal/api/settings_test.go
git commit -m "feat(settings): default_downloader setting (validated against registered downloaders)"
```

---

## Task 8: FE — `defaultDownloader` in settings + Settings UI select

**Files:**
- Modify: `web/src/lib/settingsApi.ts` (`AppSettings.defaultDownloader`)
- Modify: `web/src/routes/Settings.tsx` (a "Default downloader" row)
- Test: `web/src/routes/Settings.test.tsx`

**Interfaces:**
- Consumes: `useAdapters` (`adaptersApi.ts`), `useSettings`/`useUpdateSettings` (`settingsApi.ts`), `Select` (`components/ui`).

- [ ] **Step 1: Add the type field**

In `web/src/lib/settingsApi.ts`, extend `AppSettings`:
```ts
export interface AppSettings {
  accentColor: string
  dynamicBackground: boolean
  defaultDownloader: string
}
```

- [ ] **Step 2: Write the failing test**

Create/append `web/src/routes/Settings.test.tsx` with a focused case (mirror the file's existing render + QueryClient harness if present; otherwise wrap in a `QueryClientProvider`):
```tsx
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import Settings from './Settings'

vi.mock('../lib/adaptersApi', () => ({
  useAdapters: () => ({
    data: [
      { id: 'a1', type: 'downloader', name: 'spotdl', enabled: true, priority: 1, config: {} },
      { id: 'a2', type: 'downloader', name: 'lidarr', enabled: true, priority: 2, config: {} },
    ],
  }),
}))
const mutate = vi.fn()
vi.mock('../lib/settingsApi', async (orig) => {
  const actual = await orig<typeof import('../lib/settingsApi')>()
  return {
    ...actual,
    useSettings: () => ({ data: { accentColor: '#F0354B', dynamicBackground: true, defaultDownloader: '' } }),
    useUpdateSettings: () => ({ mutate }),
  }
})

function renderSettings() {
  const qc = new QueryClient()
  return render(<QueryClientProvider client={qc}><Settings /></QueryClientProvider>)
}

describe('Settings default downloader', () => {
  beforeEach(() => mutate.mockClear())
  it('shows a Default downloader select and saves the choice', () => {
    renderSettings()
    const select = screen.getByLabelText('Default downloader')
    fireEvent.change(select, { target: { value: 'lidarr' } })
    expect(mutate).toHaveBeenCalledWith({ defaultDownloader: 'lidarr' })
  })
})
```

- [ ] **Step 3: Run, expect FAIL**

Run (from `web/`): `npx vitest run src/routes/Settings.test.tsx`
Expected: FAIL — no "Default downloader" control.

- [ ] **Step 4: Add the Settings row**

In `web/src/routes/Settings.tsx`:
- Add imports: `import { Select } from '../components/ui'` (add to the existing ui import) and `import { useAdapters } from '../lib/adaptersApi'`.
- In the component, after `const updateSettings = useUpdateSettings()`, add:
```tsx
  const adapters = useAdapters()
  const downloaders = (adapters.data ?? [])
    .filter((a) => a.type === 'downloader' && a.enabled)
    .sort((a, b) => a.priority - b.priority)
  const downloaderOptions = [
    { value: '', label: 'Always ask' },
    ...downloaders.map((d) => ({ value: d.name, label: d.name })),
  ]
```
- In the **Appearance** tab's `divide-y` block (after the "Dynamic album background" row, before the Theme row), add a new row:
```tsx
          {/* Default downloader row */}
          <div className="flex items-center gap-5 py-5">
            <div className="flex-1 min-w-0">
              <div className="text-sm font-bold text-text-primary">Default downloader</div>
              <div className="text-xs text-text-secondary mt-0.5">
                Skip the picker and use this downloader for one-click downloads. &ldquo;Always ask&rdquo; shows the picker when more than one is enabled.
              </div>
            </div>
            <div className="flex-none">
              <Select
                label="Default downloader"
                value={settings.data?.defaultDownloader ?? ''}
                options={downloaderOptions}
                onChange={(v) => updateSettings.mutate({ defaultDownloader: v })}
              />
            </div>
          </div>
```

- [ ] **Step 5: Run, expect PASS; typecheck**

Run (from `web/`): `npx vitest run src/routes/Settings.test.tsx && npx tsc --noEmit`
Expected: PASS, tsc clean.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/settingsApi.ts web/src/routes/Settings.tsx web/src/routes/Settings.test.tsx
git commit -m "feat(web): Default downloader setting in Settings"
```

---

## Task 9: FE — picker default/override + Lidarr album disclosure

**Files:**
- Modify: `web/src/components/download/DownloadAction.tsx`
- Test: `web/src/components/download/DownloadAction.test.tsx`

**Interfaces:**
- Consumes: `useSettings` (`settingsApi.ts`), `useDownloaders` (local), `DownloadPopover` (existing), the local `enqueue` helper.

Behavior to add to the "Available (≥1 downloader, no active job)" branch (state 6):
- Read `defaultDownloader` from settings. With ≥2 downloaders and a valid (still-enabled) default, a normal click enqueues the default directly; a **split-button caret** opens the popover to override. With no/disabled default → click opens the popover (today's behavior). With exactly 1 downloader → click enqueues it (unchanged).
- **Lidarr disclosure:** when the chosen downloader (default or picked) is `lidarr`, show a confirm first ("Lidarr fetches the whole album …") before enqueueing.

- [ ] **Step 1: Write the failing tests**

In `web/src/components/download/DownloadAction.test.tsx`:

(a) Add a `useSettings` mock (default = `spotdl`) alongside the file's existing `vi.mock` calls:
```tsx
vi.mock('../../lib/settingsApi', () => ({
  useSettings: () => ({ data: { accentColor: '#F0354B', dynamicBackground: true, defaultDownloader: 'spotdl' } }),
}))
```

(b) Add these two cases. They require `useAdapters` to return TWO enabled downloaders (`spotdl` + `lidarr`) — reuse the file's existing 2-downloader adapters mock (the file already has a spotdl+lidarr case). `postDownloadMock` is the file's existing mock/spy of `postDownload` (it already observes downloads); reuse it.
```tsx
it('default downloader: a normal click enqueues spotdl directly (no popover)', () => {
  const result = { source: 'spotify', externalId: 'e1', title: 'T', artist: 'A', album: 'Al' } as never
  render(<DownloadAction result={result} />)
  fireEvent.click(screen.getByRole('button', { name: 'Download T' }))
  expect(postDownloadMock).toHaveBeenCalledWith(expect.objectContaining({ downloader: 'spotdl' }))
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
})

it('override caret → pick Lidarr → album disclosure → confirm enqueues lidarr', () => {
  const result = { source: 'spotify', externalId: 'e2', title: 'T2', artist: 'A', album: 'Discovery' } as never
  render(<DownloadAction result={result} />)
  // The split-button caret opens the picker even though a default is set.
  fireEvent.click(screen.getByRole('button', { name: 'Choose downloader' }))
  fireEvent.click(screen.getByRole('button', { name: 'lidarr' }))
  // Lidarr routes through the album disclosure — NOT enqueued yet.
  expect(screen.getByText(/whole album/i)).toBeInTheDocument()
  expect(screen.getByText(/Discovery/)).toBeInTheDocument()
  expect(postDownloadMock).not.toHaveBeenCalledWith(expect.objectContaining({ downloader: 'lidarr' }))
  // Confirm → enqueues via lidarr.
  fireEvent.click(screen.getByRole('button', { name: 'Confirm Lidarr album download' }))
  expect(postDownloadMock).toHaveBeenCalledWith(expect.objectContaining({ downloader: 'lidarr' }))
})
```
(Match the file's existing imports — it already imports `render`, `screen`, `fireEvent`, `vi`, `DownloadAction`, and observes `postDownload`. If the file observes `postDownload` under a different local name than `postDownloadMock`, use that name.)

- [ ] **Step 2: Run, expect FAIL**

Run (from `web/`): `npx vitest run src/components/download/DownloadAction.test.tsx`
Expected: FAIL — default path + disclosure not implemented.

- [ ] **Step 3: Implement the default + override + disclosure**

In `web/src/components/download/DownloadAction.tsx`:
- Add import: `import { useSettings } from '../../lib/settingsApi'`.
- In the component, read the default + add a disclosure state:
```tsx
  const settings = useSettings()
  const defaultDownloader = settings.data?.defaultDownloader ?? ''
  // Lidarr (any async/album-level downloader) gets an explicit album disclosure.
  const [pendingLidarr, setPendingLidarr] = useState<string | null>(null) // downloader name awaiting confirm
```
- Add a helper that enqueues but routes Lidarr through the disclosure:
```tsx
  function chooseDownloader(name: string) {
    if (name === 'lidarr') {
      setPendingLidarr(name)
      return
    }
    enqueue(name)
  }
```
- Replace `handleDownloadClick` so it honors the default:
```tsx
  function handleDownloadClick(e: React.MouseEvent) {
    e.stopPropagation()
    if (downloaders.length === 1) {
      chooseDownloader(downloaders[0].name)
      return
    }
    // ≥2 downloaders: use the default if it's set AND still enabled, else ask.
    const def = downloaders.find((d) => d.name === defaultDownloader)
    if (def) {
      chooseDownloader(def.name)
    } else {
      setPopoverOpen(true)
    }
  }
```
- Update `handlePick` to route through the disclosure too:
```tsx
  function handlePick(name: string) {
    setPopoverOpen(false)
    chooseDownloader(name)
  }
```
- In the state-6 return (Available), add a split-button caret next to the Download button (only when ≥2 downloaders and a default is active) and the disclosure modal. Replace the state-6 `return (...)` with:
```tsx
  const hasActiveDefault = downloaders.length > 1 && downloaders.some((d) => d.name === defaultDownloader)
  return (
    <span className="relative inline-flex items-center justify-end gap-1">
      <Button
        variant="secondary"
        size="sm"
        aria-label={`Download ${result.title}`}
        onClick={handleDownloadClick}
      >
        Download
      </Button>

      {hasActiveDefault && (
        <button
          type="button"
          aria-label="Choose downloader"
          onClick={(e) => { e.stopPropagation(); setPopoverOpen(true) }}
          className="inline-grid h-7 w-6 place-items-center rounded-full border border-border-subtle text-text-secondary transition-colors hover:bg-raised-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
        >
          <Icon name="dl" className="text-xs" />
        </button>
      )}

      {popoverOpen && (
        <DownloadPopover
          downloaders={downloaders.map((d) => ({ id: d.id, name: d.name }))}
          trackTitle={result.title}
          onPick={handlePick}
          onClose={() => setPopoverOpen(false)}
        />
      )}

      {pendingLidarr &&
        createPortal(
          <>
            <div className="fixed inset-0 z-40" aria-hidden="true" onClick={() => setPendingLidarr(null)} />
            <div
              role="dialog"
              aria-modal="true"
              aria-label="Confirm Lidarr download"
              className="fixed left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 z-50 w-80 max-w-[calc(100vw-2rem)] rounded-xl border border-border-subtle bg-raised p-4 shadow-pop"
              onClick={(e) => e.stopPropagation()}
            >
              <p className="text-sm font-bold text-text-primary">Download the whole album?</p>
              <p className="mt-1 text-xs text-text-secondary">
                Lidarr fetches the full album{result.album ? ` “${result.album}”` : ''}, not just “{result.title}”.
              </p>
              <div className="mt-3 flex justify-end gap-2">
                <Button variant="ghost" size="sm" aria-label="Cancel" onClick={() => setPendingLidarr(null)}>
                  Cancel
                </Button>
                <Button
                  variant="primary"
                  size="sm"
                  aria-label="Confirm Lidarr album download"
                  onClick={() => { const n = pendingLidarr; setPendingLidarr(null); enqueue(n!) }}
                >
                  Download album
                </Button>
              </div>
            </div>
          </>,
          document.body,
        )}
    </span>
  )
```
(`createPortal` and `Icon` are already imported in this file; `useState` too.)

- [ ] **Step 4: Run, expect PASS; typecheck + build**

Run (from `web/`): `npx vitest run src/components/download/ && npx tsc --noEmit && npm run build`
Expected: PASS, tsc clean, build succeeds.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/download/DownloadAction.tsx web/src/components/download/DownloadAction.test.tsx
git commit -m "feat(web): default-downloader one-click + override caret + Lidarr album disclosure"
```

---

## Task 10: Whole-branch verification & gate

**Files:** none (verification only).

- [ ] **Step 1: Backend gate**

Run (repo root): `go test ./... && go build ./... && go vet ./...`
Expected: all green.

- [ ] **Step 2: Backend race check on the download package** (async + reconciler are concurrent)

Run: `go test -race ./internal/download/...`
Expected: no data races.

- [ ] **Step 3: Frontend gate**

Run (from `web/`): `npx vitest run && npx tsc --noEmit && npm run build`
Expected: all green (`npm run build` must pass — it catches breakage tsc misses).

- [ ] **Step 4: e2e (stays 3/3)**

Run (from `web/`): `npm run e2e`
Expected: **3/3** pass. The picker change defaults to the existing single-downloader behavior in the e2e (the e2e configures only spotDL via mocks, so `downloaders.length === 1` → unchanged path). Do not add a 4th spec.

- [ ] **Step 5: Manual smoke notes for the user**

After the user pushes + rebuilds (`soulkiller:8090`): add a Lidarr downloader in Admin (URL + API key + root folder = the music folder + quality/metadata profile IDs); set a Default downloader in Settings; download a track via Lidarr → confirm the album disclosure → watch the job track Lidarr's progress in `/downloads` → on Lidarr import the track flips to owned. (Diagnose via the Claude-in-Chrome tools against the live session; a fix isn't live until the rebuild.)

- [ ] **Step 6: Final whole-branch review**

Invoke `superpowers:requesting-code-review` for the whole branch diff before merge. Address Critical/Important findings, re-run the full gate, then fast-forward merge `feat/lidarr-integration` → local `main` (the user pushes).

---

## Notes & gotchas (read before starting)
- **`make gen` required** after editing `download_jobs.sql` (Task 1) — the `DownloaderRef`/`UpdateDownloadJobRef` generated symbols don't exist until you regenerate.
- **spotDL untouched**: it's a plain sync `Downloader` (no `AsyncDownloader`), so the worker pool, pause gate, and existing tests are unaffected. The reconciler only starts when an async downloader is configured.
- **Submit runs in the enqueue HTTP path** (a few Lidarr calls, ~seconds). That's acceptable — the LONG part (the actual download, minutes-to-hours) is async via the reconciler. If Lidarr lookups prove slow in practice, moving `submitAsync` to a goroutine is a later, isolated change.
- **Lidarr API field names**: verify against your Lidarr version's live JSON during Task 4/5; the unit tests use canned JSON via a fake `Doer` so they're deterministic regardless. `TestConnection` gives runtime validation of URL+key.
- **No discography grab**: the artist is added `monitored:false` + `addOptions.monitor:"none"`; only the target album is monitored + searched. Task 5's `TestSubmitResolvesAddsMonitorsSearches` asserts this.
- **Lidarr jobs surface in the Phase-2-B queue UI for free** (they're `DownloadJob`s with live progress from the reconciler); cancel/clear work via the existing controls.
