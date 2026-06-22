import { api } from './api'
import type { DownloadJob, ExternalResult, ExternalTrackRef } from './types'

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

// reqFromResult builds a download request from a search result. `downloader` is
// optional — omitting it lets the server pick via the fallback chain.
export function reqFromResult(r: ExternalResult, downloader?: string): CreateDownloadReq {
  return {
    source: r.source,
    externalId: r.externalId,
    artist: r.artist,
    title: r.title,
    album: r.album,
    isrc: r.isrc,
    durationMs: r.durationMs,
    downloader,
  }
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

export function postBatchDownload(tracks: ExternalTrackRef[]): Promise<DownloadJob[]> {
  return api.post<DownloadJob[]>('/downloads/batch', { tracks })
}

export function reqFromExternalRef(t: ExternalTrackRef): CreateDownloadReq {
  return {
    source: t.source,
    externalId: t.externalId,
    artist: t.artist ?? '',
    title: t.title,
    album: t.album ?? '',
    isrc: t.isrc,
    durationMs: t.durationMs,
  }
}
