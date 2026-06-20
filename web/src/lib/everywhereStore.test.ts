import { describe, expect, it } from 'vitest'
import { applyEnvelope, dedupKey, emptyEverywhere } from './everywhereStore'
import type { ExternalResult, SearchEnvelope } from './types'

function track(p: Partial<ExternalResult>): ExternalResult {
  return {
    source: 's', externalId: 'e', title: 'T', artist: 'A', album: 'Al',
    durationMs: 1000, type: 'track', ...p,
  }
}
function env(p: Partial<SearchEnvelope>): SearchEnvelope {
  return { source: 's', status: 'ok', results: [], ...p }
}

describe('dedupKey', () => {
  it('prefers ISRC when present', () => {
    expect(dedupKey(track({ isrc: 'USX1' }))).toBe('isrc:usx1')
  })
  it('falls back to normalized artist+title', () => {
    expect(dedupKey(track({ artist: 'The Band', title: 'Song (feat. X)' }))).toBe(dedupKey(track({ artist: 'The Band', title: 'Song' })))
  })
  it('daft punk case: word-boundary fix prevents over-stripping', () => {
    // Without \b, "Daft Punk" would be stripped to "da" (matching "ft" in "Daft")
    // With \b fix, "Daft Punk" normalizes to "daft punk"
    const key = dedupKey(track({ artist: 'Daft Punk', title: 'Get Lucky' }))
    expect(key).toContain('daft punk')
  })
  it('separator prevents artist+title collision', () => {
    // "a"+"bc" must NOT equal "ab"+"c"
    const k1 = dedupKey(track({ artist: 'a', title: 'bc' }))
    const k2 = dedupKey(track({ artist: 'ab', title: 'c' }))
    expect(k1).not.toBe(k2)
  })
})

describe('applyEnvelope', () => {
  it('appends tracks and records source status', () => {
    const s1 = applyEnvelope(emptyEverywhere, env({ source: 'spotify', results: [track({ externalId: 'a' })] }))
    expect(s1.tracks).toHaveLength(1)
    expect(s1.sources).toEqual([{ source: 'spotify', status: 'ok' }])

    const s2 = applyEnvelope(s1, env({ source: 'deezer', results: [track({ externalId: 'b', isrc: 'ZZ9', artist: 'Other', title: 'Diff' })] }))
    expect(s2.tracks.map((t) => t.externalId)).toEqual(['a', 'b'])
    expect(s2.sources).toHaveLength(2)
  })

  it('never reorders shown rows and dedupes across sources by key', () => {
    const a = applyEnvelope(emptyEverywhere, env({ source: 'spotify', results: [
      track({ externalId: 'x', isrc: 'SAME' }),
      track({ externalId: 'y', isrc: 'OTHER' }),
    ]}))
    const b = applyEnvelope(a, env({ source: 'deezer', results: [
      track({ externalId: 'dup', isrc: 'SAME' }), // duplicate of x → dropped
      track({ externalId: 'z', isrc: 'NEW' }),
    ]}))
    expect(b.tracks.map((t) => t.externalId)).toEqual(['x', 'y', 'z'])
  })

  it('routes albums and artists into their own sections', () => {
    const s = applyEnvelope(emptyEverywhere, env({ results: [
      track({ externalId: 't', type: 'track' }),
      track({ externalId: 'al', type: 'album' }),
      track({ externalId: 'ar', type: 'artist' }),
    ]}))
    expect(s.tracks).toHaveLength(1)
    expect(s.albums).toHaveLength(1)
    expect(s.artists).toHaveLength(1)
  })

  it('updates an existing source status in place (timeout)', () => {
    const a = applyEnvelope(emptyEverywhere, env({ source: 'spotify', status: 'ok', results: [track({})] }))
    const b = applyEnvelope(a, env({ source: 'spotify', status: 'timeout', results: [] }))
    expect(b.sources).toEqual([{ source: 'spotify', status: 'timeout' }])
  })
})
