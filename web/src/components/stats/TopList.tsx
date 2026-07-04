import { useNavigate } from 'react-router-dom'
import { Cover } from '../ui/Cover'
import { coverUrl } from '../../lib/libraryApi'
import { msToHuman } from '../../lib/range'
import { usePlayer } from '../../lib/playerStore'
import type { TopRow } from '../../lib/statsApi'
import type { Track } from '../../lib/types'

type EntityKind = 'track' | 'artist' | 'album'

interface Props {
  title: string
  rows: TopRow[]
  kind: EntityKind
}

/** Returns the primary display name for a row based on its kind. */
function displayName(kind: EntityKind, row: TopRow): string {
  if (kind === 'track') return row.Title
  if (kind === 'artist') return row.Artist
  return row.Album
}

/** Returns the nav path for artist/album rows; null for track rows (they play instead). */
function entityPath(kind: EntityKind, row: TopRow): string | null {
  if (kind === 'artist' && row.Artist) return `/artist/library/${encodeURIComponent(row.Artist)}`
  if (kind === 'album' && row.Album) return `/album/library/${encodeURIComponent(row.Album)}`
  return null
}

/** Synthesizes a minimal playable Track from a TopRow (track kind only). */
function trackFromTopRow(row: TopRow): Track {
  return {
    id: row.CatalogID,
    title: row.Title,
    albumId: '',
    album: row.Album,
    artistId: '',
    artist: row.Artist,
    coverArtId: row.CatalogID,
    trackNumber: 0,
    discNumber: 0,
    durationMs: 0,
    bitRate: 0,
    suffix: '',
    contentType: '',
  }
}

export function TopList({ title, rows, kind }: Props) {
  const navigate = useNavigate()
  const playTrackList = usePlayer((s) => s.playTrackList)

  return (
    <section aria-label={title}>
      <h2 className="text-base font-bold text-text-primary mb-3">{title}</h2>
      <div className="flex flex-col gap-0.5">
        {rows.map((row, i) => {
          const src = row.CatalogID ? coverUrl(row.CatalogID, 48) : ''
          const path = entityPath(kind, row)
          const name = displayName(kind, row)
          const meta = `${row.Plays} plays · ${msToHuman(row.MsPlayed)}`
          // aria-label: descriptive for tracks; just the name for artist/album rows
          const label = kind === 'track'
            ? `${row.Title} by ${row.Artist}`
            : name
          // Secondary text: artist name for tracks; nothing for artist/album (meta shown right)
          const secondary = kind === 'track' ? row.Artist : null

          function handleClick() {
            if (kind === 'track') {
              playTrackList([trackFromTopRow(row)], 0)
            } else if (path) {
              navigate(path)
            }
          }

          const clickable = kind === 'track' ? !!row.CatalogID : !!path

          return (
            <button
              key={`${kind}-${i}`}
              type="button"
              aria-label={label}
              disabled={!clickable}
              onClick={handleClick}
              className={[
                'group flex items-center gap-3 px-3 py-2 rounded-md text-left transition-colors',
                clickable
                  ? 'hover:bg-raised cursor-pointer'
                  : 'cursor-default',
                'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
              ].join(' ')}
            >
              {/* Rank */}
              <span className="w-5 flex-none text-center text-sm font-bold tabular-nums text-text-muted">
                {i + 1}
              </span>

              {/* Cover — only when there's a CatalogID */}
              <div className="flex-none">
                <Cover
                  src={src}
                  alt={name}
                  size={40}
                  rounded="md"
                />
              </div>

              {/* Text */}
              <div className="flex-1 min-w-0">
                <div className="text-sm font-semibold text-text-primary truncate">
                  {name}
                </div>
                {secondary && (
                  <div className="text-xs text-text-muted truncate">
                    {secondary}
                  </div>
                )}
              </div>

              {/* Plays + time — right-aligned */}
              <div className="flex-none text-right">
                <div className="text-xs font-semibold text-text-muted tabular-nums">
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
