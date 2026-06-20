# Crate M3 — Download Loop Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Each task is a self-contained unit: a fresh implementer with ZERO prior context can complete it from the file paths, interfaces, and complete code given here. Tasks are ordered domain types → Downloader interface/conformance → store → Manager (dedup/fallback/debounce/cancel/retry) → spotDL adapter → API REST + WS → composition → frontend WS client → downloadStore → DownloadTray → ExternalRow ⟳/↓/✓ + playWhenReady → app-shell wiring → smoke.

**Goal:** Close the core loop — a user clicks download on a not-in-library "Everywhere" result, the job queues, progress streams live to a Download Tray and an in-place ⟳ ring on the result row, the track appears in the library, the row flips to ✓, and (if requested) it auto-plays. This is the download spine: download domain types → `Downloader` interface (+conformance) → `download_jobs` store → `download.Manager` (dedup-join, fallback chain, scan-debounce, cancel/retry, EventBus events) → spotDL adapter (injectable exec runner, graceful stdout-parse degradation) → REST endpoints + a NEW WebSocket endpoint streaming typed events → composition wiring → frontend `RealtimeConnection` (WS, distinct from SSE) + `downloadStore` + `DownloadTray` + functional `ExternalRow` ↓/⟳/✓ + `playWhenReady` auto-play + surgical TanStack Query invalidation.

**Architecture:** Builds on M0 (binary serving `/api/v1` + embedded SPA), M1 (`core`, `library` + Subsonic adapter incl. `StartScan`/`ScanStatus`, stream proxy, frontend player + `uiStore` right-panel slot), and M2 (`search` + Spotify adapter + SSE aggregator, `matching` + `Normalize` + `match_cache`/`library_version`, frontend SSE `SearchStream` + Everywhere UI + `ExternalRow` with the M3 download seam marked). M3 adds a `download` package (`Downloader` interface + conformance + `Manager`), a `download/spotdl` adapter, a `0003` migration with `download_jobs` + sqlc queries, REST download endpoints + a WebSocket endpoint on the existing chi router, explicit downloader registration + Manager wiring at the composition root, and a frontend WS `RealtimeConnection` + `downloadStore` + `DownloadTray` + functional `ExternalRow` states. Library data is NEVER persisted — `download_jobs` stores only Crate job state; on completion the Manager re-matches via the existing `MatchingService` and bumps `library_version` (invalidating `match_cache`). The WebSocket is a DISTINCT transport from the SSE search stream.

**Tech Stack:** Go 1.23 (toolchain present), chi v5, `net/http`, `net/http/httptest`, `os/exec` (behind an injectable `Runner`), `github.com/coder/websocket v1.8.15` (the WS library, pinned), `github.com/google/uuid` (already a dependency, for job IDs), sqlc v1.31.1 (installed) for `download_jobs` queries. React 19, TypeScript ~6, Vite 8, Vitest 4, Tailwind 3.4, React Router 6, TanStack Query 5, Zustand 4 (all already in `web/`); browser-native `WebSocket` for the realtime transport (stubbed in tests, no real network/media).

## Global Constraints

- Go module `github.com/maximusjb/crate`; Go 1.23; SQLite modernc only.
- dedup_key = hash of matching.Normalize(artist)+sep+Normalize(title)+sep+Normalize(album) — REUSE matching.Normalize (single source of truth). In-flight/queued same-key → join.
- Fallback chain: iterate enabled downloaders by priority via CanDownload; configurable.
- spotDL via injectable exec runner; stdout parse degrades gracefully (unknown progress, never error); version-pinned (comment + doc).
- Scan debounce ~5s with an INJECTABLE clock (tests don't wait real time); coalesce → one StartScan; then poll getScanStatus → re-match → set library_track_id + bump library_version (invalidates match_cache).
- library data never persisted; download_jobs stores job state only.
- WS is a DISTINCT transport from SSE; typed event payloads carry IDs for surgical invalidation; backoff reconnect; auth-gated (cookie). Pin ONE ws library (`github.com/coder/websocket v1.8.15`).
- Downloaders registered EXPLICITLY at the composition root; per-source init failures warn-and-skip.
- Result row ⟳ state cross-references active jobs by externalId+source; in-place ✓ flip on complete; the M2 download seam becomes functional now.
- Tests: Go via fakes (injectable exec runner, injectable clock for debounce, fake downloader/library/matcher, in-memory store or temp DB); `go test ./cmd/... ./internal/...`; the Manager's concurrency tested with -race-worthy tests (dedup-join, fallback, debounce-coalesce, cancel/retry); WS handler testable (httptest + a ws client or a fake); frontend Vitest stubbing WebSocket (no real network/media). Every downloader passes conformance.
- TDD always: failing test → confirm red → minimal code → confirm green → conventional-commit. Run Go tests with `go test ./cmd/... ./internal/...` (NOT `./...`). Frontend Vitest with stubbed `WebSocket`/`fetch`; typecheck via `cd web && npm run build`. sqlc generated code is committed; regenerate via the installed `sqlc` binary (fallback `go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate`).

---

## File Structure

**Go (backend) — created/modified in M3:**

| Path | Responsibility |
|---|---|
| `internal/core/download.go` | NEW: `DownloadRequest`, `DownloadJob`, `DownloadStatus` enum, event payload types (`DownloadEvent`, `LibraryUpdatedEvent`). JSON camelCase. |
| `internal/core/download_test.go` | NEW: JSON round-trip + camelCase key + status-constant assertions. |
| `internal/download/download.go` | NEW: `Downloader` interface (embeds `registry.Plugin`) + topic constants. |
| `internal/download/conformance.go` | NEW: `RunConformance(t, Downloader)`. |
| `internal/download/conformance_test.go` | NEW: a fake downloader proving conformance passes. |
| `internal/download/dedup.go` | NEW: `DedupKey(req)` using `matching.Normalize`. |
| `internal/download/dedup_test.go` | NEW: dedup-key stability + normalization tests. |
| `internal/download/manager.go` | NEW: `Manager` (queue, worker pool, dedup-join, fallback chain, scan-debounce w/ injectable clock, cancel/retry, EventBus events, re-match + version bump). |
| `internal/download/manager_test.go` | NEW: dedup-join, fallback, debounce-coalesce, cancel/retry, completion→rematch→version-bump (fakes + injectable clock). |
| `internal/download/spotdl/runner.go` | NEW: `Runner` interface + real `os/exec` impl streaming stdout lines. |
| `internal/download/spotdl/adapter.go` | NEW: `Adapter` implementing `registry.Plugin` + `download.Downloader`; parses progress, degrades gracefully. |
| `internal/download/spotdl/adapter_test.go` | NEW: fake runner (canned stdout incl. malformed line), progress parse, CanDownload, conformance. |
| `internal/store/migrations/0003_download_jobs.sql` | NEW: additive migration creating `download_jobs`. |
| `internal/store/queries/download_jobs.sql` | NEW: insert/get/get-active-by-dedup/list/update-*/increment-attempts queries. |
| `internal/store/db/*` | REGENERATED by sqlc (committed). |
| `internal/api/downloads.go` | NEW: REST handlers (`POST /downloads`, `GET /downloads`, cancel, retry). |
| `internal/api/downloads_test.go` | NEW: handler tests (enqueue/join, list, cancel, retry, auth). |
| `internal/api/ws.go` | NEW: `GET /api/v1/ws` WebSocket endpoint subscribing the EventBus, streaming typed frames. |
| `internal/api/ws_test.go` | NEW: WS handler test (httptest + a ws client, receives a published event; auth-gated). |
| `internal/api/server.go` | MODIFY: add `Downloads` + `Events` to `Deps`; mount REST + WS routes. |
| `internal/api/auth_flow_test.go` | MODIFY (none required — new Deps fields are interfaces/pointers, zero value nil). |
| `internal/api/search_test.go` | MODIFY (none required — same). |
| `cmd/crate/download_wiring.go` | NEW: `buildDownloaders` (enabled `downloader` adapter_instances + env override) + `wireSpotdl` registration. |
| `cmd/crate/download_wiring_test.go` | NEW: env-override + enabled-filter + warn-and-skip tests. |
| `cmd/crate/main.go` | MODIFY: register spotdl factory, build downloaders, construct the Manager (worker count, deduper, library adapter, matching service, store, EventBus), wire Manager + EventBus into `api.Deps`. |
| `go.mod` / `go.sum` | MODIFY: add `github.com/coder/websocket v1.8.15`. |

**React (frontend) — created/modified in M3, under `web/`:**

| Path | Responsibility |
|---|---|
| `src/lib/types.ts` | MODIFY: add `DownloadJob`, `DownloadStatus`, `DownloadEvent`, `LibraryUpdatedEvent`, `RealtimeEvent`. |
| `src/lib/realtime.ts` | NEW: `RealtimeConnection` (browser `WebSocket` to `/api/v1/ws`, backoff reconnect + resubscribe, typed dispatch, `close()`). DISTINCT from SSE. |
| `src/lib/realtime.test.ts` | NEW: Vitest with a stubbed `WebSocket` (no real network). |
| `src/lib/downloadStore.ts` | NEW: Zustand store of jobs keyed by id, updated from WS events; active list + lookup by externalId+source. |
| `src/lib/downloadStore.test.ts` | NEW: reducer/store tests (upsert, status transitions, lookup, resync). |
| `src/lib/downloadApi.ts` | NEW: REST helpers (`postDownload`, `getDownloads`, `cancelDownload`, `retryDownload`) + TanStack invalidation helper. |
| `src/components/DownloadTray.tsx` | NEW: the second right-panel (uiStore 'downloads'); active/queued/done jobs + progress + cancel. |
| `src/components/DownloadTray.test.tsx` | NEW: RTL test (renders jobs from the store, cancel calls the API). |
| `src/components/ExternalRow.tsx` | MODIFY: not-in-library rows get a ↓ button (POST /downloads); active job → ⟳ ring (determinate/indeterminate); complete → ✓ flip + plays matched track. |
| `src/components/ExternalRow.test.tsx` | MODIFY: add ↓/⟳/✓ state tests (stubbed store + fetch). |
| `src/components/Sidebar.tsx` | MODIFY: add a ⟳ Downloads entry opening the tray. |
| `src/components/PlayerBar.tsx` | MODIFY: enable the Downloads button (toggles 'downloads'). |
| `src/components/AppShell.tsx` | MODIFY: render `DownloadTray` in the right slot; mount the WS connection + downloadStore wiring app-wide. |
| `src/lib/realtimeWiring.ts` | NEW: `useRealtime()` hook connecting `RealtimeConnection` to `downloadStore` + the QueryClient invalidation + `playWhenReady` auto-play. |

---

## Task 1: Download domain types (`internal/core/download.go`)

**Files:**
- Create: `internal/core/download.go`
- Test: `internal/core/download_test.go`

**Interfaces:**
- Consumes: nothing (pure types; uses `EntityType` from `core/types.go`).
- Produces (exact, consumed by `download`, `spotdl`, `api`, frontend mirror):
  ```go
  type DownloadStatus string
  const (
      DownloadQueued    DownloadStatus = "queued"
      DownloadRunning   DownloadStatus = "running"
      DownloadCompleted DownloadStatus = "completed"
      DownloadFailed    DownloadStatus = "failed"
      DownloadCanceled  DownloadStatus = "canceled"
  )
  type DownloadRequest struct {
      Source, ExternalID, Artist, Title, Album, ISRC string
      Downloader   string // optional explicit downloader name
      PlayWhenReady bool
  }
  type DownloadJob struct {
      ID, DedupKey string
      Status       DownloadStatus
      Progress     int // 0-100; -1 = unknown/indeterminate
      Error        string
      OutputPath   string
      LibraryTrackID string
      DownloaderName string
      Priority     int
      Attempts     int
      Source, ExternalID string
      PlayWhenReady bool
      CreatedAt, StartedAt, FinishedAt int64 // unix seconds; 0 = unset
  }
  // Event payloads (published on the EventBus, marshaled over WS).
  type DownloadEvent struct {
      JobID, DedupKey string
      Status DownloadStatus
      Progress int
      Error string
      Source, ExternalID string
      LibraryTrackID string
      ArtistID, AlbumID string
  }
  type LibraryUpdatedEvent struct {
      ArtistIDs, AlbumIDs []string
  }
  ```

- [ ] **Step 1: Write the failing test**

Create `internal/core/download_test.go`:
```go
package core

import (
	"encoding/json"
	"testing"
)

func TestDownloadJobJSONRoundTrip(t *testing.T) {
	in := DownloadJob{
		ID: "j1", DedupKey: "dk1", Status: DownloadRunning, Progress: 42,
		Error: "", OutputPath: "/music/x.mp3", LibraryTrackID: "t1",
		DownloaderName: "spotdl", Priority: 0, Attempts: 1,
		Source: "spotify", ExternalID: "sp1", PlayWhenReady: true,
		CreatedAt: 100, StartedAt: 110, FinishedAt: 0,
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out DownloadJob
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.ID != "j1" || out.Status != DownloadRunning || out.Progress != 42 || out.LibraryTrackID != "t1" {
		t.Fatalf("round trip mismatch: %+v", out)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for _, k := range []string{"id", "dedupKey", "status", "progress", "outputPath", "libraryTrackId", "downloaderName", "externalId", "playWhenReady", "createdAt"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("expected camelCase key %q, got %v", k, m)
		}
	}
}

func TestDownloadStatusConstants(t *testing.T) {
	if DownloadQueued != "queued" || DownloadRunning != "running" || DownloadCompleted != "completed" ||
		DownloadFailed != "failed" || DownloadCanceled != "canceled" {
		t.Fatal("download status constant drift")
	}
}

func TestDownloadEventCamelCase(t *testing.T) {
	b, _ := json.Marshal(DownloadEvent{
		JobID: "j1", DedupKey: "dk", Status: DownloadCompleted, Progress: 100,
		Source: "spotify", ExternalID: "sp1", LibraryTrackID: "t1", ArtistID: "ar1", AlbumID: "al1",
	})
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for _, k := range []string{"jobId", "dedupKey", "status", "progress", "source", "externalId", "libraryTrackId", "artistId", "albumId"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("DownloadEvent missing camelCase key %q: %v", k, m)
		}
	}
}

func TestLibraryUpdatedEventCamelCase(t *testing.T) {
	b, _ := json.Marshal(LibraryUpdatedEvent{ArtistIDs: []string{"ar1"}, AlbumIDs: []string{"al1"}})
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if _, ok := m["artistIds"]; !ok {
		t.Fatalf("missing artistIds: %v", m)
	}
	if _, ok := m["albumIds"]; !ok {
		t.Fatalf("missing albumIds: %v", m)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/ -run Download -v`
Expected: FAIL — `undefined: DownloadJob` / `undefined: DownloadRunning`.

- [ ] **Step 3: Write the implementation**

Create `internal/core/download.go`:
```go
package core

// DownloadStatus is the lifecycle state of a DownloadJob.
type DownloadStatus string

const (
	DownloadQueued    DownloadStatus = "queued"
	DownloadRunning   DownloadStatus = "running"
	DownloadCompleted DownloadStatus = "completed"
	DownloadFailed    DownloadStatus = "failed"
	DownloadCanceled  DownloadStatus = "canceled"
)

// DownloadRequest is built from an ExternalResult when the user clicks download.
// Downloader is optional (empty = let the Manager pick via the fallback chain).
type DownloadRequest struct {
	Source        string `json:"source"`
	ExternalID    string `json:"externalId"`
	Artist        string `json:"artist"`
	Title         string `json:"title"`
	Album         string `json:"album"`
	ISRC          string `json:"isrc,omitempty"`
	Downloader    string `json:"downloader,omitempty"`
	PlayWhenReady bool   `json:"playWhenReady"`
}

// DownloadJob is the persisted state of one download. Progress is 0-100, or -1
// when the downloader cannot report it (indeterminate ring on the client).
type DownloadJob struct {
	ID             string         `json:"id"`
	DedupKey       string         `json:"dedupKey"`
	Status         DownloadStatus `json:"status"`
	Progress       int            `json:"progress"`
	Error          string         `json:"error,omitempty"`
	OutputPath     string         `json:"outputPath,omitempty"`
	LibraryTrackID string         `json:"libraryTrackId,omitempty"`
	DownloaderName string         `json:"downloaderName"`
	Priority       int            `json:"priority"`
	Attempts       int            `json:"attempts"`
	Source         string         `json:"source"`
	ExternalID     string         `json:"externalId"`
	PlayWhenReady  bool           `json:"playWhenReady"`
	CreatedAt      int64          `json:"createdAt"`
	StartedAt      int64          `json:"startedAt"`
	FinishedAt     int64          `json:"finishedAt"`
}

// DownloadEvent is published on the EventBus (topics download.queued|progress|
// complete|failed) and marshaled over the WebSocket. ArtistID/AlbumID are set on
// completion for surgical client cache invalidation (empty when unknown).
type DownloadEvent struct {
	JobID          string         `json:"jobId"`
	DedupKey       string         `json:"dedupKey"`
	Status         DownloadStatus `json:"status"`
	Progress       int            `json:"progress"`
	Error          string         `json:"error,omitempty"`
	Source         string         `json:"source"`
	ExternalID     string         `json:"externalId"`
	LibraryTrackID string         `json:"libraryTrackId,omitempty"`
	ArtistID       string         `json:"artistId,omitempty"`
	AlbumID        string         `json:"albumId,omitempty"`
}

// LibraryUpdatedEvent is published on topic library.updated after a scan-driven
// re-match so the client can invalidate exactly the affected queries.
type LibraryUpdatedEvent struct {
	ArtistIDs []string `json:"artistIds"`
	AlbumIDs  []string `json:"albumIds"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/ -v`
Expected: PASS (existing M1/M2 core tests + the new Download tests).

- [ ] **Step 5: Commit**

```bash
git add internal/core/download.go internal/core/download_test.go
git commit -m "feat(core): download request, job, and event domain types"
```

---

## Task 2: Downloader interface + conformance suite (`internal/download`)

**Files:**
- Create: `internal/download/download.go`, `internal/download/conformance.go`
- Test: `internal/download/conformance_test.go`

**Interfaces:**
- Consumes: `internal/registry` (`registry.Plugin`), `internal/core`.
- Produces:
  ```go
  type Downloader interface {
      registry.Plugin
      // CanDownload is a CHEAP heuristic used in the search/fallback path; the
      // real check happens at Start. Never performs a network download.
      CanDownload(ctx context.Context, req core.DownloadRequest) (bool, error)
      // Start runs the download to completion (blocking). It reports progress via
      // onProgress (0-100, or -1 unknown) and returns the output path on success.
      // The ctx is cancelable (Cancel kills the in-flight exec).
      Start(ctx context.Context, req core.DownloadRequest, onProgress func(int)) (outputPath string, err error)
  }
  // Topic constants for EventBus events.
  const (
      TopicQueued        = "download.queued"
      TopicProgress      = "download.progress"
      TopicComplete      = "download.complete"
      TopicFailed        = "download.failed"
      TopicLibraryUpdate = "library.updated"
  )
  func RunConformance(t *testing.T, d Downloader)
  ```

> **Design note (spec §3 vs Manager):** the spec lists `Enqueue/Status/Cancel` on the `Downloader`, but those are queue/lifecycle concerns the `download.Manager` owns (it persists jobs and tracks status/cancellation). Per the prompt's "Enqueue/Start" wording, the adapter contract is the cheap `CanDownload` + a blocking `Start(ctx, req, onProgress)` whose ctx the Manager cancels. The Manager (Task 5) implements `Enqueue`/`Status`/`Cancel`/`Retry` at the orchestration layer. `QualityProfileDownloader`/`MonitoringDownloader` are P2 optional interfaces and are NOT defined here.

- [ ] **Step 1: Write the failing conformance test (with a fake downloader)**

Create `internal/download/conformance_test.go`:
```go
package download

import (
	"context"
	"testing"

	"github.com/maximusjb/crate/internal/core"
	"github.com/maximusjb/crate/internal/registry"
)

type fakeDownloader struct{}

func (fakeDownloader) Type() string                             { return "downloader" }
func (fakeDownloader) Name() string                             { return "fake" }
func (fakeDownloader) ConfigSchema() registry.ConfigSchema      { return registry.ConfigSchema{} }
func (fakeDownloader) Init(cfg map[string]any) error            { return nil }
func (fakeDownloader) TestConnection(ctx context.Context) error { return nil }
func (fakeDownloader) CanDownload(ctx context.Context, req core.DownloadRequest) (bool, error) {
	return req.Title != "", nil
}
func (fakeDownloader) Start(ctx context.Context, req core.DownloadRequest, onProgress func(int)) (string, error) {
	onProgress(50)
	onProgress(100)
	return "/out/" + req.ExternalID + ".mp3", nil
}

func TestFakeDownloaderConformance(t *testing.T) {
	RunConformance(t, fakeDownloader{})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/download/ -v`
Expected: FAIL — `undefined: RunConformance` / `undefined: Downloader`.

- [ ] **Step 3: Write the interface**

Create `internal/download/download.go`:
```go
// Package download defines the Downloader contract, a conformance suite, the
// dedup key, and the Manager (queue/workers/dedup-join/fallback/scan-debounce/
// cancel/retry). Adapters live in subpackages (e.g. download/spotdl).
package download

import (
	"context"

	"github.com/maximusjb/crate/internal/core"
	"github.com/maximusjb/crate/internal/registry"
)

// EventBus topics published by the Manager.
const (
	TopicQueued        = "download.queued"
	TopicProgress      = "download.progress"
	TopicComplete      = "download.complete"
	TopicFailed        = "download.failed"
	TopicLibraryUpdate = "library.updated"
)

// Downloader acquires an external track. CanDownload is a cheap heuristic for the
// fallback chain; Start performs the actual (blocking) download.
type Downloader interface {
	registry.Plugin

	// CanDownload is a cheap capability heuristic (no network download).
	CanDownload(ctx context.Context, req core.DownloadRequest) (bool, error)

	// Start runs the download to completion. It reports progress via onProgress
	// (0-100, or -1 when unknown) and returns the output path on success. ctx is
	// cancelable: when canceled, an in-flight download must abort promptly.
	Start(ctx context.Context, req core.DownloadRequest, onProgress func(int)) (outputPath string, err error)
}
```

- [ ] **Step 4: Write the conformance suite**

Create `internal/download/conformance.go`:
```go
package download

import (
	"context"
	"testing"

	"github.com/maximusjb/crate/internal/core"
)

// RunConformance exercises the Downloader contract. Call it from each adapter's
// test package with a configured downloader pointed at a FAKE runner (never a
// real download). Start must report at least one progress value and return a
// non-empty output path on success.
func RunConformance(t *testing.T, d Downloader) {
	t.Helper()
	ctx := context.Background()

	t.Run("Plugin/identity", func(t *testing.T) {
		if d.Type() != "downloader" {
			t.Errorf("Type() = %q, want \"downloader\"", d.Type())
		}
		if d.Name() == "" {
			t.Error("Name() must not be empty")
		}
	})

	req := core.DownloadRequest{
		Source: "spotify", ExternalID: "e1", Artist: "Artist", Title: "Song", Album: "Album",
	}

	t.Run("CanDownload/cheap-bool", func(t *testing.T) {
		if _, err := d.CanDownload(ctx, req); err != nil {
			t.Fatalf("CanDownload: %v", err)
		}
	})

	t.Run("Start/reports-progress-and-output", func(t *testing.T) {
		var last = -2
		var calls int
		out, err := d.Start(ctx, req, func(p int) { last = p; calls++ })
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
		if out == "" {
			t.Error("Start returned empty output path on success")
		}
		if calls == 0 {
			t.Error("Start never reported progress")
		}
		_ = last
	})

	t.Run("Start/respects-canceled-ctx", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		// A canceled ctx may error or return promptly; it must not panic or block.
		_, _ = d.Start(cctx, req, func(int) {})
	})
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/download/ -v`
Expected: PASS (`TestFakeDownloaderConformance` + subtests).

- [ ] **Step 6: Commit**

```bash
git add internal/download/download.go internal/download/conformance.go internal/download/conformance_test.go
git commit -m "feat(download): Downloader interface, topic constants, and conformance suite"
```

---

## Task 3: Dedup key (`internal/download/dedup.go`)

**Files:**
- Create: `internal/download/dedup.go`
- Test: `internal/download/dedup_test.go`

**Interfaces:**
- Consumes: `internal/matching` (`matching.Normalize`), `internal/core`, `crypto/sha256`.
- Produces:
  ```go
  // DedupKey is sha256(Normalize(artist)+sep+Normalize(title)+sep+Normalize(album)),
  // hex-encoded. REUSES matching.Normalize so dedup and matching never drift.
  func DedupKey(req core.DownloadRequest) string
  ```
  - Separator is the unit separator rune `"␟"` (␟), matching the spec's `dedup_key` definition.

- [ ] **Step 1: Write the failing test**

Create `internal/download/dedup_test.go`:
```go
package download

import (
	"testing"

	"github.com/maximusjb/crate/internal/core"
)

func TestDedupKeyStableAndNormalized(t *testing.T) {
	a := core.DownloadRequest{Artist: "The Beatles", Title: "Hey Jude", Album: "1"}
	// Cosmetic noise the matcher's Normalize strips: case, feat group, punctuation.
	b := core.DownloadRequest{Artist: "the beatles", Title: "Hey Jude (feat. Nobody)", Album: "1"}
	if DedupKey(a) == "" {
		t.Fatal("DedupKey must be non-empty")
	}
	if DedupKey(a) != DedupKey(b) {
		t.Fatalf("normalized-equal requests must share a key: %q vs %q", DedupKey(a), DedupKey(b))
	}
}

func TestDedupKeyDistinguishesDifferentTracks(t *testing.T) {
	a := core.DownloadRequest{Artist: "Radiohead", Title: "Creep", Album: "Pablo Honey"}
	b := core.DownloadRequest{Artist: "TLC", Title: "Creep", Album: "CrazySexyCool"}
	if DedupKey(a) == DedupKey(b) {
		t.Fatal("different artist/album must produce different keys")
	}
}

func TestDedupKeyDeterministicLength(t *testing.T) {
	k := DedupKey(core.DownloadRequest{Artist: "x", Title: "y", Album: "z"})
	if len(k) != 64 {
		t.Fatalf("sha256 hex must be 64 chars, got %d (%q)", len(k), k)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/download/ -run Dedup -v`
Expected: FAIL — `undefined: DedupKey`.

- [ ] **Step 3: Write the implementation**

Create `internal/download/dedup.go`:
```go
package download

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/maximusjb/crate/internal/core"
	"github.com/maximusjb/crate/internal/matching"
)

// dedupSep is the unit-separator rune (␟) joining the normalized fields, matching
// the spec's dedup_key definition. It cannot appear in normalized text.
const dedupSep = "␟"

// DedupKey computes the deduplication key for a download request. It is
// sha256(Normalize(artist)+␟+Normalize(title)+␟+Normalize(album)), hex-encoded.
// It REUSES matching.Normalize so the dedup key and the matcher can never drift.
func DedupKey(req core.DownloadRequest) string {
	raw := matching.Normalize(req.Artist) + dedupSep +
		matching.Normalize(req.Title) + dedupSep +
		matching.Normalize(req.Album)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/download/ -v`
Expected: PASS (conformance + dedup tests).

- [ ] **Step 5: Commit**

```bash
git add internal/download/dedup.go internal/download/dedup_test.go
git commit -m "feat(download): dedup key via shared matching.Normalize"
```

---

## Task 4: Store — `download_jobs` migration + queries + sqlc regen

**Files:**
- Create: `internal/store/migrations/0003_download_jobs.sql`
- Create: `internal/store/queries/download_jobs.sql`
- Regenerate (committed): `internal/store/db/download_jobs.sql.go`, `internal/store/db/models.go` (adds `DownloadJob`)
- Test: extend `internal/store/store_test.go` (a `download_jobs` insert/get round-trip via `*db.Queries`)

**Interfaces:**
- Consumes: goose, sqlc (engine sqlite).
- Produces (sqlc-generated on `*db.Queries`):
  ```go
  func (q *Queries) InsertDownloadJob(ctx, arg db.InsertDownloadJobParams) error
  func (q *Queries) GetDownloadJob(ctx, id string) (db.DownloadJob, error)
  func (q *Queries) GetActiveDownloadJobByDedup(ctx, dedupKey string) (db.DownloadJob, error)
  func (q *Queries) ListDownloadJobs(ctx) ([]db.DownloadJob, error)
  func (q *Queries) ListDownloadJobsByStatus(ctx, status string) ([]db.DownloadJob, error)
  func (q *Queries) UpdateDownloadJobStatus(ctx, arg db.UpdateDownloadJobStatusParams) error
  func (q *Queries) UpdateDownloadJobProgress(ctx, arg db.UpdateDownloadJobProgressParams) error
  func (q *Queries) UpdateDownloadJobError(ctx, arg db.UpdateDownloadJobErrorParams) error
  func (q *Queries) UpdateDownloadJobOutputPath(ctx, arg db.UpdateDownloadJobOutputPathParams) error
  func (q *Queries) UpdateDownloadJobLibraryTrackID(ctx, arg db.UpdateDownloadJobLibraryTrackIDParams) error
  func (q *Queries) IncrementDownloadJobAttempts(ctx, id string) error
  ```
  - `db.DownloadJob` columns: `ID string`, `DedupKey string`, `RequestJson string`, `DownloaderName string`, `Status string`, `Progress int64`, `Error string`, `OutputPath string`, `LibraryTrackID sql.NullString`, `Priority int64`, `RequestedBy sql.NullString`, `Attempts int64`, `CreatedAt int64`, `StartedAt sql.NullInt64`, `FinishedAt sql.NullInt64`.

- [ ] **Step 1: Write the migration**

Create `internal/store/migrations/0003_download_jobs.sql`:
```sql
-- +goose Up
CREATE TABLE download_jobs (
    id               TEXT PRIMARY KEY,
    dedup_key        TEXT NOT NULL,
    request_json     TEXT NOT NULL DEFAULT '{}',
    downloader_name  TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'queued',
    progress         INTEGER NOT NULL DEFAULT 0,
    error            TEXT NOT NULL DEFAULT '',
    output_path      TEXT NOT NULL DEFAULT '',
    library_track_id TEXT,                       -- NULL until re-matched after scan
    priority         INTEGER NOT NULL DEFAULT 0,
    requested_by     TEXT,                       -- NULL: P3 multi-user stub
    attempts         INTEGER NOT NULL DEFAULT 0,
    created_at       INTEGER NOT NULL DEFAULT (unixepoch()),
    started_at       INTEGER,
    finished_at      INTEGER
);

-- Active-job lookup for dedup-join (queued or running rows share a dedup_key).
CREATE INDEX idx_download_jobs_dedup_active ON download_jobs (dedup_key, status);

-- +goose Down
DROP INDEX idx_download_jobs_dedup_active;
DROP TABLE download_jobs;
```

- [ ] **Step 2: Write the queries**

Create `internal/store/queries/download_jobs.sql`:
```sql
-- name: InsertDownloadJob :exec
INSERT INTO download_jobs (
    id, dedup_key, request_json, downloader_name, status, progress, error,
    output_path, library_track_id, priority, requested_by, attempts,
    created_at, started_at, finished_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, unixepoch(), NULL, NULL);

-- name: GetDownloadJob :one
SELECT id, dedup_key, request_json, downloader_name, status, progress, error,
       output_path, library_track_id, priority, requested_by, attempts,
       created_at, started_at, finished_at
FROM download_jobs WHERE id = ?;

-- name: GetActiveDownloadJobByDedup :one
SELECT id, dedup_key, request_json, downloader_name, status, progress, error,
       output_path, library_track_id, priority, requested_by, attempts,
       created_at, started_at, finished_at
FROM download_jobs
WHERE dedup_key = ? AND status IN ('queued', 'running')
ORDER BY created_at ASC
LIMIT 1;

-- name: ListDownloadJobs :many
SELECT id, dedup_key, request_json, downloader_name, status, progress, error,
       output_path, library_track_id, priority, requested_by, attempts,
       created_at, started_at, finished_at
FROM download_jobs
ORDER BY created_at DESC;

-- name: ListDownloadJobsByStatus :many
SELECT id, dedup_key, request_json, downloader_name, status, progress, error,
       output_path, library_track_id, priority, requested_by, attempts,
       created_at, started_at, finished_at
FROM download_jobs
WHERE status = ?
ORDER BY created_at DESC;

-- name: UpdateDownloadJobStatus :exec
UPDATE download_jobs
SET status = ?,
    started_at = CASE WHEN ? = 'running' AND started_at IS NULL THEN unixepoch() ELSE started_at END,
    finished_at = CASE WHEN ? IN ('completed','failed','canceled') THEN unixepoch() ELSE finished_at END
WHERE id = ?;

-- name: UpdateDownloadJobProgress :exec
UPDATE download_jobs SET progress = ? WHERE id = ?;

-- name: UpdateDownloadJobError :exec
UPDATE download_jobs SET error = ? WHERE id = ?;

-- name: UpdateDownloadJobOutputPath :exec
UPDATE download_jobs SET output_path = ? WHERE id = ?;

-- name: UpdateDownloadJobLibraryTrackID :exec
UPDATE download_jobs SET library_track_id = ? WHERE id = ?;

-- name: IncrementDownloadJobAttempts :exec
UPDATE download_jobs SET attempts = attempts + 1 WHERE id = ?;
```

> **sqlc note:** the `UpdateDownloadJobStatus` query has three `?` for the status value plus the `id`. sqlc names positional params `?1`-style only with explicit numbering; to keep generated params readable, this query is intentionally written so sqlc emits `UpdateDownloadJobStatusParams{Status string; Column2 string; Column3 string; ID string}`. The Manager (Task 5) passes the same status string into all three status slots. If your sqlc version names them differently, pass the status string to every status-typed field and the id last — do NOT change the SQL.

- [ ] **Step 3: Regenerate sqlc (installed binary; fallback go run)**

Run from the repo root:
```bash
sqlc generate || go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate
```
Expected: no output on success. New file `internal/store/db/download_jobs.sql.go`; `internal/store/db/models.go` gains a `DownloadJob` struct.

Verify the generated model and methods exist:
```bash
grep -n "type DownloadJob struct" internal/store/db/models.go
grep -n "func (q \*Queries) GetActiveDownloadJobByDedup" internal/store/db/download_jobs.sql.go
```
Expected: both grep lines print a match.

- [ ] **Step 4: Add a store round-trip test**

Append to `internal/store/store_test.go`:
```go
func TestDownloadJobRoundTrip(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/dj.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	q := st.Q()

	if err := q.InsertDownloadJob(ctx, db.InsertDownloadJobParams{
		ID: "j1", DedupKey: "dk1", RequestJson: `{"title":"Song"}`, DownloaderName: "spotdl",
		Status: "queued", Progress: 0, Error: "", OutputPath: "",
		LibraryTrackID: sql.NullString{}, Priority: 0, RequestedBy: sql.NullString{}, Attempts: 0,
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, err := q.GetDownloadJob(ctx, "j1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.DedupKey != "dk1" || got.Status != "queued" || got.DownloaderName != "spotdl" {
		t.Fatalf("round trip mismatch: %+v", got)
	}

	// Dedup-join lookup finds the active (queued) job.
	active, err := q.GetActiveDownloadJobByDedup(ctx, "dk1")
	if err != nil {
		t.Fatalf("active lookup: %v", err)
	}
	if active.ID != "j1" {
		t.Fatalf("active = %q, want j1", active.ID)
	}

	// Move to running, then completed; finished_at must be set.
	if err := q.UpdateDownloadJobStatus(ctx, db.UpdateDownloadJobStatusParams{
		Status: "running", Column2: "running", Column3: "running", ID: "j1",
	}); err != nil {
		t.Fatalf("status running: %v", err)
	}
	if err := q.UpdateDownloadJobStatus(ctx, db.UpdateDownloadJobStatusParams{
		Status: "completed", Column2: "completed", Column3: "completed", ID: "j1",
	}); err != nil {
		t.Fatalf("status completed: %v", err)
	}
	done, _ := q.GetDownloadJob(ctx, "j1")
	if !done.FinishedAt.Valid || !done.StartedAt.Valid {
		t.Fatalf("started/finished not set: %+v", done)
	}

	// A completed job is no longer "active" for dedup-join.
	if _, err := q.GetActiveDownloadJobByDedup(ctx, "dk1"); err != sql.ErrNoRows {
		t.Fatalf("completed job should not be active, err=%v", err)
	}
}
```

> NOTE: if the sqlc-generated param names for `UpdateDownloadJobStatus` differ from `Status/Column2/Column3/ID`, adjust the field names in THIS test to match the generated struct (run `grep -n "UpdateDownloadJobStatusParams" internal/store/db/download_jobs.sql.go` to read them). The values are all the same status string + the id.

The test file imports must include `context`, `database/sql`, and `github.com/maximusjb/crate/internal/store/db` — add any missing ones to the existing import block.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/store/ -v`
Expected: PASS (existing store tests + `TestDownloadJobRoundTrip`).

- [ ] **Step 6: Commit**

```bash
git add internal/store/migrations/0003_download_jobs.sql internal/store/queries/download_jobs.sql internal/store/db/ internal/store/store_test.go
git commit -m "feat(store): download_jobs migration, queries, and sqlc regen"
```

---

## Task 5: Manager part 1 — injectable Clock + Enqueue (dedup-join), worker pool, fallback chain

**Files:**
- Create: `internal/download/clock.go`
- Create: `internal/download/manager.go`
- Test: `internal/download/manager_test.go`

**Interfaces:**
- Consumes: `core`, `registry`, `events.Bus`, `*db.Queries` (via narrow interfaces), `library.LibraryAdapter` (via a narrow `ScanController`), `matching.Service` (via a narrow `Rematcher`), the `Downloader`s.
- Produces:
  ```go
  // Clock is injectable so debounce tests don't wait real time.
  type Clock interface {
      Now() time.Time
      // AfterFunc schedules f after d and returns a stop func; a fake controls it.
      AfterFunc(d time.Duration, f func()) (stop func() bool)
  }
  type realClock struct{}      // production: time-based
  type JobStore interface { ... } // the *db.Queries slice the Manager needs
  type ScanController interface {  // the library.LibraryAdapter slice
      StartScan(ctx context.Context) error
      ScanStatus(ctx context.Context) (core.ScanStatus, error)
  }
  type Rematcher interface { // matching.Service slice
      Match(ctx context.Context, ext core.ExternalResult) (core.MatchResult, error)
  }
  type Publisher interface { Publish(events.Event) } // *events.Bus slice
  type VersionBumper interface {
      LibraryVersion(ctx context.Context) (int64, error)
      SetLibraryVersion(ctx context.Context, v int64) error
  }
  type Config struct {
      Workers       int
      DebounceWindow time.Duration // ~5s
      ScanPollEvery  time.Duration
      ScanPollMax    time.Duration
  }
  func NewManager(cfg Config, downloaders []Downloader, store JobStore, bus Publisher,
      scanner ScanController, rematcher Rematcher, version VersionBumper, clock Clock) *Manager
  func (m *Manager) Enqueue(ctx context.Context, req core.DownloadRequest) (core.DownloadJob, error)
  func (m *Manager) Status(ctx context.Context, jobID string) (core.DownloadJob, error)
  func (m *Manager) List(ctx context.Context) ([]core.DownloadJob, error)
  func (m *Manager) Start() // launches worker goroutines
  func (m *Manager) Stop()  // drains + stops workers (for tests/shutdown)
  ```
  - `*db.Queries` satisfies `JobStore`; `*library.Adapter`/subsonic satisfies `ScanController`; `*matching.Service` satisfies `Rematcher`; `*events.Bus` satisfies `Publisher`; `*store.Store` satisfies `VersionBumper` (needs a new `SetLibraryVersion` method — added in this task).

- [ ] **Step 1: Add `SetLibraryVersion` to the store (the bump path)**

Append to `internal/store/store.go`:
```go
// SetLibraryVersion writes the monotonic library_version into settings. The
// Manager bumps it on scan-completion to invalidate stale match_cache rows.
func (s *Store) SetLibraryVersion(ctx context.Context, v int64) error {
	return s.q.SetLibraryVersion(ctx, strconv.FormatInt(v, 10))
}
```
(`strconv` is already imported in `store.go`. `SetLibraryVersion` query already exists in `queries/library_version.sql` from M2 and is generated.)

Add a quick test to `internal/store/store_test.go`:
```go
func TestSetAndGetLibraryVersion(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/lv.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := st.SetLibraryVersion(ctx, 7); err != nil {
		t.Fatal(err)
	}
	v, err := st.LibraryVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v != 7 {
		t.Fatalf("library_version = %d, want 7", v)
	}
}
```

Run: `go test ./internal/store/ -run LibraryVersion -v`
Expected: PASS.

- [ ] **Step 2: Write the failing Manager test (dedup-join + fallback + worker completion)**

Create `internal/download/manager_test.go`:
```go
package download

import (
	"context"
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
type fakeDL struct {
	name        string
	canDownload bool
	block       chan struct{} // if non-nil, Start blocks until closed/canceled
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

func (s *memStore) Insert(_ context.Context, j core.DownloadJob) error {
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
```

Run with the race detector:
Run: `go test ./internal/download/ -run "Enqueue|Dedup|Concurrent" -race -v`
Expected: FAIL — `undefined: NewManager` / `undefined: RealClock`.

- [ ] **Step 3: Write the Clock**

Create `internal/download/clock.go`:
```go
package download

import "time"

// Clock abstracts time so debounce tests don't wait real seconds.
type Clock interface {
	Now() time.Time
	// AfterFunc schedules f to run after d, returning a stop func that cancels it
	// (returns true if it stopped the timer before firing). Mirrors time.AfterFunc.
	AfterFunc(d time.Duration, f func()) (stop func() bool)
}

// RealClock is the production, time-based Clock.
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now() }
func (RealClock) AfterFunc(d time.Duration, f func()) func() bool {
	t := time.AfterFunc(d, f)
	return t.Stop
}
```

> NOTE the signature mismatch hazard: the interface method returns `(stop func() bool)`. `RealClock.AfterFunc` above returns `func() bool` (unnamed) — that satisfies the interface. The fake clock (Task 6) returns the same shape.

- [ ] **Step 4: Write the Manager (part 1: construction, Enqueue, worker pool, fallback)**

Create `internal/download/manager.go`:
```go
package download

import (
	"context"
	"encoding/json"
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
	Insert(ctx context.Context, j core.DownloadJob) error
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

	mu        sync.Mutex
	cancels   map[string]context.CancelFunc // in-flight job cancel funcs
	debounce  func() bool                    // active debounce timer stop (or nil)
	pending   bool                           // a completion is awaiting the scan window

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

// Stop signals workers to drain and waits for them. Idempotent.
func (m *Manager) Stop() {
	m.stopOnce.Do(func() { close(m.stopCh) })
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
		PlayWhenReady:  req.PlayWhenReady,
		CreatedAt:      m.clock.Now().Unix(),
	}
	if err := m.store.Insert(ctx, job); err != nil {
		return core.DownloadJob{}, err
	}
	m.publishEvent(TopicQueued, job, "")

	select {
	case m.queue <- job.ID:
	default:
		// Queue full: still persisted as queued; a worker will not pick it up until
		// space frees. For MVP the buffer (256) is generous; treat as enqueued.
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
// extra is an optional override topic-specific field carrier (unused for now).
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

// requestFromJob reconstructs the DownloadRequest a worker needs from a job.
// The full request is stored as request_json by the sqlStore adapter; the
// in-memory test store reconstructs from job fields. We re-derive the minimal
// fields needed by the downloader from the persisted request_json when present.
func requestJSON(req core.DownloadRequest) string {
	b, _ := json.Marshal(req)
	return string(b)
}
```

> **request_json carrying:** the worker (Task 6) needs the artist/title/album to pass to `Downloader.Start`. The in-memory test store keeps the whole job; the SQLite `sqlStore` (Task 7) persists `request_json` and rehydrates it. To keep the `core.DownloadJob` lean while still carrying the request for the worker, Task 6 stores the originating `core.DownloadRequest` in an in-Manager map keyed by job ID at Enqueue time (cleared on terminal status), AND the sqlStore persists `request_json` for durability across restarts. This task lays the field plumbing; the worker loop and that map are added in Task 6.

- [ ] **Step 5: Run the part-1 tests**

The dedup/fallback tests reference `m.worker()` which does not exist yet. Add a TEMPORARY no-op worker so part-1 compiles and the dedup-join test (which needs the worker to actually run `Start`) can pass. Replace the `requestFromJob`/`requestJSON` tail with a real worker in Task 6; for now add this minimal worker + request map to `manager.go`:

```go
// --- minimal worker plumbing (expanded in Task 6) ---

// reqs holds the originating request per in-flight/queued job (rehydration).
// Added as a field; initialize in NewManager.
//   m.reqs = map[string]core.DownloadRequest{}
```

Add the field `reqs map[string]core.DownloadRequest` to the `Manager` struct, initialize it in `NewManager` (`reqs: map[string]core.DownloadRequest{}`), store the request in `Enqueue` right after a successful insert (`m.reqs[job.ID] = req`), and add the worker:

```go
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
```

Run: `go test ./internal/download/ -run "Enqueue|Dedup|Concurrent" -race -v`
Expected: PASS — fallback picks the right downloader; no-downloader errors; explicit downloader honored; dedup-join returns the same job ID; concurrent same-key enqueues collapse to one job. (Race detector clean.)

- [ ] **Step 6: Commit**

```bash
git add internal/download/clock.go internal/download/manager.go internal/download/manager_test.go internal/store/store.go internal/store/store_test.go
git commit -m "feat(download): Manager enqueue with dedup-join, fallback chain, worker pool"
```

---

## Task 6: Manager part 2 — scan-debounce (injectable clock), re-match + version bump, cancel/retry

**Files:**
- Modify: `internal/download/manager.go` (replace the part-1 `process` completion tail with the full debounce + re-match path; add `Cancel`/`Retry`; add a fake-clock-driven debounce)
- Modify: `internal/download/manager_test.go` (add a fake clock + debounce-coalesce, completion→rematch→version-bump, cancel, retry tests)

**Interfaces:**
- Produces (new on `*Manager`):
  ```go
  func (m *Manager) Cancel(ctx context.Context, jobID string) error // cancels in-flight exec; marks canceled
  func (m *Manager) Retry(ctx context.Context, jobID string) (core.DownloadJob, error) // failed → queued, attempts++
  ```
- A test fake clock:
  ```go
  type fakeClock struct { mu sync.Mutex; now time.Time; fns []*fakeTimer }
  func (c *fakeClock) Advance(d time.Duration) // fires due timers
  ```

- [ ] **Step 1: Write the failing tests (fake clock + debounce + completion + cancel + retry)**

Append to `internal/download/manager_test.go`:
```go
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
		[]Downloader{dl}, store, bus, scanner, fakeRematcher{trackID: "t1"}, ver, clk)
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
	m := NewManager(Config{Workers: 1, DebounceWindow: 5 * time.Second, ScanPollEvery: time.Millisecond, ScanPollMax: time.Second},
		[]Downloader{dl}, store, bus, &fakeScanner{}, fakeRematcher{trackID: "lib-track-9"}, &fakeVersion{v: 1}, clk)
	t.Cleanup(m.Stop)
	m.Start()

	// Subscribe to the complete topic.
	ch, unsub := bus.Subscribe(TopicComplete)
	defer unsub()

	job, err := m.Enqueue(context.Background(), core.DownloadRequest{Source: "spotify", ExternalID: "e1", Artist: "A", Title: "T", Album: "Al"})
	if err != nil {
		t.Fatal(err)
	}

	// Drain progress/complete events; wait until the job is completed in the store.
	go func() {
		for range ch {
		}
	}()
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
	clk.Advance(5 * time.Second) // fire the debounced scan → re-match → set library_track_id

	cur, _, _ := store.Get(context.Background(), job.ID)
	if cur.LibraryTrackID != "lib-track-9" {
		t.Fatalf("library_track_id not set after re-match, got %q", cur.LibraryTrackID)
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
	_ = store.Insert(context.Background(), failed)

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
```

Run: `go test ./internal/download/ -run "Completion|Cancel|Retry" -race -v`
Expected: FAIL — `undefined: Cancel`, `undefined: Retry`, `undefined: SeedRequest`, and the completion tests fail because the part-1 `process` does not run the debounce/re-match.

- [ ] **Step 2: Replace the `process` completion tail + add debounce, Cancel, Retry, SeedRequest**

In `internal/download/manager.go`, REPLACE the part-1 `process` function body's completion section (from the `cur.Status = core.DownloadCompleted` line through the end of `process`) so that on success it schedules the debounced scan. Replace the whole `process` function with:

```go
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

	outPath, serr := dl.Start(jctx, req, func(p int) {
		m.mu.Lock()
		cur, _, _ := m.store.Get(ctx, id)
		cur.Progress = p
		_ = m.store.Update(ctx, cur)
		m.mu.Unlock()
		m.publishEvent(TopicProgress, cur, "")
	})

	cur, _, _ := m.store.Get(ctx, id)
	if serr != nil {
		// A canceled ctx => canceled status; any other error => failed.
		if jctx.Err() == context.Canceled {
			cur.Status = core.DownloadCanceled
			cur.FinishedAt = m.clock.Now().Unix()
			_ = m.store.Update(ctx, cur)
			m.publishEvent(TopicFailed, cur, "canceled")
			return
		}
		cur.Status = core.DownloadFailed
		cur.Error = serr.Error()
		cur.FinishedAt = m.clock.Now().Unix()
		_ = m.store.Update(ctx, cur)
		m.publishEvent(TopicFailed, cur, serr.Error())
		return
	}

	cur.Status = core.DownloadCompleted
	cur.Progress = 100
	cur.OutputPath = outPath
	cur.FinishedAt = m.clock.Now().Unix()
	_ = m.store.Update(ctx, cur)
	m.publishEvent(TopicComplete, cur, "")

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
		return
	}
	// Poll getScanStatus until idle or the poll budget elapses.
	deadline := m.clock.Now().Add(m.cfg.ScanPollMax)
	for {
		st, err := m.scanner.ScanStatus(ctx)
		if err != nil || !st.Scanning {
			break
		}
		if !m.clock.Now().Before(deadline) {
			break
		}
		time.Sleep(m.cfg.ScanPollEvery)
	}

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
	var artistIDs, albumIDs []string
	for _, j := range jobs {
		if j.Status != core.DownloadCompleted || j.LibraryTrackID != "" {
			continue
		}
		if m.rematcher == nil {
			continue
		}
		res, merr := m.rematcher.Match(ctx, core.ExternalResult{
			Source: j.Source, ExternalID: j.ExternalID, Type: core.EntityTrack,
		})
		if merr != nil || res.Status != core.MatchInLibrary {
			continue
		}
		j.LibraryTrackID = res.LibraryTrackID
		_ = m.store.Update(ctx, j)
		m.publishComplete(j, res.LibraryTrackID)
	}
	if m.bus != nil {
		m.bus.Publish(events.Event{Topic: TopicLibraryUpdate, Payload: core.LibraryUpdatedEvent{
			ArtistIDs: artistIDs, AlbumIDs: albumIDs,
		}})
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
func (m *Manager) Retry(ctx context.Context, jobID string) (core.DownloadJob, error) {
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
	m.publishEvent(TopicQueued, job, "")
	select {
	case m.queue <- job.ID:
	default:
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
```

> NOTE: remove the now-unused `requestFromJob`/`requestJSON` helpers from Task 5 IF the compiler flags them unused; `requestJSON` IS used by the sqlStore in Task 7, so keep it. The part-1 worker's temporary completion code is fully superseded by this `process`.

- [ ] **Step 3: Run the part-2 tests (race)**

Run: `go test ./internal/download/ -race -v`
Expected: PASS — all Task-5 + Task-6 tests: dedup-join, fallback, debounce-coalesce (exactly one scan for 4 completions; version bumps 1→2), completion sets library_track_id + publishes complete, cancel transitions to canceled, retry resets to queued with attempts++.

- [ ] **Step 4: Commit**

```bash
git add internal/download/manager.go internal/download/manager_test.go
git commit -m "feat(download): scan-debounce, re-match + version bump, cancel and retry"
```

---

## Task 7: spotDL adapter — injectable Runner, graceful stdout parse, conformance

**Files:**
- Create: `internal/download/spotdl/runner.go`
- Create: `internal/download/spotdl/adapter.go`
- Test: `internal/download/spotdl/adapter_test.go`

**Interfaces:**
- Consumes: `os/exec`, `bufio`, `regexp`, `core`, `registry`, `download.RunConformance`.
- Produces:
  ```go
  // Runner streams a process's stdout line-by-line so the parser is testable.
  type Runner interface {
      Run(ctx context.Context, name string, args []string, onLine func(string)) error
  }
  type ExecRunner struct{} // production: os/exec with a stdout pipe
  type Adapter struct { ... }
  func New() *Adapter
  func (a *Adapter) WithRunner(r Runner) *Adapter // test seam
  func (a *Adapter) Type() string                 // "downloader"
  func (a *Adapter) Name() string                 // "spotdl"
  func (a *Adapter) ConfigSchema() registry.ConfigSchema // output_dir, binary_path
  func (a *Adapter) Init(cfg map[string]any) error
  func (a *Adapter) TestConnection(ctx context.Context) error // runs `spotdl --version`
  func (a *Adapter) CanDownload(ctx context.Context, req core.DownloadRequest) (bool, error)
  func (a *Adapter) Start(ctx context.Context, req core.DownloadRequest, onProgress func(int)) (string, error)
  ```
  - `var _ download.Downloader = (*Adapter)(nil)` compile-time assertion.

- [ ] **Step 1: Write the failing adapter test (fake runner with a malformed line)**

Create `internal/download/spotdl/adapter_test.go`:
```go
package spotdl

import (
	"context"
	"testing"

	"github.com/maximusjb/crate/internal/core"
	"github.com/maximusjb/crate/internal/download"
)

// fakeRunner replays canned stdout lines (incl. one malformed line) and records
// the command it was asked to run. It never shells out.
type fakeRunner struct {
	lines    []string
	gotName  string
	gotArgs  []string
	runErr   error
}

func (f *fakeRunner) Run(ctx context.Context, name string, args []string, onLine func(string)) error {
	f.gotName = name
	f.gotArgs = args
	for _, l := range f.lines {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		onLine(l)
	}
	return f.runErr
}

func newAdapter(t *testing.T, r Runner) *Adapter {
	t.Helper()
	a := New().WithRunner(r)
	if err := a.Init(map[string]any{"output_dir": "/tmp/music", "binary_path": "spotdl"}); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestIdentityAndSchema(t *testing.T) {
	a := New()
	if a.Type() != "downloader" || a.Name() != "spotdl" {
		t.Fatalf("identity: %q/%q", a.Type(), a.Name())
	}
	keys := map[string]bool{}
	for _, f := range a.ConfigSchema().Fields {
		keys[f.Key] = true
	}
	if !keys["output_dir"] {
		t.Error("schema missing output_dir")
	}
}

func TestCanDownloadHeuristic(t *testing.T) {
	a := newAdapter(t, &fakeRunner{})
	ok, err := a.CanDownload(context.Background(), core.DownloadRequest{Artist: "A", Title: "T"})
	if err != nil || !ok {
		t.Fatalf("CanDownload(complete req) = %v,%v want true,nil", ok, err)
	}
	ok, _ = a.CanDownload(context.Background(), core.DownloadRequest{})
	if ok {
		t.Fatal("CanDownload(empty req) should be false")
	}
}

func TestStartParsesProgressAndDegradesGracefully(t *testing.T) {
	// Realistic spotDL output incl. a malformed line that must NOT error.
	r := &fakeRunner{lines: []string{
		`Found 1 song`,
		`Downloading "A - T": 25%`,
		`THIS IS A MALFORMED LINE WITH NO PERCENT`,
		`Downloading "A - T": 80%`,
		`Downloaded "A - T": /tmp/music/A - T.mp3`,
	}}
	a := newAdapter(t, r)

	var seen []int
	out, err := a.Start(context.Background(), core.DownloadRequest{Source: "spotify", ExternalID: "e1", Artist: "A", Title: "T"}, func(p int) {
		seen = append(seen, p)
	})
	if err != nil {
		t.Fatalf("Start errored on malformed line (must degrade): %v", err)
	}
	if out == "" {
		t.Fatal("Start returned empty output path")
	}
	// At least the 25 and 80 progress values were parsed; the malformed line is ignored.
	has := func(v int) bool {
		for _, p := range seen {
			if p == v {
				return true
			}
		}
		return false
	}
	if !has(25) || !has(80) {
		t.Fatalf("expected parsed progress 25 and 80, got %v", seen)
	}
}

func TestStartUnknownProgressIsNotAnError(t *testing.T) {
	// No parseable percentage at all → progress reported as -1 (indeterminate),
	// success still returns an output path (the URL/query forms the spotdl arg).
	r := &fakeRunner{lines: []string{`some opaque output`, `more opaque output`}}
	a := newAdapter(t, r)
	out, err := a.Start(context.Background(), core.DownloadRequest{Source: "spotify", ExternalID: "e2", Artist: "A", Title: "T"}, func(int) {})
	if err != nil {
		t.Fatalf("unknown progress must not error: %v", err)
	}
	if out == "" {
		t.Fatal("expected a non-empty output path even with unknown progress")
	}
}

func TestStartPassesOutputDirAndQuery(t *testing.T) {
	r := &fakeRunner{lines: []string{`Downloaded: ok`}}
	a := newAdapter(t, r)
	_, _ = a.Start(context.Background(), core.DownloadRequest{Source: "spotify", ExternalID: "e1", Artist: "Daft Punk", Title: "One More Time"}, func(int) {})
	if r.gotName != "spotdl" {
		t.Fatalf("binary = %q, want spotdl", r.gotName)
	}
	joined := ""
	for _, a := range r.gotArgs {
		joined += a + " "
	}
	if !contains(joined, "/tmp/music") {
		t.Fatalf("output dir not passed in args: %v", r.gotArgs)
	}
	if !contains(joined, "Daft Punk") && !contains(joined, "One More Time") {
		t.Fatalf("search query not passed in args: %v", r.gotArgs)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

func TestSpotdlConformance(t *testing.T) {
	// Conformance Start must report progress + return an output path: feed a
	// runner that yields a progress line and a completion line.
	r := &fakeRunner{lines: []string{`Downloading "x": 50%`, `Downloaded: /tmp/music/x.mp3`}}
	a := newAdapter(t, r)
	download.RunConformance(t, a)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/download/spotdl/ -v`
Expected: FAIL — `undefined: New` / `undefined: Adapter`.

- [ ] **Step 3: Write the Runner**

Create `internal/download/spotdl/runner.go`:
```go
package spotdl

import (
	"bufio"
	"context"
	"os/exec"
)

// Runner streams a process's combined stdout/stderr line-by-line. Abstracted so
// the parser is unit-testable with canned output and no real downloads occur.
type Runner interface {
	Run(ctx context.Context, name string, args []string, onLine func(string)) error
}

// ExecRunner is the production Runner. It uses os/exec with a piped stdout so
// progress lines stream as spotDL emits them. The ctx is honored: canceling it
// kills the child process (exec.CommandContext).
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args []string, onLine func(string)) error {
	cmd := exec.CommandContext(ctx, name, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout // spotDL writes progress to stdout; merge any stderr too
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		onLine(sc.Text())
	}
	return cmd.Wait()
}
```

- [ ] **Step 4: Write the adapter**

Create `internal/download/spotdl/adapter.go`:
```go
// Package spotdl is the spotDL Downloader adapter. It shells out via an injectable
// Runner and parses progress from stdout, DEGRADING GRACEFULLY: an unparseable
// line yields unknown progress (-1), never an error.
//
// VERSION PIN: spotDL output formatting is fragile. The Docker image pins spotDL
// (see deployment docs / docker-compose); if upgrading spotDL, re-verify the
// progress regex below against the new output format.
package spotdl

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/maximusjb/crate/internal/core"
	"github.com/maximusjb/crate/internal/download"
	"github.com/maximusjb/crate/internal/registry"
)

var _ download.Downloader = (*Adapter)(nil)

// progressRe extracts an integer percentage from a stdout line, e.g. "...: 80%".
var progressRe = regexp.MustCompile(`(\d{1,3})\s*%`)

// Adapter implements download.Downloader for spotDL.
type Adapter struct {
	runner    Runner
	outputDir string
	binary    string
}

func New() *Adapter {
	return &Adapter{runner: ExecRunner{}, binary: "spotdl"}
}

// WithRunner injects a Runner (test seam). Call before Init.
func (a *Adapter) WithRunner(r Runner) *Adapter {
	a.runner = r
	return a
}

func (a *Adapter) Type() string { return "downloader" }
func (a *Adapter) Name() string { return "spotdl" }

func (a *Adapter) ConfigSchema() registry.ConfigSchema {
	return registry.ConfigSchema{Fields: []registry.ConfigField{
		{Key: "output_dir", Label: "Output directory", Type: "string", Required: true},
		{Key: "binary_path", Label: "spotDL binary path", Type: "string", Required: false},
	}}
}

func (a *Adapter) Init(cfg map[string]any) error {
	if v, ok := cfg["output_dir"].(string); ok && v != "" {
		a.outputDir = v
	}
	if a.outputDir == "" {
		return fmt.Errorf("spotdl: output_dir is required")
	}
	if v, ok := cfg["binary_path"].(string); ok && v != "" {
		a.binary = v
	}
	if a.runner == nil {
		a.runner = ExecRunner{}
	}
	return nil
}

// TestConnection runs `<binary> --version` to confirm spotDL is present/runnable.
func (a *Adapter) TestConnection(ctx context.Context) error {
	ran := false
	err := a.runner.Run(ctx, a.binary, []string{"--version"}, func(string) { ran = true })
	if err != nil {
		return fmt.Errorf("spotdl --version: %w", err)
	}
	_ = ran
	return nil
}

// CanDownload is a cheap heuristic: spotDL can attempt any track that has at least
// a title and an artist. No network call.
func (a *Adapter) CanDownload(ctx context.Context, req core.DownloadRequest) (bool, error) {
	return req.Title != "" && req.Artist != "", nil
}

// Start shells out to spotDL and streams progress. Unparseable lines degrade to
// unknown progress (onProgress(-1) once), never an error. On success it returns
// the output directory as the path hint (spotDL writes the file under output_dir;
// the scan picks it up — the exact filename is spotDL's concern).
func (a *Adapter) Start(ctx context.Context, req core.DownloadRequest, onProgress func(int)) (string, error) {
	query := strings.TrimSpace(req.Artist + " - " + req.Title)
	args := []string{"download", query, "--output", a.outputDir}

	sawProgress := false
	rerr := a.runner.Run(ctx, a.binary, args, func(line string) {
		if m := progressRe.FindStringSubmatch(line); m != nil {
			if p, err := strconv.Atoi(m[1]); err == nil && p >= 0 && p <= 100 {
				sawProgress = true
				onProgress(p)
				return
			}
		}
		// Unparseable line: ignore (graceful degradation).
	})
	if rerr != nil {
		return "", fmt.Errorf("spotdl download %q: %w", query, rerr)
	}
	if !sawProgress {
		onProgress(-1) // indeterminate: spotDL gave no parseable percentage
	}
	return a.outputDir, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/download/spotdl/ -v`
Expected: PASS (identity/schema, CanDownload heuristic, progress parse with a malformed line degrading gracefully, unknown-progress-not-error, args carry output dir + query, conformance).

- [ ] **Step 6: Commit**

```bash
git add internal/download/spotdl/runner.go internal/download/spotdl/adapter.go internal/download/spotdl/adapter_test.go
git commit -m "feat(spotdl): downloader adapter with injectable runner and graceful progress parse"
```

---

## Task 8: SQLite-backed JobStore adapter (`sqlStore`)

**Files:**
- Create: `internal/download/sqlstore.go`
- Test: `internal/download/sqlstore_test.go`

**Interfaces:**
- Consumes: `*db.Queries` (Task 4), `core`.
- Produces:
  ```go
  // sqlStore adapts *db.Queries to the Manager's JobStore, marshaling the request
  // into request_json and mapping sql.Null* columns to core.DownloadJob fields.
  type sqlStore struct { q *db.Queries }
  func NewSQLStore(q *db.Queries) JobStore
  ```
  - The Manager stores the full request via `SeedRequest`, but `sqlStore.Insert` ALSO persists `request_json` (durability). For MVP the `core.DownloadJob` does not carry the request fields; `Insert` accepts the job + the originating request is passed by the Manager through the in-memory map. To persist request_json, `NewSQLStore` exposes the job's request via a side map is overkill — instead the Manager passes the request_json by storing it on the job is wrong (no field). RESOLUTION: add an `InsertWithRequest` path. The cleanest fit given the JobStore interface: `sqlStore.Insert` writes `request_json='{}'` and the Manager seeds the request in-memory (sufficient for MVP single-process). Durable rehydration across restart is a documented P2 follow-up. See note.

> **MVP simplification (durability):** persisting full `request_json` requires threading the `core.DownloadRequest` into `JobStore.Insert`. To keep the `JobStore` interface clean and the in-memory test store simple, MVP persists job lifecycle state in `download_jobs` and keeps the originating request in the Manager's in-memory `reqs` map (seeded at Enqueue). Cross-restart rehydration of queued jobs (reading `request_json` back) is a documented P2 follow-up — the column exists and `sqlStore.Insert` writes a marshaled request when available via the optional `RequestCarrier` below, so no migration change is needed later.

- [ ] **Step 1: Write the failing test**

Create `internal/download/sqlstore_test.go`:
```go
package download

import (
	"context"
	"testing"

	"github.com/maximusjb/crate/internal/core"
	"github.com/maximusjb/crate/internal/store"
)

func newSQLStore(t *testing.T) JobStore {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/ss.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	return NewSQLStore(st.Q())
}

func TestSQLStoreInsertGetUpdate(t *testing.T) {
	s := newSQLStore(t)
	ctx := context.Background()
	job := core.DownloadJob{
		ID: "j1", DedupKey: "dk1", Status: core.DownloadQueued, DownloaderName: "spotdl",
		Source: "spotify", ExternalID: "e1", Progress: 0,
	}
	if err := s.Insert(ctx, job); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Get(ctx, "j1")
	if err != nil || !ok {
		t.Fatalf("get: %v ok=%v", err, ok)
	}
	if got.DedupKey != "dk1" || got.Status != core.DownloadQueued {
		t.Fatalf("mismatch: %+v", got)
	}

	got.Status = core.DownloadRunning
	got.Progress = 60
	if err := s.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	active, ok, err := s.ActiveByDedup(ctx, "dk1")
	if err != nil || !ok {
		t.Fatalf("active: %v ok=%v", err, ok)
	}
	if active.Progress != 60 || active.Status != core.DownloadRunning {
		t.Fatalf("active mismatch: %+v", active)
	}

	got.Status = core.DownloadCompleted
	got.LibraryTrackID = "t9"
	if err := s.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.ActiveByDedup(ctx, "dk1"); ok {
		t.Fatal("completed job must not be active")
	}
	fin, _, _ := s.Get(ctx, "j1")
	if fin.LibraryTrackID != "t9" {
		t.Fatalf("library_track_id not persisted: %+v", fin)
	}

	list, err := s.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v len=%d", err, len(list))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/download/ -run SQLStore -v`
Expected: FAIL — `undefined: NewSQLStore`.

- [ ] **Step 3: Write the sqlStore**

Create `internal/download/sqlstore.go`:
```go
package download

import (
	"context"
	"database/sql"

	"github.com/maximusjb/crate/internal/core"
	"github.com/maximusjb/crate/internal/store/db"
)

// sqlStore adapts *db.Queries to JobStore, mapping core.DownloadJob ⇄ db rows.
type sqlStore struct{ q *db.Queries }

// NewSQLStore wraps generated queries as a Manager JobStore.
func NewSQLStore(q *db.Queries) JobStore { return &sqlStore{q: q} }

func toCore(r db.DownloadJob) core.DownloadJob {
	j := core.DownloadJob{
		ID:             r.ID,
		DedupKey:       r.DedupKey,
		Status:         core.DownloadStatus(r.Status),
		Progress:       int(r.Progress),
		Error:          r.Error,
		OutputPath:     r.OutputPath,
		DownloaderName: r.DownloaderName,
		Priority:       int(r.Priority),
		Attempts:       int(r.Attempts),
		CreatedAt:      r.CreatedAt,
	}
	if r.LibraryTrackID.Valid {
		j.LibraryTrackID = r.LibraryTrackID.String
	}
	if r.StartedAt.Valid {
		j.StartedAt = r.StartedAt.Int64
	}
	if r.FinishedAt.Valid {
		j.FinishedAt = r.FinishedAt.Int64
	}
	// Source/ExternalID/PlayWhenReady are carried in request_json; rehydrate them.
	var req core.DownloadRequest
	if r.RequestJson != "" {
		_ = jsonUnmarshal(r.RequestJson, &req)
	}
	j.Source = req.Source
	j.ExternalID = req.ExternalID
	j.PlayWhenReady = req.PlayWhenReady
	return j
}

func (s *sqlStore) Insert(ctx context.Context, j core.DownloadJob) error {
	req := core.DownloadRequest{
		Source: j.Source, ExternalID: j.ExternalID, PlayWhenReady: j.PlayWhenReady,
	}
	return s.q.InsertDownloadJob(ctx, db.InsertDownloadJobParams{
		ID:             j.ID,
		DedupKey:       j.DedupKey,
		RequestJson:    requestJSON(req),
		DownloaderName: j.DownloaderName,
		Status:         string(j.Status),
		Progress:       int64(j.Progress),
		Error:          j.Error,
		OutputPath:     j.OutputPath,
		LibraryTrackID: nullString(j.LibraryTrackID),
		Priority:       int64(j.Priority),
		RequestedBy:    sql.NullString{},
		Attempts:       int64(j.Attempts),
	})
}

func (s *sqlStore) Get(ctx context.Context, id string) (core.DownloadJob, bool, error) {
	r, err := s.q.GetDownloadJob(ctx, id)
	if err == sql.ErrNoRows {
		return core.DownloadJob{}, false, nil
	}
	if err != nil {
		return core.DownloadJob{}, false, err
	}
	return toCore(r), true, nil
}

func (s *sqlStore) ActiveByDedup(ctx context.Context, dedup string) (core.DownloadJob, bool, error) {
	r, err := s.q.GetActiveDownloadJobByDedup(ctx, dedup)
	if err == sql.ErrNoRows {
		return core.DownloadJob{}, false, nil
	}
	if err != nil {
		return core.DownloadJob{}, false, err
	}
	return toCore(r), true, nil
}

func (s *sqlStore) List(ctx context.Context) ([]core.DownloadJob, error) {
	rows, err := s.q.ListDownloadJobs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]core.DownloadJob, 0, len(rows))
	for _, r := range rows {
		out = append(out, toCore(r))
	}
	return out, nil
}

func (s *sqlStore) Update(ctx context.Context, j core.DownloadJob) error {
	if err := s.q.UpdateDownloadJobStatus(ctx, db.UpdateDownloadJobStatusParams{
		Status: string(j.Status), Column2: string(j.Status), Column3: string(j.Status), ID: j.ID,
	}); err != nil {
		return err
	}
	if err := s.q.UpdateDownloadJobProgress(ctx, db.UpdateDownloadJobProgressParams{Progress: int64(j.Progress), ID: j.ID}); err != nil {
		return err
	}
	if err := s.q.UpdateDownloadJobError(ctx, db.UpdateDownloadJobErrorParams{Error: j.Error, ID: j.ID}); err != nil {
		return err
	}
	if err := s.q.UpdateDownloadJobOutputPath(ctx, db.UpdateDownloadJobOutputPathParams{OutputPath: j.OutputPath, ID: j.ID}); err != nil {
		return err
	}
	return s.q.UpdateDownloadJobLibraryTrackID(ctx, db.UpdateDownloadJobLibraryTrackIDParams{LibraryTrackID: nullString(j.LibraryTrackID), ID: j.ID})
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
```

Add a tiny JSON helper (used by both sqlStore and the Manager's `requestJSON`) — append to `internal/download/dedup.go` (it already imports nothing conflicting) OR create `internal/download/json.go`:
```go
package download

import "encoding/json"

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }
```

> NOTE: `requestJSON` is defined in `manager.go` (Task 5). Keep it there. `jsonUnmarshal` lives in `json.go`. If the part-1 `requestJSON` was removed as unused, re-add it (it is used by `sqlStore.Insert`):
> ```go
> func requestJSON(req core.DownloadRequest) string { b, _ := json.Marshal(req); return string(b) }
> ```
> and ensure `manager.go` imports `encoding/json` (it does, from Task 5).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/download/ -run SQLStore -v`
Expected: PASS.

- [ ] **Step 5: Run the whole download package (race)**

Run: `go test ./internal/download/... -race`
Expected: PASS (core Manager tests, dedup, spotdl, sqlstore).

- [ ] **Step 6: Commit**

```bash
git add internal/download/sqlstore.go internal/download/sqlstore_test.go internal/download/json.go internal/download/manager.go
git commit -m "feat(download): SQLite-backed JobStore adapter"
```

---

## Task 9: API — download REST endpoints + `Deps` wiring

**Files:**
- Create: `internal/api/downloads.go`
- Test: `internal/api/downloads_test.go`
- Modify: `internal/api/server.go` (add `Downloads` to `Deps`, mount routes)

**Interfaces:**
- Consumes: `core`, the Manager (via a narrow `DownloadManager` interface so tests use a fake).
- Produces (in `server.go`):
  ```go
  // DownloadManager is the subset of *download.Manager the API needs.
  type DownloadManager interface {
      Enqueue(ctx context.Context, req core.DownloadRequest) (core.DownloadJob, error)
      List(ctx context.Context) ([]core.DownloadJob, error)
      Cancel(ctx context.Context, jobID string) error
      Retry(ctx context.Context, jobID string) (core.DownloadJob, error)
  }
  // Deps gains: Downloads DownloadManager
  ```
- Routes (all auth-gated, under the protected group):
  - `POST /api/v1/downloads` body `{source, externalId, artist, title, album, isrc?, downloader?, playWhenReady?}` → `Enqueue` → 200 with the `DownloadJob` (dedup-join returns the existing job)
  - `GET /api/v1/downloads` → `List` → 200 `[]DownloadJob`
  - `POST /api/v1/downloads/{id}/cancel` → `Cancel` → 200 `{ok:true}`
  - `POST /api/v1/downloads/{id}/retry` → `Retry` → 200 with the `DownloadJob`

- [ ] **Step 1: Add `DownloadManager` to `Deps` and mount routes**

In `internal/api/server.go`, add to the import block (already imports `context`, `core`): no new imports needed. Add the interface and the Deps field, and mount the routes.

Add after the `Streamer` interface:
```go
// DownloadManager is the subset of *download.Manager the API needs.
type DownloadManager interface {
	Enqueue(ctx context.Context, req core.DownloadRequest) (core.DownloadJob, error)
	List(ctx context.Context) ([]core.DownloadJob, error)
	Cancel(ctx context.Context, jobID string) error
	Retry(ctx context.Context, jobID string) (core.DownloadJob, error)
}
```

Add to `Deps`:
```go
	Downloads        DownloadManager
	Events           EventSubscriber // added in Task 10
```
> NOTE: add the `Events` field NOW (as a field of an interface type defined in Task 10) only AFTER Task 10 defines `EventSubscriber`. To keep this task compiling on its own, add ONLY `Downloads DownloadManager` here; Task 10 adds `Events EventSubscriber`.

So for THIS task, add to `Deps`:
```go
	Downloads        DownloadManager
```

Mount the routes inside the protected group in `routes()` (after `pr.Get("/search/everywhere", ...)`):
```go
			pr.Post("/downloads", s.handleCreateDownload)
			pr.Get("/downloads", s.handleListDownloads)
			pr.Post("/downloads/{id}/cancel", s.handleCancelDownload)
			pr.Post("/downloads/{id}/retry", s.handleRetryDownload)
```

- [ ] **Step 2: Write the failing handler test**

Create `internal/api/downloads_test.go`:
```go
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/maximusjb/crate/internal/auth"
	"github.com/maximusjb/crate/internal/core"
	"github.com/maximusjb/crate/internal/registry"
	"github.com/maximusjb/crate/internal/store"
)

// fakeManager is an in-memory DownloadManager.
type fakeManager struct {
	jobs      map[string]core.DownloadJob
	lastReq   core.DownloadRequest
	canceled  []string
	retried   []string
}

func newFakeManager() *fakeManager { return &fakeManager{jobs: map[string]core.DownloadJob{}} }

func (m *fakeManager) Enqueue(_ context.Context, req core.DownloadRequest) (core.DownloadJob, error) {
	m.lastReq = req
	j := core.DownloadJob{ID: "job-" + req.ExternalID, DedupKey: "dk", Status: core.DownloadQueued, Source: req.Source, ExternalID: req.ExternalID, PlayWhenReady: req.PlayWhenReady}
	m.jobs[j.ID] = j
	return j, nil
}
func (m *fakeManager) List(context.Context) ([]core.DownloadJob, error) {
	out := []core.DownloadJob{}
	for _, j := range m.jobs {
		out = append(out, j)
	}
	return out, nil
}
func (m *fakeManager) Cancel(_ context.Context, id string) error { m.canceled = append(m.canceled, id); return nil }
func (m *fakeManager) Retry(_ context.Context, id string) (core.DownloadJob, error) {
	m.retried = append(m.retried, id)
	return core.DownloadJob{ID: id, Status: core.DownloadQueued, Attempts: 1}, nil
}

func downloadTestServer(t *testing.T, mgr DownloadManager) (*Server, *http.Cookie) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/api.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	authSvc := auth.NewService(st.Q(), time.Now)
	if err := authSvc.SetAdminPassword(context.Background(), "pw"); err != nil {
		t.Fatal(err)
	}
	tok, _ := authSvc.CreateSession(context.Background())
	srv := NewServer(Deps{
		Auth:       authSvc,
		Downloads:  mgr,
		Search:     registry.NewRegistry("search"),
		Downloader: registry.NewRegistry("downloader"),
	})
	return srv, &http.Cookie{Name: sessionCookie, Value: tok}
}

func TestCreateDownloadEnqueues(t *testing.T) {
	mgr := newFakeManager()
	srv, cookie := downloadTestServer(t, mgr)
	body := `{"source":"spotify","externalId":"sp1","artist":"A","title":"T","album":"Al","playWhenReady":true}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/downloads", bytes.NewBufferString(body))
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var job core.DownloadJob
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if job.ExternalID != "sp1" || job.Status != core.DownloadQueued {
		t.Fatalf("job = %+v", job)
	}
	if !mgr.lastReq.PlayWhenReady {
		t.Fatal("playWhenReady not forwarded to Enqueue")
	}
}

func TestListDownloads(t *testing.T) {
	mgr := newFakeManager()
	mgr.jobs["j1"] = core.DownloadJob{ID: "j1", Status: core.DownloadRunning}
	srv, cookie := downloadTestServer(t, mgr)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/downloads", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var jobs []core.DownloadJob
	if err := json.Unmarshal(rec.Body.Bytes(), &jobs); err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ID != "j1" {
		t.Fatalf("jobs = %+v", jobs)
	}
}

func TestCancelDownload(t *testing.T) {
	mgr := newFakeManager()
	srv, cookie := downloadTestServer(t, mgr)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/downloads/j9/cancel", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(mgr.canceled) != 1 || mgr.canceled[0] != "j9" {
		t.Fatalf("canceled = %v", mgr.canceled)
	}
}

func TestRetryDownload(t *testing.T) {
	mgr := newFakeManager()
	srv, cookie := downloadTestServer(t, mgr)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/downloads/j5/retry", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(mgr.retried) != 1 || mgr.retried[0] != "j5" {
		t.Fatalf("retried = %v", mgr.retried)
	}
}

func TestDownloadsRequireAuth(t *testing.T) {
	srv, _ := downloadTestServer(t, newFakeManager())
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/downloads", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestCreateDownloadNilManager503(t *testing.T) {
	srv, cookie := downloadTestServer(t, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/downloads", bytes.NewBufferString(`{"externalId":"x"}`))
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/api/ -run Download -v`
Expected: FAIL — `s.handleCreateDownload` undefined (and `Deps.Downloads` may already compile if added in Step 1).

- [ ] **Step 4: Write the handlers**

Create `internal/api/downloads.go`:
```go
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/maximusjb/crate/internal/core"
)

// createDownloadBody is the POST /downloads request DTO.
type createDownloadBody struct {
	Source        string `json:"source"`
	ExternalID    string `json:"externalId"`
	Artist        string `json:"artist"`
	Title         string `json:"title"`
	Album         string `json:"album"`
	ISRC          string `json:"isrc"`
	Downloader    string `json:"downloader"`
	PlayWhenReady bool   `json:"playWhenReady"`
}

func (s *Server) handleCreateDownload(w http.ResponseWriter, r *http.Request) {
	if s.deps.Downloads == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no downloader configured"})
		return
	}
	var body createDownloadBody
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	if body.ExternalID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "externalId is required"})
		return
	}
	job, err := s.deps.Downloads.Enqueue(r.Context(), core.DownloadRequest{
		Source:        body.Source,
		ExternalID:    body.ExternalID,
		Artist:        body.Artist,
		Title:         body.Title,
		Album:         body.Album,
		ISRC:          body.ISRC,
		Downloader:    body.Downloader,
		PlayWhenReady: body.PlayWhenReady,
	})
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleListDownloads(w http.ResponseWriter, r *http.Request) {
	if s.deps.Downloads == nil {
		writeJSON(w, http.StatusOK, []core.DownloadJob{})
		return
	}
	jobs, err := s.deps.Downloads.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not list downloads"})
		return
	}
	if jobs == nil {
		jobs = []core.DownloadJob{}
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (s *Server) handleCancelDownload(w http.ResponseWriter, r *http.Request) {
	if s.deps.Downloads == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no downloader configured"})
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.deps.Downloads.Cancel(r.Context(), id); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleRetryDownload(w http.ResponseWriter, r *http.Request) {
	if s.deps.Downloads == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no downloader configured"})
		return
	}
	id := chi.URLParam(r, "id")
	job, err := s.deps.Downloads.Retry(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, job)
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/api/ -run Download -v`
Expected: PASS (create/enqueue forwarding playWhenReady, list, cancel, retry, auth-gate, nil-manager 503).

- [ ] **Step 6: Confirm the rest of the api package still compiles + passes**

Run: `go test ./internal/api/ -v`
Expected: PASS (existing M0–M2 tests unaffected; `Deps.Downloads` zero-value nil in those test servers).

- [ ] **Step 7: Commit**

```bash
git add internal/api/downloads.go internal/api/downloads_test.go internal/api/server.go
git commit -m "feat(api): download REST endpoints (create/list/cancel/retry)"
```

---

## Task 10: API — WebSocket endpoint (`GET /api/v1/ws`)

**Files:**
- Modify: `go.mod` / `go.sum` (add `github.com/coder/websocket v1.8.15`)
- Create: `internal/api/ws.go`
- Test: `internal/api/ws_test.go`
- Modify: `internal/api/server.go` (add `Events EventSubscriber` to `Deps`; mount `GET /ws`)

**Interfaces:**
- Consumes: `github.com/coder/websocket`, `internal/events`, `core`.
- Produces (in `server.go`):
  ```go
  // EventSubscriber is the EventBus slice the WS handler needs. *events.Bus fits.
  type EventSubscriber interface {
      Subscribe(topic string) (<-chan events.Event, func())
  }
  // Deps gains: Events EventSubscriber
  ```
- The WS handler subscribes to the M3 topics, fans them into one client stream, and writes each as a JSON frame `{type, payload}`. Auth-gated (it lives in the protected group, so `requireAuth` runs first and the cookie is checked). On client disconnect (read error / ctx done) it unsubscribes and closes.

> **WS framing contract (client mirror in Task 12):** each frame is `{"type": "<topic>", "payload": <event JSON>}`, e.g. `{"type":"download.progress","payload":{"jobId":"j1","status":"running","progress":42,...}}`.

- [ ] **Step 1: Add the dependency**

Run from the repo root:
```bash
go get github.com/coder/websocket@v1.8.15
```
Expected: `go.mod` gains `github.com/coder/websocket v1.8.15`; `go.sum` updated. Verify:
```bash
grep coder/websocket go.mod
```
Expected: `github.com/coder/websocket v1.8.15`.

- [ ] **Step 2: Add `EventSubscriber` to `Deps` + mount the route**

In `internal/api/server.go`, add the `internal/events` import, the interface, the Deps field, and the route.

Add to imports:
```go
	"github.com/maximusjb/crate/internal/events"
```

Add after the `DownloadManager` interface:
```go
// EventSubscriber is the EventBus slice the WS handler needs.
type EventSubscriber interface {
	Subscribe(topic string) (<-chan events.Event, func())
}
```

Add to `Deps`:
```go
	Events           EventSubscriber
```

Mount inside the protected group (after the downloads routes):
```go
			pr.Get("/ws", s.handleWS)
```

- [ ] **Step 3: Write the failing WS test**

Create `internal/api/ws_test.go`:
```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/maximusjb/crate/internal/auth"
	"github.com/maximusjb/crate/internal/core"
	"github.com/maximusjb/crate/internal/download"
	"github.com/maximusjb/crate/internal/events"
	"github.com/maximusjb/crate/internal/registry"
	"github.com/maximusjb/crate/internal/store"
)

func wsTestServer(t *testing.T) (*httptest.Server, *events.Bus, string) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/ws.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	authSvc := auth.NewService(st.Q(), time.Now)
	if err := authSvc.SetAdminPassword(context.Background(), "pw"); err != nil {
		t.Fatal(err)
	}
	tok, _ := authSvc.CreateSession(context.Background())
	bus := events.New()
	srv := NewServer(Deps{
		Auth:       authSvc,
		Events:     bus,
		Search:     registry.NewRegistry("search"),
		Downloader: registry.NewRegistry("downloader"),
	})
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)
	return hs, bus, tok
}

// wsFrame mirrors the {type, payload} envelope the handler writes.
type wsFrame struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func TestWSStreamsPublishedEvents(t *testing.T) {
	hs, bus, tok := wsTestServer(t)
	wsURL := "ws" + hs.URL[len("http"):] + "/api/v1/ws"

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	c, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Cookie": {sessionCookie + "=" + tok}},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	// Give the handler a moment to subscribe, then publish a progress event.
	time.Sleep(50 * time.Millisecond)
	bus.Publish(events.Event{Topic: download.TopicProgress, Payload: core.DownloadEvent{
		JobID: "j1", Status: core.DownloadRunning, Progress: 42, Source: "spotify", ExternalID: "sp1",
	}})

	var frame wsFrame
	if err := wsjson.Read(ctx, c, &frame); err != nil {
		t.Fatalf("read frame: %v", err)
	}
	if frame.Type != download.TopicProgress {
		t.Fatalf("frame type = %q, want %q", frame.Type, download.TopicProgress)
	}
	var ev core.DownloadEvent
	if err := json.Unmarshal(frame.Payload, &ev); err != nil {
		t.Fatal(err)
	}
	if ev.JobID != "j1" || ev.Progress != 42 {
		t.Fatalf("payload = %+v", ev)
	}
}

func TestWSRequiresAuth(t *testing.T) {
	hs, _, _ := wsTestServer(t)
	wsURL := "ws" + hs.URL[len("http"):] + "/api/v1/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// No cookie → handshake should be rejected (401 before upgrade).
	_, _, err := websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		t.Fatal("expected dial to fail without auth")
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `go test ./internal/api/ -run WS -v`
Expected: FAIL — `s.handleWS` undefined.

- [ ] **Step 5: Write the WS handler**

Create `internal/api/ws.go`:
```go
package api

import (
	"context"
	"net/http"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/maximusjb/crate/internal/download"
	"github.com/maximusjb/crate/internal/events"
)

// wsTopics are the EventBus topics streamed to WS clients.
var wsTopics = []string{
	download.TopicQueued,
	download.TopicProgress,
	download.TopicComplete,
	download.TopicFailed,
	download.TopicLibraryUpdate,
}

// wsEnvelope is the JSON frame written to the client: {type, payload}.
type wsEnvelope struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

// handleWS upgrades to a WebSocket, subscribes to the EventBus topics, and writes
// each event as a JSON frame. It is a DISTINCT transport from the SSE search
// stream. Auth is enforced by requireAuth (this route is in the protected group),
// so the handshake only succeeds with a valid session cookie/bearer. It returns
// (unsubscribing + closing) when the client disconnects or ctx is done.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if s.deps.Events == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "events unavailable"})
		return
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Same-origin only by default; the SPA is served from the same host.
		InsecureSkipVerify: s.deps.Dev, // dev: allow the Vite origin
	})
	if err != nil {
		return
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	ctx := r.Context()

	// Fan the per-topic subscriptions into one merged channel.
	merged := make(chan events.Event, 64)
	var unsubs []func()
	for _, topic := range wsTopics {
		ch, unsub := s.deps.Events.Subscribe(topic)
		unsubs = append(unsubs, unsub)
		go func(ch <-chan events.Event) {
			for ev := range ch {
				select {
				case merged <- ev:
				case <-ctx.Done():
					return
				}
			}
		}(ch)
	}
	defer func() {
		for _, u := range unsubs {
			u()
		}
	}()

	// Detect client disconnect: a reader goroutine cancels ctx on read error.
	readCtx, cancelRead := context.WithCancel(ctx)
	defer cancelRead()
	go func() {
		for {
			if _, _, err := c.Read(readCtx); err != nil {
				cancelRead()
				return
			}
		}
	}()

	for {
		select {
		case <-readCtx.Done():
			return
		case ev := <-merged:
			if err := wsjson.Write(readCtx, c, wsEnvelope{Type: ev.Topic, Payload: ev.Payload}); err != nil {
				return
			}
		}
	}
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/api/ -run WS -v`
Expected: PASS (a published `download.progress` is delivered as a `{type,payload}` frame; an unauthenticated dial fails).

- [ ] **Step 7: Full api package + race**

Run: `go test ./internal/api/... -race`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add go.mod go.sum internal/api/ws.go internal/api/ws_test.go internal/api/server.go
git commit -m "feat(api): WebSocket endpoint streaming typed EventBus frames"
```

---

## Task 11: Composition root — register spotdl, build downloaders, wire the Manager + EventBus

**Files:**
- Create: `cmd/crate/download_wiring.go`
- Test: `cmd/crate/download_wiring_test.go`
- Modify: `cmd/crate/main.go`

**Interfaces:**
- Consumes: `registry`, `download`, `download/spotdl`, `db.AdapterInstance`, the existing `libAdapter` + `matching.Service` + `store.Store` + `events.Bus`.
- Produces:
  ```go
  func buildDownloaders(reg *registry.Registry, instances []db.AdapterInstance, getenv func(string) string) []download.Downloader
  ```
  - Builds every ENABLED `downloader` adapter_instance, applying env overrides (`CRATE_SPOTDL_PATH` → `binary_path`, `CRATE_DOWNLOAD_DIR` → `output_dir`) before `Init`. Per-source init failures warn-and-skip.

- [ ] **Step 1: Write the failing wiring test**

Create `cmd/crate/download_wiring_test.go`:
```go
package main

import (
	"testing"

	"github.com/maximusjb/crate/internal/download/spotdl"
	"github.com/maximusjb/crate/internal/registry"
	"github.com/maximusjb/crate/internal/store/db"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestBuildDownloadersEnabledOnly(t *testing.T) {
	reg := registry.NewRegistry("downloader")
	reg.Register("spotdl", func() registry.Plugin { return spotdl.New() })
	instances := []db.AdapterInstance{
		{Type: "downloader", Name: "spotdl", Enabled: 1, ConfigJson: `{"output_dir":"/music"}`},
		{Type: "downloader", Name: "spotdl", Enabled: 0, ConfigJson: `{"output_dir":"/music2"}`},
		{Type: "library", Name: "subsonic", Enabled: 1, ConfigJson: `{}`},
	}
	out := buildDownloaders(reg, instances, env(nil))
	if len(out) != 1 {
		t.Fatalf("want 1 enabled downloader, got %d", len(out))
	}
	if out[0].Name() != "spotdl" {
		t.Fatalf("name = %q", out[0].Name())
	}
}

func TestBuildDownloadersEnvOverrideAndSkipOnBadConfig(t *testing.T) {
	reg := registry.NewRegistry("downloader")
	reg.Register("spotdl", func() registry.Plugin { return spotdl.New() })
	instances := []db.AdapterInstance{
		// Missing output_dir in config; env supplies it → must succeed.
		{Type: "downloader", Name: "spotdl", Enabled: 1, ConfigJson: `{}`},
		// Unknown adapter → warn-and-skip, not a panic.
		{Type: "downloader", Name: "ghost", Enabled: 1, ConfigJson: `{}`},
	}
	out := buildDownloaders(reg, instances, env(map[string]string{"CRATE_DOWNLOAD_DIR": "/from/env"}))
	if len(out) != 1 {
		t.Fatalf("want 1 downloader (env-supplied dir), got %d", len(out))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/crate/ -run Downloaders -v`
Expected: FAIL — `undefined: buildDownloaders`.

- [ ] **Step 3: Write the wiring helper**

Create `cmd/crate/download_wiring.go`:
```go
package main

import (
	"encoding/json"
	"log"

	"github.com/maximusjb/crate/internal/download"
	"github.com/maximusjb/crate/internal/registry"
	"github.com/maximusjb/crate/internal/store/db"
)

// buildDownloaders instantiates every ENABLED adapter_instance of type
// "downloader" from the registry, applying env overrides (CRATE_SPOTDL_PATH →
// binary_path, CRATE_DOWNLOAD_DIR → output_dir) just before Init. instances are
// ordered by (type, priority) from ListAdapterInstances, so the returned slice is
// already in fallback-chain order. Per-source failures warn-and-skip.
func buildDownloaders(reg *registry.Registry, instances []db.AdapterInstance, getenv func(string) string) []download.Downloader {
	out := []download.Downloader{}
	for i := range instances {
		inst := instances[i]
		if inst.Type != "downloader" || inst.Enabled != 1 {
			continue
		}
		plugin, err := reg.Create(inst.Name)
		if err != nil {
			log.Printf("WARNING: downloader %q create failed: %v — skipping", inst.Name, err)
			continue
		}
		dl, ok := plugin.(download.Downloader)
		if !ok {
			log.Printf("WARNING: adapter %q is not a Downloader — skipping", inst.Name)
			continue
		}

		cfg := map[string]any{}
		if inst.ConfigJson != "" {
			if err := json.Unmarshal([]byte(inst.ConfigJson), &cfg); err != nil {
				log.Printf("WARNING: downloader %q config parse failed: %v — skipping", inst.Name, err)
				continue
			}
		}
		// Env overrides (spotdl) before Init.
		if inst.Name == "spotdl" {
			if p := getenv("CRATE_SPOTDL_PATH"); p != "" {
				cfg["binary_path"] = p
			}
			if d := getenv("CRATE_DOWNLOAD_DIR"); d != "" {
				cfg["output_dir"] = d
			}
		}

		if err := dl.Init(cfg); err != nil {
			log.Printf("WARNING: downloader %q init failed: %v — skipping", inst.Name, err)
			continue
		}
		out = append(out, dl)
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/crate/ -run Downloaders -v`
Expected: PASS (enabled-only filter; env-supplied output_dir; unknown adapter skipped).

- [ ] **Step 5: Wire the Manager + EventBus into `main.go`**

In `cmd/crate/main.go`, make these changes:

(a) Add imports:
```go
	"github.com/maximusjb/crate/internal/download"
	"github.com/maximusjb/crate/internal/download/spotdl"
	"github.com/maximusjb/crate/internal/events"
```

(b) Register the spotdl factory next to the others (after `downloaderReg := registry.NewRegistry("downloader")`):
```go
	downloaderReg.Register("spotdl", func() registry.Plugin { return spotdl.New() })
```

(c) After the aggregator block (before building `deps`), construct the EventBus, build downloaders, and the Manager:
```go
	// EventBus backs both the WS endpoint and the Manager's typed events.
	bus := events.New()

	// Build the download Manager from enabled downloader instances.
	var manager *download.Manager
	downloaders := buildDownloaders(downloaderReg, instances, os.Getenv)
	if len(downloaders) > 0 && libAdapter != nil {
		var rematcher download.Rematcher
		rematcher = matching.NewService(libAdapter, st.Q(), st.LibraryVersion)
		manager = download.NewManager(
			download.Config{Workers: 2, DebounceWindow: 5 * time.Second},
			downloaders,
			download.NewSQLStore(st.Q()),
			bus,
			libAdapter,           // ScanController (StartScan/ScanStatus)
			rematcher,            // Rematcher
			st,                   // VersionBumper (LibraryVersion/SetLibraryVersion)
			download.RealClock{}, // production clock
		)
		manager.Start()
		defer manager.Stop()
		log.Printf("download manager active: %d downloader(s)", len(downloaders))
	} else if len(downloaders) > 0 {
		log.Printf("WARNING: downloaders configured but no library adapter — download manager disabled")
	} else {
		log.Printf("no downloaders configured (add one via settings)")
	}
```

(d) Extend the `deps` literal to wire `Events` and (conditionally) `Downloads`:
```go
	deps := api.Deps{
		Auth:       authSvc,
		Library:    libAdapter,
		Search:     searchReg,
		Downloader: downloaderReg,
		Events:     bus,
		Dev:        cfg.Dev,
	}
	if aggregator != nil {
		deps.SearchAggregator = aggregator
	}
	if manager != nil {
		deps.Downloads = manager
	}
```

> NOTE: `st` (the `*store.Store`) satisfies `download.VersionBumper` because Task 5 added `SetLibraryVersion` and M2 added `LibraryVersion`. `libAdapter` (a `library.LibraryAdapter`) satisfies `download.ScanController` (it has `StartScan`/`ScanStatus`). `*matching.Service` satisfies `download.Rematcher` (it has `Match`). `*events.Bus` satisfies both `api.EventSubscriber` and `download.Publisher`.

- [ ] **Step 6: Build + vet the whole module**

Run: `go build ./... && go vet ./cmd/... ./internal/...`
Expected: no output (compiles + vets clean).

Run: `go test ./cmd/... ./internal/...`
Expected: PASS (all Go tests across the module).

- [ ] **Step 7: Commit**

```bash
git add cmd/crate/download_wiring.go cmd/crate/download_wiring_test.go cmd/crate/main.go
git commit -m "feat(cmd): wire download Manager, downloaders, and EventBus at the composition root"
```

---

## Task 12: Frontend types + `RealtimeConnection` WS client

**Files:**
- Modify: `web/src/lib/types.ts` (add download + event types, mirroring the Go camelCase tags)
- Create: `web/src/lib/realtime.ts`
- Test: `web/src/lib/realtime.test.ts`

**Interfaces:**
- Produces (TS, mirroring `core.DownloadJob`/`DownloadEvent`/`LibraryUpdatedEvent` exactly):
  ```ts
  export type DownloadStatus = 'queued' | 'running' | 'completed' | 'failed' | 'canceled'
  export interface DownloadJob { id; dedupKey; status; progress; error?; outputPath?;
    libraryTrackId?; downloaderName; priority; attempts; source; externalId;
    playWhenReady; createdAt; startedAt; finishedAt }
  export interface DownloadEvent { jobId; dedupKey; status; progress; error?; source;
    externalId; libraryTrackId?; artistId?; albumId? }
  export interface LibraryUpdatedEvent { artistIds: string[]; albumIds: string[] }
  export interface RealtimeEvent { type: string; payload: unknown }
  ```
- `RealtimeConnection` (browser `WebSocket` to `/api/v1/ws`, DISTINCT from SSE), capped-backoff reconnect, resubscribe on reconnect, typed dispatch, `close()`:
  ```ts
  export interface RealtimeHandlers {
    onEvent(ev: RealtimeEvent): void
    onOpen?(): void   // resync hook (re-fetch GET /downloads)
    onClose?(): void
  }
  export class RealtimeConnection {
    constructor(handlers: RealtimeHandlers, makeSocket?: (url: string) => WebSocketLike)
    close(): void
  }
  ```

- [ ] **Step 1: Add the TS types**

Append to `web/src/lib/types.ts`:
```ts
export type DownloadStatus = 'queued' | 'running' | 'completed' | 'failed' | 'canceled'

export interface DownloadJob {
  id: string
  dedupKey: string
  status: DownloadStatus
  progress: number // 0-100, or -1 = unknown (indeterminate)
  error?: string
  outputPath?: string
  libraryTrackId?: string
  downloaderName: string
  priority: number
  attempts: number
  source: string
  externalId: string
  playWhenReady: boolean
  createdAt: number
  startedAt: number
  finishedAt: number
}

export interface DownloadEvent {
  jobId: string
  dedupKey: string
  status: DownloadStatus
  progress: number
  error?: string
  source: string
  externalId: string
  libraryTrackId?: string
  artistId?: string
  albumId?: string
}

export interface LibraryUpdatedEvent {
  artistIds: string[]
  albumIds: string[]
}

// RealtimeEvent is one WS frame: {type, payload}. type is the EventBus topic.
export interface RealtimeEvent {
  type: string
  payload: unknown
}
```

- [ ] **Step 2: Write the failing realtime test (stubbed WebSocket)**

Create `web/src/lib/realtime.test.ts`:
```ts
import { describe, it, expect, vi } from 'vitest'
import { RealtimeConnection, type WebSocketLike } from './realtime'

// stubSocket captures handlers so the test drives open/message/close.
function makeStub() {
  const sockets: StubSocket[] = []
  class StubSocket implements WebSocketLike {
    onopen: (() => void) | null = null
    onmessage: ((ev: { data: string }) => void) | null = null
    onclose: (() => void) | null = null
    onerror: (() => void) | null = null
    closed = false
    constructor(public url: string) {
      sockets.push(this)
    }
    close() {
      this.closed = true
      this.onclose?.()
    }
  }
  return { sockets, StubSocket }
}

describe('RealtimeConnection', () => {
  it('dispatches typed frames and calls onOpen', () => {
    const { sockets, StubSocket } = makeStub()
    const events: { type: string }[] = []
    let opened = 0
    const conn = new RealtimeConnection(
      { onEvent: (ev) => events.push(ev), onOpen: () => opened++ },
      (url) => new StubSocket(url),
    )
    const s = sockets[0]
    expect(s.url).toContain('/api/v1/ws')

    s.onopen?.()
    expect(opened).toBe(1)

    s.onmessage?.({ data: JSON.stringify({ type: 'download.progress', payload: { jobId: 'j1', progress: 42 } }) })
    expect(events).toHaveLength(1)
    expect(events[0].type).toBe('download.progress')

    // Malformed frame is ignored (no throw).
    s.onmessage?.({ data: 'not json' })
    expect(events).toHaveLength(1)

    conn.close()
    expect(s.closed).toBe(true)
  })

  it('reconnects with backoff after an unexpected close', () => {
    vi.useFakeTimers()
    const { sockets, StubSocket } = makeStub()
    const conn = new RealtimeConnection({ onEvent: () => {} }, (url) => new StubSocket(url))
    // Simulate the socket dropping.
    sockets[0].onclose?.()
    // After the first backoff delay, a new socket is created.
    vi.advanceTimersByTime(1000)
    expect(sockets.length).toBe(2)
    conn.close()
    vi.useRealTimers()
  })
})
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd web && npx vitest run src/lib/realtime.test.ts`
Expected: FAIL — cannot resolve `./realtime`.

- [ ] **Step 4: Write the RealtimeConnection**

Create `web/src/lib/realtime.ts`:
```ts
import type { RealtimeEvent } from './types'

// WebSocketLike is the minimal slice we use so tests inject a stub (no real
// network/socket). The browser `WebSocket` satisfies it.
export interface WebSocketLike {
  onopen: (() => void) | null
  onmessage: ((ev: { data: string }) => void) | null
  onclose: (() => void) | null
  onerror: (() => void) | null
  close(): void
}

export interface RealtimeHandlers {
  onEvent(ev: RealtimeEvent): void
  onOpen?(): void
  onClose?(): void
}

const MAX_BACKOFF_MS = 15_000
const BASE_BACKOFF_MS = 1_000

// RealtimeConnection is the WebSocket transport for live download/library events.
// It is DISTINCT from the SSE SearchStream: it connects to /api/v1/ws (same-origin
// → the session cookie is sent automatically), reconnects with capped backoff,
// and resubscribes by reopening (the server re-subscribes all topics on connect).
// On (re)open, onOpen fires so the caller can resync (re-fetch GET /downloads).
export class RealtimeConnection {
  private socket: WebSocketLike | null = null
  private closedByUser = false
  private backoff = BASE_BACKOFF_MS
  private retryTimer: ReturnType<typeof setTimeout> | null = null
  private readonly makeSocket: (url: string) => WebSocketLike

  constructor(
    private handlers: RealtimeHandlers,
    makeSocket?: (url: string) => WebSocketLike,
  ) {
    this.makeSocket =
      makeSocket ??
      ((url) => new WebSocket(url) as unknown as WebSocketLike)
    this.connect()
  }

  private url(): string {
    // Same-origin ws(s):// URL derived from the page location.
    if (typeof location !== 'undefined') {
      const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
      return `${proto}//${location.host}/api/v1/ws`
    }
    return 'ws://localhost/api/v1/ws'
  }

  private connect() {
    const s = this.makeSocket(this.url())
    this.socket = s
    s.onopen = () => {
      this.backoff = BASE_BACKOFF_MS // reset on a successful connect
      this.handlers.onOpen?.()
    }
    s.onmessage = (ev) => {
      try {
        const frame = JSON.parse(ev.data) as RealtimeEvent
        if (frame && typeof frame.type === 'string') {
          this.handlers.onEvent(frame)
        }
      } catch {
        // ignore malformed frame
      }
    }
    s.onclose = () => {
      this.handlers.onClose?.()
      if (!this.closedByUser) this.scheduleReconnect()
    }
    s.onerror = () => {
      // onclose follows; reconnect is handled there.
    }
  }

  private scheduleReconnect() {
    if (this.retryTimer) clearTimeout(this.retryTimer)
    const delay = this.backoff
    this.backoff = Math.min(this.backoff * 2, MAX_BACKOFF_MS)
    this.retryTimer = setTimeout(() => {
      if (!this.closedByUser) this.connect()
    }, delay)
  }

  close() {
    this.closedByUser = true
    if (this.retryTimer) clearTimeout(this.retryTimer)
    this.socket?.close()
  }
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd web && npx vitest run src/lib/realtime.test.ts`
Expected: PASS (typed dispatch + onOpen; malformed frame ignored; reconnect after drop creates a second socket).

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/types.ts web/src/lib/realtime.ts web/src/lib/realtime.test.ts
git commit -m "feat(web): download types and RealtimeConnection WS client (distinct from SSE)"
```

---

## Task 13: Frontend `downloadStore` + `downloadApi`

**Files:**
- Create: `web/src/lib/downloadStore.ts`
- Test: `web/src/lib/downloadStore.test.ts`
- Create: `web/src/lib/downloadApi.ts`

**Interfaces:**
- `downloadStore` (Zustand) keyed by job id, updated from WS events + REST resync; exposes the active list + a lookup by externalId+source:
  ```ts
  interface DownloadStore {
    jobs: Record<string, DownloadJob>
    upsert(job: DownloadJob): void
    applyEvent(ev: DownloadEvent): void   // create/patch a job from a WS event
    setAll(jobs: DownloadJob[]): void      // resync from GET /downloads
    active(): DownloadJob[]                // queued|running, newest first
    byExternal(source: string, externalId: string): DownloadJob | undefined
  }
  ```
- `downloadApi`:
  ```ts
  postDownload(req): Promise<DownloadJob>
  getDownloads(): Promise<DownloadJob[]>
  cancelDownload(id): Promise<void>
  retryDownload(id): Promise<DownloadJob>
  ```

- [ ] **Step 1: Write the failing store test**

Create `web/src/lib/downloadStore.test.ts`:
```ts
import { describe, it, expect, beforeEach } from 'vitest'
import { useDownloads } from './downloadStore'
import type { DownloadEvent, DownloadJob } from './types'

function job(partial: Partial<DownloadJob>): DownloadJob {
  return {
    id: 'j1', dedupKey: 'dk', status: 'queued', progress: 0, downloaderName: 'spotdl',
    priority: 0, attempts: 0, source: 'spotify', externalId: 'sp1', playWhenReady: false,
    createdAt: 1, startedAt: 0, finishedAt: 0, ...partial,
  }
}

describe('downloadStore', () => {
  beforeEach(() => {
    useDownloads.setState({ jobs: {} })
  })

  it('upserts and looks up by externalId+source', () => {
    useDownloads.getState().upsert(job({ id: 'j1', source: 'spotify', externalId: 'sp1' }))
    const found = useDownloads.getState().byExternal('spotify', 'sp1')
    expect(found?.id).toBe('j1')
    expect(useDownloads.getState().byExternal('spotify', 'nope')).toBeUndefined()
  })

  it('applyEvent patches an existing job and creates a new one', () => {
    useDownloads.getState().upsert(job({ id: 'j1', status: 'queued', progress: 0 }))
    const ev: DownloadEvent = { jobId: 'j1', dedupKey: 'dk', status: 'running', progress: 55, source: 'spotify', externalId: 'sp1' }
    useDownloads.getState().applyEvent(ev)
    expect(useDownloads.getState().jobs['j1'].status).toBe('running')
    expect(useDownloads.getState().jobs['j1'].progress).toBe(55)

    // Unknown job → created from the event.
    useDownloads.getState().applyEvent({ jobId: 'j2', dedupKey: 'dk2', status: 'queued', progress: 0, source: 'spotify', externalId: 'sp2' })
    expect(useDownloads.getState().jobs['j2']).toBeDefined()
  })

  it('complete event sets libraryTrackId', () => {
    useDownloads.getState().upsert(job({ id: 'j1' }))
    useDownloads.getState().applyEvent({ jobId: 'j1', dedupKey: 'dk', status: 'completed', progress: 100, source: 'spotify', externalId: 'sp1', libraryTrackId: 't9' })
    expect(useDownloads.getState().jobs['j1'].libraryTrackId).toBe('t9')
    expect(useDownloads.getState().jobs['j1'].status).toBe('completed')
  })

  it('active() returns only queued/running newest-first', () => {
    useDownloads.getState().setAll([
      job({ id: 'a', status: 'completed', createdAt: 3 }),
      job({ id: 'b', status: 'running', createdAt: 2 }),
      job({ id: 'c', status: 'queued', createdAt: 1 }),
    ])
    const active = useDownloads.getState().active()
    expect(active.map((j) => j.id)).toEqual(['b', 'c'])
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/lib/downloadStore.test.ts`
Expected: FAIL — cannot resolve `./downloadStore`.

- [ ] **Step 3: Write the store**

Create `web/src/lib/downloadStore.ts`:
```ts
import { create } from 'zustand'
import type { DownloadEvent, DownloadJob } from './types'

interface DownloadStore {
  jobs: Record<string, DownloadJob>
  upsert(job: DownloadJob): void
  applyEvent(ev: DownloadEvent): void
  setAll(jobs: DownloadJob[]): void
  active(): DownloadJob[]
  byExternal(source: string, externalId: string): DownloadJob | undefined
}

// jobFromEvent builds a minimal DownloadJob for an event referencing an unknown
// job (e.g. progress arrived before the POST response was stored).
function jobFromEvent(ev: DownloadEvent): DownloadJob {
  return {
    id: ev.jobId,
    dedupKey: ev.dedupKey,
    status: ev.status,
    progress: ev.progress,
    error: ev.error,
    libraryTrackId: ev.libraryTrackId,
    downloaderName: '',
    priority: 0,
    attempts: 0,
    source: ev.source,
    externalId: ev.externalId,
    playWhenReady: false,
    createdAt: Date.now() / 1000,
    startedAt: 0,
    finishedAt: 0,
  }
}

export const useDownloads = create<DownloadStore>((set, get) => ({
  jobs: {},
  upsert: (job) => set((s) => ({ jobs: { ...s.jobs, [job.id]: job } })),
  applyEvent: (ev) =>
    set((s) => {
      const existing = s.jobs[ev.jobId]
      const next: DownloadJob = existing
        ? {
            ...existing,
            status: ev.status,
            progress: ev.progress,
            error: ev.error ?? existing.error,
            libraryTrackId: ev.libraryTrackId || existing.libraryTrackId,
          }
        : jobFromEvent(ev)
      return { jobs: { ...s.jobs, [ev.jobId]: next } }
    }),
  setAll: (jobs) =>
    set(() => {
      const map: Record<string, DownloadJob> = {}
      for (const j of jobs) map[j.id] = j
      return { jobs: map }
    }),
  active: () =>
    Object.values(get().jobs)
      .filter((j) => j.status === 'queued' || j.status === 'running')
      .sort((a, b) => b.createdAt - a.createdAt),
  byExternal: (source, externalId) =>
    Object.values(get().jobs).find((j) => j.source === source && j.externalId === externalId),
}))
```

- [ ] **Step 4: Write the REST helpers**

Create `web/src/lib/downloadApi.ts`:
```ts
import { api } from './api'
import type { DownloadJob } from './types'

export interface CreateDownloadReq {
  source: string
  externalId: string
  artist: string
  title: string
  album: string
  isrc?: string
  downloader?: string
  playWhenReady?: boolean
}

export function postDownload(req: CreateDownloadReq): Promise<DownloadJob> {
  return api.post<DownloadJob>('/downloads', req)
}

export function getDownloads(): Promise<DownloadJob[]> {
  return api.get<DownloadJob[]>('/downloads')
}

export function cancelDownload(id: string): Promise<unknown> {
  return api.post(`/downloads/${encodeURIComponent(id)}/cancel`)
}

export function retryDownload(id: string): Promise<DownloadJob> {
  return api.post<DownloadJob>(`/downloads/${encodeURIComponent(id)}/retry`)
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd web && npx vitest run src/lib/downloadStore.test.ts`
Expected: PASS.

- [ ] **Step 6: Typecheck + commit**

Run: `cd web && npm run build`
Expected: build succeeds.
```bash
git add web/src/lib/downloadStore.ts web/src/lib/downloadStore.test.ts web/src/lib/downloadApi.ts
git commit -m "feat(web): download store (WS-driven) and download REST helpers"
```

---

## Task 14: `DownloadTray` component + sidebar/player-bar entry points

**Files:**
- Create: `web/src/components/DownloadTray.tsx`
- Test: `web/src/components/DownloadTray.test.tsx`
- Modify: `web/src/components/Sidebar.tsx` (add a ⟳ Downloads entry opening the tray)
- Modify: `web/src/components/PlayerBar.tsx` (enable the Downloads button)

**Interfaces:**
- `DownloadTray` renders only when `useUI.rightPanel === 'downloads'` (mutually exclusive with `PlayQueue`, which renders only for `'queue'`). It shows all jobs from `useDownloads` (active + recent done), each with a progress bar + a cancel button (active) or a retry button (failed). Cancel/retry call `downloadApi`.

- [ ] **Step 1: Write the failing tray test**

Create `web/src/components/DownloadTray.test.tsx`:
```tsx
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { DownloadTray } from './DownloadTray'
import { useDownloads } from '../lib/downloadStore'
import { useUI } from '../lib/uiStore'
import type { DownloadJob } from '../lib/types'

vi.mock('../lib/downloadApi', () => ({
  cancelDownload: vi.fn(() => Promise.resolve()),
  retryDownload: vi.fn(() => Promise.resolve({} as DownloadJob)),
}))
import { cancelDownload, retryDownload } from '../lib/downloadApi'

function job(p: Partial<DownloadJob>): DownloadJob {
  return {
    id: 'j1', dedupKey: 'dk', status: 'running', progress: 40, downloaderName: 'spotdl',
    priority: 0, attempts: 0, source: 'spotify', externalId: 'sp1', playWhenReady: false,
    createdAt: 1, startedAt: 0, finishedAt: 0, ...p,
  }
}

describe('DownloadTray', () => {
  beforeEach(() => {
    useDownloads.setState({ jobs: {} })
    useUI.setState({ rightPanel: 'downloads' })
  })

  it('renders nothing when the panel is not downloads', () => {
    useUI.setState({ rightPanel: 'queue' })
    const { container } = render(<DownloadTray />)
    expect(container.firstChild).toBeNull()
  })

  it('lists jobs and cancels an active one', () => {
    useDownloads.getState().upsert(job({ id: 'j1', status: 'running', progress: 40, title: 'Song' } as Partial<DownloadJob>))
    render(<DownloadTray />)
    expect(screen.getByText('Download Tray')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /cancel/i }))
    expect(cancelDownload).toHaveBeenCalledWith('j1')
  })

  it('retries a failed job', () => {
    useDownloads.getState().upsert(job({ id: 'j2', status: 'failed', progress: 0 }))
    render(<DownloadTray />)
    fireEvent.click(screen.getByRole('button', { name: /retry/i }))
    expect(retryDownload).toHaveBeenCalledWith('j2')
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/components/DownloadTray.test.tsx`
Expected: FAIL — cannot resolve `./DownloadTray`.

- [ ] **Step 3: Write the DownloadTray**

Create `web/src/components/DownloadTray.tsx`:
```tsx
import { useDownloads } from '../lib/downloadStore'
import { useUI } from '../lib/uiStore'
import { cancelDownload, retryDownload } from '../lib/downloadApi'
import type { DownloadJob } from '../lib/types'

function statusLabel(j: DownloadJob): string {
  switch (j.status) {
    case 'queued':
      return 'Queued'
    case 'running':
      return j.progress >= 0 ? `${j.progress}%` : 'Downloading…'
    case 'completed':
      return 'Done'
    case 'failed':
      return j.error ? `Failed: ${j.error}` : 'Failed'
    case 'canceled':
      return 'Canceled'
  }
}

function ProgressBar({ progress }: { progress: number }) {
  // progress < 0 → indeterminate; otherwise determinate width.
  if (progress < 0) {
    return <div className="h-1 w-full overflow-hidden rounded bg-neutral-800"><div className="h-full w-1/3 animate-pulse bg-accent" /></div>
  }
  return (
    <div className="h-1 w-full overflow-hidden rounded bg-neutral-800">
      <div className="h-full bg-accent" style={{ width: `${progress}%` }} />
    </div>
  )
}

export function DownloadTray() {
  const rightPanel = useUI((s) => s.rightPanel)
  const closePanel = useUI((s) => s.closePanel)
  const jobs = useDownloads((s) => s.jobs)

  if (rightPanel !== 'downloads') return null

  const list = Object.values(jobs).sort((a, b) => b.createdAt - a.createdAt)

  return (
    <aside className="absolute right-0 top-0 z-20 flex h-full w-80 flex-col border-l border-neutral-800 bg-neutral-950/95 backdrop-blur">
      <div className="flex items-center justify-between border-b border-neutral-800 p-4">
        <h2 className="text-lg font-bold">Download Tray</h2>
        <button type="button" aria-label="Close downloads" onClick={closePanel} className="text-neutral-400 hover:text-white">
          ✕
        </button>
      </div>
      <div className="flex-1 overflow-auto p-2">
        {list.length === 0 && <div className="px-2 py-4 text-sm text-neutral-500">No downloads yet.</div>}
        <ul className="space-y-2">
          {list.map((j) => {
            const active = j.status === 'queued' || j.status === 'running'
            return (
              <li key={j.id} className="rounded px-2 py-2 hover:bg-neutral-900">
                <div className="flex items-center gap-2">
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-sm">{j.externalId}</div>
                    <div className="truncate text-xs text-neutral-400">{statusLabel(j)}</div>
                  </div>
                  {active && (
                    <button
                      type="button"
                      aria-label={`Cancel ${j.id}`}
                      onClick={() => void cancelDownload(j.id)}
                      className="text-neutral-500 hover:text-accent"
                    >
                      Cancel
                    </button>
                  )}
                  {(j.status === 'failed' || j.status === 'canceled') && (
                    <button
                      type="button"
                      aria-label={`Retry ${j.id}`}
                      onClick={() => void retryDownload(j.id)}
                      className="text-neutral-400 hover:text-accent"
                    >
                      Retry
                    </button>
                  )}
                </div>
                {active && <div className="mt-1"><ProgressBar progress={j.progress} /></div>}
              </li>
            )
          })}
        </ul>
      </div>
    </aside>
  )
}
```

- [ ] **Step 4: Add the sidebar ⟳ Downloads entry**

Replace `web/src/components/Sidebar.tsx`:
```tsx
import { NavLink } from 'react-router-dom'
import { useUI } from '../lib/uiStore'
import { useDownloads } from '../lib/downloadStore'

const items = [
  { to: '/search', label: 'Search' },
  { to: '/library', label: 'Library' },
  { to: '/settings', label: 'Settings' },
]

export function Sidebar() {
  const togglePanel = useUI((s) => s.togglePanel)
  const rightPanel = useUI((s) => s.rightPanel)
  const activeCount = useDownloads((s) => s.active().length)

  return (
    <nav className="w-56 shrink-0 border-r border-neutral-800 p-4 space-y-1">
      <div className="text-xl font-bold mb-4 text-accent">Crate</div>
      {items.map((i) => (
        <NavLink
          key={i.to}
          to={i.to}
          className={({ isActive }) =>
            `block rounded px-3 py-2 ${isActive ? 'bg-accent/20 text-accent' : 'hover:bg-neutral-800'}`
          }
        >
          {i.label}
        </NavLink>
      ))}
      <button
        type="button"
        onClick={() => togglePanel('downloads')}
        className={`mt-2 flex w-full items-center justify-between rounded px-3 py-2 text-left ${
          rightPanel === 'downloads' ? 'bg-accent/20 text-accent' : 'hover:bg-neutral-800'
        }`}
      >
        <span>⟳ Downloads</span>
        {activeCount > 0 && (
          <span className="rounded-full bg-accent px-1.5 text-xs text-white">{activeCount}</span>
        )}
      </button>
    </nav>
  )
}
```

- [ ] **Step 5: Enable the player-bar Downloads button**

In `web/src/components/PlayerBar.tsx`, REPLACE the disabled Downloads button:
```tsx
        <button
          type="button"
          disabled
          title="Downloads (coming in M3)"
          className="cursor-not-allowed rounded px-2 py-1 text-sm text-neutral-600"
        >
          Downloads
        </button>
```
with:
```tsx
        <button
          type="button"
          onClick={() => togglePanel('downloads')}
          className={`rounded px-2 py-1 text-sm ${rightPanel === 'downloads' ? 'text-accent' : 'text-neutral-300 hover:text-white'}`}
        >
          Downloads
        </button>
```
(`togglePanel` and `rightPanel` are already pulled from `useUI` in `PlayerBar`.)

- [ ] **Step 6: Run tests + typecheck**

Run: `cd web && npx vitest run src/components/DownloadTray.test.tsx`
Expected: PASS.

Run: `cd web && npm run build`
Expected: build succeeds (Sidebar + PlayerBar typecheck with the new imports).

- [ ] **Step 7: Commit**

```bash
git add web/src/components/DownloadTray.tsx web/src/components/DownloadTray.test.tsx web/src/components/Sidebar.tsx web/src/components/PlayerBar.tsx
git commit -m "feat(web): Download Tray panel with sidebar and player-bar entry points"
```

---

## Task 15: Make `ExternalRow` functional — ↓ download, ⟳ progress ring, ✓ in-place flip

**Files:**
- Modify: `web/src/components/ExternalRow.tsx` (replace the M2 download seam with the live 4-state row)
- Modify: `web/src/components/ExternalRow.test.tsx` (add ↓/⟳/✓ state tests)

**Interfaces:**
- The row cross-references `useDownloads.byExternal(source, externalId)` for an active/recent job. The 4 states (spec §7):
  - **✓ In Library** — `match.status==='in_library'` with a `libraryTrackId` → click plays the matched track (UNCHANGED from M2). A completed job that set `libraryTrackId` is treated the same (the row flips to ✓ in place via WS without a refetch).
  - **⟳ Queued/Downloading** — an active job exists → a progress ring (determinate when `progress>=0`, indeterminate spinner when `progress<0`).
  - **↓ Available** — no match, no active job → a ↓ button that POSTs `/downloads`.
  - **(—) external-only** — no match, no downloader/job and POST not attempted → plain.
- For MVP, every not-in-library track is assumed downloadable (spotDL's cheap heuristic accepts any artist+title), so the ↓ button shows whenever there is no match and no active job.

- [ ] **Step 1: Update the test**

Replace `web/src/components/ExternalRow.test.tsx`:
```tsx
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { ExternalRow } from './ExternalRow'
import { useDownloads } from '../lib/downloadStore'
import type { ExternalResult, DownloadJob } from '../lib/types'

vi.mock('../lib/downloadApi', () => ({
  postDownload: vi.fn(() => Promise.resolve({ id: 'job-sp1', source: 'spotify', externalId: 'sp1', status: 'queued', progress: 0, dedupKey: 'dk', downloaderName: 'spotdl', priority: 0, attempts: 0, playWhenReady: false, createdAt: 1, startedAt: 0, finishedAt: 0 } as DownloadJob)),
}))
import { postDownload } from '../lib/downloadApi'

const playTrackList = vi.fn()
vi.mock('../lib/playerStore', () => ({
  usePlayer: (sel: (s: { playTrackList: typeof playTrackList }) => unknown) => sel({ playTrackList }),
}))

function result(p: Partial<ExternalResult>): ExternalResult {
  return { source: 'spotify', externalId: 'sp1', title: 'Song', artist: 'Artist', album: 'Album', durationMs: 200000, type: 'track', ...p }
}

function job(p: Partial<DownloadJob>): DownloadJob {
  return { id: 'j1', dedupKey: 'dk', status: 'running', progress: 50, downloaderName: 'spotdl', priority: 0, attempts: 0, source: 'spotify', externalId: 'sp1', playWhenReady: false, createdAt: 1, startedAt: 0, finishedAt: 0, ...p }
}

describe('ExternalRow', () => {
  beforeEach(() => {
    useDownloads.setState({ jobs: {} })
    vi.clearAllMocks()
  })

  it('in-library row shows ✓ and plays the matched track', () => {
    render(<ExternalRow result={result({ match: { status: 'in_library', libraryTrackId: 't3', method: 'isrc', confidence: 1 } })} />)
    expect(screen.getByTitle(/in library/i)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button'))
    expect(playTrackList).toHaveBeenCalled()
  })

  it('not-in-library row shows ↓ and posts a download', async () => {
    render(<ExternalRow result={result({ match: { status: 'not_in_library', libraryTrackId: '', method: 'none', confidence: 0 } })} />)
    const dl = screen.getByRole('button', { name: /download/i })
    fireEvent.click(dl)
    await waitFor(() => expect(postDownload).toHaveBeenCalled())
  })

  it('active job shows the ⟳ progress ring (determinate)', () => {
    useDownloads.getState().upsert(job({ status: 'running', progress: 50 }))
    render(<ExternalRow result={result({})} />)
    expect(screen.getByLabelText(/downloading/i)).toBeInTheDocument()
  })

  it('completed job with libraryTrackId flips to ✓ and plays', () => {
    useDownloads.getState().upsert(job({ status: 'completed', progress: 100, libraryTrackId: 't9' }))
    render(<ExternalRow result={result({})} />)
    expect(screen.getByTitle(/in library/i)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button'))
    expect(playTrackList).toHaveBeenCalled()
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/components/ExternalRow.test.tsx`
Expected: FAIL — the ↓/⟳ affordances and completed-job flip don't exist yet.

- [ ] **Step 3: Rewrite `ExternalRow.tsx`**

Replace `web/src/components/ExternalRow.tsx`:
```tsx
import type { ExternalResult, Track } from '../lib/types'
import { formatDuration } from '../lib/types'
import { usePlayer } from '../lib/playerStore'
import { useDownloads } from '../lib/downloadStore'
import { postDownload } from '../lib/downloadApi'

interface Props {
  result: ExternalResult
}

// trackFromMatch synthesizes a minimal library Track from the external metadata,
// using the matched library track id so the stream proxy can play it.
function trackFromMatch(r: ExternalResult, libraryTrackId: string): Track {
  return {
    id: libraryTrackId,
    title: r.title,
    albumId: '',
    album: r.album,
    artistId: '',
    artist: r.artist,
    coverArtId: r.coverArtId ?? '',
    trackNumber: 0,
    discNumber: 0,
    durationMs: r.durationMs,
    bitRate: 0,
    suffix: '',
    contentType: '',
    isrc: r.isrc,
  }
}

// ProgressRing renders a determinate ring (progress>=0) or an indeterminate
// spinner (progress<0). It is the ⟳ state of the result row.
function ProgressRing({ progress }: { progress: number }) {
  const label = progress >= 0 ? `Downloading ${progress}%` : 'Downloading'
  if (progress < 0) {
    return (
      <span aria-label={label} title={label} className="inline-block h-4 w-4 animate-spin rounded-full border-2 border-neutral-600 border-t-accent" />
    )
  }
  const deg = Math.round((progress / 100) * 360)
  return (
    <span
      aria-label={label}
      title={label}
      className="inline-block h-4 w-4 rounded-full"
      style={{ background: `conic-gradient(rgb(var(--color-accent)) ${deg}deg, rgb(64 64 64) ${deg}deg)` }}
    />
  )
}

export function ExternalRow({ result }: Props) {
  const playTrackList = usePlayer((s) => s.playTrackList)
  const job = useDownloads((s) => s.byExternal(result.source, result.externalId))

  // A completed job that matched a library track makes the row in-library too.
  const matchedTrackId =
    (result.match?.status === 'in_library' && result.match.libraryTrackId) ||
    (job?.status === 'completed' && job.libraryTrackId) ||
    ''
  const inLibrary = !!matchedTrackId
  const active = !!job && (job.status === 'queued' || job.status === 'running')

  function onDownload() {
    void postDownload({
      source: result.source,
      externalId: result.externalId,
      artist: result.artist,
      title: result.title,
      album: result.album,
      isrc: result.isrc,
    }).then((j) => useDownloads.getState().upsert(j))
  }

  const cover = result.coverUrl ? (
    <img src={result.coverUrl} alt="" className="h-9 w-9 rounded object-cover" />
  ) : (
    <div className="h-9 w-9 rounded bg-neutral-800" />
  )

  let action: React.ReactNode
  if (inLibrary) {
    action = <span title="In library" className="text-accent">✓</span>
  } else if (active) {
    action = <ProgressRing progress={job!.progress} />
  } else {
    action = (
      <button
        type="button"
        aria-label={`Download ${result.title}`}
        onClick={(e) => {
          e.stopPropagation()
          onDownload()
        }}
        className="text-neutral-400 hover:text-accent"
      >
        ↓
      </button>
    )
  }

  const body = (
    <>
      {cover}
      <span className="flex-1 truncate">
        <span className="block truncate text-sm font-medium">{result.title}</span>
        <span className="block truncate text-xs text-neutral-400">{result.artist}</span>
      </span>
      {action}
      <span className="w-12 text-right text-xs text-neutral-500">{formatDuration(result.durationMs)}</span>
    </>
  )

  if (inLibrary) {
    return (
      <button
        type="button"
        onClick={() => playTrackList([trackFromMatch(result, matchedTrackId)], 0)}
        className="group flex w-full items-center gap-3 rounded px-2 py-1.5 text-left text-neutral-200 hover:bg-neutral-800"
      >
        {body}
      </button>
    )
  }
  return <div className="flex w-full items-center gap-3 rounded px-2 py-1.5 text-neutral-300">{body}</div>
}
```

> NOTE: `import type React` is not needed for the `React.ReactNode` annotation under the new JSX transform only if `React` is in scope; to be safe, add `import type { ReactNode } from 'react'` and change `React.ReactNode` to `ReactNode`. Apply that change if `npm run build` complains about `React` being undefined.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run src/components/ExternalRow.test.tsx`
Expected: PASS (✓ plays matched; ↓ posts; ⟳ determinate ring for active; completed-with-libraryTrackId flips to ✓ and plays).

- [ ] **Step 5: Typecheck + commit**

Run: `cd web && npm run build`
Expected: build succeeds.
```bash
git add web/src/components/ExternalRow.tsx web/src/components/ExternalRow.test.tsx
git commit -m "feat(web): functional ExternalRow with download, progress ring, and in-place check flip"
```

---
