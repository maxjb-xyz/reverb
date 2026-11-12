// Where the current queue came from: an album, a playlist, search, radio...
// The listen tracker stamps every listen with it so a skip can count against
// the station or lane that picked the song (radio and DJ pass one explicitly;
// everything else is inferred from the page the user pressed play on).

export interface PlayContext {
  kind: string
  id?: string
}

let current: PlayContext = { kind: 'other' }

export function setPlayContext(ctx: PlayContext): void {
  current = { kind: ctx.kind || 'other', ...(ctx.id ? { id: ctx.id } : {}) }
}

export function getPlayContext(): PlayContext {
  return current
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
