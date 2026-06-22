import { useRef, useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { usePlaylistDetail } from '../lib/coverageApi'
import { coverUrl, renamePlaylist, deletePlaylist, removePlaylistTrack } from '../lib/libraryApi'
import { formatDuration } from '../lib/types'
import type { Track } from '../lib/types'
import { usePlayer } from '../lib/playerStore'
import { TrackRow } from '../components/ui/TrackRow'
import { Button, IconButton, Cover, Skeleton, EmptyState } from '../components/ui'
import { useAlbumPalette } from '../lib/useAlbumPalette'
import { rgbToCss } from '../lib/palette'
import { PortalMenu } from '../components/PortalMenu'

export default function Playlist() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const qc = useQueryClient()
  const { data: playlist, isLoading, isError } = usePlaylistDetail(id)
  const playTrackList = usePlayer((s) => s.playTrackList)
  const toggleShuffle = usePlayer((s) => s.toggleShuffle)
  const shuffle = usePlayer((s) => s.shuffle)

  // "…" menu state
  const [menuOpen, setMenuOpen] = useState(false)
  const menuTriggerRef = useRef<HTMLDivElement>(null)
  // Inline rename state
  const [renaming, setRenaming] = useState(false)
  const [renameValue, setRenameValue] = useState('')
  const palette = useAlbumPalette(playlist?.coverArtId ? coverUrl(playlist.coverArtId, 300) : undefined)

  if (isLoading) {
    return (
      <div data-testid="playlist-skeleton" className="space-y-6">
        {/* Header skeleton */}
        <header className="flex items-end gap-6 pt-4">
          <Skeleton className="h-52 w-52 flex-none" rounded="md" />
          <div className="flex-1 space-y-3 pb-2">
            <Skeleton className="h-3 w-12" />
            <Skeleton className="h-10 w-64" />
            <Skeleton className="h-3 w-48" />
            <Skeleton className="h-10 w-28 rounded-full" rounded="md" />
          </div>
        </header>
        {/* Track row skeletons */}
        <div className="space-y-1">
          {Array.from({ length: 8 }).map((_, i) => (
            <Skeleton key={i} className="h-14 w-full" rounded="md" />
          ))}
        </div>
      </div>
    )
  }

  if (isError || !playlist) {
    return (
      <EmptyState
        icon="browse"
        title="Playlist not found"
        hint="This playlist may have been removed from your library."
      />
    )
  }

  const tracks: Track[] = playlist.tracks ?? []
  const hasTracks = tracks.length > 0

  const coverSrc = playlist.coverArtId ? coverUrl(playlist.coverArtId, 300) : undefined

  function openRename() {
    setRenameValue(playlist!.name)
    setRenaming(true)
    setMenuOpen(false)
  }

  async function commitRename() {
    const name = renameValue.trim()
    if (!name || name === playlist!.name) {
      setRenaming(false)
      return
    }
    try {
      await renamePlaylist(id, name)
      setRenaming(false)
      qc.invalidateQueries({ queryKey: ['playlist-detail', id] })
      qc.invalidateQueries({ queryKey: ['library', 'playlists'] })
    } catch (err) {
      console.error('Failed to rename playlist:', err)
      // Keep the input open so the user can retry
    }
  }

  function cancelRename() {
    setRenaming(false)
  }

  async function handleDelete() {
    setMenuOpen(false)
    if (!window.confirm(`Delete playlist "${playlist!.name}"?`)) return
    try {
      await deletePlaylist(id)
      qc.invalidateQueries({ queryKey: ['library', 'playlists'] })
      navigate('/library')
    } catch (err) {
      console.error('Failed to delete playlist:', err)
      // Stay on the page; the playlist remains visible
    }
  }

  async function handleRemoveTrack(index: number) {
    try {
      await removePlaylistTrack(id, index)
      qc.invalidateQueries({ queryKey: ['playlist-detail', id] })
    } catch (err) {
      console.error('Failed to remove track from playlist:', err)
    }
  }

  return (
    <div className="space-y-6">
      {/* Subtle gradient wash behind header */}
      <div
        className="relative -mx-4 -mt-4 px-4 pt-4 pb-6 rounded-b-2xl overflow-hidden bg-gradient-to-b from-raised to-transparent"
        style={palette ? { background: `linear-gradient(to bottom, ${rgbToCss(palette.rgb, 0.55)} 0%, transparent 100%)` } : undefined}
      >
        <header className="relative z-10 flex items-end gap-6 pt-2">
          <Cover
            src={coverSrc}
            alt={playlist.name}
            size={208}
            rounded="md"
            className="shadow-cover flex-none"
          />
          <div className="min-w-0 pb-1">
            <div className="text-xs font-semibold uppercase tracking-widest text-text-muted mb-1">
              Playlist
            </div>
            {renaming ? (
              <input
                autoFocus
                type="text"
                value={renameValue}
                aria-label="Rename playlist"
                onChange={(e) => setRenameValue(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') { e.preventDefault(); void commitRename() }
                  if (e.key === 'Escape') { e.preventDefault(); cancelRename() }
                }}
                onBlur={() => void commitRename()}
                className="text-4xl font-black leading-tight tracking-tight text-text-primary bg-transparent border-b border-accent focus-visible:outline-none w-full"
              />
            ) : (
              <h1 className="text-4xl font-black leading-tight tracking-tight text-text-primary truncate">
                {playlist.name}
              </h1>
            )}
            <div className="mt-2 text-sm text-text-secondary flex flex-wrap items-center gap-x-1">
              {playlist.songCount > 0 ? <span>{playlist.songCount} songs</span> : null}
              {playlist.durationMs > 0 ? <span>· {formatDuration(playlist.durationMs)}</span> : null}
            </div>
            <div className="mt-4 flex items-center gap-3">
              <Button
                variant="primary"
                size="md"
                disabled={!hasTracks}
                onClick={() => hasTracks && playTrackList(tracks, 0)}
                aria-label={`Play ${playlist.name}`}
              >
                Play
              </Button>
              <IconButton
                name="shuffle"
                label={`Shuffle ${playlist.name}`}
                onClick={() => {
                  if (!hasTracks) return
                  if (!shuffle) toggleShuffle()
                  playTrackList(tracks, 0)
                }}
                disabled={!hasTracks}
              />
              {/* "…" overflow menu — rendered via portal to escape scroll-container clip */}
              <div ref={menuTriggerRef} className="inline-flex">
                <IconButton
                  name="down"
                  label="More options"
                  onClick={() => setMenuOpen((o) => !o)}
                  aria-label="More options"
                />
              </div>
              {menuOpen && (
                <PortalMenu
                  triggerRef={menuTriggerRef}
                  onClose={() => setMenuOpen(false)}
                  label="Playlist options"
                  widthClass="w-48"
                >
                  <button
                    type="button"
                    role="menuitem"
                    onClick={openRename}
                    className="flex w-full items-center gap-3 rounded-t-xl px-3 py-2.5 text-sm text-text-primary hover:bg-raised-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
                  >
                    Rename
                  </button>
                  <button
                    type="button"
                    role="menuitem"
                    onClick={() => void handleDelete()}
                    className="flex w-full items-center gap-3 rounded-b-xl px-3 py-2.5 text-sm text-text-primary hover:bg-raised-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
                  >
                    Delete playlist
                  </button>
                </PortalMenu>
              )}
            </div>
          </div>
        </header>
      </div>

      {/* Track list */}
      <div className="space-y-0.5">
        {tracks.map((t, i) => (
          <TrackRow
            key={t.id}
            track={t}
            index={i}
            onPlay={() => playTrackList(tracks, i)}
            right={
              <IconButton
                name="x"
                size="sm"
                label={`Remove ${t.title} from playlist`}
                className="opacity-0 group-hover:opacity-100 transition-opacity"
                onClick={(e) => {
                  e.stopPropagation()
                  void handleRemoveTrack(i)
                }}
              />
            }
          />
        ))}
        {tracks.length === 0 && (
          <EmptyState icon="browse" title="No tracks in this playlist" />
        )}
      </div>
    </div>
  )
}
