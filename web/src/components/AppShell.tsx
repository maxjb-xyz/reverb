import { Suspense, useEffect } from 'react'
import { Outlet } from 'react-router-dom'
import { TopBar } from './shell/TopBar'
import { LibraryRail } from './shell/LibraryRail'
import { PlayerBar } from './shell/PlayerBar'
import { NowPlayingPanel } from './shell/NowPlayingPanel'
import { DownloadTray } from './DownloadTray'
import { MobileTabNav } from './MobileTabNav'
import { MiniPlayer } from './MiniPlayer'
import { NowPlayingOverlay } from './NowPlayingOverlay'
import { Toaster } from './ui/Toaster'
import { Skeleton } from './ui/Skeleton'
import { useRealtime } from '../lib/realtimeWiring'
import { usePlayer, engine } from '../lib/playerStore'
import { useUI } from '../lib/uiStore'
import { useAlbumPalette } from '../lib/useAlbumPalette'
import { trackCoverUrl } from '../lib/libraryApi'
import { rgbToCss } from '../lib/palette'
import { startPlayTracker } from '../lib/playTracker'
import { startNowPlaying } from '../lib/nowPlaying'
import { startMediaSession } from '../lib/mediaSession'

function RouteLoading() {
  return (
    <div className="space-y-6" aria-label="Loading page" aria-busy="true">
      <div className="space-y-2">
        <Skeleton className="h-7 w-40" />
        <Skeleton className="h-4 w-64 max-w-full" />
      </div>
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-5">
        {Array.from({ length: 5 }).map((_, index) => (
          <div key={index} className="space-y-2">
            <Skeleton className="aspect-square w-full" />
            <Skeleton className="h-4 w-3/4" />
            <Skeleton className="h-3 w-1/2" />
          </div>
        ))}
      </div>
    </div>
  )
}

export function AppShell() {
  // One app-wide realtime WS (distinct from the SSE search stream): drives the
  // download store, TanStack invalidation, and play-when-ready auto-play.
  useRealtime()

  // One app-wide play tracker: scores qualified plays off the audio engine and
  // POSTs them to /api/v1/plays.  Starts once when AppShell mounts and cleans
  // up (unsubscribes) when it unmounts.
  useEffect(() => startPlayTracker(engine), [])
  useEffect(() => startNowPlaying(engine), [])
  useEffect(() => startMediaSession(engine), [])

  const current = usePlayer((s) => s.current)
  const rightPanel = useUI((s) => s.rightPanel)
  const palette = useAlbumPalette(current ? trackCoverUrl(current, 80) : undefined)

  // Ambient dynamic background: a subtle gradient from the dominant color into
  // the token base. When no palette, leave the static dark base. NOT blur-over-art.
  const ambient = palette
    ? {
        background: `radial-gradient(120% 120% at 50% 0%, ${rgbToCss(palette.rgb, 0.22)} 0%, var(--bg-base) 60%)`,
      }
    : undefined

  // Whether the right column is open (a desktop column or an overlay at mid-widths)
  const rightOpen = rightPanel === 'nowplaying' || rightPanel === 'downloads'

  return (
    <div
      data-testid="app-shell-root"
      className="grid h-full grid-cols-[minmax(0,1fr)] grid-rows-[64px_minmax(0,1fr)_auto] bg-canvas gap-2 p-2"
      style={ambient}
    >
      {/* ── Row 1: TopBar ────────────────────────────────────────────── */}
      <TopBar />

      {/* ── Row 2: Middle three-pane ─────────────────────────────────── */}
      {/*
        Desktop ≥1200px: three columns [LibraryRail | main | RightPanel]
        Mid 900–1200px:  two columns [LibraryRail | main], right panel overlays
        Mobile <md:      single column, LibraryRail hidden
      */}
      <div
        className={[
          'relative flex min-h-0 gap-2',
          // LibraryRail is always rendered; hidden on mobile via its own class.
        ].join(' ')}
      >
        {/* Left rail — hidden on mobile */}
        <div className="hidden md:block md:w-64 xl:w-80 flex-none min-h-0">
          <LibraryRail />
        </div>

        {/* Center main content — bg-surface rounded card that scrolls */}
        <main className="flex-1 min-h-0 overflow-auto bg-surface rounded-lg relative">
          <div className="px-4 py-5 sm:px-6 sm:py-6 lg:px-8">
            <Suspense fallback={<RouteLoading />}>
              <Outlet />
            </Suspense>
          </div>
        </main>

        {/* Right column — desktop: occupies a column at xl+; mid: overlay; absent when null */}
        {rightOpen && (
          <div
            data-testid="right-panel-column"
            className={[
              // At xl+ (≥1200px): static column
              'xl:relative xl:inset-auto xl:z-auto xl:block xl:w-80 xl:flex-none xl:min-h-0',
              // Below xl (900–1200px): fixed overlay, positioned over main content
              'absolute inset-y-0 right-0 z-20 w-80',
              'animate-slide-in-right',
            ].join(' ')}
          >
            {rightPanel === 'nowplaying' && <NowPlayingPanel />}
            {rightPanel === 'downloads' && <DownloadTray />}
          </div>
        )}
      </div>

      {/* ── Row 3: PlayerBar (desktop only) ──────────────────────────── */}
      <PlayerBar />

      {/* ── Mobile chrome (< md): MiniPlayer + MobileTabNav ─────────── */}
      {/* MiniPlayer sits above the tab nav; NowPlayingOverlay is full-screen. */}
      <MiniPlayer />
      <MobileTabNav />
      <NowPlayingOverlay />
      <Toaster />
    </div>
  )
}
