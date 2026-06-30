import { useNavigate } from 'react-router-dom'
import { Cover } from '../ui/Cover'
import { coverUrl } from '../../lib/libraryApi'
import { msToHuman } from '../../lib/range'
import type { TopRow } from '../../lib/statsApi'

type EntityKind = 'track' | 'artist' | 'album'

interface Props {
  title: string
  rows: TopRow[]
  kind: EntityKind
}

function entityPath(kind: EntityKind, row: TopRow): string | null {
  if (kind === 'track' && row.CatalogID) return `/album/library/${row.CatalogID}`
  if (kind === 'artist') return `/artist/library/${encodeURIComponent(row.Title)}`
  if (kind === 'album') return `/album/library/${encodeURIComponent(row.Title)}`
  return null
}

export function TopList({ title, rows, kind }: Props) {
  const navigate = useNavigate()

  return (
    <section aria-label={title}>
      <h2 className="text-base font-bold text-primary mb-3">{title}</h2>
      <div className="flex flex-col gap-0.5">
        {rows.map((row, i) => {
          const src = row.CatalogID ? coverUrl(row.CatalogID, 48) : ''
          const path = entityPath(kind, row)
          const meta = `${row.Plays} plays · ${msToHuman(row.MsPlayed)}`
          const label = kind === 'track'
            ? `${row.Title} by ${row.Artist}`
            : row.Title

          return (
            <button
              key={`${kind}-${i}`}
              type="button"
              aria-label={label}
              disabled={!path}
              onClick={() => path && navigate(path)}
              className={[
                'group flex items-center gap-3 px-3 py-2 rounded-md text-left transition-colors',
                path
                  ? 'hover:bg-surface-raised cursor-pointer'
                  : 'cursor-default',
                'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
              ].join(' ')}
            >
              {/* Rank */}
              <span className="w-5 flex-none text-center text-sm font-bold tabular-nums text-secondary">
                {i + 1}
              </span>

              {/* Cover — only when there's a CatalogID */}
              <div className="flex-none">
                <Cover
                  src={src}
                  alt={row.Title}
                  size={40}
                  rounded="md"
                />
              </div>

              {/* Text */}
              <div className="flex-1 min-w-0">
                <div className="text-sm font-semibold text-primary truncate">
                  {row.Title}
                </div>
                <div className="text-xs text-secondary truncate">
                  {kind === 'track' ? row.Artist : (row.Artist || meta)}
                </div>
              </div>

              {/* Plays + time — right-aligned */}
              <div className="flex-none text-right">
                <div className="text-xs font-semibold text-secondary tabular-nums">
                  {meta}
                </div>
              </div>
            </button>
          )
        })}
      </div>
    </section>
  )
}
