import { coverUrl, useAlbums } from '../../lib/libraryApi'
import { Cover } from '../ui/Cover'
import type { RecentRow } from '../../lib/statsApi'

interface Props {
  rows: RecentRow[]
}

function metadataKey(name: string, artist = ''): string {
  return `${name}\u0000${artist}`.trim().toLocaleLowerCase()
}

/** Format a Unix-second timestamp as a relative "Xm ago" / "Xh ago" / "Xd ago" string. */
function relTime(sec: number): string {
  const diff = Math.floor(Date.now() / 1000) - sec
  if (diff < 60) return 'just now'
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`
  return `${Math.floor(diff / 86400)}d ago`
}

export function RecentList({ rows }: Props) {
  const albums = useAlbums('alphabeticalByName', 500)
  return (
    <section aria-label="Recently played">
      <h2 className="text-base font-bold text-text-primary mb-3">Recently played</h2>
      <div className="flex flex-col gap-0.5">
        {rows.map((row, i) => {
          const album = (albums.data ?? []).find((item) => metadataKey(item.name, item.artist) === metadataKey(row.Album, row.Artist))
          // Catalog IDs from play history resolve to the current library cover,
          // making recent tracks work even when the album is outside the browse set.
          const src = coverUrl(album?.coverArtId || row.CatalogID, 48)
          return (
            <div
              key={`recent-${i}`}
              className="flex items-center gap-3 px-3 py-2 rounded-md"
            >
              <div className="flex-none">
                <Cover
                  src={src}
                  alt={row.Title}
                  size={40}
                  rounded="md"
                />
              </div>
              <div className="flex-1 min-w-0">
                <div className="text-sm font-semibold text-text-primary truncate">
                  {row.Title}
                </div>
                <div className="text-xs text-text-muted truncate">
                  {row.Artist}
                  {row.Album ? ` · ${row.Album}` : ''}
                </div>
              </div>
              <div className="flex-none text-xs text-text-muted tabular-nums whitespace-nowrap">
                {relTime(row.PlayedAt)}
              </div>
            </div>
          )
        })}
      </div>
    </section>
  )
}
