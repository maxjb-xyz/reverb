import { api } from './api'
import type { DownloadJob } from './types'

export interface CreateDownloadReq {
  source: string
  externalId: string
  artist: string
  title: string
  album: string
  isrc?: string
  durationMs?: number
  downloader?: string
  playWhenReady?: boolean
}

export function postDownload(req: CreateDownloadReq): Promise<DownloadJob> {
  return api.post<DownloadJob>('/downloads', req)
}

export function getDownloads(): Promise<DownloadJob[]> {
  return api.get<DownloadJob[]>('/downloads')
}

export function cancelDownload(id: string): Promise<unknown> {
  return api.post(`/downloads/${encodeURIComponent(id)}/cancel`)
}

export function retryDownload(id: string): Promise<DownloadJob> {
  return api.post<DownloadJob>(`/downloads/${encodeURIComponent(id)}/retry`)
}
