import { useEffect, useReducer } from 'react'
import type { AlbumCoverage } from './types'
import { CoverageStream } from './coverageStream'

export type CoverageMap = Record<string, AlbumCoverage>

type Action = { type: 'reset' } | { type: 'coverage'; c: AlbumCoverage }

function reducer(state: CoverageMap, action: Action): CoverageMap {
  if (action.type === 'reset') return {}
  const prev = state[action.c.externalAlbumId]
  // Idempotent: if the same frame is re-delivered (e.g. EventSource reconnect),
  // return the SAME reference so React skips the re-render entirely.
  if (prev && prev.state === action.c.state && prev.ownedCount === action.c.ownedCount) return state
  return { ...state, [action.c.externalAlbumId]: action.c }
}

// useCoverageStream opens a CoverageStream for (source, id) when enabled,
// accumulating per-album coverage frames into a map keyed by externalAlbumId.
// The stream is closed on unmount / when source/id/enabled change (no leaks).
export function useCoverageStream(source: string, id: string, enabled: boolean): CoverageMap {
  const [state, dispatch] = useReducer(reducer, {})

  useEffect(() => {
    dispatch({ type: 'reset' })
    if (!enabled || !source || !id) return
    const stream = new CoverageStream(source, id, {
      onCoverage: (c) => dispatch({ type: 'coverage', c }),
    })
    return () => stream.close()
  }, [source, id, enabled])

  return state
}
