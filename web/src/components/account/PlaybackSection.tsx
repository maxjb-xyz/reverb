import { Segmented } from '../ui'
import { usePlayer } from '../../lib/playerStore'

const CROSSFADE_OPTIONS = [
  { value: 0, label: 'Off' },
  { value: 3000, label: '3 s' },
  { value: 6000, label: '6 s' },
  { value: 9000, label: '9 s' },
  { value: 12000, label: '12 s' },
]

/** Playback tab panel: settings for how this device plays music. */
export function PlaybackSection() {
  const crossfadeMs = usePlayer((s) => s.crossfadeMs)
  const setCrossfade = usePlayer((s) => s.setCrossfade)
  // A value saved by an older build may not be one of the steps; show the nearest.
  const selected = CROSSFADE_OPTIONS.reduce((best, o) =>
    Math.abs(o.value - crossfadeMs) < Math.abs(best.value - crossfadeMs) ? o : best,
  ).value

  return (
    <div className="space-y-0 divide-y divide-border-subtle">
      <div className="flex flex-col gap-3 py-5 sm:flex-row sm:items-center sm:gap-5">
        <div className="flex-1 min-w-0">
          <div className="text-sm font-bold text-text-primary">Crossfade</div>
          <div className="text-xs text-text-secondary mt-0.5">
            Fade each song into the next instead of stopping between them. Saved on this device.
          </div>
        </div>
        <div className="flex-none">
          <Segmented options={CROSSFADE_OPTIONS} value={selected} onChange={setCrossfade} />
        </div>
      </div>
    </div>
  )
}
