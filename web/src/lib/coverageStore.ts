import { useEffect, useReducer } from 'react'
import type { AlbumCoverage } from './types'
import { CoverageStream } from './coverageStream'
import { useLibraryRevision } from './libraryRevisionStore'

export type CoverageMap = Record<string, AlbumCoverage>

export type Action = { type: 'reset' } | { type: 'coverage'; c: AlbumCoverage }

export function reducer(state: CoverageMap, action: Action): CoverageMap {
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
// It also re-opens when the library revision bumps (i.e. after a download
// completes) so coverage chips flip to "full" without a hard reload.
export function useCoverageStream(source: string, id: string, enabled: boolean): CoverageMap {
  const [state, dispatch] = useReducer(reducer, {})
  const revision = useLibraryRevision((s) => s.revision)

  useEffect(() => {
    dispatch({ type: 'reset' })
    if (!enabled || !source || !id) return
    const stream = new CoverageStream(source, id, {
      onCoverage: (c) => dispatch({ type: 'coverage', c }),
    })
    return () => stream.close()
  }, [source, id, enabled, revision])

  return state
}
