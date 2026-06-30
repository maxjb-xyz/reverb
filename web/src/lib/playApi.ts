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

export async function recordPlay(input: PlayInput): Promise<void> {
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
  await api.post<null>('/plays', wire)
}
