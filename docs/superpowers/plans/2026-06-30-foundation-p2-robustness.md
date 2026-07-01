# Library-Identity Foundation P2 (Robustness) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use `- [ ]`. Each implementer follows superpowers:test-driven-development; the plan gives interfaces, migrations, mechanisms, ordering contracts, and test strategies (implementers TDD the bodies).

**Goal:** Wire the resolver into the Build-constructed live services (download `Manager`, `playlistsync`) and give download-jobs a stored canonical id, so backend swaps/scans refresh covers/streams/links through the canonical **binding cache at read time** — retiring the swap-only "clear every link and re-match" dance and killing download-job **cover-rot**. This is robustness on top of an already-correct P1 (P1 self-heals lazily; P2 removes the dance + first-request-after-swap blocking).

**Architecture:** Break the P1 construction cycle with a **resolver provider seam** on the Builder (mirror the existing matcher-provider), inject a nil-safe resolver dependency into `download.Manager` + `playlistsync.Service`, add an additive `download_jobs.canonical_id` (minted at link time via `catalog.CanonicalFor`, runtime-backfilled for open rows only), centralize the merge repoint list in one `catalogRef` helper, and add `runScan` targeted binding-refresh + a bounded post-swap pre-warm sweep.

**Tech Stack:** Go 1.23, modernc sqlite + goose + sqlc (v1.31.1); React 19/TS.

## Global Constraints

- **Scope (confirmed with user 2026-06-30): ROBUSTNESS ONLY.** IN: resolver-into-Manager wiring; `catalogRef` helper; download-jobs canonical id + resolve-at-read + `runScan` refresh + retire `ClearMatchedDownloadJobLibraryRefs`; playlist-sync resolve-at-read; post-swap async sweep. **EXPLICITLY OUT (deferred with P3, low-value dedup + regression risk):** folding `match_cache`, `album_external_map`/`artist_external_map`, `requests.cover_art_id` onto canonical. Do NOT touch those tables/paths.
- **No in-migration backfill** (spec §7.1 — goose runs before any adapter exists; a NOT NULL column on the populated soulkiller DB would fail). Migrations are additive only (`ADD COLUMN … NULL` / `CREATE TABLE`). Backfill is **runtime**: bounded, idempotent, **scoped to open/visible rows only, never archived rows, never force-mint**.
- **Reads use `catalog.Lookup` (no-mint); only durable-reference writes use `catalog.CanonicalFor` (mint).** Preserve the §6.1 no-live-browse-pollution invariant — a live search/coverage/browse view must NEVER mint a catalog entity.
- **Resolver reached via a PROVIDER (`func() *resolver.Service` or a narrow interface), never a captured instance** — the `Manager` is rebuilt on every hot-reload; the resolver is a singleton read lazily. `resolver.Resolve` keeps reading `s.matcher()` per-call and must tolerate a nil matcher (no panic when no library is configured). Every new resolver dep is nil-safe (the "non-nil interface wrapping nil pointer" guard, like `deps.Downloads`/`deps.Sync`).
- **`runScan` ordering (spec §5.5):** a plain scan bumps `library_version` but must NOT bump `binding_epoch`. `RefreshLinked` operates at the current (unchanged) epoch, marking-stale-then-resolving ONLY the just-linked ids; read the epoch fresh per call, never cache it across a bump.
- **The cover/stream boundary only resolves `trk_`/`alb_`/`art_` prefixes.** Never route a live-browse/raw backend id to the resolver. `Cover.tsx` `onError` fallback stays as defense-in-depth.
- Non-destructive, idempotent, rollback-safe, verifiable on soulkiller. Next migration `0022`. sqlc v1.31.1. FE tokens-only. Commit footer `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`. Branch `feat/foundation-p2` (created; base 6c28e82). Never touch main.

## Reference map
The full current-state map (per area: current behavior, mechanism, files, risks, tests) is the workflow output that produced this plan — cited inline per task. Key P1 facts: `resolver.Service.Resolve` (resolver.go:93) + `RefreshLinked` (resolver.go:200, epoch-1-stale-then-resolve) already exist; the matcher-provider seam is `serviceReloader.matcherProvider()` (cmd/reverb/reload.go); the swap reconcile is `wiring.reconcileLibraryIdentity` (wiring.go:301-342, bumps library_version + `resolver.BumpEpoch`); the deferral comment is wiring.go:316-321; the clear-and-rematch dance is `ClearMatchedDownloadJobLibraryRefs` (wiring.go:353).

---

## Task 1: Resolver provider seam — make the resolver reachable from live services

**Files:** Modify `cmd/reverb/main.go` (construction order), `cmd/reverb/reload.go` (provider), `internal/wiring/wiring.go` (Builder gains a resolver provider; pass to Manager/Sync), `internal/download/manager.go` (+ nil-safe resolver dep), `internal/playlistsync/service.go` (+ nil-safe resolver dep), `internal/resolver/resolver.go` (nil-matcher tolerance); Test: `cmd/reverb/resolver_wiring_test.go` (extend), `internal/download/manager_test.go`.

**Interfaces — Produces:**
```go
// A narrow read/refresh seam the live services depend on; *resolver.Service satisfies it.
type BindingResolver interface {
    Resolve(ctx context.Context, catalogID string) (resolver.Addressing, error)
    RefreshLinked(ctx context.Context, catalogIDs []string) error
}
// download.NewManager / playlistsync.NewService gain an optional `resolve func() BindingResolver`
// (a provider func, nil-safe: provider may be nil, or return nil when no resolver/matcher yet).
```
The construction cycle break: build the resolver against `st.Q()` + the matcher-provider (both available before `Build`), then give the Builder a `resolverProvider func() BindingResolver` set BEFORE the first `Build`, read lazily inside Build when constructing Manager/Sync. The resolver reaches the live adapter only through its own matcher-provider (unchanged).

- [ ] **Step 1 — nil-matcher tolerance (RED first):** test that `resolver.Resolve`/`RefreshLinked` return a benign result (not panic) when the matcher-provider yields nil (no library configured). Fix `resolver.go` to nil-check `s.matcher()` before use.
- [ ] **Step 2 — provider seam:** add the `resolverProvider` to the Builder + thread a nil-safe `resolve func() BindingResolver` into `download.NewManager` and `playlistsync.NewService` (do NOT call it yet — Tasks 3-5 add the calls). Reorder `main.go` so the resolver is constructed before `Build` and injected via the provider.
- [ ] **Step 3 — tests (extend `resolver_wiring_test.go`):** the injected provider returns the SAME singleton across successive `Build`/`Reload` (mirror `TestResolverProvider_FollowsReload`); stays non-nil-safe with no matcher published; the P2 construction order (Builder→resolver→Build) does not panic when a service calls the resolver before any matcher exists. **Gate:** `go test ./... && go build ./... && go vet ./...`. **Commit.**

---

## Task 2: `catalogRef` helper — centralize the merge repoint list

**Files:** Create `internal/catalog/catalog_ref.go` (or add to `merge.go`); Modify `internal/catalog/merge.go`; Test: `internal/catalog/merge_test.go` (extend).

**Consumes:** none. **Produces:** one helper enumerating every stored canonical-id reference so `merge(loser→winner)` repoints them all in one auditable place.
```go
// repointCanonicalRefs repoints every STORED canonical_id reference from loser→winner,
// in FK-safe order (plays before the loser delete). Extend this ONE function when a task
// adds a stored canonical_id column (Task 3 download_jobs). merge() calls it.
func (s *Service) repointCanonicalRefs(ctx context.Context, winner, loser string) error
```
Currently `merge.go:33-64` hardcodes: RepointAliases → repointBindingsPreferWinner → RepointPlays → DeleteCatalogEntity. Refactor so the repoint steps live in `repointCanonicalRefs`; `merge` calls it then deletes. Behavior-preserving.

- [ ] **Step 1 — RED:** a merge_test asserting `merge` still repoints aliases + bindings + plays to the winner (mirror `TestMerge_RepointsPlays`, merge_test.go:133) — now via the centralized helper; and that the binding PK-collision path keeps the binding with a real backend_id (not blindly deleting all loser bindings when the winner's is empty).
- [ ] **Step 2 — refactor** merge into `repointCanonicalRefs` (no behavior change; the plays-before-delete FK order preserved). **Gate:** `go test ./internal/catalog/... && go build ./...`. **Commit.**

---

## Task 3: download-jobs canonical id — mint, resolve-at-read, retire the dance (the cover-rot killer)

**Files:** Create `internal/store/migrations/0022_download_job_canonical.sql`; Modify `internal/store/queries/download_jobs.sql`, `internal/download/manager.go`, `internal/catalog/catalog_ref.go` (register), `internal/wiring/wiring.go` (retire the clear dance), `internal/api/*` (emit canonical id), `web/src/lib/libraryApi.ts` (consume); regenerate sqlc; Test: `internal/download/manager_test.go`, `internal/store/migrate_*_test.go`, an api/e2e cover test.

**Consumes:** Task 1 (resolver in Manager), Task 2 (`catalogRef`), `catalog.CanonicalFor`.

- [ ] **Step 1 — migration** `0022`: `ALTER TABLE download_jobs ADD COLUMN canonical_id TEXT NOT NULL DEFAULT ''` (additive; empty = not-yet-minted; Down drops the column via table-rebuild per the repo's no-DROP-COLUMN convention — mirror an existing additive-column migration). Regenerate sqlc. Migration test: additive, pre-existing rows byte-identical, Down→Up round-trips.
- [ ] **Step 2 — mint at link time (runtime, scoped):** where a download job is linked to a library track (`runScan`/`backfillUnlinked` in manager.go, after a successful match), call `catalog.CanonicalFor(Identity{Kind:"track", Source, ExternalID, ISRC, Title, Artist, Album, DurationMs})` (the job carries these durable columns) and store `canonical_id` on the job row. Only for jobs being actively linked (open/visible) — do NOT bulk-backfill archived jobs, do NOT mint on a browse. Register `download_jobs.canonical_id` in Task 2's `repointCanonicalRefs`.
- [ ] **Step 3 — resolve-at-read + retire the dance:** emit the job's `canonical_id` (the `trk_` id) to the FE for its cover + stream (the DTO/API in `manager.go`/`api` + `libraryApi.ts`), so the boundary resolver owns re-resolution. Remove the swap-only `ClearMatchedDownloadJobLibraryRefs` call in `wiring.reconcileDownloadJobIdentity` (wiring.go:353) — bindings now re-resolve lazily through the canonical id instead of the clear+rematch dance. Keep `cover_url`/raw fallback where present.
- [ ] **Step 4 — tests:** (a) a completed download job's cover/playback is correct AFTER a simulated backend swap WITHOUT the clear-rematch dance (the resolver path re-resolves the canonical id) — the load-bearing swap-survival test; (b) `canonical_id` mint is idempotent + only for linked/open jobs (assert an archived/unlinked job is NOT minted); (c) a merge repoints `download_jobs.canonical_id` to the winner (via Task 2's helper). **Gate:** `go test ./internal/... && go build ./... && go vet ./...` + `cd web && npx tsc --noEmit && npm run build`. **Commit.**

---

## Task 4: `runScan` targeted binding-refresh

**Files:** Modify `internal/download/manager.go` (`runScan`); Test: `internal/download/manager_test.go`.

**Consumes:** Task 1 (resolver in Manager), Task 3 (jobs carry `canonical_id`).

- [ ] **Step 1 — RED test:** drive `runScan` against a fake scan controller + fake `BindingResolver`; assert after the scan links jobs, `RefreshLinked` is called with EXACTLY the canonical ids of the jobs linked in that scan; assert the ORDERING contract — a binding written during the scan is read as **fresh** (not stale) on the immediately-following `Resolve`; assert a plain scan does NOT bump `binding_epoch` (only `library_version`), so unrelated bindings survive.
- [ ] **Step 2 — implement:** in `runScan`, after the re-match/link loop, collect the linked jobs' `canonical_id`s and call `s.resolve().RefreshLinked(ctx, ids)` (nil-safe). Honor ordering: bump library_version first (existing), then RefreshLinked at the current epoch. **Gate:** `go test ./internal/download/... && go build ./...`. **Commit.**

---

## Task 5: playlist-sync resolve-at-read

**Files:** Modify `internal/playlistsync/service.go` (mint canonical for library-source tracks at add time; `Detail()` resolves via the resolver), the sync add path; Test: `internal/playlistsync/service_test.go`.

**Consumes:** Task 1 (resolver in Sync), `catalog.CanonicalFor`.

- [ ] **Step 1 — RED test:** with a resolver injected, a `library`-source track in `Detail()` resolves via `resolver.Resolve(canonicalID)` (not the raw `s.match.Match` fuzzy rung); and after a simulated swap (`BumpEpoch`) it re-resolves to the NEW backend id at read time — proving the storage-plus-read-time-resolve symmetry (retires the deferral comment at service.go:172-174).
- [ ] **Step 2 — implement:** mint a canonical id for a library-source synced track when it's added (CanonicalFor over its durable metadata; store it where the synced track ref lives — `tracks_json`/a column); `Detail()`'s library branch resolves via the resolver, nil-safe fallback to today's behavior when the resolver is absent. Register any new stored canonical ref in Task 2's helper if it's a distinct column. **Gate:** `go test ./internal/playlistsync/... && go build ./...`. **Commit.**

---

## Task 6: post-swap async pre-warm sweep

**Files:** Modify `internal/wiring/wiring.go` (`reconcileLibraryIdentity` schedules the sweep) or `cmd/reverb/main.go` (schedule after resolver construction); Test: `internal/wiring/*_test.go`.

**Consumes:** Task 1 (resolver reachable from the swap hook).

- [ ] **Step 1 — RED test:** on an identity change, a bounded async sweep pre-resolves the durably-referenced canonical rows (SP3 `plays.catalog_id`, completed download jobs' `canonical_id`); assert it's **best-effort** (a sweep failure does NOT fail `Build`), **bounded** (concurrency-capped), and idempotent (re-run with the same identity = no-op); a non-swept id still resolves lazily under singleflight.
- [ ] **Step 2 — implement:** after `BumpEpoch` in the swap path, enqueue a bounded, concurrency-limited goroutine (`context.WithoutCancel`-style lifetime) that calls `resolver.Resolve` over the durable canonical ids (query distinct `plays.catalog_id` ∪ `download_jobs.canonical_id` where non-empty). Cap concurrency; swallow per-id errors; never block Build. Remove/replace the wiring.go:316-321 DEFERRED comment. **Gate:** `go test ./internal/wiring/... && go build ./...`. **Commit.**

---

## Task 7: Full gate + whole-branch review + merge

- [ ] **Step 1 — full gate:** `go test ./... && go build ./... && go vet ./...`; `cd web && npx vitest run && npx tsc --noEmit && npm run build`. gofmt touched Go files.
- [ ] **Step 2 — whole-branch review (most capable model):** the construction-cycle break (no captured adapter/matcher — provider only; hot-reload survives); `runScan` ordering (no `binding_epoch` bump on plain scan; RefreshLinked at fresh epoch); the download-job swap-survival WITHOUT the clear-rematch dance; the mint-scoping (no pollution — only linked/open rows mint; reads Lookup-no-mint); `catalogRef` covers every stored ref (merge drops nothing); migration additive + runtime-backfill-only; nil-safe resolver deps. Confirm the OUT-OF-SCOPE caches (match_cache/external_maps/requests) are untouched.
- [ ] **Step 3 — fix blockers, ff-merge `feat/foundation-p2` → local main** (do NOT push; user pushes when ready). Live-verify plan: on soulkiller, a completed download's cover survives a backend re-scan/swap without the clear-rematch dance (the cover-rot killer), and playback works — verifiable via Claude-in-Chrome.

---

## Deferred (with P3): folding match_cache / album+artist external_maps / requests.cover_art_id onto canonical (dedup, self-healing already, catalog-pollution + artist-false-merge regression risk). P3 (canonical-on-the-wire) remains optional — reconfirm with the user after P2.
