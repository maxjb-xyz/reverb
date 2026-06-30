import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import {
  getLinks,
  lastfmAuthUrl,
  lastfmComplete,
  lastfmDisconnect,
  nowPlaying,
  getLastfmConfig,
  setLastfmConfig,
  ScrobbleError,
} from './scrobbleApi'

// Helper to create a stub fetch that returns a given status + JSON body.
function stubFetch(status: number, body: unknown) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async () =>
      new Response(JSON.stringify(body), { status }),
    ),
  )
}

// Helper: get the last fetch call args.
function lastCall() {
  const mockFetch = fetch as unknown as ReturnType<typeof vi.fn>
  return mockFetch.mock.calls.at(-1)! as [string, RequestInit]
}

afterEach(() => vi.unstubAllGlobals())

// ──────────────────────────────────────────────────────────────────────────────
// getLinks
// ──────────────────────────────────────────────────────────────────────────────

describe('getLinks', () => {
  beforeEach(() => {
    stubFetch(200, { configured: true, links: [{ provider: 'lastfm', username: 'user1', status: 'active' }] })
  })

  it('GETs /api/v1/scrobble/links', async () => {
    await getLinks()
    expect(lastCall()[0]).toBe('/api/v1/scrobble/links')
    expect(lastCall()[1].method).toBe('GET')
  })

  it('returns { configured, links }', async () => {
    const result = await getLinks()
    expect(result.configured).toBe(true)
    expect(result.links).toHaveLength(1)
    expect(result.links[0].provider).toBe('lastfm')
    expect(result.links[0].username).toBe('user1')
    expect(result.links[0].status).toBe('active')
  })
})

// ──────────────────────────────────────────────────────────────────────────────
// lastfmAuthUrl
// ──────────────────────────────────────────────────────────────────────────────

describe('lastfmAuthUrl', () => {
  it('POSTs /api/v1/scrobble/lastfm/auth-url and returns { authUrl, token }', async () => {
    stubFetch(200, { authUrl: 'https://last.fm/auth', token: 'tok123' })
    const result = await lastfmAuthUrl()
    expect(lastCall()[0]).toBe('/api/v1/scrobble/lastfm/auth-url')
    expect(lastCall()[1].method).toBe('POST')
    expect(result.authUrl).toBe('https://last.fm/auth')
    expect(result.token).toBe('tok123')
  })

  it('throws ScrobbleError with code lastfm_not_configured on 400 { error: "lastfm_not_configured" }', async () => {
    stubFetch(400, { error: 'lastfm_not_configured' })
    await expect(lastfmAuthUrl()).rejects.toSatisfy(
      (e: unknown) => e instanceof ScrobbleError && e.code === 'lastfm_not_configured',
    )
  })

  it('throws ScrobbleError with code lastfm_unavailable on 502', async () => {
    stubFetch(502, { error: 'lastfm_unavailable' })
    await expect(lastfmAuthUrl()).rejects.toSatisfy(
      (e: unknown) => e instanceof ScrobbleError && e.code === 'lastfm_unavailable',
    )
  })

  it('throws ScrobbleError with code lastfm_unavailable on 503', async () => {
    stubFetch(503, { error: 'service unavailable' })
    await expect(lastfmAuthUrl()).rejects.toSatisfy(
      (e: unknown) => e instanceof ScrobbleError && e.code === 'lastfm_unavailable',
    )
  })
})

// ──────────────────────────────────────────────────────────────────────────────
// lastfmComplete
// ──────────────────────────────────────────────────────────────────────────────

describe('lastfmComplete', () => {
  it('POSTs /api/v1/scrobble/lastfm/complete with token and returns { username }', async () => {
    stubFetch(200, { username: 'musicfan' })
    const result = await lastfmComplete('tok-abc')
    expect(lastCall()[0]).toBe('/api/v1/scrobble/lastfm/complete')
    expect(lastCall()[1].method).toBe('POST')
    expect(JSON.parse(lastCall()[1].body as string)).toEqual({ token: 'tok-abc' })
    expect(result.username).toBe('musicfan')
  })
})

// ──────────────────────────────────────────────────────────────────────────────
// lastfmDisconnect
// ──────────────────────────────────────────────────────────────────────────────

describe('lastfmDisconnect', () => {
  it('DELETEs /api/v1/scrobble/lastfm and returns void', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response(null, { status: 204 })),
    )
    const result = await lastfmDisconnect()
    expect(lastCall()[0]).toBe('/api/v1/scrobble/lastfm')
    expect(lastCall()[1].method).toBe('DELETE')
    expect(result).toBeNull()
  })
})

// ──────────────────────────────────────────────────────────────────────────────
// nowPlaying
// ──────────────────────────────────────────────────────────────────────────────

describe('nowPlaying', () => {
  it('POSTs /api/v1/scrobble/nowplaying with track data', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response(null, { status: 204 })),
    )
    await nowPlaying({ title: 'Hurt', artist: 'Johnny Cash', album: 'American IV', durationMs: 218000 })
    expect(lastCall()[0]).toBe('/api/v1/scrobble/nowplaying')
    expect(lastCall()[1].method).toBe('POST')
    const sentBody = JSON.parse(lastCall()[1].body as string)
    expect(sentBody.title).toBe('Hurt')
    expect(sentBody.artist).toBe('Johnny Cash')
    expect(sentBody.album).toBe('American IV')
    expect(sentBody.durationMs).toBe(218000)
  })
})

// ──────────────────────────────────────────────────────────────────────────────
// getLastfmConfig (admin)
// ──────────────────────────────────────────────────────────────────────────────

describe('getLastfmConfig', () => {
  it('GETs /api/v1/admin/integrations/lastfm and returns { apiKey, apiSecretSet }', async () => {
    stubFetch(200, { apiKey: 'my-key', apiSecretSet: true })
    const result = await getLastfmConfig()
    expect(lastCall()[0]).toBe('/api/v1/admin/integrations/lastfm')
    expect(lastCall()[1].method).toBe('GET')
    expect(result.apiKey).toBe('my-key')
    expect(result.apiSecretSet).toBe(true)
  })
})

// ──────────────────────────────────────────────────────────────────────────────
// setLastfmConfig (admin)
// ──────────────────────────────────────────────────────────────────────────────

describe('setLastfmConfig', () => {
  it('PUTs /api/v1/admin/integrations/lastfm with { apiKey, apiSecret } and returns void', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response(null, { status: 204 })),
    )
    const result = await setLastfmConfig({ apiKey: 'k', apiSecret: 's' })
    expect(lastCall()[0]).toBe('/api/v1/admin/integrations/lastfm')
    expect(lastCall()[1].method).toBe('PUT')
    const sentBody = JSON.parse(lastCall()[1].body as string)
    expect(sentBody.apiKey).toBe('k')
    expect(sentBody.apiSecret).toBe('s')
    expect(result).toBeNull()
  })
})
