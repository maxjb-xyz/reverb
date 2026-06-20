import { Outlet } from 'react-router-dom'
import { Sidebar } from './Sidebar'
import { PlayerBar } from './PlayerBar'
import { PlayQueue } from './PlayQueue'
import { DownloadTray } from './DownloadTray'
import { useRealtime } from '../lib/realtimeWiring'

export function AppShell() {
  // One app-wide realtime WS (distinct from the SSE search stream): drives the
  // download store, TanStack invalidation, and play-when-ready auto-play.
  useRealtime()

  return (
    <div className="flex h-full flex-col">
      <div className="relative flex min-h-0 flex-1">
        <Sidebar />
        <main className="flex-1 overflow-auto p-6">
          <Outlet />
        </main>
        {/* Single right-panel slot, mutually exclusive via useUI.rightPanel:
            'queue' → PlayQueue, 'downloads' → DownloadTray. Each renders null
            when it is not the active panel. */}
        <PlayQueue />
        <DownloadTray />
      </div>
      <PlayerBar />
    </div>
  )
}
