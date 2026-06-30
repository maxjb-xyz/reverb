# Listening History & Stats (SP3-3a) — Design Spec

> Record what each user listens to and surface a **your_spotify-class stats dashboard** — summary cards, listening-over-time graphs, a "when do you listen" hour×weekday heatmap, top tracks/artists/albums, recently played, and per-entity stats woven into the detail pages — over **any timeframe the user picks** (rolling presets, calendar-aligned periods, or a custom calendar range). This is **SP3-3a**: the *internal* history + stats sub-project of the Social/Multi-User epic. **Last.fm scrobbling (3b) is a separate later cycle** and is out of scope here.

- **Status:** Approved design (brainstormed 2026-06-29). Ready for implementation planning.
- **Author:** Reverb maintainer + Claude.
- **Builds on (now merged):** the **library identity foundation** — `internal/catalog` (`CanonicalFor(Identity) → catalog_id`, the durable per-entity id; `merge` repoints stored consumer refs) and `internal/resolver` (`Resolve(catalog_id) → current backend cover/track id`). A play row references `catalog_id`, so history is churn-safe by construction and survives a catalog merge. **SP3-3a is the first production producer of canonical ids — it activates the (currently dormant) catalog.**
- **Also builds on:** the FE audio engine (`web/src/lib/audioEngine.ts` `subscribe`/`getState`), the per-user auth/scoping model (the notifications feature's airtight per-user SQL scoping is the reference), `internal/api/settings.go`, goose + sqlc, the existing Home carousels + Album/Artist detail pages.
- **North star:** *own your music, again* — your listening data is yours, private, and richly explorable, on a self-hosted app with no external dependency.

---

## 1. Goals & Non-Goals

### Goals
1. **Record every qualified play** per user, keyed by the durable `catalog_id`, so history survives library re-matches / backend swaps / merges.
2. **A genuinely useful, your_spotify-class stats dashboard** — not a few lists. Cards + time-series + a when-you-listen heatmap + ranked top-lists + recently-played + per-entity drill-down.
3. **Arbitrary timeframe selection** — rolling presets, calendar-aligned periods (this week/month/year), and a custom calendar range — all over one range-parameterized API.
4. **Strictly per-user-private.** No cross-user leakage (SQL-scoped on every query). Public/shared stats are SP4.
5. **No external dependency.** Everything computed from local play events. (Spotify-metadata-dependent stats — genres, audio features, popularity — are explicitly deferred.)

### Non-Goals (this cycle)
- **Last.fm scrobbling + now-playing** (`track.scrobble` / `track.updateNowPlaying`) → **SP3-3b**, separate cycle.
- **Genre / audio-feature / popularity analysis** — needs a Spotify (or MusicBrainz) enrichment pass Reverb doesn't have; deferred to a later cycle.
- **Reverb Wrapped** (year-in-review) — builds on this data later.
- **Live "now playing" presence / listening parties** — SP4.
- **Dedicated per-entity *stat* pages** (a full your_spotify artist-stats page) — v1 folds per-entity stats into the existing Album/Artist detail pages; dedicated stat pages are a later refinement.

---

## 2. Data model

One new table (migration `0020_plays.sql` — latest applied is `0019_catalog`). **Lean**: it references the catalog rather than denormalizing metadata, because `catalog_entity` already holds the durable title/artist/album/duration.

```sql
CREATE TABLE plays (
  id          TEXT PRIMARY KEY,
  user_id     TEXT NOT NULL,                       -- per-user scoping (every query filters this)
  catalog_id  TEXT NOT NULL REFERENCES catalog_entity(id),  -- durable track identity (kind='track')
  played_at   INTEGER NOT NULL,                    -- play START, unix seconds (UTC)
  ms_played   INTEGER NOT NULL,                    -- accumulated FOREGROUND listen time
  completed   INTEGER NOT NULL DEFAULT 0,          -- 1 = track finished naturally
  created_at  INTEGER NOT NULL
);
CREATE INDEX idx_plays_user_time     ON plays(user_id, played_at);     -- recently-played + range scans + time-series
CREATE INDEX idx_plays_user_catalog  ON plays(user_id, catalog_id);    -- play-counts + top-tracks grouping
```

- **`catalog_id` is the join key for everything.** Top-tracks = `GROUP BY catalog_id`; play-counts = `COUNT(*) GROUP BY catalog_id`; recently-played = `ORDER BY played_at DESC`; display metadata + top-artists/albums come from `JOIN catalog_entity`. (Top-artists v1 groups by `catalog_entity.artist`; a normalized `artist_key` is a later refinement — noted §8.)
- **`plays.catalog_id` is a "stored consumer reference"** in the catalog `merge` sense — when two catalog entities merge, `catalog.merge` repoints `plays.catalog_id` loser→winner, so a user's history auto-consolidates and never double-counts across a merge. (`plays` is the **first** table to store a durable `catalog_id`; today `merge` repoints only `catalog_alias` + `backend_binding`, so SP3 extends it to also repoint `plays` — the generalization the foundation design anticipated.)
- **Retention: keep-all.** History is the basis for all-time stats + a future Wrapped. No pruning in v1.
- **Qualified plays only** — one row per play that crosses the threshold (§3); no sub-threshold rows.

---

## 3. Ingest — detecting and recording a play

### 3.1 FE play-tracker
A new `web/src/lib/playTracker.ts` subscribes to the audio engine (`engine.subscribe`, which delivers `{current: Track, currentTimeMs, durationMs, playing, repeat}`). It owns a small state machine per current track:

- Accumulates **foreground listen time** (`ms_played`) from `currentTimeMs` deltas while `playing` — *not* wall-clock, so pauses don't count.
- Handles the traps the engine audit found: **seek backwards** (don't re-accrue replayed seconds toward the threshold twice — accrue by forward progress), **repeat-one** (a re-loop is a *new* play once it re-crosses the threshold), and **track change** (finalize the outgoing track before `current.id` flips — the engine emits no "outgoing track" event, so the tracker snapshots it).
- **Qualifies** a play by the de-facto standard (Last.fm's): `durationMs > 30_000` **and** (`ms_played ≥ durationMs/2` **or** `ms_played ≥ 240_000`). Waits for a settled `durationchange` before judging length (durationMs can be 0/stale at track start).
- On qualification (once per play), fires a single `POST /api/v1/plays`. `completed` = the track reached natural end.

### 3.2 The endpoint + mint
`POST /api/v1/plays` (behind `requireAuth`) body:
```jsonc
{ "libraryTrackId":"...", "title":"...", "artist":"...", "album":"...",
  "durationMs":218000, "isrc":"GB...", "msPlayed":140000, "completed":true, "playedAt":1719... }
```
Server (`internal/play/`):
1. `catalog_id := catalog.CanonicalFor(ctx, catalog.Identity{Kind:"track", Title, Artist, Album, DurationMs, ISRC})` — mint-on-play. The FE `Track` carries **no track-level external id**, so `Source`/`ExternalID` are empty → the entity is anchored by `isrc` (when present) and the norm fingerprint. Mint is **backend-independent** (pure DB), so a play is recorded even when the library is mid-swap — *a play is never dropped*.
2. Direct `INSERT` into `plays` (`user_id` from `currentUser(r)`, never the body). **Not** routed through the EventBus (that bus is lossy by design; a play write must not be dropped).
3. `playedAt` defaults to server-now if absent; `msPlayed`/`completed` stored as sent (the FE is the only place that can score them).

`libraryTrackId` is **not stored** — it's volatile; the durable identity is the minted `catalog_id`, and the *current* backend id for playback/cover is re-derived on read via `resolver.Resolve(catalog_id)`.

> **Threshold/identity edge:** a played track with no ISRC and no external id mints a **pure-library catalog entity** (`source=""`, `external_id=""`). That is the case §4 fixes.

---

## 4. Prerequisite fix — `match_cache` empty-key collision

When the stats UI resolves a pure-library `catalog_id`'s cover (`resolver.Resolve` → re-match via the matcher), the matcher consults `match_cache` keyed on `(source, external_id)`. For pure-library entities that key is `("","")` — **so all of them collide on one cache row**, cross-contaminating their resolved backend ids/covers.

**Fix (small, contained):** the resolver must **not** use the external `match_cache` for empty-external entities. Either (a) skip the `match_cache` read/write when `source==""` && `external_id==""` and resolve fresh each time (correct, slightly more matcher calls — acceptable, the `backend_binding` still caches the result per the foundation), or (b) key the binding/cache by `catalog_id` for these. Implement (a): in `resolver.rematchAndStore`, when the catalog entity has no external key, bypass `match_cache` and rely solely on the per-`catalog_id` `backend_binding`. (Confirmed unreachable until now because the catalog was dormant; SP3-3a is what makes pure-library entities real.)

---

## 5. Stats — model & API

**Compute-on-read.** Plays are human-paced and rows are tiny; windowed `GROUP BY`/`COUNT` over the two indexes is cheap and always fresh. No materialized aggregates in v1 (trivial to add later if a window ever gets slow).

### 5.1 The range model (the flexibility requirement)
**Every stats endpoint is parameterized by an absolute window** `from` (unix s, inclusive) and `to` (unix s, exclusive), plus a `tzOffsetMinutes` for local-time bucketing. **The FE owns all timeframe convenience** and resolves every selection to a `from`/`to` in the browser's local timezone:
- **Rolling presets:** last 7d / 30d / 90d / 365d / all-time.
- **Calendar-aligned:** this week / this month / this year (boundaries computed in local TZ).
- **Custom range:** a calendar date-range picker (any start → any end).

The backend stays range-agnostic — it never needs to know "a week"; it just aggregates `played_at ∈ [from, to)`. Time-series endpoints also take `bucket` (`day`|`week`|`month`); the FE defaults it by span (≤ ~45d → day, ≤ ~18mo → week, else month) and the user can override.

> **Timezone:** `played_at` is stored UTC. Local-time aggregations (the hour×weekday heatmap, day boundaries) apply `tzOffsetMinutes` sent by the FE (the browser's current offset). A single offset is a documented approximation across a DST boundary (±1h for part of a long range) — acceptable for these stats; a per-user IANA-tz refinement is a later option.

### 5.2 Endpoints (all `requireAuth`, all `user_id`-scoped in SQL)
| Endpoint | Returns |
|---|---|
| `GET /api/v1/stats/summary?from&to` | counts: plays, distinct tracks/artists/albums, total `ms_played`. |
| `GET /api/v1/stats/top/tracks?from&to&limit` | ranked `[{catalogId, title, artist, album, plays, msPlayed}]` (+ `resolver` cover/id at render). |
| `GET /api/v1/stats/top/artists?from&to&limit` | ranked `[{artist, plays, msPlayed, distinctTracks}]`. |
| `GET /api/v1/stats/top/albums?from&to&limit` | ranked `[{album, artist, plays, msPlayed}]`. |
| `GET /api/v1/stats/timeline?from&to&bucket&metric` | `[{bucketStart, plays, msPlayed}]` for the over-time graph. |
| `GET /api/v1/stats/clock?from&to&tzOffsetMinutes` | a 7×24 grid `[{weekday, hour, plays, msPlayed}]` for the heatmap. |
| `GET /api/v1/stats/recent?before&limit` | chronological recently-played `[{catalogId, title, artist, album, playedAt}]` (cursor by `played_at`). |
| `GET /api/v1/stats/entity?kind&id&from&to` | per-entity stats (an artist name or a `catalog_id`): plays, msPlayed, first/last played, your top tracks for it. Backs the detail-page sections. |

All "top"/recent rows carry the `catalog_id` so the FE renders covers + play affordance via the existing `coverUrl`/`streamUrl` against the resolver-served boundary.

---

## 6. The stats surface (FE)

### 6.1 `/stats` dashboard (the centerpiece — your_spotify-class)
A new route `/stats` with a sticky **range selector** (presets · calendar-aligned · custom range picker; default "last 30 days"), and:
- **Summary cards:** songs played · time listened · distinct tracks / artists / albums.
- **Listening-over-time** area/line chart: X = time buckets, Y = plays (toggle to minutes).
- **"When you listen"** heatmap: 7 weekday rows × 24 hour columns, cell intensity = plays (the signature your_spotify view).
- **Top tracks / top artists / top albums:** ranked cards with covers (resolver), play count + listening time, linking to detail pages.
- **Recently played:** chronological feed (also powers Home's carousel).

**Charts:** add one lightweight, tree-shakeable primitive — prefer **hand-rolled SVG** for the bar/area/heatmap (they're simple and let us hit the craft bar exactly), or a minimal lib (e.g. a small `recharts`/`visx` subset) if it stays clean. **Strictly design-token styled** — accent surfaces use `text-on-accent`, no raw hex, no generic-chart defaults. Must read as a polished Spotify-class panel.

### 6.2 Woven into existing surfaces
- **Home:** the existing "Jump back in" / recently-played carousel is fed by real `GET /stats/recent` data (replacing today's approximation).
- **Album / Artist detail pages:** a per-entity stats strip via `GET /stats/entity` — "you · N plays · X hrs · first listened …", and per-track **"played N×"** on the track rows.
- **Nav:** a `/stats` entry (sidebar or the avatar menu).

---

## 7. Identity & churn edges
- **Merge:** extend `catalog.merge(loser, winner)` to also repoint `plays.catalog_id` (it today repoints `catalog_alias` + `backend_binding` only) — history consolidates, never double-counts.
- **Covers/playback for stats rows:** always via `resolver.Resolve(catalog_id)` so they stay correct across a backend swap; a not-currently-resolvable track still shows in history (title/artist from `catalog_entity`) with a placeholder cover and a disabled/"unavailable" play affordance — never a dead id.
- **A play is never dropped** for lack of a live backend (mint is backend-independent).

---

## 8. Edge cases & decisions (summary)
| Case | Decision |
|---|---|
| Pauses / wall-clock | `ms_played` = accumulated foreground play time, not wall-clock. |
| Seek backward | accrue by forward progress; replays don't double-count toward the threshold. |
| Repeat-one | a re-loop is a new play once it re-crosses the threshold. |
| Stale `durationMs` at track start | wait for a settled `durationchange` before judging length. |
| No ISRC / no external id | mint a pure-library catalog entity (norm-anchored); §4 makes its cover resolve correctly. |
| Top-artists grouping | v1 groups by `catalog_entity.artist` (exact string); normalized `artist_key` is a later refinement. |
| Timezone / DST | FE sends `tzOffsetMinutes`; single-offset bucketing is a documented ±1h approximation over a long DST-crossing range. |
| Privacy | every stats/plays query SQL-scoped to `currentUser`; user identity never from the request body. |
| Retention | keep-all. |

---

## 9. Testing
- **Unit (FE):** `playTracker` threshold scoring incl. the seek-backward / repeat-one / track-change / stale-duration traps (drive the injectable `AudioElement`); the range selector resolving presets / calendar-aligned / custom to correct `from`/`to`.
- **Backend:** the `POST /plays` mint-and-insert path (real sqlite; verifies `CanonicalFor` mint + user-scoped insert; a play records with the library down); each stats query over a seeded play set incl. the empty-window and single-play edge cases; the `clock` tz-offset bucketing; per-user isolation (one user's plays never appear in another's stats).
- **§4 fix:** two pure-library entities with different metadata resolve to *different* covers (no `match_cache` empty-key collision).
- **Merge:** plays repoint loser→winner on a catalog merge (no double-count).
- **e2e:** a hermetic spec — play a track past threshold → it appears in recently-played + increments the count; the `/stats` dashboard renders cards/graph/heatmap/top-lists; switching the range refetches. Keep the suite green.

---

## 10. Phasing (within this one spec/plan)
1. **Data + ingest + the §4 fix:** migration `0020`, `internal/play` service + `POST /plays`, the FE `playTracker`, the resolver empty-key fix. *(Testable headless; history starts accumulating.)*
2. **Stats API:** the §5 endpoints (compute-on-read, range-parameterized).
3. **The dashboard FE:** `/stats` (cards · timeline · heatmap · top-lists · recently-played · range selector), then the Home carousel + detail-page stat strips.

Each phase ends green and is independently reviewable.

---

## 11. File-touch map
| Area | Path |
|---|---|
| Migration + queries | `internal/store/migrations/0020_plays.sql`, `internal/store/queries/plays.sql` (+ `make gen`) |
| Play service + ingest API | `internal/play/` (service, `CanonicalFor` mint), `internal/api/plays.go` |
| Stats service + API | `internal/play/stats.go` (compute-on-read queries), `internal/api/stats.go` |
| Resolver §4 fix | `internal/resolver/resolver.go` (bypass `match_cache` for empty-external entities) |
| Catalog merge hook | `internal/catalog/merge.go` (extend `merge` to repoint `plays.catalog_id`) + a `RepointPlays` query |
| Wiring | `internal/wiring/wiring.go` + `cmd/reverb/main.go` (construct play/stats services; the play service needs `catalog.Service`) |
| FE play tracker | `web/src/lib/playTracker.ts` (+ `playApi.ts`) |
| FE stats | `web/src/routes/Stats.tsx`, `web/src/lib/statsApi.ts`, chart components, a `RangeSelector` |
| FE woven surfaces | `web/src/routes/Home.tsx` (carousel), `Album.tsx`/`Artist.tsx` (stat strip + per-track counts), nav |

*Reverb — own your music, again.*
