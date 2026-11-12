import { Icon } from '../ui/Icon'
import { stopDJ, useDJ } from '../../lib/dj'
import { useStartDJ } from './useStartDJ'

/**
 * While the DJ is on: what lane is playing, the DJ's latest line, a way to
 * change it up and a way to turn the DJ off. Floats over the bottom of the
 * page, above the player.
 */
export function DJBar() {
  const { active, label, card, busy } = useDJ()
  const { change } = useStartDJ()
  if (!active) return null
  return (
    // A zero-height sticky slot at the end of the page: the card hangs above
    // it, so it stays at the bottom of the view without covering the rail.
    <div data-testid="dj-bar" className="pointer-events-none sticky bottom-0 z-20 h-0">
      <div className="absolute inset-x-3 bottom-3 flex justify-center">
        <div className="pointer-events-auto w-full max-w-md rounded-xl border border-white/[.06] bg-raised/90 backdrop-blur-xl shadow-pop">
          <div className="flex items-center gap-2 px-3 py-2">
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
              onClick={stopDJ}
              className="inline-grid place-items-center w-7 h-7 rounded-full text-text-muted hover:text-text-primary hover:bg-white/[.06] transition-colors"
            >
              <Icon name="x" className="w-3.5 h-3.5" />
            </button>
          </div>
          <div aria-live="polite">
            {card && (
              <p key={card.key} data-testid="dj-line" className="px-3 pb-3 text-sm leading-relaxed text-text-secondary animate-toast-in">
                {card.text}
              </p>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
