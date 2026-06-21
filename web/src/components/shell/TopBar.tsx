import { useState, useRef, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { IconButton, Button, Icon } from '../ui'
import { useUI } from '../../lib/uiStore'
import { useDownloads } from '../../lib/downloadStore'

export function TopBar() {
  const navigate = useNavigate()
  const togglePanel = useUI((s) => s.togglePanel)
  const activeCount = useDownloads((s) => s.active().length)

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
      {/* Left — history nav */}
      <div className="flex items-center gap-2">
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

      {/* Center — home + search pill + browse */}
      <div className="flex items-center gap-2 flex-1 mx-4 min-w-0">
        <button
          type="button"
          aria-label="Home"
          onClick={() => navigate('/')}
          className={[
            'w-12 h-12 rounded-full bg-raised flex-none',
            'flex items-center justify-center text-text-primary',
            'hover:scale-105 transition-transform',
            'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
          ].join(' ')}
        >
          <Icon name="home" className="w-5 h-5" />
        </button>

        <button
          type="button"
          aria-label="Search"
          onClick={() => navigate('/search')}
          className={[
            'flex items-center gap-3 h-12 px-4 rounded-full bg-input flex-1 min-w-0 max-w-lg',
            'text-text-secondary text-sm font-medium text-left',
            'hover:bg-raised-hover border border-transparent hover:border-border-subtle',
            'transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
          ].join(' ')}
        >
          <Icon name="search" className="w-4 h-4 flex-none" />
          <span className="truncate">Search your library — or everywhere</span>
          <span className="w-px h-6 bg-border-subtle flex-none mx-1" aria-hidden="true" />
          <Icon name="browse" className="w-4 h-4 flex-none" />
        </button>
      </div>

      {/* Right — downloads + avatar */}
      <div className="flex items-center gap-3 flex-none">
        {/* Downloads button with badge */}
        <div className="relative">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => togglePanel('downloads')}
          >
            <span className="flex items-center gap-1.5" aria-label="Downloads">
              <Icon name="dl" className="w-4 h-4" />
              <span>Downloads</span>
            </span>
          </Button>
          {activeCount > 0 && (
            <span
              data-testid="downloads-badge"
              className="absolute -top-1 -right-1 min-w-[15px] h-[15px] px-1 rounded-full bg-accent text-black text-[10px] font-extrabold grid place-items-center pointer-events-none"
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
            aria-haspopup="true"
            aria-expanded={menuOpen}
            onClick={() => setMenuOpen((o) => !o)}
            className={[
              'w-8 h-8 rounded-full bg-gradient-to-br from-purple-600 to-indigo-500',
              'flex items-center justify-center text-white font-extrabold text-sm',
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
