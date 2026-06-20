import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useLibrarySearch, coverUrl } from '../lib/libraryApi'
import { TrackRow } from '../components/TrackRow'
import { useEverywhere } from '../lib/everywhereStore'
import { ExternalRow } from '../components/ExternalRow'
import { SourceChips } from '../components/SourceChips'

type Mode = 'library' | 'everywhere'

export default function Search() {
  const [q, setQ] = useState('')
  const [mode, setMode] = useState<Mode>('library')

  // Library mode: a single fast REST query (TanStack Query), unchanged from M1.
  const lib = useLibrarySearch(mode === 'library' ? q : '')
  // Everywhere mode: SSE stream accumulated into stable sections (distinct transport).
  const everywhere = useEverywhere(q, 'track', mode === 'everywhere')

  const tracks = lib.data?.tracks ?? []
  const albums = lib.data?.albums ?? []
  const artists = lib.data?.artists ?? []

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
          <button
            type="button"
            onClick={() => setMode('library')}
            className={`px-3 py-1 text-sm ${mode === 'library' ? 'bg-accent text-white' : 'text-neutral-400 hover:text-neutral-200'}`}
          >
            My Library
          </button>
          <button
            type="button"
            onClick={() => setMode('everywhere')}
            className={`px-3 py-1 text-sm ${mode === 'everywhere' ? 'bg-accent text-white' : 'text-neutral-400 hover:text-neutral-200'}`}
          >
            Everywhere
          </button>
        </div>
      </div>

      {q.trim() === '' && <p className="text-neutral-500">Type to search.</p>}

      {mode === 'library' && (
        <>
          {lib.isFetching && <p className="text-neutral-500">Searching…</p>}
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
        </>
      )}

      {mode === 'everywhere' && q.trim() !== '' && (
        <>
          <SourceChips sources={everywhere.sources} />
          {/* Stable sections: results append within each section, never reflow. */}
          <section>
            <h2 className="mb-2 text-lg font-bold">Tracks</h2>
            {everywhere.tracks.length === 0 ? (
              <p className="text-neutral-500">Searching sources…</p>
            ) : (
              <div className="space-y-0.5">
                {everywhere.tracks.map((r) => (
                  <ExternalRow key={`${r.source}:${r.externalId}`} result={r} />
                ))}
              </div>
            )}
          </section>
          {everywhere.albums.length > 0 && (
            <section>
              <h2 className="mb-2 text-lg font-bold">Albums</h2>
              <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
                {everywhere.albums.map((r) => (
                  <div key={`${r.source}:${r.externalId}`} className="group">
                    {r.coverUrl ? (
                      <img src={r.coverUrl} alt="" className="aspect-square w-full rounded object-cover" />
                    ) : (
                      <div className="aspect-square w-full rounded bg-neutral-800" />
                    )}
                    <div className="mt-1 truncate text-sm font-medium">{r.title}</div>
                    <div className="truncate text-xs text-neutral-400">{r.artist}</div>
                  </div>
                ))}
              </div>
            </section>
          )}
          {everywhere.artists.length > 0 && (
            <section>
              <h2 className="mb-2 text-lg font-bold">Artists</h2>
              <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6">
                {everywhere.artists.map((r) => (
                  <div key={`${r.source}:${r.externalId}`} className="group text-center">
                    {r.coverUrl ? (
                      <img src={r.coverUrl} alt="" className="aspect-square w-full rounded-full object-cover" />
                    ) : (
                      <div className="aspect-square w-full rounded-full bg-neutral-800" />
                    )}
                    <div className="mt-1 truncate text-sm font-medium">{r.title}</div>
                  </div>
                ))}
              </div>
            </section>
          )}
        </>
      )}
    </div>
  )
}
