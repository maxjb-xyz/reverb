import { api } from './api'

// The DJ's server side: what is configured, the line writer, and the voice.

export interface DJProvider {
  available: boolean
  name: string
}

export interface DJStatus {
  voice: DJProvider
  writer: DJProvider
}

/** A song as the DJ talks about it. Field names match the server's dj.Track. */
export interface DJTrack {
  title: string
  artist: string
  album?: string
  year?: number
}

/** Why the songs were picked, as facts the DJ may mention. */
export interface DJReason {
  artist?: string
  play_count?: number
  last_played?: string
  seed?: string
}

/** One break: the facts the DJ talks from. Mirrors the server's dj.Segment. */
export interface DJSegment {
  lane?: string
  reason: DJReason
  previous?: DJTrack
  next: DJTrack[]
  listener?: string
  hour: number
  request?: string
}

export interface DJLine {
  text: string
  source: 'llm' | 'template'
}

export interface DJClip {
  id: string
  url: string
}

export const getDJStatus = () => api.get<DJStatus>('/dj/status')
export const writeLine = (seg: DJSegment) => api.post<DJLine>('/dj/line', seg)
export const sayLine = (text: string) => api.post<DJClip>('/dj/say', { text })
