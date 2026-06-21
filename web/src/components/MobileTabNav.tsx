import { NavLink } from 'react-router-dom'
import { useUI } from '../lib/uiStore'
import { useDownloads } from '../lib/downloadStore'
import { Icon } from './ui/Icon'
import type { IconName } from './ui/Icon'

// Bottom tab bar shown only < md. Icon-first (Settings lives in the avatar menu).
const tabs: { to: string; label: string; icon: IconName; end?: boolean }[] = [
  { to: '/', label: 'Home', icon: 'home', end: true },
  { to: '/search', label: 'Search', icon: 'search' },
  { to: '/library', label: 'Library', icon: 'browse' },
]

export function MobileTabNav() {
  const togglePanel = useUI((s) => s.togglePanel)
  const rightPanel = useUI((s) => s.rightPanel)
  const activeCount = useDownloads((s) => s.active().length)

  const itemClass = (active: boolean) =>
    [
      'flex min-h-[48px] min-w-[44px] flex-1 flex-col items-center justify-center gap-0.5 rounded px-1 py-1',
      'transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
      active ? 'text-accent' : 'text-text-secondary hover:text-text-primary',
    ].join(' ')

  return (
    <nav
      data-testid="mobile-tab-nav"
      className="flex shrink-0 items-stretch gap-1 border-t border-border-subtle bg-surface/95 px-1 py-1 backdrop-blur md:hidden"
    >
      {tabs.map((t) => (
        <NavLink
          key={t.to}
          to={t.to}
          end={t.end}
          aria-label={t.label}
          className={({ isActive }) => itemClass(isActive)}
        >
          <Icon name={t.icon} className="text-xl" />
          <span className="text-[10px] font-semibold leading-none">{t.label}</span>
        </NavLink>
      ))}

      <button
        type="button"
        aria-label="Downloads"
        onClick={() => togglePanel('downloads')}
        className={`relative ${itemClass(rightPanel === 'downloads')}`}
      >
        <Icon name="dl" className="text-xl" />
        <span className="text-[10px] font-semibold leading-none">Downloads</span>
        {activeCount > 0 && (
          <span className="absolute right-2 top-0 grid h-4 min-w-4 place-items-center rounded-full bg-accent px-1 text-xs font-extrabold text-on-accent pointer-events-none">
            {activeCount}
          </span>
        )}
      </button>
    </nav>
  )
}
