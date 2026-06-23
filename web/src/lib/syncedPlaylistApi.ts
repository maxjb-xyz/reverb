import { useQuery } from '@tanstack/react-query'
import { api } from './api'
import type { SyncedPlaylist, SyncedPlaylistDetail, DownloadJob } from './types'

export function useSyncedPlaylists() {
  return useQuery({
    queryKey: ['synced-playlists'],
    queryFn: () => api.get<SyncedPlaylist[]>('/synced-playlists'),
  })
}

export function useSyncedPlaylist(id: string) {
  return useQuery({
    queryKey: ['synced-playlist', id],
    queryFn: () => api.get<SyncedPlaylistDetail>(`/synced-playlists/${encodeURIComponent(id)}`),
    enabled: !!id,
  })
}

export function importPlaylist(url: string, downloadMissing: boolean): Promise<SyncedPlaylistDetail> {
  return api.post<SyncedPlaylistDetail>('/synced-playlists', { url, downloadMissing })
}

export function syncNow(id: string): Promise<SyncedPlaylistDetail> {
  return api.post<SyncedPlaylistDetail>(`/synced-playlists/${encodeURIComponent(id)}/sync`)
}

export function downloadMissingForPlaylist(id: string): Promise<DownloadJob[]> {
  return api.post<DownloadJob[]>(`/synced-playlists/${encodeURIComponent(id)}/download-missing`)
}

export interface UpdateSyncSettingsReq {
  syncEnabled: boolean
  intervalSec: number
  autoDownload: boolean
}

export function updateSyncSettings(id: string, settings: UpdateSyncSettingsReq): Promise<unknown> {
  return api.put(`/synced-playlists/${encodeURIComponent(id)}/settings`, settings)
}

export function deleteSyncedPlaylist(id: string): Promise<unknown> {
  return api.del(`/synced-playlists/${encodeURIComponent(id)}`)
}

export function removeSyncedTrack(id: string, source: string, externalId: string): Promise<SyncedPlaylistDetail> {
  const url = `/synced-playlists/${encodeURIComponent(id)}/tracks?source=${encodeURIComponent(source)}&externalId=${encodeURIComponent(externalId)}`
  return api.del<SyncedPlaylistDetail>(url)
}
