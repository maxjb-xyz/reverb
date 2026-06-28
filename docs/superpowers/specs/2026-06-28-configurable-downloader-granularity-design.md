# Configurable Downloader Granularity + Separated Chains — Design

**Status:** Approved (brainstorm 2026-06-28). Refines the just-shipped "granularity-aware downloader chains" feature (spec `2026-06-28-downloader-chains-album-requests-design.md`).

**Goal:** Make a downloader's granularity **admin-configurable** rather than a fixed property of its type — a downloader declares which granularities it's *capable* of (spotDL = song **and** album; Lidarr = album), and the admin enables which it *serves* per downloader. Give spotDL real album-download capability so albums work without Lidarr, and show the song/album chains as a clean two-column layout.

**Tech stack:** Go (download manager, registry, sqlc adapter instances) + React/TS Settings. Builds directly on the granularity model already in `internal/download`.

---

## Background — what exists today

- `download.Downloader.Granularity() core.DownloadGranularity` returns ONE fixed value: spotDL=`track`, Lidarr=`album` (`spotdl/adapter.go`, `lidarr/adapter.go`). The value `track` is the code term; the UI calls it **"Song"**.
- `manager.go pick()`/`pickAfter()` filter candidates by `d.Granularity() == req.Granularity` before `CanDownload`.
- A `grain:album` capability probe (`main.go`) surfaces granularity to the FE; Settings shows a single flat "Downloaders" list with a read-only Track/Album label + up/down reorder writing `adapter_instances.priority`.
- spotDL builds its target as `https://open.spotify.com/track/<ExternalID>` (`spotdl/adapter.go Start`); it can equally take `…/album/<id>` to fetch every track. `CanDownload` = title+artist present.
- Album requests route to the album chain; with no album-enabled downloader the request is created but the enqueue fails ("no album downloader") — the wall hit in live testing.

## Decisions (from brainstorm)

1. **Capable vs. enabled.** A downloader declares `SupportedGranularities()` (capability, code-level). Each *instance* has an admin-chosen **enabled** subset. The chains are built from the enabled sets.
2. **spotDL album = one long sync job** (point spotDL at the album URL). Album-granularity sync jobs get a longer timeout. One request ↔ one job is preserved (no fan-out).
3. **Default enablement:** when an instance hasn't been configured, enabled = its full supported set. So spotDL defaults to **{song, album}** (album requests work out of the box) and Lidarr to **{album}** — no data migration needed.
4. **Two-column UI**, single shared `priority` for ordering (independent per-chain ordering deferred — see §6).

---

## Architecture

### 1. SupportedGranularities (capability)

Replace `Downloader.Granularity() core.DownloadGranularity` with `SupportedGranularities() []core.DownloadGranularity`:
- spotDL → `{GranularityTrack, GranularityAlbum}`
- Lidarr → `{GranularityAlbum}`

The conformance suite asserts each downloader returns a non-empty set of valid granularities.

### 2. Enabled granularities (per-instance config)

Each downloader `adapter_instance`'s config JSON gains an optional `granularities` array (the enabled subset), e.g. `{"granularities":["track","album"]}`. **Resolution rule:** an instance's enabled granularities = `config.granularities` if present and non-empty, else `SupportedGranularities()` (the full supported set). This makes the §Decisions-3 default automatic (unset → all supported enabled) with **no migration**, and an admin restricts by editing the instance.

The built `Downloader` must carry its enabled set so `pick()` can filter without re-reading the DB. `wiring.BuildDownloaders` already reads each instance; it wraps each constructed downloader with its resolved enabled granularities (a small wrapper carrying `enabled []core.DownloadGranularity` + the underlying `Downloader`, or an added `EnabledGranularities()` accessor the manager populates at build). `pick(G)`/`pickAfter` then filter on **enabled** (contains G) instead of `Granularity() == G`. The "track never reaches album" guarantee is unchanged — it now keys on the enabled set.

### 3. spotDL album capability

`spotdl/adapter.go Start`: when `req.Granularity == GranularityAlbum` and `req.Source == "spotify"` and `req.ExternalID != ""`, target `https://open.spotify.com/album/<ExternalID>` (spotDL downloads every track into the music folder; the post-download scan + the album request's tracker handle the rest). All other cases unchanged. `CanDownload` stays title/artist-based (an album request carries the album name as title+artist).

### 4. Longer timeout for album sync jobs

A full-album spotDL download far exceeds the per-track `JobTimeout`. Add an **album job timeout** (a new manager config, default generous — e.g. 2h) applied when the job's granularity is album and it runs on the sync lane. (Lidarr/album stays async on the reconciler lane with its existing `AsyncMaxAge`, untouched.)

### 5. Capability/API surface

Replace the single `grain:album` probe with a per-instance granularity surface the FE can read: each downloader adapter-instance DTO exposes its **supported** granularities and its **enabled** granularities (e.g. two string arrays, derived via the registry from the plugin's `SupportedGranularities()` + the instance config). The FE uses *supported* to render the toggle checkboxes and *enabled* to place the downloader in the Song/Album columns.

### 6. UI — two columns + per-downloader toggles

- **Adapter edit form** (the downloader's options, in Settings): a checkbox per **supported** granularity — spotDL shows **Song** + **Album**, Lidarr shows **Album** only (single supported → shown, not unconfigurable below zero). Checking/unchecking writes `config.granularities`. At least one granularity must stay enabled (can't disable a downloader's last granularity — it'd belong to no chain).
- **Settings → Downloaders section**: ONE section titled "Downloaders" with two columns side by side — **Song** and **Album**. Each column lists the instances whose *enabled* set includes that granularity, ordered by `priority`, with up/down reorder (swapping `priority`). A downloader enabled for both appears in both columns. Helper copy: "Each chain is tried in order. Enable a downloader for a granularity in its settings." Design tokens only.
- **Ordering model:** single shared `priority` per instance; the columns display filtered+sorted by it. With the realistic downloader set (spotDL both, Lidarr album-only) this is already effectively independent. Fully-independent per-chain ordering (needed only when 2+ downloaders serve both chains with *different* desired orders) is a deferred future add — noted, not built.

---

## Data flow

- **Track add** → song chain = downloaders with `track` enabled, in priority order → fallback among them on failure (unchanged mechanics).
- **Album request** → album chain = downloaders with `album` enabled, in priority order. If spotDL is album-enabled (default), an auto-approved album request enqueues a spotDL album job (long sync job, album timeout); if Lidarr is configured + album-enabled, it's in the chain too (priority decides order, fallback applies). With no album-enabled downloader, the request still creates but enqueue errors clearly ("no album downloader") — same as today.

## Error handling

- An instance with an empty `config.granularities` resolves to its full supported set (never "belongs to no chain" by accident); the adapter form prevents unticking the last granularity.
- `pick(G)` errors clearly when no enabled downloader serves G (existing "no <granularity> downloader" message).
- A spotDL album job that exceeds the album timeout fails like any timeout (terminal, no fallback) — unchanged worker semantics.

## Migration / compatibility

- **No data migration.** `SupportedGranularities` is code-derived; enabled = config-or-supported (unset → all supported), so existing spotDL instances immediately serve song+album and existing Lidarr instances serve album.
- The old `grain:album` probe is replaced by the supported/enabled DTO surface; the FE Settings reads the new fields.

## Out of scope (explicit)

- **Fully-independent per-chain ordering** (per-granularity priority) — deferred; single shared `priority` until a downloader set actually needs different per-chain orders.
- **Album fan-out** (one job per track) — rejected in brainstorm; one spotDL job per album.
- **The deferred Retry-of-failed-album-job fix** from the prior feature — separate follow-up, not this spec.

## Testing

- `SupportedGranularities` returns the right sets (spotDL both, Lidarr album); conformance asserts non-empty/valid.
- Enabled resolution: unset config → full supported; an explicit `["track"]` → only track; the resolver is unit-tested.
- `pick(track)` selects only track-enabled downloaders; `pick(album)` only album-enabled; an instance enabled for both is selected for both; the track-never-reaches-album guarantee holds via the enabled set.
- spotDL `Start` with album granularity builds the `…/album/<id>` URL; track granularity unchanged.
- The album sync job uses the album timeout, not the per-track one.
- The adapter DTO exposes supported + enabled granularities; the adapter form renders a checkbox per supported granularity and blocks unticking the last; Settings renders the two columns from enabled sets and reorders via `priority`.
- e2e/integration: an album request with spotDL album-enabled (no Lidarr) enqueues a spotDL album job (granularity album); the Settings two-column layout shows spotDL under both Song and Album after enabling album on it.
