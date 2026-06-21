import { Outlet } from 'react-router-dom'
import { Sidebar } from './Sidebar'
import { PlayerBar } from './PlayerBar'
import { PlayQueue } from './PlayQueue'
import { DownloadTray } from './DownloadTray'
import { MobileTabNav } from './MobileTabNav'
import { MiniPlayer } from './MiniPlayer'
import { NowPlayingOverlay } from './NowPlayingOverlay'
import { useRealtime } from '../lib/realtimeWiring'
import { usePlayer } from '../lib/playerStore'
import { useAlbumPalette } from '../lib/useAlbumPalette'
import { coverUrl } from '../lib/libraryApi'
import { rgbToCss } from '../lib/palette'

export function AppShell() {
  // One app-wide realtime WS (distinct from the SSE search stream): drives the
  // download store, TanStack invalidation, and play-when-ready auto-play.
  useRealtime()

  const current = usePlayer((s) => s.current)
  const palette = useAlbumPalette(current?.coverArtId ? coverUrl(current.coverArtId, 80) : undefined)

  // Ambient dynamic background: a subtle gradient from the dominant color into the
  // static base. When no palette (setting off / nothing playing / not yet resolved),
  // leave the body's static dark base. NOT blur-over-art.
  const ambient = palette
    ? {
        background: `radial-gradient(120% 120% at 50% 0%, ${rgbToCss(palette.rgb, 0.22)} 0%, rgb(13 13 15) 60%)`,
      }
    : undefined

  return (
    <div data-testid="app-shell-root" className="flex h-full flex-col" style={ambient}>
      <div className="relative flex min-h-0 flex-1">
        {/* Desktop sidebar — hidden on mobile (the bottom tab nav replaces it). */}
        <Sidebar />
        <main className="flex-1 overflow-auto p-6 pb-24 md:pb-6">
          <Outlet />
        </main>
        {/* Single right-panel slot: side slide-over on desktop, full-screen sheet
            on mobile (the components apply the responsive classes themselves). */}
        <PlayQueue />
        <DownloadTray />
      </div>

      {/* Desktop bottom player bar (hidden < md from PlayerBar's own classes). */}
      <PlayerBar />

      {/* Mobile chrome: mini player + bottom tab nav, both hidden ≥ md. The
          fullscreen now-playing overlay is portal-free and self-gates on open. */}
      <MiniPlayer />
      <MobileTabNav />
      <NowPlayingOverlay />
    </div>
  )
}
