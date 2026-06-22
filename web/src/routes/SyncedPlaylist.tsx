import { useEffect, useRef, useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import {
  useSyncedPlaylist,
  syncNow,
  downloadMissingForPlaylist,
  updateSyncSettings,
  deleteSyncedPlaylist,
} from '../lib/syncedPlaylistApi'
import { TrackRow } from '../components/ui/TrackRow'
import { DownloadAction } from '../components/download/DownloadAction'
import { Button, IconButton, Cover, Skeleton, EmptyState, Badge, Toggle, Select } from '../components/ui'
import { PortalMenu } from '../components/PortalMenu'
import type { ExternalResult, ExternalTrackRef, AlbumDetailTrack, Track } from '../lib/types'
import { usePlayer } from '../lib/playerStore'
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

/** Build a display-only Track from a missing AlbumDetailTrack (no library id). */
function asTrack(t: AlbumDetailTrack): Track {
  return {
    id: '',
    title: t.title,
    album: t.album ?? '',
    albumId: '',
    artist: t.artist,
    artistId: '',
    coverArtId: '',
    trackNumber: t.trackNumber,
    discNumber: 1,
    durationMs: t.durationMs,
    bitRate: 0,
    suffix: '',
    contentType: '',
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

  // "…" menu state
  const [menuOpen, setMenuOpen] = useState(false)
  const menuTriggerRef = useRef<HTMLDivElement>(null)

  // Schedule settings local state — seeded from detail once loaded
  const [syncEnabled, setSyncEnabled] = useState<boolean | null>(null)
  const [intervalSec, setIntervalSec] = useState<number | null>(null)
  const [autoDownload, setAutoDownload] = useState<boolean | null>(null)

  // Seed local state from detail once it loads / changes
  useEffect(() => {
    if (!detail) return
    /* eslint-disable react-hooks/set-state-in-effect -- intentional: seed local form state when server record loads */
    setSyncEnabled(detail.syncEnabled)
    setIntervalSec(detail.syncIntervalSec)
    setAutoDownload(detail.autoDownload)
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

  const ownedTracks: Track[] = detail.tracks
    .filter((t) => t.state === 'full' && t.libraryTrack)
    .map((t) => t.libraryTrack!)

  const missingCount = detail.tracks.filter((t) => t.state === 'none').length

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

  // ── Render ──────────────────────────────────────────────────────────────────

  return (
    <div className="space-y-6">
      {/* Gradient wash header */}
      <div
        className="relative -mx-4 -mt-4 px-4 pt-4 pb-6 rounded-b-2xl overflow-hidden bg-gradient-to-b from-raised to-transparent"
        style={palette ? { background: `linear-gradient(to bottom, ${rgbToCss(palette.rgb, 0.55)} 0%, transparent 100%)` } : undefined}
      >
        <header className="relative z-10 flex items-end gap-6 pt-2">
          <Cover
            src={detail.coverUrl}
            alt={detail.name}
            size={208}
            rounded="md"
            className="shadow-cover flex-none"
          />
          <div className="min-w-0 pb-1">
            <div className="flex items-center gap-2 mb-1">
              <span className="text-xs font-semibold uppercase tracking-widest text-text-muted">
                Synced playlist
              </span>
              <Badge kind="status" tone="success">
                {detail.source}
              </Badge>
            </div>
            <h1 className="text-4xl font-black leading-tight tracking-tight text-text-primary truncate">
              {detail.name}
            </h1>
            <div className="mt-2 text-sm text-text-secondary flex flex-wrap items-center gap-x-1">
              <span>
                {detail.ownedCount} of {detail.totalCount} in library
              </span>
              {missingCount > 0 && (
                <span className="text-accent">· {missingCount} missing</span>
              )}
            </div>
            <div className="mt-1 text-xs text-text-muted">
              Synced {relativeTime(detail.lastSyncedAt)}
            </div>
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
              {missingCount > 0 && (
                <Button
                  variant="secondary"
                  size="md"
                  onClick={() => void handleDownloadMissing()}
                  aria-label={`Download all missing · ${missingCount}`}
                >
                  Download all missing · {missingCount}
                </Button>
              )}
              <Button
                variant="secondary"
                size="md"
                onClick={() => void handleSyncNow()}
                aria-label="Sync now"
              >
                Sync now
              </Button>
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
                  {/* Schedule settings panel */}
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
        {detail.tracks.map((t, i) => {
          if (t.state === 'full' && t.libraryTrack) {
            const ownedIdx = ownedIndexMap.get(t.libraryTrack.id) ?? 0
            return (
              <TrackRow
                key={t.libraryTrack.id}
                track={t.libraryTrack}
                index={i}
                onPlay={() => playTrackList(ownedTracks, ownedIdx)}
                coverSrc={t.libraryTrack?.coverArtId ? undefined : t.coverUrl}
              />
            )
          }

          // Non-owned tracks: display row with DownloadAction right slot
          const displayTrack = asTrack(t)
          const right = t.externalRef
            ? (
              <DownloadAction
                result={refToExternalResult(t.externalRef, detail.name)}
                onPlay={(libraryTrackId) => playTrackList([{ ...asTrack(t), id: libraryTrackId }], 0)}
              />
            )
            : undefined
          return (
            <TrackRow
              key={t.libraryTrack?.id ?? t.externalRef?.externalId ?? i}
              track={displayTrack}
              index={i}
              onPlay={() => {}}
              coverSrc={t.coverUrl ?? detail.coverUrl}
              right={right}
              rightWidth={right ? '120px' : undefined}
            />
          )
        })}
        {detail.tracks.length === 0 && (
          <EmptyState icon="browse" title="No tracks in this playlist" />
        )}
      </div>
    </div>
  )
}
