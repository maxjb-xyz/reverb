import { useEffect } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { RealtimeConnection, type WebSocketLike } from './realtime'
import { useDownloads } from './downloadStore'
import { useLibraryRevision } from './libraryRevisionStore'
import { getDownloads, getQueueState } from './downloadApi'
import { usePlayer } from './playerStore'
import { getMyRequests, getAllRequests, useRequestStore, type RequestEventPayload } from './requestApi'
import { getNotifications, useNotificationStore, type Notification } from './notificationApi'
import { useAuthStore } from './authStore'
import { useToastStore } from './toastStore'
import type { DownloadEvent, DownloadRemovedEvent, LibraryUpdatedEvent, QueueStateEvent, RealtimeEvent, Track } from './types'

// trackFromJob synthesizes a minimal library Track for play-when-ready auto-play,
// using the re-matched libraryTrackId. The stream proxy plays by id; the rest is
// best-effort display metadata.
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
    // Detail-page queries use separate root keys — invalidate them too so a
    // completed download flips a missing row to playable without a hard reload.
    function invalidateLibrary(ids?: { artistId?: string; albumId?: string }) {
      void qc.invalidateQueries({ queryKey: ['library'] })
      if (ids?.albumId) void qc.invalidateQueries({ queryKey: ['library', 'album', ids.albumId] })
      if (ids?.artistId) void qc.invalidateQueries({ queryKey: ['library', 'artist', ids.artistId] })
      void qc.invalidateQueries({ queryKey: ['album-detail'] })
      void qc.invalidateQueries({ queryKey: ['artist-detail'] })
      void qc.invalidateQueries({ queryKey: ['synced-playlist'] })
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
          // Bump the library revision so coverage streams re-open and chips flip.
          useLibraryRevision.getState().bump()
          break
        }
        case 'library.updated': {
          const ev = frame.payload as LibraryUpdatedEvent
          const albumId = ev.albumIds?.[0]
          const artistId = ev.artistIds?.[0]
          invalidateLibrary({ artistId, albumId })
          // Bump the library revision so coverage streams re-open and chips flip.
          useLibraryRevision.getState().bump()
          break
        }
        case 'download.queue': {
          useDownloads.getState().setPaused((frame.payload as QueueStateEvent).paused)
          break
        }
        case 'download.removed': {
          useDownloads.getState().remove((frame.payload as DownloadRemovedEvent).jobIds)
          break
        }
        case 'request.created': {
          useRequestStore.getState().applyRequestEvent(frame.payload as RequestEventPayload)
          break
        }
        case 'notification': {
          useNotificationStore.getState().add((frame.payload as { notification: Notification }).notification)
          break
        }
        case 'request.updated': {
          const payload = frame.payload as RequestEventPayload
          useRequestStore.getState().applyRequestEvent(payload)
          const { status, title } = payload.request
          if (status === 'fulfilled') {
            useToastStore.getState().push(`Your request for "${title}" was added`, 'success')
          } else if (status === 'denied') {
            useToastStore.getState().push(`Your request for "${title}" was denied`, 'error')
          } else if (status === 'failed') {
            useToastStore.getState().push(`Your request for "${title}" failed`, 'error')
          }
          break
        }
        default:
          break
      }
    }

    function onOpen() {
      // Resync the full job list on (re)connect so we never miss a transition.
      void getDownloads().then((jobs) => useDownloads.getState().setAll(jobs))
      // Resync the paused flag (another client may have paused while we were away).
      void getQueueState()
        .then((q) => useDownloads.getState().setPaused(q.paused))
        .catch(() => {})
      // Hydrate the request store for the capabilities the user has, so the
      // pending-count nav badge and track "Requested" affordance are correct on
      // load — not only after visiting /requests.
      const auth = useAuthStore.getState()
      if (auth.can('request')) {
        void getMyRequests()
          .then((r) => useRequestStore.getState().setMine(r))
          .catch(() => {})
      }
      if (auth.can('manage_requests')) {
        void getAllRequests('pending')
          .then((r) => useRequestStore.getState().setQueue(r))
          .catch(() => {})
      }
      // Hydrate notifications for all authed users — no cap gate.
      void getNotifications()
        .then((r) => useNotificationStore.getState().setAll(r.notifications, r.unread))
        .catch(() => {})
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
