# Playlist Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Import a public Spotify playlist by URL into a Reverb-managed synced playlist (stored Spotify tracklist + live have/missing matching), with one-click download-missing, manual "Sync now," and a background scheduler.

**Architecture:** A `synced_playlists` table stores each imported playlist's ordered Spotify track refs + schedule settings; ownership is computed live per view via the existing matching service (reusing the `AlbumDetail` pattern). One `Sync` function (re-fetch Spotify → replace tracklist → optional auto-download) serves both manual "Sync now" and a background ticker scheduler. Reuses the completeness engine, batch-download manager, and album-page rendering.

**Tech Stack:** Go (chi, modernc sqlite, goose, sqlc), React 19 + TS (Vite, Tailwind, TanStack Query, Zustand), Vitest + Playwright.

## Global Constraints

- Go module path: `github.com/maxjb-xyz/reverb`. Binary `reverb`.
- Exported domain types live in `internal/core` with stable camelCase JSON tags.
- DB: goose migrations in `internal/store/migrations/NNNN_name.sql` (next number is **0006**); queries in `internal/store/queries/*.sql`; regenerate with **`make gen`** (or `sqlc generate`) from repo root — never hand-edit `internal/store/db/*.go`.
- Auth scope: **public Spotify playlists only**, via the existing client-credentials token. No user OAuth. Spotify editorial/algorithmic playlists are inaccessible (Spotify policy) — not our concern to support.
- Have/missing is **computed live** per view via the matching service (`library_version`-invalidated) — NEVER stored on the synced playlist row.
- Reverb-managed only: do NOT create a Subsonic/Navidrome playlist.
- Sync intervals: `0` = Manual (scheduler skips), `86400` = Daily, `604800` = Weekly.
- Frontend tests = Vitest; backend = `go test ./...`. The real build gate is `npm run build` (= `tsc -b && vite build`) — run it, not just `tsc --noEmit`.
- Design tokens only (`text-accent`, `text-text-*`, `bg-*`, `text-on-accent`/`text-surface`) — never hex, `text-black`, or `text-white`.
- Adding a method to `LibraryAdapter` or `search` interfaces breaks test fakes → run `go test ./...` (not just `go build ./...`).
- IMPORTANT (process): implementers must NOT run `git reset`/`checkout`/`rebase`/`stash` or change branches — only edit + `git commit` on the current branch.
- Reuse, don't reinvent: matching service + `match_cache`, the download `Manager` (`Enqueue`), the album-page track rendering (`TrackRow` + the missing-row `DownloadAction`), `Cover` `coverSrc`, `Select`/`Toggle`/`Chip`/`Button`/`IconButton`, the admin modal pattern, the `coverage` service (template for the sync service).

---

## File Structure

**Backend (new):**
- `internal/core/playlistsync.go` — `ExternalPlaylist`, `SyncedPlaylist`, `SyncedPlaylistDetail`.
- `internal/playlistsync/service.go` (+ `_test.go`) — Import/List/Detail/Sync/UpdateSettings/Delete + interfaces.
- `internal/playlistsync/scheduler.go` (+ `_test.go`) — background ticker + due-selection.
- `internal/store/migrations/0006_synced_playlists.sql`, `internal/store/queries/synced_playlists.sql`.
- `internal/api/synced_playlists.go` (+ `_test.go`) — 7 handlers.

**Backend (modified):**
- `internal/search/search.go` — add `PlaylistProvider` capability interface.
- `internal/search/spotify/{adapter,dto}.go` — `GetPlaylist` + `ParsePlaylistID`.
- `internal/api/server.go` — routes + `live.sync` + `Deps`.
- `internal/wiring/*.go`, `cmd/reverb/*.go` — build the sync service + start the scheduler.

**Frontend (new):**
- `web/src/lib/syncedPlaylistApi.ts` (+ test), `web/src/components/ImportPlaylistDialog.tsx` (+ test), `web/src/routes/SyncedPlaylist.tsx` (+ test).

**Frontend (modified):**
- `web/src/lib/types.ts`, `web/src/App.tsx`, `web/src/routes/Library.tsx`, `web/src/components/shell/LibraryRail.tsx`.

---

# Phase 1 — Backend

### Task 1: Spotify `GetPlaylist` + `ParsePlaylistID` + `PlaylistProvider`

**Files:**
- Modify: `internal/search/search.go`, `internal/search/spotify/adapter.go`, `internal/search/spotify/dto.go`
- Create: `internal/core/playlistsync.go` (the `ExternalPlaylist` type)
- Test: `internal/search/spotify/adapter_test.go` (add)

**Interfaces:**
- Produces: `core.ExternalPlaylist{Source, ExternalID, Name, CoverURL string; Tracks []core.ExternalResult}`; `search.PlaylistProvider` interface; `(*spotify.Adapter).GetPlaylist(ctx, externalID) (core.ExternalPlaylist, error)`; `spotify.ParsePlaylistID(url string) (string, bool)`.

- [ ] **Step 1: Add the core type** — create `internal/core/playlistsync.go`:

```go
package core

// ExternalPlaylist is a playlist fetched from a SearchSource (e.g. Spotify).
type ExternalPlaylist struct {
	Source     string           `json:"source"`
	ExternalID string           `json:"externalId"`
	Name       string           `json:"name"`
	CoverURL   string           `json:"coverUrl,omitempty"`
	Tracks     []ExternalResult `json:"tracks"`
}
```

- [ ] **Step 2: Add the capability interface** to `internal/search/search.go` (next to `DiscographyProvider`):

```go
// PlaylistProvider is an OPTIONAL capability (P2 playlist sync). Detected via a
// type assertion; conformance does not require it.
type PlaylistProvider interface {
	GetPlaylist(ctx context.Context, externalID string) (core.ExternalPlaylist, error)
}
```

- [ ] **Step 3: Write the failing test** (`adapter_test.go`) — covers URL parsing + a paginated playlist fetch:

```go
func TestParsePlaylistID(t *testing.T) {
	cases := map[string]string{
		"https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M":        "37i9dQZF1DXcBWIGoYBM5M",
		"https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M?si=abc": "37i9dQZF1DXcBWIGoYBM5M",
		"spotify:playlist:37i9dQZF1DXcBWIGoYBM5M":                        "37i9dQZF1DXcBWIGoYBM5M",
	}
	for in, want := range cases {
		got, ok := ParsePlaylistID(in)
		if !ok || got != want {
			t.Fatalf("ParsePlaylistID(%q) = %q,%v; want %q,true", in, got, ok, want)
		}
	}
	if _, ok := ParsePlaylistID("https://open.spotify.com/album/123"); ok {
		t.Fatal("album URL must not parse as a playlist")
	}
}

func TestGetPlaylistMapsAndPaginates(t *testing.T) {
	page1 := `{"name":"Chill","images":[{"url":"http://img/p"}],"tracks":{
	  "items":[{"track":{"id":"t1","name":"One","artists":[{"name":"A"}],"duration_ms":1000,"external_ids":{"isrc":"X1"},"album":{"name":"AL","images":[{"url":"http://img/a"}]}}}],
	  "next":"NEXTURL"}}`
	page2 := `{"items":[{"track":{"id":"t2","name":"Two","artists":[{"name":"B"}],"duration_ms":2000,"album":{"name":"AL2"}}}],"next":null}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/playlists/") && strings.HasSuffix(r.URL.Path, "/tracks"):
			_, _ = w.Write([]byte(page2)) // the "next" page
		case strings.Contains(r.URL.Path, "/playlists/"):
			_, _ = w.Write([]byte(page1)) // playlist object (meta + first tracks page)
		default:
			_, _ = w.Write([]byte(`{"access_token":"t","expires_in":3600}`))
		}
	}))
	defer srv.Close()
	a := New().WithBaseURLs(srv.URL, srv.URL)
	if err := a.Init(map[string]any{"client_id": "x", "client_secret": "y"}); err != nil {
		t.Fatal(err)
	}
	pl, err := a.GetPlaylist(context.Background(), "pl1")
	if err != nil {
		t.Fatal(err)
	}
	if pl.Name != "Chill" || pl.CoverURL != "http://img/p" {
		t.Fatalf("bad meta: %+v", pl)
	}
	if len(pl.Tracks) != 2 || pl.Tracks[0].ExternalID != "t1" || pl.Tracks[1].ExternalID != "t2" {
		t.Fatalf("bad tracks: %+v", pl.Tracks)
	}
	if pl.Tracks[0].ISRC != "X1" || pl.Tracks[0].Type != core.EntityTrack {
		t.Fatalf("bad track mapping: %+v", pl.Tracks[0])
	}
}
```

(Imports if absent: `context`, `net/http`, `net/http/httptest`, `strings`.)

- [ ] **Step 4: Run** `go test ./internal/search/spotify/ -run 'ParsePlaylistID|GetPlaylist'` → FAIL (undefined).

- [ ] **Step 5: Add the DTOs** (`internal/search/spotify/dto.go`):

```go
type playlistObjectDTO struct {
	Name   string          `json:"name"`
	Images []imageDTO      `json:"images"`
	Tracks playlistPageDTO `json:"tracks"`
}

type playlistPageDTO struct {
	Items []playlistItemDTO `json:"items"`
	Next  string            `json:"next"`
}

type playlistItemDTO struct {
	Track trackDTO `json:"track"`
}
```

(`trackDTO`, `imageDTO`, `artistRefDTO` already exist and `mapTrack` already maps a `trackDTO` → `core.ExternalResult`.)

- [ ] **Step 6: Implement** `ParsePlaylistID` + `GetPlaylist` (`adapter.go`):

```go
var _ search.PlaylistProvider = (*Adapter)(nil)

var playlistIDRe = regexp.MustCompile(`(?:open\.spotify\.com/playlist/|spotify:playlist:)([A-Za-z0-9]+)`)

// ParsePlaylistID extracts a Spotify playlist id from a URL or URI; ok=false if absent.
func ParsePlaylistID(s string) (string, bool) {
	m := playlistIDRe.FindStringSubmatch(s)
	if len(m) < 2 {
		return "", false
	}
	return m[1], true
}

// GetPlaylist fetches a public Spotify playlist's metadata + all tracks (paginated).
func (a *Adapter) GetPlaylist(ctx context.Context, externalID string) (core.ExternalPlaylist, error) {
	var obj playlistObjectDTO
	if err := a.client.apiGet(ctx, "/playlists/"+url.PathEscape(externalID), url.Values{}, &obj); err != nil {
		return core.ExternalPlaylist{}, err
	}
	pl := core.ExternalPlaylist{
		Source: "spotify", ExternalID: externalID, Name: obj.Name,
		CoverURL: firstImage(obj.Images), Tracks: []core.ExternalResult{},
	}
	page := obj.Tracks
	for {
		for _, it := range page.Items {
			if it.Track.ID == "" { // local/unavailable tracks have no id
				continue
			}
			pl.Tracks = append(pl.Tracks, a.mapTrack(it.Track))
		}
		if page.Next == "" {
			break
		}
		// Follow the absolute next URL; apiGet takes a path+params, so re-issue the
		// tracks endpoint with an offset derived from accumulated count.
		params := url.Values{}
		params.Set("offset", strconv.Itoa(len(pl.Tracks)))
		params.Set("limit", "100")
		var next playlistPageDTO
		if err := a.client.apiGet(ctx, "/playlists/"+url.PathEscape(externalID)+"/tracks", params, &next); err != nil {
			return core.ExternalPlaylist{}, err
		}
		if len(next.Items) == 0 {
			break
		}
		page = next
	}
	return pl, nil
}
```

(Imports: add `regexp` if absent; `url`, `strconv` already used.)

- [ ] **Step 7: Run** the test → PASS. Then `go build ./...`.

- [ ] **Step 8: Commit**

```bash
git add internal/core/playlistsync.go internal/search/
git commit -m "feat(spotify): GetPlaylist (paginated) + ParsePlaylistID + PlaylistProvider"
```

---

### Task 2: `synced_playlists` store

**Files:**
- Create: `internal/store/migrations/0006_synced_playlists.sql`, `internal/store/queries/synced_playlists.sql`
- Generated: `internal/store/db/synced_playlists.sql.go` (via `make gen`)
- Test: `internal/store/store_test.go` (add)

**Interfaces:**
- Produces (generated): `db.InsertSyncedPlaylist`/`UpsertSyncedPlaylist`, `db.GetSyncedPlaylist`, `db.GetSyncedPlaylistBySource`, `db.ListSyncedPlaylists`, `db.ListDueSyncedPlaylists`, `db.UpdateSyncedPlaylistTracks`, `db.UpdateSyncedPlaylistSettings`, `db.DeleteSyncedPlaylist` + their `db.*Params`.

- [ ] **Step 1: Migration** `internal/store/migrations/0006_synced_playlists.sql`:

```sql
-- +goose Up
CREATE TABLE synced_playlists (
  id                TEXT PRIMARY KEY,
  source            TEXT NOT NULL,
  external_id       TEXT NOT NULL,
  name              TEXT NOT NULL,
  cover_url         TEXT NOT NULL DEFAULT '',
  tracks_json       TEXT NOT NULL,
  sync_enabled      INTEGER NOT NULL DEFAULT 0,
  sync_interval_sec INTEGER NOT NULL DEFAULT 0,
  auto_download     INTEGER NOT NULL DEFAULT 0,
  last_synced_at    INTEGER NOT NULL DEFAULT 0,
  created_at        INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_synced_playlists_source_external ON synced_playlists(source, external_id);

-- +goose Down
DROP TABLE synced_playlists;
```

- [ ] **Step 2: Queries** `internal/store/queries/synced_playlists.sql`:

```sql
-- name: UpsertSyncedPlaylist :one
INSERT INTO synced_playlists (id, source, external_id, name, cover_url, tracks_json, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(source, external_id) DO UPDATE SET
  name = excluded.name, cover_url = excluded.cover_url, tracks_json = excluded.tracks_json
RETURNING *;

-- name: GetSyncedPlaylist :one
SELECT * FROM synced_playlists WHERE id = ?;

-- name: ListSyncedPlaylists :many
SELECT * FROM synced_playlists ORDER BY created_at DESC;

-- name: ListDueSyncedPlaylists :many
SELECT * FROM synced_playlists
WHERE sync_enabled = 1 AND sync_interval_sec > 0 AND (last_synced_at + sync_interval_sec) <= ?;

-- name: UpdateSyncedPlaylistTracks :exec
UPDATE synced_playlists SET name = ?, cover_url = ?, tracks_json = ?, last_synced_at = ? WHERE id = ?;

-- name: UpdateSyncedPlaylistSettings :exec
UPDATE synced_playlists SET sync_enabled = ?, sync_interval_sec = ?, auto_download = ? WHERE id = ?;

-- name: DeleteSyncedPlaylist :exec
DELETE FROM synced_playlists WHERE id = ?;
```

- [ ] **Step 3: Generate** — run `make gen` from repo root. Confirm `internal/store/db/synced_playlists.sql.go` created and `go build ./...` compiles.

- [ ] **Step 4: Round-trip test** (`store_test.go`, mirror the existing helper e.g. `openMigrated(t)`):

```go
func TestSyncedPlaylistRoundTrip(t *testing.T) {
	st := openMigrated(t)
	q := st.Q()
	ctx := context.Background()
	row, err := q.UpsertSyncedPlaylist(ctx, db.UpsertSyncedPlaylistParams{
		ID: "sp1", Source: "spotify", ExternalID: "ext1", Name: "Chill",
		CoverUrl: "http://img", TracksJson: `[]`, CreatedAt: 100,
	})
	if err != nil || row.Name != "Chill" {
		t.Fatalf("upsert: %+v err=%v", row, err)
	}
	// Upsert again with same (source, external_id) updates, not duplicates.
	if _, err := q.UpsertSyncedPlaylist(ctx, db.UpsertSyncedPlaylistParams{
		ID: "sp1", Source: "spotify", ExternalID: "ext1", Name: "Renamed", TracksJson: `[]`, CreatedAt: 100,
	}); err != nil {
		t.Fatal(err)
	}
	all, _ := q.ListSyncedPlaylists(ctx)
	if len(all) != 1 || all[0].Name != "Renamed" {
		t.Fatalf("want 1 row 'Renamed', got %+v", all)
	}
}
```

- [ ] **Step 5: Run** `go test ./internal/store/ -run TestSyncedPlaylistRoundTrip` → PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/store/
git commit -m "feat(store): synced_playlists table + queries"
```

---

### Task 3: Sync service

**Files:**
- Create: `internal/playlistsync/service.go`, `internal/playlistsync/service_test.go`
- Modify: `internal/core/playlistsync.go` (add `SyncedPlaylist`, `SyncedPlaylistDetail`)

**Interfaces:**
- Consumes: `core.ExternalPlaylist` (Task 1); a `PlaylistSource` (`GetPlaylist` + `ParsePlaylistID` — `*spotify.Adapter` satisfies via a thin wrapper, see below); a `Matcher` (`Match(ctx, core.ExternalResult)(core.MatchResult,error)`); a `Downloader` (`Enqueue(ctx, core.DownloadRequest)(core.DownloadJob,error)`); a `Store` (the `db.*` synced-playlist methods); `now func() int64`; an id generator `newID func() string`.
- Produces: `playlistsync.NewService(...)`; `Import(ctx, url string, downloadMissing bool)`, `List(ctx)`, `Detail(ctx, id)`, `Sync(ctx, id)`, `UpdateSettings(ctx, id, enabled bool, intervalSec int, autoDownload bool)`, `Delete(ctx, id)`; `core.SyncedPlaylist`, `core.SyncedPlaylistDetail`.

- [ ] **Step 1: Add core types** to `internal/core/playlistsync.go`:

```go
// SyncedPlaylist is the stored synced-playlist summary (no ownership — computed live).
type SyncedPlaylist struct {
	ID              string `json:"id"`
	Source          string `json:"source"`
	ExternalID      string `json:"externalId"`
	Name            string `json:"name"`
	CoverURL        string `json:"coverUrl,omitempty"`
	SyncEnabled     bool   `json:"syncEnabled"`
	SyncIntervalSec int    `json:"syncIntervalSec"`
	AutoDownload    bool   `json:"autoDownload"`
	LastSyncedAt    int64  `json:"lastSyncedAt"`
	TrackCount      int    `json:"trackCount"`
}

// SyncedPlaylistDetail adds live per-track ownership (mirrors AlbumDetail).
type SyncedPlaylistDetail struct {
	SyncedPlaylist
	OwnedCount int                `json:"ownedCount"`
	TotalCount int                `json:"totalCount"`
	Tracks     []AlbumDetailTrack `json:"tracks"`
}
```

- [ ] **Step 2: Write the failing test** (`service_test.go`) — import + detail live-match + sync diff. Provide map-backed fakes (`fakeSource`, `fakeMatcher`, `fakeDownloader`, `memStore`) at the top of the file:

```go
package playlistsync

import (
	"context"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/core"
)

func track(id string) core.ExternalResult {
	return core.ExternalResult{Source: "spotify", ExternalID: id, Title: id, Type: core.EntityTrack}
}

func TestImportThenDetailComputesOwnership(t *testing.T) {
	src := &fakeSource{playlists: map[string]core.ExternalPlaylist{
		"PL": {Source: "spotify", ExternalID: "PL", Name: "Chill", Tracks: []core.ExternalResult{track("t1"), track("t2")}},
	}}
	m := fakeMatcher{owned: map[string]string{"t1": "L1"}} // t2 missing
	svc := NewService(src, m, fakeDownloader{}, newMemStore(), func() int64 { return 100 }, seqID())
	det, err := svc.Import(context.Background(), "https://open.spotify.com/playlist/PL", false)
	if err != nil {
		t.Fatal(err)
	}
	if det.Name != "Chill" || det.TotalCount != 2 || det.OwnedCount != 1 {
		t.Fatalf("bad import detail: %+v", det)
	}
	if det.Tracks[0].State != core.CoverageFull || det.Tracks[1].State != core.CoverageNone {
		t.Fatalf("bad ownership: %+v", det.Tracks)
	}
}

func TestSyncReplacesTracklist(t *testing.T) {
	src := &fakeSource{playlists: map[string]core.ExternalPlaylist{
		"PL": {Source: "spotify", ExternalID: "PL", Name: "Chill", Tracks: []core.ExternalResult{track("t1")}},
	}}
	store := newMemStore()
	svc := NewService(src, fakeMatcher{}, fakeDownloader{}, store, func() int64 { return 100 }, seqID())
	det, _ := svc.Import(context.Background(), "spotify:playlist:PL", false)
	// Spotify playlist gains a track; sync must reflect it.
	src.playlists["PL"] = core.ExternalPlaylist{Source: "spotify", ExternalID: "PL", Name: "Chill", Tracks: []core.ExternalResult{track("t1"), track("t3")}}
	det2, err := svc.Sync(context.Background(), det.ID)
	if err != nil || det2.TotalCount != 2 {
		t.Fatalf("sync should reflect 2 tracks, got %+v err=%v", det2, err)
	}
}
```

Write `fakeSource` (`GetPlaylist` from a map; `ParsePlaylistID` delegating to a tiny inline parser or `spotify.ParsePlaylistID` — but to avoid importing spotify, give `fakeSource` a `parse` returning the trailing id), `fakeMatcher` (as in coverage tests), `fakeDownloader` (records Enqueue calls), `newMemStore()` (map-backed implementing the `Store` interface), `seqID()` (returns "sp1","sp2",…). NOTE: the service's `PlaylistSource` interface should expose `ParsePlaylistID(url)(string,bool)` so it's injectable/fakeable.

- [ ] **Step 3: Run** `go test ./internal/playlistsync/` → FAIL (undefined).

- [ ] **Step 4: Implement** `service.go`:

```go
package playlistsync

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/maxjb-xyz/reverb/internal/core"
)

type PlaylistSource interface {
	ParsePlaylistID(url string) (string, bool)
	GetPlaylist(ctx context.Context, externalID string) (core.ExternalPlaylist, error)
}
type Matcher interface {
	Match(ctx context.Context, ext core.ExternalResult) (core.MatchResult, error)
}
type Downloader interface {
	Enqueue(ctx context.Context, req core.DownloadRequest) (core.DownloadJob, error)
}
type Store interface {
	Upsert(ctx context.Context, p core.SyncedPlaylist, tracksJSON string, createdAt int64) (string, error) // returns id
	Get(ctx context.Context, id string) (row SyncedRow, err error)
	List(ctx context.Context) ([]SyncedRow, error)
	ListDue(ctx context.Context, now int64) ([]SyncedRow, error)
	UpdateTracks(ctx context.Context, id, name, coverURL, tracksJSON string, lastSyncedAt int64) error
	UpdateSettings(ctx context.Context, id string, enabled bool, intervalSec int, autoDownload bool) error
	Delete(ctx context.Context, id string) error
}

// SyncedRow is the store's row shape (decoupled from db.*; the wiring adapter maps db rows to this).
type SyncedRow struct {
	ID, Source, ExternalID, Name, CoverURL, TracksJSON string
	SyncEnabled, AutoDownload                          bool
	SyncIntervalSec                                    int
	LastSyncedAt, CreatedAt                            int64
}

type Service struct {
	src   PlaylistSource
	match Matcher
	dl    Downloader
	store Store
	now   func() int64
	newID func() string
}

func NewService(src PlaylistSource, m Matcher, dl Downloader, store Store, now func() int64, newID func() string) *Service {
	return &Service{src: src, match: m, dl: dl, store: store, now: now, newID: newID}
}

func (s *Service) Import(ctx context.Context, rawURL string, downloadMissing bool) (core.SyncedPlaylistDetail, error) {
	extID, ok := s.src.ParsePlaylistID(rawURL)
	if !ok {
		return core.SyncedPlaylistDetail{}, fmt.Errorf("not a spotify playlist url")
	}
	pl, err := s.src.GetPlaylist(ctx, extID)
	if err != nil {
		return core.SyncedPlaylistDetail{}, err
	}
	tj, _ := json.Marshal(pl.Tracks)
	id, err := s.store.Upsert(ctx, core.SyncedPlaylist{
		ID: s.newID(), Source: pl.Source, ExternalID: pl.ExternalID, Name: pl.Name, CoverURL: pl.CoverURL,
	}, string(tj), s.now())
	if err != nil {
		return core.SyncedPlaylistDetail{}, err
	}
	det, err := s.Detail(ctx, id)
	if err != nil {
		return det, err
	}
	if downloadMissing {
		s.enqueueMissing(ctx, det)
	}
	return det, nil
}

func (s *Service) Detail(ctx context.Context, id string) (core.SyncedPlaylistDetail, error) {
	row, err := s.store.Get(ctx, id)
	if err != nil {
		return core.SyncedPlaylistDetail{}, err
	}
	var tracks []core.ExternalResult
	_ = json.Unmarshal([]byte(row.TracksJSON), &tracks)
	det := core.SyncedPlaylistDetail{SyncedPlaylist: rowToSummary(row, len(tracks))}
	det.TotalCount = len(tracks)
	for i, tr := range tracks {
		res, mErr := s.match.Match(ctx, tr)
		if mErr != nil {
			return core.SyncedPlaylistDetail{}, mErr
		}
		dt := core.AlbumDetailTrack{Title: tr.Title, Artist: tr.Artist, TrackNumber: i + 1, DurationMs: tr.DurationMs}
		if res.Status == core.MatchInLibrary && res.LibraryTrackID != "" {
			det.OwnedCount++
			dt.State = core.CoverageFull
			dt.LibraryTrack = &core.Track{ID: res.LibraryTrackID, Title: tr.Title, Artist: tr.Artist, DurationMs: tr.DurationMs}
		} else {
			dt.State = core.CoverageNone
			ref := core.ExternalTrackRef{Source: tr.Source, ExternalID: tr.ExternalID, Title: tr.Title, Artist: tr.Artist, Album: tr.Album, ISRC: tr.ISRC, DurationMs: tr.DurationMs}
			dt.ExternalRef = &ref
		}
		det.Tracks = append(det.Tracks, dt)
	}
	return det, nil
}

func (s *Service) Sync(ctx context.Context, id string) (core.SyncedPlaylistDetail, error) {
	row, err := s.store.Get(ctx, id)
	if err != nil {
		return core.SyncedPlaylistDetail{}, err
	}
	pl, err := s.src.GetPlaylist(ctx, row.ExternalID)
	if err != nil {
		// Keep the last-known tracklist; surface the error.
		return core.SyncedPlaylistDetail{}, fmt.Errorf("sync %s: %w", id, err)
	}
	tj, _ := json.Marshal(pl.Tracks)
	if err := s.store.UpdateTracks(ctx, id, pl.Name, pl.CoverURL, string(tj), s.now()); err != nil {
		return core.SyncedPlaylistDetail{}, err
	}
	det, err := s.Detail(ctx, id)
	if err != nil {
		return det, err
	}
	if row.AutoDownload {
		s.enqueueMissing(ctx, det)
	}
	return det, nil
}

func (s *Service) enqueueMissing(ctx context.Context, det core.SyncedPlaylistDetail) {
	for _, t := range det.Tracks {
		if t.State == core.CoverageNone && t.ExternalRef != nil {
			_, _ = s.dl.Enqueue(ctx, core.DownloadRequest{
				Source: t.ExternalRef.Source, ExternalID: t.ExternalRef.ExternalID, Artist: t.ExternalRef.Artist,
				Title: t.ExternalRef.Title, Album: t.ExternalRef.Album, ISRC: t.ExternalRef.ISRC, DurationMs: t.ExternalRef.DurationMs,
			})
		}
	}
}

func (s *Service) List(ctx context.Context) ([]core.SyncedPlaylist, error) {
	rows, err := s.store.List(ctx)
	if err != nil {
		return nil, err
	}
	out := []core.SyncedPlaylist{}
	for _, r := range rows {
		var tracks []core.ExternalResult
		_ = json.Unmarshal([]byte(r.TracksJSON), &tracks)
		out = append(out, rowToSummary(r, len(tracks)))
	}
	return out, nil
}

func (s *Service) UpdateSettings(ctx context.Context, id string, enabled bool, intervalSec int, autoDownload bool) error {
	return s.store.UpdateSettings(ctx, id, enabled, intervalSec, autoDownload)
}
func (s *Service) Delete(ctx context.Context, id string) error { return s.store.Delete(ctx, id) }

// DownloadMissing enqueues the missing tracks for a synced playlist; returns jobs.
func (s *Service) DownloadMissing(ctx context.Context, id string) ([]core.DownloadJob, error) {
	det, err := s.Detail(ctx, id)
	if err != nil {
		return nil, err
	}
	jobs := []core.DownloadJob{}
	for _, t := range det.Tracks {
		if t.State == core.CoverageNone && t.ExternalRef != nil {
			j, e := s.dl.Enqueue(ctx, core.DownloadRequest{
				Source: t.ExternalRef.Source, ExternalID: t.ExternalRef.ExternalID, Artist: t.ExternalRef.Artist,
				Title: t.ExternalRef.Title, Album: t.ExternalRef.Album, ISRC: t.ExternalRef.ISRC, DurationMs: t.ExternalRef.DurationMs,
			})
			if e == nil {
				jobs = append(jobs, j)
			}
		}
	}
	return jobs, nil
}

func rowToSummary(r SyncedRow, trackCount int) core.SyncedPlaylist {
	return core.SyncedPlaylist{
		ID: r.ID, Source: r.Source, ExternalID: r.ExternalID, Name: r.Name, CoverURL: r.CoverURL,
		SyncEnabled: r.SyncEnabled, SyncIntervalSec: r.SyncIntervalSec, AutoDownload: r.AutoDownload,
		LastSyncedAt: r.LastSyncedAt, TrackCount: trackCount,
	}
}
```

> The `Store.Upsert` returns the id: when a `(source, external_id)` already exists, the
> store returns the EXISTING row's id (not the freshly-generated one). The wiring
> adapter (Task 5) implements this by checking `GetSyncedPlaylistBySource` first, or by
> reading the `RETURNING id`. Make the in-test `memStore` do the same so re-import returns
> the same id.

- [ ] **Step 5: Run** `go test ./internal/playlistsync/` → PASS (write the fakes until green). Then `go build ./...`.

- [ ] **Step 6: Commit**

```bash
git add internal/core/playlistsync.go internal/playlistsync/service.go internal/playlistsync/service_test.go
git commit -m "feat(playlistsync): sync service (import/detail/sync/download-missing)"
```

---

### Task 4: Scheduler

**Files:**
- Create: `internal/playlistsync/scheduler.go`, `internal/playlistsync/scheduler_test.go`

**Interfaces:**
- Consumes: `*Service` (its `store.ListDue` + `Sync`), `now func() int64`.
- Produces: `NewScheduler(svc *Service, interval time.Duration) *Scheduler`; `(*Scheduler).Run(ctx)` (blocks until ctx done, ticking); `(*Scheduler).tick(ctx)` (exported-for-test or lowercase + same-package test) which syncs all due playlists sequentially.

- [ ] **Step 1: Failing test** (`scheduler_test.go`) — a due playlist gets synced on tick:

```go
func TestSchedulerTickSyncsDue(t *testing.T) {
	src := &fakeSource{playlists: map[string]core.ExternalPlaylist{
		"PL": {Source: "spotify", ExternalID: "PL", Name: "P", Tracks: []core.ExternalResult{track("t1")}},
	}}
	store := newMemStore()
	svc := NewService(src, fakeMatcher{}, fakeDownloader{}, store, func() int64 { return 1000 }, seqID())
	det, _ := svc.Import(context.Background(), "spotify:playlist:PL", false)
	// enable daily sync, last_synced far in the past → due
	_ = svc.UpdateSettings(context.Background(), det.ID, true, 60, false)
	store.setLastSynced(det.ID, 0) // long ago
	src.syncCount = 0
	sch := NewScheduler(svc, time.Minute)
	sch.tick(context.Background())
	if src.syncCount == 0 {
		t.Fatal("scheduler tick should have re-fetched the due playlist")
	}
}
```

(`fakeSource` records `syncCount` on each `GetPlaylist`; `memStore.setLastSynced` test helper.)

- [ ] **Step 2: Run** `go test ./internal/playlistsync/ -run Scheduler` → FAIL.

- [ ] **Step 3: Implement** `scheduler.go`:

```go
package playlistsync

import (
	"context"
	"time"
)

type Scheduler struct {
	svc      *Service
	interval time.Duration
}

func NewScheduler(svc *Service, interval time.Duration) *Scheduler {
	return &Scheduler{svc: svc, interval: interval}
}

// Run ticks until ctx is cancelled, syncing due playlists each tick.
func (s *Scheduler) Run(ctx context.Context) {
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.tick(ctx)
		}
	}
}

// tick syncs every due playlist sequentially; failures are logged and skipped.
func (s *Scheduler) tick(ctx context.Context) {
	rows, err := s.svc.store.ListDue(ctx, s.svc.now())
	if err != nil {
		return
	}
	for _, r := range rows {
		if _, err := s.svc.Sync(ctx, r.ID); err != nil {
			// log-and-continue (use the project logger if one is wired; else skip)
			continue
		}
	}
}
```

- [ ] **Step 4: Run** → PASS. `go build ./...`.

- [ ] **Step 5: Commit**

```bash
git add internal/playlistsync/scheduler.go internal/playlistsync/scheduler_test.go
git commit -m "feat(playlistsync): background sync scheduler"
```

---

### Task 5: API handlers + store adapter + wiring + scheduler start

**Files:**
- Create: `internal/api/synced_playlists.go`, `internal/api/synced_playlists_test.go`
- Modify: `internal/api/server.go` (routes + `Deps.Sync` + `live.sync` + accessor), `internal/wiring/*.go` (build the service + the `Store` adapter over `*db.Queries`, pick the `PlaylistProvider`), `cmd/reverb/*.go` (start the scheduler goroutine), `internal/api/openapi.yaml`.

**Interfaces:**
- Consumes: `playlistsync.Service` methods; `DownloadManager` (existing).
- Produces routes (§5 of the spec). The API-side `SyncService` interface:
```go
type SyncService interface {
	Import(ctx context.Context, url string, downloadMissing bool) (core.SyncedPlaylistDetail, error)
	List(ctx context.Context) ([]core.SyncedPlaylist, error)
	Detail(ctx context.Context, id string) (core.SyncedPlaylistDetail, error)
	Sync(ctx context.Context, id string) (core.SyncedPlaylistDetail, error)
	DownloadMissing(ctx context.Context, id string) ([]core.DownloadJob, error)
	UpdateSettings(ctx context.Context, id string, enabled bool, intervalSec int, autoDownload bool) error
	Delete(ctx context.Context, id string) error
}
```

- [ ] **Step 1: Failing handler test** (`synced_playlists_test.go`, mirror `coverage_test.go`'s harness + a fake `SyncService`): `POST /api/v1/synced-playlists {url}` → 200 with the detail; `GET /api/v1/synced-playlists` → list; `POST /…/{id}/download-missing` → jobs; nil service → 503; bad url → the service returns an error → 422.

- [ ] **Step 2: Run** `go test ./internal/api/ -run Synced` → FAIL.

- [ ] **Step 3: Implement handlers** (`internal/api/synced_playlists.go`) — accessor `s.sync()` under RLock (like `s.coverage()`); 7 handlers mapping body/URL params to the service and `writeJSON`. `POST /synced-playlists` decodes `{url, downloadMissing}`; on a service error from a bad/inaccessible URL return `422` with the error message; nil service → `503`. `PUT /…/settings` decodes `{syncEnabled, intervalSec, autoDownload}`. Follow `internal/api/coverage.go` + `downloads.go` exactly for style (`decode`, `writeJSON`, `chi.URLParam`).

- [ ] **Step 4: Routes** in `server.go` (protected group):

```go
	pr.Post("/synced-playlists", s.handleImportSyncedPlaylist)
	pr.Get("/synced-playlists", s.handleListSyncedPlaylists)
	pr.Get("/synced-playlists/{id}", s.handleSyncedPlaylistDetail)
	pr.Post("/synced-playlists/{id}/sync", s.handleSyncNow)
	pr.Post("/synced-playlists/{id}/download-missing", s.handleSyncedDownloadMissing)
	pr.Put("/synced-playlists/{id}/settings", s.handleSyncedSettings)
	pr.Delete("/synced-playlists/{id}", s.handleDeleteSyncedPlaylist)
```
Add `sync SyncService` to `Deps` + `Server.live`, set in `NewServer`, add `s.sync()` accessor. (Reload swap is optional — the sync service holds the matcher/downloader which are stable; do NOT block on reload. If trivial, swap it like coverage; else leave it built once.)

- [ ] **Step 5: Store adapter + wiring** (`internal/wiring`): a `syncStore` struct over `*db.Queries` implementing `playlistsync.Store` (map `db.*` rows ↔ `playlistsync.SyncedRow`; `Upsert` checks `GetSyncedPlaylistBySource` to return the existing id on conflict; bools ↔ `int64` 0/1). `BuildSyncService`: pick the first enabled search adapter implementing `search.PlaylistProvider` AND wrap it with `ParsePlaylistID` (the spotify pkg func) into a `PlaylistSource`; pass the `matching.Service`, the download `Manager`, the store adapter, `time.Now().Unix`, and a uuid generator. Pass to `api.Deps.Sync`. If no provider → nil service (handlers 503).

- [ ] **Step 6: Start the scheduler** in `cmd/reverb` (where services are built + the server runs): if the sync service exists, `go playlistsync.NewScheduler(svc, 15*time.Minute).Run(ctx)` using the app's root context (cancelled on shutdown).

- [ ] **Step 7: OpenAPI** — document the 7 routes in `internal/api/openapi.yaml` (mirror existing style).

- [ ] **Step 8: Verify** `go test ./...` + `go build ./...` green. Commit:

```bash
git add internal/api/ internal/wiring/ cmd/reverb/ internal/store/
git commit -m "feat(api): synced-playlist endpoints + wiring + scheduler start"
```

---

# Phase 2 — Frontend

### Task 6: FE types + `syncedPlaylistApi`

**Files:**
- Modify: `web/src/lib/types.ts`
- Create: `web/src/lib/syncedPlaylistApi.ts`, `web/src/lib/syncedPlaylistApi.test.tsx`

**Interfaces:**
- Produces: TS `SyncedPlaylist`, `SyncedPlaylistDetail` (mirror §4.2 — `SyncedPlaylistDetail` reuses `AlbumDetailTrack`); hooks `useSyncedPlaylists()`, `useSyncedPlaylist(id)`; fns `importPlaylist(url, downloadMissing)`, `syncNow(id)`, `downloadMissingForPlaylist(id)`, `updateSyncSettings(id, {syncEnabled, intervalSec, autoDownload})`, `deleteSyncedPlaylist(id)`.

- [ ] **Step 1:** Add TS types to `types.ts`:

```ts
export interface SyncedPlaylist {
  id: string; source: string; externalId: string; name: string; coverUrl?: string
  syncEnabled: boolean; syncIntervalSec: number; autoDownload: boolean
  lastSyncedAt: number; trackCount: number
}
export interface SyncedPlaylistDetail extends SyncedPlaylist {
  ownedCount: number; totalCount: number; tracks: AlbumDetailTrack[]
}
```

- [ ] **Step 2: Failing test** (mirror `coverageApi.test.tsx`): `importPlaylist` POSTs `/synced-playlists` with `{url, downloadMissing}`; `useSyncedPlaylist` GETs `/synced-playlists/{id}`; `updateSyncSettings` PUTs the settings body.

- [ ] **Step 3: Implement** `syncedPlaylistApi.ts` (mirror `coverageApi.ts`/`downloadApi.ts` — `useQuery` + `api.get/post/put/del`). `useSyncedPlaylist(id)` query key `['synced-playlist', id]`; `useSyncedPlaylists()` key `['synced-playlists']`.

- [ ] **Step 4: Run** `npx vitest run src/lib/syncedPlaylistApi.test.tsx` (in `web/`) → PASS; `npx tsc --noEmit` clean.

- [ ] **Step 5: Commit** `git commit -am "feat(web): synced-playlist types + api client"`.

---

### Task 7: Import dialog + entry point

**Files:**
- Create: `web/src/components/ImportPlaylistDialog.tsx`, `web/src/components/ImportPlaylistDialog.test.tsx`
- Modify: `web/src/routes/Library.tsx` (entry button)

**Interfaces:** Consumes `importPlaylist`, `useNavigate`. Produces `<ImportPlaylistDialog open onClose />`.

- [ ] **Step 1: Failing test** — entering a URL + clicking Import calls `importPlaylist(url, downloadMissing)` and navigates to `/synced-playlist/{id}`; an import error renders an inline message.

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement** — reuse the existing admin **modal pattern** (read `web/src/components/admin/` / `AdapterForm` for the modal shell). The dialog: a URL `input`, a "Download missing now" `Toggle`/checkbox, **Import** + **Cancel** `Button`s; on submit call `importPlaylist(url, downloadMissing)`, then `navigate('/synced-playlist/${detail.id}')`; on error show the message inline. Tokens + focus-visible. Add an **"Import from Spotify"** `Button` in `Library.tsx`'s playlists section header that opens the dialog.

- [ ] **Step 4: Run** the focused test + full `npx vitest run` + `npx tsc --noEmit` → green.

- [ ] **Step 5: Commit** `git commit -am "feat(web): import-from-Spotify dialog + Library entry point"`.

---

### Task 8: Synced playlist page + schedule settings

**Files:**
- Create: `web/src/routes/SyncedPlaylist.tsx`, `web/src/routes/SyncedPlaylist.test.tsx`
- Modify: `web/src/App.tsx` (route `/synced-playlist/:id`)

**Interfaces:** Consumes `useSyncedPlaylist`, `syncNow`, `downloadMissingForPlaylist`, `updateSyncSettings`, `deleteSyncedPlaylist`, `usePlayer`, the Album-page track rendering pattern.

- [ ] **Step 1: Failing test** — mock `useSyncedPlaylist` → a detail with 2 owned + 1 missing; assert: header "2 of 3 in library" + "Synced …"; owned rows playable; missing row shows Download; "Sync now" calls `syncNow`; "Download all missing" calls `downloadMissingForPlaylist`; the settings control calls `updateSyncSettings`; delete (confirm) calls `deleteSyncedPlaylist` + navigates.

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement** — model on `Album.tsx` (read it). Header: `Cover coverSrc={detail.coverUrl}`, "Synced playlist" eyebrow + a synced badge, name, `"{ownedCount} of {totalCount} in library"` (+ `· {missing} missing`), `"Synced {relativeTime(lastSyncedAt)}"`. Actions: **Play** (owned tracks → `playTrackList`), **Download all missing · N** → `downloadMissingForPlaylist(id)`, **Sync now** → `syncNow(id)` (invalidate `['synced-playlist', id]`), a **"⋯" menu** (reuse the Playlist page's menu pattern) with **Schedule settings** (a `Toggle` for sync on/off, a `Select` Manual/Daily/Weekly mapping to `intervalSec` 0/86400/604800, a `Toggle` auto-download → `updateSyncSettings`) + **Remove** (`window.confirm` → `deleteSyncedPlaylist` → `navigate('/library')`). Tracklist: reuse the Album-page owned/missing row rendering (owned → `TrackRow` + `playTrackList`; missing → `TrackRow` with the `DownloadAction` right slot — build the `ExternalResult` from `externalRef` exactly as `Album.tsx` does). Add the route to `App.tsx`. Tokens only; mutations wrapped in try/catch (per the playlist-page error-handling precedent).

- [ ] **Step 4: Run** focused + full `npx vitest run` + `npm run build` → green.

- [ ] **Step 5: Commit** `git commit -am "feat(web): synced playlist page + schedule settings"`.

---

### Task 9: Library integration (rail + grid badges + routing)

**Files:** Modify `web/src/components/shell/LibraryRail.tsx`, `web/src/routes/Library.tsx`.

**Interfaces:** Consumes `useSyncedPlaylists`.

- [ ] **Step 1: Failing tests** — `LibraryRail` and `Library` render synced playlists (from a mocked `useSyncedPlaylists`) alongside library playlists, each with a synced badge, and clicking one navigates to `/synced-playlist/{id}` (library playlists still go to `/playlist/{id}`).

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement** — in both surfaces, fetch `useSyncedPlaylists()` and render those entries in the playlists section with a small synced/Spotify badge (a `Badge` or an `Icon`), navigating to `/synced-playlist/${p.id}`. Keep library playlists unchanged. Tokens only.

- [ ] **Step 4: Run** full `npx vitest run` + `npm run build` → green.

- [ ] **Step 5: Commit** `git commit -am "feat(web): surface synced playlists in rail + library grid"`.

---

# Phase 3 — e2e + verification

### Task 10: Playwright e2e — the sync flow

**Files:** Create `web/e2e/playlist-sync.spec.ts`; extend `web/e2e/mocks.ts`.

- [ ] **Step 1: Write the e2e** (mirror `completeness.spec.ts` + its mocks; served-once-then-204 not needed here — these are plain JSON endpoints). Mock: `POST /synced-playlists` → a detail (2 owned + 1 missing, the missing `externalId='ext-miss-1'`); `GET /synced-playlists` → `[that]`; `GET /synced-playlists/{id}` → stateful (after download, the missing track is owned); `POST /…/download-missing` → enqueues; reuse the WS `complete()` to flip the track; `POST /…/sync` → returns a detail with one more track. Flow: login → open Import dialog → paste URL → Import → land on synced playlist page → see "2 of 3 in library" + a missing-row Download → download it → `ws.complete()` → flips owned → "Sync now" → an added track appears.
- [ ] **Step 2:** Run `npm run e2e` (in `web/`) twice → green + stable; core-loop + completeness still pass.
- [ ] **Step 3: Commit** `git commit -am "test(e2e): import playlist -> have/missing -> download -> sync"`.

### Task 11: Full verification + whole-branch review

- [ ] **Step 1:** `go test ./...`, `npx vitest run`, `npm run build`, `npm run e2e` — all green.
- [ ] **Step 2:** Use `superpowers:requesting-code-review` for a whole-branch review (base = `git merge-base main HEAD`).
- [ ] **Step 3:** Use `superpowers:finishing-a-development-branch`.

---

## Self-Review Notes

- **Spec coverage:** import public playlist by URL (T1,T3,T5,T7); Reverb-managed synced playlist + live matching (T2,T3); download missing (T3,T5,T8); manual Sync now (T3,T5,T8) + scheduler (T4,T5); library integration (T9); edges — bad/inaccessible URL → 422, no-Spotify → 503 (T5), sync-failure keeps tracklist (T3), pagination (T1), scheduler sequential log-continue (T4); testing incl. e2e (T10). All spec §§1–8 map to tasks.
- **Type consistency:** `SyncedPlaylist`/`SyncedPlaylistDetail` identical Go (T3) ↔ TS (T6); `SyncedPlaylistDetail` reuses `core.AlbumDetailTrack`/`CoverageState` from sub-project A; the API `SyncService` interface (T5) matches `playlistsync.Service` methods (T3) incl. `DownloadMissing`; `Store` interface (T3) ↔ the wiring `syncStore` adapter (T5).
- **Deferred (spec §1 non-goals):** user OAuth / private playlists; editorial playlists (Spotify policy); Navidrome materialization. Not in any task by design.
