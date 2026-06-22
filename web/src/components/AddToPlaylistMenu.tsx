import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { Icon } from './ui'
import {
  usePlaylists,
  createPlaylist,
  addTracksToPlaylist,
} from '../lib/libraryApi'

interface AddToPlaylistMenuProps {
  trackId: string
  onClose: () => void
}

const FOCUSABLE = 'button, [href], input, [tabindex]:not([tabindex="-1"])'

/**
 * AddToPlaylistMenu — small popover that adds the given track to one of the
 * user's library playlists, or to a freshly created one. Mirrors the
 * focus-trap / Esc / backdrop pattern of DownloadPopover.
 */
export function AddToPlaylistMenu({ trackId, onClose }: AddToPlaylistMenuProps) {
  const panelRef = useRef<HTMLDivElement>(null)
  const qc = useQueryClient()
  const navigate = useNavigate()
  const { data: playlists, isLoading } = usePlaylists()

  const [newName, setNewName] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Focus trap + Esc close (mirrors DownloadPopover).
  useEffect(() => {
    const previouslyFocused = document.activeElement as HTMLElement | null
    const panel = panelRef.current
    if (panel) {
      const focusable = Array.from(panel.querySelectorAll<HTMLElement>(FOCUSABLE))
      focusable[0]?.focus()
    }

    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        onClose()
        return
      }
      if (e.key === 'Tab' && panelRef.current) {
        const focusable = Array.from(
          panelRef.current.querySelectorAll<HTMLElement>(FOCUSABLE),
        ).filter((el) => !el.hasAttribute('disabled'))
        if (focusable.length === 0) return
        const first = focusable[0]
        const last = focusable[focusable.length - 1]
        if (e.shiftKey) {
          if (document.activeElement === first) {
            e.preventDefault()
            last.focus()
          }
        } else if (document.activeElement === last) {
          e.preventDefault()
          first.focus()
        }
      }
    }

    document.addEventListener('keydown', handleKey)
    return () => {
      document.removeEventListener('keydown', handleKey)
      previouslyFocused?.focus()
    }
  }, [onClose])

  function done(playlistId?: string) {
    qc.invalidateQueries({ queryKey: ['library', 'playlists'] })
    if (playlistId) {
      qc.invalidateQueries({ queryKey: ['playlist-detail', playlistId] })
    }
    onClose()
  }

  async function addToExisting(id: string) {
    if (busy) return
    setBusy(true)
    setError(null)
    try {
      await addTracksToPlaylist(id, [trackId])
      done(id)
    } catch {
      setError('Could not add to playlist.')
      setBusy(false)
    }
  }

  async function createAndAdd() {
    const name = newName.trim()
    if (!name || busy) return
    setBusy(true)
    setError(null)
    try {
      const pl = await createPlaylist(name)
      await addTracksToPlaylist(pl.id, [trackId])
      done(pl.id)
      navigate(`/playlist/${pl.id}`)
    } catch {
      setError('Could not create playlist.')
      setBusy(false)
    }
  }

  return (
    <>
      {/* Backdrop — click closes */}
      <div
        data-testid="add-to-playlist-backdrop"
        className="fixed inset-0 z-20"
        aria-hidden="true"
        onClick={onClose}
      />

      {/* Popover panel — anchored above the trigger (bottom-full) */}
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-label="Add to playlist"
        className="absolute bottom-full left-0 z-30 mb-2 w-64 rounded-xl border border-border-subtle bg-raised shadow-pop"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="px-3 pb-1 pt-3">
          <p className="text-sm font-bold text-text-primary">Add to playlist</p>
        </div>

        {/* New playlist — inline input */}
        <div className="px-3 pb-2 pt-1">
          <div className="flex items-center gap-2">
            <input
              type="text"
              value={newName}
              placeholder="New playlist"
              aria-label="New playlist name"
              disabled={busy}
              onChange={(e) => setNewName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault()
                  void createAndAdd()
                }
              }}
              className="min-w-0 flex-1 rounded-lg border border-border-subtle bg-surface px-2.5 py-1.5 text-sm text-text-primary placeholder:text-text-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
            />
            <button
              type="button"
              aria-label="Create playlist and add"
              disabled={busy || newName.trim() === ''}
              onClick={() => void createAndAdd()}
              className="inline-grid h-8 w-8 flex-none place-items-center rounded-lg bg-accent text-on-accent transition-opacity hover:opacity-90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:cursor-not-allowed disabled:opacity-40"
            >
              <Icon name="plus" className="text-base" />
            </button>
          </div>
        </div>

        {/* Existing playlists */}
        <ul className="max-h-60 overflow-y-auto p-1.5 pt-0" role="list">
          {isLoading && (
            <li className="px-2.5 py-2 text-xs text-text-muted">Loading playlists…</li>
          )}
          {!isLoading && (playlists?.length ?? 0) === 0 && (
            <li className="px-2.5 py-2 text-xs text-text-muted">
              No playlists yet — name one above to start.
            </li>
          )}
          {playlists?.map((pl) => (
            <li key={pl.id}>
              <button
                type="button"
                aria-label={`Add to ${pl.name}`}
                disabled={busy}
                onClick={() => void addToExisting(pl.id)}
                className="flex w-full items-center gap-3 rounded-lg px-2.5 py-2 text-left transition-colors hover:bg-raised-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent active:opacity-80 disabled:cursor-not-allowed disabled:opacity-40"
              >
                <span className="flex h-8 w-8 flex-none items-center justify-center rounded-lg bg-surface text-accent">
                  <Icon name="music" className="text-base" />
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm font-semibold text-text-primary">
                    {pl.name}
                  </span>
                  <span className="block text-xs text-text-muted">
                    {pl.songCount} {pl.songCount === 1 ? 'song' : 'songs'}
                  </span>
                </span>
              </button>
            </li>
          ))}
        </ul>

        {error && (
          <p className="px-3 pb-3 text-xs text-text-muted" role="alert">
            {error}
          </p>
        )}
      </div>
    </>
  )
}
