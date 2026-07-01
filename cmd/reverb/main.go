package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/maxjb-xyz/reverb/internal/api"
	"github.com/maxjb-xyz/reverb/internal/auth"
	"github.com/maxjb-xyz/reverb/internal/catalog"
	"github.com/maxjb-xyz/reverb/internal/config"
	"github.com/maxjb-xyz/reverb/internal/download"
	"github.com/maxjb-xyz/reverb/internal/download/lidarr"
	"github.com/maxjb-xyz/reverb/internal/download/spotdl"
	"github.com/maxjb-xyz/reverb/internal/events"
	"github.com/maxjb-xyz/reverb/internal/library/embedded"
	"github.com/maxjb-xyz/reverb/internal/library/subsonic"
	"github.com/maxjb-xyz/reverb/internal/notification"
	"github.com/maxjb-xyz/reverb/internal/play"
	"github.com/maxjb-xyz/reverb/internal/playlistsync"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/request"
	"github.com/maxjb-xyz/reverb/internal/resolver"
	"github.com/maxjb-xyz/reverb/internal/scrobble"
	"github.com/maxjb-xyz/reverb/internal/scrobble/lastfm"
	"github.com/maxjb-xyz/reverb/internal/search/spotify"
	"github.com/maxjb-xyz/reverb/internal/store"
	"github.com/maxjb-xyz/reverb/internal/store/db"
	"github.com/maxjb-xyz/reverb/internal/wiring"
)

func main() {
	log.Printf("reverb %s starting", version)

	// Root context cancelled when main returns, so background goroutines (e.g. the
	// playlist-sync scheduler) shut down with the process.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, err := config.Load(os.Args[1:], os.Getenv)
	if err != nil {
		log.Fatal(err)
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		log.Fatal(err)
	}

	authSvc := auth.NewService(st.Q(), time.Now)
	// Bootstrap an owner from REVERB_ADMIN_PASSWORD when no users exist yet: stash
	// the hash in the legacy admin_password_hash setting so EnsureSeed below migrates
	// it into the "admin" owner account (single, idempotent code path).
	if cfg.AdminPassword != "" {
		if req, _ := authSvc.IsSetupRequired(context.Background()); req {
			if h, err := auth.HashPassword(cfg.AdminPassword); err == nil {
				_ = st.Q().UpsertSetting(context.Background(), db.UpsertSettingParams{Key: "admin_password_hash", Value: h})
			}
		}
	}
	if cfg.AuthDisabled {
		log.Printf("WARNING: REVERB_AUTH_DISABLED is no longer supported — auth is always enforced; ignoring")
	}
	// Seed system roles + registration-policy defaults, and migrate a legacy
	// single-admin install into an owner account. Idempotent; fail loudly on error.
	if err := authSvc.EnsureSeed(context.Background()); err != nil {
		log.Fatalf("seed identity defaults: %v", err)
	}

	// spotDL is bundled with the image, so present it as a configured downloader
	// out of the box (no manual setup) when none exists yet.
	seedBundledDownloader(context.Background(), st.Q(), os.Getenv)

	// Registries (explicit registration at the composition root — no init() side-effects).
	libraryReg := registry.NewRegistry("library")
	libraryReg.Register("subsonic", func() registry.Plugin { return subsonic.New() })
	searchReg := registry.NewRegistry("search")
	searchReg.Register("spotify", func() registry.Plugin { return spotify.New() })
	downloaderReg := registry.NewRegistry("downloader")
	downloaderReg.Register("spotdl", func() registry.Plugin { return spotdl.New() })
	downloaderReg.Register("lidarr", func() registry.Plugin { return lidarr.New() })
	// Surface the async capability to the admin UI (/adapters/available).
	registry.RegisterCapability("async", func(p registry.Plugin) bool {
		_, ok := p.(download.AsyncDownloader)
		return ok
	})
	// grain:album removed: granularity info is now exposed directly on the adapter
	// instance DTO via SupportedGranularities and Granularities fields.

	// EventBus backs both the WS endpoint and the Manager's typed events.
	bus := events.New()

	// Request system: service + tracker. The tracker subscribes to the stable bus
	// (survives download-manager reloads). Start before any API traffic.
	reqSvc := request.NewService(st.Q(), bus, time.Now)
	tracker := request.NewTracker(reqSvc, bus)
	tracker.Start(ctx)
	defer tracker.Stop()

	// Notification system: service + notifier. The notifier listens on the same bus
	// for request lifecycle events and fans out in-app notifications.
	notifSvc := notification.NewService(st.Q(), time.Now)
	notifier := notification.NewNotifier(bus, notifSvc, &authManagerLister{svc: authSvc})
	notifier.Start(ctx)
	defer notifier.Stop()

	dirty := &atomicDirty{}

	// The Builder constructs the active library/search/download services from the
	// current enabled adapter_instance rows. It is used for the initial build here
	// and reused by the API server to rebuild live on any adapter mutation.
	builder := wiring.NewBuilder(
		libraryReg, searchReg, downloaderReg,
		st.Q(), st, bus, download.RealClock{}, os.Getenv,
		filepath.Dir(cfg.DBPath),
	)

	// P2 construction order: reloader → resolver → SetResolverProvider → Build.
	//
	// The reloader owns the live-matcher atomic holder. It is created BEFORE Build
	// so we can construct the resolver singleton against it (the provider reads the
	// holder per-resolve; the holder is empty until publishMatcher below, which is
	// fine — live services only call Resolve at runtime, after the matcher is up).
	//
	// The resolverProvider func is set on the Builder BEFORE the first Build call so
	// that download.Manager and playlistsync.Service (both constructed inside Build)
	// receive the resolver dep. They do NOT call it during Build — Tasks 3-5 add the
	// actual Resolve/RefreshLinked call sites. The matcher-provider seam is unchanged:
	// the resolver still reads s.matcher() per-resolve for hot-reload correctness.
	reloader := newServiceReloader(builder)
	resolverSvc := resolver.NewService(st.Q(), reloader.matcherProvider(), time.Now)
	builder.SetResolverProvider(func() wiring.BindingResolver { return resolverSvc })

	// P2 Task 3: catalogSvc is backend-independent (st.Q() + time + uuid only) so it
	// is constructed BEFORE builder.Build and injected into the Manager via
	// SetCanonicalMinter AFTER Build returns the Manager. catalogSvc is used as both
	// the download.CanonicalMinter and the play/stats service dependency below.
	catalogSvc := catalog.NewService(st.Q(), time.Now, uuid.NewString)

	bundle, err := builder.Build(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	// Publish the boot matcher (may be nil when no library is configured) into the
	// live-matcher holder so the resolver singleton sees it on its first call.
	// Matches the pre-P2 behaviour (publishMatcher was called here before the reorder).
	reloader.publishMatcher(bundle.Matcher)

	// Start the bundled-library supervisor (no-op in external mode).
	if bundle.Supervisor != nil {
		bundle.Supervisor.Start()
	}

	if bundle.Manager != nil {
		// Task 3: inject the canonical minter so the Manager mints stable catalog ids
		// at link time (BackfillUnlinked / runScan). Nil-safe if catalogSvc is nil.
		bundle.Manager.SetCanonicalMinter(catalogSvc)
		bundle.Manager.Start()
		defer bundle.Manager.Stop()
	}

	// Re-run the backfill after the bundled Navidrome reports ready so that the
	// boot-race (backfill at Start() fires before Navidrome is serving) is healed.
	if bundle.Supervisor != nil && bundle.Manager != nil {
		go waitReadyThenBackfill(ctx, bundle.Supervisor.Ready, bundle.Manager.BackfillUnlinked)
	}

	// Start the playlist-sync scheduler when a sync service is configured. It ticks
	// every 15 minutes, syncing due playlists, and stops when ctx is cancelled.
	if bundle.Sync != nil {
		go playlistsync.NewScheduler(bundle.Sync, 15*time.Minute).Run(ctx)
		// One-time migration: copy existing Navidrome playlists into managed playlists.
		// Runs in the background so startup is not blocked; guarded by a settings flag.
		go func() {
			if err := bundle.Sync.MigrateLibraryPlaylists(ctx); err != nil {
				log.Printf("WARNING: library playlist migration: %v", err)
			}
		}()
	}

	// catalogSvc is already constructed above (before Build) and injected into the Manager.
	playSvc := play.NewService(st.Q(), catalogSvc, time.Now, uuid.NewString)
	statsSvc := play.NewStats(st.Q())

	// Scrobbling: cfg() reads app key/secret from settings on every call so that
	// admin changes (Task 5 UI) take effect without a restart.
	scrobbleCfg := func() scrobble.Creds {
		ctx := context.Background()
		key, _ := st.Q().GetSetting(ctx, "scrobble:lastfm:api_key")
		secret, _ := st.Q().GetSetting(ctx, "scrobble:lastfm:api_secret")
		return scrobble.Creds{APIKey: key, APISecret: secret}
	}
	scrobbleSvc := scrobble.NewService(st.Q(), lastfm.New(), scrobbleCfg, time.Now, uuid.NewString)
	go scrobbleSvc.RunWorker(ctx, 30*time.Second)

	deps := api.Deps{
		Auth:          authSvc,
		Library:       bundle.Library,
		Lib:           libraryReg,
		Search:        searchReg,
		Downloader:    downloaderReg,
		Adapters:      st.Q(),
		PlaylistOwner: st.Q(),
		Events:        bus,
		ConfigDirty:   dirty,
		Reload:        reloader,
		Dev:           cfg.Dev,
		Version:       version,
		DataDir:       filepath.Dir(cfg.DBPath),
		Requests:      reqSvc,
		Notifications: notifSvc,
		Resolver:      resolverSvc,
		Play:          playSvc,
		Stats:         statsSvc,
		Scrobble:      scrobbleSvc,
	}
	// Guard against the "non-nil interface wrapping a nil pointer" trap: only set
	// the interface fields when the concrete service is actually present.
	if bundle.Aggregator != nil {
		deps.SearchAggregator = bundle.Aggregator
	}
	if bundle.Coverage != nil {
		deps.Coverage = bundle.Coverage
	}
	if bundle.Manager != nil {
		deps.Downloads = bundle.Manager
	}
	if bundle.Sync != nil {
		deps.Sync = bundle.Sync
	}
	if bundle.Supervisor != nil {
		sup := bundle.Supervisor
		// LibraryStatus closure and supervisor are boot-bound: backend-mode changes are
		// restart-only, so bundle is immutable after wiring. The unsynchronised
		// bundle.Library read below is safe — the bundle is never mutated post-boot.
		deps.LibraryStatus = func() (string, string) {
			h := sup.Health()
			if h == embedded.HealthExternal {
				if bundle.Library != nil {
					return "external", "ready"
				}
				return "external", "unconfigured"
			}
			return "built-in", string(h)
		}
	}
	srv := api.NewServer(deps)

	addr := fmt.Sprintf(":%d", cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("reverb listening on %s (dev=%v)", addr, cfg.Dev)

	stop := make(chan struct{})
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sig; close(stop) }()

	httpSrv := &http.Server{Handler: srv.Handler()}
	if err := serveWithShutdown(httpSrv, ln, stop, func(ctx context.Context) error {
		if bundle.Supervisor != nil {
			return bundle.Supervisor.Shutdown(ctx)
		}
		return nil
	}); err != nil {
		log.Fatal(err)
	}
}

// authManagerLister adapts auth.Service to notification.AuthLister.
// It enumerates users whose role carries the manage_requests capability.
type authManagerLister struct {
	svc *auth.Service
}

func (a *authManagerLister) ListManagerIDs(ctx context.Context) ([]string, error) {
	roles, err := a.svc.ListRoles(ctx)
	if err != nil {
		return nil, err
	}
	managerRoles := make(map[string]bool)
	for _, r := range roles {
		for _, cap := range r.Capabilities {
			if cap == auth.CapManageRequests {
				managerRoles[r.ID] = true
				break
			}
		}
	}
	users, err := a.svc.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, u := range users {
		if !u.Disabled && managerRoles[u.RoleID] {
			ids = append(ids, u.ID)
		}
	}
	return ids, nil
}
