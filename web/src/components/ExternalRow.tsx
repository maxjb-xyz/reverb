import type { ReactNode } from 'react'
import type { ExternalResult, Track } from '../lib/types'
import { formatDuration } from '../lib/types'
import { usePlayer } from '../lib/playerStore'
import { useDownloads } from '../lib/downloadStore'
import { postDownload } from '../lib/downloadApi'

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

// ProgressRing renders a determinate ring (progress>=0) or an indeterminate
// spinner (progress<0). It is the ⟳ state of the result row.
function ProgressRing({ progress }: { progress: number }) {
  const label = progress >= 0 ? `Downloading ${progress}%` : 'Downloading'
  if (progress < 0) {
    return (
      <span aria-label={label} title={label} className="inline-block h-4 w-4 animate-spin rounded-full border-2 border-neutral-600 border-t-accent" />
    )
  }
  const deg = Math.round((progress / 100) * 360)
  return (
    <span
      aria-label={label}
      title={label}
      className="inline-block h-4 w-4 rounded-full"
      style={{ background: `conic-gradient(rgb(var(--color-accent)) ${deg}deg, rgb(64 64 64) ${deg}deg)` }}
    />
  )
}

export function ExternalRow({ result }: Props) {
  const playTrackList = usePlayer((s) => s.playTrackList)
  const job = useDownloads((s) => s.byExternal(result.source, result.externalId))

  // A completed job that matched a library track makes the row in-library too.
  const matchedTrackId =
    (result.match?.status === 'in_library' && result.match.libraryTrackId) ||
    (job?.status === 'completed' && job.libraryTrackId) ||
    ''
  const inLibrary = !!matchedTrackId
  const active = !!job && (job.status === 'queued' || job.status === 'running')

  function onDownload() {
    void postDownload({
      source: result.source,
      externalId: result.externalId,
      artist: result.artist,
      title: result.title,
      album: result.album,
      isrc: result.isrc,
    }).then((j) => useDownloads.getState().upsert(j))
  }

  const cover = result.coverUrl ? (
    <img src={result.coverUrl} alt="" className="h-9 w-9 rounded object-cover" />
  ) : (
    <div className="h-9 w-9 rounded bg-neutral-800" />
  )

  let action: ReactNode
  if (inLibrary) {
    action = <span title="In library" className="text-accent">✓</span>
  } else if (active) {
    action = <ProgressRing progress={job!.progress} />
  } else {
    action = (
      <button
        type="button"
        aria-label={`Download ${result.title}`}
        onClick={(e) => {
          e.stopPropagation()
          onDownload()
        }}
        className="text-neutral-400 hover:text-accent"
      >
        ↓
      </button>
    )
  }

  const body = (
    <>
      {cover}
      <span className="flex-1 truncate">
        <span className="block truncate text-sm font-medium">{result.title}</span>
        <span className="block truncate text-xs text-neutral-400">{result.artist}</span>
      </span>
      {action}
      <span className="w-12 text-right text-xs text-neutral-500">{formatDuration(result.durationMs)}</span>
    </>
  )

  if (inLibrary) {
    return (
      <button
        type="button"
        onClick={() => playTrackList([trackFromMatch(result, matchedTrackId)], 0)}
        className="group flex w-full items-center gap-3 rounded px-2 py-1.5 text-left text-neutral-200 hover:bg-neutral-800"
      >
        {body}
      </button>
    )
  }
  return <div className="flex w-full items-center gap-3 rounded px-2 py-1.5 text-neutral-300">{body}</div>
}
