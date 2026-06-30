import { useEffect, useRef, useState } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import {
  useSyncedPlaylist,
  syncNow,
  downloadMissingForPlaylist,
  updateSyncSettings,
  deleteSyncedPlaylist,
  renameSyncedPlaylist,
  removeSyncedTrack,
  uploadPlaylistCover,
  reorderSyncedTracks,
} from '../lib/syncedPlaylistApi'
import type { TrackOrderEntry } from '../lib/syncedPlaylistApi'
import { TrackRow } from '../components/ui/TrackRow'
import { DownloadAction } from '../components/download/DownloadAction'
import { Button, IconButton, Cover, Skeleton, EmptyState, Badge, Toggle, Select, Icon } from '../components/ui'
import { PortalMenu } from '../components/PortalMenu'
import type { ExternalResult, ExternalTrackRef, AlbumDetailTrack, Track } from '../lib/types'
import { usePlayer } from '../lib/playerStore'
import { useAuthStore } from '../lib/authStore'
import { useAlbumPalette } from '../lib/useAlbumPalette'
import { rgbToCss } from '../lib/palette'

// ── Local helpers ─────────────────────────────────────────────────────────────

/** Relative human-readable time from a unix timestamp (seconds). */
function relativeTime(unixSeconds: number): string {
  if (!unixSeconds) return 'Never synced'
  const diffSec = Math.floor(Date.now() / 1000) - unixSeconds
  if (diffSec < 60) return 'just now'
  const diffMin = Math.floor(diffSec / 60)
  if (diffMin < 60) return `${diffMin}m ago`
  const diffHr = Math.floor(diffMin / 60)
  if (diffHr < 24) return `${diffHr}h ago`
  const diffDays = Math.floor(diffHr / 24)
  return `${diffDays}d ago`
}

/** Build a display Track from an AlbumDetailTrack. When the row is owned, thread the
 *  matched library track's ids through so the artist + album render as clickable links
 *  and the cover resolves; otherwise these fall back to '' (plain text, no link). */
function asTrack(t: AlbumDetailTrack): Track {
  return {
    id: '',
    title: t.title,
    album: t.album ?? '',
    albumId: t.libraryTrack?.albumId ?? '',
    artist: t.artist,
    artistId: t.libraryTrack?.artistId ?? '',
    coverArtId: t.libraryTrack?.coverArtId ?? '',
    trackNumber: t.trackNumber,
    discNumber: 1,
    durationMs: t.durationMs,
    bitRate: 0,
    suffix: '',
    contentType: '',
    ...(t.artistExternalId ? { artistExternalId: t.artistExternalId } : {}),
  }
}

/** Build an ExternalResult from an ExternalTrackRef so DownloadAction can drive it. */
function refToExternalResult(ref: ExternalTrackRef, playlistName: string): ExternalResult {
  return {
    source: ref.source,
    externalId: ref.externalId,
    title: ref.title,
    artist: ref.artist ?? '',
    album: ref.album ?? playlistName,
    durationMs: ref.durationMs,
    isrc: ref.isrc,
    type: 'track',
  }
}

const INTERVAL_OPTIONS = [
  { value: '0', label: 'Manual' },
  { value: '86400', label: 'Daily' },
  { value: '604800', label: 'Weekly' },
]

// ── Component ─────────────────────────────────────────────────────────────────

export default function SyncedPlaylist() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const qc = useQueryClient()
  const { data: detail, isLoading, isError } = useSyncedPlaylist(id)
  const playTrackList = usePlayer((s) => s.playTrackList)
  const currentTrack = usePlayer((s) => s.current)
  const isPlaying = usePlayer((s) => s.playing)
  // "Download all missing" is a direct download — gated on auto_approve. There is
  // no bulk-request path here; a user without auto_approve sees no bulk-download
  // button (per-item DownloadAction on missing rows is unaffected).
  const canAutoApprove = useAuthStore((s) => s.can('auto_approve'))

  // "…" menu state
  const [menuOpen, setMenuOpen] = useState(false)
  const menuTriggerRef = useRef<HTMLDivElement>(null)

  // Inline title edit state
  const [editingName, setEditingName] = useState(false)
  const [nameInput, setNameInput] = useState('')
  const nameInputRef = useRef<HTMLInputElement>(null)
  // Set synchronously by Escape so the blur-triggered handleRename can tell a
  // cancel from a save (React state wouldn't flush before the blur fires).
  const renameCancelledRef = useRef(false)

  // Schedule settings local state — seeded from detail once loaded
  const [syncEnabled, setSyncEnabled] = useState<boolean | null>(null)
  const [intervalSec, setIntervalSec] = useState<number | null>(null)
  const [autoDownload, setAutoDownload] = useState<boolean | null>(null)

  // Cover upload state
  const coverInputRef = useRef<HTMLInputElement>(null)
  const [coverUploading, setCoverUploading] = useState(false)
  const [coverError, setCoverError] = useState<string | null>(null)

  // Drag-reorder state: optimistic local ordering of track indices
  const [trackOrder, setTrackOrder] = useState<number[] | null>(null)
  const dragSourceIdx = useRef<number | null>(null)

  // Seed local state from detail once it loads / changes
  useEffect(() => {
    if (!detail) return
    /* eslint-disable react-hooks/set-state-in-effect -- intentional: seed local form state when server record loads */
    setSyncEnabled(detail.syncEnabled)
    setIntervalSec(detail.syncIntervalSec)
    setAutoDownload(detail.autoDownload)
    setTrackOrder(null) // reset optimistic order when playlist changes
    /* eslint-enable react-hooks/set-state-in-effect */
    // eslint-disable-next-line react-hooks/exhaustive-deps -- intentional: re-seed only when the playlist id changes, not on every detail refresh
  }, [detail?.id])

  const palette = useAlbumPalette(detail?.coverUrl)

  // ── Loading / error states ──────────────────────────────────────────────────

  if (isLoading) {
    return (
      <div data-testid="synced-playlist-skeleton" className="space-y-6">
        <header className="flex items-end gap-6 pt-4">
          <Skeleton className="h-52 w-52 flex-none" rounded="md" />
          <div className="flex-1 space-y-3 pb-2">
            <Skeleton className="h-3 w-24" />
            <Skeleton className="h-10 w-64" />
            <Skeleton className="h-3 w-48" />
            <Skeleton className="h-10 w-28 rounded-full" rounded="md" />
          </div>
        </header>
        <div className="space-y-1">
          {Array.from({ length: 8 }).map((_, i) => (
            <Skeleton key={i} className="h-14 w-full" rounded="md" />
          ))}
        </div>
      </div>
    )
  }

  if (isError || !detail) {
    return (
      <EmptyState
        icon="browse"
        title="Playlist not found"
        hint="This synced playlist may have been removed."
      />
    )
  }

  // ── Derived data ────────────────────────────────────────────────────────────

  const tracks = detail.tracks ?? []

  const ownedTracks: Track[] = tracks
    .filter((t) => t.state === 'full' && t.libraryTrack)
    .map((t) => ({ ...t.libraryTrack!, ...(t.artistExternalId ? { artistExternalId: t.artistExternalId } : {}) }))

  const missingCount = tracks.filter((t) => t.state === 'none').length

  const ownedIndexMap = new Map<string, number>(
    ownedTracks.map((t, i) => [t.id, i]),
  )

  // Resolved local settings (fall back to detail values)
  const effectiveSyncEnabled = syncEnabled ?? detail.syncEnabled
  const effectiveIntervalSec = intervalSec ?? detail.syncIntervalSec
  const effectiveAutoDownload = autoDownload ?? detail.autoDownload

  // ── Mutation helpers ────────────────────────────────────────────────────────

  async function handleSyncNow() {
    try {
      await syncNow(id)
      qc.invalidateQueries({ queryKey: ['synced-playlist', id] })
    } catch (err) {
      console.error('Sync failed:', err)
    }
  }

  async function handleDownloadMissing() {
    try {
      await downloadMissingForPlaylist(id)
    } catch (err) {
      console.error('Download missing failed:', err)
    }
  }

  async function handleUpdateSettings(patch: { syncEnabled?: boolean; intervalSec?: number; autoDownload?: boolean }) {
    const next = {
      syncEnabled: patch.syncEnabled ?? effectiveSyncEnabled,
      intervalSec: patch.intervalSec ?? effectiveIntervalSec,
      autoDownload: patch.autoDownload ?? effectiveAutoDownload,
    }
    try {
      await updateSyncSettings(id, next)
      qc.invalidateQueries({ queryKey: ['synced-playlist', id] })
    } catch (err) {
      console.error('Failed to update sync settings:', err)
    }
  }

  async function handleRename() {
    setEditingName(false)
    if (renameCancelledRef.current) {
      renameCancelledRef.current = false
      return
    }
    const trimmed = nameInput.trim()
    if (!trimmed || trimmed === detail?.name) return
    try {
      await renameSyncedPlaylist(id, trimmed)
      void qc.invalidateQueries({ queryKey: ['synced-playlist', id] })
    } catch {
      // silent — title will revert on next render
    }
  }

  async function handleDelete() {
    setMenuOpen(false)
    if (!window.confirm(`Remove synced playlist "${detail!.name}"?`)) return
    try {
      await deleteSyncedPlaylist(id)
      qc.invalidateQueries({ queryKey: ['synced-playlists'] })
      navigate('/library')
    } catch (err) {
      console.error('Failed to delete synced playlist:', err)
    }
  }

  async function handleRemoveTrack(source: string, externalId: string) {
    try {
      await removeSyncedTrack(id, source, externalId)
      qc.invalidateQueries({ queryKey: ['synced-playlist', id] })
    } catch (err) {
      console.error('Failed to remove track:', err)
    }
  }

  async function handleCoverFileChange(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    if (!file) return
    setCoverError(null)
    setCoverUploading(true)
    try {
      await uploadPlaylistCover(id, file)
      qc.invalidateQueries({ queryKey: ['synced-playlist', id] })
    } catch {
      setCoverError("Couldn't upload — try a smaller image")
    } finally {
      setCoverUploading(false)
      // Reset so the same file can be re-selected
      if (coverInputRef.current) coverInputRef.current.value = ''
    }
  }

  // Build the track order payload from current (possibly reordered) tracks
  function buildTrackOrderPayload(orderedTracks: AlbumDetailTrack[]): TrackOrderEntry[] {
    return orderedTracks
      .filter((t) => t.key)
      .map((t) => ({ source: t.key!.source, externalId: t.key!.externalId }))
  }

  function handleDragStart(idx: number) {
    dragSourceIdx.current = idx
  }

  function handleDragOver(e: React.DragEvent<HTMLDivElement>, idx: number) {
    e.preventDefault()
    const from = dragSourceIdx.current
    if (from === null || from === idx || !detail) return
    const currentOrder = trackOrder ?? tracks.map((_, i) => i)
    const next = [...currentOrder]
    const [moved] = next.splice(from, 1)
    next.splice(idx, 0, moved)
    dragSourceIdx.current = idx
    setTrackOrder(next)
  }

  async function handleDrop(e: React.DragEvent<HTMLDivElement>) {
    e.preventDefault()
    dragSourceIdx.current = null
    if (!trackOrder || !detail) return
    const orderedTracks = trackOrder.map((i) => tracks[i])
    const order = buildTrackOrderPayload(orderedTracks)
    try {
      await reorderSyncedTracks(id, order)
      qc.invalidateQueries({ queryKey: ['synced-playlist', id] })
    } catch (err) {
      console.error('Failed to reorder tracks:', err)
      // Restore server order on failure
      setTrackOrder(null)
      qc.invalidateQueries({ queryKey: ['synced-playlist', id] })
    }
  }

  function handleDragEnd() {
    dragSourceIdx.current = null
  }

  // ── Render ──────────────────────────────────────────────────────────────────

  return (
    <div className="space-y-6">
      {/* Gradient wash header */}
      <div
        className="relative -mx-4 -mt-4 px-4 pt-4 pb-6 rounded-b-2xl overflow-hidden bg-gradient-to-b from-raised to-transparent"
        style={palette ? { background: `linear-gradient(to bottom, ${rgbToCss(palette.rgb, 0.55)} 0%, transparent 100%)` } : undefined}
      >
        <header className="relative z-10 flex items-end gap-6 pt-2">
          {/* Cover — interactive (change-cover) for mode='once' */}
          <div className="relative flex-none group/cover">
            <Cover
              src={detail.coverUrl}
              alt={detail.name}
              size={208}
              rounded="md"
              className="shadow-cover"
            />
            {detail.mode === 'once' && (
              <>
                <button
                  type="button"
                  aria-label="Change cover"
                  disabled={coverUploading}
                  onClick={() => coverInputRef.current?.click()}
                  className="absolute inset-0 flex flex-col items-center justify-center gap-1 rounded-md bg-black/0 group-hover/cover:bg-black/50 transition-colors opacity-0 group-hover/cover:opacity-100 focus-visible:opacity-100 focus-visible:bg-black/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent text-white cursor-pointer disabled:cursor-wait"
                >
                  <Icon name="camera" className="text-2xl" />
                  <span className="text-xs font-semibold">
                    {coverUploading ? 'Uploading…' : 'Change cover'}
                  </span>
                </button>
                <input
                  ref={coverInputRef}
                  type="file"
                  accept="image/png,image/jpeg,image/webp"
                  className="sr-only"
                  aria-label="Upload cover image"
                  onChange={(e) => void handleCoverFileChange(e)}
                  data-testid="cover-file-input"
                />
              </>
            )}
          </div>
          {coverError && (
            <p role="alert" className="absolute bottom-2 left-0 right-0 text-center text-xs text-red-400">
              {coverError}
            </p>
          )}
          <div className="min-w-0 pb-1">
            <div className="flex items-center gap-2 mb-1">
              <span className="text-xs font-semibold uppercase tracking-widest text-text-muted">
                {detail.source === 'spotify' && detail.mode === 'synced' ? 'Synced playlist' : 'Playlist'}
              </span>
              {detail.source === 'spotify' && (
                <Badge kind="status" tone="success">
                  {detail.source}
                </Badge>
              )}
            </div>
            {editingName ? (
              <input
                ref={nameInputRef}
                value={nameInput}
                aria-label="Playlist name"
                className="text-4xl font-black leading-tight tracking-tight text-text-primary bg-transparent border-b border-text-primary outline-none w-full truncate"
                onChange={(e) => setNameInput(e.target.value)}
                onBlur={() => void handleRename()}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') { e.currentTarget.blur() }
                  if (e.key === 'Escape') { renameCancelledRef.current = true; e.currentTarget.blur() }
                }}
                autoFocus
              />
            ) : (
              <h1
                className="text-4xl font-black leading-tight tracking-tight text-text-primary truncate cursor-pointer hover:opacity-80 transition-opacity"
                onClick={() => {
                  setNameInput(detail.name)
                  setEditingName(true)
                }}
                title="Click to rename"
              >
                {detail.name}
              </h1>
            )}
            <div className="mt-2 text-sm text-text-secondary flex flex-wrap items-center gap-x-1">
              <span>
                {detail.source === 'local'
                  ? `${detail.totalCount} song${detail.totalCount !== 1 ? 's' : ''}`
                  : `${detail.ownedCount} of ${detail.totalCount} in library`}
              </span>
              {detail.source !== 'local' && missingCount > 0 && (
                <span className="text-accent">· {missingCount} missing</span>
              )}
            </div>
            {detail.source === 'spotify' && detail.mode === 'synced' && (
              <div className="mt-1 text-xs text-text-muted">
                Synced {relativeTime(detail.lastSyncedAt)}
              </div>
            )}
            <div className="mt-4 flex items-center gap-3 flex-wrap">
              <Button
                variant="primary"
                size="md"
                disabled={ownedTracks.length === 0}
                onClick={() => ownedTracks.length && playTrackList(ownedTracks, 0)}
                aria-label={`Play ${detail.name}`}
              >
                Play
              </Button>
              {canAutoApprove && missingCount > 0 && (
                <Button
                  variant="secondary"
                  size="md"
                  onClick={() => void handleDownloadMissing()}
                  aria-label={`Download all missing · ${missingCount}`}
                >
                  Download all missing · {missingCount}
                </Button>
              )}
              {detail.mode !== 'once' && (
                <Button
                  variant="secondary"
                  size="md"
                  onClick={() => void handleSyncNow()}
                  aria-label="Sync now"
                >
                  Sync now
                </Button>
              )}
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
                  label="Synced playlist options"
                  widthClass="w-72"
                >
                  {/* Schedule settings panel — hidden for one-time imports */}
                  {detail.mode !== 'once' && (
                    <div className="px-4 py-3 space-y-3 border-b border-border-subtle">
                      <p className="text-xs font-semibold uppercase tracking-widest text-text-muted">
                        Schedule
                      </p>
                      <div className="flex items-center justify-between gap-3">
                        <span className="text-sm text-text-primary">Auto-sync</span>
                        <Toggle
                          checked={effectiveSyncEnabled}
                          label="Auto-sync"
                          onChange={(v) => {
                            setSyncEnabled(v)
                            void handleUpdateSettings({ syncEnabled: v })
                          }}
                        />
                      </div>
                      <div className="flex items-center justify-between gap-3">
                        <span className="text-sm text-text-primary">Interval</span>
                        <Select
                          value={String(effectiveIntervalSec)}
                          options={INTERVAL_OPTIONS}
                          label="Sync interval"
                          onChange={(v) => {
                            const sec = Number(v)
                            setIntervalSec(sec)
                            void handleUpdateSettings({ intervalSec: sec })
                          }}
                        />
                      </div>
                      <div className="flex items-center justify-between gap-3">
                        <span className="text-sm text-text-primary">Auto-download missing</span>
                        <Toggle
                          checked={effectiveAutoDownload}
                          label="Auto-download missing"
                          onChange={(v) => {
                            setAutoDownload(v)
                            void handleUpdateSettings({ autoDownload: v })
                          }}
                        />
                      </div>
                    </div>
                  )}
                  <button
                    type="button"
                    role="menuitem"
                    onClick={() => void handleDelete()}
                    className="flex w-full items-center gap-3 rounded-b-xl px-4 py-2.5 text-sm text-text-primary hover:bg-raised-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
                  >
                    Remove
                  </button>
                </PortalMenu>
              )}
            </div>
          </div>
        </header>
      </div>

      {/* Track list */}
      <div className="space-y-0.5">
        {(trackOrder ?? tracks.map((_, i) => i)).map((origIdx, displayIdx) => {
          const t = tracks[origIdx]
          const isDraggable = detail.mode === 'once'

          const dragHandle = isDraggable
            ? (
              <div
                aria-label="Drag to reorder"
                className="opacity-0 group-hover:opacity-100 flex items-center px-1 text-text-muted cursor-grab active:cursor-grabbing focus-visible:opacity-100 transition-opacity"
              >
                <Icon name="grip" className="text-base" />
              </div>
            )
            : undefined

          const dragProps = isDraggable
            ? {
              draggable: true,
              onDragStart: () => handleDragStart(displayIdx),
              onDragOver: (e: React.DragEvent<HTMLDivElement>) => handleDragOver(e, displayIdx),
              onDrop: (e: React.DragEvent<HTMLDivElement>) => void handleDrop(e),
              onDragEnd: handleDragEnd,
            }
            : {}

          if (t.state === 'full' && t.libraryTrack) {
            const ownedIdx = ownedIndexMap.get(t.libraryTrack.id) ?? 0
            const isActive = currentTrack?.id === t.libraryTrack.id
            return (
              <div key={t.libraryTrack.id} className="flex items-center group" {...dragProps}>
                {dragHandle}
                <div className="flex-1 min-w-0">
                  <TrackRow
                    track={t.libraryTrack}
                    index={origIdx}
                    active={isActive}
                    playing={isActive ? isPlaying : undefined}
                    onPlay={() => playTrackList(ownedTracks, ownedIdx)}
                    coverSrc={t.libraryTrack?.coverArtId ? undefined : t.coverUrl}
                    artistTo={t.artistExternalId ? `/artist/spotify/${t.artistExternalId}` : undefined}
                    albumTo={t.albumExternalId ? `/album/spotify/${t.albumExternalId}` : undefined}
                    right={
                      <div className="flex items-center gap-1 group">
                        {detail.mode === 'once' && t.key && (
                          <button
                            type="button"
                            aria-label={`Remove ${t.title} from playlist`}
                            className="opacity-0 group-hover:opacity-100 transition-opacity rounded p-1 text-text-muted hover:text-text-primary focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
                            onClick={(e) => { e.stopPropagation(); void handleRemoveTrack(t.key!.source, t.key!.externalId) }}
                          >
                            <Icon name="x" className="text-xs" />
                          </button>
                        )}
                        <Badge kind="in-library">
                          <Icon name="check" className="text-xs" />
                          In Library
                        </Badge>
                      </div>
                    }
                    rightWidth={detail.mode === 'once' ? '156px' : '120px'}
                  />
                </div>
              </div>
            )
          }

          // Non-owned tracks: display row with DownloadAction right slot
          const displayTrack = asTrack(t)
          const missingArtistTo = t.artistExternalId ? `/artist/spotify/${t.artistExternalId}` : undefined
          const missingAlbumTo = t.albumExternalId ? `/album/spotify/${t.albumExternalId}` : undefined
          const missingArtistNode = missingArtistTo
            ? (
              <Link
                to={missingArtistTo}
                onClick={(e) => e.stopPropagation()}
                onDoubleClick={(e) => e.stopPropagation()}
                className="hover:underline focus-visible:outline-none focus-visible:underline"
              >
                {t.artist}
              </Link>
            )
            : undefined
          const missingAlbumNode = missingAlbumTo
            ? (
              <Link
                to={missingAlbumTo}
                onClick={(e) => e.stopPropagation()}
                onDoubleClick={(e) => e.stopPropagation()}
                className="hover:underline focus-visible:outline-none focus-visible:underline"
              >
                {t.album ?? ''}
              </Link>
            )
            : undefined
          const downloadAction = t.externalRef
            ? (
              <DownloadAction
                result={refToExternalResult(t.externalRef, detail.name)}
                onPlay={(libraryTrackId) => playTrackList([{ ...asTrack(t), id: libraryTrackId }], 0)}
              />
            )
            : undefined
          const right = (t.key || downloadAction)
            ? (
              <div className="flex items-center gap-1 group">
                {detail.mode === 'once' && t.key && (
                  <button
                    type="button"
                    aria-label={`Remove ${t.title} from playlist`}
                    className="opacity-0 group-hover:opacity-100 transition-opacity rounded p-1 text-text-muted hover:text-text-primary focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
                    onClick={(e) => { e.stopPropagation(); void handleRemoveTrack(t.key!.source, t.key!.externalId) }}
                  >
                    <Icon name="x" className="text-xs" />
                  </button>
                )}
                {downloadAction}
              </div>
            )
            : undefined
          return (
            <div key={t.libraryTrack?.id ?? t.key?.externalId ?? origIdx} className="flex items-center group" {...dragProps}>
              {dragHandle}
              <div className="flex-1 min-w-0">
                <TrackRow
                  track={displayTrack}
                  index={origIdx}
                  onPlay={() => {}}
                  coverSrc={t.coverUrl ?? detail.coverUrl}
                  artistNode={missingArtistNode}
                  albumNode={missingAlbumNode}
                  right={right}
                  rightWidth={right ? (detail.mode === 'once' ? '156px' : '120px') : undefined}
                />
              </div>
            </div>
          )
        })}
        {tracks.length === 0 && (
          <EmptyState icon="browse" title="No tracks in this playlist" />
        )}
      </div>
    </div>
  )
}
