# Reverb M1 — Library Playback Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Each task is a self-contained unit: a fresh implementer with ZERO prior context can complete it from the file paths, interfaces, and complete code given here.

**Goal:** Connect Reverb to a Navidrome/Subsonic server, browse and search YOUR library, and play tracks with a real queue (shuffle/repeat/near-gapless). This is the "library playback spine": core domain types → `LibraryAdapter` interface (+conformance suite) → Subsonic adapter (token auth, httptest-tested) → composition wiring + REST/stream/cover handlers → frontend AudioEngine (dual-`<audio>`, outside React) → Zustand player store → player bar (waveform-styled seek) → Play Queue panel → Library-mode search + Album/Artist pages.

**Architecture:** Builds directly on the M0 foundation (modular-monolith Go binary serving `/api/v1` + embedded React SPA). M1 adds a `core` domain package, a `library` package (interface + conformance), a `library/subsonic` adapter, new REST handlers and a streaming proxy mounted on the existing chi router, and a frontend player subsystem. The library is NEVER persisted in SQLite — it always comes through the `LibraryAdapter`, so a future standalone adapter (P3) is a drop-in. Streaming is a Range-forwarding proxy so Subsonic credentials never reach the browser.

**Tech Stack:** Go 1.23, chi v5, `crypto/md5`, `net/http`, `net/http/httptest` (no live Subsonic server in tests); React 19, TypeScript ~6, Vite 8, Vitest 4, Tailwind 3.4, React Router 6, TanStack Query 5, Zustand 4 (all already installed in `web/`).

## Global Constraints

- Go module path: `github.com/maximusjb/reverb` (verbatim in every `import`).
- Go version floor: `go 1.23`.
- SQLite driver: `modernc.org/sqlite` only (cgo-free). **Library data is NEVER persisted in SQLite** — it always comes from the `LibraryAdapter` (keeps standalone-mode P3 a drop-in). No new migrations in M1.
- API base path: `/api/v1` for every endpoint. Session-cookie auth (`reverb_session`); all M1 endpoints (including stream/cover) sit behind `requireAuth`. HTML5 `<audio>`/`<img>` send the cookie automatically for same-origin requests, so cookie auth works for media.
- **Streaming is a PROXY** through `GET /api/v1/stream/:id`, forwarding the inbound `Range` header upstream and copying back status (200 or 206), `Content-Type`, `Content-Length`, `Accept-Ranges`, `Content-Range`. Subsonic credentials never reach the browser. Use an injectable `*http.Client` so the proxy is testable with `httptest`.
- **Adapter registration is EXPLICIT at the composition root** (`cmd/reverb/main.go`), NOT `init()` side-effects. The registry holds factories; `main` builds the active adapter from the enabled `library` `adapter_instance` row, applying env secret overrides (e.g. `REVERB_LIBRARY_PASSWORD`) just before `Init()`.
- Subsonic token auth: query params `u`=username, `t`=md5(password+salt), `s`=salt, `v`=1.16.1, `c`=reverb, `f`=json. Salt is a random per-request hex string. Subsonic JSON is wrapped: `{"subsonic-response":{"status":"ok",...}}` — decode and check `status`; map `"failed"` to a Go error including the error `code` and `message`.
- `go:embed` cannot use `..` (M0 footgun). Recorded test JSON lives under the test package dir: `internal/library/subsonic/testdata/`.
- Frontend: TanStack Query for server state, Zustand for player/UI state. The `AudioEngine` is imperative and lives OUTSIDE React; near-gapless via dual-`<audio>` preload; the seek bar is waveform-STYLED (true peaks deferred). The audio-element dependency in `AudioEngine` is injectable/abstracted so queue/shuffle/repeat/advance logic is unit-testable in jsdom without real media playback.
- Tests: TDD always (failing test → confirm red → minimal code → confirm green → commit, conventional-commit messages). Go tests use `httptest` + recorded Subsonic JSON (no live server). Run Go tests with `go test ./cmd/... ./internal/...` (NOT `./...` — avoids `web/node_modules`). Frontend: `cd web && npm run test` (Vitest 4); typecheck via `cd web && npm run build`.
- Every `LibraryAdapter` must pass `library.RunConformance(t, adapter)`.

---

## File Structure

**Go (backend) — created/modified in M1:**

| Path | Responsibility |
|---|---|
| `internal/core/types.go` | Shared serializable domain types: `EntityType`, `Track`, `Album`, `Artist`, `Playlist`, `SearchResults`, `StreamHandle`, `StreamOpts`, `ScanStatus`, `CoverArt`. JSON-tagged. |
| `internal/library/library.go` | `LibraryAdapter` interface (embeds `registry.Plugin`). |
| `internal/library/conformance.go` | `RunConformance(t, adapter)` suite; documents `StartScan`/`ScanStatus` as optional/stubable. |
| `internal/library/conformance_test.go` | A fake in-memory adapter proving `RunConformance` passes. |
| `internal/library/subsonic/client.go` | Low-level Subsonic HTTP client: token auth, request builder, wrapped-JSON decode, error mapping. Injectable `*http.Client`. |
| `internal/library/subsonic/dto.go` | Subsonic JSON DTO structs (the `subsonic-response` envelope + entities). |
| `internal/library/subsonic/adapter.go` | `Adapter` implementing `registry.Plugin` + `library.LibraryAdapter`; maps Subsonic DTOs → `core` types. (Will get large — keep mapping helpers here.) |
| `internal/library/subsonic/client_test.go` | Token-auth + envelope/error-mapping unit tests (httptest). |
| `internal/library/subsonic/adapter_test.go` | Adapter method tests against recorded JSON + `library.RunConformance`. |
| `internal/library/subsonic/testdata/*.json` | Recorded Subsonic responses (ping, search3, getArtists, getArtist, getAlbum, getAlbumList2, getPlaylists, getPlaylist, getScanStatus, error). |
| `internal/api/library.go` | REST handlers: search/artists/artist/album/playlists. |
| `internal/api/stream.go` | Stream proxy + cover proxy handlers. |
| `internal/api/library_test.go` | Handler tests using a fake `library.LibraryAdapter` injected into `Deps`. |
| `internal/api/stream_test.go` | Stream/cover proxy tests (Range forwarding, 206). |
| `internal/api/server.go` | MODIFY: add `Library library.LibraryAdapter` to `Deps`; mount the new routes under `/api/v1`. (`Deps` lives in `server.go`, not `handlers.go`.) |
| `cmd/reverb/library_wiring.go` | `buildLibraryAdapter` — builds the active adapter from the enabled `library` adapter_instance row + env secret override. |
| `cmd/reverb/main.go` | MODIFY: register the subsonic factory, build the active library adapter, pass into `api.Deps.Library`. |

**React (frontend) — created/modified in M1, under `web/`:**

| Path | Responsibility |
|---|---|
| `src/lib/types.ts` | TS mirrors of the `core` domain types + API response shapes. |
| `src/lib/libraryApi.ts` | Typed REST + TanStack Query hooks for library endpoints. |
| `src/lib/audioEngine.ts` | Framework-agnostic dual-`<audio>` engine: queue, transport, shuffle, repeat, volume, preload. Injectable audio-element factory. |
| `src/lib/audioEngine.test.ts` | Vitest unit tests for queue/shuffle/repeat/advance using a fake audio element. |
| `src/lib/playerStore.ts` | Zustand store mirroring `AudioEngine` state + exposing actions; singleton engine. |
| `src/lib/uiStore.ts` | Zustand store for the single right-panel slot (`rightPanel: 'queue' \| 'downloads' \| null`). |
| `src/components/PlayerBar.tsx` | REWRITE: art thumb, title/artist, transport, waveform-styled seek bar w/ buffered range, volume, shuffle/repeat, Queue + (disabled) Downloads buttons, global keyboard shortcuts. |
| `src/components/PlayQueue.tsx` | Right slide-over: now-playing header, up-next list, drag-reorder, remove. |
| `src/components/TrackRow.tsx` | Reusable track row with a play action (used by Search/Album/Artist). |
| `src/components/PlayerBar.test.tsx` | RTL test: transport + seek render from store state. |
| `src/components/PlayQueue.test.tsx` | RTL test: queue list + remove. |
| `src/routes/Search.tsx` | REWRITE: Library-mode search box, TanStack-Query results in Tracks/Albums/Artists sections; clicking a track plays it. Marked seam for the M2 Everywhere toggle. |
| `src/routes/Album.tsx` | NEW route `/album/:id`: album metadata + track list with play actions. |
| `src/routes/Artist.tsx` | NEW route `/artist/:id`: artist metadata + album list. |
| `src/routes/Library.tsx` | REWRITE: Artists/Albums browse tabs (getArtists / getAlbumList2). |
| `src/App.tsx` | MODIFY: add routes `/album/:id`, `/artist/:id`; wrap with `QueryClientProvider`; mount `<PlayQueue/>`. |
| `src/main.tsx` | MODIFY: provide a `QueryClient` (or do it in App — App chosen here). |
| `src/components/Sidebar.tsx` | MODIFY: nav still Search/Library/Settings (Library now functional). |

---

## Task 1: Core domain types

**Files:**
- Create: `internal/core/types.go`
- Test: `internal/core/types_test.go`

**Interfaces:**
- Produces (exact, consumed by `library`, `subsonic`, `api`):
  ```go
  type EntityType string
  const ( EntityTrack EntityType = "track"; EntityAlbum EntityType = "album"; EntityArtist EntityType = "artist"; EntityPlaylist EntityType = "playlist" )

  type Track struct {
      ID, Title, AlbumID, Album, ArtistID, Artist, CoverArtID string
      TrackNumber, DiscNumber int
      DurationMs, BitRate int
      Suffix, ContentType string
      ISRC string
  }
  type Album struct {
      ID, Name, ArtistID, Artist, CoverArtID string
      Year, SongCount, DurationMs int
      Tracks []Track
  }
  type Artist struct {
      ID, Name, CoverArtID string
      AlbumCount int
      Albums []Album
  }
  type Playlist struct {
      ID, Name, CoverArtID string
      SongCount, DurationMs int
      Tracks []Track
  }
  type SearchResults struct {
      Tracks []Track; Albums []Album; Artists []Artist
  }
  type StreamOpts struct { MaxBitRate int; Format string }
  type StreamHandle struct {
      Body          io.ReadCloser
      ContentType   string
      ContentLength int64
      AcceptRanges  string
      ContentRange  string
      StatusCode    int
  }
  type ScanStatus struct { Scanning bool; Count int }
  type CoverArt struct { Body io.ReadCloser; ContentType string }
  ```

- [ ] **Step 1: Write the failing test**

Create `internal/core/types_test.go`:
```go
package core

import (
	"encoding/json"
	"testing"
)

func TestTrackJSONRoundTrip(t *testing.T) {
	in := Track{
		ID: "t1", Title: "Song", AlbumID: "al1", Album: "Album",
		ArtistID: "ar1", Artist: "Artist", CoverArtID: "co1",
		TrackNumber: 3, DiscNumber: 1, DurationMs: 210000, BitRate: 320,
		Suffix: "mp3", ContentType: "audio/mpeg", ISRC: "US-X-12",
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Track
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", out, in)
	}
	// JSON keys are camelCase
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if _, ok := m["durationMs"]; !ok {
		t.Fatalf("expected durationMs key, got %v", m)
	}
}

func TestSearchResultsZeroValueMarshals(t *testing.T) {
	b, err := json.Marshal(SearchResults{})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) == "" {
		t.Fatal("empty marshal")
	}
}

func TestEntityTypeConstants(t *testing.T) {
	if EntityTrack != "track" || EntityAlbum != "album" || EntityArtist != "artist" || EntityPlaylist != "playlist" {
		t.Fatal("entity type constant drift")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/ -v`
Expected: FAIL — `undefined: Track` / package has no Go files.

- [ ] **Step 3: Write the implementation**

Create `internal/core/types.go`:
```go
// Package core holds Reverb's shared, serializable domain types. These cross the
// adapter boundary (LibraryAdapter, future SearchSource) and are emitted by the
// REST API, so every exported field carries a stable camelCase JSON tag.
package core

import "io"

type EntityType string

const (
	EntityTrack    EntityType = "track"
	EntityAlbum    EntityType = "album"
	EntityArtist   EntityType = "artist"
	EntityPlaylist EntityType = "playlist"
)

// Track is a single playable item.
type Track struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	AlbumID     string `json:"albumId"`
	Album       string `json:"album"`
	ArtistID    string `json:"artistId"`
	Artist      string `json:"artist"`
	CoverArtID  string `json:"coverArtId"`
	TrackNumber int    `json:"trackNumber"`
	DiscNumber  int    `json:"discNumber"`
	DurationMs  int    `json:"durationMs"`
	BitRate     int    `json:"bitRate"`
	Suffix      string `json:"suffix"`
	ContentType string `json:"contentType"`
	ISRC        string `json:"isrc,omitempty"`
}

type Album struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	ArtistID   string  `json:"artistId"`
	Artist     string  `json:"artist"`
	CoverArtID string  `json:"coverArtId"`
	Year       int     `json:"year"`
	SongCount  int     `json:"songCount"`
	DurationMs int     `json:"durationMs"`
	Tracks     []Track `json:"tracks,omitempty"`
}

type Artist struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	CoverArtID string  `json:"coverArtId"`
	AlbumCount int     `json:"albumCount"`
	Albums     []Album `json:"albums,omitempty"`
}

type Playlist struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	CoverArtID string  `json:"coverArtId"`
	SongCount  int     `json:"songCount"`
	DurationMs int     `json:"durationMs"`
	Tracks     []Track `json:"tracks,omitempty"`
}

type SearchResults struct {
	Tracks  []Track  `json:"tracks"`
	Albums  []Album  `json:"albums"`
	Artists []Artist `json:"artists"`
}

// StreamOpts reserves transcoding knobs; MVP passes through (Navidrome transcodes).
type StreamOpts struct {
	MaxBitRate int    `json:"maxBitRate"`
	Format     string `json:"format"`
}

// StreamHandle is the upstream stream response, carried through the proxy.
// Body must be closed by the consumer. Not JSON-serialized.
type StreamHandle struct {
	Body          io.ReadCloser `json:"-"`
	ContentType   string        `json:"-"`
	ContentLength int64         `json:"-"`
	AcceptRanges  string        `json:"-"`
	ContentRange  string        `json:"-"`
	StatusCode    int           `json:"-"`
}

type ScanStatus struct {
	Scanning bool `json:"scanning"`
	Count    int  `json:"count"`
}

// CoverArt is an image stream from the adapter. Body must be closed. Not JSON-serialized.
type CoverArt struct {
	Body        io.ReadCloser `json:"-"`
	ContentType string        `json:"-"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/ -v`
Expected: PASS (all three).

- [ ] **Step 5: Commit**

```bash
git add internal/core
git commit -m "feat(core): shared serializable domain types for library playback"
```

---

## Task 2: LibraryAdapter interface + conformance suite

**Files:**
- Create: `internal/library/library.go`, `internal/library/conformance.go`
- Test: `internal/library/conformance_test.go`

**Interfaces:**
- Consumes: `internal/registry` (`registry.Plugin`), `internal/core`.
- Produces:
  ```go
  type LibraryAdapter interface {
      registry.Plugin
      Search(ctx context.Context, q string, types []core.EntityType) (core.SearchResults, error)
      GetArtist(ctx context.Context, id string) (core.Artist, error)
      GetAlbum(ctx context.Context, id string) (core.Album, error)
      GetPlaylists(ctx context.Context) ([]core.Playlist, error)
      Stream(ctx context.Context, trackID string, opts core.StreamOpts, rangeHeader string) (core.StreamHandle, error)
      CoverArt(ctx context.Context, id string, size int) (core.CoverArt, error)
      StartScan(ctx context.Context) error
      ScanStatus(ctx context.Context) (core.ScanStatus, error)
  }
  func RunConformance(t *testing.T, a LibraryAdapter)
  ```
  > NOTE on `Stream`: the spec lists `Stream(ctx, trackID, opts)`. We add a `rangeHeader string` param so the adapter forwards the browser's `Range` to upstream. This is the M1 contract every adapter implements.

- [ ] **Step 1: Write the failing conformance test (with a fake adapter)**

Create `internal/library/conformance_test.go`:
```go
package library

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/maximusjb/reverb/internal/core"
	"github.com/maximusjb/reverb/internal/registry"
)

// fakeAdapter is a minimal in-memory LibraryAdapter that satisfies the contract.
type fakeAdapter struct{}

func (fakeAdapter) Type() string                             { return "library" }
func (fakeAdapter) Name() string                             { return "fake" }
func (fakeAdapter) ConfigSchema() registry.ConfigSchema      { return registry.ConfigSchema{} }
func (fakeAdapter) Init(cfg map[string]any) error            { return nil }
func (fakeAdapter) TestConnection(ctx context.Context) error { return nil }

func (fakeAdapter) Search(ctx context.Context, q string, types []core.EntityType) (core.SearchResults, error) {
	return core.SearchResults{
		Tracks:  []core.Track{{ID: "t1", Title: "Song"}},
		Albums:  []core.Album{{ID: "al1", Name: "Album"}},
		Artists: []core.Artist{{ID: "ar1", Name: "Artist"}},
	}, nil
}
func (fakeAdapter) GetArtist(ctx context.Context, id string) (core.Artist, error) {
	return core.Artist{ID: id, Name: "Artist", Albums: []core.Album{{ID: "al1", Name: "Album"}}}, nil
}
func (fakeAdapter) GetAlbum(ctx context.Context, id string) (core.Album, error) {
	return core.Album{ID: id, Name: "Album", Tracks: []core.Track{{ID: "t1", Title: "Song"}}}, nil
}
func (fakeAdapter) GetPlaylists(ctx context.Context) ([]core.Playlist, error) {
	return []core.Playlist{{ID: "p1", Name: "Mix"}}, nil
}
func (fakeAdapter) Stream(ctx context.Context, trackID string, opts core.StreamOpts, rangeHeader string) (core.StreamHandle, error) {
	return core.StreamHandle{
		Body:          io.NopCloser(strings.NewReader("audio-bytes")),
		ContentType:   "audio/mpeg",
		ContentLength: 11,
		AcceptRanges:  "bytes",
		StatusCode:    200,
	}, nil
}
func (fakeAdapter) CoverArt(ctx context.Context, id string, size int) (core.CoverArt, error) {
	return core.CoverArt{Body: io.NopCloser(strings.NewReader("img")), ContentType: "image/jpeg"}, nil
}
func (fakeAdapter) StartScan(ctx context.Context) error { return nil }
func (fakeAdapter) ScanStatus(ctx context.Context) (core.ScanStatus, error) {
	return core.ScanStatus{}, nil
}

func TestFakeAdapterConformance(t *testing.T) {
	RunConformance(t, fakeAdapter{})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/library/ -v`
Expected: FAIL — `undefined: RunConformance` / `undefined: LibraryAdapter`.

- [ ] **Step 3: Write the interface**

Create `internal/library/library.go`:
```go
// Package library defines the LibraryAdapter contract and a conformance suite
// every adapter must pass. Library data is never persisted by Reverb — it always
// flows through an adapter, so a future standalone (folder-scan) adapter is a
// drop-in replacement.
package library

import (
	"context"

	"github.com/maximusjb/reverb/internal/core"
	"github.com/maximusjb/reverb/internal/registry"
)

type LibraryAdapter interface {
	registry.Plugin

	Search(ctx context.Context, q string, types []core.EntityType) (core.SearchResults, error)
	GetArtist(ctx context.Context, id string) (core.Artist, error)
	GetAlbum(ctx context.Context, id string) (core.Album, error)
	GetPlaylists(ctx context.Context) ([]core.Playlist, error)

	// Stream forwards rangeHeader (the browser's inbound Range, may be "")
	// to the upstream source and returns the upstream response for proxying.
	Stream(ctx context.Context, trackID string, opts core.StreamOpts, rangeHeader string) (core.StreamHandle, error)
	CoverArt(ctx context.Context, id string, size int) (core.CoverArt, error)

	// StartScan / ScanStatus are library-maintenance operations modeled on
	// Subsonic/Navidrome. A future folder-scan adapter (P3) owns scanning
	// itself and MAY implement these as no-ops — see RunConformance.
	StartScan(ctx context.Context) error
	ScanStatus(ctx context.Context) (core.ScanStatus, error)
}
```

- [ ] **Step 4: Write the conformance suite**

Create `internal/library/conformance.go`:
```go
package library

import (
	"context"
	"io"
	"testing"

	"github.com/maximusjb/reverb/internal/core"
)

// RunConformance exercises the LibraryAdapter contract. Call it from each
// adapter's test package with a configured, ready-to-use adapter.
//
// StartScan / ScanStatus are treated as OPTIONAL / STUBABLE: an adapter that
// owns its own scanning (a future folder-scan adapter) may implement them as
// no-ops. The suite only asserts they do not panic and return a usable value /
// nil-or-error pair; it never requires a scan to actually run.
func RunConformance(t *testing.T, a LibraryAdapter) {
	t.Helper()
	ctx := context.Background()

	t.Run("Plugin/identity", func(t *testing.T) {
		if a.Type() != "library" {
			t.Errorf("Type() = %q, want \"library\"", a.Type())
		}
		if a.Name() == "" {
			t.Error("Name() must not be empty")
		}
	})

	t.Run("Search/returns-non-nil-slices", func(t *testing.T) {
		res, err := a.Search(ctx, "test", []core.EntityType{core.EntityTrack, core.EntityAlbum, core.EntityArtist})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		// Slices may be empty but must be addressable (no nil-deref downstream).
		_ = res.Tracks
		_ = res.Albums
		_ = res.Artists
	})

	t.Run("GetArtist", func(t *testing.T) {
		ar, err := a.GetArtist(ctx, "ar1")
		if err != nil {
			t.Fatalf("GetArtist: %v", err)
		}
		if ar.ID == "" {
			t.Error("GetArtist returned empty ID")
		}
	})

	t.Run("GetAlbum", func(t *testing.T) {
		al, err := a.GetAlbum(ctx, "al1")
		if err != nil {
			t.Fatalf("GetAlbum: %v", err)
		}
		if al.ID == "" {
			t.Error("GetAlbum returned empty ID")
		}
	})

	t.Run("GetPlaylists", func(t *testing.T) {
		if _, err := a.GetPlaylists(ctx); err != nil {
			t.Fatalf("GetPlaylists: %v", err)
		}
	})

	t.Run("Stream/range-aware", func(t *testing.T) {
		h, err := a.Stream(ctx, "t1", core.StreamOpts{}, "")
		if err != nil {
			t.Fatalf("Stream: %v", err)
		}
		if h.Body == nil {
			t.Fatal("Stream returned nil Body")
		}
		defer h.Body.Close()
		if _, err := io.ReadAll(h.Body); err != nil {
			t.Fatalf("read stream body: %v", err)
		}
		if h.StatusCode == 0 {
			t.Error("Stream returned zero StatusCode")
		}
	})

	t.Run("CoverArt", func(t *testing.T) {
		c, err := a.CoverArt(ctx, "co1", 300)
		if err != nil {
			t.Fatalf("CoverArt: %v", err)
		}
		if c.Body == nil {
			t.Fatal("CoverArt returned nil Body")
		}
		c.Body.Close()
	})

	// Optional / stubable — must not panic; error is acceptable for a no-op adapter.
	t.Run("StartScan/optional", func(t *testing.T) {
		_ = a.StartScan(ctx)
	})
	t.Run("ScanStatus/optional", func(t *testing.T) {
		_, _ = a.ScanStatus(ctx)
	})
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/library/ -v`
Expected: PASS (`TestFakeAdapterConformance` and all subtests).

- [ ] **Step 6: Commit**

```bash
git add internal/library/library.go internal/library/conformance.go internal/library/conformance_test.go
git commit -m "feat(library): LibraryAdapter interface and conformance suite"
```

---

## Task 3: Subsonic client — token auth, envelope decode, error mapping

**Files:**
- Create: `internal/library/subsonic/client.go`, `internal/library/subsonic/dto.go`
- Test: `internal/library/subsonic/client_test.go`

**Interfaces:**
- Consumes: `net/http`, `crypto/md5`, `crypto/rand`, the wrapped Subsonic JSON envelope.
- Produces:
  ```go
  type Client struct { ... }
  func NewClient(baseURL, username, password string, httpClient *http.Client) *Client
  // get builds the authed URL for endpoint+params, performs the request, and
  // returns the raw *http.Response (caller closes Body). Used by stream/cover.
  func (c *Client) RawGet(ctx context.Context, endpoint string, params url.Values, rangeHeader string) (*http.Response, error)
  // getJSON performs RawGet, decodes the subsonic-response envelope into out,
  // and returns an error if status != "ok".
  func (c *Client) GetJSON(ctx context.Context, endpoint string, params url.Values, out any) error
  func (c *Client) Ping(ctx context.Context) error
  ```
  - DTOs in `dto.go`: `type envelope struct { Response subsonicResponse `json:"subsonic-response"` }` and the entity structs used by the adapter.

- [ ] **Step 1: Write the failing client test**

Create `internal/library/subsonic/client_test.go`:
```go
package subsonic

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTokenAuthParams(t *testing.T) {
	var gotQuery map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"subsonic-response":{"status":"ok","version":"1.16.1"}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "alice", "secret", srv.Client())
	if err := c.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}

	if gotQuery.Get("u") != "alice" {
		t.Errorf("u = %q, want alice", gotQuery.Get("u"))
	}
	if gotQuery.Get("v") != "1.16.1" || gotQuery.Get("c") != "reverb" || gotQuery.Get("f") != "json" {
		t.Errorf("missing fixed params: %v", gotQuery)
	}
	salt := gotQuery.Get("s")
	if len(salt) < 8 {
		t.Fatalf("salt too short: %q", salt)
	}
	// token must be md5(password + salt)
	sum := md5.Sum([]byte("secret" + salt))
	wantTok := hex.EncodeToString(sum[:])
	if gotQuery.Get("t") != wantTok {
		t.Errorf("t = %q, want %q", gotQuery.Get("t"), wantTok)
	}
	if gotQuery.Get("p") != "" {
		t.Error("must not send plaintext password param p")
	}
}

func TestGetJSONMapsFailedStatusToError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"subsonic-response":{"status":"failed","version":"1.16.1","error":{"code":40,"message":"Wrong username or password"}}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "alice", "bad", srv.Client())
	// GetJSON takes a *subsonicResponse (or nil to skip payload decode).
	err := c.GetJSON(context.Background(), "ping", nil, nil)
	if err == nil {
		t.Fatal("expected error for failed status")
	}
	if got := err.Error(); got == "" ||
		!contains(got, "40") || !contains(got, "Wrong username or password") {
		t.Fatalf("error missing code/message: %q", got)
	}
}

func TestRawGetForwardsRangeAndReturnsBytes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "bytes=0-3" {
			t.Errorf("Range not forwarded: %q", r.Header.Get("Range"))
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("Accept-Ranges", "bytes")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("abcd"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "alice", "secret", srv.Client())
	resp, err := c.RawGet(context.Background(), "stream", nil, "bytes=0-3")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", resp.StatusCode)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/library/subsonic/ -run TestTokenAuthParams -v`
Expected: FAIL — `undefined: NewClient`.

- [ ] **Step 3: Write the DTOs**

Create `internal/library/subsonic/dto.go`:
```go
package subsonic

// envelope wraps every Subsonic JSON response: {"subsonic-response": {...}}.
type envelope struct {
	Response subsonicResponse `json:"subsonic-response"`
}

type subsonicResponse struct {
	Status  string         `json:"status"`
	Version string         `json:"version"`
	Error   *subsonicError `json:"error,omitempty"`

	// Endpoint-specific payloads (only the one in use is populated).
	SearchResult3 *searchResult3 `json:"searchResult3,omitempty"`
	Artists       *artistsIndex  `json:"artists,omitempty"`
	Artist        *artistDetail  `json:"artist,omitempty"`
	Album         *albumDetail   `json:"album,omitempty"`
	AlbumList2    *albumList2    `json:"albumList2,omitempty"`
	Playlists     *playlistsList `json:"playlists,omitempty"`
	Playlist      *playlistDetail `json:"playlist,omitempty"`
	ScanStatus    *scanStatusDTO `json:"scanStatus,omitempty"`
}

type subsonicError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type childDTO struct {
	ID          string `json:"id"`
	Parent      string `json:"parent"`
	Title       string `json:"title"`
	Album       string `json:"album"`
	AlbumID     string `json:"albumId"`
	Artist      string `json:"artist"`
	ArtistID    string `json:"artistId"`
	CoverArt    string `json:"coverArt"`
	Track       int    `json:"track"`
	DiscNumber  int    `json:"discNumber"`
	Duration    int    `json:"duration"` // seconds
	BitRate     int    `json:"bitRate"`
	Suffix      string `json:"suffix"`
	ContentType string `json:"contentType"`
	IsDir       bool   `json:"isDir"`
}

type albumDTO struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Artist    string     `json:"artist"`
	ArtistID  string     `json:"artistId"`
	CoverArt  string     `json:"coverArt"`
	Year      int        `json:"year"`
	SongCount int        `json:"songCount"`
	Duration  int        `json:"duration"` // seconds
	Song      []childDTO `json:"song"`
}

type artistDTO struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CoverArt   string     `json:"coverArt"`
	AlbumCount int        `json:"albumCount"`
	Album      []albumDTO `json:"album"`
}

type searchResult3 struct {
	Artist []artistDTO `json:"artist"`
	Album  []albumDTO  `json:"album"`
	Song   []childDTO  `json:"song"`
}

type artistsIndex struct {
	Index []struct {
		Name   string      `json:"name"`
		Artist []artistDTO `json:"artist"`
	} `json:"index"`
}

type artistDetail struct {
	artistDTO
}

type albumDetail struct {
	albumDTO
}

type albumList2 struct {
	Album []albumDTO `json:"album"`
}

type playlistDTO struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	CoverArt  string     `json:"coverArt"`
	SongCount int        `json:"songCount"`
	Duration  int        `json:"duration"`
	Entry     []childDTO `json:"entry"`
}

type playlistsList struct {
	Playlist []playlistDTO `json:"playlist"`
}

type playlistDetail struct {
	playlistDTO
}

type scanStatusDTO struct {
	Scanning bool `json:"scanning"`
	Count    int  `json:"count"`
}
```

- [ ] **Step 4: Write the client**

Create `internal/library/subsonic/client.go`:
```go
package subsonic

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	apiVersion = "1.16.1"
	clientName = "reverb"
)

// Client is a low-level Subsonic API client using token auth. The *http.Client
// is injectable so tests can drive it against an httptest.Server.
type Client struct {
	baseURL  string
	username string
	password string
	http     *http.Client
}

func NewClient(baseURL, username, password string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		username: username,
		password: password,
		http:     httpClient,
	}
}

func newSalt() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func token(password, salt string) string {
	sum := md5.Sum([]byte(password + salt))
	return hex.EncodeToString(sum[:])
}

// buildURL appends auth + fixed params to a copy of params and returns the URL.
func (c *Client) buildURL(endpoint string, params url.Values) string {
	q := url.Values{}
	for k, vs := range params {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	salt := newSalt()
	q.Set("u", c.username)
	q.Set("t", token(c.password, salt))
	q.Set("s", salt)
	q.Set("v", apiVersion)
	q.Set("c", clientName)
	q.Set("f", "json")
	return fmt.Sprintf("%s/rest/%s?%s", c.baseURL, endpoint, q.Encode())
}

// RawGet performs an authed GET and returns the raw response (caller closes Body).
// rangeHeader, when non-empty, is forwarded as the inbound Range request header.
func (c *Client) RawGet(ctx context.Context, endpoint string, params url.Values, rangeHeader string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.buildURL(endpoint, params), nil)
	if err != nil {
		return nil, err
	}
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	return c.http.Do(req)
}

// GetJSON performs RawGet, decodes the subsonic-response envelope, validates the
// status, and unmarshals the response payload into out (out must be a *subsonicResponse
// or nil to skip payload decoding). It returns an error for status == "failed".
func (c *Client) GetJSON(ctx context.Context, endpoint string, params url.Values, out *subsonicResponse) error {
	resp, err := c.RawGet(ctx, endpoint, params, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("subsonic %s: HTTP %d", endpoint, resp.StatusCode)
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("subsonic %s: decode: %w", endpoint, err)
	}
	if env.Response.Status != "ok" {
		if env.Response.Error != nil {
			return fmt.Errorf("subsonic %s: error %d: %s", endpoint, env.Response.Error.Code, env.Response.Error.Message)
		}
		return fmt.Errorf("subsonic %s: status %q", endpoint, env.Response.Status)
	}
	if out != nil {
		*out = env.Response
	}
	return nil
}

func (c *Client) Ping(ctx context.Context) error {
	return c.GetJSON(ctx, "ping", nil, nil)
}
```

> Signature note: `GetJSON`'s last parameter is `*subsonicResponse` (pass `nil` to skip payload decoding). The Task-3 test already passes `nil`, so it matches as written — no separate fix step is needed.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/library/subsonic/ -v`
Expected: PASS (`TestTokenAuthParams`, `TestGetJSONMapsFailedStatusToError`, `TestRawGetForwardsRangeAndReturnsBytes`).

- [ ] **Step 6: Commit**

```bash
git add internal/library/subsonic/client.go internal/library/subsonic/dto.go internal/library/subsonic/client_test.go
git commit -m "feat(subsonic): token-auth client with envelope decode and error mapping"
```

---

## Task 4: Subsonic adapter — Plugin + LibraryAdapter, DTO→core mapping, conformance

**Files:**
- Create: `internal/library/subsonic/adapter.go`, `internal/library/subsonic/testdata/*.json`
- Test: `internal/library/subsonic/adapter_test.go`

**Interfaces:**
- Consumes: `Client` (Task 3), `core`, `registry`, `library.RunConformance`.
- Produces:
  ```go
  type Adapter struct { client *Client; baseURL, username, password string; httpClient *http.Client }
  func New() *Adapter                 // factory for the registry (zero-value, configured via Init)
  func (a *Adapter) Type() string                 // "library"
  func (a *Adapter) Name() string                 // "subsonic"
  func (a *Adapter) ConfigSchema() registry.ConfigSchema  // url, username, password[secret]
  func (a *Adapter) Init(cfg map[string]any) error        // reads url/username/password, builds Client
  func (a *Adapter) TestConnection(ctx context.Context) error // ping
  // + all LibraryAdapter methods (Search, GetArtist, GetAlbum, GetPlaylists, Stream, CoverArt, StartScan, ScanStatus)
  func (a *Adapter) WithHTTPClient(h *http.Client) *Adapter // test seam: inject httptest client before Init
  ```
  - `var _ library.LibraryAdapter = (*Adapter)(nil)` compile-time assertion in adapter.go.

- [ ] **Step 1: Record the test fixtures**

Create `internal/library/subsonic/testdata/ping.json`:
```json
{"subsonic-response":{"status":"ok","version":"1.16.1"}}
```

Create `internal/library/subsonic/testdata/error.json`:
```json
{"subsonic-response":{"status":"failed","version":"1.16.1","error":{"code":40,"message":"Wrong username or password"}}}
```

Create `internal/library/subsonic/testdata/search3.json`:
```json
{"subsonic-response":{"status":"ok","version":"1.16.1","searchResult3":{
  "artist":[{"id":"ar1","name":"The Artists","coverArt":"ar-1","albumCount":2}],
  "album":[{"id":"al1","name":"First Album","artist":"The Artists","artistId":"ar1","coverArt":"al-1","year":2020,"songCount":2,"duration":420}],
  "song":[{"id":"t1","parent":"al1","title":"Opening","album":"First Album","albumId":"al1","artist":"The Artists","artistId":"ar1","coverArt":"al-1","track":1,"discNumber":1,"duration":210,"bitRate":320,"suffix":"mp3","contentType":"audio/mpeg","isDir":false}]
}}}
```

Create `internal/library/subsonic/testdata/getArtists.json`:
```json
{"subsonic-response":{"status":"ok","version":"1.16.1","artists":{"index":[
  {"name":"T","artist":[{"id":"ar1","name":"The Artists","coverArt":"ar-1","albumCount":2}]},
  {"name":"Z","artist":[{"id":"ar2","name":"Zephyr","coverArt":"ar-2","albumCount":1}]}
]}}}
```

Create `internal/library/subsonic/testdata/getArtist.json`:
```json
{"subsonic-response":{"status":"ok","version":"1.16.1","artist":{
  "id":"ar1","name":"The Artists","coverArt":"ar-1","albumCount":2,
  "album":[
    {"id":"al1","name":"First Album","artist":"The Artists","artistId":"ar1","coverArt":"al-1","year":2020,"songCount":2,"duration":420},
    {"id":"al2","name":"Second Album","artist":"The Artists","artistId":"ar1","coverArt":"al-2","year":2022,"songCount":1,"duration":180}
  ]
}}}
```

Create `internal/library/subsonic/testdata/getAlbum.json`:
```json
{"subsonic-response":{"status":"ok","version":"1.16.1","album":{
  "id":"al1","name":"First Album","artist":"The Artists","artistId":"ar1","coverArt":"al-1","year":2020,"songCount":2,"duration":420,
  "song":[
    {"id":"t1","parent":"al1","title":"Opening","album":"First Album","albumId":"al1","artist":"The Artists","artistId":"ar1","coverArt":"al-1","track":1,"discNumber":1,"duration":210,"bitRate":320,"suffix":"mp3","contentType":"audio/mpeg","isDir":false},
    {"id":"t2","parent":"al1","title":"Closing","album":"First Album","albumId":"al1","artist":"The Artists","artistId":"ar1","coverArt":"al-1","track":2,"discNumber":1,"duration":210,"bitRate":320,"suffix":"mp3","contentType":"audio/mpeg","isDir":false}
  ]
}}}
```

Create `internal/library/subsonic/testdata/getAlbumList2.json`:
```json
{"subsonic-response":{"status":"ok","version":"1.16.1","albumList2":{"album":[
  {"id":"al1","name":"First Album","artist":"The Artists","artistId":"ar1","coverArt":"al-1","year":2020,"songCount":2,"duration":420},
  {"id":"al2","name":"Second Album","artist":"The Artists","artistId":"ar1","coverArt":"al-2","year":2022,"songCount":1,"duration":180}
]}}}
```

Create `internal/library/subsonic/testdata/getPlaylists.json`:
```json
{"subsonic-response":{"status":"ok","version":"1.16.1","playlists":{"playlist":[
  {"id":"p1","name":"Roadtrip","coverArt":"pl-1","songCount":2,"duration":420}
]}}}
```

Create `internal/library/subsonic/testdata/getScanStatus.json`:
```json
{"subsonic-response":{"status":"ok","version":"1.16.1","scanStatus":{"scanning":true,"count":1234}}}
```

- [ ] **Step 2: Write the failing adapter test**

Create `internal/library/subsonic/adapter_test.go`:
```go
package subsonic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/maximusjb/reverb/internal/core"
	"github.com/maximusjb/reverb/internal/library"
)

// fixtureServer serves a recorded JSON file based on the requested endpoint
// (the last path segment of /rest/<endpoint>). Unknown endpoints → ping.json.
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	routes := map[string]string{
		"ping":          "ping.json",
		"search3":       "search3.json",
		"getArtists":    "getArtists.json",
		"getArtist":     "getArtist.json",
		"getAlbum":      "getAlbum.json",
		"getAlbumList2": "getAlbumList2.json",
		"getPlaylists":  "getPlaylists.json",
		"getScanStatus": "getScanStatus.json",
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		endpoint := filepath.Base(r.URL.Path)
		switch endpoint {
		case "stream":
			w.Header().Set("Content-Type", "audio/mpeg")
			w.Header().Set("Accept-Ranges", "bytes")
			if r.Header.Get("Range") != "" {
				w.Header().Set("Content-Range", "bytes 0-3/100")
				w.WriteHeader(http.StatusPartialContent)
			}
			_, _ = w.Write([]byte("abcd"))
			return
		case "getCoverArt":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("img-bytes"))
			return
		case "startScan":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"subsonic-response":{"status":"ok","version":"1.16.1","scanStatus":{"scanning":true,"count":0}}}`))
			return
		}
		file, ok := routes[endpoint]
		if !ok {
			file = "ping.json"
		}
		b, err := os.ReadFile(filepath.Join("testdata", file))
		if err != nil {
			t.Fatalf("read fixture %s: %v", file, err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}))
}

func newTestAdapter(t *testing.T) *Adapter {
	t.Helper()
	srv := fixtureServer(t)
	t.Cleanup(srv.Close)
	a := New().WithHTTPClient(srv.Client())
	if err := a.Init(map[string]any{
		"url":      srv.URL,
		"username": "alice",
		"password": "secret",
	}); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAdapterIdentityAndSchema(t *testing.T) {
	a := New()
	if a.Type() != "library" || a.Name() != "subsonic" {
		t.Fatalf("identity: %q/%q", a.Type(), a.Name())
	}
	keys := map[string]bool{}
	for _, f := range a.ConfigSchema().Fields {
		keys[f.Key] = f.Secret
	}
	if _, ok := keys["url"]; !ok {
		t.Error("schema missing url")
	}
	if _, ok := keys["username"]; !ok {
		t.Error("schema missing username")
	}
	if secret, ok := keys["password"]; !ok || !secret {
		t.Error("schema missing password or password not marked secret")
	}
}

func TestSearchMapsToCore(t *testing.T) {
	a := newTestAdapter(t)
	res, err := a.Search(context.Background(), "x", []core.EntityType{core.EntityTrack, core.EntityAlbum, core.EntityArtist})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tracks) != 1 || res.Tracks[0].ID != "t1" || res.Tracks[0].Title != "Opening" {
		t.Fatalf("tracks: %+v", res.Tracks)
	}
	if res.Tracks[0].DurationMs != 210000 {
		t.Fatalf("duration not seconds→ms: %d", res.Tracks[0].DurationMs)
	}
	if len(res.Albums) != 1 || res.Albums[0].Name != "First Album" {
		t.Fatalf("albums: %+v", res.Albums)
	}
	if len(res.Artists) != 1 || res.Artists[0].Name != "The Artists" {
		t.Fatalf("artists: %+v", res.Artists)
	}
}

func TestGetAlbumIncludesTracks(t *testing.T) {
	a := newTestAdapter(t)
	al, err := a.GetAlbum(context.Background(), "al1")
	if err != nil {
		t.Fatal(err)
	}
	if al.Name != "First Album" || len(al.Tracks) != 2 {
		t.Fatalf("album: %+v", al)
	}
	if al.Tracks[1].Title != "Closing" {
		t.Fatalf("track order: %+v", al.Tracks)
	}
}

func TestGetArtistIncludesAlbums(t *testing.T) {
	a := newTestAdapter(t)
	ar, err := a.GetArtist(context.Background(), "ar1")
	if err != nil {
		t.Fatal(err)
	}
	if ar.Name != "The Artists" || len(ar.Albums) != 2 {
		t.Fatalf("artist: %+v", ar)
	}
}

func TestStreamForwardsRange(t *testing.T) {
	a := newTestAdapter(t)
	h, err := a.Stream(context.Background(), "t1", core.StreamOpts{}, "bytes=0-3")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Body.Close()
	if h.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", h.StatusCode)
	}
	if h.ContentRange == "" {
		t.Error("missing Content-Range passthrough")
	}
}

func TestCoverArt(t *testing.T) {
	a := newTestAdapter(t)
	c, err := a.CoverArt(context.Background(), "al-1", 300)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Body.Close()
	if c.ContentType != "image/jpeg" {
		t.Fatalf("content type = %q", c.ContentType)
	}
}

func TestScanStatus(t *testing.T) {
	a := newTestAdapter(t)
	st, err := a.ScanStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.Scanning || st.Count != 1234 {
		t.Fatalf("scan status: %+v", st)
	}
}

func TestSubsonicConformance(t *testing.T) {
	a := newTestAdapter(t)
	library.RunConformance(t, a)
}
```

> NOTE: `RunConformance` calls `GetArtist(ctx,"ar1")`, `GetAlbum(ctx,"al1")`, `CoverArt(ctx,"co1",...)`, `Stream(ctx,"t1",...)`. The fixture server ignores IDs and serves the same recorded file per endpoint, so all of these succeed.

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/library/subsonic/ -run TestAdapterIdentityAndSchema -v`
Expected: FAIL — `undefined: New` / `Adapter`.

- [ ] **Step 4: Write the adapter**

Create `internal/library/subsonic/adapter.go`:
```go
package subsonic

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/maximusjb/reverb/internal/core"
	"github.com/maximusjb/reverb/internal/library"
	"github.com/maximusjb/reverb/internal/registry"
)

// compile-time assertions
var (
	_ library.LibraryAdapter = (*Adapter)(nil)
	_ registry.Plugin        = (*Adapter)(nil)
)

// Adapter is the Subsonic/Navidrome LibraryAdapter. Configure it via Init.
type Adapter struct {
	baseURL    string
	username   string
	password   string
	httpClient *http.Client
	client     *Client
}

// New returns an unconfigured adapter (the registry factory).
func New() *Adapter { return &Adapter{} }

// WithHTTPClient injects an *http.Client (test seam). Call before Init.
func (a *Adapter) WithHTTPClient(h *http.Client) *Adapter {
	a.httpClient = h
	return a
}

func (a *Adapter) Type() string { return "library" }
func (a *Adapter) Name() string { return "subsonic" }

func (a *Adapter) ConfigSchema() registry.ConfigSchema {
	return registry.ConfigSchema{Fields: []registry.ConfigField{
		{Key: "url", Label: "Server URL", Type: "string", Required: true},
		{Key: "username", Label: "Username", Type: "string", Required: true},
		{Key: "password", Label: "Password", Type: "string", Required: true, Secret: true},
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
	a.baseURL = cfgString(cfg, "url")
	a.username = cfgString(cfg, "username")
	a.password = cfgString(cfg, "password")
	if a.baseURL == "" || a.username == "" {
		return fmt.Errorf("subsonic: url and username are required")
	}
	a.client = NewClient(a.baseURL, a.username, a.password, a.httpClient)
	return nil
}

func (a *Adapter) TestConnection(ctx context.Context) error {
	if a.client == nil {
		return fmt.Errorf("subsonic: not initialized")
	}
	return a.client.Ping(ctx)
}

// --- mapping helpers (Subsonic seconds → core ms; field renames) ---

func mapTrack(c childDTO) core.Track {
	return core.Track{
		ID:          c.ID,
		Title:       c.Title,
		AlbumID:     c.AlbumID,
		Album:       c.Album,
		ArtistID:    c.ArtistID,
		Artist:      c.Artist,
		CoverArtID:  c.CoverArt,
		TrackNumber: c.Track,
		DiscNumber:  c.DiscNumber,
		DurationMs:  c.Duration * 1000,
		BitRate:     c.BitRate,
		Suffix:      c.Suffix,
		ContentType: c.ContentType,
	}
}

func mapAlbum(a albumDTO) core.Album {
	al := core.Album{
		ID:         a.ID,
		Name:       a.Name,
		ArtistID:   a.ArtistID,
		Artist:     a.Artist,
		CoverArtID: a.CoverArt,
		Year:       a.Year,
		SongCount:  a.SongCount,
		DurationMs: a.Duration * 1000,
	}
	for _, s := range a.Song {
		al.Tracks = append(al.Tracks, mapTrack(s))
	}
	return al
}

func mapArtist(a artistDTO) core.Artist {
	ar := core.Artist{
		ID:         a.ID,
		Name:       a.Name,
		CoverArtID: a.CoverArt,
		AlbumCount: a.AlbumCount,
	}
	for _, al := range a.Album {
		ar.Albums = append(ar.Albums, mapAlbum(al))
	}
	return ar
}

func mapPlaylist(p playlistDTO) core.Playlist {
	pl := core.Playlist{
		ID:         p.ID,
		Name:       p.Name,
		CoverArtID: p.CoverArt,
		SongCount:  p.SongCount,
		DurationMs: p.Duration * 1000,
	}
	for _, e := range p.Entry {
		pl.Tracks = append(pl.Tracks, mapTrack(e))
	}
	return pl
}

// --- LibraryAdapter methods ---

func (a *Adapter) Search(ctx context.Context, q string, types []core.EntityType) (core.SearchResults, error) {
	params := url.Values{}
	params.Set("query", q)
	var resp subsonicResponse
	if err := a.client.GetJSON(ctx, "search3", params, &resp); err != nil {
		return core.SearchResults{}, err
	}
	res := core.SearchResults{Tracks: []core.Track{}, Albums: []core.Album{}, Artists: []core.Artist{}}
	if resp.SearchResult3 != nil {
		for _, s := range resp.SearchResult3.Song {
			res.Tracks = append(res.Tracks, mapTrack(s))
		}
		for _, al := range resp.SearchResult3.Album {
			res.Albums = append(res.Albums, mapAlbum(al))
		}
		for _, ar := range resp.SearchResult3.Artist {
			res.Artists = append(res.Artists, mapArtist(ar))
		}
	}
	return res, nil
}

func (a *Adapter) GetArtist(ctx context.Context, id string) (core.Artist, error) {
	params := url.Values{}
	params.Set("id", id)
	var resp subsonicResponse
	if err := a.client.GetJSON(ctx, "getArtist", params, &resp); err != nil {
		return core.Artist{}, err
	}
	if resp.Artist == nil {
		return core.Artist{}, fmt.Errorf("subsonic getArtist %q: empty response", id)
	}
	return mapArtist(resp.Artist.artistDTO), nil
}

func (a *Adapter) GetAlbum(ctx context.Context, id string) (core.Album, error) {
	params := url.Values{}
	params.Set("id", id)
	var resp subsonicResponse
	if err := a.client.GetJSON(ctx, "getAlbum", params, &resp); err != nil {
		return core.Album{}, err
	}
	if resp.Album == nil {
		return core.Album{}, fmt.Errorf("subsonic getAlbum %q: empty response", id)
	}
	return mapAlbum(resp.Album.albumDTO), nil
}

func (a *Adapter) GetPlaylists(ctx context.Context) ([]core.Playlist, error) {
	var resp subsonicResponse
	if err := a.client.GetJSON(ctx, "getPlaylists", nil, &resp); err != nil {
		return nil, err
	}
	out := []core.Playlist{}
	if resp.Playlists != nil {
		for _, p := range resp.Playlists.Playlist {
			out = append(out, mapPlaylist(p))
		}
	}
	return out, nil
}

func (a *Adapter) Stream(ctx context.Context, trackID string, opts core.StreamOpts, rangeHeader string) (core.StreamHandle, error) {
	params := url.Values{}
	params.Set("id", trackID)
	if opts.MaxBitRate > 0 {
		params.Set("maxBitRate", strconv.Itoa(opts.MaxBitRate))
	}
	if opts.Format != "" {
		params.Set("format", opts.Format)
	}
	resp, err := a.client.RawGet(ctx, "stream", params, rangeHeader)
	if err != nil {
		return core.StreamHandle{}, err
	}
	return core.StreamHandle{
		Body:          resp.Body,
		ContentType:   resp.Header.Get("Content-Type"),
		ContentLength: resp.ContentLength,
		AcceptRanges:  resp.Header.Get("Accept-Ranges"),
		ContentRange:  resp.Header.Get("Content-Range"),
		StatusCode:    resp.StatusCode,
	}, nil
}

func (a *Adapter) CoverArt(ctx context.Context, id string, size int) (core.CoverArt, error) {
	params := url.Values{}
	params.Set("id", id)
	if size > 0 {
		params.Set("size", strconv.Itoa(size))
	}
	resp, err := a.client.RawGet(ctx, "getCoverArt", params, "")
	if err != nil {
		return core.CoverArt{}, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return core.CoverArt{}, fmt.Errorf("subsonic getCoverArt %q: HTTP %d", id, resp.StatusCode)
	}
	return core.CoverArt{Body: resp.Body, ContentType: resp.Header.Get("Content-Type")}, nil
}

func (a *Adapter) StartScan(ctx context.Context) error {
	return a.client.GetJSON(ctx, "startScan", nil, nil)
}

func (a *Adapter) ScanStatus(ctx context.Context) (core.ScanStatus, error) {
	var resp subsonicResponse
	if err := a.client.GetJSON(ctx, "getScanStatus", nil, &resp); err != nil {
		return core.ScanStatus{}, err
	}
	if resp.ScanStatus == nil {
		return core.ScanStatus{}, nil
	}
	return core.ScanStatus{Scanning: resp.ScanStatus.Scanning, Count: resp.ScanStatus.Count}, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/library/subsonic/ -v`
Expected: PASS (all adapter tests + `TestSubsonicConformance` subtests + the Task-3 client tests).

- [ ] **Step 6: Commit**

```bash
git add internal/library/subsonic/adapter.go internal/library/subsonic/adapter_test.go internal/library/subsonic/testdata
git commit -m "feat(subsonic): adapter mapping Subsonic to core types, passing conformance"
```

---

## Task 5: API library handlers + stream/cover proxy + Deps wiring

**Files:**
- Create: `internal/api/library.go`, `internal/api/stream.go`
- Modify: `internal/api/server.go` (add `Library` to `Deps`, mount routes), `internal/api/handlers.go` (update `handleAdaptersAvailable` registry loop), `internal/api/auth_flow_test.go` (update `testServer` Deps literal)
- Test: `internal/api/library_test.go`, `internal/api/stream_test.go`

**Interfaces:**
- Consumes: `library.LibraryAdapter`, `core`.
- Produces (extends `Deps`):
  ```go
  type Deps struct {
      Auth       *auth.Service
      Library    library.LibraryAdapter   // NEW (may be nil if no library configured)
      Search     *registry.Registry
      Downloader *registry.Registry
      Dev        bool
  }
  ```
- Endpoints (all behind `requireAuth`):
  - `GET /api/v1/library/search?q=&type=` → `core.SearchResults`
  - `GET /api/v1/library/artists` → `[]core.Artist` (top-level browse via getArtists; reuses Search w/ empty handled below — see note)
  - `GET /api/v1/library/artist/{id}` → `core.Artist`
  - `GET /api/v1/library/album/{id}` → `core.Album`
  - `GET /api/v1/library/playlists` → `[]core.Playlist`
  - `GET /api/v1/library/albums?type=newest&size=` → `[]core.Album` (browse via getAlbumList2)
  - `GET /api/v1/stream/{id}` → Range-forwarding audio proxy
  - `GET /api/v1/cover/{id}?size=` → image proxy
  > NOTE on `GET /library/artists` and `GET /library/albums`: these browse endpoints need `GetArtists` / `GetAlbumList2`, which are NOT on the `LibraryAdapter` interface. To stay interface-clean for M1, we expose them via an OPTIONAL interface check on the adapter: `type artistBrowser interface { GetArtistsBrowse(ctx) ([]core.Artist, error) }` and `type albumBrowser interface { GetAlbumsBrowse(ctx, listType string, size int) ([]core.Album, error) }`. The subsonic `Adapter` implements both (added in this task). If the active adapter doesn't implement them, the handler returns `200 []`. This keeps the core interface minimal while enabling browse.

- [ ] **Step 1: Add browse methods to the subsonic adapter + fixture route**

Append to `internal/library/subsonic/adapter.go`:
```go
// GetArtistsBrowse returns the full artist list (Subsonic getArtists), flattened
// across index buckets. Used by the /library/artists browse endpoint.
func (a *Adapter) GetArtistsBrowse(ctx context.Context) ([]core.Artist, error) {
	var resp subsonicResponse
	if err := a.client.GetJSON(ctx, "getArtists", nil, &resp); err != nil {
		return nil, err
	}
	out := []core.Artist{}
	if resp.Artists != nil {
		for _, idx := range resp.Artists.Index {
			for _, ar := range idx.Artist {
				out = append(out, mapArtist(ar))
			}
		}
	}
	return out, nil
}

// GetAlbumsBrowse returns albums via Subsonic getAlbumList2 (listType e.g.
// "newest", "frequent", "recent", "alphabeticalByName"). size defaults to 50.
func (a *Adapter) GetAlbumsBrowse(ctx context.Context, listType string, size int) ([]core.Album, error) {
	if listType == "" {
		listType = "newest"
	}
	if size <= 0 {
		size = 50
	}
	params := url.Values{}
	params.Set("type", listType)
	params.Set("size", strconv.Itoa(size))
	var resp subsonicResponse
	if err := a.client.GetJSON(ctx, "getAlbumList2", params, &resp); err != nil {
		return nil, err
	}
	out := []core.Album{}
	if resp.AlbumList2 != nil {
		for _, al := range resp.AlbumList2.Album {
			out = append(out, mapAlbum(al))
		}
	}
	return out, nil
}
```

Add browse coverage to the subsonic adapter test. Append to `internal/library/subsonic/adapter_test.go`:
```go
func TestGetArtistsBrowse(t *testing.T) {
	a := newTestAdapter(t)
	arts, err := a.GetArtistsBrowse(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 2 {
		t.Fatalf("artists: %+v", arts)
	}
}

func TestGetAlbumsBrowse(t *testing.T) {
	a := newTestAdapter(t)
	albs, err := a.GetAlbumsBrowse(context.Background(), "newest", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(albs) != 2 {
		t.Fatalf("albums: %+v", albs)
	}
}
```

Run: `go test ./internal/library/subsonic/ -run 'Browse' -v`
Expected: PASS (after adapter methods exist).

- [ ] **Step 2: Write the failing handler tests**

Create `internal/api/library_test.go`:
```go
package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/maximusjb/reverb/internal/auth"
	"github.com/maximusjb/reverb/internal/core"
	"github.com/maximusjb/reverb/internal/registry"
	"github.com/maximusjb/reverb/internal/store"
)

// fakeLibrary implements library.LibraryAdapter (+ browse interfaces) for tests.
type fakeLibrary struct{ lastRange string }

func (fakeLibrary) Type() string                             { return "library" }
func (fakeLibrary) Name() string                             { return "fake" }
func (fakeLibrary) ConfigSchema() registry.ConfigSchema      { return registry.ConfigSchema{} }
func (fakeLibrary) Init(cfg map[string]any) error            { return nil }
func (fakeLibrary) TestConnection(ctx context.Context) error { return nil }
func (fakeLibrary) Search(ctx context.Context, q string, types []core.EntityType) (core.SearchResults, error) {
	return core.SearchResults{Tracks: []core.Track{{ID: "t1", Title: "Song " + q}}}, nil
}
func (fakeLibrary) GetArtist(ctx context.Context, id string) (core.Artist, error) {
	return core.Artist{ID: id, Name: "Artist"}, nil
}
func (fakeLibrary) GetAlbum(ctx context.Context, id string) (core.Album, error) {
	return core.Album{ID: id, Name: "Album"}, nil
}
func (fakeLibrary) GetPlaylists(ctx context.Context) ([]core.Playlist, error) {
	return []core.Playlist{{ID: "p1", Name: "Mix"}}, nil
}
func (f *fakeLibrary) Stream(ctx context.Context, trackID string, opts core.StreamOpts, rangeHeader string) (core.StreamHandle, error) {
	f.lastRange = rangeHeader
	status := http.StatusOK
	cr := ""
	if rangeHeader != "" {
		status = http.StatusPartialContent
		cr = "bytes 0-3/100"
	}
	return core.StreamHandle{
		Body:          io.NopCloser(strings.NewReader("abcd")),
		ContentType:   "audio/mpeg",
		ContentLength: 4,
		AcceptRanges:  "bytes",
		ContentRange:  cr,
		StatusCode:    status,
	}, nil
}
func (fakeLibrary) CoverArt(ctx context.Context, id string, size int) (core.CoverArt, error) {
	return core.CoverArt{Body: io.NopCloser(strings.NewReader("img")), ContentType: "image/jpeg"}, nil
}
func (fakeLibrary) StartScan(ctx context.Context) error { return nil }
func (fakeLibrary) ScanStatus(ctx context.Context) (core.ScanStatus, error) {
	return core.ScanStatus{}, nil
}
func (fakeLibrary) GetArtistsBrowse(ctx context.Context) ([]core.Artist, error) {
	return []core.Artist{{ID: "ar1", Name: "Artist"}}, nil
}
func (fakeLibrary) GetAlbumsBrowse(ctx context.Context, listType string, size int) ([]core.Album, error) {
	return []core.Album{{ID: "al1", Name: "Album"}}, nil
}

func libTestServer(t *testing.T, lib *fakeLibrary) (*Server, *http.Cookie) {
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
		Auth:       authSvc,
		Library:    lib,
		Search:     registry.NewRegistry("search"),
		Downloader: registry.NewRegistry("downloader"),
	})
	return srv, &http.Cookie{Name: sessionCookie, Value: tok}
}

func doAuthed(t *testing.T, srv *Server, method, target string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestLibrarySearchHandler(t *testing.T) {
	srv, cookie := libTestServer(t, &fakeLibrary{})
	rec := doAuthed(t, srv, http.MethodGet, "/api/v1/library/search?q=hello", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var res core.SearchResults
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Tracks) != 1 || res.Tracks[0].Title != "Song hello" {
		t.Fatalf("results: %+v", res)
	}
}

func TestLibrarySearchRequiresAuth(t *testing.T) {
	srv, _ := libTestServer(t, &fakeLibrary{})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/library/search?q=x", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestLibraryArtistAlbumPlaylistsHandlers(t *testing.T) {
	srv, cookie := libTestServer(t, &fakeLibrary{})
	for _, tc := range []struct {
		path string
		want string
	}{
		{"/api/v1/library/artist/ar1", "Artist"},
		{"/api/v1/library/album/al1", "Album"},
		{"/api/v1/library/artists", "Artist"},
		{"/api/v1/library/albums?type=newest", "Album"},
		{"/api/v1/library/playlists", "Mix"},
	} {
		rec := doAuthed(t, srv, http.MethodGet, tc.path, cookie)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d: %s", tc.path, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), tc.want) {
			t.Fatalf("%s body missing %q: %s", tc.path, tc.want, rec.Body.String())
		}
	}
}

func TestLibraryNilAdapterReturns503(t *testing.T) {
	st, _ := store.Open(t.TempDir() + "/n.db")
	t.Cleanup(func() { st.Close() })
	_ = st.Migrate()
	authSvc := auth.NewService(st.Q(), time.Now)
	_ = authSvc.SetAdminPassword(context.Background(), "pw")
	tok, _ := authSvc.CreateSession(context.Background())
	srv := NewServer(Deps{Auth: authSvc, Library: nil,
		Search: registry.NewRegistry("search"), Downloader: registry.NewRegistry("downloader")})
	rec := doAuthed(t, srv, http.MethodGet, "/api/v1/library/search?q=x", &http.Cookie{Name: sessionCookie, Value: tok})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
```

Create `internal/api/stream_test.go`:
```go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStreamProxyForwardsRangeAnd206(t *testing.T) {
	lib := &fakeLibrary{}
	srv, cookie := libTestServer(t, lib)

	rec := doAuthed(t, srv, http.MethodGet, "/api/v1/stream/t1", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("no-range status = %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "audio/mpeg" {
		t.Fatalf("content-type = %q", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("accept-ranges = %q", rec.Header().Get("Accept-Ranges"))
	}

	// With Range → 206 + Content-Range passthrough; range forwarded to adapter.
	r2rec := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodGet, "/api/v1/stream/t1", nil)
	r2.AddCookie(cookie)
	r2.Header.Set("Range", "bytes=0-3")
	srv.Handler().ServeHTTP(r2rec, r2)
	if r2rec.Code != http.StatusPartialContent {
		t.Fatalf("range status = %d, want 206", r2rec.Code)
	}
	if r2rec.Header().Get("Content-Range") == "" {
		t.Fatal("missing Content-Range passthrough")
	}
	if lib.lastRange != "bytes=0-3" {
		t.Fatalf("range not forwarded to adapter: %q", lib.lastRange)
	}
}

func TestCoverProxy(t *testing.T) {
	srv, cookie := libTestServer(t, &fakeLibrary{})
	rec := doAuthed(t, srv, http.MethodGet, "/api/v1/cover/al-1?size=300", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("content-type = %q", rec.Header().Get("Content-Type"))
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/api/ -run 'Library|Stream|Cover' -v`
Expected: FAIL — `Deps` has no field `Library` / undefined handlers.

- [ ] **Step 4: Extend Deps + mount routes**

Edit `internal/api/server.go`. Add the import `"github.com/maximusjb/reverb/internal/library"` to the import block, add the `Library` field to `Deps`, and mount the routes. Replace the `Deps` struct:
```go
type Deps struct {
	Auth       *auth.Service
	Library    library.LibraryAdapter
	Search     *registry.Registry
	Downloader *registry.Registry
	Dev        bool
}
```
Inside the protected `r.Group(func(pr chi.Router){...})` block (after the existing `pr.Get("/me", ...)` and `pr.Get("/adapters/available", ...)` lines), add:
```go
			pr.Get("/library/search", s.handleLibrarySearch)
			pr.Get("/library/artists", s.handleLibraryArtists)
			pr.Get("/library/artist/{id}", s.handleLibraryArtist)
			pr.Get("/library/album/{id}", s.handleLibraryAlbum)
			pr.Get("/library/albums", s.handleLibraryAlbums)
			pr.Get("/library/playlists", s.handleLibraryPlaylists)
			pr.Get("/stream/{id}", s.handleStream)
			pr.Get("/cover/{id}", s.handleCover)
```

- [ ] **Step 4a: Update `handleAdaptersAvailable` in `internal/api/handlers.go`**

The library adapter is now the active-instance type (`library.LibraryAdapter`), not a `*registry.Registry`, so remove it from the registry loop. Change the loop line to:
```go
	for _, reg := range []*registry.Registry{s.deps.Search, s.deps.Downloader} {
```
(Library adapters are surfaced via the composition-root registry in M4's settings work; for M1 the active library adapter is not listed in `/adapters/available`.)

- [ ] **Step 4b: Update the `testServer` helper in `internal/api/auth_flow_test.go`**

Remove the `Library: registry.NewRegistry("library"),` line from the `Deps{...}` literal (leave `Library` nil — those auth tests don't touch library routes). Keep `Search` and `Downloader`, so the `registry` import stays used. Resulting literal:
```go
	return NewServer(Deps{
		Auth:       auth.NewService(st.Q(), time.Now),
		Search:     registry.NewRegistry("search"),
		Downloader: registry.NewRegistry("downloader"),
	})
```

- [ ] **Step 5: Write the library handlers**

Create `internal/api/library.go`:
```go
package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/maximusjb/reverb/internal/core"
)

// optional browse interfaces (implemented by the subsonic adapter).
type artistBrowser interface {
	GetArtistsBrowse(ctx context.Context) ([]core.Artist, error)
}
type albumBrowser interface {
	GetAlbumsBrowse(ctx context.Context, listType string, size int) ([]core.Album, error)
}

// libraryReady writes 503 and returns false if no library adapter is configured.
func (s *Server) libraryReady(w http.ResponseWriter) bool {
	if s.deps.Library == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no library configured"})
		return false
	}
	return true
}

func (s *Server) handleLibrarySearch(w http.ResponseWriter, r *http.Request) {
	if !s.libraryReady(w) {
		return
	}
	q := r.URL.Query().Get("q")
	var types []core.EntityType
	switch r.URL.Query().Get("type") {
	case "track":
		types = []core.EntityType{core.EntityTrack}
	case "album":
		types = []core.EntityType{core.EntityAlbum}
	case "artist":
		types = []core.EntityType{core.EntityArtist}
	default:
		types = []core.EntityType{core.EntityTrack, core.EntityAlbum, core.EntityArtist}
	}
	res, err := s.deps.Library.Search(r.Context(), q, types)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleLibraryArtist(w http.ResponseWriter, r *http.Request) {
	if !s.libraryReady(w) {
		return
	}
	ar, err := s.deps.Library.GetArtist(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, ar)
}

func (s *Server) handleLibraryAlbum(w http.ResponseWriter, r *http.Request) {
	if !s.libraryReady(w) {
		return
	}
	al, err := s.deps.Library.GetAlbum(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, al)
}

func (s *Server) handleLibraryPlaylists(w http.ResponseWriter, r *http.Request) {
	if !s.libraryReady(w) {
		return
	}
	pls, err := s.deps.Library.GetPlaylists(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, pls)
}

func (s *Server) handleLibraryArtists(w http.ResponseWriter, r *http.Request) {
	if !s.libraryReady(w) {
		return
	}
	br, ok := s.deps.Library.(artistBrowser)
	if !ok {
		writeJSON(w, http.StatusOK, []core.Artist{})
		return
	}
	arts, err := br.GetArtistsBrowse(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, arts)
}

func (s *Server) handleLibraryAlbums(w http.ResponseWriter, r *http.Request) {
	if !s.libraryReady(w) {
		return
	}
	br, ok := s.deps.Library.(albumBrowser)
	if !ok {
		writeJSON(w, http.StatusOK, []core.Album{})
		return
	}
	listType := r.URL.Query().Get("type")
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	albs, err := br.GetAlbumsBrowse(r.Context(), listType, size)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, albs)
}
```

- [ ] **Step 6: Write the stream + cover proxy**

Create `internal/api/stream.go`:
```go
package api

import (
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/maximusjb/reverb/internal/core"
)

// handleStream proxies an audio stream from the library adapter, forwarding the
// inbound Range header upstream and copying back the status, Content-Type,
// Content-Length, Accept-Ranges, and Content-Range. Subsonic credentials never
// reach the browser.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	if !s.libraryReady(w) {
		return
	}
	id := chi.URLParam(r, "id")
	handle, err := s.deps.Library.Stream(r.Context(), id, core.StreamOpts{}, r.Header.Get("Range"))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	defer handle.Body.Close()

	h := w.Header()
	if handle.ContentType != "" {
		h.Set("Content-Type", handle.ContentType)
	}
	if handle.AcceptRanges != "" {
		h.Set("Accept-Ranges", handle.AcceptRanges)
	}
	if handle.ContentRange != "" {
		h.Set("Content-Range", handle.ContentRange)
	}
	if handle.ContentLength > 0 {
		h.Set("Content-Length", strconv.FormatInt(handle.ContentLength, 10))
	}
	status := handle.StatusCode
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = io.Copy(w, handle.Body)
}

// handleCover proxies cover art from the library adapter.
func (s *Server) handleCover(w http.ResponseWriter, r *http.Request) {
	if !s.libraryReady(w) {
		return
	}
	id := chi.URLParam(r, "id")
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	cover, err := s.deps.Library.CoverArt(r.Context(), id, size)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	defer cover.Body.Close()
	if cover.ContentType != "" {
		w.Header().Set("Content-Type", cover.ContentType)
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, cover.Body)
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/api/ -v`
Expected: PASS (existing M0 tests + all new Library/Stream/Cover tests).

- [ ] **Step 8: Commit**

```bash
git add internal/api/library.go internal/api/stream.go internal/api/server.go internal/api/library_test.go internal/api/stream_test.go internal/library/subsonic/adapter.go internal/library/subsonic/adapter_test.go
git commit -m "feat(api): library REST handlers and Range-forwarding stream/cover proxy"
```

---

## Task 6: Composition root — explicit registration + build active adapter from config

**Files:**
- Modify: `cmd/reverb/main.go`
- Create: `cmd/reverb/library_wiring.go`, `cmd/reverb/library_wiring_test.go`

**Interfaces:**
- Consumes: `store.Store` (`ListAdapterInstances`), `registry.Registry`, `subsonic.New`, env (`REVERB_LIBRARY_PASSWORD`), `library.LibraryAdapter`.
- Produces:
  ```go
  // buildLibraryAdapter finds the enabled library adapter_instance, creates it
  // from the registry, applies env secret overrides, calls Init, and returns it.
  // Returns (nil, nil) when no enabled library instance exists (library optional).
  func buildLibraryAdapter(ctx context.Context, reg *registry.Registry, instances []db.AdapterInstance, getenv func(string) string) (library.LibraryAdapter, error)
  ```

- [ ] **Step 1: Write the failing wiring test**

Create `cmd/reverb/library_wiring_test.go`:
```go
package main

import (
	"context"
	"testing"

	"github.com/maximusjb/reverb/internal/library"
	"github.com/maximusjb/reverb/internal/registry"
	"github.com/maximusjb/reverb/internal/store/db"
)

// stubLib captures the config passed to Init so we can assert env override + parse.
type stubLib struct {
	got map[string]any
	library.LibraryAdapter
}

func (s *stubLib) Type() string                             { return "library" }
func (s *stubLib) Name() string                             { return "subsonic" }
func (s *stubLib) ConfigSchema() registry.ConfigSchema      { return registry.ConfigSchema{} }
func (s *stubLib) Init(cfg map[string]any) error            { s.got = cfg; return nil }
func (s *stubLib) TestConnection(ctx context.Context) error { return nil }

func TestBuildLibraryAdapterAppliesEnvSecret(t *testing.T) {
	reg := registry.NewRegistry("library")
	captured := &stubLib{}
	reg.Register("subsonic", func() registry.Plugin { return captured })

	instances := []db.AdapterInstance{{
		ID: "i1", Type: "library", Name: "subsonic", Enabled: 1, Priority: 0,
		ConfigJson: `{"url":"http://nav:4533","username":"alice","password":"file-pw"}`,
	}}
	env := map[string]string{"REVERB_LIBRARY_PASSWORD": "env-pw"}

	got, err := buildLibraryAdapter(context.Background(), reg, instances, func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected an adapter")
	}
	if captured.got["password"] != "env-pw" {
		t.Fatalf("env override not applied: %v", captured.got["password"])
	}
	if captured.got["url"] != "http://nav:4533" {
		t.Fatalf("url not parsed: %v", captured.got["url"])
	}
}

func TestBuildLibraryAdapterNoEnabledInstance(t *testing.T) {
	reg := registry.NewRegistry("library")
	reg.Register("subsonic", func() registry.Plugin { return &stubLib{} })
	instances := []db.AdapterInstance{{ID: "i1", Type: "library", Name: "subsonic", Enabled: 0}}
	got, err := buildLibraryAdapter(context.Background(), reg, instances, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatal("expected nil when no enabled library instance")
	}
}

func TestBuildLibraryAdapterIgnoresNonLibraryTypes(t *testing.T) {
	reg := registry.NewRegistry("library")
	reg.Register("subsonic", func() registry.Plugin { return &stubLib{} })
	instances := []db.AdapterInstance{{ID: "i1", Type: "search", Name: "spotify", Enabled: 1}}
	got, _ := buildLibraryAdapter(context.Background(), reg, instances, func(string) string { return "" })
	if got != nil {
		t.Fatal("expected nil — only library type counts")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/reverb/ -v`
Expected: FAIL — `undefined: buildLibraryAdapter`.

- [ ] **Step 3: Write the wiring helper**

Create `cmd/reverb/library_wiring.go`:
```go
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/maximusjb/reverb/internal/library"
	"github.com/maximusjb/reverb/internal/registry"
	"github.com/maximusjb/reverb/internal/store/db"
)

// buildLibraryAdapter builds the active LibraryAdapter from the first enabled
// adapter_instance of type "library". It applies env secret overrides
// (REVERB_LIBRARY_PASSWORD) onto the stored config_json before Init. The library
// is optional: with no enabled library instance it returns (nil, nil).
func buildLibraryAdapter(
	ctx context.Context,
	reg *registry.Registry,
	instances []db.AdapterInstance,
	getenv func(string) string,
) (library.LibraryAdapter, error) {
	var inst *db.AdapterInstance
	for i := range instances {
		if instances[i].Type == "library" && instances[i].Enabled == 1 {
			inst = &instances[i]
			break
		}
	}
	if inst == nil {
		return nil, nil
	}

	plugin, err := reg.Create(inst.Name)
	if err != nil {
		return nil, fmt.Errorf("library adapter %q: %w", inst.Name, err)
	}
	lib, ok := plugin.(library.LibraryAdapter)
	if !ok {
		return nil, fmt.Errorf("adapter %q is not a LibraryAdapter", inst.Name)
	}

	cfg := map[string]any{}
	if inst.ConfigJson != "" {
		if err := json.Unmarshal([]byte(inst.ConfigJson), &cfg); err != nil {
			return nil, fmt.Errorf("library adapter %q config: %w", inst.Name, err)
		}
	}
	// Env secret override — env wins for the password just before Init().
	if pw := getenv("REVERB_LIBRARY_PASSWORD"); pw != "" {
		cfg["password"] = pw
	}

	if err := lib.Init(cfg); err != nil {
		return nil, fmt.Errorf("library adapter %q init: %w", inst.Name, err)
	}
	return lib, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/reverb/ -v`
Expected: PASS.

- [ ] **Step 5: Wire into main**

Edit `cmd/reverb/main.go`. Add imports `"github.com/maximusjb/reverb/internal/library/subsonic"` and `"github.com/maximusjb/reverb/internal/library"` (library only if referenced; the var below uses it). After the `authSvc` block and before `srv := api.NewServer(...)`, insert:
```go
	// Registries (explicit registration at the composition root — no init() side-effects).
	libraryReg := registry.NewRegistry("library")
	libraryReg.Register("subsonic", func() registry.Plugin { return subsonic.New() })
	searchReg := registry.NewRegistry("search")
	downloaderReg := registry.NewRegistry("downloader")

	// Build the active library adapter from the enabled adapter_instance row.
	instances, err := st.Q().ListAdapterInstances(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	libAdapter, err := buildLibraryAdapter(context.Background(), libraryReg, instances, os.Getenv)
	if err != nil {
		log.Printf("WARNING: library adapter not available: %v", err)
	}
	if libAdapter != nil {
		log.Printf("library adapter active: %s", libAdapter.Name())
	} else {
		log.Printf("no library adapter configured (add one via settings)")
	}
```
Then replace the `srv := api.NewServer(...)` call with:
```go
	srv := api.NewServer(api.Deps{
		Auth:       authSvc,
		Library:    libAdapter,
		Search:     searchReg,
		Downloader: downloaderReg,
		Dev:        cfg.Dev,
	})
```
Remove the previous inline `registry.NewRegistry(...)` calls inside the old `api.NewServer` literal (they are replaced by `libraryReg`/`searchReg`/`downloaderReg`). The `library` import is used by `buildLibraryAdapter`'s signature in the same package, so `main.go` itself only needs `subsonic` and `registry` (already imported). Do not add an unused `library` import to main.go.

> After editing, run `go build ./cmd/...` to confirm imports resolve: keep the `registry` import (still used by the `NewRegistry`/`Register` calls) and do NOT add an unused `library` import unless `buildLibraryAdapter` references the `library` package directly. `context` and `os` are already imported.

- [ ] **Step 6: Build + full backend test**

Run: `go build ./cmd/... ./internal/... && go test ./cmd/... ./internal/...`
Expected: build OK, all PASS.

- [ ] **Step 7: Commit**

```bash
go mod tidy
git add cmd/reverb go.mod go.sum
git commit -m "feat(cmd): explicit subsonic registration and active adapter wiring from config"
```

---

## Task 7: Frontend types + library API + QueryClient

**Files:**
- Create: `web/src/lib/types.ts`, `web/src/lib/libraryApi.ts`
- Modify: `web/src/App.tsx` (wrap in `QueryClientProvider`)
- Test: `web/src/lib/libraryApi.test.tsx`

**Interfaces:**
- Produces TS types mirroring `core` (camelCase to match JSON tags), plus:
  ```ts
  export function streamUrl(id: string): string          // '/api/v1/stream/' + id
  export function coverUrl(id: string, size?: number): string
  export function useLibrarySearch(q: string)            // TanStack Query, enabled when q.length>0
  export function useArtist(id: string)
  export function useAlbum(id: string)
  export function useArtists()                            // browse
  export function useAlbums(type?: string)               // browse
  ```

- [ ] **Step 1: Write the types**

Create `web/src/lib/types.ts`:
```ts
export interface Track {
  id: string
  title: string
  albumId: string
  album: string
  artistId: string
  artist: string
  coverArtId: string
  trackNumber: number
  discNumber: number
  durationMs: number
  bitRate: number
  suffix: string
  contentType: string
  isrc?: string
}

export interface Album {
  id: string
  name: string
  artistId: string
  artist: string
  coverArtId: string
  year: number
  songCount: number
  durationMs: number
  tracks?: Track[]
}

export interface Artist {
  id: string
  name: string
  coverArtId: string
  albumCount: number
  albums?: Album[]
}

export interface Playlist {
  id: string
  name: string
  coverArtId: string
  songCount: number
  durationMs: number
  tracks?: Track[]
}

export interface SearchResults {
  tracks: Track[]
  albums: Album[]
  artists: Artist[]
}

export function formatDuration(ms: number): string {
  const total = Math.floor(ms / 1000)
  const m = Math.floor(total / 60)
  const s = total % 60
  return `${m}:${s.toString().padStart(2, '0')}`
}
```

- [ ] **Step 2: Write the library API + hooks**

Create `web/src/lib/libraryApi.ts`:
```ts
import { useQuery } from '@tanstack/react-query'
import { api } from './api'
import type { Album, Artist, Playlist, SearchResults } from './types'

export function streamUrl(id: string): string {
  return `/api/v1/stream/${encodeURIComponent(id)}`
}

export function coverUrl(id: string, size = 300): string {
  if (!id) return ''
  return `/api/v1/cover/${encodeURIComponent(id)}?size=${size}`
}

export function useLibrarySearch(q: string) {
  return useQuery({
    queryKey: ['library', 'search', q],
    queryFn: () => api.get<SearchResults>(`/library/search?q=${encodeURIComponent(q)}`),
    enabled: q.trim().length > 0,
  })
}

export function useArtist(id: string) {
  return useQuery({
    queryKey: ['library', 'artist', id],
    queryFn: () => api.get<Artist>(`/library/artist/${encodeURIComponent(id)}`),
    enabled: !!id,
  })
}

export function useAlbum(id: string) {
  return useQuery({
    queryKey: ['library', 'album', id],
    queryFn: () => api.get<Album>(`/library/album/${encodeURIComponent(id)}`),
    enabled: !!id,
  })
}

export function useArtists() {
  return useQuery({
    queryKey: ['library', 'artists'],
    queryFn: () => api.get<Artist[]>('/library/artists'),
  })
}

export function useAlbums(type = 'newest') {
  return useQuery({
    queryKey: ['library', 'albums', type],
    queryFn: () => api.get<Album[]>(`/library/albums?type=${encodeURIComponent(type)}`),
  })
}

export function usePlaylists() {
  return useQuery({
    queryKey: ['library', 'playlists'],
    queryFn: () => api.get<Playlist[]>('/library/playlists'),
  })
}
```

- [ ] **Step 3: Wrap App in QueryClientProvider**

Replace `web/src/App.tsx`:
```tsx
import { Navigate, Route, Routes } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AppShell } from './components/AppShell'
import { useSessionStatus } from './lib/session'
import Search from './routes/Search'
import Library from './routes/Library'
import Settings from './routes/Settings'
import Login from './routes/Login'
import Setup from './routes/Setup'
import Album from './routes/Album'
import Artist from './routes/Artist'

const queryClient = new QueryClient({
  defaultOptions: { queries: { staleTime: 30_000, retry: 1 } },
})

function Routed() {
  const s = useSessionStatus()
  if (s.loading) return <div className="p-6 text-neutral-500">Loading…</div>
  if (s.setupRequired) return <Setup />
  if (!s.authenticated) return <Login />
  return (
    <Routes>
      <Route element={<AppShell />}>
        <Route path="/search" element={<Search />} />
        <Route path="/library" element={<Library />} />
        <Route path="/album/:id" element={<Album />} />
        <Route path="/artist/:id" element={<Artist />} />
        <Route path="/settings" element={<Settings />} />
        <Route path="*" element={<Navigate to="/search" replace />} />
      </Route>
    </Routes>
  )
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <Routed />
    </QueryClientProvider>
  )
}
```

> NOTE: `Album.tsx`, `Artist.tsx` are created in Task 12. To keep this task independently green, create minimal placeholder files now and replace them in Task 12. Create `web/src/routes/Album.tsx`:
> ```tsx
> export default function Album() {
>   return <h1 className="text-2xl font-bold">Album</h1>
> }
> ```
> Create `web/src/routes/Artist.tsx`:
> ```tsx
> export default function Artist() {
>   return <h1 className="text-2xl font-bold">Artist</h1>
> }
> ```

- [ ] **Step 4: Write the failing API test**

Create `web/src/lib/libraryApi.test.tsx`:
```tsx
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { coverUrl, streamUrl, useLibrarySearch } from './libraryApi'

describe('url builders', () => {
  it('builds stream and cover urls', () => {
    expect(streamUrl('t 1')).toBe('/api/v1/stream/t%201')
    expect(coverUrl('al-1', 200)).toBe('/api/v1/cover/al-1?size=200')
    expect(coverUrl('')).toBe('')
  })
})

describe('useLibrarySearch', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        new Response(JSON.stringify({ tracks: [{ id: 't1', title: 'Song' }], albums: [], artists: [] }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    )
  })
  afterEach(() => vi.unstubAllGlobals())

  it('fetches when query is non-empty', async () => {
    const qc = new QueryClient()
    const wrapper = ({ children }: { children: React.ReactNode }) => (
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    )
    const { result } = renderHook(() => useLibrarySearch('hello'), { wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data?.tracks[0].title).toBe('Song')
  })
})
```

- [ ] **Step 5: Run frontend tests**

Run: `cd web && npm run test`
Expected: existing tests + `libraryApi.test.tsx` PASS.

- [ ] **Step 6: Typecheck + commit**

Run: `cd web && npm run build` (tsc + vite build; expect success).
```bash
git add web/src/lib/types.ts web/src/lib/libraryApi.ts web/src/App.tsx web/src/lib/libraryApi.test.tsx web/src/routes/Album.tsx web/src/routes/Artist.tsx
git commit -m "feat(web): library API hooks, query client, and shared domain types"
```

---

## Task 8: AudioEngine (dual-`<audio>`, outside React, testable logic)

**Files:**
- Create: `web/src/lib/audioEngine.ts`
- Test: `web/src/lib/audioEngine.test.ts`

**Interfaces:**
- Produces:
  ```ts
  export type RepeatMode = 'off' | 'all' | 'one'
  export interface AudioElement {                 // injectable abstraction over HTMLAudioElement
    src: string
    currentTime: number
    duration: number
    volume: number
    paused: boolean
    play(): Promise<void>
    pause(): void
    load(): void
    buffered: { length: number; end(i: number): number; start(i: number): number }
    addEventListener(type: string, cb: () => void): void
    removeEventListener(type: string, cb: () => void): void
  }
  export interface PlayerState {
    queue: Track[]
    index: number          // -1 when nothing loaded
    current: Track | null
    playing: boolean
    currentTimeMs: number
    durationMs: number
    bufferedMs: number
    volume: number
    shuffle: boolean
    repeat: RepeatMode
  }
  export class AudioEngine {
    constructor(factory?: () => AudioElement)     // default builds real HTMLAudioElement wrappers
    subscribe(cb: (s: PlayerState) => void): () => void
    getState(): PlayerState
    setQueue(tracks: Track[], startIndex?: number): void
    playTrackList(tracks: Track[], startIndex: number): void
    enqueue(track: Track): void
    removeAt(index: number): void
    moveItem(from: number, to: number): void
    play(): void
    pause(): void
    toggle(): void
    next(): void
    prev(): void
    seekMs(ms: number): void
    setVolume(v: number): void
    toggleShuffle(): void
    cycleRepeat(): void
  }
  ```
  - Engine uses a `srcResolver: (track: Track) => string` defaulting to `streamUrl(track.id)` — but to keep the unit test pure (no import side-effects), the engine takes an optional second constructor arg `resolveSrc`. Default resolver imports `streamUrl` lazily.

- [ ] **Step 1: Write the failing engine test (fake audio element, jsdom)**

Create `web/src/lib/audioEngine.test.ts`:
```ts
import { describe, expect, it, beforeEach } from 'vitest'
import { AudioEngine, type AudioElement } from './audioEngine'
import type { Track } from './types'

function track(id: string): Track {
  return {
    id,
    title: 'T' + id,
    albumId: 'al',
    album: 'Album',
    artistId: 'ar',
    artist: 'Artist',
    coverArtId: 'co',
    trackNumber: 1,
    discNumber: 1,
    durationMs: 1000,
    bitRate: 320,
    suffix: 'mp3',
    contentType: 'audio/mpeg',
  }
}

// fakeAudio is a minimal AudioElement stub: records play/pause, fires ended on demand.
class FakeAudio implements AudioElement {
  src = ''
  currentTime = 0
  duration = 0
  volume = 1
  paused = true
  private listeners: Record<string, Array<() => void>> = {}
  buffered = { length: 0, end: () => 0, start: () => 0 }
  async play() {
    this.paused = false
  }
  pause() {
    this.paused = true
  }
  load() {}
  addEventListener(type: string, cb: () => void) {
    ;(this.listeners[type] ||= []).push(cb)
  }
  removeEventListener(type: string, cb: () => void) {
    this.listeners[type] = (this.listeners[type] || []).filter((f) => f !== cb)
  }
  fire(type: string) {
    ;(this.listeners[type] || []).forEach((cb) => cb())
  }
}

function newEngine() {
  const audios: FakeAudio[] = []
  const engine = new AudioEngine(() => {
    const a = new FakeAudio()
    audios.push(a)
    return a
  }, (t) => `mock://${t.id}`)
  return { engine, audios }
}

const list = [track('1'), track('2'), track('3')]

describe('AudioEngine queue + transport', () => {
  let engine: AudioEngine
  let audios: FakeAudio[]
  beforeEach(() => {
    ;({ engine, audios } = newEngine())
  })

  it('plays a track list from an index', () => {
    engine.playTrackList(list, 1)
    const s = engine.getState()
    expect(s.index).toBe(1)
    expect(s.current?.id).toBe('2')
    expect(s.playing).toBe(true)
  })

  it('next advances and wraps only with repeat all', () => {
    engine.playTrackList(list, 2)
    engine.next() // at last track, repeat off → stops
    expect(engine.getState().playing).toBe(false)

    engine.cycleRepeat() // off -> all
    engine.playTrackList(list, 2)
    engine.next()
    expect(engine.getState().index).toBe(0) // wrapped
  })

  it('prev goes back, clamps at start', () => {
    engine.playTrackList(list, 1)
    engine.prev()
    expect(engine.getState().index).toBe(0)
    engine.prev()
    expect(engine.getState().index).toBe(0)
  })

  it('repeat one replays same index on track end', () => {
    engine.playTrackList(list, 0)
    engine.cycleRepeat() // off -> all
    engine.cycleRepeat() // all -> one
    expect(engine.getState().repeat).toBe('one')
    audios[0].fire('ended')
    expect(engine.getState().index).toBe(0)
    expect(engine.getState().playing).toBe(true)
  })

  it('ended advances to next track when repeat off', () => {
    engine.playTrackList(list, 0)
    audios[0].fire('ended')
    expect(engine.getState().index).toBe(1)
  })

  it('shuffle produces a permutation covering all tracks', () => {
    engine.playTrackList(list, 0)
    engine.toggleShuffle()
    const seen = new Set<string>()
    seen.add(engine.getState().current!.id)
    engine.next()
    seen.add(engine.getState().current!.id)
    engine.next()
    seen.add(engine.getState().current!.id)
    expect(seen.size).toBe(3) // all three visited, no repeats within a cycle
  })

  it('enqueue and removeAt mutate the queue', () => {
    engine.setQueue(list, 0)
    engine.enqueue(track('4'))
    expect(engine.getState().queue.length).toBe(4)
    engine.removeAt(3)
    expect(engine.getState().queue.length).toBe(3)
  })

  it('moveItem reorders and keeps current track index correct', () => {
    engine.playTrackList(list, 0) // current = '1'
    engine.moveItem(0, 2) // move current to the end
    const s = engine.getState()
    expect(s.current?.id).toBe('1')
    expect(s.index).toBe(2)
    expect(s.queue.map((t) => t.id)).toEqual(['2', '3', '1'])
  })

  it('setVolume clamps 0..1 and notifies subscribers', () => {
    let notified = 0
    engine.subscribe(() => notified++)
    engine.setVolume(2)
    expect(engine.getState().volume).toBe(1)
    engine.setVolume(-1)
    expect(engine.getState().volume).toBe(0)
    expect(notified).toBeGreaterThan(0)
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/lib/audioEngine.test.ts`
Expected: FAIL — cannot find module `./audioEngine`.

- [ ] **Step 3: Write the AudioEngine**

Create `web/src/lib/audioEngine.ts`:
```ts
import type { Track } from './types'
import { streamUrl } from './libraryApi'

export type RepeatMode = 'off' | 'all' | 'one'

export interface AudioElement {
  src: string
  currentTime: number
  duration: number
  volume: number
  paused: boolean
  play(): Promise<void>
  pause(): void
  load(): void
  buffered: { length: number; end(i: number): number; start(i: number): number }
  addEventListener(type: string, cb: () => void): void
  removeEventListener(type: string, cb: () => void): void
}

export interface PlayerState {
  queue: Track[]
  index: number
  current: Track | null
  playing: boolean
  currentTimeMs: number
  durationMs: number
  bufferedMs: number
  volume: number
  shuffle: boolean
  repeat: RepeatMode
}

function realAudioFactory(): AudioElement {
  return new Audio() as unknown as AudioElement
}

export class AudioEngine {
  private factory: () => AudioElement
  private resolveSrc: (t: Track) => string
  private active: AudioElement
  private preload: AudioElement
  private listeners = new Set<(s: PlayerState) => void>()

  private queue: Track[] = []
  private index = -1
  private playing = false
  private currentTimeMs = 0
  private durationMs = 0
  private bufferedMs = 0
  private volume = 1
  private shuffle = false
  private repeat: RepeatMode = 'off'

  // shuffle order: a permutation of queue indices; shufflePos points into it.
  private shuffleOrder: number[] = []
  private shufflePos = -1

  constructor(
    factory: () => AudioElement = realAudioFactory,
    resolveSrc: (t: Track) => string = (t) => streamUrl(t.id),
  ) {
    this.factory = factory
    this.resolveSrc = resolveSrc
    this.active = this.factory()
    this.preload = this.factory()
    this.bindActive()
  }

  private bindActive() {
    this.active.addEventListener('timeupdate', this.onTime)
    this.active.addEventListener('durationchange', this.onTime)
    this.active.addEventListener('progress', this.onTime)
    this.active.addEventListener('ended', this.onEnded)
    this.active.addEventListener('play', this.onPlayState)
    this.active.addEventListener('pause', this.onPlayState)
  }

  private unbindActive() {
    this.active.removeEventListener('timeupdate', this.onTime)
    this.active.removeEventListener('durationchange', this.onTime)
    this.active.removeEventListener('progress', this.onTime)
    this.active.removeEventListener('ended', this.onEnded)
    this.active.removeEventListener('play', this.onPlayState)
    this.active.removeEventListener('pause', this.onPlayState)
  }

  private onTime = () => {
    this.currentTimeMs = Math.round((this.active.currentTime || 0) * 1000)
    this.durationMs = Number.isFinite(this.active.duration) ? Math.round((this.active.duration || 0) * 1000) : this.durationMs
    const b = this.active.buffered
    if (b && b.length > 0) {
      this.bufferedMs = Math.round(b.end(b.length - 1) * 1000)
    }
    this.emit()
  }

  private onPlayState = () => {
    this.playing = !this.active.paused
    this.emit()
  }

  private onEnded = () => {
    if (this.repeat === 'one') {
      this.active.currentTime = 0
      void this.active.play()
      this.playing = true
      this.emit()
      return
    }
    this.advance(1, true)
  }

  subscribe(cb: (s: PlayerState) => void): () => void {
    this.listeners.add(cb)
    cb(this.getState())
    return () => this.listeners.delete(cb)
  }

  getState(): PlayerState {
    return {
      queue: this.queue,
      index: this.index,
      current: this.index >= 0 && this.index < this.queue.length ? this.queue[this.index] : null,
      playing: this.playing,
      currentTimeMs: this.currentTimeMs,
      durationMs: this.durationMs,
      bufferedMs: this.bufferedMs,
      volume: this.volume,
      shuffle: this.shuffle,
      repeat: this.repeat,
    }
  }

  private emit() {
    const s = this.getState()
    this.listeners.forEach((cb) => cb(s))
  }

  setQueue(tracks: Track[], startIndex = 0) {
    this.queue = tracks.slice()
    this.index = tracks.length ? Math.min(Math.max(startIndex, 0), tracks.length - 1) : -1
    this.rebuildShuffle()
    this.emit()
  }

  playTrackList(tracks: Track[], startIndex: number) {
    this.setQueue(tracks, startIndex)
    this.loadCurrent(true)
  }

  enqueue(track: Track) {
    this.queue = [...this.queue, track]
    if (this.index === -1) this.index = 0
    this.rebuildShuffle()
    this.emit()
  }

  removeAt(i: number) {
    if (i < 0 || i >= this.queue.length) return
    const wasCurrent = i === this.index
    this.queue = this.queue.filter((_, idx) => idx !== i)
    if (i < this.index) this.index--
    if (this.index >= this.queue.length) this.index = this.queue.length - 1
    this.rebuildShuffle()
    if (wasCurrent) this.loadCurrent(this.playing)
    this.emit()
  }

  moveItem(from: number, to: number) {
    if (from < 0 || from >= this.queue.length || to < 0 || to >= this.queue.length) return
    const currentId = this.index >= 0 ? this.queue[this.index]?.id : null
    const q = this.queue.slice()
    const [item] = q.splice(from, 1)
    q.splice(to, 0, item)
    this.queue = q
    if (currentId) {
      this.index = q.findIndex((t) => t.id === currentId)
    }
    this.rebuildShuffle()
    this.emit()
  }

  private loadCurrent(autoplay: boolean) {
    const t = this.getState().current
    if (!t) {
      this.playing = false
      this.emit()
      return
    }
    this.active.src = this.resolveSrc(t)
    this.active.load()
    this.currentTimeMs = 0
    if (autoplay) {
      void this.active.play()
      this.playing = true
    }
    this.preloadNext()
    this.emit()
  }

  private preloadNext() {
    const ni = this.peekNextIndex()
    if (ni < 0 || ni >= this.queue.length) return
    this.preload.src = this.resolveSrc(this.queue[ni])
    this.preload.load()
  }

  play() {
    if (this.index < 0 && this.queue.length) this.index = 0
    if (this.getState().current) {
      if (!this.active.src) this.loadCurrent(true)
      else {
        void this.active.play()
        this.playing = true
      }
    }
    this.emit()
  }

  pause() {
    this.active.pause()
    this.playing = false
    this.emit()
  }

  toggle() {
    if (this.playing) this.pause()
    else this.play()
  }

  private rebuildShuffle() {
    if (!this.shuffle) {
      this.shuffleOrder = []
      this.shufflePos = -1
      return
    }
    const idxs = this.queue.map((_, i) => i)
    // Fisher-Yates
    for (let i = idxs.length - 1; i > 0; i--) {
      const j = Math.floor(Math.random() * (i + 1))
      ;[idxs[i], idxs[j]] = [idxs[j], idxs[i]]
    }
    // ensure current track is first in the shuffle cycle
    if (this.index >= 0) {
      const at = idxs.indexOf(this.index)
      if (at > 0) [idxs[0], idxs[at]] = [idxs[at], idxs[0]]
    }
    this.shuffleOrder = idxs
    this.shufflePos = 0
  }

  private peekNextIndex(): number {
    if (this.queue.length === 0) return -1
    if (this.shuffle) {
      const np = this.shufflePos + 1
      if (np < this.shuffleOrder.length) return this.shuffleOrder[np]
      if (this.repeat === 'all') return this.shuffleOrder[0]
      return -1
    }
    const ni = this.index + 1
    if (ni < this.queue.length) return ni
    if (this.repeat === 'all') return 0
    return -1
  }

  private advance(dir: 1 | -1, fromEnded = false) {
    if (this.queue.length === 0) return
    if (this.shuffle) {
      let np = this.shufflePos + dir
      if (np >= this.shuffleOrder.length) {
        if (this.repeat === 'all') np = 0
        else {
          this.playing = false
          this.emit()
          return
        }
      }
      if (np < 0) np = 0
      this.shufflePos = np
      this.index = this.shuffleOrder[np]
      this.loadCurrent(this.playing || fromEnded)
      return
    }
    let ni = this.index + dir
    if (ni >= this.queue.length) {
      if (this.repeat === 'all') ni = 0
      else {
        this.playing = false
        this.emit()
        return
      }
    }
    if (ni < 0) ni = 0
    this.index = ni
    this.loadCurrent(this.playing || fromEnded)
  }

  next() {
    this.advance(1)
  }

  prev() {
    // restart current if >3s in, else go back
    if (this.currentTimeMs > 3000) {
      this.seekMs(0)
      return
    }
    this.advance(-1)
  }

  seekMs(ms: number) {
    this.active.currentTime = Math.max(0, ms / 1000)
    this.currentTimeMs = ms
    this.emit()
  }

  setVolume(v: number) {
    this.volume = Math.min(1, Math.max(0, v))
    this.active.volume = this.volume
    this.preload.volume = this.volume
    this.emit()
  }

  toggleShuffle() {
    this.shuffle = !this.shuffle
    this.rebuildShuffle()
    this.emit()
  }

  cycleRepeat() {
    this.repeat = this.repeat === 'off' ? 'all' : this.repeat === 'all' ? 'one' : 'off'
    this.emit()
  }
}
```

> NOTE: the default `resolveSrc` is `(t: Track) => streamUrl(t.id)` so production streams hit `/api/v1/stream/:id`. The test injects `resolveSrc = (t) => 'mock://'+t.id` to keep the unit test free of the real URL helper. The `preload` element is only loaded (not played); near-gapless polish (swapping `active`/`preload` on advance) is a safe follow-up — for M1 the preload `load()` warms the browser cache, which is the testable contract here.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run src/lib/audioEngine.test.ts`
Expected: PASS (all subtests).

- [ ] **Step 5: Typecheck + commit**

Run: `cd web && npm run build`
Expected: success.
```bash
git add web/src/lib/audioEngine.ts web/src/lib/audioEngine.test.ts
git commit -m "feat(web): framework-agnostic dual-audio engine with testable queue logic"
```

---

## Task 9: Zustand player store + UI panel store

**Files:**
- Create: `web/src/lib/playerStore.ts`, `web/src/lib/uiStore.ts`
- Test: `web/src/lib/playerStore.test.ts`

**Interfaces:**
- Produces:
  ```ts
  // playerStore.ts — singleton AudioEngine mirrored into Zustand.
  export const engine: AudioEngine
  export interface PlayerStore extends PlayerState {
    playTrackList(tracks: Track[], startIndex: number): void
    enqueue(t: Track): void
    removeAt(i: number): void
    moveItem(from: number, to: number): void
    toggle(): void
    next(): void
    prev(): void
    seekMs(ms: number): void
    setVolume(v: number): void
    toggleShuffle(): void
    cycleRepeat(): void
  }
  export const usePlayer: UseBoundStore<...>   // selector-friendly
  // uiStore.ts — the single right-panel slot.
  export type RightPanel = 'queue' | 'downloads' | null
  export const useUI: store with { rightPanel; openPanel(p); closePanel(); togglePanel(p) }
  ```

- [ ] **Step 1: Write the failing store test**

Create `web/src/lib/playerStore.test.ts`:
```ts
import { describe, expect, it } from 'vitest'
import { act } from '@testing-library/react'
import { usePlayer } from './playerStore'
import type { Track } from './types'

function track(id: string): Track {
  return {
    id, title: 'T' + id, albumId: 'al', album: 'Album', artistId: 'ar', artist: 'Artist',
    coverArtId: 'co', trackNumber: 1, discNumber: 1, durationMs: 1000, bitRate: 320,
    suffix: 'mp3', contentType: 'audio/mpeg',
  }
}

describe('playerStore', () => {
  it('mirrors engine state into the store after playTrackList', () => {
    act(() => {
      usePlayer.getState().playTrackList([track('1'), track('2')], 0)
    })
    expect(usePlayer.getState().current?.id).toBe('1')
    expect(usePlayer.getState().queue.length).toBe(2)
  })

  it('next updates the mirrored current', () => {
    act(() => {
      usePlayer.getState().playTrackList([track('1'), track('2')], 0)
      usePlayer.getState().cycleRepeat() // off -> all so next wraps within 2 items
      usePlayer.getState().next()
    })
    expect(usePlayer.getState().current?.id).toBe('2')
  })
})
```

> NOTE: jsdom's `HTMLMediaElement.play()` is not implemented and throws. The store's singleton engine uses the REAL audio factory by default. To keep `playerStore.test.ts` green in jsdom, stub `play`/`load`/`pause` on the prototype in a setup block. Add to `web/src/setupTests.ts`:
> ```ts
> import '@testing-library/jest-dom'
> // jsdom does not implement media playback — stub so the AudioEngine singleton works under test.
> if (typeof window !== 'undefined' && window.HTMLMediaElement) {
>   window.HTMLMediaElement.prototype.play = async () => {}
>   window.HTMLMediaElement.prototype.pause = () => {}
>   window.HTMLMediaElement.prototype.load = () => {}
> }
> ```
> (Append the stub; keep the existing `import '@testing-library/jest-dom'` line.)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/lib/playerStore.test.ts`
Expected: FAIL — cannot find module `./playerStore`.

- [ ] **Step 3: Write the UI store**

Create `web/src/lib/uiStore.ts`:
```ts
import { create } from 'zustand'

// RightPanel models the single right-side slot. M1 ships 'queue'. M3 adds
// 'downloads' (Download Tray) into the SAME slot — opening one closes the other.
export type RightPanel = 'queue' | 'downloads' | null

interface UIStore {
  rightPanel: RightPanel
  openPanel(p: Exclude<RightPanel, null>): void
  closePanel(): void
  togglePanel(p: Exclude<RightPanel, null>): void
}

export const useUI = create<UIStore>((set, get) => ({
  rightPanel: null,
  openPanel: (p) => set({ rightPanel: p }),
  closePanel: () => set({ rightPanel: null }),
  togglePanel: (p) => set({ rightPanel: get().rightPanel === p ? null : p }),
}))
```

- [ ] **Step 4: Write the player store**

Create `web/src/lib/playerStore.ts`:
```ts
import { create } from 'zustand'
import { AudioEngine, type PlayerState } from './audioEngine'
import type { Track } from './types'

// Single imperative engine instance, living OUTSIDE React.
export const engine = new AudioEngine()

interface PlayerActions {
  playTrackList(tracks: Track[], startIndex: number): void
  enqueue(t: Track): void
  removeAt(i: number): void
  moveItem(from: number, to: number): void
  play(): void
  pause(): void
  toggle(): void
  next(): void
  prev(): void
  seekMs(ms: number): void
  setVolume(v: number): void
  toggleShuffle(): void
  cycleRepeat(): void
}

export type PlayerStore = PlayerState & PlayerActions

export const usePlayer = create<PlayerStore>((set) => {
  // Mirror engine state into the store on every change.
  engine.subscribe((s) => set(s))
  return {
    ...engine.getState(),
    playTrackList: (tracks, startIndex) => engine.playTrackList(tracks, startIndex),
    enqueue: (t) => engine.enqueue(t),
    removeAt: (i) => engine.removeAt(i),
    moveItem: (from, to) => engine.moveItem(from, to),
    play: () => engine.play(),
    pause: () => engine.pause(),
    toggle: () => engine.toggle(),
    next: () => engine.next(),
    prev: () => engine.prev(),
    seekMs: (ms) => engine.seekMs(ms),
    setVolume: (v) => engine.setVolume(v),
    toggleShuffle: () => engine.toggleShuffle(),
    cycleRepeat: () => engine.cycleRepeat(),
  }
})
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd web && npx vitest run src/lib/playerStore.test.ts`
Expected: PASS.

- [ ] **Step 6: Typecheck + commit**

Run: `cd web && npm run build`
```bash
git add web/src/lib/playerStore.ts web/src/lib/uiStore.ts web/src/lib/playerStore.test.ts web/src/setupTests.ts
git commit -m "feat(web): zustand player store mirroring the engine and right-panel ui store"
```

---

## Task 10: TrackRow + PlayerBar rewrite (waveform seek, transport, keyboard)

**Files:**
- Create: `web/src/components/TrackRow.tsx`
- Modify: `web/src/components/PlayerBar.tsx` (rewrite)
- Test: `web/src/components/PlayerBar.test.tsx`

**Interfaces:**
- `TrackRow` props: `{ track: Track; index: number; queue: Track[] }` — clicking plays the whole `queue` from `index`.
- `PlayerBar`: reads `usePlayer` + `useUI`; renders art/title/artist, prev/play-pause/next, a waveform-styled seek bar with buffered range, volume, shuffle/repeat toggles, a Queue button (toggles the queue panel) and a disabled Downloads button. Binds global keyboard shortcuts (space, ←/→ seek 5s, shift+←/→ prev/next) via a `useEffect` on `window`.

- [ ] **Step 1: Write the TrackRow component**

Create `web/src/components/TrackRow.tsx`:
```tsx
import type { Track } from '../lib/types'
import { formatDuration } from '../lib/types'
import { coverUrl } from '../lib/libraryApi'
import { usePlayer } from '../lib/playerStore'

interface Props {
  track: Track
  index: number
  queue: Track[]
}

export function TrackRow({ track, index, queue }: Props) {
  const playTrackList = usePlayer((s) => s.playTrackList)
  const current = usePlayer((s) => s.current)
  const isCurrent = current?.id === track.id
  return (
    <button
      type="button"
      onClick={() => playTrackList(queue, index)}
      className={`group flex w-full items-center gap-3 rounded px-2 py-1.5 text-left hover:bg-neutral-800 ${
        isCurrent ? 'text-accent' : 'text-neutral-200'
      }`}
    >
      <span className="w-6 text-right text-sm text-neutral-500">{track.trackNumber || index + 1}</span>
      {track.coverArtId ? (
        <img src={coverUrl(track.coverArtId, 80)} alt="" className="h-9 w-9 rounded object-cover" />
      ) : (
        <div className="h-9 w-9 rounded bg-neutral-800" />
      )}
      <span className="flex-1 truncate">
        <span className="block truncate text-sm font-medium">{track.title}</span>
        <span className="block truncate text-xs text-neutral-400">{track.artist}</span>
      </span>
      <span className="text-xs text-neutral-500">{formatDuration(track.durationMs)}</span>
    </button>
  )
}
```

- [ ] **Step 2: Write the failing PlayerBar test**

Create `web/src/components/PlayerBar.test.tsx`:
```tsx
import { describe, expect, it, beforeEach } from 'vitest'
import { render, screen, fireEvent, act } from '@testing-library/react'
import { PlayerBar } from './PlayerBar'
import { usePlayer } from '../lib/playerStore'
import { useUI } from '../lib/uiStore'
import type { Track } from '../lib/types'

function track(id: string): Track {
  return {
    id, title: 'Song ' + id, albumId: 'al', album: 'Album', artistId: 'ar', artist: 'Artist',
    coverArtId: 'co', trackNumber: 1, discNumber: 1, durationMs: 200000, bitRate: 320,
    suffix: 'mp3', contentType: 'audio/mpeg',
  }
}

describe('PlayerBar', () => {
  beforeEach(() => {
    act(() => {
      usePlayer.getState().playTrackList([track('1'), track('2')], 0)
      useUI.getState().closePanel()
    })
  })

  it('shows the current track title and artist', () => {
    render(<PlayerBar />)
    expect(screen.getByText('Song 1')).toBeInTheDocument()
    expect(screen.getAllByText('Artist').length).toBeGreaterThan(0)
  })

  it('Queue button toggles the right panel', () => {
    render(<PlayerBar />)
    fireEvent.click(screen.getByRole('button', { name: /queue/i }))
    expect(useUI.getState().rightPanel).toBe('queue')
  })

  it('Downloads button is disabled (M3 placeholder)', () => {
    render(<PlayerBar />)
    expect(screen.getByRole('button', { name: /downloads/i })).toBeDisabled()
  })
})
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd web && npx vitest run src/components/PlayerBar.test.tsx`
Expected: FAIL — current `PlayerBar` renders only "Nothing playing".

- [ ] **Step 4: Rewrite PlayerBar**

Replace `web/src/components/PlayerBar.tsx`:
```tsx
import { useEffect } from 'react'
import { usePlayer } from '../lib/playerStore'
import { useUI } from '../lib/uiStore'
import { coverUrl } from '../lib/libraryApi'
import { formatDuration } from '../lib/types'

// SeekBar is waveform-STYLED (CSS bars, not real peaks). It shows played
// progress, the buffered range, and accepts click-to-seek. True peaks deferred.
function SeekBar() {
  const currentTimeMs = usePlayer((s) => s.currentTimeMs)
  const durationMs = usePlayer((s) => s.durationMs)
  const bufferedMs = usePlayer((s) => s.bufferedMs)
  const seekMs = usePlayer((s) => s.seekMs)

  const pct = durationMs > 0 ? (currentTimeMs / durationMs) * 100 : 0
  const bufPct = durationMs > 0 ? (bufferedMs / durationMs) * 100 : 0

  // 48 static "waveform" bars; heights are deterministic so SSR/test is stable.
  const bars = Array.from({ length: 48 }, (_, i) => 30 + ((i * 37) % 70))

  function onClick(e: React.MouseEvent<HTMLDivElement>) {
    if (durationMs <= 0) return
    const rect = e.currentTarget.getBoundingClientRect()
    const ratio = (e.clientX - rect.left) / rect.width
    seekMs(Math.max(0, Math.min(1, ratio)) * durationMs)
  }

  return (
    <div className="flex items-center gap-2 text-xs text-neutral-500">
      <span className="w-10 text-right tabular-nums">{formatDuration(currentTimeMs)}</span>
      <div
        role="slider"
        aria-label="Seek"
        aria-valuemin={0}
        aria-valuemax={durationMs}
        aria-valuenow={currentTimeMs}
        onClick={onClick}
        className="relative h-8 flex-1 cursor-pointer overflow-hidden rounded"
      >
        {/* buffered range */}
        <div className="absolute inset-y-0 left-0 bg-neutral-700/40" style={{ width: `${bufPct}%` }} />
        {/* waveform bars */}
        <div className="absolute inset-0 flex items-center gap-px px-px">
          {bars.map((h, i) => {
            const barPct = ((i + 0.5) / bars.length) * 100
            const played = barPct <= pct
            return (
              <div
                key={i}
                className={`flex-1 rounded-sm ${played ? 'bg-accent' : 'bg-neutral-600'}`}
                style={{ height: `${h}%` }}
              />
            )
          })}
        </div>
      </div>
      <span className="w-10 tabular-nums">{formatDuration(durationMs)}</span>
    </div>
  )
}

export function PlayerBar() {
  const current = usePlayer((s) => s.current)
  const playing = usePlayer((s) => s.playing)
  const shuffle = usePlayer((s) => s.shuffle)
  const repeat = usePlayer((s) => s.repeat)
  const volume = usePlayer((s) => s.volume)
  const toggle = usePlayer((s) => s.toggle)
  const next = usePlayer((s) => s.next)
  const prev = usePlayer((s) => s.prev)
  const seekMs = usePlayer((s) => s.seekMs)
  const currentTimeMs = usePlayer((s) => s.currentTimeMs)
  const setVolume = usePlayer((s) => s.setVolume)
  const toggleShuffle = usePlayer((s) => s.toggleShuffle)
  const cycleRepeat = usePlayer((s) => s.cycleRepeat)

  const togglePanel = useUI((s) => s.togglePanel)
  const rightPanel = useUI((s) => s.rightPanel)

  // Global keyboard shortcuts. Ignore when typing in an input/textarea.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      const el = e.target as HTMLElement | null
      if (el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.isContentEditable)) return
      if (e.code === 'Space') {
        e.preventDefault()
        toggle()
      } else if (e.key === 'ArrowRight' && e.shiftKey) {
        e.preventDefault()
        next()
      } else if (e.key === 'ArrowLeft' && e.shiftKey) {
        e.preventDefault()
        prev()
      } else if (e.key === 'ArrowRight') {
        e.preventDefault()
        seekMs(currentTimeMs + 5000)
      } else if (e.key === 'ArrowLeft') {
        e.preventDefault()
        seekMs(Math.max(0, currentTimeMs - 5000))
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [toggle, next, prev, seekMs, currentTimeMs])

  return (
    <div className="flex h-20 items-center gap-4 border-t border-neutral-800 px-4">
      {/* left: art + meta */}
      <div className="flex min-w-0 flex-1 items-center gap-3">
        {current?.coverArtId ? (
          <img src={coverUrl(current.coverArtId, 80)} alt="" className="h-12 w-12 rounded object-cover" />
        ) : (
          <div className="h-12 w-12 rounded bg-neutral-800" />
        )}
        <div className="min-w-0">
          <div className="truncate text-sm font-medium">{current ? current.title : 'Nothing playing'}</div>
          <div className="truncate text-xs text-neutral-400">{current?.artist ?? ''}</div>
        </div>
      </div>

      {/* center: transport + seek */}
      <div className="flex min-w-0 flex-[2] flex-col gap-1">
        <div className="flex items-center justify-center gap-4">
          <button
            type="button"
            aria-label="Shuffle"
            onClick={toggleShuffle}
            className={shuffle ? 'text-accent' : 'text-neutral-400 hover:text-neutral-200'}
          >
            ⤮
          </button>
          <button type="button" aria-label="Previous" onClick={prev} className="text-neutral-300 hover:text-white">
            ⏮
          </button>
          <button
            type="button"
            aria-label={playing ? 'Pause' : 'Play'}
            onClick={toggle}
            className="flex h-9 w-9 items-center justify-center rounded-full bg-white text-black"
          >
            {playing ? '⏸' : '▶'}
          </button>
          <button type="button" aria-label="Next" onClick={next} className="text-neutral-300 hover:text-white">
            ⏭
          </button>
          <button
            type="button"
            aria-label={`Repeat ${repeat}`}
            onClick={cycleRepeat}
            className={repeat !== 'off' ? 'text-accent' : 'text-neutral-400 hover:text-neutral-200'}
          >
            {repeat === 'one' ? '🔂' : '🔁'}
          </button>
        </div>
        <SeekBar />
      </div>

      {/* right: volume + panel buttons */}
      <div className="flex flex-1 items-center justify-end gap-3">
        <input
          type="range"
          min={0}
          max={1}
          step={0.01}
          value={volume}
          aria-label="Volume"
          onChange={(e) => setVolume(Number(e.target.value))}
          className="w-24 accent-[rgb(var(--color-accent))]"
        />
        <button
          type="button"
          onClick={() => togglePanel('queue')}
          className={`rounded px-2 py-1 text-sm ${rightPanel === 'queue' ? 'text-accent' : 'text-neutral-300 hover:text-white'}`}
        >
          Queue
        </button>
        <button
          type="button"
          disabled
          title="Downloads (coming in M3)"
          className="cursor-not-allowed rounded px-2 py-1 text-sm text-neutral-600"
        >
          Downloads
        </button>
      </div>
    </div>
  )
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd web && npx vitest run src/components/PlayerBar.test.tsx`
Expected: PASS.

- [ ] **Step 6: Typecheck + commit**

Run: `cd web && npm run build`
```bash
git add web/src/components/TrackRow.tsx web/src/components/PlayerBar.tsx web/src/components/PlayerBar.test.tsx
git commit -m "feat(web): player bar with waveform seek, transport, volume, and keyboard shortcuts"
```

---

## Task 11: Play Queue panel (the single right-panel slot)

**Files:**
- Create: `web/src/components/PlayQueue.tsx`
- Modify: `web/src/components/AppShell.tsx` (mount the panel slot)
- Test: `web/src/components/PlayQueue.test.tsx`

**Interfaces:**
- `PlayQueue` reads `usePlayer` (queue/index) + `useUI` (open/close). It is the M1 occupant of the single right-panel slot. Drag-reorder via native HTML5 drag events calling `moveItem(from, to)`; remove via `removeAt(i)`.
- `AppShell` renders `<PlayQueue/>` as an overlay so M3 can add `<DownloadTray/>` in the same slot (mutually exclusive via `useUI.rightPanel`).

- [ ] **Step 1: Write the failing panel test**

Create `web/src/components/PlayQueue.test.tsx`:
```tsx
import { describe, expect, it, beforeEach } from 'vitest'
import { render, screen, fireEvent, act } from '@testing-library/react'
import { PlayQueue } from './PlayQueue'
import { usePlayer } from '../lib/playerStore'
import { useUI } from '../lib/uiStore'
import type { Track } from '../lib/types'

function track(id: string): Track {
  return {
    id, title: 'Song ' + id, albumId: 'al', album: 'Album', artistId: 'ar', artist: 'Artist',
    coverArtId: 'co', trackNumber: 1, discNumber: 1, durationMs: 1000, bitRate: 320,
    suffix: 'mp3', contentType: 'audio/mpeg',
  }
}

describe('PlayQueue', () => {
  beforeEach(() => {
    act(() => {
      usePlayer.getState().playTrackList([track('1'), track('2'), track('3')], 0)
      useUI.getState().openPanel('queue')
    })
  })

  it('renders the now-playing header and up-next items', () => {
    render(<PlayQueue />)
    expect(screen.getByText('Now Playing')).toBeInTheDocument()
    expect(screen.getByText('Song 1')).toBeInTheDocument()
    expect(screen.getByText('Song 2')).toBeInTheDocument()
  })

  it('remove drops a track from the queue', () => {
    render(<PlayQueue />)
    const removeButtons = screen.getAllByRole('button', { name: /remove/i })
    fireEvent.click(removeButtons[removeButtons.length - 1])
    expect(usePlayer.getState().queue.length).toBe(2)
  })

  it('is hidden when the panel is closed', () => {
    act(() => useUI.getState().closePanel())
    const { container } = render(<PlayQueue />)
    expect(container.firstChild).toBeNull()
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/components/PlayQueue.test.tsx`
Expected: FAIL — cannot find module `./PlayQueue`.

- [ ] **Step 3: Write the PlayQueue panel**

Create `web/src/components/PlayQueue.tsx`:
```tsx
import { useRef } from 'react'
import { usePlayer } from '../lib/playerStore'
import { useUI } from '../lib/uiStore'
import { coverUrl } from '../lib/libraryApi'

export function PlayQueue() {
  const rightPanel = useUI((s) => s.rightPanel)
  const closePanel = useUI((s) => s.closePanel)
  const queue = usePlayer((s) => s.queue)
  const index = usePlayer((s) => s.index)
  const current = usePlayer((s) => s.current)
  const removeAt = usePlayer((s) => s.removeAt)
  const moveItem = usePlayer((s) => s.moveItem)

  const dragFrom = useRef<number | null>(null)

  if (rightPanel !== 'queue') return null

  const upNext = queue
    .map((t, i) => ({ t, i }))
    .filter(({ i }) => i !== index)

  return (
    <aside className="absolute right-0 top-0 z-20 flex h-full w-80 flex-col border-l border-neutral-800 bg-neutral-950/95 backdrop-blur">
      <div className="flex items-center justify-between border-b border-neutral-800 p-4">
        <h2 className="text-lg font-bold">Play Queue</h2>
        <button type="button" aria-label="Close queue" onClick={closePanel} className="text-neutral-400 hover:text-white">
          ✕
        </button>
      </div>

      <div className="border-b border-neutral-800 p-4">
        <div className="mb-2 text-xs uppercase tracking-wide text-neutral-500">Now Playing</div>
        {current ? (
          <div className="flex items-center gap-3">
            {current.coverArtId ? (
              <img src={coverUrl(current.coverArtId, 80)} alt="" className="h-10 w-10 rounded object-cover" />
            ) : (
              <div className="h-10 w-10 rounded bg-neutral-800" />
            )}
            <div className="min-w-0">
              <div className="truncate text-sm font-medium text-accent">{current.title}</div>
              <div className="truncate text-xs text-neutral-400">{current.artist}</div>
            </div>
          </div>
        ) : (
          <div className="text-sm text-neutral-500">Nothing playing</div>
        )}
      </div>

      <div className="flex-1 overflow-auto p-2">
        <div className="px-2 py-1 text-xs uppercase tracking-wide text-neutral-500">Up Next</div>
        <ul>
          {upNext.map(({ t, i }) => (
            <li
              key={`${t.id}-${i}`}
              draggable
              onDragStart={() => (dragFrom.current = i)}
              onDragOver={(e) => e.preventDefault()}
              onDrop={() => {
                if (dragFrom.current !== null && dragFrom.current !== i) {
                  moveItem(dragFrom.current, i)
                }
                dragFrom.current = null
              }}
              className="flex items-center gap-2 rounded px-2 py-1.5 hover:bg-neutral-800"
            >
              <span className="cursor-grab text-neutral-600">⠿</span>
              <div className="min-w-0 flex-1">
                <div className="truncate text-sm">{t.title}</div>
                <div className="truncate text-xs text-neutral-400">{t.artist}</div>
              </div>
              <button
                type="button"
                aria-label={`Remove ${t.title}`}
                onClick={() => removeAt(i)}
                className="text-neutral-500 hover:text-accent"
              >
                ✕
              </button>
            </li>
          ))}
          {upNext.length === 0 && <li className="px-2 py-4 text-sm text-neutral-500">Queue is empty.</li>}
        </ul>
      </div>
    </aside>
  )
}
```

- [ ] **Step 4: Mount the panel slot in AppShell**

Replace `web/src/components/AppShell.tsx`:
```tsx
import { Outlet } from 'react-router-dom'
import { Sidebar } from './Sidebar'
import { PlayerBar } from './PlayerBar'
import { PlayQueue } from './PlayQueue'

export function AppShell() {
  return (
    <div className="flex h-full flex-col">
      <div className="relative flex min-h-0 flex-1">
        <Sidebar />
        <main className="flex-1 overflow-auto p-6">
          <Outlet />
        </main>
        {/* Single right-panel slot. M1: PlayQueue. M3 adds DownloadTray here,
            mutually exclusive via useUI.rightPanel. */}
        <PlayQueue />
      </div>
      <PlayerBar />
    </div>
  )
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd web && npx vitest run src/components/PlayQueue.test.tsx`
Expected: PASS.

- [ ] **Step 6: Typecheck + commit**

Run: `cd web && npm run build`
```bash
git add web/src/components/PlayQueue.tsx web/src/components/AppShell.tsx web/src/components/PlayQueue.test.tsx
git commit -m "feat(web): play queue slide-over with drag-reorder and remove"
```

---

## Task 12: Search (Library mode) + Album + Artist + Library browse pages

**Files:**
- Modify: `web/src/routes/Search.tsx` (rewrite), `web/src/routes/Album.tsx` (replace placeholder), `web/src/routes/Artist.tsx` (replace placeholder), `web/src/routes/Library.tsx` (rewrite)
- Test: `web/src/routes/Search.test.tsx`, `web/src/routes/Album.test.tsx`

**Interfaces:**
- `Search`: a search box (Library mode only). On results, renders Tracks (via `TrackRow`, clicking REPLACES the queue and plays from that track), Albums and Artists sections (linking to `/album/:id` and `/artist/:id`). The M2 "Everywhere" toggle seam is marked with a comment + a disabled pill.
- `Album`: `useAlbum(id)` → header (cover, name, artist link, year) + track list (`TrackRow`, queue = the album's tracks).
- `Artist`: `useArtist(id)` → header + album grid linking to `/album/:id`.
- `Library`: tabs Artists / Albums using `useArtists()` / `useAlbums('newest')`, each linking to its page.

- [ ] **Step 1: Write the failing route tests**

Create `web/src/routes/Search.test.tsx`:
```tsx
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import Search from './Search'

function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>
  )
}

describe('Search (library mode)', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        new Response(
          JSON.stringify({
            tracks: [{ id: 't1', title: 'Found Song', artist: 'A', durationMs: 1000, trackNumber: 1 }],
            albums: [{ id: 'al1', name: 'Found Album', artist: 'A' }],
            artists: [{ id: 'ar1', name: 'Found Artist' }],
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        ),
      ),
    )
  })
  afterEach(() => vi.unstubAllGlobals())

  it('renders results in sections after typing a query', async () => {
    render(wrap(<Search />))
    fireEvent.change(screen.getByPlaceholderText(/search your library/i), { target: { value: 'found' } })
    await waitFor(() => expect(screen.getByText('Found Song')).toBeInTheDocument())
    expect(screen.getByText('Found Album')).toBeInTheDocument()
    expect(screen.getByText('Found Artist')).toBeInTheDocument()
  })
})
```

Create `web/src/routes/Album.test.tsx`:
```tsx
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import Album from './Album'

describe('Album page', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        new Response(
          JSON.stringify({
            id: 'al1', name: 'Great Album', artist: 'A', artistId: 'ar1', year: 2021,
            tracks: [{ id: 't1', title: 'Track One', artist: 'A', durationMs: 1000, trackNumber: 1 }],
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        ),
      ),
    )
  })
  afterEach(() => vi.unstubAllGlobals())

  it('renders album header and tracks', async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={['/album/al1']}>
          <Routes>
            <Route path="/album/:id" element={<Album />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
    await waitFor(() => expect(screen.getByText('Great Album')).toBeInTheDocument())
    expect(screen.getByText('Track One')).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && npx vitest run src/routes/Search.test.tsx src/routes/Album.test.tsx`
Expected: FAIL — current `Search` is a stub; `Album` is a placeholder.

- [ ] **Step 3: Rewrite Search**

Replace `web/src/routes/Search.tsx`:
```tsx
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useLibrarySearch, coverUrl } from '../lib/libraryApi'
import { TrackRow } from '../components/TrackRow'

export default function Search() {
  const [q, setQ] = useState('')
  // M2 SEAM: an "Everywhere" mode toggle goes here (segmented pill). For M1 it
  // is Library-only; the disabled pill marks the seam without wiring SSE.
  const { data, isFetching } = useLibrarySearch(q)

  const tracks = data?.tracks ?? []
  const albums = data?.albums ?? []
  const artists = data?.artists ?? []

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
          <span className="bg-accent px-3 py-1 text-sm text-white">My Library</span>
          {/* M2 SEAM: enable this pill and switch to /search/everywhere (SSE). */}
          <button type="button" disabled title="Everywhere search arrives in M2" className="cursor-not-allowed px-3 py-1 text-sm text-neutral-600">
            Everywhere
          </button>
        </div>
      </div>

      {q.trim() === '' && <p className="text-neutral-500">Type to search your library.</p>}
      {isFetching && <p className="text-neutral-500">Searching…</p>}

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
    </div>
  )
}
```

> Queue behavior (documented decision): clicking a track in the Tracks section REPLACES the queue with the current results list and plays from the clicked index. This matches Spotify-like "play this list" intent.

- [ ] **Step 4: Write the Album page**

Replace `web/src/routes/Album.tsx`:
```tsx
import { Link, useParams } from 'react-router-dom'
import { useAlbum, coverUrl } from '../lib/libraryApi'
import { TrackRow } from '../components/TrackRow'
import { formatDuration } from '../lib/types'
import { usePlayer } from '../lib/playerStore'

export default function Album() {
  const { id = '' } = useParams()
  const { data: album, isLoading, isError } = useAlbum(id)
  const playTrackList = usePlayer((s) => s.playTrackList)

  if (isLoading) return <p className="text-neutral-500">Loading album…</p>
  if (isError || !album) return <p className="text-neutral-500">Album not found.</p>

  const tracks = album.tracks ?? []

  return (
    <div className="space-y-6">
      <header className="flex items-end gap-6">
        {album.coverArtId ? (
          <img src={coverUrl(album.coverArtId, 300)} alt="" className="h-44 w-44 rounded object-cover shadow-lg" />
        ) : (
          <div className="h-44 w-44 rounded bg-neutral-800" />
        )}
        <div>
          <div className="text-xs uppercase tracking-wide text-neutral-400">Album</div>
          <h1 className="text-3xl font-bold">{album.name}</h1>
          <div className="mt-1 text-sm text-neutral-400">
            <Link to={`/artist/${album.artistId}`} className="hover:text-accent">
              {album.artist}
            </Link>
            {album.year ? ` · ${album.year}` : ''}
            {album.songCount ? ` · ${album.songCount} songs` : ''}
            {album.durationMs ? ` · ${formatDuration(album.durationMs)}` : ''}
          </div>
          <button
            type="button"
            onClick={() => tracks.length && playTrackList(tracks, 0)}
            className="mt-3 rounded-full bg-accent px-5 py-2 text-sm font-medium text-white disabled:opacity-50"
            disabled={tracks.length === 0}
          >
            Play
          </button>
        </div>
      </header>

      <div className="space-y-0.5">
        {tracks.map((t, i) => (
          <TrackRow key={t.id} track={t} index={i} queue={tracks} />
        ))}
      </div>
    </div>
  )
}
```

- [ ] **Step 5: Write the Artist page**

Replace `web/src/routes/Artist.tsx`:
```tsx
import { Link, useParams } from 'react-router-dom'
import { useArtist, coverUrl } from '../lib/libraryApi'

export default function Artist() {
  const { id = '' } = useParams()
  const { data: artist, isLoading, isError } = useArtist(id)

  if (isLoading) return <p className="text-neutral-500">Loading artist…</p>
  if (isError || !artist) return <p className="text-neutral-500">Artist not found.</p>

  const albums = artist.albums ?? []

  return (
    <div className="space-y-6">
      <header className="flex items-end gap-6">
        {artist.coverArtId ? (
          <img src={coverUrl(artist.coverArtId, 300)} alt="" className="h-44 w-44 rounded-full object-cover shadow-lg" />
        ) : (
          <div className="h-44 w-44 rounded-full bg-neutral-800" />
        )}
        <div>
          <div className="text-xs uppercase tracking-wide text-neutral-400">Artist</div>
          <h1 className="text-3xl font-bold">{artist.name}</h1>
          <div className="mt-1 text-sm text-neutral-400">{artist.albumCount || albums.length} albums</div>
        </div>
      </header>

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
              <div className="truncate text-xs text-neutral-400">{al.year || ''}</div>
            </Link>
          ))}
          {albums.length === 0 && <p className="text-neutral-500">No albums.</p>}
        </div>
      </section>
    </div>
  )
}
```

- [ ] **Step 6: Rewrite the Library browse page**

Replace `web/src/routes/Library.tsx`:
```tsx
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useAlbums, useArtists, coverUrl } from '../lib/libraryApi'

type Tab = 'artists' | 'albums'

export default function Library() {
  const [tab, setTab] = useState<Tab>('albums')
  const albums = useAlbums('newest')
  const artists = useArtists()

  return (
    <div className="space-y-6">
      <div className="flex gap-2">
        {(['albums', 'artists'] as Tab[]).map((t) => (
          <button
            key={t}
            type="button"
            onClick={() => setTab(t)}
            className={`rounded-full px-4 py-1.5 text-sm capitalize ${
              tab === t ? 'bg-accent text-white' : 'bg-neutral-900 text-neutral-300 hover:bg-neutral-800'
            }`}
          >
            {t}
          </button>
        ))}
      </div>

      {tab === 'albums' && (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
          {(albums.data ?? []).map((al) => (
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
          {albums.isLoading && <p className="text-neutral-500">Loading albums…</p>}
        </div>
      )}

      {tab === 'artists' && (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6">
          {(artists.data ?? []).map((ar) => (
            <Link key={ar.id} to={`/artist/${ar.id}`} className="group text-center">
              {ar.coverArtId ? (
                <img src={coverUrl(ar.coverArtId, 300)} alt="" className="aspect-square w-full rounded-full object-cover" />
              ) : (
                <div className="aspect-square w-full rounded-full bg-neutral-800" />
              )}
              <div className="mt-1 truncate text-sm font-medium group-hover:text-accent">{ar.name}</div>
            </Link>
          ))}
          {artists.isLoading && <p className="text-neutral-500">Loading artists…</p>}
        </div>
      )}
    </div>
  )
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `cd web && npx vitest run src/routes/Search.test.tsx src/routes/Album.test.tsx`
Expected: PASS.

- [ ] **Step 8: Full frontend test + typecheck**

Run: `cd web && npm run test && npm run build`
Expected: all tests PASS; build succeeds.

- [ ] **Step 9: Commit**

```bash
git add web/src/routes/Search.tsx web/src/routes/Album.tsx web/src/routes/Artist.tsx web/src/routes/Library.tsx web/src/routes/Search.test.tsx web/src/routes/Album.test.tsx
git commit -m "feat(web): library search and album/artist/library browse pages"
```

---

## Task 13: Sidebar nav + full-stack smoke verification

**Files:**
- Modify: `web/src/components/Sidebar.tsx` (keep Search/Library/Settings; Library is now functional)
- Test: manual full-stack run (documented expected output)

**Interfaces:**
- The Sidebar links remain `/search`, `/library`, `/settings`. No interface change needed — this task confirms navigation and runs an end-to-end smoke against a live Navidrome (docker-compose.dev from M0).

- [ ] **Step 1: Confirm Sidebar links (no code change unless missing)**

Open `web/src/components/Sidebar.tsx`. Confirm it contains links to `/search`, `/library`, `/settings`. It already does (from M0). No edit required. If `Library` is missing a label, ensure the items array is:
```tsx
const items = [
  { to: '/search', label: 'Search' },
  { to: '/library', label: 'Library' },
  { to: '/settings', label: 'Settings' },
]
```

- [ ] **Step 2: Seed a library adapter_instance for the live smoke test**

This requires a running Navidrome with a user. Bring up the M0 dev stack and create a Navidrome admin (one-time, via http://localhost:4533). Then seed an adapter row directly (until the settings UI lands in M4):
```bash
docker compose -f docker-compose.dev.yml up -d
# Wait for Navidrome, create a user 'reverb'/'reverbpass' in its UI, add a CC track to dev/music.
rm -f data/reverb.db
REVERB_ADMIN_PASSWORD=devpw go run ./cmd/reverb &
sleep 2
# Log in to get a session cookie:
curl -s -c /tmp/reverb.cookies -X POST localhost:8090/api/v1/auth/login -H 'Content-Type: application/json' -d '{"password":"devpw"}'
```

Then insert the adapter row using sqlite3 (the DB is at ./data/reverb.db). Run:
```bash
sqlite3 data/reverb.db "INSERT INTO adapter_instances (id,type,name,enabled,priority,config_json) VALUES ('lib1','library','subsonic',1,0,'{\"url\":\"http://localhost:4533\",\"username\":\"reverb\",\"password\":\"reverbpass\"}');"
```
Restart Reverb so it builds the adapter from the row:
```bash
kill %1 2>/dev/null
REVERB_ADMIN_PASSWORD=devpw go run ./cmd/reverb &
sleep 2
```
Expected log line: `library adapter active: subsonic`.

- [ ] **Step 3: Smoke the library endpoints**

Run:
```bash
curl -s -b /tmp/reverb.cookies "localhost:8090/api/v1/library/albums?type=newest" | head -c 300
curl -s -b /tmp/reverb.cookies "localhost:8090/api/v1/library/search?q=a" | head -c 300
```
Expected: JSON arrays/objects from Navidrome (not `{"error":...}`). If you added a track, it appears in results.

- [ ] **Step 4: Smoke the stream proxy Range behavior**

Run (replace TRACK_ID with an `id` from the search output):
```bash
curl -s -D - -o /dev/null -b /tmp/reverb.cookies -H 'Range: bytes=0-1023' "localhost:8090/api/v1/stream/TRACK_ID" | grep -iE 'HTTP/|content-range|accept-ranges|content-type'
```
Expected: `HTTP/1.1 206 Partial Content`, an `Accept-Ranges: bytes`, a `Content-Range: bytes 0-1023/...`, and an audio `Content-Type`.

Tear down:
```bash
kill %1 2>/dev/null
docker compose -f docker-compose.dev.yml down
```

- [ ] **Step 5: Commit (only if Sidebar changed)**

If you edited the Sidebar, commit:
```bash
git add web/src/components/Sidebar.tsx
git commit -m "feat(web): confirm sidebar navigation for functional library"
```
Otherwise no commit for this task — it is verification-only.

---

## Definition of Done (M1)

- `go test ./cmd/... ./internal/...` is green: core types, library conformance, subsonic client + adapter (httptest, recorded JSON), API library/stream/cover handlers, and the composition wiring all pass.
- `cd web && npm run test` is green: audio-engine queue/shuffle/repeat logic, player store mirror, player bar, play queue, library API hooks, and the Search/Album route tests.
- `cd web && npm run build` (tsc + vite) succeeds — no TS errors.
- The Subsonic adapter passes `library.RunConformance`.
- A configured Navidrome (seeded `adapter_instances` row) yields: `library adapter active: subsonic` at startup; `/api/v1/library/albums`, `/search`, `/artist/:id`, `/album/:id`, `/playlists` return mapped data; `/api/v1/stream/:id` returns `206` with `Content-Range`/`Accept-Ranges` when a `Range` header is sent; `/api/v1/cover/:id` returns image bytes. Subsonic credentials never appear in any browser-visible response.
- In the SPA: searching the library shows Tracks/Albums/Artists; clicking a track plays it through the dual-`<audio>` engine; the player bar shows art/title/artist, working transport, a waveform-styled seek bar with buffered range, volume, and shuffle/repeat; the Queue button opens the Play Queue panel with drag-reorder + remove; keyboard shortcuts (space, ←/→, shift+←/→) work; Album and Artist pages render and play; Library browse lists albums/artists. The Downloads button is rendered disabled (M3).
- Adapter registration is explicit in `main.go` (no `init()` side-effects). Library data is never written to SQLite.

---

## Self-Review

**Spec coverage (M1 line items):**
- Core domain types ✓ (Task 1 — Track/Album/Artist/Playlist/SearchResults/EntityType/StreamHandle/StreamOpts/ScanStatus/CoverArt, JSON-tagged).
- `LibraryAdapter` interface + conformance suite with `StartScan`/`ScanStatus` documented optional/stubable ✓ (Task 2).
- Subsonic adapter: token auth (md5(password+salt), `c=reverb&v=1.16.1&f=json`), wrapped-JSON decode + failed→error mapping, endpoints ping/search3/getArtists/getArtist/getAlbum/getAlbumList2/getPlaylists/stream(Range)/getCoverArt/startScan/getScanStatus, DTO→core mapping, Plugin (ConfigSchema url/username/password[secret], TestConnection=ping), httptest + recorded JSON ✓ (Tasks 3–5).
- Composition + API: explicit registration at composition root, active adapter from enabled `library` adapter_instance row with `REVERB_LIBRARY_PASSWORD` override, `Library` in Deps, REST `/library/*` + `/stream/:id` (Range proxy) + `/cover/:id`, all behind `requireAuth` ✓ (Tasks 5–6).
- Frontend AudioEngine: dual-`<audio>`, queue/transport/shuffle/repeat/volume + preload, subscribe callback, injectable audio element, unit-tested logic in jsdom ✓ (Task 8).
- Zustand player store mirror + actions ✓ (Task 9).
- Player bar rewrite: art via /cover, title/artist, transport, waveform-styled seek with buffered range, volume, shuffle/repeat, Queue button, disabled Downloads placeholder, global keyboard shortcuts ✓ (Task 10).
- Play Queue panel: slide-over, now-playing header, up-next, drag-reorder, remove; single right-panel slot designed for M3's Download Tray ✓ (Tasks 9 uiStore, 11).
- Search page (Library mode), TanStack-Query-backed Tracks/Albums/Artists, click-to-play (replaces queue — documented), album/artist links, marked M2 Everywhere seam ✓ (Task 12).
- Album + Artist pages routed `/album/:id` `/artist/:id`, wired into App routes and Sidebar/Library browse (getArtists/getAlbumList2) ✓ (Tasks 7, 12, 13).

**Placeholder scan:** every code block is complete and runnable. The two intentional, explicitly-replaced stubs are `web/src/routes/Album.tsx` / `Artist.tsx` placeholders created in Task 7 (so App compiles) and rewritten in Task 12 — flagged in both tasks. The `srcFor` ambiguity in the AudioEngine draft was removed; the constructor default is `(t) => streamUrl(t.id)` and call sites use `this.resolveSrc(t)`. No `TODO`/`add error handling`/`similar to above` remain.

**Type consistency across tasks:**
- `core.Track` field set + JSON tags (Task 1) is mirrored exactly by `web/src/lib/types.ts` `Track` (Task 7) — both use camelCase (`durationMs`, `coverArtId`, `trackNumber`). Subsonic `duration` (seconds) → `DurationMs` (×1000) is applied consistently in `mapTrack`/`mapAlbum`/`mapPlaylist` (Task 4) and asserted in `TestSearchMapsToCore`.
- `LibraryAdapter.Stream(ctx, trackID, opts, rangeHeader)` (Task 2) is implemented by the subsonic adapter (Task 4), consumed by the fake in `library_test.go`/`stream_test.go` (Task 5), and the API proxy forwards `r.Header.Get("Range")` and copies back status/Content-Type/Content-Length/Accept-Ranges/Content-Range (Task 5) — matching the StreamHandle shape from Task 1.
- `Deps.Library library.LibraryAdapter` (Task 5) is set in `main.go` from `buildLibraryAdapter` (Task 6), which returns `library.LibraryAdapter`; nil-safe via `libraryReady` (503).
- Browse endpoints use optional interfaces `artistBrowser`/`albumBrowser` (Task 5) implemented by the subsonic adapter's `GetArtistsBrowse`/`GetAlbumsBrowse` (Task 5 Step 1) and by the fake in tests; non-implementers return `200 []`.
- Frontend: `usePlayer` actions (`playTrackList`, `enqueue`, `removeAt`, `moveItem`, `toggle`, `next`, `prev`, `seekMs`, `setVolume`, `toggleShuffle`, `cycleRepeat`) (Task 9) match `AudioEngine`'s public methods (Task 8) and are consumed by `TrackRow`/`PlayerBar`/`PlayQueue`/`Album` (Tasks 10–12). `useUI.rightPanel` (Task 9) is the single slot used by `PlayerBar` (toggle 'queue') and `PlayQueue`/`AppShell` (Tasks 10–11).
- `coverUrl`/`streamUrl` (Task 7) are the single source of media URLs used by every component and the engine's default resolver.

**Known follow-ups (intentionally deferred, not M1):** true near-gapless active/preload element swap on advance (Task 8 loads but does not hot-swap — noted); Everywhere search (M2 seam marked in Search); Download Tray in the right-panel slot (M3); settings UI to create the `adapter_instances` row (M4 — M1 seeds it via SQL in the Task 13 smoke); dynamic album palette / responsive mobile chrome (M4b).
