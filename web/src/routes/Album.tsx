import { Link, useParams } from 'react-router-dom'
import { useAlbum, coverUrl } from '../lib/libraryApi'
import { TrackRow } from '../components/TrackRow'
import { formatDuration } from '../lib/types'
import { usePlayer } from '../lib/playerStore'

export default function Album() {
  const { id = '' } = useParams()
  const { data: album, isLoading, isError } = useAlbum(id)
  const playTrackList = usePlayer((s) => s.playTrackList)

  if (isLoading) return <p className="text-neutral-500">Loading album…</p>
  if (isError || !album) return <p className="text-neutral-500">Album not found.</p>

  const tracks = album.tracks ?? []

  return (
    <div className="space-y-6">
      <header className="flex items-end gap-6">
        {album.coverArtId ? (
          <img src={coverUrl(album.coverArtId, 300)} alt="" className="h-44 w-44 rounded object-cover shadow-lg" />
        ) : (
          <div className="h-44 w-44 rounded bg-neutral-800" />
        )}
        <div>
          <div className="text-xs uppercase tracking-wide text-neutral-400">Album</div>
          <h1 className="text-3xl font-bold">{album.name}</h1>
          <div className="mt-1 text-sm text-neutral-400">
            <Link to={`/artist/${album.artistId}`} className="hover:text-accent">
              {album.artist}
            </Link>
            {album.year ? ` · ${album.year}` : ''}
            {album.songCount ? ` · ${album.songCount} songs` : ''}
            {album.durationMs ? ` · ${formatDuration(album.durationMs)}` : ''}
          </div>
          <button
            type="button"
            onClick={() => tracks.length && playTrackList(tracks, 0)}
            className="mt-3 rounded-full bg-accent px-5 py-2 text-sm font-medium text-white disabled:opacity-50"
            disabled={tracks.length === 0}
          >
            Play
          </button>
        </div>
      </header>

      <div className="space-y-0.5">
        {tracks.map((t, i) => (
          <TrackRow key={t.id} track={t} index={i} queue={tracks} />
        ))}
      </div>
    </div>
  )
}
