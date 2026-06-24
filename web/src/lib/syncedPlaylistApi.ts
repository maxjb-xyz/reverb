import { useQuery } from '@tanstack/react-query'
import { api, ApiError } from './api'
import type { SyncedPlaylist, SyncedPlaylistDetail, DownloadJob } from './types'

const BASE = '/api/v1'

export function useSyncedPlaylists() {
  return useQuery({
    queryKey: ['synced-playlists'],
    queryFn: () => api.get<SyncedPlaylist[]>('/playlists'),
  })
}

export function useSyncedPlaylist(id: string) {
  return useQuery({
    queryKey: ['synced-playlist', id],
    queryFn: () => api.get<SyncedPlaylistDetail>(`/playlists/${encodeURIComponent(id)}`),
    enabled: !!id,
  })
}

export function importPlaylist(url: string, downloadMissing: boolean): Promise<SyncedPlaylistDetail> {
  return api.post<SyncedPlaylistDetail>('/playlists/import-synced', { url, downloadMissing })
}

export function syncNow(id: string): Promise<SyncedPlaylistDetail> {
  return api.post<SyncedPlaylistDetail>(`/playlists/${encodeURIComponent(id)}/sync`)
}

export function downloadMissingForPlaylist(id: string): Promise<DownloadJob[]> {
  return api.post<DownloadJob[]>(`/playlists/${encodeURIComponent(id)}/download-missing`)
}

export interface UpdateSyncSettingsReq {
  syncEnabled: boolean
  intervalSec: number
  autoDownload: boolean
}

export function updateSyncSettings(id: string, settings: UpdateSyncSettingsReq): Promise<unknown> {
  return api.put(`/playlists/${encodeURIComponent(id)}/settings`, settings)
}

export function renameSyncedPlaylist(id: string, name: string): Promise<SyncedPlaylistDetail> {
  return api.put<SyncedPlaylistDetail>(`/playlists/${encodeURIComponent(id)}`, { name })
}

export function deleteSyncedPlaylist(id: string): Promise<unknown> {
  return api.del(`/playlists/${encodeURIComponent(id)}`)
}

export function removeSyncedTrack(id: string, source: string, externalId: string): Promise<SyncedPlaylistDetail> {
  const url = `/playlists/${encodeURIComponent(id)}/tracks?source=${encodeURIComponent(source)}&externalId=${encodeURIComponent(externalId)}`
  return api.del<SyncedPlaylistDetail>(url)
}

export interface SyncedTrackEntry {
  source: string
  externalId: string
  title: string
  artist?: string
  album?: string
  isrc?: string
  durationMs?: number
  coverArtId?: string
}

export function addSyncedTrack(playlistId: string, entry: SyncedTrackEntry): Promise<SyncedPlaylistDetail> {
  return api.post<SyncedPlaylistDetail>(`/playlists/${encodeURIComponent(playlistId)}/tracks`, entry)
}

/**
 * Upload a new cover image for a managed (mode='once') playlist.
 * Uses raw fetch with multipart/form-data since api.post forces JSON.
 */
export async function uploadPlaylistCover(id: string, file: File): Promise<SyncedPlaylistDetail> {
  const form = new FormData()
  form.append('image', file)
  const res = await fetch(`${BASE}/playlists/${encodeURIComponent(id)}/cover`, {
    method: 'POST',
    credentials: 'include',
    body: form,
  })
  if (!res.ok) throw new ApiError('POST', `/playlists/${id}/cover`, res.status)
  return res.json() as Promise<SyncedPlaylistDetail>
}

export interface TrackOrderEntry {
  source: string
  externalId: string
}

/**
 * Reorder tracks in a managed (mode='once') playlist.
 */
export function reorderSyncedTracks(id: string, order: TrackOrderEntry[]): Promise<SyncedPlaylistDetail> {
  return api.put<SyncedPlaylistDetail>(`/playlists/${encodeURIComponent(id)}/tracks/order`, { order })
}
