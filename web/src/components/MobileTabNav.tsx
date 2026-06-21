import { NavLink } from 'react-router-dom'
import { useUI } from '../lib/uiStore'
import { useDownloads } from '../lib/downloadStore'

const tabs = [
  { to: '/search', label: 'Search' },
  { to: '/library', label: 'Library' },
  { to: '/settings', label: 'Settings' },
]

const tabClass = 'flex min-h-[44px] min-w-[44px] flex-1 items-center justify-center rounded px-2 py-2 text-sm'

// MobileTabNav is the bottom tab bar shown only < md. Routes are identical to
// desktop; this is purely alternate chrome. Tap targets are ≥44px.
export function MobileTabNav() {
  const togglePanel = useUI((s) => s.togglePanel)
  const rightPanel = useUI((s) => s.rightPanel)
  const activeCount = useDownloads((s) => s.active().length)

  return (
    <nav
      data-testid="mobile-tab-nav"
      className="flex shrink-0 items-stretch gap-1 border-t border-neutral-800 bg-base/95 px-1 py-1 backdrop-blur md:hidden"
    >
      {tabs.map((t) => (
        <NavLink
          key={t.to}
          to={t.to}
          className={({ isActive }) => `${tabClass} ${isActive ? 'text-accent' : 'text-neutral-300'}`}
        >
          {t.label}
        </NavLink>
      ))}
      <button
        type="button"
        onClick={() => togglePanel('downloads')}
        className={`${tabClass} relative ${rightPanel === 'downloads' ? 'text-accent' : 'text-neutral-300'}`}
      >
        Downloads
        {activeCount > 0 && (
          <span className="absolute right-1 top-0 rounded-full bg-accent px-1.5 text-xs text-white">{activeCount}</span>
        )}
      </button>
    </nav>
  )
}
