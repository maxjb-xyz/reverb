# Configurable Downloader Granularity + Separated Chains — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make a downloader's granularity admin-configurable (capable vs. enabled), give spotDL real album-download capability, and show the song/album chains as a two-column Settings layout with independent per-chain ordering.

**Architecture:** Replace the fixed `Downloader.Granularity()` with `SupportedGranularities()`; each downloader instance carries an enabled-granularity→per-chain-order map (in its config JSON, defaulting to all supported ordered by `priority`); the manager holds `[]DownloaderEntry{Downloader, Order}` and `pick(G)` selects entries whose `Order` contains G, sorted by `Order[G]`. spotDL targets the album URL for album jobs (with a longer album-job timeout). The adapter DTO exposes supported + enabled granularities; Settings renders Song | Album columns reordered independently.

**Tech stack:** Go (download manager, registry, sqlc adapter instances) + React/TS Settings. Builds on the granularity model in `internal/download`.

## Global Constraints

- Granularity code values are exactly `"track"` (UI label "Song") and `"album"`; type `core.DownloadGranularity`, consts `core.GranularityTrack`/`core.GranularityAlbum`.
- `SupportedGranularities()`: spotDL = `{track, album}`, Lidarr = `{album}`.
- An instance's **enabled granularities + per-chain order** = `config.granularities` (a `map[string]int`: key = enabled granularity, value = order within that chain, ascending) when present & non-empty, ELSE all of `SupportedGranularities()` each mapped to the instance's `priority` column value. No data migration.
- `pick(G)`/`pickAfter` select entries whose `Order` map contains G, sorted by `Order[G]` (ties: by stable input order). A track request must NEVER select a downloader without `track` enabled (the guarantee, now keyed on the enabled set).
- spotDL album job → `https://open.spotify.com/album/<ExternalID>`; album-granularity SYNC jobs use a new `Config.AlbumJobTimeout` (default `2 * time.Hour`); track jobs keep `JobTimeout` (15m).
- The adapter form blocks unticking a downloader's LAST enabled granularity. Design tokens only in FE. Every task gates green: `go test ./... && go build ./... && go vet ./...`; FE tasks also `npx vitest run && npx tsc --noEmit && npm run build`. Commit footer `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.

---

### Task 1: `SupportedGranularities` + `DownloaderEntry` + granularity-aware `pick`

**Files:**
- Modify: `internal/download/download.go` (interface), `internal/download/spotdl/adapter.go` + `lidarr/adapter.go` (return sets), `internal/download/conformance.go`
- Modify: `internal/download/manager.go` (`DownloaderEntry`, `NewManager`, `m.downloaders`, `pick`, `pickAfter`)
- Modify: `internal/wiring/wiring.go` (`BuildDownloaders` wraps into entries with DEFAULT order)
- Test: `internal/download/manager_test.go`, adapter tests

**Interfaces produced:**
- `download.Downloader.SupportedGranularities() []core.DownloadGranularity` (replaces `Granularity()`).
- `download.DownloaderEntry struct { Downloader Downloader; Order map[core.DownloadGranularity]int }`.
- `download.NewManager(cfg Config, downloaders []DownloaderEntry, …)` (was `[]Downloader`).
- `pick(ctx, req) (Downloader, error)` and `pickAfter(ctx, req, afterName) (Downloader, error)`: select among entries whose `Order` contains `req.Granularity` (empty→track), sorted ascending by `Order[g]`.

- [ ] **Step 1 — failing tests** (`manager_test.go`): two fake track entries with `Order{track:1}` and `Order{track:0}` → `pick` returns the `Order{track:0}` one (order respected); an album req with only an album entry selects it; a track req never selects an album-only entry; `pickAfter` returns the next track entry by order after the named one. Fakes implement `SupportedGranularities()`.
- [ ] **Step 2 — run, expect FAIL** (compile: method renamed, NewManager sig).
- [ ] **Step 3 — implement:** rename the interface method to `SupportedGranularities()` returning the sets (spotDL `{track,album}`, Lidarr `{album}`); add `DownloaderEntry`; change `m.downloaders` to `[]DownloaderEntry` and `NewManager` accordingly; rewrite `pick`/`pickAfter` to filter entries by `Order` containing the (defaulted) granularity and sort by `Order[g]`. In `wiring.BuildDownloaders`, wrap each built downloader into a `DownloaderEntry` whose `Order` is `{g: int(inst.Priority)}` for every `g` in the plugin's `SupportedGranularities()` (DEFAULT resolution — config parsing comes in Task 2). Update conformance to assert `SupportedGranularities()` is non-empty and all valid. Fix all fakes/callers (manager_test fakes, any other NewManager callers) to compile.
- [ ] **Step 4 — run FULL `go test ./...`**, fix cross-package breakage (the interface rename + NewManager sig ripple).
- [ ] **Step 5 — gate green + commit.**

---

### Task 2: Resolve enabled+order from instance config

**Files:**
- Modify: `internal/wiring/wiring.go` (`BuildDownloaders` reads `config.granularities`)
- Create helper: `internal/wiring/granularity.go` (or inline) `resolveGranularityOrder(cfg map[string]any, supported []core.DownloadGranularity, priority int) map[core.DownloadGranularity]int`
- Test: `internal/wiring/wiring_test.go` (or a new `granularity_test.go`)

**Interfaces consumed:** Task 1's `DownloaderEntry`, `SupportedGranularities`.
**Interfaces produced:** `resolveGranularityOrder` — if `cfg["granularities"]` is a non-empty map, returns `{granularity: order}` from it (keys filtered to valid + supported granularities); else `{g: priority}` for every supported `g`.

- [ ] **Step 1 — failing tests:** `resolveGranularityOrder` with `cfg{"granularities":{"track":0,"album":2}}` → `{track:0, album:2}`; with empty cfg + supported `{track,album}` + priority 5 → `{track:5, album:5}`; a `granularities` key not in supported is dropped; a `granularities` with only `{"track":0}` for a both-supporting downloader → only `{track:0}` (album NOT enabled).
- [ ] **Step 2 — run, expect FAIL.**
- [ ] **Step 3 — implement** `resolveGranularityOrder` (JSON numbers unmarshal to `float64` in a `map[string]any` — convert to int); in `BuildDownloaders` replace the Task-1 default wrap with `Order: resolveGranularityOrder(cfg, plugin.SupportedGranularities(), int(inst.Priority))`.
- [ ] **Step 4 — run, expect PASS;** full `go test ./...`.
- [ ] **Step 5 — gate green + commit.**

---

### Task 3: spotDL album capability

**Files:**
- Modify: `internal/download/spotdl/adapter.go` (`Start` query construction)
- Test: `internal/download/spotdl/adapter_test.go`

- [ ] **Step 1 — failing test:** a `Start` (or the query-building helper) with `req.Granularity == core.GranularityAlbum`, `Source=="spotify"`, `ExternalID=="ALB"` builds the target `https://open.spotify.com/album/ALB`; with track granularity (or empty) + `ExternalID=="TRK"` builds `…/track/TRK` (unchanged). (If the URL is built inline in `Start`, extract a tiny `spotifyTargetURL(req) string` helper to make it unit-testable.)
- [ ] **Step 2 — run, expect FAIL.**
- [ ] **Step 3 — implement:** in the default (no-ManualURL) Spotify branch, choose `/album/` when `req.Granularity == core.GranularityAlbum`, else `/track/`. ManualURL + non-Spotify branches unchanged.
- [ ] **Step 4 — run, expect PASS.**
- [ ] **Step 5 — gate green + commit.**

---

### Task 4: Album-job timeout

**Files:**
- Modify: `internal/download/manager.go` (`Config.AlbumJobTimeout`, `withDefaults`, the per-job `jctx` timeout selection in the worker)
- Test: `internal/download/manager_test.go`

**Interfaces produced:** `Config.AlbumJobTimeout time.Duration` (default `2 * time.Hour`); the worker uses it for jobs whose request granularity is album, else `JobTimeout`.

- [ ] **Step 1 — failing test:** assert `Config{}.withDefaults().AlbumJobTimeout == 2*time.Hour`; and (via a fake slow album downloader + a tiny configured AlbumJobTimeout vs an even tinier JobTimeout) that an ALBUM-granularity job is allowed to run past `JobTimeout` but is killed at `AlbumJobTimeout` (a track job is killed at `JobTimeout`). Keep timing tolerances generous.
- [ ] **Step 2 — run, expect FAIL.**
- [ ] **Step 3 — implement:** add `AlbumJobTimeout` to `Config` + `withDefaults` (default `2*time.Hour`); where the worker builds the per-job timeout context (`jctx`), use `m.cfg.AlbumJobTimeout` when the job/request granularity is album, else `m.cfg.JobTimeout`.
- [ ] **Step 4 — run, expect PASS.**
- [ ] **Step 5 — gate green + commit.**

---

### Task 5: Adapter DTO — supported + enabled granularities (replace `grain:album`)

**Files:**
- Modify: `internal/api/adapters.go` (`toDTO` + `adapterInstanceDTO`), `cmd/reverb/main.go` (remove the `grain:album` probe), `web/src/lib/adaptersApi.ts` (DTO type)
- Test: `internal/api/adapters_test.go`

**Interfaces produced:** the downloader adapter-instance DTO gains `supportedGranularities []string` (from the plugin's `SupportedGranularities()`, via `pluginFor`) and `granularities map[string]int` (the RESOLVED enabled→order map for the instance, via the same `resolveGranularityOrder` as Task 2). Non-downloader adapters get empty/omitted.

- [ ] **Step 1 — failing test:** `GET /adapters` (or the toDTO unit) for a spotDL instance with no config → `supportedGranularities == ["track","album"]` and `granularities == {"track":<priority>,"album":<priority>}`; for a Lidarr instance → `supportedGranularities == ["album"]`, `granularities == {"album":<priority>}`; a spotDL instance with `config.granularities {"track":0}` → `granularities == {"track":0}` (album absent). The `grain:album` capability is gone from the caps list.
- [ ] **Step 2 — run, expect FAIL.**
- [ ] **Step 3 — implement:** in `toDTO`, when the plugin is a `download.Downloader`, set `SupportedGranularities` (as strings) and `Granularities` (resolved map). Remove the `RegisterCapability("grain:album", …)` call in `main.go`. Add the two fields to `adapterInstanceDTO` (+ FE `AdapterInstance` type in `adaptersApi.ts`). Reuse `resolveGranularityOrder` (export it from wiring or duplicate a tiny resolver in api — prefer sharing).
- [ ] **Step 4 — run, expect PASS;** `go test ./...` + FE `tsc`.
- [ ] **Step 5 — gate green + commit.**

---

### Task 6: FE — granularity checkboxes in the downloader's adapter form

**Files:**
- Modify: `web/src/routes/Settings.tsx` (the downloader adapter add/edit form), `web/src/lib/adaptersApi.ts` if a config helper is needed
- Test: `web/src/routes/Settings.test.tsx`

**Interfaces consumed:** the DTO's `supportedGranularities` + `granularities` (Task 5).

- [ ] **Step 1 — failing tests:** in a downloader's config form, a checkbox renders per `supportedGranularities` ("Song" for track, "Album" for album), checked when the granularity is a key of `granularities`; toggling a checkbox calls the adapter-update mutation writing `config.granularities` (adding the key with a default order = current max+1, or removing it); the LAST checked granularity's checkbox is disabled (can't untick to zero).
- [ ] **Step 2 — run, expect FAIL.**
- [ ] **Step 3 — implement:** render the checkboxes from `supportedGranularities`; on toggle, update `config.granularities` (add/remove the key) via the existing adapter-update mutation; disable unticking when only one granularity remains. Token-styled.
- [ ] **Step 4 — run vitest + tsc, expect PASS.**
- [ ] **Step 5 — gate green + commit.**

---

### Task 7: FE — two-column Downloaders section with independent reorder

**Files:**
- Modify: `web/src/routes/Settings.tsx` (replace the single Downloaders list with two columns)
- Test: `web/src/routes/Settings.test.tsx`

**Interfaces consumed:** the DTO's `granularities` map per instance.

- [ ] **Step 1 — failing tests:** the Downloaders section renders a **Song** column listing instances whose `granularities` has `"track"`, sorted by `granularities["track"]`, and an **Album** column for `"album"` sorted by `granularities["album"]`; a downloader enabled for both appears in both; up/down in the **Album** column swaps the `album` order of two instances (writing `config.granularities`) and does NOT change the Song column's order; first-row up / last-row down disabled per column.
- [ ] **Step 2 — run, expect FAIL.**
- [ ] **Step 3 — implement:** two token-styled columns built from `granularities`; per-column up/down reorder that swaps the order value for THAT granularity between two adjacent instances and persists via the adapter-update mutation (independent per chain). Remove the old flat single-list + the `grain:album`-derived label.
- [ ] **Step 4 — run FULL vitest + tsc + build, expect PASS.**
- [ ] **Step 5 — gate green + commit.**

---

### Task 8: e2e + final gate

**Files:**
- Modify: `web/e2e/mocks.ts` (adapter mocks expose `supportedGranularities` + `granularities`), `web/e2e/*.spec.ts` (a downloaders-settings spec)
- Test: the hermetic Playwright suite

- [ ] **Step 1 — write the spec:** mock a spotDL downloader adapter (supported `["track","album"]`, granularities `{"track":0,"album":0}`) + a Lidarr one (`["album"]`, `{"album":1}`); on Settings, assert the **Song** column lists spotDL and the **Album** column lists spotDL + Lidarr; toggling album off spotDL (via its form) removes it from the Album column; reordering the Album column persists. (Hermetic — assert the PATCH body carries the updated `config.granularities`.)
- [ ] **Step 2 — run the FULL gate:** `go test ./... && go build ./... && go vet ./...`; `cd web && npx vitest run && npx tsc --noEmit && npm run build && npm run e2e`. Report exact counts. (`playlist-sync.spec.ts` has a known `net::ERR_ABORTED` flake — re-run once if only that fails.)
- [ ] **Step 3 — commit.**

---

## Self-review notes
- **Spec coverage:** SupportedGranularities (T1), enabled+order resolution (T1 default / T2 config), independent pick by order (T1), spotDL album URL (T3), album timeout (T4), DTO supported+enabled (T5), adapter-form checkboxes (T6), two-column independent reorder (T7), e2e (T8). All spec sections mapped.
- **Type consistency:** `core.GranularityTrack/GranularityAlbum`; `SupportedGranularities() []core.DownloadGranularity`; `DownloaderEntry{Downloader, Order map[core.DownloadGranularity]int}`; `resolveGranularityOrder(cfg, supported, priority)`; DTO `supportedGranularities []string` + `granularities map[string]int`; `Config.AlbumJobTimeout`. Used identically across tasks.
- **Out of scope (unchanged):** album fan-out; the deferred Retry-of-failed-album-job fix.
