import type { Track } from '../lib/types'
import { formatDuration } from '../lib/types'
import { coverUrl } from '../lib/libraryApi'
import { usePlayer } from '../lib/playerStore'

interface Props {
  track: Track
  index: number
  queue: Track[]
}

export function TrackRow({ track, index, queue }: Props) {
  const playTrackList = usePlayer((s) => s.playTrackList)
  const current = usePlayer((s) => s.current)
  const isCurrent = current?.id === track.id
  return (
    <button
      type="button"
      onClick={() => playTrackList(queue, index)}
      className={`group flex w-full items-center gap-3 rounded px-2 py-1.5 text-left hover:bg-neutral-800 ${
        isCurrent ? 'text-accent' : 'text-neutral-200'
      }`}
    >
      <span className="w-6 text-right text-sm text-neutral-500">{track.trackNumber || index + 1}</span>
      {track.coverArtId ? (
        <img src={coverUrl(track.coverArtId, 80)} alt="" className="h-9 w-9 rounded object-cover" />
      ) : (
        <div className="h-9 w-9 rounded bg-neutral-800" />
      )}
      <span className="flex-1 truncate">
        <span className="block truncate text-sm font-medium">{track.title}</span>
        <span className="block truncate text-xs text-neutral-400">{track.artist}</span>
      </span>
      <span className="text-xs text-neutral-500">{formatDuration(track.durationMs)}</span>
    </button>
  )
}
