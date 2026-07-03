// Package wiring builds the active library/search/download services from the
// enabled adapter_instance rows. It is shared by the composition root (cmd/reverb)
// for the initial build and by the API server for live rebuilds on adapter
// mutations (no restart required).
package wiring

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/maxjb-xyz/reverb/internal/core"
	"github.com/maxjb-xyz/reverb/internal/coverage"
	"github.com/maxjb-xyz/reverb/internal/download"
	"github.com/maxjb-xyz/reverb/internal/library"
	"github.com/maxjb-xyz/reverb/internal/library/embedded"
	"github.com/maxjb-xyz/reverb/internal/matching"
	"github.com/maxjb-xyz/reverb/internal/playlistsync"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/resolver"
	"github.com/maxjb-xyz/reverb/internal/search"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// BuildLibraryAdapter builds the active library adapter. In built-in mode it
// synthesizes a subsonic adapter pointed at the bundled localhost Navidrome with
// the internal admin credentials, ignoring stored instances. In external mode it
// uses the first enabled "library" adapter instance (legacy behavior).
func BuildLibraryAdapter(
	ctx context.Context,
	reg *registry.Registry,
	instances []db.AdapterInstance,
	getenv func(string) string,
	mode embedded.Mode,
	creds embedded.Credentials,
) (library.LibraryAdapter, error) {
	if mode == embedded.ModeBuiltIn {
		plugin, err := reg.Create("subsonic")
		if err != nil {
			return nil, fmt.Errorf("built-in library: %w", err)
		}
		lib, ok := plugin.(library.LibraryAdapter)
		if !ok {
			return nil, fmt.Errorf("built-in library: subsonic is not a LibraryAdapter")
		}
		if err := lib.Init(map[string]any{
			"url":      "http://127.0.0.1:4533",
			"username": creds.Username,
			"password": creds.Password,
		}); err != nil {
			return nil, fmt.Errorf("built-in library init: %w", err)
		}
		return lib, nil
	}

	// external mode (unchanged behavior)
	var inst *db.AdapterInstance
	for i := range instances {
		if instances[i].Type == "library" && instances[i].Enabled == 1 {
			inst = &instances[i]
			break
		}
	}
	if inst == nil {
		return nil, nil
	}
	plugin, err := reg.Create(inst.Name)
	if err != nil {
		return nil, fmt.Errorf("library adapter %q: %w", inst.Name, err)
	}
	lib, ok := plugin.(library.LibraryAdapter)
	if !ok {
		return nil, fmt.Errorf("library adapter %q: not a LibraryAdapter", inst.Name)
	}
	cfg := map[string]any{}
	if inst.ConfigJson != "" {
		if err := json.Unmarshal([]byte(inst.ConfigJson), &cfg); err != nil {
			return nil, fmt.Errorf("library adapter %q config: %w", inst.Name, err)
		}
	}
	if pw := getenv("REVERB_LIBRARY_PASSWORD"); pw != "" {
		cfg["password"] = pw
	}
	if err := lib.Init(cfg); err != nil {
		return nil, fmt.Errorf("library adapter %q init: %w", inst.Name, err)
	}
	return lib, nil
}

// BuildSearchSources instantiates every ENABLED adapter_instance of type
// "search" from the registry, applying REVERB_SPOTIFY_CLIENT_SECRET onto the
// spotify config_json just before Init (env wins; never sent to the browser).
// instances are already ordered by (type, priority) from ListAdapterInstances.
func BuildSearchSources(reg *registry.Registry, instances []db.AdapterInstance, getenv func(string) string) []search.SearchSource {
	out := []search.SearchSource{}
	for i := range instances {
		inst := instances[i]
		if inst.Type != "search" || inst.Enabled != 1 {
			continue
		}
		plugin, err := reg.Create(inst.Name)
		if err != nil {
			log.Printf("WARNING: search source %q create failed: %v — skipping", inst.Name, err)
			continue
		}
		src, ok := plugin.(search.SearchSource)
		if !ok {
			log.Printf("WARNING: adapter %q is not a SearchSource — skipping", inst.Name)
			continue
		}

		cfg := map[string]any{}
		if inst.ConfigJson != "" {
			if err := json.Unmarshal([]byte(inst.ConfigJson), &cfg); err != nil {
				log.Printf("WARNING: search source %q config parse failed: %v — skipping", inst.Name, err)
				continue
			}
		}
		// Env secret override (Spotify) — env wins for client_secret before Init.
		if inst.Name == "spotify" {
			if sec := getenv("REVERB_SPOTIFY_CLIENT_SECRET"); sec != "" {
				cfg["client_secret"] = sec
			}
		}

		if err := src.Init(cfg); err != nil {
			log.Printf("WARNING: search source %q init failed: %v — skipping", inst.Name, err)
			continue
		}
		out = append(out, src)
	}
	return out
}

// BuildDownloaders instantiates every ENABLED adapter_instance of type
// "downloader" from the registry, applying env overrides (REVERB_SPOTDL_PATH →
// binary_path, REVERB_DOWNLOAD_DIR → output_dir) just before Init. instances are
// ordered by (type, priority) from ListAdapterInstances, so the returned slice is
// already in fallback-chain order. Each downloader is wrapped into a
// DownloaderEntry whose Order map contains {g: int(inst.Priority)} for every g in
// plugin.SupportedGranularities() — the DEFAULT resolution; Task 2 adds config
// parsing for per-granularity overrides. Per-source failures warn-and-skip.
func BuildDownloaders(reg *registry.Registry, instances []db.AdapterInstance, getenv func(string) string) []download.DownloaderEntry {
	out := []download.DownloaderEntry{}
	hasDownloaderInstance := false
	for i := range instances {
		inst := instances[i]
		if inst.Type != "downloader" {
			continue
		}
		hasDownloaderInstance = true
		if inst.Enabled != 1 {
			continue
		}
		plugin, err := reg.Create(inst.Name)
		if err != nil {
			log.Printf("WARNING: downloader %q create failed: %v — skipping", inst.Name, err)
			continue
		}
		dl, ok := plugin.(download.Downloader)
		if !ok {
			log.Printf("WARNING: adapter %q is not a Downloader — skipping", inst.Name)
			continue
		}

		cfg := map[string]any{}
		if inst.ConfigJson != "" {
			if err := json.Unmarshal([]byte(inst.ConfigJson), &cfg); err != nil {
				log.Printf("WARNING: downloader %q config parse failed: %v — skipping", inst.Name, err)
				continue
			}
		}
		// Env overrides (spotdl) before Init.
		if inst.Name == "spotdl" {
			if p := getenv("REVERB_SPOTDL_PATH"); p != "" {
				cfg["binary_path"] = p
			}
			if d := getenv("REVERB_DOWNLOAD_DIR"); d != "" {
				cfg["output_dir"] = d
			}
			// Reuse the same Spotify app creds as the search source (env wins).
			if id := getenv("REVERB_SPOTIFY_CLIENT_ID"); id != "" {
				cfg["client_id"] = id
			}
			if sec := getenv("REVERB_SPOTIFY_CLIENT_SECRET"); sec != "" {
				cfg["client_secret"] = sec
			}
		}

		if err := dl.Init(cfg); err != nil {
			log.Printf("WARNING: downloader %q init failed: %v — skipping", inst.Name, err)
			continue
		}
		out = append(out, download.DownloaderEntry{
			Downloader: dl,
			Order:      resolveGranularityOrder(cfg, dl.SupportedGranularities(), int(inst.Priority)),
		})
	}

	// Bundled default: the image ships spotDL + ffmpeg, so when the user has not
	// configured any downloader, fall back to a spotDL instance writing to
	// REVERB_DOWNLOAD_DIR (the Docker image sets this to /music). This makes
	// downloads work out of the box with zero setup. We only inject the default
	// when there is NO downloader instance at all — if the user configured (or
	// deliberately disabled) one, that choice is respected. Gated on the env being
	// set so local/dev runs without it are unaffected.
	if len(out) == 0 && !hasDownloaderInstance {
		if dir := getenv("REVERB_DOWNLOAD_DIR"); dir != "" {
			if entry := buildDefaultSpotdl(reg, dir, getenv); entry != nil {
				out = append(out, *entry)
			}
		}
	}
	return out
}

// buildDefaultSpotdl constructs the bundled spotDL downloader entry (output_dir=dir).
// Returns nil (with a log line) if spotDL can't be created/initialised, e.g. a
// build/registry without it — never fatal.
func buildDefaultSpotdl(reg *registry.Registry, dir string, getenv func(string) string) *download.DownloaderEntry {
	plugin, err := reg.Create("spotdl")
	if err != nil {
		log.Printf("bundled spotdl downloader unavailable: %v", err)
		return nil
	}
	dl, ok := plugin.(download.Downloader)
	if !ok {
		return nil
	}
	cfg := map[string]any{"output_dir": dir}
	if p := getenv("REVERB_SPOTDL_PATH"); p != "" {
		cfg["binary_path"] = p
	}
	if id := getenv("REVERB_SPOTIFY_CLIENT_ID"); id != "" {
		cfg["client_id"] = id
	}
	if sec := getenv("REVERB_SPOTIFY_CLIENT_SECRET"); sec != "" {
		cfg["client_secret"] = sec
	}
	if err := dl.Init(cfg); err != nil {
		log.Printf("bundled spotdl downloader unavailable: %v", err)
		return nil
	}
	log.Printf("using bundled spotdl downloader (output_dir=%s)", dir)
	// Default order: priority 0 for all supported granularities.
	order := make(map[core.DownloadGranularity]int, len(dl.SupportedGranularities()))
	for _, g := range dl.SupportedGranularities() {
		order[g] = 0
	}
	return &download.DownloaderEntry{Downloader: dl, Order: order}
}

// ServiceBundle is the set of active services built from the current enabled
// adapter_instance rows. Any field may be nil when nothing is configured. The
// Manager is constructed but NOT started — the caller controls its lifecycle.
type ServiceBundle struct {
	Library    library.LibraryAdapter // may be nil
	Aggregator *search.Aggregator     // may be nil
	Coverage   *coverage.Service      // may be nil (needs a library + a DiscoSource)
	Manager    *download.Manager      // may be nil; NOT started yet
	Sync       *playlistsync.Service  // may be nil (needs a library, a Manager + a PlaylistProvider)
	Supervisor *embedded.Supervisor   // bundled Navidrome supervisor; nil in external mode wiring helpers
	// Matcher is the live *matching.Service for the current library adapter, exposed
	// so the long-lived resolver singleton can re-match against the CURRENT adapter
	// after a hot-reload rebuilds the bundle. Nil when no library is configured.
	Matcher resolver.Rematcher
}

// VersionStore is the library_version reader/writer the Manager + matcher need.
// *store.Store satisfies it.
type VersionStore interface {
	LibraryVersion(ctx context.Context) (int64, error)
	SetLibraryVersion(ctx context.Context, v int64) error
}

const settingLibraryIdentity = "library_identity"
const settingDownloadJobIdentity = "download_jobs_library_identity"

// libraryIdentity returns a stable fingerprint of the active library backend.
// Different backends (bundled vs a given external server) assign different track
// IDs, so a change in identity means cached matches are no longer valid.
func libraryIdentity(mode embedded.Mode, instances []db.AdapterInstance) string {
	if mode == embedded.ModeBuiltIn {
		return "builtin"
	}
	for i := range instances {
		if instances[i].Type == "library" && instances[i].Enabled == 1 {
			var cfg map[string]any
			_ = json.Unmarshal([]byte(instances[i].ConfigJson), &cfg)
			if u, ok := cfg["url"].(string); ok && u != "" {
				return "external:" + u
			}
			return "external:" + instances[i].ID
		}
	}
	return "external"
}

// sweepLimit is the maximum number of durable canonical IDs the post-swap
// pre-warm sweep will resolve. Bounds memory + work on a large library.
// The query uses LIMIT so only this many rows are ever fetched.
const sweepLimit = 500

// sweepConcurrency is the semaphore width for the post-swap sweep goroutine:
// at most this many Resolve calls run concurrently.
const sweepConcurrency = 6

// reconcileLibraryIdentity bumps library_version (invalidating the match + coverage
// caches) when the active library backend's identity differs from the last boot,
// so matches from a previous backend (with different track IDs) are not reused.
// On the identity-CHANGED path it also bumps binding_epoch:<identity> for the
// NEW identity, causing all resolver bindings for that identity to be treated as
// stale on the next Resolve. This handles the "returning to a previously-used
// backend whose IDs may have changed while away" case; for a never-seen identity
// the bump is harmless (epoch goes 1→2, and was never read at 1 anyway).
//
// The epoch bump is done directly via b.queries (the settings table) rather than
// via resolver.Service.BumpEpoch because the resolver Service is constructed
// AFTER Builder.Build returns — wiring it in here would create a construction
// cycle. resolver.BumpEpoch is the shared helper that ensures the key format is
// identical to what the Service reads.
//
// P2/SP3 piece 3 (wired): after the epoch bump, if resolverProvider is set, a
// bounded best-effort async sweep pre-warms bindings for all durable canonical ids
// (plays.catalog_id ∪ download_jobs.canonical_id) so the first post-swap /stream
// or /cover doesn't block on a synchronous re-resolve. The sweep is NEVER a
// correctness dependency — missed ids re-resolve lazily on the first request.
//
// No-op when the identity is unchanged.
func (b *Builder) reconcileLibraryIdentity(ctx context.Context, identity string) error {
	if stored, err := b.queries.GetSetting(ctx, settingLibraryIdentity); err == nil && stored == identity {
		return nil
	}
	cur, err := b.version.LibraryVersion(ctx)
	if err != nil {
		return err
	}
	if err := b.version.SetLibraryVersion(ctx, cur+1); err != nil {
		return err
	}
	// Bump the per-identity binding epoch so the resolver treats all cached
	// bindings for this identity as stale. Uses the shared resolver.BumpEpoch
	// helper to guarantee the key format matches what the resolver reads.
	if err := resolver.BumpEpoch(ctx, b.queries, identity); err != nil {
		return err
	}
	if err := b.queries.UpsertSetting(ctx, db.UpsertSettingParams{Key: settingLibraryIdentity, Value: identity}); err != nil {
		return err
	}
	// Launch the async pre-warm sweep (piece 3). Fire-and-forget: never blocks
	// or fails reconcile. See schedulePostSwapSweep for the full contract.
	b.schedulePostSwapSweep()
	return nil
}

// schedulePostSwapSweep launches a bounded, best-effort goroutine that reads
// all durable canonical ids (plays + download_jobs) and pre-warms their resolver
// bindings via Resolve. The goroutine:
//   - uses a DETACHED context (context.WithoutCancel + 2-minute timeout) so it
//     outlives the caller's request context and is still bounded in wall-clock time;
//   - caps the total ids fetched to sweepLimit;
//   - caps in-flight Resolve calls to sweepConcurrency via a semaphore channel;
//   - swallows per-id errors and logs a summary if any failed;
//   - never panics on nil resolver or nil provider.
func (b *Builder) schedulePostSwapSweep() {
	if b.resolverProvider == nil {
		return
	}
	res := b.resolverProvider()
	if res == nil {
		return
	}
	// Snapshot the queries pointer so the goroutine doesn't close over b.
	q := b.queries
	go func() {
		// Detached context: severs cancellation from the caller while preserving
		// trace values; bounded by a 2-minute timeout so a slow library can't
		// leave this goroutine running indefinitely.
		sweepCtx, cancel := context.WithTimeout(context.WithoutCancel(context.Background()), 2*time.Minute)
		defer cancel()

		ids, err := q.DistinctDurableCanonicalIDs(sweepCtx, sweepLimit)
		if err != nil {
			log.Printf("wiring: post-swap sweep: list durable ids: %v", err)
			return
		}

		sem := make(chan struct{}, sweepConcurrency)
		var wg sync.WaitGroup
		var (
			mu       sync.Mutex
			errCount int
		)
		for _, id := range ids {
			id := id
			sem <- struct{}{}
			wg.Add(1)
			go func() {
				defer func() { <-sem; wg.Done() }()
				if _, rerr := res.Resolve(sweepCtx, id); rerr != nil {
					mu.Lock()
					errCount++
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		if errCount > 0 {
			log.Printf("wiring: post-swap sweep: %d/%d ids failed to pre-resolve (best-effort, ignored)", errCount, len(ids))
		}
	}()
}

// reconcileDownloadJobIdentity is now a no-op stub (Task 3: cover-rot killer).
// The clear-and-rematch dance (ClearMatchedDownloadJobLibraryRefs) has been retired:
// download jobs now carry a stable canonical_id minted at link time, so covers and
// streams re-resolve lazily through the canonical id after a backend swap without
// needing to clear volatile library refs first. The setting key is preserved so an
// existing stored value does not cause issues on upgrade.
func (b *Builder) reconcileDownloadJobIdentity(ctx context.Context, identity string) error {
	// Persist the identity so future builds can detect no-change quickly (idempotent).
	return b.queries.UpsertSetting(ctx, db.UpsertSettingParams{Key: settingDownloadJobIdentity, Value: identity})
}

// BindingResolver is the narrow catalog-resolution seam exposed to the Builder
// so the download.Manager and playlistsync.Service (both constructed inside Build)
// can hold a reference to the long-lived resolver singleton. *resolver.Service
// satisfies this interface (Go structural typing). Defined here so the composition
// root, tests, and both consumers share one name without any package importing
// another in a cycle-forming direction (resolver never imports wiring/download/sync).
type BindingResolver interface {
	Resolve(ctx context.Context, catalogID string) (resolver.Addressing, error)
	RefreshLinked(ctx context.Context, catalogIDs []string) error
}

// Builder captures everything needed to (re)build a ServiceBundle from the
// current DB state: the registries, the DB queries (adapter rows + match cache +
// download persistence), the version store, the event bus, the clock, and the
// getenv used for secret overrides.
type Builder struct {
	libraryReg    *registry.Registry
	searchReg     *registry.Registry
	downloaderReg *registry.Registry
	queries       *db.Queries
	version       VersionStore
	bus           download.Publisher
	clock         download.Clock
	getenv        func(string) string
	dataDir       string
	// resolverProvider is set by the composition root before the first Build call
	// (P2 construction order: reloader→resolver→SetResolverProvider→Build). Build
	// reads it lazily — it is never called during Build itself, only stored so
	// Manager/Sync can call it at runtime. Nil is safe (Manager/Sync nil-guard it).
	resolverProvider func() BindingResolver
	// canonicalMinter is set by SetCanonicalMinter (Task 5) so BuildSyncService can
	// forward it to playlistsync.Service via WithCanonicalMinter. Nil-safe.
	canonicalMinter playlistsync.CanonicalMinter
}

// SetResolverProvider injects the resolver provider into the Builder. Call this
// BEFORE the first Build so that Manager and Sync receive the resolver dep.
// The provider func is stored and forwarded to download.NewManager and
// playlistsync.NewService; they call it at runtime (Tasks 3-5), not during Build.
func (b *Builder) SetResolverProvider(p func() BindingResolver) {
	b.resolverProvider = p
}

// SetCanonicalMinter injects the catalog minter into the Builder so that
// BuildSyncService can wire it into playlistsync.Service.WithCanonicalMinter.
// Call this BEFORE Build (same pattern as SetResolverProvider). Nil-safe: if
// never called, minting is silently skipped in the sync service.
func (b *Builder) SetCanonicalMinter(m playlistsync.CanonicalMinter) {
	b.canonicalMinter = m
}

// NewBuilder constructs a Builder. clock may be nil (download.NewManager applies
// a RealClock default). dataDir is Reverb's data directory (filepath.Dir of DBPath);
// it is used to derive Navidrome's data directory in built-in mode.
func NewBuilder(
	libraryReg, searchReg, downloaderReg *registry.Registry,
	queries *db.Queries,
	version VersionStore,
	bus download.Publisher,
	clock download.Clock,
	getenv func(string) string,
	dataDir string,
) *Builder {
	return &Builder{
		libraryReg:    libraryReg,
		searchReg:     searchReg,
		downloaderReg: downloaderReg,
		queries:       queries,
		version:       version,
		bus:           bus,
		clock:         clock,
		getenv:        getenv,
		dataDir:       dataDir,
	}
}

// naviBin returns the Navidrome binary path. It honours REVERB_NAVIDROME_BIN;
// otherwise it falls back to "navidrome" (resolved on PATH — the Docker image
// installs it at /usr/local/bin/navidrome).
func (b *Builder) naviBin() string {
	if v := b.getenv("REVERB_NAVIDROME_BIN"); v != "" {
		return v
	}
	return "navidrome"
}

// nowMilli is the coverage cache clock (epoch millis). It honors an injected
// download.Clock so tests that pin time see deterministic fetched_at values,
// falling back to wall-clock time when no clock is configured.
func (b *Builder) nowMilli() int64 {
	if b.clock != nil {
		return b.clock.Now().UnixMilli()
	}
	return time.Now().UnixMilli()
}

// Build reads the current enabled adapter_instance rows and constructs a fresh
// ServiceBundle. It mirrors the composition-root logic: build library → build
// search sources into an aggregator (with a matcher when a library is present) →
// build downloaders into a Manager (only when downloaders AND a library are
// present). It does NOT start the Manager — the caller controls its lifecycle.
func (b *Builder) Build(ctx context.Context) (ServiceBundle, error) {
	instances, err := b.queries.ListAdapterInstances(ctx)
	if err != nil {
		return ServiceBundle{}, err
	}

	var bundle ServiceBundle

	// Resolve effective backend mode and (if built-in) ensure internal creds.
	modeSetting, _ := b.queries.GetSetting(ctx, "library_backend_mode")
	hasLibInst := false
	for i := range instances {
		if instances[i].Type == "library" && instances[i].Enabled == 1 {
			hasLibInst = true
			break
		}
	}
	mode := embedded.ResolveMode(modeSetting, hasLibInst)

	// When the active library backend changes identity (e.g. external Navidrome →
	// bundled), its track IDs are entirely different. Bump library_version so the
	// match cache (which stores per-library track IDs) is invalidated and playlists
	// re-match against the new library — otherwise playback/links use dead IDs.
	if err := b.reconcileLibraryIdentity(ctx, libraryIdentity(mode, instances)); err != nil {
		log.Printf("WARNING: library identity reconcile: %v", err)
	}
	// Persist the current library identity for download jobs (Task 3: the clear-and-
	// rematch dance is retired). Download jobs now carry a stable canonical_id and
	// re-resolve their covers/streams lazily through the resolver after a backend swap,
	// so no volatile-ref clearing is needed here.
	if err := b.reconcileDownloadJobIdentity(ctx, libraryIdentity(mode, instances)); err != nil {
		log.Printf("WARNING: download job identity reconcile: %v", err)
	}

	var creds embedded.Credentials
	if mode == embedded.ModeBuiltIn {
		creds, err = embedded.EnsureInternalCredentials(ctx, b.queries)
		if err != nil {
			return bundle, fmt.Errorf("built-in library credentials: %w", err)
		}
	}
	libAdapter, err := BuildLibraryAdapter(ctx, b.libraryReg, instances, b.getenv, mode, creds)
	if err != nil {
		libAdapter = nil
		log.Printf("WARNING: library adapter not available: %v", err)
	} else if libAdapter == nil {
		log.Printf("no library adapter configured (add one via settings)")
	} else {
		log.Printf("library adapter active: %s", libAdapter.Name())
	}
	bundle.Library = libAdapter

	// Bundled-Navidrome supervisor (no-op when not built-in).
	var naviEnv []string
	if mode == embedded.ModeBuiltIn {
		opts := embedded.DefaultNaviOptions(b.dataDir, embedded.MusicDir(b.getenv), creds.Password)
		naviEnv = embedded.BuildNavidromeEnv(opts)
	}
	// RELOAD-PATH CONTRACT: Build is called both at boot (main.go) AND on every live
	// adapter create/update/delete (via serviceReloader.Reload). The Supervisor
	// constructed here is only Started/Shutdown by main.go at boot; on the live-reload
	// path the returned bundle's Supervisor is intentionally NOT started or swapped into
	// the running process — backend-mode changes are restart-only (matching the
	// "takes effect after a restart" UI copy). Do NOT start this supervisor on the reload
	// path: doing so would exec a SECOND Navidrome on 127.0.0.1:4533, causing a port
	// conflict and a broken resource invariant.
	bundle.Supervisor = embedded.New(embedded.Options{
		Mode:   mode,
		Env:    naviEnv,
		Runner: embedded.ExecRunner(b.naviBin()),
		Probe:  embedded.PingProbe("http://127.0.0.1:4533", nil),
	})

	// One matcher per library adapter, shared by the aggregator, the download
	// Rematcher, AND the resolver singleton (via bundle.Matcher). A single instance
	// keeps all three re-matching against the same backend; on a hot-reload a fresh
	// matcher is built here and republished, so the resolver follows the live adapter.
	var matcher *matching.Service
	if libAdapter != nil {
		matcher = matching.NewService(libAdapter, b.queries, b.version.LibraryVersion)
		bundle.Matcher = matcher
	}

	// Search sources + matcher + aggregator.
	sources := BuildSearchSources(b.searchReg, instances, b.getenv)
	if len(sources) > 0 {
		var aggMatcher search.Matcher
		if matcher != nil {
			aggMatcher = matcher
		}
		bundle.Aggregator = search.NewAggregator(sources, aggMatcher, 8*time.Second)
		log.Printf("search sources active: %d", len(sources))
	} else {
		log.Printf("no search sources configured (add one via settings)")
	}

	// Coverage service (artist/album/coverage pages). Needs a library to match
	// against AND a search source implementing coverage.DiscoSource (spotify does).
	// Nil when either is missing — the API handlers 503 in that case.
	bundle.Coverage = b.BuildCoverageService(sources, libAdapter, b.nowMilli)
	if bundle.Coverage != nil {
		log.Printf("coverage service active")
	}

	// Downloaders → Manager (constructed but not started).
	downloaders := BuildDownloaders(b.downloaderReg, instances, b.getenv)
	if len(downloaders) > 0 && libAdapter != nil {
		// Reuse the single matcher built above (libAdapter != nil guarantees it is
		// non-nil here) — functionally identical to a fresh instance, one fewer alloc.
		var rematcher download.Rematcher = matcher
		// Wrap the wiring-level resolverProvider into a download.BindingResolver
		// provider. Both interface shapes are structurally identical and satisfied
		// by *resolver.Service; the wrapper bridges the two local interface types
		// without any type assertion. Nil when SetResolverProvider was not called.
		var dlResolve func() download.BindingResolver
		if b.resolverProvider != nil {
			rp := b.resolverProvider // capture once
			dlResolve = func() download.BindingResolver {
				r := rp()
				if r == nil {
					return nil
				}
				return r
			}
		}
		bundle.Manager = download.NewManager(
			download.Config{Workers: 2, DebounceWindow: 5 * time.Second},
			downloaders,
			download.NewSQLStore(b.queries),
			b.bus,
			libAdapter, // ScanController (StartScan/ScanStatus)
			rematcher,  // Rematcher
			b.version,  // VersionBumper (LibraryVersion/SetLibraryVersion)
			b.clock,    // production clock (nil → RealClock default)
			libAdapter, // PlaylistAdder (AddTracksToPlaylist) — subsonic adapter satisfies it
			dlResolve,  // optional resolver provider; Tasks 3-5 add call sites
		)
		log.Printf("download manager active: %d downloader(s)", len(downloaders))
	} else if len(downloaders) > 0 {
		log.Printf("WARNING: downloaders configured but no library adapter — download manager disabled")
	} else {
		log.Printf("no downloaders configured (add one via settings)")
	}

	// Playlist-sync service (managed playlists + optional Spotify import/sync).
	// Requires a library and a download Manager; Spotify is optional — when no
	// PlaylistProvider-capable search source is configured, Import/ImportOnce/Sync
	// return ErrSpotifyNotConfigured but all managed-playlist operations work.
	bundle.Sync = b.BuildSyncService(sources, libAdapter, bundle.Manager)
	if bundle.Sync != nil {
		log.Printf("playlist sync service active")
	}

	return bundle, nil
}
