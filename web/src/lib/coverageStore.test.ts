import { describe, expect, it, vi } from 'vitest'
import { renderHook } from '@testing-library/react'
import { CoverageStream } from './coverageStream'
import { useCoverageStream, reducer } from './coverageStore'
import type { AlbumCoverage } from './types'

// Mock libraryRevisionStore so useCoverageStream gets a stable revision=0
// in all existing tests (no re-stream behaviour needed here).
vi.mock('./libraryRevisionStore', () => ({
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  useLibraryRevision: vi.fn((selector: (s: any) => unknown) => selector({ revision: 0 })),
}))

// StubSource lets the test fire messages/errors synchronously; records the URL + close.
class StubSource {
  onmessage: ((ev: { data: string }) => void) | null = null
  onerror: (() => void) | null = null
  closed = false
  url: string
  constructor(url: string) { this.url = url }
  close() {
    this.closed = true
  }
  emit(c: AlbumCoverage) {
    this.onmessage?.({ data: JSON.stringify(c) })
  }
}

function makeCoverage(p: Partial<AlbumCoverage> & { externalAlbumId: string }): AlbumCoverage {
  return {
    source: 'spotify',
    state: 'full',
    ownedCount: 10,
    totalCount: 10,
    missingTracks: [],
    ...p,
  }
}

describe('CoverageStream', () => {
  it('opens the correct URL with encoded path segments', () => {
    let made: StubSource | null = null
    const got: AlbumCoverage[] = []
    const cs = new CoverageStream('spotify', 'artist/123', { onCoverage: (c) => got.push(c) }, (url) => {
      made = new StubSource(url)
      return made
    })
    expect(made).not.toBeNull()
    expect(made!.url).toBe('/api/v1/artist/spotify/artist%2F123/coverage')

    made!.emit(makeCoverage({ externalAlbumId: 'alb1' }))
    expect(got).toHaveLength(1)
    expect(got[0].externalAlbumId).toBe('alb1')

    cs.close()
    expect(made!.closed).toBe(true)
  })

  it('calls onError on stream error', () => {
    const onError = vi.fn()
    let made: StubSource | null = null
    new CoverageStream('spotify', 'id', { onCoverage: () => {}, onError }, (url) => {
      made = new StubSource(url)
      return made
    })
    made!.onerror?.()
    expect(onError).toHaveBeenCalled()
  })

  it('closes the source on error so the browser does not auto-reconnect-loop', () => {
    let made: StubSource | null = null
    new CoverageStream('spotify', 'id', { onCoverage: () => {}, onError: () => {} }, (url) => {
      made = new StubSource(url)
      return made
    })
    expect(made!.closed).toBe(false)
    made!.onerror?.()
    expect(made!.closed).toBe(true)
  })
})

describe('useCoverageStream', () => {
  it('returns empty map when not enabled', () => {
    const { result } = renderHook(() => useCoverageStream('spotify', 'artist-1', false))
    // When disabled, no stream is opened and state is empty
    expect(result.current).toEqual({})
  })

  it('builds a map from two pushed coverage frames', () => {
    // Push two different albums → both keys present
    const c1 = makeCoverage({ externalAlbumId: 'alb1', ownedCount: 5, totalCount: 10, state: 'partial' })
    const c2 = makeCoverage({ externalAlbumId: 'alb2', ownedCount: 10, totalCount: 10, state: 'full' })

    let state = reducer({}, { type: 'coverage', c: c1 })
    state = reducer(state, { type: 'coverage', c: c2 })

    expect(Object.keys(state)).toHaveLength(2)
    expect(state['alb1'].state).toBe('partial')
    expect(state['alb2'].state).toBe('full')
  })

  it('is idempotent: re-delivering the same frame returns the SAME reference', () => {
    const c1 = makeCoverage({ externalAlbumId: 'alb1', ownedCount: 10, totalCount: 10, state: 'full' })

    const prev = reducer({}, { type: 'coverage', c: c1 })
    const next = reducer(prev, { type: 'coverage', c: c1 }) // same frame again
    expect(next).toBe(prev) // same reference → no re-render
  })

  it('resets to empty map on key change', () => {
    const c1 = makeCoverage({ externalAlbumId: 'alb1' })
    let state = reducer({}, { type: 'coverage', c: c1 })
    expect(Object.keys(state)).toHaveLength(1)

    // Reset simulates key change (source/id/enabled change)
    state = reducer(state, { type: 'reset' })
    expect(state).toEqual({})
  })
})
