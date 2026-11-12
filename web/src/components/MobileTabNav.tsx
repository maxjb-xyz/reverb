import { NavLink } from 'react-router-dom'
import { useDownloads } from '../lib/downloadStore'
import { Icon } from './ui/Icon'
import type { IconName } from './ui/Icon'

// Bottom tab bar shown only < md. Four real destinations; Stats, Requests and
// Settings live in the avatar menu. Downloads is a page here, not a slide-over:
// a tab that opened a panel over the current page behaved unlike its siblings.
const tabs: { to: string; label: string; icon: IconName; end?: boolean }[] = [
  { to: '/', label: 'Home', icon: 'home', end: true },
  { to: '/search', label: 'Search', icon: 'search' },
  { to: '/library', label: 'Library', icon: 'browse' },
  { to: '/downloads', label: 'Downloads', icon: 'dl' },
]

export function MobileTabNav() {
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
          className={({ isActive }) => `relative ${itemClass(isActive)}`}
        >
          <Icon name={t.icon} className="text-xl" />
          <span className="text-[10px] font-semibold leading-none">{t.label}</span>
          {t.to === '/downloads' && activeCount > 0 && (
            <span className="pointer-events-none absolute right-2 top-0 grid h-4 min-w-4 place-items-center rounded-full bg-accent px-1 text-xs font-extrabold text-on-accent">
              {activeCount}
            </span>
          )}
        </NavLink>
      ))}
    </nav>
  )
}
