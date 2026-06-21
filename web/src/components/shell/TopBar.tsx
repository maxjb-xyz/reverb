import { useState, useRef, useEffect } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import { IconButton, Button, Icon } from '../ui'
import { useUI } from '../../lib/uiStore'
import { useDownloads } from '../../lib/downloadStore'
import { useSearch } from '../../lib/searchStore'

export function TopBar() {
  const navigate = useNavigate()
  const location = useLocation()
  const togglePanel = useUI((s) => s.togglePanel)
  const activeCount = useDownloads((s) => s.active().length)
  const query = useSearch((s) => s.query)
  const setQuery = useSearch((s) => s.setQuery)

  // Typing routes to /search (once) and keeps the query live there.
  function onSearchChange(value: string) {
    setQuery(value)
    if (location.pathname !== '/search') navigate('/search')
  }

  const [menuOpen, setMenuOpen] = useState(false)
  const menuRef = useRef<HTMLDivElement>(null)

  // Close menu on outside click
  useEffect(() => {
    if (!menuOpen) return
    function handler(e: MouseEvent) {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        setMenuOpen(false)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [menuOpen])

  async function handleLogout() {
    await fetch('/api/v1/auth/logout', { method: 'POST', credentials: 'include' })
    window.location.reload()
  }

  return (
    <header className="flex items-center justify-between px-4 h-16 bg-surface">
      {/* Mobile wordmark — desktop shows the history nav + search instead */}
      <span className="select-none text-lg font-bold tracking-tight text-text-primary md:hidden">
        Reverb<span className="text-accent">.</span>
      </span>

      {/* Left — history nav (desktop only) */}
      <div className="hidden items-center gap-2 md:flex">
        <IconButton
          name="back"
          label="Back"
          onClick={() => window.history.back()}
        />
        <IconButton
          name="fwd"
          label="Forward"
          onClick={() => window.history.forward()}
        />
      </div>

      {/* Center — home + centered search pill (desktop only; mobile uses the
          Search tab in the bottom nav) */}
      <div className="hidden items-center justify-center gap-2 flex-1 mx-4 min-w-0 md:flex">
        <IconButton
          name="home"
          label="Home"
          onClick={() => navigate('/')}
        />

        <div
          className={[
            'flex items-center gap-3 h-12 px-4 rounded-full bg-input w-full min-w-0 max-w-md',
            'border border-transparent focus-within:border-border-subtle',
            'focus-within:ring-2 focus-within:ring-accent transition-colors',
          ].join(' ')}
        >
          <Icon name="search" className="w-4 h-4 flex-none text-text-secondary" />
          <input
            type="text"
            aria-label="Search"
            value={query}
            onChange={(e) => onSearchChange(e.target.value)}
            onFocus={() => { if (location.pathname !== '/search') navigate('/search') }}
            placeholder="Search your library — or everywhere"
            className="w-full min-w-0 bg-transparent text-sm font-medium text-text-primary placeholder:text-text-secondary outline-none"
          />
        </div>
      </div>

      {/* Right — downloads + avatar */}
      <div className="flex items-center gap-3 flex-none">
        {/* Downloads button with badge (desktop only; mobile uses the bottom nav) */}
        <div className="relative hidden md:block">
          <Button
            variant="ghost"
            size="sm"
            aria-label="Downloads"
            onClick={() => togglePanel('downloads')}
          >
            <span className="flex items-center gap-1.5">
              <Icon name="dl" className="w-4 h-4" />
              <span>Downloads</span>
            </span>
          </Button>
          {activeCount > 0 && (
            <span
              data-testid="downloads-badge"
              className="absolute -top-1 -right-1 min-w-4 h-4 px-1 rounded-full bg-accent text-on-accent text-xs font-extrabold grid place-items-center pointer-events-none"
            >
              {activeCount}
            </span>
          )}
        </div>

        {/* Avatar / account menu */}
        <div ref={menuRef} className="relative">
          <button
            type="button"
            aria-label="Account menu"
            aria-haspopup="menu"
            aria-expanded={menuOpen}
            onClick={() => setMenuOpen((o) => !o)}
            className={[
              'w-8 h-8 rounded-full bg-accent',
              'flex items-center justify-center text-on-accent font-extrabold text-sm',
              'hover:scale-105 transition-transform',
              'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
            ].join(' ')}
          >
            R
          </button>

          {menuOpen && (
            <div
              role="menu"
              className={[
                'absolute right-0 top-10 w-40 rounded-lg bg-raised shadow-pop border border-border-subtle',
                'py-1 z-50',
              ].join(' ')}
            >
              <button
                role="menuitem"
                type="button"
                onClick={() => { setMenuOpen(false); navigate('/settings') }}
                className={[
                  'w-full text-left px-4 py-2 text-sm text-text-primary',
                  'hover:bg-raised-hover transition-colors',
                  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-inset',
                ].join(' ')}
              >
                Settings
              </button>
              <button
                role="menuitem"
                type="button"
                onClick={() => { setMenuOpen(false); navigate('/admin') }}
                className={[
                  'w-full text-left px-4 py-2 text-sm text-text-primary',
                  'hover:bg-raised-hover transition-colors',
                  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-inset',
                ].join(' ')}
              >
                Admin
              </button>
              <button
                role="menuitem"
                type="button"
                onClick={handleLogout}
                className={[
                  'w-full text-left px-4 py-2 text-sm text-text-primary',
                  'hover:bg-raised-hover transition-colors',
                  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-inset',
                ].join(' ')}
              >
                Logout
              </button>
            </div>
          )}
        </div>
      </div>
    </header>
  )
}
