import { ProgressRing } from './ProgressRing'
import { Icon } from './Icon'
import type { CoverageState } from '../../lib/types'

interface Props {
  state: CoverageState
  owned: number
  total: number
}

export function CoverageChip({ state, owned, total }: Props) {
  if (state === 'none') return null
  const base = 'inline-flex items-center gap-1 rounded-full bg-black/60 backdrop-blur px-2 h-6 text-[11px] font-extrabold'
  if (state === 'full') {
    return (
      <span data-testid="coverage-full" className={`${base} text-accent`}>
        <Icon name="check" className="text-xs" />
      </span>
    )
  }
  if (state === 'pending') {
    return (
      <span className={`${base} text-text-muted`} aria-label="Checking library">
        <ProgressRing value={0} size={14} indeterminate />
      </span>
    )
  }
  // partial
  const pct = total > 0 ? Math.round((owned / total) * 100) : 0
  return (
    <span className={`${base} text-text-primary`}>
      <ProgressRing value={pct} size={14} />
      {owned}/{total}
    </span>
  )
}
