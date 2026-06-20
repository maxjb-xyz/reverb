import type { ExternalResult, Track } from '../lib/types'
import { formatDuration } from '../lib/types'
import { usePlayer } from '../lib/playerStore'

interface Props {
  result: ExternalResult
}

// trackFromMatch synthesizes a minimal library Track from the external metadata,
// using the matched library track id so the stream proxy can play it.
function trackFromMatch(r: ExternalResult, libraryTrackId: string): Track {
  return {
    id: libraryTrackId,
    title: r.title,
    albumId: '',
    album: r.album,
    artistId: '',
    artist: r.artist,
    coverArtId: r.coverArtId ?? '',
    trackNumber: 0,
    discNumber: 0,
    durationMs: r.durationMs,
    bitRate: 0,
    suffix: '',
    contentType: '',
    isrc: r.isrc,
  }
}

export function ExternalRow({ result }: Props) {
  const playTrackList = usePlayer((s) => s.playTrackList)
  const inLibrary = result.match?.status === 'in_library' && !!result.match.libraryTrackId

  const cover = result.coverUrl ? (
    <img src={result.coverUrl} alt="" className="h-9 w-9 rounded object-cover" />
  ) : (
    <div className="h-9 w-9 rounded bg-neutral-800" />
  )

  const body = (
    <>
      {cover}
      <span className="flex-1 truncate">
        <span className="block truncate text-sm font-medium">{result.title}</span>
        <span className="block truncate text-xs text-neutral-400">{result.artist}</span>
      </span>
      {inLibrary ? (
        <span title="In library" className="text-accent">✓</span>
      ) : (
        /* M3 SEAM: the download affordance (↓ popover + ⟳ progress ring) goes here.
           For M2 an unmatched external result is a plain, non-interactive row. */
        <span className="text-xs text-neutral-600">—</span>
      )}
      <span className="w-12 text-right text-xs text-neutral-500">{formatDuration(result.durationMs)}</span>
    </>
  )

  if (inLibrary) {
    return (
      <button
        type="button"
        onClick={() => playTrackList([trackFromMatch(result, result.match!.libraryTrackId)], 0)}
        className="group flex w-full items-center gap-3 rounded px-2 py-1.5 text-left text-neutral-200 hover:bg-neutral-800"
      >
        {body}
      </button>
    )
  }
  return <div className="flex w-full items-center gap-3 rounded px-2 py-1.5 text-neutral-300">{body}</div>
}
