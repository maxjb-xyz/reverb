# Crate M2 — Everywhere Search Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Each task is a self-contained unit: a fresh implementer with ZERO prior context can complete it from the file paths, interfaces, and complete code given here. Tasks are ordered so the matching fixture corpus is authored BEFORE the MatchingService implementation.

**Goal:** Let a user search "Everywhere" (external sources, MVP = Spotify) and immediately see what they already have versus what they don't. This is the matching spine: external + match domain types → `SearchSource` interface (+conformance) → fan-out aggregator (per-source goroutine deadlines, streaming envelopes) → Spotify adapter (client-credentials OAuth, ISRC) → the `MatchingService` crown jewel (ISRC→MBID→normalized-fuzzy+duration, cache-first via `match_cache`/`library_version`) → SSE endpoint (each result pre-matched) → composition wiring → frontend SSE client + Everywhere UI (append-in-stable-sections, per-source chips, ✓/plain rows; NO download — that's M3).

**Architecture:** Builds on M0 (binary serving `/api/v1` + embedded SPA) and M1 (`core`, `library` + Subsonic adapter, REST/stream handlers, frontend player). M2 adds a `search` package (interface + conformance + aggregator), a `search/spotify` adapter, a `matching` package (Normalize + MatchingService + fixture corpus), a `0002` migration with `match_cache` and a `library_version` accessor, an SSE endpoint on the existing chi router, and a frontend `SearchStream` (EventSource) plus Everywhere render in `Search.tsx`. Library data is NEVER persisted: the matcher queries the live `LibraryAdapter.Search` and caches only the match DECISION. Everywhere search is SSE — a distinct transport from REST and WS.

**Tech Stack:** Go 1.23 (toolchain 1.26 present), chi v5, `net/http`, `net/http/httptest` (no live Spotify in tests), `encoding/base64`, `crypto/md5` (unused here), `golang.org/x/text` is NOT added — unicode normalization is done with `unicode` + manual folding to avoid a new dependency; sqlc v1.31.1 (installed) for `match_cache` queries. React 19, TypeScript ~6, Vite 8, Vitest 4, Tailwind 3.4, React Router 6, TanStack Query 5, Zustand 4 (all already in `web/`); browser-native `EventSource` for SSE (stubbed in tests, no real network).

## Global Constraints

- Go module path: `github.com/maximusjb/crate` (verbatim in every `import`). Go version floor `go 1.23`. SQLite driver: `modernc.org/sqlite` only.
- **ISRC is DATA on `ExternalResult`** (no capability flag). Matching priority: ISRC → MBID → normalized-fuzzy + duration (±2–3s). `Normalize` is a PURE function, shared (future `dedup_key`), SYMMETRIC (applied to both external and library sides), and must NOT over-strip version qualifiers (a live cut must not collapse onto the studio cut).
- `match_cache` stores NEGATIVE matches (a `not_in_library` decision is cached too); invalidated by a monotonic `library_version` (in `settings`, default 1; bumped on scan/download in M3, not M2).
- **Library data is never persisted.** Matching queries the `LibraryAdapter.Search` live and caches only the match decision (the row in `match_cache`), never the library track itself.
- **Everywhere search = SSE** (distinct transport). Each source runs in its own goroutine with an individual `context.WithTimeout`; results stream per-source as they arrive; one slow/down source never blocks others. Frontend renders append-in-stable-sections (Tracks/Albums/Artists), never reflowing already-shown rows.
- **Spotify:** client-credentials OAuth (Basic auth = base64(client_id:client_secret) to `accounts.spotify.com/api/token`, grant_type=client_credentials; token cached with expiry). `client_secret` comes from `CRATE_SPOTIFY_CLIENT_SECRET` env (overrides `config_json`) and is NEVER sent to the browser. Search sources are registered EXPLICITLY at the composition root (no `init()` side-effects). Injectable base URLs + `*http.Client` so tests point at `httptest`.
- **M2 result rows: ✓ in-library vs plain.** No ↓/⟳ affordances (downloaders are M3). Leave the download seam clearly marked in `Search.tsx`.
- **Tests:** TDD always (failing test → confirm red → minimal code → confirm green → conventional-commit). Go adapter/matching tests use `httptest` + recorded JSON under the PACKAGE's `testdata/` (never `..`). Run Go tests with `go test ./cmd/... ./internal/...` (NOT `./...`). Frontend Vitest with stubbed `EventSource`/`fetch` (no real network); typecheck via `cd web && npm run build`. Every `SearchSource` passes `search.RunConformance(t, source)`.
- **sqlc generated code is committed.** Regenerate via the installed `sqlc` binary; fall back to `go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.27.0 generate` if `sqlc` is not on PATH.
- **SSE framing:** `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`; use `http.Flusher`; write `data: <json>\n\n` per event and flush; stop when `r.Context().Done()`; the aggregator closes its channel so the handler returns. The aggregator is unit-testable WITHOUT HTTP (assert on envelopes from the channel); the SSE handler is testable with `httptest` (parse the streamed events).
- **`EventSource`** only hits same-origin `/api/v1/search/everywhere?q=&type=` and carries the session cookie automatically. Library mode stays a normal REST query (`GET /api/v1/library/search`) — keep it as-is.

---

## File Structure

**Go (backend) — created/modified in M2:**

| Path | Responsibility |
|---|---|
| `internal/core/external.go` | NEW: `ExternalResult`, `ExternalAlbum`, `MatchResult`, `MatchStatus`/`MatchMethod` consts. JSON camelCase. |
| `internal/core/external_test.go` | NEW: JSON round-trip + camelCase key assertions. |
| `internal/search/search.go` | NEW: `SearchSource` interface (embeds `registry.Plugin`), `DiscographyProvider` optional interface, `Envelope`, `EnvelopeStatus`. |
| `internal/search/conformance.go` | NEW: `RunConformance(t, SearchSource)`. |
| `internal/search/conformance_test.go` | NEW: a fake source proving conformance passes. |
| `internal/search/aggregator.go` | NEW: `Matcher` interface + `Aggregator` fan-out (per-source goroutine + `context.WithTimeout`, channel of `Envelope`). |
| `internal/search/aggregator_test.go` | NEW: channel-level tests (timeout isolation, per-source envelopes, pre-match applied, channel closes). |
| `internal/search/spotify/client.go` | NEW: client-credentials OAuth (token fetch + cache/expiry), injectable base URLs + `*http.Client`. |
| `internal/search/spotify/dto.go` | NEW: Spotify search/album JSON DTOs. |
| `internal/search/spotify/adapter.go` | NEW: `Adapter` implementing `registry.Plugin` + `search.SearchSource`; maps to `core.ExternalResult` incl. ISRC + cover. |
| `internal/search/spotify/client_test.go` | NEW: token Basic-auth + cache tests (httptest). |
| `internal/search/spotify/adapter_test.go` | NEW: search/getAlbum mapping + `search.RunConformance` (httptest + recorded JSON). |
| `internal/search/spotify/testdata/*.json` | NEW: recorded `token`, `search_tracks`, `search_albums`, `search_artists`, `album` responses. |
| `internal/matching/normalize.go` | NEW: pure `Normalize()` (+ helpers). |
| `internal/matching/normalize_test.go` | NEW: table tests driven by fixtures + inline cases. |
| `internal/matching/matching.go` | NEW: `LibrarySearcher` interface, `MatchCacheStore` interface, `Service`, `Match()` priority chain, cache-first, `library_version` invalidation. |
| `internal/matching/matching_test.go` | NEW: table tests driven by the fixture corpus (ISRC/MBID/fuzzy/duration/negative) + cache behavior. |
| `internal/matching/testdata/*.json` | NEW: the enumerated fixture corpus (8 files). |
| `internal/store/migrations/0002_match_cache.sql` | NEW: additive migration creating `match_cache`. |
| `internal/store/queries/match_cache.sql` | NEW: get/upsert/delete-by-source/clear queries. |
| `internal/store/queries/library_version.sql` | NEW: `library_version` get/set helper (lives in `settings`). |
| `internal/store/db/*` | REGENERATED by sqlc (committed). |
| `internal/store/store.go` | MODIFY: add `LibraryVersion(ctx)` accessor returning 1 when absent. |
| `internal/store/store_test.go` | NEW: `LibraryVersion` default + set/get test. |
| `internal/api/search.go` | NEW: `GET /api/v1/search/everywhere` SSE handler. |
| `internal/api/search_test.go` | NEW: SSE handler test (parse streamed events). |
| `internal/api/server.go` | MODIFY: add `SearchAggregator` to `Deps`; mount the SSE route. |
| `internal/api/auth_flow_test.go` | MODIFY: leave `SearchAggregator` nil in the `testServer` Deps literal (no change needed — it's a pointer, zero value nil). |
| `cmd/crate/search_wiring.go` | NEW: `buildSearchSources` (build enabled `search` adapter_instances + env secret override) + `wireSpotify` registration. |
| `cmd/crate/search_wiring_test.go` | NEW: env-override + enabled-filter tests. |
| `cmd/crate/main.go` | MODIFY: register spotify factory, build active search sources, construct the aggregator + matching service, pass into `api.Deps.SearchAggregator`. |

**React (frontend) — created/modified in M2, under `web/`:**

| Path | Responsibility |
|---|---|
| `src/lib/types.ts` | MODIFY: add `ExternalResult`, `ExternalAlbum`, `MatchResult`, `MatchStatus`, plus `SearchEnvelope`. |
| `src/lib/searchStream.ts` | NEW: `SearchStream` class wrapping `EventSource`; per-source envelope callbacks; `close()` on unmount. |
| `src/lib/searchStream.test.ts` | NEW: Vitest with a stubbed `EventSource` (no real network). |
| `src/lib/everywhereStore.ts` | NEW: pure reducer/state for append-in-stable-sections + per-source status (no Zustand needed; a tested reducer + a tiny hook). |
| `src/lib/everywhereStore.test.ts` | NEW: reducer tests (append, dedup by ISRC/normalized key, never-reorder, status transitions). |
| `src/components/ExternalRow.tsx` | NEW: external track row — ✓ when `match.status==='in_library'` (click plays the matched library track) else a plain row; marked M3 download seam. |
| `src/components/ExternalRow.test.tsx` | NEW: RTL test (✓ in-library plays matched track; plain row otherwise). |
| `src/components/SourceChips.tsx` | NEW: per-source status chips ("Spotify ✓ · … · timed out"). |
| `src/routes/Search.tsx` | MODIFY: activate the Everywhere toggle; Library mode unchanged (REST); Everywhere mode uses `SearchStream` + the reducer, rendering stable sections + chips. |
| `src/routes/Search.test.tsx` | MODIFY: add an Everywhere-mode test (stubbed EventSource drives sections + chips). |

---

## Task 1: External + match domain types (`internal/core/external.go`)

**Files:**
- Create: `internal/core/external.go`
- Test: `internal/core/external_test.go`

**Interfaces:**
- Consumes: nothing (pure types).
- Produces (exact, consumed by `search`, `matching`, `spotify`, `api`, frontend mirror):
  ```go
  type MatchStatus string
  type MatchMethod string
  type MatchResult struct {
      Status         MatchStatus
      LibraryTrackID string
      Method         MatchMethod
      Confidence     float64
  }
  type ExternalResult struct {
      Source, ExternalID, Title, Artist, Album string
      DurationMs int
      ISRC, MBID, CoverURL, CoverArtID string
      Type EntityType
      Match *MatchResult
  }
  type ExternalAlbum struct {
      Source, ExternalID, Name, Artist, CoverURL string
      Year int
      Tracks []ExternalResult
  }
  ```

- [ ] **Step 1: Write the failing test**

Create `internal/core/external_test.go`:
```go
package core

import (
	"encoding/json"
	"testing"
)

func TestExternalResultJSONRoundTrip(t *testing.T) {
	in := ExternalResult{
		Source: "spotify", ExternalID: "sp1", Title: "Song", Artist: "Artist",
		Album: "Album", DurationMs: 210000, ISRC: "USX1234", MBID: "mb-1",
		CoverURL: "https://img/x.jpg", CoverArtID: "", Type: EntityTrack,
		Match: &MatchResult{Status: MatchInLibrary, LibraryTrackID: "t1", Method: MatchISRC, Confidence: 1.0},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out ExternalResult
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.ExternalID != "sp1" || out.Match == nil || out.Match.LibraryTrackID != "t1" {
		t.Fatalf("round trip mismatch: %+v", out)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for _, k := range []string{"externalId", "durationMs", "coverUrl", "match"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("expected camelCase key %q, got %v", k, m)
		}
	}
	mm := m["match"].(map[string]any)
	if _, ok := mm["libraryTrackId"]; !ok {
		t.Fatalf("expected match.libraryTrackId, got %v", mm)
	}
}

func TestMatchConstants(t *testing.T) {
	if MatchInLibrary != "in_library" || MatchNotInLibrary != "not_in_library" || MatchUnknown != "unknown" {
		t.Fatal("match status constant drift")
	}
	if MatchISRC != "isrc" || MatchMBID != "mbid" || MatchFuzzy != "fuzzy" || MatchNone != "none" {
		t.Fatal("match method constant drift")
	}
}

func TestExternalResultOmitsNilMatch(t *testing.T) {
	b, _ := json.Marshal(ExternalResult{Source: "s", ExternalID: "e"})
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if _, ok := m["match"]; ok {
		t.Fatalf("nil Match must be omitted, got %v", m)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/ -run External -v`
Expected: FAIL — `undefined: ExternalResult` / `undefined: MatchInLibrary`.

- [ ] **Step 3: Write the implementation**

Create `internal/core/external.go`:
```go
package core

// MatchStatus is the verdict of MatchingService for an external result.
type MatchStatus string

const (
	MatchInLibrary    MatchStatus = "in_library"
	MatchNotInLibrary MatchStatus = "not_in_library"
	MatchUnknown      MatchStatus = "unknown"
)

// MatchMethod records which rung of the priority chain decided the match.
type MatchMethod string

const (
	MatchISRC  MatchMethod = "isrc"
	MatchMBID  MatchMethod = "mbid"
	MatchFuzzy MatchMethod = "fuzzy"
	MatchNone  MatchMethod = "none"
)

// MatchResult is attached to an ExternalResult after MatchingService runs.
// LibraryTrackID is set only when Status == MatchInLibrary. Confidence is a
// documented heuristic in [0,1]: 1.0 for ISRC/MBID exact, ~0.6–0.9 for fuzzy.
type MatchResult struct {
	Status         MatchStatus `json:"status"`
	LibraryTrackID string      `json:"libraryTrackId"`
	Method         MatchMethod `json:"method"`
	Confidence     float64     `json:"confidence"`
}

// ExternalResult is one search hit from an external SearchSource. ISRC and MBID
// are DATA (optional) — the matcher uses them when non-empty. Match is filled in
// by MatchingService before the result is emitted to the client.
type ExternalResult struct {
	Source     string      `json:"source"`
	ExternalID string      `json:"externalId"`
	Title      string      `json:"title"`
	Artist     string      `json:"artist"`
	Album      string      `json:"album"`
	DurationMs int         `json:"durationMs"`
	ISRC       string      `json:"isrc,omitempty"`
	MBID       string      `json:"mbid,omitempty"`
	CoverURL   string      `json:"coverUrl,omitempty"`
	CoverArtID string      `json:"coverArtId,omitempty"`
	Type       EntityType  `json:"type"`
	Match      *MatchResult `json:"match,omitempty"`
}

// ExternalAlbum is an album fetched from a SearchSource (GetAlbum).
type ExternalAlbum struct {
	Source     string           `json:"source"`
	ExternalID string           `json:"externalId"`
	Name       string           `json:"name"`
	Artist     string           `json:"artist"`
	CoverURL   string           `json:"coverUrl,omitempty"`
	Year       int              `json:"year"`
	Tracks     []ExternalResult `json:"tracks"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/ -v`
Expected: PASS (existing M1 core tests + the new External tests).

- [ ] **Step 5: Commit**

```bash
git add internal/core/external.go internal/core/external_test.go
git commit -m "feat(core): external result and match domain types"
```

---

## Task 2: SearchSource interface + conformance suite (`internal/search`)

**Files:**
- Create: `internal/search/search.go`, `internal/search/conformance.go`
- Test: `internal/search/conformance_test.go`

**Interfaces:**
- Consumes: `internal/registry` (`registry.Plugin`), `internal/core`.
- Produces:
  ```go
  type SearchSource interface {
      registry.Plugin
      Search(ctx context.Context, q string, t core.EntityType) ([]core.ExternalResult, error)
      GetAlbum(ctx context.Context, externalID string) (core.ExternalAlbum, error)
  }
  type DiscographyProvider interface { // optional (P2), not required by conformance
      GetArtistDiscography(ctx context.Context, externalID string) ([]core.ExternalAlbum, error)
  }
  type EnvelopeStatus string
  const ( StatusOK EnvelopeStatus = "ok"; StatusTimeout EnvelopeStatus = "timeout"; StatusError EnvelopeStatus = "error" )
  type Envelope struct {
      Source  string
      Status  EnvelopeStatus
      Results []core.ExternalResult
      Cursor  string
      Error   string
  }
  func RunConformance(t *testing.T, s SearchSource)
  ```

- [ ] **Step 1: Write the failing conformance test (with a fake source)**

Create `internal/search/conformance_test.go`:
```go
package search

import (
	"context"
	"testing"

	"github.com/maximusjb/crate/internal/core"
	"github.com/maximusjb/crate/internal/registry"
)

type fakeSource struct{}

func (fakeSource) Type() string                             { return "search" }
func (fakeSource) Name() string                             { return "fake" }
func (fakeSource) ConfigSchema() registry.ConfigSchema      { return registry.ConfigSchema{} }
func (fakeSource) Init(cfg map[string]any) error            { return nil }
func (fakeSource) TestConnection(ctx context.Context) error { return nil }
func (fakeSource) Search(ctx context.Context, q string, t core.EntityType) ([]core.ExternalResult, error) {
	return []core.ExternalResult{{Source: "fake", ExternalID: "e1", Title: "Song", Type: t}}, nil
}
func (fakeSource) GetAlbum(ctx context.Context, externalID string) (core.ExternalAlbum, error) {
	return core.ExternalAlbum{Source: "fake", ExternalID: externalID, Name: "Album", Tracks: []core.ExternalResult{}}, nil
}

func TestFakeSourceConformance(t *testing.T) {
	RunConformance(t, fakeSource{})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/search/ -v`
Expected: FAIL — `undefined: RunConformance` / `undefined: SearchSource`.

- [ ] **Step 3: Write the interface**

Create `internal/search/search.go`:
```go
// Package search defines the SearchSource contract, a conformance suite, and a
// fan-out aggregator that streams per-source results (each pre-matched) over a
// channel of Envelopes. Adapters live in subpackages (e.g. search/spotify).
package search

import (
	"context"

	"github.com/maximusjb/crate/internal/core"
	"github.com/maximusjb/crate/internal/registry"
)

// SearchSource is an external catalog (MVP: Spotify). ISRC/MBID are DATA on the
// returned results, not capabilities.
type SearchSource interface {
	registry.Plugin
	Search(ctx context.Context, q string, t core.EntityType) ([]core.ExternalResult, error)
	GetAlbum(ctx context.Context, externalID string) (core.ExternalAlbum, error)
}

// DiscographyProvider is an OPTIONAL capability (P2 artist pages). Conformance
// does NOT require it; the aggregator/UI detect it via a type assertion.
type DiscographyProvider interface {
	GetArtistDiscography(ctx context.Context, externalID string) ([]core.ExternalAlbum, error)
}

// EnvelopeStatus is the per-source outcome streamed to the client.
type EnvelopeStatus string

const (
	StatusOK      EnvelopeStatus = "ok"
	StatusTimeout EnvelopeStatus = "timeout"
	StatusError   EnvelopeStatus = "error"
)

// Envelope is one per-source result batch. Each Result already carries its Match.
type Envelope struct {
	Source  string                `json:"source"`
	Status  EnvelopeStatus        `json:"status"`
	Results []core.ExternalResult `json:"results"`
	Cursor  string                `json:"cursor,omitempty"`
	Error   string                `json:"error,omitempty"`
}
```

- [ ] **Step 4: Write the conformance suite**

Create `internal/search/conformance.go`:
```go
package search

import (
	"context"
	"testing"

	"github.com/maximusjb/crate/internal/core"
)

// RunConformance exercises the SearchSource contract. Call it from each adapter's
// test package with a configured, ready-to-use source (pointed at httptest).
func RunConformance(t *testing.T, s SearchSource) {
	t.Helper()
	ctx := context.Background()

	t.Run("Plugin/identity", func(t *testing.T) {
		if s.Type() != "search" {
			t.Errorf("Type() = %q, want \"search\"", s.Type())
		}
		if s.Name() == "" {
			t.Error("Name() must not be empty")
		}
	})

	t.Run("Search/track-returns-results", func(t *testing.T) {
		res, err := s.Search(ctx, "test", core.EntityTrack)
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		for _, r := range res {
			if r.Source == "" || r.ExternalID == "" {
				t.Fatalf("result missing Source/ExternalID: %+v", r)
			}
		}
	})

	t.Run("Search/album-and-artist-do-not-error", func(t *testing.T) {
		if _, err := s.Search(ctx, "test", core.EntityAlbum); err != nil {
			t.Fatalf("Search(album): %v", err)
		}
		if _, err := s.Search(ctx, "test", core.EntityArtist); err != nil {
			t.Fatalf("Search(artist): %v", err)
		}
	})

	t.Run("GetAlbum/returns-album", func(t *testing.T) {
		al, err := s.GetAlbum(ctx, "al1")
		if err != nil {
			t.Fatalf("GetAlbum: %v", err)
		}
		if al.ExternalID == "" {
			t.Error("GetAlbum returned empty ExternalID")
		}
		// Tracks slice must be addressable (may be empty).
		_ = al.Tracks
	})
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/search/ -v`
Expected: PASS (`TestFakeSourceConformance` + subtests).

- [ ] **Step 6: Commit**

```bash
git add internal/search/search.go internal/search/conformance.go internal/search/conformance_test.go
git commit -m "feat(search): SearchSource interface, envelope types, and conformance suite"
```

---

## Task 3: Fan-out aggregator (per-source deadlines, streaming envelopes)

**Files:**
- Create: `internal/search/aggregator.go`
- Test: `internal/search/aggregator_test.go`

**Interfaces:**
- Consumes: `SearchSource` (Task 2), `core`.
- Produces:
  ```go
  // Matcher pre-matches a result; the MatchingService implements it (Task 9).
  type Matcher interface {
      Match(ctx context.Context, ext core.ExternalResult) (core.MatchResult, error)
  }
  type Aggregator struct { ... }
  func NewAggregator(sources []SearchSource, matcher Matcher, timeout time.Duration) *Aggregator
  // Stream fans out to each source in its own goroutine with an individual
  // context.WithTimeout, pre-matches each result, and emits one Envelope per
  // source on the returned channel; the channel is CLOSED when all sources
  // finish. A slow/down source never blocks others. Respects ctx cancellation.
  func (a *Aggregator) Stream(ctx context.Context, q string, t core.EntityType) <-chan Envelope
  ```

- [ ] **Step 1: Write the failing aggregator test**

Create `internal/search/aggregator_test.go`:
```go
package search

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maximusjb/crate/internal/core"
	"github.com/maximusjb/crate/internal/registry"
)

// scriptedSource returns canned results after an artificial delay.
type scriptedSource struct {
	name    string
	delay   time.Duration
	results []core.ExternalResult
	err     error
}

func (s *scriptedSource) Type() string                        { return "search" }
func (s *scriptedSource) Name() string                        { return s.name }
func (s *scriptedSource) ConfigSchema() registry.ConfigSchema { return registry.ConfigSchema{} }
func (s *scriptedSource) Init(map[string]any) error           { return nil }
func (s *scriptedSource) TestConnection(context.Context) error { return nil }
func (s *scriptedSource) GetAlbum(context.Context, string) (core.ExternalAlbum, error) {
	return core.ExternalAlbum{}, nil
}
func (s *scriptedSource) Search(ctx context.Context, q string, t core.EntityType) ([]core.ExternalResult, error) {
	select {
	case <-time.After(s.delay):
		return s.results, s.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// fakeMatcher marks any result with a non-empty ISRC as in_library.
type fakeMatcher struct{}

func (fakeMatcher) Match(ctx context.Context, ext core.ExternalResult) (core.MatchResult, error) {
	if ext.ISRC != "" {
		return core.MatchResult{Status: core.MatchInLibrary, LibraryTrackID: "lib-" + ext.ExternalID, Method: core.MatchISRC, Confidence: 1}, nil
	}
	return core.MatchResult{Status: core.MatchNotInLibrary, Method: core.MatchNone}, nil
}

func collect(ch <-chan Envelope) map[string]Envelope {
	out := map[string]Envelope{}
	for e := range ch {
		out[e.Source] = e
	}
	return out
}

func TestAggregatorPreMatchesAndEmitsPerSource(t *testing.T) {
	fast := &scriptedSource{name: "fast", delay: 0, results: []core.ExternalResult{
		{Source: "fast", ExternalID: "f1", Title: "A", ISRC: "USX1"},
		{Source: "fast", ExternalID: "f2", Title: "B"},
	}}
	other := &scriptedSource{name: "other", delay: 0, results: []core.ExternalResult{
		{Source: "other", ExternalID: "o1", Title: "C"},
	}}
	agg := NewAggregator([]SearchSource{fast, other}, fakeMatcher{}, time.Second)
	got := collect(agg.Stream(context.Background(), "q", core.EntityTrack))

	if len(got) != 2 {
		t.Fatalf("want 2 envelopes, got %d: %+v", len(got), got)
	}
	if got["fast"].Status != StatusOK || len(got["fast"].Results) != 2 {
		t.Fatalf("fast envelope wrong: %+v", got["fast"])
	}
	if got["fast"].Results[0].Match == nil || got["fast"].Results[0].Match.Status != core.MatchInLibrary {
		t.Fatalf("ISRC result not pre-matched: %+v", got["fast"].Results[0])
	}
	if got["fast"].Results[1].Match == nil || got["fast"].Results[1].Match.Status != core.MatchNotInLibrary {
		t.Fatalf("non-ISRC result not pre-matched: %+v", got["fast"].Results[1])
	}
}

func TestAggregatorTimeoutDoesNotBlockOthers(t *testing.T) {
	slow := &scriptedSource{name: "slow", delay: 200 * time.Millisecond, results: []core.ExternalResult{{Source: "slow", ExternalID: "s1"}}}
	fast := &scriptedSource{name: "fast", delay: 0, results: []core.ExternalResult{{Source: "fast", ExternalID: "f1"}}}
	agg := NewAggregator([]SearchSource{slow, fast}, fakeMatcher{}, 20*time.Millisecond)
	got := collect(agg.Stream(context.Background(), "q", core.EntityTrack))

	if got["fast"].Status != StatusOK {
		t.Fatalf("fast should be ok, got %+v", got["fast"])
	}
	if got["slow"].Status != StatusTimeout {
		t.Fatalf("slow should be timeout, got %+v", got["slow"])
	}
}

func TestAggregatorErrorEnvelope(t *testing.T) {
	bad := &scriptedSource{name: "bad", delay: 0, err: errors.New("boom")}
	agg := NewAggregator([]SearchSource{bad}, fakeMatcher{}, time.Second)
	got := collect(agg.Stream(context.Background(), "q", core.EntityTrack))
	if got["bad"].Status != StatusError || got["bad"].Error == "" {
		t.Fatalf("want error envelope, got %+v", got["bad"])
	}
}

func TestAggregatorChannelClosesWithNoSources(t *testing.T) {
	agg := NewAggregator(nil, fakeMatcher{}, time.Second)
	// Range terminates only if the channel is closed.
	for range agg.Stream(context.Background(), "q", core.EntityTrack) {
		t.Fatal("expected no envelopes")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/search/ -run Aggregator -v`
Expected: FAIL — `undefined: NewAggregator`.

- [ ] **Step 3: Write the aggregator**

Create `internal/search/aggregator.go`:
```go
package search

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/maximusjb/crate/internal/core"
)

// Matcher pre-matches an external result against the library. Implemented by
// matching.Service (Task 9). A nil Matcher leaves Match unset.
type Matcher interface {
	Match(ctx context.Context, ext core.ExternalResult) (core.MatchResult, error)
}

// Aggregator fans out a query to every enabled SearchSource concurrently.
type Aggregator struct {
	sources []SearchSource
	matcher Matcher
	timeout time.Duration
}

// NewAggregator builds an aggregator. timeout is the per-source deadline.
func NewAggregator(sources []SearchSource, matcher Matcher, timeout time.Duration) *Aggregator {
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	return &Aggregator{sources: sources, matcher: matcher, timeout: timeout}
}

// Stream runs each source in its own goroutine with an individual
// context.WithTimeout, pre-matches each result, and emits one Envelope per
// source. The channel is closed once every source completes (so an SSE handler
// ranging over it returns). A slow/down source never blocks the others.
func (a *Aggregator) Stream(ctx context.Context, q string, t core.EntityType) <-chan Envelope {
	out := make(chan Envelope)
	var wg sync.WaitGroup
	for _, src := range a.sources {
		wg.Add(1)
		go func(src SearchSource) {
			defer wg.Done()
			out <- a.runOne(ctx, src, q, t)
		}(src)
	}
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}

func (a *Aggregator) runOne(ctx context.Context, src SearchSource, q string, t core.EntityType) Envelope {
	cctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	results, err := src.Search(cctx, q, t)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(cctx.Err(), context.DeadlineExceeded) {
			return Envelope{Source: src.Name(), Status: StatusTimeout, Results: []core.ExternalResult{}}
		}
		return Envelope{Source: src.Name(), Status: StatusError, Results: []core.ExternalResult{}, Error: err.Error()}
	}

	if a.matcher != nil {
		for i := range results {
			m, merr := a.matcher.Match(cctx, results[i])
			if merr == nil {
				mc := m
				results[i].Match = &mc
			} else {
				results[i].Match = &core.MatchResult{Status: core.MatchUnknown, Method: core.MatchNone}
			}
		}
	}
	if results == nil {
		results = []core.ExternalResult{}
	}
	return Envelope{Source: src.Name(), Status: StatusOK, Results: results}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/search/ -v`
Expected: PASS (conformance + all aggregator tests, including timeout isolation).

- [ ] **Step 5: Commit**

```bash
git add internal/search/aggregator.go internal/search/aggregator_test.go
git commit -m "feat(search): fan-out aggregator with per-source deadlines and pre-matching"
```

---

## Task 4: Spotify client — client-credentials OAuth (token fetch + cache)

**Files:**
- Create: `internal/search/spotify/client.go`, `internal/search/spotify/dto.go`
- Test: `internal/search/spotify/client_test.go`
- Fixture: `internal/search/spotify/testdata/token.json`

**Interfaces:**
- Consumes: `net/http`, `encoding/base64`, `net/url`.
- Produces:
  ```go
  type Client struct { ... }
  // NewClient: accountsURL is the OAuth host (token endpoint = accountsURL+"/api/token");
  // apiURL is the Web API host (e.g. https://api.spotify.com/v1). httpClient injectable.
  func NewClient(accountsURL, apiURL, clientID, clientSecret string, httpClient *http.Client) *Client
  // token returns a cached token, refreshing when expired/empty.
  func (c *Client) token(ctx context.Context) (string, error)
  // apiGet performs an authed GET against apiURL+path?query and decodes JSON into out.
  func (c *Client) apiGet(ctx context.Context, path string, q url.Values, out any) error
  ```
  - The token endpoint is POST `accountsURL+"/api/token"` with `Authorization: Basic base64(clientID:clientSecret)`, body `grant_type=client_credentials` (`application/x-www-form-urlencoded`). Response `{access_token, token_type, expires_in}` cached until `now + expires_in - 60s`.

- [ ] **Step 1: Record the token fixture**

Create `internal/search/spotify/testdata/token.json`:
```json
{"access_token":"BQDtESTtoken123","token_type":"Bearer","expires_in":3600}
```

- [ ] **Step 2: Write the failing client test**

Create `internal/search/spotify/client_test.go`:
```go
package spotify

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestTokenBasicAuthAndForm(t *testing.T) {
	var gotAuth, gotGrant, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		_ = r.ParseForm()
		gotGrant = r.Form.Get("grant_type")
		b, _ := os.ReadFile(filepath.Join("testdata", "token.json"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.URL+"/v1", "cid", "csecret", srv.Client())
	tok, err := c.token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tok != "BQDtESTtoken123" {
		t.Fatalf("token = %q", tok)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("cid:csecret"))
	if gotAuth != want {
		t.Fatalf("auth = %q, want %q", gotAuth, want)
	}
	if gotGrant != "client_credentials" {
		t.Fatalf("grant_type = %q", gotGrant)
	}
	if gotCT != "application/x-www-form-urlencoded" {
		t.Fatalf("content-type = %q", gotCT)
	}
}

func TestTokenIsCached(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		b, _ := os.ReadFile(filepath.Join("testdata", "token.json"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.URL+"/v1", "cid", "csecret", srv.Client())
	for i := 0; i < 3; i++ {
		if _, err := c.token(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("token endpoint hit %d times, want 1 (cached)", hits)
	}
}

func TestApiGetSendsBearer(t *testing.T) {
	var gotAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/token", func(w http.ResponseWriter, r *http.Request) {
		b, _ := os.ReadFile(filepath.Join("testdata", "token.json"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	})
	mux.HandleFunc("/v1/ping", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL, srv.URL+"/v1", "cid", "csecret", srv.Client())
	var out map[string]any
	if err := c.apiGet(context.Background(), "/ping", url.Values{}, &out); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer BQDtESTtoken123" {
		t.Fatalf("authorization = %q", gotAuth)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/search/spotify/ -run Token -v`
Expected: FAIL — `undefined: NewClient`.

- [ ] **Step 4: Write the DTOs**

Create `internal/search/spotify/dto.go`:
```go
package spotify

// tokenResponse is the client-credentials OAuth response.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

type imageDTO struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type artistRefDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type albumRefDTO struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	ReleaseDate string     `json:"release_date"`
	Images      []imageDTO `json:"images"`
	Artists     []artistRefDTO `json:"artists"`
}

type trackDTO struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	DurationMs  int            `json:"duration_ms"`
	Artists     []artistRefDTO `json:"artists"`
	Album       albumRefDTO    `json:"album"`
	ExternalIDs struct {
		ISRC string `json:"isrc"`
	} `json:"external_ids"`
}

type fullAlbumDTO struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	ReleaseDate string         `json:"release_date"`
	Images      []imageDTO     `json:"images"`
	Artists     []artistRefDTO `json:"artists"`
	Tracks      struct {
		Items []trackDTO `json:"items"`
	} `json:"tracks"`
}

// searchResponse mirrors GET /v1/search. Only the requested type is populated.
type searchResponse struct {
	Tracks  *struct{ Items []trackDTO } `json:"tracks"`
	Albums  *struct{ Items []albumRefDTO } `json:"albums"`
	Artists *struct {
		Items []struct {
			ID     string     `json:"id"`
			Name   string     `json:"name"`
			Images []imageDTO `json:"images"`
		}
	} `json:"artists"`
}
```

- [ ] **Step 5: Write the client**

Create `internal/search/spotify/client.go`:
```go
package spotify

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Client is a low-level Spotify Web API client using client-credentials OAuth.
// Base URLs and *http.Client are injectable so tests run against httptest.
type Client struct {
	accountsURL  string
	apiURL       string
	clientID     string
	clientSecret string
	http         *http.Client
	now          func() time.Time

	mu        sync.Mutex
	token_    string
	tokenExp  time.Time
}

func NewClient(accountsURL, apiURL, clientID, clientSecret string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		accountsURL:  strings.TrimRight(accountsURL, "/"),
		apiURL:       strings.TrimRight(apiURL, "/"),
		clientID:     clientID,
		clientSecret: clientSecret,
		http:         httpClient,
		now:          time.Now,
	}
}

// token returns a cached access token, refreshing it when empty or near expiry.
func (c *Client) token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token_ != "" && c.now().Before(c.tokenExp) {
		return c.token_, nil
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.accountsURL+"/api/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	basic := base64.StdEncoding.EncodeToString([]byte(c.clientID + ":" + c.clientSecret))
	req.Header.Set("Authorization", "Basic "+basic)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("spotify token: HTTP %d: %s", resp.StatusCode, string(body))
	}
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", fmt.Errorf("spotify token: decode: %w", err)
	}
	if tr.AccessToken == "" {
		return "", fmt.Errorf("spotify token: empty access_token")
	}
	c.token_ = tr.AccessToken
	// Refresh 60s early to avoid using a just-expired token.
	c.tokenExp = c.now().Add(time.Duration(tr.ExpiresIn-60) * time.Second)
	return c.token_, nil
}

// apiGet performs an authed GET against apiURL+path with query q, decoding the
// JSON body into out.
func (c *Client) apiGet(ctx context.Context, path string, q url.Values, out any) error {
	tok, err := c.token(ctx)
	if err != nil {
		return err
	}
	u := c.apiURL + path
	if enc := q.Encode(); enc != "" {
		u += "?" + enc
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("spotify GET %s: HTTP %d: %s", path, resp.StatusCode, string(body))
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("spotify GET %s: decode: %w", path, err)
		}
	}
	return nil
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/search/spotify/ -v`
Expected: PASS (`TestTokenBasicAuthAndForm`, `TestTokenIsCached`, `TestApiGetSendsBearer`).

- [ ] **Step 7: Commit**

```bash
git add internal/search/spotify/client.go internal/search/spotify/dto.go internal/search/spotify/client_test.go internal/search/spotify/testdata/token.json
git commit -m "feat(spotify): client-credentials OAuth client with token caching"
```

---

## Task 5: Spotify adapter — Plugin + SearchSource, DTO→ExternalResult, conformance

**Files:**
- Create: `internal/search/spotify/adapter.go`, `internal/search/spotify/testdata/{search_tracks,search_albums,search_artists,album}.json`
- Test: `internal/search/spotify/adapter_test.go`

**Interfaces:**
- Consumes: `Client` (Task 4), `core`, `registry`, `search.RunConformance`.
- Produces:
  ```go
  type Adapter struct { ... }
  func New() *Adapter
  func (a *Adapter) Type() string                 // "search"
  func (a *Adapter) Name() string                 // "spotify"
  func (a *Adapter) ConfigSchema() registry.ConfigSchema  // client_id, client_secret[secret]
  func (a *Adapter) Init(cfg map[string]any) error
  func (a *Adapter) TestConnection(ctx context.Context) error // fetch a token
  func (a *Adapter) Search(ctx context.Context, q string, t core.EntityType) ([]core.ExternalResult, error)
  func (a *Adapter) GetAlbum(ctx context.Context, externalID string) (core.ExternalAlbum, error)
  func (a *Adapter) WithHTTPClient(h *http.Client) *Adapter            // test seam
  func (a *Adapter) WithBaseURLs(accountsURL, apiURL string) *Adapter  // test seam — call before Init
  ```
  - `var _ search.SearchSource = (*Adapter)(nil)` compile-time assertion.
  - Spotify search: `GET /search?q=<q>&type=track|album|artist&limit=20`. Map ISRC from `external_ids.isrc`; cover from the album/artist `images[0].url`. Year parsed from `release_date` (first 4 chars).

- [ ] **Step 1: Record the fixtures**

Create `internal/search/spotify/testdata/search_tracks.json`:
```json
{"tracks":{"items":[
  {"id":"sp_t1","name":"Opening","duration_ms":210000,
   "artists":[{"id":"sp_ar1","name":"The Artists"}],
   "album":{"id":"sp_al1","name":"First Album","release_date":"2020-05-01","images":[{"url":"https://img/al1.jpg","width":640,"height":640}],"artists":[{"id":"sp_ar1","name":"The Artists"}]},
   "external_ids":{"isrc":"USX1234567"}},
  {"id":"sp_t2","name":"Closing","duration_ms":195000,
   "artists":[{"id":"sp_ar1","name":"The Artists"}],
   "album":{"id":"sp_al1","name":"First Album","release_date":"2020","images":[{"url":"https://img/al1.jpg","width":640,"height":640}],"artists":[{"id":"sp_ar1","name":"The Artists"}]},
   "external_ids":{}}
]}}
```

Create `internal/search/spotify/testdata/search_albums.json`:
```json
{"albums":{"items":[
  {"id":"sp_al1","name":"First Album","release_date":"2020-05-01","images":[{"url":"https://img/al1.jpg","width":640,"height":640}],"artists":[{"id":"sp_ar1","name":"The Artists"}]}
]}}
```

Create `internal/search/spotify/testdata/search_artists.json`:
```json
{"artists":{"items":[
  {"id":"sp_ar1","name":"The Artists","images":[{"url":"https://img/ar1.jpg","width":640,"height":640}]}
]}}
```

Create `internal/search/spotify/testdata/album.json`:
```json
{"id":"sp_al1","name":"First Album","release_date":"2020-05-01",
 "images":[{"url":"https://img/al1.jpg","width":640,"height":640}],
 "artists":[{"id":"sp_ar1","name":"The Artists"}],
 "tracks":{"items":[
   {"id":"sp_t1","name":"Opening","duration_ms":210000,"artists":[{"id":"sp_ar1","name":"The Artists"}],"album":{"id":"sp_al1","name":"First Album","release_date":"2020-05-01"},"external_ids":{"isrc":"USX1234567"}},
   {"id":"sp_t2","name":"Closing","duration_ms":195000,"artists":[{"id":"sp_ar1","name":"The Artists"}],"album":{"id":"sp_al1","name":"First Album","release_date":"2020-05-01"},"external_ids":{}}
 ]}}
```

- [ ] **Step 2: Write the failing adapter test**

Create `internal/search/spotify/adapter_test.go`:
```go
package spotify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/maximusjb/crate/internal/core"
	"github.com/maximusjb/crate/internal/search"
)

// fixtureServer serves token + search/album fixtures based on the path & type.
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	serve := func(w http.ResponseWriter, file string) {
		b, err := os.ReadFile(filepath.Join("testdata", file))
		if err != nil {
			t.Fatalf("read fixture %s: %v", file, err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/token", func(w http.ResponseWriter, r *http.Request) { serve(w, "token.json") })
	mux.HandleFunc("/v1/search", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("type") {
		case "album":
			serve(w, "search_albums.json")
		case "artist":
			serve(w, "search_artists.json")
		default:
			serve(w, "search_tracks.json")
		}
	})
	mux.HandleFunc("/v1/albums/", func(w http.ResponseWriter, r *http.Request) { serve(w, "album.json") })
	return httptest.NewServer(mux)
}

func newTestAdapter(t *testing.T) *Adapter {
	t.Helper()
	srv := fixtureServer(t)
	t.Cleanup(srv.Close)
	a := New().WithHTTPClient(srv.Client()).WithBaseURLs(srv.URL, srv.URL+"/v1")
	if err := a.Init(map[string]any{"client_id": "cid", "client_secret": "csecret"}); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAdapterIdentityAndSchema(t *testing.T) {
	a := New()
	if a.Type() != "search" || a.Name() != "spotify" {
		t.Fatalf("identity: %q/%q", a.Type(), a.Name())
	}
	secret := map[string]bool{}
	for _, f := range a.ConfigSchema().Fields {
		secret[f.Key] = f.Secret
	}
	if _, ok := secret["client_id"]; !ok {
		t.Error("schema missing client_id")
	}
	if s, ok := secret["client_secret"]; !ok || !s {
		t.Error("client_secret missing or not marked secret")
	}
}

func TestSearchTracksMapsISRCAndCover(t *testing.T) {
	a := newTestAdapter(t)
	res, err := a.Search(context.Background(), "x", core.EntityTrack)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("want 2 tracks, got %d", len(res))
	}
	r := res[0]
	if r.Source != "spotify" || r.ExternalID != "sp_t1" || r.Title != "Opening" {
		t.Fatalf("track0: %+v", r)
	}
	if r.Artist != "The Artists" || r.Album != "First Album" || r.DurationMs != 210000 {
		t.Fatalf("track0 fields: %+v", r)
	}
	if r.ISRC != "USX1234567" {
		t.Fatalf("ISRC not mapped: %q", r.ISRC)
	}
	if r.CoverURL != "https://img/al1.jpg" {
		t.Fatalf("cover not mapped: %q", r.CoverURL)
	}
	if r.Type != core.EntityTrack {
		t.Fatalf("type: %q", r.Type)
	}
	if res[1].ISRC != "" {
		t.Fatalf("track1 should have empty ISRC, got %q", res[1].ISRC)
	}
}

func TestSearchAlbumsAndArtists(t *testing.T) {
	a := newTestAdapter(t)
	als, err := a.Search(context.Background(), "x", core.EntityAlbum)
	if err != nil {
		t.Fatal(err)
	}
	if len(als) != 1 || als[0].Title != "First Album" || als[0].Type != core.EntityAlbum {
		t.Fatalf("albums: %+v", als)
	}
	ars, err := a.Search(context.Background(), "x", core.EntityArtist)
	if err != nil {
		t.Fatal(err)
	}
	if len(ars) != 1 || ars[0].Title != "The Artists" || ars[0].Type != core.EntityArtist {
		t.Fatalf("artists: %+v", ars)
	}
}

func TestGetAlbumIncludesTracks(t *testing.T) {
	a := newTestAdapter(t)
	al, err := a.GetAlbum(context.Background(), "sp_al1")
	if err != nil {
		t.Fatal(err)
	}
	if al.Name != "First Album" || al.Year != 2020 || len(al.Tracks) != 2 {
		t.Fatalf("album: %+v", al)
	}
	if al.Tracks[0].ISRC != "USX1234567" {
		t.Fatalf("album track ISRC: %q", al.Tracks[0].ISRC)
	}
}

func TestSpotifyConformance(t *testing.T) {
	a := newTestAdapter(t)
	search.RunConformance(t, a)
}
```

> NOTE: `RunConformance` calls `GetAlbum(ctx,"al1")`. The fixture mux serves `album.json` for any `/v1/albums/...` path, so it succeeds regardless of the ID.

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/search/spotify/ -run AdapterIdentity -v`
Expected: FAIL — `undefined: New` / `Adapter`.

- [ ] **Step 4: Write the adapter**

Create `internal/search/spotify/adapter.go`:
```go
package spotify

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/maximusjb/crate/internal/core"
	"github.com/maximusjb/crate/internal/registry"
	"github.com/maximusjb/crate/internal/search"
)

var (
	_ search.SearchSource = (*Adapter)(nil)
	_ registry.Plugin     = (*Adapter)(nil)
)

const (
	defaultAccountsURL = "https://accounts.spotify.com"
	defaultAPIURL      = "https://api.spotify.com/v1"
)

// Adapter is the Spotify SearchSource. Configure it via Init.
type Adapter struct {
	clientID     string
	clientSecret string
	accountsURL  string
	apiURL       string
	httpClient   *http.Client
	client       *Client
}

// New returns an unconfigured adapter (the registry factory) using production URLs.
func New() *Adapter {
	return &Adapter{accountsURL: defaultAccountsURL, apiURL: defaultAPIURL}
}

// WithHTTPClient injects an *http.Client (test seam). Call before Init.
func (a *Adapter) WithHTTPClient(h *http.Client) *Adapter {
	a.httpClient = h
	return a
}

// WithBaseURLs overrides the OAuth + API hosts (test seam). Call before Init.
func (a *Adapter) WithBaseURLs(accountsURL, apiURL string) *Adapter {
	a.accountsURL = accountsURL
	a.apiURL = apiURL
	return a
}

func (a *Adapter) Type() string { return "search" }
func (a *Adapter) Name() string { return "spotify" }

func (a *Adapter) ConfigSchema() registry.ConfigSchema {
	return registry.ConfigSchema{Fields: []registry.ConfigField{
		{Key: "client_id", Label: "Client ID", Type: "string", Required: true},
		{Key: "client_secret", Label: "Client Secret", Type: "string", Required: true, Secret: true},
	}}
}

func cfgString(cfg map[string]any, key string) string {
	if v, ok := cfg[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func (a *Adapter) Init(cfg map[string]any) error {
	a.clientID = cfgString(cfg, "client_id")
	a.clientSecret = cfgString(cfg, "client_secret")
	if a.clientID == "" || a.clientSecret == "" {
		return fmt.Errorf("spotify: client_id and client_secret are required")
	}
	a.client = NewClient(a.accountsURL, a.apiURL, a.clientID, a.clientSecret, a.httpClient)
	return nil
}

// TestConnection verifies credentials by fetching a token.
func (a *Adapter) TestConnection(ctx context.Context) error {
	if a.client == nil {
		return fmt.Errorf("spotify: not initialized")
	}
	_, err := a.client.token(ctx)
	return err
}

// --- mapping helpers ---

func firstImage(imgs []imageDTO) string {
	if len(imgs) > 0 {
		return imgs[0].URL
	}
	return ""
}

func yearFromReleaseDate(s string) int {
	if len(s) >= 4 {
		if y, err := strconv.Atoi(s[:4]); err == nil {
			return y
		}
	}
	return 0
}

func artistName(arts []artistRefDTO) string {
	if len(arts) > 0 {
		return arts[0].Name
	}
	return ""
}

func (a *Adapter) mapTrack(t trackDTO) core.ExternalResult {
	return core.ExternalResult{
		Source:     "spotify",
		ExternalID: t.ID,
		Title:      t.Name,
		Artist:     artistName(t.Artists),
		Album:      t.Album.Name,
		DurationMs: t.DurationMs,
		ISRC:       t.ExternalIDs.ISRC,
		CoverURL:   firstImage(t.Album.Images),
		Type:       core.EntityTrack,
	}
}

// --- SearchSource methods ---

func (a *Adapter) Search(ctx context.Context, q string, t core.EntityType) ([]core.ExternalResult, error) {
	stype := "track"
	switch t {
	case core.EntityAlbum:
		stype = "album"
	case core.EntityArtist:
		stype = "artist"
	}
	params := url.Values{}
	params.Set("q", q)
	params.Set("type", stype)
	params.Set("limit", "20")

	var resp searchResponse
	if err := a.client.apiGet(ctx, "/search", params, &resp); err != nil {
		return nil, err
	}

	out := []core.ExternalResult{}
	switch t {
	case core.EntityAlbum:
		if resp.Albums != nil {
			for _, al := range resp.Albums.Items {
				out = append(out, core.ExternalResult{
					Source: "spotify", ExternalID: al.ID, Title: al.Name,
					Artist: artistName(al.Artists), CoverURL: firstImage(al.Images),
					Type: core.EntityAlbum,
				})
			}
		}
	case core.EntityArtist:
		if resp.Artists != nil {
			for _, ar := range resp.Artists.Items {
				out = append(out, core.ExternalResult{
					Source: "spotify", ExternalID: ar.ID, Title: ar.Name,
					Artist: ar.Name, CoverURL: firstImage(ar.Images),
					Type: core.EntityArtist,
				})
			}
		}
	default:
		if resp.Tracks != nil {
			for _, tr := range resp.Tracks.Items {
				out = append(out, a.mapTrack(tr))
			}
		}
	}
	return out, nil
}

func (a *Adapter) GetAlbum(ctx context.Context, externalID string) (core.ExternalAlbum, error) {
	var full fullAlbumDTO
	if err := a.client.apiGet(ctx, "/albums/"+url.PathEscape(externalID), url.Values{}, &full); err != nil {
		return core.ExternalAlbum{}, err
	}
	al := core.ExternalAlbum{
		Source:     "spotify",
		ExternalID: full.ID,
		Name:       full.Name,
		Artist:     artistName(full.Artists),
		CoverURL:   firstImage(full.Images),
		Year:       yearFromReleaseDate(full.ReleaseDate),
		Tracks:     []core.ExternalResult{},
	}
	for _, tr := range full.Tracks.Items {
		mt := a.mapTrack(tr)
		// album-tracks endpoint omits album images on each track; backfill name/cover.
		mt.Album = full.Name
		if mt.CoverURL == "" {
			mt.CoverURL = al.CoverURL
		}
		al.Tracks = append(al.Tracks, mt)
	}
	return al, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/search/spotify/ -v`
Expected: PASS (all adapter tests + `TestSpotifyConformance` subtests + Task-4 client tests).

- [ ] **Step 6: Commit**

```bash
git add internal/search/spotify/adapter.go internal/search/spotify/adapter_test.go internal/search/spotify/testdata
git commit -m "feat(spotify): SearchSource adapter mapping to ExternalResult with ISRC, passing conformance"
```

---

## Task 6: Store — `match_cache` migration, sqlc queries, `library_version` accessor

**Files:**
- Create: `internal/store/migrations/0002_match_cache.sql`, `internal/store/queries/match_cache.sql`, `internal/store/queries/library_version.sql`
- Modify: `internal/store/store.go` (add `LibraryVersion(ctx)` accessor)
- Regenerate (committed): `internal/store/db/*` via sqlc
- Test: `internal/store/store_test.go`

**Interfaces:**
- Produces (sqlc-generated, exact names):
  ```go
  type MatchCache struct {
      Source string; ExternalID string; LibraryTrackID sql.NullString
      Method string; Confidence float64; Isrc string; Mbid string
      DurationMs int64; LibraryVersion int64; MatchedAt int64
  }
  func (q *Queries) GetMatchCache(ctx, GetMatchCacheParams) (MatchCache, error)
  func (q *Queries) UpsertMatchCache(ctx, UpsertMatchCacheParams) error
  func (q *Queries) DeleteMatchCacheBySource(ctx, source string) error
  func (q *Queries) ClearMatchCache(ctx) error
  // library_version helpers
  func (q *Queries) GetLibraryVersion(ctx) (string, error)   // raw settings read; "" / ErrNoRows when absent
  func (q *Queries) SetLibraryVersion(ctx, value string) error
  ```
- Produces (hand-written):
  ```go
  func (s *Store) LibraryVersion(ctx context.Context) (int64, error) // returns 1 when absent
  ```

> NOTE on sqlc null mapping: `library_track_id` is nullable (NEGATIVE matches store NULL) → sqlc emits `sql.NullString`. `GetMatchCache`'s WHERE needs both PK columns, so it takes a params struct `GetMatchCacheParams{Source, ExternalID}`. The library_version is stored in `settings` as a string; `library_version.sql` is just a named alias over settings so the matcher reads/writes it without touching `GetSetting` semantics.

- [ ] **Step 1: Write the additive migration (0002, do NOT edit 0001)**

Create `internal/store/migrations/0002_match_cache.sql`:
```sql
-- +goose Up
CREATE TABLE match_cache (
    source           TEXT NOT NULL,
    external_id      TEXT NOT NULL,
    library_track_id TEXT,                 -- NULL = negative match (not in library)
    method           TEXT NOT NULL DEFAULT 'none',
    confidence       REAL NOT NULL DEFAULT 0,
    isrc             TEXT NOT NULL DEFAULT '',
    mbid             TEXT NOT NULL DEFAULT '',
    duration_ms      INTEGER NOT NULL DEFAULT 0,
    library_version  INTEGER NOT NULL DEFAULT 1,
    matched_at       INTEGER NOT NULL DEFAULT (unixepoch()),
    PRIMARY KEY (source, external_id)
);

-- Seed library_version = 1 so a fresh DB has a deterministic baseline.
INSERT INTO settings (key, value) VALUES ('library_version', '1')
    ON CONFLICT(key) DO NOTHING;

-- +goose Down
DROP TABLE match_cache;
DELETE FROM settings WHERE key = 'library_version';
```

- [ ] **Step 2: Write the sqlc queries**

Create `internal/store/queries/match_cache.sql`:
```sql
-- name: GetMatchCache :one
SELECT source, external_id, library_track_id, method, confidence, isrc, mbid, duration_ms, library_version, matched_at
FROM match_cache
WHERE source = ? AND external_id = ?;

-- name: UpsertMatchCache :exec
INSERT INTO match_cache (
    source, external_id, library_track_id, method, confidence, isrc, mbid, duration_ms, library_version, matched_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, unixepoch())
ON CONFLICT(source, external_id) DO UPDATE SET
    library_track_id = excluded.library_track_id,
    method           = excluded.method,
    confidence       = excluded.confidence,
    isrc             = excluded.isrc,
    mbid             = excluded.mbid,
    duration_ms      = excluded.duration_ms,
    library_version  = excluded.library_version,
    matched_at       = excluded.matched_at;

-- name: DeleteMatchCacheBySource :exec
DELETE FROM match_cache WHERE source = ?;

-- name: ClearMatchCache :exec
DELETE FROM match_cache;
```

Create `internal/store/queries/library_version.sql`:
```sql
-- name: GetLibraryVersion :one
SELECT value FROM settings WHERE key = 'library_version';

-- name: SetLibraryVersion :exec
INSERT INTO settings (key, value) VALUES ('library_version', ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value;
```

- [ ] **Step 3: Regenerate sqlc (committed output)**

Run (prefer the installed binary; fall back if absent):
```bash
(command -v sqlc >/dev/null && sqlc generate || go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.27.0 generate)
```
Expected: no output on success; `internal/store/db/match_cache.sql.go` and `library_version.sql.go` appear; `models.go` gains a `MatchCache` struct. Confirm:
```bash
ls internal/store/db/match_cache.sql.go internal/store/db/library_version.sql.go
```
Expected: both paths listed (no "No such file").

- [ ] **Step 4: Write the failing store test**

Create `internal/store/store_test.go`:
```go
package store

import (
	"context"
	"database/sql"
	"testing"

	"github.com/maximusjb/crate/internal/store/db"
)

func openMigrated(t *testing.T) *Store {
	t.Helper()
	st, err := Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestLibraryVersionDefaultsToOne(t *testing.T) {
	st := openMigrated(t)
	v, err := st.LibraryVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v != 1 {
		t.Fatalf("library_version = %d, want 1", v)
	}
}

func TestLibraryVersionSetAndGet(t *testing.T) {
	st := openMigrated(t)
	if err := st.Q().SetLibraryVersion(context.Background(), "5"); err != nil {
		t.Fatal(err)
	}
	v, err := st.LibraryVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v != 5 {
		t.Fatalf("library_version = %d, want 5", v)
	}
}

func TestMatchCacheUpsertPositiveAndNegative(t *testing.T) {
	st := openMigrated(t)
	ctx := context.Background()
	q := st.Q()

	// Positive match.
	if err := q.UpsertMatchCache(ctx, db.UpsertMatchCacheParams{
		Source: "spotify", ExternalID: "sp1",
		LibraryTrackID: sql.NullString{String: "t1", Valid: true},
		Method:         "isrc", Confidence: 1, Isrc: "USX1", Mbid: "", DurationMs: 210000, LibraryVersion: 1,
	}); err != nil {
		t.Fatal(err)
	}
	row, err := q.GetMatchCache(ctx, db.GetMatchCacheParams{Source: "spotify", ExternalID: "sp1"})
	if err != nil {
		t.Fatal(err)
	}
	if !row.LibraryTrackID.Valid || row.LibraryTrackID.String != "t1" || row.Method != "isrc" {
		t.Fatalf("positive row: %+v", row)
	}

	// Negative match (library_track_id NULL).
	if err := q.UpsertMatchCache(ctx, db.UpsertMatchCacheParams{
		Source: "spotify", ExternalID: "sp2",
		LibraryTrackID: sql.NullString{Valid: false},
		Method:         "none", Confidence: 0, LibraryVersion: 1,
	}); err != nil {
		t.Fatal(err)
	}
	neg, err := q.GetMatchCache(ctx, db.GetMatchCacheParams{Source: "spotify", ExternalID: "sp2"})
	if err != nil {
		t.Fatal(err)
	}
	if neg.LibraryTrackID.Valid {
		t.Fatalf("negative row should have NULL library_track_id: %+v", neg)
	}

	// DeleteBySource clears both.
	if err := q.DeleteMatchCacheBySource(ctx, "spotify"); err != nil {
		t.Fatal(err)
	}
	if _, err := q.GetMatchCache(ctx, db.GetMatchCacheParams{Source: "spotify", ExternalID: "sp1"}); err == nil {
		t.Fatal("expected ErrNoRows after delete")
	}
}
```

- [ ] **Step 5: Run test to verify it fails**

Run: `go test ./internal/store/ -run LibraryVersion -v`
Expected: FAIL — `st.LibraryVersion` undefined (the accessor is not written yet).

- [ ] **Step 6: Add the `LibraryVersion` accessor**

Edit `internal/store/store.go`. Add `"context"`, `"database/sql"`, and `"strconv"` to the import block (alongside the existing imports), then append this method:
```go
// LibraryVersion returns the monotonic library_version from settings, returning
// 1 when the key is absent or unparseable. A match_cache row is stale iff its
// library_version is below this value.
func (s *Store) LibraryVersion(ctx context.Context) (int64, error) {
	v, err := s.q.GetLibraryVersion(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return 1, nil
		}
		return 0, err
	}
	n, perr := strconv.ParseInt(v, 10, 64)
	if perr != nil {
		return 1, nil
	}
	return n, nil
}
```

> NOTE: `store.go` already imports `"fmt"`, `"os"`, `"path/filepath"`, `"sync"`, `"embed"`, `"database/sql"`. If `database/sql` is already imported, do not add it again; only add `"context"` and `"strconv"`.

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/store/ -v`
Expected: PASS (LibraryVersion default/set + match_cache upsert positive/negative/delete).

- [ ] **Step 8: Commit**

```bash
git add internal/store/migrations/0002_match_cache.sql internal/store/queries/match_cache.sql internal/store/queries/library_version.sql internal/store/db internal/store/store.go internal/store/store_test.go
git commit -m "feat(store): match_cache migration, queries, and library_version accessor"
```

---

## Task 7: Matching fixture corpus (authored BEFORE the service)

**Files:**
- Create: `internal/matching/testdata/feat-in-title.json`, `remaster-deluxe-suffixes.json`, `live-vs-studio.json`, `same-title-different-artist.json`, `non-latin-scripts.json`, `isrc-present-vs-absent.json`, `short-track-duration-ambiguity.json`, `punctuation-and-whitespace.json`

**Fixture schema (shared, consumed by Tasks 8 & 9):** each file is a JSON object with `cases: []`. Each case has:
```json
{
  "name": "human label",
  "external": {"title":"...","artist":"...","album":"...","durationMs":0,"isrc":"","mbid":""},
  "library":  [ {"id":"t1","title":"...","artist":"...","album":"...","durationMs":0,"isrc":"","mbid":""} ],
  "expect": {"status":"in_library|not_in_library","libraryTrackId":"t1","method":"isrc|mbid|fuzzy|none"},
  "normalize": {"title":"normalized expected title","artist":"normalized expected artist"}
}
```
- `library` is the candidate set the fake `LibrarySearcher` returns for that case (Task 9 wires it).
- `normalize` is OPTIONAL; when present, the Normalize table test (Task 8) asserts `Normalize(external.title) == normalize.title` and `Normalize(external.artist) == normalize.artist`. It documents the EXACT normalized form expected.
- `expect.libraryTrackId` is empty when `status == not_in_library`.

These cases pin behavior; the implementer must make Normalize + Match satisfy them WITHOUT changing the fixtures.

- [ ] **Step 1: Author `feat-in-title.json`**

Create `internal/matching/testdata/feat-in-title.json`:
```json
{"cases":[
  {"name":"feat. stripped symmetrically vs plain library title",
   "external":{"title":"Sunrise (feat. Aluna)","artist":"DJ Sol","album":"Mornings","durationMs":200000},
   "library":[{"id":"t1","title":"Sunrise","artist":"DJ Sol","album":"Mornings","durationMs":200500}],
   "expect":{"status":"in_library","libraryTrackId":"t1","method":"fuzzy"},
   "normalize":{"title":"sunrise","artist":"dj sol"}},
  {"name":"featuring spelled out, both sides carry it",
   "external":{"title":"Skyline featuring Mara","artist":"Nova","album":"City","durationMs":180000},
   "library":[{"id":"t2","title":"Skyline (featuring Mara)","artist":"Nova","album":"City","durationMs":181000}],
   "expect":{"status":"in_library","libraryTrackId":"t2","method":"fuzzy"},
   "normalize":{"title":"skyline","artist":"nova"}},
  {"name":"ft. variant",
   "external":{"title":"Echoes ft. K","artist":"Vale","album":"Deep","durationMs":240000},
   "library":[{"id":"t3","title":"Echoes","artist":"Vale","album":"Deep","durationMs":240000}],
   "expect":{"status":"in_library","libraryTrackId":"t3","method":"fuzzy"},
   "normalize":{"title":"echoes","artist":"vale"}}
]}
```

- [ ] **Step 2: Author `remaster-deluxe-suffixes.json`** (qualifiers are PRESERVED through Normalize — they must NOT be over-stripped. A remaster matches a library copy that carries the SAME qualifier; the preserved qualifier text is exactly what prevents a remaster/deluxe from collapsing onto a differently-labeled version. Within the same qualifier, duration tolerance still applies.)

Create `internal/matching/testdata/remaster-deluxe-suffixes.json`:
```json
{"cases":[
  {"name":"remaster matches same-labeled library copy within duration tolerance",
   "external":{"title":"Wanderer (Remaster 2011)","artist":"Gold Coast","album":"Wanderer","durationMs":215000},
   "library":[{"id":"t1","title":"Wanderer (Remaster 2011)","artist":"Gold Coast","album":"Wanderer","durationMs":216000}],
   "expect":{"status":"in_library","libraryTrackId":"t1","method":"fuzzy"},
   "normalize":{"title":"wanderer (remaster 2011)","artist":"gold coast"}},
  {"name":"deluxe edition qualifier preserved (not stripped)",
   "external":{"title":"Horizon (Deluxe Edition)","artist":"Pale Sun","album":"Horizon","durationMs":190000},
   "library":[{"id":"t2","title":"Horizon (Deluxe Edition)","artist":"Pale Sun","album":"Horizon","durationMs":190200}],
   "expect":{"status":"in_library","libraryTrackId":"t2","method":"fuzzy"},
   "normalize":{"title":"horizon (deluxe edition)","artist":"pale sun"}},
  {"name":"2009 remastered version qualifier preserved",
   "external":{"title":"Tideways (2009 Remastered Version)","artist":"Estuary","album":"Tideways","durationMs":253000},
   "library":[{"id":"t3","title":"Tideways (2009 Remastered Version)","artist":"Estuary","album":"Tideways","durationMs":253000}],
   "expect":{"status":"in_library","libraryTrackId":"t3","method":"fuzzy"},
   "normalize":{"title":"tideways (2009 remastered version)","artist":"estuary"}}
]}
```

- [ ] **Step 3: Author `live-vs-studio.json`** (same title/artist; the live cut must NOT match the studio cut because duration differs beyond tolerance)

Create `internal/matching/testdata/live-vs-studio.json`:
```json
{"cases":[
  {"name":"live external must not match studio library by duration",
   "external":{"title":"Falling (Live)","artist":"Cinder","album":"Live at the Hall","durationMs":312000},
   "library":[{"id":"studio","title":"Falling","artist":"Cinder","album":"Embers","durationMs":244000}],
   "expect":{"status":"not_in_library","libraryTrackId":"","method":"none"},
   "normalize":{"title":"falling (live)","artist":"cinder"}},
  {"name":"studio external matches studio library; live candidate rejected",
   "external":{"title":"Falling","artist":"Cinder","album":"Embers","durationMs":244000},
   "library":[
     {"id":"live","title":"Falling (Live)","artist":"Cinder","album":"Live at the Hall","durationMs":312000},
     {"id":"studio","title":"Falling","artist":"Cinder","album":"Embers","durationMs":243500}
   ],
   "expect":{"status":"in_library","libraryTrackId":"studio","method":"fuzzy"},
   "normalize":{"title":"falling","artist":"cinder"}}
]}
```

- [ ] **Step 4: Author `same-title-different-artist.json`** (Creep/Radiohead vs Creep/TLC — must NOT match)

Create `internal/matching/testdata/same-title-different-artist.json`:
```json
{"cases":[
  {"name":"Creep by Radiohead must not match Creep by TLC",
   "external":{"title":"Creep","artist":"Radiohead","album":"Pablo Honey","durationMs":238000},
   "library":[{"id":"tlc","title":"Creep","artist":"TLC","album":"CrazySexyCool","durationMs":265000}],
   "expect":{"status":"not_in_library","libraryTrackId":"","method":"none"},
   "normalize":{"title":"creep","artist":"radiohead"}},
  {"name":"correct artist among multiple same-title candidates",
   "external":{"title":"Creep","artist":"Radiohead","album":"Pablo Honey","durationMs":238000},
   "library":[
     {"id":"tlc","title":"Creep","artist":"TLC","album":"CrazySexyCool","durationMs":265000},
     {"id":"rh","title":"Creep","artist":"Radiohead","album":"Pablo Honey","durationMs":239000}
   ],
   "expect":{"status":"in_library","libraryTrackId":"rh","method":"fuzzy"},
   "normalize":{"title":"creep","artist":"radiohead"}}
]}
```

- [ ] **Step 5: Author `non-latin-scripts.json`** (Cyrillic/CJK/diacritics; unicode normalization; diacritics folded so an accented external matches an unaccented library title)

Create `internal/matching/testdata/non-latin-scripts.json`:
```json
{"cases":[
  {"name":"diacritics folded: Bjork accented matches unaccented",
   "external":{"title":"Jóga","artist":"Björk","album":"Homogenic","durationMs":295000},
   "library":[{"id":"j1","title":"Joga","artist":"Bjork","album":"Homogenic","durationMs":295000}],
   "expect":{"status":"in_library","libraryTrackId":"j1","method":"fuzzy"},
   "normalize":{"title":"joga","artist":"bjork"}},
  {"name":"cyrillic exact match preserved",
   "external":{"title":"Кукушка","artist":"Кино","album":"Звезда","durationMs":366000},
   "library":[{"id":"k1","title":"Кукушка","artist":"Кино","album":"Звезда","durationMs":366000}],
   "expect":{"status":"in_library","libraryTrackId":"k1","method":"fuzzy"},
   "normalize":{"title":"кукушка","artist":"кино"}},
  {"name":"CJK exact match preserved",
   "external":{"title":"夜曲","artist":"周杰倫","album":"十一月的蕭邦","durationMs":227000},
   "library":[{"id":"c1","title":"夜曲","artist":"周杰倫","album":"十一月的蕭邦","durationMs":227000}],
   "expect":{"status":"in_library","libraryTrackId":"c1","method":"fuzzy"},
   "normalize":{"title":"夜曲","artist":"周杰倫"}}
]}
```

- [ ] **Step 6: Author `isrc-present-vs-absent.json`** (ISRC exact path vs fall-through to fuzzy)

Create `internal/matching/testdata/isrc-present-vs-absent.json`:
```json
{"cases":[
  {"name":"ISRC exact wins even when title differs",
   "external":{"title":"Different Title On Spotify","artist":"Loom","album":"X","durationMs":201000,"isrc":"GBARL0700001"},
   "library":[{"id":"i1","title":"Canonical Title","artist":"Loom","album":"X","durationMs":201000,"isrc":"GBARL0700001"}],
   "expect":{"status":"in_library","libraryTrackId":"i1","method":"isrc"}},
  {"name":"ISRC mismatch falls through to fuzzy by title+artist+duration",
   "external":{"title":"Glasshouse","artist":"Loom","album":"X","durationMs":201000,"isrc":"GBARL0700002"},
   "library":[{"id":"i2","title":"Glasshouse","artist":"Loom","album":"X","durationMs":201500,"isrc":"GBARL0799999"}],
   "expect":{"status":"in_library","libraryTrackId":"i2","method":"fuzzy"}},
  {"name":"ISRC absent on external, fuzzy used",
   "external":{"title":"Glasshouse","artist":"Loom","album":"X","durationMs":201000},
   "library":[{"id":"i3","title":"Glasshouse","artist":"Loom","album":"X","durationMs":201000,"isrc":"GBARL0700003"}],
   "expect":{"status":"in_library","libraryTrackId":"i3","method":"fuzzy"}}
]}
```

- [ ] **Step 7: Author `short-track-duration-ambiguity.json`** (short tracks where title is ambiguous and duration is decisive)

Create `internal/matching/testdata/short-track-duration-ambiguity.json`:
```json
{"cases":[
  {"name":"two short interludes same title; duration picks the right one",
   "external":{"title":"Interlude","artist":"Mosaic","album":"Phase I","durationMs":42000},
   "library":[
     {"id":"int2","title":"Interlude","artist":"Mosaic","album":"Phase II","durationMs":75000},
     {"id":"int1","title":"Interlude","artist":"Mosaic","album":"Phase I","durationMs":43000}
   ],
   "expect":{"status":"in_library","libraryTrackId":"int1","method":"fuzzy"}},
  {"name":"short track with no duration match is rejected",
   "external":{"title":"Intro","artist":"Mosaic","album":"Phase I","durationMs":30000},
   "library":[{"id":"intro2","title":"Intro","artist":"Mosaic","album":"Phase II","durationMs":61000}],
   "expect":{"status":"not_in_library","libraryTrackId":"","method":"none"}}
]}
```

- [ ] **Step 8: Author `punctuation-and-whitespace.json`** (`Pt. 1` vs `Part 1`, `&` vs `and`, stray punctuation/whitespace)

Create `internal/matching/testdata/punctuation-and-whitespace.json`:
```json
{"cases":[
  {"name":"ampersand normalizes to and",
   "external":{"title":"Salt & Sea","artist":"Harbor","album":"Coast","durationMs":205000},
   "library":[{"id":"p1","title":"Salt and Sea","artist":"Harbor","album":"Coast","durationMs":205000}],
   "expect":{"status":"in_library","libraryTrackId":"p1","method":"fuzzy"},
   "normalize":{"title":"salt and sea","artist":"harbor"}},
  {"name":"Pt. abbreviation normalizes to part",
   "external":{"title":"Movement Pt. 1","artist":"Quartet","album":"Suite","durationMs":300000},
   "library":[{"id":"p2","title":"Movement Part 1","artist":"Quartet","album":"Suite","durationMs":300500}],
   "expect":{"status":"in_library","libraryTrackId":"p2","method":"fuzzy"},
   "normalize":{"title":"movement part 1","artist":"quartet"}},
  {"name":"stray punctuation and double spaces collapsed",
   "external":{"title":"  Hello,  World!! ","artist":"The Coders","album":"Init","durationMs":123000},
   "library":[{"id":"p3","title":"Hello World","artist":"The Coders","album":"Init","durationMs":123000}],
   "expect":{"status":"in_library","libraryTrackId":"p3","method":"fuzzy"},
   "normalize":{"title":"hello world","artist":"the coders"}}
]}
```

- [ ] **Step 9: Sanity-check the JSON is valid**

Run:
```bash
for f in internal/matching/testdata/*.json; do python3 -c "import json,sys;json.load(open('$f'))" && echo "ok $f"; done
```
Expected: one `ok <path>` line per file, no traceback.

- [ ] **Step 10: Commit**

```bash
git add internal/matching/testdata
git commit -m "test(matching): enumerated fixture corpus for Normalize and Match"
```

---

## Task 8: `Normalize()` — pure, shared, symmetric, no over-strip

**Files:**
- Create: `internal/matching/normalize.go`
- Test: `internal/matching/normalize_test.go`

**Interfaces:**
- Consumes: `unicode`, `strings` (no new module dependency — diacritic folding done with a small explicit fold map covering the fixture corpus + Latin-1 supplement).
- Produces:
  ```go
  func Normalize(s string) string // pure; deterministic; safe for "" input
  ```

**Normalization contract (must satisfy the `normalize` fields in Task 7 fixtures):**
1. Unicode: fold common Latin diacritics to ASCII (`ó→o`, `ö→o`, `ä→a`, `ö→o`, `é→e`, `ü→u`, `ç→c`, `ñ→n`, etc.); leave Cyrillic/CJK code points intact.
2. Lowercase (Unicode-aware via `strings.ToLower`).
3. Strip feat groups SYMMETRICALLY: remove a trailing/standalone `feat.`/`featuring`/`ft.`/`ft` token and everything after it WITHIN the same segment, including a wrapping parenthetical (e.g. `(feat. Aluna)` and ` featuring Mara` and ` ft. K`). Do NOT remove other parentheticals.
4. Replace `&` with `and`.
5. Expand `pt.`/`pt` token (word-boundary) to `part`.
6. Remove stray punctuation (anything that is not a letter, digit, or space) EXCEPT keep `(` `)` so preserved qualifiers like `(remaster 2011)` survive — but the feat parenthetical is removed in step 3 before this step.
7. Collapse runs of whitespace to a single space; trim.
- Order matters: fold → lowercase → strip-feat → `&`→and → pt→part → strip-punct(keep parens) → collapse. The result for `"Wanderer (Remaster 2011)"` is `"wanderer (remaster 2011)"` (qualifier preserved); for `"Sunrise (feat. Aluna)"` it is `"sunrise"` (feat group removed).

- [ ] **Step 1: Write the failing test (fixtures + explicit edge cases)**

Create `internal/matching/normalize_test.go`:
```go
package matching

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// fixtureFile is the shared corpus schema (also used by matching_test.go).
type fixtureFile struct {
	Cases []fixtureCase `json:"cases"`
}
type fixtureTrack struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Album      string `json:"album"`
	DurationMs int    `json:"durationMs"`
	ISRC       string `json:"isrc"`
	MBID       string `json:"mbid"`
}
type fixtureExpect struct {
	Status         string `json:"status"`
	LibraryTrackID string `json:"libraryTrackId"`
	Method         string `json:"method"`
}
type fixtureNormalize struct {
	Title  string `json:"title"`
	Artist string `json:"artist"`
}
type fixtureCase struct {
	Name      string            `json:"name"`
	External  fixtureTrack      `json:"external"`
	Library   []fixtureTrack    `json:"library"`
	Expect    fixtureExpect     `json:"expect"`
	Normalize *fixtureNormalize `json:"normalize"`
}

func loadFixtures(t *testing.T) []fixtureCase {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("testdata", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no fixtures found")
	}
	var all []fixtureCase
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		var ff fixtureFile
		if err := json.Unmarshal(b, &ff); err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		all = append(all, ff.Cases...)
	}
	return all
}

func TestNormalizeAgainstFixtures(t *testing.T) {
	for _, c := range loadFixtures(t) {
		if c.Normalize == nil {
			continue
		}
		if got := Normalize(c.External.Title); got != c.Normalize.Title {
			t.Errorf("%s: Normalize(title)=%q want %q", c.Name, got, c.Normalize.Title)
		}
		if got := Normalize(c.External.Artist); got != c.Normalize.Artist {
			t.Errorf("%s: Normalize(artist)=%q want %q", c.Name, got, c.Normalize.Artist)
		}
	}
}

func TestNormalizeEdgeCases(t *testing.T) {
	cases := map[string]string{
		"":                          "",
		"   ":                       "",
		"Hello,  World!!":           "hello world",
		"Salt & Sea":                "salt and sea",
		"Movement Pt. 1":            "movement part 1",
		"Sunrise (feat. Aluna)":     "sunrise",
		"Echoes ft. K":              "echoes",
		"Skyline featuring Mara":    "skyline",
		"Wanderer (Remaster 2011)":  "wanderer (remaster 2011)",
		"Björk":                     "bjork",
		"Jóga":                      "joga",
		// Regression: feat/ft must not strip mid-word (word-boundary guard).
		"Daft Punk":                 "daft punk",
		"Drift":                     "drift",
		"Gift":                      "gift",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q)=%q want %q", in, got, want)
		}
	}
	// Cyrillic preserved (lowercased).
	if got := Normalize("Кукушка"); got != "кукушка" {
		t.Errorf("cyrillic: %q", got)
	}
	// Symmetry: feat group removed identically wherever it appears.
	if Normalize("Sunrise (feat. Aluna)") != Normalize("Sunrise") {
		t.Error("feat stripping not symmetric")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/matching/ -run Normalize -v`
Expected: FAIL — `undefined: Normalize`.

- [ ] **Step 3: Write the implementation**

Create `internal/matching/normalize.go`:
```go
// Package matching implements Crate's external⇄library matcher and the shared,
// pure Normalize() used by both matching and (future) dedup_key. Normalization is
// SYMMETRIC: callers apply it to both sides before comparison.
package matching

import (
	"regexp"
	"strings"
)

// diacriticFold maps common Latin diacritics to ASCII. Cyrillic/CJK are untouched.
var diacriticFold = map[rune]rune{
	'á': 'a', 'à': 'a', 'â': 'a', 'ä': 'a', 'ã': 'a', 'å': 'a', 'ā': 'a',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e', 'ē': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i', 'ī': 'i',
	'ó': 'o', 'ò': 'o', 'ô': 'o', 'ö': 'o', 'õ': 'o', 'ø': 'o', 'ō': 'o',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u', 'ū': 'u',
	'ç': 'c', 'ñ': 'n', 'ý': 'y', 'ÿ': 'y', 'ß': 's',
}

func foldDiacritics(s string) string {
	var b strings.Builder
	for _, r := range s {
		if f, ok := diacriticFold[r]; ok {
			b.WriteRune(f)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// featRe removes a feat./featuring/ft. group: an optional opening paren/bracket,
// the keyword, and everything to the end of the string. Applied after lowercasing.
var featRe = regexp.MustCompile(`(?i)\s*[\(\[]?\s*\b(feat\.?|featuring|ft\.?)\b.*$`)

// ptRe expands a "pt"/"pt." token at a word boundary into "part".
var ptRe = regexp.MustCompile(`\bpt\.?\b`)

// keepRe matches characters to KEEP: letters/digits (any script), space, parens.
// Everything else is dropped. \p{L} and \p{N} cover Cyrillic/CJK code points.
var dropRe = regexp.MustCompile(`[^\p{L}\p{N}\s()]+`)

var wsRe = regexp.MustCompile(`\s+`)

// Normalize lowercases, folds Latin diacritics, strips feat groups symmetrically,
// expands &→and and pt→part, removes stray punctuation (keeping parentheses so
// version qualifiers like "(remaster 2011)" survive), and collapses whitespace.
// It is pure and deterministic. It does NOT strip version qualifiers.
func Normalize(s string) string {
	s = foldDiacritics(s)
	s = strings.ToLower(s)
	s = featRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "&", " and ")
	s = ptRe.ReplaceAllString(s, "part")
	s = dropRe.ReplaceAllString(s, " ")
	s = wsRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}
```

> Correctness note: `&` is replaced with `" and "` (spaces) so `Salt & Sea` → `salt and sea` after whitespace collapse, never `saltandsea`. `dropRe` keeps `()` so `wanderer (remaster 2011)` survives; the feat parenthetical is already removed by `featRe` (which consumes the opening paren and the rest of the string). `ptRe` runs before punctuation removal so `pt.` → `part` (the `.` would otherwise be dropped first, still leaving `pt` to expand — either order works, but this order is fixed for determinism).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/matching/ -run Normalize -v`
Expected: PASS (`TestNormalizeAgainstFixtures`, `TestNormalizeEdgeCases`).

- [ ] **Step 5: Commit**

```bash
git add internal/matching/normalize.go internal/matching/normalize_test.go
git commit -m "feat(matching): pure symmetric Normalize that preserves version qualifiers"
```

---

## Task 9: MatchingService — priority chain, cache-first, library_version invalidation

**Files:**
- Create: `internal/matching/matching.go`
- Test: `internal/matching/matching_test.go`

**Interfaces:**
- Consumes: `core`, `internal/store/db` (cache params), `database/sql`, the fixture corpus, `library.LibraryAdapter` (via the `LibrarySearcher` interface so matching does not import `library` — avoids a cycle and keeps it testable).
- Produces:
  ```go
  // LibrarySearcher is the slice of LibraryAdapter that matching needs.
  // *library.Adapter / library.LibraryAdapter satisfy this (Search signature matches).
  type LibrarySearcher interface {
      Search(ctx context.Context, q string, types []core.EntityType) (core.SearchResults, error)
  }
  // MatchCacheStore is the slice of *db.Queries that matching needs.
  type MatchCacheStore interface {
      GetMatchCache(ctx context.Context, arg db.GetMatchCacheParams) (db.MatchCache, error)
      UpsertMatchCache(ctx context.Context, arg db.UpsertMatchCacheParams) error
  }
  // VersionProvider returns the current monotonic library_version (store.LibraryVersion).
  type VersionProvider func(ctx context.Context) (int64, error)

  type Service struct { ... }
  func NewService(lib LibrarySearcher, cache MatchCacheStore, version VersionProvider) *Service
  func (s *Service) Match(ctx context.Context, ext core.ExternalResult) (core.MatchResult, error)
  ```
- Constant: `const DurationToleranceMs = 3000`.
- `Service.Match` implements `search.Matcher` (signature identical), so it is passed straight into the aggregator.

**Algorithm (deterministic):**
1. **Cache-first:** read `GetMatchCache(source, externalID)`. If found AND `row.library_version >= currentVersion`, return the cached decision (positive: `library_track_id` valid → in_library; negative: NULL → not_in_library).
2. Otherwise fetch candidates: `lib.Search(ctx, ext.Title, []EntityType{EntityTrack})` → candidate tracks. (Title-only query; the source decides recall. If empty title, fall back to `ext.Artist`.)
3. **Priority chain over candidates:**
   - **ISRC:** if `ext.ISRC != ""` and a candidate has the same ISRC → in_library, method=isrc, confidence=1.0.
   - **MBID:** if `ext.MBID != ""` and a candidate has the same MBID → in_library, method=mbid, confidence=1.0.
   - **Fuzzy:** among candidates where `Normalize(cand.Title)==Normalize(ext.Title)` AND `Normalize(cand.Artist)==Normalize(ext.Artist)`, pick the one whose `|durationMs - ext.durationMs|` is smallest; accept iff that delta `<= DurationToleranceMs`. Album normalized-equality is a tiebreaker when two candidates tie on duration delta. Confidence = `0.9` when album also matches, else `0.7`.
   - Else **not_in_library**, method=none, confidence=0.
4. **Write-through cache:** upsert the decision (positive or negative) with `currentVersion`, duration, isrc, mbid.

- [ ] **Step 1: Write the failing matching test (fixtures + cache behavior)**

Create `internal/matching/matching_test.go`:
```go
package matching

import (
	"context"
	"database/sql"
	"testing"

	"github.com/maximusjb/crate/internal/core"
	"github.com/maximusjb/crate/internal/store/db"
)

// fakeLib returns a fixed candidate set for any query (the case's library tracks).
type fakeLib struct{ tracks []core.Track }

func (f fakeLib) Search(ctx context.Context, q string, types []core.EntityType) (core.SearchResults, error) {
	return core.SearchResults{Tracks: f.tracks}, nil
}

// memCache is an in-memory MatchCacheStore.
type memCache struct{ rows map[string]db.MatchCache }

func newMemCache() *memCache { return &memCache{rows: map[string]db.MatchCache{}} }
func key(s, e string) string { return s + "\x1f" + e }
func (m *memCache) GetMatchCache(ctx context.Context, arg db.GetMatchCacheParams) (db.MatchCache, error) {
	r, ok := m.rows[key(arg.Source, arg.ExternalID)]
	if !ok {
		return db.MatchCache{}, sql.ErrNoRows
	}
	return r, nil
}
func (m *memCache) UpsertMatchCache(ctx context.Context, arg db.UpsertMatchCacheParams) error {
	m.rows[key(arg.Source, arg.ExternalID)] = db.MatchCache{
		Source: arg.Source, ExternalID: arg.ExternalID, LibraryTrackID: arg.LibraryTrackID,
		Method: arg.Method, Confidence: arg.Confidence, Isrc: arg.Isrc, Mbid: arg.Mbid,
		DurationMs: arg.DurationMs, LibraryVersion: arg.LibraryVersion,
	}
	return nil
}

func fixtureToTrack(ft fixtureTrack) core.Track {
	return core.Track{ID: ft.ID, Title: ft.Title, Artist: ft.Artist, Album: ft.Album, DurationMs: ft.DurationMs, ISRC: ft.ISRC}
}
func fixtureToExternal(ft fixtureTrack) core.ExternalResult {
	return core.ExternalResult{
		Source: "spotify", ExternalID: "ext-" + ft.Title, Title: ft.Title, Artist: ft.Artist,
		Album: ft.Album, DurationMs: ft.DurationMs, ISRC: ft.ISRC, MBID: ft.MBID, Type: core.EntityTrack,
	}
}

func TestMatchAgainstFixtures(t *testing.T) {
	for _, c := range loadFixtures(t) {
		t.Run(c.Name, func(t *testing.T) {
			var cands []core.Track
			for _, lt := range c.Library {
				cands = append(cands, fixtureToTrack(lt))
			}
			svc := NewService(fakeLib{tracks: cands}, newMemCache(), func(context.Context) (int64, error) { return 1, nil })
			ext := fixtureToExternal(c.External)

			got, err := svc.Match(context.Background(), ext)
			if err != nil {
				t.Fatal(err)
			}
			if string(got.Status) != c.Expect.Status {
				t.Fatalf("status=%q want %q (result %+v)", got.Status, c.Expect.Status, got)
			}
			if got.LibraryTrackID != c.Expect.LibraryTrackID {
				t.Fatalf("libraryTrackId=%q want %q", got.LibraryTrackID, c.Expect.LibraryTrackID)
			}
			if c.Expect.Method != "" && string(got.Method) != c.Expect.Method {
				t.Fatalf("method=%q want %q", got.Method, c.Expect.Method)
			}
		})
	}
}

func TestMatchCacheFirstAndInvalidation(t *testing.T) {
	cands := []core.Track{{ID: "t1", Title: "Song", Artist: "A", Album: "X", DurationMs: 200000, ISRC: "USX1"}}
	cache := newMemCache()
	version := int64(1)
	svc := NewService(fakeLib{tracks: cands}, cache, func(context.Context) (int64, error) { return version, nil })
	ext := core.ExternalResult{Source: "spotify", ExternalID: "sp1", Title: "Song", Artist: "A", DurationMs: 200000, ISRC: "USX1", Type: core.EntityTrack}

	first, _ := svc.Match(context.Background(), ext)
	if first.Status != core.MatchInLibrary || first.Method != core.MatchISRC {
		t.Fatalf("first match: %+v", first)
	}
	// Now the library no longer has the track, but the cached positive (version 1) should still be served.
	svc.lib = fakeLib{tracks: nil}
	cached, _ := svc.Match(context.Background(), ext)
	if cached.Status != core.MatchInLibrary {
		t.Fatalf("expected cached positive, got %+v", cached)
	}
	// Bump library_version → cache stale → re-match against the (now empty) library → negative.
	version = 2
	fresh, _ := svc.Match(context.Background(), ext)
	if fresh.Status != core.MatchNotInLibrary {
		t.Fatalf("expected re-match negative after version bump, got %+v", fresh)
	}
}

func TestMatchNegativeIsCached(t *testing.T) {
	cache := newMemCache()
	svc := NewService(fakeLib{tracks: nil}, cache, func(context.Context) (int64, error) { return 1, nil })
	ext := core.ExternalResult{Source: "spotify", ExternalID: "sp9", Title: "Nope", Artist: "Z", DurationMs: 100000, Type: core.EntityTrack}
	if _, err := svc.Match(context.Background(), ext); err != nil {
		t.Fatal(err)
	}
	row, err := cache.GetMatchCache(context.Background(), db.GetMatchCacheParams{Source: "spotify", ExternalID: "sp9"})
	if err != nil {
		t.Fatalf("negative match not cached: %v", err)
	}
	if row.LibraryTrackID.Valid {
		t.Fatalf("cached negative should have NULL library_track_id: %+v", row)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/matching/ -run Match -v`
Expected: FAIL — `undefined: NewService`.

- [ ] **Step 3: Write the implementation**

Create `internal/matching/matching.go`:
```go
package matching

import (
	"context"
	"database/sql"

	"github.com/maximusjb/crate/internal/core"
	"github.com/maximusjb/crate/internal/store/db"
)

// DurationToleranceMs is the max |external-library| duration delta accepted by
// the fuzzy rung. A live cut is rarely within 3s of the studio cut, so duration
// is the disambiguator that prevents cross-version false positives.
const DurationToleranceMs = 3000

// LibrarySearcher is the slice of LibraryAdapter that matching needs.
type LibrarySearcher interface {
	Search(ctx context.Context, q string, types []core.EntityType) (core.SearchResults, error)
}

// MatchCacheStore is the slice of *db.Queries that matching needs.
type MatchCacheStore interface {
	GetMatchCache(ctx context.Context, arg db.GetMatchCacheParams) (db.MatchCache, error)
	UpsertMatchCache(ctx context.Context, arg db.UpsertMatchCacheParams) error
}

// VersionProvider returns the current monotonic library_version.
type VersionProvider func(ctx context.Context) (int64, error)

// Service is the MatchingService. It is deterministic and cache-first.
type Service struct {
	lib     LibrarySearcher
	cache   MatchCacheStore
	version VersionProvider
}

func NewService(lib LibrarySearcher, cache MatchCacheStore, version VersionProvider) *Service {
	return &Service{lib: lib, cache: cache, version: version}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// Match resolves an external result to a library track via the priority chain
// ISRC → MBID → normalized-fuzzy+duration. Reads/writes match_cache; respects
// library_version for invalidation. Positive AND negative decisions are cached.
func (s *Service) Match(ctx context.Context, ext core.ExternalResult) (core.MatchResult, error) {
	curVer, err := s.version(ctx)
	if err != nil {
		return core.MatchResult{}, err
	}

	// 1. Cache-first (fresh rows only).
	if s.cache != nil {
		if row, cerr := s.cache.GetMatchCache(ctx, db.GetMatchCacheParams{Source: ext.Source, ExternalID: ext.ExternalID}); cerr == nil {
			if row.LibraryVersion >= curVer {
				return cachedToResult(row), nil
			}
		} else if cerr != sql.ErrNoRows {
			return core.MatchResult{}, cerr
		}
	}

	// 2. Candidate fetch.
	query := ext.Title
	if query == "" {
		query = ext.Artist
	}
	res, err := s.lib.Search(ctx, query, []core.EntityType{core.EntityTrack})
	if err != nil {
		return core.MatchResult{}, err
	}
	cands := res.Tracks

	// 3. Priority chain.
	result := s.resolve(ext, cands)

	// 4. Write-through cache.
	if s.cache != nil {
		ltid := sql.NullString{}
		if result.Status == core.MatchInLibrary {
			ltid = sql.NullString{String: result.LibraryTrackID, Valid: true}
		}
		if uerr := s.cache.UpsertMatchCache(ctx, db.UpsertMatchCacheParams{
			Source: ext.Source, ExternalID: ext.ExternalID, LibraryTrackID: ltid,
			Method: string(result.Method), Confidence: result.Confidence,
			Isrc: ext.ISRC, Mbid: ext.MBID, DurationMs: int64(ext.DurationMs), LibraryVersion: curVer,
		}); uerr != nil {
			return core.MatchResult{}, uerr
		}
	}
	return result, nil
}

func cachedToResult(row db.MatchCache) core.MatchResult {
	if row.LibraryTrackID.Valid {
		return core.MatchResult{Status: core.MatchInLibrary, LibraryTrackID: row.LibraryTrackID.String, Method: core.MatchMethod(row.Method), Confidence: row.Confidence}
	}
	return core.MatchResult{Status: core.MatchNotInLibrary, Method: core.MatchMethod(row.Method), Confidence: row.Confidence}
}

func (s *Service) resolve(ext core.ExternalResult, cands []core.Track) core.MatchResult {
	// ISRC exact.
	if ext.ISRC != "" {
		for _, c := range cands {
			if c.ISRC != "" && c.ISRC == ext.ISRC {
				return core.MatchResult{Status: core.MatchInLibrary, LibraryTrackID: c.ID, Method: core.MatchISRC, Confidence: 1.0}
			}
		}
	}
	// MBID exact (core.Track has no MBID field today; external MBID matches only
	// when a future library track carries it — guarded so it is a no-op for now).
	// Left intentionally as a structural rung; no library MBID source in M2.

	// Fuzzy: normalized title+artist equal, pick smallest duration delta within tolerance.
	nTitle := Normalize(ext.Title)
	nArtist := Normalize(ext.Artist)
	nAlbum := Normalize(ext.Album)
	best := -1
	bestDelta := DurationToleranceMs + 1
	bestAlbumMatch := false
	for i, c := range cands {
		if Normalize(c.Title) != nTitle || Normalize(c.Artist) != nArtist {
			continue
		}
		delta := abs(c.DurationMs - ext.DurationMs)
		if delta > DurationToleranceMs {
			continue
		}
		albumMatch := nAlbum != "" && Normalize(c.Album) == nAlbum
		better := delta < bestDelta || (delta == bestDelta && albumMatch && !bestAlbumMatch)
		if best == -1 || better {
			best = i
			bestDelta = delta
			bestAlbumMatch = albumMatch
		}
	}
	if best >= 0 {
		conf := 0.7
		if bestAlbumMatch {
			conf = 0.9
		}
		return core.MatchResult{Status: core.MatchInLibrary, LibraryTrackID: cands[best].ID, Method: core.MatchFuzzy, Confidence: conf}
	}
	return core.MatchResult{Status: core.MatchNotInLibrary, Method: core.MatchNone, Confidence: 0}
}
```

> NOTE on the MBID rung: `core.Track` (M1) has no MBID field, so there is no library MBID to compare against in M2. The chain keeps ISRC → (MBID placeholder) → fuzzy ordering structurally; the `isrc-present-vs-absent` fixtures exercise ISRC and the fuzzy fall-through, which is the M2-observable behavior. Adding a library MBID source is a P2 follow-up that drops into this rung without restructuring.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/matching/ -v`
Expected: PASS (`TestNormalize*`, `TestMatchAgainstFixtures` for every fixture case, `TestMatchCacheFirstAndInvalidation`, `TestMatchNegativeIsCached`).

- [ ] **Step 5: Commit**

```bash
git add internal/matching/matching.go internal/matching/matching_test.go
git commit -m "feat(matching): cache-first priority-chain matcher with duration disambiguation"
```

---

## Task 10: API SSE endpoint — `GET /api/v1/search/everywhere`

**Files:**
- Create: `internal/api/search.go`
- Modify: `internal/api/server.go` (add `SearchAggregator` to `Deps`, mount the route)
- Test: `internal/api/search_test.go`

**Interfaces:**
- Consumes: `search.Aggregator` via a minimal interface so tests inject a fake:
  ```go
  // Streamer is the slice of *search.Aggregator that the SSE handler needs.
  // *search.Aggregator satisfies it.
  type Streamer interface {
      Stream(ctx context.Context, q string, t core.EntityType) <-chan search.Envelope
  }
  ```
- Produces (extends `Deps`):
  ```go
  type Deps struct {
      Auth             *auth.Service
      Library          library.LibraryAdapter
      SearchAggregator Streamer        // NEW (may be nil)
      Search           *registry.Registry
      Downloader       *registry.Registry
      Dev              bool
  }
  ```
- Endpoint (behind `requireAuth`): `GET /api/v1/search/everywhere?q=&type=track|album|artist` → `text/event-stream`; one `data: <Envelope JSON>\n\n` per source; flush per event; stop on `r.Context().Done()`.

- [ ] **Step 1: Write the failing SSE test**

Create `internal/api/search_test.go`:
```go
package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/maximusjb/crate/internal/auth"
	"github.com/maximusjb/crate/internal/core"
	"github.com/maximusjb/crate/internal/registry"
	"github.com/maximusjb/crate/internal/search"
	"github.com/maximusjb/crate/internal/store"
)

// fakeAgg emits a fixed set of envelopes then closes the channel.
type fakeAgg struct{ envs []search.Envelope }

func (f fakeAgg) Stream(ctx context.Context, q string, t core.EntityType) <-chan search.Envelope {
	ch := make(chan search.Envelope)
	go func() {
		defer close(ch)
		for _, e := range f.envs {
			select {
			case ch <- e:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}

func searchTestServer(t *testing.T, agg Streamer) (*Server, *http.Cookie) {
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
	tok, err := authSvc.CreateSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Deps{
		Auth:             authSvc,
		SearchAggregator: agg,
		Search:           registry.NewRegistry("search"),
		Downloader:       registry.NewRegistry("downloader"),
	})
	return srv, &http.Cookie{Name: sessionCookie, Value: tok}
}

func TestEverywhereSSEStreamsEnvelopes(t *testing.T) {
	envs := []search.Envelope{
		{Source: "spotify", Status: search.StatusOK, Results: []core.ExternalResult{
			{Source: "spotify", ExternalID: "sp1", Title: "Song", Type: core.EntityTrack,
				Match: &core.MatchResult{Status: core.MatchInLibrary, LibraryTrackID: "t1", Method: core.MatchISRC, Confidence: 1}},
		}},
		{Source: "deezer", Status: search.StatusTimeout, Results: []core.ExternalResult{}},
	}
	srv, cookie := searchTestServer(t, fakeAgg{envs: envs})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search/everywhere?q=song&type=track", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %q", ct)
	}

	// Parse "data: <json>" lines.
	var parsed []search.Envelope
	sc := bufio.NewScanner(strings.NewReader(rec.Body.String()))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "data: ") {
			var e search.Envelope
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &e); err != nil {
				t.Fatalf("bad event json: %v (%q)", err, line)
			}
			parsed = append(parsed, e)
		}
	}
	if len(parsed) != 2 {
		t.Fatalf("want 2 events, got %d: %s", len(parsed), rec.Body.String())
	}
	if parsed[0].Source != "spotify" || parsed[0].Results[0].Match == nil || parsed[0].Results[0].Match.Status != core.MatchInLibrary {
		t.Fatalf("event0 wrong: %+v", parsed[0])
	}
	if parsed[1].Status != search.StatusTimeout {
		t.Fatalf("event1 should be timeout: %+v", parsed[1])
	}
}

func TestEverywhereRequiresAuth(t *testing.T) {
	srv, _ := searchTestServer(t, fakeAgg{})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/search/everywhere?q=x", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestEverywhereNilAggregatorReturns503(t *testing.T) {
	srv, cookie := searchTestServer(t, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search/everywhere?q=x", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
```

> NOTE: `httptest.ResponseRecorder` implements `http.Flusher` (its `Flush()` sets `rec.Flushed`), so the handler's flush calls are safe in tests.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run Everywhere -v`
Expected: FAIL — `Deps` has no field `SearchAggregator` / undefined `handleEverywhere`.

- [ ] **Step 3: Extend `Deps` + mount the route**

Edit `internal/api/server.go`. Add `"github.com/maximusjb/crate/internal/search"` and `"github.com/maximusjb/crate/internal/core"` and `"context"` to the import block, add the `Streamer` interface + `SearchAggregator` field, and mount the route. Replace the `Deps` struct with:
```go
type Deps struct {
	Auth             *auth.Service
	Library          library.LibraryAdapter
	SearchAggregator Streamer
	Search           *registry.Registry
	Downloader       *registry.Registry
	Dev              bool
}

// Streamer is the slice of *search.Aggregator the SSE handler needs.
type Streamer interface {
	Stream(ctx context.Context, q string, t core.EntityType) <-chan search.Envelope
}
```
Inside the protected group (after `pr.Get("/cover/{id}", s.handleCover)`), add:
```go
			pr.Get("/search/everywhere", s.handleEverywhere)
```

- [ ] **Step 4: Write the SSE handler**

Create `internal/api/search.go`:
```go
package api

import (
	"encoding/json"
	"net/http"

	"github.com/maximusjb/crate/internal/core"
)

// handleEverywhere streams per-source search envelopes as Server-Sent Events.
// Each event is `data: <Envelope JSON>\n\n`, flushed immediately. Each result in
// an envelope is already pre-matched by the MatchingService (via the aggregator).
// The handler returns when the aggregator closes its channel or the client
// disconnects (r.Context().Done()).
func (s *Server) handleEverywhere(w http.ResponseWriter, r *http.Request) {
	if s.deps.SearchAggregator == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no search sources configured"})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}

	q := r.URL.Query().Get("q")
	var et core.EntityType
	switch r.URL.Query().Get("type") {
	case "album":
		et = core.EntityAlbum
	case "artist":
		et = core.EntityArtist
	default:
		et = core.EntityTrack
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch := s.deps.SearchAggregator.Stream(r.Context(), q, et)
	for {
		select {
		case <-r.Context().Done():
			return
		case env, open := <-ch:
			if !open {
				return
			}
			b, err := json.Marshal(env)
			if err != nil {
				continue
			}
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(b)
			_, _ = w.Write([]byte("\n\n"))
			flusher.Flush()
		}
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/api/ -v`
Expected: PASS (existing M0/M1 tests + Everywhere SSE/auth/503 tests). The M1 `auth_flow_test.go` `testServer` literal does not set `SearchAggregator`, which is fine — the zero value is nil and no auth test hits the SSE route.

- [ ] **Step 6: Commit**

```bash
git add internal/api/search.go internal/api/server.go internal/api/search_test.go
git commit -m "feat(api): Server-Sent Events endpoint for Everywhere search"
```

---

## Task 11: Composition root — search sources, matching service, aggregator

**Files:**
- Create: `cmd/crate/search_wiring.go`, `cmd/crate/search_wiring_test.go`
- Modify: `cmd/crate/main.go`

**Interfaces:**
- Consumes: `registry.Registry`, `spotify.New`, `db.AdapterInstance`, env (`CRATE_SPOTIFY_CLIENT_SECRET`), `search.SearchSource`, `matching.Service`, `search.NewAggregator`, `store.LibraryVersion`.
- Produces:
  ```go
  // buildSearchSources builds every ENABLED adapter_instance of type "search",
  // applying CRATE_SPOTIFY_CLIENT_SECRET onto spotify config before Init. Sources
  // are ordered by Priority (ascending, matching the SQL ORDER BY).
  func buildSearchSources(reg *registry.Registry, instances []db.AdapterInstance, getenv func(string) string) ([]search.SearchSource, error)
  ```

- [ ] **Step 1: Write the failing wiring test**

Create `cmd/crate/search_wiring_test.go`:
```go
package main

import (
	"context"
	"testing"

	"github.com/maximusjb/crate/internal/core"
	"github.com/maximusjb/crate/internal/registry"
	"github.com/maximusjb/crate/internal/search"
	"github.com/maximusjb/crate/internal/store/db"
)

type stubSource struct {
	got map[string]any
}

func (s *stubSource) Type() string                             { return "search" }
func (s *stubSource) Name() string                             { return "spotify" }
func (s *stubSource) ConfigSchema() registry.ConfigSchema      { return registry.ConfigSchema{} }
func (s *stubSource) Init(cfg map[string]any) error            { s.got = cfg; return nil }
func (s *stubSource) TestConnection(ctx context.Context) error { return nil }
func (s *stubSource) Search(ctx context.Context, q string, t core.EntityType) ([]core.ExternalResult, error) {
	return nil, nil
}
func (s *stubSource) GetAlbum(ctx context.Context, id string) (core.ExternalAlbum, error) {
	return core.ExternalAlbum{}, nil
}

func TestBuildSearchSourcesAppliesEnvSecret(t *testing.T) {
	reg := registry.NewRegistry("search")
	captured := &stubSource{}
	reg.Register("spotify", func() registry.Plugin { return captured })

	instances := []db.AdapterInstance{{
		ID: "s1", Type: "search", Name: "spotify", Enabled: 1, Priority: 0,
		ConfigJson: `{"client_id":"cid","client_secret":"file-secret"}`,
	}}
	env := map[string]string{"CRATE_SPOTIFY_CLIENT_SECRET": "env-secret"}

	got, err := buildSearchSources(reg, instances, func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 source, got %d", len(got))
	}
	if captured.got["client_secret"] != "env-secret" {
		t.Fatalf("env override not applied: %v", captured.got["client_secret"])
	}
	if captured.got["client_id"] != "cid" {
		t.Fatalf("client_id not parsed: %v", captured.got["client_id"])
	}
}

func TestBuildSearchSourcesSkipsDisabledAndNonSearch(t *testing.T) {
	reg := registry.NewRegistry("search")
	reg.Register("spotify", func() registry.Plugin { return &stubSource{} })
	instances := []db.AdapterInstance{
		{ID: "s1", Type: "search", Name: "spotify", Enabled: 0},
		{ID: "l1", Type: "library", Name: "subsonic", Enabled: 1},
	}
	got, err := buildSearchSources(reg, instances, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("want 0 sources, got %d", len(got))
	}
}

// Compile-time guard that the produced type matches the aggregator's input.
var _ = func() {
	var _ []search.SearchSource
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/crate/ -run BuildSearch -v`
Expected: FAIL — `undefined: buildSearchSources`.

- [ ] **Step 3: Write the wiring helper**

Create `cmd/crate/search_wiring.go`:
```go
package main

import (
	"encoding/json"
	"fmt"

	"github.com/maximusjb/crate/internal/registry"
	"github.com/maximusjb/crate/internal/search"
	"github.com/maximusjb/crate/internal/store/db"
)

// buildSearchSources instantiates every ENABLED adapter_instance of type
// "search" from the registry, applying CRATE_SPOTIFY_CLIENT_SECRET onto the
// spotify config_json just before Init (env wins; never sent to the browser).
// instances are already ordered by (type, priority) from ListAdapterInstances.
func buildSearchSources(reg *registry.Registry, instances []db.AdapterInstance, getenv func(string) string) ([]search.SearchSource, error) {
	out := []search.SearchSource{}
	for i := range instances {
		inst := instances[i]
		if inst.Type != "search" || inst.Enabled != 1 {
			continue
		}
		plugin, err := reg.Create(inst.Name)
		if err != nil {
			return nil, fmt.Errorf("search source %q: %w", inst.Name, err)
		}
		src, ok := plugin.(search.SearchSource)
		if !ok {
			return nil, fmt.Errorf("adapter %q is not a SearchSource", inst.Name)
		}

		cfg := map[string]any{}
		if inst.ConfigJson != "" {
			if err := json.Unmarshal([]byte(inst.ConfigJson), &cfg); err != nil {
				return nil, fmt.Errorf("search source %q config: %w", inst.Name, err)
			}
		}
		// Env secret override (Spotify) — env wins for client_secret before Init.
		if inst.Name == "spotify" {
			if sec := getenv("CRATE_SPOTIFY_CLIENT_SECRET"); sec != "" {
				cfg["client_secret"] = sec
			}
		}

		if err := src.Init(cfg); err != nil {
			return nil, fmt.Errorf("search source %q init: %w", inst.Name, err)
		}
		out = append(out, src)
	}
	return out, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/crate/ -run BuildSearch -v`
Expected: PASS.

- [ ] **Step 5: Wire into main**

Edit `cmd/crate/main.go`. Add imports `"time"` is already present; add:
```go
	"github.com/maximusjb/crate/internal/matching"
	"github.com/maximusjb/crate/internal/search"
	"github.com/maximusjb/crate/internal/search/spotify"
```
Register the spotify factory next to the subsonic one — change the search registry block to:
```go
	searchReg := registry.NewRegistry("search")
	searchReg.Register("spotify", func() registry.Plugin { return spotify.New() })
```
After `libAdapter` is built (and before `srv := api.NewServer(...)`), add:
```go
	// Build active search sources + the matching service + the aggregator.
	sources, err := buildSearchSources(searchReg, instances, os.Getenv)
	if err != nil {
		log.Printf("WARNING: search sources not available: %v", err)
		sources = nil
	}
	var aggregator *search.Aggregator
	if len(sources) > 0 {
		var matcher search.Matcher
		if libAdapter != nil {
			matcher = matching.NewService(libAdapter, st.Q(), st.LibraryVersion)
		}
		aggregator = search.NewAggregator(sources, matcher, 8*time.Second)
		log.Printf("search sources active: %d", len(sources))
	} else {
		log.Printf("no search sources configured (add one via settings)")
	}
```
Then add `SearchAggregator: aggregator,` to the `api.Deps{...}` literal:
```go
	srv := api.NewServer(api.Deps{
		Auth:             authSvc,
		Library:          libAdapter,
		SearchAggregator: aggregator,
		Search:           searchReg,
		Downloader:       downloaderReg,
		Dev:              cfg.Dev,
	})
```

> NOTE: passing a typed-nil `*search.Aggregator` into the `Streamer` interface field would make the interface non-nil (interface holding a nil pointer). To keep `Deps.SearchAggregator == nil` true when there are no sources, the handler's nil check must also treat a nil aggregator pointer as absent. Avoid the footgun: when `aggregator` is nil, assign the interface explicitly. Replace the literal field with a guarded assignment AFTER constructing the server is messy; instead build deps in two steps:
> ```go
> 	deps := api.Deps{
> 		Auth:       authSvc,
> 		Library:    libAdapter,
> 		Search:     searchReg,
> 		Downloader: downloaderReg,
> 		Dev:        cfg.Dev,
> 	}
> 	if aggregator != nil {
> 		deps.SearchAggregator = aggregator
> 	}
> 	srv := api.NewServer(deps)
> ```
> This guarantees `deps.SearchAggregator` stays a true nil interface when no sources exist, so the SSE handler's `== nil` check returns 503 correctly.

- [ ] **Step 6: Build + full backend test**

Run: `go build ./cmd/... ./internal/... && go test ./cmd/... ./internal/...`
Expected: build OK; all PASS.

- [ ] **Step 7: Commit**

```bash
go mod tidy
git add cmd/crate go.mod go.sum
git commit -m "feat(cmd): wire spotify source, matching service, and SSE aggregator"
```

---

## Task 12: Frontend types + `SearchStream` (EventSource SSE client)

**Files:**
- Modify: `web/src/lib/types.ts` (add external/match/envelope types)
- Create: `web/src/lib/searchStream.ts`
- Test: `web/src/lib/searchStream.test.ts`

**Interfaces:**
- Produces (TS, mirroring `core` JSON exactly):
  ```ts
  export type MatchStatus = 'in_library' | 'not_in_library' | 'unknown'
  export type MatchMethod = 'isrc' | 'mbid' | 'fuzzy' | 'none'
  export interface MatchResult { status: MatchStatus; libraryTrackId: string; method: MatchMethod; confidence: number }
  export interface ExternalResult {
    source: string; externalId: string; title: string; artist: string; album: string
    durationMs: number; isrc?: string; mbid?: string; coverUrl?: string; coverArtId?: string
    type: 'track' | 'album' | 'artist'; match?: MatchResult
  }
  export type EnvelopeStatus = 'ok' | 'timeout' | 'error'
  export interface SearchEnvelope { source: string; status: EnvelopeStatus; results: ExternalResult[]; cursor?: string; error?: string }
  ```
  And `searchStream.ts`:
  ```ts
  export interface SearchStreamHandlers { onEnvelope(e: SearchEnvelope): void; onError?(): void }
  export class SearchStream {
    constructor(q: string, type: 'track'|'album'|'artist', handlers: SearchStreamHandlers, makeSource?: (url: string) => EventSourceLike)
    close(): void
  }
  ```
  - `EventSourceLike` is a tiny interface (`onmessage`, `onerror`, `close()`) so tests inject a stub and no real network is opened. The default `makeSource` builds a real `EventSource` (same-origin, cookie carried automatically).

- [ ] **Step 1: Add the external types**

Append to `web/src/lib/types.ts`:
```ts
export type MatchStatus = 'in_library' | 'not_in_library' | 'unknown'
export type MatchMethod = 'isrc' | 'mbid' | 'fuzzy' | 'none'

export interface MatchResult {
  status: MatchStatus
  libraryTrackId: string
  method: MatchMethod
  confidence: number
}

export interface ExternalResult {
  source: string
  externalId: string
  title: string
  artist: string
  album: string
  durationMs: number
  isrc?: string
  mbid?: string
  coverUrl?: string
  coverArtId?: string
  type: 'track' | 'album' | 'artist'
  match?: MatchResult
}

export type EnvelopeStatus = 'ok' | 'timeout' | 'error'

export interface SearchEnvelope {
  source: string
  status: EnvelopeStatus
  results: ExternalResult[]
  cursor?: string
  error?: string
}
```

- [ ] **Step 2: Write the failing SearchStream test (stubbed EventSource)**

Create `web/src/lib/searchStream.test.ts`:
```ts
import { describe, expect, it, vi } from 'vitest'
import { SearchStream, type EventSourceLike } from './searchStream'
import type { SearchEnvelope } from './types'

// stubSource lets the test fire messages/errors synchronously; records the URL + close.
class StubSource implements EventSourceLike {
  onmessage: ((ev: { data: string }) => void) | null = null
  onerror: (() => void) | null = null
  closed = false
  constructor(public url: string) {}
  close() {
    this.closed = true
  }
  emit(env: SearchEnvelope) {
    this.onmessage?.({ data: JSON.stringify(env) })
  }
}

describe('SearchStream', () => {
  it('opens a same-origin URL with q and type, and forwards envelopes', () => {
    let made: StubSource | null = null
    const got: SearchEnvelope[] = []
    const ss = new SearchStream('hello world', 'track', { onEnvelope: (e) => got.push(e) }, (url) => {
      made = new StubSource(url)
      return made
    })
    expect(made).not.toBeNull()
    expect(made!.url).toBe('/api/v1/search/everywhere?q=hello%20world&type=track')

    made!.emit({ source: 'spotify', status: 'ok', results: [] })
    expect(got).toHaveLength(1)
    expect(got[0].source).toBe('spotify')

    ss.close()
    expect(made!.closed).toBe(true)
  })

  it('calls onError on stream error', () => {
    const onError = vi.fn()
    let made: StubSource | null = null
    new SearchStream('q', 'track', { onEnvelope: () => {}, onError }, (url) => {
      made = new StubSource(url)
      return made
    })
    made!.onerror?.()
    expect(onError).toHaveBeenCalled()
  })
})
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd web && npx vitest run src/lib/searchStream.test.ts`
Expected: FAIL — cannot resolve `./searchStream`.

- [ ] **Step 4: Write the SearchStream**

Create `web/src/lib/searchStream.ts`:
```ts
import type { SearchEnvelope } from './types'

// EventSourceLike is the minimal slice of EventSource we use, so tests inject a
// stub and no real network connection is opened.
export interface EventSourceLike {
  onmessage: ((ev: { data: string }) => void) | null
  onerror: (() => void) | null
  close(): void
}

export interface SearchStreamHandlers {
  onEnvelope(e: SearchEnvelope): void
  onError?(): void
}

// SearchStream is the SSE transport for Everywhere search. It is DISTINCT from
// the REST fetch wrapper and the (future) WebSocket: EventSource hits the
// same-origin endpoint and carries the session cookie automatically.
export class SearchStream {
  private source: EventSourceLike

  constructor(
    q: string,
    type: 'track' | 'album' | 'artist',
    handlers: SearchStreamHandlers,
    makeSource: (url: string) => EventSourceLike = (url) => new EventSource(url, { withCredentials: true }) as unknown as EventSourceLike,
  ) {
    const url = `/api/v1/search/everywhere?q=${encodeURIComponent(q)}&type=${type}`
    this.source = makeSource(url)
    this.source.onmessage = (ev) => {
      try {
        handlers.onEnvelope(JSON.parse(ev.data) as SearchEnvelope)
      } catch {
        // ignore malformed event
      }
    }
    this.source.onerror = () => {
      handlers.onError?.()
    }
  }

  close() {
    this.source.close()
  }
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd web && npx vitest run src/lib/searchStream.test.ts`
Expected: PASS.

- [ ] **Step 6: Typecheck + commit**

Run: `cd web && npm run build`
```bash
git add web/src/lib/types.ts web/src/lib/searchStream.ts web/src/lib/searchStream.test.ts
git commit -m "feat(web): EventSource-based SearchStream and external result types"
```

---

## Task 13: Everywhere reducer — append-in-stable-sections + cross-source dedup

**Files:**
- Create: `web/src/lib/everywhereStore.ts`
- Test: `web/src/lib/everywhereStore.test.ts`

**Interfaces:**
- Produces (a PURE reducer + a tiny React hook):
  ```ts
  export interface SourceStatus { source: string; status: EnvelopeStatus }
  export interface EverywhereState {
    tracks: ExternalResult[]; albums: ExternalResult[]; artists: ExternalResult[]
    sources: SourceStatus[]
  }
  export const emptyEverywhere: EverywhereState
  export function dedupKey(r: ExternalResult): string  // isrc || normalized(artist+title)
  export function applyEnvelope(state: EverywhereState, env: SearchEnvelope): EverywhereState
  export function useEverywhere(q: string, type: 'track'|'album'|'artist', enabled: boolean): EverywhereState
  ```
- **Append-in-stable-sections contract:** `applyEnvelope` APPENDS new results to the end of their section; it NEVER reorders or removes already-present rows. Cross-source dedup: a result whose `dedupKey` already exists in the section is dropped (the first source to deliver it wins its position). `sources` is updated/added by source name (status replaced).

- [ ] **Step 1: Write the failing reducer test**

Create `web/src/lib/everywhereStore.test.ts`:
```ts
import { describe, expect, it } from 'vitest'
import { applyEnvelope, dedupKey, emptyEverywhere } from './everywhereStore'
import type { ExternalResult, SearchEnvelope } from './types'

function track(p: Partial<ExternalResult>): ExternalResult {
  return {
    source: 's', externalId: 'e', title: 'T', artist: 'A', album: 'Al',
    durationMs: 1000, type: 'track', ...p,
  }
}
function env(p: Partial<SearchEnvelope>): SearchEnvelope {
  return { source: 's', status: 'ok', results: [], ...p }
}

describe('dedupKey', () => {
  it('prefers ISRC when present', () => {
    expect(dedupKey(track({ isrc: 'USX1' }))).toBe('isrc:usx1')
  })
  it('falls back to normalized artist+title', () => {
    expect(dedupKey(track({ artist: 'The Band', title: 'Song (feat. X)' }))).toBe(dedupKey(track({ artist: 'The Band', title: 'Song' })))
  })
})

describe('applyEnvelope', () => {
  it('appends tracks and records source status', () => {
    const s1 = applyEnvelope(emptyEverywhere, env({ source: 'spotify', results: [track({ externalId: 'a' })] }))
    expect(s1.tracks).toHaveLength(1)
    expect(s1.sources).toEqual([{ source: 'spotify', status: 'ok' }])

    const s2 = applyEnvelope(s1, env({ source: 'deezer', results: [track({ externalId: 'b', isrc: 'ZZ9', artist: 'Other', title: 'Diff' })] }))
    expect(s2.tracks.map((t) => t.externalId)).toEqual(['a', 'b'])
    expect(s2.sources).toHaveLength(2)
  })

  it('never reorders shown rows and dedupes across sources by key', () => {
    const a = applyEnvelope(emptyEverywhere, env({ source: 'spotify', results: [
      track({ externalId: 'x', isrc: 'SAME' }),
      track({ externalId: 'y', isrc: 'OTHER' }),
    ]}))
    const b = applyEnvelope(a, env({ source: 'deezer', results: [
      track({ externalId: 'dup', isrc: 'SAME' }), // duplicate of x → dropped
      track({ externalId: 'z', isrc: 'NEW' }),
    ]}))
    expect(b.tracks.map((t) => t.externalId)).toEqual(['x', 'y', 'z'])
  })

  it('routes albums and artists into their own sections', () => {
    const s = applyEnvelope(emptyEverywhere, env({ results: [
      track({ externalId: 't', type: 'track' }),
      track({ externalId: 'al', type: 'album' }),
      track({ externalId: 'ar', type: 'artist' }),
    ]}))
    expect(s.tracks).toHaveLength(1)
    expect(s.albums).toHaveLength(1)
    expect(s.artists).toHaveLength(1)
  })

  it('updates an existing source status in place (timeout)', () => {
    const a = applyEnvelope(emptyEverywhere, env({ source: 'spotify', status: 'ok', results: [track({})] }))
    const b = applyEnvelope(a, env({ source: 'spotify', status: 'timeout', results: [] }))
    expect(b.sources).toEqual([{ source: 'spotify', status: 'timeout' }])
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/lib/everywhereStore.test.ts`
Expected: FAIL — cannot resolve `./everywhereStore`.

- [ ] **Step 3: Write the reducer + hook**

Create `web/src/lib/everywhereStore.ts`:
```ts
import { useEffect, useReducer } from 'react'
import type { EnvelopeStatus, ExternalResult, SearchEnvelope } from './types'
import { SearchStream } from './searchStream'

export interface SourceStatus {
  source: string
  status: EnvelopeStatus
}

export interface EverywhereState {
  tracks: ExternalResult[]
  albums: ExternalResult[]
  artists: ExternalResult[]
  sources: SourceStatus[]
}

export const emptyEverywhere: EverywhereState = { tracks: [], albums: [], artists: [], sources: [] }

// normalize mirrors the backend matching.Normalize closely enough for client-side
// dedup: lowercase, strip feat groups, &→and, drop non-alphanumerics, collapse ws.
function normalize(s: string): string {
  return s
    .toLowerCase()
    .replace(/\s*[([]?\s*\b(feat\.?|featuring|ft\.?)\b.*$/i, '')
    .replace(/&/g, ' and ')
    .replace(/[^\p{L}\p{N}\s]+/gu, ' ')
    .replace(/\s+/g, ' ')
    .trim()
}

export function dedupKey(r: ExternalResult): string {
  if (r.isrc) return `isrc:${r.isrc.toLowerCase()}`
  return `nf:${normalize(r.artist)}␟${normalize(r.title)}`
}

function appendSection(existing: ExternalResult[], incoming: ExternalResult[]): ExternalResult[] {
  const seen = new Set(existing.map(dedupKey))
  const out = existing.slice() // preserve order; never reflow
  for (const r of incoming) {
    const k = dedupKey(r)
    if (seen.has(k)) continue
    seen.add(k)
    out.push(r)
  }
  return out
}

export function applyEnvelope(state: EverywhereState, env: SearchEnvelope): EverywhereState {
  const incTracks = env.results.filter((r) => r.type === 'track')
  const incAlbums = env.results.filter((r) => r.type === 'album')
  const incArtists = env.results.filter((r) => r.type === 'artist')

  const sources = state.sources.some((s) => s.source === env.source)
    ? state.sources.map((s) => (s.source === env.source ? { source: env.source, status: env.status } : s))
    : [...state.sources, { source: env.source, status: env.status }]

  return {
    tracks: appendSection(state.tracks, incTracks),
    albums: appendSection(state.albums, incAlbums),
    artists: appendSection(state.artists, incArtists),
    sources,
  }
}

type Action = { type: 'reset' } | { type: 'envelope'; env: SearchEnvelope }

function reducer(state: EverywhereState, action: Action): EverywhereState {
  switch (action.type) {
    case 'reset':
      return emptyEverywhere
    case 'envelope':
      return applyEnvelope(state, action.env)
  }
}

// useEverywhere opens a SearchStream for (q,type) when enabled, accumulating
// per-source envelopes via the pure reducer. The stream is closed on unmount /
// when q/type/enabled change (no leaked connections).
export function useEverywhere(q: string, type: 'track' | 'album' | 'artist', enabled: boolean): EverywhereState {
  const [state, dispatch] = useReducer(reducer, emptyEverywhere)

  useEffect(() => {
    dispatch({ type: 'reset' })
    if (!enabled || q.trim() === '') return
    const stream = new SearchStream(q, type, { onEnvelope: (env) => dispatch({ type: 'envelope', env }) })
    return () => stream.close()
  }, [q, type, enabled])

  return state
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run src/lib/everywhereStore.test.ts`
Expected: PASS (append, dedup, never-reorder, section routing, status update).

- [ ] **Step 5: Typecheck + commit**

Run: `cd web && npm run build`
```bash
git add web/src/lib/everywhereStore.ts web/src/lib/everywhereStore.test.ts
git commit -m "feat(web): everywhere reducer with stable-section append and cross-source dedup"
```

---

## Task 14: ExternalRow + SourceChips components

**Files:**
- Create: `web/src/components/ExternalRow.tsx`, `web/src/components/SourceChips.tsx`
- Test: `web/src/components/ExternalRow.test.tsx`

**Interfaces:**
- `ExternalRow` props: `{ result: ExternalResult }`. When `result.match?.status === 'in_library'`, render a ✓ and make the row clickable to PLAY the matched library track (`usePlayer().playTrackList([trackFromMatch], 0)` — a minimal `Track` synthesized from the external metadata + `match.libraryTrackId` as the id, so the existing stream proxy plays it). Otherwise render a PLAIN, non-interactive row (no ↓/⟳ — downloaders are M3). A clearly-marked M3 download seam comment sits where the download affordance will go.
- `SourceChips` props: `{ sources: SourceStatus[] }`. Renders "Spotify ✓ · Deezer … · timed out" — `ok`→✓, `timeout`→"timed out", `error`→"error". Source names are Title-cased.

- [ ] **Step 1: Write the failing ExternalRow test**

Create `web/src/components/ExternalRow.test.tsx`:
```tsx
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { ExternalRow } from './ExternalRow'
import type { ExternalResult } from '../lib/types'

const play = vi.fn()
vi.mock('../lib/playerStore', () => ({
  usePlayer: (sel: (s: { playTrackList: typeof play }) => unknown) => sel({ playTrackList: play }),
}))

function res(p: Partial<ExternalResult>): ExternalResult {
  return {
    source: 'spotify', externalId: 'sp1', title: 'Song', artist: 'Artist', album: 'Album',
    durationMs: 200000, type: 'track', ...p,
  }
}

describe('ExternalRow', () => {
  beforeEach(() => play.mockClear())

  it('shows in-library check and plays the matched track on click', () => {
    render(<ExternalRow result={res({ match: { status: 'in_library', libraryTrackId: 't1', method: 'isrc', confidence: 1 } })} />)
    expect(screen.getByTitle(/in library/i)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button'))
    expect(play).toHaveBeenCalledTimes(1)
    const arg = play.mock.calls[0][0] as Array<{ id: string }>
    expect(arg[0].id).toBe('t1')
  })

  it('renders a plain non-button row when not in library', () => {
    render(<ExternalRow result={res({ match: { status: 'not_in_library', libraryTrackId: '', method: 'none', confidence: 0 } })} />)
    expect(screen.queryByRole('button')).toBeNull()
    expect(screen.getByText('Song')).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/components/ExternalRow.test.tsx`
Expected: FAIL — cannot resolve `./ExternalRow`.

- [ ] **Step 3: Write ExternalRow**

Create `web/src/components/ExternalRow.tsx`:
```tsx
import type { ExternalResult, Track } from '../lib/types'
import { formatDuration } from '../lib/types'
import { usePlayer } from '../lib/playerStore'

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

export function ExternalRow({ result }: Props) {
  const playTrackList = usePlayer((s) => s.playTrackList)
  const inLibrary = result.match?.status === 'in_library' && !!result.match.libraryTrackId

  const cover = result.coverUrl ? (
    <img src={result.coverUrl} alt="" className="h-9 w-9 rounded object-cover" />
  ) : (
    <div className="h-9 w-9 rounded bg-neutral-800" />
  )

  const body = (
    <>
      {cover}
      <span className="flex-1 truncate">
        <span className="block truncate text-sm font-medium">{result.title}</span>
        <span className="block truncate text-xs text-neutral-400">{result.artist}</span>
      </span>
      {inLibrary ? (
        <span title="In library" className="text-accent">✓</span>
      ) : (
        /* M3 SEAM: the download affordance (↓ popover + ⟳ progress ring) goes here.
           For M2 an unmatched external result is a plain, non-interactive row. */
        <span className="text-xs text-neutral-600">—</span>
      )}
      <span className="w-12 text-right text-xs text-neutral-500">{formatDuration(result.durationMs)}</span>
    </>
  )

  if (inLibrary) {
    return (
      <button
        type="button"
        onClick={() => playTrackList([trackFromMatch(result, result.match!.libraryTrackId)], 0)}
        className="group flex w-full items-center gap-3 rounded px-2 py-1.5 text-left text-neutral-200 hover:bg-neutral-800"
      >
        {body}
      </button>
    )
  }
  return <div className="flex w-full items-center gap-3 rounded px-2 py-1.5 text-neutral-300">{body}</div>
}
```

- [ ] **Step 4: Write SourceChips**

Create `web/src/components/SourceChips.tsx`:
```tsx
import type { SourceStatus } from '../lib/everywhereStore'

function label(s: SourceStatus): string {
  const name = s.source.charAt(0).toUpperCase() + s.source.slice(1)
  switch (s.status) {
    case 'ok':
      return `${name} ✓`
    case 'timeout':
      return `${name} timed out`
    case 'error':
      return `${name} error`
  }
}

export function SourceChips({ sources }: { sources: SourceStatus[] }) {
  if (sources.length === 0) return null
  return (
    <div className="flex flex-wrap items-center gap-x-2 text-xs text-neutral-400">
      {sources.map((s, i) => (
        <span key={s.source}>
          {label(s)}
          {i < sources.length - 1 ? ' ·' : ''}
        </span>
      ))}
    </div>
  )
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd web && npx vitest run src/components/ExternalRow.test.tsx`
Expected: PASS.

- [ ] **Step 6: Typecheck + commit**

Run: `cd web && npm run build`
```bash
git add web/src/components/ExternalRow.tsx web/src/components/SourceChips.tsx web/src/components/ExternalRow.test.tsx
git commit -m "feat(web): external result row (in-library check) and per-source status chips"
```

---

## Task 15: Activate the Everywhere toggle in `Search.tsx`

**Files:**
- Modify: `web/src/routes/Search.tsx` (activate the toggle; Library mode unchanged)
- Test: `web/src/routes/Search.test.tsx` (add an Everywhere-mode test)

**Interfaces:**
- `Search` gains a `mode: 'library' | 'everywhere'` state (the segmented pill). Library mode is the existing TanStack-Query REST render (unchanged). Everywhere mode calls `useEverywhere(q, 'track', mode==='everywhere')`, renders `SourceChips`, then the three STABLE sections (Tracks via `ExternalRow`, Albums/Artists as simple cards). No download UI (M3).

- [ ] **Step 1: Add the Everywhere-mode test (keep the existing library test)**

Append to `web/src/routes/Search.test.tsx` (after the existing `describe('Search (library mode)')` block):
```tsx
import { act } from '@testing-library/react'
import type { EventSourceLike } from '../lib/searchStream'

describe('Search (everywhere mode)', () => {
  it('streams external results into stable sections with source chips', async () => {
    // Stub EventSource so no real network is opened; capture the instance to emit.
    let inst: { onmessage: ((ev: { data: string }) => void) | null; onerror: (() => void) | null; close(): void } | null = null
    class StubES implements EventSourceLike {
      onmessage: ((ev: { data: string }) => void) | null = null
      onerror: (() => void) | null = null
      constructor(public url: string) {
        inst = this
      }
      close() {}
    }
    vi.stubGlobal('EventSource', StubES as unknown as typeof EventSource)

    render(wrap(<Search />))
    fireEvent.change(screen.getByPlaceholderText(/search/i), { target: { value: 'echoes' } })
    fireEvent.click(screen.getByRole('button', { name: /everywhere/i }))

    act(() => {
      inst!.onmessage?.({
        data: JSON.stringify({
          source: 'spotify',
          status: 'ok',
          results: [
            { source: 'spotify', externalId: 'sp1', title: 'Echoes', artist: 'Vale', album: 'Deep', durationMs: 240000, type: 'track', match: { status: 'in_library', libraryTrackId: 't3', method: 'fuzzy', confidence: 0.9 } },
          ],
        }),
      })
    })

    await waitFor(() => expect(screen.getByText('Echoes')).toBeInTheDocument())
    expect(screen.getByText(/Spotify ✓/)).toBeInTheDocument()
    expect(screen.getByTitle(/in library/i)).toBeInTheDocument()

    vi.unstubAllGlobals()
  })
})
```

> NOTE: the existing library-mode test stubs `fetch` and uses `getByPlaceholderText(/search your library/i)`. The Everywhere test uses the looser `/search/i` so it matches the same input regardless of the placeholder. Both `describe` blocks share the `wrap` helper already defined at the top of the file (no duplicate import needed; add `import { act } from '@testing-library/react'` and the `EventSourceLike` type import at the top with the other imports if not already present).

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/routes/Search.test.tsx`
Expected: FAIL — the Everywhere pill is disabled / no streaming render yet.

- [ ] **Step 3: Rewrite `Search.tsx` to activate Everywhere**

Replace `web/src/routes/Search.tsx`:
```tsx
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useLibrarySearch, coverUrl } from '../lib/libraryApi'
import { TrackRow } from '../components/TrackRow'
import { useEverywhere } from '../lib/everywhereStore'
import { ExternalRow } from '../components/ExternalRow'
import { SourceChips } from '../components/SourceChips'

type Mode = 'library' | 'everywhere'

export default function Search() {
  const [q, setQ] = useState('')
  const [mode, setMode] = useState<Mode>('library')

  // Library mode: a single fast REST query (TanStack Query), unchanged from M1.
  const lib = useLibrarySearch(mode === 'library' ? q : '')
  // Everywhere mode: SSE stream accumulated into stable sections (distinct transport).
  const everywhere = useEverywhere(q, 'track', mode === 'everywhere')

  const tracks = lib.data?.tracks ?? []
  const albums = lib.data?.albums ?? []
  const artists = lib.data?.artists ?? []

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-3">
        <input
          autoFocus
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Search your library…"
          className="w-full max-w-xl rounded bg-neutral-900 px-4 py-2 outline-none ring-1 ring-neutral-800 focus:ring-accent"
        />
        <div className="flex overflow-hidden rounded-full ring-1 ring-neutral-800">
          <button
            type="button"
            onClick={() => setMode('library')}
            className={`px-3 py-1 text-sm ${mode === 'library' ? 'bg-accent text-white' : 'text-neutral-400 hover:text-neutral-200'}`}
          >
            My Library
          </button>
          <button
            type="button"
            onClick={() => setMode('everywhere')}
            className={`px-3 py-1 text-sm ${mode === 'everywhere' ? 'bg-accent text-white' : 'text-neutral-400 hover:text-neutral-200'}`}
          >
            Everywhere
          </button>
        </div>
      </div>

      {q.trim() === '' && <p className="text-neutral-500">Type to search.</p>}

      {mode === 'library' && (
        <>
          {lib.isFetching && <p className="text-neutral-500">Searching…</p>}
          {tracks.length > 0 && (
            <section>
              <h2 className="mb-2 text-lg font-bold">Tracks</h2>
              <div className="space-y-0.5">
                {tracks.map((t, i) => (
                  <TrackRow key={t.id} track={t} index={i} queue={tracks} />
                ))}
              </div>
            </section>
          )}
          {albums.length > 0 && (
            <section>
              <h2 className="mb-2 text-lg font-bold">Albums</h2>
              <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
                {albums.map((al) => (
                  <Link key={al.id} to={`/album/${al.id}`} className="group">
                    {al.coverArtId ? (
                      <img src={coverUrl(al.coverArtId, 300)} alt="" className="aspect-square w-full rounded object-cover" />
                    ) : (
                      <div className="aspect-square w-full rounded bg-neutral-800" />
                    )}
                    <div className="mt-1 truncate text-sm font-medium group-hover:text-accent">{al.name}</div>
                    <div className="truncate text-xs text-neutral-400">{al.artist}</div>
                  </Link>
                ))}
              </div>
            </section>
          )}
          {artists.length > 0 && (
            <section>
              <h2 className="mb-2 text-lg font-bold">Artists</h2>
              <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6">
                {artists.map((ar) => (
                  <Link key={ar.id} to={`/artist/${ar.id}`} className="group text-center">
                    {ar.coverArtId ? (
                      <img src={coverUrl(ar.coverArtId, 300)} alt="" className="aspect-square w-full rounded-full object-cover" />
                    ) : (
                      <div className="aspect-square w-full rounded-full bg-neutral-800" />
                    )}
                    <div className="mt-1 truncate text-sm font-medium group-hover:text-accent">{ar.name}</div>
                  </Link>
                ))}
              </div>
            </section>
          )}
        </>
      )}

      {mode === 'everywhere' && q.trim() !== '' && (
        <>
          <SourceChips sources={everywhere.sources} />
          {/* Stable sections: results append within each section, never reflow. */}
          <section>
            <h2 className="mb-2 text-lg font-bold">Tracks</h2>
            {everywhere.tracks.length === 0 ? (
              <p className="text-neutral-500">Searching sources…</p>
            ) : (
              <div className="space-y-0.5">
                {everywhere.tracks.map((r) => (
                  <ExternalRow key={`${r.source}:${r.externalId}`} result={r} />
                ))}
              </div>
            )}
          </section>
          {everywhere.albums.length > 0 && (
            <section>
              <h2 className="mb-2 text-lg font-bold">Albums</h2>
              <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
                {everywhere.albums.map((r) => (
                  <div key={`${r.source}:${r.externalId}`} className="group">
                    {r.coverUrl ? (
                      <img src={r.coverUrl} alt="" className="aspect-square w-full rounded object-cover" />
                    ) : (
                      <div className="aspect-square w-full rounded bg-neutral-800" />
                    )}
                    <div className="mt-1 truncate text-sm font-medium">{r.title}</div>
                    <div className="truncate text-xs text-neutral-400">{r.artist}</div>
                  </div>
                ))}
              </div>
            </section>
          )}
          {everywhere.artists.length > 0 && (
            <section>
              <h2 className="mb-2 text-lg font-bold">Artists</h2>
              <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6">
                {everywhere.artists.map((r) => (
                  <div key={`${r.source}:${r.externalId}`} className="group text-center">
                    {r.coverUrl ? (
                      <img src={r.coverUrl} alt="" className="aspect-square w-full rounded-full object-cover" />
                    ) : (
                      <div className="aspect-square w-full rounded-full bg-neutral-800" />
                    )}
                    <div className="mt-1 truncate text-sm font-medium">{r.title}</div>
                  </div>
                ))}
              </div>
            </section>
          )}
        </>
      )}
    </div>
  )
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd web && npx vitest run src/routes/Search.test.tsx`
Expected: PASS (library-mode test + everywhere-mode test).

- [ ] **Step 5: Full frontend test + typecheck**

Run: `cd web && npm run test && npm run build`
Expected: all tests PASS; build succeeds.

- [ ] **Step 6: Commit**

```bash
git add web/src/routes/Search.tsx web/src/routes/Search.test.tsx
git commit -m "feat(web): activate Everywhere search with stable sections and source chips"
```

---

## Task 16: Full-stack smoke verification (live Spotify, optional)

**Files:** none (verification-only).

**Interfaces:** confirms the SSE wire end-to-end with a real Spotify app credential. Requires a Spotify developer client_id/client_secret (free). If no credentials are available, this task is documented-but-skippable — the unit/httptest coverage already proves the behavior.

- [ ] **Step 1: Seed a Spotify search adapter_instance**

With Crate stopped, seed the row (until the settings UI lands in M4). The DB is at `./data/crate.db`:
```bash
sqlite3 data/crate.db "INSERT INTO adapter_instances (id,type,name,enabled,priority,config_json) VALUES ('srch1','search','spotify',1,0,'{\"client_id\":\"YOUR_CLIENT_ID\"}');"
```
Start Crate with the secret in the env (never in config_json):
```bash
CRATE_ADMIN_PASSWORD=devpw CRATE_SPOTIFY_CLIENT_SECRET=YOUR_CLIENT_SECRET go run ./cmd/crate &
sleep 2
```
Expected log lines: `search sources active: 1` (and `library adapter active: subsonic` if M1's library row is also seeded).

- [ ] **Step 2: Log in for a session cookie**

Run:
```bash
curl -s -c /tmp/crate.cookies -X POST localhost:8090/api/v1/auth/login -H 'Content-Type: application/json' -d '{"password":"devpw"}'
```
Expected: `{"ok":true}`.

- [ ] **Step 3: Smoke the SSE endpoint**

Run (stream a few seconds, then it ends when the aggregator closes):
```bash
curl -sN -b /tmp/crate.cookies "localhost:8090/api/v1/search/everywhere?q=daft%20punk&type=track" | head -c 600
```
Expected: one or more `data: {"source":"spotify","status":"ok","results":[...]}` lines, each result carrying `match` (`in_library` if it is in your seeded Navidrome library, else `not_in_library`). With no library configured, `match.status` is `not_in_library` for everything (matcher with a nil library returns negatives — note: when `libAdapter` is nil the aggregator's matcher is nil, so `match` is absent; that is acceptable for M2 and the UI renders plain rows).

- [ ] **Step 4: Confirm the secret never leaks**

Run:
```bash
curl -s -b /tmp/crate.cookies "localhost:8090/api/v1/adapters/available" | grep -i secret || echo "no secret value exposed"
curl -sN -b /tmp/crate.cookies "localhost:8090/api/v1/search/everywhere?q=x&type=track" | grep -i "YOUR_CLIENT_SECRET" || echo "secret not in SSE stream"
```
Expected: `no secret value exposed` and `secret not in SSE stream`.

Tear down:
```bash
kill %1 2>/dev/null
```

- [ ] **Step 5: No commit (verification-only).**

---

## Definition of Done (M2)

- `go test ./cmd/... ./internal/...` is green: external/match core types; SearchSource conformance; aggregator (timeout isolation, per-source envelopes, pre-matching, channel close); Spotify client (Basic-auth token + cache) + adapter (ISRC + cover mapping, httptest + recorded JSON) passing `search.RunConformance`; `match_cache` migration + queries + `library_version` accessor; Normalize (fixture-driven, preserves qualifiers, symmetric feat stripping); MatchingService (every fixture case + cache-first + version invalidation + negative caching); the SSE handler (streamed events parsed, auth-gated, 503 when no aggregator); and both composition-wiring helpers.
- `cd web && npm run test` is green: `SearchStream` (stubbed EventSource), the Everywhere reducer (append-in-stable-sections, cross-source dedup, never-reorder, status updates), `ExternalRow` (✓ in-library plays matched track; plain row otherwise), and `Search.tsx` Everywhere mode (streamed render + source chips).
- `cd web && npm run build` (tsc + vite) succeeds — no TS errors.
- sqlc generated code is regenerated and committed (`internal/store/db/match_cache.sql.go`, `library_version.sql.go`, `MatchCache` in `models.go`).
- The migration is additive (`0002_match_cache.sql`); `0001_init.sql` is untouched; `library_version` defaults to 1 (seeded + accessor fallback).
- Spotify search sources are registered EXPLICITLY at the composition root; `CRATE_SPOTIFY_CLIENT_SECRET` overrides `config_json` and never reaches the browser; the SSE endpoint is auth-gated, streams correctly framed `data: ...\n\n` events flushed per source, pre-matches each result via MatchingService, and respects client disconnect.
- The frontend Everywhere mode uses `EventSource` (distinct transport), appends results in stable Tracks/Albums/Artists sections without reflow, dedupes across sources by ISRC/normalized key, shows per-source status chips, and renders ✓ for in-library results (click plays the matched library track) with NO download affordance (M3 seam clearly marked). Library mode remains a normal REST query, unchanged from M1.

---

## Self-Review

**Spec coverage (M2 line items):**
- External + match domain types (`ExternalResult` incl. ISRC/MBID/cover + `Match`, `ExternalAlbum`, `MatchResult{Status,LibraryTrackID,Method,Confidence}`), serializable camelCase ✓ (Task 1).
- `SearchSource` interface embeds `registry.Plugin`; `Search`/`GetAlbum`; optional `DiscographyProvider` (P2-ready, not required by conformance); reusable conformance suite ✓ (Task 2). Fan-out aggregator: per-source goroutine + individual `context.WithTimeout`, channel of `{source,status,results,cursor}` envelopes, one slow source never blocks others, channel closes ✓ (Task 3).
- Spotify adapter: client-credentials OAuth (Basic-auth token + cache/expiry, injectable base URLs + `*http.Client`), search tracks/albums/artists, ISRC from `external_ids.isrc` + cover image, `GetAlbum`, Plugin (ConfigSchema client_id/client_secret[secret], TestConnection=token fetch), httptest + recorded JSON under package `testdata/` (no `..`) ✓ (Tasks 4–5).
- MatchingService: PURE symmetric `Normalize` (lowercase, strip punctuation, collapse ws, unicode fold, feat/featuring/ft stripped symmetrically, version qualifiers preserved); `Match` priority chain ISRC→MBID(structural)→normalized-fuzzy disambiguated by DURATION(±3s)+album; cache-first via `match_cache` storing positive AND negative, invalidated by `library_version`; queries `LibrarySearcher` (the `LibraryAdapter.Search` slice) for candidates; enumerated fixture corpus authored first (8 files) + table tests; `Normalize` exported for reuse ✓ (Tasks 7–9).
- Store: additive `0002` migration creating `match_cache` (PK(source,external_id), nullable library_track_id, method, confidence, isrc, mbid, duration_ms, library_version, matched_at); queries get/upsert/delete-by-source/clear + `library_version` get/set; sqlc regenerated; `LibraryVersion` accessor (default 1) ✓ (Task 6).
- API + composition: `Search`/aggregator + matching service added to `Deps`; search sources wired from `adapter_instances` (type='search') at the composition root with `CRATE_SPOTIFY_CLIENT_SECRET` override; `GET /api/v1/search/everywhere?q=&type=` as SSE (each event a per-source envelope pre-matched; correct framing, flush per event, respects request-context disconnect; aggregator closes channel so handler returns); auth-gated; local `/library/search` kept as-is ✓ (Tasks 10–11).
- Frontend: distinct `SearchStream` (`EventSource`, same-origin, cookie auto, `close()` on unmount); Everywhere toggle activated in `Search.tsx`; append-in-stable-sections deduped across sources by ISRC/normalized key, never reordering; per-source status chips; rows show ✓ for `match.status==='in_library'` (click plays matched library track) else plain row with NO download affordance (M3 seam marked); Library mode stays REST ✓ (Tasks 12–15).

**Placeholder scan:** every code block is complete and runnable. No `TODO`/`add error handling`/`similar to above`. The two intentional, clearly-labeled seams are: the MBID rung in `matching.go` (structural — `core.Track` has no MBID field in M2; documented as a P2 drop-in, with ISRC + fuzzy fully exercised) and the M3 download affordance comment inside `ExternalRow.tsx` (the spot where ↓/⟳ land in M3). Both are explicitly flagged, not unfinished work.

**Type consistency across tasks:**
- `core.ExternalResult` field set + camelCase JSON tags (Task 1) is mirrored exactly by `web/src/lib/types.ts` `ExternalResult` (Task 12): `externalId`, `durationMs`, `coverUrl`, `coverArtId`, optional `isrc`/`mbid`/`match`. `MatchResult` (`status`, `libraryTrackId`, `method`, `confidence`) matches on both sides; `MatchStatus`/`MatchMethod` string values are identical (`in_library`/`not_in_library`/`unknown`; `isrc`/`mbid`/`fuzzy`/`none`).
- `search.Envelope` (`source`, `status`, `results`, `cursor`, `error`) ↔ `SearchEnvelope` in TS (Task 12); `EnvelopeStatus` values `ok`/`timeout`/`error` match the Go consts (Task 2).
- `search.Matcher.Match(ctx, core.ExternalResult) (core.MatchResult, error)` (Task 3) is implemented by `matching.Service.Match` (Task 9) with the identical signature, so the service is passed straight into `NewAggregator` (Task 11). The aggregator sets `Result.Match` (pointer) which the SSE handler marshals and the frontend reads.
- `matching.LibrarySearcher.Search(ctx, q, []core.EntityType) (core.SearchResults, error)` (Task 9) is exactly the M1 `library.LibraryAdapter.Search` signature, so `libAdapter` satisfies it directly at the composition root (Task 11) with no adapter shim.
- `matching.MatchCacheStore` (Task 9) is the `*db.Queries` slice (`GetMatchCache`/`UpsertMatchCache`) generated in Task 6; `db.GetMatchCacheParams` / `db.UpsertMatchCacheParams` / `db.MatchCache` (with `LibraryTrackID sql.NullString`) are produced by sqlc from the Task-6 queries and consumed identically in `matching_test.go`, `store_test.go`, and `matching.go`.
- `api.Deps.SearchAggregator` is the `Streamer` interface (`Stream(ctx, q, core.EntityType) <-chan search.Envelope`, Task 10), satisfied by `*search.Aggregator` (Task 3) and the test `fakeAgg`; the two-step deps build in `main.go` (Task 11) keeps it a true nil interface when no sources exist so the 503 path holds.
- `everywhereStore.dedupKey` (Task 13) uses a client `normalize` that mirrors the backend `Normalize` semantics (feat stripping, &→and, punctuation drop, ws collapse) so cross-source dedup keys agree with server-side intent; the reducer's append-only contract guarantees the "never reflow" UX requirement (Task 13/15).
- `ExternalRow` synthesizes a `Track` (Task 14) using the exact M1 `Track` shape (`web/src/lib/types.ts`) with `match.libraryTrackId` as `id`, and calls `usePlayer().playTrackList` (the M1 player action) so playback flows through the existing stream proxy unchanged.

**Known follow-ups (intentionally deferred, not M2):** MBID matching needs a library MBID source (P2 — structural rung already in place); the download affordance (↓ popover, ⟳ progress ring, `download` field on results, ⟳→✓ in-place patch) is M3; `DiscographyProvider`-backed artist discography pages are P2; SSE `Last-Event-ID` reconnection is a polish follow-up (M2 opens a fresh stream per query, which is correct for search); settings UI to create the `search` `adapter_instances` row is M4 (M2 seeds it via SQL in the Task-16 smoke); pagination via `cursor` is plumbed through the envelope but the UI consumes only the first batch per source for M2.
