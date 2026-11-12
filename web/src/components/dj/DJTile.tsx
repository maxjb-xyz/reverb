import { Icon } from '../ui/Icon'
import { useDJ } from '../../lib/dj'
import { useStartDJ } from './useStartDJ'

/** The DJ's card in "Radio for you": one tap and the DJ takes over. */
export function DJTile() {
  const { start, busy } = useStartDJ()
  const active = useDJ((s) => s.active)
  return (
    <button
      type="button"
      aria-label={active ? 'DJ is on' : 'Start the DJ'}
      data-testid="dj-tile"
      disabled={busy}
      onClick={() => void start()}
      className={[
        'group relative w-full text-left p-3 rounded-lg bg-raised hover:bg-raised-hover',
        'transition-[background-color,transform] duration-200 ease-smooth active:scale-[.985]',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:opacity-70',
      ].join(' ')}
    >
      <div className="relative mb-3 aspect-square w-full rounded-md overflow-hidden shadow-cover transition-transform duration-300 ease-smooth group-hover:-translate-y-1">
        <div
          className="absolute inset-0"
          style={{ background: 'radial-gradient(120% 120% at 20% 10%, rgb(var(--color-accent) / .95) 0%, rgb(var(--color-accent) / .35) 55%, rgb(0 0 0 / .6) 100%)' }}
        />
        <div className="absolute inset-0 grid place-items-center text-white">
          <Icon name="mic" className="w-12 h-12 drop-shadow" />
        </div>
        <span className="absolute left-3 bottom-2 text-2xl font-bold tracking-tight text-white drop-shadow">DJ</span>
      </div>
      <p className="truncate text-sm font-semibold text-text-primary leading-snug">{busy ? 'Starting…' : 'DJ'}</p>
      <p className="mt-1 text-xs text-text-secondary line-clamp-2 leading-snug">
        {active ? 'On now. Tap to start over.' : 'Your music, picked and introduced for you'}
      </p>
    </button>
  )
}
