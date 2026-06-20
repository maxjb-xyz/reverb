import { Outlet } from 'react-router-dom'
import { Sidebar } from './Sidebar'
import { PlayerBar } from './PlayerBar'
import { PlayQueue } from './PlayQueue'

export function AppShell() {
  return (
    <div className="flex h-full flex-col">
      <div className="relative flex min-h-0 flex-1">
        <Sidebar />
        <main className="flex-1 overflow-auto p-6">
          <Outlet />
        </main>
        {/* Single right-panel slot. M1: PlayQueue. M3 adds DownloadTray here,
            mutually exclusive via useUI.rightPanel. */}
        <PlayQueue />
      </div>
      <PlayerBar />
    </div>
  )
}
