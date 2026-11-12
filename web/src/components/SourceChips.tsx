import { adapterDisplayName } from '../lib/displayNames'
import type { SourceStatus } from '../lib/everywhereStore'

function label(s: SourceStatus): string {
  const name = adapterDisplayName(s.source)
  switch (s.status) {
    case 'ok':
      return `${name} ok`
    case 'timeout':
      return `${name} timed out`
    case 'error':
      return `${name} unavailable`
  }
}

export function SourceChips({ sources }: { sources: SourceStatus[] }) {
  if (sources.length === 0) return null
  return (
    <div className="flex flex-wrap items-center gap-x-2 text-xs text-text-muted">
      {sources.map((s, i) => (
        <span key={s.source}>
          {label(s)}
          {i < sources.length - 1 ? ' ·' : ''}
        </span>
      ))}
    </div>
  )
}
