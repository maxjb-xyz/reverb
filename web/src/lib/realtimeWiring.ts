import { useEffect } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { RealtimeConnection, type WebSocketLike } from './realtime'
import { useDownloads } from './downloadStore'
import { getDownloads } from './downloadApi'
import { usePlayer } from './playerStore'
import type { DownloadEvent, LibraryUpdatedEvent, RealtimeEvent, Track } from './types'

// trackFromJob synthesizes a minimal library Track for play-when-ready auto-play,
// using the re-matched libraryTrackId (mirrors ExternalRow.trackFromMatch). The
// stream proxy plays by id; the rest is best-effort display metadata.
function trackFromJob(libraryTrackId: string, meta: { title?: string; album?: string; artist?: string; durationMs?: number; isrc?: string }): Track {
  return {
    id: libraryTrackId,
    title: meta.title ?? '',
    albumId: '',
    album: meta.album ?? '',
    artistId: '',
    artist: meta.artist ?? '',
    coverArtId: '',
    trackNumber: 0,
    discNumber: 0,
    durationMs: meta.durationMs ?? 0,
    bitRate: 0,
    suffix: '',
    contentType: '',
    isrc: meta.isrc,
  }
}

// useRealtime opens ONE app-wide WebSocket (distinct from the SSE search stream),
// fans typed events into the download store, drives TanStack invalidation, and
// auto-plays a completion whose job was started with playWhenReady. makeSocket is
// injectable for tests (a stub socket; no real network/media).
export function useRealtime(makeSocket?: (url: string) => WebSocketLike): void {
  const qc = useQueryClient()
  // Read the player action imperatively to avoid re-subscribing the effect.
  const playTrackList = usePlayer((s) => s.playTrackList)

  useEffect(() => {
    // Broad library invalidation is the MVP behavior; per-album/artist is a
    // best-effort optimization applied only when the id is present (deferred:
    // the backend may surface empty artistId/albumId on download.complete).
    function invalidateLibrary(ids?: { artistId?: string; albumId?: string }) {
      void qc.invalidateQueries({ queryKey: ['library'] })
      if (ids?.albumId) void qc.invalidateQueries({ queryKey: ['library', 'album', ids.albumId] })
      if (ids?.artistId) void qc.invalidateQueries({ queryKey: ['library', 'artist', ids.artistId] })
    }

    function onEvent(frame: RealtimeEvent) {
      switch (frame.type) {
        case 'download.queued':
        case 'download.progress':
        case 'download.failed': {
          useDownloads.getState().applyEvent(frame.payload as DownloadEvent)
          break
        }
        case 'download.complete': {
          const ev = frame.payload as DownloadEvent
          useDownloads.getState().applyEvent(ev)
          // After applying, read the job to see if it was play-when-ready.
          const job = useDownloads.getState().jobs[ev.jobId]
          const trackId = ev.libraryTrackId || job?.libraryTrackId || ''
          // playWhenReady auto-play: intentional forward-compat seam for M3; no UI
          // affordance sets playWhenReady yet — M4 may add a "download & play" control.
          if (job?.playWhenReady && trackId) {
            playTrackList(
              [trackFromJob(trackId, { title: job.title, album: job.album, artist: job.artist, isrc: job.isrc })],
              0,
            )
          }
          invalidateLibrary({ artistId: ev.artistId, albumId: ev.albumId })
          break
        }
        case 'library.updated': {
          const ev = frame.payload as LibraryUpdatedEvent
          const albumId = ev.albumIds?.[0]
          const artistId = ev.artistIds?.[0]
          invalidateLibrary({ artistId, albumId })
          break
        }
        default:
          break
      }
    }

    function onOpen() {
      // Resync the full job list on (re)connect so we never miss a transition.
      void getDownloads().then((jobs) => useDownloads.getState().setAll(jobs))
    }

    const conn = new RealtimeConnection({ onEvent, onOpen }, makeSocket)
    return () => conn.close()
    // playTrackList is stable (zustand action); makeSocket is test-only/stable.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [qc])

  // Polling fallback: while any download is active, refresh the job list on an
  // interval. The WebSocket is the primary channel, but a reverse proxy that
  // doesn't upgrade WebSocket connections would otherwise leave the UI frozen at
  // the optimistic "queued" state — this keeps it accurate regardless.
  const activeCount = useDownloads((s) => s.active().length)
  useEffect(() => {
    if (activeCount === 0) return
    const t = setInterval(() => {
      void getDownloads().then((jobs) => useDownloads.getState().setAll(jobs))
    }, 3000)
    return () => clearInterval(t)
  }, [activeCount])
}
