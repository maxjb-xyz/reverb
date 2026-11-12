// Where the current queue came from: an album, a playlist, search, radio...
// The listen tracker stamps every listen with it so a skip can count against
// the station or lane that picked the song (radio and DJ pass one explicitly;
// everything else is inferred from the page the user pressed play on).

export interface PlayContext {
  kind: string
  id?: string
}

let current: PlayContext = { kind: 'other' }
const perTrack = new Map<string, PlayContext>()

export function setPlayContext(ctx: PlayContext): void {
  current = { kind: ctx.kind || 'other', ...(ctx.id ? { id: ctx.id } : {}) }
}

/**
 * The context the current queue was played from. Given a track id, a finer
 * context set for that track (the DJ lane that picked it) wins, but only while
 * the queue is still of the same kind: the same song played later from an
 * album counts as the album's.
 */
export function getPlayContext(trackId?: string): PlayContext {
  const own = trackId ? perTrack.get(trackId) : undefined
  return own && own.kind === current.kind ? own : current
}

/**
 * Give queued tracks their own contexts (set these before playing them, so the
 * first song is stamped correctly). `replace` forgets the earlier ones first.
 */
export function setTrackContexts(entries: Iterable<[string, PlayContext]>, replace = false): void {
  if (replace) perTrack.clear()
  for (const [id, ctx] of entries) perTrack.set(id, ctx)
}

/** Infer a context from the route the user was on when they pressed play. */
export function contextFromPath(pathname: string): PlayContext {
  const parts = pathname.split('/').filter(Boolean)
  const [first, ...rest] = parts
  const id = rest.length ? decodeURIComponent(rest[rest.length - 1]) : undefined
  switch (first) {
    case undefined:
      return { kind: 'home' }
    case 'album':
      return { kind: 'album', id }
    case 'artist':
      return { kind: 'artist', id }
    case 'playlist':
      return { kind: 'playlist', id }
    case 'mix':
      return { kind: 'mix', id }
    case 'search':
      return { kind: 'search' }
    case 'library':
      return { kind: 'library' }
    case 'stats':
      return { kind: 'stats' }
    case 'downloads':
      return { kind: 'downloads' }
    default:
      return { kind: 'other' }
  }
}
