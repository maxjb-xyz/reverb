import { Link, useParams } from 'react-router-dom'
import { useArtist, coverUrl } from '../lib/libraryApi'

export default function Artist() {
  const { id = '' } = useParams()
  const { data: artist, isLoading, isError } = useArtist(id)

  if (isLoading) return <p className="text-neutral-500">Loading artist…</p>
  if (isError || !artist) return <p className="text-neutral-500">Artist not found.</p>

  const albums = artist.albums ?? []

  return (
    <div className="space-y-6">
      <header className="flex items-end gap-6">
        {artist.coverArtId ? (
          <img src={coverUrl(artist.coverArtId, 300)} alt="" className="h-44 w-44 rounded-full object-cover shadow-lg" />
        ) : (
          <div className="h-44 w-44 rounded-full bg-neutral-800" />
        )}
        <div>
          <div className="text-xs uppercase tracking-wide text-neutral-400">Artist</div>
          <h1 className="text-3xl font-bold">{artist.name}</h1>
          <div className="mt-1 text-sm text-neutral-400">{artist.albumCount || albums.length} albums</div>
        </div>
      </header>

      <section>
        <h2 className="mb-2 text-lg font-bold">Albums</h2>
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
          {albums.map((al) => (
            <Link key={al.id} to={`/album/${al.id}`} className="group">
              {al.coverArtId ? (
                <img src={coverUrl(al.coverArtId, 300)} alt="" className="aspect-square w-full rounded object-cover" />
              ) : (
                <div className="aspect-square w-full rounded bg-neutral-800" />
              )}
              <div className="mt-1 truncate text-sm font-medium group-hover:text-accent">{al.name}</div>
              <div className="truncate text-xs text-neutral-400">{al.year || ''}</div>
            </Link>
          ))}
          {albums.length === 0 && <p className="text-neutral-500">No albums.</p>}
        </div>
      </section>
    </div>
  )
}
