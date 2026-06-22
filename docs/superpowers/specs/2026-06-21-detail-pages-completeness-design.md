# Music Detail Pages + Library Completeness — Design Spec

> Phase 2 / sub-project A. Artist, Album, and Playlist detail pages unified by a
> streaming **library-completeness engine**: fetch an artist's full discography
> from an external source (Spotify), exact-match it against the local library,
> and surface per-album / per-track "what you have vs. what exists" with
> one-click download affordances — woven into every existing surface that links
> to artists, albums, or playlists.

- **Status:** Approved design (brainstormed 2026-06-21), ready for implementation planning.
- **Author:** Reverb maintainer + Claude.
- **Supersedes:** the library-only `Artist.tsx` / `Album.tsx` detail pages.

---

## 1. Goals & Non-Goals

### Goals
1. **Artist page = the killer feature.** Show an artist's full deduped discography
   (Albums + Singles/EPs) from Spotify, each album marked **Full** (✓, in library),
   **Partial** (X of Y), or **Missing** (available to download), with one-click
   download for missing/partial albums and a "Download all missing" roll-up.
2. **Album page** handles owned, partial, and fully-external albums — track-level
   owned-vs-missing with per-track download, "Download missing · N", and live
   download progress that flips tracks to owned in place.
3. **Playlist page** — a dedicated detail page for library playlists (today's gap),
   built on the same shell so **Playlist Sync** (a later sub-project) drops in with
   no new components.
4. **Smart library indicators** — "you have N of M albums" on the artist page; the
   same coverage data can later feed artist/album cards elsewhere.
5. **End-to-end integration** — every existing surface that references an artist,
   album, or playlist routes to and behaves consistently with these pages. No
   island pages, no dead links.

### Non-Goals (this sub-project)
- Spotify **playlist import / sync** (URL paste, "download missing playlist",
  scheduled refresh) — later sub-project C. We only ensure the seam exists.
- **Lidarr** or any second downloader — later sub-project D.
- A manual "wrong artist? fix the match" override UI — the mapping table makes it a
  future drop-in; not built now.
- Compilations and "appears on" sections — hidden by default (possible later toggle).
- Artist-level "download the entire discography" — see §10 (guarded/deferred);
  album-level and "download all *missing*" are in scope.

---

## 2. Locked Product Decisions

These were settled during brainstorming and are not open in implementation:

| Decision | Choice | Rationale |
|---|---|---|
| Discography scope | **Albums + Singles/EPs, deduped** | Spotify-faithful without compilation/appears-on clutter. |
| Duplicate releases | **Collapse** remaster/deluxe/explicit/region into one **canonical = standard edition** | Avoids phantom bonus tracks under the Exact rule. |
| Completeness rule | **Exact** — Full only if *every* canonical track is matched; else Partial X/Y; zero = Missing | Honest; a Partial album's missing tracks are exactly its ↓ targets. |
| Compute / delivery | **Streaming enrichment** over the existing SSE/EventBus | Fast page + accurate Exact badges; reuses proven Phase-1 infra. |
| "Full" color | **Accent** (matches existing `Badge kind="in-library"`) | Consistency; green stays a status-only color. |
| Missing "at rest" | **Clean** (no cover marker; subtitle says "Available"; ↓ on hover) | Less noise on artists you own little of. |
| Playlist scope | **Library playlist detail page only**, sync-ready | Clean boundary with sub-project C. |

---

## 3. Architecture — the completeness engine

### 3.1 Reconciliation (library artist ⇄ Spotify artist)
Primary entry is a **library** artist (browse collection → click artist). To fetch
their discography we resolve the library artist to a Spotify artist:

- Spotify artist search by name → best candidate by name similarity + popularity,
  gated by a **confidence threshold**.
- Persist the mapping in **`artist_external_map`** so resolution happens once.
- **No confident match → graceful degrade**: render the library-only view (albums
  you own), no discography rail, **no error state**.
- Reverse entry (artist clicked from *Everywhere* search → already a Spotify id):
  resolution is skipped.

### 3.2 Enrichment pipeline (per album)
1. Fetch the artist's release list — implement Spotify's declared-but-unbuilt
   `DiscographyProvider.GetArtistDiscography`.
2. **Dedup → canonical releases**: group by normalized title; choose the standard
   edition (fewest deluxe/remaster/explicit markers; earliest base release). Keep
   Albums + Singles/EPs; drop compilations & appears-on.
3. For each canonical album, fetch its tracklist (Spotify `GetAlbum`, cached) and
   run each track through the **existing matching service** (ISRC/MBID/fuzzy +
   `match_cache`).
4. Roll up to an **`AlbumCoverage`** record: `full | partial | none`, owned/total
   counts, the matched **library album id** (so a click plays from the library),
   and the **missing external track refs** (the ↓ download targets).

### 3.3 Delivery — streaming
- The page first renders a fast **skeleton**: the deduped album list (cover, title,
  year, `state: pending`), warm from cache where available.
- Coverage then **streams in per album** over a request-scoped **SSE** endpoint
  (mirroring `search/everywhere`); the UI fills badges and the "N of M albums"
  header progressively.

### 3.4 Caching & invalidation
Three caches, deliberately split by dependency:

| Cache | Key → value | Lifetime / invalidation |
|---|---|---|
| `artist_external_map` | library artist id ⇄ (source, external artist id) + confidence | Stable; manual re-resolve is future work. |
| `discography_cache` | (source, external artist id) → canonical album list | Long TTL (discographies change rarely). **Library-independent.** |
| `album_coverage` | (source, external album id) → coverage + matched library album id + `library_version` | **Version-stamped staleness** (mirrors `match_cache`): each row records the `library_version` it was computed against; a cached row is served only while its stamp is `>=` the current version, otherwise it is recomputed lazily on next view. |

Splitting discography (library-independent) from coverage (library-dependent) means
a freshly-downloaded track flips an album partial→full automatically: a scan /
download-complete bumps `library_version`, which leaves every `album_coverage` row
stale so the next view recomputes it — while the discography fetch (stamped by its
own long TTL, not `library_version`) is untouched. This staleness check replaces an
explicit delete-on-`library.updated` invalidation: nothing is purged, rows simply
expire against the version counter the way `match_cache` does.

### 3.5 Reuse ledger
- **Reused as-is:** matching service, `match_cache`, SSE streaming pattern,
  EventBus `library.updated` signal, download manager (queue/dedup/fallback).
- **New backend:** Spotify `GetArtistDiscography` + artist-search resolution; the
  `coverage` orchestration service (resolve → discography → dedup → rollup → cache →
  stream); library-adapter `GetPlaylist(id)`; the batch-download handler.

---

## 4. Data Model

### 4.1 New tables (goose migrations, matching existing `match_cache` / `download_jobs` style)
```
artist_external_map(
  library_artist_id TEXT, source TEXT, external_artist_id TEXT,
  confidence REAL, created_at,
  PRIMARY KEY (library_artist_id, source)
)

discography_cache(
  source TEXT, external_artist_id TEXT,
  albums_json TEXT, fetched_at,
  PRIMARY KEY (source, external_artist_id)
)

album_coverage(
  source TEXT, external_album_id TEXT,
  coverage_json TEXT, library_album_id TEXT, fetched_at,
  PRIMARY KEY (source, external_album_id)
)
```

### 4.2 Core type — `AlbumCoverage` (the unit that streams to the client)
```
AlbumCoverage {
  source, externalAlbumId
  state: "pending" | "none" | "partial" | "full"
  ownedCount, totalCount               // → "7 of 10"
  libraryAlbumId?                      // set if ≥1 track matched → click plays from library
  missingTrackRefs: ExternalTrackRef[] // the ↓ download targets on partial/none
}
ExternalTrackRef { source, externalId, title, isrc?, durationMs }  // enough to enqueue + render
```

---

## 5. API Surface

Source-qualified so one code path serves both library- and search-origin entities.
`{source}` ∈ `library | spotify`.

- `GET /api/v1/artist/{source}/{id}` → `ArtistDetail`: header, resolved cross-source
  ref, deduped **discography skeleton** (each album `pending` or warm-from-cache). Fast.
- `GET /api/v1/artist/{source}/{id}/coverage` → **SSE**; streams `AlbumCoverage` per
  album as it resolves, closes when done.
- `GET /api/v1/album/{source}/{id}` → `AlbumDetail`: tracks with per-track state
  (library track id if owned, `ExternalTrackRef` if missing).
- `GET /api/v1/library/playlist/{id}` → `PlaylistDetail` (header + tracks). **New** —
  needs `GetPlaylist(id)` on the library adapter (Subsonic `getPlaylist`).
- `POST /api/v1/downloads/batch` → enqueue a set of `ExternalTrackRef`s
  ("download missing" / "download album" / "download all missing"); dedup-joins
  through the existing manager; returns job ids.

The library-only `/api/v1/library/artist/{id}` and `/library/album/{id}` detail
endpoints are **retired** for the pages once they migrate (avoid two code paths);
the Library *grid* keeps using `/library/artists`, `/library/albums`, `/library/playlists`.

---

## 6. Frontend

### 6.1 Routing
Canonical routes are **source-qualified**; a redirect shim keeps old links working.
```
/artist/:source/:id     (source = library | spotify)
/album/:source/:id
/playlist/:id           (library playlists; sync later adds /playlist/:source/:id)

# back-compat redirects:
/artist/:id  → /artist/library/:id
/album/:id   → /album/library/:id
```

### 6.2 Components
- **New `CoverageChip`** — accent ✓ (full) / accent ring + `X/Y` (partial) /
  indeterminate (pending); wraps `ProgressRing`. Dark-translucent backing for
  legibility on cover art.
- **Extend `MediaCard`** — optional `coverage` prop (renders `CoverageChip` in the
  existing top-left slot) and a **download hover-action** variant alongside the
  current play reveal (accent circle, bottom-right; ▶ for full, ↓ for partial/missing).
- **New `useCoverageStream` hook + `coverageStore`** (Zustand, keyed by artist ref) —
  mirrors `everywhereStore`; merges streamed `AlbumCoverage` into a map.
- **New route `Playlist.tsx`**; **rework `Artist.tsx` / `Album.tsx`** to consume the
  source-qualified endpoints + coverage stream.
- **Reused as-is:** `TrackRow`, `DownloadAction` (its "Available" → "Downloading" →
  owned lifecycle is exactly the album/playlist track treatment), `Chip` (discography
  filters), `Button`, `IconButton`, `ProgressRing`, `Cover`.

### 6.3 Page UX (validated via visual companion)
- **Artist:** circular portrait + name + "N of M albums · K partial · J missing"
  stat; actions Play / Shuffle / **Download all missing · N**; `Chip` filters
  (All / Albums / Singles & EPs); 5-col `MediaCard` grid with `CoverageChip`;
  pending albums shimmer and resolve live.
- **Album:** album-wash header + "X of Y in library" + "Download missing · N";
  `TrackRow` list — owned rows play (faint accent ✓ on hover), missing rows are
  muted with a `Download` button, downloading rows show the live accent ring.
- **Playlist:** same shell as Album; cover (or 4-up mosaic), "Playlist" eyebrow,
  `owner · N songs · duration`, Play / Shuffle; library playlists render all-owned
  (no missing rows, no "download missing"); remove-from-playlist as a row action.

---

## 7. End-to-End Integration (first-class requirement)

Every surface that references an artist/album/playlist must route to and behave
consistently with the new pages. Audited touchpoints and required behavior:

| Surface | File | Required change |
|---|---|---|
| Left rail — playlists list | `components/shell/LibraryRail.tsx` | Playlist rows navigate to **`/playlist/:id`** (today: no route → dead). Album/artist rows updated to source-qualified library paths. |
| Library grid | `routes/Library.tsx` | Playlist cards link to `/playlist/:id`; artist/album cards source-qualified. |
| Search results | `routes/Search.tsx` | Artist/album results route to source-qualified pages; **external** (Everywhere) artist/album results route to `…/spotify/:id` so a not-yet-owned artist/album opens its enriched page. |
| Search suggest dropdown | `components/search/SearchSuggest.tsx` | Same source-qualified routing for album/artist suggestions. |
| Home | `routes/Home.tsx` | Album/artist tiles source-qualified; any playlist tiles → `/playlist/:id`. |
| Now Playing panel | `components/shell/NowPlayingPanel.tsx` | Current track's artist/album link to the new pages. |
| Player bar | `components/shell/PlayerBar.tsx` | Track artist/album links (if any) source-qualified; add-to-playlist stays consistent. |
| Add-to-playlist | `components/AddToPlaylistMenu.tsx` | After adding, the target playlist's detail page reflects it; create-playlist flows land on the new `/playlist/:id`. |
| Artist → album, Album → artist | `routes/Artist.tsx`, `routes/Album.tsx` | Internal links source-qualified. |

**Acceptance:** from any of the above, clicking an artist/album/playlist lands on the
correct new page with correct data and no console/route errors; a playlist is
reachable and playable from the rail and the grid; an external search result opens an
enriched (downloadable) detail page.

---

## 8. Edge Cases
- **No confident Spotify artist match** → library-only view, no error.
- **Dedup ambiguity** → deterministic canonical pick (normalized-title group →
  standard edition heuristic); ties broken by earliest release date then track count.
- **Spotify rate-limit / per-album fetch failure** → that album streams a `none` +
  retry affordance; the page and other albums are unaffected (no all-or-nothing).
- **Cache staleness** → `library.updated` invalidates `album_coverage`; next view
  recomputes. A download completing mid-view flips the album/track via the existing
  WS event without a manual refresh.
- **Empty discography / unknown artist** → friendly empty state, library albums still shown.
- **Large discographies** → streaming + concurrency cap on per-album fetch; skeleton
  stays responsive.
- **Playlist with no cover** → 4-up mosaic from track covers; fallback to music-note placeholder.

---

## 9. Testing
Matches the existing bar (Go unit + FE component + hermetic e2e):
- **Go unit:** dedup/canonicalization; coverage rollup (full/partial/none under Exact);
  artist resolution + confidence threshold; cache invalidation on `library.updated`;
  batch-download dedup-join.
- **FE component:** `CoverageChip` states; `MediaCard` coverage + download-hover variant;
  `useCoverageStream` merge; Artist/Album/Playlist render + states; source-qualified
  routing + back-compat redirects.
- **e2e (hermetic):** artist → partial album → "Download missing" → track flips to
  owned and album badge flips partial→full; library playlist reachable from rail →
  plays; external search artist → enriched page.

---

## 10. Deferred / Guarded
- **Artist-level "Download entire discography"** (every track of every album) — a
  footgun that could enqueue hundreds of jobs. Out of scope; "Download all *missing*"
  (already-surfaced albums' missing tracks) is the in-scope roll-up, and even that
  should confirm the count before enqueuing.
- **Manual artist re-match** override — table supports it; UI later.
- **Coverage on cards outside the artist page** (library/search grids) — data is
  available; surfacing it broadly is a later polish.

---

## 11. Sequencing (for the plan)
1. **Backend completeness engine** — Spotify discography + resolution; dedup; coverage
   service + caches + invalidation; SSE endpoint; `GetPlaylist`; batch download. (TDD)
2. **Frontend core** — types/hooks/store; `CoverageChip`; `MediaCard` extension.
3. **Pages** — Artist (rework), Album (rework), Playlist (new).
4. **End-to-end integration** — §7 touchpoints + source-qualified routing + redirects.
5. **e2e + polish** — the hermetic flow; empty/degraded states.

Each step verified before the next; whole-branch review before merge.
