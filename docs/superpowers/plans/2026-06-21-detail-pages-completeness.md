# Music Detail Pages + Library Completeness — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build Artist/Album/Playlist detail pages unified by a streaming library-completeness engine — fetch an artist's deduped Spotify discography, exact-match it against the library, and surface per-album / per-track "have vs. available" with one-click download — wired into every existing surface that links to artists, albums, or playlists.

**Architecture:** A new Go `coverage` package orchestrates resolve → discography → dedup → per-album exact-match rollup → cache → SSE stream, reusing the existing `matching.Service`, EventBus `library.updated` signal, and download `Manager`. The frontend adds source-qualified routes, a `CoverageChip`, a `coverageStore` + `useCoverageStream` (SSE, mirroring `everywhereStore`/`searchStream`), reworks Artist/Album pages, adds a Playlist page, and re-points every detail navigation.

**Tech Stack:** Go (chi, modernc sqlite, goose, sqlc), React 19 + TS (Vite, Tailwind, TanStack Query, Zustand), Vitest + Playwright.

## Global Constraints

- Go module path: `github.com/maxjb-xyz/reverb`. Binary `reverb`; env prefix `REVERB_*`.
- All exported domain types live in `internal/core` with stable camelCase JSON tags.
- DB: goose migrations in `internal/store/migrations/NNNN_name.sql` (next number is **0004**); queries in `internal/store/queries/*.sql`; regenerate typed code with **`sqlc generate`** (run from repo root) — never hand-edit `internal/store/db/*.go`.
- Library data is **never persisted**; it always flows through `LibraryAdapter`. Caches store only external/derived data.
- "Full" / "In Library" renders in the **accent** color (`text-accent`); green is status-only. Accent default is red `#F0354B` but is configurable — always use the `accent` token, never a hex literal.
- Exact completeness rule: an album is `full` only if **every** canonical external track matches a library track; otherwise `partial` (with owned/total) or `none`.
- Discography scope: **Albums + Singles/EPs only**, deduped to the canonical standard edition; drop compilations and "appears on".
- Missing albums are **clean at rest** (no cover marker); download affordance appears on hover.
- Frontend tests use Vitest; backend uses `go test ./...`. Commit after every green task.
- Reuse, don't reinvent: `TrackRow`, `DownloadAction`, `ProgressRing`, `Chip`, `Button`, `IconButton`, `Cover`, `MediaCard`, `matching.Service`, the download `Manager`.

---

## File Structure

**Backend (new)**
- `internal/core/coverage.go` — `CoverageState`, `ExternalTrackRef`, `AlbumCoverage`, `DiscographyAlbum`, `ArtistDetail`, `AlbumDetail`.
- `internal/coverage/canonical.go` (+ `_test.go`) — pure discography dedup/canonicalization.
- `internal/coverage/rollup.go` (+ `_test.go`) — per-album exact-match rollup → `AlbumCoverage`.
- `internal/coverage/resolve.go` (+ `_test.go`) — library artist → external artist id (search + cache).
- `internal/coverage/service.go` (+ `_test.go`) — orchestration + caches + streaming channel.
- `internal/store/migrations/0004_coverage.sql`, `internal/store/queries/coverage.sql` — 3 cache tables.
- `internal/api/coverage.go` (+ `_test.go`) — `ArtistDetail`, coverage SSE, `AlbumDetail`, batch download, playlist detail handlers.

**Backend (modified)**
- `internal/core/external.go` — add `Kind`, `TotalTracks` to `ExternalAlbum`.
- `internal/search/spotify/{adapter,client,dto}.go` — `GetArtistDiscography`; artist search already exists.
- `internal/library/library.go`, `internal/library/subsonic/adapter.go`, `internal/library/conformance.go` — `GetPlaylist(id)`.
- `internal/api/server.go` — new routes.
- `internal/wiring/*.go` — construct the coverage service and pass it to `api.Deps`.

**Frontend (new)**
- `web/src/lib/coverageApi.ts`, `web/src/lib/coverageStream.ts`, `web/src/lib/coverageStore.ts` (+ tests).
- `web/src/components/ui/CoverageChip.tsx` (+ test).
- `web/src/routes/Playlist.tsx` (+ test).

**Frontend (modified)**
- `web/src/lib/types.ts`, `web/src/lib/libraryApi.ts`, `web/src/lib/downloadApi.ts`.
- `web/src/components/ui/MediaCard.tsx`.
- `web/src/routes/{Artist,Album}.tsx`, `web/src/App.tsx`.
- `web/src/components/shell/LibraryRail.tsx`, `web/src/routes/{Library,Search,Home}.tsx`, `web/src/components/search/SearchSuggest.tsx`, `web/src/components/shell/NowPlayingPanel.tsx`, `web/src/components/AddToPlaylistMenu.tsx`.
- `web/e2e/*` — completeness flow.

---

# Phase 1 — Backend completeness engine

### Task 1: Core coverage types

**Files:**
- Create: `internal/core/coverage.go`
- Modify: `internal/core/external.go` (add two fields)
- Test: `internal/core/coverage_test.go`

**Interfaces:**
- Produces: `core.CoverageState`, `core.ExternalTrackRef`, `core.AlbumCoverage`, `core.DiscographyAlbum`, `core.ArtistDetail`, `core.AlbumDetail`; `core.ExternalAlbum.Kind`, `core.ExternalAlbum.TotalTracks`.

- [ ] **Step 1: Write the failing test**

```go
// internal/core/coverage_test.go
package core

import (
	"encoding/json"
	"testing"
)

func TestAlbumCoverageJSONTags(t *testing.T) {
	c := AlbumCoverage{
		Source: "spotify", ExternalAlbumID: "abc", State: CoveragePartial,
		OwnedCount: 7, TotalCount: 10, LibraryAlbumID: "lib1",
		MissingTracks: []ExternalTrackRef{{Source: "spotify", ExternalID: "t1", Title: "x", DurationMs: 1000}},
	}
	b, _ := json.Marshal(c)
	got := string(b)
	for _, want := range []string{`"state":"partial"`, `"ownedCount":7`, `"totalCount":10`, `"libraryAlbumId":"lib1"`, `"missingTracks"`, `"externalAlbumId":"abc"`} {
		if !contains(got, want) {
			t.Fatalf("missing %s in %s", want, got)
		}
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0) }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/ -run TestAlbumCoverageJSONTags`
Expected: FAIL (undefined: AlbumCoverage).

- [ ] **Step 3: Write the types**

```go
// internal/core/coverage.go
package core

// CoverageState is an album's exact-match verdict against the local library.
type CoverageState string

const (
	CoveragePending CoverageState = "pending" // not computed yet (skeleton)
	CoverageNone    CoverageState = "none"    // zero tracks owned
	CoveragePartial CoverageState = "partial" // some, not all, tracks owned
	CoverageFull    CoverageState = "full"    // every canonical track owned
)

// ExternalTrackRef is the minimum needed to enqueue a download and render a row.
type ExternalTrackRef struct {
	Source     string `json:"source"`
	ExternalID string `json:"externalId"`
	Title      string `json:"title"`
	Artist     string `json:"artist,omitempty"`
	Album      string `json:"album,omitempty"`
	ISRC       string `json:"isrc,omitempty"`
	DurationMs int    `json:"durationMs"`
}

// AlbumCoverage is the per-album rollup streamed to the client.
type AlbumCoverage struct {
	Source          string             `json:"source"`
	ExternalAlbumID string             `json:"externalAlbumId"`
	State           CoverageState      `json:"state"`
	OwnedCount      int                `json:"ownedCount"`
	TotalCount      int                `json:"totalCount"`
	LibraryAlbumID  string             `json:"libraryAlbumId,omitempty"`
	MissingTracks   []ExternalTrackRef `json:"missingTracks"`
}

// DiscographyAlbum is one deduped release in the artist-page skeleton.
type DiscographyAlbum struct {
	Source      string `json:"source"`
	ExternalID  string `json:"externalId"`
	Name        string `json:"name"`
	CoverURL    string `json:"coverUrl,omitempty"`
	Year        int    `json:"year"`
	Kind        string `json:"kind"` // "album" | "single"
	TotalTracks int    `json:"totalTracks"`
}

// ArtistDetail is the artist-page response: header + deduped discography skeleton.
// When Resolved is false, Albums holds the library-owned albums (graceful degrade)
// and no coverage stream is opened.
type ArtistDetail struct {
	Source           string             `json:"source"`
	ID               string             `json:"id"`
	Name             string             `json:"name"`
	CoverArtID       string             `json:"coverArtId,omitempty"`
	CoverURL         string             `json:"coverUrl,omitempty"`
	LibraryArtistID  string             `json:"libraryArtistId,omitempty"`
	ExternalArtistID string             `json:"externalArtistId,omitempty"`
	Resolved         bool               `json:"resolved"`
	Albums           []DiscographyAlbum `json:"albums"`
}

// AlbumDetailTrack is one track on the album page, owned or missing.
type AlbumDetailTrack struct {
	State          CoverageState    `json:"state"` // full = owned, none = missing
	LibraryTrack   *Track           `json:"libraryTrack,omitempty"`
	ExternalRef    *ExternalTrackRef `json:"externalRef,omitempty"`
	Title          string           `json:"title"`
	Artist         string           `json:"artist"`
	TrackNumber    int              `json:"trackNumber"`
	DurationMs     int              `json:"durationMs"`
}

// AlbumDetail is the album-page response with per-track ownership.
type AlbumDetail struct {
	Source          string             `json:"source"`
	ID              string             `json:"id"`
	Name            string             `json:"name"`
	Artist          string             `json:"artist"`
	ArtistID        string             `json:"artistId,omitempty"`
	CoverArtID      string             `json:"coverArtId,omitempty"`
	CoverURL        string             `json:"coverUrl,omitempty"`
	Year            int                `json:"year"`
	LibraryAlbumID  string             `json:"libraryAlbumId,omitempty"`
	OwnedCount      int                `json:"ownedCount"`
	TotalCount      int                `json:"totalCount"`
	Tracks          []AlbumDetailTrack `json:"tracks"`
}
```

Add to `internal/core/external.go` inside `ExternalAlbum`:

```go
	Kind        string `json:"kind,omitempty"`        // "album" | "single"
	TotalTracks int    `json:"totalTracks,omitempty"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/ -run TestAlbumCoverageJSONTags`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core/coverage.go internal/core/external.go internal/core/coverage_test.go
git commit -m "feat(core): add coverage + detail-page domain types"
```

---

### Task 2: Spotify `GetArtistDiscography` (DiscographyProvider)

**Files:**
- Modify: `internal/search/spotify/adapter.go`, `internal/search/spotify/client.go`, `internal/search/spotify/dto.go`
- Test: `internal/search/spotify/adapter_test.go` (add), `internal/search/spotify/testdata/` (add fixture)

**Interfaces:**
- Consumes: `Client.apiGet` (existing), `core.ExternalAlbum` (with new `Kind`/`TotalTracks`).
- Produces: `(*Adapter).GetArtistDiscography(ctx, externalID) ([]core.ExternalAlbum, error)` — satisfies `search.DiscographyProvider`. Returns Albums + Singles (Spotify `include_groups=album,single`), each with `ExternalID`, `Name`, `CoverURL`, `Year`, `Kind`, `TotalTracks` set and empty `Tracks`.

- [ ] **Step 1: Write the failing test** (paginated artist albums; assert mapping + filtering)

```go
// internal/search/spotify/adapter_test.go  (add)
func TestGetArtistDiscographyMapsAndFilters(t *testing.T) {
	page := `{"items":[
	  {"id":"al1","name":"OK Computer","album_type":"album","total_tracks":12,"release_date":"1997-05-21","images":[{"url":"http://img/1"}]},
	  {"id":"s1","name":"Creep","album_type":"single","total_tracks":1,"release_date":"1992-09-21","images":[{"url":"http://img/2"}]}
	],"next":null}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/artists/") && strings.HasSuffix(r.URL.Path, "/albums") {
			_, _ = w.Write([]byte(page)); return
		}
		_, _ = w.Write([]byte(`{"access_token":"t","expires_in":3600}`))
	}))
	defer srv.Close()
	a := New().WithBaseURLs(srv.URL, srv.URL)
	if err := a.Init(map[string]any{"client_id": "x", "client_secret": "y"}); err != nil {
		t.Fatal(err)
	}
	albums, err := a.GetArtistDiscography(context.Background(), "art1")
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) != 2 {
		t.Fatalf("want 2 albums, got %d", len(albums))
	}
	if albums[0].Name != "OK Computer" || albums[0].Kind != "album" || albums[0].TotalTracks != 12 || albums[0].Year != 1997 {
		t.Fatalf("bad album mapping: %+v", albums[0])
	}
	if albums[1].Kind != "single" {
		t.Fatalf("want single, got %q", albums[1].Kind)
	}
}
```

(Imports needed in the test file if not present: `context`, `net/http`, `net/http/httptest`, `strings`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/search/spotify/ -run TestGetArtistDiscographyMapsAndFilters`
Expected: FAIL (undefined: GetArtistDiscography).

- [ ] **Step 3: Add the DTO** (`internal/search/spotify/dto.go`)

```go
// artistAlbumsResponse is /artists/{id}/albums (paged).
type artistAlbumsResponse struct {
	Items []artistAlbumDTO `json:"items"`
	Next  string           `json:"next"`
}

type artistAlbumDTO struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	AlbumType   string     `json:"album_type"`   // "album" | "single" | "compilation"
	TotalTracks int        `json:"total_tracks"`
	ReleaseDate string     `json:"release_date"`
	Images      []imageDTO `json:"images"`
}
```

- [ ] **Step 4: Implement the method** (`internal/search/spotify/adapter.go`)

```go
// Assert the optional capability at compile time.
var _ search.DiscographyProvider = (*Adapter)(nil)

// GetArtistDiscography returns the artist's Albums + Singles/EPs (no tracklists).
// It follows Spotify pagination and asks only for album,single groups so
// compilations and "appears on" never reach the dedup stage.
func (a *Adapter) GetArtistDiscography(ctx context.Context, externalID string) ([]core.ExternalAlbum, error) {
	out := []core.ExternalAlbum{}
	params := url.Values{}
	params.Set("include_groups", "album,single")
	params.Set("limit", "50")
	offset := 0
	for {
		params.Set("offset", strconv.Itoa(offset))
		var resp artistAlbumsResponse
		if err := a.client.apiGet(ctx, "/artists/"+url.PathEscape(externalID)+"/albums", params, &resp); err != nil {
			return nil, err
		}
		for _, it := range resp.Items {
			kind := "album"
			if it.AlbumType == "single" {
				kind = "single"
			}
			out = append(out, core.ExternalAlbum{
				Source:      "spotify",
				ExternalID:  it.ID,
				Name:        it.Name,
				CoverURL:    firstImage(it.Images),
				Year:        yearFromReleaseDate(it.ReleaseDate),
				Kind:        kind,
				TotalTracks: it.TotalTracks,
				Tracks:      []core.ExternalResult{},
			})
		}
		if resp.Next == "" || len(resp.Items) == 0 {
			break
		}
		offset += len(resp.Items)
	}
	return out, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/search/spotify/ -run TestGetArtistDiscographyMapsAndFilters`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/search/spotify/
git commit -m "feat(spotify): implement GetArtistDiscography (albums+singles, paged)"
```

---

### Task 3: Discography canonicalization (dedup)

**Files:**
- Create: `internal/coverage/canonical.go`
- Test: `internal/coverage/canonical_test.go`

**Interfaces:**
- Produces: `coverage.Canonicalize(albums []core.ExternalAlbum) []core.DiscographyAlbum` — groups by normalized title (reusing `matching.Normalize`), picks the standard edition per group (fewest deluxe/remaster/explicit markers; tie → earliest year; tie → most total tracks), sorts result Albums-before-Singles then by year descending.

- [ ] **Step 1: Write the failing test**

```go
// internal/coverage/canonical_test.go
package coverage

import (
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
)

func TestCanonicalizeCollapsesEditions(t *testing.T) {
	in := []core.ExternalAlbum{
		{ExternalID: "a1", Name: "Kid A", Year: 2000, Kind: "album", TotalTracks: 10},
		{ExternalID: "a2", Name: "Kid A (Deluxe Edition)", Year: 2009, Kind: "album", TotalTracks: 22},
		{ExternalID: "a3", Name: "Kid A (Remastered)", Year: 2016, Kind: "album", TotalTracks: 10},
		{ExternalID: "s1", Name: "Creep", Year: 1992, Kind: "single", TotalTracks: 1},
	}
	got := Canonicalize(in)
	if len(got) != 2 {
		t.Fatalf("want 2 canonical releases, got %d: %+v", len(got), got)
	}
	if got[0].Kind != "album" || got[0].ExternalID != "a1" {
		t.Fatalf("want standard Kid A (a1) first, got %+v", got[0])
	}
	if got[1].Kind != "single" {
		t.Fatalf("want single last, got %+v", got[1])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/coverage/ -run TestCanonicalizeCollapsesEditions`
Expected: FAIL (undefined: Canonicalize).

- [ ] **Step 3: Implement**

```go
// internal/coverage/canonical.go
package coverage

import (
	"sort"
	"strings"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/matching"
)

var editionMarkers = []string{"deluxe", "remaster", "remastered", "expanded", "anniversary", "explicit", "deluxe edition", "special edition"}

// editionScore counts edition markers in a title (lower = closer to standard).
func editionScore(name string) int {
	l := strings.ToLower(name)
	n := 0
	for _, m := range editionMarkers {
		if strings.Contains(l, m) {
			n++
		}
	}
	return n
}

// Canonicalize collapses duplicate releases to one standard edition per title and
// returns the artist-page skeleton sorted Albums-first then newest-first.
func Canonicalize(albums []core.ExternalAlbum) []core.DiscographyAlbum {
	type group struct {
		best  core.ExternalAlbum
		score int
	}
	groups := map[string]*group{}
	for _, al := range albums {
		key := al.Kind + "\x00" + matching.Normalize(al.Name)
		sc := editionScore(al.Name)
		g, ok := groups[key]
		if !ok {
			groups[key] = &group{best: al, score: sc}
			continue
		}
		// Prefer fewer markers; tie → earlier year; tie → more tracks.
		better := sc < g.score ||
			(sc == g.score && al.Year > 0 && (g.best.Year == 0 || al.Year < g.best.Year)) ||
			(sc == g.score && al.Year == g.best.Year && al.TotalTracks > g.best.TotalTracks)
		if better {
			g.best, g.score = al, sc
		}
	}
	out := make([]core.DiscographyAlbum, 0, len(groups))
	for _, g := range groups {
		out = append(out, core.DiscographyAlbum{
			Source: g.best.Source, ExternalID: g.best.ExternalID, Name: g.best.Name,
			CoverURL: g.best.CoverURL, Year: g.best.Year, Kind: g.best.Kind, TotalTracks: g.best.TotalTracks,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind == "album" // albums before singles
		}
		return out[i].Year > out[j].Year // newest first
	})
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/coverage/ -run TestCanonicalizeCollapsesEditions`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/coverage/canonical.go internal/coverage/canonical_test.go
git commit -m "feat(coverage): canonicalize discography (collapse editions, sort)"
```

---

### Task 4: Per-album exact-match rollup

**Files:**
- Create: `internal/coverage/rollup.go`
- Test: `internal/coverage/rollup_test.go`

**Interfaces:**
- Consumes: `core.ExternalAlbum` (with tracklist), a `Matcher` interface `Match(ctx, core.ExternalResult) (core.MatchResult, error)` (satisfied by `*matching.Service`).
- Produces: `coverage.RollUp(ctx, m Matcher, al core.ExternalAlbum) (core.AlbumCoverage, error)` — runs each track through the matcher; `full` iff all matched; sets `OwnedCount`/`TotalCount`, `LibraryAlbumID` (the most common matched track's `AlbumID`), and `MissingTracks` (unmatched, as `ExternalTrackRef`). Also `coverage.Matcher` interface declared here.

- [ ] **Step 1: Write the failing test**

```go
// internal/coverage/rollup_test.go
package coverage

import (
	"context"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
)

type fakeMatcher struct{ owned map[string]string } // externalID -> libraryTrackID ("" = miss)

func (f fakeMatcher) Match(_ context.Context, e core.ExternalResult) (core.MatchResult, error) {
	if id, ok := f.owned[e.ExternalID]; ok && id != "" {
		return core.MatchResult{Status: core.MatchInLibrary, LibraryTrackID: id}, nil
	}
	return core.MatchResult{Status: core.MatchNotInLibrary}, nil
}

func album(ids ...string) core.ExternalAlbum {
	al := core.ExternalAlbum{Source: "spotify", ExternalID: "AL", Name: "Kid A"}
	for _, id := range ids {
		al.Tracks = append(al.Tracks, core.ExternalResult{Source: "spotify", ExternalID: id, Title: id, Type: core.EntityTrack})
	}
	return al
}

func TestRollUpPartial(t *testing.T) {
	m := fakeMatcher{owned: map[string]string{"t1": "L1", "t2": "L2", "t3": ""}}
	cov, err := RollUp(context.Background(), m, album("t1", "t2", "t3"))
	if err != nil {
		t.Fatal(err)
	}
	if cov.State != core.CoveragePartial || cov.OwnedCount != 2 || cov.TotalCount != 3 {
		t.Fatalf("bad rollup: %+v", cov)
	}
	if len(cov.MissingTracks) != 1 || cov.MissingTracks[0].ExternalID != "t3" {
		t.Fatalf("want t3 missing, got %+v", cov.MissingTracks)
	}
}

func TestRollUpFull(t *testing.T) {
	m := fakeMatcher{owned: map[string]string{"t1": "L1", "t2": "L2"}}
	cov, _ := RollUp(context.Background(), m, album("t1", "t2"))
	if cov.State != core.CoverageFull || len(cov.MissingTracks) != 0 {
		t.Fatalf("want full/no-missing, got %+v", cov)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/coverage/ -run TestRollUp`
Expected: FAIL (undefined: RollUp).

- [ ] **Step 3: Implement**

```go
// internal/coverage/rollup.go
package coverage

import (
	"context"

	"github.com/maxjb-xyz/reverb/internal/core"
)

// Matcher is the slice of *matching.Service the rollup needs.
type Matcher interface {
	Match(ctx context.Context, ext core.ExternalResult) (core.MatchResult, error)
}

// RollUp computes exact coverage for one album. full iff every track matches.
func RollUp(ctx context.Context, m Matcher, al core.ExternalAlbum) (core.AlbumCoverage, error) {
	cov := core.AlbumCoverage{
		Source: al.Source, ExternalAlbumID: al.ExternalID,
		TotalCount: len(al.Tracks), MissingTracks: []core.ExternalTrackRef{},
	}
	albumVotes := map[string]int{}
	for _, tr := range al.Tracks {
		res, err := m.Match(ctx, tr)
		if err != nil {
			return core.AlbumCoverage{}, err
		}
		if res.Status == core.MatchInLibrary && res.LibraryTrackID != "" {
			cov.OwnedCount++
			// LibraryTrackID's owning album is unknown here; the service backfills
			// LibraryAlbumID from library lookups. Record the track id as a vote key.
			albumVotes[res.LibraryTrackID]++
		} else {
			cov.MissingTracks = append(cov.MissingTracks, core.ExternalTrackRef{
				Source: tr.Source, ExternalID: tr.ExternalID, Title: tr.Title,
				Artist: tr.Artist, Album: al.Name, ISRC: tr.ISRC, DurationMs: tr.DurationMs,
			})
		}
	}
	switch {
	case cov.TotalCount > 0 && cov.OwnedCount == cov.TotalCount:
		cov.State = core.CoverageFull
	case cov.OwnedCount == 0:
		cov.State = core.CoverageNone
	default:
		cov.State = core.CoveragePartial
	}
	return cov, nil
}
```

> Note: `LibraryAlbumID` is resolved by the service (Task 6) from a matched track,
> because `MatchResult` carries only the track id. The rollup stays library-agnostic.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/coverage/ -run TestRollUp`
Expected: PASS (both subtests).

- [ ] **Step 5: Commit**

```bash
git add internal/coverage/rollup.go internal/coverage/rollup_test.go
git commit -m "feat(coverage): exact per-album match rollup"
```

---

### Task 5: Coverage cache tables (migration + sqlc)

**Files:**
- Create: `internal/store/migrations/0004_coverage.sql`, `internal/store/queries/coverage.sql`
- Generated: `internal/store/db/coverage.sql.go` (via `sqlc generate`)
- Test: `internal/store/store_test.go` (add a round-trip smoke test)

**Interfaces:**
- Produces (generated): `db.GetArtistExternalMap`, `db.UpsertArtistExternalMap`, `db.GetDiscographyCache`, `db.UpsertDiscographyCache`, `db.GetAlbumCoverage`, `db.UpsertAlbumCoverage`, `db.DeleteAlbumCoverage` with `db.*Params` structs.

- [ ] **Step 1: Write the migration**

```sql
-- internal/store/migrations/0004_coverage.sql
-- +goose Up
CREATE TABLE artist_external_map (
  library_artist_id  TEXT NOT NULL,
  source             TEXT NOT NULL,
  external_artist_id TEXT NOT NULL,
  confidence         REAL NOT NULL,
  created_at         INTEGER NOT NULL,
  PRIMARY KEY (library_artist_id, source)
);
CREATE TABLE discography_cache (
  source             TEXT NOT NULL,
  external_artist_id TEXT NOT NULL,
  albums_json        TEXT NOT NULL,
  fetched_at         INTEGER NOT NULL,
  PRIMARY KEY (source, external_artist_id)
);
CREATE TABLE album_coverage (
  source           TEXT NOT NULL,
  external_album_id TEXT NOT NULL,
  coverage_json    TEXT NOT NULL,
  library_album_id TEXT NOT NULL DEFAULT '',
  fetched_at       INTEGER NOT NULL,
  PRIMARY KEY (source, external_album_id)
);

-- +goose Down
DROP TABLE album_coverage;
DROP TABLE discography_cache;
DROP TABLE artist_external_map;
```

- [ ] **Step 2: Write the queries**

```sql
-- internal/store/queries/coverage.sql
-- name: GetArtistExternalMap :one
SELECT * FROM artist_external_map WHERE library_artist_id = ? AND source = ?;

-- name: UpsertArtistExternalMap :exec
INSERT INTO artist_external_map (library_artist_id, source, external_artist_id, confidence, created_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(library_artist_id, source) DO UPDATE SET
  external_artist_id = excluded.external_artist_id, confidence = excluded.confidence;

-- name: GetDiscographyCache :one
SELECT * FROM discography_cache WHERE source = ? AND external_artist_id = ?;

-- name: UpsertDiscographyCache :exec
INSERT INTO discography_cache (source, external_artist_id, albums_json, fetched_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(source, external_artist_id) DO UPDATE SET
  albums_json = excluded.albums_json, fetched_at = excluded.fetched_at;

-- name: GetAlbumCoverage :one
SELECT * FROM album_coverage WHERE source = ? AND external_album_id = ?;

-- name: UpsertAlbumCoverage :exec
INSERT INTO album_coverage (source, external_album_id, coverage_json, library_album_id, fetched_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(source, external_album_id) DO UPDATE SET
  coverage_json = excluded.coverage_json, library_album_id = excluded.library_album_id, fetched_at = excluded.fetched_at;

-- name: DeleteAlbumCoverageForLibraryAlbum :exec
DELETE FROM album_coverage WHERE library_album_id = ?;
```

- [ ] **Step 3: Generate typed code**

Run: `sqlc generate`
Expected: `internal/store/db/coverage.sql.go` created; `go build ./...` still compiles.

- [ ] **Step 4: Write a round-trip test**

```go
// internal/store/store_test.go  (add)
func TestAlbumCoverageRoundTrip(t *testing.T) {
	st := openTestStore(t) // use the existing helper in this file
	q := st.Q()
	ctx := context.Background()
	if err := q.UpsertAlbumCoverage(ctx, db.UpsertAlbumCoverageParams{
		Source: "spotify", ExternalAlbumID: "AL", CoverageJson: `{"state":"full"}`,
		LibraryAlbumID: "L1", FetchedAt: 123,
	}); err != nil {
		t.Fatal(err)
	}
	row, err := q.GetAlbumCoverage(ctx, db.GetAlbumCoverageParams{Source: "spotify", ExternalAlbumID: "AL"})
	if err != nil || row.CoverageJson != `{"state":"full"}` || row.LibraryAlbumID != "L1" {
		t.Fatalf("round-trip failed: %+v err=%v", row, err)
	}
}
```

> If `openTestStore` doesn't exist, mirror the construction used by the other tests
> in `store_test.go` (Open a temp-file DB, call `Migrate()`).

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestAlbumCoverageRoundTrip`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/store/migrations/0004_coverage.sql internal/store/queries/coverage.sql internal/store/db/coverage.sql.go internal/store/store_test.go
git commit -m "feat(store): coverage cache tables (artist map, discography, album coverage)"
```

---

### Task 6: Coverage service (resolve + orchestrate + cache + stream)

**Files:**
- Create: `internal/coverage/resolve.go`, `internal/coverage/service.go`
- Test: `internal/coverage/service_test.go`

**Interfaces:**
- Consumes: `Matcher` (Task 4); `DiscoSource` (`Search`, `GetAlbum`, `GetArtistDiscography` — `*spotify.Adapter` satisfies it); `LibraryArtist` (`GetArtist(ctx,id)(core.Artist,error)`, `GetAlbum(ctx,id)(core.Album,error)` — `LibraryAdapter` satisfies it); `CoverageCache` (the `db.Queries` slice from Task 5); a `nowFn func() int64`.
- Produces:
  - `coverage.NewService(...) *Service`
  - `(*Service).ArtistDetail(ctx, source, id string) (core.ArtistDetail, error)` — resolves + returns deduped skeleton (warm from `discography_cache`); degrades to library albums when unresolved.
  - `(*Service).StreamCoverage(ctx, source, id string) <-chan core.AlbumCoverage` — emits per-album coverage (cache-first, else compute+cache), closes when done.
  - `(*Service).AlbumDetail(ctx, source, id string) (core.AlbumDetail, error)` — per-track ownership.
  - `(*Service).InvalidateLibraryAlbum(ctx, libraryAlbumID string) error` — deletes `album_coverage` rows.
  - `ResolveArtist(ctx, src DiscoSource, lib LibraryArtist, cache CoverageCache, now func() int64, libraryArtistID string) (extID string, conf float64, err error)`.

- [ ] **Step 1: Write the failing test** (resolution caches; streaming yields cached-first)

```go
// internal/coverage/service_test.go
package coverage

import (
	"context"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
)

type fakeDisco struct {
	artists []core.ExternalResult
	albums  map[string]core.ExternalAlbum // externalAlbumID -> full album (with tracks)
	disco   []core.ExternalAlbum
}

func (f fakeDisco) Search(_ context.Context, q string, t core.EntityType) ([]core.ExternalResult, error) {
	if t == core.EntityArtist {
		return f.artists, nil
	}
	return nil, nil
}
func (f fakeDisco) GetAlbum(_ context.Context, id string) (core.ExternalAlbum, error) { return f.albums[id], nil }
func (f fakeDisco) GetArtistDiscography(_ context.Context, _ string) ([]core.ExternalAlbum, error) {
	return f.disco, nil
}

func TestStreamCoverageComputesPerAlbum(t *testing.T) {
	disco := fakeDisco{
		artists: []core.ExternalResult{{Source: "spotify", ExternalID: "art1", Title: "Radiohead", Type: core.EntityArtist}},
		disco:   []core.ExternalAlbum{{Source: "spotify", ExternalID: "AL", Name: "Kid A", Kind: "album", TotalTracks: 2}},
		albums: map[string]core.ExternalAlbum{"AL": album("t1", "t2")},
	}
	m := fakeMatcher{owned: map[string]string{"t1": "L1"}} // t2 missing → partial
	svc := NewService(disco, m, fakeLibrary{}, newMemCache(), func() int64 { return 1 })
	ch := svc.StreamCoverage(context.Background(), "library", "libArtist1")
	var got []core.AlbumCoverage
	for c := range ch {
		got = append(got, c)
	}
	if len(got) != 1 || got[0].State != core.CoveragePartial || got[0].OwnedCount != 1 {
		t.Fatalf("bad stream: %+v", got)
	}
}
```

> `fakeLibrary` implements `GetArtist`→`core.Artist{ID:"libArtist1",Name:"Radiohead"}`
> and `GetAlbum`. `newMemCache()` is an in-memory `CoverageCache` test double whose
> methods satisfy the interface (map-backed). Write both at the top of the test file.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/coverage/ -run TestStreamCoverageComputesPerAlbum`
Expected: FAIL (undefined: NewService).

- [ ] **Step 3: Implement `resolve.go`**

```go
// internal/coverage/resolve.go
package coverage

import (
	"context"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/matching"
)

const resolveConfidenceFloor = 0.6

// ResolveArtist maps a library artist to an external artist id, caching the result.
// Returns ("",0,nil) when no confident match (caller degrades to library-only).
func ResolveArtist(ctx context.Context, src DiscoSource, lib LibraryArtist, cache CoverageCache, now func() int64, libraryArtistID string) (string, float64, error) {
	if row, err := cache.GetArtistExternalMap(ctx, libraryArtistID, "spotify"); err == nil && row.ExternalArtistID != "" {
		return row.ExternalArtistID, row.Confidence, nil
	}
	art, err := lib.GetArtist(ctx, libraryArtistID)
	if err != nil {
		return "", 0, err
	}
	cands, err := src.Search(ctx, art.Name, core.EntityArtist)
	if err != nil || len(cands) == 0 {
		return "", 0, err
	}
	want := matching.Normalize(art.Name)
	for _, c := range cands {
		if matching.Normalize(c.Title) == want {
			_ = cache.UpsertArtistExternalMap(ctx, libraryArtistID, "spotify", c.ExternalID, 1.0, now())
			return c.ExternalID, 1.0, nil
		}
	}
	// Fall back to the top result if "close enough"; here Spotify already ranks by
	// relevance, so accept the first candidate above the floor (heuristic 0.7).
	top := cands[0]
	_ = cache.UpsertArtistExternalMap(ctx, libraryArtistID, "spotify", top.ExternalID, 0.7, now())
	return top.ExternalID, 0.7, nil
}
```

- [ ] **Step 4: Implement `service.go`**

```go
// internal/coverage/service.go
package coverage

import (
	"context"
	"encoding/json"

	"github.com/maxjb-xyz/reverb/internal/core"
)

// DiscoSource is the external source used for discography + resolution.
type DiscoSource interface {
	Search(ctx context.Context, q string, t core.EntityType) ([]core.ExternalResult, error)
	GetAlbum(ctx context.Context, externalID string) (core.ExternalAlbum, error)
	GetArtistDiscography(ctx context.Context, externalID string) ([]core.ExternalAlbum, error)
}

// LibraryArtist is the library slice the service needs.
type LibraryArtist interface {
	GetArtist(ctx context.Context, id string) (core.Artist, error)
	GetAlbum(ctx context.Context, id string) (core.Album, error)
}

// CoverageCache is the persistence slice (satisfied by a thin wrapper over *db.Queries).
type CoverageCache interface {
	GetArtistExternalMap(ctx context.Context, libraryArtistID, source string) (ArtistMapRow, error)
	UpsertArtistExternalMap(ctx context.Context, libraryArtistID, source, externalID string, confidence float64, now int64) error
	GetDiscographyCache(ctx context.Context, source, externalArtistID string) (DiscoRow, error)
	UpsertDiscographyCache(ctx context.Context, source, externalArtistID, albumsJSON string, now int64) error
	GetAlbumCoverage(ctx context.Context, source, externalAlbumID string) (CoverageRow, error)
	UpsertAlbumCoverage(ctx context.Context, source, externalAlbumID, coverageJSON, libraryAlbumID string, now int64) error
	DeleteAlbumCoverageForLibraryAlbum(ctx context.Context, libraryAlbumID string) error
}

type ArtistMapRow struct{ ExternalArtistID string; Confidence float64 }
type DiscoRow struct{ AlbumsJSON string }
type CoverageRow struct{ CoverageJSON, LibraryAlbumID string; Found bool }

type Service struct {
	src   DiscoSource
	match Matcher
	lib   LibraryArtist
	cache CoverageCache
	now   func() int64
}

func NewService(src DiscoSource, m Matcher, lib LibraryArtist, cache CoverageCache, now func() int64) *Service {
	return &Service{src: src, match: m, lib: lib, cache: cache, now: now}
}

// ArtistDetail returns the page skeleton. source is "library" or "spotify".
func (s *Service) ArtistDetail(ctx context.Context, source, id string) (core.ArtistDetail, error) {
	extID, libArtistID := "", ""
	det := core.ArtistDetail{Source: source, ID: id}
	if source == "library" {
		libArtistID = id
		art, err := s.lib.GetArtist(ctx, id)
		if err != nil {
			return det, err
		}
		det.Name, det.CoverArtID, det.LibraryArtistID = art.Name, art.CoverArtID, id
		extID, _, _ = ResolveArtist(ctx, s.src, s.lib, s.cache, s.now, id)
	} else {
		extID = id
	}
	if extID == "" {
		// Degrade: show library-owned albums as full.
		det.Resolved = false
		det.Albums = s.libraryAlbumsAsSkeleton(ctx, libArtistID)
		return det, nil
	}
	det.Resolved = true
	det.ExternalArtistID = extID
	albums, err := s.discography(ctx, extID)
	if err != nil {
		return det, err
	}
	det.Albums = Canonicalize(albums)
	if det.Name == "" && len(albums) > 0 {
		det.Name = albums[0].Artist
	}
	return det, nil
}

// discography is cache-first.
func (s *Service) discography(ctx context.Context, extID string) ([]core.ExternalAlbum, error) {
	if row, err := s.cache.GetDiscographyCache(ctx, "spotify", extID); err == nil && row.AlbumsJSON != "" {
		var cached []core.ExternalAlbum
		if json.Unmarshal([]byte(row.AlbumsJSON), &cached) == nil {
			return cached, nil
		}
	}
	albums, err := s.src.GetArtistDiscography(ctx, extID)
	if err != nil {
		return nil, err
	}
	if b, mErr := json.Marshal(albums); mErr == nil {
		_ = s.cache.UpsertDiscographyCache(ctx, "spotify", extID, string(b), s.now())
	}
	return albums, nil
}

func (s *Service) libraryAlbumsAsSkeleton(ctx context.Context, libArtistID string) []core.DiscographyAlbum {
	out := []core.DiscographyAlbum{}
	if libArtistID == "" {
		return out
	}
	art, err := s.lib.GetArtist(ctx, libArtistID)
	if err != nil {
		return out
	}
	for _, al := range art.Albums {
		out = append(out, core.DiscographyAlbum{
			Source: "library", ExternalID: al.ID, Name: al.Name, Year: al.Year,
			Kind: "album", TotalTracks: al.SongCount,
		})
	}
	return out
}

// StreamCoverage emits one AlbumCoverage per canonical album (cache-first).
func (s *Service) StreamCoverage(ctx context.Context, source, id string) <-chan core.AlbumCoverage {
	out := make(chan core.AlbumCoverage)
	go func() {
		defer close(out)
		det, err := s.ArtistDetail(ctx, source, id)
		if err != nil || !det.Resolved {
			return
		}
		for _, da := range det.Albums {
			cov, cErr := s.coverageForAlbum(ctx, da.ExternalID)
			if cErr != nil {
				cov = core.AlbumCoverage{Source: "spotify", ExternalAlbumID: da.ExternalID, State: core.CoverageNone, MissingTracks: []core.ExternalTrackRef{}}
			}
			select {
			case <-ctx.Done():
				return
			case out <- cov:
			}
		}
	}()
	return out
}

func (s *Service) coverageForAlbum(ctx context.Context, extAlbumID string) (core.AlbumCoverage, error) {
	if row, err := s.cache.GetAlbumCoverage(ctx, "spotify", extAlbumID); err == nil && row.Found {
		var cov core.AlbumCoverage
		if json.Unmarshal([]byte(row.CoverageJSON), &cov) == nil {
			return cov, nil
		}
	}
	full, err := s.src.GetAlbum(ctx, extAlbumID)
	if err != nil {
		return core.AlbumCoverage{}, err
	}
	cov, err := RollUp(ctx, s.match, full)
	if err != nil {
		return core.AlbumCoverage{}, err
	}
	cov.LibraryAlbumID = s.backfillLibraryAlbumID(ctx, cov)
	if b, mErr := json.Marshal(cov); mErr == nil {
		_ = s.cache.UpsertAlbumCoverage(ctx, "spotify", extAlbumID, string(b), cov.LibraryAlbumID, s.now())
	}
	return cov, nil
}

// backfillLibraryAlbumID returns the owning library album of the first matched
// track (so a click on a partial/full album opens the local album).
func (s *Service) backfillLibraryAlbumID(ctx context.Context, cov core.AlbumCoverage) string {
	if cov.OwnedCount == 0 {
		return ""
	}
	// We don't carry matched track ids out of RollUp; recompute cheaply via the
	// library album of any owned track is overkill — instead the service stores the
	// linkage when known. For MVP, leave empty unless the rollup is extended; the
	// client falls back to the external album view. (See Task 8 for album-page play.)
	return ""
}

// InvalidateLibraryAlbum drops cached coverage rows for a library album id.
func (s *Service) InvalidateLibraryAlbum(ctx context.Context, libraryAlbumID string) error {
	return s.cache.DeleteAlbumCoverageForLibraryAlbum(ctx, libraryAlbumID)
}
```

> **Decision recorded:** `backfillLibraryAlbumID` is left as a no-op seam in this
> task to keep `RollUp` library-agnostic. Task 8 (AlbumDetail) is the surface that
> needs real library track ids, and it gets them directly from per-track matching.
> If artist-page "click partial album → open *library* album" is desired, extend
> `RollUp` to return matched `LibraryTrackID`s and resolve their `AlbumID` via
> `lib.GetAlbum` here — tracked as a follow-up, not blocking.

- [ ] **Step 5: Implement `AlbumDetail`** (append to `service.go`)

```go
// AlbumDetail returns per-track ownership for an album. source "library" returns
// all-owned; "spotify" matches each external track against the library.
func (s *Service) AlbumDetail(ctx context.Context, source, id string) (core.AlbumDetail, error) {
	if source == "library" {
		al, err := s.lib.GetAlbum(ctx, id)
		if err != nil {
			return core.AlbumDetail{}, err
		}
		det := core.AlbumDetail{
			Source: "library", ID: al.ID, Name: al.Name, Artist: al.Artist, ArtistID: al.ArtistID,
			CoverArtID: al.CoverArtID, Year: al.Year, LibraryAlbumID: al.ID,
			OwnedCount: len(al.Tracks), TotalCount: len(al.Tracks),
		}
		for _, t := range al.Tracks {
			tt := t
			det.Tracks = append(det.Tracks, core.AlbumDetailTrack{
				State: core.CoverageFull, LibraryTrack: &tt, Title: t.Title, Artist: t.Artist,
				TrackNumber: t.TrackNumber, DurationMs: t.DurationMs,
			})
		}
		return det, nil
	}
	full, err := s.src.GetAlbum(ctx, id)
	if err != nil {
		return core.AlbumDetail{}, err
	}
	det := core.AlbumDetail{
		Source: "spotify", ID: full.ExternalID, Name: full.Name, Artist: full.Artist,
		CoverURL: full.CoverURL, Year: full.Year, TotalCount: len(full.Tracks),
	}
	for i, tr := range full.Tracks {
		res, mErr := s.match.Match(ctx, tr)
		if mErr != nil {
			return core.AlbumDetail{}, mErr
		}
		dt := core.AlbumDetailTrack{Title: tr.Title, Artist: tr.Artist, TrackNumber: i + 1, DurationMs: tr.DurationMs}
		if res.Status == core.MatchInLibrary && res.LibraryTrackID != "" {
			det.OwnedCount++
			dt.State = core.CoverageFull
			dt.LibraryTrack = &core.Track{ID: res.LibraryTrackID, Title: tr.Title, Artist: tr.Artist, DurationMs: tr.DurationMs}
		} else {
			dt.State = core.CoverageNone
			ref := core.ExternalTrackRef{Source: tr.Source, ExternalID: tr.ExternalID, Title: tr.Title, Artist: tr.Artist, Album: full.Name, ISRC: tr.ISRC, DurationMs: tr.DurationMs}
			dt.ExternalRef = &ref
		}
		det.Tracks = append(det.Tracks, dt)
	}
	return det, nil
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/coverage/`
Expected: PASS (all subtests). Fix the in-test fakes until green.

- [ ] **Step 7: Commit**

```bash
git add internal/coverage/resolve.go internal/coverage/service.go internal/coverage/service_test.go
git commit -m "feat(coverage): resolve + orchestrate + cache + stream service"
```

---

### Task 7: Library `GetPlaylist(id)` (interface + subsonic + conformance)

**Files:**
- Modify: `internal/library/library.go`, `internal/library/subsonic/adapter.go`, `internal/library/conformance.go`
- Test: `internal/library/subsonic/playlist_test.go` (add)

**Interfaces:**
- Produces: `LibraryAdapter.GetPlaylist(ctx, id string) (core.Playlist, error)` returning the playlist with `Tracks` populated (Subsonic `getPlaylist.view`).

- [ ] **Step 1: Add the failing test** mirroring the existing `GetPlaylists` test in `playlist_test.go` but hitting `getPlaylist.view` and asserting `Tracks` is populated. (Model it on the existing fixtures in `internal/library/subsonic/testdata/`.)

- [ ] **Step 2: Run** `go test ./internal/library/subsonic/ -run GetPlaylist` → FAIL (undefined).

- [ ] **Step 3: Add to the interface** (`internal/library/library.go`), after `GetPlaylists`:

```go
	// GetPlaylist returns one playlist with its tracks populated.
	GetPlaylist(ctx context.Context, id string) (core.Playlist, error)
```

- [ ] **Step 4: Implement on the subsonic adapter** mirroring `GetAlbum`/`GetPlaylists` (call `getPlaylist.view?id=`, map entries → `core.Track`, set `Tracks`, `SongCount`, `DurationMs`). Add the conformance assertion in `internal/library/conformance.go` if it enumerates required methods.

- [ ] **Step 5: Run** `go test ./internal/library/...` → PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/library/
git commit -m "feat(library): GetPlaylist(id) with tracks (subsonic)"
```

---

### Task 8: API handlers — artist/album/coverage/playlist/batch + routes + wiring

**Files:**
- Create: `internal/api/coverage.go`
- Modify: `internal/api/server.go` (routes), `internal/api/library.go` (playlist detail handler may live here), `internal/wiring/*.go` (build coverage service; add to `Deps`), `internal/api/server.go` `Deps`/`Server` (hold `Coverage`).
- Test: `internal/api/coverage_test.go`

**Interfaces:**
- Consumes: `coverage.Service` methods (Task 6); `DownloadManager.Enqueue` (existing).
- Produces routes:
  - `GET /api/v1/artist/{source}/{id}` → `core.ArtistDetail`
  - `GET /api/v1/artist/{source}/{id}/coverage` → SSE of `core.AlbumCoverage`
  - `GET /api/v1/album/{source}/{id}` → `core.AlbumDetail`
  - `GET /api/v1/library/playlist/{id}` → `core.Playlist`
  - `POST /api/v1/downloads/batch` → `[]core.DownloadJob`

- [ ] **Step 1: Write a failing handler test** for the artist detail + SSE + batch (use the existing `testhelpers_test.go` server harness; inject a fake coverage service via a small interface).

Define the API-side interface in `coverage.go`:

```go
// CoverageService is the slice of *coverage.Service the API needs.
type CoverageService interface {
	ArtistDetail(ctx context.Context, source, id string) (core.ArtistDetail, error)
	StreamCoverage(ctx context.Context, source, id string) <-chan core.AlbumCoverage
	AlbumDetail(ctx context.Context, source, id string) (core.AlbumDetail, error)
}
```

Test asserts: `GET /api/v1/artist/library/abc` returns 200 + JSON with `albums`; the SSE endpoint writes `data: {...}\n\n` frames; `POST /downloads/batch` with two refs calls `Enqueue` twice and returns two jobs.

- [ ] **Step 2: Run** `go test ./internal/api/ -run Coverage` → FAIL.

- [ ] **Step 3: Implement handlers** (`internal/api/coverage.go`)

```go
package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/maxjb-xyz/reverb/internal/core"
)

func (s *Server) coverage() CoverageService {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.live.coverage
}

func (s *Server) handleArtistDetail(w http.ResponseWriter, r *http.Request) {
	cov := s.coverage()
	if cov == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "coverage unavailable"})
		return
	}
	det, err := cov.ArtistDetail(r.Context(), chi.URLParam(r, "source"), chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, det)
}

func (s *Server) handleArtistCoverage(w http.ResponseWriter, r *http.Request) {
	cov := s.coverage()
	flusher, ok := w.(http.Flusher)
	if cov == nil || !ok {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "coverage stream unavailable"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	ch := cov.StreamCoverage(r.Context(), chi.URLParam(r, "source"), chi.URLParam(r, "id"))
	for {
		select {
		case <-r.Context().Done():
			return
		case c, open := <-ch:
			if !open {
				return
			}
			b, err := json.Marshal(c)
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

func (s *Server) handleAlbumDetail(w http.ResponseWriter, r *http.Request) {
	cov := s.coverage()
	if cov == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "coverage unavailable"})
		return
	}
	det, err := cov.AlbumDetail(r.Context(), chi.URLParam(r, "source"), chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, det)
}

func (s *Server) handleLibraryPlaylist(w http.ResponseWriter, r *http.Request) {
	lib, ok := s.libraryReady(w)
	if !ok {
		return
	}
	pl, err := lib.GetPlaylist(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, pl)
}

type batchDownloadBody struct {
	Tracks []core.ExternalTrackRef `json:"tracks"`
}

func (s *Server) handleBatchDownload(w http.ResponseWriter, r *http.Request) {
	dl := s.downloads()
	if dl == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no downloader configured"})
		return
	}
	var body batchDownloadBody
	if err := decode(r, &body); err != nil || len(body.Tracks) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tracks is required"})
		return
	}
	jobs := []core.DownloadJob{}
	for _, t := range body.Tracks {
		job, err := dl.Enqueue(r.Context(), core.DownloadRequest{
			Source: t.Source, ExternalID: t.ExternalID, Artist: t.Artist,
			Title: t.Title, Album: t.Album, ISRC: t.ISRC, DurationMs: t.DurationMs,
		})
		if err != nil {
			continue // dedup-join / per-item failure shouldn't abort the batch
		}
		jobs = append(jobs, job)
	}
	writeJSON(w, http.StatusOK, jobs)
}

// LibraryGetPlaylist is the slice added to the library adapter interface (Task 7).
var _ = context.Background
```

- [ ] **Step 4: Add `coverage CoverageService` to `Server.live` and `Deps`** in `server.go`, set it in `NewServer`, add it to the reload swap (if the reloader rebuilds it), and register routes inside the protected group:

```go
	pr.Get("/artist/{source}/{id}", s.handleArtistDetail)
	pr.Get("/artist/{source}/{id}/coverage", s.handleArtistCoverage)
	pr.Get("/album/{source}/{id}", s.handleAlbumDetail)
	pr.Get("/library/playlist/{id}", s.handleLibraryPlaylist)
	pr.Post("/downloads/batch", s.handleBatchDownload)
```

- [ ] **Step 5: Wire the coverage service** in `internal/wiring/*.go`: build a `CoverageCache` adapter over `*db.Queries` (a small struct mapping the interface methods in Task 6 to the generated `db.*` calls and translating `sql.ErrNoRows`→`Found:false`), pick the first enabled search adapter implementing `coverage.DiscoSource`, pass `matching.Service`, the library adapter, and a `nowFn` (use the existing clock pattern). Hand the constructed `*coverage.Service` to `api.Deps`. Subscribe to `library.updated` and call `InvalidateLibraryAlbum` for each `AlbumID`.

- [ ] **Step 6: Run** `go test ./internal/api/ ./internal/wiring/...` → PASS. Then `go build ./...`.

- [ ] **Step 7: Commit**

```bash
git add internal/api/ internal/wiring/ internal/store/
git commit -m "feat(api): artist/album/coverage/playlist/batch endpoints + coverage wiring"
```

---

### Task 9: OpenAPI + backend phase verification

**Files:** Modify `internal/api/openapi.yaml` (document the 5 new routes). 

- [ ] **Step 1:** Add path entries for the 5 endpoints (mirror existing style).
- [ ] **Step 2: Run the whole backend suite** `go test ./...` → PASS.
- [ ] **Step 3: Commit** `git commit -am "docs(openapi): document detail-page + coverage routes"`.

---

# Phase 2 — Frontend core

### Task 10: FE types + coverage API client

**Files:**
- Modify: `web/src/lib/types.ts`, `web/src/lib/downloadApi.ts`
- Create: `web/src/lib/coverageApi.ts`
- Test: `web/src/lib/coverageApi.test.tsx`

**Interfaces:**
- Produces TS types mirroring Task 1 (`CoverageState`, `ExternalTrackRef`, `AlbumCoverage`, `DiscographyAlbum`, `ArtistDetail`, `AlbumDetailTrack`, `AlbumDetail`); hooks `useArtistDetail(source,id)`, `useAlbumDetail(source,id)`, `usePlaylistDetail(id)`; `postBatchDownload(tracks: ExternalTrackRef[])`.

- [ ] **Step 1:** Add the TS interfaces to `types.ts` (exact mirror of the Go JSON):

```ts
export type CoverageState = 'pending' | 'none' | 'partial' | 'full'

export interface ExternalTrackRef {
  source: string
  externalId: string
  title: string
  artist?: string
  album?: string
  isrc?: string
  durationMs: number
}

export interface AlbumCoverage {
  source: string
  externalAlbumId: string
  state: CoverageState
  ownedCount: number
  totalCount: number
  libraryAlbumId?: string
  missingTracks: ExternalTrackRef[]
}

export interface DiscographyAlbum {
  source: string
  externalId: string
  name: string
  coverUrl?: string
  year: number
  kind: 'album' | 'single'
  totalTracks: number
}

export interface ArtistDetail {
  source: string
  id: string
  name: string
  coverArtId?: string
  coverUrl?: string
  libraryArtistId?: string
  externalArtistId?: string
  resolved: boolean
  albums: DiscographyAlbum[]
}

export interface AlbumDetailTrack {
  state: CoverageState
  libraryTrack?: Track
  externalRef?: ExternalTrackRef
  title: string
  artist: string
  trackNumber: number
  durationMs: number
}

export interface AlbumDetail {
  source: string
  id: string
  name: string
  artist: string
  artistId?: string
  coverArtId?: string
  coverUrl?: string
  year: number
  libraryAlbumId?: string
  ownedCount: number
  totalCount: number
  tracks: AlbumDetailTrack[]
}
```

- [ ] **Step 2: Write the failing test** for `useArtistDetail` + `postBatchDownload` (mock `api`, mirror `libraryApi.test.tsx`).

- [ ] **Step 3: Implement** `coverageApi.ts`:

```ts
import { useQuery } from '@tanstack/react-query'
import { api } from './api'
import type { AlbumDetail, ArtistDetail, Playlist } from './types'

export function useArtistDetail(source: string, id: string) {
  return useQuery({
    queryKey: ['artist-detail', source, id],
    queryFn: () => api.get<ArtistDetail>(`/artist/${encodeURIComponent(source)}/${encodeURIComponent(id)}`),
    enabled: !!source && !!id,
  })
}

export function useAlbumDetail(source: string, id: string) {
  return useQuery({
    queryKey: ['album-detail', source, id],
    queryFn: () => api.get<AlbumDetail>(`/album/${encodeURIComponent(source)}/${encodeURIComponent(id)}`),
    enabled: !!source && !!id,
  })
}

export function usePlaylistDetail(id: string) {
  return useQuery({
    queryKey: ['playlist-detail', id],
    queryFn: () => api.get<Playlist>(`/library/playlist/${encodeURIComponent(id)}`),
    enabled: !!id,
  })
}
```

Add to `downloadApi.ts`:

```ts
import type { ExternalTrackRef } from './types'

export function postBatchDownload(tracks: ExternalTrackRef[]): Promise<DownloadJob[]> {
  return api.post<DownloadJob[]>('/downloads/batch', { tracks })
}

export function reqFromExternalRef(t: ExternalTrackRef): CreateDownloadReq {
  return { source: t.source, externalId: t.externalId, artist: t.artist ?? '', title: t.title, album: t.album ?? '', isrc: t.isrc, durationMs: t.durationMs }
}
```

- [ ] **Step 4: Run** `npx vitest run src/lib/coverageApi.test.tsx` → PASS.
- [ ] **Step 5: Commit** `git commit -am "feat(web): coverage/detail types + api client"`.

---

### Task 11: `CoverageChip` component

**Files:**
- Create: `web/src/components/ui/CoverageChip.tsx`, add export to `web/src/components/ui/index.ts`
- Test: `web/src/components/ui/CoverageChip.test.tsx`

**Interfaces:**
- Produces: `<CoverageChip state owned total />` — `full`→accent check chip; `partial`→accent `ProgressRing` (`value = owned/total*100`, size 16) + `owned/total`; `pending`→indeterminate ring; `none`→renders `null` (clean at rest).

- [ ] **Step 1: Write failing test**

```tsx
// web/src/components/ui/CoverageChip.test.tsx
import { render } from '@testing-library/react'
import { CoverageChip } from './CoverageChip'

test('full renders a check, none renders nothing', () => {
  const { container, rerender } = render(<CoverageChip state="full" owned={10} total={10} />)
  expect(container.querySelector('[data-testid="coverage-full"]')).toBeTruthy()
  rerender(<CoverageChip state="none" owned={0} total={12} />)
  expect(container.firstChild).toBeNull()
})

test('partial shows owned/total', () => {
  const { getByText } = render(<CoverageChip state="partial" owned={7} total={10} />)
  expect(getByText('7/10')).toBeTruthy()
})
```

- [ ] **Step 2: Run** `npx vitest run src/components/ui/CoverageChip.test.tsx` → FAIL.

- [ ] **Step 3: Implement**

```tsx
// web/src/components/ui/CoverageChip.tsx
import { ProgressRing } from './ProgressRing'
import { Icon } from './Icon'
import type { CoverageState } from '../../lib/types'

interface Props {
  state: CoverageState
  owned: number
  total: number
}

export function CoverageChip({ state, owned, total }: Props) {
  if (state === 'none') return null
  const base = 'inline-flex items-center gap-1 rounded-full bg-black/60 backdrop-blur px-2 h-6 text-[11px] font-extrabold'
  if (state === 'full') {
    return (
      <span data-testid="coverage-full" className={`${base} text-accent`}>
        <Icon name="check" className="text-xs" />
      </span>
    )
  }
  if (state === 'pending') {
    return (
      <span className={`${base} text-text-muted`} aria-label="Checking library">
        <ProgressRing value={0} size={14} indeterminate />
      </span>
    )
  }
  // partial
  const pct = total > 0 ? Math.round((owned / total) * 100) : 0
  return (
    <span className={`${base} text-white`}>
      <ProgressRing value={pct} size={14} />
      {owned}/{total}
    </span>
  )
}
```

- [ ] **Step 4: Run** test → PASS. Add `export { CoverageChip } from './CoverageChip'` to `index.ts`.
- [ ] **Step 5: Commit** `git commit -am "feat(web): CoverageChip (full/partial/pending/none)"`.

---

### Task 12: `coverageStream` + `coverageStore` (SSE)

**Files:**
- Create: `web/src/lib/coverageStream.ts`, `web/src/lib/coverageStore.ts`
- Test: `web/src/lib/coverageStore.test.ts`

**Interfaces:**
- Produces: `CoverageStream` class (mirrors `SearchStream`, endpoint `/api/v1/artist/{source}/{id}/coverage`, one-shot, self-closes on error); `useCoverageStream(source, id, enabled)` returning `Record<externalAlbumId, AlbumCoverage>` accumulated from the stream (keyed map; idempotent; resets on key change).

- [ ] **Step 1: Write the failing test** (inject a fake `EventSourceLike`, push two coverage frames, assert the map keys/states; mirror `searchStream.test.ts`/`everywhereStore.test.ts`).

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement `coverageStream.ts`** (copy `searchStream.ts`, swap the URL + payload type to `AlbumCoverage`):

```ts
import type { AlbumCoverage } from './types'

export interface EventSourceLike {
  onmessage: ((ev: { data: string }) => void) | null
  onerror: (() => void) | null
  close(): void
}

export interface CoverageStreamHandlers {
  onCoverage(c: AlbumCoverage): void
  onError?(): void
}

export class CoverageStream {
  private source: EventSourceLike
  constructor(
    source: string,
    id: string,
    handlers: CoverageStreamHandlers,
    makeSource: (url: string) => EventSourceLike = (url) => new EventSource(url, { withCredentials: true }) as unknown as EventSourceLike,
  ) {
    const url = `/api/v1/artist/${encodeURIComponent(source)}/${encodeURIComponent(id)}/coverage`
    this.source = makeSource(url)
    this.source.onmessage = (ev) => {
      try { handlers.onCoverage(JSON.parse(ev.data) as AlbumCoverage) } catch { /* ignore */ }
    }
    this.source.onerror = () => { this.source.close(); handlers.onError?.() }
  }
  close() { this.source.close() }
}
```

- [ ] **Step 4: Implement `coverageStore.ts`** (a hook, mirroring `useEverywhere`):

```ts
import { useEffect, useReducer } from 'react'
import type { AlbumCoverage } from './types'
import { CoverageStream } from './coverageStream'

export type CoverageMap = Record<string, AlbumCoverage>

type Action = { type: 'reset' } | { type: 'coverage'; c: AlbumCoverage }

function reducer(state: CoverageMap, action: Action): CoverageMap {
  if (action.type === 'reset') return {}
  const prev = state[action.c.externalAlbumId]
  if (prev && prev.state === action.c.state && prev.ownedCount === action.c.ownedCount) return state
  return { ...state, [action.c.externalAlbumId]: action.c }
}

export function useCoverageStream(source: string, id: string, enabled: boolean): CoverageMap {
  const [state, dispatch] = useReducer(reducer, {})
  useEffect(() => {
    dispatch({ type: 'reset' })
    if (!enabled || !source || !id) return
    const stream = new CoverageStream(source, id, { onCoverage: (c) => dispatch({ type: 'coverage', c }) })
    return () => stream.close()
  }, [source, id, enabled])
  return state
}
```

- [ ] **Step 5: Run** test → PASS.
- [ ] **Step 6: Commit** `git commit -am "feat(web): coverage SSE stream + accumulating store hook"`.

---

### Task 13: Extend `MediaCard` with coverage + download hover action

**Files:**
- Modify: `web/src/components/ui/MediaCard.tsx`
- Test: `web/src/components/ui/MediaCard.test.tsx` (add cases)

**Interfaces:**
- Produces: `MediaCard` gains optional `coverage?: { state: CoverageState; owned: number; total: number }` (renders `<CoverageChip>` in the existing top-left slot) and `onDownload?: () => void` (renders an accent download circle in the bottom-right reveal slot when `onPlay` is absent; uses `Icon name="dl"`).

- [ ] **Step 1: Add failing test** asserting: with `coverage.state="partial"` a `7/10` chip renders; with `onDownload` and no `onPlay`, a button labeled `Download <title>` renders and fires `onDownload`.

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement** — replace the `badge` prop usage path so coverage takes the top-left slot, and add the download reveal mirroring the play reveal:

```tsx
// add to MediaCardProps
  coverage?: { state: CoverageState; owned: number; total: number }
  onDownload?: () => void
```

```tsx
// in the cover div, replace the badge block with:
{coverage && (
  <div className="absolute left-2 top-2"><CoverageChip state={coverage.state} owned={coverage.owned} total={coverage.total} /></div>
)}
{badge && !coverage && <div className="absolute left-2 top-2">{badge}</div>}

// after the existing onPlay reveal button, add:
{!onPlay && onDownload && (
  <button
    type="button"
    aria-label={`Download ${title}`}
    onClick={(e) => { e.stopPropagation(); onDownload() }}
    className={[
      'absolute right-3 bottom-3 w-10 h-10 rounded-full bg-accent text-surface',
      'inline-grid place-items-center shadow-cover',
      'opacity-0 translate-y-2 group-hover:opacity-100 group-hover:translate-y-0',
      'transition-all duration-150 focus-visible:opacity-100',
    ].join(' ')}
  >
    <Icon name="dl" className="w-4 h-4" />
  </button>
)}
```

Import `CoverageChip` and `CoverageState`.

- [ ] **Step 4: Run** the MediaCard tests → PASS (existing + new).
- [ ] **Step 5: Commit** `git commit -am "feat(web): MediaCard coverage chip + download hover action"`.

---

# Phase 3 — Pages

### Task 14: Artist page rework

**Files:**
- Modify: `web/src/routes/Artist.tsx`
- Test: `web/src/routes/Artist.test.tsx` (rewrite)

**Interfaces:**
- Consumes: `useArtistDetail`, `useCoverageStream`, `CoverageChip`, `MediaCard`, `Chip`, `Button`, `IconButton`, `postBatchDownload`, the route params `{source, id}` (Task 16 adds the route).

- [ ] **Step 1: Rewrite the test** to mock `useArtistDetail` (returns a 2-album skeleton) and `useCoverageStream` (returns a coverage map), asserting: albums render as cards; a `7/10` partial chip shows when coverage says partial; filter chips (All/Albums/Singles & EPs) filter the grid; "Download all missing" calls `postBatchDownload`.

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement** the page. Read `source`/`id` from params (default `source='library'` when only `:id` present via the redirect in Task 16). Render: circular `Cover`, name, the "N of M albums · K partial · J missing" stat (computed from the coverage map), action row, `Chip` filters, and a `MediaCard` grid where each card passes `coverage={{state, owned, total}}` from the map (default `pending` until streamed) and either `onPlay` (full → open library album) or `onDownload` (partial/none → batch-download missing). Compute "Download all missing" as the union of every album's `missingTracks` from the map. Use the exact class names validated in the mockup (`grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5`, accent stat, etc.). Keep the loading skeleton already in the file.

> The full component is ~150 lines of JSX assembling already-built primitives; it
> introduces no new logic beyond wiring the map → card props and the filter state.
> Build it to satisfy the test from Step 1, matching the approved mockup.

- [ ] **Step 4: Run** `npx vitest run src/routes/Artist.test.tsx` → PASS.
- [ ] **Step 5: Commit** `git commit -am "feat(web): artist page with streaming discography coverage"`.

---

### Task 15: Album page rework

**Files:**
- Modify: `web/src/routes/Album.tsx`
- Test: `web/src/routes/Album.test.tsx` (rewrite)

**Interfaces:**
- Consumes: `useAlbumDetail`, `TrackRow`, `DownloadAction` (for missing rows — build a minimal `ExternalResult` from `externalRef` so the existing component drives the lifecycle), `postBatchDownload`, `usePlayer`.

- [ ] **Step 1: Rewrite the test**: mock `useAlbumDetail` returning a partial album (2 owned, 1 missing). Assert: owned rows are playable; the missing row renders a Download control; header shows "X of Y in library" and "Download missing · 1" which calls `postBatchDownload` with the 1 missing ref.

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement**: render the album-wash header (reuse the existing wash + `--album-wash`), the "X of Y in library" line when `ownedCount < totalCount`, and the "Download missing · N" `Button`. Map `tracks`: owned (`state==='full'`) → `TrackRow` with the `libraryTrack` and `onPlay`; missing (`state==='none'`) → `TrackRow` with `right={<DownloadAction result={refToExternalResult(t.externalRef)} />}`. Add a small `refToExternalResult(ref): ExternalResult` helper (type `'track'`, no match). For a `source==='library'` album, all rows are owned (unchanged behavior).

- [ ] **Step 4: Run** the Album test → PASS.
- [ ] **Step 5: Commit** `git commit -am "feat(web): album page with per-track ownership + download missing"`.

---

### Task 16: Playlist page (new) + routing + redirects

**Files:**
- Create: `web/src/routes/Playlist.tsx`, `web/src/routes/Playlist.test.tsx`
- Modify: `web/src/App.tsx`
- Test: add `web/src/App.test.tsx` route cases if one exists; else cover via the page test.

**Interfaces:**
- Consumes: `usePlaylistDetail`, `TrackRow`, `usePlayer`, `Cover`, `Button`, `IconButton`.
- Produces routes: `/artist/:source/:id`, `/album/:source/:id`, `/playlist/:id`, plus redirects `/artist/:id`→`/artist/library/:id`, `/album/:id`→`/album/library/:id`.

- [ ] **Step 1: Write the Playlist test**: mock `usePlaylistDetail` → playlist with 3 tracks; assert header (name, "3 songs"), Play plays the list, rows render.

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement `Playlist.tsx`** — the Album shell minus coverage: header (`Cover` or first-track cover, "Playlist" eyebrow, `name`, `songCount` + duration), Play/Shuffle, a `TrackRow` list (all owned; rows show artist + album since a playlist is heterogeneous). Loading skeleton mirroring Album.

- [ ] **Step 4: Update `App.tsx` routes**:

```tsx
import Playlist from './routes/Playlist'
// ...
<Route path="/album/:source/:id" element={<Album />} />
<Route path="/album/:id" element={<Navigate to="library" replace relative="path" />} />
<Route path="/artist/:source/:id" element={<Artist />} />
<Route path="/artist/:id" element={<Navigate to="library" replace relative="path" />} />
<Route path="/playlist/:id" element={<Playlist />} />
```

> Verify the redirect form against react-router v6: if `relative="path"` Navigate is
> awkward, instead implement tiny wrapper routes that read `:id` and `<Navigate to={`/album/library/${id}`} replace />`. Pick whichever the test in Step 5 proves.

- [ ] **Step 5: Update `Artist.tsx`/`Album.tsx` to read `source`** from params with a `'library'` fallback, and run `npx vitest run src/routes/Playlist.test.tsx src/routes/Album.test.tsx src/routes/Artist.test.tsx` → PASS.
- [ ] **Step 6: Commit** `git commit -am "feat(web): playlist page + source-qualified routes & redirects"`.

---

# Phase 4 — End-to-end integration

### Task 17: Re-point every detail navigation + wire playlists

**Files:** Modify `web/src/components/shell/LibraryRail.tsx`, `web/src/routes/Library.tsx`, `web/src/routes/Search.tsx`, `web/src/components/search/SearchSuggest.tsx`, `web/src/routes/Home.tsx`, `web/src/components/shell/NowPlayingPanel.tsx`, `web/src/components/AddToPlaylistMenu.tsx`, `web/src/components/shell/PlayerBar.tsx`.

**Interfaces:** Consumes the routes from Task 16. Every artist/album/playlist navigation becomes source-qualified (`library` for library entities, `spotify` for external search results).

- [ ] **Step 1: Update each touchpoint** per the spec's §7 table. Concretely:
  - `LibraryRail.tsx`: `navigate(`/album/library/${al.id}`)`, `navigate(`/artist/library/${ar.id}`)`, and add playlist rows → `navigate(`/playlist/${pl.id}`)`.
  - `Library.tsx`: album/artist cards source-qualified; playlist cards → `/playlist/:id`.
  - `Search.tsx`: library results → `…/library/:id`; **Everywhere** album/artist results → `/album/spotify/:externalId`, `/artist/spotify/:externalId`.
  - `SearchSuggest.tsx`: same as Search for album/artist suggestions.
  - `Home.tsx`: album/artist tiles source-qualified; any playlist tiles → `/playlist/:id`; the inline `api.get<Album>(`/library/album/${id}`)` calls stay (data fetch, not nav) but should switch to `/album/library/${id}` via the detail endpoint where they drive the hero.
  - `Album.tsx` artist link → `/artist/library/${album.artistId}`.
  - `Artist.tsx` album click → `/album/library/${al.id}` for owned albums, `/album/spotify/${externalId}` for not-owned.
  - `NowPlayingPanel.tsx`/`PlayerBar.tsx`: current track's artist/album links source-qualified (`library`).
  - `AddToPlaylistMenu.tsx`: after creating a playlist, navigate to `/playlist/${created.id}`; after adding, invalidate the `['playlist-detail', id]` query.

- [ ] **Step 2: Update the affected component tests** that assert navigation targets (grep for `/album/` and `/artist/` in `*.test.tsx`). Run the full FE suite: `npx vitest run` → PASS.

- [ ] **Step 3: Manual smoke (typecheck + build):** `npm run build` (in `web/`) → succeeds, no TS errors.

- [ ] **Step 4: Commit** `git commit -am "feat(web): wire detail pages into rail, library, search, home, player, playlists"`.

---

# Phase 5 — e2e + degraded states

### Task 18: Degraded / empty states

**Files:** Modify `web/src/routes/Artist.tsx` (unresolved → library-only, no error), `web/src/routes/Album.tsx` (external fetch error → empty state), and confirm `none`-coverage albums are clean at rest.

- [ ] **Step 1:** Add tests: `useArtistDetail` with `resolved:false` renders library albums and no coverage stream is opened (assert `CoverageStream` not constructed); album detail error → `EmptyState`.
- [ ] **Step 2:** Implement the guards (`enabled` flag on `useCoverageStream` gated by `detail.resolved`).
- [ ] **Step 3: Run** the page tests → PASS.
- [ ] **Step 4: Commit** `git commit -am "feat(web): graceful degrade for unresolved artists + album errors"`.

### Task 19: Playwright e2e — the completeness flow

**Files:** Create `web/e2e/completeness.spec.ts` (mirror the existing hermetic e2e harness — stubbed library + search + downloader).

- [ ] **Step 1: Write the e2e**: seed a library missing one track of an album; open the artist page; assert a partial badge `…/…`; click "Download missing"; assert the stubbed downloader receives the request and (on completion event) the album badge flips to full / the track row flips to owned. Reach a playlist from the rail and assert it plays.
- [ ] **Step 2: Run** the e2e per the repo's existing command (e.g. `npm run e2e` in `web/`). Expected: PASS.
- [ ] **Step 3: Commit** `git commit -am "test(e2e): artist→partial album→download missing→flips to owned"`.

### Task 20: Full verification + whole-branch review

- [ ] **Step 1: Run everything**: `go test ./...` and (in `web/`) `npx vitest run` and `npm run build`. All green.
- [ ] **Step 2:** Use `superpowers:requesting-code-review` for a whole-branch review before merge.
- [ ] **Step 3:** Use `superpowers:finishing-a-development-branch` to decide merge/PR.

---

## Self-Review Notes

- **Spec coverage:** Artist discography + badges (Tasks 2–6,14); exact partial X/Y (Tasks 4,14,15); smart "N of M" (Task 14); album per-track + download-missing (Tasks 8,15); playlist detail page + sync-ready shell (Tasks 7,16); caches + `library.updated` invalidation (Tasks 5,8); SSE streaming (Tasks 8,12); end-to-end integration (Task 17); degrade/edges (Task 18); e2e (Task 19). All spec §§1–10 map to tasks.
- **Deferred (spec §10):** artist-level "download entire discography" intentionally omitted; `backfillLibraryAlbumID` left as a documented seam (Task 6) — does not block any test.
- **Type consistency:** `AlbumCoverage`/`ExternalTrackRef`/`DiscographyAlbum`/`ArtistDetail`/`AlbumDetail` identical across Go (Task 1) and TS (Task 10); `CoverageService` (Task 8) matches `coverage.Service` methods (Task 6); `useCoverageStream` map keyed by `externalAlbumId` consumed by Task 14.
