/**
 * NowPlayingPanel — desktop right-side Now-Playing panel (Phase 3).
 * Opens when rightPanel === 'nowplaying' in uiStore.
 *
 * Sections:
 *   1. Header  — album/context name + close button
 *   2. Cover   — large square cover art
 *   3. Meta    — title / artist + heart
 *   4. "Next in queue" card — up-next tracks; click → jumpTo
 *   5. "About the artist" card — cover, name, "In your library · N albums"
 */
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { usePlayer } from '../../lib/playerStore'
import { useUI } from '../../lib/uiStore'
import { coverUrl, useArtist } from '../../lib/libraryApi'
import { Cover } from '../ui/Cover'
import { IconButton } from '../ui/IconButton'
import { AddToPlaylistMenu } from '../AddToPlaylistMenu'

// ---------------------------------------------------------------------------
// Artist card
// ---------------------------------------------------------------------------
function ArtistCard({ artistId }: { artistId: string }) {
  const { data: artist } = useArtist(artistId)
  if (!artist) return null

  const artistCoverSrc = artist.coverArtId ? coverUrl(artist.coverArtId, 300) : undefined

  return (
    <div className="mt-3.5 overflow-hidden rounded-lg bg-raised">
      {/* Artist image header */}
      <div className="relative h-36">
        <Cover src={artistCoverSrc} alt={artist.name} size="full" rounded="md" />
        <div className="pointer-events-none absolute inset-0 bg-gradient-to-t from-black/60 to-transparent" />
        <span className="absolute bottom-3 left-4 text-lg font-bold text-text-primary">
          {artist.name}
        </span>
      </div>
      {/* Body */}
      <div className="p-3.5">
        <div className="text-sm font-semibold text-text-secondary">
          In your library · {artist.albumCount} album{artist.albumCount !== 1 ? 's' : ''}
        </div>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// NowPlayingPanel
// ---------------------------------------------------------------------------
export function NowPlayingPanel() {
  const rightPanel = useUI((s) => s.rightPanel)
  const closePanel = useUI((s) => s.closePanel)

  const current = usePlayer((s) => s.current)
  const queue = usePlayer((s) => s.queue)
  const index = usePlayer((s) => s.index)
  const jumpTo = usePlayer((s) => s.jumpTo)

  const navigate = useNavigate()
  const [addMenuOpen, setAddMenuOpen] = useState(false)

  if (rightPanel !== 'nowplaying') return null

  const upNext = queue
    .map((t, i) => ({ t, i }))
    .filter(({ i }) => i > index)
    .slice(0, 5) // show at most 5 upcoming tracks

  const coverSrc = current?.coverArtId ? coverUrl(current.coverArtId, 300) : undefined

  return (
    <aside
      data-testid="now-playing-panel"
      className={[
        'flex h-full w-80 flex-col border-l border-border-subtle bg-surface',
        'absolute inset-y-0 right-0 z-20 md:relative md:inset-auto',
      ].join(' ')}
    >
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3.5">
        {current?.album && current.albumId ? (
          <button
            type="button"
            onClick={() => navigate(`/album/library/${current.albumId}`)}
            className="truncate text-left text-sm font-bold text-text-primary hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
          >
            {current.album}
          </button>
        ) : (
          <span className="text-sm font-bold text-text-primary">
            {current?.album ?? 'Now Playing'}
          </span>
        )}
        <IconButton
          name="x"
          label="Close panel"
          size="sm"
          onClick={closePanel}
        />
      </div>

      {/* Scrollable content */}
      <div className="flex-1 overflow-y-auto px-3 pb-4">
        {/* Big cover */}
        <div className="w-full">
          <Cover
            src={coverSrc}
            alt={current?.title ?? 'Nothing playing'}
            size="full"
            rounded="md"
            className="aspect-square shadow-pop"
          />
        </div>

        {/* Title + artist */}
        <div className="mt-4 flex items-start justify-between gap-2">
          <div className="min-w-0">
            <div className="truncate text-xl font-extrabold leading-tight tracking-tight text-text-primary">
              {current?.title ?? 'Nothing playing'}
            </div>
            {current?.artist && current.artistId ? (
              <button
                type="button"
                onClick={() => navigate(`/artist/library/${current.artistId}`)}
                className="mt-1 block max-w-full truncate text-left text-sm text-text-secondary hover:text-text-primary hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
              >
                {current.artist}
              </button>
            ) : (
              <div className="mt-1 truncate text-sm text-text-secondary">
                {current?.artist ?? ''}
              </div>
            )}
          </div>
          {current && (
            <div className="relative flex-none">
              <IconButton
                name="plus"
                label="Add to playlist"
                size="sm"
                active={addMenuOpen}
                onClick={() => setAddMenuOpen((o) => !o)}
              />
              {addMenuOpen && (
                <AddToPlaylistMenu
                  trackId={current.id}
                  onClose={() => setAddMenuOpen(false)}
                />
              )}
            </div>
          )}
        </div>

        {/* Next in queue */}
        <div className="mt-3.5 overflow-hidden rounded-lg bg-raised">
          <div className="flex items-center justify-between px-4 py-3.5">
            <span className="text-sm font-bold text-text-primary">Next in queue</span>
          </div>
          <ul>
            {upNext.length === 0 && (
              <li className="px-4 pb-4 text-sm text-text-muted">Queue is empty.</li>
            )}
            {upNext.map(({ t, i }) => (
              <li key={`${t.id}-${i}`}>
                <button
                  type="button"
                  aria-label={`Play ${t.title}`}
                  onClick={() => jumpTo(i)}
                  className={[
                    'flex w-full items-center gap-3 px-4 py-2',
                    'text-left transition-colors',
                    'hover:bg-raised-hover',
                    'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
                  ].join(' ')}
                >
                  <Cover
                    src={t.coverArtId ? coverUrl(t.coverArtId, 48) : undefined}
                    alt={t.title}
                    size={40}
                    rounded="md"
                  />
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-sm font-medium text-text-primary">{t.title}</div>
                    <div className="truncate text-xs text-text-secondary">{t.artist}</div>
                  </div>
                </button>
              </li>
            ))}
          </ul>
        </div>

        {/* About the artist */}
        {current?.artistId && <ArtistCard artistId={current.artistId} />}
      </div>
    </aside>
  )
}
