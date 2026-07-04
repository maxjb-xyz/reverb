/**
 * NowPlayingPanel — desktop right-side Now-Playing panel (Phase 3).
 * Opens when rightPanel === 'nowplaying' in uiStore.
 *
 * Sections:
 *   1. Header  — album/context name + close button
 *   2. Cover   — large square cover art
 *   3. Meta    — title / artist + add-to-playlist
 *   4. "Next in queue" card — up-next tracks; click → jumpTo
 *   5. "About the artist" card — cover, name, "In your library · N albums"
 */
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { usePlayer } from '../../lib/playerStore'
import { useUI } from '../../lib/uiStore'
import { coverUrl, trackCoverUrl, useArtist } from '../../lib/libraryApi'
import { useArtistProfile } from '../../lib/coverageApi'
import { Cover } from '../ui/Cover'
import { IconButton } from '../ui/IconButton'
import { AddToPlaylistMenu } from '../AddToPlaylistMenu'

// ---------------------------------------------------------------------------
// Artist card
// ---------------------------------------------------------------------------
function ArtistCard({ artistId, artistExternalId }: { artistId?: string; artistExternalId?: string }) {
  const { data: spotifyProfile } = useArtistProfile('spotify', artistExternalId ?? '')
  const { data: libraryArtist } = useArtist(artistExternalId ? '' : (artistId ?? ''))

  if (artistExternalId) {
    // Spotify path — wait until profile loads
    if (!spotifyProfile) return null
    return (
      <div className="mt-3.5 overflow-hidden rounded-lg bg-raised">
        <div className="relative h-36">
          <Cover src={spotifyProfile.coverUrl} alt={spotifyProfile.name} size="full" rounded="md" />
          <div className="pointer-events-none absolute inset-0 bg-gradient-to-t from-black/60 to-transparent" />
          <span className="absolute bottom-3 left-4 text-lg font-bold text-text-primary">
            {spotifyProfile.name}
          </span>
        </div>
        <div className="p-3.5">
          <div className="text-sm font-semibold text-text-secondary">
            About the artist
          </div>
        </div>
      </div>
    )
  }

  // Library path
  if (!libraryArtist) return null

  const artistCoverSrc = libraryArtist.coverArtId ? coverUrl(libraryArtist.coverArtId, 300) : undefined

  return (
    <div className="mt-3.5 overflow-hidden rounded-lg bg-raised">
      {/* Artist image header */}
      <div className="relative h-36">
        <Cover src={artistCoverSrc} alt={libraryArtist.name} size="full" rounded="md" />
        <div className="pointer-events-none absolute inset-0 bg-gradient-to-t from-black/60 to-transparent" />
        <span className="absolute bottom-3 left-4 text-lg font-bold text-text-primary">
          {libraryArtist.name}
        </span>
      </div>
      {/* Body */}
      <div className="p-3.5">
        <div className="text-sm font-semibold text-text-secondary">
          In your library · {libraryArtist.albumCount} album{libraryArtist.albumCount !== 1 ? 's' : ''}
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

  const coverSrc = current ? trackCoverUrl(current, 300) || undefined : undefined

  const artistRoute = current?.artistExternalId
    ? `/artist/spotify/${current.artistExternalId}`
    : current?.artistId
      ? `/artist/library/${current.artistId}`
      : null

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
            {current?.artist && artistRoute ? (
              <button
                type="button"
                onClick={() => navigate(artistRoute)}
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
                  track={current}
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
                    src={trackCoverUrl(t, 48) || undefined}
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
        {(current?.artistExternalId || current?.artistId) && (
          <ArtistCard
            artistId={current.artistId}
            artistExternalId={current.artistExternalId}
          />
        )}
      </div>
    </aside>
  )
}
