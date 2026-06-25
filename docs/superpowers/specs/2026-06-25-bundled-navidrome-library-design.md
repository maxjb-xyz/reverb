# Batteries-Included Library (Bundled Navidrome) — Design Spec

> Phase 3 / first library-engine milestone. Make Reverb work **out of the box with
> zero external dependencies** by bundling **Navidrome** inside the Reverb image and
> running it as a **supervised child process**. A fresh install is a single container:
> mount a music folder, done. "Bring your own Subsonic server" stays a first-class but
> **secondary** option. Because Navidrome already does scanning, transcoding, and
> file-watching, this **one milestone subsumes** the originally-planned native
> scan/transcode/watch engine work. **No new `LibraryAdapter` is written** — the
> existing conformance-tested `subsonic` adapter is pointed at a localhost child.

- **Status:** Approved design (brainstormed 2026-06-25), ready for implementation planning.
- **Author:** Reverb maintainer + Claude.
- **Supersedes (strategy):** the prior "build our own native folder-scan library engine" direction (the old Phase-3 M1 scanner / M2 transcoder / M3 watcher milestones). We instead **bundle** a mature server. Building a native engine is **not abandoned forever** — it is deferred behind the `LibraryAdapter` seam and revisited only if a concrete constraint (operational pain or a feature the Subsonic API can't express) ever justifies it.
- **Builds on:** the `library.LibraryAdapter` seam + conformance suite (`internal/library/library.go`, `internal/library/conformance.go`), the existing `subsonic` adapter (`internal/library/subsonic/`), DB-canonical config + the library/search/downloader registries and wiring (`internal/wiring`, `internal/config`), the existing `StartScan`/`ScanStatus` proxy, and the Docker image that already ships python3 + ffmpeg + spotdl.

---

## 1. Goals & Non-Goals

### Goals
1. **Zero-dependency default.** A fresh Reverb install is a single container. The user mounts a music folder (default `/music`) and Reverb serves, scans, transcodes, and watches it — with no separately-installed Navidrome.
2. **Bundle, don't build.** Ship a pinned Navidrome binary in the image and run it as a supervised child. Reuse the existing `subsonic` adapter wholesale (no new adapter, no new library schema in Reverb).
3. **Two backend modes, one config switch.** `built-in` (default, the bundled child) and `external` (today's user-provided Subsonic server). External deployments are untouched by this change.
4. **Invisible Navidrome.** The user never sees or configures Navidrome. Its config, data dir, and an internal admin credential are auto-provisioned; its web UI is bound to localhost and never exposed or proxied.
5. **Resource invariant.** Navidrome runs **if and only if** backend mode is `built-in`. In `external` mode nothing is exec'd — no process, no scan, no DB, no RAM/CPU. Switching modes at runtime starts/stops the child cleanly; the two states are mutually exclusive and never leave an orphan.
6. **Resilient lifecycle.** Reverb supervises the child (start → wait-for-ready → healthy, restart-on-crash with backoff, clean SIGTERM shutdown). If Navidrome can't stay up, Reverb stays up and reports a **degraded library** rather than crash-looping the whole container.

### Non-Goals (this milestone)
- **A native scanner / transcoder / file-watcher.** Navidrome owns all three. Deferred behind the adapter seam.
- **Library federation / aggregation.** Decided: **replace (pick-one)** — built-in OR external is active at a time, never both merged. No namespaced IDs, no merged search, no aggregation layer.
- **Exposing or proxying Navidrome's web UI / admin**, or mapping Reverb users onto Navidrome users. Single internal credential only.
- **Forced migration** of existing external-Subsonic deployments. Bundled is the default for *fresh* installs; existing configs are left as-is.
- **Bundling an alternative server** (gonic, etc.). Navidrome only; the mode switch leaves room.
- **Multi-arch beyond what the image already targets.** We match the current image's arch set (amd64/arm64), no more.

---

## 2. Locked Product Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Bundle vs. build native | **Bundle Navidrome** | The user-facing win (single container, no setup) comes from packaging, not re-implementing. The scanner's hard 80% (compilations, multi-disc, embedded art, incremental-scan correctness, transcode cache, scale) is undifferentiated muck Navidrome already hardened. Reverb's edge is the experience layer. |
| Coexistence model | **Replace (pick-one)** | Built-in is just a second way to populate the *same* `subsonic` adapter. No federation layer touching search/IDs/streaming. |
| Which server | **Navidrome** | Most mature; great transcoder/scanner; single Go binary, trivial to bundle. **License: GPL-3.0.** We ship the *unmodified upstream binary* and run it as a *separate process* (exec'd, **not** linked into Reverb's binary) — mere aggregation, compatible with Reverb's AGPL-3.0-only. Comply with GPL distribution terms by pointing at the unmodified upstream source. |
| Packaging / supervision | **Single container; Reverb execs + supervises Navidrome as a child** | Matches the "single Docker container" philosophy; Reverb owns the child's lifecycle. No s6/second base layer needed for one child. |
| Adapter | **Reuse the existing `subsonic` adapter, unchanged** | Bundled mode = auto-provision the external-Subsonic config to point at `127.0.0.1:4533`. Conformance suite already covers it. |
| Navidrome UI | **Hidden (localhost-only, never proxied)** | Reverb is the only face; avoids a confusing second UI/auth surface. |
| Resource use in external mode | **Child never started** | Supervision gated on `mode == built-in`. No idle Navidrome. |
| Failure posture | **Degraded-but-alive** | A repeatedly-failing child must not crash-loop the whole container; Reverb reports a degraded library and keeps serving everything else. |
| Migration | **None forced** | Existing external setups untouched; bundled is the fresh-install default only. |

---

## 3. Architecture

### 3.1 Components

```
container (entrypoint = Reverb, with subreaper/tini for zombie reaping)
 ├─ Reverb Go process
 │   ├─ internal/library/embedded   ← NEW: supervisor + config provisioning
 │   │     • generates Navidrome config (data dir, music dir, internal creds, localhost bind)
 │   │     • execs ./navidrome as a child  ⇔  only when mode == built-in
 │   │     • monitors: start → ready → healthy; restart w/ backoff; SIGTERM on shutdown
 │   │     • exposes Ready()/Healthy() → feeds library/scan status
 │   └─ internal/library/subsonic    ← UNCHANGED adapter, pointed at 127.0.0.1:4533
 └─ navidrome (child)  127.0.0.1:4533, UI not exposed
       • data/DB at /config/navidrome
       • scans /music ; transcodes ; watches
```

### 3.2 The supervisor (`internal/library/embedded`)
A new package owning the child's lifecycle. Sketch of the seam (final names settled in the plan):

```go
// Supervisor manages a bundled Navidrome child process. It runs the child IFF
// backend mode is built-in; in external mode every method is a safe no-op.
type Supervisor interface {
    // Ensure brings actual state in line with desired state: starts the child if
    // mode==built-in and it isn't running; stops + reaps it if mode==external.
    // Idempotent — safe to call on config change.
    Ensure(ctx context.Context, cfg BackendConfig) error

    // Ready reports whether the child has come up and answers a Subsonic ping.
    Ready() bool
    // Health reports running / starting / degraded for the library status surface.
    Health() LibraryHealth

    // Shutdown sends SIGTERM to the child and waits (bounded) for it to exit.
    Shutdown(ctx context.Context) error
}
```

- **Start:** write/refresh Navidrome's config file, `exec` the binary, then poll the Subsonic `ping` endpoint until it answers (bounded readiness wait) → `Ready`.
- **Restart-on-crash:** if the child exits unexpectedly while desired==built-in, restart with exponential backoff up to a cap; after the cap, mark `degraded` and stop hammering (Reverb stays up).
- **Shutdown:** forward SIGTERM, wait with a timeout, then SIGKILL as a last resort.
- **Mode switch at runtime:** `Ensure` is called whenever backend config changes. built-in→external stops + reaps the child; external→built-in starts it. The two are mutually exclusive by construction.

### 3.3 Config provisioning (invisible Navidrome)
On entering built-in mode, Reverb generates Navidrome's config deterministically:
- **Data dir:** `/config/navidrome` (its own SQLite DB, cache, search index).
- **Music dir:** the configured library path (default `/music`).
- **Bind:** `127.0.0.1:4533` only — never `0.0.0.0`, never published.
- **Internal credential:** a random admin username + strong password, generated once and **persisted in Reverb's config/DB**, so the `subsonic` adapter can authenticate. Regenerated only if missing.
- **Scan:** Navidrome's own schedule + scan-on-startup; Reverb's existing `StartScan` proxy still triggers an on-demand scan.

The `subsonic` adapter's existing URL/username/password config fields are populated from these values in built-in mode. **This is the entire integration** — bundled mode is "auto-fill the external config to point at our child."

### 3.4 Reverb data model touchpoints
- A **backend mode** setting (`built-in` | `external`) in DB-canonical config, defaulting to `built-in` on fresh installs.
- Internal Navidrome credentials persisted in config (not user-editable).
- No change to `core` domain types, the adapter contract, or managed-playlist storage — those already treat the library as adapter-provided and survive the backend choice.

---

## 4. Config & Onboarding UX

- **Settings → Library** gains a **backend mode** control:
  - **Built-in (default):** no URL field. Shows the **music folder path** (prefilled `/music`) and a **"Scan now"** button (reuses `StartScan`/`ScanStatus`). A "library starting / scanning / N tracks" status reflects supervisor health + scan state. First scan auto-runs on first boot.
  - **External Subsonic:** today's URL / username / password form + Test Connection, unchanged.
- **Setup wizard:** the "connect your library" step leads with **built-in** ("Reverb will manage your music folder for you"); **external** is the "I already run Navidrome/Subsonic" toggle.
- **Switching modes** in settings calls `Supervisor.Ensure` → starts/stops the child accordingly, with clear UI feedback ("starting built-in library…").
- **Existing external deployments:** their stored config already selects external; they see no behavior change and no running child.

---

## 5. Packaging & Deployment

- **Dockerfile:** download a **pinned** Navidrome release for each target arch (amd64/arm64) and bake the binary into the image, alongside the existing python3 + ffmpeg + spotdl layers. Keep the image non-root.
- **Entrypoint:** Reverb is PID 1 with proper subreaping (tini or a Go subreaper) so the Navidrome child is reaped and signals propagate.
- **Version surfacing:** the pinned Navidrome version is reported in `GET /api/v1/version` next to Reverb's.
- **Volumes:** `/config` (Reverb config/DB + `/config/navidrome`), `/music` (library source; also spotDL's download output, so downloads land where the bundled scanner sees them). Compose docs updated: built-in needs only the `/music` mount; the external example stays for BYO users.
- **Image size:** grows by the Navidrome binary (~tens of MB) — acceptable.

---

## 6. Lifecycle, Health & Failure Handling

| Event | Behavior |
|---|---|
| First boot (built-in) | Provision config → start child → wait-for-ready → auto-scan; UI shows "library starting" then "scanning" then ready. |
| Child crashes (desired=built-in) | Restart with exponential backoff up to a cap. |
| Crash cap exceeded | Mark library **degraded**; stop restart attempts; Reverb stays up and surfaces the degraded state. Library-dependent reads return a clear "library unavailable" rather than hanging. |
| Mode → external | SIGTERM + reap the child; supervisor goes inert; adapter now targets the user's server. |
| Mode → built-in | Provision (if needed) + start the child; adapter retargets localhost. |
| Container SIGTERM | Reverb forwards SIGTERM to the child, waits (bounded), then exits. |
| Library read before ready | Gated on `Ready()`/`Health()` — UI shows "library starting," not an error. |

---

## 7. Testing Strategy

- **Unit (`internal/library/embedded`):**
  - Supervisor lifecycle against a **fake/stub child** (a controllable process or injected runner): start→ready, crash→restart-with-backoff, cap→degraded, shutdown reaps.
  - `Ensure` idempotency and **mode-gating**: external mode starts nothing; built-in→external stops + reaps; external→built-in starts. Assert **no child in external mode**.
  - Config generation: deterministic Navidrome config (paths, localhost bind, internal creds persisted + reused, not regenerated when present).
- **Integration:** run the **existing `subsonic` conformance suite** against a bundled instance to prove the adapter behaves identically pointed at the child.
- **E2e (fresh-install path):** container boots in built-in mode → Navidrome comes up → scan completes over a tiny fixture library → a track plays through the existing core loop. Plus a mode-switch check: flipping to external stops the child.
- **Build gate (unchanged):** `npm run build` + `go test` (`-race`) + `npm run e2e`, all green, before merge.

---

## 8. Open Questions / Deferred

- **Exposing Navidrome's UI** for power users (an opt-in "advanced" toggle/port) — deferred; default hidden.
- **Bundled-server choice beyond Navidrome** (gonic for a lighter image) — deferred; the mode switch leaves room.
- **Native engine** (own scanner/transcoder/watcher) — deferred behind the adapter seam; revisit only on a concrete constraint.
- **Pinned-version auto-update** of the bundled Navidrome — out of scope; version bumps are a Dockerfile/release change.

---

*Reverb — own your music, again. Now with batteries included.*
