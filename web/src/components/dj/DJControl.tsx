import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { Icon } from '../ui/Icon'
import { stopDJ, useDJ } from '../../lib/dj'
import { useStartDJ } from './useStartDJ'
import { VoiceNudge } from './VoiceNudge'

/**
 * The DJ's place in the player: a "DJ" button that starts the DJ, shows what
 * it just said in a bubble, and opens its controls (what lane is playing, a
 * box to ask for something, change it up, turn it off) in a panel above the
 * player. `look="mini"` is the phone player's version, shown only while the DJ
 * is on.
 */
export function DJControl({ look = 'bar' }: { look?: 'bar' | 'mini' }) {
  const { active, card, busy } = useDJ()
  const { start } = useStartDJ()
  const [open, setOpen] = useState(false)
  const root = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (root.current && !root.current.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  if (!active && look === 'mini') return null
  const shown = open && active
  const mini = look === 'mini'

  return (
    <div ref={root} className={mini ? 'flex-none' : 'relative flex-none'}>
      <button
        type="button"
        aria-label="DJ"
        aria-expanded={active ? shown : undefined}
        title={active ? 'DJ controls' : 'Start the DJ'}
        data-testid="dj-button"
        disabled={busy && !active}
        onClick={() => (active ? setOpen((o) => !o) : void start())}
        className={[
          'rvb-press inline-flex items-center gap-1 rounded-full font-bold tracking-tight transition-colors',
          'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:opacity-60',
          mini ? 'h-11 px-3 text-sm' : 'h-7 px-2.5 text-xs',
          active ? 'bg-accent text-on-accent hover:brightness-110' : 'text-text-secondary hover:text-text-primary hover:bg-white/[.06]',
        ].join(' ')}
      >
        <Icon name="mic" className="w-3.5 h-3.5" />
        DJ
      </button>

      {/* What the DJ just said, while the controls are closed */}
      {active && !shown && card && (
        <button
          type="button"
          key={card.key}
          data-testid="dj-line"
          aria-live="polite"
          onClick={() => setOpen(true)}
          className={[
            'absolute bottom-full z-40 mb-3 rounded-xl border border-white/[.06] bg-raised px-3 py-2 text-left text-sm leading-relaxed',
            'text-text-primary shadow-pop animate-toast-in',
            mini ? 'inset-x-2' : 'right-0 w-72',
          ].join(' ')}
        >
          {card.text}
        </button>
      )}

      {shown && (
        <div
          className={[
            'absolute bottom-full z-40 mb-3',
            mini ? 'inset-x-2' : 'right-0 w-[22rem]',
          ].join(' ')}
        >
          <DJPanel focus={!mini} onDone={() => setOpen(false)} />
        </div>
      )}
    </div>
  )
}

/** The DJ's controls. */
function DJPanel({ focus, onDone }: { focus: boolean; onDone: () => void }) {
  const { label, card, busy, missing } = useDJ()
  const { change, ask } = useStartDJ()
  const navigate = useNavigate()
  const [text, setText] = useState('')

  async function submit(e: FormEvent) {
    e.preventDefault()
    if (await ask(text)) setText('')
  }

  return (
    <div
      data-testid="dj-panel"
      role="dialog"
      aria-label="DJ"
      className="rounded-xl border border-white/[.06] bg-raised shadow-pop animate-toast-in"
    >
      <div className="flex items-center gap-2 px-3 pt-3 pb-2">
        <span className="inline-grid place-items-center w-7 h-7 rounded-full bg-accent text-on-accent flex-none">
          <Icon name="mic" className="w-3.5 h-3.5" />
        </span>
        <div className="min-w-0 flex-1">
          <p className="text-[11px] font-semibold uppercase tracking-wider text-text-muted leading-none">DJ</p>
          <p className="truncate text-sm font-medium text-text-primary" data-testid="dj-lane">{label}</p>
        </div>
        <button
          type="button"
          onClick={() => void change()}
          disabled={busy}
          className="rounded-full px-3 py-1.5 text-xs font-semibold bg-white/[.06] hover:bg-white/[.12] text-text-primary disabled:opacity-60 transition-colors"
        >
          {busy ? 'Switching…' : 'Change it up'}
        </button>
        <button
          type="button"
          aria-label="Turn off the DJ"
          title="Turn off the DJ"
          onClick={() => {
            stopDJ()
            onDone()
          }}
          className="inline-grid place-items-center w-7 h-7 rounded-full text-text-muted hover:text-text-primary hover:bg-white/[.06] transition-colors"
        >
          <Icon name="x" className="w-3.5 h-3.5" />
        </button>
      </div>
      <div aria-live="polite">
        {card && (
          <p key={card.key} data-testid="dj-line" className="px-3 pb-2 text-sm leading-relaxed text-text-secondary">
            {card.text}
          </p>
        )}
        {missing && (
          <div data-testid="dj-missing" className="mx-3 mb-2 flex items-center gap-2 rounded-lg bg-white/[.04] px-3 py-2 text-sm">
            <span className="min-w-0 flex-1 text-text-secondary">
              <span className="font-medium text-text-primary">{missing}</span> isn't in your library yet.
            </span>
            <button
              type="button"
              onClick={() => {
                onDone()
                navigate(`/search?q=${encodeURIComponent(missing)}`)
              }}
              className="flex-none rounded-full px-3 py-1 text-xs font-semibold bg-accent text-on-accent hover:brightness-110 transition"
            >
              Find it
            </button>
          </div>
        )}
      </div>
      <VoiceNudge />
      <form onSubmit={(e) => void submit(e)} className="flex items-center gap-2 px-3 pb-3">
        <input
          type="text"
          value={text}
          onChange={(e) => setText(e.target.value)}
          maxLength={200}
          autoFocus={focus}
          aria-label="Ask the DJ"
          placeholder="An artist, a genre, a decade, a mood…"
          className="min-w-0 flex-1 rounded-full bg-white/[.06] px-3 py-1.5 text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus-visible:ring-2 focus-visible:ring-accent"
        />
        <button
          type="submit"
          disabled={busy || !text.trim()}
          aria-label="Send to the DJ"
          className="inline-grid flex-none place-items-center w-8 h-8 rounded-full bg-accent text-on-accent disabled:opacity-50 transition-opacity"
        >
          <Icon name="up" className="w-3.5 h-3.5" />
        </button>
      </form>
    </div>
  )
}
