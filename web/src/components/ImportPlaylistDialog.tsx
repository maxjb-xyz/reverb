import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Button } from './ui/Button'
import { Toggle } from './ui/Toggle'
import { Segmented } from './ui/Segmented'
import { importPlaylist } from '../lib/syncedPlaylistApi'
import { importPlaylistOnce } from '../lib/libraryApi'
import { useAuthStore } from '../lib/authStore'

interface ImportPlaylistDialogProps {
  open: boolean
  onClose: () => void
  initialURL?: string
}

const FOCUSABLE = 'button, [href], input, [tabindex]:not([tabindex="-1"])'

export function ImportPlaylistDialog({ open, onClose, initialURL = '' }: ImportPlaylistDialogProps) {
  const navigate = useNavigate()
  const panelRef = useRef<HTMLDivElement>(null)
  const canAutoApprove = useAuthStore((s) => s.can('auto_approve'))

  const [mode, setMode] = useState<'sync' | 'one-time'>('sync')
  const [url, setUrl] = useState('')
  const [downloadMissing, setDownloadMissing] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Reset state whenever dialog opens
  useEffect(() => {
    if (open) {
      /* eslint-disable react-hooks/set-state-in-effect -- intentional: reset form fields when dialog reopens */
      setMode('sync')
      setUrl(initialURL)
      setDownloadMissing(false)
      setBusy(false)
      setError(null)
      /* eslint-enable react-hooks/set-state-in-effect */
    }
  }, [open, initialURL])

  // Focus trap + Esc close
  useEffect(() => {
    if (!open) return
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
  }, [open, onClose])

  if (!open) return null

  async function handleImport() {
    const trimmed = url.trim()
    if (!trimmed || busy) return
    setBusy(true)
    setError(null)
    try {
      if (mode === 'one-time') {
        const detail = await importPlaylistOnce(trimmed)
        onClose()
        navigate(`/playlist/${detail.id}`)
      } else {
        const detail = await importPlaylist(trimmed, canAutoApprove && downloadMissing)
        onClose()
        navigate(`/playlist/${detail.id}`)
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "Couldn't import — is the playlist public?")
      setBusy(false)
    }
  }

  const modeDescription =
    mode === 'sync'
      ? 'Live mirror — auto-updates from Spotify (read-only)'
      : 'Snapshot copied into your library — fully editable'

  return (
    <>
      {/* Backdrop */}
      <div
        data-testid="import-playlist-backdrop"
        className="fixed inset-0 z-40 bg-canvas/80 backdrop-blur-sm"
        aria-hidden="true"
        onClick={onClose}
      />

      {/* Centered dialog panel */}
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="import-dialog-title"
        className="fixed inset-0 z-50 flex items-center justify-center p-4"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="w-full max-w-md rounded-xl border border-border-subtle bg-raised shadow-pop animate-scale-in">
          <div className="space-y-5 p-6">
            {/* Heading */}
            <h2 id="import-dialog-title" className="text-lg font-extrabold tracking-tight text-text-primary">
              Import from Spotify
            </h2>

            {/* Mode toggle */}
            <div className="space-y-1.5">
              <Segmented
                options={[
                  { value: 'sync', label: 'Sync' },
                  { value: 'one-time', label: 'One-time' },
                ]}
                value={mode}
                onChange={setMode}
              />
              <p className="text-sm text-text-muted">{modeDescription}</p>
            </div>

            {/* URL input */}
            <div className="space-y-1.5">
              <label htmlFor="spotify-url" className="block text-sm font-semibold text-text-primary">
                Playlist URL
              </label>
              <input
                id="spotify-url"
                type="url"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault()
                    void handleImport()
                  }
                }}
                placeholder="Paste a public Spotify playlist link"
                disabled={busy}
                className="w-full rounded-lg border border-border-subtle bg-input px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:opacity-50"
              />
            </div>

            {/* Download missing toggle — sync mode, auto_approve users only */}
            {mode === 'sync' && canAutoApprove && (
              <div className="flex items-center gap-3">
                <Toggle
                  checked={downloadMissing}
                  onChange={setDownloadMissing}
                  label="Download missing now"
                />
                <span className="text-sm text-text-secondary">Download missing now</span>
              </div>
            )}

            {/* Inline error */}
            {error && (
              <p role="alert" className="text-sm text-accent">
                {error}
              </p>
            )}

            {/* Action buttons */}
            <div className="flex items-center justify-end gap-3 pt-1">
              <Button variant="ghost" onClick={onClose}>
                Cancel
              </Button>
              <Button
                variant="primary"
                onClick={() => void handleImport()}
                disabled={busy || url.trim() === ''}
                aria-label={busy ? 'Importing…' : 'Import'}
              >
                {busy ? 'Importing…' : 'Import'}
              </Button>
            </div>
          </div>
        </div>
      </div>
    </>
  )
}
