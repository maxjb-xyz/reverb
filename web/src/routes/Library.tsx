import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useAlbums, useArtists, coverUrl } from '../lib/libraryApi'

type Tab = 'artists' | 'albums'

export default function Library() {
  const [tab, setTab] = useState<Tab>('albums')
  const albums = useAlbums('newest')
  const artists = useArtists()

  return (
    <div className="space-y-6">
      <div className="flex gap-2">
        {(['albums', 'artists'] as Tab[]).map((t) => (
          <button
            key={t}
            type="button"
            onClick={() => setTab(t)}
            className={`rounded-full px-4 py-1.5 text-sm capitalize ${
              tab === t ? 'bg-accent text-white' : 'bg-neutral-900 text-neutral-300 hover:bg-neutral-800'
            }`}
          >
            {t}
          </button>
        ))}
      </div>

      {tab === 'albums' && (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
          {(albums.data ?? []).map((al) => (
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
          {albums.isLoading && <p className="text-neutral-500">Loading albums…</p>}
        </div>
      )}

      {tab === 'artists' && (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6">
          {(artists.data ?? []).map((ar) => (
            <Link key={ar.id} to={`/artist/${ar.id}`} className="group text-center">
              {ar.coverArtId ? (
                <img src={coverUrl(ar.coverArtId, 300)} alt="" className="aspect-square w-full rounded-full object-cover" />
              ) : (
                <div className="aspect-square w-full rounded-full bg-neutral-800" />
              )}
              <div className="mt-1 truncate text-sm font-medium group-hover:text-accent">{ar.name}</div>
            </Link>
          ))}
          {artists.isLoading && <p className="text-neutral-500">Loading artists…</p>}
        </div>
      )}
    </div>
  )
}
