# Library Identity Foundation — Design Spec

> Reverb addresses every library entity by the **backend's** opaque id (Navidrome/Subsonic track/album/artist/cover ids), minted at the one adapter boundary and passed through verbatim to the browser. **The backend's id space is the app's id space.** Built-in (bundled Navidrome) and external backends assign **disjoint** id namespaces, and swappability is shipped — so a backend swap makes *every* stored id dead, and the "cover bug" we keep re-patching is just the visible symptom of that. This spec introduces a **canonical, backend-independent identity layer** — a stable internal id per library entity, with backend addressing resolved on demand — so listening history, playlists, covers, and playback survive a backend swap or re-scan. It is the bedrock the listening-history feature (SP3) lands on.

- **Status:** Approved direction (brainstormed 2026-06-29), hardened by a 5-pass adversarial red-team, ready for implementation planning. Target end-state is the full canonical catalog (option ②); implemented **foundation-first** in phases so each ships and verifies independently on the live instance.
- **Author:** Reverb maintainer + Claude.
- **Why now:** Backend swappability (built-in bundled Navidrome ↔ external) is **shipped** (`library_backend_mode`, `internal/library/embedded/`), so id churn is a live, present event — not a future risk. SP3 (listening history) must not inherit the rot, so this lands first.
- **Builds on / reuses:** the existing matcher (`internal/matching/matching.Service`) — already a clean, cache-first, `library_version`-gated external→backend resolver, and `match_cache` is already a proto-catalog (durable `(source, external_id)` key + a re-derivable volatile-id snapshot); the swap-detection machinery (`libraryIdentity()`, `reconcileLibraryIdentity`, `reconcileDownloadJobIdentity` in `internal/wiring/wiring.go`); the single `subsonic` adapter + the two addressing handlers (`internal/api/stream.go`); `Normalize`/dedup (`internal/matching/normalize.go`); goose + sqlc.

---

## 1. Goals & Non-Goals

### Goals
1. **Decouple internal identity from backend addressing.** Introduce one stable, backend-independent id per library entity (track/album/artist). Backend ids (`library_track_id`, `cover_art_id`, …) become a *re-derivable resolution detail*, never a stored identity.
2. **Survive churn.** A backend swap, a Reverb-driven re-scan, *or* an out-of-band external Navidrome re-scan must not orphan history, playlists, covers, or playback. Where re-resolution is impossible, degrade *visibly and safely* (placeholder + "unavailable in current library"), never silently wrong.
3. **Fix the cover/stream boundary at its root.** Centralize resolve→validate→fallback server-side; stop the FE from holding churnable ids for these endpoints; give playback the error recovery it currently lacks.
4. **Unblock SP3.** A play/scrobble row references a canonical id and is recordable even when the library is mid-swap — so listening history is churn-safe by construction.
5. **Be safe on live data.** Every phase is non-destructive, idempotent, rollback-safe, and verifiable on the running `soulkiller` instance.

### Non-Goals (this spec)
- **Not** indexing/persisting the whole library. Canonical ids are minted **lazily**, only for entities Reverb durably references (played / downloaded / playlisted / matched). Live browse/search stay adapter-fresh and unpersisted — the existing "library data is never persisted" invariant holds.
- **Not** canonical-ids-on-the-wire for *live* views (that is P3, optional). During P1/P2 live views keep emitting backend ids; the boundary accepts both.
- **Not** building SP3 itself. This is the foundation; SP3 is the next spec.
- **Not** live built-in↔external mode switching — that stays restart-only (the bundled-Navidrome `:4533` double-exec hazard is out of scope).

---

## 2. The core model

Three new tables (migration `0019_catalog.sql`). Conceptually this **promotes and splits** `match_cache`: durable identity in a catalog, volatile addressing in a binding, joined by a minted internal id.

```sql
-- "What it is" — durable, never churns. Snapshot captured at mint time.
CREATE TABLE catalog_entity (
  id           TEXT PRIMARY KEY,    -- minted surrogate: 'trk_<uuid>' | 'alb_<uuid>' | 'art_<uuid>'
  kind         TEXT NOT NULL,       -- 'track' | 'album' | 'artist'
  title        TEXT NOT NULL DEFAULT '',
  artist       TEXT NOT NULL DEFAULT '',
  album        TEXT NOT NULL DEFAULT '',
  duration_ms  INTEGER NOT NULL DEFAULT 0,
  isrc         TEXT NOT NULL DEFAULT '',
  mbid         TEXT NOT NULL DEFAULT '',
  source       TEXT NOT NULL DEFAULT '',   -- a primary external alias, e.g. 'spotify'
  external_id  TEXT NOT NULL DEFAULT '',
  created_at   INTEGER NOT NULL
);

-- Every durable key that resolves to an entity. Lookup priority: isrc -> external -> norm.
CREATE TABLE catalog_alias (
  alias_kind   TEXT NOT NULL,       -- 'isrc' | 'external' | 'norm' | 'mbid'
  alias_value  TEXT NOT NULL,
  catalog_id   TEXT NOT NULL REFERENCES catalog_entity(id),
  created_at   INTEGER NOT NULL,
  PRIMARY KEY (alias_kind, alias_value)   -- a durable key resolves to exactly one entity
);
CREATE INDEX idx_catalog_alias_catalog ON catalog_alias(catalog_id);

-- "Where to fetch it now" — volatile addressing. GENERALIZES match_cache.
CREATE TABLE backend_binding (
  catalog_id        TEXT NOT NULL REFERENCES catalog_entity(id),
  library_identity  TEXT NOT NULL,  -- 'builtin' | 'external:<url>'  (the backend fingerprint)
  backend_id        TEXT NOT NULL DEFAULT '',  -- '' = unresolved; resolved id otherwise
  cover_art_id      TEXT NOT NULL DEFAULT '',  -- the entity's cover (for a track: its album cover)
  known_absent      INTEGER NOT NULL DEFAULT 0, -- 1 = re-match ran and this entity is NOT in this backend
  binding_epoch     INTEGER NOT NULL,           -- per-identity epoch this binding was resolved at
  resolved_at       INTEGER NOT NULL,
  PRIMARY KEY (catalog_id, library_identity)
);
```

Notes baked in from the red-team:

- **`catalog_entity` columns are `NOT NULL DEFAULT ''`/`0`** so the table is creatable and writable without nullability gymnastics, and so partial identity (the common self-hosted case: title/artist/album only, no ISRC) is representable.
- **`backend_binding` carries an explicit `known_absent` flag** rather than overloading a NULL `backend_id`. `known_absent=1` (stamped at the current epoch) is the negative cache that makes resolve-on-404 loop-safe (§5.3).
- **`binding_epoch` is per-identity, not the global `library_version`** (§5.2) — this is the single most important reconcile fix.
- `catalog_alias` PK `(alias_kind, alias_value)` guarantees one entity per durable key. Re-used/duplicate ISRCs (they exist) are handled by the merge **corroboration gate** (§4.2), not by claiming a conflicting unique alias.

---

## 3. Canonical identity & the composite fingerprint

A canonical id is a **minted opaque surrogate** (`trk_/alb_/art_<uuid>`). Lookup/dedup happens through `catalog_alias`, **not** by making the id a content hash. Rationale: a track is reached by different durable keys on different paths (a Spotify match supplies the spotify id; a pure-library track supplies only metadata; an ISRC may surface later). The alias table lets all of those converge on one stable id — and lets us **merge** entities later discovered to be the same — without ever changing the id other tables reference. A content-hash id can't merge and would split the same song across paths.

### 3.1 The `norm` fingerprint (load-bearing)

Every entity gets a `norm` alias, and it **must be as discriminating as the matcher** or it will both falsely merge distinct recordings and falsely split the same one. It is **not** title-only. Definition:

```
norm_value = sha256(
    normArtistTokens(artist)  ␟  Normalize(title)  ␟  Normalize(album)  ␟  durationBucket  ␟  qualifierTokens
)
```

- Reuses `matching.Normalize` and the dedup recipe (`internal/matching/dedup.go`) so the fingerprint and the matcher **cannot disagree** about identity.
- `durationBucket = floor(duration_ms / 5000)` — reproduces the matcher's `DurationToleranceMs=3000` separation, so live-vs-studio of the same title don't collide and "Intro" by many artists stays distinct.
- `qualifierTokens` = version markers (`live`, `deluxe`, `remaster`, `acoustic`, `edit`, …) extracted **regardless of paren/dash/bracket form** and emitted as a sorted token set. This closes a verified punctuation bug: `Normalize` keeps `"Song (Live)"` but turns `"Song - Live"` into a bare `"song live"`, so without this step the *same* live recording from two sources produces two fingerprints (false split). A `normalize_test` case is added for the dash/bracket forms.
- A track with **no ISRC and no external id** (classic Subsonic, hand-added files — the common self-hosted case) therefore still gets a **durable, swap-stable** anchor. It is **never** keyed on a backend id. `backend_id` lives only in `backend_binding`.

### 3.2 Alias priority & minting

`canonicalFor(durable identity)`:
1. Try aliases in priority `isrc → external → norm`; the first hit returns its `catalog_id`.
2. On no hit, **mint**: create `catalog_entity` (snapshotting title/artist/album/duration/isrc/external_id) + insert all known aliases (always including `norm`).
3. **Minting is backend-independent** (pure DB/alias work — no live adapter needed). Invariant: *a play is never dropped for lack of a live backend.*

---

## 4. The alias scheme & merge policy

### 4.1 Merge is a concrete P1 operation
"Supports later merge" is not enough — without a defined merge, SP3 silently double-counts. Mechanism:

- Insert aliases with `ON CONFLICT(alias_kind, alias_value) DO NOTHING`, then **SELECT the existing alias's `catalog_id`**. If it differs from the entity we were attaching it to, that mismatch **is the merge signal** — observed, never swallowed by the PK.
- `merge(loser, winner)` where **winner = the entity anchored by the higher-priority alias** (`isrc > external > norm`), tie-broken by older `created_at` (more references accreted). One transaction:
  1. Repoint `catalog_alias.catalog_id` and `backend_binding.catalog_id` from loser → winner, plus any **stored** canonical-id reference (SP3 `plays.catalog_id` once SP3 lands; any P2 stored `canonical_id` columns) via the `catalogRef` helper. P1 consumers that *re-derive* their canonical id at reference time (`download_jobs`, synced tracks, requests) need **no** repoint — re-derivation now resolves to the winner through the merged alias.
  2. On `backend_binding` PK collision, keep the binding with a real `backend_id` (else the fresher epoch).
  3. Delete the loser entity.
- Every **stored** consumer reference goes through **one `catalogRef` helper**, so the repoint list stays centralized and auditable as P2/SP3 add stored `canonical_id` columns.

### 4.2 ISRC is strong-but-corroborated
ISRCs are not reliably unique per recording (re-issues mint new ISRCs; compilations duplicate them). So: auto-merge on ISRC collision **only when `norm` corroborates** (title+artist agree). If ISRC matches but title/artist strongly disagree, do **not** merge — the second entity keeps its `norm`/`external` identity, and the duplicate ISRC is simply **not** claimed as a unique alias for it (it would collide anyway). This stops one bad ISRC from irreversibly fusing two genuinely different entities. (An `isrc_observed` candidacy hint — a non-unique alias kind read only when gathering match candidates — is a possible later refinement, not needed for P1 correctness.)

---

## 5. The resolver service

A new `internal/resolver` service, mirroring `matching.Service`'s proven shape (pure, cache-first, lazily recomputed). It is a **long-lived singleton** owned by `cmd/reverb/main.go`, so it survives adapter reloads — but it **never holds an adapter reference** (that would reintroduce the dead-adapter bug the per-`Build` matcher rebuild avoids).

### 5.1 Reaching the live adapter
The resolver is constructed with an **adapter provider**: `currentLib func() library.LibraryAdapter` reading a shared `atomic.Pointer[library.LibraryAdapter]` that `server.reload()` updates (inverted ownership: the live adapter pointer is shared state; server and resolver both read it). At the addressing boundary, the handler instead passes the **request's already-RLock-snapshotted adapter** into `resolveWith(ctx, canonicalID, lib)`, so the re-resolution and the failed read use the *identical* adapter even across a concurrent reload.

### 5.2 Freshness key — per-identity `binding_epoch`, not global `library_version`
`backend_binding` freshness is keyed by `(catalog_id, library_identity)` and stamped with a **per-identity `binding_epoch` bumped ONLY on an identity change (swap)** — *not* on every scan. The global `library_version` is bumped on every download scan; reusing it here would force a thundering full re-resolve after every download. Scan-driven id movement is instead caught lazily by resolve-on-404 (below). `match_cache` keeps using `library_version` unchanged in P1.

### 5.3 `resolve()` — cache-first, loop-safe
```
resolve(catalog_id, lib) -> (backend_id, cover_art_id, found)
  binding := get(catalog_id, current library_identity)
  if binding fresh (binding_epoch == current) and binding.backend_id != '':  return hit
  if binding fresh and binding.known_absent:                                 return (_, _, found=false)  // negative cache — NO re-match
  // stale or missing: re-match the catalog_entity snapshot against `lib` (reuse matcher machinery), under singleflight keyed by catalog_id
  on success: upsert binding{backend_id, cover_art_id, epoch=current, known_absent=0}; return hit
  on miss:    upsert binding{backend_id='', known_absent=1, epoch=current};          return found=false
```
- **Negative caching** (`known_absent` stamped at the current epoch) bounds external calls to **at most one per (catalog_id, epoch)** — without it, a page of genuinely-dead ids becomes an unbounded live-re-match amplifier.
- **Singleflight** keyed by `catalog_id` collapses N concurrent dead-cover requests into one upstream re-match.
- **`backend_binding` is a CACHE of the exact same matcher output** (same machinery, same gates) — never an independent resolver. This is an explicit invariant so `match_cache` and `backend_binding` can only differ during a transient, converging recompute race, not structurally.

### 5.4 Reconcile hooks
- **Backend swap** (detected at `wiring.Builder.Build` via the existing `reconcileLibraryIdentity`): bump the per-identity `binding_epoch`. Idempotent via the existing stored-identity compare.
- **`runScan`** (post-download): keep bumping `library_version` for `match_cache`; additionally do a **targeted** binding refresh of only the canonical ids it just linked (not a global invalidation). Ordering contract: bump first, then write bindings stamped at the post-bump epoch (read epoch fresh per call, never cached across the bump).
- **Post-swap async sweep:** for canonical ids with no download job (SP3 plays, synced library-source tracks), the swap reconcile enqueues a **bounded, async** re-resolution over the durably-referenced rows, so the first post-swap `/stream` or `/cover` doesn't block on a synchronous Subsonic search. The synchronous resolve-on-404 path is bounded by a short timeout with a placeholder/404 fallback.

---

## 6. The addressing boundary (`/cover`, `/stream`)

### 6.1 Prefix-discriminate **before** the resolver
`handleStream`/`handleCover` branch on a cheap prefix test on `{id}`:
- `trk_`/`alb_`/`art_` → **resolver path**.
- otherwise → **direct adapter passthrough** (a live backend id from a browse/search view — today's behavior, P1/P2).

The canonical prefix namespace is provably disjoint from Navidrome ids (opaque hex / `mf-`/`al-`/`ar-`, never an underscore-suffixed `trk_`). This single check is what makes dual-accept safe: the resolver is **never** called with a raw backend id (which would otherwise *mint a spurious entity per live-browsed cover* — a catalog-pollution blocker), and known-absent canonicals are honored instead of round-tripping to the backend.

### 6.2 Tri-state the canonical path
`resolve()` returns `found`. `found=false` (no binding yet → resolve now; or `known_absent` → **404 immediately, never forward to the backend**). Only `found=true` with a non-empty `backend_id` forwards to the adapter. This gives `known_absent` real meaning.

### 6.3 Resolve → **validate** → fallback (not just catch-404)
The handler validates the resolved fetch (the adapter's existing non-image / non-audio reject is the validate step), and on a dead/stale result re-resolves. Crucially this also covers the **identity-unchanged-but-rescanned** case, where an external Navidrome re-scan silently *reassigns* an id that still returns HTTP 200 pointing at the *wrong* content — the original cover-bug class, which a pure resolve-on-404 hook would miss. A stale binding is treated as recomputable on a library-updated signal, not only on a hard 404.

### 6.4 Track covers, size/Range, caching
- **Track cover:** `backend_binding.cover_art_id` for a `trk_` entity stores the resolved **album** cover (the matcher already returns it per track). `/cover/trk_<uuid>` reads its own binding — no cross-entity `alb_` lookup, no dependency on an album entity being minted.
- **Thread `?size=` and `Range`** through every resolution/validation hop (no full-file probe before a ranged copy; no full-res cover before a sized one).
- **Cache-Control:** canonical URLs are stable across swaps, so the natural backend-id cache-bust is gone. `coverUrl()` appends `?v=<library_version>` (or the TTL drops to ~300s) so a post-swap cover actually re-fetches.

### 6.5 FE changes
- `coverUrl()/streamUrl()/trackCoverUrl()` accept canonical ids for persisted/SP3 surfaces; once a surface carries a canonical id, `trackCoverUrl` emits `coverUrl(canonicalId)` directly and drops the `albumId || coverArtId` guesswork (the server now owns track→album cover resolution). `Cover.tsx`'s `onError` fallback **stays** as cheap defense-in-depth (it also covers the external-Spotify-URL case the server never sees).
- **`audioEngine` stream-error recovery** (it has none today): bind an `error` listener to `this.active` **only** (a `preload` error nulls `preload.src` and must **not** advance). On an active error: if `repeat==='one'`, do **not** skip (reload once, then give up — no tight loop); else `advance(1, true)` guarded by a **consecutive-error counter** (stop after K=3 → `playing=false`), so a whole-backend-down doesn't storm the queue. This closes the dead-stream silent-failure symmetrically to the server-side cover recovery.

> **Honest scope on "collapse 3 patches into 1":** the server centralizes exactly one — the adapter non-image reject becomes the server validate step. The two FE patches (`trackCoverUrl` album-preference, the external-vs-proxy `coverSrc` choice) *decide the URL before any request*, so they stay — but they **simplify** once canonical ids carry the album-cover resolution server-side.

---

## 7. Migration & rollout safety

### 7.1 `0019_catalog.sql` is **purely additive**
Three `CREATE TABLE`s and nothing else — **no `ALTER` of any consumer table, no backfill, no `INSERT`** into the new tables. This is forced by hard constraints the red-team verified:
- goose runs `Migrate()` at process start, **before** `wiring.Builder.Build` constructs any adapter — so there is *no live backend* to re-match against during migration; any backfill would write garbage.
- Lazy minting means the catalog is **empty** at migration time, so FK-bearing alias/binding inserts would abort under `_pragma=foreign_keys(1)`.
- Adding a `NOT NULL` `canonical_id` column to an already-populated consumer table fails on SQLite without a constant default — and the **stale local dev DB (goose v1) would never surface it**; only the populated `soulkiller` DB would, at deploy.

`Down` = `DROP TABLE backend_binding; DROP TABLE catalog_alias; DROP TABLE catalog_entity;` (children first) — clean and fully reversible precisely because P1 added no consumer columns (honoring the established no-`DROP COLUMN` rollback contract).

### 7.2 No consumer schema change in P1 — resolve from existing durable columns
We do **not** add `canonical_id` columns to consumers in P1. `canonicalFor()` derives the catalog id at *reference time* from the durable columns these tables **already carry**: `requests(source, external_id, isrc)`; synced tracks' full external ref in `tracks_json`; `download_jobs(source, external_id, isrc, title, artist, album)`. (P2 may add stored `canonical_id` columns as a perf/clarity step — with a runtime backfill, never an in-migration one.)

### 7.3 Don't let archived rows rot
Lazy-mint-on-reference heals rows that get touched; an archived request or never-reopened playlist could keep a dead id. So the swap reconcile runs a **background pass over open/visible rows** (open requests, sync-enabled playlists, completed-but-unlinked download jobs) that pre-mints+binds them. Archived/decided rows are left to lazy mint on next reference — acceptable because they carry durable keys and self-heal the moment they're re-opened.

### 7.4 Migration test
A test applies `0019` against a **fixture DB pre-seeded with real-shaped rows** (mirroring `soulkiller`: `download_jobs`/`synced_playlists`/`requests`/`match_cache`), asserts the migration applies, asserts **all pre-existing rows are byte-identical afterward** (additive proof), and asserts `Down`→`Up` round-trips. The seeding is explicit because the local dev DB won't exercise the populated-table path.

---

## 8. Phasing

### P1 — Foundation (this build; unblocks SP3)
`catalog_entity` + `catalog_alias` (composite fingerprint) + `backend_binding` + the merge operation + the resolver (adapter-provider, backend-independent mint, cache-first resolve, per-identity epoch, loop-safe + singleflight resolve-on-404, validate-not-just-404) + the addressing boundary (prefix-discriminate, tri-state, track cover, size/Range, cache-bust, FE stream-error recovery) + the background reconcile sweep + the **two true wrong-answer-on-swap consumers**:
- **`download_jobs` rescan gap** — resolve link/cover via the resolver (removes the swap-only `ClearMatchedDownloadJobLibraryRefs` dance).
- **Synced-playlist library-source tracks** — migrate to a canonical id **and add read-time `resolve()`** mirroring the non-library branch (storage migration alone does **not** fix the no-rematch read — that asymmetry *is* the bug).

`match_cache` is left untouched (it self-heals). `requests.cover_art_id` is **deferred to P2** (it already has a non-rotting `cover_url` fallback — the weakest member; deferring keeps P1 minimal). `album_external_map`/`artist_external_map` are **deferred to P2** — the red-team showed they self-heal on swap exactly like `match_cache` (key-miss → recompute → upsert), so they are dead-weight-on-swap, **not** wrong-answer-on-swap, and don't belong in P1.

**P1 ships and verifies independently on `soulkiller`.** Acceptance: backend swap (or simulated id churn) leaves synced-playlist library tracks + download-job covers/playback correct; a dead cover/stream recovers; the catalog stays unpolluted by live-browse traffic; existing rows byte-identical post-migration.

### P2 — Fold the self-healing caches onto canonical
Re-key `match_cache`, `album_external_map`/`artist_external_map`, `album_coverage`, and `requests.cover_art_id` onto canonical ids (runtime backfill); retire `match_cache` as a separate cache. Reclaim orphaned `external_map` rows.

### P3 — Canonical-on-the-wire (optional)
Translate at the API DTO boundary so backend ids never reach the wire even for live views. Lowest value (live views re-fetch fresh; swaps are restart-only), widest blast radius — decided once P1/P2 are real.

---

## 9. SP3 plug-in contract

When SP3 records a play, the server calls `canonicalFor({libraryId, title, artist, album, isrc?, durationMs})` at record time and stores **`catalog_id`** on the play row.
- Alias priority for a **play** is `isrc → norm` — there is **no track-level external id** in the FE `Track` (`web/src/lib/types.ts`), so `external` is dropped from the play-mint path.
- Be explicit: for the common pure-library, no-ISRC track, `catalog_id` is **norm-stable, not collision-proof**. The frozen `catalog_entity` snapshot + a later `merge()` are what make this safe — a play points at a snapshotted entity, and a merge reconciles norm-collisions **without losing the play**.
- Minting is backend-independent → **a play is never dropped for lack of a live backend** (and you can't be streaming a track while the library is fully down — `/stream` 503s first). Top-tracks/counts in SP3 `GROUP BY` the canonical track id; top-artists resolves each play's artist to its own `art_` entity — all inheriting churn-safety.

---

## 10. Edge cases & invariants (summary)

| Case | Behavior |
|---|---|
| Pure-library track, no ISRC/external id | Anchored by the composite `norm` fingerprint; re-resolvable after swap; never keyed on a backend id. |
| Two recordings, same title | Separated by artist + album + duration bucket (matches the matcher's gate). |
| Same live recording, `"(Live)"` vs `"- Live"` | Same fingerprint via qualifier-token canonicalization (not punctuation-fragile). |
| ISRC learned after mint via spotify-id | Alias collision observed (not swallowed) → `merge()`. |
| Duplicated/re-used ISRC across two real tracks | **No** merge unless `norm` corroborates; the second entity keeps its own identity (the duplicate ISRC is not claimed as its unique alias). |
| Genuinely-removed track | Re-match fails → `known_absent=1` at current epoch (negative cache) → boundary 404s without re-matching until epoch advances. |
| Backend swap | `binding_epoch` bumped; async sweep refreshes visible rows; first read of a non-swept id resolves lazily under singleflight with a bounded timeout. |
| Out-of-band external re-scan (no version bump) | resolve→**validate** catches silently-reassigned (200-but-wrong) ids; stale binding recomputes. |
| Live reload (adapter swapped) | Resolver reads the live adapter via the shared atomic/provider; boundary uses the request's snapshotted adapter — no dead-adapter writes. |
| Live-browsed cover/stream | Prefix check → direct passthrough; resolver never invoked → no catalog pollution. |
| `repeat='one'` + transient stream error | No skip (reload once); whole-backend-down capped at K consecutive errors → stop, not storm. |

---

## 11. Testing strategy

- **Unit:** fingerprint determinism + the dash/bracket/paren qualifier cases (`normalize_test`); alias priority + mint; `merge()` repoint/cascade incl. binding PK collision; ISRC-corroboration gate.
- **Resolver:** cache hit/miss; negative caching loop-bound (one re-match per epoch); singleflight collapse; per-identity epoch invalidation vs global `library_version` non-invalidation on a plain scan; adapter-provider returns the post-reload adapter; runScan ordering (binding written during a scan is not stale on the next read).
- **Boundary:** prefix discrimination (canonical vs backend id); tri-state incl. `known_absent` → 404 without backend round-trip; validate catches a 200-but-wrong cover; size/Range threaded; `audioEngine` error recovery (active-only, repeat-one, K-cap) via the injected `AudioElement`.
- **Migration:** the seeded-fixture additivity + `Down`/`Up` round-trip test (§7.4).
- **Consumer (P1):** synced-playlist library-source track correct after simulated swap (read-time resolve); download-job cover/playback correct after swap without the clear-and-rematch dance.
- **e2e:** keep the suite green; add a hermetic spec asserting a persisted (canonical) cover renders and a dead one degrades to placeholder.

---

## 12. Risks & open items
- **Fingerprint tuning** (duration bucket size, qualifier token set) is the highest-judgment knob — start conservative (5s bucket; the matcher's own marker set) and adjust against real collisions, with the snapshot + `merge()` as the safety net.
- **Async sweep sizing** on a large library after a swap — bound concurrency; it's best-effort ahead of lazy resolve, not a correctness dependency.
- **P2 `external_map` re-key** changes PK/conflict semantics — its own runtime backfill + test, explicitly out of P1.

---

## 13. File-touch map (P1)
| Area | Path |
|---|---|
| Migration + queries | `internal/store/migrations/0019_catalog.sql`, `internal/store/queries/catalog.sql` (+ `make generate`) |
| Catalog + merge | `internal/catalog/` (entity/alias store, `canonicalFor`, `merge`, the `catalogRef` helper) |
| Resolver service | `internal/resolver/` (mirrors `internal/matching/`); adapter-provider wiring |
| Shared live-adapter pointer | `internal/api/server.go` (atomic pointer updated by `reload()`), `cmd/reverb/main.go` (singleton ownership, provider) |
| Reconcile hooks | `internal/wiring/wiring.go` (epoch bump on identity change; async sweep), `internal/download/manager.go` (targeted binding refresh in `runScan`) |
| Addressing boundary | `internal/api/stream.go` (prefix-discriminate, tri-state, validate), `internal/library/subsonic/adapter.go` (reuse existing validators) |
| Consumers (P1) | `internal/download/manager.go` + `internal/playlistsync/service.go` (read-time resolve) |
| Fingerprint | `internal/matching/normalize.go` / `dedup.go` (reused; add qualifier-token canonicalization + tests) |
| FE | `web/src/lib/libraryApi.ts` (canonical-aware `coverUrl`/`trackCoverUrl`, `?v=`), `web/src/lib/audioEngine.ts` (error recovery), `web/src/components/ui/Cover.tsx` (unchanged fallback) |

*Reverb — own your music, again.*
