import { NavLink } from 'react-router-dom'
import { useUI } from '../lib/uiStore'
import { useDownloads } from '../lib/downloadStore'

const items = [
  { to: '/search', label: 'Search' },
  { to: '/library', label: 'Library' },
  { to: '/settings', label: 'Settings' },
]

export function Sidebar() {
  const togglePanel = useUI((s) => s.togglePanel)
  const rightPanel = useUI((s) => s.rightPanel)
  const activeCount = useDownloads((s) => s.active().length)

  return (
    <nav className="w-56 shrink-0 border-r border-neutral-800 p-4 space-y-1">
      <div className="text-xl font-bold mb-4 text-accent">Crate</div>
      {items.map((i) => (
        <NavLink
          key={i.to}
          to={i.to}
          className={({ isActive }) =>
            `block rounded px-3 py-2 ${isActive ? 'bg-accent/20 text-accent' : 'hover:bg-neutral-800'}`
          }
        >
          {i.label}
        </NavLink>
      ))}
      <button
        type="button"
        onClick={() => togglePanel('downloads')}
        className={`mt-2 flex w-full items-center justify-between rounded px-3 py-2 text-left ${
          rightPanel === 'downloads' ? 'bg-accent/20 text-accent' : 'hover:bg-neutral-800'
        }`}
      >
        <span>⟳ Downloads</span>
        {activeCount > 0 && (
          <span className="rounded-full bg-accent px-1.5 text-xs text-white">{activeCount}</span>
        )}
      </button>
    </nav>
  )
}
