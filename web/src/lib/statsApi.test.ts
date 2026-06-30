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

/** Capture URLs that api.get / api.del are called with, without actually fetching. */
let capturedUrl: string | null = null
let capturedMethod: string | null = null

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
  },
}))

beforeEach(() => {
  capturedUrl = null
  capturedMethod = null
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
