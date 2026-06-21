import type { SourceStatus } from '../lib/everywhereStore'

function label(s: SourceStatus): string {
  const name = s.source.charAt(0).toUpperCase() + s.source.slice(1)
  switch (s.status) {
    case 'ok':
      return `${name} ok`
    case 'timeout':
      return `${name} timed out`
    case 'error':
      return `${name} error`
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
