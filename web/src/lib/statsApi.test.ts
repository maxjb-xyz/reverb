import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import * as statsApi from './statsApi'
import type { Range } from './range'

// ── Helpers ───────────────────────────────────────────────────────────────────

const RANGE: Range = {
  from: 1_700_000_000,
  to: 1_700_086_400,
  bucket: 'day',
  tzOffsetMinutes: -300,
  label: 'Test range',
}

/** Capture URLs/bodies that api.get / api.del / api.post are called with, without actually fetching. */
let capturedUrl: string | null = null
let capturedMethod: string | null = null
let capturedBody: unknown = null

vi.mock('./api', () => ({
  api: {
    get: vi.fn((path: string) => {
      capturedUrl = path
      capturedMethod = 'GET'
      return Promise.resolve(null)
    }),
    del: vi.fn((path: string) => {
      capturedUrl = path
      capturedMethod = 'DELETE'
      return Promise.resolve(null)
    }),
    post: vi.fn((path: string, body: unknown) => {
      capturedUrl = path
      capturedMethod = 'POST'
      capturedBody = body
      return Promise.resolve({ counts: {} })
    }),
  },
}))

beforeEach(() => {
  capturedUrl = null
  capturedMethod = null
  capturedBody = null
})

afterEach(() => {
  vi.clearAllMocks()
})

// ── B1: entity() must use query params, NOT a path segment ────────────────────

describe('statsApi.entity()', () => {
  it('builds GET /stats/entity with kind and id as QUERY PARAMS (not path segments)', async () => {
    await statsApi.entity('artist', 'Radiohead', RANGE).catch(() => {})
    expect(capturedUrl).not.toBeNull()
    // Must start with /stats/entity? — NOT /stats/entity/artist/...
    expect(capturedUrl).toMatch(/^\/stats\/entity\?/)
    expect(capturedUrl).toContain('kind=artist')
    expect(capturedUrl).toContain('id=Radiohead')
  })

  it('includes the range from/to/tzOffsetMinutes as query params', async () => {
    await statsApi.entity('artist', 'Radiohead', RANGE).catch(() => {})
    expect(capturedUrl).toContain(`from=${RANGE.from}`)
    expect(capturedUrl).toContain(`to=${RANGE.to}`)
    expect(capturedUrl).toContain(`tzOffsetMinutes=${RANGE.tzOffsetMinutes}`)
  })

  it('URL-encodes artist names with special characters', async () => {
    await statsApi.entity('artist', 'Sigur Rós', RANGE).catch(() => {})
    expect(capturedUrl).not.toBeNull()
    expect(capturedUrl).not.toContain('/stats/entity/artist/')
    // "Sigur Rós" URI-encoded: space → %20 or +, ó → %C3%B3
    expect(capturedUrl).toMatch(/id=Sigur/)
  })

  it('builds /stats/entity?kind=album for album kind', async () => {
    await statsApi.entity('album', 'OK Computer', RANGE).catch(() => {})
    expect(capturedUrl).toMatch(/^\/stats\/entity\?/)
    expect(capturedUrl).toContain('kind=album')
    expect(capturedUrl).toContain('id=OK+Computer')
  })

  it('builds /stats/entity?kind=track for track kind', async () => {
    await statsApi.entity('track', 'trk_abc123', RANGE).catch(() => {})
    expect(capturedUrl).toMatch(/^\/stats\/entity\?/)
    expect(capturedUrl).toContain('kind=track')
    expect(capturedUrl).toContain('id=trk_abc123')
  })

  it('appends &artist= (URL-encoded) when an artist is supplied for kind=album', async () => {
    await statsApi.entity('album', 'OK Computer', RANGE, 'Radiohead').catch(() => {})
    expect(capturedUrl).toMatch(/^\/stats\/entity\?/)
    expect(capturedUrl).toContain('kind=album')
    expect(capturedUrl).toContain('id=OK+Computer')
    expect(capturedUrl).toContain('artist=Radiohead')
  })

  it('URL-encodes the album artist name', async () => {
    await statsApi.entity('album', 'Ágætis byrjun', RANGE, 'Sigur Rós').catch(() => {})
    // "Sigur Rós" → artist=Sigur+R%C3%B3s
    expect(capturedUrl).toContain('artist=Sigur')
    expect(capturedUrl).toContain('%C3%B3s')
  })

  it('does NOT append &artist= when no artist is supplied (artist/track callers unchanged)', async () => {
    await statsApi.entity('artist', 'Radiohead', RANGE).catch(() => {})
    expect(capturedUrl).not.toContain('artist=')
  })
})

// ── playCounts: per-track lookup (POST /stats/play-counts) ────────────────────

describe('statsApi.playCounts()', () => {
  it('POSTs to /stats/play-counts', async () => {
    await statsApi.playCounts([
      { key: 'L1', title: 'Kid A', artist: 'Radiohead', album: 'Kid A', durationMs: 2000 },
    ]).catch(() => {})
    expect(capturedMethod).toBe('POST')
    expect(capturedUrl).toBe('/stats/play-counts')
  })

  it('sends the body with the exact backend json keys (tracks/key/title/artist/album/durationMs)', async () => {
    await statsApi.playCounts([
      { key: 'L1', title: 'Kid A', artist: 'Radiohead', album: 'Kid A', durationMs: 2000, isrc: 'GBABC1234567' },
    ]).catch(() => {})
    expect(capturedBody).toEqual({
      tracks: [
        { key: 'L1', title: 'Kid A', artist: 'Radiohead', album: 'Kid A', durationMs: 2000, isrc: 'GBABC1234567' },
      ],
    })
  })

  it('returns the counts map from the response', async () => {
    const { api } = await import('./api')
    vi.mocked(api.post).mockResolvedValueOnce({ counts: { L1: 7, L2: 0 } })
    const counts = await statsApi.playCounts([
      { key: 'L1', title: 'Kid A', artist: 'Radiohead', album: 'Kid A', durationMs: 2000 },
    ])
    expect(counts).toEqual({ L1: 7, L2: 0 })
  })

  it('sends an empty tracks array unchanged', async () => {
    await statsApi.playCounts([]).catch(() => {})
    expect(capturedBody).toEqual({ tracks: [] })
  })
})

// ── Other endpoints should NOT be affected ────────────────────────────────────

describe('statsApi other endpoints', () => {
  it('summary builds /stats/summary with range params', async () => {
    await statsApi.summary(RANGE).catch(() => {})
    expect(capturedUrl).toMatch(/^\/stats\/summary\?/)
    expect(capturedUrl).toContain(`from=${RANGE.from}`)
  })

  it('topArtists builds /stats/top/artists', async () => {
    await statsApi.topArtists(RANGE).catch(() => {})
    expect(capturedUrl).toMatch(/^\/stats\/top\/artists\?/)
  })

  it('topAlbums builds /stats/top/albums', async () => {
    await statsApi.topAlbums(RANGE).catch(() => {})
    expect(capturedUrl).toMatch(/^\/stats\/top\/albums\?/)
  })
})

// ── deletePlay: owner-scoped delete primitive ─────────────────────────────────

describe('statsApi.deletePlay()', () => {
  it('issues DELETE /plays/{id}', async () => {
    await statsApi.deletePlay('p1').catch(() => {})
    expect(capturedMethod).toBe('DELETE')
    expect(capturedUrl).toBe('/plays/p1')
  })

  it('URL-encodes the play id', async () => {
    await statsApi.deletePlay('a/b c').catch(() => {})
    expect(capturedMethod).toBe('DELETE')
    expect(capturedUrl).toBe('/plays/a%2Fb%20c')
  })
})
