import { api } from './api'

// PlayInput is the FE-internal type (camelCase, standard TS convention).
// recordPlay converts it to the PascalCase wire format that the Go handler
// expects (play.PlayInput has no json tags → json.Decode uses field names verbatim).
export interface PlayInput {
  libraryTrackId: string
  title: string
  artist: string
  album: string
  isrc?: string
  durationMs: number
  msPlayed: number
  completed: boolean
}

// GoPlayInput is the wire format: PascalCase field names as decoded by the Go
// play.PlayInput struct (no json struct tags → json.Decode maps PascalCase keys).
interface GoPlayInput {
  LibraryTrackID: string
  Title: string
  Artist: string
  Album: string
  ISRC?: string
  DurationMs: number
  MsPlayed: number
  Completed: boolean
}

// PlayProgress is how much of a recorded play was finally heard.
export interface PlayProgress {
  msPlayed: number
  completed: boolean
}

/** Records a qualified play and resolves to its id (null if the server sent none). */
export async function recordPlay(input: PlayInput): Promise<string | null> {
  const wire: GoPlayInput = {
    LibraryTrackID: input.libraryTrackId,
    Title: input.title,
    Artist: input.artist,
    Album: input.album,
    DurationMs: input.durationMs,
    MsPlayed: input.msPlayed,
    Completed: input.completed,
    ...(input.isrc ? { ISRC: input.isrc } : {}),
  }
  const res = await api.post<{ id?: string } | null>('/plays', wire)
  return res?.id ?? null
}

/**
 * Reports the final listened time of a recorded play. With keepalive the
 * request survives the page closing (pagehide), which a plain fetch does not.
 */
export async function updatePlay(id: string, p: PlayProgress, keepalive = false): Promise<void> {
  const body = { MsPlayed: Math.round(p.msPlayed), Completed: p.completed }
  if (!keepalive) {
    await api.patch<null>(`/plays/${encodeURIComponent(id)}`, body)
    return
  }
  await fetch(`/api/v1/plays/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    credentials: 'include',
    keepalive: true,
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}
