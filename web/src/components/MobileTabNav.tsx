import { NavLink } from 'react-router-dom'
import { useUI } from '../lib/uiStore'
import { useDownloads } from '../lib/downloadStore'
import { Icon } from './ui/Icon'

const tabs = [
  { to: '/search', label: 'Search' },
  { to: '/library', label: 'Library' },
  { to: '/settings', label: 'Settings' },
]

// MobileTabNav is the bottom tab bar shown only < md. Routes are identical to
// desktop; this is purely alternate chrome. Tap targets are ≥44px.
export function MobileTabNav() {
  const togglePanel = useUI((s) => s.togglePanel)
  const rightPanel = useUI((s) => s.rightPanel)
  const activeCount = useDownloads((s) => s.active().length)

  return (
    <nav
      data-testid="mobile-tab-nav"
      className="flex shrink-0 items-stretch gap-1 border-t border-border-subtle bg-surface/95 px-1 py-1 backdrop-blur md:hidden"
    >
      {tabs.map((t) => (
        <NavLink
          key={t.to}
          to={t.to}
          aria-label={t.label}
          className={({ isActive }) =>
            [
              'flex min-h-[44px] min-w-[44px] flex-1 items-center justify-center rounded px-2 py-2 text-sm font-semibold',
              'transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
              isActive ? 'text-accent' : 'text-text-secondary hover:text-text-primary',
            ].join(' ')
          }
        >
          {t.label}
        </NavLink>
      ))}

      <button
        type="button"
        aria-label="Downloads"
        onClick={() => togglePanel('downloads')}
        className={[
          'relative flex min-h-[44px] min-w-[44px] flex-1 items-center justify-center rounded px-2 py-2 text-sm font-semibold',
          'transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
          rightPanel === 'downloads' ? 'text-accent' : 'text-text-secondary hover:text-text-primary',
        ].join(' ')}
      >
        <Icon name="dl" className="w-5 h-5" />
        {activeCount > 0 && (
          <span className="absolute right-1 top-0 min-w-4 h-4 px-1 rounded-full bg-accent text-on-accent text-xs font-extrabold grid place-items-center pointer-events-none">
            {activeCount}
          </span>
        )}
      </button>
    </nav>
  )
}
