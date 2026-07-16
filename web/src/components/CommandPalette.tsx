import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { baseCommands, matchCommands } from '../lib/commands'
import { isManagerCaps, useAuthStore } from '../lib/authStore'
import { usePlayer } from '../lib/playerStore'
import { useSearch } from '../lib/searchStore'
import { useUI } from '../lib/uiStore'
import { Icon } from './ui/Icon'

export function CommandPalette() {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [active, setActive] = useState(0)
  const input = useRef<HTMLInputElement>(null)

  const navigate = useNavigate()
  const setSearch = useSearch((s) => s.setQuery)

  // Select actions individually for stable references (no ticking from playerStore).
  const toggle = usePlayer((s) => s.toggle)
  const next = usePlayer((s) => s.next)
  const prev = usePlayer((s) => s.prev)
  const toggleShuffle = usePlayer((s) => s.toggleShuffle)
  const cycleRepeat = usePlayer((s) => s.cycleRepeat)

  // UI actions.
  const togglePanel = useUI((s) => s.togglePanel)
  const openCinema = useUI((s) => s.openCinema)

  // Reactive manager check.
  const isManager = useAuthStore((s) => isManagerCaps(s.me?.capabilities))

  const commands = matchCommands(query, baseCommands(isManager))

  useEffect(() => {
    if (open) requestAnimationFrame(() => input.current?.focus())
  }, [open])

  // Reset the highlighted row whenever the query changes, so a narrowed
  // result list doesn't leave `active` pointing past the visible commands.
  useEffect(() => {
    setActive(0)
  }, [query])

  // The ⌘K toggle listens globally, always-on (it's how the palette opens).
  useEffect(() => {
    function handleToggle(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setActive(0)
        setOpen((v) => !v)
      }
    }
    window.addEventListener('keydown', handleToggle)
    return () => window.removeEventListener('keydown', handleToggle)
  }, [])

  // Escape-to-close only matters — and should only be registered — while open.
  useEffect(() => {
    if (!open) return
    function handleEscape(e: KeyboardEvent) {
      if (e.key === 'Escape') setOpen(false)
    }
    window.addEventListener('keydown', handleEscape)
    return () => window.removeEventListener('keydown', handleEscape)
  }, [open])

  function run(index: number) {
    const command = commands[index]
    if (command) {
      const player = { toggle, next, prev, toggleShuffle, cycleRepeat }
      const ui = { togglePanel, openCinema }
      command.run({ navigate, player, ui })
    } else if (query.trim()) {
      setSearch(query)
      navigate('/search')
    }
    setOpen(false)
  }

  if (!open) return null

  return (
    <div
      className="fixed inset-0 z-50 bg-black/60"
      onClick={() => setOpen(false)}
    >
      <div
        role="dialog"
        aria-label="Command palette"
        onClick={(e) => e.stopPropagation()}
        className={[
          'mx-auto mt-[15vh] w-full max-w-lg rounded-lg',
          'border border-border-subtle bg-raised shadow-pop animate-scale-in',
        ].join(' ')}
      >
        <input
          ref={input}
          aria-label="Type a command or search"
          placeholder="Type a command or search…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'ArrowDown') {
              e.preventDefault()
              setActive((i) => Math.min(i + 1, Math.max(0, commands.length - 1)))
            } else if (e.key === 'ArrowUp') {
              e.preventDefault()
              setActive((i) => Math.max(0, i - 1))
            } else if (e.key === 'Enter') {
              run(active)
            }
          }}
          className={[
            'w-full border-b border-border-subtle bg-transparent p-4',
            'text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
          ].join(' ')}
        />

        <div role="listbox" className="p-2">
          {commands.map((command, index) => (
            <button
              key={command.id}
              role="option"
              aria-selected={index === active}
              type="button"
              onClick={() => run(index)}
              className={[
                'flex w-full items-center gap-3 rounded p-3 text-left',
                'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
                index === active
                  ? 'bg-raised-hover text-text-primary'
                  : 'text-text-secondary',
              ].join(' ')}
            >
              <Icon name={command.icon} className="h-4 w-4" />
              <span className="flex-1">{command.title}</span>
              {command.hint && (
                <span className="text-xs text-text-muted">{command.hint}</span>
              )}
            </button>
          ))}

          {commands.length === 0 && query.trim() && (
            <button
              type="button"
              onClick={() => run(-1)}
              className={[
                'w-full rounded p-3 text-left text-text-secondary',
                'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
              ].join(' ')}
            >
              Search for “{query}”
            </button>
          )}
        </div>
      </div>
    </div>
  )
}
