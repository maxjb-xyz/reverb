# Listening History & Stats (SP3-3a) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Record per-user play history keyed by the durable `catalog_id`, and surface a your_spotify-class stats dashboard (cards, listening-over-time graph, hour×weekday heatmap, top tracks/artists/albums, recently played) over any user-chosen timeframe.

**Architecture:** A lean `plays` table (FK `catalog_id`) written by a new `internal/play` service (mints the catalog id on play via the merged `catalog.Service`, direct INSERT). Compute-on-read stats queries, range-parameterized `(from,to)`. A FE `playTracker` scores the play threshold off the audio engine; a `/stats` React dashboard with a range selector (presets · calendar-aligned · custom) and hand-rolled SVG charts.

**Tech Stack:** Go (chi, modernc sqlite, goose, sqlc), React 19/TS (Vite, Zustand, TanStack Query, Tailwind tokens, Vitest, Playwright).

## Global Constraints

- **Generated code is generated:** never hand-edit `internal/store/db/*.sql.go`/`models.go`. Edit `internal/store/queries/*.sql` + run `make gen`. Migrations are goose; **latest applied is `0019_catalog.sql`** — add `0020_plays.sql`, never edit applied ones.
- **Builds on the merged foundation:** `catalog.CanonicalFor(ctx, catalog.Identity{Kind,Title,Artist,Album,ISRC,MBID,Source,ExternalID,DurationMs}) (string, error)` and `resolver.Service.Resolve(ctx, catalogID) (resolver.Addressing{BackendID,CoverArtID string;Found bool}, error)`. `catalog.NewService(q, now func() time.Time, idgen func() string)` is **not wired yet** — this plan wires it.
- **Per-user-private:** every plays/stats query is SQL-scoped on `user_id`; user identity comes from `currentUser(r)`, **never** the request body.
- **Play threshold (Last.fm rule):** `durationMs > 30000 && (msPlayed >= durationMs/2 || msPlayed >= 240000)`.
- **Module Go 1.23:** use `context.Background()` in tests (not `t.Context()`). **Design tokens only** in FE — no raw hex, no `text-black`/`text-white`; accent surfaces use `text-on-accent`.
- **Gate before merge:** `go test ./... && go build ./... && go vet ./...`; from `web/`: `npx vitest run && npx tsc --noEmit && npm run build && npm run e2e`.
- **Commit footer:** end every commit with `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`. **Branch:** `feat/listening-history-stats` (already created; spec is its first commit). Never commit to `main`.
- **ids:** TEXT; timestamps INTEGER unix-seconds.

---

## File Structure
| File | Responsibility |
|---|---|
| `internal/store/migrations/0020_plays.sql` | `plays` table (additive). |
| `internal/store/queries/plays.sql` | InsertPlay, RepointPlays, + all stats aggregation queries. |
| `internal/play/service.go` | `Service.Record` (mint + insert). |
| `internal/play/stats.go` | `Stats` read methods (compute-on-read). |
| `internal/api/plays.go` | `POST /plays`. |
| `internal/api/stats.go` | `GET /stats/*`. |
| `internal/resolver/resolver.go` | §4 fix: bypass `match_cache` for empty-external entities. |
| `internal/catalog/merge.go` | extend `merge` to repoint `plays.catalog_id`. |
| `cmd/reverb/main.go`, `internal/api/server.go` | wire `catalog.Service`, `play.Service`, `play.Stats` into Deps + routes. |
| `web/src/lib/playTracker.ts`, `playApi.ts` | threshold scoring + `POST /plays`. |
| `web/src/lib/statsApi.ts` | stats fetches. |
| `web/src/routes/Stats.tsx`, `web/src/components/stats/*` | dashboard + charts + `RangeSelector`. |
| `web/src/routes/Home.tsx`, `Album.tsx`, `Artist.tsx` | woven surfaces. |

---

## Phase 1 — Data, ingest, prerequisite fixes

### Task 1: Migration `0020_plays` + queries

**Files:** Create `internal/store/migrations/0020_plays.sql`, `internal/store/queries/plays.sql`; Modify (generated) `internal/store/db/*`; Test `internal/store/migrate_plays_test.go`.

**Interfaces — Produces:** `db.Play` model; `InsertPlay`, `RepointPlays`, `ListRecentPlays` query methods (stats queries added in Tasks 7–8).

- [ ] **Step 1: Write `0020_plays.sql`**
```sql
-- +goose Up
CREATE TABLE plays (
  id          TEXT PRIMARY KEY,
  user_id     TEXT NOT NULL,
  catalog_id  TEXT NOT NULL REFERENCES catalog_entity(id),
  played_at   INTEGER NOT NULL,
  ms_played   INTEGER NOT NULL,
  completed   INTEGER NOT NULL DEFAULT 0,
  created_at  INTEGER NOT NULL
);
CREATE INDEX idx_plays_user_time    ON plays(user_id, played_at);
CREATE INDEX idx_plays_user_catalog ON plays(user_id, catalog_id);

-- +goose Down
DROP TABLE plays;
```
- [ ] **Step 2: Write `plays.sql` (ingest + merge queries; stats added later)**
```sql
-- name: InsertPlay :exec
INSERT INTO plays (id, user_id, catalog_id, played_at, ms_played, completed, created_at)
VALUES (?,?,?,?,?,?,?);

-- name: RepointPlays :exec
UPDATE plays SET catalog_id = ? WHERE catalog_id = ?;

-- name: ListRecentPlays :many
SELECT p.id, p.catalog_id, p.played_at, e.title, e.artist, e.album
FROM plays p JOIN catalog_entity e ON e.id = p.catalog_id
WHERE p.user_id = ? AND p.played_at < ?
ORDER BY p.played_at DESC LIMIT ?;
```
- [ ] **Step 3:** Run `make gen`; verify `go build ./internal/store/...`.
- [ ] **Step 4: Additive-migration test** (mirror `internal/store/migrate_catalog_test.go`)
```go
func TestMigration0020_Additive(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	var n int
	if err := st.DB().QueryRowContext(ctx, "SELECT count(*) FROM plays").Scan(&n); err != nil {
		t.Fatalf("plays table missing: %v", err)
	}
	if n != 0 { t.Fatalf("plays should start empty, got %d", n) }
}
```
- [ ] **Step 5:** Run `go test ./internal/store/ -run TestMigration0020 -v` → PASS. (Mirror the real `newTestStore`/`st.DB()` helpers.)
- [ ] **Step 6: Commit** `git add internal/store/migrations/0020_plays.sql internal/store/queries/plays.sql internal/store/db/ internal/store/migrate_plays_test.go && git commit -m "feat(plays): additive 0020 plays table + queries\n\nCo-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"`

---

### Task 2: Resolver §4 fix — bypass `match_cache` for empty-external entities

**Files:** Modify `internal/resolver/resolver.go` (`rematchAndStore`); Test `internal/resolver/resolver_test.go`.

**Interfaces — Consumes:** the matcher's `match_cache` behavior. **Produces:** no signature change; behavioral fix.

**Context:** A pure-library catalog entity has `source=""`, `external_id=""`. The matcher consults `match_cache` keyed on `(source, external_id)` — so `("","")` collides all pure-library entities onto one cache row. The resolver re-matches via `s.matcher().Match(core.ExternalResult{...})`; when the entity has no external key, the matcher must not read/write the shared `match_cache`. The matcher's `Match` already only uses `match_cache` when `(Source,ExternalID)` is non-empty? **Verify** in `internal/matching/matching.go`: if `Match` unconditionally caches by `(Source,ExternalID)`, the fix is to make the resolver pass a signal — but simplest: the matcher already keys the cache row by `(source,external_id)`; an empty key writes a row keyed `("","")`. So the fix lives in the matcher: skip `match_cache` get/upsert when `ext.Source==""||ext.ExternalID==""`. Implement there (it's the correct home) and assert via the resolver.

- [ ] **Step 1: Write failing test** (two distinct pure-library entities resolve to different backend ids — no collision)
```go
func TestResolve_PureLibraryEntitiesDoNotCollide(t *testing.T) {
	s, q, fm := newTestResolverPerCatalog(t) // fm returns LibraryTrackID = "nav-"+title (distinct per entity)
	ctx := context.Background()
	a := seedEntity(t, q, "", "SongA", "Artist", "Album", 200000) // source/external empty
	b := seedEntity(t, q, "", "SongB", "Artist", "Album", 200000)
	ra, _ := s.Resolve(ctx, a)
	rb, _ := s.Resolve(ctx, b)
	if ra.BackendID == rb.BackendID {
		t.Fatalf("pure-library entities must not collide: %q == %q", ra.BackendID, rb.BackendID)
	}
}
```
> `seedEntity` currently takes `externalID`; add a variant or pass `""` for source+external. The fake matcher must return a per-title backend id so a *shared* cache row would be observable as a collision.
- [ ] **Step 2:** `go test ./internal/resolver/ -run TestResolve_PureLibrary -v` → FAIL (collision via shared `("","")` cache row).
- [ ] **Step 3: Fix** in `internal/matching/matching.go` `Match`: guard the `match_cache` read and the write-through with `if ext.Source != "" && ext.ExternalID != ""`. When empty, compute the match fresh and return it without touching `match_cache` (the resolver's `backend_binding` still caches per `catalog_id`, so there's no perf regression).
- [ ] **Step 4:** `go test ./internal/matching/ ./internal/resolver/ -v` → PASS. `go build ./... && go vet ./...`.
- [ ] **Step 5: Commit** `feat(matching): skip match_cache for empty-external results (pure-library entities)`.

---

### Task 3: Extend `catalog.merge` to repoint `plays.catalog_id`

**Files:** Modify `internal/catalog/merge.go`, `internal/catalog/catalog.go` (Querier), `internal/store/queries/plays.sql` (RepointPlays already added in Task 1); Test `internal/catalog/merge_test.go`.

**Interfaces — Consumes:** `RepointPlays(ctx, db.RepointPlaysParams{CatalogID, CatalogID_2})` (Task 1). **Produces:** `merge` now also repoints plays.

- [ ] **Step 1: Write failing test** — two entities each with a play; merge; the loser's play now points at the winner.
```go
func TestMerge_RepointsPlays(t *testing.T) {
	s := newTestService(t); ctx := context.Background()
	// mint loser+winner, insert a play row referencing the loser, force a merge, assert the play's catalog_id == winner.
	// (Use the same merge trigger as TestCanonicalFor_MergesWhenISRCArrivesLater; insert the play via q.InsertPlay before the merge.)
}
```
- [ ] **Step 2:** `go test ./internal/catalog/ -run TestMerge_RepointsPlays -v` → FAIL.
- [ ] **Step 3: Implement** — add `RepointPlays(ctx, db.RepointPlaysParams) error` to the catalog `Querier` interface; in `merge(loser, winner)`, after repointing aliases + bindings, call `s.q.RepointPlays(ctx, db.RepointPlaysParams{CatalogID: winner, CatalogID_2: loser})` (confirm generated param field names). Add a doc line that `plays` is the first stored consumer reference.
- [ ] **Step 4:** `go test ./internal/catalog/ -v` → PASS.
- [ ] **Step 5: Commit** `feat(catalog): merge repoints plays.catalog_id (history consolidates)`.

---

### Task 4: `play.Service.Record` + wire `catalog.Service`

**Files:** Create `internal/play/service.go`; Modify `cmd/reverb/main.go` (construct `catalog.Service` + `play.Service`); Test `internal/play/service_test.go`.

**Interfaces — Produces:**
- `type PlayInput struct { LibraryTrackID, Title, Artist, Album, ISRC string; DurationMs, MsPlayed int; Completed bool; PlayedAt int64 }`
- `type Service struct{...}`; `func NewService(q Querier, cat CanonicalMinter, now func() time.Time, idgen func() string) *Service`
- `type CanonicalMinter interface { CanonicalFor(ctx context.Context, id catalog.Identity) (string, error) }` (satisfied by `*catalog.Service`)
- `func (s *Service) Record(ctx context.Context, userID string, in PlayInput) error`

- [ ] **Step 1: Write failing test** — Record mints a catalog id and inserts a user-scoped play.
```go
func TestRecord_MintsCatalogAndInsertsPlay(t *testing.T) {
	s, q := newTestPlayService(t) // wraps real sqlite + a real catalog.Service
	ctx := context.Background()
	err := s.Record(ctx, "user-1", PlayInput{Title:"Hurt", Artist:"Johnny Cash", Album:"American IV", DurationMs:218000, MsPlayed:140000, Completed:true, PlayedAt:1719000000})
	if err != nil { t.Fatal(err) }
	rows, _ := q.ListRecentPlays(ctx, db.ListRecentPlaysParams{UserID:"user-1", PlayedAt: 9999999999, Limit: 10})
	if len(rows) != 1 || rows[0].Title != "Hurt" { t.Fatalf("play not recorded: %+v", rows) }
}
```
- [ ] **Step 2:** `go test ./internal/play/ -run TestRecord -v` → FAIL.
- [ ] **Step 3: Implement** `service.go`:
```go
func (s *Service) Record(ctx context.Context, userID string, in PlayInput) error {
	cid, err := s.cat.CanonicalFor(ctx, catalog.Identity{
		Kind: "track", Title: in.Title, Artist: in.Artist, Album: in.Album,
		ISRC: in.ISRC, DurationMs: in.DurationMs, // Source/ExternalID empty: FE has no track-level external id
	})
	if err != nil { return err }
	played := in.PlayedAt; if played == 0 { played = s.now().Unix() }
	completed := int64(0); if in.Completed { completed = 1 }
	return s.q.InsertPlay(ctx, db.InsertPlayParams{
		ID: s.idgen(), UserID: userID, CatalogID: cid, PlayedAt: played,
		MsPlayed: int64(in.MsPlayed), Completed: completed, CreatedAt: s.now().Unix(),
	})
}
```
- [ ] **Step 4: Wire** in `main.go`: construct `catalogSvc := catalog.NewService(st.Q(), time.Now, uuid.NewString)` and `playSvc := play.NewService(st.Q(), catalogSvc, time.Now, uuid.NewString)`. **idgen = `uuid.NewString`** (`github.com/google/uuid` v1.6.0, already a dep — the pattern used by `notification`/`request` services). The catalog prefixes (`trk_`/…) are added inside `CanonicalFor`, so a bare uuid is correct.
- [ ] **Step 5:** `go test ./internal/play/ -v && go build ./...` → PASS.
- [ ] **Step 6: Commit** `feat(play): Record mints catalog id + inserts a user-scoped play`.

---

### Task 5: `POST /plays` endpoint

**Files:** Create `internal/api/plays.go`; Modify `internal/api/server.go` (Deps.Plays + route); Test `internal/api/plays_test.go`.

**Interfaces — Consumes:** `play.Service.Record`. **Produces:** `Deps.Plays *play.Service`; `POST /api/v1/plays`.

- [ ] **Step 1: Write failing tests** — authed POST records the play for the *session* user (not a body-supplied user); 401 unauthenticated; user from session.
```go
func TestHandlePlay_RecordsForSessionUser(t *testing.T) { /* POST valid body as user-1 → 204/200; play exists for user-1 */ }
func TestHandlePlay_IgnoresBodyUserID(t *testing.T) { /* body cannot set another user's id; recorded under the session user */ }
```
- [ ] **Step 2:** `go test ./internal/api/ -run TestHandlePlay -v` → FAIL.
- [ ] **Step 3: Implement** `plays.go`: decode the JSON body into a `play.PlayInput`, get `userID := currentUser(r).ID`, call `s.deps.Plays.Record(ctx, userID, in)`, respond `204`. Register `pr.Post("/plays", s.handlePlay)` in `server.go` (in the `requireAuth` group); add `Plays *play.Service` to `Deps` and set it in `main.go`. Nil-Plays → `503` (mirror other nil-dep guards).
- [ ] **Step 4:** `go test ./internal/api/ -v && go build ./...` → PASS.
- [ ] **Step 5: Commit** `feat(api): POST /plays records a qualified play (session-scoped)`.

---

### Task 6: FE `playTracker` + `POST /plays`

**Files:** Create `web/src/lib/playTracker.ts`, `web/src/lib/playApi.ts`; Modify the app bootstrap (start the tracker once, e.g. in `realtimeWiring.ts` or `App.tsx`); Test `web/src/lib/playTracker.test.ts`.

**Interfaces — Consumes:** `engine.subscribe((s: PlayerState)=>void)`, `engine.getState()`. **Produces:** `recordPlay(input)` (POST /plays); `startPlayTracker(engine, recordFn)` → unsubscribe.

- [ ] **Step 1: Write failing tests** (drive a fake engine/subscription):
```ts
// qualifies at >=50% of a >30s track → recordFn called once with msPlayed/completed
// a <30s track never qualifies
// seek-backward does not double-count toward the threshold
// repeat-one re-loop fires a SECOND qualified play
// a track change finalizes the outgoing track before current.id flips
```
- [ ] **Step 2:** `cd web && npx vitest run src/lib/playTracker.test.ts` → FAIL.
- [ ] **Step 3: Implement** `playTracker.ts`: subscribe to the engine; maintain `{ currentId, lastTime, msPlayed, fired }`. On each state, if `current.id` changed → finalize the previous (if it had qualified-but-not-fired, it already fired; reset). Accrue `msPlayed += max(0, currentTimeMs - lastTime)` only when `playing` and the delta is small+positive (a backward jump = seek → reset `lastTime` without accruing). Qualify with the threshold once a settled `durationMs>0`; on qualify call `recordPlay({libraryTrackId: current.id, title, artist, album, durationMs, isrc, msPlayed, completed})` and set `fired`. Repeat-one: detect a backward jump to ~0 on the SAME id with `fired` → reset for a new play. `playApi.recordPlay` POSTs via the `api` helper.
- [ ] **Step 4:** `cd web && npx vitest run src/lib/playTracker.test.ts && npx tsc --noEmit` → PASS.
- [ ] **Step 5: Commit** `feat(web): playTracker scores qualified plays + POST /plays`.

---

## Phase 2 — Stats API

### Task 7: Stats queries + service — summary + top tracks/artists/albums

**Files:** Modify `internal/store/queries/plays.sql`; Create `internal/play/stats.go`; Test `internal/play/stats_test.go`.

**Interfaces — Produces:**
- `type Stats struct{ q StatsQuerier }`; `func NewStats(q StatsQuerier) *Stats`
- `Summary(ctx, userID string, from, to int64) (SummaryStats, error)` where `SummaryStats{Plays, DistinctTracks, DistinctArtists, DistinctAlbums int; MsPlayed int64}`
- `TopTracks/TopArtists/TopAlbums(ctx, userID string, from, to int64, limit int) ([]TopRow, error)` where `TopRow{CatalogID, Title, Artist, Album string; Plays int; MsPlayed int64}`

- [ ] **Step 1: Write failing tests** — seed plays across a window; assert summary counts + ordering/limit of top lists; assert plays outside `[from,to)` are excluded; assert per-user isolation.
- [ ] **Step 2:** `go test ./internal/play/ -run TestStats -v` → FAIL.
- [ ] **Step 3: Implement** the sqlc queries (windowed `GROUP BY`, `JOIN catalog_entity`, `ORDER BY COUNT(*) DESC LIMIT ?`, all `WHERE user_id = ? AND played_at >= ? AND played_at < ?`) + the `Stats` methods mapping rows → the structs.
- [ ] **Step 4:** `go test ./internal/play/ -v` → PASS. `make gen` if queries added.
- [ ] **Step 5: Commit** `feat(play): stats summary + top tracks/artists/albums (compute-on-read)`.

---

### Task 8: Stats — timeline + clock (heatmap) + recent + entity

**Files:** Modify `internal/store/queries/plays.sql`, `internal/play/stats.go`; Test `internal/play/stats_test.go`.

**Interfaces — Produces:**
- `Timeline(ctx, userID string, from, to int64, bucket string) ([]TimeBucket, error)` — `TimeBucket{Start int64; Plays int; MsPlayed int64}`
- `Clock(ctx, userID string, from, to int64, tzOffsetMin int) ([]ClockCell, error)` — `ClockCell{Weekday, Hour, Plays int; MsPlayed int64}` (7×24)
- `Recent(ctx, userID string, before int64, limit int) ([]RecentRow, error)`
- `Entity(ctx, userID, kind, id string, from, to int64) (EntityStats, error)`

- [ ] **Step 1: Write failing tests** — timeline buckets plays by day given a `from/to`; clock buckets by local weekday/hour given a tz offset (seed a play at a known UTC time, assert it lands in the expected local cell); recent returns newest-first with cursor; entity aggregates for an artist name + a track catalog_id.
- [ ] **Step 2:** `go test ./internal/play/ -run 'TestTimeline|TestClock|TestRecent|TestEntity' -v` → FAIL.
- [ ] **Step 3: Implement.** Timeline: bucket via integer math on `played_at` (day = `played_at/86400`; week/month computed in Go from the bucketed days, or via SQL date functions if available — keep it deterministic). Clock: `localSec = played_at + tzOffsetMin*60`; `weekday = (localSec/86400 + 4) % 7`; `hour = (localSec % 86400)/3600` — do this in SQL (`strftime`) or in Go after a windowed select of `played_at` (a windowed scan is fine at personal scale). Recent: the `ListRecentPlays` from Task 1. Entity: `WHERE user_id=? AND (artist=? | catalog_id=?) AND played_at∈[from,to)`.
- [ ] **Step 4:** `go test ./internal/play/ -v` → PASS.
- [ ] **Step 5: Commit** `feat(play): timeline, clock heatmap, recent, per-entity stats`.

---

### Task 9: Stats API endpoints

**Files:** Create `internal/api/stats.go`; Modify `internal/api/server.go` (Deps.Stats + routes); Test `internal/api/stats_test.go`.

**Interfaces — Consumes:** `play.Stats`. **Produces:** `Deps.Stats *play.Stats`; `GET /api/v1/stats/{summary,top/tracks,top/artists,top/albums,timeline,clock,recent,entity}`.

- [ ] **Step 1: Write failing tests** — each endpoint returns session-user-scoped JSON; `from`/`to` parsed from query; `limit`/`bucket`/`tzOffsetMinutes` parsed with sane defaults; a second user's plays never appear.
- [ ] **Step 2:** `go test ./internal/api/ -run TestStats -v` → FAIL.
- [ ] **Step 3: Implement** thin handlers: parse query params (`from,to` int64; `limit` default 50; `bucket` default "day"; `tzOffsetMinutes` default 0), `userID := currentUser(r).ID`, call the `play.Stats` method, `writeJSON`. Register the routes in the `requireAuth` group; add `Stats *play.Stats` to `Deps` + `main.go`.
- [ ] **Step 4:** `go test ./internal/api/ -v && go build ./...` → PASS.
- [ ] **Step 5: Commit** `feat(api): GET /stats/* (range-parameterized, per-user)`.

---

## Phase 3 — The dashboard FE

### Task 10: `statsApi` + `RangeSelector`

**Files:** Create `web/src/lib/statsApi.ts`, `web/src/components/stats/RangeSelector.tsx`, `web/src/lib/range.ts`; Test `web/src/lib/range.test.ts`, `web/src/components/stats/RangeSelector.test.tsx`.

**Interfaces — Produces:**
- `range.ts`: `type Range = { from: number; to: number; bucket: 'day'|'week'|'month'; tzOffsetMinutes: number; label: string }`; `presetRange(key)` for `'7d'|'30d'|'90d'|'year'|'all'|'thisWeek'|'thisMonth'|'thisYear'`; `customRange(startDate, endDate)`. All compute `from`/`to` in **local** time; `tzOffsetMinutes = -new Date().getTimezoneOffset()`.
- `statsApi.ts`: `summary(r)`, `topTracks(r,limit)`, `topArtists`, `topAlbums`, `timeline(r)`, `clock(r)`, `recent(before,limit)`, `entity(kind,id,r)`.
- `RangeSelector`: renders preset chips + calendar-aligned chips + a custom date-range popover; calls `onChange(range)`.

- [ ] **Step 1: Write failing tests** — `presetRange('7d')` → `to≈now`, `from≈now-7d`, bucket 'day'; `presetRange('thisMonth')` → from = local 1st-of-month 00:00; `customRange` → inclusive start, exclusive next-day end; bucket auto by span. RangeSelector fires `onChange` with the right range on chip click.
- [ ] **Step 2:** `cd web && npx vitest run src/lib/range.test.ts src/components/stats/RangeSelector.test.tsx` → FAIL.
- [ ] **Step 3: Implement** `range.ts` (Date math, local TZ; bucket = span≤45d?'day':span≤550d?'week':'month'), `statsApi.ts` (use the `api` helper; serialize `from,to,bucket,limit,tzOffsetMinutes`), `RangeSelector.tsx` (token-styled chips + a custom-range popover; default 'last 30 days').
- [ ] **Step 4:** `cd web && npx vitest run src/lib/range.test.ts src/components/stats/RangeSelector.test.tsx && npx tsc --noEmit` → PASS.
- [ ] **Step 5: Commit** `feat(web): statsApi + RangeSelector (presets, calendar-aligned, custom range)`.

---

### Task 11: `/stats` page — cards + top-lists + recently-played

**Files:** Create `web/src/routes/Stats.tsx`, `web/src/components/stats/{SummaryCards,TopList,RecentList}.tsx`; Modify `web/src/App.tsx` (route), the sidebar/nav; Test `web/src/routes/Stats.test.tsx`.

**Interfaces — Consumes:** `statsApi`, `RangeSelector`. **Produces:** the `/stats` route.

- [ ] **Step 1: Write failing test** — Stats renders summary cards, top-tracks list (with covers via `trackCoverUrl`/`coverUrl(catalogId)`), recently-played; changing the range refetches (assert the api called with new from/to).
- [ ] **Step 2:** `cd web && npx vitest run src/routes/Stats.test.tsx` → FAIL.
- [ ] **Step 3: Implement** `Stats.tsx`: a `RangeSelector` at top; TanStack Query hooks keyed by the range for summary/top/recent; `SummaryCards` (songs · time · distinct counts; format ms→h:m), `TopList` (ranked rows, cover + count + time, link to detail), `RecentList`. Add `<Route path="/stats" element={<Stats/>}/>` and a nav entry. Strictly token-styled.
- [ ] **Step 4:** `cd web && npx vitest run src/routes/Stats.test.tsx && npx tsc --noEmit` → PASS.
- [ ] **Step 5: Commit** `feat(web): /stats dashboard — cards, top-lists, recently played`.

---

### Task 12: Charts — listening-over-time + hour×weekday heatmap (hand-rolled SVG)

**Files:** Create `web/src/components/stats/{TimelineChart,ClockHeatmap}.tsx`; Modify `web/src/routes/Stats.tsx`; Test their `.test.tsx`.

**Interfaces — Consumes:** `statsApi.timeline`, `statsApi.clock`. **Produces:** the two chart components.

- [ ] **Step 1: Write failing tests** — TimelineChart renders one bar/point per bucket with correct relative heights (assert SVG `rect`/`path` count + that the max-value bucket is tallest); ClockHeatmap renders a 7×24 grid with cell intensity scaled to plays (assert 168 cells + the busiest cell has the strongest fill class/opacity).
- [ ] **Step 2:** `cd web && npx vitest run src/components/stats/TimelineChart.test.tsx src/components/stats/ClockHeatmap.test.tsx` → FAIL.
- [ ] **Step 3: Implement** hand-rolled SVG: `TimelineChart` (area or bars; X = bucketStart formatted by bucket, Y = plays/minutes toggle; token colors via `currentColor`/accent classes, axis labels, a hover tooltip). `ClockHeatmap` (7 rows × 24 cols of `<rect>`; fill opacity = `plays/maxPlays`; weekday + hour labels; tooltip). No charting dependency. Token-styled, responsive.
- [ ] **Step 4:** `cd web && npx vitest run src/components/stats/ && npx tsc --noEmit` → PASS. Wire both into `Stats.tsx`.
- [ ] **Step 5: Commit** `feat(web): listening-over-time chart + when-you-listen heatmap (SVG)`.

---

### Task 13: Woven surfaces — Home carousel + detail-page stats

**Files:** Modify `web/src/routes/Home.tsx` (recently-played carousel → real `statsApi.recent`), `web/src/routes/Album.tsx`/`Artist.tsx` (per-entity stat strip via `statsApi.entity` + per-track "played N×"); Tests update the affected specs.

**Interfaces — Consumes:** `statsApi.recent`, `statsApi.entity`.

- [ ] **Step 1: Write/extend failing tests** — Home's recently-played carousel renders from `statsApi.recent`; Album page shows "you · N plays · X hrs" + a per-track "played N×" badge from `statsApi.entity`.
- [ ] **Step 2:** `cd web && npx vitest run src/routes/Home.test.tsx src/routes/Album.test.tsx` → FAIL.
- [ ] **Step 3: Implement** — replace Home's approximated "Jump back in" source with `statsApi.recent` (keep the existing `Carousel`); add a compact stat strip + per-track count to Album/Artist using `statsApi.entity`. Token-styled; gracefully hide when there's no history.
- [ ] **Step 4:** `cd web && npx vitest run && npx tsc --noEmit` → PASS.
- [ ] **Step 5: Commit** `feat(web): real recently-played on Home + per-entity stats on detail pages`.

---

### Task 14: Full gate + whole-branch review + merge

- [ ] **Step 1:** `go test ./... && go build ./... && go vet ./...` → all PASS.
- [ ] **Step 2:** `cd web && npx vitest run && npx tsc --noEmit && npm run build && npm run e2e` → green (re-run e2e once if the known `net::ERR_ABORTED` flake appears; if memory-pressured, run failing specs in isolation).
- [ ] **Step 3:** Add a hermetic e2e (`web/e2e/stats.spec.ts`): play a track past threshold → it appears in recently-played; `/stats` renders cards/timeline/heatmap/top-lists; switching the range refetches.
- [ ] **Step 4:** Whole-branch review via `superpowers:requesting-code-review` (per-user scoping airtight; the playTracker double-count traps; the §4 fix; token compliance; chart accessibility). Address Critical/Important.
- [ ] **Step 5:** `git checkout main && git merge --ff-only feat/listening-history-stats` (do NOT push). Tell the user to push + rebuild + verify on soulkiller.

---

## Notes for the implementer
- **Test harness:** mirror `internal/store`, `internal/catalog`, `internal/resolver`, `internal/notification` test setups (real in-memory sqlite via `store.Open`+`Migrate`+`st.Q()`). FE: mirror existing route/component tests + the injectable `AudioElement` pattern in `audioEngine.test.ts`.
- **sqlc param names:** confirm generated `...Params` field names after `make gen` (esp. `RepointPlaysParams.CatalogID_2`, the stats query params) and adjust call sites.
- **idgen:** `uuid.NewString` (github.com/google/uuid, already used by `notification`/`request` services).
- **Don't gold-plate:** genre/audio-feature stats, dedicated per-entity stat pages, Wrapped, and Last.fm are explicitly out of scope (spec §1).
