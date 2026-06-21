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
	"time"

	"github.com/maxjb-xyz/reverb/internal/download"
	"github.com/maxjb-xyz/reverb/internal/library"
	"github.com/maxjb-xyz/reverb/internal/matching"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/search"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

// BuildLibraryAdapter builds the active LibraryAdapter from the first enabled
// adapter_instance of type "library". It applies env secret overrides
// (REVERB_LIBRARY_PASSWORD) onto the stored config_json before Init. The library
// is optional: with no enabled library instance it returns (nil, nil).
func BuildLibraryAdapter(
	ctx context.Context,
	reg *registry.Registry,
	instances []db.AdapterInstance,
	getenv func(string) string,
) (library.LibraryAdapter, error) {
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
		return nil, fmt.Errorf("adapter %q is not a LibraryAdapter", inst.Name)
	}

	cfg := map[string]any{}
	if inst.ConfigJson != "" {
		if err := json.Unmarshal([]byte(inst.ConfigJson), &cfg); err != nil {
			return nil, fmt.Errorf("library adapter %q config: %w", inst.Name, err)
		}
	}
	// Env secret override — env wins for the password just before Init().
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
// already in fallback-chain order. Per-source failures warn-and-skip.
func BuildDownloaders(reg *registry.Registry, instances []db.AdapterInstance, getenv func(string) string) []download.Downloader {
	out := []download.Downloader{}
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
		}

		if err := dl.Init(cfg); err != nil {
			log.Printf("WARNING: downloader %q init failed: %v — skipping", inst.Name, err)
			continue
		}
		out = append(out, dl)
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
			if dl := buildDefaultSpotdl(reg, dir, getenv); dl != nil {
				out = append(out, dl)
			}
		}
	}
	return out
}

// buildDefaultSpotdl constructs the bundled spotDL downloader (output_dir=dir).
// Returns nil (with a log line) if spotDL can't be created/initialised, e.g. a
// build/registry without it — never fatal.
func buildDefaultSpotdl(reg *registry.Registry, dir string, getenv func(string) string) download.Downloader {
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
	if err := dl.Init(cfg); err != nil {
		log.Printf("bundled spotdl downloader unavailable: %v", err)
		return nil
	}
	log.Printf("using bundled spotdl downloader (output_dir=%s)", dir)
	return dl
}

// ServiceBundle is the set of active services built from the current enabled
// adapter_instance rows. Any field may be nil when nothing is configured. The
// Manager is constructed but NOT started — the caller controls its lifecycle.
type ServiceBundle struct {
	Library    library.LibraryAdapter // may be nil
	Aggregator *search.Aggregator     // may be nil
	Manager    *download.Manager      // may be nil; NOT started yet
}

// VersionStore is the library_version reader/writer the Manager + matcher need.
// *store.Store satisfies it.
type VersionStore interface {
	LibraryVersion(ctx context.Context) (int64, error)
	SetLibraryVersion(ctx context.Context, v int64) error
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
}

// NewBuilder constructs a Builder. clock may be nil (download.NewManager applies
// a RealClock default).
func NewBuilder(
	libraryReg, searchReg, downloaderReg *registry.Registry,
	queries *db.Queries,
	version VersionStore,
	bus download.Publisher,
	clock download.Clock,
	getenv func(string) string,
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
	}
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

	libAdapter, err := BuildLibraryAdapter(ctx, b.libraryReg, instances, b.getenv)
	if err != nil {
		libAdapter = nil
		log.Printf("WARNING: library adapter not available: %v", err)
	} else if libAdapter == nil {
		log.Printf("no library adapter configured (add one via settings)")
	} else {
		log.Printf("library adapter active: %s", libAdapter.Name())
	}
	bundle.Library = libAdapter

	// Search sources + matcher + aggregator.
	sources := BuildSearchSources(b.searchReg, instances, b.getenv)
	if len(sources) > 0 {
		var matcher search.Matcher
		if libAdapter != nil {
			matcher = matching.NewService(libAdapter, b.queries, b.version.LibraryVersion)
		}
		bundle.Aggregator = search.NewAggregator(sources, matcher, 8*time.Second)
		log.Printf("search sources active: %d", len(sources))
	} else {
		log.Printf("no search sources configured (add one via settings)")
	}

	// Downloaders → Manager (constructed but not started).
	downloaders := BuildDownloaders(b.downloaderReg, instances, b.getenv)
	if len(downloaders) > 0 && libAdapter != nil {
		var rematcher download.Rematcher = matching.NewService(libAdapter, b.queries, b.version.LibraryVersion)
		bundle.Manager = download.NewManager(
			download.Config{Workers: 2, DebounceWindow: 5 * time.Second},
			downloaders,
			download.NewSQLStore(b.queries),
			b.bus,
			libAdapter, // ScanController (StartScan/ScanStatus)
			rematcher,  // Rematcher
			b.version,  // VersionBumper (LibraryVersion/SetLibraryVersion)
			b.clock,    // production clock (nil → RealClock default)
		)
		log.Printf("download manager active: %d downloader(s)", len(downloaders))
	} else if len(downloaders) > 0 {
		log.Printf("WARNING: downloaders configured but no library adapter — download manager disabled")
	} else {
		log.Printf("no downloaders configured (add one via settings)")
	}

	return bundle, nil
}
