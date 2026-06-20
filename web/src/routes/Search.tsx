import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useLibrarySearch, coverUrl } from '../lib/libraryApi'
import { TrackRow } from '../components/TrackRow'

export default function Search() {
  const [q, setQ] = useState('')
  // M2 SEAM: an "Everywhere" mode toggle goes here (segmented pill). For M1 it
  // is Library-only; the disabled pill marks the seam without wiring SSE.
  const { data, isFetching } = useLibrarySearch(q)

  const tracks = data?.tracks ?? []
  const albums = data?.albums ?? []
  const artists = data?.artists ?? []

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-3">
        <input
          autoFocus
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Search your library…"
          className="w-full max-w-xl rounded bg-neutral-900 px-4 py-2 outline-none ring-1 ring-neutral-800 focus:ring-accent"
        />
        <div className="flex overflow-hidden rounded-full ring-1 ring-neutral-800">
          <span className="bg-accent px-3 py-1 text-sm text-white">My Library</span>
          {/* M2 SEAM: enable this pill and switch to /search/everywhere (SSE). */}
          <button type="button" disabled title="Everywhere search arrives in M2" className="cursor-not-allowed px-3 py-1 text-sm text-neutral-600">
            Everywhere
          </button>
        </div>
      </div>

      {q.trim() === '' && <p className="text-neutral-500">Type to search your library.</p>}
      {isFetching && <p className="text-neutral-500">Searching…</p>}

      {tracks.length > 0 && (
        <section>
          <h2 className="mb-2 text-lg font-bold">Tracks</h2>
          <div className="space-y-0.5">
            {tracks.map((t, i) => (
              <TrackRow key={t.id} track={t} index={i} queue={tracks} />
            ))}
          </div>
        </section>
      )}

      {albums.length > 0 && (
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
                <div className="truncate text-xs text-neutral-400">{al.artist}</div>
              </Link>
            ))}
          </div>
        </section>
      )}

      {artists.length > 0 && (
        <section>
          <h2 className="mb-2 text-lg font-bold">Artists</h2>
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6">
            {artists.map((ar) => (
              <Link key={ar.id} to={`/artist/${ar.id}`} className="group text-center">
                {ar.coverArtId ? (
                  <img src={coverUrl(ar.coverArtId, 300)} alt="" className="aspect-square w-full rounded-full object-cover" />
                ) : (
                  <div className="aspect-square w-full rounded-full bg-neutral-800" />
                )}
                <div className="mt-1 truncate text-sm font-medium group-hover:text-accent">{ar.name}</div>
              </Link>
            ))}
          </div>
        </section>
      )}
    </div>
  )
}
